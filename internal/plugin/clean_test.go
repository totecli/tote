package plugin

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/totecli/tote/internal/store"
)

// hostile builds the cases of a hostile string from runes and bytes, so that this file stays
// plain ASCII.
func hostile() map[string]string {
	r := func(rs ...rune) string { return string(rs) }

	return map[string]string{
		"escape sequence":    "a\x1b[31mred\x1b[0m",
		"osc title":          "\x1b]0;owned\x07x",
		"bell and backspace": "a\x07b\x08c",
		"nul":                "a\x00b",
		"delete":             "a\x7fb",
		"c1 csi":             "a" + r(0x9b) + "31mb",
		"c1 range":           r(0x80, 0x85, 0x9f),
		"line feed":          "a\nb",
		"tab":                "a\tb",
		"carriage return":    "a\rb",
		"line separator":     "a" + r(0x2028) + "b",
		"paragraph sep":      "a" + r(0x2029) + "b",
		"bidi override":      "a" + r(0x202e) + "gpj.exe",
		"bidi isolate":       r(0x2066, 0x2067, 0x2068, 0x2069),
		"zero width space":   "a" + r(0x200b) + "b",
		"word joiner":        "a" + r(0x2060) + "b",
		"bom":                r(0xfeff) + "a",
		"soft hyphen":        "a" + r(0xad) + "b",
		"tag characters":     r(0xe0041, 0xe0042),
		"invalid utf8":       "a\xffb\xc3",
		"overlong":           "a\xc0\xafb",
		"surrogate bytes":    "a\xed\xa0\x80b",
		"truncated":          "a\xe2\x82",
	}
}

