package e2e

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// leadOf returns the number of blank cells before the first line of the pane that holds a text.
//
// Parameters:
//   - s: look at the pane
//   - sub: text looked for
//
// Returns -1 when no line holds the text.
func leadOf(s snap, sub string) int {
	for _, l := range strings.Split(s.Text, "\n") {
		if strings.Contains(l, sub) {
			return utf8.RuneCountInString(l) - utf8.RuneCountInString(strings.TrimLeft(l, " "))
		}
	}

	return -1
}

// edgeOf returns the cell after the last text of the first line of the pane that holds a text.
//
// Parameters:
//   - s: look at the pane
//   - sub: text looked for
//
// Returns -1 when no line holds the text.
func edgeOf(s snap, sub string) int {
	for _, l := range strings.Split(s.Text, "\n") {
		if strings.Contains(l, sub) {
			return utf8.RuneCountInString(strings.TrimRight(l, " "))
		}
	}

	return -1
}

// TestPageWidthMargin starts the real binary on a terminal of 200 columns for every value of
// ui.width, with the key absent and with no file at all: the page starts at the first cell for
// "auto" and for the default, where it also reaches the last two cells of the right edge that it
// keeps free, after 40 blank cells for "120" and after 20 for "160".
//
// Parameters:
//   - t: test to report to
func TestPageWidthMargin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		lead int
		edge int
	}{
		{"no file", "", 0, 198},
		{"absent key", "schema = 1\n[ui]\ncompact = \"never\"\n", 0, 198},
		{"auto", "schema = 1\n[ui]\nwidth = \"auto\"\n", 0, 198},
		{"120", "schema = 1\n[ui]\nwidth = \"120\"\n", 40, 158},
		{"160", "schema = 1\n[ui]\nwidth = \"160\"\n", 20, 178},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)

			if tt.file != "" {
				sb.write(t, configPath(sb), tt.file)
			}

			tm := newTerm(t, sb, startOptions{W: 200, H: 40}, "tote", "co")
			s := tm.waitText(cmdOf("alpha"))

			if got := leadOf(s, "tote \u203a"); got != tt.lead {
				t.Errorf("the page starts after %d cells, want %d:\n%s", got, tt.lead, s.Text)
			}

			if got := edgeOf(s, "tote \u203a"); got != tt.edge {
				t.Errorf("the title ends at cell %d, want %d:\n%s", got, tt.edge, s.Text)
			}

			tm.quit()
		})
	}
}

// TestPageWidthInvalid starts the real binary with a ui.width it does not know: the key is a
// warning, not a reason to drop the file, so the page keeps the default width, the other
// settings apply, and the warning names the key and the allowed values on standard error.
//
// Parameters:
//   - t: test to report to
func TestPageWidthInvalid(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, configPath(sb), "schema = 1\n[ui]\nwidth = \"200\"\nmouse = false\n")

	_, errOut, code := runIn(t, sb, "co", "-l")
	want := "ui.width is \"200\", allowed values: auto, 120, 160"
	if code != 0 || !strings.Contains(errOut, want) {
		t.Errorf("code %d, stderr %q", code, errOut)
	}

	tm := newTerm(t, sb, startOptions{W: 200, H: 40}, "tote", "co")
	s := tm.waitText(cmdOf("alpha"))

	if got := leadOf(s, "tote \u203a"); got != 0 {
		t.Errorf("the page starts after %d cells, want the default 0:\n%s", got, s.Text)
	}

	tm.quit()
}

// TestPageWidthLive changes ui.width with the right and left keys on the settings screen of the
// real binary, on a terminal of 200 columns: the page moves at once, config.toml keeps its
// comments and its other keys, and the page follows a resize of the window under each value.
//
// Parameters:
//   - t: test to report to
func TestPageWidthLive(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, configPath(sb), userConfig)

	tm := newTerm(t, sb, startOptions{W: 200, H: 40, Env: []string{"TOTE_LANG="}}, "tote")

	tm.waitText("Connections")
	tm.typeText(",")
	s := tm.waitText("After a connection")

	if got := leadOf(s, "tote \u203a"); got != 0 {
		t.Fatalf("the settings start after %d cells, want 0:\n%s", got, s.Text)
	}

	// Page width is the tenth row, after Compact layout.
	tm.key("Down", "Down", "Down", "Down", "Down", "Down", "Down", "Down", "Down")
	tm.waitText("\u2039 auto (terminal width) \u203a")

	has := func(want string) func(string) bool {
		return func(c string) bool { return strings.Contains(c, want) }
	}

	steps := []struct {
		key   string
		want  string
		label string
		lead  int
	}{
		{"Right", "120", "120 columns", 40},
		{"Right", "160", "160 columns", 20},
		{"Right", "auto", "auto (terminal width)", 0},
		{"Left", "160", "160 columns", 20},
	}

	for _, st := range steps {
		tm.key(st.key)
		tm.waitFile("../config/config.toml", has(`width = "`+st.want+`"`))
		tm.waitText("\u2039 " + st.label + " \u203a")
		tm.waitFor("the page to move to "+st.want, func(s snap) bool {
			return leadOf(s, "tote \u203a") == st.lead
		})
	}

	got := readFile(t, configPath(sb))

	for _, want := range []string{
		"# my own note", "# trailing note", `editor = "nano -w"`,
		`compact = "never"`, `after_disconnect = "menu"`, `width = "160"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config.toml lost %q:\n%s", want, got)
		}
	}

	// A resize follows the setting: 160 columns of page on a window of 130, then of 200 again.
	tm.resize(130, 40)
	tm.waitFor("a page of 130 cells", func(s snap) bool { return leadOf(s, "tote \u203a") == 0 })
	tm.resize(200, 40)
	tm.waitFor("the margin of 20 again", func(s snap) bool {
		return leadOf(s, "tote \u203a") == 20
	})

	tm.key("Escape")
	tm.waitText("Your terminal tools")
	tm.quit()
}
