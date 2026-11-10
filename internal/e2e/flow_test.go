package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// panel is the start of the title of the detail panel of the first connection, drawn only
	// when the terminal is large enough.
	panel = "\u256d\u2500 alpha"

	// crumb is the header of the compact layout.
	crumb = "tote \u203a Connections"
)

// cmdOf is the command line that the status line shows for a connection of the sandbox.
//
// Parameters:
//   - host: host name before ".example.com"
func cmdOf(host string) string {
	return "ssh -l deploy -- " + host + ".example.com"
}

// TestStartAndNavigate opens the welcome page, a list, moves the cursor, filters and quits.
//
// Parameters:
//   - t: test to report to
func TestStartAndNavigate(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{}, "tote")

	s := tm.waitText("Connections")
	if !s.Alt {
		t.Error("the interface is not on the alternate screen")
	}

	if s.Cursor {
		t.Error("the cursor is visible in the interface")
	}

	tm.typeText("1")
	tm.waitText("alpha.example.com")
	tm.waitText(cmdOf("alpha"))

	tm.key("Down")
	tm.waitText(cmdOf("bravo"))

	tm.key("End")
	tm.waitText(cmdOf("hang"))

	tm.typeText("/")
	tm.typeText("fai")
	tm.waitText("1/4")

	tm.key("Escape")
	tm.waitGone("1/4")
	tm.waitText(cmdOf("alpha"))

	tm.quit()
}

// TestConnectionsCommand opens the list straight from the command line.
//
// Parameters:
//   - t: test to report to
func TestConnectionsCommand(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.key("Escape")
	tm.waitText("Repos")
	tm.quit()
}

// TestSSHRoundTrip runs the fake ssh from the list, comes back, and checks that the keys still
// work, the arrow key as well as Enter, from the first press.
//
// Parameters:
//   - t: test to report to
func TestSSHRoundTrip(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{}, "tote", "co")
	log := filepath.Join(sb.Out, "ssh.log")

	tm.waitText(cmdOf("alpha"))

	for round := 1; round <= 3; round++ {
		tm.key("Enter")
		tm.waitText("fake-ssh-token")
		tm.waitFor("the list again", func(s snap) bool {
			return !s.has("fake-ssh-token") && s.has("alpha.example.com") && s.Alt
		})

		if got := lines(log); got != round {
			t.Fatalf("round %d: the ssh log has %d lines", round, got)
		}

		// The first key after the return must not be lost. The status line of the other row is
		// not on the screen before the key is pressed.
		tm.key("Down")
		tm.waitText(cmdOf("bravo"))

		tm.key("Up")
		tm.waitGone(cmdOf("bravo"))
	}

	args := readFile(t, log)
	for _, line := range strings.Split(strings.TrimSpace(args), "\n") {
		if !strings.HasSuffix(line, " -l deploy -- alpha.example.com") {
			t.Errorf("ssh was started as %q", line)
		}
	}

	tm.quit()
}

// TestSSHFailure runs a fake ssh that fails: its output stays, the interface asks for Enter, and
// the keys typed afterwards still go to the list.
//
// Parameters:
//   - t: test to report to
func TestSSHFailure(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.key("End", "Up")
	tm.waitText(cmdOf("fail"))

	tm.key("Enter")
	tm.waitText("fake-ssh-token")
	tm.waitText("press Enter")
	tm.waitText("exit 255")

	if tm.look().Cursor == false {
		t.Error("the cursor is hidden while the interface waits for Enter")
	}

	tm.key("Enter")
	tm.waitGone("fake-ssh-token")
	tm.waitText(cmdOf("fail"))

	tm.key("Up")
	tm.waitText(cmdOf("bravo"))

	if got := lines(filepath.Join(sb.Out, "ssh.log")); got != 1 {
		t.Errorf("the ssh log has %d lines, want 1", got)
	}

	tm.quit()
}

// TestFilterEnterConnects types a filter that leaves one row and presses Enter, which applies
// the filter and leaves the row selected, then presses Enter again, which connects.
//
// Parameters:
//   - t: test to report to
func TestFilterEnterConnects(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.typeText("/")
	tm.typeText("hang")
	tm.waitText(cmdOf("hang"))
	tm.key("Enter")
	tm.key("Enter")
	tm.waitFile("ssh.pid", func(c string) bool { return strings.TrimSpace(c) != "" })

	tm.key("C-c")
	tm.waitText("press Enter")
	tm.key("Enter")
	tm.waitGone("press Enter")
	tm.quit()
}

