package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
)

// mouseRig builds a rig on a terminal of a given size, with a clock that the test moves.
//
// Parameters:
//   - t: running test
//   - cols: width of the terminal
//   - rows: height of the terminal
//   - start: first screen
//   - edit: changes the options, may be nil
//
// Returns the rig and the clock.
func mouseRig(t *testing.T, cols, rows int, start Start, edit func(*Options)) (*rig, *clock) {
	t.Helper()

	clk := &clock{t: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)}
	s := newSandbox(t).with(func(o *Options) {
		o.Width, o.Height, o.Start, o.Now = cols, rows, start, clk.now

		if edit != nil {
			edit(o)
		}
	})

	g := newRig(t, s)
	g.resize(cols, rows)

	return g, clk
}

// lineOf finds the first line of the screen that holds a text.
//
// Parameters:
//   - g: rig to look at
//   - sub: text looked for, in the screen without styles
//
// Returns the line and the column, in cells, where the text starts. It fails the test when the
// text is not on the screen.
func lineOf(g *rig, sub string) (int, int) {
	g.t.Helper()

	for y, l := range g.view() {
		plain := stripStyles(l)
		if i := strings.Index(plain, sub); i >= 0 {
			return y, ansi.StringWidth(plain[:i])
		}
	}

	g.t.Fatalf("%q is not on the screen:\n%s", sub, g.text())

	return 0, 0
}

// click plays a click of the left button: the press, then the release on the same cell. The
// interface acts at the release, see clickAt.
//
// Parameters:
//   - x: column of the click
//   - y: row of the click
func (g *rig) click(x, y int) {
	g.t.Helper()

	g.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	g.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// clickOn clicks on the first place where a text is drawn.
//
// Parameters:
//   - g: rig to click in
//   - sub: text to click on
func clickOn(g *rig, sub string) {
	g.t.Helper()

	y, x := lineOf(g, sub)
	g.click(x, y)
}

// clickFooter clicks on the last place where a text is drawn, which is where the footer is, below
// anything of the same name higher on the screen.
//
// Parameters:
//   - g: rig to click in
//   - sub: text to click on
func clickFooter(g *rig, sub string) {
	g.t.Helper()

	lines := g.view()

	for y := len(lines) - 1; y >= 0; y-- {
		plain := stripStyles(lines[y])
		if i := strings.Index(plain, sub); i >= 0 {
			g.click(ansi.StringWidth(plain[:i]), y)

			return
		}
	}

	g.t.Fatalf("%q is not on the screen:\n%s", sub, g.text())
}

// seenActivate returns the last activateMsg that the rig played.
//
// Parameters:
//   - g: rig to look at
func seenActivate(g *rig) (activateMsg, bool) {
	var got activateMsg

	found := false

	for _, m := range g.Seen {
		if a, ok := m.(activateMsg); ok {
			got, found = a, true
		}
	}

	return got, found
}

// TestKeyPress checks the names of the keys that a click on the footer can press.
//
// Parameters:
//   - t: test to report to
func TestKeyPress(t *testing.T) {
	tests := []struct {
		name string
		want string
		ok   bool
	}{
		{"esc", "esc", true},
		{"enter", "enter", true},
		{"tab", "tab", true},
		{"ctrl+k", "ctrl+k", true},
		{"ctrl+s", "ctrl+s", true},
		{"n", "n", true},
		{"U", "U", true},
		{"/", "/", true},
		{",", ",", true},
		{"?", "?", true},
		{"ctrl+", "", false},
		{"ctrl+K", "", false},
		{"ctrl+kk", "", false},
		{"up", "", false},
		{"pgdown", "", false},
		{"f1", "", false},
		{"", "", false},
		{" ", "", false},
		{"\u00e9", "", false},
	}

	for _, tt := range tests {
		got, ok := keyPress(tt.name)
		if ok != tt.ok || (ok && got.String() != tt.want) {
			t.Errorf("keyPress(%q) = %q, %v; want %q, %v", tt.name, got.String(), ok, tt.want,
				tt.ok)
		}
	}
}

// TestScrollRows checks the scrolling of the window of a list: with 10 rows and 40 items a window
// shows 8 items between its two "N more" lines, 9 at the top and at the bottom.
//
// Parameters:
//   - t: test to report to
func TestScrollRows(t *testing.T) {
	tests := []struct {
		name                     string
		sel, off, rows, n, lines int
		wantSel, wantOff         int
	}{
		{name: "down one notch", sel: 0, off: 0, rows: 10, n: 40, lines: 3, wantSel: 3, wantOff: 3},
		{
			name: "selection stays when visible", sel: 8, off: 0, rows: 10, n: 40, lines: 3,
			wantSel: 8, wantOff: 3,
		},
		{
			name: "up from the top", sel: 0, off: 0, rows: 10, n: 40, lines: -3, wantSel: 0,
			wantOff: 0,
		},
		{
			name: "up one notch", sel: 12, off: 5, rows: 10, n: 40, lines: -3, wantSel: 9,
			wantOff: 2,
		},
		{
			name: "selection follows up", sel: 14, off: 7, rows: 10, n: 40, lines: -3, wantSel: 11,
			wantOff: 4,
		},
		{
			name: "up to the top", sel: 12, off: 4, rows: 10, n: 40, lines: -30, wantSel: 8,
			wantOff: 0,
		},
		{
			name: "stops at the end", sel: 30, off: 28, rows: 10, n: 40, lines: 30, wantSel: 31,
			wantOff: 31,
		},
		{name: "list that fits", sel: 2, off: 0, rows: 10, n: 5, lines: 3, wantSel: 2, wantOff: 0},
		{name: "empty list", sel: 0, off: 0, rows: 10, n: 0, lines: 3, wantSel: 0, wantOff: 0},
		{name: "no room", sel: 3, off: 3, rows: 0, n: 10, lines: 1, wantSel: 4, wantOff: 4},
		{
			name: "two rows have no lines", sel: 0, off: 0, rows: 2, n: 10, lines: 3, wantSel: 3,
			wantOff: 3,
		},
	}

	for _, tt := range tests {
		sel, off := scrollRows(tt.sel, tt.off, tt.rows, tt.n, tt.lines)
		if sel != tt.wantSel || off != tt.wantOff {
			t.Errorf("%s: scrollRows = %d, %d; want %d, %d", tt.name, sel, off, tt.wantSel,
				tt.wantOff)
		}
	}
}

// TestMouseMode checks when the interface asks the terminal for the mouse: every motion, which is
// what the hover follows, and the focus reports with it; and nothing when the mouse is off.
//
// Parameters:
//   - t: test to report to
func TestMouseMode(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Options)
		want tea.MouseMode
	}{
		{"default", nil, tea.MouseModeAllMotion},
		{"ui.mouse false", func(o *Options) { o.Settings.UI.Mouse = false }, tea.MouseModeNone},
		{
			"dumb terminal", func(o *Options) { o.Environ = append(o.Environ, "TERM=dumb") },
			tea.MouseModeNone,
		},
		{
			"linux console", func(o *Options) { o.Environ = append(o.Environ, "TERM=linux") },
			tea.MouseModeNone,
		},
		{
			"no TERM", func(o *Options) { o.Environ = []string{"LANG=en_US.UTF-8"} },
			tea.MouseModeNone,
		},
		{
			"ascii glyphs keep the mouse", func(o *Options) { o.ASCII = true },
			tea.MouseModeAllMotion,
		},
		{"C locale keeps the mouse", func(o *Options) {
			o.Environ = []string{"TERM=xterm", "LANG=C"}
		}, tea.MouseModeAllMotion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _ := mouseRig(t, 100, 30, StartHome, tt.edit)

			v := g.r.View()

			if v.MouseMode != tt.want {
				t.Errorf("MouseMode = %v, want %v", v.MouseMode, tt.want)
			}

			if v.ReportFocus != (tt.want != tea.MouseModeNone) {
				t.Errorf("ReportFocus = %v with the mouse mode %v", v.ReportFocus, v.MouseMode)
			}
		})
	}
}

