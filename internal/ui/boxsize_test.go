package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestBoxWidth checks that a box is as wide as its content, between the base width and the caps,
// and within the room.
//
// Parameters:
//   - t: test to report to
func TestBoxWidth(t *testing.T) {
	tests := []struct {
		name                           string
		base, need, limit, screen, max int
		want                           int
	}{
		{"short content", 48, 30, 110, 200, 196, 48},
		{"content between base and cap", 48, 90, 110, 200, 196, 90},
		{"share of the terminal caps", 48, 150, 110, 150, 146, 90},
		{"absolute limit caps", 48, 300, 110, 400, 396, 110},
		{"narrow terminal keeps the base", 48, 90, 110, 80, 76, 48},
		{"share below the base keeps the base", 76, 120, 120, 100, 96, 76},
		{"room caps everything", 76, 120, 120, 200, 70, 70},
		{"room below the base", 76, 120, 120, 40, 36, 36},
		{"tiny room", 76, 120, 120, 5, 1, 4},
		{"unknown screen keeps the base", 48, 90, 110, 0, 96, 48},
		{"unknown content keeps the base", 48, 0, 110, 200, 196, 48},
	}

	for _, tt := range tests {
		if got := boxWidth(tt.base, tt.need, tt.limit, tt.screen, tt.max); got != tt.want {
			t.Errorf("%s: boxWidth(%d, %d, %d, %d, %d) = %d, want %d", tt.name, tt.base,
				tt.need, tt.limit, tt.screen, tt.max, got, tt.want)
		}
	}
}

// TestBoxWidthBounds checks, over many sizes, that a box is never narrower than the base width
// unless the room is, never wider than the room, than its limit or than its share of the screen
// unless the base is wider, and that it never shrinks when the terminal grows.
//
// Parameters:
//   - t: test to report to
func TestBoxWidthBounds(t *testing.T) {
	for _, base := range []int{pickWidth, paletteWidth} {
		for _, need := range []int{0, 10, base, base + 1, 100, 400} {
			prev := 0

			for screen := 1; screen <= 400; screen++ {
				room := max(1, screen-4)
				got := boxWidth(base, need, pickMaxWidth, screen, room)

				if got > max(4, room) || got < min(base, max(4, room)) {
					t.Fatalf("base %d need %d screen %d: width %d, room %d", base, need, screen,
						got, room)
				}

				if got > max(base, min(pickMaxWidth, screen*boxShare/100)) && got > 4 {
					t.Fatalf("base %d need %d screen %d: width %d is over the caps", base, need,
						screen, got)
				}

				if got < prev {
					t.Fatalf("base %d need %d: the box shrank from %d to %d at screen %d", base,
						need, prev, got, screen)
				}

				prev = got
			}
		}
	}
}

// TestPaletteBoxHeight checks the height of the palette box: the small screen height up to a
// terminal of about 27 lines, then growing with the room, capped, and never more than the room.
//
// Parameters:
//   - t: test to report to
func TestPaletteBoxHeight(t *testing.T) {
	tests := []struct{ maxH, want int }{
		{1, 1},
		{4, 4},
		{5, 5},
		{6, 6},
		{10, 10},
		{13, 13},
		{16, 16},
		{22, 16},
		{26, 16},
		{27, 16},
		{28, 16},
		{30, 18},
		{38, 22},
		{48, 28},
		{50, 30},
		{60, 36},
		{66, 39},
		{67, 40},
		{200, 40},
	}

	for _, tt := range tests {
		if got := paletteBoxHeight(tt.maxH); got != tt.want {
			t.Errorf("paletteBoxHeight(%d) = %d, want %d", tt.maxH, got, tt.want)
		}
	}

	prev := 0

	for maxH := 1; maxH <= 300; maxH++ {
		got := paletteBoxHeight(maxH)

		if got > maxH || got < prev || got > paletteMaxHeight && maxH > paletteMaxHeight {
			t.Fatalf("paletteBoxHeight(%d) = %d after %d", maxH, got, prev)
		}

		if want := min(maxH, paletteHeight); got < want {
			t.Fatalf("paletteBoxHeight(%d) = %d, below today's %d", maxH, got, want)
		}

		prev = got
	}
}

// TestPaletteRowsFollowTheBox checks that the rows of the palette are the box height without the
// chrome, with the tight form for a box too short for the rule, at a few terminal heights.
//
// Parameters:
//   - t: test to report to
func TestPaletteRowsFollowTheBox(t *testing.T) {
	tests := []struct {
		maxH, rows int
		tight      bool
	}{
		{1, 1, true},
		{4, 1, true},
		{5, 2, true},
		{6, 2, false},
		{10, 6, false},
		{22, 12, false},
		{38, 18, false},
		{48, 24, false},
		{80, 36, false},
	}

	for _, tt := range tests {
		if got := paletteTight(tt.maxH); got != tt.tight {
			t.Errorf("paletteTight(%d) = %v, want %v", tt.maxH, got, tt.tight)
		}

		if got := rowsFor(tt.maxH); got != tt.rows {
			t.Errorf("rowsFor(%d) = %d, want %d", tt.maxH, got, tt.rows)
		}
	}
}

