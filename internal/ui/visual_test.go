package ui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// sgrPattern matches one SGR escape sequence of a styled line.
var sgrPattern = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

// cellStyle is the foreground color, as "r;g;b", and the weight of a character of a styled line.
type cellStyle struct {
	fg   string
	bold bool
}

// styleAt tells how the first occurrence of a text is drawn in a styled line.
//
// Parameters:
//   - t: test to report to
//   - line: styled line, with SGR sequences
//   - needle: plain ASCII text to find
//
// Returns the style of its first character. The test stops when the text is not on the line.
func styleAt(t *testing.T, line, needle string) cellStyle {
	t.Helper()

	var (
		plain strings.Builder
		cur   cellStyle
		per   []cellStyle
	)

	rest := line

	for rest != "" {
		loc := sgrPattern.FindStringSubmatchIndex(rest)
		if loc == nil || loc[0] > 0 {
			end := len(rest)
			if loc != nil {
				end = loc[0]
			}

			for range end {
				per = append(per, cur)
			}

			plain.WriteString(rest[:end])
			rest = rest[end:]

			continue
		}

		params := strings.Split(rest[loc[2]:loc[3]], ";")
		for i := 0; i < len(params); i++ {
			switch n, _ := strconv.Atoi(params[i]); {
			case n == 0:
				cur = cellStyle{}
			case n == 1:
				cur.bold = true
			case n == 22:
				cur.bold = false
			case n == 39:
				cur.fg = ""
			case n == 38 && i+4 < len(params) && params[i+1] == "2":
				cur.fg = strings.Join(params[i+2:i+5], ";")
				i += 4
			}
		}

		rest = rest[loc[1]:]
	}

	at := strings.Index(plain.String(), needle)
	if at < 0 {
		t.Fatalf("%q is not on the line %q", needle, plain.String())
	}

	return per[at]
}

// lineWith returns the styled line of a frame whose plain text holds a text.
//
// Parameters:
//   - t: test to report to
//   - g: rig to read the frame of
//   - needle: plain text to look for
func lineWith(t *testing.T, g *rig, needle string) string {
	t.Helper()

	for _, l := range g.view() {
		if strings.Contains(stripStyles(l), needle) {
			return l
		}
	}

	t.Fatalf("no line holds %q:\n%s", needle, g.text())

	return ""
}

// visualRig builds a rig at a size with the dark theme at true color.
//
// Parameters:
//   - t: test to report to
//   - w: width in cells
//   - h: height in rows
//   - start: screen to open
//   - setup: edits the options, may be nil
func visualRig(t *testing.T, w, h int, start Start, setup func(*Options)) *rig {
	t.Helper()

	return newRig(t, newSandbox(t).with(func(o *Options) {
		o.Width, o.Height, o.Start = w, h, start
		o.Settings.UI.Theme = "dark"

		if setup != nil {
			setup(o)
		}
	}))
}

// TestVisualWelcomeColors pins the colors of the welcome box to the mockups
// (plans/identity/readme/html/home-dark.html and tui.html): the greeting is bold, the tagline is
// muted and the line of counts is in the plain text color. The interface draws the two the other
// way round, so the tagline stands out and the counts fade.
//
// Parameters:
//   - t: test to report to
func TestVisualWelcomeColors(t *testing.T) {
	g := visualRig(t, 110, 30, StartHome, nil)
	muted, text := darkPalette.muted.rgb, darkPalette.text.rgb

	rgb := func(hex string) string {
		var r, gr, b int

		if _, err := fmt.Sscanf(hex, "#%02X%02X%02X", &r, &gr, &b); err != nil {
			t.Fatal(err)
		}

		return fmt.Sprintf("%d;%d;%d", r, gr, b)
	}

	tag := styleAt(t, lineWith(t, g, "Your terminal tools"), "Your terminal tools")
	counts := styleAt(t, lineWith(t, g, "connections \u00b7"), "2 aliases")

	if tag.fg != rgb(muted) {
		t.Errorf("the tagline is drawn with %q, the mockups draw it muted (%q)", tag.fg, rgb(muted))
	}

	if counts.fg != rgb(text) {
		t.Errorf("the counts are drawn with %q, the mockups draw them plain (%q)", counts.fg,
			rgb(text))
	}
}

