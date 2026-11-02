package ui

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// reversed matches a span drawn in reverse video, which is how the highlight is drawn.
var reversed = regexp.MustCompile("\x1b\\[(?:4;)?7(?:;4)?m(.*?)\x1b\\[m")

// marked shows the highlight of a styled line in plain text: the spans in reverse video are put
// between double brackets and every other sequence is removed.
//
// Parameters:
//   - line: styled line
func marked(line string) string {
	// Underlined text is styled one character at a time: neighbouring spans are one.
	return strings.ReplaceAll(stripStyles(reversed.ReplaceAllString(line, "[[$1]]")), "]][[", "")
}

// markedScreen shows the highlight of every line of the screen, see marked.
//
// Parameters:
//   - g: rig to look at
func markedScreen(g *rig) []string {
	lines := g.view()
	out := make([]string, len(lines))

	for i, l := range lines {
		out[i] = marked(l)
	}

	return out
}

// bareEnv returns an environment for the pure functions of the selection.
//
// Parameters:
//   - t: running test
//   - ascii: true for the ASCII glyph set
func bareEnv(t *testing.T, ascii bool) *env {
	t.Helper()

	g := newRig(t, newSandbox(t).with(func(o *Options) { o.ASCII = ascii }))
	g.resize(80, 24)

	return g.r.e
}