// wideRig returns a rig on the connections of dataWide, at a size.
//
// Parameters:
//   - t: running test
//   - w: width of the terminal
//   - h: height of the terminal
func wideRig(t *testing.T, w, h int) *rig {
	t.Helper()

	s := frameSandbox(t, dataWide).with(func(o *Options) {
		o.Width, o.Height, o.Start = w, h, StartConnections
	})
	g := newRig(t, s)
	g.resize(w, h)

	return g
}

// TestPaletteAdapts checks the size of the palette box at several terminals: the base size on a
// small one, the longest row up to the caps and a taller box on a wide one, never beyond the room.
//
// Parameters:
//   - t: test to report to
func TestPaletteAdapts(t *testing.T) {
	tests := []struct {
		cols, rows int
		w, h       int
	}{
		{24, 7, 20, 5},
		{40, 10, 36, 8},
		{60, 15, 56, 13},
		{80, 24, 76, 16},
		{100, 30, 76, 16},
		{120, 30, 76, 16},
		{130, 40, 78, 22},
		{200, 40, 120, 22},
		{200, 50, 120, 28},
		{300, 80, 136, 40},
	}

	for _, tt := range tests {
		g := wideRig(t, tt.cols, tt.rows)
		p := openPalette(g)
		e := g.r.e
		box := p.View(e, max(1, e.Cols-4), max(1, e.Height-2))

		if len(box) != tt.h || e.Metric.width(box[0]) != tt.w {
			t.Errorf("%dx%d: box %dx%d, want %dx%d", tt.cols, tt.rows, e.Metric.width(box[0]),
				len(box), tt.w, tt.h)
		}

		for i, l := range box {
			if e.Metric.width(l) != tt.w {
				t.Errorf("%dx%d: line %d is %d cells wide", tt.cols, tt.rows, i,
					e.Metric.width(l))
			}
		}
	}
}

// TestPaletteKeepsItsSizeWhileTyping checks that the box of the palette does not change with
// the query: it follows the longest row of all the entries and not the visible ones.
//
// Parameters:
//   - t: test to report to
func TestPaletteKeepsItsSizeWhileTyping(t *testing.T) {
	g := wideRig(t, 200, 40)
	p := openPalette(g)
	e := g.r.e
	size := func() [2]int {
		box := p.View(e, e.Cols-4, e.Height-2)

		return [2]int{e.Metric.width(box[0]), len(box)}
	}

	first := size()

	for _, text := range []string{"ba", "zzz", "c "} {
		g.press("ctrl+u")
		g.typeText(text)

		if got := size(); got != first {
			t.Errorf("after %q the box is %v, was %v", text, got, first)
		}
	}
}

// TestPaletteWideZones checks that the clicks and the hover of a bigger palette land on the
// right rows: the zone of every result is as wide as the inside of the box, and a click on its
// last cell chooses it.
//
// Parameters:
//   - t: test to report to
func TestPaletteWideZones(t *testing.T) {
	g := wideRig(t, 200, 50)
	p := openPalette(g)
	e := g.r.e
	maxW, maxH := e.Cols-4, e.Height-2
	box := p.View(e, maxW, maxH)
	inner := e.Metric.width(box[0]) - 2
	zs := p.Zones(e, maxW, maxH)

	if want := min(rowsFor(maxH), len(p.hits)); len(zs) != want || want < 10 {
		t.Fatalf("%d zones, want %d (%d rows, %d results)", len(zs), want, rowsFor(maxH),
			len(p.hits))
	}

	for i, z := range zs {
		if z.x != 1 || z.w != inner || z.id != i || z.y != 3+i {
			t.Errorf("zone %d = %+v, want x 1, w %d, y %d", i, z, inner, 3+i)
		}
	}

	y, _ := lineOf(g, "Functions "+e.Glyphs.crumb+" tool")
	line := strings.Split(g.text(), "\n")[y]
	right := e.Metric.width(line[:strings.LastIndex(line, e.Glyphs.v)]) - 1

	g.move(right, y)

	id := slices.IndexFunc(p.hits, func(i int) bool { return p.entries[i].Name == "tool" })

	if h, ok := g.over(); !ok || h.id != id {
		t.Errorf("the pointer on the last cell of the row hovers %+v, %v", h, ok)
	}

	g.click(right, y)

	if sel := p.selected(); sel == nil || sel.Name != "tool" {
		t.Errorf("a click on the last cell of the row chose %+v", sel)
	}

	g.click(right+1, y)

	if len(g.r.overlays) != 1 {
		t.Error("a click on the border closed the palette")
	}
}