// TestCleanLine checks that no hostile string survives CleanLine with a character that acts on a
// terminal, and that safe text is left alone.
//
// Parameters:
//   - t: test to report to
func TestCleanLine(t *testing.T) {
	for name, in := range hostile() {
		t.Run(name, func(t *testing.T) {
			for _, got := range []string{CleanLine(in, 0), CleanText(in, 0)} {
				if !utf8.ValidString(got) {
					t.Fatalf("result is not valid UTF-8: %q", got)
				}

				for _, r := range got {
					if unsafeRune(r) && r != '\n' && r != '\t' {
						t.Fatalf("unsafe rune %U survived in %q", r, got)
					}
				}
			}

			if strings.ContainsAny(CleanLine(in, 0), "\n\t\r\x1b") {
				t.Errorf("CleanLine kept a control character: %q", CleanLine(in, 0))
			}
		})
	}

	exact := []struct {
		in   string
		max  int
		line string
		text string
	}{
		{"hello", 0, "hello", "hello"},
		{"a\nb", 0, "a?b", "a\nb"},
		{"a\tb", 0, "a?b", "a\tb"},
		{"a\r\nb", 0, "a??b", "a\nb"},
		{"a\rb", 0, "a?b", "a?b"},
		{"a\x1b[0m", 0, "a?[0m", "a?[0m"},
		{"a\xffb", 0, "a" + string(rune(0xfffd)) + "b", "a" + string(rune(0xfffd)) + "b"},
		{"abcdef", 3, "abc", "abc"},
		{"abc", 3, "abc", "abc"},
		{"", 5, "", ""},
		{"x" + string(rune(0xe9)) + "y", 2, "x" + string(rune(0xe9)), "x" + string(rune(0xe9))},
		{
			string(rune(0x200d)) + string(rune(0x200c)), 0,
			string(rune(0x200d)) + string(rune(0x200c)),
			string(rune(0x200d)) + string(rune(0x200c)),
		},
	}

	for _, tt := range exact {
		if got := CleanLine(tt.in, tt.max); got != tt.line {
			t.Errorf("CleanLine(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.line)
		}

		if got := CleanText(tt.in, tt.max); got != tt.text {
			t.Errorf("CleanText(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.text)
		}
	}

	if got := CleanLine(strings.Repeat("\xff", 10), 4); utf8.RuneCountInString(got) != 4 {
		t.Errorf("the limit counts runes after replacement: %q", got)
	}
}

// TestPlainText checks the predicate that decides whether a manifest value needs cleaning.
//
// Parameters:
//   - t: test to report to
func TestPlainText(t *testing.T) {
	for _, s := range []string{
		"", "abc", "caf" + string(rune(0xe9)), string(rune(0x1f600)),
		string(rune(0x200d)),
	} {
		if !plainText(s) {
			t.Errorf("plainText(%q) = false", s)
		}
	}

	for name, s := range hostile() {
		if plainText(s) && !strings.Contains(name, "tab") && name != "carriage return" {
			// Every hostile string holds at least one rune that is not plain.
			t.Errorf("plainText accepted the hostile string %q", name)
		}
	}
}

// TestShownText checks the predicate that decides whether a manifest or catalog text can be shown:
// everything plainText accepts, except the characters that terminals measure differently.
//
// Parameters:
//   - t: test to report to
func TestShownText(t *testing.T) {
	for _, s := range []string{
		"", "abc", "caf" + string(rune(0xe9)), string(rune(0x1f600)),
		string(rune(0x200d)), string(rune(0x4e2d)),
	} {
		if !shownText(s) {
			t.Errorf("shownText(%q) = false", s)
		}
	}

	for name, r := range map[string]rune{
		"noncharacter": 0xFFFF, "noncharacter low": 0xFFFE,
		"noncharacter range": 0xFDD0, "last of a plane": 0x10FFFF, "private use": 0xE000,
		"private use plane": 0xF0000,
	} {
		if shownText("X" + string(r) + "Y") {
			t.Errorf("shownText accepted the %s U+%04X", name, r)
		}

		if !plainText("X" + string(r) + "Y") {
			t.Errorf("plainText refused U+%04X, only shownText may", r)
		}
	}
}

// FuzzClean checks the cleaner invariants on any input: valid UTF-8, no unsafe rune, idempotence
// and the rune limit.
//
// Parameters:
//   - f: fuzz target
func FuzzClean(f *testing.F) {
	for _, s := range hostile() {
		f.Add(s, 8)
	}

	f.Fuzz(func(t *testing.T, s string, max int) {
		if max > 1000 {
			max = 1000
		}

		for _, got := range []string{CleanLine(s, max), CleanText(s, max)} {
			if !utf8.ValidString(got) {
				t.Fatalf("invalid UTF-8 from %q", s)
			}

			if max > 0 && utf8.RuneCountInString(got) > max {
				t.Fatalf("over the limit: %q", got)
			}
		}

		line := CleanLine(s, max)
		if CleanLine(line, max) != line || !plainText(line) {
			t.Fatalf("CleanLine is not idempotent or leaves unsafe runes: %q", line)
		}
	})
}

// TestPlainTextAgreesWithValidDesc checks that the text rules of a manifest are the rules the
// store applies to a description, so that a plugin string and a saved record never disagree about
// what is clean.
//
// Parameters:
//   - t: test to report to
func TestPlainTextAgreesWithValidDesc(t *testing.T) {
	corpus := []string{
		"", "plain", "caf\u00e9", "tab\there", "line\nfeed", "cr\rhere", "nul\x00byte", "esc\x1b[31m",
		"del\x7f", "c1\u009b", "ls\u2028", "ps\u2029", "bidi\u202e", "zwsp\u200b", "bom\ufeff",
		"tag\U000e0041", "shy\u00ad", "zwj\u200d", "zwnj\u200c", "family \U0001f468\u200d\U0001f469",
		"bad\xffutf8", "arabic\u061c", "wordjoiner\u2060", "nbsp\u00a0", "ideographic\u3000",
	}

	for _, s := range corpus {
		if got, want := plainText(s), store.ValidDesc(s) == nil; got != want {
			t.Errorf("plainText(%q) = %v, store.ValidDesc says %v", s, got, want)
		}
	}
}
