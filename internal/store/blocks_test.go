package store

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// scanCase is one input of TestScanBlocks.
//
// Want lists the exact text of each expected block, comment lines above the header included.
// Lines lists the expected 1-based header lines.
type scanCase struct {
	name  string
	in    string
	want  []string
	lines []int
}

// scanCases returns the inputs of TestScanBlocks.
func scanCases() []scanCase {
	return []scanCase{
		{name: "empty"},
		{name: "only comments", in: "# a\n\n# b\n"},
		{
			name:  "no trailing newline",
			in:    "[[rec]]\nname = \"a\"",
			want:  []string{"[[rec]]\nname = \"a\""},
			lines: []int{1},
		},
		{
			name: "header only",
			in:   "schema = 1\n[[rec]]\n\n[[rec]]\nname = \"b\"\n",
			want: []string{
				"[[rec]]\n",
				"[[rec]]\nname = \"b\"\n",
			},
			lines: []int{2, 4},
		},
		{
			name: "comments above belong, separated ones do not",
			in: "# file header\n\nschema = 1\n\n# --- clients ---\n\n# about a\n# more\n" +
				"[[rec]]\nname = \"a\"\n\n# section\n\n[[rec]]\nname = \"b\"\n",
			want: []string{
				"# about a\n# more\n[[rec]]\nname = \"a\"\n",
				"[[rec]]\nname = \"b\"\n",
			},
			lines: []int{9, 14},
		},
		{
			name: "comment touching the next header leaves the previous block",
			in:   "[[rec]]\nname = \"a\"\n# for b\n[[rec]]\nname = \"b\"\n",
			want: []string{
				"[[rec]]\nname = \"a\"\n",
				"# for b\n[[rec]]\nname = \"b\"\n",
			},
			lines: []int{1, 4},
		},
		{
			name: "comment inside a block stays in it",
			in:   "[[rec]]\nname = \"a\"\n# inside\ndesc = \"x\"\n\n\n",
			want: []string{
				"[[rec]]\nname = \"a\"\n# inside\ndesc = \"x\"\n",
			},
			lines: []int{1},
		},
		{
			name: "multi-line basic string hides headers",
			in: "[[rec]]\nname = \"a\"\ndesc = \"\"\"\n[[rec]]\nname = \"fake\"\n\"\"\"\n" +
				"[[rec]]\nname = \"b\"\n",
			want: []string{
				"[[rec]]\nname = \"a\"\ndesc = \"\"\"\n[[rec]]\nname = \"fake\"\n\"\"\"\n",
				"[[rec]]\nname = \"b\"\n",
			},
			lines: []int{1, 7},
		},
		{
			name: "multi-line literal string hides headers",
			in:   "[[rec]]\ndesc = '''\n[[rec]]\n# x\n\n'''\nname = \"a\"\n",
			want: []string{
				"[[rec]]\ndesc = '''\n[[rec]]\n# x\n\n'''\nname = \"a\"\n",
			},
			lines: []int{1},
		},
		{
			name: "escaped quotes do not close a multi-line string",
			in:   "[[rec]]\ndesc = \"\"\"\nsay \\\"\"\" [[rec]]\n[[rec]]\n\"\"\"\n",
			want: []string{
				"[[rec]]\ndesc = \"\"\"\nsay \\\"\"\" [[rec]]\n[[rec]]\n\"\"\"\n",
			},
			lines: []int{1},
		},
		{
			name: "closing delimiter with extra quotes",
			in:   "[[rec]]\ndesc = \"\"\"a\"\"\"\"\n[[rec]]\nname = \"b\"\n",
			want: []string{
				"[[rec]]\ndesc = \"\"\"a\"\"\"\"\n",
				"[[rec]]\nname = \"b\"\n",
			},
			lines: []int{1, 3},
		},
		{
			name: "multi-line array hides headers and comments",
			in:   "[[rec]]\ntags = [\n  \"a\",\n[[rec]]\n# note\n\n  \"b\",\n]\n[[rec]]\nname = \"b\"\n",
			want: []string{
				"[[rec]]\ntags = [\n  \"a\",\n[[rec]]\n# note\n\n  \"b\",\n]\n",
				"[[rec]]\nname = \"b\"\n",
			},
			lines: []int{1, 9},
		},
		{
			name: "brackets in strings and comments do not count",
			in:   "[[rec]]\ndesc = \"[ ] [[\" # ] ]\ndesc2 = '[[' # \"\"\"\n[[rec]]\n",
			want: []string{
				"[[rec]]\ndesc = \"[ ] [[\" # ] ]\ndesc2 = '[[' # \"\"\"\n",
				"[[rec]]\n",
			},
			lines: []int{1, 4},
		},
		{
			name: "multi-line string closed and reopened on a line",
			in:   "[[rec]]\na = \"\"\"x\"\"\" ; b = '''\n[[rec]]\n'''\n[[rec]]\n",
			want: []string{
				"[[rec]]\na = \"\"\"x\"\"\" ; b = '''\n[[rec]]\n'''\n",
				"[[rec]]\n",
			},
			lines: []int{1, 5},
		},
		{
			name: "trailing comment indentation and spaces",
			in:   "  [[rec]]   # first\nname = \"a\"\n\t[[rec]]\t\nname = \"b\"\n",
			want: []string{
				"  [[rec]]   # first\nname = \"a\"\n",
				"\t[[rec]]\t\nname = \"b\"\n",
			},
			lines: []int{1, 3},
		},
		{
			name: "other tables end the block",
			in: "[[rec]]\nname = \"a\"\n[sub]\nk = 1\n[[other]]\nk = 2\n[[rec]]\n" +
				"name = \"b\"\n[tail]\nz = 1\n",
			want: []string{
				"[[rec]]\nname = \"a\"\n",
				"[[rec]]\nname = \"b\"\n",
			},
			lines: []int{1, 7},
		},
		{
			name: "windows line endings",
			in:   "schema = 1\r\n\r\n# c\r\n[[rec]]\r\nname = \"a\"\r\n\r\n[[rec]]\r\nname = \"b\"\r\n",
			want: []string{
				"# c\r\n[[rec]]\r\nname = \"a\"\r\n",
				"[[rec]]\r\nname = \"b\"\r\n",
			},
			lines: []int{4, 7},
		},
		{
			name:  "byte order mark before the first header",
			in:    "\xef\xbb\xbf[[rec]]\nname = \"a\"\n",
			want:  []string{"\xef\xbb\xbf[[rec]]\nname = \"a\"\n"},
			lines: []int{1},
		},
		{
			name:  "unterminated string swallows the rest",
			in:    "[[rec]]\ndesc = \"\"\"\n[[rec]]\n",
			want:  []string{"[[rec]]\ndesc = \"\"\"\n[[rec]]\n"},
			lines: []int{1},
		},
		{
			name:  "unterminated single-line string stops at the line end",
			in:    "[[rec]]\ndesc = \"abc\n[[rec]]\n",
			want:  []string{"[[rec]]\ndesc = \"abc\n", "[[rec]]\n"},
			lines: []int{1, 3},
		},
	}
}

