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
	"time"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/plugin"
)

// lockBytes reads plugins.lock of the sandbox, empty when there is none.
//
// Parameters:
//   - e: sandbox
func (e *pluginEnv) lockBytes() string {
	e.t.Helper()

	data, err := os.ReadFile(filepath.Join(e.vars["TOTE_STATE_DIR"], "plugins.lock"))
	if err != nil && !os.IsNotExist(err) {
		e.t.Fatal(err)
	}

	return string(data)
}

// dataTree lists every path below the data directory of the sandbox with its mode, and the
// modification time of the files, so that a test can tell that the installed trees were not
// touched. The staging directory is the scratch area of every install: it must be empty, and the
// time of a directory is not recorded because a scratch entry that came and went changes it.
//
// Parameters:
//   - e: sandbox
func (e *pluginEnv) dataTree() []string {
	e.t.Helper()

	var out []string

	root := e.vars["TOTE_DATA_DIR"]

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}

		line := path + " " + info.Mode().String()
		if !d.IsDir() {
			line += " " + info.ModTime().String()
		}

		if filepath.Base(filepath.Dir(path)) == ".staging" {
			e.t.Errorf("left in staging: %s", path)
		}

		out = append(out, line)

		return nil
	})

	return out
}

// TestPluginInstallIdempotent checks that "plugin install" of what is installed already says so
// and exits 0, without a question and without touching the store or the lock, for a directory, a
// release and the plugin shipped with tote, with --dry-run, --json, in both languages and on a
// terminal; and that another version, another source and the same version with other files keep
// their answers.
//
// Parameters:
//   - t: test to report to
func TestPluginInstallIdempotent(t *testing.T) {
	type doc struct {
		Schema           int    `json:"schema"`
		OK               bool   `json:"ok"`
		DryRun           bool   `json:"dry_run"`
		ID               string `json:"id"`
		Version          string `json:"version"`
		Updated          bool   `json:"updated"`
		State            string `json:"state"`
		AlreadyInstalled bool   `json:"already_installed"`
		Plugin           struct {
			ID           string `json:"id"`
			NeedsConsent bool   `json:"needs_consent"`
		} `json:"plugin"`
	}

	type source struct {
		name    string
		setup   func(e *pluginEnv) (source string, extra []string)
		id      string
		version string
		state   map[string]string
	}

	sources := []source{
		{
			name: "directory", id: "demo", version: "1.0.0",
			state: map[string]string{"en": "enabled", "fr": "activ\u00e9"},
			setup: func(e *pluginEnv) (string, []string) {
				dir := e.plugin("demo", "1.0.0", "exec:echo")
				e.install(dir)

				return dir, nil
			},
		},
		{
			name: "release", id: "demo", version: "1.0.0",
			state: map[string]string{"en": "enabled", "fr": "activ\u00e9"},
			setup: func(e *pluginEnv) (string, []string) {
				data := archive(t, manifestOf("demo", "1.0.0", "exec:echo"))
				url := e.release("acme", "demo", "1.0.0", "demo.tar.gz", data)
				extra := []string{"--sha256", sum(data)}

				e.must(append([]string{
					"plugin", "install", url, "--grant",
					e.grantOf(url, extra...),
				}, extra...)...)

				return url, extra
			},
		},
		{
			name: "bundled", id: "gft", version: "1.0.0",
			state: map[string]string{"en": "missing dependency", "fr": "d\u00e9pendance manquante"},
			setup: func(e *pluginEnv) (string, []string) {
				e.withBundled()
				e.must("plugin", "install", "gft", "--grant", e.grantOf("gft"))

				return "gft", nil
			},
		},
	}

	modes := []struct {
		name  string
		flags []string
		check func(t *testing.T, s source, lang, out string)
	}{
		{"text", nil, func(t *testing.T, s source, lang, out string) {
			head := "plugin " + s.id + " " + s.version
			want := head + " is already installed (" + s.state["en"] + ")"

			if lang == "fr" {
				want = head + " d\u00e9j\u00e0 install\u00e9 (" + s.state["fr"] + ")"
			}

			if !strings.HasPrefix(out, want+"\n") {
				t.Errorf("stdout %q does not start with %q", out, want)
			}
		}},
		{"dry run", []string{"--dry-run"}, func(t *testing.T, s source, lang, out string) {
			if !strings.Contains(out, "already installed") &&
				!strings.Contains(out, "d\u00e9j\u00e0 install\u00e9") {
				t.Errorf("stdout %q", out)
			}

			if strings.Contains(out, "dry run") || strings.Contains(out, "simulation") ||
				strings.Contains(out, "grant") {
				t.Errorf("a dry run of a no-op prints what it would do: %q", out)
			}
		}},
		{"json", []string{"--json"}, func(t *testing.T, s source, _, out string) {
			var d doc

			if err := json.Unmarshal([]byte(out), &d); err != nil {
				t.Fatalf("%v in %q", err, out)
			}

			if d.Schema != 1 || !d.OK || !d.AlreadyInstalled || d.Updated || d.ID != s.id ||
				d.Version != s.version || d.State == "" || d.DryRun {
				t.Errorf("object = %+v", d)
			}
		}},
		{"dry run json", []string{"--dry-run", "--json"}, func(t *testing.T, s source, _,
			out string,
		) {
			var d doc

			if err := json.Unmarshal([]byte(out), &d); err != nil {
				t.Fatalf("%v in %q", err, out)
			}

			if d.Schema != 1 || !d.OK || !d.AlreadyInstalled || !d.DryRun || d.Plugin.ID != s.id ||
				d.Plugin.NeedsConsent {
				t.Errorf("object = %+v", d)
			}
		}},
	}

	for _, s := range sources {
		for _, lang := range []string{"en", "fr"} {
			for _, m := range modes {
				t.Run(s.name+"/"+lang+"/"+m.name, func(t *testing.T) {
					e := newPluginEnv(t)
					e.vars["TOTE_LANG"] = lang

					src, extra := s.setup(e)

					lock, data := e.lockBytes(), e.dataTree()

					// A terminal with an answer that would refuse: nothing may be asked.
					e.ttyIn, e.stdin = true, "n\n"

					args := append([]string{"plugin", "install", src}, extra...)
					rc, out, errs := e.do(append(args, m.flags...)...)

					if rc != 0 || errs != "" {
						t.Fatalf("exit %d, stdout %q, stderr %q", rc, out, errs)
					}

					m.check(t, s, lang, out)

					if after := e.lockBytes(); after != lock {
						t.Errorf("the lock changed:\n%s\n%s", lock, after)
					}

					if after := e.dataTree(); !slices.Equal(after, data) {
						t.Errorf("the store changed:\n%v\n%v", data, after)
					}
				})
			}
		}
	}

	t.Run("a plugin that is not enabled says how to fix it", func(t *testing.T) {
		e := newPluginEnv(t)
		dir := e.plugin("demo", "1.0.0", "exec:echo")
		e.install(dir)
		e.must("plugin", "disable", "demo")

		out := e.must("plugin", "install", dir)
		for _, want := range []string{
			"plugin demo 1.0.0 is already installed (disabled)",
			`plugin "demo" is not enabled (disabled):`, "tote plugin enable demo",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("a plugin that cannot be used exits 4", func(t *testing.T) {
		tests := []struct {
			name  string
			spoil func(e *pluginEnv)
			state string
			word  string
		}{
			{"needs consent", (*pluginEnv).needConsent, "needs-consent", "consent needed"},
			{"broken", func(e *pluginEnv) {
				lock := filepath.Join(e.vars["TOTE_STATE_DIR"], "plugins.lock")

				data, err := os.ReadFile(lock)
				if err != nil {
					e.t.Fatal(err)
				}

				next := strings.Replace(string(data), `tier = "declarative"`, `tier = "exec"`, 1)
				if next == string(data) {
					e.t.Fatalf("no tier in %s", data)
				}

				if err := os.WriteFile(lock, []byte(next), 0o600); err != nil {
					e.t.Fatal(err)
				}
			}, "broken", "broken"},
		}

		for _, tt := range tests {
			modes := [][]string{nil, {"--dry-run"}, {"--json"}, {"--dry-run", "--json"}}

			for _, flags := range modes {
				t.Run(tt.name+" "+strings.Join(flags, " "), func(t *testing.T) {
					e := newPluginEnv(t)
					dir := e.plugin("demo", "1.0.0", "exec:echo")
					e.install(dir)
					tt.spoil(e)

					lock := e.lockBytes()
					rc, out, errs := e.do(append([]string{"plugin", "install", dir}, flags...)...)

					if rc != exitRefused || errs != "" {
						t.Fatalf("exit %d, stdout %q, stderr %q", rc, out, errs)
					}

					if slices.Contains(flags, "--json") {
						// A dry run has no state of its own: the object is the one of the diff.
						state := slices.Contains(flags, "--dry-run") ||
							strings.Contains(out, `"state": "`+tt.state+`"`)

						if !strings.Contains(out, `"ok": false`) ||
							!strings.Contains(out, `"already_installed": true`) || !state {
							t.Errorf("stdout %q", out)
						}
					} else if !strings.Contains(out, "is already installed ("+tt.word+")") {
						t.Errorf("stdout %q", out)
					}

					if after := e.lockBytes(); after != lock {
						t.Errorf("the lock changed")
					}
				})
			}
		}
	})

	t.Run("keeps the answers of an update", func(t *testing.T) {
		e := newPluginEnv(t)
		dir := e.plugin("demo", "1.0.0", "exec:echo")
		e.install(dir)

		// Another version is an update.
		e.plugin("demo", "1.1.0", "exec:echo")

		out := e.must("plugin", "install", dir)
		if !strings.Contains(out, "updated demo from 1.0.0 to 1.1.0") {
			t.Errorf("stdout %q", out)
		}

		// The update verb is not idempotent: the same version is not newer.
		rc, _, errs := e.do("plugin", "update", "demo")
		if rc != exitFail || !strings.Contains(errs, "not newer") {
			t.Errorf("update: exit %d, stderr %q", rc, errs)
		}

		// The same version with other files is not the installed package.
		e.pluginText("demo", manifestOf("demo", "1.1.0", "exec:echo")+"\n# other bytes\n")

		rc, _, errs = e.do("plugin", "install", dir)
		if rc != exitFail || !strings.Contains(errs, "not newer") {
			t.Errorf("other files: exit %d, stderr %q", rc, errs)
		}

		// --force asks for the work, and so for the consent.
		e.plugin("demo", "1.1.0", "exec:echo")

		rc, _, errs = e.do("plugin", "install", dir, "--force")
		if rc != exitUsage || !strings.Contains(errs, "consent needed") {
			t.Errorf("--force: exit %d, stderr %q", rc, errs)
		}

		// Another directory with the same files is another source.
		other := e.pluginText("other", manifestOf("demo", "1.1.0", "exec:echo"))

		rc, _, errs = e.do("plugin", "install", other)
		if rc != exitFail || !strings.Contains(errs, "came from") {
			t.Errorf("other source: exit %d, stderr %q", rc, errs)
		}
	})
}

// TestPluginSourceChangedMessage checks the exact text of source_changed, in both languages, in
// each place where it is shown: the refusal, the warning of a --force --dry-run in text and in
// JSON, and the view of the consent. The text had three placeholders for the two values that the
// warning gives.
//
// Parameters:
//   - t: test to report to
func TestPluginSourceChangedMessage(t *testing.T) {
	// The French catalog puts a no-break space before a semicolon, written here as " ;" and
	// replaced in the loop. The paths are those of the sandbox, normalised by the harness.
	const (
		from = "<sandbox>/src/demo"
		to   = "<sandbox>/src/other"
	)

	tests := []struct {
		lang    string
		refusal string
		view    string
		reason  string
		ui      string
	}{
		{
			"en",
			`plugin "demo" came from ` + from + `, this one comes from ` + to +
				`; use --force to replace it`,
			"! Installed from " + from + ", now offered by " + to + ".",
			"it came from " + from + ", this one comes from " + to,
			`plugin "demo" came from ` + from + `, this one comes from ` + to +
				`; use --force on the command line to replace it`,
		},
		{
			"fr",
			`le plugin "demo" venait de ` + from + `, celui-ci vient de ` + to +
				` ; utilisez --force pour le remplacer`,
			"! Install\u00e9 depuis " + from + ", propos\u00e9 maintenant par " + to + ".",
			"il venait de " + from + ", celui-ci vient de " + to,
			`le plugin "demo" venait de ` + from + `, celui-ci vient de ` + to +
				` ; utilisez --force en ligne de commande pour le remplacer`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.lang, func(t *testing.T) {
			// The French catalog puts a no-break space before a semicolon.
			tc.refusal = strings.ReplaceAll(tc.refusal, " ;", "\u00a0;")
			tc.ui = strings.ReplaceAll(tc.ui, " ;", "\u00a0;")

			e := newPluginEnv(t)
			e.vars["TOTE_LANG"] = tc.lang

			e.install(e.plugin("demo", "1.0.0", "exec:echo"))
			other := e.pluginText("other", manifestOf("demo", "1.1.0", "exec:echo"))

			rc, out, errs := e.do("plugin", "update", "demo", other)
			if rc != exitFail || out != "" || !strings.Contains(errs, tc.refusal) {
				t.Errorf("refusal: exit %d, stdout %q, stderr %q", rc, out, errs)
			}

			if strings.Contains(errs, "%!") || strings.Contains(errs, "(MISSING)") {
				t.Errorf("garbled refusal %q", errs)
			}

			// The refusal of the interface says to use the command line.
			ui := e.app.t("ui_plug_err_source_changed", "demo", from, to)
			if ui != tc.ui {
				t.Errorf("interface message %q, want %q", ui, tc.ui)
			}

			_, out, _ = e.do("plugin", "update", "demo", other, "--force", "--dry-run")
			if !strings.Contains(out, tc.view) {
				t.Errorf("view lacks %q:\n%s", tc.view, out)
			}

			var d struct {
				Plugin struct {
					Warnings []struct {
						Code    string   `json:"code"`
						Args    []string `json:"args"`
						Message string   `json:"message"`
					} `json:"warnings"`
				} `json:"plugin"`
			}

			_, out, _ = e.do("plugin", "update", "demo", other, "--force", "--dry-run", "--json")
			if err := json.Unmarshal([]byte(out), &d); err != nil || len(d.Plugin.Warnings) != 1 {
				t.Fatalf("%v in %q", err, out)
			}

			w := d.Plugin.Warnings[0]
			if w.Code != "source_changed" || len(w.Args) != 2 || w.Message != tc.reason {
				t.Errorf("warning = %+v, want message %q", w, tc.reason)
			}
		})
	}
}

// TestPluginMessageArity checks, for every refusal and every warning of the update rules, that
// the number of arguments the plugin layer gives is the number of placeholders of each message
// that renders it, in both languages: the message of the code, the one of the interface and the
// line of the consent view. The producers are run for real, so that a change of either side
// fails here.
//
// Parameters:
//   - t: test to report to
func TestPluginMessageArity(t *testing.T) {
	e := newPluginEnv(t)
	dir := e.plugin("demo", "1.0.0", "exec:echo")
	e.install(dir)

	reg, err := e.app.pluginRegistry()
	if err != nil {
		t.Fatal(err)
	}

	other := e.pluginText("other", manifestOf("demo", "1.1.0", "exec:echo"))

	plan := func(src string, version string, force bool) (*plugin.Staged, error) {
		if src == dir {
			e.plugin("demo", version, "exec:echo")
		} else {
			e.pluginText("other", manifestOf("demo", version, "exec:echo"))
		}

		return reg.Plan(context.Background(), plugin.InstallRequest{Source: src, Force: force})
	}

	type produced struct {
		code string
		args []string
	}

	errOf := func(src, version string) produced {
		st, err := plan(src, version, false)
		if err == nil {
			_ = st.Abort()

			t.Fatalf("plan %s %s succeeded", src, version)
		}

		var pe *plugin.Error
		if !errors.As(err, &pe) {
			t.Fatalf("error %v", err)
		}

		return produced{pe.Code, pe.Args}
	}

	warnOf := func(src, version string) []produced {
		st, err := plan(src, version, true)
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = st.Abort() }()

		var out []produced
		for _, w := range st.Diff().Warnings {
			out = append(out, produced{w.Code, w.Args})
		}

		return out
	}

	var refusals []produced

	for _, c := range []struct{ src, version string }{
		{other, "1.1.0"}, {dir, "0.9.0"}, {dir, "1.1.0-beta.1"},
	} {
		refusals = append(refusals, errOf(c.src, c.version))
	}

	var warnings []produced

	for _, c := range []struct{ src, version string }{
		{other, "1.1.0"}, {dir, "0.9.0"}, {dir, "1.0.0"}, {dir, "1.1.0-beta.1"},
	} {
		warnings = append(warnings, warnOf(c.src, c.version)...)
	}

	wantCodes := []string{"source_changed", "downgrade", "same_version", "prerelease"}
	for _, code := range wantCodes {
		if !slices.ContainsFunc(warnings, func(p produced) bool { return p.code == code }) {
			t.Errorf("no warning %s among %v", code, warnings)
		}
	}

	for _, lang := range []string{"en", "fr"} {
		e.vars["TOTE_LANG"] = lang

		keys := func(p produced, prefixes ...string) {
			for _, prefix := range prefixes {
				key := prefix + p.code
				if !e.app.tr().Has(key) {
					continue
				}

				if n := len(directives(e.app.t(key))); n != len(p.args) {
					t.Errorf("%s: %s has %d placeholders, the producer gives %d arguments %q",
						lang, key, n, len(p.args), p.args)
				}
			}
		}

		for _, p := range refusals {
			keys(p, "plugin_err_", "ui_plug_err_")
		}

		for _, p := range warnings {
			keys(p, "plugin_reason_", "consent_warn_")

			if !e.app.tr().Has("plugin_reason_" + p.code) {
				t.Errorf("%s: no reason message for the warning %s", lang, p.code)
			}
		}
	}
}

// TestPluginExecTierRefused checks that tote 1.0 refuses a plugin of the exec tier at every
// command that reads it: check, install and update say that the type is not available, with the
// code reserved_not_available, in both languages and in JSON; and a plugin of that tier that is
// in the store from an earlier tote is listed as incompatible with the reason, and cannot be
// enabled.
//
// Parameters:
//   - t: test to report to
func TestPluginExecTierRefused(t *testing.T) {
	execManifest := strings.Replace(manifestOf("ex", "1.0.0", "exec:echo"),
		`tier = "declarative"`, `tier = "exec"`, 1) + "\n[exec]\nbin = \"bin/ex\"\n"

	msgs := map[string]string{
		"en": `tier = "exec" is not available in this version of tote (reserved for a later one)`,
		"fr": "tier = \"exec\" n\u2019est pas disponible dans cette version de tote " +
			"(r\u00e9serv\u00e9 \u00e0 une version ult\u00e9rieure)",
	}

	for _, lang := range []string{"en", "fr"} {
		t.Run(lang, func(t *testing.T) {
			e := newPluginEnv(t)
			e.vars["TOTE_LANG"] = lang

			dir := e.pluginText("ex", execManifest)

			if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
				t.Fatal(err)
			}

			writeProgram(t, filepath.Join(dir, "bin", "ex"))

			rc, out, errs := e.do("plugin", "install", dir)
			if rc != exitFail || out != "" || !strings.Contains(errs, msgs[lang]) {
				t.Errorf("install: exit %d, stdout %q, stderr %q", rc, out, errs)
			}

			rc, out, errs = e.do("plugin", "install", dir, "--dry-run", "--json")

			var obj struct {
				OK    bool `json:"ok"`
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
					Exit    int    `json:"exit"`
				} `json:"error"`
			}

			if err := json.Unmarshal([]byte(out), &obj); err != nil || obj.OK ||
				obj.Error.Code != "reserved_not_available" || obj.Error.Exit != 1 ||
				!strings.Contains(obj.Error.Message, msgs[lang]) || rc != exitFail || errs != "" {
				t.Errorf("install json: exit %d, %v, %+v, stderr %q", rc, err, obj, errs)
			}

			rc, out, errs = e.do("plugin", "check", dir)
			if rc != exitFail || !strings.Contains(out+errs, msgs[lang]) ||
				!strings.Contains(out+errs, "tier") {
				t.Errorf("check: exit %d, stdout %q, stderr %q", rc, out, errs)
			}

			rc, out, _ = e.do("plugin", "check", dir, "--json")
			if rc != exitFail || !strings.Contains(out, `"reserved_not_available"`) {
				t.Errorf("check json: exit %d, stdout %q", rc, out)
			}

			if got := e.installedIDs(); len(got) != 0 {
				t.Errorf("installed = %v", got)
			}
		})
	}

	t.Run("in the store", func(t *testing.T) {
		e := newPluginEnv(t)
		e.seedExec("ex", execManifest)

		list := e.must("plugin", "--format", "tsv")
		if !strings.Contains(list, "ex\t1.0.0\tincompatible\t") {
			t.Errorf("list:\n%s", list)
		}

		rc, _, errs := e.do("plugin", "enable", "ex", "--grant", "x")
		if rc != exitRefused || !strings.Contains(errs, msgs["en"]) {
			t.Errorf("enable: exit %d, stderr %q", rc, errs)
		}

		info := e.must("plugin", "info", "ex")
		if !strings.Contains(info, "incompatible") || !strings.Contains(info, msgs["en"]) {
			t.Errorf("info:\n%s", info)
		}

		out := e.must("plugin", "--json")
		if !strings.Contains(out, `"state": "incompatible"`) ||
			!strings.Contains(out, `"enabled": false`) {
			t.Errorf("list json:\n%s", out)
		}

		// The state repeats what the manifest says: one cause is one error.
		rc, out, errs = e.do("plugin", "check", "ex")
		if rc != exitFail || errs != "" || strings.Count(out, "error:") != 1 ||
			!strings.Contains(out, "ex: errors 1, warnings 0") {
			t.Errorf("check: exit %d, stdout %q, stderr %q", rc, out, errs)
		}
	})
}

