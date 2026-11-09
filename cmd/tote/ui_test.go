package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/ui"
	"github.com/totecli/tote/internal/update"
)

// uiProbe records how the interface was started and returns a canned outcome.
//
// calls holds the options of each start, res and err what the fake interface returns.
type uiProbe struct {
	calls []ui.Options
	res   ui.Result
	err   error
}

// run is the fake ui.Run of the tests.
//
// Parameters:
//   - o: options given by the command
func (p *uiProbe) run(o ui.Options) (ui.Result, error) {
	p.calls = append(p.calls, o)

	return p.res, p.err
}

// withUI wires a probe into a harness and gives it a terminal.
//
// Parameters:
//   - h: harness to change
func withUI(h *harness) *uiProbe {
	p := &uiProbe{}
	h.app.UI = p.run
	h.tty, h.ttyIn = true, true

	return p
}

// TestBareToteOpensInterface checks that a bare command on a terminal starts the interface on
// the welcome page with the right options.
//
// Parameters:
//   - t: test to report to
func TestBareToteOpensInterface(t *testing.T) {
	h := newHarness(t)
	p := withUI(h)
	h.write("connections.toml", fixtureConns)

	if rc := h.run(); rc != exitOK {
		t.Fatalf("exit %d, stderr %q", rc, h.errb.String())
	}

	if len(p.calls) != 1 {
		t.Fatalf("the interface was started %d times", len(p.calls))
	}

	o := p.calls[0]

	if o.Start != ui.StartHome || o.ASCII || o.Version != "1.2.3" || o.Printer == nil ||
		o.Paths.ConnectionsFile() != h.path("connections.toml") || o.Home != h.app.Home {
		t.Errorf("options = %+v", o)
	}

	if h.out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", h.out.String())
	}
}

// TestBareToteWithoutTerminal checks that the help is printed unless both ends are terminals.
//
// Parameters:
//   - t: test to report to
func TestBareToteWithoutTerminal(t *testing.T) {
	for _, tt := range []struct{ out, in bool }{{false, false}, {true, false}, {false, true}} {
		h := newHarness(t)
		p := withUI(h)
		h.tty, h.ttyIn = tt.out, tt.in

		if rc := h.run(); rc != exitOK {
			t.Errorf("out %v in %v: exit %d", tt.out, tt.in, rc)
		}

		if len(p.calls) != 0 {
			t.Errorf("out %v in %v: the interface was started", tt.out, tt.in)
		}

		if !strings.Contains(h.out.String(), "Usage:") {
			t.Errorf("out %v in %v: no help:\n%s", tt.out, tt.in, h.out.String())
		}
	}
}

// TestAsciiFlag checks that --ascii reaches the interface and is ignored by other commands.
//
// Parameters:
//   - t: test to report to
func TestAsciiFlag(t *testing.T) {
	h := newHarness(t)
	p := withUI(h)

	if rc := h.run("--ascii"); rc != exitOK || len(p.calls) != 1 || !p.calls[0].ASCII {
		t.Errorf("--ascii: exit %d calls %+v", rc, p.calls)
	}

	if rc := h.run("--ascii", "--ascii", "co"); rc != exitOK || len(p.calls) != 2 ||
		!p.calls[1].ASCII || p.calls[1].Start != ui.StartConnections {
		t.Errorf("--ascii co: exit %d calls %+v", rc, p.calls)
	}

	h.tty = false

	if rc := h.run("--ascii", "version"); rc != exitOK || len(p.calls) != 2 {
		t.Errorf("--ascii version: exit %d calls %d", rc, len(p.calls))
	}

	if !strings.Contains(h.out.String(), "1.2.3") {
		t.Errorf("version output = %q", h.out.String())
	}
}

// TestConnectionsOpensInterface checks which forms of "tote co" open the interface.
//
// Parameters:
//   - t: test to report to
func TestConnectionsOpensInterface(t *testing.T) {
	tests := []struct {
		name string
		args []string
		tty  bool
		ui   bool
	}{
		{"no argument on a terminal", []string{"co"}, true, true},
		{"alias of the command", []string{"connection"}, true, false},
		{"list flag", []string{"co", "-l"}, true, false},
		{"format flag", []string{"co", "--format", "json"}, true, false},
		{"help", []string{"co", "--help"}, true, false},
		{"a connection", []string{"co", "web1"}, true, false},
		{"no terminal", []string{"co"}, false, false},
	}

	for _, tt := range tests {
		h := newHarness(t)
		p := withUI(h)
		h.tty, h.ttyIn = tt.tty, tt.tty
		h.write("connections.toml", fixtureConns)

		h.run(tt.args...)

		if got := len(p.calls) == 1; got != tt.ui {
			t.Errorf("%s: interface started = %v, want %v", tt.name, got, tt.ui)
		}

		if tt.ui && p.calls[0].Start != ui.StartConnections {
			t.Errorf("%s: start = %d", tt.name, p.calls[0].Start)
		}
	}
}

