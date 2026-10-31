package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// hostile is a list of strings that try to act on the terminal or to confuse the width.
var hostile = []string{
	"plain",
	"",
	"a\x1b[31mred\x1b[0m",
	"title\x1b]0;owned\x07end",
	"\x1b[2J\x1b[H",
	"bell\x07 nul\x00 tab\t cr\r lf\n",
	"\x9b31m c1 \x9d0;x\x9c",
	"\xff\xfe bad utf8 \xc3",
	"rlo \u202E evil \u2066 iso \u2069",
	"zero\u200Bwidth\u2060joiner\uFEFFbom",
	"tag \U000E0041\U000E007F",
	"line\u2028sep\u2029par",
	"\U0001F680 rocket",
	"heart \u2764\uFE0F",
	"family \U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466",
	"tech \U0001F468\u200D\U0001F4BB",
	"skin \U0001F44D\U0001F3FD",
	"flag \U0001F1EB\U0001F1F7",
	"keycap 1\uFE0F\u20E3",
	"cjk \u65E5\u672C\u8A9E \uFF21\uFF22",
	"e\u0301 combining",
	"\u200D\u200C stray joiners \u200D",
	"\uFE0F\uFE0F lone selectors",
	"\U0001F600\U0001F600\U0001F600",
}

// TestSanitize checks exact results for the cases whose outcome is certain.
//
// Parameters:
//   - t: test to report to
func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"ascii", "prod-1.example.com", "prod-1.example.com"},
		{"empty", "", ""},
		{"escape", "a\x1b[31mb", "a?[31mb"},
		{"osc", "x\x1b]0;t\x07y", "x?]0;t?y"},
		{"nul", "a\x00b", "a?b"},
		{"tab", "a\tb", "a?b"},
		{"newline", "a\nb", "a?b"},
		{"c1", "a\u009bb", "a?b"},
		{"bad byte", "a\xffb", "a?b"},
		{"truncated", "a\xc3", "a?"},
		{"bidi", "a\u202Eb", "a?b"},
		{"zero width space", "a\u200Bb", "a?b"},
		{"separator", "a\u2028b", "a?b"},
		{"tag", "a\U000E0041b", "a?b"},
		{"rocket", "\U0001F680", "\U0001F680"},
		{"cjk", "\u65E5\u672C", "\u65E5\u672C"},
		{"combining", "e\u0301", "e\u0301"},
		{"selector stripped", "\u2764\uFE0F", "\u2764"},
		{"joiner stripped", "\U0001F468\u200D\U0001F4BB", "\U0001F468\U0001F4BB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitize(tt.in); got != tt.want {
				t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSanitizeInvariants checks, for hostile and unusual text, that the result is valid UTF-8,
// holds no control character and has the same width under both width rules.
//
// Parameters:
//   - t: test to report to
func TestSanitizeInvariants(t *testing.T) {
	for _, in := range hostile {
		got := sanitize(in)

		if !utf8.ValidString(got) {
			t.Errorf("sanitize(%q) = %q: invalid UTF-8", in, got)
		}

		for _, r := range got {
			if unsafeRune(r) {
				t.Errorf("sanitize(%q) = %q: keeps %U", in, got, r)
			}
		}

		if wc, gw := ansi.StringWidthWc(got), ansi.StringWidth(got); wc != gw {
			t.Errorf("sanitize(%q) = %q: widths differ, wc %d grapheme %d", in, got, wc, gw)
		}

		if again := sanitize(got); again != got {
			t.Errorf("sanitize is not idempotent on %q: %q then %q", in, got, again)
		}
	}
}

// TestUnsafeRune checks the classes of runes that are replaced.
//
// Parameters:
//   - t: test to report to
func TestUnsafeRune(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{'a', false},
		{' ', false},
		{0x1f, true},
		{0x7f, true},
		{0x85, true},
		{0x200b, true},
		{0x202e, true},
		{0x2028, true},
		{0x2029, true},
		{0xfeff, true},
		{0x200d, false},
		{0x200c, false},
		{0xe0041, true},
		{0x65e5, false},
		{0x1f680, false},
	}

	for _, tt := range tests {
		if got := unsafeRune(tt.r); got != tt.want {
			t.Errorf("unsafeRune(%U) = %v, want %v", tt.r, got, tt.want)
		}
	}
}

