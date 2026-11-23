package e2e

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// multilineManifest is a plugin whose command asks for a note on several lines and passes it to
// the program notetaker in one element of its argv, after a question.
const multilineManifest = `api = 1
id = "notes"
version = "1.0.0"
tote = ">=1.2.0, <2"
requires_features = ["field.multiline"]
title = "Notes"
description = "Keeps short notes."
license = "MIT"
tier = "declarative"
permissions = ["exec:notetaker"]

[[command]]
id = "add"
label = "Add a note"
run = ["notetaker", "add", "--body={body}"]
output = "pager"
confirm = "Keep this note?"

[[command.field]]
id = "body"
type = "text"
label = "Note"
required = true
multiline = true
lines = 3
`

// fakeNotetaker plays the program of the notes: it writes its arguments, each followed by a NUL,
// to a file that appears whole, so that a line feed inside an argument stays visible. OUT stands
// for the output directory of the sandbox.
const fakeNotetaker = `#!/bin/sh
printf '%s\0' "$@" > "OUT/argv.tmp"
mv "OUT/argv.tmp" "OUT/argv"
echo "notetaker-out: done"
exit 0
`

// multilineSandbox returns a sandbox where the plugin notes is installed with the command line and
// the fake notetaker is first on the PATH.
//
// Parameters:
//   - t: running test
func multilineSandbox(t *testing.T) *sandbox {
	t.Helper()

	sb := newSandbox(t)
	t.Cleanup(func() { openUp(sb.Dir) })

	path := filepath.Join(sb.Bin, "notetaker")
	sb.write(t, path, strings.ReplaceAll(fakeNotetaker, "OUT", sb.Out))

	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(sb.Home, "plugins", "notes")
	sb.write(t, filepath.Join(src, "plugin.toml"), multilineManifest)

	out, errs, rc := sb.run(t, "plugin", "check", src)
	if rc != 0 {
		t.Fatalf("check: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	token := ""

	for _, l := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(l, "grant token: "); ok {
			token = strings.TrimSpace(rest)
		}
	}

	if token == "" {
		t.Fatalf("no grant token in %q", out)
	}

	if out, errs, rc = sb.run(t, "plugin", "install", src, "--grant", token); rc != 0 {
		t.Fatalf("install: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	return sb
}

// TestMultilineBinary drives a multiline field with the real binary: two lines typed with Enter
// between them, Ctrl+S, the question that shows the line feed as a visible \n, the run, and the
// program that receives the value in one element of its argv with a real line feed.
//
// Parameters:
//   - t: test to report to
func TestMultilineBinary(t *testing.T) {
	t.Parallel()

	sb := multilineSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.waitText("The project")
	tm.key("C-k")
	tm.waitText("Palette")
	tm.typeText("add a note")
	tm.key("Enter")
	tm.waitText("ctrl+s run")

	// Enter starts a new line in the field instead of submitting the form.
	tm.typeText("first line")
	tm.key("Enter")
	tm.typeText("- second")
	tm.waitText("- second")

	if s := tm.look(); !s.has("first line") || s.has("Keep this note?") {
		t.Fatalf("the two lines are not in the form:\n%s", s.Text)
	}

	if _, err := os.Stat(filepath.Join(sb.Out, "argv")); err == nil {
		t.Fatal("the program ran before the form was submitted")
	}

	tm.key("C-s")
	tm.waitText("Keep this note?")

	// The whole command stands on one row, the line feed written as a visible \n.
	tm.waitText(`notetaker add '--body=first line\n- second'`)
	tm.key("Tab", "Enter")
	tm.waitText("notetaker-out: done")

	got := strings.Split(strings.TrimSuffix(tm.waitFile("argv", func(c string) bool {
		return c != ""
	}), "\x00"), "\x00")

	if want := []string{"add", "--body=first line\n- second"}; !slices.Equal(got, want) {
		t.Errorf("the program received %q, want %q", got, want)
	}

	tm.key("Escape")
	tm.waitGone("notetaker-out")
	tm.waitText("The project")
	tm.quit()
}
