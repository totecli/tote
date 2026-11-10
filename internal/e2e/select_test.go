package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	// fakeXclip plays the part of xclip: it keeps the arguments and the text it was given.
	fakeXclip = `#!/bin/sh
echo "$@" > "$E2E_DIR/clip.args"
cat > "$E2E_DIR/clip.txt"
`

	// reverseCell matches the sequence that tmux writes in a capture for cells in reverse video.
	reverseCell = `\x1b\[(?:[0-9]+;)*7(?:;[0-9]+)*m`
)

// reverseBefore reports whether the cells that start a text are in reverse video in a styled
// capture.
//
// Parameters:
//   - capture: output of capture-pane with its styles
//   - text: text looked for
func reverseBefore(capture, text string) bool {
	return regexp.MustCompile(reverseCell + regexp.QuoteMeta(text)).MatchString(capture)
}

// press sends the press of the left button, in SGR coordinates.
//
// Parameters:
//   - col: column, 1 for the first
//   - row: row, 1 for the first
func (m *term) press(col, row int) {
	m.t.Helper()
	m.send(fmt.Sprintf("\x1b[<0;%d;%dM", col, row))
}

// moveTo sends the motion of the pointer with the left button held.
//
// Parameters:
//   - col: column, 1 for the first
//   - row: row, 1 for the first
func (m *term) moveTo(col, row int) {
	m.t.Helper()
	m.send(fmt.Sprintf("\x1b[<32;%d;%dM", col, row))
}

// releaseAt sends the release of the left button.
//
// Parameters:
//   - col: column, 1 for the first
//   - row: row, 1 for the first
func (m *term) releaseAt(col, row int) {
	m.t.Helper()
	m.send(fmt.Sprintf("\x1b[<0;%d;%dm", col, row))
}

// dragFrom presses at a cell, moves along the way cell by cell as a terminal does, and releases.
//
// Parameters:
//   - col: column of the press, 1 for the first
//   - row: row of the press
//   - toCol: column of the release
//   - toRow: row of the release
func (m *term) dragFrom(col, row, toCol, toRow int) {
	m.t.Helper()

	m.press(col, row)

	for r := row; r <= toRow; r++ {
		from, to := 1, toCol

		if r == row {
			from = col + 1
		}

		if r < toRow {
			to = col + 12
		}

		for c := from; c <= to; c++ {
			m.moveTo(c, r)
		}
	}

	m.moveTo(toCol, toRow)
	m.releaseAt(toCol, toRow)
}

// styled captures the pane with its styles.
func (m *term) styled() string {
	m.t.Helper()

	out, err := m.run("capture-pane", "-p", "-e", "-t", m.Sess)
	if err != nil {
		m.t.Fatalf("capture-pane: %v", err)
	}

	return out
}

// installXclip puts the fake xclip in the sandbox.
//
// Parameters:
//   - t: running test
//   - sb: sandbox whose PATH holds it
func installXclip(t *testing.T, sb *sandbox) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(sb.Bin, "xclip"), []byte(fakeXclip), 0o700); err != nil {
		t.Fatal(err)
	}
}