// TestMouseSettingFollowsConfig checks that the setting of the Config screen turns the mouse
// off and on at once, and that it is a row of the screen.
//
// Parameters:
//   - t: test to report to
func TestMouseSettingFollowsConfig(t *testing.T) {
	g, _ := mouseRig(t, 100, 30, StartConfig, nil)

	selectRow(g, "mouse")
	g.press("enter")
	g.await("the mouse setting to be written", func() bool { return !g.r.e.Settings.UI.Mouse })

	if got := g.r.View().MouseMode; got != tea.MouseModeNone {
		t.Errorf("MouseMode after the switch = %v, want none", got)
	}

	data, err := os.ReadFile(g.r.e.Paths.ConfigFile())
	if err != nil || !strings.Contains(string(data), "mouse = false") {
		t.Errorf("config.toml does not hold mouse = false: %q, %v", data, err)
	}

	g.press("enter")
	g.await("the mouse setting to be written again", func() bool { return g.r.e.Settings.UI.Mouse })

	if got := g.r.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Errorf("MouseMode after the second switch = %v, want all motion", got)
	}
}

// TestMouseOffIgnoresEverything checks that with the mouse off a click or the wheel changes
// nothing.
//
// Parameters:
//   - t: test to report to
func TestMouseOffIgnoresEverything(t *testing.T) {
	g, _ := mouseRig(t, 100, 30, StartConnections, func(o *Options) {
		o.Settings.UI.Mouse = false
	})

	before := g.text()

	clickOn(g, "staging")
	g.send(tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown})

	if g.text() != before {
		t.Error("the screen changed with the mouse off")
	}
}

// TestMouseClickList checks that a click selects the row it hits, at several widths, and that
// the selected row is drawn on the line that was clicked.
//
// Parameters:
//   - t: test to report to
func TestMouseClickList(t *testing.T) {
	sizes := [][2]int{{100, 30}, {160, 40}, {120, 24}, {80, 24}, {60, 12}}

	for _, sz := range sizes {
		for _, name := range []string{"staging", "bastion", "hostile"} {
			t.Run(fmt.Sprintf("%dx%d/%s", sz[0], sz[1], name), func(t *testing.T) {
				g, _ := mouseRig(t, sz[0], sz[1], StartConnections, widthSetup("120"))

				y, x := lineOf(g, name)
				g.click(x, y)

				l := g.r.scr.(*listScreen)
				if it := l.selected(); it == nil || it.Key != name {
					t.Fatalf("selected = %+v, want %s", it, name)
				}

				if line := stripStyles(g.view()[y]); !strings.Contains(line, g.r.e.Glyphs.marker) {
					t.Errorf("line %d is not the selected row: %q", y, line)
				}
			})
		}
	}
}

