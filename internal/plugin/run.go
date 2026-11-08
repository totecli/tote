package plugin

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
)

// Limits of a captured run, section 5.4.
const (
	pagerCap  = 8 << 20
	drainCap  = 64 << 20
	tailCap   = 4 << 10
	pipeDrain = 250 * time.Millisecond
	drainMax  = 5 * time.Second
	openWait  = 3 * time.Second
	readChunk = 32 << 10
	truncated = "[output truncated]"
)

// Event types a run emits.
const (
	EventOutput = "output"
	EventNotice = "notice"
)

// Event is a piece of what a run shows. Text is cleaned and redacted: it holds no control
// character except line feed and tab, and no escape sequence except the colour sequences of the
// pager whitelist.
type Event struct {
	Type string
	Text string
}

// Statuses of an Outcome.
const (
	OutcomeOK         = "ok"
	OutcomeFailed     = "failed"
	OutcomeSignaled   = "signaled"
	OutcomeTimeout    = "timeout"
	OutcomeCancelled  = "cancelled"
	OutcomeRefused    = "refused"
	OutcomeNotStarted = "not_started"
)

// Outcome is how a run ended.
//
// Status is one of the Outcome constants. Exit is the exit status of the program, -1 when it did
// not exit by itself. Signal names the signal that ended it, such as TERM, when one did. Err says
// why a run was refused or could not start, or why it was stopped (output_limit). Tail is the last
// 4 KiB of the output, cleaned, for the message of a failure. Truncated tells that the output went
// past the 8 MiB the pager shows. Bytes is the amount the program wrote. A declarative run never
// counts toward quarantine.
type Outcome struct {
	Status    string
	Exit      int
	Signal    string
	Err       *Error
	Tail      string
	Truncated bool
	Duration  time.Duration
	Bytes     int64
}

// ExitCode gives the exit code of the command line for the outcome: 0 for a run that completed,
// 130 for a cancel, 127 when the program could not start, the class of the error for a refusal,
// and 1 for every other failure.
func (o Outcome) ExitCode() int {
	switch o.Status {
	case OutcomeOK:
		return 0
	case OutcomeCancelled:
		return 130
	case OutcomeNotStarted:
		return ExitCannotStart
	case OutcomeRefused:
		if o.Err != nil {
			return ExitClass(o.Err.Code)
		}
	}

	return ExitFailure
}

// afterStart is called, when set, between the start of a program and its registration in
// plugin-procs. Tests use it to change the state of the plugin in that window.
var afterStart func()

// Guard is the check that comes immediately before a program starts: the state of the plugin is
// computed again from the files as they are now, and the run must still be the one that was
// prepared. A terminal run is started by the host's own exec path, which calls Guard right before.
//
// Parameters:
//   - p: prepared run
//
// Returns nil, or an *Error: not_runnable when the plugin is no longer enabled (a disable, a
// remove, a changed tree or consent, --no-plugins), stale when the tree or the surface is not the
// one the run was prepared from, or when the program was replaced.
func (r *Registry) Guard(p Prepared) *Error {
	cur, ok := r.Recheck(p.PluginID)
	if !ok || !cur.State.Runnable() {
		if !ok {
			return newError(CodeNotRunnable, p.PluginID, "absent")
		}

		return notRunnable(cur)
	}

	if cur.Record.TreeSHA256 != p.tree || cur.Surface != p.surface {
		return newError(CodeStale, p.PluginID, "tree")
	}

	if len(p.Argv) > 0 {
		info, err := os.Stat(p.Argv[0])
		if err != nil {
			return newError(CodeStale, p.PluginID, "program")
		}

		if id, ok := statIDOf(info); !ok || id != p.program {
			return newError(CodeStale, p.PluginID, "program")
		}
	}

	return nil
}

// Run executes a prepared captured run: output pager or silent, or an open contribution. The
// program gets no stdin, its standard output and error go to one pipe, and it runs in a session of
// its own, so that it has no controlling terminal and its descendants can be reached by group.
//
// The state is checked again immediately before the start, and once more after the process was
// recorded in plugin-procs, so that a disable made in between kills it. A run ends when the direct
// child exits, not when the pipe closes: the pipe is drained until it has been silent for 250 ms
// (5 seconds at most), then closed, and the other members of the group are left alone (a browser
// opened by the program survives). Timeout, cancel and a disable send SIGTERM to the group and
// SIGKILL after 2 seconds. At most four plugin processes run at a time in a tote process; a fifth
// start is refused at once as busy.
//
// emit receives the output as it arrives, at most 8 MiB of it. It is called from one goroutine at
// a time and has returned when Run returns. A terminal run is refused (terminal_run): it belongs
// to the host's exec path, after Guard.
//
// Parameters:
//   - ctx: cancelling it stops the run
//   - p: prepared run
//   - emit: receiver of the output, may be nil
func (r *Registry) Run(ctx context.Context, p Prepared, emit func(Event)) Outcome {
	began := time.Now()
	out := r.run(ctx, p, emit)
	out.Duration = time.Since(began)

	return out
}