// TestSelectionText checks the text a selection takes: its geometry in reading order, the
// rounding to whole graphemes, the borders left out and the blanks cut.
//
// Parameters:
//   - t: test to report to
func TestSelectionText(t *testing.T) {
	tests := []struct {
		name  string
		ascii bool
		lines []string
		a, b  cellPos
		want  string
	}{
		{"one line", false, []string{"hello world"}, cellPos{6, 0}, cellPos{10, 0}, "world"},
		{
			"to the end of the line", false,
			[]string{"hello world   "},
			cellPos{6, 0},
			cellPos{13, 0},
			"world",
		},
		{"single cell", false, []string{"hello world"}, cellPos{4, 0}, cellPos{4, 0}, "o"},
		{
			"two lines", false,
			[]string{"hello world", "second line"},
			cellPos{6, 0},
			cellPos{5, 1},
			"world\nsecond",
		},
		{
			"reversed drag", false,
			[]string{"hello world", "second line"},
			cellPos{5, 1},
			cellPos{6, 0},
			"world\nsecond",
		},
		{
			"reversed on one line", false,
			[]string{"hello world"},
			cellPos{10, 0},
			cellPos{6, 0},
			"world",
		},
		{
			"lines between are whole", false,
			[]string{"one", "two words", "three"},
			cellPos{1, 0},
			cellPos{1, 2},
			"ne\ntwo words\nth",
		},
		{
			"blank lines at both ends", false,
			[]string{"", "text", ""},
			cellPos{0, 0},
			cellPos{9, 2},
			"text",
		},
		{
			"blank line between", false,
			[]string{"top", "", "bottom"},
			cellPos{0, 0},
			cellPos{9, 2},
			"top\n\nbottom",
		},
		{"nothing there", false, []string{"abc", "", ""}, cellPos{20, 1}, cellPos{30, 2}, ""},
		{
			"wide start on its second cell", false,
			[]string{"\u65E5\u672C\u8A9E text"},
			cellPos{1, 0},
			cellPos{2, 0},
			"\u65E5\u672C",
		},
		{
			"wide end on its first cell", false,
			[]string{"\u65E5\u672C\u8A9E text"},
			cellPos{0, 0},
			cellPos{4, 0},
			"\u65E5\u672C\u8A9E",
		},
		{
			"cjk after ascii", false,
			[]string{"ab\u65E5cd"},
			cellPos{3, 0},
			cellPos{3, 0},
			"\u65E5",
		},
		{
			"emoji is whole", false,
			[]string{"a\U0001F680b"},
			cellPos{2, 0},
			cellPos{2, 0},
			"\U0001F680",
		},
		{
			"emoji next to text", false,
			[]string{"x \U0001F680 y"},
			cellPos{3, 0},
			cellPos{5, 0},
			"\U0001F680 y",
		},
		{
			"heart with its selector", false,
			[]string{"a\u2764b"},
			cellPos{1, 0},
			cellPos{1, 0},
			"\u2764",
		},
		{
			"combining mark stays", false,
			[]string{"cafe\u0301 ok"},
			cellPos{3, 0},
			cellPos{3, 0},
			"e\u0301",
		},
		{"unicode box", false, []string{
			"   \u256D\u2500 Palette \u2500\u2500\u2500\u2500\u2500\u256E   ",
			"   \u2502 Connections > prod \u2502   ",
			"   \u251C" + strings.Repeat("\u2500", 19) + "\u2524   ",
			"   \u2502 New connection     \u2502   ",
			"   \u2570" + strings.Repeat("\u2500", 19) + "\u256F   ",
		}, cellPos{0, 0}, cellPos{79, 4}, "Palette\nConnections > prod\n\nNew connection"},
		{"unicode box, cut inside", false, []string{
			"   \u2502 Connections > prod \u2502   ",
			"   \u2502 New connection     \u2502   ",
		}, cellPos{17, 0}, cellPos{11, 1}, "> prod\nNew con"},
		{"ascii box", true, []string{
			"+- Palette --------+",
			"| Connections      |",
			"+------------------+",
			"| New connection   |",
			"+------------------+",
		}, cellPos{0, 0}, cellPos{79, 4}, "Palette\nConnections\n\nNew connection"},
		{
			"dash and plus in text are kept", true,
			[]string{"a - b + c"},
			cellPos{0, 0},
			cellPos{20, 0},
			"a - b + c",
		},
		{"indentation is relative", false, []string{
			"   \u276F prod", "     stage",
			"   other",
		}, cellPos{0, 0}, cellPos{50, 2}, "\u276F prod\n  stage\nother"},
		{
			"first line cut in the middle", false,
			[]string{"abc def", "   ghi"},
			cellPos{4, 0},
			cellPos{50, 1},
			"def\nghi",
		},
		{
			"trailing blanks inside the selection", false,
			[]string{"abc   def"},
			cellPos{0, 0},
			cellPos{4, 0},
			"abc",
		},
		{"more line in a box", false, []string{
			"  \u2502 \u2191 18 more                \u2502",
			"  \u2502 esc  close                \u2502",
			"  \u2502 \u2193 6 more                 \u2502",
		}, cellPos{0, 0}, cellPos{79, 2}, "esc  close"},
		{
			"more line of a list", false,
			[]string{"  prod   prod.example.com", "  \u2193 6 more"},
			cellPos{0, 0},
			cellPos{79, 1},
			"prod   prod.example.com",
		},
		{"ascii more line", true, []string{
			"| esc  close   |", "| v 6 more     |",
			"| ^ 12 more    |",
		}, cellPos{0, 0}, cellPos{79, 2}, "esc  close"},
		{
			"an arrow in text is kept", false,
			[]string{"  \u2191 up", "  \u2193 down"},
			cellPos{0, 0},
			cellPos{79, 1},
			"\u2191 up\n\u2193 down",
		},
		{
			"ascii v ends a word", true,
			[]string{"| improv |"},
			cellPos{0, 0},
			cellPos{79, 0},
			"improv",
		},
	}

	for _, tt := range tests {
		e := bareEnv(t, tt.ascii)
		a, b := orderCells(tt.a, tt.b)
		got := selectionText(e.selRows(tt.lines, a, b))

		if got != tt.want {
			t.Errorf("%s: text = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestSelectionRowsMatchTheText checks that the highlighted cells are the cells of the text that
// is copied: the span of every row is as wide as its text.
//
// Parameters:
//   - t: test to report to
func TestSelectionRowsMatchTheText(t *testing.T) {
	e := bareEnv(t, false)
	lines := []string{
		"  \u2502 \u65E5\u672C\u8A9E \U0001F680 text \u2502",
		"  \u2502 caf\u00E9 and e\u0301 here \u2502",
	}

	for x := 0; x < 24; x++ {
		for y := 0; y < 2; y++ {
			for _, row := range e.selRows(lines, cellPos{x, 0}, cellPos{x, y}) {
				if row.empty {
					continue
				}

				if got := e.Metric.width(row.text); got != row.x1-row.x0 {
					t.Errorf("(%d,%d) row %d: text %q is %d cells, span is %d", x, y, row.y,
						row.text, got, row.x1-row.x0)
				}
			}
		}
	}
}

// selectionRig builds a rig whose clipboard programs are a fake that records the text.
//
// Parameters:
//   - t: running test
//   - start: first screen
//   - copied: receives the texts that the fake takes
//   - edit: changes the options, may be nil
func selectionRig(t *testing.T, start Start, copied *[]string, edit func(*Options)) (*rig, *clock) {
	t.Helper()

	clk := &clock{t: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)}
	s := frameSandbox(t, dataFull).with(func(o *Options) {
		o.Width, o.Height, o.Start, o.Now = 100, 30, start, clk.now
		o.Clipboard = func(_ context.Context, _ []string, text string) string {
			*copied = append(*copied, text)

			return "fake"
		}

		if edit != nil {
			edit(o)
		}
	})

	g := newRig(t, s)
	g.resize(100, 30)

	return g, clk
}

// drag presses the left button on a cell, moves to another and releases there.
//
// Parameters:
//   - g: rig to play in
//   - from: cell of the press
//   - to: cell of the release
func drag(g *rig, from, to cellPos) {
	g.t.Helper()

	g.send(tea.MouseClickMsg{X: from.x, Y: from.y, Button: tea.MouseLeft})
	g.send(tea.MouseMotionMsg{X: to.x, Y: to.y, Button: tea.MouseLeft})
	g.send(tea.MouseReleaseMsg{X: to.x, Y: to.y, Button: tea.MouseLeft})
}

// TestSelectionDrag plays a drag over a list: the text is highlighted, copied once through the
// clipboard programs, and the toast says how many characters and with what.
//
// Parameters:
//   - t: test to report to
func TestSelectionDrag(t *testing.T) {
	var copied []string

	g, _ := selectionRig(t, StartConnections, &copied, nil)

	y, x := lineOf(g, "staging")
	drag(g, cellPos{x, y}, cellPos{x + 6, y})

	if len(copied) != 1 || copied[0] != "staging" {
		t.Fatalf("copied = %q, want [staging]", copied)
	}

	if got := g.toastText(); !strings.Contains(got, "Copied 7 characters (fake)") {
		t.Errorf("toast = %q", got)
	}

	if got := markedScreen(g)[y]; !strings.Contains(got, "[[staging]]") {
		t.Errorf("line %d = %q, want the word highlighted", y, got)
	}

	if n := strings.Count(strings.Join(markedScreen(g), "\n"), "[["); n != 1 {
		t.Errorf("%d highlighted spans, want one", n)
	}
}

// TestSelectionKeepsClicks checks that a click that does not move keeps its meaning, that a
// double click still activates, and that nothing is copied or highlighted.
//
// Parameters:
//   - t: test to report to
func TestSelectionKeepsClicks(t *testing.T) {
	var copied []string

	g, clk := selectionRig(t, StartAliases, &copied, nil)

	y, x := lineOf(g, "gs")
	click := func() {
		g.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		g.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	}

	click()

	if l := g.r.scr.(*listScreen); l.selected().Key != "gs" {
		t.Errorf("selected = %s, want gs", l.selected().Key)
	}

	clk.advance(50 * time.Millisecond)
	click()

	if !g.Quit || g.r.result.Payload != "git status" {
		t.Errorf("quit %v result %+v, want the alias inserted", g.Quit, g.r.result)
	}

	if len(copied) != 0 || strings.Contains(strings.Join(markedScreen(g), ""), "[[") {
		t.Errorf("a click selected text: copied %q", copied)
	}

	// A motion that stays in the cell of the press is not a drag.
	g2, _ := selectionRig(t, StartAliases, &copied, nil)
	y, x = lineOf(g2, "ll")

	g2.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	g2.send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
	g2.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})

	if len(copied) != 0 || g2.r.sel.drag {
		t.Errorf("a motion in the same cell started a selection")
	}
}

// TestSelectionClears checks what removes the highlight: a key, Escape without any other effect,
// a resize, the wheel, a new press and a frame whose content changed.
//
// Parameters:
//   - t: test to report to
func TestSelectionClears(t *testing.T) {
	tests := []struct {
		name  string
		act   func(g *rig)
		after func(t *testing.T, g *rig)
	}{
		{"a key", func(g *rig) { g.press("down") }, func(t *testing.T, g *rig) {
			if l := g.r.scr.(*listScreen); l.selected().Key != "staging" {
				t.Errorf("selected = %s, want the key to act", l.selected().Key)
			}
		}},
		{"escape", func(g *rig) { g.press("esc") }, func(t *testing.T, g *rig) {
			if _, ok := g.r.scr.(*listScreen); !ok || len(g.r.overlays) != 0 {
				t.Errorf("escape did more than clear the highlight")
			}
		}},
		{"resize", func(g *rig) { g.resize(110, 30) }, nil},
		{"wheel", func(g *rig) { g.send(tea.MouseWheelMsg{Button: tea.MouseWheelDown}) }, nil},
		{
			"a press", func(g *rig) {
				g.send(tea.MouseClickMsg{
					X: 1, Y: 1,
					Button: tea.MouseLeft,
				})
			},
			nil,
		},
		{"new content", func(g *rig) { g.r.scr = newHome(0) }, nil},
		{"the setting", func(g *rig) { g.r.e.Settings.UI.Mouse = false }, nil},
	}

	for _, tt := range tests {
		var copied []string

		g, _ := selectionRig(t, StartConnections, &copied, nil)
		y, x := lineOf(g, "prod")
		drag(g, cellPos{x, y}, cellPos{x + 3, y})

		if !strings.Contains(strings.Join(markedScreen(g), ""), "[[") {
			t.Fatalf("%s: no highlight to clear", tt.name)
		}

		tt.act(g)

		if got := strings.Join(markedScreen(g), "\n"); strings.Contains(got, "[[") {
			t.Errorf("%s: the highlight is still there:\n%s", tt.name, got)
		}

		if g.r.sel.drag {
			t.Errorf("%s: the selection is still held", tt.name)
		}

		if tt.after != nil {
			tt.after(t, g)
		}
	}
}

// TestSelectionHint checks that the first selection of a session says how to select with the
// terminal, and the next ones do not.
//
// Parameters:
//   - t: test to report to
func TestSelectionHint(t *testing.T) {
	var copied []string

	g, _ := selectionRig(t, StartConnections, &copied, nil)
	y, x := lineOf(g, "prod")

	g.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	g.send(tea.MouseMotionMsg{X: x + 3, Y: y, Button: tea.MouseLeft})

	if got := g.toastText(); got != "Shift+drag selects with the terminal instead" {
		t.Errorf("toast while dragging = %q", got)
	}

	g.send(tea.MouseReleaseMsg{X: x + 3, Y: y, Button: tea.MouseLeft})

	if got := g.toastText(); !strings.Contains(got, "Copied 4 characters (fake)") ||
		!strings.Contains(got, "Shift+drag selects with the terminal instead") {
		t.Errorf("toast after the first copy = %q", got)
	}

	drag(g, cellPos{x, y}, cellPos{x + 3, y})

	if got := g.toastText(); strings.Contains(got, "Shift+drag") {
		t.Errorf("toast after the second copy = %q, want no hint", got)
	}

	if g2, _ := selectionRig(t, StartConnections, &copied, func(o *Options) {
		o.Printer = newPrinter("fr")
	}); g2 != nil {
		y, x = lineOf(g2, "prod")
		g2.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		g2.send(tea.MouseMotionMsg{X: x + 3, Y: y, Button: tea.MouseLeft})

		want := "Maj+glisser s\u00E9lectionne avec le terminal \u00E0 la place"
		if got := g2.toastText(); got != want {
			t.Errorf("French toast = %q", got)
		}
	}
}

// TestSelectionOffScreen checks drags that leave the window: the cells are brought back inside.
//
// Parameters:
//   - t: test to report to
func TestSelectionOffScreen(t *testing.T) {
	for _, tt := range []struct {
		name string
		to   cellPos
	}{
		{"far right", cellPos{500, 0}},
		{"far below", cellPos{0, 500}},
		{"negative", cellPos{-5, -5}},
		{"corner", cellPos{500, 500}},
	} {
		var copied []string

		g, _ := selectionRig(t, StartConnections, &copied, nil)
		y, x := lineOf(g, "prod")

		drag(g, cellPos{x, y}, tt.to)

		s := g.r.sel
		if s.focus.x < 0 || s.focus.x >= 100 || s.focus.y < 0 || s.focus.y >= 30 {
			t.Errorf("%s: focus = %+v, outside the window", tt.name, s.focus)
		}

		g.view()

		for _, c := range copied {
			if strings.ContainsAny(c, "\x1b\x00") {
				t.Errorf("%s: copied %q", tt.name, c)
			}
		}
	}
}

// TestSelectionOverlay selects across the border of an overlay: the border glyphs are neither
// highlighted nor copied.
//
// Parameters:
//   - t: test to report to
func TestSelectionOverlay(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		var copied []string

		g, _ := selectionRig(t, StartConnections, &copied, func(o *Options) { o.ASCII = ascii })
		g.press("ctrl+k")

		top, _ := lineOf(g, "Palette")
		bottom, _ := lineOf(g, "legacy")

		drag(g, cellPos{0, top}, cellPos{99, bottom})

		if len(copied) != 1 {
			t.Fatalf("ascii %v: copied %q", ascii, copied)
		}

		text := copied[0]
		if !strings.HasPrefix(text, "Palette\n") || !strings.Contains(text, "legacy") {
			t.Errorf("ascii %v: copied %q", ascii, text)
		}

		for _, bad := range []string{"\u2502", "\u256D", "\u256F", "\u2500", "|", "+-"} {
			if strings.Contains(text, bad) {
				t.Errorf("ascii %v: the copied text holds the border %q:\n%s", ascii, bad, text)
			}
		}

		for _, l := range markedScreen(g) {
			if strings.Contains(l, "[[\u2502") || strings.Contains(l, "[[|") {
				t.Errorf("ascii %v: a border is highlighted: %q", ascii, l)
			}
		}
	}
}

