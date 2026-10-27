package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// rvWrite writes content to a new file in a temporary directory and returns its path.
//
// Parameters:
//   - t: test to report to
//   - content: file content
func rvWrite(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "d.toml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// rvRead returns the content of a file.
//
// Parameters:
//   - t: test to report to
//   - p: file path
func rvRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestReviewSampleFileKeepsComments checks that replacing a record of the design sample keeps
// the inline comments of the other fields.
//
// Parameters:
//   - t: test to report to
func TestReviewSampleFileKeepsComments(t *testing.T) {
	src := "# connections.toml (comments and order are kept)\nschema = 1\n\n" +
		"# --- clients ---\n[[connection]]\nname    = \"prod\"\n" +
		"via     = \"ssh\"            # ssh | a verb\ntarget  = \"10.0.0.5\"\n" +
		"user    = \"root\"           # empty = delegate to ssh_config\n" +
		"ip      = \"2001:db8::1\"    # informational\ndesc    = \"Serveur prod\"\n"
	p := rvWrite(t, src)
	c, err := OpenConnections(p)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := c.Get("prod")
	r.Desc = "changed"
	if err := c.Replace("prod", r); err != nil {
		t.Fatal(err)
	}
	got := rvRead(t, p)
	for _, want := range []string{"# ssh | a verb", "# empty = delegate", "# informational"} {
		if !strings.Contains(got, want) {
			t.Errorf("inline comment %q lost:\n%s", want, got)
		}
	}
}

// TestReviewUnknownKeyKept checks that a key unknown to the Go type survives a Replace.
//
// Parameters:
//   - t: test to report to
func TestReviewUnknownKeyKept(t *testing.T) {
	p := rvWrite(t, "schema = 1\n\n[[rec]]\nname = \"a\"\nnotes = \"hand written\"\n")
	c, _ := Open[testRec](p, "rec")
	if err := c.Replace("a", testRec{Name: "a", Desc: "x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rvRead(t, p), "hand written") {
		t.Errorf("unknown key lost:\n%s", rvRead(t, p))
	}
}

// TestReviewSectionCommentSurvivesDelete checks that deleting a record keeps a section comment
// that touches its header.
//
// Parameters:
//   - t: test to report to
func TestReviewSectionCommentSurvivesDelete(t *testing.T) {
	p := rvWrite(t, "schema = 1\n\n# --- clients ---\n[[rec]]\nname = \"a\"\n\n"+
		"[[rec]]\nname = \"b\"\n")
	c, _ := Open[testRec](p, "rec")
	if err := c.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rvRead(t, p), "--- clients ---") {
		t.Errorf("section comment lost:\n%s", rvRead(t, p))
	}
}

// TestReviewUnknownTableCase checks that a record block written with another letter case is
// not silently invisible.
//
// Parameters:
//   - t: test to report to
func TestReviewUnknownTableCase(t *testing.T) {
	p := rvWrite(t, "schema = 1\n\n[[Rec]]\nname = \"a\"\n")
	c, _ := Open[testRec](p, "rec")
	if len(c.List()) == 0 && c.Err() == nil {
		t.Errorf("block [[Rec]] silently ignored, no error")
	}
}

// TestReviewTopLevelKeyAfterBlock checks a schema key written after the first block.
//
// Parameters:
//   - t: test to report to
func TestReviewTopLevelKeyAfterBlock(t *testing.T) {
	p := rvWrite(t, "[[rec]]\nname = \"a\"\nschema = 2\n")
	c, _ := Open[testRec](p, "rec")
	if c.Err() == nil {
		// schema = 2 is a field of the record here, so it is lost by Replace.
		if err := c.Replace("a", testRec{Name: "a", Desc: "d"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rvRead(t, p), "schema") {
			t.Errorf("key lost:\n%s", rvRead(t, p))
		}
	}
}

// TestReviewAddBlankFile checks that Add on a file that holds only a blank line writes the
// schema line.
//
// Parameters:
//   - t: test to report to
func TestReviewAddBlankFile(t *testing.T) {
	p := rvWrite(t, "\n")
	c, _ := Open[testRec](p, "rec")
	if err := c.Add(testRec{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rvRead(t, p), "schema = 1") {
		t.Errorf("no schema line in file that starts with blank line:\n%q", rvRead(t, p))
	}
}

// rvPieces are the building blocks of the random documents.
var rvPieces = []string{
	"[[rec]]\n",
	"[[rec]] # trailing [[rec]]\n",
	"  [[rec]]\n",
	"\t[[rec]]\t\n",
	"name = \"n%d\"\n",
	"name = 'n%d'\n",
	"desc = \"\"\"\n[[rec]]\nname = \"fake\"\n\"\"\"\n",
	"desc = '''\n[[rec]]\n'''\n",
	"desc = \"\"\"a\\\n[[rec]]\n\"\"\"\n",
	"desc = \"\"\"x\\\"\"\"\n[[rec]]\n\"\"\"\n",
	"desc = \"\"\"x\"\"\"\"\n",
	"desc = ''''q'''\n",
	"desc = \"q\\\" # [[rec]]\"\n",
	"desc = 'q\\' \n",
	"tags = [\n  \"a\",\n  # [[rec]]\n  \"[[rec]]\",\n]\n",
	"tags = [\"a\", # c [\n  \"b\"]\n",
	"tags = [\n\"\"\"\n]\n[[rec]]\n\"\"\",\n]\n",
	"x = { a = 1,\n b = 2 }\n",
	"x = [ { a = \"]\" },\n { a = '[' } ]\n",
	"# comment\n",
	"# comment [[rec]]\n",
	"\n",
	"   \n",
	"[other]\nk = 1\n",
	"[[other]]\nk = 1\n",
	"# --- section ---\n",
	"\"quoted key ]\" = 1\n",
	"'lit key \"' = 2\n",
	"desc = \"hash # inside\" # real comment\n",
	"desc = 'it''s'\n",
}

// rvDoc builds a random document.
//
// Parameters:
//   - seed: random state
//   - crlf: whether to use CRLF line endings
func rvDoc(seed uint64, crlf bool) string {
	next := func() uint64 {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return seed
	}
	var b strings.Builder
	if next()%2 == 0 {
		b.WriteString("schema = 1\n")
	}
	n := int(next()%14) + 1
	for i := 0; i < n; i++ {
		p := rvPieces[next()%uint64(len(rvPieces))]
		if at := strings.Index(p, "%d"); at >= 0 {
			p = p[:at] + string(rune('a'+next()%26)) + p[at+2:]
		}
		b.WriteString(p)
	}
	s := b.String()
	if next()%5 == 0 {
		s = strings.TrimSuffix(s, "\n")
	}
	if crlf {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// TestReviewRandomDocs checks, on random valid documents, that edits touch only their block.
//
// Parameters:
//   - t: test to report to
func TestReviewRandomDocs(t *testing.T) {
	checked := 0
	for seed := uint64(1); seed < 3000; seed++ {
		doc := rvDoc(seed*2654435761+1, seed%3 == 0)
		p := rvWrite(t, doc)
		c, err := Open[testRec](p, "rec")
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if c.Err() != nil {
			// invalid document or unsupported layout: only complain on a layout that is valid
			// TOML with exact [[rec]] headers and a mismatch.
			continue
		}
		checked++
		recs := c.List()
		for i, r := range recs {
			sp := c.snap.Spans[i]
			if sp.Header >= sp.End || sp.Start > sp.Header {
				t.Fatalf("seed %d: bad span %+v", seed, sp)
			}
			if r.Name == "" {
				continue
			}
			pre, suf := doc[:sp.Header], doc[sp.End:]
			c2, _ := Open[testRec](p, "rec")
			nr := r
			nr.Desc = "zz"
			if err := c2.Replace(r.Name, nr); err != nil {
				continue
			}
			got := rvRead(t, p)
			if !strings.HasPrefix(got, pre) || !strings.HasSuffix(got, suf) {
				t.Fatalf("seed %d rec %d: outside bytes changed\nDOC:\n%q\nGOT:\n%q",
					seed, i, doc, got)
			}
			if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
			c3, _ := Open[testRec](p, "rec")
			if err := c3.Delete(r.Name); err != nil {
				continue
			}
			got = rvRead(t, p)
			if got != doc[:sp.Start]+doc[sp.End:] {
				t.Fatalf("seed %d rec %d: delete changed more than the block", seed, i)
			}
			c4, _ := Open[testRec](p, "rec")
			if len(c4.List()) != len(recs)-1 {
				t.Fatalf("seed %d: delete count", seed)
			}
			if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		// Add must keep the whole old content as a prefix.
		c5, _ := Open[testRec](p, "rec")
		if err := c5.Add(testRec{Name: "ADDED", Desc: "q"}); err == nil {
			got := rvRead(t, p)
			if !strings.HasPrefix(got, strings.TrimRight(doc, "\r\n")) {
				t.Fatalf("seed %d: add changed old content\n%q\n%q", seed, doc, got)
			}
		}
	}
	t.Logf("checked %d valid documents", checked)
}

// TestReviewLayoutMismatch lists valid documents refused by the engine for a layout reason.
//
// Parameters:
//   - t: test to report to
func TestReviewLayoutMismatch(t *testing.T) {
	bad := 0
	for seed := uint64(1); seed < 3000 && bad < 5; seed++ {
		doc := rvDoc(seed*2654435761+1, seed%3 == 0)
		p := rvWrite(t, doc)
		c, _ := Open[testRec](p, "rec")
		var pe *ParseError
		if errors.As(c.Err(), &pe) && strings.Contains(pe.Msg, "must be written as") {
			bad++
			t.Errorf("seed %d: %v\n%q", seed, pe, doc)
		}
	}
}

// TestReviewWeirdValuesRoundTrip checks that awkward but valid values survive Add and Replace.
//
// Parameters:
//   - t: test to report to
func TestReviewWeirdValuesRoundTrip(t *testing.T) {
	cmds := []string{
		`echo "a" # not a comment`, `printf '%s\n' "$HOME" \\ \"`, `x = """ [[alias]] """`,
		"caf\u00e9 \u4e2d\u6587 \U0001F600", `'''`, `"""`, `\`, `a\`, `"`, `'`,
		strings.Repeat("x", 100000),
		`[[alias]]`, `# c`, "\u2028 \u00a0 x", `\u0041 \x41`, `name = "x"`,
	}
	p := filepath.Join(t.TempDir(), "sub", "a.toml")
	c, err := OpenAliases(p)
	if err != nil {
		t.Fatal(err)
	}
	for i, cmd := range cmds {
		a := Alias{Name: "n" + string(rune('a'+i)), Cmd: cmd, Desc: cmd}
		if len(cmd) > 200 {
			a.Desc = ""
		}
		if err := a.Validate(); err != nil {
			t.Logf("skip %q: %v", cmd[:min(10, len(cmd))], err)
			continue
		}
		if err := c.Add(a); err != nil {
			t.Errorf("add %q: %v", cmd[:min(10, len(cmd))], err)
			continue
		}
		a.Cmd += " x"
		if err := c.Replace(a.Name, a); err != nil {
			t.Errorf("replace %q: %v", cmd[:min(10, len(cmd))], err)
		}
	}
	c2, err := OpenAliases(p)
	if err != nil || c2.Err() != nil {
		t.Fatal(err, c2.Err())
	}
	if len(c2.List()) != len(c.List()) {
		t.Errorf("reload count %d vs %d", len(c2.List()), len(c.List()))
	}
	for _, a := range c2.List() {
		if g, _ := c.Get(a.Name); g != a {
			t.Errorf("mismatch for %s", a.Name)
		}
	}
}

// TestReviewBOMAndEdgeFiles checks BOM, CRLF and odd endings.
//
// Parameters:
//   - t: test to report to
func TestReviewBOMAndEdgeFiles(t *testing.T) {
	docs := []string{
		"\xef\xbb\xbfschema = 1\r\n\r\n[[rec]]\r\nname = \"a\"\r\n",
		"\xef\xbb\xbf[[rec]]\nname = \"a\"",
		"schema = 1",
		"schema = 1\n",
		"# only a comment",
		"[[rec]]\nname = \"a\"\n# trailing comment no newline",
		"[[rec]]\nname = \"a\"\r\n[[rec]]\nname = \"b\"\r\n",
	}
	for i, d := range docs {
		p := rvWrite(t, d)
		c, _ := Open[testRec](p, "rec")
		if c.Err() != nil {
			t.Errorf("doc %d: %v", i, c.Err())
			continue
		}
		if err := c.Add(testRec{Name: "z", Desc: "d"}); err != nil {
			t.Errorf("doc %d add: %v", i, err)
			continue
		}
		got := rvRead(t, p)
		if !strings.HasPrefix(got, d) && !strings.HasPrefix(got, strings.TrimRight(d, "\r\n")) {
			t.Errorf("doc %d: prefix changed %q -> %q", i, d, got)
		}
		c2, _ := Open[testRec](p, "rec")
		if c2.Err() != nil || len(c2.List()) != len(c.List()) {
			t.Errorf("doc %d reload: %v", i, c2.Err())
		}
		for _, r := range c2.List() {
			if err := c2.Delete(r.Name); err != nil {
				t.Errorf("doc %d delete %s: %v", i, r.Name, err)
			}
		}
		c3, _ := Open[testRec](p, "rec")
		if c3.Err() != nil || len(c3.List()) != 0 {
			t.Errorf("doc %d after deletes: %v %v\n%q", i, c3.Err(), c3.List(), rvRead(t, p))
		}
	}
}

// TestReviewRenameCollision checks Replace with a rename onto an existing key.
//
// Parameters:
//   - t: test to report to
func TestReviewRenameCollision(t *testing.T) {
	p := rvWrite(t, "schema = 1\n\n[[rec]]\nname = \"a\"\n\n[[rec]]\nname = \"b\"\n")
	before := rvRead(t, p)
	c, _ := Open[testRec](p, "rec")
	if err := c.Replace("a", testRec{Name: "b"}); !errors.Is(err, ErrExists) {
		t.Errorf("got %v", err)
	}
	if rvRead(t, p) != before {
		t.Errorf("file changed")
	}
}

// rvWriter adds, replaces and deletes records of one file through its own collection.
//
// Parameters:
//   - t: test to report to
//   - p: file path
//   - w: writer number, used to build unique record names
func rvWriter(t *testing.T, p string, w int) {
	c, _ := Open[testRec](p, "rec")
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("w%d-%d", w, i)
		if err := c.Add(testRec{Name: name, Desc: "d"}); err != nil {
			t.Errorf("add %s: %v", name, err)
			return
		}
		if err := c.Replace(name, testRec{Name: name, Desc: "e"}); err != nil {
			t.Errorf("replace %s: %v", name, err)
			return
		}
		if i%3 == 0 {
			if err := c.Delete(name); err != nil {
				t.Errorf("delete %s: %v", name, err)
				return
			}
		}
	}
}

// TestReviewConcurrentWriters checks that parallel writers on one file lose no record.
//
// Parameters:
//   - t: test to report to
func TestReviewConcurrentWriters(t *testing.T) {
	p := rvWrite(t, "# header\nschema = 1\n")
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rvWriter(t, p, w)
		}()
	}
	wg.Wait()
	c, _ := Open[testRec](p, "rec")
	if c.Err() != nil {
		t.Fatal(c.Err())
	}
	if got, want := len(c.List()), 8*8; got != want {
		t.Errorf("got %d records, want %d", got, want)
	}
	if !strings.HasPrefix(rvRead(t, p), "# header\nschema = 1\n") {
		t.Errorf("header changed")
	}
}
