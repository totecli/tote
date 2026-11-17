package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// suggestManifest is a plugin whose command asks for a repository and a tag and lists the
// suggestions of the tag with the program lister.
const suggestManifest = `api = 1
id = "taglist"
version = "1.0.0"
tote = ">=1.0.0, <2"
requires_features = ["field.choices_from"]
title = "Tags"
description = "Lists the tags of a repository."
license = "MIT"
tier = "declarative"
permissions = ["exec:lister"]

[[command]]
id = "pick"
label = "Pick a tag"
run = ["lister", "--show", "--", "{name}", "{tag?}"]
output = "pager"

[[command.field]]
id = "name"
type = "text"
label = "Repository"
required = true
pattern = "^[a-z0-9/_-]+$"

[[command.field]]
id = "tag"
type = "text"
label = "Tag"
pattern = "^[A-Za-z0-9._-]{0,24}$"
choices_from = ["lister", "--tags", "{name}"]
`

// fakeLister plays the program of the suggestions and records every start and its directory. The
// value after --tags picks the behaviour (see the case block); OUT stands for the output directory
// of the sandbox.
const fakeLister = `#!/bin/sh
echo "$PWD|$*" >> "OUT/lister.log"
case "$1" in
--tags) ;;
*) echo "lister-out: $*"; exit 0;;
esac
echo $$ > "OUT/lister.pid"
case "$2" in
hang) exec sleep 30;;
slow) while [ ! -e "OUT/lister.go" ]; do sleep 0.05; done;;
fail) printf '\033[31mUnknown option: --tags\033[0m\n'; exit 1;;
esac
printf 'v1.0\tfirst\nv2.0\nv3.0\trecommended\n'
exit 0
`

// suggestSandbox returns a sandbox where the plugin taglist is installed with the command line and
// the fake lister is first on the PATH.
//
// Parameters:
//   - t: running test
func suggestSandbox(t *testing.T) *sandbox {
	t.Helper()

	sb := newSandbox(t)
	t.Cleanup(func() { openUp(sb.Dir) })

	path := filepath.Join(sb.Bin, "lister")
	sb.write(t, path, strings.ReplaceAll(fakeLister, "OUT", sb.Out))

	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(sb.Home, "plugins", "taglist")
	sb.write(t, filepath.Join(src, "plugin.toml"), suggestManifest)

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

// openPick opens the form of the command pick from the palette and types the repository.
//
// Parameters:
//   - repo: value of the repository field
func (m *term) openPick(repo string) {
	m.t.Helper()
	m.waitText("The project")
	m.key("C-k")
	m.waitText("Palette")
	m.typeText("pick a tag")
	m.key("Enter")
	m.waitText("Repository")
	m.typeText(repo)
	m.key("Tab")
	m.waitText("ctrl+l: suggestions")
}

// processGone reports whether a process no longer exists.
//
// Parameters:
//   - pid: process id
func processGone(pid int) bool {
	_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid)))

	return err != nil
}

// waitProcessGone waits for a process to disappear.
//
// Parameters:
//   - t: running test
//   - pid: process id
func waitProcessGone(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	for !processGone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the program %d is still alive", pid)
		}

		time.Sleep(pollEvery)
	}
}

// listerPid reads the process id that the fake lister recorded.
//
// Parameters:
//   - m: terminal
func listerPid(m *term) int {
	m.t.Helper()

	c := m.waitFile("lister.pid", func(c string) bool { return strings.TrimSpace(c) != "" })

	pid, err := strconv.Atoi(strings.TrimSpace(c))
	if err != nil {
		m.t.Fatal(err)
	}

	return pid
}

// TestSuggestionsBinary drives the suggestions of a field with the real binary, from Ctrl+L to
// the run of the command with the field as filled.
//
// Parameters:
//   - t: test to report to
func TestSuggestionsBinary(t *testing.T) {
	t.Parallel()

	sb := suggestSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.openPick("demo/proj")

	if _, err := os.Stat(filepath.Join(sb.Out, "lister.log")); err == nil {
		t.Fatal("the program started before the key was pressed")
	}

	tm.key("C-l")
	tm.waitText("Suggestions: Tag")
	tm.waitText("v1.0")
	tm.waitText("first")
	tm.waitText("recommended")

	log := tm.waitFile("lister.log", func(c string) bool { return strings.Contains(c, "--tags") })
	cache := filepath.Join(sb.Dir, "cache", "plugins", "taglist")

	if got := strings.TrimSpace(log); got != cache+"|--tags demo/proj" {
		t.Errorf("the program ran as %q, want it in the cache directory of the plugin %q", got, cache)
	}

	tm.key("Down", "Down", "Enter")
	tm.waitGone("Suggestions: Tag")
	tm.waitText("v3.0")

	// A second Ctrl+L answers from the memory: no second start.
	tm.key("C-l")
	tm.waitText("Suggestions: Tag")
	tm.key("Escape")
	tm.waitGone("Suggestions: Tag")

	if n := lines(filepath.Join(sb.Out, "lister.log")); n != 1 {
		t.Errorf("%d starts, want 1", n)
	}

	tm.typeText("x")
	tm.key("Enter")
	tm.waitText("lister-out: --show -- demo/proj v3.0x")
	tm.key("Escape")
	tm.waitText("The project")
	tm.quit()
}

