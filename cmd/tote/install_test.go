package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installedIDs lists the plugins that the list verb shows as installed.
//
// Parameters:
//   - e: sandbox
func (e *pluginEnv) installedIDs() []string {
	e.t.Helper()

	var ids []string

	list := strings.TrimSpace(e.must("plugin", "--format", "tsv"))

	for _, line := range strings.Split(list, "\n") {
		cols := strings.Split(line, "\t")
		if len(cols) > 2 && cols[2] != "available" {
			ids = append(ids, cols[0])
		}
	}

	return ids
}

// TestPluginInstallLocal installs a plugin from a directory in each way the consent can be given
// or refused, and checks the exit code, the messages and what is installed.
//
// Parameters:
//   - t: test to report to
func TestPluginInstallLocal(t *testing.T) {
	tests := []struct {
		name      string
		lang      string
		tty       bool
		stdin     string
		grant     func(token string) string
		wantExit  int
		wantErr   string
		wantOut   string
		installed bool
	}{
		{
			name: "token", grant: func(s string) string { return s }, wantExit: 0,
			wantOut: "installed plugin demo 1.0.0 (enabled)", installed: true,
		},
		{name: "no token and no terminal", wantExit: exitUsage, wantErr: "consent needed"},
		{
			name: "wrong token", grant: func(string) string { return "exec:echo@000000000000" },
			wantExit: exitRefused, wantErr: "does not match",
		},
		{
			name: "answer yes", tty: true, stdin: "y\n", wantExit: 0, installed: true,
			wantOut: "installed plugin demo 1.0.0 (enabled)",
			wantErr: "Install plugin demo 1.0.0?",
		},
		{name: "answer no", tty: true, stdin: "n\n", wantExit: exitFail, wantErr: declined},
		{
			name: "empty answer is no", tty: true, stdin: "\n", wantExit: exitFail,
			wantErr: declined,
		},
		{name: "end of input is no", tty: true, wantExit: exitFail, wantErr: declined},
		{
			name: "french answer", lang: "fr", tty: true, stdin: "oui\n", wantExit: 0,
			installed: true,
		},
		{
			name: "english answer in french", lang: "fr", tty: true, stdin: "y\n", wantExit: 0,
			installed: true,
		},
		{
			name: "token wins over the terminal", tty: true, stdin: "n\n",
			grant: func(s string) string { return s }, wantExit: 0, installed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPluginEnv(t)
			if tc.lang != "" {
				e.vars["TOTE_LANG"] = tc.lang
			}

			dir := e.plugin("demo", "1.0.0", "exec:echo")
			token := e.grantOf(dir)

			e.ttyIn, e.stdin = tc.tty, tc.stdin

			args := []string{"plugin", "install", dir}
			if tc.grant != nil {
				args = append(args, "--grant", tc.grant(token))
			}

			rc, out, errs := e.do(args...)
			if rc != tc.wantExit {
				t.Fatalf("exit %d, want %d; stdout %q, stderr %q", rc, tc.wantExit, out, errs)
			}

			if !strings.Contains(out, tc.wantOut) || !strings.Contains(errs, tc.wantErr) {
				t.Errorf("stdout %q (want %q), stderr %q (want %q)", out, tc.wantOut, errs,
					tc.wantErr)
			}

			e.ttyIn, e.stdin = false, ""

			got := e.installedIDs()
			if tc.installed != (len(got) == 1) {
				t.Errorf("installed = %v, want installed %v", got, tc.installed)
			}

			if rc != 0 && out != "" && !strings.Contains(out, "dry run") {
				t.Errorf("a failure printed on stdout: %q", out)
			}
		})
	}
}