// run is Run without the clock.
//
// Parameters:
//   - ctx: cancelling it stops the run
//   - p: prepared run
//   - emit: receiver of the output, may be nil
func (r *Registry) run(ctx context.Context, p Prepared, emit func(Event)) Outcome {
	if emit == nil {
		emit = func(Event) {}
	}

	switch {
	case p.IsOpen():
		return r.runOpen(ctx, p)
	case p.IsTerminal():
		return refused(newError(CodeTerminalRun, p.PluginID, p.EntryID))
	case len(p.Argv) == 0 || len(p.Env) == 0 || !utf8.ValidString(p.Dir) || p.Dir == "":
		return refused(newError(CodeProducedInvalid, "run", "unprepared"))
	case p.Confirm.Required && !p.Confirmed:
		return refused(newError(CodeConfirmNeeded, p.PluginID, p.EntryID))
	case ctx.Err() != nil:
		return Outcome{Status: OutcomeCancelled, Exit: -1}
	case !r.rt.acquire():
		return refused(newError(CodeBusy, p.PluginID))
	}

	defer r.rt.release()

	if err := r.Guard(p); err != nil {
		return refused(err)
	}

	return r.launch(ctx, p, emit)
}

// refused builds the outcome of a run that did not start because a check failed.
//
// Parameters:
//   - err: the reason
func refused(err *Error) Outcome {
	return Outcome{Status: OutcomeRefused, Exit: -1, Err: err}
}

// launch starts the program and follows it to its end.
//
// Parameters:
//   - ctx: cancelling it stops the run
//   - p: prepared run
//   - emit: receiver of the output
func (r *Registry) launch(ctx context.Context, p Prepared, emit func(Event)) Outcome {
	// The kill switch reaches a process through plugin-procs. When that file cannot be used, no
	// process starts: failing after the start would leave a program running that nothing can stop.
	pf, perr := openProcs(r.opts.Paths.ProcsFile(), r.uid())
	if perr != nil {
		return Outcome{Status: OutcomeNotStarted, Exit: -1, Err: wrapError(CodeProcsFile, perr)}
	}

	pf.close()

	pr, pw, err := os.Pipe()
	if err != nil {
		return Outcome{Status: OutcomeNotStarted, Exit: -1, Err: wrapError(CodeStartFailed, err)}
	}

	cmd := &exec.Cmd{
		Path: p.Argv[0], Args: append([]string{p.Name}, p.Argv[1:]...), Env: p.Env,
		Dir: p.Dir, SysProcAttr: &syscall.SysProcAttr{Setsid: true}, Stderr: pw,
	}

	if p.Output != OutputSilent {
		cmd.Stdout = pw
	}

	startErr := cmd.Start()
	_ = pw.Close()

	if startErr != nil {
		_ = pr.Close()

		return Outcome{Status: OutcomeNotStarted, Exit: -1, Err: wrapError(CodeStartFailed, startErr,
			p.Name)}
	}

	pid := cmd.Process.Pid
	done := make(chan error, 1)

	go func() { done <- cmd.Wait() }()

	if afterStart != nil {
		afterStart()
	}

	entry, terr := r.trackProc(p.PluginID, pid)
	if terr != nil {
		stopGroup(pid, done)
		_ = pr.Close()

		return Outcome{Status: OutcomeNotStarted, Exit: -1, Err: wrapError(CodeProcsFile, terr)}
	}

	// Registering first and checking after closes the window in which a disable could look at
	// plugin-procs before this process was in it.
	if gerr := r.Guard(p); gerr != nil {
		stopGroup(pid, done)
		_ = pr.Close()
		r.forgetIfEmpty(entry)

		return refused(gerr)
	}

	out := r.follow(ctx, p, cmd, pr, pid, done, emit)

	r.forgetIfEmpty(entry)

	return out
}

// forgetIfEmpty removes a process group from plugin-procs unless processes of its session are
// still alive, which a later disable must be able to reach.
//
// Parameters:
//   - e: entry returned by trackProc
func (r *Registry) forgetIfEmpty(e ProcEntry) {
	if !sessionAlive(e.SID, e.Start) {
		r.untrackProc(e)
	}
}

// pump copies the pipe to the output events and keeps the tail. It ends when the pipe is closed,
// at its end of file, or when the program wrote more than the drain cap.
type pump struct {
	sink      *lineSink
	emit      func(Event)
	capture   bool
	emitted   int
	total     int64
	truncated bool
	tail      tailBuf
	overflow  chan struct{}
	reads     atomic.Int64
	busy      atomic.Bool
}