// TestMouseClickEdges checks the clicks that must do nothing: outside the rows, with another
// button, with a modifier, and on the margin of a wide terminal.
//
// Parameters:
//   - t: test to report to
func TestMouseClickEdges(t *testing.T) {
	g, _ := mouseRig(t, 160, 40, StartConnections, widthSetup("120"))
	l := g.r.scr.(*listScreen)
	y, x := lineOf(g, "staging")

	tests := []struct {
		name string
		msg  tea.MouseClickMsg
	}{
		{"right button", tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight}},
		{"middle button", tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle}},
		{"control", tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl}},
		{"shift", tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModShift}},
		{"title", tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft}},
		{"margin", tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft}},
		{"below the screen", tea.MouseClickMsg{X: x, Y: 400, Button: tea.MouseLeft}},
		{"negative", tea.MouseClickMsg{X: -1, Y: -1, Button: tea.MouseLeft}},
	}

	for _, tt := range tests {
		g.send(tt.msg)
		g.send(tea.MouseReleaseMsg{X: tt.msg.X, Y: tt.msg.Y, Button: tt.msg.Button})

		if it := l.selected(); it == nil || it.Key != "prod" {
			t.Errorf("%s: selected = %+v, want prod", tt.name, it)
		}
	}

	g.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	g.send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})

	if it := l.selected(); it == nil || it.Key != "prod" {
		t.Errorf("release or drag moved the selection to %+v", it)
	}
}

// TestMouseDoubleClick checks that the second click on a row within 400 ms opens it, that a slow
// second click does not, and that a click on another row starts over.
//
// Parameters:
//   - t: test to report to
func TestMouseDoubleClick(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		s := newSandbox(t)
		clk := &clock{t: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)}
		s = s.with(func(o *Options) { o.Start, o.Now = StartRepos, clk.now })
		g := newRig(t, s)

		clickOn(g, "proj")

		if g.Quit {
			t.Fatal("one click opened the repository")
		}

		clk.advance(200 * time.Millisecond)
		clickOn(g, "proj")

		if !g.Quit || g.r.result.Action != shell.ChangeDir || g.r.result.Payload != s.Repo {
			t.Errorf("quit %v result %+v, want a cd to %s", g.Quit, g.r.result, s.Repo)
		}
	})

	t.Run("too slow", func(t *testing.T) {
		g, clk := mouseRig(t, 100, 30, StartAliases, nil)

		clickOn(g, "ll")
		clk.advance(doubleClickWindow + time.Millisecond)
		clickOn(g, "ll")

		if g.Quit {
			t.Error("a slow second click opened the alias")
		}

		clk.advance(100 * time.Millisecond)
		clickOn(g, "ll")

		if !g.Quit || g.r.result.Payload != "ls -la" {
			t.Errorf("quit %v result %+v, want the alias inserted", g.Quit, g.r.result)
		}
	})

	t.Run("two rows", func(t *testing.T) {
		g, clk := mouseRig(t, 100, 30, StartAliases, nil)

		clickOn(g, "ll")
		clk.advance(50 * time.Millisecond)
		clickOn(g, "gs")

		if g.Quit {
			t.Error("two clicks on two rows opened one")
		}

		if l := g.r.scr.(*listScreen); l.selected().Key != "gs" {
			t.Errorf("selected = %s, want gs", l.selected().Key)
		}
	})

	t.Run("three clicks", func(t *testing.T) {
		g, clk := mouseRig(t, 100, 30, StartConnections, nil)

		clickOn(g, "staging")
		clk.advance(50 * time.Millisecond)
		clickOn(g, "staging")

		if _, ok := seenActivate(g); !ok {
			t.Fatal("the double click did not activate")
		}

		g.Seen = nil
		clk.advance(50 * time.Millisecond)
		clickOn(g, "staging")

		if _, ok := seenActivate(g); ok {
			t.Error("the third click counted as a double click")
		}
	})
}

// TestMouseWheelList checks that the wheel scrolls a list by three rows per notch and keeps the
// selection on screen.
//
// Parameters:
//   - t: test to report to
func TestMouseWheelList(t *testing.T) {
	g, _ := mouseRig(t, 100, 20, StartAliases, nil)

	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i := range 40 {
		fmt.Fprintf(&b, "\n[[alias]]\nname = \"a%02d\"\ncmd = \"echo %d\"\n", i, i)
	}

	writeFile(t, filepath.Join(g.r.e.Paths.Config, "aliases.toml"), b.String())
	g.send(reloadMsg{})

	l := g.r.scr.(*listScreen)
	rows := l.rows(g.r.e)

	if rows >= 40 {
		t.Fatalf("the list has %d rows on screen, the test needs it to scroll", rows)
	}

	wheel := func(b tea.MouseButton) {
		g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: b})
	}

	wheel(tea.MouseWheelDown)

	if l.off != 3 || l.sel != 3 {
		t.Errorf("after one notch off=%d sel=%d, want 3 and 3", l.off, l.sel)
	}

	wheel(tea.MouseWheelDown)
	wheel(tea.MouseWheelDown)

	if l.off != 9 {
		t.Errorf("after three notches off=%d, want 9", l.off)
	}

	if l.sel < l.off || l.sel >= l.off+rows {
		t.Errorf("the selection %d left the window %d..%d", l.sel, l.off, l.off+rows-1)
	}

	if !strings.Contains(g.text(), "a09") || strings.Contains(g.text(), "a08") {
		t.Errorf("the window does not start at the ninth row:\n%s", g.text())
	}

	for range 30 {
		wheel(tea.MouseWheelDown)
	}

	if want := maxOffset(rows, 40); l.off != want || l.sel < l.off {
		t.Errorf("at the end off=%d sel=%d, want %d and a row of the window", l.off, l.sel, want)
	}

	for range 30 {
		wheel(tea.MouseWheelUp)
	}

	if want := windowOf(0, rows, 40).count - 1; l.off != 0 || l.sel != want {
		t.Errorf("at the top off=%d sel=%d, want 0 and %d", l.off, l.sel, want)
	}

	wheel(tea.MouseWheelLeft)
	wheel(tea.MouseWheelRight)

	if l.off != 0 {
		t.Error("a horizontal wheel scrolled the list")
	}

	clickOn(g, "a02")

	if l.selected().Key != "a02" {
		t.Errorf("a click after scrolling selected %s, want a02", l.selected().Key)
	}
}