// TestSelectionClipboardFallbacks checks where the text goes when no program takes it: the OSC
// 52 sequence of the terminal, then the toast that shows the text.
//
// Parameters:
//   - t: test to report to
func TestSelectionClipboardFallbacks(t *testing.T) {
	none := func(_ context.Context, _ []string, _ string) string { return "" }

	t.Run("terminal", func(t *testing.T) {
		g, _ := mouseRig(t, 100, 30, StartConnections, func(o *Options) { o.Clipboard = none })
		y, x := lineOf(g, "staging")
		drag(g, cellPos{x, y}, cellPos{x + 6, y})

		if got, ok := clipboardOf(g); !ok || got != "staging" {
			t.Errorf("clipboard sequence = %q, %v", got, ok)
		}

		if got := g.toastText(); !strings.Contains(got, "Sent 7 characters to the terminal (OSC 52)") {
			t.Errorf("toast = %q", got)
		}
	})

	t.Run("shown", func(t *testing.T) {
		tests := []struct {
			lang string
			want []string
		}{
			{"en", []string{
				"No copy tool: install xclip, xsel or wl-clipboard, or Shift+drag then " +
					"Ctrl+Shift+C. Text: staging",
			}},
			{"fr", []string{
				"Pas d\u2019outil de copie\u00A0: installez xclip, xsel ou wl-clipboard, ou " +
					"Maj+glisser puis Ctrl+Maj+C. Texte\u00A0: staging",
			}},
		}

		for _, tt := range tests {
			g, _ := mouseRig(t, 120, 30, StartConnections, func(o *Options) {
				o.Clipboard = none
				o.Printer = newPrinter(tt.lang)
				o.Environ = append(append([]string(nil), o.Environ...), "TERM=tmux-256color",
					"TMUX=/tmp/x,1,0")
				o.TmuxClipboard = func() string { return "off" }
			})
			y, x := lineOf(g, "staging")
			drag(g, cellPos{x, y}, cellPos{x + 6, y})

			if _, ok := clipboardOf(g); ok {
				t.Errorf("%s: a sequence was sent to a terminal that cannot take it", tt.lang)
			}

			got := g.toastText()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("%s: toast = %q, want %q", tt.lang, got, want)
				}
			}

			if hint := g.r.e.t("ui_sel_hint"); strings.Contains(got, hint) {
				t.Errorf("%s: the toast repeats the hint %q: %q", tt.lang, hint, got)
			}

			if g.r.hintPending {
				t.Errorf("%s: the hint is still pending after the copy", tt.lang)
			}
		}
	})

	t.Run("one character", func(t *testing.T) {
		g, _ := mouseRig(t, 100, 30, StartConnections, nil)
		y, x := lineOf(g, "staging")
		g.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		g.send(tea.MouseMotionMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
		g.send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
		g.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})

		if got := g.toastText(); !strings.Contains(got, "Sent 1 character to the terminal (OSC 52)") {
			t.Errorf("toast = %q", got)
		}
	})
}

