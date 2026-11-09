package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestSanitize checks that dangerous characters are replaced and nothing else is touched.
//
// Parameters:
//   - t: test to report to
func TestSanitize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"caf\u00e9 \u4e2d\u6587", "caf\u00e9 \u4e2d\u6587"},
		{"\x1b[31mred\x1b[0m", "?[31mred?[0m"},
		{"a\x00b\x07c\x7fd", "a?b?c?d"},
		{"line\nbreak\r\ttab", "line?break??tab"},
		{"c1\u0085ctl\u009b", "c1?ctl?"},
		{"\u202eevil\u202c", "?evil?"},
		{"zero\u200bwidth\ufeff", "zero?width?"},
		{"sep\u2028and\u2029", "sep?and?"},
		{"bad\xffbyte", "bad?byte"},
		{"emoji\u200djoin", "emoji\u200djoin"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := sanitize(tt.in); got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestTextWidth checks the cell width of ASCII, accents, wide and combining characters.
//
// Parameters:
//   - t: test to report to
func TestTextWidth(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"caf\u00e9", 4},
		{"e\u0301", 1},
		{"\u4e2d\u6587", 4},
		{"a\u3042b", 4},
		{"\uff21", 2},
		{"\U0001f600", 2},
	}

	for _, tt := range tests {
		if got := textWidth(tt.in); got != tt.want {
			t.Errorf("textWidth(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestWriteTable checks the alignment by display width and the removal of trailing spaces.
//
// Parameters:
//   - t: test to report to
func TestWriteTable(t *testing.T) {
	var b bytes.Buffer

	writeTable(&b, []string{"NAME", "DESC"}, [][]string{
		{"caf\u00e9", "one"},
		{"\u4e2d\u6587", "two"},
		{"x", ""},
		{"e\u0301e", "four"},
	})

	want := "NAME  DESC\n" +
		"caf\u00e9  one\n" +
		"\u4e2d\u6587  two\n" +
		"x\n" +
		"e\u0301e    four\n"
	if b.String() != want {
		t.Errorf("table =\n%q\nwant\n%q", b.String(), want)
	}
}

// TestWriteTableHostile checks that no escape sequence reaches the output of a table.
//
// Parameters:
//   - t: test to report to
func TestWriteTableHostile(t *testing.T) {
	var b bytes.Buffer

	writeTable(&b, []string{"A", "B"}, [][]string{{"\x1b]0;pwned\x07", "\x1b[2J\r\n\u202e"}})

	for _, r := range b.String() {
		if r < 0x20 && r != '\n' {
			t.Errorf("control character %q in %q", r, b.String())
		}
	}

	if strings.Count(b.String(), "\n") != 2 {
		t.Errorf("a newline in a cell added a line: %q", b.String())
	}
}

// TestWriteTSV checks the escaping of tab and newline.
//
// Parameters:
//   - t: test to report to
func TestWriteTSV(t *testing.T) {
	var b bytes.Buffer

	writeTSV(&b, [][]string{{"a\tb", "c\nd", "e\r", "f\x1b[0m", ""}, {"x"}})

	want := "a\\tb\tc\\nd\te\\r\tf?[0m\t\nx\n"
	if b.String() != want {
		t.Errorf("tsv = %q, want %q", b.String(), want)
	}

	if strings.Count(b.String(), "\n") != 2 {
		t.Errorf("tsv has %d lines, want 2", strings.Count(b.String(), "\n"))
	}
}

// TestWriteJSON checks that the output is valid JSON and keeps HTML characters readable.
//
// Parameters:
//   - t: test to report to
func TestWriteJSON(t *testing.T) {
	var b bytes.Buffer

	in := []aliasView{{"a", "x < y && \"z\"\n", "\x1b"}}
	if err := writeJSON(&b, in); err != nil {
		t.Fatal(err)
	}

	var out []aliasView
	if err := json.Unmarshal(b.Bytes(), &out); err != nil || out[0] != in[0] {
		t.Errorf("round trip: %v %+v", err, out)
	}

	if !strings.Contains(b.String(), "x < y &&") || strings.Contains(b.String(), "\x1b") {
		t.Errorf("json = %q", b.String())
	}
}

// TestHostileDescription checks, end to end, a description hand-written with an escape sequence.
//
// Parameters:
//   - t: test to report to
func TestHostileDescription(t *testing.T) {
	h := newHarness(t)
	h.write("aliases.toml", "schema = 1\n\n[[alias]]\nname = \"ok\"\ncmd = \"ls\"\n"+
		"desc = \"\\u001b[31mred\\u001b]0;title\\u0007\"\n")

	for _, args := range [][]string{
		{"alias", "--format", "table"}, {"alias", "--format", "tsv"},
	} {
		if code := h.run(args...); code != 0 {
			t.Fatalf("%v: code %d, stderr %q", args, code, h.errb.String())
		}

		if strings.ContainsRune(h.out.String(), 0x1b) || strings.ContainsRune(h.out.String(), 7) ||
			!strings.Contains(h.out.String(), "?[31mred?]0;title?") {
			t.Errorf("%v: stdout = %q", args, h.out.String())
		}
	}

	h.run("alias", "--format", "json")

	if !json.Valid(h.out.Bytes()) || strings.ContainsRune(h.out.String(), 0x1b) {
		t.Errorf("json = %q", h.out.String())
	}
}