// TestUIErrors checks the exit codes of an interface that stops.
//
// Parameters:
//   - t: test to report to
func TestUIErrors(t *testing.T) {
	h := newHarness(t)
	p := withUI(h)

	p.err = ui.ErrInterrupted

	if rc := h.run(); rc != exitInterrupted || h.errb.Len() != 0 {
		t.Errorf("interrupted: exit %d stderr %q", rc, h.errb.String())
	}

	p.err = errors.New("boom\x1b[2J")

	rc := h.run()

	if rc != exitFail || !strings.Contains(h.errb.String(), "the interface stopped: boom?[2J") {
		t.Errorf("failure: exit %d stderr %q", rc, h.errb.String())
	}
}

// TestLeaveHandoff checks that the result of the interface reaches the wrapper through the
// hand-off file.
//
// Parameters:
//   - t: test to report to
func TestLeaveHandoff(t *testing.T) {
	tests := []struct {
		name string
		res  ui.Result
		want shell.Handoff
	}{
		{
			"insert",
			ui.Result{Action: shell.Insert, Payload: "ls -la"},
			shell.Handoff{Action: shell.Insert, Payload: "ls -la"},
		},
		{
			"cd",
			ui.Result{Action: shell.ChangeDir, Payload: "/srv/docs", Reload: true},
			shell.Handoff{Action: shell.ChangeDir, Payload: "/srv/docs", Reload: true},
		},
		{"reload only", ui.Result{Reload: true}, shell.Handoff{Action: shell.None, Reload: true}},
	}

	for _, tt := range tests {
		h := newHarness(t)
		p := withUI(h)
		p.res = tt.res

		file := filepath.Join(t.TempDir(), "handoff")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		h.vars["TOTE_HANDOFF"] = file

		if rc := h.run(); rc != exitOK {
			t.Fatalf("%s: exit %d, stderr %q", tt.name, rc, h.errb.String())
		}

		got, err := shell.ReadHandoff(file)
		if err != nil || got != tt.want {
			t.Errorf("%s: handoff %+v, %v, want %+v", tt.name, got, err, tt.want)
		}

		if h.out.Len() != 0 {
			t.Errorf("%s: stdout = %q", tt.name, h.out.String())
		}
	}
}

// TestLeaveWithoutWrapper checks that the result is not lost when the shell integration is off.
//
// Parameters:
//   - t: test to report to
func TestLeaveWithoutWrapper(t *testing.T) {
	h := newHarness(t)
	p := withUI(h)
	p.res = ui.Result{Action: shell.ChangeDir, Payload: "/srv/\x1b[2Jdocs"}

	if rc := h.run(); rc != exitOK {
		t.Fatalf("exit %d", rc)
	}

	if got := h.out.String(); got != "/srv/?[2Jdocs\n" {
		t.Errorf("stdout = %q, want the sanitized directory", got)
	}

	if !strings.Contains(h.errb.String(), "shell integration is not active") {
		t.Errorf("no hint on a terminal: %q", h.errb.String())
	}

	p.res = ui.Result{Reload: true}
	h.tty, h.ttyIn = true, true

	if rc := h.run(); rc != exitOK || h.out.Len() != 0 || h.errb.Len() != 0 {
		t.Errorf("reload only: exit %d stdout %q stderr %q", rc, h.out.String(), h.errb.String())
	}

	p.res = ui.Result{}

	if rc := h.run(); rc != exitOK || h.out.Len() != 0 || h.errb.Len() != 0 {
		t.Errorf("empty: exit %d stdout %q stderr %q", rc, h.out.String(), h.errb.String())
	}
}

