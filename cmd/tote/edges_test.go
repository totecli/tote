package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// brokenConns is a connections file that cannot be parsed.
const brokenConns = "schema = 1\n\n[[connection]]\nname = \"a\"\ntarget = \n"

// TestReviewConnectBrokenFile checks that opening a connection on an unreadable file fails
// without starting anything and without touching the file.
//
// Parameters:
//   - t: test to report to
func TestReviewConnectBrokenFile(t *testing.T) {
	for _, args := range [][]string{{"co", "a"}, {"co", "--", "a"}, {"co", "a", "root"}} {
		h := newHarness(t)
		h.write("connections.toml", brokenConns)

		code := h.run(args...)

		if code != 1 || len(h.execs) != 0 || h.out.Len() != 0 ||
			!strings.Contains(h.errb.String(), "connections.toml:5:10") {
			t.Errorf("%v: code %d, execs %v, stderr %q", args, code, h.execs, h.errb.String())
		}

		if h.read("connections.toml") != brokenConns {
			t.Errorf("%v: the file changed", args)
		}
	}
}

// TestReviewReposOpenFailures checks the refusals of "repos open".
//
// Parameters:
//   - t: test to report to
func TestReviewReposOpenFailures(t *testing.T) {
	h := newHarness(t)
	h.write("repos.toml", "schema = 1\n\n[[repo]]\nname = \"a\"\npath = \n")

	if code := h.run("repos", "open", "a"); code != 1 || h.out.Len() != 0 ||
		!strings.Contains(h.errb.String(), "repos.toml:5:8") {
		t.Errorf("broken file: code %d, stdout %q, stderr %q", code, h.out.String(), h.errb.String())
	}

	h = newHarness(t)
	h.fixtures()

	cases := []struct {
		args []string
		code int
	}{
		{[]string{"repos", "open"}, 2},
		{[]string{"repos", "open", "proj", "extra"}, 2},
		{[]string{"repos", "open", "--bogus", "proj"}, 2},
		{[]string{"repos", "open", "--help"}, 0},
		{[]string{"repos", "open", "nope"}, 1},
	}

	for _, c := range cases {
		if code := h.run(c.args...); code != c.code {
			t.Errorf("%v: code %d, want %d (stderr %q)", c.args, code, c.code, h.errb.String())
		}
	}
}

