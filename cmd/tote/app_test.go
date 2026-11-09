package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/i18n"
)

const (
	// fixtureConns holds three connections, one with an IPv6 target and one with a custom via.
	fixtureConns = `schema = 1

# --- clients ---
[[connection]]
name = "web1"
target = "10.0.0.1"
user = "deploy"
aliases = ["w", "web"]
desc = "Web front"

[[connection]]
name = "db1"
via = "mosh"
target = "db1.example.org"
aliases = ["db"]

[[connection]]
name = "v6"
target = "[2001:db8::1]"
`

	// fixtureAliases holds two aliases.
	fixtureAliases = `schema = 1

[[alias]]
name = "ll"
cmd = "ls -la"
desc = "Long list"

[[alias]]
name = "gs"
cmd = "git status"
`

	// fixtureRepos holds two repositories.
	fixtureRepos = `schema = 1

[[repo]]
name = "proj"
path = "~/code/proj"
desc = "Main project"

[[repo]]
name = "docs"
path = "/srv/docs"
`

	// fixtureFuncs holds two functions, one sourced and one executable.
	fixtureFuncs = `schema = 1

[[func]]
name = "hello"
kind = "source"
script = "~/bin/hello.sh"
managed = false

[[func]]
name = "tool"
desc = "A tool"
kind = "exec"
script = "/opt/tool/tool"
managed = true
`
)

// harness is an app wired to a temporary directory and to recorders.
//
// dir is the root of the sandbox, vars the environment seen by the app, execs the argument
// vectors the app asked to run, execErr the error the fake Exec returns, tty and ttyIn the
// answers of the terminal checks and stdin the text read as standard input. probes holds the
// commands that the fake "ssh -G" was asked about and probeOut and probeErr its answer; by
// default the lookup fails, so that a test never runs the ssh of the machine.
type harness struct {
	t        *testing.T
	app      *app
	out      *bytes.Buffer
	errb     *bytes.Buffer
	dir      string
	vars     map[string]string
	execs    [][]string
	execErr  error
	probes   [][]string
	probeOut string
	probeErr error
	tty      bool
	ttyIn    bool
	stdin    string
}

// newHarness builds a harness whose directories live in t.TempDir().
//
// Parameters:
//   - t: running test
func newHarness(t *testing.T) *harness {
	t.Helper()

	dir := t.TempDir()
	h := &harness{t: t, dir: dir, out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	h.vars = map[string]string{
		"TOTE_CONFIG_DIR": filepath.Join(dir, "config"),
		"TOTE_DATA_DIR":   filepath.Join(dir, "data"),
		"TOTE_STATE_DIR":  filepath.Join(dir, "state"),
		"TOTE_CACHE_DIR":  filepath.Join(dir, "cache"),
		"TOTE_LANG":       "en",
		"SHELL":           "/bin/bash",
	}
	h.app = &app{
		Stdout:          h.out,
		Stderr:          h.errb,
		Getenv:          func(k string) string { return h.vars[k] },
		Environ:         func() []string { return []string{"A=1"} },
		Home:            filepath.Join(dir, "home"),
		Version:         "1.2.3",
		Exec:            h.recordExec,
		IsTerminal:      func(io.Writer) bool { return h.tty },
		IsInputTerminal: func(io.Reader) bool { return h.ttyIn },
		Probe:           h.fakeProbe,
	}
	h.probeErr = errors.New("no ssh")

	return h
}

// fakeProbe is the fake "ssh -G" of the harness: it records the command and answers with the
// text and the error of the harness.
//
// Parameters:
//   - ctx: ignored
//   - argv: command that was asked about
//   - environ: ignored
func (h *harness) fakeProbe(_ context.Context, argv, _ []string) (string, error) {
	h.probes = append(h.probes, slices.Clone(argv))

	return h.probeOut, h.probeErr
}

// recordExec is the fake Exec of the harness: it records the command and never starts it.
//
// Parameters:
//   - argv: command and arguments
//   - env: environment, ignored
func (h *harness) recordExec(argv []string, env []string) error {
	h.execs = append(h.execs, slices.Clone(argv))

	return h.execErr
}

// run executes one command line on a fresh copy of the app, with empty output buffers.
//
// Parameters:
//   - args: command line without the program name
func (h *harness) run(args ...string) int {
	h.t.Helper()
	h.out.Reset()
	h.errb.Reset()
	h.execs = nil
	h.probes = nil

	a := *h.app
	a.Stdin = strings.NewReader(h.stdin)

	return a.run(args)
}

// path returns the path of a file in the config directory.
//
// Parameters:
//   - name: file name such as connections.toml
func (h *harness) path(name string) string {
	return filepath.Join(h.vars["TOTE_CONFIG_DIR"], name)
}

// write creates a file of the config directory.
//
// Parameters:
//   - name: file name such as connections.toml
//   - content: text to store
func (h *harness) write(name, content string) {
	h.t.Helper()

	if err := os.MkdirAll(h.vars["TOTE_CONFIG_DIR"], 0o700); err != nil {
		h.t.Fatal(err)
	}

	if err := os.WriteFile(h.path(name), []byte(content), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// read returns a file of the config directory, or "" when it does not exist.
//
// Parameters:
//   - name: file name such as connections.toml
func (h *harness) read(name string) string {
	h.t.Helper()

	data, err := os.ReadFile(h.path(name))
	if os.IsNotExist(err) {
		return ""
	}

	if err != nil {
		h.t.Fatal(err)
	}

	return string(data)
}

// fixtures writes the four fixture files.
func (h *harness) fixtures() {
	h.t.Helper()
	h.write("connections.toml", fixtureConns)
	h.write("aliases.toml", fixtureAliases)
	h.write("repos.toml", fixtureRepos)
	h.write("funcs.toml", fixtureFuncs)
}

// TestNoArgs checks that a bare "tote" prints the help on stdout.
//
// Parameters:
//   - t: test to report to
func TestNoArgs(t *testing.T) {
	h := newHarness(t)

	if code := h.run(); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}

	if !strings.Contains(h.out.String(), "Usage:") || h.errb.Len() != 0 {
		t.Errorf("stdout = %q, stderr = %q", h.out.String(), h.errb.String())
	}
}

// TestHelpListsEveryVerb checks that the help lists every visible verb in both languages and
// hides the completion verb.
//
// Parameters:
//   - t: test to report to
func TestHelpListsEveryVerb(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		h := newHarness(t)
		h.vars["TOTE_LANG"] = lang
		p := i18n.New(lang)

		for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
			if code := h.run(args...); code != 0 {
				t.Fatalf("%s %v: code = %d", lang, args, code)
			}

			for _, c := range commands() {
				if c.hidden {
					if strings.Contains(h.out.String(), "__complete") {
						t.Errorf("%s: help shows the hidden verb", lang)
					}

					continue
				}

				if !strings.Contains(h.out.String(), c.label) ||
					!strings.Contains(h.out.String(), p.T(c.desc)) {
					t.Errorf("%s %v: help lacks %q", lang, args, c.label)
				}
			}
		}
	}
}

