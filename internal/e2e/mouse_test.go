package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mouseFlags reads the mouse modes that the program has asked the terminal for, as tmux tracks
// them for the pane.
//
// Returns "any button sgr all" as four digits separated by spaces: 1 when the mode is on. Any is
// any mouse reporting at all, button is button and drag reporting (1002), sgr the extended
// coordinates (1006) and all the reporting of every motion (1003), which is what the hover needs.
func (m *term) mouseFlags() string {
	m.t.Helper()

	out, err := m.run("display-message", "-p", "-t", m.Sess,
		"#{mouse_any_flag} #{mouse_button_flag} #{mouse_sgr_flag} #{mouse_all_flag}")
	if err != nil {
		m.t.Fatalf("display-message: %v\n%s", err, out)
	}

	return strings.TrimSpace(out)
}

// waitMouse waits until the mouse modes of the pane are the given ones.
//
// Parameters:
//   - flags: "any button sgr all", see mouseFlags
func (m *term) waitMouse(flags string) {
	m.t.Helper()

	m.waitFor("the mouse flags "+flags, func(snap) bool { return m.mouseFlags() == flags })
}

// wantMouse is the mouse modes of Bubble Tea in all motion mode: every motion reported, with SGR
// coordinates, and not the button and drag reporting that all motion replaces (tmux keeps the two
// apart: asking for 1003 clears 1002).
const wantMouse = "1 0 1 1"

// off is the mouse modes of a program that did not ask for the mouse.
const off = "0 0 0 0"

// send writes raw bytes to the pane as if the terminal had sent them.
//
// Parameters:
//   - seq: the bytes
func (m *term) send(seq string) {
	m.t.Helper()
	m.typeText(seq)
}