// TestPickAdapts checks the width of a list of choices: 48 cells while the content is short or
// the terminal narrow, then the longest row, capped by the screen, pickMaxWidth and the room.
//
// Parameters:
//   - t: test to report to
func TestPickAdapts(t *testing.T) {
	long := []pickItem{
		{label: strings.Repeat("a", 60), value: "1", note: strings.Repeat("n", 20)},
		{label: "short", value: "2", note: "x"},
	}
	short := []pickItem{{label: "one", value: "1", note: "note"}, {label: "two", value: "2"}}

	tests := []struct {
		name       string
		items      []pickItem
		title      string
		cols, rows int
		w          int
	}{
		{"short list on a small screen", short, "Pick", 80, 24, 48},
		{"short list on a wide screen", short, "Pick", 250, 24, 48},
		{"long list on a small screen", long, "Pick", 80, 24, 48},
		{"long list on a medium screen", long, "Pick", 120, 24, 72},
		{"long list on a wide screen", long, "Pick", 200, 24, 88},
		{"long list on a very wide screen", append(long, pickItem{
			label: strings.Repeat("b", 120), value: "3",
		}), "Pick", 250, 24, 110},
		{"long title", short, strings.Repeat("t", 70), 200, 24, 76},
		{"very narrow", long, "Pick", 30, 10, 26},
		{"tiny", long, "Pick", 10, 10, 6},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t))
		g.resize(tt.cols, tt.rows)

		e := g.r.e
		p := &pickOverlay{title: tt.title, items: tt.items}
		maxW, maxH := max(1, e.Cols-4), max(1, e.Height-2)
		box, zs := p.draw(e, maxW, maxH)

		if got := e.Metric.width(box[0]); got != tt.w {
			t.Errorf("%s: box is %d cells wide, want %d", tt.name, got, tt.w)
		}

		for _, l := range box {
			if e.Metric.width(l) != tt.w {
				t.Errorf("%s: a line is %d cells wide, want %d", tt.name, e.Metric.width(l),
					tt.w)
			}
		}

		for i, z := range zs {
			if z.x != 1 || z.w != tt.w-2 || z.y != i+1 {
				t.Errorf("%s: zone %d = %+v, want x 1, w %d, y %d", tt.name, i, z, tt.w-2, i+1)
			}
		}

		if len(zs) != len(tt.items) {
			t.Errorf("%s: %d zones for %d items", tt.name, len(zs), len(tt.items))
		}
	}
}

// TestPickWideShowsWhole checks that a wide terminal shows a long label and its note without the
// ellipsis that a narrow one needs.
//
// Parameters:
//   - t: test to report to
func TestPickWideShowsWhole(t *testing.T) {
	items := []pickItem{{
		label: "v1.30.0-rc.1+build.20261103.0123456789abcdef", value: "1",
		note: "release candidate cut from the maintenance branch",
	}}

	for _, tt := range []struct {
		cols  int
		whole bool
	}{{80, false}, {200, true}} {
		g := newRig(t, newSandbox(t))
		g.resize(tt.cols, 24)
		g.r.push(newPick("Pick", items, 0, func(*env, string) tea.Cmd { return nil }))

		text := g.text()

		got := strings.Contains(text, items[0].label) && strings.Contains(text, items[0].note)

		if got != tt.whole {
			t.Errorf("%d columns: shown whole is %v, want %v:\n%s", tt.cols, got, tt.whole, text)
		}
	}
}

// TestActionMenuWideLabels checks that the menu of the actions grows with long labels on a wide
// terminal, and that the plugin stays at the right of every row, the unavailable one included.
//
// Parameters:
//   - t: test to report to
func TestActionMenuWideLabels(t *testing.T) {
	g := newRig(t, newSandbox(t))
	g.resize(200, 40)

	m := cannedMenu(g.r.e)
	e := g.r.e
	box := m.View(e, e.Cols-4, e.Height-2)

	if w := e.Metric.width(box[0]); w <= pickWidth || w > pickMaxWidth {
		t.Errorf("the menu is %d cells wide with long labels", w)
	}

	g.r.push(m)

	for _, l := range strings.Split(g.text(), "\n") {
		if strings.Contains(l, "Drop a stash") && !strings.Contains(l, "gft "+e.Glyphs.v) {
			t.Errorf("the unavailable row lacks its plugin at the right: %q", l)
		}
	}
}
