package ui

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/shell"
)

const (
	// execDelay is the pause before a child process is started. It lets the renderer of
	// Bubble Tea stop its ticker before the terminal is released, which avoids a screen that
	// stays frozen after a command that returns at once. Remove it when the upstream issue
	// (charmbracelet/bubbletea #1778) is fixed.
	execDelay = 2 * time.Millisecond

	// pauseFromCode is the lowest exit status that makes the interface wait for Enter before
	// it redraws: 126 and 127 (cannot run, not found), 255 (ssh error) and 128 plus a signal.
	pauseFromCode = 126

	// hangupGap is the time a read of the raw terminal takes to give up when nothing is typed (a
	// tenth of a second). An end of file that comes much sooner is a terminal that hung up.
	hangupGap = 50 * time.Millisecond

	// hangupReads is how many quick ends of file in a row make the pause give up, since a
	// terminal that hung up never comes back.
	hangupReads = 5

	// modeReset puts the modes a child may have left on back to their defaults: mouse
	// reporting, focus reporting, bracketed paste, application cursor keys and keypad, the
	// cursor, the text attributes, the keyboard protocol flags and modifyOtherKeys. It never
	// leaves the alternate screen: on tmux 3.4 that restores an old cursor position.
	modeReset = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1005l\x1b[?1006l\x1b[?1015l" +
		"\x1b[?1004l\x1b[?2004l\x1b[?1l\x1b>\x1b[?25h\x1b[0m\x1b[<u\x1b[>4m"
)

// PausePolicy says when the interface waits for Enter after a program that owned the terminal.
//
// The zero value is PauseError, the rule of every program that tote starts for itself.
type PausePolicy int

const (
	// PauseError waits only after a program that could not run, ended on a signal or exited with
	// a status of pauseFromCode or more. It is the zero value.
	PauseError PausePolicy = iota

	// PauseAlways waits after every program, whatever its status.
	PauseAlways

	// PauseNever does not wait, whatever the status. The failure still shows as a notice once the
	// interface is back.
	PauseNever
)

// ParsePause turns the name of a pause policy into the policy.
//
// Parameters:
//   - name: "error", "always" or "never"
//
// Returns the policy and true, or PauseError and false for any other name, the empty one
// included.
func ParsePause(name string) (PausePolicy, bool) {
	switch name {
	case "error":
		return PauseError, true
	case "always":
		return PauseAlways, true
	case "never":
		return PauseNever, true
	}

	return PauseError, false
}

// RunSpec describes a program to run in the foreground of the terminal.
//
// Argv is the command and its arguments, Env the environment in KEY=VALUE form, and Stdin,
// Stdout and Stderr the streams the program is connected to. Dir is the working directory of
// the program, empty for the one of tote; a directory that cannot be entered is a failure to start
// that is never taken for a program that is not found. Pause chooses when the interface waits for
// Enter once the program ended, see PausePolicy. Flush, when true, discards the keys that are
// waiting on the terminal before the program starts and again when it ended, before the pause
// line, so that what was typed during the program is not replayed into the interface; it only
// acts when Stdin is a terminal.
type RunSpec struct {
	Argv   []string
	Env    []string
	Dir    string
	Pause  PausePolicy
	Flush  bool
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// RunResult is what happened to a program.
//
// ExitCode is the exit status, 128 plus the signal number for a program killed by a signal and
// 127 when it could not be started. Signal names the signal, empty for a normal exit. Err is
// set only when the program could not be started.
type RunResult struct {
	ExitCode int
	Signal   string
	Err      error
}

// Runner starts a program and waits for it. The interface exists so that tests can replace the
// real process by a fake.
type Runner interface {
	Run(spec RunSpec) RunResult
}

// osRunner starts real processes.
type osRunner struct{}

// Run implements Runner. The program stays in the process group of tote, so that Ctrl-C and
// Ctrl-Z behave as they do for any job of the shell and a hang-up reaches every process. It runs
// in spec.Dir when that is not empty; a directory that cannot be entered gives exit status 127
// and an error that shell.IsMissingProgram does not recognise, so that it is never explained as
// a program that is not found.
//
// Parameters:
//   - spec: program to run
func (osRunner) Run(spec RunSpec) RunResult {
	if len(spec.Argv) == 0 {
		return RunResult{ExitCode: 127, Err: errors.New("empty command")}
	}

	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Env = spec.Env
	cmd.Dir = spec.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = spec.Stdin, spec.Stdout, spec.Stderr

	err := cmd.Run()
	if err == nil {
		return RunResult{}
	}

	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return RunResult{ExitCode: 127, Err: dirFailure(spec.Dir, err)}
	}

	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return RunResult{ExitCode: 128 + int(ws.Signal()), Signal: signalName(ws.Signal())}
	}

	return RunResult{ExitCode: ee.ExitCode()}
}

// dirError is the failure to enter the working directory of a program. It has no Unwrap, so that
// a missing directory does not satisfy shell.IsMissingProgram.
type dirError struct {
	msg string
}

// Error implements error.
func (d dirError) Error() string { return d.msg }

// dirFailure marks the error of a program that did not start because of its working directory.
//
// Parameters:
//   - dir: working directory of the spec, empty for none
//   - err: error returned when the program was started
//
// Returns err itself unless it is the failure to change into dir.
func dirFailure(dir string, err error) error {
	var pe *fs.PathError

	if dir != "" && errors.As(err, &pe) && pe.Op == "chdir" {
		return dirError{err.Error()}
	}

	return err
}

// signalName returns the language independent name of a signal, such as SIGKILL.
//
// Parameters:
//   - sig: signal that ended a program
//
// Returns "signal N" for a signal without a well known name.
func signalName(sig syscall.Signal) string {
	names := map[syscall.Signal]string{
		syscall.SIGHUP:  "SIGHUP",
		syscall.SIGINT:  "SIGINT",
		syscall.SIGQUIT: "SIGQUIT",
		syscall.SIGABRT: "SIGABRT",
		syscall.SIGKILL: "SIGKILL",
		syscall.SIGSEGV: "SIGSEGV",
		syscall.SIGPIPE: "SIGPIPE",
		syscall.SIGALRM: "SIGALRM",
		syscall.SIGTERM: "SIGTERM",
	}

	if name, ok := names[sig]; ok {
		return name
	}

	return "signal " + strconv.Itoa(int(sig))
}

// execCommand is the command handed to tea.Exec in place of tea.ExecProcess.
//
// It adds to the plain process what the interface needs to get the terminal back: a short delay
// before the start, close-on-exec on every descriptor above 2, the reset of the terminal modes
// after the program, the flush of the keys typed meanwhile when the spec asks for it, and its own
// "press enter" pause that reads a line from the terminal, as the pause policy of the spec wants.
// The outcome is kept in res and dur for the callback of tea.Exec. Via is true for the connection
// of a via program, whose failure to start is then explained. Gate, when set, is asked
// immediately before the program starts and refuses it by returning an error, which is kept in
// denied: nothing is printed, started or paused then.
type execCommand struct {
	runner Runner
	spec   RunSpec
	banner string
	via    bool
	tr     *i18n.Printer
	label  string
	now    func() time.Time
	delay  time.Duration
	stop   func() bool
	waker  func() *os.File
	gate   func() error
	denied error
	res    RunResult
	dur    time.Duration
}

// newExecCommand prepares a program for tea.Exec.
//
// Parameters:
//   - runner: starts the program
//   - spec: program and environment; the streams are filled by Bubble Tea when they are nil
//   - banner: line printed before the program starts, usually the exact command
//   - label: sanitized command shown in the pause line
//   - tr: printer for the pause line
//   - now: clock, used to time the program
//   - delay: wait before the start, see execDelay
func newExecCommand(
	runner Runner, spec RunSpec, banner, label string, tr *i18n.Printer, now func() time.Time,
	delay time.Duration,
) *execCommand {
	return &execCommand{
		runner: runner, spec: spec, banner: banner, label: label, tr: tr,
		now: now, delay: delay,
	}
}

// SetStdin implements tea.ExecCommand.
//
// Parameters:
//   - r: input of the interface, used when the spec has none
func (c *execCommand) SetStdin(r io.Reader) {
	if c.spec.Stdin == nil {
		c.spec.Stdin = r
	}
}

// SetStdout implements tea.ExecCommand.
//
// Parameters:
//   - w: output of the interface, used when the spec has none
func (c *execCommand) SetStdout(w io.Writer) {
	if c.spec.Stdout == nil {
		c.spec.Stdout = w
	}
}

// SetStderr implements tea.ExecCommand.
//
// Parameters:
//   - w: error stream of the interface, used when the spec has none
func (c *execCommand) SetStderr(w io.Writer) {
	if c.spec.Stderr == nil {
		c.spec.Stderr = w
	}
}

// pauseNeeded reports whether the output of a program must stay on screen until Enter by the
// rule of PauseError.
//
// Parameters:
//   - r: result of the program
func pauseNeeded(r RunResult) bool {
	return r.Err != nil || r.ExitCode >= pauseFromCode
}

// pauses reports whether a policy waits for Enter after a program.
//
// Parameters:
//   - p: policy chosen by the caller
//   - r: result of the program
func (p PausePolicy) pauses(r RunResult) bool {
	switch p {
	case PauseAlways:
		return true
	case PauseNever:
		return false
	}

	return pauseNeeded(r)
}

// Run implements tea.ExecCommand. It always returns nil: the outcome of the program goes
// through res, and an error here would only make Bubble Tea skip the pause.
func (c *execCommand) Run() error {
	time.Sleep(c.delay)
	closeOnExecAbove(2)

	if c.gate != nil {
		if err := c.gate(); err != nil {
			c.denied = err

			return nil
		}
	}

	out := c.spec.Stdout
	if c.banner != "" {
		_, _ = io.WriteString(out, c.banner+"\n")
	}

	var back func()

	in, _ := c.spec.Stdin.(*os.File)
	if in != nil {
		back = holdMode(in)

		if c.spec.Flush {
			_ = flushInput(in)
		}
	}

	start := c.now()
	c.res = c.runner.Run(c.spec)
	c.dur = c.now().Sub(start)

	if back != nil {
		back()
	}

	_, _ = io.WriteString(out, modeReset)

	if in != nil && c.spec.Flush {
		_ = flushInput(in)
	}

	if c.spec.Pause.pauses(c.res) {
		_, _ = io.WriteString(out, c.pauseLine()+"\n")
		readLine(c.spec.Stdin, c.stop, c.wakeFile())
	}

	return nil
}

// wakeFile returns the file that is written to when the pause must look at its stop function
// again, nil when there is none.
func (c *execCommand) wakeFile() *os.File {
	if c.waker == nil {
		return nil
	}

	return c.waker()
}

// pauseLine builds the line that asks the user to press Enter.
func (c *execCommand) pauseLine() string {
	switch {
	case c.res.Err != nil && c.via && shell.IsMissingProgram(c.res.Err):
		return c.tr.T("ui_exec_novia", c.label, sanitize(c.res.Err.Error()),
			c.tr.T("cli_via_hint"))
	case c.res.Err != nil:
		return c.tr.T("ui_exec_failed", c.label, sanitize(c.res.Err.Error()))
	case c.res.Signal != "":
		return c.tr.T("ui_exec_signal", c.label, sanitize(c.res.Signal),
			c.dur.Round(time.Millisecond).String())
	}

	return c.tr.T("ui_exec_exit", c.label, strconv.Itoa(c.res.ExitCode),
		c.dur.Round(time.Millisecond).String())
}

// readLine waits for the user to press Enter.
//
// Parameters:
//   - in: input of the terminal. On a terminal the wait is done in raw mode, so that Ctrl-C and
//     Ctrl-D arrive as keys and end it like Enter, instead of a signal that nobody would act
//     on; the mode is put back before the function returns and no echo is drawn meanwhile.
//     Any other file is read one byte at a time until a line break, so that nothing typed after
//     it is consumed, and a reader that is not a file returns at its first line or its end.
//   - stop: ends the wait on a terminal when it returns true; nil never ends it. It is asked
//     whenever a byte arrives on wake, and between keys; without a wake file the terminal is
//     read with a timeout of a tenth of a second and asked after each timeout
//   - wake: read end of a pipe that is written to when stop must be asked again, so that the wait
//     does not poll; nil when there is none
func readLine(in io.Reader, stop func() bool, wake *os.File) {
	if in == nil {
		return
	}

	f, ok := in.(*os.File)
	if !ok {
		_, _ = bufio.NewReader(in).ReadString('\n')

		return
	}

	if restore := rawInput(f); restore != nil {
		defer restore()

		if stop == nil {
			stop = func() bool { return false }
		}

		waitKey(f, pauseKeys, stop, wake)

		return
	}

	waitKey(f, "\n", nil, nil)
}

// waitKey reads a file one byte at a time until one of the given bytes shows up.
//
// Parameters:
//   - f: input to read
//   - ends: bytes that end the wait; a read error ends it too, and so do a few ends of file in a
//     row that come at once, which is how a terminal that hung up reads
//   - stop: asked before each key, and after a byte on wake or, without wake, after each read
//     that gives up on a terminal after a tenth of a second (see makeRaw) and shows as an end of
//     file; it ends the wait by returning true. With nil, an end of file ends the wait as any
//     other error does.
//   - wake: read end of a pipe, nil to read with a timeout. While nothing happens the wait is
//     blocked in the system and does not wake up at all; a byte on wake makes it ask stop.
func waitKey(f *os.File, ends string, stop func() bool, wake *os.File) {
	var (
		b    [1]byte
		gone int
	)

	for {
		if stop != nil && wake != nil && stop() {
			return
		}

		if wake != nil {
			key, err := awaitKey(f, wake)

			switch {
			case err != nil:
				wake = nil
			case !key:
				continue
			}
		}

		began := time.Now()
		n, err := f.Read(b[:])

		switch {
		case n == 1 && strings.IndexByte(ends, b[0]) >= 0:
			return
		case err == nil:
			gone = 0
		case stop == nil || !errors.Is(err, io.EOF) || stop():
			return
		case time.Since(began) >= hangupGap:
			gone = 0
		default:
			if gone++; gone >= hangupReads {
				return
			}
		}
	}
}

// describeExit formats the status of a program for a toast.
//
// Parameters:
//   - tr: printer
//   - label: sanitized command
//   - r: result of the program
//   - via: true for the connection of a via program, whose program that is not found gets the
//     explanation of what a via must be
//
// Returns an empty string for a normal exit.
func describeExit(tr *i18n.Printer, label string, r RunResult, via bool) string {
	switch {
	case r.Err != nil && via && shell.IsMissingProgram(r.Err):
		return tr.T("cli_exec_novia", label, sanitize(r.Err.Error()), tr.T("cli_via_hint"))
	case r.Err != nil:
		return tr.T("cli_exec_failed", label, sanitize(r.Err.Error()))
	case r.Signal != "":
		return tr.T("ui_toast_signal", label, sanitize(r.Signal))
	case r.ExitCode != 0:
		return tr.T("ui_toast_exit", label, strconv.Itoa(r.ExitCode))
	}

	return ""
}

// closeOnExecAbove marks every open descriptor above a number close-on-exec, so that a child
// process does not inherit the descriptors of the input reader of Bubble Tea. Errors are
// ignored: a descriptor that cannot be marked is only a leak.
//
// Parameters:
//   - lowest: descriptors up to and including this one are left alone
func closeOnExecAbove(lowest int) {
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return
	}

	for _, ent := range entries {
		if n, err := strconv.Atoi(ent.Name()); err == nil && n > lowest {
			syscall.CloseOnExec(n)
		}
	}
}
