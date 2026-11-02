package ui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// hoverMode is a way to draw the interface: the colors of the terminal, the scheme and the glyph
// set. Look is "bg" when the hover is a background and "underline" when it is an underline.
type hoverMode struct {
	name  string
	env   string
	theme string
	ascii bool
	look  string
}

// hoverModes lists the modes the hover is checked in: a background with true color on a dark and
// on a light terminal, and with 256 colors, an underline with 16 colors, with no color at all and
// in ASCII.
var hoverModes = []hoverMode{
	{name: "truecolor", env: "TOTE_COLOR=truecolor", theme: "dark", look: "bg"},
	{name: "truecolor light", env: "TOTE_COLOR=truecolor", theme: "light", look: "bg"},
	{name: "256", env: "TOTE_COLOR=256", theme: "dark", look: "bg"},
	{name: "16", env: "TOTE_COLOR=16", theme: "dark", look: "underline"},
	{name: "none", env: "TOTE_COLOR=none", theme: "dark", look: "underline"},
	{name: "ascii", env: "TOTE_COLOR=none", theme: "dark", ascii: true, look: "underline"},
}

// hoverRig builds a rig on a terminal of a given size, in a mode, with the pointer nowhere.
//
// Parameters:
//   - t: running test
//   - m: mode to draw in
//   - cols: width of the terminal
//   - rows: height of the terminal
//   - start: first screen
//   - edit: changes the options, may be nil
func hoverRig(t *testing.T, m hoverMode, cols, rows int, start Start, edit func(*Options)) *rig {
	t.Helper()

	g, _ := mouseRig(t, cols, rows, start, func(o *Options) {
		o.Environ = append(append([]string(nil), o.Environ...), m.env)
		o.Settings.UI.Theme = m.theme
		o.ASCII = m.ascii

		if m.ascii {
			cut := strings.Index(fixtureConns, "[[connection]]\nname = \"emoji\"")
			writeFile(t, o.Paths.ConnectionsFile(), fixtureConns[:cut])
		}

		if edit != nil {
			edit(o)
		}
	})

	return g
}

// move plays a move of the pointer with no button down, as the terminal reports it in all-motion
// mode.
//
// Parameters:
//   - x: column
//   - y: row
func (g *rig) move(x, y int) {
	g.t.Helper()
	g.send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseNone})
}

// over returns the hovered element and whether there is one.
func (g *rig) over() (hit, bool) {
	return g.r.hover.cur, g.r.hover.over
}

// hoverOn moves the pointer onto the first place where a text is drawn.
//
// Parameters:
//   - sub: text to point at
//
// Returns the cell of the pointer.
func (g *rig) hoverOn(sub string) cellPos {
	g.t.Helper()

	y, x := lineOf(g, sub)
	g.move(x, y)

	return cellPos{x: x, y: y}
}

// footerAt finds the last place where a text is drawn, which is where the footer is.
//
// Parameters:
//   - sub: text looked for
func (g *rig) footerAt(sub string) cellPos {
	g.t.Helper()

	lines := g.view()

	for y := len(lines) - 1; y >= 0; y-- {
		plain := stripStyles(lines[y])
		if i := strings.Index(plain, sub); i >= 0 {
			return cellPos{x: ansi.StringWidth(plain[:i]), y: y}
		}
	}

	g.t.Fatalf("%q is not on the screen:\n%s", sub, g.text())

	return cellPos{}
}

// changedLines lists the lines where two frames differ.
//
// Parameters:
//   - a: first frame
//   - b: second frame
func changedLines(a, b string) []int {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")

	var out []int

	for i := range min(len(x), len(y)) {
		if x[i] != y[i] {
			out = append(out, i)
		}
	}

	return out
}

// sameText reports whether two frames have the same text, apart from the blanks at the end of a
// line: a hovered row that is shorter than its zone is filled with blanks, like the selected row.
//
// Parameters:
//   - a: first frame
//   - b: second frame
func sameText(a, b string) bool {
	x, y := strings.Split(stripStyles(a), "\n"), strings.Split(stripStyles(b), "\n")
	if len(x) != len(y) {
		return false
	}

	for i := range x {
		if strings.TrimRight(x[i], " ") != strings.TrimRight(y[i], " ") {
			return false
		}
	}

	return true
}