// TestScanBlocks checks the spans found in a range of inputs.
//
// Parameters:
//   - t: test to report to
func TestScanBlocks(t *testing.T) {
	for _, tc := range scanCases() {
		t.Run(tc.name, func(t *testing.T) { checkScanCase(t, tc) })
	}
}

// checkScanCase scans the input of one case and compares the blocks found with the expected ones.
//
// Parameters:
//   - t: test to report to
//   - tc: case to run
func checkScanCase(t *testing.T, tc scanCase) {
	t.Helper()

	res := scanBlocks([]byte(tc.in), testTable)

	var got []string
	var lines []int
	for _, sp := range res.Blocks {
		got = append(got, tc.in[sp.Start:sp.End])
		lines = append(lines, sp.Line)
	}
	if !reflect.DeepEqual(got, tc.want) {
		t.Errorf("blocks = %q, want %q", got, tc.want)
	}
	if !reflect.DeepEqual(lines, tc.lines) {
		t.Errorf("lines = %v, want %v", lines, tc.lines)
	}
	if len(res.Bad) != 0 {
		t.Errorf("bad headers = %v, want none", res.Bad)
	}
	checkSpans(t, []byte(tc.in), res)
}

// TestScanBlocksSpanFields checks every offset of a span on one small input.
//
// Parameters:
//   - t: test to report to
func TestScanBlocksSpanFields(t *testing.T) {
	in := "x = 1\n# c\n[[rec]]\nname = \"a\"\n\n"
	res := scanBlocks([]byte(in), testTable)
	want := []span{{Start: 6, Header: 10, End: 29, Line: 3}}
	if !reflect.DeepEqual(res.Blocks, want) {
		t.Errorf("blocks = %+v, want %+v", res.Blocks, want)
	}
}