// TestHelpVerb checks the help of each verb under every spelling, and with -h.
//
// Parameters:
//   - t: test to report to
func TestHelpVerb(t *testing.T) {
	h := newHarness(t)
	p := i18n.New("en")

	for _, c := range commands() {
		if c.hidden {
			continue
		}

		for _, name := range c.names {
			if code := h.run("help", name); code != 0 {
				t.Fatalf("help %s: code = %d", name, code)
			}

			for _, key := range c.usage {
				if !strings.Contains(h.out.String(), p.T(key)) {
					t.Errorf("help %s lacks the usage line %s", name, key)
				}
			}
		}
	}

	if code := h.run("co", "add", "-h"); code != 0 || !strings.Contains(h.out.String(), "tote co") {
		t.Errorf("co add -h: code %d, stdout %q", code, h.out.String())
	}

	if code := h.run("help", "nope"); code != 2 || h.out.Len() != 0 {
		t.Errorf("help nope: code %d, stdout %q", code, h.out.String())
	}
}

// TestUnknownCommand checks the exit code, the stream and the suggestion of an unknown verb.
//
// Parameters:
//   - t: test to report to
func TestUnknownCommand(t *testing.T) {
	tests := []struct {
		args    []string
		want    string
		suggest string
	}{
		{[]string{"nope"}, `unknown command "nope"`, ""},
		{[]string{"conections"}, `unknown command "conections"`, `did you mean "connections"?`},
		{[]string{"hlep"}, `unknown command "hlep"`, `did you mean "help"?`},
		{[]string{"vrsion"}, `unknown command "vrsion"`, `did you mean "version"?`},
		{[]string{"--nope"}, `unknown option "--nope"`, ""},
	}

	for _, tt := range tests {
		h := newHarness(t)

		if code := h.run(tt.args...); code != 2 {
			t.Errorf("%v: code = %d, want 2", tt.args, code)
		}

		if h.out.Len() != 0 {
			t.Errorf("%v: stdout = %q, want empty", tt.args, h.out.String())
		}

		if !strings.Contains(h.errb.String(), tt.want) {
			t.Errorf("%v: stderr = %q, want %q", tt.args, h.errb.String(), tt.want)
		}

		if tt.suggest != "" && !strings.Contains(h.errb.String(), tt.suggest) {
			t.Errorf("%v: stderr = %q, want %q", tt.args, h.errb.String(), tt.suggest)
		}

		if tt.suggest == "" && strings.Contains(h.errb.String(), "did you mean") {
			t.Errorf("%v: unexpected suggestion in %q", tt.args, h.errb.String())
		}
	}
}

