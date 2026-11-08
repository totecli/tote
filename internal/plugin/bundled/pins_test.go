package bundled

import (
	"strings"
	"testing"
)

// good builds a valid block for the tests.
//
// Parameters:
//   - id: plugin id
func good(id string) string {
	h := strings.Repeat("ab", 32)

	return "[[plugin]]\nid = \"" + id + "\"\nversion = \"1.2.3\"\ntree_sha256 = \"" + h +
		"\"\nsurface_sha256 = \"" + h + "\"\n"
}

// TestParsePins checks the reader of pins.toml: the shipped file, then a table of files that must
// be refused, each of which could otherwise weaken a pin without a visible failure.
//
// Parameters:
//   - t: test to report to
func TestParsePins(t *testing.T) {
	pins, err := Pins()
	if err != nil || len(pins) == 0 {
		t.Fatalf("shipped pins: %v %v", pins, err)
	}

	h := strings.Repeat("ab", 32)

	tests := []struct {
		name string
		text string
		want int
		fail string
	}{
		{"one block", "schema = 1\n" + good("gft"), 1, ""},
		{"comments and blanks", "# c\n\nschema = 1\n  # c\n" + good("gft") + "\n", 1, ""},
		{"two blocks", "schema = 1\n" + good("gft") + good("other"), 2, ""},
		{"no block", "schema = 1\n", 0, ""},
		{"empty file", "", 0, "no schema"},
		{"no schema", good("gft"), 0, "unexpected block"},
		{"schema 2", "schema = 2\n" + good("gft"), 0, "schema = 1"},
		{"schema twice", "schema = 1\nschema = 1\n" + good("gft"), 0, "schema = 1"},
		{"schema after a block", good("gft") + "schema = 1\n", 0, "unexpected block"},
		{"same id twice", "schema = 1\n" + good("gft") + good("gft"), 0, "twice"},
		{"unknown key", "schema = 1\n" + good("gft") + "extra = \"x\"\n", 0, "unknown key"},
		{"duplicate key", "schema = 1\n" + good("gft") + "id = \"gft\"\n", 0, "duplicate"},
		{
			"missing key", "schema = 1\n[[plugin]]\nid = \"gft\"\nversion = \"1.0.0\"\n", 0,
			"tree_sha256",
		},
		{"short hash", "schema = 1\n" + strings.Replace(good("gft"), h, "ab", 1), 0, "64"},
		{"upper case hash", "schema = 1\n" + strings.Replace(good("gft"), h,
			strings.ToUpper(h), 1), 0, "64"},
		{"long hash", "schema = 1\n" + strings.Replace(good("gft"), h, h+"a", 1), 0, "64"},
		{
			"bad version", "schema = 1\n" + strings.Replace(good("gft"), "1.2.3", "1.2", 1), 0,
			"semantic",
		},
		{"leading zero version", "schema = 1\n" + strings.Replace(good("gft"), "1.2.3", "01.2.3",
			1), 0, "semantic"},
		{"bad id", "schema = 1\n" + good("../gft"), 0, "plugin id"},
		{"upper case id", "schema = 1\n" + good("Gft"), 0, "plugin id"},
		{"unquoted string", "schema = 1\n[[plugin]]\nid = gft\n", 0, "id"},
		{"escape in a string", "schema = 1\n" + strings.Replace(good("gft"), "1.2.3",
			`1.2.\x33`, 1), 0, "plain string"},
		{"not a key", "schema = 1\nnonsense\n", 0, "key = value"},
		{"empty value", "schema = 1\n[[plugin]]\nid =\n", 0, "key = value"},
		{"table", "schema = 1\n[plugin]\n", 0, "key = value"},
		{"seventeen blocks", "schema = 1\n" + strings.Repeat(good("gft"), 17), 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePins(tt.text)

			switch {
			case tt.name == "seventeen blocks":
				if err == nil {
					t.Error("17 blocks accepted")
				}
			case tt.fail == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tt.fail != "" && (err == nil || !strings.Contains(err.Error(), tt.fail)):
				t.Errorf("error %v, want one that mentions %q", err, tt.fail)
			case err == nil && len(got) != tt.want:
				t.Errorf("%d pins, want %d", len(got), tt.want)
			}
		})
	}
}