// TestSuggestionsFailureBinary checks that a program that fails gives a readable reason without
// its colors and leaves the field free.
//
// Parameters:
//   - t: test to report to
func TestSuggestionsFailureBinary(t *testing.T) {
	t.Parallel()

	sb := suggestSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.openPick("fail")
	tm.key("C-l")
	tm.waitText("Unknown option: --tags")

	s := tm.look()
	if !s.has("(the program exited with status 1)") || strings.Contains(s.Text, "[31m") {
		t.Errorf("the failure is not readable:\n%s", s.Text)
	}

	tm.typeText("v9")
	tm.waitGone("Unknown option")
	tm.key("Escape")
	tm.waitText("The project")
	tm.quit()
}

// TestSuggestionsCancelBinary checks that Esc stops the program of the suggestions and keeps the
// form, that the box shows the exact command while it runs, and that the end of tote
// leaves no program of the plugin behind.
//
// Parameters:
//   - t: test to report to
func TestSuggestionsCancelBinary(t *testing.T) {
	t.Parallel()

	sb := suggestSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.openPick("hang")
	tm.key("C-l")
	tm.waitText("Loading")
	tm.waitText("lister --tags hang")

	pid := listerPid(tm)

	tm.key("Escape")
	tm.waitText("Listing stopped.")
	tm.waitText("Repository")
	waitProcessGone(t, pid)

	if err := os.Remove(filepath.Join(sb.Out, "lister.pid")); err != nil {
		t.Fatal(err)
	}

	// The same, ended by the end of tote itself.
	tm.key("C-l")
	tm.waitText("Loading")

	pid = listerPid(tm)

	tm.key("C-d")

	if code := tm.waitDone(); code != 0 {
		t.Errorf("exit status %d", code)
	}

	waitProcessGone(t, pid)
	tm.restored()

	if out, err := exec.Command("pgrep", "-x", "sleep").Output(); err == nil {
		for _, p := range strings.Fields(string(out)) {
			args, _ := os.ReadFile(filepath.Join("/proc", p, "cmdline"))
			if string(args) == "sleep\x0030\x00" {
				if cwd, err := os.Readlink(filepath.Join("/proc", p, "cwd")); err == nil &&
					strings.HasPrefix(cwd, sb.Dir) {
					t.Errorf("the program of the plugin outlived tote: pid %s", p)
				}
			}
		}
	}
}

// TestSuggestionsLoadingBinary checks the box of a slow listing: it opens at once with a loading
// row and the argv once (not repeated on the status line), then gives way to the list on answer.
//
// Parameters:
//   - t: test to report to
func TestSuggestionsLoadingBinary(t *testing.T) {
	t.Parallel()

	sb := suggestSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.openPick("slow")
	tm.key("C-l")
	tm.waitText("Suggestions: Tag")
	tm.waitText("Loading")
	tm.waitText("lister --tags slow")

	pid := listerPid(tm)

	if s := tm.look(); s.has("v1.0") || !s.has("Loading") || !s.has("cancel") ||
		strings.Count(s.Text, "lister --tags slow") != 1 {
		t.Errorf("the loading box is not what is expected:\n%s", s.Text)
	}

	// The spinner and the count of seconds are moved by the timer of the interface.
	tm.waitText("1 s")

	// A key typed while it loads goes nowhere, and a second Ctrl+L starts nothing.
	tm.typeText("x")
	tm.key("C-l")

	if n := lines(filepath.Join(sb.Out, "lister.log")); n != 1 {
		t.Errorf("%d starts while loading, want 1", n)
	}

	if err := os.WriteFile(filepath.Join(sb.Out, "lister.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tm.waitText("v1.0")
	tm.waitText("recommended")
	tm.waitGone("Loading")
	waitProcessGone(t, pid)

	// Esc on the list closes it, and the field is as it was: the typed key was dropped.
	tm.key("Escape")
	tm.waitGone("Suggestions: Tag")

	if s := tm.look(); !s.has("Repository") || regexp.MustCompile(`Tag\s+x`).MatchString(s.Text) {
		t.Errorf("the form changed while the box loaded:\n%s", s.Text)
	}

	tm.key("Escape")
	tm.waitText("The project")
	tm.quit()
}
