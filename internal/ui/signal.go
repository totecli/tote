package ui

import (
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
)

// interruptWindow is how long after a program returns that Ctrl-C is still ignored. The signal
// that stopped the program also reaches tote, a few milliseconds late, and must not quit it.
const interruptWindow = 500 * time.Millisecond

// sigAction is what to do with a signal.
type sigAction int

const (
	// sigIgnore drops the signal.
	sigIgnore sigAction = iota

	// sigInterrupt asks the program to stop as if Ctrl-C was pressed.
	sigInterrupt

	// sigQuit asks the program to quit.
	sigQuit

	// sigDefer keeps the signal for when the running child returns.
	sigDefer
)

// guard decides what SIGINT and SIGTERM do. Bubble Tea has its own handler, which is turned
// off: it cannot tell a Ctrl-C meant for a child from one meant for tote.
//
// While a program started through the exec runner is running, and during interruptWindow after
// it, SIGINT is ignored; SIGTERM is remembered and applied when the program returns. At any
// other time SIGINT interrupts and SIGTERM quits. SIGHUP is not intercepted: the default
// action ends tote and its children together. A nil guard ignores everything and does nothing.
//
// WakeR and WakeW are the two ends of a pipe that is made the first time a pause asks for it: a
// SIGTERM kept during a program writes a byte to it, which is what ends the wait of the pause
// at once without the wait having to look at a flag ten times a second.
type guard struct {
	now   func() time.Time
	busy  atomic.Int32
	until atomic.Int64
	term  atomic.Bool
	pipe  sync.Once
	wakeR *os.File
	wakeW atomic.Pointer[os.File]
}

// newGuard returns a guard.
//
// Parameters:
//   - now: clock
func newGuard(now func() time.Time) *guard {
	return &guard{now: now}
}

// begin marks the start of a foreground program. It must be called before the command that
// runs the program is returned to Bubble Tea, so that no signal can slip in between.
func (g *guard) begin() {
	if g != nil {
		g.busy.Add(1)
	}
}

// end marks the end of a foreground program and opens the window where SIGINT is still ignored.
func (g *guard) end() {
	if g == nil {
		return
	}

	if g.busy.Add(-1) < 0 {
		g.busy.Store(0)
	}

	g.until.Store(g.now().Add(interruptWindow).UnixNano())
}

// running reports whether a foreground program is pending or running. Key presses that arrive
// meanwhile are not meant for the interface and must not start a second program.
func (g *guard) running() bool {
	return g != nil && g.busy.Load() > 0
}

// active reports whether a signal for a child may still arrive.
func (g *guard) active() bool {
	return g.busy.Load() > 0 || g.now().UnixNano() < g.until.Load()
}

// classify decides what to do with a signal.
//
// Parameters:
//   - sig: signal received
func (g *guard) classify(sig os.Signal) sigAction {
	if g == nil {
		return sigIgnore
	}

	switch sig {
	case syscall.SIGINT:
		if g.active() {
			return sigIgnore
		}

		return sigInterrupt
	case syscall.SIGTERM:
		if g.busy.Load() > 0 {
			g.term.Store(true)
			g.poke()

			return sigDefer
		}

		return sigQuit
	}

	return sigIgnore
}

// takeTerm returns true once if a SIGTERM was kept while a program was running.
func (g *guard) takeTerm() bool {
	return g != nil && g.term.Swap(false)
}

// termPending reports whether a SIGTERM was kept while a program was running, without taking it.
func (g *guard) termPending() bool {
	return g != nil && g.term.Load()
}

// watch starts the single signal handler of the process.
//
// Parameters:
//   - send: delivers a message to the program, usually Program.Send
//
// Returns a function that stops the handler.
func (g *guard) watch(send func(tea.Msg)) func() {
	ch := make(chan os.Signal, 4)
	done := make(chan struct{})

	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			select {
			case s := <-ch:
				switch g.classify(s) {
				case sigInterrupt:
					send(tea.InterruptMsg{})
				case sigQuit:
					send(tea.QuitMsg{})
				}
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// waker returns the read end of the pipe that ends the wait of a pause, making the pipe on the
// first call.
//
// Returns nil when the pipe cannot be made, in which case the pause reads with a timeout.
func (g *guard) waker() *os.File {
	if g == nil {
		return nil
	}

	g.pipe.Do(func() {
		r, w, err := os.Pipe()
		if err != nil {
			return
		}

		g.wakeR = r
		g.wakeW.Store(w)
	})

	return g.wakeR
}

// poke writes a byte to the pipe of the pause, when there is one, so that a wait that is blocked
// asks whether it must stop.
func (g *guard) poke() {
	if w := g.wakeW.Load(); w != nil {
		_, _ = w.Write([]byte{0})
	}
}
