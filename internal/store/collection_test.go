package store

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// engFixture is a file with a header, section comments, owned comments and an inline comment.
const engFixture = "# connections\n" +
	"\n" +
	"schema = 1\n" +
	"\n" +
	"# --- clients ---\n" +
	"\n" +
	"# the first one\n" +
	"[[rec]]\n" +
	"name = \"a\"\n" +
	"desc = \"alpha\"   # inline\n" +
	"\n" +
	"# --- internal ---\n" +
	"\n" +
	"[[rec]]\n" +
	"name = \"b\"\n" +
	"\n" +
	"# about c\n" +
	"[[rec]]\n" +
	"name = \"c\"\n" +
	"tags = [\"x\"]\n" +
	"\n" +
	"# end of file\n"

// engWrite writes a file in a fresh temporary directory.
//
// Parameters:
//   - t: test to report to
//   - content: file content
//
// Returns the path of the file, which has mode 0600.
func engWrite(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "recs.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// engOpen opens a collection of test records and fails the test on an I/O error.
//
// Parameters:
//   - t: test to report to
//   - path: file path
func engOpen(t *testing.T, path string) *Collection[testRec] {
	t.Helper()

	c, err := Open[testRec](path, testTable)
	if err != nil {
		t.Fatalf("Open(%s) = %v", path, err)
	}

	return c
}

// engRead returns the content of a file as a string.
//
// Parameters:
//   - t: test to report to
//   - path: file path
func engRead(t *testing.T, path string) string {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(got)
}

// engKeys returns the keys of a record list in order.
//
// Parameters:
//   - recs: records to read
func engKeys(recs []testRec) []string {
	keys := make([]string, 0, len(recs))
	for _, r := range recs {
		keys = append(keys, r.Name)
	}

	return keys
}

// engNoTemp fails the test when a temporary file of the writer is left in a directory.
//
// Parameters:
//   - t: test to report to
//   - dir: directory to inspect
func engNoTemp(t *testing.T, dir string) {
	t.Helper()

	for _, name := range engListDir(t, dir) {
		if strings.Contains(name, ".tmp-") {
			t.Errorf("temporary file %s left in %s", name, dir)
		}
	}
}

// engCheckFile compares the file with the expected content.
//
// Parameters:
//   - t: test to report to
//   - path: file path
//   - want: expected content
func engCheckFile(t *testing.T, path, want string) {
	t.Helper()

	if got := engRead(t, path); got != want {
		t.Errorf("file content:\n%q\nwant:\n%q", got, want)
	}
	engNoTemp(t, filepath.Dir(path))
}

// TestOpenMissingFile checks an empty collection on a path that does not exist.
//
// Parameters:
//   - t: test to report to
func TestOpenMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "recs.toml")
	c := engOpen(t, path)

	if c.Err() != nil || len(c.List()) != 0 || c.Path() != path {
		t.Errorf("Err = %v, List = %v, Path = %q", c.Err(), c.List(), c.Path())
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open created the directory, stat error = %v", err)
	}
}

// TestOpenArguments checks the arguments Open refuses.
//
// Parameters:
//   - t: test to report to
func TestOpenArguments(t *testing.T) {
	if c, err := Open[testRec]("", testTable); err == nil || c != nil {
		t.Errorf("Open with an empty path = %v, %v", c, err)
	}
	for _, table := range []string{"", "a b", "a.b", "[x]", "a]"} {
		if c, err := Open[testRec]("x.toml", table); err == nil || c != nil {
			t.Errorf("Open with table %q = %v, %v", table, c, err)
		}
	}
}

// TestOpenUnreadable checks that a path that cannot be read is an error, not a parse problem.
//
// Parameters:
//   - t: test to report to
func TestOpenUnreadable(t *testing.T) {
	c, err := Open[testRec](t.TempDir(), testTable)
	if err == nil || c == nil {
		t.Fatalf("Open on a directory = %v, %v, want a collection and an error", c, err)
	}
	if c.Err() == nil || errors.Is(c.Err(), ErrReadOnly) {
		t.Errorf("Err = %v", c.Err())
	}
	if err := c.Add(testRec{Name: "a"}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Add = %v, want ErrReadOnly", err)
	}
}

// TestAddCreatesFile checks the first write: content, modes and the lock file.
//
// Parameters:
//   - t: test to report to
func TestAddCreatesFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a", "b")
	path := filepath.Join(dir, "recs.toml")
	c := engOpen(t, path)

	if err := c.Add(testRec{Name: "one", Desc: "first"}); err != nil {
		t.Fatal(err)
	}

	engCheckFile(t, path, "schema = 1\n\n[[rec]]\nname = \"one\"\ndesc = \"first\"\n")
	if m := engMode(t, path); m != 0o600 {
		t.Errorf("file mode = %o, want 600", m)
	}
	for _, d := range []string{filepath.Join(root, "a"), dir} {
		if m := engMode(t, d); m != 0o700 {
			t.Errorf("%s mode = %o, want 700", d, m)
		}
	}
	if m := engMode(t, path+".lock"); m != 0o600 {
		t.Errorf("lock file mode = %o, want 600", m)
	}
	if got := engKeys(c.List()); !reflect.DeepEqual(got, []string{"one"}) {
		t.Errorf("List = %v", got)
	}
}

// TestAddAppends checks that an add keeps every existing byte and appends after a blank line.
//
// Parameters:
//   - t: test to report to
func TestAddAppends(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	if err := c.Add(testRec{Name: "d", Tags: []string{"p", "q"}}); err != nil {
		t.Fatal(err)
	}

	engCheckFile(t, path, engFixture+"\n[[rec]]\nname = \"d\"\ntags = [\"p\", \"q\"]\n")
	if got := engKeys(c.List()); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("List = %v", got)
	}
}

