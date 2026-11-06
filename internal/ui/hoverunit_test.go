package ui

import (
	"math"
	"strconv"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// luminance computes the relative luminance of a color written as #RRGGBB, as the contrast rules of
// WCAG define it.
//
// Parameters:
//   - t: running test
//   - rgb: color to measure
func luminance(t *testing.T, rgb string) float64 {
	t.Helper()

	var ch [3]float64

	for i := range ch {
		v, err := strconv.ParseUint(rgb[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("%q is not a color: %v", rgb, err)
		}

		c := float64(v) / 255

		ch[i] = c / 12.92
		if c > 0.03928 {
			ch[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}

	return 0.2126*ch[0] + 0.7152*ch[1] + 0.0722*ch[2]
}

// contrast computes the contrast ratio of two colors written as #RRGGBB.
//
// Parameters:
//   - t: running test
//   - a: first color
//   - b: second color
func contrast(t *testing.T, a, b string) float64 {
	t.Helper()

	la, lb := luminance(t, a), luminance(t, b)
	if la < lb {
		la, lb = lb, la
	}

	return (la + 0.05) / (lb + 0.05)
}

// TestHoverTone checks the background of the hover in each palette: the text and the muted text
// stay readable on it (4.5 or more), it is visible against the background of the terminal that
// the palette is made for, and it stays well short of the selected row in that respect. The
// background of the terminal is written as its three channels, since a color value belongs to
// theme.go.
//
// Parameters:
//   - t: test to report to
func TestHoverTone(t *testing.T) {
	tests := []struct {
		name string
		pal  palette
		base string
	}{
		{"dark", darkPalette, "#" + hexOf(21, 19, 26)},
		{"light", lightPalette, "#" + hexOf(251, 248, 244)},
	}

	for _, tt := range tests {
		hover := tt.pal.hover.rgb

		fgs := map[string]string{"text": tt.pal.text.rgb, "muted": tt.pal.muted.rgb}

		for name, fg := range fgs {
			if got := contrast(t, fg, hover); got < 4.5 {
				t.Errorf("%s: %s on the hover is %.2f, want 4.5 or more", tt.name, name, got)
			}
		}

		seen, chosen := contrast(t, hover, tt.base), contrast(t, tt.pal.selBG.rgb, tt.base)
		if seen < 1.05 {
			t.Errorf("%s: the hover is %.2f from the background, too faint to see", tt.name, seen)
		}

		if seen >= chosen {
			t.Errorf("%s: the hover (%.2f) is not weaker than the selection (%.2f)", tt.name, seen,
				chosen)
		}

		if tt.pal.hover.c256 == "" || tt.pal.hover.c256 == tt.pal.selBG.c256 {
			t.Errorf("%s: the 256 color hover is %q", tt.name, tt.pal.hover.c256)
		}
	}
}

// TestHoverTone256 checks the hover of the palettes with 256 colors, which are greys of the cube of
// the terminal: the text stays readable on them, and the muted text of the dark one is a little
// short of 4.5 (4.29), which is accepted since the tone must stay close to the background to be
// discreet. A change of tone that goes further is noticed here.
//
// Parameters:
//   - t: test to report to
func TestHoverTone256(t *testing.T) {
	tests := []struct {
		name  string
		pal   palette
		muted float64
	}{
		{"dark", darkPalette, 4.2},
		{"light", lightPalette, 4.5},
	}

	for _, tt := range tests {
		n, err := strconv.Atoi(tt.pal.hover.c256)
		if err != nil || n < 232 || n > 255 {
			t.Fatalf("%s: the 256 color hover %q is not one of the greys", tt.name,
				tt.pal.hover.c256)
		}

		v := 8 + 10*(n-232)
		grey := "#" + hexOf(v, v, v)

		if got := contrast(t, tt.pal.text.rgb, grey); got < 4.5 {
			t.Errorf("%s: the text on %s is %.2f, want 4.5 or more", tt.name, grey, got)
		}

		if got := contrast(t, tt.pal.muted.rgb, grey); got < tt.muted {
			t.Errorf("%s: the muted text on %s is %.2f, want %.1f or more", tt.name, grey, got,
				tt.muted)
		}
	}
}

// hexOf writes three channels as six hexadecimal digits.
//
// Parameters:
//   - r: red
//   - g: green
//   - b: blue
func hexOf(r, g, b int) string {
	const digits = "0123456789ABCDEF"

	out := make([]byte, 0, 6)

	for _, c := range []int{r, g, b} {
		out = append(out, digits[c>>4], digits[c&15])
	}

	return string(out)
}

// TestThemeHover checks the role of the theme in each profile: a background of the palette with
// 256 colors and true color, and an underline, with no color, with 16 colors and with none.
//
// Parameters:
//   - t: test to report to
func TestThemeHover(t *testing.T) {
	back := func(pal palette, p colorprofile.Profile) string {
		return ansi.NewStyle().BackgroundColor(pal.hover.color(p)).String()
	}

	tests := []struct {
		name string
		mode themeMode
		p    colorprofile.Profile
		want string
	}{
		{
			"truecolor dark", modeDark, colorprofile.TrueColor,
			back(darkPalette, colorprofile.TrueColor),
		},
		{
			"truecolor light", modeLight, colorprofile.TrueColor,
			back(lightPalette, colorprofile.TrueColor),
		},
		{"256 dark", modeDark, colorprofile.ANSI256, back(darkPalette, colorprofile.ANSI256)},
		{"256 light", modeLight, colorprofile.ANSI256, back(lightPalette, colorprofile.ANSI256)},
		{"16", modeDark, colorprofile.ANSI, "\x1b[4m"},
		{"ascii profile", modeDark, colorprofile.ASCII, "\x1b[4m"},
		{"no tty", modeLight, colorprofile.NoTTY, "\x1b[4m"},
		{"neutral", modeNeutral, colorprofile.TrueColor, "\x1b[4m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := newTheme(tt.mode, tt.p)

			if got := th.Hover.String(); got != tt.want {
				t.Errorf("Hover = %q, want %q", got, tt.want)
			}

			if bg := tt.want != "\x1b[4m"; th.underline == bg {
				t.Errorf("underline = %v for a hover that is a background: %v", th.underline, bg)
			}
		})
	}
}

// TestThemeHoverText checks how the role is put on a styled span: at the start, again after each
// reset inside the span, a reset at the end and none doubled.
//
// Parameters:
//   - t: test to report to
func TestThemeHoverText(t *testing.T) {
	th := newTheme(modeDark, colorprofile.ANSI)
	open := th.Hover.String()

	tests := []struct {
		name string
		span string
		want string
	}{
		{"plain", "abc", open + "abc\x1b[m"},
		{"one style", "\x1b[1mabc\x1b[m", open + "\x1b[1mabc\x1b[m"},
		{
			"two styles", "\x1b[1ma\x1b[m \x1b[3mb\x1b[m",
			open + "\x1b[1ma\x1b[m" + open + " " + "\x1b[3mb\x1b[m",
		},
		{"long reset", "\x1b[1ma\x1b[0m b", open + "\x1b[1ma\x1b[0m" + open + " b\x1b[m"},
		{"empty", "", open + "\x1b[m"},
	}

	for _, tt := range tests {
		if got := th.hover(tt.span); got != tt.want {
			t.Errorf("%s: hover(%q) = %q, want %q", tt.name, tt.span, got, tt.want)
		}
	}

	if got := (theme{}).hover("abc"); got != "abc" {
		t.Errorf("a theme with no hover changed the text: %q", got)
	}
}

// TestThemeSelected checks which spans count as the selected row: a span with text drawn while the
// selection style is on, whether it is set inside the span or before it, and not one whose
// selected part was closed before its text.
//
// Parameters:
//   - t: test to report to
func TestThemeSelected(t *testing.T) {
	for _, p := range []colorprofile.Profile{
		colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII,
	} {
		th := newTheme(modeDark, p)
		sel, text := th.Selected.Render("row"), th.Text.Render("other")

		tests := []struct {
			name string
			span string
			want bool
		}{
			{"the selected row", sel, true},
			{"a part of it", sel[:len(sel)-len("\x1b[m")-1] + "\x1b[m", true},
			{"selection open from before", th.selOpen + "tail", true},
			{"selection closed before the text", sel + text, true},
			{"only the text after it", th.selOpen + "\x1b[m" + text, false},
			{"re-emitted and closed", th.selOpen + "\x1b[m\x1b[m" + text, false},
			{"plain", "plain text", false},
			{"the text", text, false},
			{"empty", "", false},
			{"a cut sequence", "\x1b[1;7", false},
		}

		for _, tt := range tests {
			if got := th.selected(tt.span); got != tt.want {
				t.Errorf("profile %v, %s: selected(%q) = %v, want %v", p, tt.name, tt.span, got,
					tt.want)
			}
		}
	}

	if (theme{}).selected("anything") {
		t.Error("a theme that has no selection style selects")
	}
}

// TestTidy checks that the leftovers of a cut are removed and nothing else: a style closed at
// once, doubled resets and a reset at the start.
//
// Parameters:
//   - t: test to report to
func TestTidy(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"nothing", "abc", "abc"},
		{"style kept", "\x1b[1mabc\x1b[m", "\x1b[1mabc\x1b[m"},
		{"style closed at once", "\x1b[1m\x1b[mabc", "abc"},
		{
			"several closed at once", "\x1b[1m\x1b[m\x1b[3;38;5;1m\x1b[m\x1b[4mabc\x1b[m",
			"\x1b[4mabc\x1b[m",
		},
		{"doubled resets", "a\x1b[m\x1b[m\x1b[mb", "a\x1b[mb"},
		{"a style that is open stays", "\x1b[1m\x1b[m\x1b[38;5;2mabc", "\x1b[38;5;2mabc"},
		{"reset at the start", "\x1b[mabc", "abc"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		if got := tidy(tt.in); got != tt.want {
			t.Errorf("%s: tidy(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

// TestStyled checks the test for a style that is still on at the end of a text.
//
// Parameters:
//   - t: test to report to
func TestStyled(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"abc", false},
		{"\x1b[1mabc", true},
		{"\x1b[1mabc\x1b[m", false},
		{"\x1b[1mabc\x1b[0m", false},
		{"\x1b[1mabc\x1b[m\x1b[3m", true},
	}

	for _, tt := range tests {
		if got := styled(tt.in); got != tt.want {
			t.Errorf("styled(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestTextRuns checks how an underlined span is cut into runs: each run goes from a character that
// is not a blank to the last one before two blanks or more, a single blank does not cut, and the
// cells count in columns even for wide characters.
//
// Parameters:
//   - t: test to report to
func TestTextRuns(t *testing.T) {
	e := &env{Metric: metric{method: ansi.GraphemeWidth}}

	tests := []struct {
		name  string
		plain string
		x0    int
		want  [][2]int
	}{
		{"empty", "", 0, nil},
		{"blanks", "      ", 0, nil},
		{"one word", "abc", 4, [][2]int{{4, 7}}},
		{"a phrase", "ab cd ef", 0, [][2]int{{0, 8}}},
		{"two columns", "  ab    cd  ", 10, [][2]int{{12, 14}, {18, 20}}},
		{"three columns", "a  b  c", 0, [][2]int{{0, 1}, {3, 4}, {6, 7}}},
		{"a wide character", "\u65e5\u672c  x", 2, [][2]int{{2, 6}, {8, 9}}},
		{"leading and trailing", "   x   ", 0, [][2]int{{3, 4}}},
	}

	for _, tt := range tests {
		got := e.textRuns(tt.plain, tt.x0)

		if len(got) != len(tt.want) {
			t.Errorf("%s: textRuns(%q, %d) = %v, want %v", tt.name, tt.plain, tt.x0, got, tt.want)

			continue
		}

		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: textRuns(%q, %d) = %v, want %v", tt.name, tt.plain, tt.x0, got,
					tt.want)

				break
			}
		}
	}
}

// TestHoverOf checks the search for the element under a cell: the one drawn on top, the hints that
// press a key and the items, and nothing for a "N more" line or an empty cell.
//
// Parameters:
//   - t: test to report to
func TestHoverOf(t *testing.T) {
	hits := []hit{
		{y: 2, x: 0, w: 20, id: 0},
		{y: 3, x: 0, w: 20, id: zoneMoreUp},
		{y: 4, x: 5, w: 10, id: 7},
		{y: 4, x: 8, w: 4, id: 9},
		{y: 9, x: 2, w: 6, press: "esc"},
		{y: 9, x: 12, w: 4, id: zoneMoreDown},
	}

	tests := []struct {
		name string
		at   cellPos
		want hit
		ok   bool
	}{
		{"an item", cellPos{x: 3, y: 2}, hits[0], true},
		{"its last cell", cellPos{x: 19, y: 2}, hits[0], true},
		{"past its end", cellPos{x: 20, y: 2}, hit{}, false},
		{"a more line", cellPos{x: 3, y: 3}, hits[1], false},
		{"the one drawn on top", cellPos{x: 9, y: 4}, hits[3], true},
		{"under the one on top", cellPos{x: 6, y: 4}, hits[2], true},
		{"a hint", cellPos{x: 2, y: 9}, hits[4], true},
		{"the last more line", cellPos{x: 13, y: 9}, hits[5], false},
		{"an empty row", cellPos{x: 3, y: 5}, hit{}, false},
	}

	for _, tt := range tests {
		got, ok := hoverOf(hits, tt.at)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: hoverOf(%v) = %+v, %v, want %+v, %v", tt.name, tt.at, got, ok, tt.want,
				tt.ok)
		}
	}

	if _, ok := hoverOf(nil, cellPos{}); ok {
		t.Error("an element is found among no hits")
	}
}
