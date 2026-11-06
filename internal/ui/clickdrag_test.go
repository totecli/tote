package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/totecli/tote/internal/config"
)

// TestReviewMouseHomeDoubleClickOpensOnlyTheList checks that a double click on an entry of the
// welcome menu opens its list and does nothing else. It fails in the compact layout: a click on
// an entry opens the screen at once and remembers the cell it hit, and the entries of the
// compact welcome page stand on the very lines where the rows of the compact list start, with the
// same numbers, so the second click of the double click lands on row 0 or 1 of the new list and
// is counted as a double click on it. The first connection is then connected to, the first
// repository opened, whatever the user meant. The full layout does not show it only because the
// rows of the list start lower than the entries of the menu.
//
// Parameters:
//   - t: test to report to
func TestReviewMouseHomeDoubleClickOpensOnlyTheList(t *testing.T) {
	tests := []struct {
		name  string
		cols  int
		rows  int
		edit  func(*config.Settings)
		entry int
	}{
		{"short window, connections", 100, 14, nil, 0},
		{"short window, repositories", 100, 14, nil, 1},
		{"narrow window, connections", 60, 30, nil, 0},
		{
			"welcome compact, connections", 110, 30,
			func(s *config.Settings) { s.UI.Welcome = "compact" }, 0,
		},
		{
			"compact always, repositories", 110, 30,
			func(s *config.Settings) { s.UI.Compact = "always" }, 1,
		},
		{"full layout, connections", 110, 30, nil, 0},
		{"full layout, repositories", 110, 30, nil, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, clk := mouseRig(t, tt.cols, tt.rows, StartHome, func(o *Options) {
				if tt.edit != nil {
					tt.edit(&o.Settings)
				}
			})

			_, hits := g.r.compose(true)

			var entry hit

			for _, h := range hits {
				if h.press == "" && h.id == tt.entry {
					entry = h
				}
			}

			g.click(entry.x+3, entry.y)
			clk.advance(80 * time.Millisecond)
			g.click(entry.x+3, entry.y)

			if _, ok := seenActivate(g); ok || g.Quit {
				t.Errorf("a double click on the entry %d of the menu activated a record of the "+
					"list (activate seen %v, quit %v), the screen is:\n%s", tt.entry, ok, g.Quit,
					g.text())
			}
		})
	}
}

// TestReviewMouseDragFromAHintOrAnEntryActsOnNothing checks that a drag, the gesture that selects
// text, does nothing when it starts on something a click would press. It fails: the press acts at
// once, before the interface can know whether the button is going to move, so a drag that starts
// on the "q quit" hint of the footer quits the program, one that starts on an entry of the
// welcome menu opens the list (and then copies a text of the other screen), and one that starts
// on "esc back" leaves the screen. Text on these cells cannot be selected with the mouse, and
// Shift+drag is the only way.
//
// Parameters:
//   - t: test to report to
func TestReviewMouseDragFromAHintOrAnEntryActsOnNothing(t *testing.T) {
	tests := []struct {
		name  string
		start Start
		on    string
		foot  bool
		same  func(*rig) bool
	}{
		{
			"quit hint of a list", StartConnections, "q quit", true,
			func(g *rig) bool { return !g.Quit },
		},
		{
			"back hint of a list", StartConnections, "esc back", true,
			func(g *rig) bool {
				l, ok := g.r.scr.(*listScreen)

				return ok && l.kind == kindConn
			},
		},
		{
			"entry of the menu", StartHome, "Repos", false,
			func(g *rig) bool {
				_, ok := g.r.scr.(*homeScreen)

				return ok
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _ := mouseRig(t, 110, 30, tt.start, func(o *Options) {
				o.Clipboard = func(context.Context, []string, string) string { return "test" }
			})

			var y, x int

			if tt.foot {
				lines := g.view()
				y = len(lines) - 1
				plain := stripStyles(lines[y])
				x = ansi.StringWidth(plain[:strings.Index(plain, tt.on)])
			} else {
				y, x = lineOf(g, tt.on)
			}

			g.send(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
			g.send(tea.MouseMotionMsg{X: x + 4, Y: y, Button: tea.MouseLeft})
			g.send(tea.MouseReleaseMsg{X: x + 4, Y: y, Button: tea.MouseLeft})

			if !tt.same(g) {
				t.Errorf("a drag over %q acted as a click, the screen is now:\n%s", tt.on,
					g.text())
			}
		})
	}
}

// TestReviewColorAutoIsNotUnknown checks that TOTE_COLOR=auto is not reported as an unknown value.
// It fails: "auto" is the word of the never, always and auto convention that names exactly what
// the interface does when the variable is unset, yet the command line prints "unknown TOTE_COLOR
// value 'auto', using automatic detection", a warning that contradicts itself. ("always" is not
// understood either and falls back to the detection instead of forcing the colors, which the
// warning does say, so it is not tested here.)
//
// Parameters:
//   - t: test to report to
func TestReviewColorAutoIsNotUnknown(t *testing.T) {
	for _, v := range []string{"auto", "AUTO"} {
		if got, bad := UnknownColor([]string{"TOTE_COLOR=" + v}); bad {
			t.Errorf("TOTE_COLOR=%s is reported as unknown (%q)", v, got)
		}
	}
}
