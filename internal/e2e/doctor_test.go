package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// brokenShell leaves the terminal the way a program killed in the middle of its work does: on
// the alternate screen, with a hidden cursor, mouse, focus and paste reporting on and the line
// discipline raw. It then waits for the test to look, before it runs the repair.
const brokenShell = `printf '\033[?1049h\033[?25l\033[?1000h\033[?1004h\033[?2004h'
stty -echo -icanon
touch "$E2E_DIR/broken"
while [ ! -e "$E2E_DIR/repair" ]; do sleep 0.02; done
exec tote doctor --reset`

// mouseOn reports whether the pane tracks the mouse.
func (m *term) mouseOn() bool {
	m.t.Helper()

	out, err := m.run("display-message", "-p", "-t", m.Sess, "#{mouse_any_flag}")
	if err != nil {
		m.t.Fatalf("tmux display-message: %v", err)
	}

	return strings.TrimSpace(out) == "1"
}

// TestDoctorResetRepairsTerminal breaks the terminal of a real tmux pane and checks that
// "tote doctor --reset" puts it back: main screen, visible cursor, no mouse reporting and a
// cooked line discipline.
//
// Parameters:
//   - t: test to report to
func TestDoctorResetRepairsTerminal(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{}, "sh", "-c", brokenShell)

	tm.waitFile("broken", func(string) bool { return true })

	s := tm.waitFor("the broken state", func(s snap) bool { return s.Alt && !s.Cursor })
	if !tm.mouseOn() {
		t.Fatalf("the mouse is not tracked before the repair; screen:\n%s", s.Text)
	}

	if err := os.WriteFile(filepath.Join(tm.Sb.Out, "repair"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if code := tm.waitDone(); code != 0 {
		t.Fatalf("exit status %d, want 0; screen:\n%s", code, tm.look().Text)
	}

	tm.usable()

	if tm.mouseOn() {
		t.Error("the mouse is still tracked after the repair")
	}

	if after := tm.look(); !after.has("terminal reset") {
		t.Errorf("no confirmation on the screen:\n%s", after.Text)
	}
}