// TestMouseHome checks that a click on an entry of the menu opens it at once, at several sizes,
// and that the hit of each entry is on the line that shows its name.
//
// Parameters:
//   - t: test to report to
func TestMouseHome(t *testing.T) {
	want := []string{
		"*ui.listScreen", "*ui.listScreen", "*ui.listScreen", "*ui.listScreen",
		"*ui.configScreen",
	}

	for _, size := range [][2]int{{100, 30}, {60, 14}, {160, 40}} {
		for i := range homeEntries {
			g, _ := mouseRig(t, size[0], size[1], StartHome, nil)
			lines, hits := g.r.compose(true)

			var entries []hit

			for _, h := range hits {
				if h.press == "" {
					entries = append(entries, h)
				}
			}

			if len(entries) != homeEntries {
				t.Fatalf("%v: %d entries in the hits, want %d", size, len(entries), homeEntries)
			}

			h := entries[i]
			label := g.r.e.t("ui_config_title")

			if i < len(allKinds) {
				label = g.r.e.t(allKinds[i].titleKey())
			}

			if h.id != i || !strings.Contains(stripStyles(lines[h.y]), label) {
				t.Errorf("%v: entry %d is %+v on %q, want a line with %q", size, i, h,
					stripStyles(lines[h.y]), label)
			}

			g.click(h.x+3, h.y)

			if got := fmt.Sprintf("%T", g.r.scr); got != want[i] {
				t.Errorf("%v: click on entry %d opened %s, want %s", size, i, got, want[i])
			}

			if l, ok := g.r.scr.(*listScreen); ok && l.kind != allKinds[i] {
				t.Errorf("%v: click on entry %d opened the list of %v", size, i, l.kind)
			}
		}
	}
}

// TestMousePalette checks that a click in the palette selects a result, and that a double click
// runs it.
//
// Parameters:
//   - t: test to report to
func TestMousePalette(t *testing.T) {
	g, clk := mouseRig(t, 120, 30, StartHome, nil)

	g.press("ctrl+k")

	p := g.r.overlays[0].(*paletteOverlay)

	clickOn(g, "Connections "+g.r.e.Glyphs.crumb+" staging")

	if en := p.selected(); en == nil || en.Key != "staging" {
		t.Fatalf("selected = %+v, want staging", en)
	}

	if len(g.r.overlays) != 1 {
		t.Fatal("a single click closed the palette")
	}

	clk.advance(100 * time.Millisecond)
	clickOn(g, "Connections "+g.r.e.Glyphs.crumb+" staging")

	if len(g.r.overlays) != 0 {
		t.Error("the double click left the palette open")
	}

	if a, ok := seenActivate(g); !ok || a.Key != "staging" {
		t.Errorf("activateMsg = %+v, %v; want staging", a, ok)
	}
}

// TestMousePaletteKeepsTheQuery checks that a click outside the palette does nothing, even with
// a query typed, and that the wheel scrolls the results.
//
// Parameters:
//   - t: test to report to
func TestMousePaletteKeepsTheQuery(t *testing.T) {
	g, _ := mouseRig(t, 120, 30, StartConnections, nil)

	g.press("ctrl+k")
	g.typeText("pro")

	for _, c := range [][2]int{{0, 0}, {119, 29}, {2, 15}, {60, 0}} {
		g.click(c[0], c[1])
	}

	if len(g.r.overlays) != 1 {
		t.Fatal("a click outside closed the palette")
	}

	if got := g.r.overlays[0].(*paletteOverlay).flt.String(); got != "pro" {
		t.Errorf("query = %q, want pro", got)
	}

	g.press("ctrl+u")

	p := g.r.overlays[0].(*paletteOverlay)
	rows := rowsFor(g.r.e.Height - 2)

	if len(p.hits) <= rows {
		t.Fatalf("%d results fit in %d rows, the test needs more", len(p.hits), rows)
	}

	g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})

	if p.off != 3 || p.sel != 3 {
		t.Errorf("after the wheel off=%d sel=%d, want 3 and 3", p.off, p.sel)
	}

	g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})

	if p.off != 0 {
		t.Errorf("after the wheel up off=%d, want 0", p.off)
	}
}

// TestMouseForm checks that a click on the line of a field gives it the keys, and that a click
// outside the box leaves what was typed.
//
// Parameters:
//   - t: test to report to
func TestMouseForm(t *testing.T) {
	g, _ := mouseRig(t, 100, 30, StartConnections, nil)

	g.press("n")

	f := g.r.overlays[0].(*form)

	g.typeText("web")
	clickOn(g, "description")

	if got := f.fields[f.cur].ID; got != "desc" {
		t.Fatalf("the focus is on %q, want desc", got)
	}

	g.typeText("hello")

	clickOn(g, "name")

	if got := f.fields[f.cur].ID; got != "name" {
		t.Fatalf("the focus is on %q, want name", got)
	}

	g.click(0, 0)

	if len(g.r.overlays) != 1 {
		t.Fatal("a click outside closed the form")
	}

	if f.fields[0].text() != "web" || f.fields[f.indexOf("desc")].text() != "hello" {
		t.Errorf("typed text lost: %q, %q", f.fields[0].text(), f.fields[f.indexOf("desc")].text())
	}
}

