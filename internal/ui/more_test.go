package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// moreRe finds a "N more" line of the screen without styles: the arrow, the number and the word.
var moreRe = regexp.MustCompile(`([\x{2191}\x{2193}^v]) (\d+) (?:more|de plus)`)

// moreOf reads the "N more" lines of a screen.
//
// Parameters:
//   - text: the screen without styles
//
// Returns the number above and the number below, -1 for a side that has no line.
func moreOf(text string) (int, int) {
	up, down := -1, -1

	for _, m := range moreRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[2])

		switch m[1] {
		case "\u2191", "^":
			up = n
		case "\u2193", "v":
			down = n
		}
	}

	return up, down
}

// aliasFile writes a file of n aliases named prefix000, prefix001 and so on.
//
// Parameters:
//   - s: sandbox to write in
//   - prefix: start of every name
//   - n: number of aliases
func aliasFile(t *testing.T, s *sandbox, prefix string, n int) {
	t.Helper()

	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i := range n {
		fmt.Fprintf(&b, "\n[[alias]]\nname = \"%s%03d\"\ncmd = \"echo %d\"\n", prefix, i, i)
	}

	writeFile(t, s.Opts.Paths.AliasesFile(), b.String())
}

// scrollRig builds a rig on a sandbox the test has filled, with a terminal of a given size.
//
// Parameters:
//   - t: running test
//   - s: sandbox holding the data
//   - cols: width of the terminal
//   - rows: height of the terminal
//   - start: first screen
func scrollRig(t *testing.T, s *sandbox, cols, rows int, start Start) *rig {
	t.Helper()

	clk := &clock{t: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)}
	g := newRig(t, s.with(func(o *Options) {
		o.Width, o.Height, o.Start, o.Now = cols, rows, start, clk.now
	}))
	g.resize(cols, rows)

	return g
}

// TestMoreLines walks every position of a list in each place that scrolls and checks the "N more"
// lines: the exact numbers, a line only where rows are hidden, the rows and the lines filling the
// window, and the selected row on screen.
//
// Parameters:
//   - t: test to report to
func TestMoreLines(t *testing.T) {
	t.Run("palette", func(t *testing.T) {
		s := newSandbox(t)
		aliasFile(t, s, "pal", 60)

		g := scrollRig(t, s, 100, 24, StartHome)
		p := openPalette(g)
		g.typeText("a pal")

		checkMore(t, g, len(p.hits), regexp.MustCompile(`pal\d{3}`), func(to int) {
			p.move(g.r.e, to)
		}, func() int { return p.sel }, rowsFor(22))
	})

	t.Run("list", func(t *testing.T) {
		s := newSandbox(t)
		aliasFile(t, s, "lst", 40)

		g := scrollRig(t, s, 100, 20, StartAliases)
		l := g.r.scr.(*listScreen)

		checkMore(t, g, len(l.shown), regexp.MustCompile(`lst\d{3}`), func(to int) {
			l.move(g.r.e, to)
		}, func() int { return l.sel }, l.rows(g.r.e))
	})

	t.Run("settings", func(t *testing.T) {
		g := scrollRig(t, newSandbox(t), 60, 12, StartConfig)
		c := g.r.scr.(*configScreen)

		var names []string
		for _, d := range settingDefs {
			names = append(names, regexp.QuoteMeta(g.r.e.t(d.nameKey())))
		}

		item := regexp.MustCompile(`(?m)^ +(?:\x{276F} )?(?:` + strings.Join(names, "|") + `)  `)

		checkMore(t, g, len(settingDefs), item, func(to int) { c.move(g.r.e, to) },
			func() int { return c.sel }, c.rows(g.r.e))
	})

	t.Run("choices", func(t *testing.T) {
		g := scrollRig(t, newSandbox(t), 80, 14, StartHome)
		items := make([]pickItem, 30)

		for i := range items {
			items[i] = pickItem{label: fmt.Sprintf("choice%02d", i), value: fmt.Sprint(i)}
		}

		g.r.push(newPick("Pick", items, 0, func(*env, string) tea.Cmd { return nil }))

		p := g.top().(*pickOverlay)

		checkMore(t, g, len(items), regexp.MustCompile(`choice\d{2}`), func(to int) {
			p.move(to-p.cur, p.rows(g.r.e.Height-2))
		}, func() int { return p.cur }, p.rows(g.r.e.Height-2))
	})
}