// TestScanBadHeaders checks the headers reported as unsupported.
//
// Parameters:
//   - t: test to report to
func TestScanBadHeaders(t *testing.T) {
	cases := []struct {
		name string
		in   string
		line int
	}{
		{"spaces inside", "[[ rec ]]\nname = \"a\"\n", 1},
		{"space after first bracket", "x = 1\n[[ rec]]\n", 2},
		{"quoted", "[[\"rec\"]]\n", 1},
		{"single quoted with comment", "[['rec']] # c\n", 1},
		{"plain table", "[rec]\nname = \"a\"\n", 1},
		{"sub-table", "[[rec]]\nname = \"a\"\n[rec.sub]\nk = 1\n", 3},
		{"sub-array", "[[rec]]\nname = \"a\"\n[[rec.sub]]\nk = 1\n", 3},
		{"sub-table with spaces", "[[rec]]\n[ rec . sub ]\n", 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkBadHeader(t, tc.in, tc.line) })
	}

	res := scanBlocks([]byte("[recx]\n[[recs]]\n[other.rec]\n[[other]]\n"), testTable)
	if len(res.Bad) != 0 || len(res.Blocks) != 0 {
		t.Errorf("unrelated tables gave blocks %v and bad %v", res.Blocks, res.Bad)
	}
}

// checkBadHeader checks that the first unsupported header of an input is on the given line.
//
// Parameters:
//   - t: test to report to
//   - in: input to scan
//   - line: expected 1-based line of the first bad header
func checkBadHeader(t *testing.T, in string, line int) {
	t.Helper()

	res := scanBlocks([]byte(in), testTable)
	if len(res.Bad) == 0 || res.Bad[0].Line != line {
		t.Fatalf("bad = %+v, want first at line %d", res.Bad, line)
	}
	if res.Bad[0].Msg == "" || strings.HasSuffix(res.Bad[0].Msg, ".") {
		t.Errorf("message %q must be non-empty without a final period", res.Bad[0].Msg)
	}
}