// TestPluginInstallRefusals covers the sources and the packages that cannot be installed.
//
// Parameters:
//   - t: test to report to
func TestPluginInstallRefusals(t *testing.T) {
	e := newPluginEnv(t)

	noLicense := withoutLicense(manifestOf("demo", "1.0.0", "exec:echo"))
	uncovered := manifestOf("demo", "1.0.0")
	reserved := manifestOf("plugin", "1.0.0")

	tests := []struct {
		name string
		args func() []string
		exit int
		want string
	}{
		{
			"not a source", func() []string { return []string{"nothing"} }, exitUsage,
			"is shipped with tote",
		},
		{
			"missing directory", func() []string { return []string{"./nowhere"} }, exitUsage,
			`"./nowhere" does not exist`,
		},
		{"empty source", func() []string { return []string{""} }, exitUsage, "not a plugin source"},
		{"no manifest", func() []string {
			d := filepath.Join(e.src, "bare")
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}

			return []string{d}
		}, exitFail, ""},
		{
			"missing key", func() []string { return []string{e.pluginText("nolic", noLicense)} },
			exitFail, "license: this key is required",
		},
		{"command no permission covers", func() []string {
			return []string{e.pluginText("unc", uncovered)}
		}, exitRefused, "no exec: permission covers"},
		{
			"reserved id", func() []string { return []string{e.pluginText("plugin", reserved)} },
			exitFail, "plugin",
		},
		{
			"a bundled id that does not exist", func() []string { return []string{"nope"} },
			exitUsage, "is shipped with tote",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc, out, errs := e.do(append([]string{"plugin", "install"}, tc.args()...)...)
			if rc != tc.exit || out != "" || !strings.Contains(errs, tc.want) {
				t.Errorf("exit %d (want %d), stdout %q, stderr %q (want %q)", rc, tc.exit, out,
					errs, tc.want)
			}
		})
	}

	if got := e.installedIDs(); len(got) != 0 {
		t.Errorf("a refused install left %v", got)
	}
}

// TestPluginDryRun prints what an install would do, in both languages and as JSON, and checks
// that nothing is installed and that no consent is needed to look.
//
// Parameters:
//   - t: test to report to
func TestPluginDryRun(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		e := newPluginEnv(t)
		e.vars["TOTE_LANG"] = lang

		dir := e.plugin("demo", "1.0.0", "exec:echo", "net:example.org")
		out := e.must("plugin", "install", dir, "--dry-run")

		golden(t, "plugin_dry_"+lang+".txt", out)

		if got := e.installedIDs(); len(got) != 0 {
			t.Errorf("%s: dry run installed %v", lang, got)
		}
	}

	e := newPluginEnv(t)
	dir := e.plugin("demo", "1.0.0", "exec:echo", "net:example.org")
	golden(t, "plugin_dry.json", e.must("plugin", "install", dir, "--dry-run", "--json"))

	var doc struct {
		Schema int  `json:"schema"`
		OK     bool `json:"ok"`
		DryRun bool `json:"dry_run"`
		Plugin struct {
			ID           string `json:"id"`
			Grant        string `json:"grant"`
			NeedsConsent bool   `json:"needs_consent"`
		} `json:"plugin"`
	}

	if err := json.Unmarshal([]byte(e.must("plugin", "install", dir, "--dry-run", "--json")),
		&doc); err != nil {
		t.Fatal(err)
	}

	if doc.Schema != 1 || !doc.OK || !doc.DryRun || doc.Plugin.ID != "demo" ||
		!doc.Plugin.NeedsConsent || !strings.Contains(doc.Plugin.Grant, "@") {
		t.Errorf("dry run object: %+v", doc)
	}
}

// TestPluginInstallJSON checks the object printed by a successful install.
//
// Parameters:
//   - t: test to report to
func TestPluginInstallJSON(t *testing.T) {
	e := newPluginEnv(t)
	dir := e.plugin("demo", "1.0.0", "exec:echo")

	var doc struct {
		Schema      int      `json:"schema"`
		OK          bool     `json:"ok"`
		ID          string   `json:"id"`
		Version     string   `json:"version"`
		Updated     bool     `json:"updated"`
		State       string   `json:"state"`
		Reasons     []string `json:"reasons"`
		ConsentedBy string   `json:"consented_by"`
	}

	out := e.must("plugin", "install", dir, "--grant", e.grantOf(dir), "--json")
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}

	if doc.Schema != 1 || !doc.OK || doc.ID != "demo" || doc.Version != "1.0.0" || doc.Updated ||
		doc.State != "enabled" || len(doc.Reasons) != 0 || doc.ConsentedBy != "grant" {
		t.Errorf("object: %+v\n%s", doc, out)
	}
}