// TestFrench checks that TOTE_LANG=fr translates help, errors and confirmations.
//
// Parameters:
//   - t: test to report to
func TestFrench(t *testing.T) {
	h := newHarness(t)
	h.vars["TOTE_LANG"] = "fr"
	p := i18n.New("fr")
	h.fixtures()

	h.run("nope")

	if want := p.T("cli_unknown_command", "nope"); !strings.Contains(h.errb.String(), want) {
		t.Errorf("stderr = %q, want %q", h.errb.String(), want)
	}

	h.run("co", "zzzz")

	if want := p.T("cli_no_connection", "zzzz"); !strings.Contains(h.errb.String(), want) {
		t.Errorf("stderr = %q, want %q", h.errb.String(), want)
	}

	h.run("alias", "add", "x", "y")

	if want := p.T("cli_added", p.T("kind_alias"), "x"); h.out.String() != want+"\n" {
		t.Errorf("stdout = %q, want %q", h.out.String(), want)
	}

	h.run("co", "-l", "--format", "table")

	if !strings.HasPrefix(h.out.String(), p.T("header_name")) ||
		!strings.Contains(h.out.String(), p.T("header_user")) {
		t.Errorf("table header is not French: %q", h.out.String())
	}

	h.ttyIn, h.stdin = true, "o\n"

	if code := h.run("alias", "rm", "x"); code != 0 {
		t.Errorf("rm with o: code %d, stderr %q", code, h.errb.String())
	}

	if !strings.Contains(h.errb.String(), "[o/N]") {
		t.Errorf("prompt = %q, want [o/N]", h.errb.String())
	}
}

// TestVersion checks the plain and the JSON forms of the version.
//
// Parameters:
//   - t: test to report to
func TestVersion(t *testing.T) {
	h := newHarness(t)

	for _, args := range [][]string{{"version"}, {"-v"}, {"--version"}} {
		if code := h.run(args...); code != 0 || h.out.String() != "tote 1.2.3\n" {
			t.Errorf("%v: code %d, stdout %q", args, code, h.out.String())
		}
	}

	if code := h.run("version", "--json"); code != 0 {
		t.Fatalf("version --json: code %d", code)
	}

	var info versionInfo
	if err := json.Unmarshal(h.out.Bytes(), &info); err != nil || info.Version != "1.2.3" {
		t.Errorf("json = %q (%v), info %+v", h.out.String(), err, info)
	}

	if code := h.run("version", "extra"); code != 2 {
		t.Errorf("version extra: code %d, want 2", code)
	}
}

// TestBrokenConfigWarns checks that an unusable config.toml is reported once and defaults apply.
//
// Parameters:
//   - t: test to report to
func TestBrokenConfigWarns(t *testing.T) {
	h := newHarness(t)
	h.fixtures()
	h.write("config.toml", "lang = \n")

	if code := h.run("co", "w"); code != 0 {
		t.Fatalf("code = %d, stderr %q", code, h.errb.String())
	}

	if n := strings.Count(h.errb.String(), "config.toml ignored"); n != 1 {
		t.Errorf("warnings = %d in %q, want 1", n, h.errb.String())
	}

	if len(h.execs) != 1 {
		t.Errorf("execs = %v, want one", h.execs)
	}
}

// errNoProgram is the error of the fake Exec when the program cannot start.
var errNoProgram = errors.New("executable file not found")

// TestTerminalChecks checks that only a real terminal counts: not a buffer, a file or the null
// device.
//
// Parameters:
//   - t: test to report to
func TestTerminalChecks(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) || isInputTerminal(strings.NewReader("")) {
		t.Error("an in-memory stream is not a terminal")
	}

	file, err := os.Create(filepath.Join(t.TempDir(), "plain"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	if isTerminal(file) || isInputTerminal(file) {
		t.Error("a regular file is not a terminal")
	}

	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()

	if isTerminal(null) || isInputTerminal(null) {
		t.Error("the null device is not a terminal")
	}
}

// TestExecProgramMissing checks the error of the production Exec for an unknown program, which
// is the only case that can be run without replacing the test process.
//
// Parameters:
//   - t: test to report to
func TestExecProgramMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if err := execProgram([]string{"tote-no-such-program"}, nil); err == nil {
		t.Error("execProgram found a program in an empty PATH")
	}
}

// TestNewApp checks the wiring of the production app.
//
// Parameters:
//   - t: test to report to
func TestNewApp(t *testing.T) {
	var out, errb bytes.Buffer

	a := newApp(&out, &errb)
	if a.Stdout != &out || a.Stderr != &errb || a.Exec == nil || a.Getenv == nil ||
		a.IsTerminal == nil || a.IsInputTerminal == nil || a.Environ == nil || a.Version != version {
		t.Errorf("newApp left a field unset: %+v", a)
	}
}