// TestSelectionDragCopies sends real SGR press, motion and release sequences to the real program:
// the dragged text is highlighted and reaches the fake xclip, the toast says so, Escape removes
// the highlight without doing anything else, and a click and a double click still work.
//
// Parameters:
//   - t: test to report to
func TestSelectionDragCopies(t *testing.T) {
	sb := newSandbox(t)
	installXclip(t, sb)

	tm := newTerm(t, sb, startOptions{Env: []string{"DISPLAY=:0", "FAKE_SSH_PAUSE=1.5"}},
		"tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.waitMouse(wantMouse)

	if reverseBefore(tm.styled(), "bravo.example.com") {
		t.Fatal("the list shows a selection before any drag")
	}

	col, row := tm.where("bravo.example.com", false)
	tm.dragFrom(col, row, col+16, row)

	got := tm.waitFile("clip.txt", func(c string) bool { return c != "" })
	if got != "bravo.example.com" {
		t.Errorf("xclip got %q, want the dragged word", got)
	}

	if args := strings.TrimSpace(readFile(t, filepath.Join(sb.Out, "clip.args"))); args !=
		"-selection clipboard -in" {
		t.Errorf("xclip was run with %q", args)
	}

	tm.waitText("Copied 17 characters (xclip)")
	tm.waitText("Shift+drag selects with the terminal instead")

	if !reverseBefore(tm.styled(), "bravo.example.com") {
		t.Errorf("the word is not in reverse video:\n%q", tm.styled())
	}

	tm.key("Escape")
	tm.waitFor("the highlight to go", func(snap) bool {
		return !reverseBefore(tm.styled(), "bravo.example.com")
	})

	if s := tm.look(); !s.has("alpha.example.com") || !s.Alt {
		t.Errorf("escape did more than clear the highlight:\n%s", s.Text)
	}

	// A drag over two rows is the text of the screen between the two cells.
	if err := os.WriteFile(filepath.Join(sb.Out, "clip.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	col, row = tm.where("alpha.example.com", false)
	col2, row2 := tm.where("bravo.example.com", false)

	tm.dragFrom(col, row, col2+4, row2)

	got = tm.waitFile("clip.txt", func(c string) bool { return c != "" })
	rows := strings.Split(tm.look().Text, "\n")
	first := []rune(rows[row-1])
	second := []rune(rows[row2-1])
	want := strings.TrimRight(string(first[col-1:]), " ") + "\n" +
		strings.TrimSpace(string(second[:col2+4]))

	if !strings.HasPrefix(want, "alpha.example.com") || got != want {
		t.Errorf("two rows: xclip got %q, want %q", got, want)
	}

	// A click that does not move still selects a row, and a double click still connects.
	tm.key("Escape")
	tm.clickText("hang.example.com", false)
	tm.waitText(cmdOf("hang"))

	col, row = tm.where("alpha.example.com", false)
	time.Sleep(450 * time.Millisecond)
	tm.click(col, row)
	time.Sleep(60 * time.Millisecond)
	tm.click(col, row)

	tm.waitText("fake-ssh-token")
	tm.waitMouse(off)
	tm.waitFor("the list again", func(s snap) bool {
		return !s.has("fake-ssh-token") && s.has("alpha.example.com") && s.Alt
	})
	tm.waitMouse(wantMouse)

	tm.quit()
}

// TestSelectionCopiesToTheTerminal checks the sequence of the terminal when no program takes the
// text: tmux is told to accept it and keeps it in a paste buffer.
//
// Parameters:
//   - t: test to report to
func TestSelectionCopiesToTheTerminal(t *testing.T) {
	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{Env: []string{"TERM=xterm-256color"}}, "tote", "co")

	if err := tm.tmux("set", "-s", "set-clipboard", "on"); err != nil {
		t.Fatal(err)
	}

	tm.waitText(cmdOf("alpha"))
	tm.waitMouse(wantMouse)

	col, row := tm.where("bravo.example.com", false)
	tm.dragFrom(col, row, col+16, row)

	tm.waitText("Sent 17 characters to the terminal (OSC 52)")

	// tmux reads the sequence on its own schedule, so on a loaded machine the buffer can appear
	// a moment after the message does.
	var (
		out string
		err error
	)

	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if out, err = tm.run("show-buffer"); err == nil && out == "bravo.example.com" {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	if err != nil || out != "bravo.example.com" {
		t.Errorf("the tmux buffer holds %q (%v), want the dragged word", out, err)
	}

	tm.quit()
}

// TestSelectionOff checks that ui.mouse = false leaves the selection to the terminal: a drag
// copies nothing and highlights nothing.
//
// Parameters:
//   - t: test to report to
func TestSelectionOff(t *testing.T) {
	sb := newSandbox(t)
	installXclip(t, sb)
	sb.write(t, filepath.Join(sb.Dir, "config", "config.toml"),
		"schema = 1\n\n[ui]\nmouse = false\n")

	tm := newTerm(t, sb, startOptions{Env: []string{"DISPLAY=:0"}}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.waitMouse(off)

	col, row := tm.where("bravo.example.com", false)
	tm.dragFrom(col, row, col+16, row)
	time.Sleep(400 * time.Millisecond)

	if _, err := os.Stat(filepath.Join(sb.Out, "clip.txt")); err == nil {
		t.Error("a drag copied text with the mouse off")
	}

	if reverseBefore(tm.styled(), "bravo.example.com") {
		t.Error("a drag highlighted text with the mouse off")
	}

	tm.quit()
}