// TestLeaveRefusedHandoff checks that a hand-off file that is not the wrapper's is refused.
//
// Parameters:
//   - t: test to report to
func TestLeaveRefusedHandoff(t *testing.T) {
	h := newHarness(t)
	p := withUI(h)
	p.res = ui.Result{Action: shell.Insert, Payload: "ls"}

	file := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(file, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_HANDOFF"] = file

	if rc := h.run(); rc != exitFail {
		t.Errorf("exit %d, want %d", rc, exitFail)
	}

	if data, _ := os.ReadFile(file); string(data) != "precious" {
		t.Errorf("the file was changed: %q", data)
	}
}

// TestHelpMentionsInterface checks that the help documents the interface, --ascii and
// TOTE_COLOR in both languages.
//
// Parameters:
//   - t: test to report to
func TestHelpMentionsInterface(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		h := newHarness(t)
		h.vars["TOTE_LANG"] = lang

		h.run("help")

		for _, want := range []string{"--ascii", "TOTE_COLOR", "NO_COLOR"} {
			if !strings.Contains(h.out.String(), want) {
				t.Errorf("%s: help lacks %q:\n%s", lang, want, h.out.String())
			}
		}

		h.run("help", "co")

		if !strings.Contains(h.out.String(), "tote co") ||
			!strings.Contains(h.out.String(), "interface") {
			t.Errorf("%s: co help does not mention the interface:\n%s", lang, h.out.String())
		}
	}
}

// TestCheckUpdate checks the callback behind the key "U": every outcome of the updater, the
// package manager case, the failure messages and that nothing is installed.
//
// Parameters:
//   - t: test to report to
func TestCheckUpdate(t *testing.T) {
	tests := []struct {
		name    string
		version string
		tags    []string
		status  string
		latest  string
	}{
		{"available", "1.0.0", []string{"1.0.0", "1.1.0"}, "available", "1.1.0"},
		{"up to date", "1.1.0", []string{"1.1.0"}, "up-to-date", "1.1.0"},
		{"newer", "1.2.0", []string{"1.1.0"}, "newer", "1.1.0"},
		{"dev", "dev", []string{"1.1.0"}, "dev", "1.1.0"},
	}

	for _, tt := range tests {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))

		for _, tag := range tt.tags {
			m.publish(tag)
		}

		target := installFake(h, m, tt.version)
		before, _ := os.ReadFile(target)
		a := *h.app

		got, err := a.checkUpdate(context.Background())
		if err != nil || got.Status != tt.status || got.Latest != tt.latest ||
			got.Current != tt.version {
			t.Errorf("%s: %+v, %v", tt.name, got, err)
		}

		if after, _ := os.ReadFile(target); string(after) != string(before) {
			t.Errorf("%s: the check changed the binary", tt.name)
		}
	}

	t.Run("ignores update.check", func(t *testing.T) {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))
		m.publish("1.1.0")
		installFake(h, m, "1.0.0")
		h.write("config.toml", "schema = 1\n[update]\ncheck = false\n")

		a := *h.app
		got, err := a.checkUpdate(context.Background())
		if err != nil || got.Status != "available" {
			t.Errorf("check with update.check = false: %+v, %v", got, err)
		}
	})

	t.Run("managed", func(t *testing.T) {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))
		m.publish("2.0.0")
		target := installFake(h, m, "1.0.0")
		writeReceipt(h, update.Receipt{InstallMethod: "brew", Path: target})

		a := *h.app
		got, err := a.checkUpdate(context.Background())
		if err != nil || got.Status != "managed" || got.Command != "brew upgrade tote" {
			t.Errorf("managed: %+v, %v", got, err)
		}
	})

	t.Run("offline", func(t *testing.T) {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))
		m.publish("1.1.0")
		installFake(h, m, "1.0.0")
		h.vars["TOTE_UPDATE_BASE"] = "file://" + filepath.Join(h.dir, "gone")

		a := *h.app
		_, err := a.checkUpdate(context.Background())

		if err == nil || strings.Contains(err.Error(), "cli_") ||
			strings.TrimSpace(err.Error()) == "" {
			t.Errorf("offline: error %v is not a message", err)
		}
	})

	t.Run("wired", func(t *testing.T) {
		h := newHarness(t)
		p := withUI(h)

		if code := h.run(); code != 0 {
			t.Fatalf("code %d, stderr %q", code, h.errb.String())
		}

		if len(p.calls) != 1 || p.calls[0].CheckUpdate == nil || p.calls[0].UpdateNotice == nil {
			t.Errorf("the interface lacks its update callbacks: %+v", p.calls)
		}
	})
}

