package ui

import (
	"strings"
	"testing"

	"github.com/totecli/tote/internal/config"
)

// TestReviewContractHelpKeyVisible checks that the footer of the welcome page and of a list tells
// the user how to reach the help at 80 columns, the size of a default terminal. It fails: the
// help key is the last hint of every footer, so it is dropped first and appears only above
// about 130 columns on the welcome page and never on a list, which leaves "?" known only to
// those who already opened the help or the palette.
//
// Parameters:
//   - t: test to report to
func TestReviewContractHelpKeyVisible(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"welcome page", nil},
		{"connections", []string{"1"}},
		{"settings", []string{","}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSandbox(t).with(func(o *Options) { o.Width, o.Height = 80, 24 })
			g := newRig(t, s)
			g.press(tt.keys...)

			lines := strings.Split(g.text(), "\n")
			if foot := lines[len(lines)-1]; !strings.Contains(foot, "? help") {
				t.Errorf("no way to find the help in the footer: %q", foot)
			}
		})
	}
}

// TestReviewContractInvalidSettingsReason checks that a config.toml that the loader still refuses,
// here a schema other than 1, says why on the settings screen: the reason is on the screen and
// the file is not described as unreadable. A bad value no longer refuses the file, it is a warning
// and the key keeps its default.
//
// Parameters:
//   - t: test to report to
func TestReviewContractInvalidSettingsReason(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) { o.Width, o.Height = 100, 30 })
	writeFile(t, s.Opts.Paths.ConfigFile(), "schema = 2\n[ui]\ntheme = \"neon\"\n")

	if _, err := config.LoadSettings(s.Opts.Paths.ConfigFile()); err == nil {
		t.Fatal("the loader accepts the file, the premise of the test is wrong")
	}

	g := newRig(t, s)
	g.send(openConfigMsg{})

	text := g.text()
	if strings.Contains(text, "cannot be read") {
		t.Errorf("a readable file is described as unreadable:\n%s", text)
	}

	if !strings.Contains(text, "schema") {
		t.Errorf("the screen does not name the refused schema:\n%s", text)
	}
}

// TestReviewContractPaletteFindsConfig checks that the palette finds the settings by the name
// of the command line verb and of the file, "config". It fails: the home entry, the screen and
// the palette action are all called "Settings", so a user who types "config" in the palette,
// as in "tote config" and config.toml, gets "No result".
//
// Parameters:
//   - t: test to report to
func TestReviewContractPaletteFindsConfig(t *testing.T) {
	g := newRig(t, newSandbox(t))
	g.press("ctrl+k")
	g.typeText("config")

	if text := g.text(); !strings.Contains(text, "Settings") {
		t.Errorf("the palette does not offer the settings for \"config\":\n%s", text)
	}
}

// TestReviewContractFooterKeepsPaletteAndQuit pins that, at 80 columns, the footer of the
// settings screen keeps the keys that quit and the palette, whatever else is dropped.
//
// Parameters:
//   - t: test to report to
func TestReviewContractFooterKeepsPaletteAndQuit(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) { o.Width, o.Height = 80, 24 })
	g := newRig(t, s)
	g.press(",")

	lines := strings.Split(g.text(), "\n")
	foot := lines[len(lines)-1]

	for _, want := range []string{"ctrl+k", "q quit", "esc back"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer lost %q: %q", want, foot)
		}
	}
}

// TestReviewContractSettingLabelsNotCut checks that no label of the settings screen is replaced
// by an ellipsis on a wide terminal, in both languages. It fails: the name column is 24 cells
// wide whatever the terminal, so "After opening a repository" and its French counterpart are
// cut to "After opening a reposit..." even at 160 columns, with 40 cells of the row unused.
//
// Parameters:
//   - t: test to report to
func TestReviewContractSettingLabelsNotCut(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		t.Run(lang, func(t *testing.T) {
			s := newSandbox(t).with(func(o *Options) {
				o.Width, o.Height = 160, 30
				o.Printer = newPrinter(lang)
			})
			g := newRig(t, s)
			g.send(openConfigMsg{})

			for i, line := range strings.Split(g.text(), "\n") {
				if i > 1 && i < 16 && strings.Contains(line, string(rune(0x2026))+"  ") {
					t.Errorf("a label is cut: %q", line)
				}
			}
		})
	}
}