// indexOf returns the position of a field of the form by its identifier, -1 when it has none.
//
// Parameters:
//   - id: identifier of the field
func (f *form) indexOf(id string) int {
	for i, fd := range f.fields {
		if fd.ID == id {
			return i
		}
	}

	return -1
}

// TestMouseFormScrolls checks the zones of a form shorter than its fields: only the fields on
// screen are zones, and the one that is clicked gets the keys.
//
// Parameters:
//   - t: test to report to
func TestMouseFormScrolls(t *testing.T) {
	g, _ := mouseRig(t, 100, 12, StartConnections, nil)

	g.press("n")
	g.press("tab", "tab", "tab", "tab")

	f := g.r.overlays[0].(*form)
	_, hits := g.r.compose(true)

	var ids []int

	for _, h := range hits {
		if h.press == "" {
			ids = append(ids, h.id)
		}
	}

	if len(ids) == 0 || len(ids) >= len(f.fields) {
		t.Fatalf("%d field zones for %d fields, want some but not all", len(ids), len(f.fields))
	}

	// The window follows the focus, so the places are read again before each click.
	for _, id := range ids {
		_, now := g.r.compose(true)

		for _, h := range now {
			if h.press == "" && h.id == id {
				g.click(h.x, h.y)

				if f.cur != id {
					t.Errorf("click on the zone of field %d focused field %d", id, f.cur)
				}

				break
			}
		}
	}
}

// TestMouseConfirm checks the two buttons of the confirmation.
//
// Parameters:
//   - t: test to report to
func TestMouseConfirm(t *testing.T) {
	g, _ := mouseRig(t, 100, 30, StartAliases, nil)

	g.press("d")

	if len(g.r.overlays) != 1 {
		t.Fatal("the confirmation did not open")
	}

	g.click(0, 0)

	if len(g.r.overlays) != 1 {
		t.Fatal("a click outside closed the confirmation")
	}

	clickOn(g, "[ No ]")

	if len(g.r.overlays) != 0 {
		t.Fatal("No did not close the confirmation")
	}

	if _, found := g.r.e.Data.record(kindAlias, "ll"); !found {
		t.Fatal("No deleted the alias")
	}

	g.press("d")
	clickOn(g, "[ Yes ]")

	if len(g.r.overlays) != 0 {
		t.Fatal("Yes did not close the confirmation")
	}

	if _, found := g.r.e.Data.record(kindAlias, "ll"); found {
		t.Error("Yes did not delete the alias")
	}
}

// TestMousePick checks the list of choices of a setting: a click selects, a double click
// chooses, and the wheel scrolls.
//
// Parameters:
//   - t: test to report to
func TestMousePick(t *testing.T) {
	g, clk := mouseRig(t, 100, 30, StartConfig, nil)

	selectRow(g, "theme")
	g.press("enter")

	p := g.r.overlays[0].(*pickOverlay)

	clickOn(g, "light")

	if p.items[p.cur].value != "light" || len(g.r.overlays) != 1 {
		t.Fatalf("cur = %q, overlays = %d", p.items[p.cur].value, len(g.r.overlays))
	}

	clk.advance(100 * time.Millisecond)
	clickOn(g, "light")
	g.await("the theme to be written", func() bool { return g.r.e.Settings.UI.Theme == "light" })

	if len(g.r.overlays) != 0 {
		t.Error("the double click left the choices open")
	}
}

// TestMouseWheelOverlays checks the wheel in the help and in the details of a record.
//
// Parameters:
//   - t: test to report to
func TestMouseWheelOverlays(t *testing.T) {
	g, _ := mouseRig(t, 100, 14, StartConnections, nil)

	g.press("?")

	h := g.r.overlays[0].(*helpOverlay)

	// The box measures its text when it is drawn, as it always is before a wheel event arrives.
	_ = g.text()

	g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})

	if h.off != 3 {
		t.Errorf("help offset = %d after a notch, want 3", h.off)
	}

	for range 100 {
		g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	}

	last := maxOffset(h.rows, h.total)
	if h.off != last {
		t.Errorf("help offset = %d at the end, want %d", h.off, last)
	}

	g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})

	if h.off != last-3 {
		t.Errorf("help offset = %d after going up, want %d", h.off, last-3)
	}

	g.press("esc")
	g.press("v")

	i, ok := g.r.overlays[0].(*infoOverlay)
	if !ok {
		t.Fatalf("the details did not open: %T", g.r.overlays[0])
	}

	_ = g.text()

	g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})

	if want := min(3, maxOffset(i.rows, i.total)); i.off != want {
		t.Errorf("details offset = %d, want %d", i.off, want)
	}
}

