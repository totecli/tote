package ui

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// sgrOnly matches the one escape sequence that may reach the screen: a color sequence.
var sgrOnly = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// hostileLines are lines of output that a program could write to attack the terminal or the
// layout.
var hostileLines = []string{
	"\x1b]0;owned\x07 title",
	"\x1b]52;c;ZWNobyBoaQ==\x1b\\ clipboard",
	"\x1b[2J\x1b[H cleared",
	"\x1b[10;10H moved \x1b[1A up \x1b[K erase",
	"\x1b[?1049h alternate screen",
	"left \u202e override \u2066 isolate \u200f mark",
	"nul\x00 bell\x07 backspace\x08 formfeed\x0c del\x7f",
	"c1 \u009b31m \u009d0;x\u009c",
	"\x1bP1;1|data\x1b\\ dcs",
	"\x1b[38;2;999;0;0m bad color \x1b[999m",
	"\x1b[31mred \x1b[1;4;32mgreen\x1b[0m done",
	"tabs\tand\ttabs\there",
	"emoji \U0001F600 wide \u4e2d\u6587 combining e\u0301",
	"\x1b",
	"\x1b[",
	"\x1b[31",
}

// clean fails the test when a row holds anything but printable text and color sequences.
//
// Parameters:
//   - t: running test
//   - row: one row of the pager
func clean(t *testing.T, row string) {
	t.Helper()

	left := sgrOnly.ReplaceAllString(row, "")

	for _, r := range left {
		if unicode.IsControl(r) || r == 0x202e || r == 0x2066 || r == 0x200f || r == '\x1b' {
			t.Fatalf("row %q holds %U", row, r)
		}
	}
}

// TestPagerHostileOutput checks that nothing but text and colors comes out of the pager, whatever
// the program wrote, at every width, in color and in the mono theme.
func TestPagerHostileOutput(t *testing.T) {
	e := newTestEnv(t)

	for _, sgr := range []bool{true, false} {
		pt := newPagerText(sgr)

		for _, l := range hostileLines {
			pt.add(l + "\n")
		}

		pt.add("\x1b[31mred without end")

		for _, w := range []int{4, 5, 16, 40, 80} {
			pt.fit(e, w)

			for i := 0; i < pt.total(); i++ {
				row := pt.row(i)
				clean(t, row)

				if got := e.Metric.width(sgrOnly.ReplaceAllString(row, "")); got > w {
					t.Errorf("sgr %v width %d: row %q is %d cells", sgr, w, row, got)
				}

				if !sgr && strings.Contains(row, "\x1b") {
					t.Errorf("the mono theme kept a color in %q", row)
				}
			}
		}
	}
}

// TestPagerColorsDoNotLeak checks that a style that is open at the end of a row is closed there
// and opened again on the next row, so that no row bleeds into the frame around it.
func TestPagerColorsDoNotLeak(t *testing.T) {
	e := newTestEnv(t)
	pt := newPagerText(true)
	pt.add("\x1b[1;31m" + strings.Repeat("abcdef", 10) + "\x1b[0m tail\n")
	pt.fit(e, 20)

	if pt.total() != 4 {
		t.Fatalf("%d rows", pt.total())
	}

	for i := 0; i < 3; i++ {
		row := pt.row(i)

		if !strings.HasSuffix(row, sgrReset) {
			t.Errorf("row %d %q does not close its style", i, row)
		}

		if i > 0 && !strings.HasPrefix(row, "\x1b[1;31m") {
			t.Errorf("row %d %q does not open it again", i, row)
		}
	}

	if last := pt.row(3); strings.Contains(last, "\x1b") {
		t.Errorf("the row after the reset holds a style: %q", last)
	}
}

// TestPagerWrap checks that a line is cut at the width without losing a character, and that a
// wide character never straddles the edge.
func TestPagerWrap(t *testing.T) {
	e := newTestEnv(t)
	pt := newPagerText(true)
	pt.add("0123456789abcdefghij\n\u4e2d\u6587\u4e2d\u6587\u4e2d\n\n")
	pt.fit(e, 7)

	var plain []string

	for i := 0; i < pt.total(); i++ {
		plain = append(plain, sgrOnly.ReplaceAllString(pt.row(i), ""))
	}

	if got := strings.Join(plain[:3], ""); got != "0123456789abcdefghij" {
		t.Errorf("rows %q lose characters", plain)
	}

	for _, r := range plain {
		if e.Metric.width(r) > 7 {
			t.Errorf("row %q is wider than 7", r)
		}
	}

	if len(plain) != 3+2+1 {
		t.Errorf("%d rows: %q", len(plain), plain)
	}
}

