package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// unsafeRune reports whether a rune could act on the terminal or hide text.
//
// Parameters:
//   - r: rune to test
//
// Returns true for control characters, C1 controls, line and paragraph separators and format
// characters such as bidirectional controls, zero width spaces and tag characters. The two
// joiners U+200C and U+200D are kept: whether they stay is decided per grapheme by sanitize.
func unsafeRune(r rune) bool {
	if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
		return true
	}

	return unicode.Is(unicode.Cf, r) && r != '\u200C' && r != '\u200D'
}

// emojiModifier reports whether a rune only changes how an emoji is drawn: the variation
// selectors, the zero width joiner and the skin tone modifiers.
//
// Parameters:
//   - r: rune to test
func emojiModifier(r rune) bool {
	return r == '\uFE0F' || r == '\uFE0E' || r == '\u200D' || (r >= 0x1f3fb && r <= 0x1f3ff)
}

// sanitize makes a string coming from user data safe to draw and to measure.
//
// Parameters:
//   - s: text read from a data file, a path or a command output, any bytes
//
// Returns s where every control character, escape, C1 control, bidirectional control, tag
// character and invalid byte is replaced by a question mark. Then each grapheme is kept only when
// the two width rules of the renderer agree on it, the wide character rule and the grapheme
// rule. When they disagree, the variation selectors, the zero width joiner and the skin tone
// modifiers are removed from the grapheme; when they still disagree, the grapheme becomes a
// question mark. The result never contains an escape and never changes width between terminals.
func sanitize(s string) string {
	if isPlain(s) {
		return s
	}

	var b strings.Builder

	b.Grow(len(s))

	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if (r == utf8.RuneError && size == 1) || unsafeRune(r) {
			r = '?'
		}

		b.WriteRune(r)
		s = s[size:]
	}

	return graphemes(b.String())
}

// graphemes applies the width agreement rule of sanitize to a string made of safe runes.
//
// Parameters:
//   - s: valid UTF-8 without control characters
func graphemes(s string) string {
	if isPlain(s) {
		return s
	}

	var b strings.Builder

	for len(s) > 0 {
		cluster, gw := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if cluster == "" {
			break
		}

		s = s[len(cluster):]

		if gw == ansi.StringWidthWc(cluster) {
			b.WriteString(cluster)
			continue
		}

		stripped := strings.Map(func(r rune) rune {
			if emojiModifier(r) {
				return -1
			}

			return r
		}, cluster)

		if stripped != "" && ansi.StringWidth(stripped) == ansi.StringWidthWc(stripped) {
			b.WriteString(stripped)
			continue
		}

		b.WriteByte('?')
	}

	return b.String()
}

// isPlain reports whether a string is printable ASCII, which needs no grapheme analysis.
//
// Parameters:
//   - s: string to test
func isPlain(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] >= 0x7f {
			return false
		}
	}

	return true
}

// metric measures and cuts text the way the terminal renderer counts cells.
//
// The wide character rule is the default; the grapheme rule is used once the terminal reported
// that it clusters graphemes itself. Text that went through sanitize has the same width under
// both rules, so the choice only matters for text that did not.
type metric struct {
	method ansi.Method
}

// newMetric returns the metric of a terminal that counts wide characters.
func newMetric() metric {
	return metric{method: ansi.WcWidth}
}

// width returns the number of cells a string takes, escape sequences counting for none.
//
// Parameters:
//   - s: plain or styled text
func (m metric) width(s string) int {
	return m.method.StringWidth(s)
}

// cut returns the longest start of a plain string that fits in a number of cells.
//
// Parameters:
//   - s: plain text, which must not contain escape sequences
//   - w: available cells; zero or less gives an empty string
//
// Returns whole graphemes only, so a wide character that does not fit is dropped.
func (m metric) cut(s string, w int) string {
	if w <= 0 {
		return ""
	}

	used, end := 0, 0

	for end < len(s) {
		cluster, cw := ansi.FirstGraphemeCluster(s[end:], m.method)
		if cluster == "" || used+cw > w {
			break
		}

		used += cw
		end += len(cluster)
	}

	return s[:end]
}

// fit shortens a plain string to a width, ending it with a tail when it is cut.
//
// Parameters:
//   - s: plain text, which must not contain escape sequences
//   - w: available cells; zero or less gives an empty string
//   - tail: marker appended after a cut, usually an ellipsis
//
// Returns s when it fits, else the longest start of s that leaves room for the tail, followed
// by the tail. The result never exceeds w cells, even when the tail alone is wider than w.
func (m metric) fit(s string, w int, tail string) string {
	if w <= 0 {
		return ""
	}

	if m.width(s) <= w {
		return s
	}

	if tw := m.width(tail); tw >= w {
		return m.cut(tail, w)
	}

	return m.cut(s, w-m.width(tail)) + tail
}

// fitEnd shortens a plain string to a width by cutting its start, so that its end stays in view,
// as a line being typed does.
//
// Parameters:
//   - s: plain text, which must not contain escape sequences
//   - w: available cells; zero or less gives an empty string
//   - lead: marker put in front of a cut, usually an ellipsis
//
// Returns s when it fits, else the lead followed by the longest end of s that leaves room for it.
// The result never exceeds w cells, even when the lead alone is wider than w.
func (m metric) fitEnd(s string, w int, lead string) string {
	if w <= 0 {
		return ""
	}

	total := m.width(s)
	if total <= w {
		return s
	}

	lw := m.width(lead)
	if lw >= w {
		return m.cut(lead, w)
	}

	return lead + m.slice(s, total-(w-lw), total)
}

// pad shortens or extends a plain string to exactly a width.
//
// Parameters:
//   - s: plain text, which must not contain escape sequences
//   - w: target cells
//   - tail: marker used when s is cut
//
// Returns s fitted to w and followed by spaces up to w cells.
func (m metric) pad(s string, w int, tail string) string {
	s = m.fit(s, w, tail)

	return s + strings.Repeat(" ", max(0, w-m.width(s)))
}

// slice returns the cells of a styled line from one column to another, escape sequences kept.
//
// Parameters:
//   - s: styled text
//   - from: first column kept
//   - to: first column dropped; the result is empty when it is not after from
//
// It stands in for ansi.Method.Cut, which keeps the wrong end of the line under the wide
// character rule.
func (m metric) slice(s string, from, to int) string {
	if to <= from {
		return ""
	}

	return m.method.TruncateLeft(m.method.Truncate(s, to, ""), from, "")
}

// spaces returns a run of spaces.
//
// Parameters:
//   - n: number of spaces; zero or less gives an empty string
func spaces(n int) string {
	if n <= 0 {
		return ""
	}

	return strings.Repeat(" ", n)
}