// run reads the pipe until it ends.
//
// Parameters:
//   - pr: read end of the pipe
func (pu *pump) run(pr io.Reader) {
	buf := make([]byte, readChunk)

	for {
		n, err := pr.Read(buf)
		if n > 0 {
			pu.busy.Store(true)
			pu.reads.Add(1)

			pu.total += int64(n)
			pu.chunks(pu.sink.write(buf[:n]))

			pu.busy.Store(false)

			if pu.total > drainCap {
				close(pu.overflow)

				pu.overflow = nil

				return
			}
		}

		if err != nil {
			break
		}
	}

	pu.chunks(pu.sink.flush())
}

// closeWhenIdle closes the read end once the output has been silent for 250 ms: no read finished
// and no chunk being handled in that time. A receiver that is slow for a moment, or a loaded
// machine, therefore does not lose the end of the output that the program wrote before it exited,
// while a grandchild that holds the pipe without writing is still cut off after 250 ms. A
// grandchild that keeps writing is cut off after 5 seconds.
//
// Parameters:
//   - pr: read end of the pipe
//
// Returns the function that stops the watch.
func (pu *pump) closeWhenIdle(pr *os.File) func() {
	stop := make(chan struct{})

	go func() {
		began := time.Now()
		seen := pu.reads.Load()

		for {
			select {
			case <-stop:
				return
			case <-time.After(pipeDrain):
			}

			now := pu.reads.Load()
			active := now != seen || pu.busy.Load()
			seen = now

			if !active || time.Since(began) >= drainMax {
				_ = pr.Close()

				return
			}
		}
	}()

	return func() { close(stop) }
}

// chunks passes finished pieces of output to the tail and, within the cap, to the receiver.
//
// Parameters:
//   - list: pieces of text
func (pu *pump) chunks(list []string) {
	for _, c := range list {
		pu.tail.add(c)

		switch {
		case !pu.capture:
		case pu.emitted < pagerCap:
			pu.emitted += len(c)
			pu.emit(Event{Type: EventOutput, Text: c})
		case !pu.truncated:
			pu.truncated = true

			pu.emit(Event{Type: EventNotice, Text: truncated})
		}
	}
}

// tailBuf keeps the last tailCap bytes of the output.
type tailBuf struct {
	b []byte
}

// add appends text and drops the oldest bytes beyond the cap.
//
// Parameters:
//   - s: text to append
func (t *tailBuf) add(s string) {
	t.b = append(t.b, s...)

	if len(t.b) > 2*tailCap {
		t.b = append([]byte(nil), t.b[len(t.b)-tailCap:]...)
	}
}

// String returns the last tailCap bytes, starting on a rune boundary.
func (t *tailBuf) String() string {
	b := t.b
	if len(b) > tailCap {
		b = b[len(b)-tailCap:]
	}

	for len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}

	return string(b)
}

// follow waits for the end of a started program and builds the outcome.
//
// Parameters:
//   - ctx: cancelling it stops the run
//   - p: prepared run
//   - cmd: started command
//   - pr: read end of the pipe
//   - pid: process id of the leader, which is also its group and session
//   - done: receives the result of Wait
//   - emit: receiver of the output
func (r *Registry) follow(ctx context.Context, p Prepared, cmd *exec.Cmd, pr *os.File, pid int,
	done chan error, emit func(Event),
) Outcome {
	pu := &pump{
		sink: newLineSink(NewRedactor(p.secrets), !p.Mono), emit: emit,
		capture: p.Output == OutputPager, overflow: make(chan struct{}),
	}
	overflow := pu.overflow
	finished := make(chan struct{})

	go func() {
		defer close(finished)

		pu.run(pr)
	}()

	var timer <-chan time.Time

	if p.Timeout > 0 {
		t := time.NewTimer(p.Timeout)
		defer t.Stop()

		timer = t.C
	}

	status, cause := OutcomeOK, (*Error)(nil)

	select {
	case waitErr := <-done:
		_ = waitErr // the exit status is read from the process state, not from this error
	case <-ctx.Done():
		status = OutcomeCancelled

		stopGroup(pid, done)
	case <-timer:
		status = OutcomeTimeout

		stopGroup(pid, done)
	case <-overflow:
		status, cause = OutcomeFailed, newError(CodeOutputLimit, p.PluginID)

		stopGroup(pid, done)
	}

	// The program has exited, or was stopped. What it left holding the pipe gets a short time to
	// finish writing, and then the read end is closed: the run does not wait for a grandchild.
	stopDrain := pu.closeWhenIdle(pr)

	<-finished

	stopDrain()

	_ = pr.Close()

	return outcomeOf(status, cause, cmd.ProcessState, pu)
}

