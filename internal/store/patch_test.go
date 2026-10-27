package store

import (
	"errors"
	"strings"
	"testing"
)

// TestPatchBlock checks how Replace edits the lines of a block.
//
// Parameters:
//   - t: test to report to
func TestPatchBlock(t *testing.T) {
	cases := []struct {
		name string
		in   string
		rec  testRec
		want string
	}{
		{
			"inline comment kept on a changed value",
			"[[rec]]\nname = \"a\"\ndesc = \"x\"   # why\n",
			testRec{Name: "a", Desc: "y"},
			"[[rec]]\nname = \"a\"\ndesc = \"y\"   # why\n",
		},
		{
			"unchanged key keeps its spacing",
			"[[rec]]\nname    = \"a\"   # id\ndesc    = \"x\"\n",
			testRec{Name: "a", Desc: "x"},
			"[[rec]]\nname    = \"a\"   # id\ndesc    = \"x\"\n",
		},
		{
			"comments and blank lines between keys",
			"[[rec]]\nname = \"a\"\n\n# about desc\ndesc = \"x\"\n",
			testRec{Name: "a", Desc: "y"},
			"[[rec]]\nname = \"a\"\n\n# about desc\ndesc = \"y\"\n",
		},
		{
			"emptied key is removed",
			"[[rec]]\nname = \"a\"\ndesc = \"x\" # c\ntags = [\"p\"]\n",
			testRec{Name: "a", Tags: []string{"p"}},
			"[[rec]]\nname = \"a\"\ntags = [\"p\"]\n",
		},
		{
			"new key goes last",
			"[[rec]]\nname = \"a\"\n",
			testRec{Name: "a", Desc: "x"},
			"[[rec]]\nname = \"a\"\ndesc = \"x\"\n",
		},
		{
			"unknown key kept",
			"[[rec]]\nname = \"a\"\nnotes = \"mine\"\n# tail\nother.sub = 1\n",
			testRec{Name: "a", Desc: "x"},
			"[[rec]]\nname = \"a\"\nnotes = \"mine\"\n# tail\nother.sub = 1\ndesc = \"x\"\n",
		},
		{
			"multi-line value replaced without comment",
			"[[rec]]\nname = \"a\"\ndesc = \"\"\"\nx\n\"\"\"\n",
			testRec{Name: "a", Desc: "y"},
			"[[rec]]\nname = \"a\"\ndesc = \"y\"\n",
		},
		{
			"header comment kept",
			"[[rec]]   # my note\nname = \"a\"\n",
			testRec{Name: "b"},
			"[[rec]]   # my note\nname = \"b\"\n",
		},
		{
			"no final newline",
			"[[rec]]\nname = \"a\"",
			testRec{Name: "a", Desc: "x"},
			"[[rec]]\nname = \"a\"\ndesc = \"x\"",
		},
		{
			"crlf",
			"[[rec]]\r\nname = \"a\"   # c\r\n",
			testRec{Name: "b"},
			"[[rec]]\r\nname = \"b\"   # c\r\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkPatch(t, tc.in, tc.rec, tc.want) })
	}
}

// checkPatch replaces record a of a file and compares the result.
//
// Parameters:
//   - t: test to report to
//   - in: file content, with a record named a
//   - rec: replacement of record a
//   - want: expected file content
func checkPatch(t *testing.T, in string, rec testRec, want string) {
	p := rvWrite(t, in)
	c, err := Open[testRec](p, testTable)
	if err != nil || c.Err() != nil {
		t.Fatalf("open: %v %v", err, c.Err())
	}
	if err := c.Replace("a", rec); err != nil {
		t.Fatal(err)
	}
	if got := rvRead(t, p); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// TestScanSectionCommentNotOwned checks that a section title above a header stays in place.
//
// Parameters:
//   - t: test to report to
func TestScanSectionCommentNotOwned(t *testing.T) {
	in := "# --- a ---\n# own\n[[rec]]\nname = \"a\"\n"
	res := scanBlocks([]byte(in), testTable)
	if len(res.Blocks) != 1 || in[res.Blocks[0].Start:res.Blocks[0].Header] != "# own\n" {
		t.Fatalf("blocks = %+v", res.Blocks)
	}

	for text, want := range map[string]bool{
		"# --- clients ---": true, "#---x---": true, "# - x -": false, "# --- x": false,
		"# note": false,
	} {
		if got := isSectionComment([]byte(text)); got != want {
			t.Errorf("isSectionComment(%q) = %v, want %v", text, got, want)
		}
	}
}

// TestSchemaKeyInsideBlock checks that a schema key written after a header is refused.
//
// Parameters:
//   - t: test to report to
func TestSchemaKeyInsideBlock(t *testing.T) {
	p := rvWrite(t, "[[rec]]\nname = \"a\"\nschema = 2\n")
	c, _ := Open[testRec](p, testTable)
	var pe *ParseError
	if !errors.As(c.Err(), &pe) || pe.Line != 3 {
		t.Fatalf("Err = %v, want a parse error on line 3", c.Err())
	}
	if err := c.Add(testRec{Name: "b"}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Add = %v, want ErrReadOnly", err)
	}
}

// TestHeaderCaseMessage checks that a header with the wrong letter case is explained.
//
// Parameters:
//   - t: test to report to
func TestHeaderCaseMessage(t *testing.T) {
	p := rvWrite(t, "schema = 1\n\n[[Rec]]\nname = \"a\"\n")
	c, _ := Open[testRec](p, testTable)
	var pe *ParseError
	if !errors.As(c.Err(), &pe) || pe.Line != 3 || !strings.Contains(pe.Msg, "case sensitive") {
		t.Fatalf("Err = %v", c.Err())
	}
}

// TestNestingTooDeep checks that a small file of nested brackets is refused before parsing.
//
// Parameters:
//   - t: test to report to
func TestNestingTooDeep(t *testing.T) {
	n := 200000
	p := rvWrite(t, "x = "+strings.Repeat("[", n)+strings.Repeat("]", n)+"\n")
	c, err := OpenConnections(p)
	if err != nil {
		t.Fatal(err)
	}
	var pe *ParseError
	if !errors.As(c.Err(), &pe) || !strings.Contains(pe.Msg, "nested") {
		t.Fatalf("Err = %v", c.Err())
	}
}

// TestRecordsAreCopies checks that changing a returned record does not change the collection.
//
// Parameters:
//   - t: test to report to
func TestRecordsAreCopies(t *testing.T) {
	c, err := OpenConnections(rvWrite(t, "schema = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Add(Connection{Name: "a", Target: "h", Aliases: []string{"x", "y"}}); err != nil {
		t.Fatal(err)
	}

	got, _ := c.Get("a")
	got.Aliases[0] = "zzz"
	c.List()[0].Aliases[1] = "qqq"

	again, _ := c.Get("a")
	if again.Aliases[0] != "x" || again.Aliases[1] != "y" {
		t.Errorf("collection changed through a returned record: %v", again.Aliases)
	}
}

// TestDescKeepsJoiners checks that the joiners of emoji sequences pass ValidDesc.
//
// Parameters:
//   - t: test to report to
func TestDescKeepsJoiners(t *testing.T) {
	if err := ValidDesc("family \U0001F468\u200D\U0001F469 and \u0645\u200C\u06CC"); err != nil {
		t.Errorf("ValidDesc = %v", err)
	}
}

// FuzzReplace checks that Replace either fails and leaves the file alone, or changes only the
// block of the record.
//
// Parameters:
//   - f: fuzzing driver
func FuzzReplace(f *testing.F) {
	f.Add([]byte("schema = 1\n\n[[rec]] # x\nname = \"a\" # y\ndesc = \"d\"\nz = [\n1,\n]\n"), "n")
	f.Add([]byte("[[rec]]\r\nname = \"a\"\r\n\r\n# c\r\n[[rec]]\r\nname = \"b\"\r\n"), "")
	f.Add([]byte("[[rec]]\nname = \"a\"\ndesc = \"\"\"\n[[rec]]\n\"\"\"\ntags = ['''x''']"), "q")

	f.Fuzz(func(t *testing.T, data []byte, desc string) {
		fuzzReplace(t, data, desc)
	})
}

// fuzzReplace is the body of FuzzReplace for one input.
//
// Parameters:
//   - t: test to report to
//   - data: file content
//   - desc: new description of the first record
func fuzzReplace(t *testing.T, data []byte, desc string) {
	p := rvWrite(t, string(data))
	c, err := Open[testRec](p, testTable)
	if err != nil || c.Err() != nil || len(c.List()) == 0 {
		return
	}

	first := c.List()[0]
	sp := c.snap.Spans[0]
	next := first
	next.Desc = desc
	if err := c.Replace(first.Name, next); err != nil {
		if got := rvRead(t, p); got != string(data) {
			t.Fatalf("failed Replace changed the file: %v", err)
		}
		return
	}

	got := rvRead(t, p)
	if !strings.HasPrefix(got, string(data[:sp.Header])) {
		t.Fatalf("bytes before the block changed:\n%q\n%q", data, got)
	}
	if !strings.HasSuffix(got, string(data[sp.End:])) {
		t.Fatalf("bytes after the block changed:\n%q\n%q", data, got)
	}
	again, err := Open[testRec](p, testTable)
	if err != nil || again.Err() != nil {
		t.Fatalf("result does not parse: %v %v", err, again.Err())
	}
	if rec, ok := again.Get(first.Name); !ok || rec.Desc != desc {
		t.Fatalf("record after Replace = %+v, %v", rec, ok)
	}
}
