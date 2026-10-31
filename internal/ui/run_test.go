package ui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/shell"
)

// lockedBuffer is a buffer that the program and the test can use at the same time.
type lockedBuffer struct {
	mu  chan struct{}
	buf bytes.Buffer
}

// newLockedBuffer returns an empty buffer.
func newLockedBuffer() *lockedBuffer {
	b := &lockedBuffer{mu: make(chan struct{}, 1)}
	b.mu <- struct{}{}

	return b
}

// Write implements io.Writer.
//
// Parameters:
//   - p: bytes to append
func (b *lockedBuffer) Write(p []byte) (int, error) {
	<-b.mu
	defer func() { b.mu <- struct{}{} }()

	return b.buf.Write(p)
}

// String returns what was written.
func (b *lockedBuffer) String() string {
	<-b.mu
	defer func() { b.mu <- struct{}{} }()

	return b.buf.String()
}

// runWith runs the interface on a pipe, sends keys after a pause and returns the outcome.
//
// Parameters:
//   - t: running test
//   - s: sandbox giving the options
//   - keys: bytes typed, each string separated by a short pause
func runWith(t *testing.T, s *sandbox, keys ...string) (Result, error, string) {
	t.Helper()

	return runPaced(t, s, 120*time.Millisecond, keys...)
}

// waitMark starts a string of keys that is not typed: the text after it is waited for in the output
// of the interface, for at most waitLimit, before the next key is typed.
const waitMark = "\x00"

// waitLimit is the longest runPaced waits for a text of the output.
const waitLimit = 20 * time.Second

// runPaced is runWith with the pause before each key given. A string that starts with waitMark
// makes it wait for the text that follows in the output, so that a key which must come after the
// end of a write does not depend on how fast the machine is.
//
// Parameters:
//   - t: running test
//   - s: sandbox giving the options
//   - pause: delay before each string of keys
//   - keys: bytes typed
func runPaced(t *testing.T, s *sandbox, pause time.Duration,
	keys ...string,
) (Result, error, string) {
	t.Helper()

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = pr.Close() }()

	out := newLockedBuffer()
	o := s.Opts
	o.Input, o.Output = pr, out

	go func() {
		for _, k := range keys {
			time.Sleep(pause)

			if text, wait := strings.CutPrefix(k, waitMark); wait {
				for end := time.Now().Add(waitLimit); time.Now().Before(end) &&
					!strings.Contains(stripStyles(out.String()), text); {
					time.Sleep(10 * time.Millisecond)
				}

				continue
			}

			if _, err := pw.Write([]byte(k)); err != nil {
				return
			}
		}
	}()

	type outcome struct {
		res Result
		err error
	}

	done := make(chan outcome, 1)

	go func() {
		res, err := Run(o)
		done <- outcome{res, err}
	}()

	select {
	case r := <-done:
		_ = pw.Close()
		return r.res, r.err, out.String()
	case <-time.After(10 * time.Second):
		_ = pw.Close()
		t.Fatal("Run did not return")
	}

	return Result{}, nil, ""
}

// TestRunQuit runs the real program loop on a pipe and quits with q.
//
// Parameters:
//   - t: test to report to
func TestRunQuit(t *testing.T) {
	res, err, out := runWith(t, newSandbox(t), "q")

	if err != nil || !res.Empty() {
		t.Fatalf("Run = %+v, %v", res, err)
	}

	if !strings.Contains(out, "Connections") || !strings.Contains(out, "\x1b[?1049h") {
		t.Errorf("nothing was drawn on the alternate screen: %q", out)
	}
}

// TestRunCtrlD checks the quit by Ctrl-D, which is not an interruption.
//
// Parameters:
//   - t: test to report to
func TestRunCtrlD(t *testing.T) {
	if res, err, _ := runWith(t, newSandbox(t), "\x04"); err != nil || !res.Empty() {
		t.Errorf("Run = %+v, %v", res, err)
	}
}

