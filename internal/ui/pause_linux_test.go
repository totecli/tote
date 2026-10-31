package ui

import (
	"io"
	"os"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// getTermios reads the mode of a terminal.
//
// Parameters:
//   - t: running test
//   - f: terminal to read
func getTermios(t *testing.T, f *os.File) syscall.Termios {
	t.Helper()

	var tio syscall.Termios

	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS,
		uintptr(unsafe.Pointer(&tio))); e != 0 {
		t.Fatalf("TCGETS: %v", e)
	}

	return tio
}

// openPty opens a pseudo terminal.
//
// Parameters:
//   - t: running test; both ends are closed when it ends
//
// Returns the master and the slave.
func openPty(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}

	var unlock, n int32

	for req, arg := range map[uintptr]*int32{syscall.TIOCSPTLCK: &unlock, syscall.TIOCGPTN: &n} {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), req,
			uintptr(unsafe.Pointer(arg))); e != 0 {
			_ = m.Close()
			t.Skipf("pseudo terminal: %v", e)
		}
	}

	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = m.Close()
		t.Skipf("pseudo terminal: %v", err)
	}

	t.Cleanup(func() {
		_ = s.Close()
		_ = m.Close()
	})

	return m, s
}

// TestPauseOnTerminal checks the pause on a real terminal: Enter, Ctrl-C and Ctrl-D end it
// without a signal, typed text is not echoed while it waits, and the mode comes back.
//
// Parameters:
//   - t: test to report to
func TestPauseOnTerminal(t *testing.T) {
	for _, key := range []string{"\r", "\x03", "\x04"} {
		m, s := openPty(t)

		before := getTermios(t, s)

		done := make(chan struct{})

		go func() {
			readLine(s, nil, nil)
			close(done)
		}()

		const cooked = syscall.ICANON | syscall.ECHO | syscall.ISIG

		var during syscall.Termios

		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
			if during = getTermios(t, s); during.Lflag&cooked == 0 {
				break
			}

			time.Sleep(5 * time.Millisecond)
		}

		if during.Lflag&cooked != 0 {
			t.Errorf("%q: the terminal is not raw while waiting: %+v", key, during)
		}

		select {
		case <-done:
			t.Fatalf("%q: the pause ended by itself", key)
		case <-time.After(250 * time.Millisecond):
		}

		if _, err := m.WriteString(key); err != nil {
			t.Fatal(err)
		}

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%q: the pause did not end", key)
		}

		if after := getTermios(t, s); after != before {
			t.Errorf("%q: the mode was not restored: %+v, %+v", key, after, before)
		}
	}
}

// TestPauseStops checks that the wait on a terminal ends, without a key, when the stop function
// says so, which is how a SIGTERM kept during the pause is acted on.
//
// Parameters:
//   - t: test to report to
func TestPauseStops(t *testing.T) {
	_, s := openPty(t)
	before := getTermios(t, s)

	var flag atomic.Bool

	done := make(chan struct{})

	go func() {
		readLine(s, flag.Load, nil)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("the pause ended by itself")
	case <-time.After(300 * time.Millisecond):
	}

	flag.Store(true)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the pause did not end when asked to stop")
	}

	if after := getTermios(t, s); after != before {
		t.Errorf("the mode was not restored: %+v, %+v", after, before)
	}
}

// TestPauseWithoutPolling checks the pause that is given a wake pipe, as the real one is: it sits
// blocked in the system without asking the stop function, a byte on the pipe that does not mean
// stop is answered once and does not end it, a key still ends it, and a SIGTERM kept by the guard
// ends it at once.
//
// Parameters:
//   - t: test to report to
func TestPauseWithoutPolling(t *testing.T) {
	m, s := openPty(t)
	before := getTermios(t, s)

	g := newGuard(time.Now)
	g.begin()

	var asked atomic.Int64

	stop := func() bool {
		asked.Add(1)

		return g.termPending()
	}

	done := make(chan struct{})

	go func() {
		readLine(s, stop, g.waker())
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("the pause ended by itself")
	case <-time.After(1500 * time.Millisecond):
	}

	if n := asked.Load(); n > 3 {
		t.Errorf("the stop function was asked %d times in 1.5 s, want the pause to be blocked", n)
	}

	g.poke()
	time.Sleep(200 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("a wake-up that does not mean stop ended the pause")
	default:
	}

	if g.classify(syscall.SIGTERM) != sigDefer {
		t.Fatal("SIGTERM was not kept during a program")
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the pause did not end on a kept SIGTERM")
	}

	if after := getTermios(t, s); after != before {
		t.Errorf("the mode was not restored: %+v, %+v", after, before)
	}

	_ = m
}

// TestPauseWithWakeStillTakesKeys checks that a pause with a wake pipe ends on Enter and leaves
// what follows unread.
//
// Parameters:
//   - t: test to report to
func TestPauseWithWakeStillTakesKeys(t *testing.T) {
	m, s := openPty(t)
	g := newGuard(time.Now)

	done := make(chan struct{})

	go func() {
		readLine(s, g.termPending, g.waker())
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)

	if _, err := m.WriteString("ab\rrest"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the pause did not end on Enter")
	}

	buf := make([]byte, 16)

	if err := s.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Skipf("the terminal has no read deadline: %v", err)
	}

	n, _ := s.Read(buf)
	if got := string(buf[:n]); got != "rest" {
		t.Errorf("left %q after Enter, want %q", got, "rest")
	}
}

// TestExecRestoresTheTerminalMode checks that the mode of the terminal is put back after a
// program that changed it and did not restore it, such as a ssh that was killed: Bubble Tea would
// take the leftover as the mode to restore when the interface ends.
//
// Parameters:
//   - t: test to report to
func TestExecRestoresTheTerminalMode(t *testing.T) {
	_, s := openPty(t)
	before := getTermios(t, s)

	leaves := runnerFunc(func(spec RunSpec) RunResult {
		if makeRaw(int(s.Fd())) == nil {
			t.Error("the stand-in program could not change the mode")
		}

		return RunResult{}
	})

	c := newExecCommand(leaves, RunSpec{Stdin: s, Stdout: io.Discard, Stderr: io.Discard}, "", "x",
		newPrinter("en"), time.Now, 0)

	if err := c.Run(); err != nil {
		t.Fatal(err)
	}

	if after := getTermios(t, s); after != before {
		t.Errorf("the mode left by the program was kept: %+v, want %+v", after, before)
	}
}

// TestPauseHangup checks that the pause ends when the terminal goes away while it waits, instead
// of reading an end of file for ever, with and without a wake pipe.
//
// Parameters:
//   - t: test to report to
func TestPauseHangup(t *testing.T) {
	for _, withWake := range []bool{false, true} {
		m, s := openPty(t)

		var (
			wake *os.File
			flag atomic.Bool
		)

		if withWake {
			g := newGuard(time.Now)
			g.begin()
			wake = g.waker()
		}

		done := make(chan struct{})

		go func() {
			readLine(s, flag.Load, wake)
			close(done)
		}()

		select {
		case <-done:
			t.Fatalf("wake %v: the pause ended by itself", withWake)
		case <-time.After(300 * time.Millisecond):
		}

		if err := m.Close(); err != nil {
			t.Fatal(err)
		}

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("wake %v: the pause did not end when the terminal went away", withWake)
		}
	}
}