// TestVisualFooterIsMuted pins the footer to the mockups, where the whole line of hints, the keys
// included, is in the muted color and nothing is bold. The interface draws every key bold in the
// plain text color.
//
// Parameters:
//   - t: test to report to
func TestVisualFooterIsMuted(t *testing.T) {
	for _, start := range []Start{StartHome, StartConnections} {
		g := visualRig(t, 110, 30, start, nil)
		foot := lineWith(t, g, "ctrl+k")

		key, desc := styleAt(t, foot, "ctrl+k"), styleAt(t, foot, "palette")
		if key != desc {
			t.Errorf("start %d: the key ctrl+k is drawn %+v and its description %+v; the mockups "+
				"draw the whole footer in one muted style", start, key, desc)
		}
	}
}

// TestVisualEmptyListFooter checks that the footer of a list with no record does not offer the
// keys that act on a record: with nothing under the cursor, "connect", "edit", "delete" and "next
// user" do nothing.
//
// Parameters:
//   - t: test to report to
func TestVisualEmptyListFooter(t *testing.T) {
	g := visualRig(t, 110, 30, StartConnections, func(o *Options) {
		writeFile(t, filepath.Join(o.Paths.Config, "connections.toml"), "schema = 1\n")
	})

	if !strings.Contains(g.text(), "Nothing here yet") {
		t.Fatalf("the list is not empty:\n%s", g.text())
	}

	foot := g.footerText()

	for _, word := range []string{"connect", "edit", "delete", "next user"} {
		if strings.Contains(foot, word) {
			t.Errorf("the footer of an empty list offers %q: %q", word, foot)
		}
	}
}

// TestVisualFormTellsAboutHiddenFields checks that a form too tall for the terminal says that
// fields are out of view, as the lists, the palette and the help do with their "N more" lines.
// At 45 by 11 the form shows five of its eight fields and says nothing about the others.
//
// Parameters:
//   - t: test to report to
func TestVisualFormTellsAboutHiddenFields(t *testing.T) {
	g := visualRig(t, 45, 11, StartConnections, nil)
	g.press("n")

	text := g.text()
	if !strings.Contains(text, "New connection") || strings.Contains(text, "description") {
		t.Fatalf("expected a form that hides its last fields:\n%s", text)
	}

	if !strings.Contains(text, g.r.e.Glyphs.below) {
		t.Errorf("the form hides fields and shows no marker for them:\n%s", text)
	}
}

// TestVisualFormCursorStandsOut checks the text cursor of an empty form field. It is drawn with
// the colors of the selected row, which have a contrast of 1.3 to 1 with the page, so on an empty
// field it is nearly invisible, and it is not the caret of the palette. It must be reverse video
// or the caret glyph.
//
// Parameters:
//   - t: test to report to
func TestVisualFormCursorStandsOut(t *testing.T) {
	g := visualRig(t, 110, 30, StartConnections, nil)
	g.press("n")

	line := lineWith(t, g, "\u276f name")

	var r, gr, b int
	if _, err := fmt.Sscanf(darkPalette.selBG.rgb, "#%02X%02X%02X", &r, &gr, &b); err != nil {
		t.Fatal(err)
	}

	paint := fmt.Sprintf("48;2;%d;%d;%d", r, gr, b)
	reverse := false

	for _, m := range sgrPattern.FindAllStringSubmatch(line, -1) {
		reverse = reverse || slices.Contains(strings.Split(m[1], ";"), "7")
	}

	if strings.Contains(line, paint) && !reverse && !strings.Contains(line, g.r.e.Glyphs.cursor) {
		t.Errorf("the cursor of the empty field is only painted with the selection color: %q",
			line)
	}
}