// bandOf finds the cells that a look covers on a styled line.
//
// Parameters:
//   - line: styled line
//   - open: sequence that starts the look
//
// Returns the first cell of the first run that starts with the sequence and the first cell after
// the last one, and false when the line does not have the look.
func bandOf(line, open string) (int, int, bool) {
	first := strings.Index(line, open)
	if first < 0 {
		return 0, 0, false
	}

	last := strings.LastIndex(line, open)
	end := len(line)

	if i := strings.Index(line[last:], ansi.ResetStyle); i >= 0 {
		end = last + i
	}

	return ansi.StringWidth(line[:first]), ansi.StringWidth(line[:end]), true
}

// hoverCase is something the pointer can rest on: the screen, the keys that open it, the text to
// point at, and whether the text is looked for from the bottom, where the footer is. Chosen is
// true when the element is the one that the keyboard has selected, which the hover leaves as it is.
// Width is the value of ui.width of a reference frame, empty for the default.
type hoverCase struct {
	name   string
	start  Start
	keys   []string
	target string
	footer bool
	chosen bool
	width  string
}

// hoverCases lists one element of every kind that reacts to the pointer, and the ones that the
// keyboard has selected.
func hoverCases() []hoverCase {
	return []hoverCase{
		{name: "list row", start: StartConnections, target: "staging.example.com"},
		{
			name: "list row in the middle", start: StartConnections, keys: []string{"down"},
			target: "bastion.example.com",
		},
		{name: "repository row", start: StartRepos, target: "docs"},
		{name: "home entry", start: StartHome, target: "Repos"},
		{name: "home settings entry", start: StartHome, target: "Settings"},
		{name: "palette row", start: StartConnections, keys: []string{"ctrl+k"}, target: "staging"},
		{name: "settings row", start: StartConfig, target: "Mouse"},
		{name: "footer hint", start: StartConnections, target: "ctrl+k", footer: true},
		{name: "footer hint of a list", start: StartConnections, target: "q quit", footer: true},
		{name: "confirm button", start: StartConnections, keys: []string{"d"}, target: "[ Yes ]"},
		{name: "picker row", start: StartConfig, keys: []string{"enter"}, target: "English"},
		{name: "form field", start: StartConnections, keys: []string{"n"}, target: "target"},
		{
			name: "footer hint of an overlay", start: StartConnections, keys: []string{"ctrl+k"},
			target: "esc", footer: true,
		},

		{
			name: "selected list row", start: StartConnections, target: "prod.example.com",
			chosen: true,
		},
		{name: "selected home entry", start: StartHome, target: "Connections", chosen: true},
		{
			name: "selected palette row", start: StartConnections, keys: []string{"ctrl+k"},
			target: "prod", chosen: true,
		},
		{name: "selected settings row", start: StartConfig, target: "Language", chosen: true},
		{
			name: "selected confirm button", start: StartConnections, keys: []string{"d"},
			target: "[ No ]", chosen: true,
		},
		{
			name: "selected picker row", start: StartConfig, keys: []string{"enter"},
			target: "auto", chosen: true,
		},
	}
}

// pointAt moves the pointer onto the element of a case, and returns its cell.
//
// Parameters:
//   - g: rig to play in
//   - c: the case
func pointAt(g *rig, c hoverCase) cellPos {
	g.t.Helper()

	if c.footer {
		p := g.footerAt(c.target)
		g.move(p.x, p.y)

		return p
	}

	return g.hoverOn(c.target)
}

// TestHoverKinds moves the pointer onto an element of every kind in every mode: the element
// becomes the hovered one, which is the one a click there would act on, the frame changes on its
// line only, keeps the same text and the same width, and goes back to what it was when the
// pointer leaves. The element that the keyboard has selected is hovered too but is drawn as it
// was, since the selection is the stronger look.
//
// Parameters:
//   - t: test to report to
func TestHoverKinds(t *testing.T) {
	for _, m := range hoverModes {
		for _, c := range hoverCases() {
			t.Run(m.name+"/"+c.name, func(t *testing.T) {
				g := hoverRig(t, m, 100, 30, c.start, nil)
				g.press(c.keys...)

				before := g.r.render()
				p := pointAt(g, c)

				h, ok := g.over()
				if !ok {
					t.Fatalf("nothing is hovered at %v:\n%s", p, g.text())
				}

				click, found := g.r.hitAt(p.x, p.y)
				if !found || click != h {
					t.Errorf("hovered %+v, but a click at %v acts on %+v (%v)", h, p, click, found)
				}

				after := g.r.render()
				if !sameText(after, before) {
					t.Fatalf("the text changed:\n%s\n--\n%s", stripStyles(before),
						stripStyles(after))
				}

				checkScreen(t, m.name+" "+c.name, after, 100, 30, m.ascii)

				if c.chosen {
					if after != before {
						t.Errorf("the selected element is drawn differently when hovered:\n%q\n%q",
							strings.Split(before, "\n")[p.y], strings.Split(after, "\n")[p.y])
					}

					return
				}

				if lines := changedLines(before, after); len(lines) != 1 || lines[0] != p.y {
					t.Errorf("lines %v changed, want only %d", lines, p.y)
				}

				line := strings.Split(after, "\n")[p.y]
				if look := g.r.e.Theme.Hover.String(); !strings.Contains(line, look) {
					t.Errorf("the %s look %q is not on the hovered line: %q", m.look, look, line)
				}

				g.move(0, 0)

				if _, ok := g.over(); ok {
					t.Fatalf("the corner of the screen is an element")
				}

				if got := g.r.render(); got != before {
					t.Errorf("the frame did not come back when the pointer left:\n%s", got)
				}
			})
		}
	}
}

