package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/plugin"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/ui"
)

// TestLeaveVanishedHandoff checks that a hand-off file that no longer exists, which is what a
// suspension with Ctrl+Z leaves behind because the wrapper has already removed it, is treated
// like a missing wrapper: the directory or the command is printed and nothing fails.
//
// Parameters:
//   - t: test to report to
func TestLeaveVanishedHandoff(t *testing.T) {
	tests := []struct {
		name  string
		res   ui.Result
		stdin string
		hint  bool
	}{
		{"cd", ui.Result{Action: shell.ChangeDir, Payload: "/srv/docs"}, "/srv/docs\n", true},
		{"insert", ui.Result{Action: shell.Insert, Payload: "ls -l"}, "ls -l\n", true},
		{"reload", ui.Result{Reload: true}, "", false},
	}

	for _, tt := range tests {
		h := newHarness(t)
		p := withUI(h)
		p.res = tt.res
		h.vars["TOTE_HANDOFF"] = filepath.Join(h.dir, "gone")

		if rc := h.run(); rc != exitOK {
			t.Fatalf("%s: exit %d, stderr %q", tt.name, rc, h.errb.String())
		}

		if h.out.String() != tt.stdin {
			t.Errorf("%s: stdout = %q, want %q", tt.name, h.out.String(), tt.stdin)
		}

		if got := strings.Contains(h.errb.String(), "shell integration is not active"); got != tt.hint {
			t.Errorf("%s: hint = %v, stderr %q", tt.name, got, h.errb.String())
		}

		if strings.Contains(h.errb.String(), "error") {
			t.Errorf("%s: stderr = %q", tt.name, h.errb.String())
		}

		if _, err := os.Stat(h.vars["TOTE_HANDOFF"]); err == nil {
			t.Errorf("%s: the hand-off file was created", tt.name)
		}
	}
}

// TestRepoOpenVanishedHandoff checks "repos open" when the hand-off file vanished, and that a
// stored path that is not absolute is refused instead of being left to CDPATH.
//
// Parameters:
//   - t: test to report to
func TestRepoOpenVanishedHandoff(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.dir, "proj")

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	h.write("repos.toml", "schema = 1\n\n[[repo]]\nname = \"proj\"\npath = \""+dir+"\"\n"+
		"\n[[repo]]\nname = \"rel\"\npath = \"rel/dir\"\n")
	h.vars["TOTE_HANDOFF"] = filepath.Join(h.dir, "gone")
	h.tty = true

	if rc := h.run("repos", "open", "proj"); rc != exitOK || h.out.String() != dir+"\n" ||
		!strings.Contains(h.errb.String(), "shell integration is not active") {
		t.Errorf("vanished file: exit %d, stdout %q, stderr %q", rc, h.out.String(), h.errb.String())
	}

	if rc := h.run("repos", "open", "rel"); rc != exitFail || h.out.Len() != 0 ||
		!strings.Contains(h.errb.String(), "is not absolute: rel/dir") {
		t.Errorf("relative path: exit %d, stdout %q, stderr %q", rc, h.out.String(), h.errb.String())
	}

	file := filepath.Join(h.dir, "handoff")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_HANDOFF"] = file

	if rc := h.run("repos", "open", "rel"); rc != exitFail {
		t.Errorf("relative path with the wrapper: exit %d", rc)
	}

	if data, _ := os.ReadFile(file); len(data) != 0 {
		t.Errorf("a relative path was handed to the shell: %q", data)
	}
}

// TestUpdateUsageJSON checks that a misuse of "update --json" is the JSON error object, with
// nothing on standard error, and that without --json it stays a line on standard error.
//
// Parameters:
//   - t: test to report to
func TestUpdateUsageJSON(t *testing.T) {
	tests := []struct {
		name string
		args []string
		json bool
	}{
		{"unknown option", []string{"update", "--json", "--bogus"}, true},
		{"unknown option first", []string{"update", "--bogus", "--json"}, true},
		{"extra word", []string{"update", "--json", "now"}, true},
		{"no json", []string{"update", "--bogus"}, false},
		{"json after the end of the options", []string{"update", "--bogus", "--", "--json"}, false},
	}

	for _, tt := range tests {
		h := newHarness(t)

		if rc := h.run(tt.args...); rc != exitUsage {
			t.Errorf("%s: exit %d", tt.name, rc)
		}

		if !tt.json {
			if h.out.Len() != 0 || !strings.Contains(h.errb.String(), "tote: error:") {
				t.Errorf("%s: stdout %q, stderr %q", tt.name, h.out.String(), h.errb.String())
			}

			continue
		}

		var doc struct {
			Schema int    `json:"schema"`
			Status string `json:"status"`
			Error  struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Exit    int    `json:"exit"`
			} `json:"error"`
		}

		if err := json.Unmarshal(h.out.Bytes(), &doc); err != nil {
			t.Fatalf("%s: %v in %q", tt.name, err, h.out.String())
		}

		if doc.Schema != 1 || doc.Status != "error" || doc.Error.Code != "usage" ||
			doc.Error.Exit != exitUsage || doc.Error.Message == "" || h.errb.Len() != 0 {
			t.Errorf("%s: document %+v, stderr %q", tt.name, doc, h.errb.String())
		}
	}
}