// TestReviewHandoffPathRefused checks that a refused hand-off file is an error and never a
// silent success, and that nothing reaches standard output.
//
// Parameters:
//   - t: test to report to
func TestReviewHandoffPathRefused(t *testing.T) {
	h := newHarness(t)
	h.fixtures()
	h.write("config.toml", "schema = 1\n\n[repos]\nafter_open = \"cd\"\n")

	docs := filepath.Join(h.dir, "docs")
	if err := os.MkdirAll(docs, 0o700); err != nil {
		t.Fatal(err)
	}

	h.write("repos.toml", "schema = 1\n\n[[repo]]\nname = \"docs\"\npath = \""+docs+"\"\n")

	target := filepath.Join(h.dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(h.dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_HANDOFF"] = link

	if code := h.run("repos", "open", "docs"); code != 1 || h.out.Len() != 0 {
		t.Errorf("symlink: code %d, stdout %q, stderr %q", code, h.out.String(), h.errb.String())
	}

	if data, _ := os.ReadFile(target); string(data) != "keep" {
		t.Errorf("the link target was written: %q", data)
	}

	h.vars["TOTE_HANDOFF"] = filepath.Join(h.dir, "missing")

	if code := h.run("repos", "open", "docs"); code != 0 || h.out.String() != docs+"\n" {
		t.Errorf("missing file: code %d, stdout %q, stderr %q", code, h.out.String(), h.errb.String())
	}

	if _, err := os.Stat(h.vars["TOTE_HANDOFF"]); err == nil {
		t.Error("a missing hand-off file was created")
	}
}

// TestReviewConfigVerb checks the refusals of "config".
//
// Parameters:
//   - t: test to report to
func TestReviewConfigVerb(t *testing.T) {
	h := newHarness(t)

	cases := []struct {
		args []string
		code int
		err  string
	}{
		{[]string{"config", "get"}, 2, `unknown subcommand "get"`},
		{[]string{"config", "--bogus"}, 2, "--bogus"},
		{[]string{"config"}, 2, "missing"},
		{[]string{"config", "path", "x"}, 2, "too many"},
		{[]string{"config", "-h"}, 0, ""},
	}

	for _, c := range cases {
		if code := h.run(c.args...); code != c.code || !strings.Contains(h.errb.String(), c.err) {
			t.Errorf("%v: code %d, stderr %q, want %d %q", c.args, code, h.errb.String(), c.code, c.err)
		}
	}
}

// TestReviewRelativeDirectories checks that an unusable directory override is an error for
// every verb that needs the files, with no panic and no file created in the working directory.
//
// Parameters:
//   - t: test to report to
func TestReviewRelativeDirectories(t *testing.T) {
	h := newHarness(t)
	h.vars["TOTE_CONFIG_DIR"] = "relative/dir"

	cases := [][]string{
		{"co"},
		{"co", "x"},
		{"co", "add", "n", "--target", "h"},
		{"co", "rm", "n", "-y"},
		{"alias"},
		{"repos", "open", "x"},
		{"funcs", "-l"},
		{"config", "path"},
		{"init", "bash"},
	}

	for _, args := range cases {
		if code := h.run(args...); code != 1 || len(h.execs) != 0 || h.out.Len() != 0 {
			t.Errorf("%v: code %d, execs %v, stdout %q", args, code, h.execs, h.out.String())
		}
	}

	if code := h.run("__complete", "co", ""); code != 0 || h.out.Len() != 0 {
		t.Errorf("__complete: code %d, stdout %q", code, h.out.String())
	}

	if _, err := os.Stat("relative"); err == nil {
		t.Error("a relative directory was created in the working directory")
	}
}

// TestReviewWriteErrors checks the message and the exit code of each store error on a write.
//
// Parameters:
//   - t: test to report to
func TestReviewWriteErrors(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{store.ErrExists, `connection "k" already exists`},
		{store.ErrNotFound, `connection "k" not found`},
		{store.ErrConflict, "connections.toml"},
		{errors.New("disk on fire"), "disk on fire"},
	}

	for _, c := range cases {
		h := newHarness(t)
		h.fixtures()

		a := *h.app
		s := connSpec()

		coll, rc := s.load(&a)
		if coll == nil {
			t.Fatalf("load: rc %d", rc)
		}

		wrapped := fmt.Errorf("wrapped: %w", c.err)

		if code := s.writeErr(&a, coll, "k", wrapped); code != 1 ||
			!strings.Contains(h.errb.String(), c.want) {
			t.Errorf("%v: code %d, stderr %q, want %q", c.err, code, h.errb.String(), c.want)
		}
	}
}

// TestReviewWriteErrorReadOnly checks that a write refused by a read-only collection reports the
// syntax error of the file and not the generic sentinel.
//
// Parameters:
//   - t: test to report to
func TestReviewWriteErrorReadOnly(t *testing.T) {
	h := newHarness(t)
	h.write("connections.toml", brokenConns)

	a := *h.app
	s := connSpec()

	paths, err := a.cfgPaths()
	if err != nil {
		t.Fatal(err)
	}

	coll, err := s.open(s.file(paths))
	if err != nil {
		t.Fatal(err)
	}

	err = coll.Add(store.Connection{Name: "n", Target: "h"})
	if !errors.Is(err, store.ErrReadOnly) {
		t.Fatalf("Add on a broken file = %v, want ErrReadOnly", err)
	}

	if code := s.writeErr(&a, coll, "n", err); code != 1 ||
		!strings.Contains(h.errb.String(), "connections.toml:5:10") {
		t.Errorf("code %d, stderr %q", code, h.errb.String())
	}
}

// TestReviewConcurrentEdit checks that a record removed by someone else between the lookup and
// the write is reported as a conflict or as missing, never as a success.
//
// Parameters:
//   - t: test to report to
func TestReviewConcurrentEdit(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	a := *h.app
	s := connSpec()

	coll, rc := s.load(&a)
	if coll == nil {
		t.Fatalf("load: rc %d", rc)
	}

	other := strings.Replace(fixtureConns, "name = \"db1\"", "name = \"db2\"", 1)
	h.write("connections.toml", other)

	err := coll.Delete("db1")
	if err == nil {
		t.Fatal("Delete of a record renamed on disk succeeded")
	}

	if code := s.writeErr(&a, coll, "db1", err); code != 1 || h.errb.Len() == 0 {
		t.Errorf("code %d, stderr %q", code, h.errb.String())
	}

	if h.read("connections.toml") != other {
		t.Error("the other writer's file was overwritten")
	}
}

// TestReviewConfirmAnswers checks the confirmation of a removal: only an explicit yes counts,
// and a closed input, an empty line or a longer sentence are refusals that leave the file alone.
//
// Parameters:
//   - t: test to report to
func TestReviewConfirmAnswers(t *testing.T) {
	for _, tt := range []struct {
		lang, stdin string
		code        int
	}{
		{"en", "", 1},
		{"en", "\n", 1},
		{"en", "yes please\n", 1},
		{"en", "o\n", 1},
		{"en", "no\n", 1},
		{"en", "YES\n", 0},
		{"en", "  y  \n", 0},
		{"fr", "", 1},
		{"fr", "yes please\n", 1},
		{"fr", "oui\n", 0},
		{"fr", "O\n", 0},
		{"fr", "y\n", 0},
		{"fr", "non\n", 1},
	} {
		h := newHarness(t)
		h.fixtures()
		h.vars["TOTE_LANG"] = tt.lang
		h.ttyIn, h.stdin = true, tt.stdin

		code := h.run("alias", "rm", "gs")
		removed := !strings.Contains(h.read("aliases.toml"), `name = "gs"`)

		if code != tt.code || removed != (tt.code == 0) {
			t.Errorf("%s %q: code %d removed %v, want code %d", tt.lang, tt.stdin, code, removed,
				tt.code)
		}
	}
}

// TestReviewHelpRefusals checks the misuse of the help and version verbs.
//
// Parameters:
//   - t: test to report to
func TestReviewHelpRefusals(t *testing.T) {
	h := newHarness(t)

	for _, args := range [][]string{
		{"help", "a", "b"},
		{"help", "--bogus"},
		{"version", "--bogus"},
		{"-v", "extra"},
		{"help", "nonexistentverb"},
		{"--help", "--bogus"},
	} {
		if code := h.run(args...); code == 0 {
			t.Errorf("%v: code 0, stdout %q", args, h.out.String())
		}
	}

	for _, args := range [][]string{{"help", "co"}, {"version", "-h"}, {"init", "-h"}} {
		if code := h.run(args...); code != 0 || h.out.Len() == 0 {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, h.out.String(), h.errb.String())
		}
	}
}