// TestHoverLook checks the look itself in each mode: a background of the palette with 256 colors
// and true color, and an underline of each run of text with 16 colors and none, which leaves the
// gaps between the columns alone.
//
// Parameters:
//   - t: test to report to
func TestHoverLook(t *testing.T) {
	tests := []struct {
		mode    hoverMode
		want    string
		without string
	}{
		{hoverModes[0], "\x1b[48;2;34;31;41m", "\x1b[4m"},
		{hoverModes[1], "\x1b[48;2;242;237;231m", "\x1b[4m"},
		{hoverModes[2], "\x1b[48;5;235m", "\x1b[4m"},
		{hoverModes[3], "\x1b[4m", "48;"},
		{hoverModes[4], "\x1b[4m", "48;"},
		{hoverModes[5], "\x1b[4m", "48;"},
	}

	for _, tt := range tests {
		t.Run(tt.mode.name, func(t *testing.T) {
			g := hoverRig(t, tt.mode, 100, 30, StartConnections, nil)
			p := g.hoverOn("staging.example.com")
			line := strings.Split(g.r.render(), "\n")[p.y]

			if !strings.Contains(line, tt.want) {
				t.Errorf("the line lacks %q: %q", tt.want, line)
			}

			if strings.Contains(line, tt.without) {
				t.Errorf("the line has %q: %q", tt.without, line)
			}

			if tt.mode.look != "underline" {
				return
			}

			for _, run := range []string{"staging", "staging.example.com", "deploy"} {
				if want := "\x1b[4m" + run + "\x1b[m"; !strings.Contains(line, want) {
					t.Errorf("%q is not underlined on its own: %q", run, line)
				}
			}
		})
	}
}

// hoverFiles lists the files below a directory with their size and time of change.
//
// Parameters:
//   - t: running test
//   - dir: directory to list
func hoverFiles(t *testing.T, dir string) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		out = append(out, fmt.Sprintf("%s %d %d", path, info.Size(), info.ModTime().UnixNano()))

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// TestHoverAlone checks what a move of the pointer may cause: no message for a screen or an
// overlay, no command, no program started, no ssh -G, no file written, and no change of the
// screen, the overlays or the keys counted. The pointer crosses every part of the screen of each
// kind of element, the footer included.
//
// Parameters:
//   - t: test to report to
func TestHoverAlone(t *testing.T) {
	for _, c := range hoverCases() {
		t.Run(c.name, func(t *testing.T) {
			var probes atomic.Int32

			g := hoverRig(t, hoverModes[0], 100, 30, c.start, func(o *Options) {
				o.Probe = func(context.Context, []string, []string) (string, error) {
					probes.Add(1)

					return "", errors.New("no ssh")
				}
			})
			g.press(c.keys...)

			dir := filepath.Dir(g.r.e.Paths.Config)
			files := hoverFiles(t, dir)
			seen, depth, screen := len(g.Seen), len(g.r.overlays), fmt.Sprintf("%T", g.r.scr)
			keys, frame := g.r.keys, g.text()

			for y := range 30 {
				for x := 0; x < 100; x += 3 {
					_, cmd := g.r.Update(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseNone})
					if cmd != nil {
						t.Fatalf("a move at %d,%d returned a command", x, y)
					}
				}
			}

			if len(g.Seen) != seen {
				t.Errorf("the moves played messages: %v", g.Seen[seen:])
			}

			if probes.Load() != 0 {
				t.Errorf("a move ran ssh -G %d times", probes.Load())
			}

			if calls := g.r.runner.(*fakeRunner).calls(); len(calls) != 0 {
				t.Errorf("a move started a program: %v", calls)
			}

			if got := hoverFiles(t, dir); fmt.Sprint(got) != fmt.Sprint(files) {
				t.Errorf("a move changed the files:\n%v\n%v", files, got)
			}

			if len(g.r.overlays) != depth || fmt.Sprintf("%T", g.r.scr) != screen ||
				g.r.keys != keys || g.text() != frame {
				t.Errorf("a move changed the screen:\n%s\n--\n%s", frame, g.text())
			}
		})
	}
}