// TestVisualUnknownCountWord checks the line of counts of the welcome box when a data file cannot
// be read: the unknown count must be worded like the others ("? connections"), not with the short
// word of the compact header ("? conn") between full words.
//
// Parameters:
//   - t: test to report to
func TestVisualUnknownCountWord(t *testing.T) {
	g := visualRig(t, 110, 30, StartHome, func(o *Options) {
		writeFile(t, filepath.Join(o.Paths.Config, "connections.toml"),
			"schema = 1\n\n[[connection]\nname = \"x\"\n")
	})

	if text := g.text(); strings.Contains(text, "? conn ") {
		t.Errorf("the welcome box mixes a short word with full ones: %s", text)
	}
}

// TestVisualPaletteQueryShowsItsEnd checks that a long query keeps its end on screen: what is
// typed last must stay visible next to the cursor. The query is cut at the right with an ellipsis
// instead, so the last characters typed cannot be seen.
//
// Parameters:
//   - t: test to report to
func TestVisualPaletteQueryShowsItsEnd(t *testing.T) {
	g := paletteRig(t, 110, 30, strings.Repeat("x", 90)+"END", nil)

	if text := g.text(); !strings.Contains(text, "END") {
		t.Errorf("the end of the query is not visible:\n%s", text)
	}
}

// boxOf finds the box of an overlay in a plain frame.
//
// Parameters:
//   - e: shared environment, for the width rule
//   - lines: plain lines of the frame
//
// Returns the top and bottom rows of the first box, and the blank cells before and after it.
func boxOf(e *env, lines []string) (int, int, int, int, bool) {
	top, bottom := -1, -1

	for i, l := range lines {
		s := strings.TrimLeft(l, " ")

		if strings.HasPrefix(s, "\u256d") && top < 0 {
			top = i
		}

		if strings.HasPrefix(s, "\u2570") {
			bottom = i
		}
	}

	if top < 0 || bottom < top {
		return 0, 0, 0, 0, false
	}

	lead, right := frameBounds(e, lines[top])

	return top, bottom, lead, e.Cols - right, true
}

// TestVisualOverlaysAreCentered pins the placement of every overlay: whatever the size of the
// terminal, the box has the same number of blank cells on both sides (one more at most) and the
// box and its footer are centered vertically (two rows more or less).
//
// Parameters:
//   - t: test to report to
func TestVisualOverlaysAreCentered(t *testing.T) {
	opens := map[string][]string{
		"palette": {"ctrl+k"}, "form": {"n"}, "confirm": {"d"}, "details": {"v"}, "help": {"?"},
	}

	for _, size := range [][2]int{{60, 12}, {80, 24}, {110, 30}, {120, 40}, {160, 40}, {200, 50}} {
		for name, keys := range opens {
			g := visualRig(t, size[0], size[1], StartConnections, nil)
			g.press(keys...)

			e := g.r.e
			lines := strings.Split(g.text(), "\n")

			top, bottom, left, right, ok := boxOf(e, lines)
			if !ok {
				t.Errorf("%s at %v: no box:\n%s", name, size, g.text())
				continue
			}

			if d := left - right; d < -1 || d > 1 {
				t.Errorf("%s at %v: %d blank cells at the left, %d at the right", name, size,
					left, right)
			}

			foot := 0

			for _, l := range lines[bottom+1:] {
				if strings.TrimSpace(l) != "" {
					foot++
				}
			}

			above, below := top, len(lines)-1-bottom-foot

			if d := above - below; d < -2 || d > 2 {
				t.Errorf("%s at %v: %d rows above the box and %d below it", name, size, above,
					below)
			}
		}
	}
}