// click sends a press and a release of the left button, in SGR coordinates.
//
// Parameters:
//   - col: column, 1 for the first
//   - row: row, 1 for the first
func (m *term) click(col, row int) {
	m.t.Helper()
	m.send(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", col, row, col, row))
}

// wheel sends one notch of the wheel.
//
// Parameters:
//   - down: true to scroll down
//   - col: column, 1 for the first
//   - row: row, 1 for the first
func (m *term) wheel(down bool, col, row int) {
	m.t.Helper()

	button := 64
	if down {
		button = 65
	}

	m.send(fmt.Sprintf("\x1b[<%d;%d;%dM", button, col, row))
}

// where finds a text on the screen.
//
// Parameters:
//   - sub: text looked for
//   - fromBottom: true to search the last line first, which is where the footer is
//
// Returns the column and the row, both starting at 1 as the mouse protocol counts them. It fails
// the test when the text is not on the screen.
func (m *term) where(sub string, fromBottom bool) (int, int) {
	m.t.Helper()

	s := m.look()
	rows := strings.Split(s.Text, "\n")

	for n := range rows {
		y := n
		if fromBottom {
			y = len(rows) - 1 - n
		}

		if i := strings.Index(rows[y], sub); i >= 0 {
			// The captured text is UTF-8 and every glyph before the text is one cell wide in
			// these screens, except the box drawing ones that are one cell too.
			return len([]rune(rows[y][:i])) + 1, y + 1
		}
	}

	m.t.Fatalf("%q is not on the screen:\n%s", sub, s.Text)

	return 0, 0
}

// clickText clicks on the first cell of a text.
//
// Parameters:
//   - sub: text to click on
//   - fromBottom: see where
func (m *term) clickText(sub string, fromBottom bool) {
	m.t.Helper()

	col, row := m.where(sub, fromBottom)
	m.click(col, row)
}

// TestMouseClickAndDoubleClick sends real SGR mouse sequences to the real program: a click
// selects a row, a double click connects, the mouse is off while the program runs and back on
// when it returns, and the footer hints can be clicked.
//
// Parameters:
//   - t: test to report to
func TestMouseClickAndDoubleClick(t *testing.T) {
	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{Env: []string{"FAKE_SSH_PAUSE=1.5"}}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.waitMouse(wantMouse)

	tm.clickText("bravo.example.com", false)
	tm.waitText(cmdOf("bravo"))
	tm.waitGone(cmdOf("alpha"))

	tm.clickText("hang.example.com", false)
	tm.waitText(cmdOf("hang"))

	if lines(filepath.Join(sb.Out, "ssh.log")) != 0 {
		t.Fatal("a single click started ssh")
	}

	// A footer hint: "ctrl+k" opens the palette, "esc" closes it.
	tm.clickText("ctrl+k", true)
	tm.waitText("Palette")
	tm.waitMouse(wantMouse)
	tm.clickText("esc close", true)
	tm.waitGone("Palette")

	col, row := tm.where("alpha.example.com", false)
	tm.click(col, row)
	time.Sleep(60 * time.Millisecond)
	tm.click(col, row)

	tm.waitText("fake-ssh-token")
	tm.waitMouse(off)

	tm.waitFor("the list again", func(s snap) bool {
		return !s.has("fake-ssh-token") && s.has("alpha.example.com") && s.Alt
	})
	tm.waitMouse(wantMouse)

	if got := lines(filepath.Join(sb.Out, "ssh.log")); got != 1 {
		t.Errorf("the ssh log has %d lines, want 1", got)
	}

	args := strings.TrimSpace(readFile(t, filepath.Join(sb.Out, "ssh.log")))
	if !strings.HasSuffix(args, " -l deploy -- alpha.example.com") {
		t.Errorf("ssh was started as %q", args)
	}

	// The click after the return still works.
	tm.clickText("bravo.example.com", false)
	tm.waitText(cmdOf("bravo"))

	tm.quit()
	tm.waitMouse(off)
}

// TestMouseWheel scrolls a long list with the wheel and checks that a click still hits the right
// row afterwards.
//
// Parameters:
//   - t: test to report to
func TestMouseWheel(t *testing.T) {
	sb := newSandbox(t)

	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i := range 40 {
		fmt.Fprintf(&b, "\n[[alias]]\nname = \"a%02d\"\ncmd = \"echo %d\"\n", i, i)
	}

	sb.write(t, filepath.Join(sb.Dir, "config", "aliases.toml"), b.String())

	tm := newTerm(t, sb, startOptions{}, "tote")

	tm.waitText("Connections")
	tm.typeText("3")
	tm.waitText("a00")
	tm.waitMouse(wantMouse)

	if tm.look().has("a26") {
		t.Fatal("the list is long enough to show a26 before it scrolls")
	}

	// The "N more" lines take two rows of the window, so it takes one more tick to reach a26.
	for range 4 {
		tm.wheel(true, 20, 10)
	}

	tm.waitText("a26")
	tm.waitText("more")
	tm.waitGone("a00")

	for range 5 {
		tm.wheel(false, 20, 10)
	}

	tm.waitText("a00")

	tm.clickText("a03", false)
	tm.waitFor("a03 to be the selected row", func(s snap) bool {
		for _, l := range strings.Split(s.Text, "\n") {
			if strings.Contains(l, "a03") && strings.Contains(l, "\u276f") {
				return true
			}
		}

		return false
	})

	tm.quit()
}

// TestMouseOff checks that nothing asks for the mouse when ui.mouse is false, and on a terminal
// that has none.
//
// Parameters:
//   - t: test to report to
func TestMouseOff(t *testing.T) {
	tests := []struct {
		name  string
		conf  string
		extra []string
	}{
		{"setting", "schema = 1\n[ui]\nmouse = false\n", nil},
		{"linux console", "", []string{"TERM=linux"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := newSandbox(t)

			if tt.conf != "" {
				sb.write(t, filepath.Join(sb.Dir, "config", "config.toml"), tt.conf)
			}

			tm := newTerm(t, sb, startOptions{Env: tt.extra}, "tote", "co")

			tm.waitText("alpha.example.com")
			tm.waitText(cmdOf("alpha"))

			if got := tm.mouseFlags(); got != off {
				t.Errorf("mouse flags = %q, want %q", got, off)
			}

			// A click that reaches the program anyway must not select anything.
			tm.clickText("bravo.example.com", false)
			time.Sleep(300 * time.Millisecond)

			if tm.look().has(cmdOf("bravo")) {
				t.Error("a click selected a row with the mouse off")
			}

			tm.quit()
		})
	}
}

// TestMouseSettingFromTheScreen turns the mouse off and on again from the settings screen, with
// the mouse itself, and watches the terminal modes follow.
//
// Parameters:
//   - t: test to report to
func TestMouseSettingFromTheScreen(t *testing.T) {
	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{}, "tote")

	tm.waitText("Connections")
	tm.waitMouse(wantMouse)

	tm.typeText(",")
	tm.waitText("Mouse  ")

	// Select the row, then flip it with the footer hint.
	// The label is followed by blanks; the path of the sandbox in the editor row holds "Mouse" too.
	tm.clickText("Mouse  ", false)
	tm.waitFor("Mouse to be the selected row", func(s snap) bool {
		for _, l := range strings.Split(s.Text, "\n") {
			if strings.Contains(l, "Mouse  ") && strings.Contains(l, "\u276f") {
				return true
			}
		}

		return false
	})
	tm.clickText("change", true)
	tm.waitMouse(off)
	waitFile(t, filepath.Join(sb.Dir, "config", "config.toml"),
		func(c string) bool { return strings.Contains(c, "mouse = false") }, nil)

	tm.key("Enter")
	tm.waitMouse(wantMouse)

	tm.key("Escape")
	tm.waitText("Connections")
	tm.quit()
}