// TestHoverNeverSelects moves the pointer over every row of a list and of the settings, and checks
// that the keyboard selection stays where it was: the hover only highlights.
//
// Parameters:
//   - t: test to report to
func TestHoverNeverSelects(t *testing.T) {
	g := hoverRig(t, hoverModes[0], 100, 30, StartConnections, nil)
	g.press("down", "down")

	list := g.r.scr.(*listScreen)
	if list.sel != 2 {
		t.Fatalf("selected row = %d, want 2", list.sel)
	}

	for _, sub := range []string{"prod.example.com", "staging.example.com", "old.example.com"} {
		g.hoverOn(sub)

		if list.sel != 2 {
			t.Errorf("the pointer on %q moved the selection to %d", sub, list.sel)
		}
	}

	g.press("ctrl+k")
	g.hoverOn("staging")

	pal := g.r.overlays[0].(*paletteOverlay)
	if pal.sel != 0 {
		t.Errorf("the pointer moved the selection of the palette to %d", pal.sel)
	}

	if got := g.text(); !strings.Contains(got, g.r.e.Glyphs.marker+" Connections") {
		t.Errorf("the marker of the selection moved:\n%s", got)
	}
}

// TestHoverSameElement checks that a move that stays on the same element costs nothing: the model
// keeps its state, the view of the last frame is given again instead of being drawn again, and the
// move allocates nothing, so that a stream of them cannot pile up work. A move to another element
// draws the frame again.
//
// Parameters:
//   - t: test to report to
func TestHoverSameElement(t *testing.T) {
	g := hoverRig(t, hoverModes[0], 100, 30, StartConnections, nil)
	p := g.hoverOn("staging")
	first := g.r.View()
	shown := g.r.shown

	if shown == nil {
		t.Fatal("the view was not kept")
	}

	g.move(p.x+3, p.y)

	if !g.r.reuse || g.r.shown != shown {
		t.Fatal("a move on the same row drops the view that was drawn")
	}

	if again := g.r.View(); again.Content != first.Content || g.r.shown != shown {
		t.Error("a move on the same row draws the frame again")
	}

	steps := []tea.MouseMotionMsg{
		{X: p.x + 1, Y: p.y}, {X: p.x + 9, Y: p.y}, {X: p.x + 20, Y: p.y}, {X: p.x + 2, Y: p.y},
	}
	n := 0

	allocs := testing.AllocsPerRun(200, func() {
		g.r.glide(steps[n%len(steps)])
		n++
	})
	if allocs != 0 {
		t.Errorf("a move on the same row allocates %v times", allocs)
	}

	_, cmd := g.r.Update(tea.MouseMotionMsg{X: p.x + 40, Y: p.y})
	if cmd != nil {
		t.Error("a move on the same row returned a command")
	}

	g.move(p.x, p.y+1)

	if g.r.shown != nil {
		t.Error("a move to another row keeps the old view")
	}

	if next := g.r.View(); next.Content == first.Content {
		t.Error("a move to another row does not change the frame")
	}
}