// TestScrubURLs checks that an address loses its credentials, query and fragment, and that a text
// without one is not touched.
//
// Parameters:
//   - t: test to report to
func TestScrubURLs(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"/home/u/x.tar.gz", "/home/u/x.tar.gz"},
		{"https://github.com/o/r", "https://github.com/o/r"},
		{"https://user:pw@github.com/o/r/x.tgz", "https://github.com/o/r/x.tgz"},
		{"https://github.com/o/r/x.tgz?token=abc#frag", "https://github.com/o/r/x.tgz"},
		{"https://tok@host.example.org?k=v", "https://host.example.org"},
		{"https://a@b@host.example.org/p", "https://host.example.org/p"},
		{"https://u:p@[::1]:8443/p?q", "https://[::1]:8443/p"},
		{"https://%zz:p@host/p?q=1", "https://host/p"},
		{`Get "http://u:p@host/p?k=s": dial tcp: refused`, `Get "http://host/p": dial tcp: refused`},
		{"from https://u:p@h/a?x and file://u@h/b?y.", "from https://h/a and file://h/b"},
	}

	for _, tt := range tests {
		if got := scrubURLs(tt.in); got != tt.want {
			t.Errorf("scrubURLs(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestNoSecretInSourceErrors checks that a user and password, or a token in the query of an
// address typed by the user, is not printed back by the plugin verbs or by the update.
//
// Parameters:
//   - t: test to report to
func TestNoSecretInSourceErrors(t *testing.T) {
	for _, src := range []string{
		"https://user:SECRETPW@github.com/o/r/releases/download/v1/x.tar.gz",
		"https://github.com/o/r/releases/download/v1/x.tar.gz?token=SECRETQ",
		"http://user:SECRETPW@example.org/x.tar.gz?token=SECRETQ#SECRETF",
	} {
		for _, js := range []bool{true, false} {
			h := newHarness(t)
			args := []string{"plugin", "install", src}

			if js {
				args = append(args, "--json")
			}

			if rc := h.run(args...); rc == exitOK {
				t.Errorf("%s: exit %d", src, rc)
			}

			if all := h.out.String() + h.errb.String(); strings.Contains(all, "SECRET") ||
				all == "" {
				t.Errorf("%s json=%v: output %q", src, js, all)
			}
		}
	}

	h := newHarness(t)
	h.vars["TOTE_UPDATE_BASE"] = "http://user:SECRETPW@example.org/x?k=SECRETQ"

	rc := h.run("update", "--check")
	all := h.out.String() + h.errb.String()

	if rc == exitOK || strings.Contains(all, "SECRET") || !strings.Contains(all, "example.org/x") {
		t.Errorf("update: exit %d, output %q", rc, all)
	}

	h = newHarness(t)
	h.vars["TOTE_UPDATE_BASE"] = "https://user:SECRETPW@127.0.0.1:1/x?k=SECRETQ"

	for _, js := range []bool{false, true} {
		h.out.Reset()
		h.errb.Reset()

		args := []string{"update", "--check"}
		if js {
			args = append(args, "--json")
		}

		rc = h.run(args...)
		all = h.out.String() + h.errb.String()

		if rc == exitOK || strings.Contains(all, "SECRET") || all == "" {
			t.Errorf("update offline json=%v: exit %d, output %q", js, rc, all)
		}
	}
}

// TestPluginInterrupted checks that a signal during a plugin command is the exit code 130 with a
// translated message and the code "interrupted" in the JSON object, whatever layer noticed it.
//
// Parameters:
//   - t: test to report to
func TestPluginInterrupted(t *testing.T) {
	cancelled := func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		return ctx, cancel
	}

	src := "https://github.com/o/r/releases/download/v1/x.tar.gz"
	sum := strings.Repeat("0", 64)

	for _, lang := range []string{"en", "fr"} {
		h := newHarness(t)
		h.vars["TOTE_LANG"] = lang
		h.app.Context = cancelled

		rc := h.run("plugin", "install", src, "--sha256", sum, "--dry-run")
		if rc != exitInterrupted || strings.Contains(h.errb.String(), "context canceled") {
			t.Errorf("%s: exit %d, stderr %q", lang, rc, h.errb.String())
		}

		want := map[string]string{"en": "interrupted", "fr": "interrompu"}[lang]
		if !strings.Contains(h.errb.String(), want) {
			t.Errorf("%s: stderr %q lacks %q", lang, h.errb.String(), want)
		}
	}

	h := newHarness(t)
	h.app.Context = cancelled

	rc := h.run("plugin", "install", src, "--sha256", sum, "--dry-run", "--json")

	var doc struct {
		Error struct {
			Code string `json:"code"`
			Exit int    `json:"exit"`
		} `json:"error"`
	}

	if err := json.Unmarshal(h.out.Bytes(), &doc); err != nil {
		t.Fatalf("%v in %q", err, h.out.String())
	}

	if rc != exitInterrupted || doc.Error.Code != "interrupted" || doc.Error.Exit != exitInterrupted ||
		h.errb.Len() != 0 {
		t.Errorf("json: exit %d, document %+v, stderr %q", rc, doc, h.errb.String())
	}

	wrapped := &plugin.Error{Code: plugin.CodeNetOffline, Args: []string{"h"}, Err: context.Canceled}
	if code, _, exit := h.app.describe(wrapped); code != "interrupted" || exit != exitInterrupted {
		t.Errorf("wrapped cancellation: %q, %d", code, exit)
	}

	if code, _, exit := h.app.describe(errors.New("other")); code == "interrupted" ||
		exit == exitInterrupted {
		t.Errorf("another error: %q, %d", code, exit)
	}
}

// TestCompleteEverySpelling checks that every spelling of a verb gets the options and the
// connection names of its canonical spelling, over the whole command table.
//
// Parameters:
//   - t: test to report to
func TestCompleteEverySpelling(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	for _, c := range commands() {
		if c.hidden || len(c.names) < 2 {
			continue
		}

		canon := c.names[0]

		for _, name := range c.names[1:] {
			for _, words := range [][]string{{"--"}, {""}, {"-"}, {"--j"}, {"w"}, {"web1", ""}} {
				want := completeLines(t, h, append([]string{canon}, words...)...)
				got := completeLines(t, h, append([]string{name}, words...)...)

				if !slices.Equal(got, want) {
					t.Errorf("%s %q = %q, but %s gives %q", name, words, got, canon, want)
				}
			}
		}
	}

	for _, name := range []string{"repo", "r", "a", "aliases", "f", "func", "conn", "connexions"} {
		if got := completeLines(t, h, name, "--"); !slices.Equal(got, []string{"--format", "--json"}) {
			t.Errorf("%s -- = %q", name, got)
		}
	}

	if got := names(completeLines(t, h, "plugins", "")); !slices.Contains(got, "list") {
		t.Errorf("plugins '' = %q", got)
	}

	if got := names(completeLines(t, h, "connexions", "")); !slices.Contains(got, "web1") {
		t.Errorf("connexions '' = %q", got)
	}
}

// TestStoreErrorsTranslated checks that the refusals of the store that come from a mistake of
// the user are in the language of the run.
//
// Parameters:
//   - t: test to report to
func TestStoreErrorsTranslated(t *testing.T) {
	h := newHarness(t)
	h.vars["TOTE_LANG"] = "fr"

	if rc := h.run("co", "add", "a b", "--target", "h"); rc != exitFail ||
		!strings.Contains(h.errb.String(), "doit commencer par une lettre") ||
		strings.Contains(h.errb.String(), "must ") || strings.Contains(h.errb.String(), "invalid") {
		t.Errorf("name: exit %d, stderr %q", rc, h.errb.String())
	}

	if rc := h.run("co", "add", "x"); rc != exitUsage && rc != exitFail ||
		strings.Contains(h.errb.String(), "must not") {
		t.Errorf("target: exit %d, stderr %q", rc, h.errb.String())
	}

	if rc := h.run("co", "add", "x", "--target", "-o"); rc != exitFail ||
		!strings.Contains(h.errb.String(), "cible") ||
		!strings.Contains(h.errb.String(), "ne doit pas commencer par -") {
		t.Errorf("target dash: exit %d, stderr %q", rc, h.errb.String())
	}

	h.write("aliases.toml", "schema = 2\n")

	if rc := h.run("alias", "add", "ll", "ls"); rc != exitFail ||
		!strings.Contains(h.errb.String(), "sch\u00e9ma 2 non pris en charge") ||
		strings.Contains(h.errb.String(), "unsupported") {
		t.Errorf("schema: exit %d, stderr %q", rc, h.errb.String())
	}

	h.write("repos.toml", "schema = 1\n")

	if err := os.Chmod(h.path("repos.toml"), 0o400); err != nil {
		t.Fatal(err)
	}

	if rc := h.run("repos", "add", "r", "/tmp"); os.Getuid() != 0 &&
		(rc != exitFail || !strings.Contains(h.errb.String(), "lecture seule") ||
			strings.Contains(h.errb.String(), "read-only")) {
		t.Errorf("read-only file: exit %d, stderr %q", rc, h.errb.String())
	}

	for _, args := range [][]string{
		{"funcs", "add", "f", "--script", "/s", "--kind", "bad"},
		{"co", "add", "x", "--target", "h", "--via", "ssh 'a'"},
	} {
		if rc := h.run(args...); rc != exitFail || strings.Contains(h.errb.String(), "must ") ||
			strings.Contains(h.errb.String(), "are not interpreted") {
			t.Errorf("%q: exit %d, stderr %q", args, rc, h.errb.String())
		}
	}

	h.vars["TOTE_LANG"] = "en"

	if rc := h.run("co", "add", "a b", "--target", "h"); rc != exitFail ||
		!strings.Contains(h.errb.String(), `invalid connection "a b": name: must start with`) {
		t.Errorf("en: exit %d, stderr %q", rc, h.errb.String())
	}
}
