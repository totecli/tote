package ui

import (
	"fmt"
	"strings"
	"testing"
)

// frameBounds returns the number of blank cells before and the right edge of the text of a plain
// line, in cells. A blank line gives -1 and -1.
//
// Parameters:
//   - e: shared environment, for the width rule
//   - line: plain line
func frameBounds(e *env, line string) (int, int) {
	if strings.TrimSpace(line) == "" {
		return -1, -1
	}

	return e.Metric.width(line) - e.Metric.width(strings.TrimLeft(line, " ")),
		e.Metric.width(strings.TrimRight(line, " "))
}

// TestPageAlignment checks the single rule that places the page: it is as wide as the terminal up
// to the cap of ui.width (none, 120 or 160), centered beyond that, and every line of every screen
// stays between the margin and the right edge of the page. The welcome box and the title of a list
// start exactly at the margin and the widest line reaches the right edge, so that the page follows
// the window.
//
// Parameters:
//   - t: test to report to
func TestPageAlignment(t *testing.T) {
	starts := []Start{
		StartHome, StartConnections, StartRepos, StartAliases, StartFuncs,
		StartConfig,
	}
	modes := [][]string{{"TOTE_COLOR=truecolor"}, {"TOTE_COLOR=none", "LANG=C"}}

	for _, setting := range []string{"auto", "120", "160"} {
		for _, cols := range []int{100, 110, 120, 121, 160, 161, 200} {
			for _, start := range starts {
				for _, env := range modes {
					name := fmt.Sprintf("%s, %d columns, start %d, %v", setting, cols, start, env)
					g := newRig(t, newSandbox(t).with(func(o *Options) {
						o.Start, o.Width, o.Height = start, cols, 40
						o.Settings.UI.Width = setting
						o.Environ = append(o.Environ, env...)
					}))

					checkAlignment(t, g, name, cols, setting)
				}
			}
		}
	}
}

// checkAlignment applies the rule of TestPageAlignment to one screen.
//
// Parameters:
//   - t: running test
//   - g: rig on the screen, 40 lines high
//   - name: description of the case, for the failures
//   - cols: width of the terminal
//   - setting: value of ui.width
func checkAlignment(t *testing.T, g *rig, name string, cols int, setting string) {
	t.Helper()

	width, margin := pageGeometry(cols, setting)
	e := g.r.e

	if e.Width != width || e.Margin != margin || e.Cols != cols {
		t.Fatalf("%s: env has width %d margin %d cols %d, want %d %d %d", name, e.Width,
			e.Margin, e.Cols, width, margin, cols)
	}

	lines := strings.Split(g.text(), "\n")
	if len(lines) != 40 {
		t.Fatalf("%s: %d lines, want 40", name, len(lines))
	}

	least, most := cols, 0

	for i, l := range lines {
		if e.Metric.width(l) > cols {
			t.Errorf("%s: line %d is %d cells wide: %q", name, i, e.Metric.width(l), l)
		}

		lead, right := frameBounds(e, l)
		if right < 0 {
			continue
		}

		if lead < margin || right > margin+width {
			t.Errorf("%s: line %d spans %d to %d, outside the page %d to %d: %q", name, i, lead,
				right, margin, margin+width, l)
		}

		least, most = min(least, lead), max(most, right)
	}

	if least != margin {
		t.Errorf("%s: the page starts at %d, want the margin %d", name, least, margin)
	}

	if most < margin+width-spareColumns {
		t.Errorf("%s: the page ends at %d, want %d: it does not follow the window", name, most,
			margin+width)
	}
}

// TestPageGrowsWithTheWindow checks the welcome box and the detail panel at several widths and for
// each value of ui.width: both are as wide as the page, with no cap below it.
//
// Parameters:
//   - t: test to report to
func TestPageGrowsWithTheWindow(t *testing.T) {
	for _, setting := range []string{"auto", "120", "160"} {
		for _, cols := range []int{72, 73, 90, 110, 120, 160, 200} {
			want, _ := pageGeometry(cols, setting)

			g := newRig(t, newSandbox(t).with(func(o *Options) {
				o.Width, o.Height = cols, 40
				o.Settings.UI.Width = setting
			}))

			if got := g.r.e.Metric.width(welcomeBox(g.r.e)[0]); got != want {
				t.Errorf("%s, %d columns: the welcome box is %d cells wide, want %d", setting,
					cols, got, want)
			}

			g.press("enter")

			panel := detailPanel(g.r.e, *g.r.scr.(*listScreen).selected(), g.r.e.Width, "")
			if got := g.r.e.Metric.width(panel[0]); got != want {
				t.Errorf("%s, %d columns: the detail panel is %d cells wide, want %d", setting,
					cols, got, want)
			}
		}
	}
}