// TestHoverNeedsTheMouse checks that no hover can exist without the mouse: with ui.mouse off, on a
// terminal that has none, on a terminal too small to draw anything and while a program owns the
// terminal, a move changes nothing and nothing is requested from the terminal; and that turning
// the setting off while an element is hovered clears it.
//
// Parameters:
//   - t: test to report to
func TestHoverNeedsTheMouse(t *testing.T) {
	tests := []struct {
		name string
		size [2]int
		edit func(*Options)
		asks bool
	}{
		{"ui.mouse off", [2]int{100, 30}, func(o *Options) { o.Settings.UI.Mouse = false }, false},
		{"dumb terminal", [2]int{100, 30}, func(o *Options) {
			o.Environ = append(o.Environ, "TERM=dumb")
		}, false},
		{"too small", [2]int{minWidth - 1, minHeight + 1}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := hoverRig(t, hoverModes[0], tt.size[0], tt.size[1], StartConnections, tt.edit)
			before := g.r.render()

			for y := range tt.size[1] {
				for x := range tt.size[0] {
					g.move(x, y)
				}
			}

			if g.r.hover.known || g.r.hover.over {
				t.Errorf("the pointer is known: %+v", g.r.hover)
			}

			if got := g.r.render(); got != before {
				t.Error("a move changed the frame")
			}

			if v := g.r.View(); !tt.asks && (v.MouseMode != tea.MouseModeNone || v.ReportFocus) {
				t.Errorf("the view asks for the mouse: %v, focus %v", v.MouseMode, v.ReportFocus)
			}
		})
	}

	t.Run("a program owns the terminal", func(t *testing.T) {
		g := hoverRig(t, hoverModes[0], 100, 30, StartConnections, nil)
		g.hoverOn("staging")

		if !g.r.hover.over {
			t.Fatal("nothing is hovered")
		}

		g.r.guard.begin()
		defer g.r.guard.end()

		g.move(5, 8)

		if g.r.hover.known || g.r.hover.over {
			t.Errorf("a program runs and the pointer is known: %+v", g.r.hover)
		}
	})

	t.Run("turning the setting off", func(t *testing.T) {
		g := hoverRig(t, hoverModes[0], 100, 30, StartConnections, nil)
		g.hoverOn("staging")

		if !g.r.hover.over {
			t.Fatal("nothing is hovered")
		}

		g.r.e.Settings.UI.Mouse = false
		g.press("down")

		if g.r.hover.known || g.r.hover.over {
			t.Errorf("the pointer is known with the mouse off: %+v", g.r.hover)
		}

		if strings.Contains(g.r.render(), g.r.e.Theme.Hover.String()) {
			t.Error("a hover is drawn with the mouse off")
		}
	})
}

// TestHoverDrag checks that a drag that selects text owns the pointer: the hover goes away when
// the drag starts and stays away until the release, and comes back at the first move after the
// release. A press alone, and a click, keep it.
//
// Parameters:
//   - t: test to report to
func TestHoverDrag(t *testing.T) {
	g := hoverRig(t, hoverModes[0], 100, 30, StartConnections, nil)
	p := g.hoverOn("staging")

	if !g.r.hover.over {
		t.Fatal("nothing is hovered")
	}

	g.send(tea.MouseClickMsg{X: p.x, Y: p.y, Button: tea.MouseLeft})

	if !g.r.hover.over {
		t.Error("a press alone cleared the hover")
	}

	g.send(tea.MouseMotionMsg{X: p.x + 6, Y: p.y, Button: tea.MouseLeft})

	if g.r.hover.known || g.r.hover.over {
		t.Errorf("a drag keeps the hover: %+v", g.r.hover)
	}

	if !g.r.sel.drag {
		t.Fatal("the motion with the button held is not a drag")
	}

	if strings.Contains(g.r.render(), g.r.e.Theme.Hover.String()) {
		t.Error("a hover is drawn during a drag")
	}

	g.send(tea.MouseMotionMsg{X: p.x + 9, Y: p.y + 2, Button: tea.MouseLeft})

	if g.r.hover.known || g.r.hover.over {
		t.Errorf("a move with the button held set the hover: %+v", g.r.hover)
	}

	g.send(tea.MouseReleaseMsg{X: p.x + 12, Y: p.y + 2, Button: tea.MouseLeft})

	if g.r.hover.known || g.r.hover.over {
		t.Errorf("the release of a drag set the hover: %+v", g.r.hover)
	}

	g.move(p.x, p.y+2)

	if !g.r.hover.over {
		t.Error("the hover did not come back after the drag")
	}

	g.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	g.click(p.x, p.y)

	if !g.r.hover.known || g.r.hover.at != p {
		t.Errorf("a click lost the pointer: %+v", g.r.hover)
	}
}

