package ui

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// smallRig builds a rig on the welcome page at a size, with the given environment, glyph set
// and language.
//
// Parameters:
//   - t: running test
//   - w: width in cells
//   - h: height in rows
//   - env: variables added to the environment of the sandbox
//   - ascii: true for the ASCII glyph set
//   - lang: "en" or "fr"
func smallRig(t *testing.T, w, h int, env []string, ascii bool, lang string) *rig {
	t.Helper()

	g, _ := mouseRig(t, w, h, StartHome, func(o *Options) {
		o.Environ = append(append([]string(nil), o.Environ...), env...)
		o.ASCII = ascii
		o.Printer = newPrinter(lang)
	})

	g.send(tea.BackgroundColorMsg{Color: color.Black})

	return g
}

// blankEdges counts the empty lines above and under the content of a frame.
//
// Parameters:
//   - lines: lines of a frame without styles
func blankEdges(lines []string) (int, int) {
	top, bottom := 0, 0

	for top < len(lines) && strings.TrimSpace(lines[top]) == "" {
		top++
	}

	for bottom < len(lines)-top && strings.TrimSpace(lines[len(lines)-1-bottom]) == "" {
		bottom++
	}

	return top, bottom
}

// TestTooSmallTiers checks the three tiers of the too small screen at every size where one
// begins or ends, around the minimum of 24 columns by 7 rows: the line count, the width of every
// line, the centering, the size line, and the ASCII glyphs.
//
// Parameters:
//   - t: test to report to
func TestTooSmallTiers(t *testing.T) {
	tests := []struct {
		w, h   int
		tier   string
		rows   int
		size   string
		narrow bool
		short  bool
	}{
		{1, 1, "C", 0, "", true, true},
		{5, 2, "C", 0, "", true, true},
		{11, 4, "C", 0, "", true, true},
		{12, 3, "B", 2, "12 \u00D7 3", true, true},
		{19, 6, "B", 2, "19 \u00D7 6  \u2192 24 \u00D7 7", true, true},
		{20, 5, "B", 2, "20 \u00D7 5  needs 24 \u00D7 7", true, true},
		{20, 6, "A", 6, "20 \u00D7 6  needs 24 \u00D7 7", true, true},
		{24, 6, "A", 6, "24 \u00D7 6  needs 24 \u00D7 7", false, true},
		{26, 5, "B", 2, "26 \u00D7 5  needs 24 \u00D7 7", false, true},
		{23, 7, "A", 7, "23 \u00D7 7  needs 24 \u00D7 7", true, false},
		{23, 30, "A", 7, "23 \u00D7 30  needs 24 \u00D7 7", true, false},
		{120, 6, "A", 6, "120 \u00D7 6  needs 24 \u00D7 7", false, true},
		{60, 3, "B", 2, "60 \u00D7 3  needs 24 \u00D7 7", false, true},
	}

	modes := []struct {
		name  string
		env   []string
		ascii bool
	}{
		{"truecolor", []string{"TOTE_COLOR=truecolor"}, false},
		{"16 colors", []string{"TOTE_COLOR=16"}, false},
		{"mono", []string{"TOTE_COLOR=none"}, false},
		{"ascii", []string{"TOTE_COLOR=none", "LANG=C"}, true},
	}

	for _, m := range modes {
		for _, tt := range tests {
			name := fmt.Sprintf("%s %dx%d", m.name, tt.w, tt.h)
			g := smallRig(t, tt.w, tt.h, m.env, m.ascii, "en")
			raw := g.r.render()

			checkScreen(t, name, raw, tt.w, tt.h, m.ascii)

			lines := strings.Split(stripStyles(raw), "\n")
			text := strings.Join(lines, "\n")

			if tt.tier == "C" {
				want := g.r.e.Metric.fit("Terminal too small", tt.w, g.r.e.Glyphs.ellipsis)
				if lines[0] != want {
					t.Errorf("%s: first row %q, want %q", name, lines[0], want)
				}

				if strings.TrimSpace(strings.Join(lines[1:], "")) != "" {
					t.Errorf("%s: more than the message:\n%s", name, text)
				}

				continue
			}

			top, bottom := blankEdges(lines)
			if d := top - bottom; d < -1 || d > 1 || tt.h-top-bottom != tt.rows {
				t.Errorf("%s: %d rows above and %d under a block of %d rows:\n%s", name, top,
					bottom, tt.rows, text)
			}

			for i, l := range lines {
				if l == "" {
					continue
				}

				left := len(l) - len(strings.TrimLeft(l, " "))
				right := tt.w - left - g.r.e.Metric.width(strings.TrimSpace(l))

				if d := left - right; d < -1 || d > 1 {
					t.Errorf("%s: line %d is not centered (%d left, %d right): %q", name, i, left,
						right, l)
				}
			}

			g.checkSmallContent(t, name, text, raw, tt.tier, tt.size, tt.narrow, tt.short, m.ascii)
		}
	}
}

