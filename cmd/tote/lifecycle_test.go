package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// needConsent rewrites the install record of a plugin so that it has not consented to the access
// the plugin asks for: the plugin is then waiting for consent.
//
// Parameters:
//   - e: sandbox
func (e *pluginEnv) needConsent() {
	e.t.Helper()

	lock := filepath.Join(e.vars["TOTE_STATE_DIR"], "plugins.lock")

	data, err := os.ReadFile(lock)
	if err != nil {
		e.t.Fatal(err)
	}

	next := strings.Replace(string(data), `caps = ["exec:echo"]`, `caps = []`, 1)
	if next == string(data) {
		e.t.Fatalf("no capability to remove in %s", data)
	}

	if err := os.WriteFile(lock, []byte(next), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// TestPluginDisableEnable runs a plugin through disable and enable, twice each, in text and in
// JSON.
//
// Parameters:
//   - t: test to report to
func TestPluginDisableEnable(t *testing.T) {
	e := newPluginEnv(t)
	e.install(e.plugin("demo", "1.0.0", "exec:echo"))

	steps := []struct {
		args []string
		exit int
		out  string
		err  string
	}{
		{
			[]string{"plugin", "disable", "demo"},
			0,
			`plugin "demo" disabled, its settings, data and consent are kept`, "",
		},
		{[]string{"plugin", "disable", "demo"}, 0, `plugin "demo" is already disabled`, ""},
		{[]string{"plugin", "enable", "demo"}, 0, `plugin "demo" enabled`, ""},
		{[]string{"plugin", "enable", "demo"}, 0, `plugin "demo" is already enabled`, ""},
		{[]string{"plugin", "disable", "nope"}, exitFail, "", `plugin "nope" is not installed`},
		{[]string{"plugin", "enable", "nope"}, exitFail, "", `plugin "nope" is not installed`},
		{[]string{"plugin", "disable", "../data"}, exitUsage, "", "invalid plugin id"},
		{[]string{"plugin", "enable", "Demo"}, exitUsage, "", "invalid plugin id"},
		{[]string{"plugin", "disable"}, exitUsage, "", "missing argument: id"},
		{[]string{"plugin", "disable", "a", "b"}, exitUsage, "", "too many arguments"},
	}

	for _, s := range steps {
		rc, out, errs := e.do(s.args...)
		if rc != s.exit || !strings.Contains(out, s.out) || !strings.Contains(errs, s.err) {
			t.Errorf("%v: exit %d (want %d), stdout %q, stderr %q", s.args, rc, s.exit, out, errs)
		}
	}

	if out := e.must("plugin", "disable", "demo", "--json"); !strings.Contains(out,
		`"already": false`) {
		t.Errorf("disable --json: %s", out)
	}

	out := e.must("plugin", "enable", "demo", "--json")
	for _, want := range []string{`"ok": true`, `"state": "enabled"`, `"already": false`} {
		if !strings.Contains(out, want) {
			t.Errorf("enable --json lacks %s:\n%s", want, out)
		}
	}
}

// TestPluginDisableLayout checks that a disable still works when config.toml spells the plugin
// table in a way tote does not rewrite, and says which file to edit.
//
// Parameters:
//   - t: test to report to
func TestPluginDisableLayout(t *testing.T) {
	e := newPluginEnv(t)
	e.install(e.plugin("demo", "1.0.0", "exec:echo"))
	e.write("config.toml", "schema = 1\nplugins.demo.enabled = true\n")

	rc, out, errs := e.do("plugin", "disable", "demo")
	if rc != 0 || !strings.Contains(out, "disabled") ||
		!strings.Contains(errs, "could not be edited") {
		t.Fatalf("disable: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	if got := e.must("plugin", "--format", "tsv"); !strings.Contains(got, "demo\t1.0.0\tdisabled") {
		t.Errorf("list: %s", got)
	}
}

// TestPluginEnableConsent enables a plugin that waits for consent, with every way of giving or
// refusing it.
//
// Parameters:
//   - t: test to report to
func TestPluginEnableConsent(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		tty     bool
		stdin   string
		exit    int
		err     string
		enabled bool
	}{
		{name: "no token", args: []string{}, exit: exitUsage, err: "consent needed"},
		{
			name: "wrong token", args: []string{"--grant", "exec:echo@000000000000"},
			exit: exitRefused, err: "does not match",
		},
		{name: "token", args: []string{"--grant", "TOKEN"}, enabled: true},
		{
			name: "yes", tty: true, stdin: "y\n", enabled: true,
			err: "Allow plugin demo 1.0.0 to run?",
		},
		{name: "no", tty: true, stdin: "n\n", exit: exitFail, err: "cancelled, nothing changed"},
		{name: "default is no", tty: true, exit: exitFail, err: "cancelled, nothing changed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPluginEnv(t)
			e.install(e.plugin("demo", "1.0.0", "exec:echo"))
			e.needConsent()

			if got := e.must("plugin", "--format", "tsv"); !strings.Contains(got,
				"demo\t1.0.0\tneeds-consent") {
				t.Fatalf("setup: %s", got)
			}

			args := []string{"plugin", "enable", "demo"}
			for _, a := range tc.args {
				if a == "TOKEN" {
					a = e.infoGrant("demo")
				}

				args = append(args, a)
			}

			e.ttyIn, e.stdin = tc.tty, tc.stdin

			rc, out, errs := e.do(args...)
			if rc != tc.exit || !strings.Contains(errs, tc.err) {
				t.Errorf("exit %d (want %d), stdout %q, stderr %q", rc, tc.exit, out, errs)
			}

			e.ttyIn = false

			want := "needs-consent"
			if tc.enabled {
				want = "enabled"
			}

			if got := e.must("plugin", "--format", "tsv"); !strings.Contains(got,
				"demo\t1.0.0\t"+want) {
				t.Errorf("state after: %s, want %s", got, want)
			}
		})
	}
}

// TestPluginEnableRefused checks that a plugin whose files changed cannot be enabled (exit 4) and
// that the output says why and what to do.
//
// Parameters:
//   - t: test to report to
func TestPluginEnableRefused(t *testing.T) {
	e := newPluginEnv(t)
	e.install(e.plugin("demo", "1.0.0", "exec:echo"))

	lock := filepath.Join(e.vars["TOTE_STATE_DIR"], "plugins.lock")

	data, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}

	next := strings.Replace(string(data), `tree_sha256 = "`, `tree_sha256 = "00`, 1)
	if err := os.WriteFile(lock, []byte(next), 0o600); err != nil {
		t.Fatal(err)
	}

	rc, out, errs := e.do("plugin", "enable", "demo")
	if rc != exitRefused || out != "" || !strings.Contains(errs, "cannot enable demo: ") {
		t.Errorf("enable: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	out = e.must("plugin", "info", "demo")
	if !strings.Contains(out, "state: broken") || !strings.Contains(out, "fix: remove it") {
		t.Errorf("info:\n%s", out)
	}

	rc, out, _ = e.do("plugin", "enable", "demo", "--json")
	if rc != exitRefused || !strings.Contains(out, `"code": "enable_refused"`) ||
		!strings.Contains(out, `"exit": 4`) {
		t.Errorf("enable --json: exit %d, stdout %q", rc, out)
	}
}

// TestPluginRemove removes a plugin: the confirmation, the refusals before any file system call
// and what is kept without --purge.
//
// Parameters:
//   - t: test to report to
func TestPluginRemove(t *testing.T) {
	setup := func(t *testing.T) *pluginEnv {
		e := newPluginEnv(t)
		e.install(e.plugin("demo", "1.0.0", "exec:echo"))

		return e
	}

	t.Run("no terminal and no --yes", func(t *testing.T) {
		e := setup(t)

		rc, out, errs := e.do("plugin", "remove", "demo")
		if rc != exitUsage || out != "" || !strings.Contains(errs, "pass --yes") {
			t.Errorf("exit %d, stdout %q, stderr %q", rc, out, errs)
		}

		if got := e.installedIDs(); len(got) != 1 {
			t.Errorf("installed = %v", got)
		}

		rc, out, _ = e.do("plugin", "remove", "demo", "--json")
		if rc != exitUsage || !strings.Contains(out, `"code": "confirm_needed"`) {
			t.Errorf("--json: exit %d, stdout %q", rc, out)
		}
	})

	t.Run("answer no", func(t *testing.T) {
		e := setup(t)
		e.ttyIn, e.stdin = true, "n\n"

		rc, _, errs := e.do("plugin", "remove", "demo")
		if rc != exitFail || !strings.Contains(errs, `removing plugin "demo" deletes:`) ||
			!strings.Contains(errs, "its installed files, its install record and enabled = false") ||
			!strings.Contains(errs, "cancelled, nothing changed") {
			t.Errorf("exit %d, stderr %q", rc, errs)
		}

		e.ttyIn = false

		if got := e.installedIDs(); len(got) != 1 {
			t.Errorf("installed = %v", got)
		}
	})

	t.Run("answer yes, purge", func(t *testing.T) {
		e := setup(t)
		e.ttyIn, e.stdin = true, "y\n"

		rc, out, errs := e.do("plugin", "remove", "demo", "--purge")
		if rc != 0 || !strings.Contains(out, `removed plugin "demo"`) ||
			!strings.Contains(errs, "its settings, state, cache and host files (the rest of its table") ||
			strings.Contains(out, "kept:") {
			t.Errorf("exit %d, stdout %q, stderr %q", rc, out, errs)
		}

		e.ttyIn = false

		if got := e.installedIDs(); len(got) != 0 {
			t.Errorf("installed = %v", got)
		}
	})

	t.Run("--yes keeps the data", func(t *testing.T) {
		e := setup(t)

		rc, out, _ := e.do("plugin", "remove", "demo", "--yes")
		if rc != 0 || !strings.Contains(out, "kept: ") ||
			!strings.Contains(out, "tote plugin remove --purge deletes them") {
			t.Errorf("exit %d, stdout %q", rc, out)
		}

		tree := filepath.Join(e.vars["TOTE_DATA_DIR"], "plugins", "demo")
		if _, err := os.Stat(tree); err == nil {
			t.Error("the plugin directory is still there")
		}

		rc, _, errs := e.do("plugin", "remove", "demo", "--yes")
		if rc != exitFail || !strings.Contains(errs, "is not installed") {
			t.Errorf("second remove: exit %d, stderr %q", rc, errs)
		}
	})

	t.Run("json", func(t *testing.T) {
		e := setup(t)

		out := e.must("plugin", "remove", "demo", "--yes", "--json")
		for _, want := range []string{`"ok": true`, `"id": "demo"`, `"kept": [`} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %s:\n%s", want, out)
			}
		}
	})

	t.Run("bad ids touch nothing", func(t *testing.T) {
		e := setup(t)

		marker := filepath.Join(e.vars["TOTE_DATA_DIR"], "keep.txt")
		if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}

		for _, id := range []string{"../data", "a/b", "..", ".", "Demo", ""} {
			rc, _, errs := e.do("plugin", "remove", id, "--yes")
			if rc != exitUsage || !strings.Contains(errs, "invalid plugin id") {
				t.Errorf("remove %q: exit %d, stderr %q", id, rc, errs)
			}
		}

		if _, err := os.Stat(marker); err != nil {
			t.Errorf("the data directory was touched: %v", err)
		}

		if got := e.installedIDs(); len(got) != 1 {
			t.Errorf("installed = %v", got)
		}
	})
}