// TestSelectionDisabled checks that nothing happens with ui.mouse off or on a terminal without a
// mouse, and while a program runs.
//
// Parameters:
//   - t: test to report to
func TestSelectionDisabled(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*Options)
	}{
		{"setting", func(o *Options) { o.Settings.UI.Mouse = false }},
		{"dumb terminal", func(o *Options) { o.Environ = []string{"TERM=dumb", "LANG=C.UTF-8"} }},
	} {
		var copied []string

		g, _ := selectionRig(t, StartConnections, &copied, tt.edit)
		y, x := lineOf(g, "staging")
		drag(g, cellPos{x, y}, cellPos{x + 6, y})

		if len(copied) != 0 || g.r.sel.drag || g.r.sel.held {
			t.Errorf("%s: copied %q, selection %+v", tt.name, copied, g.r.sel)
		}

		if g.toastText() != "" {
			t.Errorf("%s: toast = %q", tt.name, g.toastText())
		}
	}
}

// TestSelectionFrames keeps reference frames of a highlighted selection in English, in French and
// in ASCII, with the copied text under each. The highlight is written between double brackets.
//
// Parameters:
//   - t: test to report to
func TestSelectionFrames(t *testing.T) {
	cases := []struct {
		name  string
		start Start
		keys  []string
		lang  string
		ascii bool
		word  string
		to    string
	}{
		{"selectionlist", StartConnections, nil, "en", false, "staging", "ops"},
		{"selectionlistfrench", StartConnections, nil, "fr", false, "staging", "ops"},
		{"selectionlistascii", StartConnections, nil, "en", true, "staging", "ops"},
		{"selectionlistnone", StartConnections, nil, "en", false, "prod", "staging"},
		{
			"selectionpalette", StartConnections,
			[]string{"ctrl+k"},
			"en", false, "Palette",
			"legacy",
		},
		{
			"selectionpalettefrench", StartConnections,
			[]string{"ctrl+k"},
			"fr", false, "Palette",
			"legacy",
		},
		{
			"selectionpaletteascii", StartConnections,
			[]string{"ctrl+k"},
			"en", true, "Palette",
			"legacy",
		},
		{"selectionhelp", StartHome, []string{"?"}, "en", false, "Quick start", "Welcome page"},
		{"selectionhelpmouse", StartHome, []string{"?"}, "en", false, "Mouse", "Uninstalling"},
		{
			"selectionhelpfrench", StartHome,
			[]string{"?"},
			"fr", false, "Prise en main",
			"Page d\u2019accueil",
		},
	}

	for _, c := range cases {
		var copied []string

		g, _ := selectionRig(t, c.start, &copied, func(o *Options) {
			o.Printer = newPrinter(c.lang)
			o.ASCII = c.ascii

			if strings.HasSuffix(c.name, "none") {
				o.Environ = append(append([]string(nil), o.Environ...), "TOTE_COLOR=none")
			}
		})
		g.resize(80, 24)
		g.press(c.keys...)

		if c.name == "selectionhelpmouse" {
			g.view()
			g.press("end")
		}

		y0, x0 := lineOf(g, c.word)
		y1, x1 := lineOf(g, c.to)

		drag(g, cellPos{x0, y0}, cellPos{x1 + 3, y1})

		if len(copied) != 1 {
			t.Fatalf("%s: copied %q (screen:\n%s)", c.name, copied, g.text())
		}

		var b strings.Builder

		b.WriteString(strings.Join(markedScreen(g), "\n"))
		b.WriteString("\n-- copied --\n")
		b.WriteString(copied[0])
		b.WriteString("\n")

		compareGolden(t, "testdata/"+c.name+".golden", []byte(b.String()))
	}
}

