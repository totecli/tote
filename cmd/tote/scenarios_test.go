package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errFullDisk is the error of a writer that refuses everything, like /dev/full.
var errFullDisk = errors.New("no space left on device")

// failWriter is a writer whose every write fails.
type failWriter struct{}

// Write fails like a full device does.
//
// Parameters:
//   - p: bytes to write, ignored
//
// Returns 0 and errFullDisk.
func (failWriter) Write(p []byte) (int, error) {
	return 0, errFullDisk
}

// TestReviewE2EWriteErrorIsReported checks that a list printed to a full device does not exit 0.
//
// Parameters:
//   - t: test to report to
func TestReviewE2EWriteErrorIsReported(t *testing.T) {
	h := newHarness(t)
	h.fixtures()
	h.app.Stdout = failWriter{}

	for _, args := range [][]string{{"co"}, {"alias"}, {"version"}, {"help"}} {
		if code := h.run(args...); code == 0 {
			t.Errorf("tote %v > /dev/full: code 0, want a failure", args)
		}
	}
}

// TestReviewE2EDuplicateAliasRefused checks that adding an alias that another connection already
// owns does not silently succeed, because "co web" would then pick the first host.
//
// Parameters:
//   - t: test to report to
func TestReviewE2EDuplicateAliasRefused(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	code := h.run("co", "add", "other", "--target", "10.9.9.9", "--alias", "web")
	if code == 0 && !strings.Contains(h.errb.String(), "web") {
		t.Errorf("alias \"web\" is already owned by web1: code 0 and no warning (stderr %q)",
			h.errb.String())
	}

	code = h.run("co", "add", "web", "--target", "10.9.9.9")
	if code == 0 && !strings.Contains(h.errb.String(), "web") {
		t.Errorf("name \"web\" is already an alias of web1: code 0 and no warning (stderr %q)",
			h.errb.String())
	}
}

// TestReviewE2EExactNameIgnoresCase checks that an exact name typed in another case is not
// ambiguous with a longer name that merely starts with it, as the design promises.
//
// Parameters:
//   - t: test to report to
func TestReviewE2EExactNameIgnoresCase(t *testing.T) {
	h := newHarness(t)
	h.write("connections.toml", `schema = 1

[[connection]]
name = "prod"
target = "10.0.0.1"

[[connection]]
name = "prod2"
target = "10.0.0.2"
`)

	if code := h.run("co", "PROD"); code != 0 {
		t.Fatalf("tote co PROD: code %d, stderr %q", code, h.errb.String())
	}

	if len(h.execs) != 1 || h.execs[0][len(h.execs[0])-1] != "10.0.0.1" {
		t.Errorf("execs = %v, want a connection to 10.0.0.1", h.execs)
	}
}

// TestReviewE2ERepoOpenMissingDirectory checks that opening a repository whose directory does
// not exist is reported instead of handing a doomed cd to the shell.
//
// Parameters:
//   - t: test to report to
func TestReviewE2ERepoOpenMissingDirectory(t *testing.T) {
	h := newHarness(t)
	h.write("config.toml", "schema = 1\n[repos]\nafter_open = \"cd\"\n")
	h.write("repos.toml", "schema = 1\n\n[[repo]]\nname = \"gone\"\npath = \"/nonexistent/dir\"\n")
	h.vars["TOTE_HANDOFF"] = filepath.Join(h.dir, "handoff")

	if err := os.WriteFile(h.vars["TOTE_HANDOFF"], nil, 0o600); err != nil {
		t.Fatal(err)
	}

	code := h.run("repos", "open", "gone")
	if code == 0 && h.errb.Len() == 0 {
		t.Errorf("code 0 and nothing on stderr for a missing directory")
	}
}

// TestReviewE2ERepoOpenDefaultSettings checks that, with the default settings and a wrapper
// installed, a repository alias changes the directory of the shell.
//
// Parameters:
//   - t: test to report to
func TestReviewE2ERepoOpenDefaultSettings(t *testing.T) {
	h := newHarness(t)
	h.write("repos.toml", "schema = 1\n\n[[repo]]\nname = \"here\"\npath = \"/tmp\"\n")
	file := filepath.Join(h.dir, "handoff")
	h.vars["TOTE_HANDOFF"] = file

	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if code := h.run("repos", "open", "here"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(data), "v1 cd") {
		t.Errorf("hand-off = %q, stdout = %q: the shell was not told to cd", data, h.out.String())
	}
}

// TestReviewE2ERelativeRepoPathRefused checks that a repository path that depends on the current
// directory is not stored silently.
//
// Parameters:
//   - t: test to report to
func TestReviewE2ERelativeRepoPathRefused(t *testing.T) {
	h := newHarness(t)

	code := h.run("repos", "add", "rel", "relative/path")
	if code == 0 && h.errb.Len() == 0 {
		t.Errorf("relative path accepted with code 0 and no warning")
	}
}

// TestReviewE2EBrokenConfigWarnsOnList checks that a broken config.toml is mentioned by the
// listing verbs too, not only when connecting.
//
// Parameters:
//   - t: test to report to
func TestReviewE2EBrokenConfigWarnsOnList(t *testing.T) {
	h := newHarness(t)
	h.fixtures()
	h.write("config.toml", "lang = \n")

	if code := h.run("co", "add", "extra", "--target", "h"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if !strings.Contains(h.errb.String(), "config.toml") {
		t.Errorf("stderr = %q, want a warning about config.toml", h.errb.String())
	}
}

// TestReviewE2EInitUnsupportedShellFromEnv checks that an unsupported $SHELL is named in the
// error instead of reporting a missing argument.
//
// Parameters:
//   - t: test to report to
func TestReviewE2EInitUnsupportedShellFromEnv(t *testing.T) {
	h := newHarness(t)
	h.vars["SHELL"] = "/usr/bin/fish"

	if code := h.run("init"); code == 0 {
		t.Fatalf("code 0 for fish")
	}

	if !strings.Contains(h.errb.String(), "fish") {
		t.Errorf("stderr = %q, want the shell name", h.errb.String())
	}
}