// checkMore moves the selection through every row of a list and checks what the screen says.
//
// Parameters:
//   - t: running test
//   - g: rig showing the list
//   - n: number of rows of the list
//   - item: pattern that one drawn row matches
//   - move: puts the selection on a row
//   - sel: reads the selected row
//   - rows: rows of the window, "N more" lines included
func checkMore(t *testing.T, g *rig, n int, item *regexp.Regexp, move func(int), sel func() int,
	rows int,
) {
	t.Helper()

	if n <= rows {
		t.Fatalf("%d rows fit in %d: nothing to scroll", n, rows)
	}

	seenUp, seenDown, seenBoth := false, false, false

	for to := range n {
		move(to)

		text := g.text()
		up, down := moreOf(text)
		shown := len(item.FindAllString(text, -1))
		name := fmt.Sprintf("selection %d of %d", sel(), n)

		if sel() != to {
			t.Fatalf("%s: moving to %d", name, to)
		}

		if max(up, 0)+shown+max(down, 0) != n {
			t.Fatalf("%s: %d above, %d shown and %d below do not make %d:\n%s", name, up, shown,
				down, n, text)
		}

		if (up > 0) != (up >= 0) || (down > 0) != (down >= 0) {
			t.Fatalf("%s: a line says there is nothing hidden: %d, %d", name, up, down)
		}

		if want := b2i(up >= 0) + b2i(down >= 0) + shown; want != rows {
			t.Fatalf("%s: %d rows are used, want %d:\n%s", name, want, rows, text)
		}

		seenUp, seenDown = seenUp || up >= 0, seenDown || down >= 0
		seenBoth = seenBoth || (up >= 0 && down >= 0)

		if (to == 0 && up >= 0) || (to == n-1 && down >= 0) {
			t.Fatalf("%s: a line at the end where nothing is hidden", name)
		}
	}

	if !seenUp || !seenDown || !seenBoth {
		t.Errorf("up %v, down %v, both %v: not every state was met", seenUp, seenDown, seenBoth)
	}
}

// TestMoreLinesExactCounts checks the numbers at the four places that matter on a list of 120
// rows in the palette: the start, one scroll down, the middle and the end.
//
// Parameters:
//   - t: test to report to
func TestMoreLinesExactCounts(t *testing.T) {
	s := newSandbox(t)
	aliasFile(t, s, "al", 120)

	g := newRig(t, s.with(func(o *Options) { o.Width, o.Height = 100, 24 }))
	p := openPalette(g)
	g.typeText("a al")

	rows := rowsFor(22)
	tests := []struct {
		name         string
		sel          int
		up, down     int
		first, final string
	}{
		{"start", 0, -1, 109, "al000", "\u2193 109 more"},
		{"one scroll", rows - 1, 2, 108, "\u2191 2 more", "\u2193 108 more"},
		{"middle", 60, 51, 59, "\u2191 51 more", "\u2193 59 more"},
		{"end", 119, 109, -1, "\u2191 109 more", "al119"},
	}

	for _, tt := range tests {
		p.step(g.r.e, tt.sel)

		up, down := moreOf(g.text())
		if up != tt.up || down != tt.down {
			t.Errorf("%s: up %d down %d, want %d and %d:\n%s", tt.name, up, down, tt.up, tt.down,
				g.text())
		}

		box := paletteBox(g)
		if !strings.Contains(box[0], tt.first) || !strings.Contains(box[len(box)-1], tt.final) {
			t.Errorf("%s: first %q, last %q; want %q and %q", tt.name, box[0], box[len(box)-1],
				tt.first, tt.final)
		}
	}
}

// paletteBox returns the rows of the results of the open palette, without styles and borders.
//
// Parameters:
//   - g: rig with the palette open
func paletteBox(g *rig) []string {
	var rows []string

	in := false

	for _, l := range strings.Split(g.text(), "\n") {
		switch {
		case strings.Contains(l, "\u251c"):
			in = true
		case strings.Contains(l, "\u2570"):
			return rows
		case in:
			rows = append(rows, strings.Trim(l, " \u2502"))
		}
	}

	return rows
}

// TestMoreLinesAscii checks the ASCII arrows and the French words.
//
// Parameters:
//   - t: test to report to
func TestMoreLinesAscii(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(*Options)
		down  string
	}{
		{"ascii", func(o *Options) { o.ASCII = true }, "v 109 more"},
		{"french", func(o *Options) { o.Printer = newPrinter("fr") }, "\u2193 109 de plus"},
		{
			"french ascii", func(o *Options) { o.Printer, o.ASCII = newPrinter("fr"), true },
			"v 109 de plus",
		},
	} {
		s := newSandbox(t)
		aliasFile(t, s, "al", 120)

		g := newRig(t, s.with(func(o *Options) {
			o.Width, o.Height = 100, 24
			tt.setup(o)
		}))
		openPalette(g)
		g.typeText("a al")

		if !strings.Contains(g.text(), tt.down) {
			t.Errorf("%s: no %q:\n%s", tt.name, tt.down, g.text())
		}
	}
}