const (
	// dragSamples is the number of samples of TestDragMotionCost for each measure.
	dragSamples = 12

	// dragRuns is the number of motion events or frames in a sample, about ten milliseconds.
	dragRuns = 12

	// dragAttempts is the number of times TestDragMotionCost measures before it fails.
	dragAttempts = 4

	// dragUnits is the most a motion event may cost, in units of referenceWork measured at the
	// same time: about four times what is measured and two fifths of what code ten times slower
	// costs.
	dragUnits = 20

	// dragRatio is the most that a motion event may cost compared with a frame without a
	// selection, twice or so what is measured.
	dragRatio = 3
)

// dragCost measures the cost of a motion event of a drag with a selection, and of a frame without
// one, in the same conditions.
//
// Parameters:
//   - g: rig on a list, 120 by 40, whose mouse button is held down over a selection
//
// Returns the lowest mean cost of the update and of the frame that follows it with the selection
// highlighted, and of a frame with no selection; see bestMean.
func dragCost(g *rig) (time.Duration, time.Duration) {
	i := 0

	motion := bestMean(dragSamples, dragRuns, func() {
		g.r.Update(tea.MouseMotionMsg{X: 40 + i%40, Y: 5 + i%3, Button: tea.MouseLeft})
		g.r.render()

		i++
	})

	g.r.sel = selection{}

	plain := bestMean(dragSamples, dragRuns, func() { g.r.render() })

	return motion, plain
}