// TestUnknownColorWarning checks that a value of TOTE_COLOR that names no profile is reported
// once on stderr before the interface starts, that it does not stop it, and that the words
// for "no color" are understood without a warning.
//
// Parameters:
//   - t: test to report to
func TestUnknownColorWarning(t *testing.T) {
	tests := []struct {
		name string
		env  string
		lang string
		want string
	}{
		{
			"unknown", "TOTE_COLOR=rainbow", "en",
			"tote: unknown TOTE_COLOR value \"rainbow\", using automatic detection\n",
		},
		{
			"unknown in French", "TOTE_COLOR=rainbow", "fr",
			"tote: valeur inconnue de TOTE_COLOR",
		},
		{"never", "TOTE_COLOR=never", "en", ""},
		{"off", "TOTE_COLOR=off", "en", ""},
		{"no", "TOTE_COLOR=no", "en", ""},
		{"false", "TOTE_COLOR=false", "en", ""},
		{"zero", "TOTE_COLOR=0", "en", ""},
		{"none", "TOTE_COLOR=none", "en", ""},
		{"truecolor", "TOTE_COLOR=truecolor", "en", ""},
		{"auto", "TOTE_COLOR=auto", "en", ""},
		{"always", "TOTE_COLOR=always", "en", ""},
		{"empty", "TOTE_COLOR=", "en", ""},
		{"unset", "A=1", "en", ""},
		{"escape sequence", "TOTE_COLOR=\x1b[2Jx", "en", "value \"?[2Jx\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			p := withUI(h)
			h.vars["TOTE_LANG"] = tt.lang
			h.app.Environ = func() []string { return []string{tt.env} }

			if rc := h.run(); rc != exitOK || len(p.calls) != 1 {
				t.Fatalf("exit %d, %d starts, stderr %q", rc, len(p.calls), h.errb.String())
			}

			got := h.errb.String()
			if tt.want == "" && got != "" {
				t.Errorf("stderr = %q, want nothing", got)
			}

			if tt.want != "" && !strings.Contains(strings.ReplaceAll(got, "\u00a0", " "),
				strings.ReplaceAll(tt.want, "\u00a0", " ")) {
				t.Errorf("stderr = %q, want it to hold %q", got, tt.want)
			}
		})
	}
}

// TestUnknownColorWarningOnlyForTheInterface checks that the commands that do not draw an
// interface say nothing about the colors.
//
// Parameters:
//   - t: test to report to
func TestUnknownColorWarningOnlyForTheInterface(t *testing.T) {
	h := newHarness(t)
	withUI(h)
	h.app.Environ = func() []string { return []string{"TOTE_COLOR=rainbow"} }

	if rc := h.run("version"); rc != exitOK || h.errb.Len() != 0 {
		t.Errorf("version: exit %d, stderr %q", rc, h.errb.String())
	}
}

// TestConfigOpensSettings checks that "tote config" without argument opens the settings screen on
// a terminal, and prints the usage otherwise, and that "tote config path" is unchanged.
//
// Parameters:
//   - t: test to report to
func TestConfigOpensSettings(t *testing.T) {
	h := newHarness(t)
	p := withUI(h)

	if rc := h.run("config"); rc != exitOK || len(p.calls) != 1 ||
		p.calls[0].Start != ui.StartConfig {
		t.Fatalf("config on a terminal: exit %d calls %+v", rc, p.calls)
	}

	if rc := h.run("config", "path"); rc != exitOK || len(p.calls) != 1 ||
		strings.TrimSpace(h.out.String()) != h.vars["TOTE_CONFIG_DIR"] {
		t.Errorf("config path: exit %d calls %d stdout %q", rc, len(p.calls), h.out.String())
	}

	if rc := h.run("config", "--help"); rc != exitOK || len(p.calls) != 1 ||
		!strings.Contains(h.out.String(), "tote config") {
		t.Errorf("config --help: exit %d calls %d stdout %q", rc, len(p.calls), h.out.String())
	}

	h.tty = false

	if rc := h.run("config"); rc != exitUsage || len(p.calls) != 1 {
		t.Errorf("config without a terminal: exit %d calls %d", rc, len(p.calls))
	}
}

// TestUINoWrapper checks that the interface is told when the shell wrapper is not active.
//
// Parameters:
//   - t: test to report to
func TestUINoWrapper(t *testing.T) {
	for _, handoff := range []string{"", filepath.Join(t.TempDir(), "handoff")} {
		h := newHarness(t)
		p := withUI(h)
		h.vars["TOTE_HANDOFF"] = handoff

		if rc := h.run(); rc != exitOK || len(p.calls) != 1 {
			t.Fatalf("exit %d calls %d", rc, len(p.calls))
		}

		if got := p.calls[0].NoWrapper; got != (handoff == "") {
			t.Errorf("TOTE_HANDOFF=%q: NoWrapper = %v", handoff, got)
		}
	}
}