// TestScanSchemaLine checks the detection of the top-level schema key.
//
// Parameters:
//   - t: test to report to
func TestScanSchemaLine(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"schema = 1\n", 1},
		{"# c\n\nschema=1\n[[rec]]\n", 3},
		{"  schema   = 1\n", 1},
		{"[[rec]]\nschema = 1\n", 0},
		{"schemas = 1\n", 0},
		{"desc = \"\"\"\nschema = 1\n\"\"\"\nschema = 2\n", 4},
	}

	for _, tc := range cases {
		if got := scanBlocks([]byte(tc.in), testTable).SchemaLine; got != tc.want {
			t.Errorf("SchemaLine(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// checkSpans verifies the invariants every scan result must satisfy.
//
// Parameters:
//   - t: test to report to
//   - data: scanned input
//   - res: result of scanning data
func checkSpans(t *testing.T, data []byte, res scanResult) {
	t.Helper()

	prevEnd := 0
	for i, sp := range res.Blocks {
		if sp.Start < 0 || sp.Start > sp.Header || sp.Header >= sp.End || sp.End > len(data) {
			t.Fatalf("span %d = %+v is outside 0 <= Start <= Header < End <= %d",
				i, sp, len(data))
		}
		if sp.Start < prevEnd {
			t.Fatalf("span %d = %+v overlaps the previous one ending at %d", i, sp, prevEnd)
		}
		prevEnd = sp.End

		if sp.Header != 0 && data[sp.Header-1] != '\n' {
			t.Fatalf("span %d header offset %d is not at a line start", i, sp.Header)
		}
		if want := bytes.Count(data[:sp.Header], []byte("\n")) + 1; sp.Line != want {
			t.Fatalf("span %d line = %d, want %d", i, sp.Line, want)
		}
		for _, line := range bytes.Split(bytes.TrimSuffix(data[sp.Start:sp.Header], []byte("\n")),
			[]byte("\n")) {
			if sp.Start != sp.Header && !bytes.HasPrefix(bytes.Trim(line, " \t\r"), []byte("#")) {
				t.Fatalf("span %d owns the non-comment line %q above its header", i, line)
			}
		}
	}
}

// FuzzScanBlocks checks that the scanner never panics and returns sane spans on any input.
//
// Parameters:
//   - f: fuzz target that receives the seed corpus and the fuzz function
func FuzzScanBlocks(f *testing.F) {
	for _, tc := range scanCases() {
		f.Add([]byte(tc.in))
	}
	f.Add([]byte("[[rec]]\n\"\"\"\n'''\n[\n{\n"))
	f.Add([]byte("\r\n\r[[rec]]\r\n\x00\xff[[rec]]"))
	f.Add([]byte("[[rec]]\nx = \"\\"))
	f.Add([]byte("\r#\n[[rec]]\n"))
	f.Add([]byte("\"\"\"\"\"\"\"\"\n[[rec]]\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		res := scanBlocks(data, testTable)
		checkSpans(t, data, res)
		for _, b := range res.Bad {
			if b.Line < 1 || b.Line > bytes.Count(data, []byte("\n"))+1 {
				t.Fatalf("bad header line %d is outside the input", b.Line)
			}
		}
	})
}

// scanPieces are the building blocks of the generated files of TestScanAgainstParser.
//
// Each piece that holds a %s is a record whose name is filled in; the others are noise.
var scanPieces = []string{
	"[[rec]]\nname = %q\n",
	"[[rec]]\nname = %q\ndesc = \"\"\"\n[[rec]]\nname = \"fake\"\n\"\"\"\n",
	"[[rec]]\nname = %q\ndesc = '''\n[[rec]]\n'''\n",
	"# about\n[[rec]]\nname = %q\ntags = [\n  \"a\",\n  \"[[rec]]\",\n]\n",
	"[[rec]] # note\nname = %q # inline\n",
	"  [[rec]]\n  name = %q\n",
	"[[rec]]\nname = %q\ndesc = \"say \\\"[[rec]]\\\" \\\\\"\n",
}

// scanNoise are the lines between records of the generated files of TestScanAgainstParser.
var scanNoise = []string{
	"\n",
	"# --- section ---\n",
	"\n# detached\n\n",
	"[other]\nk = 1\n",
	"[[other]]\nk = [\n  1,\n  2,\n]\n",
}

// TestScanAgainstParser checks the scanner against the TOML parser on generated files.
//
// Parameters:
//   - t: test to report to
func TestScanAgainstParser(t *testing.T) {
	rng := rand.New(rand.NewSource(7))

	for n := range 300 {
		var sb strings.Builder
		sb.WriteString("schema = 1\n")

		var names []string
		for i := range rng.Intn(8) {
			if rng.Intn(2) == 0 {
				sb.WriteString(scanNoise[rng.Intn(len(scanNoise))])
			}
			name := fmt.Sprintf("n%d-%d", n, i)
			names = append(names, name)
			fmt.Fprintf(&sb, scanPieces[rng.Intn(len(scanPieces))], name)
		}
		in := sb.String()
		if rng.Intn(2) == 0 {
			in = strings.ReplaceAll(in, "\n", "\r\n")
		}

		res := scanBlocks([]byte(in), testTable)
		if len(res.Blocks) != len(names) || len(res.Bad) != 0 {
			t.Fatalf("input %q: %d blocks and bad %v, want %d blocks", in, len(res.Blocks),
				res.Bad, len(names))
		}
		for i, sp := range res.Blocks {
			var got struct {
				Name string `toml:"name"`
			}
			body := blockBody([]byte(in), sp)
			if _, err := toml.Decode(string(body), &got); err != nil {
				t.Fatalf("input %q: block %d does not parse alone: %v", in, i, err)
			}
			if got.Name != names[i] {
				t.Fatalf("input %q: block %d is %q, want %q", in, i, got.Name, names[i])
			}
		}
	}
}

// TestDominantEOL checks the choice of the line ending of a file.
//
// Parameters:
//   - t: test to report to
func TestDominantEOL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "\n"},
		{"a", "\n"},
		{"a\nb\n", "\n"},
		{"a\r\nb\r\n", "\r\n"},
		{"a\r\nb\n", "\n"},
		{"a\r\nb\r\nc\n", "\r\n"},
	}

	for _, tc := range cases {
		if got := dominantEOL([]byte(tc.in)); got != tc.want {
			t.Errorf("dominantEOL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestEncodeBlock checks the text written for a record.
//
// Parameters:
//   - t: test to report to
func TestEncodeBlock(t *testing.T) {
	rec := testRec{Name: "a", Tags: []string{"x", "y"}}

	got, err := encodeBlock(testTable, rec, "\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[[rec]]\nname = \"a\"\ntags = [\"x\", \"y\"]\n"; string(got) != want {
		t.Errorf("block = %q, want %q", got, want)
	}

	got, err = encodeBlock(testTable, testRec{Name: "b"}, "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[[rec]]\r\nname = \"b\"\r\n"; string(got) != want {
		t.Errorf("CRLF block = %q, want %q", got, want)
	}

	if _, err := encodeBlock(testTable, make(chan int), "\n"); err == nil {
		t.Error("encoding a channel succeeded")
	}
}

// TestAddBlock checks where and how a block is appended.
//
// Parameters:
//   - t: test to report to
func TestAddBlock(t *testing.T) {
	block := []byte("[[rec]]\nname = \"n\"\n")
	cases := []struct {
		name string
		in   string
		eol  string
		want string
		sch  bool
	}{
		{"empty file", "", "\n", "schema = 1\n\n[[rec]]\nname = \"n\"\n", false},
		{"empty file crlf", "", "\r\n", "schema = 1\r\n\r\n[[rec]]\nname = \"n\"\n", false},
		{"after content", "schema = 1\n", "\n", "schema = 1\n\n[[rec]]\nname = \"n\"\n", false},
		{"no final newline", "schema = 1", "\n", "schema = 1\n\n[[rec]]\nname = \"n\"\n", false},
		{"already blank", "schema = 1\n\n", "\n", "schema = 1\n\n[[rec]]\nname = \"n\"\n", false},
		{"several blanks", "a = 1\n\n\n\n", "\n", "a = 1\n\n\n\n[[rec]]\nname = \"n\"\n", false},
		{"blank with spaces", "a = 1\n  \n", "\n", "a = 1\n  \n[[rec]]\nname = \"n\"\n", false},
		{"crlf", "a = 1\r\n", "\r\n", "a = 1\r\n\r\n[[rec]]\nname = \"n\"\n", false},
		{"crlf no final newline", "a = 1", "\r\n", "a = 1\r\n\r\n[[rec]]\nname = \"n\"\n", false},
		{"only a newline", "\n", "\n", "\nschema = 1\n\n[[rec]]\nname = \"n\"\n", true},
		{"only blanks", " \n\t\n", "\n", " \n\t\nschema = 1\n\n[[rec]]\nname = \"n\"\n", true},
		{"comment only", "# c\n", "\n", "# c\n\nschema = 1\n\n[[rec]]\nname = \"n\"\n", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := []byte(tc.in)
			if got := addBlock(in, block, tc.eol, tc.sch); string(got) != tc.want {
				t.Errorf("addBlock = %q, want %q", got, tc.want)
			}
			if string(in) != tc.in {
				t.Errorf("input was modified to %q", in)
			}
		})
	}
}

// TestReplaceAndRemoveBlock checks the byte surgery on a block.
//
// Parameters:
//   - t: test to report to
func TestReplaceAndRemoveBlock(t *testing.T) {
	in := "# head\n\n# own\n[[rec]]\nname = \"a\"\n\n# keep\n\n[[rec]]\nname = \"b\"\n"
	res := scanBlocks([]byte(in), testTable)
	if len(res.Blocks) != 2 {
		t.Fatalf("got %d blocks", len(res.Blocks))
	}
	repl := []byte("[[rec]]\nname = \"A\"\n")

	got := replaceBlock([]byte(in), res.Blocks[0], repl)
	want := "# head\n\n# own\n[[rec]]\nname = \"A\"\n\n# keep\n\n[[rec]]\nname = \"b\"\n"
	if string(got) != want {
		t.Errorf("replaceBlock = %q, want %q", got, want)
	}

	got = removeBlock([]byte(in), res.Blocks[0])
	want = "# head\n\n\n# keep\n\n[[rec]]\nname = \"b\"\n"
	if string(got) != want {
		t.Errorf("removeBlock = %q, want %q", got, want)
	}

	last := "[[rec]]\nname = \"z\""
	res = scanBlocks([]byte(last), testTable)
	got = replaceBlock([]byte(last), res.Blocks[0], []byte("[[rec]]\r\nname = \"y\"\r\n"))
	if want := "[[rec]]\r\nname = \"y\""; string(got) != want {
		t.Errorf("replaceBlock without final newline = %q, want %q", got, want)
	}
}