// TestReviewHostileNamesInErrors checks that a hostile handle never reaches the terminal raw in
// an error message.
//
// Parameters:
//   - t: test to report to
func TestReviewHostileNamesInErrors(t *testing.T) {
	h := newHarness(t)
	h.fixtures()
	hostile := "web\x1b]0;pwned\x07\u202e1"

	for _, args := range [][]string{
		{"co", hostile},
		{"alias", "rm", hostile, "-y"},
		{"repos", "open", hostile},
		{"funcs", "rm", hostile, "-y"},
	} {
		h.run(args...)

		if strings.ContainsAny(h.errb.String(), "\x1b\x07\u202e") ||
			strings.ContainsAny(h.out.String(), "\x1b\x07\u202e") {
			t.Errorf("%q: raw control characters in %q", args, h.errb.String())
		}
	}
}

// TestReviewCompleteWithBrokenConfig checks that completion stays silent and successful when
// the files are unreadable, because it runs inside the user's shell.
//
// Parameters:
//   - t: test to report to
func TestReviewCompleteWithBrokenConfig(t *testing.T) {
	h := newHarness(t)
	h.write("connections.toml", brokenConns)
	h.write("config.toml", "lang = \n")

	for _, args := range [][]string{{"__complete", "co", ""}, {"__complete", ""}, {"__complete"}} {
		if code := h.run(args...); code != 0 || h.errb.Len() != 0 {
			t.Errorf("%v: code %d, stderr %q", args, code, h.errb.String())
		}
	}
}
