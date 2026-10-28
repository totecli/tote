package i18n

import "strings"

// folder rewrites the letters, punctuation and spaces that the catalogs use into ASCII. The
// Latin-1 letters lose their accent, a ligature becomes two letters, the typographic quotes and
// guillemets become straight quotes, the no-break spaces become a plain space and the ellipsis
// becomes three dots. Every character of the table is written with an escape so that this file
// stays ASCII.
var folder = strings.NewReplacer(
	"\u00A0", " ", "\u202F", " ", "\u2009", " ", "\u2026", "...", "\u00D7", "x",
	"\u2018", "'", "\u2019", "'", "\u201C", "\"", "\u201D", "\"", "\u00AB", "\"", "\u00BB", "\"",
	"\u2013", "-", "\u2014", "-", "\u2212", "-",
	"\u00C0", "A", "\u00C1", "A", "\u00C2", "A", "\u00C3", "A", "\u00C4", "A", "\u00C5", "A",
	"\u00C6", "AE", "\u00C7", "C", "\u00C8", "E", "\u00C9", "E", "\u00CA", "E", "\u00CB", "E",
	"\u00CC", "I", "\u00CD", "I", "\u00CE", "I", "\u00CF", "I", "\u00D1", "N",
	"\u00D2", "O", "\u00D3", "O", "\u00D4", "O", "\u00D5", "O", "\u00D6", "O", "\u0152", "OE",
	"\u00D9", "U", "\u00DA", "U", "\u00DB", "U", "\u00DC", "U", "\u00DD", "Y",
	"\u00E0", "a", "\u00E1", "a", "\u00E2", "a", "\u00E3", "a", "\u00E4", "a", "\u00E5", "a",
	"\u00E6", "ae", "\u00E7", "c", "\u00E8", "e", "\u00E9", "e", "\u00EA", "e", "\u00EB", "e",
	"\u00EC", "i", "\u00ED", "i", "\u00EE", "i", "\u00EF", "i", "\u00F1", "n",
	"\u00F2", "o", "\u00F3", "o", "\u00F4", "o", "\u00F5", "o", "\u00F6", "o", "\u0153", "oe",
	"\u00F9", "u", "\u00FA", "u", "\u00FB", "u", "\u00FC", "u", "\u00FD", "y", "\u00FF", "y",
	"\u00B1", "+/-", "\u2248", "~",
)

// Fold rewrites a text into pure ASCII for a terminal that cannot show anything else.
//
// Parameters:
//   - s: text of a catalog, with its accents, guillemets and no-break spaces
//
// Returns the text with accents removed (e acute gives e), guillemets and typographic quotes
// straightened, no-break spaces turned into spaces and the ellipsis into three dots. A
// character the table does not know becomes a question mark, so the result is always ASCII and
// the layout does not shift. Printf directives and ASCII text pass through unchanged.
func Fold(s string) string {
	s = folder.Replace(s)

	if isASCII(s) {
		return s
	}

	var b strings.Builder

	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
		} else {
			b.WriteByte('?')
		}
	}

	return b.String()
}

// isASCII reports whether a string holds only ASCII characters.
//
// Parameters:
//   - s: text to test
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}

	return true
}

// ASCII returns a Printer that says the same messages folded to ASCII, for an interface that
// draws with plain ASCII glyphs: --ascii, TERM=linux or dumb, or a locale that is not UTF-8. Only
// the text of the catalog is folded: the values given to T as arguments, which may be names typed
// by the user, are inserted as they are.
func (p *Printer) ASCII() *Printer {
	own := make(map[string]string, len(p.own))
	for k, v := range p.own {
		own[k] = Fold(v)
	}

	if p.lang == langEnglish {
		return &Printer{lang: p.lang, own: own, base: own}
	}

	base := make(map[string]string, len(p.base))
	for k, v := range p.base {
		base[k] = Fold(v)
	}

	return &Printer{lang: p.lang, own: own, base: base}
}