// TestCtrlCDuringExec sends an interrupt while a program runs: the program receives it, tote
// survives, and it still answers afterwards.
//
// Parameters:
//   - t: test to report to
func TestCtrlCDuringExec(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.key("End")
	tm.waitText(cmdOf("hang"))

	tm.key("Enter")
	tm.waitFile("ssh.pid", func(c string) bool { return strings.TrimSpace(c) != "" })

	tm.key("C-c")
	tm.waitFile("ssh.int", func(c string) bool { return strings.TrimSpace(c) != "" })

	// The child exited with 130: the interface asks for Enter, and has not been killed.
	tm.waitText("press Enter")

	if _, err := os.Stat(filepath.Join(sb.Out, "exit")); err == nil {
		t.Fatal("tote ended on Ctrl-C during a program")
	}

	tm.key("Enter")
	tm.waitGone("press Enter")
	tm.waitText(cmdOf("hang"))

	tm.key("Up")
	tm.waitText(cmdOf("fail"))

	tm.quit()
}

// TestResize resizes the pane while the interface runs: the layout follows, nothing overflows,
// and a too small terminal gets a message.
//
// Parameters:
//   - t: test to report to
func TestResize(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{W: 100, H: 30}, "tote", "co")

	tm.waitText(panel)

	checks := []struct {
		w, h int
		want string
		gone string
	}{
		{60, 15, crumb, panel},
		{23, 15, "Terminal too small", crumb},
		{100, 6, "Terminal too small", "alpha.example.com"},
		{24, 7, "tote \u203a Connecti", "Terminal too small"},
		{23, 7, "Terminal too small", crumb},
		{24, 6, "Terminal too small", crumb},
		{120, 40, panel, "Terminal too small"},
		{80, 24, panel, ""},
		{70, 20, crumb, panel},
	}

	for _, c := range checks {
		tm.resize(c.w, c.h)

		s := tm.waitFor("the layout of "+size(c.w, c.h), func(s snap) bool {
			return s.has(c.want) && (c.gone == "" || !s.has(c.gone))
		})

		rows := strings.Split(strings.TrimRight(s.Text, "\n"), "\n")
		if len(rows) > c.h {
			t.Errorf("%s: %d rows", size(c.w, c.h), len(rows))
		}

		if !s.Alt {
			t.Errorf("%s: left the alternate screen", size(c.w, c.h))
		}

	}

	tm.quit()
}

// size formats a terminal size.
//
// Parameters:
//   - w: width in cells
//   - h: height in rows
func size(w, h int) string {
	return fmt.Sprintf("%dx%d", w, h)
}

// TestEditor opens a repository in the fake editor with o and comes back to the list.
//
// Parameters:
//   - t: test to report to
func TestEditor(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{}, "tote")

	tm.waitText("Repos")
	tm.typeText("2")
	tm.waitText("The project")

	tm.typeText("o")
	tm.waitText("fake-editor-token")
	tm.waitGone("fake-editor-token")

	got := strings.TrimSpace(readFile(t, filepath.Join(sb.Out, "editor.log")))
	if want := filepath.Join(sb.Home, "code", "proj"); got != want {
		t.Errorf("the editor was started with %q, want %q", got, want)
	}

	// The title of the detail panel changes with the row, the list already shows both names.
	tm.key("Down")
	tm.waitText("\u256d\u2500 docs")
	tm.quit()
}

// TestPanicRestoresTerminal crashes the interface on purpose and checks that the terminal is
// restored before the error is printed.
//
// Parameters:
//   - t: test to report to
func TestPanicRestoresTerminal(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{Env: []string{"TOTE_DEBUG_PANIC=1"}}, "tote")

	tm.waitText("Connections")
	tm.key("Down")

	if code := tm.waitDone(); code != 1 {
		t.Errorf("exit status %d after a panic, want 1", code)
	}

	tm.waitText("stopped")
	tm.usable()
}