// TestMouseFooter checks that a click on a hint of the footer presses its key, and only for a
// hint that is one key.
//
// Parameters:
//   - t: test to report to
func TestMouseFooter(t *testing.T) {
	t.Run("new on a list", func(t *testing.T) {
		g, _ := mouseRig(t, 120, 30, StartAliases, nil)

		clickFooter(g, "n new")

		if _, ok := g.r.overlayTop().(*form); !ok {
			t.Errorf("the hint n did not open the form: %T", g.r.overlayTop())
		}
	})

	t.Run("palette on the home", func(t *testing.T) {
		g, _ := mouseRig(t, 120, 30, StartHome, nil)

		clickFooter(g, "ctrl+k")

		if _, ok := g.r.overlayTop().(*paletteOverlay); !ok {
			t.Errorf("the hint ctrl+k did not open the palette: %T", g.r.overlayTop())
		}

		clickFooter(g, "esc close")

		if g.r.overlayTop() != nil {
			t.Error("the hint esc did not close the palette")
		}
	})

	t.Run("enter on the home", func(t *testing.T) {
		g, _ := mouseRig(t, 120, 30, StartHome, nil)

		clickFooter(g, g.r.e.Glyphs.enter+" open")

		if _, ok := g.r.scr.(*listScreen); !ok {
			t.Errorf("the hint enter did not open the selected entry: %T", g.r.scr)
		}
	})

	t.Run("filter", func(t *testing.T) {
		g, _ := mouseRig(t, 120, 30, StartAliases, nil)

		clickFooter(g, "/ filter")

		if !g.r.scr.Typing() {
			t.Error("the hint / did not start the filter")
		}
	})

	t.Run("hints that are not one key", func(t *testing.T) {
		g, _ := mouseRig(t, 160, 30, StartHome, nil)
		_, hits := g.r.compose(true)

		for _, h := range hits {
			if h.press == "" {
				continue
			}

			if _, ok := keyPress(h.press); !ok {
				t.Errorf("hit presses %q, which is not a key", h.press)
			}
		}

		before := g.text()

		clickFooter(g, g.r.e.Glyphs.upDown)
		clickFooter(g, "1-4")

		if g.text() != before {
			t.Error("a hint that is not one key changed the screen")
		}
	})

	t.Run("form", func(t *testing.T) {
		g, _ := mouseRig(t, 120, 30, StartAliases, nil)

		g.press("n")
		clickFooter(g, "esc cancel")

		if g.r.overlayTop() != nil {
			t.Error("the hint esc did not close the form")
		}
	})
}

// overlayTop returns the overlay on top of the stack, nil when there is none.
func (r *root) overlayTop() overlay {
	if n := len(r.overlays); n > 0 {
		return r.overlays[n-1]
	}

	return nil
}

// TestMouseBusy checks that nothing reacts while a program owns the terminal, and on a terminal
// that is too small.
//
// Parameters:
//   - t: test to report to
func TestMouseBusy(t *testing.T) {
	g, _ := mouseRig(t, 100, 30, StartAliases, nil)
	y, x := lineOf(g, "gs")

	g.r.guard.begin()
	g.click(x, y)
	g.send(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})

	if l := g.r.scr.(*listScreen); l.selected().Key != "ll" {
		t.Errorf("a click while a program runs selected %s", l.selected().Key)
	}

	g.r.guard.end()

	g.resize(minWidth-1, minHeight+1)
	g.click(3, 3)

	if len(g.r.overlays) != 0 {
		t.Error("a click on the too small screen did something")
	}

	g.resize(100, 30)
	g.click(x, y)

	if l := g.r.scr.(*listScreen); l.selected().Key != "gs" {
		t.Errorf("the click after the program selected %s, want gs", l.selected().Key)
	}
}

// TestHitsMatchFrame checks, for every screen and overlay at several sizes, that each hit is
// inside the terminal and lies on drawn text, so that a hit cannot point at an empty cell.
//
// Parameters:
//   - t: test to report to
func TestHitsMatchFrame(t *testing.T) {
	opens := []struct {
		name  string
		start Start
		keys  []string
	}{
		{"home", StartHome, nil},
		{"connections", StartConnections, nil},
		{"repos", StartRepos, nil},
		{"aliases", StartAliases, nil},
		{"funcs", StartFuncs, nil},
		{"config", StartConfig, nil},
		{"filter", StartConnections, []string{"/"}},
		{"palette", StartHome, []string{"ctrl+k"}},
		{"palette query", StartHome, []string{"ctrl+k", "c", " ", "p"}},
		{"form", StartConnections, []string{"n"}},
		{"edit", StartRepos, []string{"e"}},
		{"confirm", StartConnections, []string{"d"}},
		{"help", StartConnections, []string{"?"}},
		{"details", StartConnections, []string{"v"}},
		{"pick", StartConnections, []string{"U"}},
	}

	for _, size := range [][2]int{{60, 12}, {80, 24}, {100, 30}, {110, 30}, {120, 40}, {160, 40}} {
		for _, o := range opens {
			for _, ascii := range []bool{false, true} {
				name := fmt.Sprintf("%dx%d/%s/%v", size[0], size[1], o.name, ascii)

				t.Run(name, func(t *testing.T) {
					checkHits(t, size, o.start, o.keys, ascii)
				})
			}
		}
	}
}

// checkHits opens a screen, and checks that every hit lies inside the terminal on drawn text.
//
// Parameters:
//   - t: running test
//   - size: width and height of the terminal
//   - start: first screen
//   - keys: keys that open an overlay on it, may be empty
//   - ascii: true for the ASCII glyph set
func checkHits(t *testing.T, size [2]int, start Start, keys []string, ascii bool) {
	t.Helper()

	g, _ := mouseRig(t, size[0], size[1], start, func(o *Options) { o.ASCII = ascii })
	g.press(keys...)

	lines, hits := g.r.compose(true)

	for _, h := range hits {
		if h.y < 0 || h.y >= len(lines) || h.x < 0 || h.w <= 0 || h.x+h.w > size[0] {
			t.Errorf("hit %+v is outside the %dx%d terminal", h, size[0], size[1])

			continue
		}

		plain := stripStyles(lines[h.y])
		if strings.TrimSpace(ansi.Cut(plain, h.x, h.x+h.w)) == "" {
			t.Errorf("hit %+v lies on blank cells of %q", h, plain)
		}
	}
}