// TestMoreLinesClick checks the mouse on the "N more" lines: a click scrolls by a page toward the
// hidden rows, keeps the selection inside the window and does not run anything, and a double
// click on a line is two scrolls, not a run.
//
// Parameters:
//   - t: test to report to
func TestMoreLinesClick(t *testing.T) {
	t.Run("palette", func(t *testing.T) {
		s := newSandbox(t)
		aliasFile(t, s, "al", 120)

		g := scrollRig(t, s, 100, 24, StartHome)
		p := openPalette(g)
		g.typeText("a al")

		rows := rowsFor(22)
		page := pageOf(0, rows, 120)

		clickOn(g, "\u2193 109 more")

		if p.off != page || p.sel < p.off || len(g.r.overlays) != 1 {
			t.Errorf("after a click below: off %d sel %d, want off %d", p.off, p.sel, page)
		}

		clickOn(g, "\u2191 "+strconv.Itoa(page)+" more")

		if want := page - pageOf(page, rows, 120); p.off != want {
			t.Errorf("after a click above: off %d, want %d", p.off, want)
		}

		clickOn(g, "\u2193 109 more")
		clickOn(g, "\u2193 ")

		if seen[activateMsg](g) || len(g.r.overlays) != 1 {
			t.Error("a click on a line of hidden rows ran an entry")
		}
	})

	t.Run("list", func(t *testing.T) {
		s := newSandbox(t)
		aliasFile(t, s, "lst", 40)

		g := scrollRig(t, s, 100, 20, StartAliases)
		l := g.r.scr.(*listScreen)
		rows := l.rows(g.r.e)
		page := pageOf(0, rows, 40)

		clickOn(g, "\u2193 ")

		if l.off != page || l.sel < l.off {
			t.Errorf("after a click below: off %d sel %d, want off %d", l.off, l.sel, page)
		}

		clickOn(g, "\u2191 ")

		if want := page - pageOf(page, rows, 40); l.off != want {
			t.Errorf("after a click above: off %d, want %d", l.off, want)
		}
	})

	t.Run("settings", func(t *testing.T) {
		g, _ := mouseRig(t, 60, 12, StartConfig, nil)
		c := g.r.scr.(*configScreen)

		clickOn(g, "\u2193 ")

		if c.off == 0 {
			t.Error("a click below did not scroll")
		}

		if len(g.r.overlays) != 0 {
			t.Error("a click on a line of hidden rows changed a setting")
		}
	})

	t.Run("help", func(t *testing.T) {
		g, _ := mouseRig(t, 100, 14, StartConnections, nil)
		g.press("?")

		h := g.top().(*helpOverlay)
		_ = g.text()

		clickOn(g, "\u2193 ")

		page := pageOf(0, h.rows, h.total)
		if h.off != page {
			t.Errorf("after a click below: off %d, want %d", h.off, page)
		}

		clickOn(g, "\u2191 ")

		if want := page - pageOf(page, h.rows, h.total); h.off != want {
			t.Errorf("after a click above: off %d, want %d", h.off, want)
		}
	})

	t.Run("details", func(t *testing.T) {
		g, _ := mouseRig(t, 60, 12, StartHome, nil)

		var fields []infoField
		for i := range 20 {
			fields = append(fields, infoField{label: fmt.Sprintf("field%02d", i), value: "value"})
		}

		g.r.push(&infoOverlay{title: "record", fields: fields})

		i := g.top().(*infoOverlay)
		_ = g.text()

		clickOn(g, "\u2193 ")

		if want := pageOf(0, i.rows, i.total); i.off != want {
			t.Errorf("after a click below: off %d, want %d", i.off, want)
		}

		clickOn(g, "\u2191 ")

		if i.off >= pageOf(0, i.rows, i.total) {
			t.Errorf("after a click above: off %d", i.off)
		}
	})

	t.Run("choices", func(t *testing.T) {
		g, _ := mouseRig(t, 80, 14, StartHome, nil)
		items := make([]pickItem, 30)

		for i := range items {
			items[i] = pickItem{label: fmt.Sprintf("choice%02d", i), value: fmt.Sprint(i)}
		}

		done := false

		g.r.push(newPick("Pick", items, 0, func(*env, string) tea.Cmd {
			done = true

			return nil
		}))

		p := g.top().(*pickOverlay)

		clickOn(g, "\u2193 ")

		if p.off == 0 || done {
			t.Errorf("off %d, chosen %v", p.off, done)
		}
	})
}