// checkSmallContent checks the words, the size line and the flagged dimension of a too small
// screen of the tier A or B.
//
// Parameters:
//   - t: running test
//   - name: name of the case
//   - text: screen without styles
//   - raw: screen with styles
//   - tier: "A" or "B"
//   - size: expected size line with the glyphs of the Unicode set
//   - narrow: true when the width is the dimension that is too small
//   - short: true when the height is the dimension that is too small
//   - ascii: true for the ASCII glyph set
func (g *rig) checkSmallContent(t *testing.T, name, text, raw, tier, size string, narrow, short,
	ascii bool,
) {
	t.Helper()

	if ascii {
		size = strings.NewReplacer("\u00D7", "x", "\u2192", "->").Replace(size)
	}

	if !strings.Contains(text, size) {
		t.Errorf("%s: no size line %q:\n%s", name, size, text)
	}

	title := g.r.e.Metric.fit("Terminal too small", g.r.e.Cols, g.r.e.Glyphs.ellipsis)

	for _, want := range []struct {
		text string
		tier string
	}{{title, "AB"}, {"tote", "A"}, {"Make the window", "A"}} {
		if has := strings.Contains(text, want.text); has != strings.Contains(want.tier, tier) {
			t.Errorf("%s: %q shown is %v on the tier %s:\n%s", name, want.text, has, tier, text)
		}
	}

	th := g.r.e.Theme
	dims := strings.Fields(strings.NewReplacer("\u00D7", " ", "x", " ").Replace(
		strings.SplitN(size, " ", 4)[0] + " " + strings.SplitN(size, " ", 4)[2]))

	var sizeRaw string

	for _, l := range strings.Split(raw, "\n") {
		if strings.Contains(stripStyles(l), size) {
			sizeRaw = l
		}
	}

	for i, flagged := range []bool{narrow, short} {
		if has := strings.Contains(sizeRaw, th.flagged().Render(dims[i])); has != flagged {
			t.Errorf("%s: dimension %s flagged is %v, want %v: %q", name, dims[i], has, flagged,
				sizeRaw)
		}
	}
}

// TestTooSmallTierCIsTheOldMessage checks that below the smallest tier the screen is what it
// was before the block: the message alone, cut to the width, on the first row.
//
// Parameters:
//   - t: test to report to
func TestTooSmallTierCIsTheOldMessage(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, s := range [][2]int{{1, 1}, {5, 2}, {11, 4}, {11, 40}, {30, 2}, {120, 1}, {200, 2}} {
			g := smallRig(t, s[0], s[1], nil, ascii, "en")
			e := g.r.e

			old := fitLines([]string{e.Metric.fit(e.t("ui_too_small"), e.Width, e.Glyphs.ellipsis)},
				e.Height)

			if got := g.r.render(); got != strings.Join(old, "\n") {
				t.Errorf("%dx%d ascii %v: got %q, want %q", s[0], s[1], ascii, got,
					strings.Join(old, "\n"))
			}
		}
	}
}

// TestTooSmallFlaggedHasMoreThanColor checks that the dimension that is too small differs from
// the other one by bold, and by an underline where the theme has no color to tell it.
//
// Parameters:
//   - t: test to report to
func TestTooSmallFlaggedHasMoreThanColor(t *testing.T) {
	underline := regexp.MustCompile("\x1b\\[(?:[0-9;]*;)?4(?:;[0-9;]*)?m")
	bold := regexp.MustCompile("\x1b\\[(?:[0-9;]*;)?1(?:;[0-9;]*)?m")

	for _, tt := range []struct {
		env       string
		underline bool
	}{
		{"TOTE_COLOR=truecolor", false},
		{"TOTE_COLOR=256", false},
		{"TOTE_COLOR=16", true},
		{"TOTE_COLOR=none", true},
	} {
		g := smallRig(t, 23, 12, []string{tt.env}, false, "en")
		th := g.r.e.Theme
		got := th.flagged().Render("23")

		if !bold.MatchString(got) {
			t.Errorf("%s: the flagged dimension is not bold: %q", tt.env, got)
		}

		if underline.MatchString(got) != tt.underline {
			t.Errorf("%s: underline is %v, want %v: %q", tt.env, !tt.underline, tt.underline, got)
		}

		if got == th.Text.Render("23") {
			t.Errorf("%s: the flagged dimension looks like the normal text", tt.env)
		}
	}
}

// TestTooSmallLanguages checks the French text, and that the ASCII set folds it.
//
// Parameters:
//   - t: test to report to
func TestTooSmallLanguages(t *testing.T) {
	g := smallRig(t, 80, 6, []string{"TOTE_COLOR=none"}, false, "fr")
	text := g.text()

	for _, want := range []string{
		"Terminal trop petit", "80 \u00D7 6  il faut 24 \u00D7 7",
		"Agrandissez la fen\u00EAtre, ou tapez q pour quitter.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("French screen lacks %q:\n%s", want, text)
		}
	}

	g = smallRig(t, 80, 6, []string{"TOTE_COLOR=none", "LANG=C"}, true, "fr")
	text = g.text()

	for _, want := range []string{"80 x 6  il faut 24 x 7", "Agrandissez la fenetre, ou tapez q"} {
		if !strings.Contains(text, want) {
			t.Errorf("French ASCII screen lacks %q:\n%s", want, text)
		}
	}
}