// TestColumnsGrow checks that spare room is shared between the columns of a table, that no
// column ends up narrower than its natural width, that no column but the last is wider than twice
// it, that the room that is still left goes to the last column, and that a tight table is left as
// it was.
//
// Parameters:
//   - t: test to report to
func TestColumnsGrow(t *testing.T) {
	natural := []int{10, 19, 8, 14}

	tight := columnWidths(natural, 57)
	if fmt.Sprint(tight) != "[10 19 8 14]" {
		t.Errorf("a table that just fits changed: %v", tight)
	}

	for _, avail := range []int{61, 80, 104, 160, 195, 400} {
		got := columnWidths(natural, avail)
		sum := columnGap * (len(got) - 1)

		for i, w := range got {
			sum += w

			if w < natural[i] {
				t.Errorf("avail %d: column %d is %d wide, natural %d", avail, i, w, natural[i])
			}

			if i < len(got)-1 && w > 2*natural[i] {
				t.Errorf("avail %d: column %d is %d wide, more than twice its natural %d", avail,
					i, w, natural[i])
			}
		}

		if sum != avail {
			t.Errorf("avail %d: the table is %d wide, want the whole room", avail, sum)
		}
	}

	if got := columnWidths(natural, 400); fmt.Sprint(got[:3]) != "[20 38 16]" || got[3] <= 28 {
		t.Errorf("a wide table stretched the short columns or left the last one alone: %v", got)
	}

	if got := columnWidths(natural, 30); len(got) >= len(natural) {
		t.Errorf("a narrow table kept every column: %v", got)
	}
}

// assertFloating checks that the overlay on top of the stack floats alone: nothing of the page
// below is drawn, the box is centered on the terminal with the same blank space at both sides,
// and only the box, its footer and the line under the footer have text.
//
// Parameters:
//   - t: test to report to
//   - g: rig with an overlay open
//   - page: plain text of the screen as it was drawn before the overlay opened
func assertFloating(t *testing.T, g *rig, page string) {
	t.Helper()

	e := g.r.e
	ov := g.r.overlays[len(g.r.overlays)-1]
	block := ov.View(e, max(1, e.Cols-4), max(1, e.Height-2))
	bw := e.Metric.width(block[0])
	x, y := (e.Cols-bw)/2, (e.Height-len(block)-1)/2
	label := fmt.Sprintf("%dx%d %T", e.Cols, e.Height, ov)

	text := g.text()
	frame := strings.Split(text, "\n")

	if len(frame) != e.Height {
		t.Fatalf("%s: %d lines, want %d", label, len(frame), e.Height)
	}

	for i, l := range frame {
		if e.Metric.width(l) > e.Cols {
			t.Errorf("%s: line %d is %d cells wide", label, i, e.Metric.width(l))
		}

		lead, right := frameBounds(e, l)
		if right < 0 {
			continue
		}

		inBox := i >= y && i < y+len(block)

		if !inBox && i != y+len(block) && i != y+len(block)+1 {
			t.Errorf("%s: line %d holds text outside the box and its footer: %q", label, i, l)
		}

		if lead < x || right > x+bw {
			t.Errorf("%s: line %d spans %d to %d, outside the box %d to %d: %q", label, i, lead,
				right, x, x+bw, l)
		}

		if inBox && (lead != x || right != x+bw) {
			t.Errorf("%s: box line %d spans %d to %d, want %d to %d", label, i, lead, right, x,
				x+bw)
		}
	}

	if gap := e.Cols - (x + bw) - x; gap < 0 || gap > 1 {
		t.Errorf("%s: the box has %d blank cells at the left and %d at the right", label, x,
			e.Cols-x-bw)
	}

	for _, l := range strings.Split(page, "\n") {
		if kept := strings.TrimSpace(l); len(kept) >= 16 && strings.Contains(text, kept) {
			t.Errorf("%s: a line of the screen below is still drawn: %q", label, kept)
		}
	}
}

// TestOverlaysFloat checks every overlay: it is drawn alone, centered, with the rule of
// assertFloating, on terminals narrower and wider than the page.
//
// Parameters:
//   - t: test to report to
func TestOverlaysFloat(t *testing.T) {
	cases := []struct {
		name  string
		start Start
		keys  []string
	}{
		{"palette", StartHome, []string{"ctrl+k"}},
		{"palette on a list", StartRepos, []string{"ctrl+k", "p"}},
		{"form", StartConnections, []string{"n"}},
		{"edit form", StartAliases, []string{"e"}},
		{"confirm", StartConnections, []string{"d"}},
		{"help", StartConnections, []string{"?"}},
		{"details", StartConnections, []string{"v"}},
		{"pick user", StartConnections, []string{"U"}},
	}

	for _, c := range cases {
		for _, size := range [][2]int{{80, 24}, {110, 30}, {120, 40}, {160, 40}, {201, 30}} {
			g := newRig(t, newSandbox(t).with(func(o *Options) {
				o.Start = c.start
				o.Width, o.Height = size[0], size[1]
			}))

			page := stripStyles(g.r.render())
			g.press(c.keys...)

			if len(g.r.overlays) == 0 {
				t.Fatalf("%s: no overlay open", c.name)
			}

			assertFloating(t, g, page)
		}
	}
}

// TestOverlayOverCompactAndTiny checks the overlays on the compact layout and that the tiny
// layout never draws one.
//
// Parameters:
//   - t: test to report to
func TestOverlayOverCompactAndTiny(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start, o.Width, o.Height = StartConnections, 60, 14
	}))

	page := stripStyles(g.r.render())
	g.press("ctrl+k", "z", "z", "z")
	assertFloating(t, g, page)

	g.resize(minWidth-1, minHeight+1)

	if text := g.text(); strings.Contains(text, "Palette") {
		t.Errorf("the tiny layout draws the palette:\n%s", text)
	}
}
