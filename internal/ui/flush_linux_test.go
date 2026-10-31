package ui

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// queued returns the number of bytes waiting to be read on a terminal.
//
// Parameters:
//   - t: running test
//   - f: terminal to ask
func queued(t *testing.T, f *os.File) int {
	t.Helper()

	var n int32

	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCINQ,
		uintptr(unsafe.Pointer(&n))); e != 0 {
		t.Fatalf("TIOCINQ: %v", e)
	}

	return int(n)
}

// waitQueued waits until a terminal has bytes to read.
//
// Parameters:
//   - t: running test
//   - f: terminal to ask
func waitQueued(t *testing.T, f *os.File) {
	t.Helper()

	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if queued(t, f) > 0 {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("nothing was queued on the terminal")
}

// waitSettled waits until the number of bytes waiting on a terminal stops changing, which is how a
// test knows that the echo of what it typed has been written, whatever the load of the machine.
//
// Parameters:
//   - t: running test
//   - f: terminal to ask
//
// Returns the number of bytes waiting once it has been the same for 100 ms.
func waitSettled(t *testing.T, f *os.File) int {
	t.Helper()

	last, since := queued(t, f), time.Now()

	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)

		if n := queued(t, f); n != last {
			last, since = n, time.Now()
		} else if time.Since(since) >= 100*time.Millisecond {
			return last
		}
	}

	t.Fatal("the output of the terminal never settled")

	return 0
}

// TestFlushInput checks, on a pseudo terminal, that keys waiting to be read are discarded, that
// the next keys are not, and that a file that is not a terminal is refused without harm.
//
// Parameters:
//   - t: test to report to
func TestFlushInput(t *testing.T) {
	m, s := openPty(t)

	if _, err := m.WriteString("typed ahead\n"); err != nil {
		t.Fatal(err)
	}

	waitQueued(t, s)

	if err := flushInput(s); err != nil {
		t.Fatalf("flushInput: %v", err)
	}

	if n := queued(t, s); n != 0 {
		t.Errorf("%d bytes are still queued after the flush", n)
	}

	if _, err := m.WriteString("after\n"); err != nil {
		t.Fatal(err)
	}

	waitQueued(t, s)

	if n := queued(t, s); n != len("after\n") {
		t.Errorf("%d bytes queued after the flush, want %d", n, len("after\n"))
	}

	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	if err := flushInput(f); err == nil {
		t.Error("flushInput on a file that is not a terminal returned nil")
	}
}

// TestFlushInputKeepsTheOutput checks that only the input queue is emptied: what the program
// printed and the other side has not read yet stays, and a terminal that was closed is an error.
//
// Parameters:
//   - t: test to report to
func TestFlushInputKeepsTheOutput(t *testing.T) {
	m, s := openPty(t)

	if _, err := s.WriteString("out"); err != nil {
		t.Fatal(err)
	}

	waitQueued(t, m)

	if _, err := m.WriteString("typed\n"); err != nil {
		t.Fatal(err)
	}

	waitQueued(t, s)

	// The terminal echoes what was typed, so the output holds more than "out", and the echo is
	// written a moment after the input is readable: wait for it before counting.
	before := waitSettled(t, m)

	if err := flushInput(s); err != nil {
		t.Fatalf("flushInput: %v", err)
	}

	if n := queued(t, m); n != before || n < len("out") {
		t.Errorf("%d bytes of output are waiting, want the %d from before the flush", n, before)
	}

	if n := queued(t, s); n != 0 {
		t.Errorf("%d bytes of input are still queued", n)
	}

	closed, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}

	_ = closed.Close()

	if err := flushInput(closed); err == nil {
		t.Error("flushInput on a closed file returned nil")
	}
}

// TestExecCommandFlush checks the flush around a program that owns the terminal: keys that were
// waiting before it starts and keys typed while it runs are dropped when the spec asks for it,
// and kept when it does not.
//
// Parameters:
//   - t: test to report to
func TestExecCommandFlush(t *testing.T) {
	tests := []struct {
		name       string
		flush      bool
		wantBefore int
		wantAfter  int
	}{
		{"flush", true, 0, 0},
		{"no flush", false, len("early\n"), len("early\nduring\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, s := openPty(t)

			if _, err := m.WriteString("early\n"); err != nil {
				t.Fatal(err)
			}

			waitQueued(t, s)

			var before int

			run := runnerFunc(func(RunSpec) RunResult {
				before = queued(t, s)

				if _, err := m.WriteString("during\n"); err != nil {
					t.Error(err)
				}

				for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
					if queued(t, s) > before {
						break
					}

					time.Sleep(5 * time.Millisecond)
				}

				return RunResult{}
			})

			spec := RunSpec{Argv: []string{"tool"}, Flush: tt.flush, Pause: PauseNever}
			c := newExecCommand(run, spec, "", "tool", newPrinter("en"), (&clock{}).now, 0)
			c.SetStdin(s)
			c.SetStdout(&bytes.Buffer{})
			c.SetStderr(&bytes.Buffer{})

			if err := c.Run(); err != nil {
				t.Fatal(err)
			}

			if before != tt.wantBefore {
				t.Errorf("%d bytes queued when the program started, want %d", before, tt.wantBefore)
			}

			if after := queued(t, s); after != tt.wantAfter {
				t.Errorf("%d bytes queued when the program ended, want %d", after, tt.wantAfter)
			}
		})
	}
}

// TestFlushedPauseWaitsForAKey checks that a pause after a flushed run is not ended by what was
// typed during the program, and ends with the first key typed afterwards.
//
// Parameters:
//   - t: test to report to
func TestFlushedPauseWaitsForAKey(t *testing.T) {
	m, s := openPty(t)

	run := runnerFunc(func(RunSpec) RunResult {
		if _, err := m.WriteString("replayed\r\n"); err != nil {
			t.Error(err)
		}

		waitQueued(t, s)

		return RunResult{}
	})

	spec := RunSpec{Argv: []string{"tool"}, Flush: true, Pause: PauseAlways}
	c := newExecCommand(run, spec, "", "tool", newPrinter("en"), (&clock{}).now, 0)
	c.SetStdin(s)
	c.SetStdout(&bytes.Buffer{})
	c.SetStderr(&bytes.Buffer{})

	done := make(chan struct{})

	go func() {
		_ = c.Run()

		close(done)
	}()

	select {
	case <-done:
		t.Fatal("the pause ended on keys typed during the program")
	case <-time.After(400 * time.Millisecond):
	}

	if _, err := m.WriteString("\r"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the pause did not end on a key")
	}
}
