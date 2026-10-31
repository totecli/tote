package ui

import (
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestGuardClassify checks what each signal does before, during and after a program.
//
// Parameters:
//   - t: test to report to
func TestGuardClassify(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	g := newGuard(clk.now)

	if got := g.classify(syscall.SIGINT); got != sigInterrupt {
		t.Errorf("idle SIGINT = %d, want interrupt", got)
	}

	if got := g.classify(syscall.SIGTERM); got != sigQuit {
		t.Errorf("idle SIGTERM = %d, want quit", got)
	}

	if got := g.classify(syscall.SIGHUP); got != sigIgnore {
		t.Errorf("SIGHUP = %d, want ignore", got)
	}

	g.begin()

	if got := g.classify(syscall.SIGINT); got != sigIgnore {
		t.Errorf("SIGINT during a program = %d, want ignore", got)
	}

	if got := g.classify(syscall.SIGTERM); got != sigDefer {
		t.Errorf("SIGTERM during a program = %d, want defer", got)
	}

	g.end()

	if !g.takeTerm() || g.takeTerm() {
		t.Error("the deferred SIGTERM must be taken exactly once")
	}

	if got := g.classify(syscall.SIGINT); got != sigIgnore {
		t.Errorf("SIGINT just after a program = %d, want ignore", got)
	}

	clk.advance(interruptWindow - time.Millisecond)

	if got := g.classify(syscall.SIGINT); got != sigIgnore {
		t.Errorf("SIGINT inside the window = %d, want ignore", got)
	}

	clk.advance(2 * time.Millisecond)

	if got := g.classify(syscall.SIGINT); got != sigInterrupt {
		t.Errorf("SIGINT after the window = %d, want interrupt", got)
	}

	g.end()
	g.end()

	if g.busy.Load() != 0 {
		t.Errorf("busy = %d after unbalanced ends, want 0", g.busy.Load())
	}
}

// TestGuardNil checks that a nil guard does nothing and never panics.
//
// Parameters:
//   - t: test to report to
func TestGuardNil(t *testing.T) {
	var g *guard

	g.begin()
	g.end()

	if g.classify(syscall.SIGINT) != sigIgnore || g.takeTerm() {
		t.Error("a nil guard acted")
	}
}

// signals collects the messages that the watcher sends.
type signals struct {
	mu   sync.Mutex
	msgs []tea.Msg
	ch   chan struct{}
}

// send records a message.
//
// Parameters:
//   - m: message sent by the watcher
func (s *signals) send(m tea.Msg) {
	s.mu.Lock()
	s.msgs = append(s.msgs, m)
	s.mu.Unlock()

	select {
	case s.ch <- struct{}{}:
	default:
	}
}

// wait waits for the next message or gives up.
func (s *signals) wait() bool {
	select {
	case <-s.ch:
		return true
	case <-time.After(10 * time.Second):
		return false
	}
}

// TestGuardWatch sends real signals to the test process while the watcher is on.
//
// Parameters:
//   - t: test to report to
func TestGuardWatch(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	g := newGuard(clk.now)
	got := &signals{ch: make(chan struct{}, 4)}
	stop := g.watch(got.send)

	defer stop()

	self := os.Getpid()

	if err := syscall.Kill(self, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	if !got.wait() {
		t.Fatal("no message after SIGINT")
	}

	if err := syscall.Kill(self, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	if !got.wait() {
		t.Fatal("no message after SIGTERM")
	}

	g.begin()

	if err := syscall.Kill(self, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)

	got.mu.Lock()
	defer got.mu.Unlock()

	if len(got.msgs) != 2 {
		t.Fatalf("messages = %v, want an interrupt and a quit only", got.msgs)
	}

	if _, ok := got.msgs[0].(tea.InterruptMsg); !ok {
		t.Errorf("first message = %T, want InterruptMsg", got.msgs[0])
	}

	if _, ok := got.msgs[1].(tea.QuitMsg); !ok {
		t.Errorf("second message = %T, want QuitMsg", got.msgs[1])
	}
}
