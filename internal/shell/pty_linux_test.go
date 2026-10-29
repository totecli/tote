package shell

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// ptyTimeout is how long a pty session waits for an expected output before it gives up.
const ptyTimeout = 20 * time.Second

// ptySession is an interactive bash running on a pseudo terminal.
//
// The output of the shell is collected by a goroutine in out, protected by mu. master is the side
// of the pseudo terminal that the test types on and reads from.
type ptySession struct {
	t      *testing.T
	master *os.File
	cmd    *exec.Cmd
	mu     sync.Mutex
	out    bytes.Buffer
}

// ioctl calls an ioctl request that takes a pointer to an integer.
//
// Parameters:
//   - fd: descriptor of the pseudo terminal master
//   - req: request number
//   - arg: integer read or written by the request
func ioctl(fd uintptr, req uintptr, arg *uint32) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(arg)))
	if errno != 0 {
		return errno
	}

	return nil
}

// openPty opens a pseudo terminal pair.
//
// Parameters:
//   - t: test to report to, which is skipped when no pseudo terminal can be opened
//
// Returns the master and the slave sides.
func openPty(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal available: %v", err)
	}

	var unlock, num uint32

	if err := ioctl(master.Fd(), syscall.TIOCSPTLCK, &unlock); err != nil {
		_ = master.Close()
		t.Skipf("cannot unlock the pseudo terminal: %v", err)
	}

	if err := ioctl(master.Fd(), syscall.TIOCGPTN, &num); err != nil {
		_ = master.Close()
		t.Skipf("cannot name the pseudo terminal: %v", err)
	}

	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", num), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		t.Skipf("cannot open the pseudo terminal: %v", err)
	}

	return master, slave
}

// startPty starts an interactive bash that has loaded the wrapper from a file of the sandbox.
//
// Parameters:
//   - t: test to report to
//   - w: wrapper environment whose sandbox and fake tote the shell uses
func startPty(t *testing.T, w *wrapperEnv) *ptySession {
	t.Helper()

	rc := filepath.Join(w.Dir, "rc.sh")
	if err := os.WriteFile(rc, []byte(Wrapper(Bash)), 0o600); err != nil {
		t.Fatal(err)
	}

	master, slave := openPty(t)

	cmd := exec.Command(w.shell, "--noprofile", "--norc", "-i")
	cmd.Dir = w.Dir
	cmd.Env = append(append([]string{}, w.Env...), "TERM=dumb", "INPUTRC=/dev/null", "PS1=$ ")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}

	if err := cmd.Start(); err != nil {
		_ = master.Close()
		_ = slave.Close()
		t.Fatalf("starting bash: %v", err)
	}

	_ = slave.Close()

	s := &ptySession{t: t, master: master, cmd: cmd}

	go s.collect()
	t.Cleanup(s.stop)

	s.send(". " + Quote(rc) + "\necho READY_$((40+2))\n")
	s.expect("READY_42")

	return s
}

// collect copies the output of the shell into the buffer until the terminal closes.
func (s *ptySession) collect() {
	buf := make([]byte, 4096)

	for {
		n, err := s.master.Read(buf)

		s.mu.Lock()
		s.out.Write(buf[:n])
		s.mu.Unlock()

		if err != nil {
			return
		}
	}
}

// stop kills the shell if it is still running and closes the terminal.
func (s *ptySession) stop() {
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
	_ = s.master.Close()
}

// output returns everything the shell has printed so far.
func (s *ptySession) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.out.String()
}

// send types text on the terminal.
//
// Parameters:
//   - text: bytes to type, a newline ending a line
func (s *ptySession) send(text string) {
	s.t.Helper()

	if _, err := s.master.WriteString(text); err != nil {
		s.t.Fatalf("typing %q: %v", text, err)
	}
}

