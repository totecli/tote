package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// waitLimit is how long a test waits for a state before it fails.
const waitLimit = 15 * time.Second

// pollEvery is the delay between two looks at the terminal.
const pollEvery = 20 * time.Millisecond

// serverCount makes the names of the private tmux servers unique within a run.
var serverCount atomic.Int64

// wrapper is the script that runs the program under test inside the pane. It records the state
// of the terminal before and after, the exit status, and then keeps the pane alive. It starts
// the program only when the test has created the go file, so that the test can attach to the
// pane first.
const wrapper = `#!/bin/sh
D=$1
shift
trap : INT
while [ ! -e "$D/go" ]; do sleep 0.02; done
stty -a | sed 's/rows [0-9]*; columns [0-9]*;//' > "$D/stty.before"
"$@"
echo $? > "$D/exit.tmp"
mv "$D/exit.tmp" "$D/exit"
stty -a | sed 's/rows [0-9]*; columns [0-9]*;//' > "$D/stty.after"
echo ok > "$D/done"
exec sleep 600
`

// term is a program running in a pane of a private tmux server.
//
// Sock is the name of the server, Path its socket file, Sess the session, Sb the sandbox the
// program runs in and W and H the current size of the pane.
type term struct {
	t    *testing.T
	Sock string
	Sess string
	Path string
	Sb   *sandbox
	W, H int
}

// requireTmux skips the test when tmux is not usable (fails it when the environment requires the
// tools, see requireTool).
//
// Parameters:
//   - t: running test
func requireTmux(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("tmux end-to-end tests are skipped with -short")
	}

	requireTool(t, "tmux")
	requireTool(t, "stty")
}

// startOptions tunes a pane.
//
// Prefix is put before the program (for example taskset), Env adds variables to the
// environment, Hold keeps the program from starting until term.release is called.
type startOptions struct {
	W, H   int
	Prefix []string
	Env    []string
	Hold   bool
}

// newTerm starts a private tmux server with one pane that runs a program in a sandbox.
//
// Parameters:
//   - t: running test; the server is killed when it ends
//   - sb: sandbox the program runs in
//   - o: size, prefix and extra environment
//   - argv: program and arguments, run with the sandbox environment
func newTerm(t *testing.T, sb *sandbox, o startOptions, argv ...string) *term {
	t.Helper()
	requireTmux(t)

	if o.W == 0 {
		o.W, o.H = 100, 30
	}

	sock := fmt.Sprintf("tote-e2e-%d-%d", os.Getpid(), serverCount.Add(1))
	tm := &term{t: t, Sock: sock, Sess: "t", Sb: sb, W: o.W, H: o.H}

	t.Cleanup(tm.stop)

	script := filepath.Join(sb.Dir, "run.sh")
	if err := os.WriteFile(script, []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := append([]string{"env", "-i"}, sb.Env(o.Env...)...)
	cmd = append(cmd, "sh", script, sb.Out)
	cmd = append(cmd, o.Prefix...)
	cmd = append(cmd, argv...)

	args := append([]string{
		"new-session", "-d", "-s", tm.Sess, "-x", strconv.Itoa(o.W), "-y",
		strconv.Itoa(o.H), "-c", sb.Home,
	}, cmd...)

	if out, err := tm.run(args...); err != nil {
		t.Fatalf("tmux new-session: %v\n%s", err, out)
	}

	if out, err := tm.run("display-message", "-p", "#{socket_path}"); err == nil {
		tm.Path = strings.TrimSpace(out)
	}

	if !o.Hold {
		tm.release()
	}

	return tm
}

// stop kills the private server and waits until it is gone, so that nothing still runs in the
// sandbox when the test removes it.
func (m *term) stop() {
	_ = m.tmux("kill-server")

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if _, err := m.run("list-sessions"); err != nil {
			break
		}

		time.Sleep(pollEvery)
	}

	// A killed server leaves its socket file behind.
	if m.Path != "" {
		_ = os.Remove(m.Path)
	}

	// The panes receive a hang-up when the server dies: give them a moment to exit.
	time.Sleep(100 * time.Millisecond)
}

// release lets the program start.
func (m *term) release() {
	m.t.Helper()

	if err := os.WriteFile(filepath.Join(m.Sb.Out, "go"), nil, 0o600); err != nil {
		m.t.Fatal(err)
	}
}