// seedExec puts a plugin of the exec tier in the store of the sandbox, installed and consented,
// the way an earlier tote left it.
//
// Parameters:
//   - id: plugin id
//   - manifest: contents of plugin.toml
func (e *pluginEnv) seedExec(id, manifest string) {
	e.t.Helper()

	paths, err := config.Resolve(func(k string) string { return e.vars[k] }, e.app.Home)
	if err != nil {
		e.t.Fatal(err)
	}

	idDir, err := paths.PluginDir(id)
	if err != nil {
		e.t.Fatal(err)
	}

	build := filepath.Join(idDir, ".build")
	if err := os.MkdirAll(filepath.Join(build, "bin"), 0o700); err != nil {
		e.t.Fatal(err)
	}

	for name, mode := range map[string]os.FileMode{"plugin.toml": 0o400, "bin/ex": 0o500} {
		content := []byte("#!/bin/sh\n")
		if name == "plugin.toml" {
			content = []byte(manifest)
		}

		if err := os.WriteFile(filepath.Join(build, name), content, mode); err != nil {
			e.t.Fatal(err)
		}
	}

	if err := os.Chmod(filepath.Join(build, "bin"), 0o500); err != nil {
		e.t.Fatal(err)
	}

	if err := os.Chmod(build, 0o500); err != nil {
		e.t.Fatal(err)
	}

	tree, err := plugin.HashTree(build)
	if err != nil {
		e.t.Fatal(err)
	}

	dest, err := paths.PluginTree(id, tree.ID())
	if err != nil {
		e.t.Fatal(err)
	}

	if err := os.Rename(build, dest); err != nil {
		e.t.Fatal(err)
	}

	lk, err := plugin.OpenLock(paths.LockFile())
	if err != nil {
		e.t.Fatal(err)
	}

	now := time.Date(2026, 11, 3, 10, 15, 0, 0, time.UTC).Format(time.RFC3339)

	rec := plugin.LockRecord{
		ID: id, Version: "1.0.0", Tier: plugin.TierExec,
		Kind: plugin.KindLocal, Source: filepath.Join(e.src, id), TreeSHA256: tree.SHA256,
		SurfaceSHA256: strings.Repeat("a", 64), InstalledAt: now, UpdatedAt: now,
		Caps: []string{"exec:echo"}, ConsentedAt: now, ConsentedBy: plugin.ConsentPrompt,
	}

	if err := lk.Put(rec); err != nil {
		e.t.Fatal(err)
	}
}