// expect waits until the output holds a marker that the typed text cannot contain.
//
// Parameters:
//   - marker: text that must appear in the output
func (s *ptySession) expect(marker string) {
	s.t.Helper()

	deadline := time.Now().Add(ptyTimeout)

	for time.Now().Before(deadline) {
		if strings.Contains(s.output(), marker) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	s.t.Fatalf("timeout waiting for %q, output so far:\n%s", marker, s.output())
}

// insertSession starts an interactive shell whose fake tote asks to insert a command, and runs
// the wrapper once with three arguments.
//
// Parameters:
//   - t: test to report to
//   - payload: command line that the fake tote leaves for the shell
func insertSession(t *testing.T, payload string) (*ptySession, *wrapperEnv) {
	t.Helper()

	w := newWrapperEnv(t)
	w.setenv("FAKE_ACTION", "insert")
	w.setenv("FAKE_PAYLOAD", payload)

	s := startPty(t, w)
	s.send("tote a b c\n")

	return s, w
}

// TestReviewWrapperInsertInteractive checks the insert action in a real interactive shell: the
// line is offered for editing, accepted with enter, run without the arguments of tote, and kept
// in the history.
//
// Parameters:
//   - t: test to report to
func TestReviewWrapperInsertInteractive(t *testing.T) {
	needShell(t, "bash")

	s, w := insertSession(t, ": MARK_ONE; printf %s \"$#\" >n")
	s.expect("MARK_ONE")
	s.send("\n")
	s.send("echo FINISHED_$((40+2))\n")
	s.expect("FINISHED_42")
	s.send("history | grep -cE '^ *[0-9]+ +: MARK_ONE' | sed 's/^/HIST=/'\n")
	s.expect("HIST=1")
	s.send("exit\n")

	data, err := os.ReadFile(filepath.Join(w.Dir, "n"))
	if err != nil {
		t.Fatalf("the inserted command did not run: %v\n%s", err, s.output())
	}

	if string(data) != "0" {
		t.Errorf("the command saw %q positional parameters, want 0", data)
	}
}

// TestReviewWrapperInsertCancelled checks that clearing the offered line runs nothing.
//
// Parameters:
//   - t: test to report to
func TestReviewWrapperInsertCancelled(t *testing.T) {
	needShell(t, "bash")

	s, w := insertSession(t, ": MARK_TWO; touch ran")
	s.expect("MARK_TWO")
	s.send("\x15\n")
	s.send("echo FINISHED_$((40+2))\n")
	s.expect("FINISHED_42")
	s.send("exit\n")

	if _, err := os.Stat(filepath.Join(w.Dir, "ran")); err == nil {
		t.Error("a cleared line was run")
	}
}

// TestReviewWrapperInsertEdited checks that what is run is the edited line and not the payload.
//
// Parameters:
//   - t: test to report to
func TestReviewWrapperInsertEdited(t *testing.T) {
	needShell(t, "bash")

	s, w := insertSession(t, ": MARK_THREE; touch original")
	s.expect("MARK_THREE")
	s.send("\x15touch edited\n")
	s.send("echo FINISHED_$((40+2))\n")
	s.expect("FINISHED_42")
	s.send("exit\n")

	if _, err := os.Stat(filepath.Join(w.Dir, "edited")); err != nil {
		t.Errorf("the edited line did not run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(w.Dir, "original")); err == nil {
		t.Error("the original payload ran after being replaced")
	}
}

// TestWrapperInsertHidesLocals checks that the inserted line cannot see the variables of the
// wrapper, whose old names are the likeliest to clash with a variable of the user.
//
// Parameters:
//   - t: test to report to
func TestWrapperInsertHidesLocals(t *testing.T) {
	needShell(t, "bash")

	s, w := insertSession(t, ": MARK_FOUR; printf '[%s]' \"${f-}${hdr-}${act-}${arg-}\" >vars")
	s.expect("MARK_FOUR")
	s.send("\n")
	s.send("echo FINISHED_$((40+2))\n")
	s.expect("FINISHED_42")
	s.send("exit\n")

	data, err := os.ReadFile(filepath.Join(w.Dir, "vars"))
	if err != nil {
		t.Fatalf("the inserted command did not run: %v\n%s", err, s.output())
	}

	if string(data) != "[]" {
		t.Errorf("the command saw wrapper variables: %q", data)
	}
}