// TestPagerBounds checks that the pager keeps a bounded amount of text and says so.
func TestPagerBounds(t *testing.T) {
	pt := newPagerText(true)
	pt.add(strings.Repeat("x", pagerMaxLine*3) + "\n")

	if len(pt.lines) != 1 || len(pt.lines[0]) != pagerMaxLine {
		t.Fatalf("a long line kept %d bytes", len(pt.lines[0]))
	}

	pt = newPagerText(true)
	chunk := strings.Repeat("l\n", 1000)

	for i := 0; i < pagerMaxLines/1000+5; i++ {
		pt.add(chunk)
	}

	if len(pt.lines) != pagerMaxLines || !pt.cut {
		t.Fatalf("kept %d lines, cut %v", len(pt.lines), pt.cut)
	}

	e := newTestEnv(t)
	v := &pagerView{text: pt, follow: true}
	lines, _ := v.draw(e, 60, 10)

	if !strings.Contains(strings.Join(lines, "\n"), "too long to keep") {
		t.Errorf("no truncation notice:\n%s", strings.Join(lines, "\n"))
	}
}

// TestPagerScroll checks that the view follows the end of a growing text until the user scrolls
// up, and takes it again at the end.
func TestPagerScroll(t *testing.T) {
	e := newTestEnv(t)
	pt := newPagerText(true)

	for i := 0; i < 50; i++ {
		pt.add("line\n")
	}

	v := &pagerView{text: pt, follow: true}
	v.draw(e, 40, 10)

	if v.off != v.last() || v.last() == 0 {
		t.Fatalf("off %d last %d", v.off, v.last())
	}

	v.move(-3)

	if v.follow || v.off != v.last()-3 {
		t.Errorf("after up: follow %v off %d", v.follow, v.off)
	}

	pt.add("more\n")
	v.draw(e, 40, 10)

	if v.off == v.last() {
		t.Error("the view moved while the user read above")
	}

	v.move(1000)

	if !v.follow || v.off != v.last() {
		t.Errorf("at the end: follow %v off %d", v.follow, v.off)
	}
}

// TestPagerNoLineNumbers checks the rows of the pager hold the text and nothing in front of it.
func TestPagerNoLineNumbers(t *testing.T) {
	e := newTestEnv(t)
	pt := newPagerText(true)
	pt.add("alpha\nbeta\n")
	v := &pagerView{text: pt}
	lines, _ := v.draw(e, 40, 10)

	if !strings.HasPrefix(stripStyles(lines[0]), " alpha") {
		t.Errorf("first row %q", lines[0])
	}
}

// newTestEnv returns the environment of a rig, for the functions that only draw.
//
// Parameters:
//   - t: running test
func newTestEnv(t *testing.T) *env {
	t.Helper()

	return newRig(t, newSandbox(t)).r.e
}

// TestRunViewScrolls checks the keys and the wheel of the output of a run, that Esc closes a view
// whose run ended, and that a program that is still going is asked to stop first.
func TestRunViewScrolls(t *testing.T) {
	g := newRig(t, newSandbox(t))
	g.resize(80, 24)

	v := cannedRun(g.r.e, "done")
	g.r.push(v)
	g.r.render()

	if !v.follow || v.off != v.last() {
		t.Fatalf("a finished run does not show its end: off %d last %d", v.off, v.last())
	}

	g.press("home")

	if v.off != 0 || v.follow {
		t.Errorf("Home: off %d follow %v", v.off, v.follow)
	}

	g.press("pgdown")
	g.r.render()

	if v.off == 0 {
		t.Error("PgDown did not scroll")
	}

	was := v.off
	g.send(tea.MouseWheelMsg{X: 10, Y: 8, Button: tea.MouseWheelDown})
	g.r.render()

	if v.off <= was {
		t.Errorf("the wheel did not scroll: %d then %d", was, v.off)
	}

	g.press("esc")

	if g.top() != nil {
		t.Errorf("Esc left %T", g.top())
	}

	run := cannedRun(g.r.e, "running")
	g.r.push(run)
	g.press("esc")

	if g.top() != run || !run.stopping {
		t.Errorf("Esc closed a view whose program runs: %T stopping %v", g.top(), run.stopping)
	}
}