// TestDragMotionCost measures what one motion event of a drag costs on a 120 by 40 terminal, the
// update and the frame that follows it with the selection highlighted, and compares it with a
// frame without a selection. The terminal sends at most one event per cell crossed, and the
// renderer of Bubble Tea draws at most renderFPS frames a second, so the cost bounds the load of a
// drag.
//
// A clock on the wall is the only one a test has, and on a loaded machine it counts the time of
// the other processes. Each measure is the best of several short samples, which keeps the
// samples that nobody interrupted, and the test is given dragAttempts tries: it fails only when
// every try is over the limits. The limits are in units that follow the speed of the machine: a
// ratio to the frame of the same run, which catches the part that belongs to the selection, and a
// number of times a fixed piece of work that does not use the interface (referenceWork), which
// catches code that is slower as a whole. Code ten times slower fails both.
//
// Parameters:
//   - t: test to report to
func TestDragMotionCost(t *testing.T) {
	var copied []string

	g, _ := selectionRig(t, StartConnections, &copied, nil)
	g.resize(120, 40)

	var motion, plain, unit time.Duration

	for try := 1; try <= dragAttempts; try++ {
		g.send(tea.MouseClickMsg{X: 3, Y: 4, Button: tea.MouseLeft})
		g.send(tea.MouseMotionMsg{X: 60, Y: 6, Button: tea.MouseLeft})

		motion, plain = dragCost(g)

		t.Logf("try %d: a motion event with a selection: %v, a frame without: %v", try, motion,
			plain)

		unit = bestMean(dragSamples, dragRuns, referenceWork)
		t.Logf("try %d: the reference work: %v", try, unit)

		if motion <= dragUnits*unit && motion <= dragRatio*plain {
			return
		}
	}

	t.Errorf("a motion event takes %v and a frame without a selection %v, want at most %d times "+
		"the reference work (%v) and %d times the frame", motion, plain, dragUnits, unit,
		dragRatio)
}