// TestReplaceKeepsComments checks that a replace changes the keys that changed and nothing else.
//
// Parameters:
//   - t: test to report to
func TestReplaceKeepsComments(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	if err := c.Replace("a", testRec{Name: "a", Desc: "new"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(engFixture,
		"[[rec]]\nname = \"a\"\ndesc = \"alpha\"   # inline\n",
		"[[rec]]\nname = \"a\"\ndesc = \"new\"   # inline\n", 1)
	engCheckFile(t, path, want)

	if err := c.Replace("c", testRec{Name: "c"}); err != nil {
		t.Fatal(err)
	}
	want = strings.Replace(want, "name = \"c\"\ntags = [\"x\"]\n", "name = \"c\"\n", 1)
	engCheckFile(t, path, want)
}

// TestReplaceIdentical checks that replacing a record by itself changes no byte.
//
// Parameters:
//   - t: test to report to
func TestReplaceIdentical(t *testing.T) {
	canon := "schema = 1\n\n# keep\n[[rec]]\nname = \"a\"\ndesc = \"x\"\n\n[[rec]]\nname = \"b\"\n"
	path := engWrite(t, canon)
	c := engOpen(t, path)

	if err := c.Replace("a", testRec{Name: "a", Desc: "x"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path, canon)
}

// TestDeleteRemovesOwnedComments checks that a delete removes the block and its comment run.
//
// Parameters:
//   - t: test to report to
func TestDeleteRemovesOwnedComments(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	if err := c.Delete("a"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(engFixture,
		"# the first one\n[[rec]]\nname = \"a\"\ndesc = \"alpha\"   # inline\n", "", 1)
	engCheckFile(t, path, want)

	if err := c.Delete("c"); err != nil {
		t.Fatal(err)
	}
	want = strings.Replace(want, "# about c\n[[rec]]\nname = \"c\"\ntags = [\"x\"]\n", "", 1)
	engCheckFile(t, path, want)

	if err := c.Delete("b"); err != nil {
		t.Fatal(err)
	}
	want = strings.Replace(want, "[[rec]]\nname = \"b\"\n", "", 1)
	engCheckFile(t, path, want)
	if len(c.List()) != 0 {
		t.Errorf("List = %v, want empty", c.List())
	}
}

// TestSectionCommentUntouched checks that a comment separated by a blank line never moves.
//
// Parameters:
//   - t: test to report to
func TestSectionCommentUntouched(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	if err := c.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if err := c.Replace("b", testRec{Name: "b", Desc: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(testRec{Name: "z"}); err != nil {
		t.Fatal(err)
	}

	got := engRead(t, path)
	for _, keep := range []string{
		"# connections\n\nschema = 1\n\n# --- clients ---\n\n",
		"\n# --- internal ---\n\n", "# end of file\n",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("file lost %q:\n%s", keep, got)
		}
	}
}

// TestCRLF checks that a Windows file round-trips unchanged outside the edited block.
//
// Parameters:
//   - t: test to report to
func TestCRLF(t *testing.T) {
	crlf := strings.ReplaceAll(engFixture, "\n", "\r\n")
	path := engWrite(t, crlf)
	c := engOpen(t, path)

	if err := c.Replace("b", testRec{Name: "b", Desc: "w"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(crlf, "[[rec]]\r\nname = \"b\"\r\n",
		"[[rec]]\r\nname = \"b\"\r\ndesc = \"w\"\r\n", 1)
	engCheckFile(t, path, want)

	if err := c.Add(testRec{Name: "d"}); err != nil {
		t.Fatal(err)
	}
	want += "\r\n[[rec]]\r\nname = \"d\"\r\n"
	engCheckFile(t, path, want)

	if err := c.Delete("c"); err != nil {
		t.Fatal(err)
	}
	want = strings.Replace(want, "# about c\r\n[[rec]]\r\nname = \"c\"\r\ntags = [\"x\"]\r\n", "", 1)
	engCheckFile(t, path, want)
}

// TestMixedLineEndings checks that untouched lines keep their ending in a mixed file.
//
// Parameters:
//   - t: test to report to
func TestMixedLineEndings(t *testing.T) {
	in := "schema = 1\r\n\r\n[[rec]]\r\nname = \"a\"\r\n\r\n[[rec]]\nname = \"b\"\n"
	path := engWrite(t, in)
	c := engOpen(t, path)

	if err := c.Replace("b", testRec{Name: "b", Desc: "x"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path,
		"schema = 1\r\n\r\n[[rec]]\r\nname = \"a\"\r\n\r\n[[rec]]\nname = \"b\"\ndesc = \"x\"\n")
}

// TestNoTrailingNewline checks files whose last line has no line ending.
//
// Parameters:
//   - t: test to report to
func TestNoTrailingNewline(t *testing.T) {
	in := "schema = 1\n\n[[rec]]\nname = \"a\""
	path := engWrite(t, in)
	c := engOpen(t, path)

	if err := c.Replace("a", testRec{Name: "a", Desc: "x"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path, "schema = 1\n\n[[rec]]\nname = \"a\"\ndesc = \"x\"")

	if err := c.Add(testRec{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path,
		"schema = 1\n\n[[rec]]\nname = \"a\"\ndesc = \"x\"\n\n[[rec]]\nname = \"b\"\n")

	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	c = engOpen(t, path)
	if err := c.Delete("a"); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path, "schema = 1\n\n")
}

// TestMultilineStrings checks that a header-looking line inside a string is not a block.
//
// Parameters:
//   - t: test to report to
func TestMultilineStrings(t *testing.T) {
	in := "schema = 1\n\n[[rec]]\nname = \"a\"\ndesc = \"\"\"\n[[rec]]\nname = \"fake\"\n\"\"\"\n\n" +
		"[[rec]]\nname = \"b\"\ndesc = '''\n[[rec]]\n'''\n\n[[rec]]\nname = \"c\"\n"
	path := engWrite(t, in)
	c := engOpen(t, path)

	if c.Err() != nil {
		t.Fatal(c.Err())
	}
	if got := engKeys(c.List()); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("List = %v", got)
	}
	if got := c.Line("c"); got != 16 {
		t.Errorf("Line(c) = %d, want 16", got)
	}

	if err := c.Replace("b", testRec{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete("a"); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path, "schema = 1\n\n\n[[rec]]\nname = \"b\"\n\n[[rec]]\nname = \"c\"\n")
}

// TestSpecialCharacters checks that awkward strings survive a write and a reload.
//
// Parameters:
//   - t: test to report to
func TestSpecialCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recs.toml")
	c := engOpen(t, path)

	descs := []string{
		"say \"hi\" and 'bye'", "back\\slash", "tab\there", "line1\nline2\r\nline3",
		"[[rec]]", "\"\"\"", "'''", "# not a comment", "caf\u00e9 \u2603 \U0001F600",
		"\x01\x7f", "name = \"x\"", "[other]\nk = 1", "trailing backslash \\",
	}
	for i, d := range descs {
		rec := testRec{Name: fmt.Sprintf("r%d", i), Desc: d, Tags: []string{d, "[[rec]]"}}
		if err := c.Add(rec); err != nil {
			t.Fatalf("Add(%q) = %v", d, err)
		}
	}

	again := engOpen(t, path)
	if again.Err() != nil {
		t.Fatal(again.Err())
	}
	for i, d := range descs {
		got, ok := again.Get(fmt.Sprintf("r%d", i))
		if !ok || got.Desc != d || len(got.Tags) != 2 || got.Tags[0] != d {
			t.Errorf("record %d = %+v, %v, want desc %q", i, got, ok, d)
		}
	}
	if !reflect.DeepEqual(c.List(), again.List()) {
		t.Error("the collection and a fresh load disagree")
	}
}

// TestEmptyTagsRoundTrip checks that an empty but non-nil slice is accepted and reads back nil.
//
// Parameters:
//   - t: test to report to
func TestEmptyTagsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recs.toml")
	c := engOpen(t, path)

	if err := c.Add(testRec{Name: "e", Tags: []string{}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get("e"); got.Tags != nil {
		t.Errorf("Tags = %#v, want nil as read back from the file", got.Tags)
	}
	engCheckFile(t, path, "schema = 1\n\n[[rec]]\nname = \"e\"\n")
}

// TestReplaceRename checks a rename: same place, old key gone, collisions refused.
//
// Parameters:
//   - t: test to report to
func TestReplaceRename(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	if err := c.Replace("b", testRec{Name: "a"}); !errors.Is(err, ErrExists) {
		t.Fatalf("rename onto a = %v, want ErrExists", err)
	}
	engCheckFile(t, path, engFixture)

	if err := c.Replace("b", testRec{Name: "bb", Desc: "renamed"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(engFixture, "name = \"b\"\n", "name = \"bb\"\ndesc = \"renamed\"\n", 1)
	engCheckFile(t, path, want)

	if got := engKeys(c.List()); !reflect.DeepEqual(got, []string{"a", "bb", "c"}) {
		t.Errorf("List = %v", got)
	}
	if _, ok := c.Get("b"); ok {
		t.Error("the old key is still found")
	}
	if err := c.Replace("bb", testRec{Name: "bb", Desc: "same key"}); err != nil {
		t.Errorf("replace keeping the key = %v", err)
	}
}

// TestSentinelErrors checks ErrExists, ErrNotFound and the case sensitivity of keys.
//
// Parameters:
//   - t: test to report to
func TestSentinelErrors(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	if err := c.Add(testRec{Name: "a"}); !errors.Is(err, ErrExists) {
		t.Errorf("Add duplicate = %v, want ErrExists", err)
	}
	if err := c.Replace("zz", testRec{Name: "zz"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Replace missing = %v, want ErrNotFound", err)
	}
	if err := c.Delete("zz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing = %v, want ErrNotFound", err)
	}
	if err := c.Delete("A"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete with another case = %v, want ErrNotFound", err)
	}
	engCheckFile(t, path, engFixture)

	if err := c.Add(testRec{Name: "A"}); err != nil {
		t.Errorf("Add with another case = %v", err)
	}
}

// TestValidationBeforeWrite checks that an invalid record writes nothing, not even a directory.
//
// Parameters:
//   - t: test to report to
func TestValidationBeforeWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never")
	c := engOpen(t, filepath.Join(dir, "recs.toml"))

	err := c.Add(testRec{Name: "has space"})
	if err == nil || errors.Is(err, ErrExists) || !strings.Contains(err.Error(), "space") {
		t.Errorf("Add invalid = %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the directory was created, stat error = %v", err)
	}

	path := engWrite(t, engFixture)
	c = engOpen(t, path)
	if err := c.Replace("a", testRec{Name: ""}); err == nil {
		t.Error("Replace with an empty name succeeded")
	}
	engCheckFile(t, path, engFixture)
}

// TestListGetCopies checks that callers cannot change the collection through its results.
//
// Parameters:
//   - t: test to report to
func TestListGetCopies(t *testing.T) {
	c := engOpen(t, engWrite(t, engFixture))

	list := c.List()
	list[0].Name = "hacked"
	if got, _ := c.Get("a"); got.Name != "a" {
		t.Error("List returned the internal slice")
	}

	c.snap.Invalid["x"] = errors.New("x")
	inv := c.Invalid()
	inv["y"] = errors.New("y")
	if _, ok := c.Invalid()["y"]; ok {
		t.Error("Invalid returned the internal map")
	}
}

// TestExistingEmptyFile checks an add to a file that exists with no content.
//
// Parameters:
//   - t: test to report to
func TestExistingEmptyFile(t *testing.T) {
	path := engWrite(t, "")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	c := engOpen(t, path)

	if err := c.Add(testRec{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path, "schema = 1\n\n[[rec]]\nname = \"a\"\n")
	if m := engMode(t, path); m != 0o640 {
		t.Errorf("mode = %o, want 640", m)
	}
}

// TestAddAfterTrailingBlanks checks that no extra blank line piles up before a new block.
//
// Parameters:
//   - t: test to report to
func TestAddAfterTrailingBlanks(t *testing.T) {
	path := engWrite(t, "schema = 1\n\n[[rec]]\nname = \"a\"\n\n")
	c := engOpen(t, path)

	if err := c.Add(testRec{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path, "schema = 1\n\n[[rec]]\nname = \"a\"\n\n[[rec]]\nname = \"b\"\n")
}

// TestNoSchemaAndBOM checks files without a schema key and with a byte order mark.
//
// Parameters:
//   - t: test to report to
func TestNoSchemaAndBOM(t *testing.T) {
	path := engWrite(t, "\xef\xbb\xbf[[rec]]\nname = \"a\"\n")
	c := engOpen(t, path)
	if c.Err() != nil || len(c.List()) != 1 {
		t.Fatalf("Err = %v, List = %v", c.Err(), c.List())
	}

	if err := c.Add(testRec{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path, "\xef\xbb\xbf[[rec]]\nname = \"a\"\n\n[[rec]]\nname = \"b\"\n")
}

// TestUnknownKeys checks that unknown keys do not break a load and survive untouched blocks.
//
// Parameters:
//   - t: test to report to
func TestUnknownKeys(t *testing.T) {
	in := "schema = 1\n\n[[rec]]\nname = \"a\"\nfuture = 5\n\n[[rec]]\nname = \"b\"\n"
	path := engWrite(t, in)
	c := engOpen(t, path)

	if err := c.Replace("b", testRec{Name: "b", Desc: "x"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path, strings.Replace(in, "name = \"b\"\n", "name = \"b\"\ndesc = \"x\"\n", 1))
}

// TestInvalidAndLine checks the reporting of records that fail Validate and the header lines.
//
// Parameters:
//   - t: test to report to
func TestInvalidAndLine(t *testing.T) {
	in := "schema = 1\n\n[[rec]]\nname = \"ok\"\n\n[[rec]]\nname = \"bad name\"\n\n" +
		"[[rec]]\nname = \"\"\n"
	path := engWrite(t, in)
	c := engOpen(t, path)

	if c.Err() != nil {
		t.Fatal(c.Err())
	}
	if got := engKeys(c.List()); !reflect.DeepEqual(got, []string{"ok", "bad name", ""}) {
		t.Fatalf("List = %q, invalid records must be kept", got)
	}
	inv := c.Invalid()
	if len(inv) != 2 || inv["bad name"] == nil || inv[""] == nil || inv["ok"] != nil {
		t.Errorf("Invalid = %v", inv)
	}
	for key, want := range map[string]int{"ok": 3, "bad name": 6, "": 9, "missing": 0} {
		if got := c.Line(key); got != want {
			t.Errorf("Line(%q) = %d, want %d", key, got, want)
		}
	}

	if err := c.Replace("bad name", testRec{Name: "fixed"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(""); err != nil {
		t.Fatal(err)
	}
	if len(c.Invalid()) != 0 {
		t.Errorf("Invalid = %v after the fixes", c.Invalid())
	}
	if got := c.Line("fixed"); got != 6 {
		t.Errorf("Line(fixed) = %d, want 6", got)
	}
}

// TestLineFixture checks the header lines of the commented fixture and their update.
//
// Parameters:
//   - t: test to report to
func TestLineFixture(t *testing.T) {
	c := engOpen(t, engWrite(t, engFixture))

	for key, want := range map[string]int{"a": 8, "b": 14, "c": 18} {
		if got := c.Line(key); got != want {
			t.Errorf("Line(%q) = %d, want %d", key, got, want)
		}
	}
	if err := c.Delete("a"); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int{"b": 10, "c": 14} {
		if got := c.Line(key); got != want {
			t.Errorf("after delete Line(%q) = %d, want %d", key, got, want)
		}
	}
}

// engExternal overwrites a file the way another program would, without going through the engine.
//
// Parameters:
//   - t: test to report to
//   - path: file path
//   - content: new file content
func engExternal(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestConflictCleanReapply checks that an unrelated external change is merged into the write.
//
// Parameters:
//   - t: test to report to
func TestConflictCleanReapply(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	ext := engFixture + "\n[[rec]]\nname = \"ext\"\n"
	engExternal(t, path, ext)

	if err := c.Add(testRec{Name: "mine"}); err != nil {
		t.Fatalf("Add = %v, want a clean re-apply", err)
	}
	engCheckFile(t, path, ext+"\n[[rec]]\nname = \"mine\"\n")
	if got := engKeys(c.List()); !reflect.DeepEqual(got, []string{"a", "b", "c", "ext", "mine"}) {
		t.Errorf("List = %v", got)
	}
}

// TestConflictReplaceOtherRecord checks a replace after an external edit of another record.
//
// Parameters:
//   - t: test to report to
func TestConflictReplaceOtherRecord(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	ext := strings.Replace(engFixture, "name = \"b\"\n", "name = \"b\"\ndesc = \"ext\"\n", 1)
	engExternal(t, path, ext)

	if err := c.Replace("c", testRec{Name: "c", Desc: "mine"}); err != nil {
		t.Fatalf("Replace = %v", err)
	}
	want := strings.Replace(ext, "name = \"c\"\ntags = [\"x\"]\n",
		"name = \"c\"\ndesc = \"mine\"\n", 1)
	engCheckFile(t, path, want)
	if err := c.Delete("a"); err != nil {
		t.Errorf("Delete after the reload = %v", err)
	}
}

// TestConflictErrors checks the edits that no longer apply after an external change.
//
// Parameters:
//   - t: test to report to
func TestConflictErrors(t *testing.T) {
	cases := []struct {
		name string
		ext  string
		do   func(c *Collection[testRec]) error
	}{
		{
			name: "same key added",
			ext:  engFixture + "\n[[rec]]\nname = \"mine\"\n",
			do:   func(c *Collection[testRec]) error { return c.Add(testRec{Name: "mine"}) },
		},
		{
			name: "replaced record deleted",
			ext:  strings.Replace(engFixture, "[[rec]]\nname = \"b\"\n", "", 1),
			do: func(c *Collection[testRec]) error {
				return c.Replace("b", testRec{Name: "b", Desc: "x"})
			},
		},
		{
			name: "replaced record modified",
			ext:  strings.Replace(engFixture, "name = \"b\"\n", "name = \"b\"\ndesc = \"ext\"\n", 1),
			do: func(c *Collection[testRec]) error {
				return c.Replace("b", testRec{Name: "b", Desc: "x"})
			},
		},
		{
			name: "rename target appeared",
			ext:  engFixture + "\n[[rec]]\nname = \"new\"\n",
			do:   func(c *Collection[testRec]) error { return c.Replace("b", testRec{Name: "new"}) },
		},
		{
			name: "deleted record already deleted",
			ext:  strings.Replace(engFixture, "[[rec]]\nname = \"b\"\n", "", 1),
			do:   func(c *Collection[testRec]) error { return c.Delete("b") },
		},
		{
			name: "deleted record modified",
			ext:  strings.Replace(engFixture, "name = \"b\"\n", "name = \"b\"\ndesc = \"ext\"\n", 1),
			do:   func(c *Collection[testRec]) error { return c.Delete("b") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkConflict(t, tc.ext, tc.do) })
	}
}

// checkConflict loads the fixture, replaces the file behind the collection's back, runs an edit
// and expects ErrConflict, the external content untouched and the collection reloaded.
//
// Parameters:
//   - t: test to report to
//   - ext: content written behind the collection's back
//   - do: edit to run
func checkConflict(t *testing.T, ext string, do func(c *Collection[testRec]) error) {
	t.Helper()

	path := engWrite(t, engFixture)
	c := engOpen(t, path)
	engExternal(t, path, ext)

	if err := do(c); !errors.Is(err, ErrConflict) {
		t.Fatalf("edit = %v, want ErrConflict", err)
	}
	engCheckFile(t, path, ext)

	fresh := engOpen(t, path)
	if !reflect.DeepEqual(c.List(), fresh.List()) {
		t.Errorf("collection was not reloaded: %v, want %v", c.List(), fresh.List())
	}
}

// TestConflictFileAppears checks a file that was created after the collection was opened.
//
// Parameters:
//   - t: test to report to
func TestConflictFileAppears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recs.toml")
	c := engOpen(t, path)

	engExternal(t, path, "schema = 1\n\n[[rec]]\nname = \"ext\"\n")
	if err := c.Add(testRec{Name: "mine"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, path, "schema = 1\n\n[[rec]]\nname = \"ext\"\n\n[[rec]]\nname = \"mine\"\n")
}

// TestConflictFileVanishes checks a file that was removed after the collection was loaded.
//
// Parameters:
//   - t: test to report to
func TestConflictFileVanishes(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := c.Replace("a", testRec{Name: "a", Desc: "x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Replace = %v, want ErrConflict", err)
	}
	if err := c.Add(testRec{Name: "n"}); err != nil {
		t.Fatalf("Add = %v", err)
	}
	engCheckFile(t, path, "schema = 1\n\n[[rec]]\nname = \"n\"\n")
}

// TestConflictBrokenFile checks an external edit that leaves the file unparsable.
//
// Parameters:
//   - t: test to report to
func TestConflictBrokenFile(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	engExternal(t, path, "schema = 1\n[[rec]]\nname = \n")
	err := c.Add(testRec{Name: "mine"})

	var pe *ParseError
	if !errors.Is(err, ErrReadOnly) || !errors.As(err, &pe) || pe.Line != 3 {
		t.Fatalf("Add = %v, want ErrReadOnly wrapping a ParseError at line 3", err)
	}
	if c.Err() == nil {
		t.Error("the collection did not become read-only")
	}
	engCheckFile(t, path, "schema = 1\n[[rec]]\nname = \n")
}

// TestLoadPicksUpChanges checks Load after an external edit, and the way out of read-only mode.
//
// Parameters:
//   - t: test to report to
func TestLoadPicksUpChanges(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	engExternal(t, path, "schema = 1\n[[rec]]\nname = \"x\"\n")
	if err := c.Load(); err != nil || !reflect.DeepEqual(engKeys(c.List()), []string{"x"}) {
		t.Fatalf("Load = %v, List = %v", err, c.List())
	}

	engExternal(t, path, "schema = 1\n[[rec]]\nname =\n")
	var pe *ParseError
	if err := c.Load(); !errors.As(err, &pe) || c.Err() == nil || len(c.List()) != 0 {
		t.Fatalf("Load = %v, Err = %v, List = %v", err, c.Err(), c.List())
	}
	if err := c.Add(testRec{Name: "y"}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Add = %v, want ErrReadOnly", err)
	}

	engExternal(t, path, "schema = 1\n")
	if err := c.Load(); err != nil || c.Err() != nil {
		t.Fatalf("Load = %v, Err = %v after the fix", err, c.Err())
	}
	if err := c.Add(testRec{Name: "y"}); err != nil {
		t.Errorf("Add after the fix = %v", err)
	}
}

// TestConcurrentAdds checks that collections of several goroutines never lose a write.
//
// Parameters:
//   - t: test to report to
func TestConcurrentAdds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recs.toml")
	const workers, each = 8, 6

	var wg sync.WaitGroup
	errs := make(chan error, workers*each)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- addMany(path, w, each)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}

	c := engOpen(t, path)
	if c.Err() != nil || len(c.List()) != workers*each {
		t.Fatalf("Err = %v, %d records, want %d", c.Err(), len(c.List()), workers*each)
	}
	engNoTemp(t, filepath.Dir(path))
}

// addMany opens its own collection and adds records named after a worker.
//
// Parameters:
//   - path: file shared by all workers
//   - worker: worker number, used in the names
//   - n: number of records to add
//
// Returns the first error met, or nil.
func addMany(path string, worker, n int) error {
	c, err := Open[testRec](path, testTable)
	if err != nil {
		return err
	}
	for i := range n {
		if err := c.Add(testRec{Name: fmt.Sprintf("w%d-%d", worker, i)}); err != nil {
			return err
		}
	}

	return nil
}

// TestConcurrentSameKey checks that two writers adding one key give one success and one failure.
//
// Parameters:
//   - t: test to report to
func TestConcurrentSameKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recs.toml")
	const workers = 6

	var wg sync.WaitGroup
	results := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := Open[testRec](path, testTable)
			if err == nil {
				err = c.Add(testRec{Name: "same"})
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	ok := 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrConflict) && !errors.Is(err, ErrExists):
			t.Errorf("unexpected error %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d writers succeeded, want exactly 1", ok)
	}
	if got := len(engOpen(t, path).List()); got != 1 {
		t.Errorf("%d records on disk, want 1", got)
	}
}

// TestLockExcludes checks that a write waits for the lock held by somebody else.
//
// Parameters:
//   - t: test to report to
func TestLockExcludes(t *testing.T) {
	path := engWrite(t, "schema = 1\n")
	c := engOpen(t, path)

	target, err := resolveTarget(path)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := acquireLock(target, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- c.Add(testRec{Name: "late"}) }()

	select {
	case err := <-done:
		releaseLock(lock)
		t.Fatalf("Add returned %v while the lock was held", err)
	case <-time.After(200 * time.Millisecond):
	}
	if got := engRead(t, path); got != "schema = 1\n" {
		t.Errorf("file changed while locked: %q", got)
	}

	releaseLock(lock)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Add = %v after the release", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Add still blocked after the lock was released")
	}
	engCheckFile(t, path, "schema = 1\n\n[[rec]]\nname = \"late\"\n")
}

// TestLockWaitBounded checks that a write gives up with ErrBusy when the lock stays taken for
// longer than the wait of the collection, and that nothing is written.
//
// Parameters:
//   - t: test to report to
func TestLockWaitBounded(t *testing.T) {
	path := engWrite(t, "schema = 1\n")
	c := engOpen(t, path)
	c.SetLockWait(50 * time.Millisecond)

	target, err := resolveTarget(path)
	if err != nil {
		t.Fatal(err)
	}

	lock, err := acquireLock(target, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	defer releaseLock(lock)

	start := time.Now()
	err = c.Add(testRec{Name: "late"})

	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Add = %v, want ErrBusy", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Add waited %v for a lock it should have given up on after 50ms", d)
	}
	if got := engRead(t, path); got != "schema = 1\n" {
		t.Errorf("file changed while locked: %q", got)
	}
}

// TestSymlinkedFile checks that a write goes through a symlink and leaves the link alone.
//
// Parameters:
//   - t: test to report to
func TestSymlinkedFile(t *testing.T) {
	root := engRoot(t)
	realDir := filepath.Join(root, "real")
	linkDir := filepath.Join(root, "links")
	for _, d := range []string{realDir, linkDir} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	real := filepath.Join(realDir, "recs.toml")
	link := filepath.Join(linkDir, "recs.toml")
	engExternal(t, real, engFixture)
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	c := engOpen(t, link)
	if err := c.Add(testRec{Name: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Replace("a", testRec{Name: "a", Desc: "z"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v, %v", info, err)
	}
	if dest, _ := os.Readlink(link); dest != real {
		t.Errorf("link points to %q, want %q", dest, real)
	}
	if got := engRead(t, real); !strings.Contains(got, "name = \"d\"") ||
		!strings.Contains(got, "desc = \"z\"") {
		t.Errorf("target not updated:\n%s", got)
	}
	wantFiles := []string{"recs.toml", "recs.toml.lock"}
	if got := engListDir(t, realDir); !reflect.DeepEqual(got, wantFiles) {
		t.Errorf("target directory holds %v", got)
	}
	if got := engListDir(t, linkDir); !reflect.DeepEqual(got, []string{"recs.toml"}) {
		t.Errorf("link directory holds %v", got)
	}
}

// TestSymlinkedDirNewFile checks that a new file lands in the target of a symlinked directory.
//
// Parameters:
//   - t: test to report to
func TestSymlinkedDirNewFile(t *testing.T) {
	root := engRoot(t)
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	c := engOpen(t, filepath.Join(root, "link", "recs.toml"))
	if err := c.Add(testRec{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	engCheckFile(t, filepath.Join(realDir, "recs.toml"), "schema = 1\n\n[[rec]]\nname = \"a\"\n")
}

// TestDanglingSymlink checks that a dangling symlink is reported and left in place.
//
// Parameters:
//   - t: test to report to
func TestDanglingSymlink(t *testing.T) {
	root := engRoot(t)
	link := filepath.Join(root, "recs.toml")
	if err := os.Symlink(filepath.Join(root, "gone.toml"), link); err != nil {
		t.Fatal(err)
	}

	c := engOpen(t, link)
	if err := c.Add(testRec{Name: "a"}); err == nil || !strings.Contains(err.Error(), "dangling") {
		t.Errorf("Add = %v, want a dangling symlink error", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the symlink was replaced: %v, %v", info, err)
	}
}

// TestVerifyFailureWritesNothing checks that a record that cannot round-trip aborts the write.
//
// Parameters:
//   - t: test to report to
func TestVerifyFailureWritesNothing(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)
	bad := testRec{Name: "lost", Lost: "never written"}

	if err := c.Add(bad); err == nil || !strings.Contains(err.Error(), "verify") {
		t.Errorf("Add = %v, want a verify error", err)
	}
	if err := c.Replace("a", testRec{Name: "a", Lost: "x"}); err == nil {
		t.Error("Replace of a record that cannot round-trip succeeded")
	}
	engCheckFile(t, path, engFixture)
	if got := engKeys(c.List()); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("List = %v after the failures", got)
	}
	wantFiles := []string{"recs.toml", "recs.toml.lock"}
	if got := engListDir(t, filepath.Dir(path)); !reflect.DeepEqual(got, wantFiles) {
		t.Errorf("directory holds %v", got)
	}
}

// TestVerifyFailureOnNewFile checks that a failed first write creates no data file.
//
// Parameters:
//   - t: test to report to
func TestVerifyFailureOnNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recs.toml")
	c := engOpen(t, path)

	if err := c.Add(testRec{Name: "lost", Lost: "x"}); err == nil {
		t.Fatal("Add succeeded")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("data file exists after a failed first write, stat error = %v", err)
	}
	engNoTemp(t, filepath.Dir(path))
}

// TestRenameFailureLeavesNoTemp checks the cleanup when the final rename fails.
//
// Parameters:
//   - t: test to report to
func TestRenameFailureLeavesNoTemp(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	orig := renameFile
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { renameFile = orig })

	if err := c.Add(testRec{Name: "x"}); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("Add = %v, want the injected error", err)
	}
	engCheckFile(t, path, engFixture)
	if got := engKeys(c.List()); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("List = %v after the failure", got)
	}
}

// TestModesPreserved checks that an existing file and directory keep their permission bits.
//
// Parameters:
//   - t: test to report to
func TestModesPreserved(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "recs.toml")
	engExternal(t, path, engFixture)
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}

	c := engOpen(t, path)
	if err := c.Add(testRec{Name: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Replace("a", testRec{Name: "a", Desc: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete("b"); err != nil {
		t.Fatal(err)
	}

	if m := engMode(t, path); m != 0o664 {
		t.Errorf("file mode = %o, want 664", m)
	}
	if m := engMode(t, dir); m != 0o755 {
		t.Errorf("directory mode = %o, want 755", m)
	}
}

// parseCase is one bad file of TestParseErrors, with the position expected for its error.
type parseCase struct {
	name string
	in   string
	line int
	col  int
	msg  string
}

// TestParseErrors checks the read-only mode and the position of each kind of bad file.
//
// Parameters:
//   - t: test to report to
func TestParseErrors(t *testing.T) {
	cases := []parseCase{
		{"missing value", "schema = 1\n[[rec]]\nname = \n", 3, 8, "expected value"},
		{
			"duplicate toml key", "schema = 1\n\n[[rec]]\nname = \"a\"\nname = \"b\"\n", 5, 12,
			"already been defined",
		},
		{
			"wrong type", "schema = 1\n[[rec]]\nname = \"a\"\n\n[[rec]]\nname = 5\n", 6, 1,
			"incompatible types",
		},
		{"wrong type in a list", "[[rec]]\nname = \"a\"\ntags = 4\n", 3, 1, "incompatible types"},
		{"unknown schema", "schema = 2\n", 1, 1, "unsupported schema 2"},
		{
			"unknown schema later", "# c\n\nschema = 3\n[[rec]]\nname = \"a\"\n", 3, 1,
			"unsupported schema 3",
		},
		{"schema zero", "schema = 0\n", 1, 1, "unsupported schema 0"},
		{"schema not a number", "schema = \"x\"\n", 1, 1, "schema must be an integer"},
		{
			"duplicate record", "schema = 1\n[[rec]]\nname = \"a\"\n\n[[rec]]\nname = \"a\"\n", 5,
			1,
			"duplicate key \"a\"",
		},
		{"unterminated string", "schema = 1\n[[rec]]\ndesc = \"\"\"\nabc\n", 4, 4, "unexpected eof"},
		{"spaced header", "schema = 1\n[[ rec ]]\nname = \"a\"\n", 2, 1, "exactly as [[rec]]"},
		{"sub-table", "schema = 1\n[[rec]]\nname = \"a\"\n[rec.sub]\nk = 1\n", 4, 1, "sub-tables"},
		{"plain table", "schema = 1\n[rec]\nname = \"a\"\n", 2, 1, "array of tables"},
		{"inline array", "schema = 1\nrec = [{name = \"a\"}]\n", 0, 0, "[[rec]] blocks"},
		{"not toml", "this is not toml\n", 1, 0, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkParseError(t, tc) })
	}
}

// checkParseError opens a bad file and checks the error, the read-only mode and the file.
//
// Parameters:
//   - t: test to report to
//   - tc: case to run
func checkParseError(t *testing.T, tc parseCase) {
	t.Helper()

	path := engWrite(t, tc.in)
	c := engOpen(t, path)

	var pe *ParseError
	if !errors.As(c.Err(), &pe) {
		t.Fatalf("Err = %v, want a *ParseError", c.Err())
	}
	if pe.File != path || pe.Line != tc.line || (tc.col != 0 && pe.Column != tc.col) {
		t.Errorf("position = %s:%d:%d, want %s:%d:%d", pe.File, pe.Line, pe.Column, path,
			tc.line, tc.col)
	}
	if !strings.Contains(strings.ToLower(pe.Msg), tc.msg) || strings.HasSuffix(pe.Msg, ".") {
		t.Errorf("message %q, want it to contain %q without a final period", pe.Msg, tc.msg)
	}
	if len(c.List()) != 0 || len(c.Invalid()) != 0 {
		t.Errorf("a read-only collection must be empty: %v, %v", c.List(), c.Invalid())
	}

	writes := map[string]error{
		"Add":     c.Add(testRec{Name: "n"}),
		"Replace": c.Replace("a", testRec{Name: "a"}),
		"Delete":  c.Delete("a"),
	}
	for name, err := range writes {
		if !errors.Is(err, ErrReadOnly) {
			t.Errorf("%s = %v, want ErrReadOnly", name, err)
		}
		if !errors.As(err, &pe) {
			t.Errorf("%s error does not carry the ParseError: %v", name, err)
		}
	}
	engCheckFile(t, path, tc.in)
}

// TestParseErrorString checks the text of a ParseError with and without a position.
//
// Parameters:
//   - t: test to report to
func TestParseErrorString(t *testing.T) {
	cases := []struct {
		err  ParseError
		want string
	}{
		{ParseError{File: "f.toml", Line: 3, Column: 7, Msg: "bad"}, "f.toml:3:7: bad"},
		{ParseError{File: "f.toml", Line: 3, Msg: "bad"}, "f.toml:3: bad"},
		{ParseError{File: "f.toml", Msg: "bad"}, "f.toml: bad"},
	}

	for _, tc := range cases {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
	}
}

// TestOpenBadFileKeepsCollection checks that a bad file is a banner, not an Open error.
//
// Parameters:
//   - t: test to report to
func TestOpenBadFileKeepsCollection(t *testing.T) {
	c, err := Open[testRec](engWrite(t, "schema = 9\n"), testTable)
	if err != nil || c == nil || c.Err() == nil {
		t.Errorf("Open = %v, %v, Err = %v", c, err, c.Err())
	}
}

// TestFileOrderNeverSorted checks that records stay in file order whatever their keys.
//
// Parameters:
//   - t: test to report to
func TestFileOrderNeverSorted(t *testing.T) {
	path := engWrite(t, "schema = 1\n")
	c := engOpen(t, path)

	for _, name := range []string{"m", "z", "a", "q", "B"} {
		if err := c.Add(testRec{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"m", "z", "a", "q", "B"}
	if got := engKeys(engOpen(t, path).List()); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v", got)
	}
}

// engNasty are descriptions that stress the scanner and the encoder.
var engNasty = []string{
	"", "plain", "[[rec]]", "\"\"\"", "'''", "a\nb", "# c", "x = [", "\\", "\"", "'", "]",
	"line\r\nbreak", "tab\t", "caf\u00e9",
}

// TestRandomEdits checks a long random sequence of edits against a model of the records.
//
// Parameters:
//   - t: test to report to
func TestRandomEdits(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	path := engWrite(t, engFixture)
	c := engOpen(t, path)
	model := c.List()

	for step := range 300 {
		model = randomEdit(t, rng, c, model, step)

		fresh := engOpen(t, path)
		if fresh.Err() != nil || !sameRecords(fresh.List(), model) {
			t.Fatalf("step %d: file and model differ: %v, %v\n%s", step, fresh.Err(), model,
				engRead(t, path))
		}
		if !sameRecords(c.List(), model) {
			t.Fatalf("step %d: collection and model differ", step)
		}
	}
	engNoTemp(t, filepath.Dir(path))
	if !strings.HasPrefix(engRead(t, path), "# connections\n\nschema = 1\n\n# --- clients ---\n") {
		t.Error("the file header did not survive")
	}
}

// randomEdit performs one random edit on a collection and applies it to the model.
//
// Parameters:
//   - t: test to report to
//   - rng: source of randomness
//   - c: collection to edit
//   - model: records the collection must hold before the edit
//   - step: step number, used to build unique names
//
// Returns the model after the edit.
func randomEdit(t *testing.T, rng *rand.Rand, c *Collection[testRec], model []testRec,
	step int,
) []testRec {
	t.Helper()

	rec := testRec{
		Name: fmt.Sprintf("n%d", step),
		Desc: engNasty[rng.Intn(len(engNasty))],
		Tags: engNasty[:rng.Intn(3)],
	}
	if len(model) == 0 || rng.Intn(3) == 0 {
		if err := c.Add(rec); err != nil {
			t.Fatalf("step %d: Add = %v", step, err)
		}
		return append(model, rec)
	}

	i := rng.Intn(len(model))
	key := model[i].Name
	if rng.Intn(2) == 0 {
		if err := c.Delete(key); err != nil {
			t.Fatalf("step %d: Delete = %v", step, err)
		}
		return slices.Delete(model, i, i+1)
	}

	if err := c.Replace(key, rec); err != nil {
		t.Fatalf("step %d: Replace = %v", step, err)
	}
	model[i] = rec

	return model
}