// TestRunChangeDir opens the repository list and presses Enter on the first repository.
//
// Parameters:
//   - t: test to report to
func TestRunChangeDir(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) { o.Start = StartRepos })

	res, err, _ := runWith(t, s, "\r")

	if err != nil || res.Action != shell.ChangeDir || res.Payload != s.Repo {
		t.Fatalf("Run = %+v, %v", res, err)
	}

	if h := res.Handoff(); h.Action != shell.ChangeDir || h.Payload != s.Repo || h.Reload {
		t.Errorf("handoff = %+v", h)
	}
}

// TestRunReloadOnQuit writes an alias through the form and leaves with q: the result must ask
// the wrapper to load the shell definitions again, as it does when the interface is left by an
// action.
//
// Parameters:
//   - t: test to report to
func TestRunReloadOnQuit(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) { o.Start = StartAliases })

	res, err, _ := runPaced(t, s, 60*time.Millisecond, "n", "zz", "\t", "echo hi", "\x13",
		waitMark+`saved alias "zz"`, "q")
	if err != nil || res.Action != shell.None && res.Action != "" {
		t.Fatalf("Run = %+v, %v", res, err)
	}

	if !res.Reload || res.Empty() {
		t.Errorf("Run = %+v, want a reload request", res)
	}

	if h := res.Handoff(); !h.Reload || h.Action != shell.None {
		t.Errorf("handoff = %+v", h)
	}
}

// TestRunConnect runs a connection through the fake runner and comes back to the list.
//
// Parameters:
//   - t: test to report to
func TestRunConnect(t *testing.T) {
	run := &fakeRunner{Out: "FAKE SSH\n"}
	s := newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.Runner = run
	})

	res, err, out := runWith(t, s, "\r", "q")

	if err != nil || !res.Empty() {
		t.Fatalf("Run = %+v, %v", res, err)
	}

	calls := run.calls()
	if len(calls) != 1 || strings.Join(calls[0].Argv, " ") != "ssh -l deploy -- prod.example.com" {
		t.Fatalf("calls = %+v", calls)
	}

	if !strings.Contains(out, "-> ssh -l deploy -- prod.example.com") &&
		!strings.Contains(out, "\u2192 ssh -l deploy -- prod.example.com") {
		t.Errorf("the banner was not printed: %q", out)
	}
}

// TestRunInterrupted sends SIGINT to the process while the interface waits for a key.
//
// Parameters:
//   - t: test to report to
func TestRunInterrupted(t *testing.T) {
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()

	res, err, _ := runWith(t, newSandbox(t), "")

	if !errors.Is(err, ErrInterrupted) || !res.Empty() {
		t.Errorf("Run = %+v, %v, want ErrInterrupted", res, err)
	}
}

// TestInterruptTo checks that an interrupt becomes the message of the ordinary quit and that
// every other message is passed on as it is.
//
// Parameters:
//   - t: test to report to
func TestInterruptTo(t *testing.T) {
	var got []tea.Msg

	send := interruptTo(func(m tea.Msg) { got = append(got, m) })

	send(tea.InterruptMsg{})
	send(tea.QuitMsg{})
	send(toastMsg{Text: "x"})

	if len(got) != 3 {
		t.Fatalf("messages = %v, want 3", got)
	}

	if _, ok := got[0].(interruptMsg); !ok {
		t.Errorf("first message = %T, want interruptMsg", got[0])
	}

	if _, ok := got[1].(tea.QuitMsg); !ok {
		t.Errorf("second message = %T, want QuitMsg", got[1])
	}

	if m, ok := got[2].(toastMsg); !ok || m.Text != "x" {
		t.Errorf("third message = %#v, want the toast unchanged", got[2])
	}
}

