package plugin

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// choiceValues lists the values of a parse.
//
// Parameters:
//   - l: parsed list
func choiceValues(l ChoiceList) []string {
	out := make([]string, 0, len(l.Items))
	for _, c := range l.Items {
		out = append(out, c.Value)
	}

	return out
}

// TestParseChoices checks the grammar and the drops of the parser, one hostile input at a time: a
// value is kept exactly as it was printed or dropped and counted, and nothing is rewritten.
//
// Parameters:
//   - t: test to report to
func TestParseChoices(t *testing.T) {
	long := strings.Repeat("a", ChoiceMaxValue)
	tests := []struct {
		name   string
		in     string
		rules  ChoiceRules
		want   []string
		hidden int
	}{
		{"empty output", "", ChoiceRules{}, nil, 0},
		{"one value", "v1\n", ChoiceRules{}, []string{"v1"}, 0},
		{"last line without a line feed", "v1\nv2", ChoiceRules{}, []string{"v1", "v2"}, 0},
		{"empty lines are ignored", "\n\nv1\n\n", ChoiceRules{}, []string{"v1"}, 0},
		{"value and label", "v1\tfirst\nv2\n", ChoiceRules{}, []string{"v1", "v2"}, 0},
		{"order is kept", "b\na\nc\n", ChoiceRules{}, []string{"b", "a", "c"}, 0},
		{"CR LF is dropped, not trimmed", "v1\r\nv2\r\n", ChoiceRules{}, nil, 2},
		{"ESC in a value", "v\x1b[31m1\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"bare ESC", "\x1b\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"CSI only", "\x1b[2J\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"C1 control", "a\u009bb\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"NUL", "a\x00b\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"bidi override", "a\u202eb\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"bidi isolate", "a\u2066b\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"braille blank", "v1\u2800\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"hangul filler", "v1\u3164\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"halfwidth hangul filler", "v1\uffa0x\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"hangul choseong filler", "v1\u115f\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"grapheme joiner", "v1\u034fx\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"variation selector", "v1\ufe0f\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"supplementary variation selector", "v1\U000e0100\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"khmer inherent vowel", "v1\u17b4\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"zero width space", "a\u200bb\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"zero width joiner", "ma\u200din\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"zero width non-joiner", "ma\u200cin\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"byte order mark", "a\ufeffb\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"tag characters", "a\U000E0041b\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"invalid UTF-8", "a\xffb\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"truncated UTF-8", "a\xe2\x82\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"space", "a b\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"only spaces", "   \nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"no break space", "a\u00a0b\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"ideographic space", "a\u3000b\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"empty value with a label", "\tlabel\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"a second TAB drops the entry", "v1\tone\ttwo\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"two TABs in a row", "v1\t\t\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"TAB then TAB-less empty label", "v1\t\nok\n", ChoiceRules{}, []string{"v1", "ok"}, 0},
		{"128 bytes", long + "\n", ChoiceRules{}, []string{long}, 0},
		{"129 bytes", long + "a\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{"huge line", strings.Repeat("x", 60<<10) + "\nok\n", ChoiceRules{}, []string{"ok"}, 1},
		{
			"max_len in runes", "\u00e9\u00e9\u00e9\n\u00e9\u00e9\u00e9\u00e9\u00e9\n",
			ChoiceRules{MaxLen: 4},
			[]string{"\u00e9\u00e9\u00e9"},
			1,
		},
		{"pattern", "v1\nV2\nv3\n", ChoiceRules{Pattern: "^[a-z][0-9]$"}, []string{"v1", "v3"}, 1},
		{
			"a tag like an option without a pattern", "--upgrade\nok\n",
			ChoiceRules{},
			[]string{"--upgrade", "ok"},
			0,
		},
		{
			"a tag like an option and a pattern that starts with an alphanumeric",
			"--upgrade\n-q\nok\n",
			ChoiceRules{Pattern: "^[A-Za-z0-9][A-Za-z0-9._-]*$"},
			[]string{"ok"},
			2,
		},
		{"a double dot without a pattern", "a..b\n", ChoiceRules{}, []string{"a..b"}, 0},
		{
			"a double dot and a pattern", "a..b\na.b\n",
			ChoiceRules{Pattern: `^[a-z0-9]+(\.[a-z0-9]+)*$`},
			[]string{"a.b"},
			1,
		},
		{
			"duplicates keep the first", "v1\tfirst\nv2\nv1\tsecond\n",
			ChoiceRules{},
			[]string{"v1", "v2"},
			0,
		},
		{"a duplicate does not count as hidden", "v1\nv1\nv1\n", ChoiceRules{}, []string{"v1"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseChoices([]byte(tt.in), tt.rules, nil)

			if v := choiceValues(got); !equalStrings(v, tt.want) {
				t.Errorf("values %q, want %q", v, tt.want)
			}

			if got.Hidden != tt.hidden {
				t.Errorf("hidden %d, want %d", got.Hidden, tt.hidden)
			}

			if got.More != 0 {
				t.Errorf("more %d", got.More)
			}

			for _, c := range got.Items {
				if !strings.Contains(tt.in, c.Value) || !utf8.ValidString(c.Value) {
					t.Errorf("value %q was rewritten", c.Value)
				}
			}
		})
	}
}

// equalStrings compares two lists, a nil one and an empty one being equal.
//
// Parameters:
//   - a: first list
//   - b: second list
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// TestParseChoicesFirstEntryOfADuplicate checks that the label of the first of two equal values
// is the one that stays.
//
// Parameters:
//   - t: test to report to
func TestParseChoicesFirstEntryOfADuplicate(t *testing.T) {
	got := ParseChoices([]byte("v1\tfirst\nv1\tsecond\n"), ChoiceRules{}, nil)

	if len(got.Items) != 1 || got.Items[0].Label != "first" {
		t.Errorf("items %+v", got.Items)
	}
}

// TestParseChoicesLimit checks the 200 entries that are kept, the valid ones after them that are
// only counted, and the entries that the rules dropped, which are counted apart.
//
// Parameters:
//   - t: test to report to
func TestParseChoicesLimit(t *testing.T) {
	var b strings.Builder

	for i := 0; i < 300; i++ {
		b.WriteString("v" + itoa(i) + "\n")
	}

	b.WriteString("bad value\n")
	b.WriteString("v0\n")

	got := ParseChoices([]byte(b.String()), ChoiceRules{}, nil)

	if len(got.Items) != ChoiceMaxEntries || got.More != 100 || got.Hidden != 1 {
		t.Fatalf("items %d more %d hidden %d", len(got.Items), got.More, got.Hidden)
	}

	if got.Items[0].Value != "v0" || got.Items[199].Value != "v199" {
		t.Errorf("first %q last %q", got.Items[0].Value, got.Items[199].Value)
	}

	exact := ParseChoices([]byte(strings.Repeat("v\n", 1)), ChoiceRules{}, nil)
	if len(exact.Items) != 1 || exact.More != 0 {
		t.Errorf("one entry: %+v", exact)
	}

	var c strings.Builder

	for i := 0; i < ChoiceMaxEntries+1; i++ {
		c.WriteString("w" + itoa(i) + "\n")
	}

	got = ParseChoices([]byte(c.String()), ChoiceRules{}, nil)
	if len(got.Items) != ChoiceMaxEntries || got.More != 1 {
		t.Errorf("201 entries: items %d more %d", len(got.Items), got.More)
	}
}

// TestParseChoicesLabels checks that a label is cleaned, cut at 60 runes and redacted, never
// rewrites its value, and that a hostile label does not drop a good value.
//
// Parameters:
//   - t: test to report to
func TestParseChoicesLabels(t *testing.T) {
	rd := NewRedactor([]string{"supersecretvalue"})
	long := strings.Repeat("\u00e9", 80)
	tests := []struct {
		name, in, want string
	}{
		{"plain", "v\tLatest release\n", "Latest release"},
		{"empty", "v\t\n", ""},
		{"escape sequence", "v\t\x1b[31mred\x1b[0m\n", "red"},
		{"other escape", "v\t\x1b]0;title\x07x\n", "?]0;title?x"},
		{"bidi", "v\ta\u202eb\n", "a?b"},
		{"zero width", "v\ta\u200bb\n", "a?b"},
		{"C1", "v\ta\u009bb\n", "a?b"},
		{"invalid UTF-8", "v\ta\xffb\n", "a\ufffdb"},
		{"cut at 60 runes", "v\t" + long + "\n", strings.Repeat("\u00e9", ChoiceMaxLabel)},
		{"secret", "v\tcontains supersecretvalue here\n", "contains " + Mask + " here"},
		{"secret split by an escape", "v\tsupersecret\x1b[0mvalue\n", Mask},
		{"label keeps the joiner", "v\ta\u200db\n", "a\u200db"},
		{"token shape", "v\tghp_abcdefghijklmnopqrstuvwxyz0123456789\n", Mask},
		{"spaces around", "v\t  padded  \n", "padded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseChoices([]byte(tt.in), ChoiceRules{}, rd)

			if len(got.Items) != 1 || got.Items[0].Value != "v" || got.Hidden != 0 {
				t.Fatalf("list %+v", got)
			}

			if got.Items[0].Label != tt.want {
				t.Errorf("label %q, want %q", got.Items[0].Label, tt.want)
			}
		})
	}
}

// TestParseChoicesNeverRedactsValues checks that the redactor touches labels only: a value that
// looks like a secret is listed as printed.
//
// Parameters:
//   - t: test to report to
func TestParseChoicesNeverRedactsValues(t *testing.T) {
	rd := NewRedactor([]string{"supersecretvalue"})
	got := ParseChoices([]byte("supersecretvalue\tsupersecretvalue\n"), ChoiceRules{}, rd)

	if len(got.Items) != 1 || got.Items[0].Value != "supersecretvalue" ||
		strings.Contains(got.Items[0].Label, "supersecretvalue") {
		t.Errorf("items %+v", got.Items)
	}
}

// TestParseChoicesBytes checks every possible byte in a value, one at a time: only the printable
// ASCII bytes other than the space are accepted, and a value is never changed.
//
// Parameters:
//   - t: test to report to
func TestParseChoicesBytes(t *testing.T) {
	for b := 0; b < 256; b++ {
		if b == '\n' || b == '\t' {
			continue
		}

		in := []byte{'a', byte(b), 'z', '\n'}
		got := ParseChoices(in, ChoiceRules{}, nil)
		ok := b > ' ' && b < 0x7f

		if ok != (len(got.Items) == 1) {
			t.Errorf("byte 0x%02x: kept=%v", b, len(got.Items) == 1)
		}

		if len(got.Items) == 1 && !bytes.Equal([]byte(got.Items[0].Value), in[:3]) {
			t.Errorf("byte 0x%02x: value %q", b, got.Items[0].Value)
		}
	}
}

// TestChoiceDetail checks which line explains a failure: the last non-empty line of the standard
// error, else the first non-empty line of the standard output.
//
// Parameters:
//   - t: test to report to
func TestChoiceDetail(t *testing.T) {
	tests := []struct {
		name, stderr, stdout, want string
	}{
		{"nothing", "", "", ""},
		{"stderr last line", "one\ntwo\n\n", "x\n", "two"},
		{"stderr wins", "bad\n", "Unknown option\n", "bad"},
		{"stdout first line", "", "\n  Unknown option: --plain\nusage: gft\n", "Unknown option: --plain"},
		{"spaces only", "  \n\t\n", "first\n", "first"},
		{"tab becomes a space", "a\tb\n", "", "a b"},
		{"long line is cut", strings.Repeat("x", 500), "", strings.Repeat("x", choiceDetailRunes)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := choiceDetail(tt.stderr, tt.stdout); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
