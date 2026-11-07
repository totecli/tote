package plugin

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// unsafeRune reports whether a rune could act on a terminal or hide text.
//
// Parameters:
//   - r: rune to test
//
// Returns true for control characters (C0, DEL and C1 including U+009B), line and paragraph
// separators, and format characters such as bidirectional controls, zero-width spaces and tag
// characters. The two joiners U+200C and U+200D are kept, as in the renderer: whether they stay
// is decided per grapheme at render time. The predicate is the one of the renderer's own cleaner
// on purpose, so that the two copies cannot drift.
func unsafeRune(r rune) bool {
	if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
		return true
	}

	return unicode.Is(unicode.Cf, r) && r != '\u200C' && r != '\u200D'
}

// CleanLine makes a single-line string from a plugin safe to store and to show.
//
// Parameters:
//   - s: text from a manifest, a catalog, an event, a hint or stderr, any bytes
//   - maxRunes: longest result in runes, cut on a rune boundary; 0 or less means no limit
//
// Returns s where every invalid UTF-8 byte becomes U+FFFD and every unsafe rune, including line
// feed and tab, becomes a question mark. The result never holds an escape sequence.
func CleanLine(s string, maxRunes int) string {
	return clean(s, maxRunes, false)
}

// CleanText makes a multi-line string from a plugin safe to store and to show.
//
// Parameters:
//   - s: text such as a result body or a detail, any bytes
//   - maxRunes: longest result in runes, cut on a rune boundary; 0 or less means no limit
//
// Returns s as CleanLine does, except that line feed and tab are kept and a carriage return that
// precedes a line feed is dropped. A lone carriage return becomes a question mark.
func CleanText(s string, maxRunes int) string {
	return clean(s, maxRunes, true)
}

// clean is the shared body of CleanLine and CleanText.
//
// Parameters:
//   - s: text to clean
//   - maxRunes: longest result in runes, 0 or less for no limit
//   - multi: true to keep line feed and tab
func clean(s string, maxRunes int, multi bool) string {
	if isPlainASCII(s, multi) && (maxRunes <= 0 || len(s) <= maxRunes) {
		return s
	}

	var b strings.Builder

	b.Grow(len(s))

	count := 0

	for i := 0; i < len(s); {
		if maxRunes > 0 && count >= maxRunes {
			break
		}

		r, size := utf8.DecodeRuneInString(s[i:])
		i += size

		switch {
		case r == utf8.RuneError && size == 1:
			r = '\uFFFD'
		case multi && r == '\r' && i < len(s) && s[i] == '\n':
			continue
		case multi && (r == '\n' || r == '\t'):
		case unsafeRune(r):
			r = '?'
		}

		b.WriteRune(r)

		count++
	}

	return b.String()
}

// isPlainASCII reports whether a string is printable ASCII, so that cleaning would not change it.
//
// Parameters:
//   - s: text to test
//   - multi: true when line feed and tab are acceptable
func isPlainASCII(s string, multi bool) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c < 0x7f {
			continue
		}

		if multi && (c == '\n' || c == '\t') {
			continue
		}

		return false
	}

	return true
}

// shownText reports whether a string of a manifest or of a catalog can be shown as it is: plain
// text without the characters that terminals draw with different widths, which are the
// noncharacters (U+FDD0 to U+FDEF and the last two code points of each plane) and the private use
// ones. A plugin that wants an icon uses a character that every terminal knows.
//
// Parameters:
//   - s: text to test
func shownText(s string) bool {
	if !plainText(s) {
		return false
	}

	for _, r := range s {
		if unicode.Is(unicode.Co, r) || (r >= 0xFDD0 && r <= 0xFDEF) || r&0xFFFE == 0xFFFE {
			return false
		}
	}

	return true
}

// plainText reports whether a string can be stored as a manifest value without cleaning: valid
// UTF-8, no control character, no line or paragraph separator and no hidden format character.
//
// Parameters:
//   - s: text to test
func plainText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}

	for _, r := range s {
		if unsafeRune(r) {
			return false
		}
	}

	return true
}