// TestHoverLostRelease checks that a plain move while the interface still thinks the button is
// down ends the hold, since a terminal reports every move with a button down as a drag: the release
// went where the terminal could not report it. A press that made no selection is dropped without a
// click, a selection is copied where the pointer was last seen, and the hover works again at once.
//
// Parameters:
//   - t: test to report to
func TestHoverLostRelease(t *testing.T) {
	t.Run("a press", func(t *testing.T) {
		var copied []string

		g, _ := selectionRig(t, StartConnections, &copied, nil)
		y1, x1 := lineOf(g, "staging")
		y2, x2 := lineOf(g, "bastion")
		first := g.r.scr.(*listScreen).sel

		g.send(tea.MouseClickMsg{X: x1, Y: y1, Button: tea.MouseLeft})

		if !g.r.sel.held {
			t.Fatal("the press is not held")
		}

		g.move(x2, y2)

		if g.r.sel.held || g.r.sel.drag {
			t.Errorf("the hold survives a plain move: %+v", g.r.sel)
		}

		if h, ok := g.over(); !ok || h.y != y2 {
			t.Errorf("hovered %+v (%v), want the row at %d", h, ok, y2)
		}

		if got := g.r.scr.(*listScreen).sel; got != first || len(copied) != 0 {
			t.Errorf("a press that was lost acted: row %d, copied %q", got, copied)
		}

		g.send(tea.MouseReleaseMsg{X: x2, Y: y2, Button: tea.MouseLeft})

		if got := g.r.scr.(*listScreen).sel; got != first {
			t.Errorf("a late release clicked: row %d, want %d", got, first)
		}
	})

	t.Run("a drag", func(t *testing.T) {
		var copied []string

		g, _ := selectionRig(t, StartConnections, &copied, nil)
		y, x := lineOf(g, "staging")
		y2, x2 := lineOf(g, "bastion")

		g.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		g.send(tea.MouseMotionMsg{X: x + 6, Y: y, Button: tea.MouseLeft})

		if !g.r.sel.drag {
			t.Fatal("the motion with the button held is not a drag")
		}

		g.move(x2, y2)

		if g.r.sel.held {
			t.Error("the hold survives a plain move")
		}

		g.await("the copy", func() bool { return len(copied) == 1 })

		if copied[0] != "staging" {
			t.Errorf("copied = %q, want the selection up to the last cell seen", copied)
		}

		if h, ok := g.over(); !ok || h.y != y2 {
			t.Errorf("hovered %+v (%v), want the row at %d", h, ok, y2)
		}
	})

	t.Run("no button", func(t *testing.T) {
		var copied []string

		g, _ := selectionRig(t, StartConnections, &copied, nil)
		y, x := lineOf(g, "staging")

		g.move(x, y)
		g.move(x+2, y)

		if len(copied) != 0 || g.r.sel.held || g.r.sel.drag {
			t.Errorf("a plain move selected: %+v, copied %q", g.r.sel, copied)
		}
	})
}

// TestHoverBand checks that a row under the pointer is tinted as far as the selected row is, on
// terminals narrower and wider than the page, whatever the width the table of the screen is drawn
// at: the pointer anywhere in the zone of the row has a band to show for it.
//
// Parameters:
//   - t: test to report to
func TestHoverBand(t *testing.T) {
	screens := []struct {
		name  string
		start Start
	}{
		{"connections", StartConnections},
		{"repositories", StartRepos},
		{"aliases", StartAliases},
		{"settings", StartConfig},
	}

	for _, size := range [][2]int{{80, 24}, {120, 40}, {200, 50}} {
		for _, sc := range screens {
			t.Run(fmt.Sprintf("%s %dx%d", sc.name, size[0], size[1]), func(t *testing.T) {
				g := hoverRig(t, hoverModes[0], size[0], size[1], sc.start, widthSetup("120"))
				th := g.r.e.Theme

				chosen := -1

				for y, l := range g.view() {
					if strings.Contains(l, th.selOpen) {
						chosen = y

						break
					}
				}

				if chosen < 0 {
					t.Fatalf("no selected row:\n%s", g.text())
				}

				c0, c1, _ := bandOf(g.view()[chosen], th.selOpen)
				g.move(c0+2, chosen+1)

				h, ok := g.over()
				if !ok || h.y != chosen+1 {
					t.Fatalf("hovered %+v (%v), want the row below the selected one", h, ok)
				}

				h0, h1, found := bandOf(g.view()[chosen+1], th.Hover.String())
				if !found || h0 != c0 || h1 != c1 {
					t.Errorf("the hover covers cells %d to %d (%v), the selected row %d to %d\n%q",
						h0, h1, found, c0, c1, g.view()[chosen+1])
				}

				if edge := g.r.e.Margin + g.r.e.Width - spareColumns; h1 > edge {
					t.Errorf("the hover ends at %d, past the page edge %d", h1, edge)
				}
			})
		}
	}
}

// TestHoverWaitsForThePointer checks that a session which never sees the pointer pays nothing for
// the hover: keys and messages hover nothing, draw no extra frame and keep no hit.
//
// Parameters:
//   - t: test to report to
func TestHoverWaitsForThePointer(t *testing.T) {
	g := hoverRig(t, hoverModes[0], 100, 30, StartConnections, nil)

	g.press("down", "up", "down", "end", "home")
	g.send(toastMsg{Text: "hello"})
	g.resize(90, 28)

	h := g.r.hover
	if h.known || h.fresh || h.over || len(h.hits) != 0 {
		t.Errorf("the hover works without a pointer: %+v", h)
	}

	if strings.Contains(g.r.render(), g.r.e.Theme.Hover.String()) {
		t.Error("a hover is drawn without a pointer")
	}
}