// TestVisualAmbiguousDataKeepsColumns checks that a list whose values hold characters of
// different widths keeps its columns: in every row the description starts at the same cell. The
// values are a wide character, an emoji, a combining accent, a joiner sequence, a flag, a
// variation selector and a full width letter, all of which the renderer must measure as the
// terminal does.
//
// Parameters:
//   - t: test to report to
func TestVisualAmbiguousDataKeepsColumns(t *testing.T) {
	values := []string{
		"plain", "e\u0301e\u0301", "\u30c6\u30b9\u30c8", "\U0001F680", "\U0001F468\u200D\U0001F469",
		"\U0001F1EB\U0001F1F7", "\u2764\uFE0F", "\uFF21\uFF22", "\U0001F44D\U0001F3FD", "\u00e9",
	}

	body := "schema = 1\n"
	for i, v := range values {
		body += fmt.Sprintf("\n[[alias]]\nname = \"a%d\"\ncmd = \"echo [%s]\"\n", i, v) +
			"desc = \"DESC\"\n"
	}

	for _, width := range []int{60, 80, 110} {
		g := visualRig(t, width, 30, StartAliases, func(o *Options) {
			writeFile(t, filepath.Join(o.Paths.Config, "aliases.toml"), body)
		})

		e := g.r.e
		at := -1

		for _, l := range strings.Split(g.text(), "\n") {
			i := strings.Index(l, "DESC")
			if i < 0 || !strings.Contains(l, "echo [") {
				continue
			}

			col := e.Metric.width(l[:i])
			if at >= 0 && col != at {
				t.Errorf("width %d: a description starts at %d, the others at %d: %q", width,
					col, at, l)
			}

			at = col
		}

		if at < 0 {
			t.Fatalf("width %d: no description on screen:\n%s", width, g.text())
		}
	}
}

// TestVisualResizeKeepsTheOverlay checks that shrinking the terminal below the minimum while an
// overlay is open, and growing it again, brings back the same overlay with its text and a frame
// that fits every size on the way.
//
// Parameters:
//   - t: test to report to
func TestVisualResizeKeepsTheOverlay(t *testing.T) {
	g := paletteRig(t, 110, 30, "conn", nil)
	want := g.text()

	for _, size := range [][2]int{{60, 12}, {39, 9}, {200, 50}, {45, 11}, {110, 30}} {
		g.resize(size[0], size[1])

		lines := strings.Split(g.text(), "\n")
		if len(lines) != size[1] {
			t.Errorf("%v: %d lines", size, len(lines))
		}

		for i, l := range lines {
			if w := g.r.e.Metric.width(l); w > size[0] {
				t.Errorf("%v: line %d is %d cells wide: %q", size, i, w, l)
			}
		}
	}

	if got := g.text(); got != want {
		t.Errorf("the palette did not come back as it was:\n%s\nwant\n%s", got, want)
	}
}

// TestVisualNoColorUsesNoColor pins the NO_COLOR frames of every screen to bold, italic and
// reverse video: no foreground or background color sequence may appear.
//
// Parameters:
//   - t: test to report to
func TestVisualNoColorUsesNoColor(t *testing.T) {
	colored := func(line string) bool {
		for _, m := range sgrPattern.FindAllStringSubmatch(line, -1) {
			for _, p := range strings.Split(m[1], ";") {
				n, _ := strconv.Atoi(p)
				if (n >= 30 && n <= 38) || (n >= 40 && n <= 48) || (n >= 90 && n <= 107) {
					return true
				}
			}
		}

		return false
	}

	for _, start := range []Start{
		StartHome, StartConnections, StartRepos, StartAliases,
		StartFuncs, StartConfig,
	} {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Width, o.Height, o.Start = 110, 30, start
			o.Environ = append(o.Environ, "NO_COLOR=1")
		}))

		for _, keys := range [][]string{nil, {"ctrl+k"}, {"esc", "?"}} {
			g.press(keys...)

			for i, l := range g.view() {
				if colored(l) {
					t.Errorf("start %d, keys %v: line %d has a color: %q", start, keys, i, l)
				}
			}
		}
	}
}
