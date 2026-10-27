package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// newPath returns a file path inside a directory that does not exist yet.
//
// Parameters:
//   - t: test handle
//   - name: file name
func newPath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "tote", name)
}

// readText returns the content of a file as a string.
//
// Parameters:
//   - t: test handle
//   - path: file to read
func readText(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

// writeText writes a file with mode 0600, creating nothing else.
//
// Parameters:
//   - t: test handle
//   - path: file to write, inside an existing directory
//   - text: content
func writeText(t *testing.T, path, text string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestOpenConnectionsLifecycle adds, reads, renames and deletes connections on a new file.
//
// Parameters:
//   - t: test to report to
func TestOpenConnectionsLifecycle(t *testing.T) {
	path := newPath(t, "connections.toml")

	c, err := OpenConnections(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if len(c.List()) != 0 || c.Err() != nil {
		t.Fatalf("new collection not empty: %v %v", c.List(), c.Err())
	}

	prod := Connection{
		Name: "prod", Via: "ssh", Target: "10.0.0.5", User: "root",
		Aliases: []string{"p", "web1"}, IP: "2001:db8::1", Desc: "Serveur prod",
	}
	review := Connection{Name: "odd", Target: "fe80::1", NeedsReview: true}

	for _, r := range []Connection{prod, review} {
		if err := c.Add(r); err != nil {
			t.Fatalf("add %s: %v", r.Name, err)
		}
	}

	again, err := OpenConnections(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	if got := again.List(); !reflect.DeepEqual(got, []Connection{prod, review}) {
		t.Errorf("reloaded = %+v", got)
	}

	if err := again.Replace("odd", Connection{Name: "odd2", Target: "h"}); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if _, ok := again.Get("odd"); ok {
		t.Error("old key still present after rename")
	}

	if err := again.Delete("prod"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if got := again.List(); len(got) != 1 || got[0].Name != "odd2" {
		t.Errorf("after delete = %+v", got)
	}
}

// TestOpenConnectionsFileMode checks the 0600 mode of the file and the 0700 mode of its parent.
//
// Parameters:
//   - t: test to report to
func TestOpenConnectionsFileMode(t *testing.T) {
	path := newPath(t, "connections.toml")

	c, err := OpenConnections(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := c.Add(Connection{Name: "a", Target: "h"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}

	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v", fi.Mode().Perm())
	}

	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}

	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v", di.Mode().Perm())
	}

	text := readText(t, path)
	if !strings.HasPrefix(text, "schema = 1\n") || !strings.Contains(text, "[[connection]]") {
		t.Errorf("unexpected file:\n%s", text)
	}
}

// TestOpenConnectionsRejectsInvalid checks that an invalid record is refused and never written.
//
// Parameters:
//   - t: test to report to
func TestOpenConnectionsRejectsInvalid(t *testing.T) {
	path := newPath(t, "connections.toml")

	c, err := OpenConnections(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	bad := []Connection{
		{Name: "a b", Target: "h"},
		{Name: "a", Target: "fe80::"},
		{Name: "a", Target: "h", Aliases: []string{"a"}},
		{Name: "a", Via: "x {nope}", Target: "h"},
	}

	for _, r := range bad {
		if err := c.Add(r); !errors.Is(err, ErrInvalidField) {
			t.Errorf("add %+v = %v, want ErrInvalidField", r, err)
		}
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file written for invalid records: %v", err)
	}
}

// TestOpenConnectionsKeys checks that keys are unique and compared case-sensitively.
//
// Parameters:
//   - t: test to report to
func TestOpenConnectionsKeys(t *testing.T) {
	c, err := OpenConnections(newPath(t, "connections.toml"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	for _, name := range []string{"prod", "Prod"} {
		if err := c.Add(Connection{Name: name, Target: "h"}); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}

	if err := c.Add(Connection{Name: "prod", Target: "other"}); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate add = %v, want ErrExists", err)
	}

	if err := c.Delete("PROD"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete wrong case = %v, want ErrNotFound", err)
	}

	if err := c.Replace("prod", Connection{Name: "Prod", Target: "h"}); !errors.Is(err, ErrExists) {
		t.Errorf("colliding rename = %v, want ErrExists", err)
	}
}

// TestOpenConnectionsEmptyAliases checks that an empty alias list is written and read back.
//
// Parameters:
//   - t: test to report to
func TestOpenConnectionsEmptyAliases(t *testing.T) {
	path := newPath(t, "connections.toml")

	c, err := OpenConnections(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := c.Add(Connection{Name: "a", Target: "h", Aliases: []string{}}); err != nil {
		t.Fatalf("add: %v", err)
	}

	assertAbsent(t, readText(t, path), "aliases")
}

// TestOpenConnectionsKeepsComments checks that comments and order survive a write.
//
// Parameters:
//   - t: test to report to
func TestOpenConnectionsKeepsComments(t *testing.T) {
	path := newPath(t, "connections.toml")
	writeText(t, path, `schema = 1

# --- clients ---
# the production box
[[connection]]
name = "zeta"
target = "z"

[[connection]]
name = "alpha"
target = "a"
`)

	c, err := OpenConnections(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := c.Add(Connection{Name: "mid", Target: "m"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	text := readText(t, path)
	if !strings.Contains(text, "# --- clients ---\n# the production box\n[[connection]]") {
		t.Errorf("comments lost:\n%s", text)
	}

	var names []string
	for _, r := range c.List() {
		names = append(names, r.Name)
	}

	if !reflect.DeepEqual(names, []string{"zeta", "alpha", "mid"}) {
		t.Errorf("order = %v", names)
	}
}

// TestOpenConnectionsInvalidKept checks that a hand-edited invalid record is listed, not trusted.
//
// Parameters:
//   - t: test to report to
func TestOpenConnectionsInvalidKept(t *testing.T) {
	path := newPath(t, "connections.toml")
	writeText(t, path, `schema = 1

[[connection]]
name = "good"
target = "h"

[[connection]]
name = "bad name"
target = "-oProxyCommand=id"
`)

	c, err := OpenConnections(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if c.Err() != nil {
		t.Fatalf("a Validate failure must not make the file read-only: %v", c.Err())
	}

	inv := c.Invalid()
	if _, ok := inv["bad name"]; !ok || len(inv) != 1 {
		t.Errorf("invalid = %v", inv)
	}

	if _, ok := inv["bad name"]; ok && !errors.Is(inv["bad name"], ErrInvalidField) {
		t.Errorf("invalid error does not wrap ErrInvalidField: %v", inv["bad name"])
	}
}

// TestOpenConnectionsReadOnly checks that a file that does not parse blocks every write.
//
// Parameters:
//   - t: test to report to
func TestOpenConnectionsReadOnly(t *testing.T) {
	path := newPath(t, "connections.toml")
	writeText(t, path, "schema = 1\n[[connection]]\nname = \"a\nbad\n")

	c, err := OpenConnections(path)
	if err != nil {
		t.Fatalf("open must not fail on a bad file: %v", err)
	}

	var pe *ParseError
	if !errors.As(c.Err(), &pe) || pe.Line == 0 {
		t.Fatalf("Err = %v, want a ParseError with a line", c.Err())
	}

	if err := c.Add(Connection{Name: "b", Target: "h"}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("add on bad file = %v, want ErrReadOnly", err)
	}
}

// TestOpenConnectionsIgnoresOtherTables checks that only the connection tables are read.
//
// Parameters:
//   - t: test to report to
func TestOpenConnectionsIgnoresOtherTables(t *testing.T) {
	path := newPath(t, "connections.toml")
	writeText(t, path, "schema = 1\n[[alias]]\nname = \"x\"\ncmd = \"y\"\n")

	c, err := OpenConnections(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if len(c.List()) != 0 {
		t.Errorf("read records of another table: %+v", c.List())
	}
}

// TestOpenAliases checks the alias collection and its table name.
//
// Parameters:
//   - t: test to report to
func TestOpenAliases(t *testing.T) {
	path := newPath(t, "aliases.toml")

	c, err := OpenAliases(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	want := []Alias{
		{Name: "ll", Cmd: "ls -l"},
		{Name: "..", Cmd: "cd ..", Desc: "up"},
	}

	for _, a := range want {
		if err := c.Add(a); err != nil {
			t.Fatalf("add %s: %v", a.Name, err)
		}
	}

	if err := c.Add(Alias{Name: "tote", Cmd: "x"}); !errors.Is(err, ErrInvalidField) {
		t.Errorf("reserved alias = %v, want ErrInvalidField", err)
	}

	if !strings.Contains(readText(t, path), "[[alias]]") {
		t.Error("alias table header missing")
	}

	again, err := OpenAliases(path)
	if err != nil || !reflect.DeepEqual(again.List(), want) {
		t.Errorf("reloaded = %+v, %v", again.List(), err)
	}

	if a, ok := again.Get(".."); !ok || a.Cmd != "cd .." {
		t.Errorf("Get(..) = %+v, %v", a, ok)
	}
}

// TestOpenRepos checks the repository collection and the verbatim storage of paths.
//
// Parameters:
//   - t: test to report to
func TestOpenRepos(t *testing.T) {
	path := newPath(t, "repos.toml")

	c, err := OpenRepos(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	want := []Repo{
		{Name: "proj", Path: "~/Documents/GitHub/Repositories/tote", Desc: "CLI"},
		{Name: "home", Path: "$HOME/x"},
	}

	for _, r := range want {
		if err := c.Add(r); err != nil {
			t.Fatalf("add %s: %v", r.Name, err)
		}
	}

	if err := c.Add(Repo{Name: "a b", Path: "~/x"}); !errors.Is(err, ErrInvalidField) {
		t.Errorf("bad repo name = %v, want ErrInvalidField", err)
	}

	again, err := OpenRepos(path)
	if err != nil || !reflect.DeepEqual(again.List(), want) {
		t.Errorf("reloaded = %+v, %v", again.List(), err)
	}

	if !strings.Contains(readText(t, path), "[[repo]]") {
		t.Error("repo table header missing")
	}
}

// TestOpenFuncs checks the function collection, its table name and the managed switch.
//
// Parameters:
//   - t: test to report to
func TestOpenFuncs(t *testing.T) {
	path := newPath(t, "funcs.toml")

	c, err := OpenFuncs(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	want := []Func{
		{Name: "mkcd", Desc: "mkdir + cd", Kind: "source", Script: "/usr/local/bin/mkcd"},
		{Name: "up", Kind: "exec", Script: "~/.local/bin/up", Managed: true},
	}

	for _, f := range want {
		if err := c.Add(f); err != nil {
			t.Fatalf("add %s: %v", f.Name, err)
		}
	}

	if err := c.Add(Func{Name: "x", Kind: "", Script: "s"}); !errors.Is(err, ErrInvalidField) {
		t.Errorf("empty kind = %v, want ErrInvalidField", err)
	}

	again, err := OpenFuncs(path)
	if err != nil || !reflect.DeepEqual(again.List(), want) {
		t.Errorf("reloaded = %+v, %v", again.List(), err)
	}

	if !strings.Contains(readText(t, path), "[[func]]") {
		t.Error("func table header missing")
	}
}