// TestHoverUnderTheSelection checks that text selected with the mouse is the stronger look: on a
// row under the pointer the selected span is drawn as the selection alone draws it, without the
// hover under it.
//
// Parameters:
//   - t: test to report to
func TestHoverUnderTheSelection(t *testing.T) {
	var copied []string

	g, _ := selectionRig(t, StartConnections, &copied, func(o *Options) {
		o.Environ = append(append([]string(nil), o.Environ...), "TOTE_COLOR=truecolor")
		o.Settings.UI.Theme = "dark"
	})

	y, x := lineOf(g, "staging")
	drag(g, cellPos{x, y}, cellPos{x + 6, y})

	alone := strings.Split(g.r.render(), "\n")[y]
	mark := g.r.e.Theme.Highlight.Render("staging")

	if !strings.Contains(alone, mark) {
		t.Fatalf("the selection is not drawn as %q: %q", mark, alone)
	}

	g.move(x+20, y)

	if h, ok := g.over(); !ok || h.y != y {
		t.Fatalf("hovered %+v (%v), want the row of the selection", h, ok)
	}

	line := strings.Split(g.r.render(), "\n")[y]
	if line == alone {
		t.Fatal("the row is not hovered")
	}

	i := strings.Index(line, mark)
	if i < 0 {
		t.Fatalf("the selected span is lost on the hovered row: %q", line)
	}

	if open := g.r.e.Theme.Hover.String(); strings.HasSuffix(line[:i], open) {
		t.Errorf("the hover is under the selected span: %q", line)
	}
}

// TestHoverNotOnMoreLines checks that the "N more" lines of a list that goes on are not
// highlighted: they scroll when they are clicked and are not items.
//
// Parameters:
//   - t: test to report to
func TestHoverNotOnMoreLines(t *testing.T) {
	g := hoverRig(t, hoverModes[0], 100, 14, StartAliases, nil)
	longAliases(g)

	base := g.r.render()
	p := g.hoverOn("more")

	if _, ok := g.over(); ok {
		t.Errorf("the line %q is hovered", strings.Split(g.text(), "\n")[p.y])
	}

	if g.r.render() != base {
		t.Error("the frame changed over a \"more\" line")
	}

	g.hoverOn("a01")

	if _, ok := g.over(); !ok {
		t.Error("a row of the same list is not hovered")
	}
}

// TestHoverFollowsTheScreen checks, after each change that can move what is under a pointer that
// does not move, that the hovered element is the one a click there would act on, and that the
// frame draws the hover on that line: a scroll, a resize, a switch of screen, an overlay that
// opens and closes, a key that moves the selection and a message.
//
// Parameters:
//   - t: test to report to
func TestHoverFollowsTheScreen(t *testing.T) {
	g := hoverRig(t, hoverModes[0], 100, 30, StartAliases, nil)
	longAliases(g)

	p := g.hoverOn("a03")

	steps := []struct {
		name string
		do   func()
		same bool
	}{
		{"nothing", func() {}, true},
		{"the wheel", func() {
			g.send(tea.MouseWheelMsg{X: p.x, Y: p.y, Button: tea.MouseWheelDown})
		}, false},
		{"the wheel back", func() {
			g.send(tea.MouseWheelMsg{X: p.x, Y: p.y, Button: tea.MouseWheelUp})
		}, false},
		{"a key", func() { g.press("down") }, true},
		{"a toast", func() { g.send(toastMsg{Text: "hello"}) }, true},
		{"a narrower terminal", func() { g.resize(70, 14) }, false},
		{"the palette", func() { g.press("ctrl+k") }, false},
		{"the palette closed", func() { g.press("esc") }, false},
		{"the help", func() { g.press("?") }, false},
		{"the help closed", func() { g.press("esc") }, false},
		{"another list", func() { g.press("h") }, false},
		{"a taller terminal", func() { g.resize(100, 40) }, false},
		{"the home page", func() { g.press("esc") }, false},
	}

	for _, st := range steps {
		st.do()

		h := g.r.hover
		if !h.known || h.at != p {
			t.Fatalf("after %s the pointer is %+v, want %v", st.name, h.at, p)
		}

		want, ok := hoverOf(g.r.composeFrame(true).hits, p)
		if ok != h.over || (ok && want != h.cur) {
			t.Errorf("after %s: hovered %+v (%v), under the pointer %+v (%v)", st.name, h.cur,
				h.over, want, ok)
		}

		if lines := strings.Split(g.r.render(), "\n"); h.over && !strings.Contains(lines[h.cur.y],
			g.r.e.Theme.Hover.String()) && !g.r.e.Theme.selected(lines[h.cur.y]) {
			t.Errorf("after %s the hovered line has no hover: %q", st.name, lines[h.cur.y])
		}
	}

	g.press("ctrl+k")
	g.move(0, 0)
	g.press("esc")

	if _, ok := g.over(); ok {
		t.Error("the hover is on the screen under a pointer at the corner")
	}
}