// TestEmojiModifier checks which runes only change how an emoji is drawn.
//
// Parameters:
//   - t: test to report to
func TestEmojiModifier(t *testing.T) {
	for _, r := range []rune{0xfe0f, 0xfe0e, 0x200d, 0x1f3fb, 0x1f3ff} {
		if !emojiModifier(r) {
			t.Errorf("emojiModifier(%U) = false", r)
		}
	}

	for _, r := range []rune{'a', 0x1f680, 0x1f3fa, 0x1f400} {
		if emojiModifier(r) {
			t.Errorf("emojiModifier(%U) = true", r)
		}
	}
}

// TestIsPlain checks the fast path of the grapheme analysis.
//
// Parameters:
//   - t: test to report to
func TestIsPlain(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{{"abc 123", true}, {"", true}, {"a\x7f", false}, {"a\x1f", false}, {"\u00E9", false}}

	for _, tt := range tests {
		if got := isPlain(tt.in); got != tt.want {
			t.Errorf("isPlain(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestMetricFit checks that cut, fit and pad never exceed the width, under both rules, for
// every width from 0 to 12 and for text with wide characters.
//
// Parameters:
//   - t: test to report to
func TestMetricFit(t *testing.T) {
	metrics := map[string]metric{
		"wc":       newMetric(),
		"grapheme": {method: ansi.GraphemeWidth},
	}

	texts := []string{
		"", "short", "a longer line of text",
		"\u65E5\u672C\u8A9E\u306E\u30C6\u30AD\u30B9\u30C8",
		"\U0001F680\U0001F680\U0001F680\U0001F680", "e\u0301e\u0301e\u0301e\u0301e\u0301",
	}

	for name, m := range metrics {
		for _, s := range texts {
			for w := -1; w <= 12; w++ {
				if got := m.width(m.cut(s, w)); got > max(0, w) {
					t.Errorf("%s: cut(%q, %d) is %d cells", name, s, w, got)
				}

				if got := m.width(m.fit(s, w, "\u2026")); got > max(0, w) {
					t.Errorf("%s: fit(%q, %d) is %d cells", name, s, w, got)
				}

				if got := m.width(m.fit(s, w, "...")); got > max(0, w) {
					t.Errorf("%s: fit(%q, %d, dots) is %d cells", name, s, w, got)
				}

				if w >= 0 && m.width(m.pad(s, w, "\u2026")) != w {
					t.Errorf("%s: pad(%q, %d) is not %d cells", name, s, w, w)
				}
			}
		}
	}
}

// TestMetricFitResults checks a few exact results.
//
// Parameters:
//   - t: test to report to
func TestMetricFitResults(t *testing.T) {
	m := newMetric()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"fits", m.fit("abc", 5, "\u2026"), "abc"},
		{"cut", m.fit("abcdef", 4, "\u2026"), "abc\u2026"},
		{"wide dropped", m.fit("\u65E5\u672C\u8A9E", 4, "\u2026"), "\u65E5\u2026"},
		{"tail too wide", m.fit("abcdef", 2, "..."), ".."},
		{"zero", m.fit("abc", 0, "."), ""},
		{"pad", m.pad("ab", 4, "."), "ab  "},
		{"pad cut", m.pad("abcdef", 4, "."), "abc."},
		{"cut wide", m.cut("\u65E5\u672C", 3), "\u65E5"},
		{"spaces", spaces(3), "   "},
		{"spaces none", spaces(-2), ""},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

// TestMetricWidthIgnoresStyles checks that escape sequences count for no cell.
//
// Parameters:
//   - t: test to report to
func TestMetricWidthIgnoresStyles(t *testing.T) {
	m := newMetric()

	if got := m.width("\x1b[1;31mab\x1b[0m"); got != 2 {
		t.Errorf("width of a styled text = %d, want 2", got)
	}

	if strings.Contains(m.fit("abcdefgh", 4, "\u2026"), "\x1b") {
		t.Error("fit introduced an escape")
	}
}