// TestPluginInstallBundled installs the plugin shipped with tote. Its program is not on the PATH
// of the sandbox, so it is installed but not enabled, and the output says why and what to do.
//
// Parameters:
//   - t: test to report to
func TestPluginInstallBundled(t *testing.T) {
	e := newPluginEnv(t).withBundled()

	out := e.must("plugin", "install", "gft", "--grant", e.grantOf("gft"))

	for _, want := range []string{
		"installed plugin gft 1.0.0 (missing dependency)",
		`plugin "gft" is not enabled (missing dependency):`, "missing dependency: gft", "fix: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}

	if !strings.Contains(e.must("plugin", "--format", "tsv"), "gft\t1.0.0\tmissing-dependency\t") {
		t.Errorf("list: %s", e.out.String())
	}

	e.vars["PATH"] = filepath.Join(e.dir, "bin")
	writeProgram(t, filepath.Join(e.dir, "bin", "gft"))

	if !strings.Contains(e.must("plugin", "--format", "tsv"), "gft\t1.0.0\tenabled\t") {
		t.Errorf("list with the program: %s", e.out.String())
	}
}

// writeProgram creates an executable file.
//
// Parameters:
//   - t: running test
//   - path: where to create it
func writeProgram(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestPluginInstallRelease installs from a release URL with the checksum of the asset, and covers
// the failures of a download: the missing and the wrong checksum, the answers of the server and
// the exit codes they map to.
//
// Parameters:
//   - t: test to report to
func TestPluginInstallRelease(t *testing.T) {
	e := newPluginEnv(t)

	data := archive(t, manifestOf("demo", "1.0.0", "exec:echo"))
	url := e.release("acme", "demo", "1.0.0", "demo.tar.gz", data)
	token := e.grantOf(url, "--sha256", sum(data))

	tests := []struct {
		name string
		args []string
		exit int
		want string
	}{
		{"no checksum", []string{url, "--grant", token}, exitUsage, "--sha256"},
		{
			"malformed checksum",
			[]string{url, "--sha256", "abc", "--grant", token},
			exitUsage,
			"64 hexadecimal",
		},
		{
			"wrong checksum",
			[]string{url, "--sha256", sum([]byte("other")), "--grant", token},
			exitIntegrity, "checksum mismatch",
		},
		{
			"wrong token",
			[]string{url, "--sha256", sum(data), "--grant", "exec:echo@000000000000"},
			exitRefused, "does not match",
		},
		{"no token", []string{url, "--sha256", sum(data)}, exitUsage, "consent needed"},
		{"not found", []string{
			"https://github.com/acme/demo/releases/download/9.9.9/x.tar.gz",
			"--sha256", sum(data), "--grant", token,
		}, exitFail, "answered 404"},
		{"checksum in capitals", []string{
			url, "--sha256", strings.ToUpper(sum(data)),
			"--grant", token,
		}, 0, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc, _, errs := e.do(append([]string{"plugin", "install"}, tc.args...)...)
			if rc != tc.exit || !strings.Contains(errs, tc.want) {
				t.Errorf("exit %d (want %d), stderr %q (want %q)", rc, tc.exit, errs, tc.want)
			}
		})
	}

	if got := e.installedIDs(); len(got) != 1 {
		t.Fatalf("installed = %v, want only demo", got)
	}

	_, out, _ := e.do("plugin", "info", "demo", "--json")
	for _, want := range []string{
		`"origin": "acme/demo"`, `"kind": "release"`,
		`"archive_sha256": "` + sum(data) + `"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("info lacks %s:\n%s", want, out)
		}
	}
}

// TestPluginInstallAuth checks that a download answered 401 exits 3 (authentication required).
//
// Parameters:
//   - t: test to report to
func TestPluginInstallAuth(t *testing.T) {
	e := newPluginEnv(t)

	e.hub.handle(hostRepo, "/acme/private/releases/download/1.0.0/p.tar.gz",
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })

	rc, out, errs := e.do("plugin", "install",
		"https://github.com/acme/private/releases/download/1.0.0/p.tar.gz", "--sha256",
		sum([]byte("x")), "--json")

	if rc != exitAuth || !strings.Contains(out, `"exit": 3`) || errs != "" {
		t.Errorf("exit %d, stdout %q, stderr %q", rc, out, errs)
	}
}

// TestPluginUpdate covers the update of a local plugin: quiet when nothing widens, a consent when
// the access grows, and the rules about the version.
//
// Parameters:
//   - t: test to report to
func TestPluginUpdate(t *testing.T) {
	setup := func(t *testing.T) (*pluginEnv, string) {
		e := newPluginEnv(t)
		dir := e.plugin("demo", "1.0.0", "exec:echo")
		e.install(dir)

		return e, dir
	}

	t.Run("quiet", func(t *testing.T) {
		e, _ := setup(t)
		e.plugin("demo", "1.1.0", "exec:echo")

		out := e.must("plugin", "update", "demo")
		if strings.TrimSpace(out) != "updated demo from 1.0.0 to 1.1.0, permissions unchanged" {
			t.Errorf("stdout %q", out)
		}

		if !strings.Contains(e.must("plugin", "--format", "tsv"), "demo\t1.1.0\tenabled") {
			t.Errorf("list: %s", e.out.String())
		}
	})

	t.Run("escalation without a token", func(t *testing.T) {
		e, _ := setup(t)
		e.plugin("demo", "1.1.0", "exec:echo", "net:example.org")

		rc, _, errs := e.do("plugin", "update", "demo")
		if rc != exitUsage || !strings.Contains(errs, "consent needed") {
			t.Errorf("exit %d, stderr %q", rc, errs)
		}

		if !strings.Contains(e.must("plugin", "--format", "tsv"), "demo\t1.0.0\tenabled") {
			t.Error("a refused update changed the version")
		}
	})

	t.Run("escalation with the old token", func(t *testing.T) {
		e, _ := setup(t)
		old := e.infoGrant("demo")

		e.plugin("demo", "1.1.0", "exec:echo", "net:example.org")

		rc, _, errs := e.do("plugin", "update", "demo", "--grant", old)
		if rc != exitRefused || !strings.Contains(errs, "does not match") {
			t.Errorf("exit %d, stderr %q", rc, errs)
		}
	})

	t.Run("escalation with the token", func(t *testing.T) {
		e, dir := setup(t)
		e.plugin("demo", "1.1.0", "exec:echo", "net:example.org")

		token := e.grantOf(dir)
		out := e.must("plugin", "update", "demo", "--grant", token)

		if strings.TrimSpace(out) != "updated plugin demo from 1.0.0 to 1.1.0 (enabled)" {
			t.Errorf("stdout %q", out)
		}
	})

	t.Run("dry run shows the new access", func(t *testing.T) {
		e, _ := setup(t)
		e.plugin("demo", "1.1.0", "exec:echo", "net:example.org")

		out := e.must("plugin", "update", "demo", "--dry-run")
		for _, want := range []string{
			"demo 1.1.0 asks for more access", "New access:",
			"+ contact example.org", "Unchanged: 1", "No longer asked: 0",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %q:\n%s", want, out)
			}
		}

		golden(t, "plugin_update_dry_en.txt", out)
	})

	t.Run("prompt", func(t *testing.T) {
		e, _ := setup(t)
		e.plugin("demo", "1.1.0", "exec:echo", "net:example.org")
		e.ttyIn, e.stdin = true, "y\n"

		rc, out, errs := e.do("plugin", "update", "demo")
		if rc != 0 || !strings.Contains(errs, "asks for more access") ||
			!strings.Contains(errs, "Update plugin") || !strings.Contains(out, "updated plugin") {
			t.Errorf("exit %d, stdout %q, stderr %q", rc, out, errs)
		}
	})

	t.Run("not newer", func(t *testing.T) {
		e, _ := setup(t)

		rc, _, errs := e.do("plugin", "update", "demo")
		if rc != exitFail || !strings.Contains(errs, "not newer") {
			t.Errorf("exit %d, stderr %q", rc, errs)
		}

		rc, _, errs = e.do("plugin", "update", "demo", "--force")
		if rc != exitUsage || !strings.Contains(errs, "consent needed") {
			t.Errorf("--force without a token: exit %d, stderr %q", rc, errs)
		}

		rc, _, errs = e.do("plugin", "update", "demo", "--force", "--grant", e.infoGrant("demo"))
		if rc != 0 {
			t.Errorf("--force: exit %d, stderr %q", rc, errs)
		}
	})

	t.Run("not installed", func(t *testing.T) {
		e := newPluginEnv(t)

		rc, _, errs := e.do("plugin", "update", "demo")
		if rc != exitFail || !strings.Contains(errs, `plugin "demo" is not installed`) {
			t.Errorf("exit %d, stderr %q", rc, errs)
		}
	})

	t.Run("release without a source", func(t *testing.T) {
		e := newPluginEnv(t)
		data := archive(t, manifestOf("demo", "1.0.0", "exec:echo"))
		url := e.release("acme", "demo", "1.0.0", "demo.tar.gz", data)
		e.must("plugin", "install", url, "--sha256", sum(data), "--grant",
			e.grantOf(url, "--sha256", sum(data)))

		rc, _, errs := e.do("plugin", "update", "demo", "--sha256", sum(data))
		if rc != exitUsage || !strings.Contains(errs, "give the new release as a source") ||
			!strings.Contains(errs, url) {
			t.Errorf("exit %d, stderr %q", rc, errs)
		}

		next := archive(t, manifestOf("demo", "1.1.0", "exec:echo"))
		url2 := e.release("acme", "demo", "1.1.0", "demo.tar.gz", next)

		out := e.must("plugin", "update", "demo", url2, "--sha256", sum(next))
		if !strings.Contains(out, "updated demo from 1.0.0 to 1.1.0") {
			t.Errorf("stdout %q", out)
		}

		other := archive(t, manifestOf("demo", "1.2.0", "exec:echo"))
		url3 := e.release("someone", "demo", "1.2.0", "demo.tar.gz", other)

		rc, _, errs = e.do("plugin", "update", "demo", url3, "--sha256", sum(other))
		if rc != exitFail || !strings.Contains(errs, "use --force to replace it") {
			t.Errorf("another origin: exit %d, stderr %q", rc, errs)
		}
	})
}

// infoGrant reads the --grant token of an installed plugin.
//
// Parameters:
//   - id: plugin id
func (e *pluginEnv) infoGrant(id string) string {
	e.t.Helper()

	var doc struct {
		Grant string `json:"grant"`
	}

	if err := json.Unmarshal([]byte(e.must("plugin", "info", id, "--json")), &doc); err != nil {
		e.t.Fatal(err)
	}

	return doc.Grant
}

// TestPluginInstallArchive installs from an archive file, with and without its checksum.
//
// Parameters:
//   - t: test to report to
func TestPluginInstallArchive(t *testing.T) {
	e := newPluginEnv(t)

	data := archive(t, manifestOf("demo", "1.0.0", "exec:echo"))
	path := filepath.Join(e.src, "demo.tar.gz")

	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	token := e.grantOf(path)

	if rc, _, errs := e.do("plugin", "install", path, "--sha256", sum([]byte("x")), "--grant",
		token); rc != exitIntegrity || !strings.Contains(errs, "checksum mismatch") {
		t.Errorf("wrong checksum: exit %d, stderr %q", rc, errs)
	}

	if got := e.installedIDs(); len(got) != 0 {
		t.Fatalf("a refused install left %v", got)
	}

	out := e.must("plugin", "install", path, "--sha256", sum(data), "--grant", token)
	if !strings.Contains(out, "installed plugin demo 1.0.0 (enabled)") {
		t.Errorf("stdout %q", out)
	}
}