// TestHoverKeyboardDoesNotMoveIt checks that a key moves the selection and not the hover, and does
// not clear it either.
//
// Parameters:
//   - t: test to report to
func TestHoverKeyboardDoesNotMoveIt(t *testing.T) {
	g := hoverRig(t, hoverModes[0], 100, 30, StartConnections, nil)
	p := g.hoverOn("bastion")
	want, _ := g.over()

	for _, k := range []string{"down", "down", "up", "end", "home"} {
		g.press(k)

		if got, ok := g.over(); !ok || got != want {
			t.Fatalf("after %s the hover is %+v (%v), want %+v", k, got, ok, want)
		}
	}

	if g.r.hover.at != p {
		t.Errorf("the pointer moved to %v", g.r.hover.at)
	}
}

// TestHoverFocus checks that the terminal losing the focus clears the hover, that gaining it sets
// nothing, and that the interface asks for the focus reports along with the mouse, and for nothing
// when the mouse is off.
//
// Parameters:
//   - t: test to report to
func TestHoverFocus(t *testing.T) {
	g := hoverRig(t, hoverModes[0], 100, 30, StartConnections, nil)

	if v := g.r.View(); !v.ReportFocus || v.MouseMode != tea.MouseModeAllMotion {
		t.Errorf("view = focus %v, mouse %v", v.ReportFocus, v.MouseMode)
	}

	g.hoverOn("staging")
	before := g.r.render()

	g.send(tea.BlurMsg{})

	if g.r.hover.known || g.r.hover.over {
		t.Errorf("a blur keeps the hover: %+v", g.r.hover)
	}

	if g.r.render() == before {
		t.Error("the frame still shows the hover after the blur")
	}

	g.send(tea.FocusMsg{})

	if g.r.hover.known || g.r.hover.over {
		t.Errorf("a focus set the hover: %+v", g.r.hover)
	}

	g.hoverOn("staging")

	if !g.r.hover.over {
		t.Error("the hover did not come back after a move")
	}
}

// TestHoverIsWellFormed puts the pointer on every element of several screens, in every mode and at
// several sizes, and applies the rules of checkScreen to the frame: the height, no line wider
// than the screen, no escape but styles and no control character; the text and the width of every
// line are those of the frame without the hover.
//
// Parameters:
//   - t: test to report to
func TestHoverIsWellFormed(t *testing.T) {
	screens := []struct {
		name  string
		start Start
		keys  []string
	}{
		{"home", StartHome, nil},
		{"connections", StartConnections, nil},
		{"settings", StartConfig, []string{"down", "down", "down"}},
		{"palette", StartConnections, []string{"ctrl+k"}},
		{"form", StartConnections, []string{"n"}},
		{"confirm", StartConnections, []string{"d"}},
		{"picker", StartConfig, []string{"enter"}},
	}

	for _, m := range hoverModes {
		for _, sc := range screens {
			for _, size := range [][2]int{{60, 12}, {80, 24}, {140, 30}} {
				name := fmt.Sprintf("%s %s %dx%d", m.name, sc.name, size[0], size[1])
				g := hoverRig(t, m, size[0], size[1], sc.start, nil)
				g.press(sc.keys...)

				base := g.r.render()
				hits := g.r.composeFrame(true).hits

				if len(hits) == 0 {
					t.Fatalf("%s: no element to point at", name)
				}

				for _, h := range hits {
					for _, x := range []int{h.x, h.x + h.w/2, h.x + h.w - 1} {
						g.move(x, h.y)

						got := g.r.render()
						checkScreen(t, name, got, size[0], size[1], m.ascii)

						if !sameText(got, base) {
							t.Fatalf("%s: pointer at %d,%d changed the text:\n%s", name, x, h.y,
								stripStyles(got))
						}
					}
				}
			}
		}
	}
}
