package ui

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestReviewE2ETinyIgnoresActions pins that a terminal too small to draw anything must not run
// programs or open forms blind: only quitting should be possible.
//
// Parameters:
//   - t: test to report to
func TestReviewE2ETinyIgnoresActions(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"enter starts ssh", []string{"enter"}},
		{"n opens a form", []string{"n"}},
	}

	for _, tt := range tests {
		s := newSandbox(t).with(func(o *Options) { o.Start = StartConnections })
		g := newRig(t, s)
		g.resize(minWidth-1, minHeight+1)
		g.press(tt.keys...)

		if g.r.guard.busy.Load() != 0 || len(g.r.overlays) != 0 {
			t.Errorf("%s: busy %d, overlays %d while the screen shows only the too small message",
				tt.name, g.r.guard.busy.Load(), len(g.r.overlays))
		}
	}
}

// TestReviewE2EExactHandleConnects pins the contract rule for the connections filter: Enter
// applies the filter and puts the cursor on the connection whose name or alias is exactly the
// query, even if other rows match as a substring, and a second Enter connects to it.
//
// Parameters:
//   - t: test to report to
func TestReviewE2EExactHandleConnects(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))

	g.press("/")
	g.typeText("p")

	if n := len(g.r.scr.(*listScreen).shown); n < 2 {
		t.Fatalf("fixture drift: %d rows match, want several", n)
	}

	g.press("enter")

	if g.r.guard.busy.Load() != 0 || g.r.scr.(*listScreen).Typing() {
		t.Error("the first Enter must only apply the filter")
	}

	g.press("enter")

	if g.r.guard.busy.Load() != 1 {
		t.Error("the second Enter on the exact alias p did not connect to prod")
	}
}

// TestReviewE2ECtrlDQuits pins the manual test sheet (section B): q and Ctrl-D quit. Ctrl-C no
// longer quits at once, see TestQuitStateMachine.
//
// Parameters:
//   - t: test to report to
func TestReviewE2ECtrlDQuits(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))

	g.press("ctrl+d")

	if !g.Quit {
		t.Error("ctrl+d did not quit")
	}
}

// TestReviewE2EWelcomeWithoutUser checks the greeting when the user name is unknown.
//
// Parameters:
//   - t: test to report to
func TestReviewE2EWelcomeWithoutUser(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.User = "" }))

	for _, l := range strings.Split(g.text(), "\n") {
		if strings.Contains(l, "Welcome") && strings.Contains(l, "Welcome,") {
			t.Errorf("greeting with a dangling comma: %q", strings.TrimSpace(l))
		}
	}
}

// TestReviewE2EWelcomeBrokenFile checks that the welcome counts do not claim "0 connections" for a
// file that could not be read: the list says the data is unavailable, the counts must not lie.
//
// Parameters:
//   - t: test to report to
func TestReviewE2EWelcomeBrokenFile(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, filepath.Join(s.Opts.Paths.Config, "connections.toml"), "[[connection\nname=\n")

	g := newRig(t, s)

	if strings.Contains(g.text(), "0 connections") {
		t.Errorf("welcome counts a broken file as empty:\n%s", g.text())
	}
}

// TestReviewE2ESaveDoesNotFreezeOnLock pins that a store write waiting for a lock held by another
// process must not block the event loop: Ctrl-C and redraws have to keep working.
//
// Parameters:
//   - t: test to report to
func TestReviewE2ESaveDoesNotFreezeOnLock(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) { o.Start = StartConnections })
	g := newRig(t, s)

	g.press("n")
	g.typeText("lockme")
	g.press("tab")
	g.typeText("lock.example.com")

	lock := filepath.Join(s.Opts.Paths.Config, "connections.toml.lock")
	f, err := openLock(lock)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	done := make(chan struct{})

	go func() {
		g.press("ctrl+s")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Update blocked on a lock held by another process (no timeout, no feedback)")
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		<-done
	}
}

// openLock opens a lock file and takes an exclusive flock on it, as a slow external writer would.
//
// Parameters:
//   - path: lock file to create
func openLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()

		return nil, err
	}

	return f, nil
}
