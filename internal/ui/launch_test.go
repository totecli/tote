package ui

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/totecli/tote/internal/i18n"
)

// TestReviewTerminalDoubleActivate checks that a second Enter that reaches the interface before
// the first program has started does not start a second connection. Two key presses read in
// one chunk (a double tap on a slow link, a paste, a script) are both handled before Bubble Tea
// runs the exec command of the first one, so the guard is armed twice and ssh runs twice.
//
// Parameters:
//   - t: test to report to
func TestReviewTerminalDoubleActivate(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))

	g.press("enter", "enter")

	if n := g.r.guard.busy.Load(); n != 1 {
		t.Errorf("two Enter presses armed the guard %d times, want one program", n)
	}
}

// TestReviewTerminalSignalWording checks the pause line of a program that a signal killed: the
// signal is shown by its fixed name (SIGKILL) and not by the English word that Go gives
// ("killed", "terminated"), which the sentence around it would repeat.
//
// Parameters:
//   - t: test to report to
func TestReviewTerminalSignalWording(t *testing.T) {
	tests := []struct {
		name string
		sig  syscall.Signal
	}{
		{"kill", syscall.SIGKILL},
		{"term", syscall.SIGTERM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newExecCommand(&fakeRunner{}, RunSpec{}, "", "ssh host", i18n.New("en"),
				time.Now, 0)
			c.res = RunResult{ExitCode: 128 + int(tt.sig), Signal: signalName(tt.sig)}

			line := c.pauseLine()

			if !strings.Contains(line, "killed by SIG") ||
				strings.Contains(line, "killed by "+tt.sig.String()) {
				t.Errorf("pause line does not name the signal: %q", line)
			}
		})
	}
}

// TestReviewTerminalChildFds pins the close-on-exec rule of the spike: a descriptor above 2 that
// the interface holds is not inherited by the program it starts.
//
// Parameters:
//   - t: test to report to
func TestReviewTerminalChildFds(t *testing.T) {
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("needs /proc")
	}

	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	high, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), 0, 60)
	if errno != 0 {
		t.Fatalf("cannot duplicate a descriptor: %v", errno)
	}

	defer func() { _ = syscall.Close(int(high)) }()

	var out bytes.Buffer

	spec := RunSpec{Argv: []string{"sh", "-c", "ls /proc/$$/fd"}, Env: os.Environ(), Stdout: &out}
	c := newExecCommand(osRunner{}, spec, "", "ls", i18n.New("en"), time.Now, 0)

	if err := c.Run(); err != nil {
		t.Fatal(err)
	}

	for _, name := range strings.Fields(out.String()) {
		if name == strconv.Itoa(int(high)) {
			t.Errorf("the program inherited descriptor %s: %q", name, out.String())
		}
	}

	if c.res.ExitCode != 0 || c.res.Err != nil {
		t.Errorf("program failed: %+v", c.res)
	}
}