// TestHitsFollowTheFrame checks that the hits come from the same layout as the frame: the line
// that holds a row is the line of its hit, for every record of every list, at a width with a
// margin and at one without.
//
// Parameters:
//   - t: test to report to
func TestHitsFollowTheFrame(t *testing.T) {
	for _, width := range []string{"auto", "120"} {
		for _, cols := range []int{100, 160} {
			for _, start := range []Start{StartConnections, StartRepos, StartAliases, StartFuncs} {
				g, _ := mouseRig(t, cols, 30, start, widthSetup(width))
				l := g.r.scr.(*listScreen)

				for i := range l.shown {
					name := l.items[l.shown[i]].Key
					y, x := lineOf(g, name)

					g.click(x, y)

					if l.selected().Key != name {
						t.Errorf("%s %d/%d: click on %q selected %q", width, cols, start, name,
							l.selected().Key)
					}
				}
			}
		}
	}
}

// TestUnknownColor checks the report of a value of TOTE_COLOR that is not understood.
//
// Parameters:
//   - t: test to report to
func TestUnknownColor(t *testing.T) {
	tests := []struct {
		env  []string
		want string
		bad  bool
	}{
		{nil, "", false},
		{[]string{"TOTE_COLOR="}, "", false},
		{[]string{"TOTE_COLOR=truecolor"}, "", false},
		{[]string{"TOTE_COLOR=never"}, "", false},
		{[]string{"TOTE_COLOR=Off"}, "", false},
		{[]string{"TOTE_COLOR=x"}, "x", true},
		{[]string{"TOTE_COLOR=always"}, "", false},
		{[]string{"TOTE_COLOR=auto"}, "", false},
		{[]string{"TOTE_COLOR=Always"}, "", false},
		{[]string{"TOTE_COLOR=sometimes"}, "sometimes", true},
		{[]string{"TOTE_COLOR=\x1b[31mred"}, "?[31mred", true},
		{[]string{"TOTE_COLOR=" + strings.Repeat("a", 100)}, strings.Repeat("a", 40), true},
	}

	for _, tt := range tests {
		got, bad := UnknownColor(tt.env)
		if got != tt.want || bad != tt.bad {
			t.Errorf("UnknownColor(%q) = %q, %v; want %q, %v", tt.env, got, bad, tt.want, tt.bad)
		}
	}
}