// TestSelectionHints checks where the user learns about the selection: the muted line of the
// settings screen, under the mouse row, in both languages.
//
// Parameters:
//   - t: test to report to
func TestSelectionHints(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		var copied []string

		g, _ := selectionRig(t, StartHome, &copied, func(o *Options) {
			o.Printer = newPrinter(lang)
		})

		for _, size := range [][2]int{{60, 12}, {100, 30}, {120, 40}} {
			g.resize(size[0], size[1])
			g.r.scr = newConfig(g.r.e, "mouse")

			want := g.r.e.t("ui_sel_hint")
			text := strings.Join(strings.Fields(g.text()), " ")

			if size[0] < 100 {
				want = strings.Fields(g.r.e.t("ui_setd_mouse"))[0]
			}

			if !strings.Contains(text, want) {
				t.Errorf("%s %v: the settings screen lacks %q:\n%s", lang, size, want, g.text())
			}
		}
	}
}

// TestSelectionEscapeWithoutHighlight checks that Escape keeps its usual meaning when there is no
// highlight to clear.
//
// Parameters:
//   - t: test to report to
func TestSelectionEscapeWithoutHighlight(t *testing.T) {
	var copied []string

	g, _ := selectionRig(t, StartConnections, &copied, nil)
	g.press("esc")

	if _, home := g.r.scr.(*homeScreen); !home {
		t.Errorf("escape on a list did not go back to the welcome page: %T", g.r.scr)
	}
}

// TestSelectionOverStatus checks that the toast of the copy, which is drawn on the status line,
// does not remove the highlight of a drag that ran over that line.
//
// Parameters:
//   - t: test to report to
func TestSelectionOverStatus(t *testing.T) {
	var copied []string

	g, _ := selectionRig(t, StartConnections, &copied, nil)
	y, x := lineOf(g, "staging")

	drag(g, cellPos{x, y}, cellPos{20, g.r.e.Height - 2})

	if len(copied) != 1 {
		t.Fatalf("copied %q", copied)
	}

	if g.toastText() == "" {
		t.Error("no toast after the copy")
	}

	if !g.r.sel.drag || !strings.Contains(strings.Join(markedScreen(g), "\n"), "[[") {
		t.Error("the toast of the copy removed the highlight")
	}
}