// TestTooSmallHintFollowsTheKeymap checks that the hint names the key of the quit binding.
//
// Parameters:
//   - t: test to report to
func TestTooSmallHintFollowsTheKeymap(t *testing.T) {
	g := smallRig(t, 80, 6, nil, false, "en")
	g.r.e.Keys.Quit = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "quit"))

	if text := g.text(); !strings.Contains(text, "press x to quit.") {
		t.Errorf("the hint does not name the key of the binding:\n%s", text)
	}
}

// TestTooSmallFollowsTheSize checks that the screen is drawn again at every resize, goes away
// when the terminal is big enough, comes back when it is not, and that the keys of the tiny
// layout are unchanged.
//
// Parameters:
//   - t: test to report to
func TestTooSmallFollowsTheSize(t *testing.T) {
	g := smallRig(t, 80, 24, []string{"TOTE_COLOR=none"}, false, "en")

	if strings.Contains(g.text(), "Terminal too small") {
		t.Fatalf("the normal screen says too small:\n%s", g.text())
	}

	steps := []struct {
		w, h int
		want string
	}{
		{23, 8, "23 \u00D7 8  needs 24 \u00D7 7"},
		{22, 9, "22 \u00D7 9  needs 24 \u00D7 7"},
		{80, 24, ""},
		{24, 7, ""},
		{20, 5, "20 \u00D7 5  needs 24 \u00D7 7"},
		{19, 5, "19 \u00D7 5  \u2192 24 \u00D7 7"},
		{80, 24, ""},
	}

	for _, st := range steps {
		g.resize(st.w, st.h)

		text := g.text()
		if has := strings.Contains(text, st.want); st.want != "" && !has {
			t.Errorf("%dx%d: lacks %q:\n%s", st.w, st.h, st.want, text)
		}

		if st.want == "" && (strings.Contains(text, "Terminal too small") ||
			g.r.e.Layout == layoutTiny || !strings.Contains(text, "Connections")) {
			t.Errorf("%dx%d: the normal screen did not come back:\n%s", st.w, st.h, text)
		}

		if got := len(g.view()); got != st.h {
			t.Errorf("%dx%d: %d lines", st.w, st.h, got)
		}
	}

	g.resize(23, 8)
	g.press("x", "esc", "down", "enter", "?")

	if g.Quit {
		t.Error("a key but the three of the exit quit the too small screen")
	}

	g.press("q")

	if !g.Quit {
		t.Error("q does not quit the too small screen")
	}
}

// TestTooSmallWrap checks how the hint is broken: whole, wrapped at the spaces when the rows
// allow it, or cut with an ellipsis when they do not.
//
// Parameters:
//   - t: test to report to
func TestTooSmallWrap(t *testing.T) {
	hint := "Make the window bigger, or press q to quit."

	tests := []struct {
		name string
		w, h int
		want []string
	}{
		{"whole", 80, 6, []string{hint}},
		{"wrapped", 23, 8, []string{"Make the window bigger,", "or press q to quit."}},
		{"wrapped at the narrowest block", 20, 8, []string{
			"Make the window", "bigger, or press",
			"q to quit.",
		}},
		{"wrapped at the minimum", 26, 6, []string{
			"Make the window bigger,",
			"or press q to quit.",
		}},
		{"no room to wrap", 20, 6, []string{"Make the window big\u2026"}},
		{"no room to wrap ascii", 20, 6, []string{"Make the window b..."}},
	}

	for _, tt := range tests {
		ascii := strings.Contains(tt.name, "ascii")
		g := smallRig(t, tt.w, tt.h, []string{"TOTE_COLOR=none"}, ascii, "en")

		var got []string

		hint, whole := g.r.e.smallHint(tt.h - 4)
		for _, l := range hint {
			got = append(got, strings.TrimSpace(stripStyles(l)))
		}

		if want := !strings.Contains(tt.name, "no room"); whole != want {
			t.Errorf("%s: whole = %v, want %v", tt.name, whole, want)
		}

		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}

	g := smallRig(t, 23, 8, nil, false, "en")

	for _, tt := range []struct {
		text string
		w    int
		want string
	}{
		{"", 10, ""},
		{"one two", 10, "one two"},
		{"one two three", 7, "one two|three"},
		{"abcdefghijkl mn", 6, "abcde\u2026|mn"},
	} {
		if got := strings.Join(g.r.e.wrap(tt.text, tt.w), "|"); got != tt.want {
			t.Errorf("wrap(%q, %d) = %q, want %q", tt.text, tt.w, got, tt.want)
		}
	}
}