// outcomeOf assembles the outcome from how the run ended and the process state.
//
// Parameters:
//   - status: what the run loop decided: ok, or why it stopped the program
//   - cause: error that goes with a stop
//   - ps: state of the exited process
//   - pu: output counters
func outcomeOf(status string, cause *Error, ps *os.ProcessState, pu *pump) Outcome {
	out := Outcome{
		Status: status, Exit: -1, Err: cause, Tail: pu.tail.String(),
		Truncated: pu.truncated, Bytes: pu.total,
	}

	if ps == nil {
		return out
	}

	ws, ok := ps.Sys().(syscall.WaitStatus)

	switch {
	case ok && ws.Signaled():
		out.Signal = signalName(ws.Signal())

		if out.Status == OutcomeOK {
			out.Status = OutcomeSignaled
		}
	case ps.ExitCode() >= 0:
		out.Exit = ps.ExitCode()

		if out.Status == OutcomeOK && out.Exit != 0 {
			out.Status = OutcomeFailed
		}
	}

	return out
}

// signalName gives the short name of a signal.
//
// Parameters:
//   - s: signal that ended a process
func signalName(s syscall.Signal) string {
	names := map[syscall.Signal]string{
		syscall.SIGTERM: "TERM", syscall.SIGKILL: "KILL",
		syscall.SIGINT: "INT", syscall.SIGHUP: "HUP", syscall.SIGQUIT: "QUIT",
		syscall.SIGSEGV: "SEGV", syscall.SIGABRT: "ABRT", syscall.SIGPIPE: "PIPE",
	}

	if n, ok := names[s]; ok {
		return n
	}

	return "SIG" + itoa(int(s))
}

// stopGroup stops a process group the way cancel and timeout do: SIGTERM, then, after two seconds
// or as soon as the leader has exited, SIGKILL to what is left. It returns when the leader has
// been reaped.
//
// Parameters:
//   - pid: leader of the group
//   - done: receives the result of Wait; it is read here
func stopGroup(pid int, done chan error) {
	_ = killGroup(pid, syscall.SIGTERM)

	select {
	case err := <-done:
		done <- err
	case <-time.After(killGrace):
	}

	_ = killGroup(pid, syscall.SIGKILL)

	err := <-done

	done <- err
}

// runOpen asks the platform opener to open the URL of an open contribution. The opener is started
// with the URL as its only argument, no input and no output, in a session of its own; the host
// waits a few seconds for it and leaves it running after that, since a browser may stay in the
// foreground.
//
// Parameters:
//   - ctx: cancelling it before the start stops the run
//   - p: prepared run
func (r *Registry) runOpen(ctx context.Context, p Prepared) Outcome {
	if p.Confirm.Required && !p.Confirmed {
		return refused(newError(CodeConfirmNeeded, p.PluginID, p.EntryID))
	}

	if ctx.Err() != nil {
		return Outcome{Status: OutcomeCancelled, Exit: -1}
	}

	if _, err := CheckOpenURL(p.OpenURL, ""); err != nil || p.opener == "" {
		return refused(newError(CodeOpenInvalid, "prepared"))
	}

	if err := r.Guard(p); err != nil {
		return refused(err)
	}

	cmd := &exec.Cmd{
		Path: p.opener, Args: []string{OpenerName(r.goos()), p.OpenURL},
		Env: r.openerEnv(), SysProcAttr: &syscall.SysProcAttr{Setsid: true},
	}

	if err := cmd.Start(); err != nil {
		return Outcome{Status: OutcomeNotStarted, Exit: -1, Err: wrapError(CodeStartFailed, err,
			OpenerName(r.goos()))}
	}

	done := make(chan error, 1)

	go func() { done <- cmd.Wait() }()

	select {
	case waitErr := <-done:
		_ = waitErr // the exit status is read from the process state, not from this error

		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() != 0 {
			return Outcome{Status: OutcomeFailed, Exit: cmd.ProcessState.ExitCode()}
		}
	case <-time.After(openWait):
	case <-ctx.Done():
	}

	return Outcome{Status: OutcomeOK, Exit: 0}
}

// goos gives the operating system the opener is chosen for.
func (r *Registry) goos() string {
	if r.opts.Host.OS != "" {
		return r.opts.Host.OS
	}

	return runtime.GOOS
}

// openerEnv builds the environment of the platform opener from the desktop variables of tote.
func (r *Registry) openerEnv() []string {
	env := map[string]string{}

	for _, name := range desktopEnv {
		if v, ok := r.lookup(name); ok && validEnvName(name) {
			env[name] = v
		}
	}

	return flattenEnv(env)
}