// run runs a tmux command on the private server and returns its output.
//
// Parameters:
//   - args: arguments after the server selection
func (m *term) run(args ...string) (string, error) {
	cmd := exec.Command("tmux", append([]string{"-L", m.Sock, "-f", "/dev/null"}, args...)...)
	cmd.Env = cleanEnv()

	out, err := cmd.CombinedOutput()

	return string(out), err
}

// tmux runs a tmux command and fails the test when it does not succeed.
//
// Parameters:
//   - args: arguments after the server selection
func (m *term) tmux(args ...string) error {
	_, err := m.run(args...)

	return err
}

// cleanEnv returns the environment of the test without the variables that would point tmux at
// another server.
func cleanEnv() []string {
	var out []string

	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") {
			out = append(out, kv)
		}
	}

	return out
}

// snap is a look at the pane.
//
// Text is the visible text, Alt, Cursor, X and Y the terminal state, Cmd the name of the
// foreground command and Dead true once the pane has ended.
type snap struct {
	Text   string
	Alt    bool
	Cursor bool
	X, Y   int
	Cmd    string
	Dead   bool
}

// look captures the pane.
func (m *term) look() snap {
	m.t.Helper()

	text, err := m.run("capture-pane", "-p", "-t", m.Sess)
	if err != nil {
		return snap{Dead: true}
	}

	info, err := m.run("display-message", "-p", "-t", m.Sess,
		"#{alternate_on} #{cursor_flag} #{cursor_x} #{cursor_y} #{pane_dead} "+
			"#{pane_current_command}")
	if err != nil {
		return snap{Text: text, Dead: true}
	}

	f := strings.Fields(info)
	s := snap{Text: text}

	if len(f) >= 6 {
		s.Alt, s.Cursor = f[0] == "1", f[1] == "1"
		s.X, _ = strconv.Atoi(f[2])
		s.Y, _ = strconv.Atoi(f[3])
		s.Dead = f[4] == "1"
		s.Cmd = f[5]
	}

	return s
}

// has reports whether the visible text holds a string.
//
// Parameters:
//   - s: text looked for
func (s snap) has(sub string) bool {
	return strings.Contains(s.Text, sub)
}

// waitFor polls the pane until a condition holds.
//
// Parameters:
//   - what: description of the state, for the failure message
//   - ok: condition on a look at the pane
//
// Returns the look that satisfied the condition. It fails the test with the last screen after
// waitLimit.
func (m *term) waitFor(what string, ok func(snap) bool) snap {
	m.t.Helper()

	deadline := time.Now().Add(waitLimit)

	for {
		s := m.look()
		if ok(s) {
			return s
		}

		if time.Now().After(deadline) {
			m.t.Fatalf("timed out waiting for %s; screen (alt=%v cursor=%v):\n%s", what, s.Alt,
				s.Cursor, s.Text)
		}

		time.Sleep(pollEvery)
	}
}

// waitText waits until the visible text holds a string.
//
// Parameters:
//   - sub: text to wait for; it must be produced by the previous action of the test
func (m *term) waitText(sub string) snap {
	m.t.Helper()

	return m.waitFor("text "+strconv.Quote(sub), func(s snap) bool { return s.has(sub) })
}

// waitGone waits until the visible text no longer holds a string.
//
// Parameters:
//   - sub: text that must disappear
func (m *term) waitGone(sub string) snap {
	m.t.Helper()

	return m.waitFor("the end of "+strconv.Quote(sub), func(s snap) bool { return !s.has(sub) })
}

// key sends named keys such as Enter, Down, Escape or C-c, one tmux key per argument.
//
// Parameters:
//   - names: tmux key names
func (m *term) key(names ...string) {
	m.t.Helper()

	if err := m.tmux(append([]string{"send-keys", "-t", m.Sess}, names...)...); err != nil {
		m.t.Fatalf("send-keys %v: %v", names, err)
	}
}

// typeText types a literal text.
//
// Parameters:
//   - text: characters to type
func (m *term) typeText(text string) {
	m.t.Helper()

	if err := m.tmux("send-keys", "-t", m.Sess, "-l", text); err != nil {
		m.t.Fatalf("send-keys -l: %v", err)
	}
}