// TestMouseState checks the description of the mouse for the doctor.
//
// Parameters:
//   - t: test to report to
func TestMouseState(t *testing.T) {
	on := config.DefaultSettings()
	off := on
	off.UI.Mouse = false

	tests := []struct {
		name string
		term string
		off  bool
		want string
	}{
		{"default", "xterm-256color", false, "on"},
		{"setting off", "xterm-256color", true, "setting"},
		{"dumb", "dumb", false, "term"},
		{"linux", "linux", false, "term"},
		{"unset", "", false, "term"},
		{"setting wins over the terminal", "dumb", true, "setting"},
	}

	for _, tt := range tests {
		s := on
		if tt.off {
			s = off
		}

		get := func(k string) string {
			if k == "TERM" {
				return tt.term
			}

			return ""
		}

		if got := MouseState(s, get); got != tt.want {
			t.Errorf("%s: MouseState = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestFooterSpans checks the places that the footer reports for its hints: they are in order, do
// not overlap, stay inside the width, lie on the text of the hint, and name a key that a click
// can press.
//
// Parameters:
//   - t: test to report to
func TestFooterSpans(t *testing.T) {
	for _, start := range []Start{StartHome, StartConnections, StartRepos, StartConfig} {
		for _, lang := range []string{"en", "fr"} {
			for _, ascii := range []bool{false, true} {
				g := newRig(t, newSandbox(t).with(func(o *Options) {
					o.Start, o.Printer, o.ASCII = start, newPrinter(lang), ascii
				}))
				e := g.r.e
				bs := g.r.scr.Keys(e)

				for _, short := range []bool{false, true} {
					for w := 150; w >= 4; w -= 3 {
						line, spans := footerDraw(e, bs, short, w)
						plain := stripStyles(line)
						end := 0

						for _, sp := range spans {
							if sp.x < end || sp.w <= 0 || sp.x+sp.w > w {
								t.Fatalf("%d/%s/%v/%v at %d: span %+v after %d in %q", start, lang,
									ascii, short, w, sp, end, plain)
							}

							end = sp.x + sp.w

							if strings.TrimSpace(ansi.Cut(plain, sp.x, end)) == "" {
								t.Fatalf("%d/%s/%v/%v at %d: span %+v is blank in %q", start, lang,
									ascii, short, w, sp, plain)
							}

							if _, ok := keyPress(sp.press); sp.press != "" && !ok {
								t.Fatalf("span %+v presses %q, which is not a key", sp, sp.press)
							}
						}
					}
				}
			}
		}
	}
}

// TestClickKey checks which hints are one key.
//
// Parameters:
//   - t: test to report to
func TestClickKey(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e
	k := e.Keys

	tests := []struct {
		name  string
		b     key.Binding
		label string
		want  string
	}{
		{"esc", k.Back, "esc", "esc"},
		{"enter glyph", k.Open, e.Glyphs.enter, "enter"},
		{"new", k.New, "n", "n"},
		{"quit has two keys", k.Quit, "q", "q"},
		{"palette", k.Palette, "ctrl+k", "ctrl+k"},
		{"help", k.Help, "?", "?"},
		{"filter", k.Filter, "/", "/"},
		{"navigate", k.Nav, e.Glyphs.upDown, ""},
		{"jump", k.Jump, "1-4", ""},
		{"tabs", k.Tabs, "h/l", ""},
		{"scroll", k.Scroll, e.Glyphs.upDown, ""},
		{"label of another key", k.New, "x", ""},
	}

	for _, tt := range tests {
		if got := clickKey(tt.b, tt.label, e.Glyphs.enter); got != tt.want {
			t.Errorf("%s: clickKey = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestMouseConfig checks that a click on a row of the settings selects it, that a double click
// changes it, and that the wheel scrolls.
//
// Parameters:
//   - t: test to report to
func TestMouseConfig(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {60, 12}, {160, 40}} {
		g, clk := mouseRig(t, size[0], size[1], StartConfig, nil)
		s := g.r.scr.(*configScreen)

		clickOn(g, "Theme")

		if s.def().id != "theme" {
			t.Fatalf("%v: click on Theme selected %s", size, s.def().id)
		}

		// A small screen shows only some of the rows: the wheel brings the one wanted into view.
		for range 4 {
			if strings.Contains(g.text(), "Mouse") {
				break
			}

			g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
		}

		clickOn(g, "Mouse")

		if s.def().id != "mouse" {
			t.Fatalf("%v: click on Mouse selected %s", size, s.def().id)
		}

		clk.advance(50 * time.Millisecond)
		clickOn(g, "Mouse")
		g.await("the mouse setting to be written", func() bool { return !g.r.e.Settings.UI.Mouse })
	}
}

// TestMouseActsOnRelease checks that a click happens at the release of the button: a press alone
// opens nothing and presses no hint, because it may be the start of a drag that selects text.
//
// Parameters:
//   - t: test to report to
func TestMouseActsOnRelease(t *testing.T) {
	g, _ := mouseRig(t, 110, 30, StartHome, nil)
	y, x := lineOf(g, "Connections")

	g.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})

	if _, ok := g.r.scr.(*homeScreen); !ok {
		t.Fatalf("a press alone opened %T", g.r.scr)
	}

	g.send(tea.MouseReleaseMsg{X: x + 2, Y: y, Button: tea.MouseLeft})

	if _, ok := g.r.scr.(*homeScreen); !ok {
		t.Fatalf("a release on another cell, with no move, opened %T", g.r.scr)
	}

	g.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	g.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})

	if l, ok := g.r.scr.(*listScreen); !ok || l.kind != kindConn {
		t.Errorf("a press and a release on the entry opened %T", g.r.scr)
	}

	g, _ = mouseRig(t, 110, 30, StartHome, nil)
	clickFooter(g, "q quit")

	if !g.Quit {
		t.Error("a click on the hint q quit did not quit")
	}
}

// TestMouseDoubleClickHintActsOnce checks that the second click of a double click on a hint of
// the footer does not press the hint that stands on the same cell of the next screen: leaving a
// list for the welcome page must not open the help or the palette on the way.
//
// Parameters:
//   - t: test to report to
func TestMouseDoubleClickHintActsOnce(t *testing.T) {
	g, clk := mouseRig(t, 110, 30, StartConnections, nil)

	lines := g.view()
	y := len(lines) - 1
	plain := stripStyles(lines[y])
	x := ansi.StringWidth(plain[:strings.Index(plain, "esc back")])

	g.click(x, y)
	clk.advance(80 * time.Millisecond)
	g.click(x, y)

	if _, ok := g.r.scr.(*homeScreen); !ok || len(g.r.overlays) != 0 || g.Quit {
		t.Errorf("a double click on esc ended on %T with %d overlays:\n%s", g.r.scr,
			len(g.r.overlays), g.text())
	}

	clk.advance(2 * doubleClickWindow)
	g.click(x, y)

	if len(g.r.overlays) == 0 && !g.Quit {
		t.Logf("a click a while later on that cell did nothing, the screen is:\n%s", g.text())
	}
}

// TestMouseKeyBreaksDoubleClick checks that a key pressed between two clicks on the same row
// makes the second one a single click.
//
// Parameters:
//   - t: test to report to
func TestMouseKeyBreaksDoubleClick(t *testing.T) {
	g, clk := mouseRig(t, 110, 30, StartConnections, nil)
	y, x := lineOf(g, "staging")

	g.click(x, y)
	g.press("up")
	clk.advance(50 * time.Millisecond)
	g.click(x, y)

	if _, ok := seenActivate(g); ok {
		t.Error("a key between two clicks did not break the double click")
	}

	if l := g.r.scr.(*listScreen); l.selected().Key != "staging" {
		t.Errorf("the second click selected %q", l.selected().Key)
	}
}

// TestMouseDisarmsQuit checks that a click or the wheel cancels the first Ctrl-C of the welcome
// page, as any key does, so that a second Ctrl-C later does not quit without a warning.
//
// Parameters:
//   - t: test to report to
func TestMouseDisarmsQuit(t *testing.T) {
	for _, how := range []string{"click", "wheel"} {
		g, _ := mouseRig(t, 110, 30, StartHome, nil)
		g.press("ctrl+c")

		if !strings.Contains(g.toastText(), "again") {
			t.Fatalf("%s: no warning after the first Ctrl-C: %q", how, g.toastText())
		}

		if how == "click" {
			clickOn(g, "Connections")
			g.press("ctrl+c")
		} else {
			g.send(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		}

		g.press("ctrl+c")

		if g.Quit {
			t.Errorf("%s: the second Ctrl-C quit after the mouse was used", how)
		}
	}
}
