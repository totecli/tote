package e2e

import (
	"strings"
	"testing"
)

// trimRows removes the trailing spaces of every row and the empty rows at the end of a capture,
// so that two captures of the same screen compare equal.
//
// Parameters:
//   - text: text of a capture of the pane
func trimRows(text string) string {
	rows := strings.Split(text, "\n")

	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}

	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}

// TestTooSmallBlock starts the interface at 80x24, shrinks the window to 23x8, one column under
// the minimum, where the block of the too small screen shows with the real size, then goes
// through the minimum of 24x7 in both directions, and grows it back to 80x24, where the screen is
// again what it was, with no character left behind.
//
// Parameters:
//   - t: test to report to
func TestTooSmallBlock(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{W: 80, H: 24}, "tote")

	first := tm.waitFor("the welcome page", func(s snap) bool {
		return s.has("Connections") && s.has("Welcome")
	})

	tm.resize(23, 8)

	s := tm.waitFor("the block at 23x8", func(s snap) bool {
		return s.has("23 \u00D7 8") && s.has("needs 24 \u00D7 7") && !s.has("Connections")
	})

	rows := strings.Split(strings.TrimRight(s.Text, "\n"), "\n")
	if len(rows) > 8 {
		t.Errorf("23x8: %d rows", len(rows))
	}

	for _, want := range []string{
		"tote", "Terminal too small", "Make the window bigger,",
		"or press q to quit.",
	} {
		if !s.has(want) {
			t.Errorf("23x8: lacks %q:\n%s", want, s.Text)
		}
	}

	if !s.Alt {
		t.Error("23x8: left the alternate screen")
	}

	tm.typeText("x")
	tm.key("Down", "Enter")

	steps := []struct {
		w, h int
		want string
		gone string
	}{
		{24, 6, "24 \u00D7 6", "Connections"},
		{24, 7, "Connections", "Terminal too small"},
		{23, 7, "23 \u00D7 7", "Connections"},
		{24, 7, "Connections", "Terminal too small"},
	}

	for _, st := range steps {
		tm.resize(st.w, st.h)

		got := tm.waitFor("the screen at "+size(st.w, st.h), func(s snap) bool {
			return s.has(st.want) && !s.has(st.gone)
		})

		if n := len(strings.Split(strings.TrimRight(got.Text, "\n"), "\n")); n > st.h {
			t.Errorf("%s: %d rows", size(st.w, st.h), n)
		}
	}

	tm.resize(80, 24)

	back := tm.waitFor("the welcome page again", func(s snap) bool {
		return s.has("Welcome") && !s.has("Terminal too small") && !s.has("needs") &&
			!s.has("press q")
	})

	if trimRows(back.Text) != trimRows(first.Text) {
		t.Errorf("the screen at 80x24 is not what it was:\n--- before\n%s\n--- after\n%s",
			first.Text, back.Text)
	}

	tm.quit()
}

// TestTooSmallQuit starts the interface on a window that is too small and quits it with q, as
// the hint says.
//
// Parameters:
//   - t: test to report to
func TestTooSmallQuit(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{W: 23, H: 8}, "tote")

	tm.waitText("23 \u00D7 8")
	tm.quit()
}

// TestMinimumSize starts the interface on a window of exactly the minimum, 24x7, where the home
// menu works with its keys: Down four times reaches the last entry, which stays on the screen,
// and Enter opens the settings.
//
// Parameters:
//   - t: test to report to
func TestMinimumSize(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{W: 24, H: 7}, "tote")

	tm.waitText("Connections")
	tm.key("Down", "Down", "Down", "Down", "Down")

	s := tm.waitFor("the settings entry", func(s snap) bool {
		return s.has("Settings") && !s.has("Terminal too small")
	})

	if n := len(strings.Split(strings.TrimRight(s.Text, "\n"), "\n")); n > 7 {
		t.Errorf("24x7: %d rows", n)
	}

	tm.key("Enter")
	tm.waitText("Language")
	tm.quit()
}