// resize changes the size of the pane.
//
// Parameters:
//   - w: width in cells
//   - h: height in rows
func (m *term) resize(w, h int) {
	m.t.Helper()

	if err := m.tmux("resize-window", "-t", m.Sess, "-x", strconv.Itoa(w), "-y",
		strconv.Itoa(h)); err != nil {
		m.t.Fatalf("resize-window: %v", err)
	}

	m.W, m.H = w, h
}

// waitDone waits for the program to end and returns its exit status.
func (m *term) waitDone() int {
	m.t.Helper()

	var code int

	deadline := time.Now().Add(waitLimit)

	for {
		if b, err := os.ReadFile(filepath.Join(m.Sb.Out, "exit")); err == nil {
			code, _ = strconv.Atoi(strings.TrimSpace(string(b)))

			break
		}

		if time.Now().After(deadline) {
			m.t.Fatalf("the program did not end; screen:\n%s", m.look().Text)
		}

		time.Sleep(pollEvery)
	}

	m.waitFile("done", func(string) bool { return true })

	return code
}

// waitFile waits for a file of the sandbox output directory to exist and satisfy a condition.
//
// Parameters:
//   - name: file name in the output directory
//   - ok: condition on the content
//
// Returns the content.
func (m *term) waitFile(name string, ok func(string) bool) string {
	m.t.Helper()

	return waitFile(m.t, filepath.Join(m.Sb.Out, name), func(c string) bool { return ok(c) },
		func() string { return m.look().Text })
}

// waitFile polls a file until it exists and satisfies a condition.
//
// Parameters:
//   - t: running test
//   - path: file to read
//   - ok: condition on the content
//   - screen: returns the screen to print when the wait fails, may be nil
//
// Returns the content.
func waitFile(t *testing.T, path string, ok func(string) bool, screen func() string) string {
	t.Helper()

	deadline := time.Now().Add(waitLimit)

	for {
		if b, err := os.ReadFile(path); err == nil && ok(string(b)) {
			return string(b)
		}

		if time.Now().After(deadline) {
			shown := ""
			if screen != nil {
				shown = "\nscreen:\n" + screen()
			}

			t.Fatalf("timed out waiting for %s%s", path, shown)
		}

		time.Sleep(pollEvery)
	}
}

// lines returns the number of lines of a file, 0 when it does not exist.
//
// Parameters:
//   - path: file to count
func lines(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	return strings.Count(string(b), "\n")
}

// restored checks that the terminal is back to what it was before the program ran: the main
// screen, a visible cursor, and the same line discipline (checked with stty, not with the
// screen, as the width of a glyph is not).
func (m *term) restored() {
	m.t.Helper()
	m.screenBack()

	before := readFile(m.t, filepath.Join(m.Sb.Out, "stty.before"))
	after := readFile(m.t, filepath.Join(m.Sb.Out, "stty.after"))

	if before != after {
		m.t.Errorf("the terminal modes changed:\nbefore: %s\nafter:  %s", before, after)
	}
}

// usable checks the terminal after a crash: the main screen, a visible cursor and a cooked line
// discipline. The crash path runs stty sane, which also resets flags such as iutf8 that the
// terminal had before, so the modes are not compared as a whole.
func (m *term) usable() {
	m.t.Helper()
	m.screenBack()

	after := strings.Fields(readFile(m.t, filepath.Join(m.Sb.Out, "stty.after")))

	for _, flag := range []string{"isig", "icanon", "echo", "opost", "icrnl", "iexten"} {
		if !slices.Contains(after, flag) {
			m.t.Errorf("the terminal is not cooked: %s is off", flag)
		}
	}
}

// screenBack checks the main screen and the cursor.
func (m *term) screenBack() {
	m.t.Helper()

	s := m.look()

	if s.Alt {
		m.t.Error("the alternate screen is still on")
	}

	if !s.Cursor {
		m.t.Error("the cursor is hidden")
	}
}

// readFile returns the content of a file, failing the test when it cannot be read.
//
// Parameters:
//   - t: running test
//   - path: file to read
func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// quit leaves the interface with q and checks the exit status and the terminal.
func (m *term) quit() {
	m.t.Helper()

	m.key("q")

	if code := m.waitDone(); code != 0 {
		m.t.Errorf("exit status %d, want 0; screen:\n%s", code, m.look().Text)
	}

	m.restored()
}