// TestInterruptMsg checks that the interrupt message of the root quits and is remembered.
//
// Parameters:
//   - t: test to report to
func TestInterruptMsg(t *testing.T) {
	g := newRig(t, newSandbox(t))

	g.send(interruptMsg{})

	if !g.Quit || !g.r.interrupted {
		t.Errorf("quit = %v, interrupted = %v, want both set", g.Quit, g.r.interrupted)
	}
}

// TestRunPanic checks that a crash restores the terminal and is reported as an error.
//
// Parameters:
//   - t: test to report to
func TestRunPanic(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) {
		o.Environ = append(o.Environ, "TOTE_DEBUG_PANIC=1")
	})

	// The input is not a file: Bubble Tea then reads it without the epoll based reader, whose
	// shutdown races with its own read loop when a program is killed, as a crash does.
	pr, pw := io.Pipe()
	out := newLockedBuffer()
	o := s.Opts
	o.Input, o.Output = pr, out

	go func() {
		time.Sleep(120 * time.Millisecond)
		_, _ = pw.Write([]byte("j"))
	}()

	_, err := Run(o)

	_ = pw.Close()

	if err == nil {
		t.Fatal("a crash was not reported")
	}

	got := out.String()

	if !strings.Contains(got, "\x1b[?1049l") || !strings.Contains(got, "\x1b[?25h") {
		t.Errorf("the terminal was not restored: %q", got)
	}
}

// TestOptionsDefaults checks the values filled in for a minimal call.
//
// Parameters:
//   - t: test to report to
func TestOptionsDefaults(t *testing.T) {
	o := Options{Environ: []string{"LOGNAME=bob"}}.withDefaults()

	if o.Printer == nil || o.Now == nil || o.Runner == nil || o.TmuxFeatures == nil ||
		o.User != "bob" {
		t.Errorf("defaults = %+v", o)
	}

	o = Options{Environ: []string{"USER=ann", "LOGNAME=bob"}}.withDefaults()

	if o.User != "ann" {
		t.Errorf("user = %q, want USER first", o.User)
	}

	if os.Getenv("HOME") != "" {
		if got := (Options{}).withDefaults(); len(got.Environ) == 0 {
			t.Error("a nil environment must default to the process one")
		}
	}

	keep := Options{User: "set"}.withDefaults()

	if keep.User != "set" {
		t.Errorf("an explicit user was replaced: %q", keep.User)
	}
}

// TestResult checks the conversion of a result into a hand-off.
//
// Parameters:
//   - t: test to report to
func TestResult(t *testing.T) {
	tests := []struct {
		name  string
		r     Result
		empty bool
		want  shell.Handoff
	}{
		{"zero", Result{}, true, shell.Handoff{Action: shell.None}},
		{"none", Result{Action: shell.None}, true, shell.Handoff{Action: shell.None}},
		{"reload", Result{Reload: true}, false, shell.Handoff{Action: shell.None, Reload: true}},
		{
			"insert",
			Result{Action: shell.Insert, Payload: "ls"},
			false,
			shell.Handoff{Action: shell.Insert, Payload: "ls"},
		},
	}

	for _, tt := range tests {
		if tt.r.Empty() != tt.empty || tt.r.Handoff() != tt.want {
			t.Errorf("%s: empty %v handoff %+v", tt.name, tt.r.Empty(), tt.r.Handoff())
		}
	}
}

// TestStartScreens checks the first screen of each start value.
//
// Parameters:
//   - t: test to report to
func TestStartScreens(t *testing.T) {
	want := map[Start]kind{
		StartConnections: kindConn, StartRepos: kindRepo,
		StartAliases: kindAlias, StartFuncs: kindFunc,
	}

	for start, k := range want {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = start }))

		if l, ok := g.r.scr.(*listScreen); !ok || l.kind != k {
			t.Errorf("start %d opened %T", start, g.r.scr)
		}
	}

	if _, ok := newRig(t, newSandbox(t)).r.scr.(*homeScreen); !ok {
		t.Error("the default start is not the welcome page")
	}
}
