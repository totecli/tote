package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// badManifest is a plugin that tote refuses: its tier is not one of the two it knows.
//
// Parameters:
//   - id: plugin id
func badManifest(id string) string {
	return strings.Replace(manifestOf(id, "1.0.0", "exec:echo"), `tier = "declarative"`,
		`tier = "wizard"`, 1)
}

// TestPluginCheckTargets checks a directory, an archive and an installed plugin, in each language
// and in JSON, and compares the text with the reference files.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckTargets(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		t.Run(lang, func(t *testing.T) {
			e := newPluginEnv(t)
			e.vars["TOTE_LANG"] = lang

			dir := e.plugin("demo", "1.0.0", "exec:echo")

			rc, out, errs := e.do("plugin", "check", dir)
			if rc != 0 || errs != "" {
				t.Fatalf("directory: exit %d, stderr %q", rc, errs)
			}

			golden(t, "plugin_check_dir_"+lang+".txt", out)

			arch := filepath.Join(e.src, "demo-1.0.0.tar.gz")
			data := archive(t, manifestOf("demo", "1.0.0", "exec:echo"))

			if err := os.WriteFile(arch, data, 0o600); err != nil {
				t.Fatal(err)
			}

			rc, out, errs = e.do("plugin", "check", arch)
			if rc != 0 || errs != "" {
				t.Fatalf("archive: exit %d, stderr %q", rc, errs)
			}

			// The compressed bytes depend on the Go version, so the hash of the archive is checked
			// against the data and then hidden from the reference file.
			if !strings.Contains(out, sum(data)) {
				t.Errorf("the hash of the archive is not in %q", out)
			}

			golden(t, "plugin_check_archive_"+lang+".txt",
				strings.ReplaceAll(out, sum(data), "<archive sha256>"))

			e.install(dir)

			rc, out, errs = e.do("plugin", "check", "demo")
			if rc != 0 || errs != "" {
				t.Fatalf("installed: exit %d, stderr %q", rc, errs)
			}

			golden(t, "plugin_check_installed_"+lang+".txt", out)
		})
	}

	e := newPluginEnv(t)
	dir := e.plugin("demo", "1.0.0", "exec:echo")

	out := e.must("plugin", "check", dir, "--json")
	golden(t, "plugin_check.json", out)

	var doc struct {
		Schema int    `json:"schema"`
		OK     bool   `json:"ok"`
		ID     string `json:"id"`
		Ver    string `json:"version"`
		Tree   string `json:"tree_sha256"`
		Sur    string `json:"surface_sha256"`
	}

	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}

	if doc.Schema != 1 || !doc.OK || doc.ID != "demo" || doc.Ver != "1.0.0" ||
		len(doc.Tree) != 64 || len(doc.Sur) != 64 {
		t.Errorf("keys scripts read are wrong: %+v", doc)
	}
}

// TestPluginCheckSidecar checks that an archive gets the line to publish next to it.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckSidecar(t *testing.T) {
	e := newPluginEnv(t)
	data := archive(t, manifestOf("demo", "1.0.0", "exec:echo"))
	arch := filepath.Join(e.src, "demo-1.0.0.tar.gz")

	if err := os.WriteFile(arch, data, 0o600); err != nil {
		t.Fatal(err)
	}

	out := e.must("plugin", "check", arch)
	if !strings.Contains(out, sum(data)+"  demo-1.0.0.tar.gz") {
		t.Errorf("no sidecar line in:\n%s", out)
	}
}

// TestPluginCheckFindings checks the exit code and the report of a plugin that is refused, and
// that --strict turns a warning into a failure.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckFindings(t *testing.T) {
	e := newPluginEnv(t)

	bad := e.pluginText("bad", badManifest("bad"))

	rc, out, _ := e.do("plugin", "check", bad)
	if rc != exitFail || !strings.Contains(out, "error") {
		t.Errorf("bad plugin: exit %d, stdout %q", rc, out)
	}

	rc, out, _ = e.do("plugin", "check", bad, "--json")

	var doc struct {
		OK       bool `json:"ok"`
		Errors   int  `json:"errors"`
		Findings []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"findings"`
	}

	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}

	if rc != exitFail || doc.OK || doc.Errors == 0 || len(doc.Findings) == 0 ||
		doc.Findings[0].Message == "" || doc.Findings[0].Severity != "error" {
		t.Errorf("bad plugin json: exit %d, %+v", rc, doc)
	}

	rc, _, errs := e.do("plugin", "check", filepath.Join(e.src, "nowhere"))
	if rc != exitUsage || errs == "" {
		t.Errorf("missing target: exit %d, stderr %q", rc, errs)
	}
}

// TestPluginCheckArgv checks the exact command a contribution would run, the options that only
// make sense with --argv and their usage errors.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckArgv(t *testing.T) {
	e := newPluginEnv(t)
	dir := e.plugin("demo", "1.0.0", "exec:echo")

	out := e.must("plugin", "check", dir, "--argv")
	if !strings.Contains(out, "echo hello") {
		t.Errorf("no command in:\n%s", out)
	}

	tests := []struct {
		name string
		args []string
		exit int
		err  string
	}{
		{"set without argv", []string{"--set", "a=b"}, exitUsage, "--argv"},
		{"repo without argv", []string{"--repo", "x"}, exitUsage, "--argv"},
		{"bad set", []string{"--argv", "--set", "nonsense"}, exitUsage, "nonsense"},
		{"set", []string{"--argv", "--set", "a=b"}, 0, ""},
		{"official", []string{"--official"}, -1, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc, _, errs := e.do(append([]string{"plugin", "check", dir}, tc.args...)...)
			if tc.exit >= 0 && rc != tc.exit || !strings.Contains(errs, tc.err) {
				t.Errorf("exit %d (want %d), stderr %q", rc, tc.exit, errs)
			}
		})
	}
}

// TestPluginCheckAll checks every installed plugin and what is left on disk.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckAll(t *testing.T) {
	e := newPluginEnv(t)

	rc, out, _ := e.do("plugin", "check")
	if rc != 0 || !strings.Contains(out, "plugins checked: 0") {
		t.Errorf("empty: exit %d, stdout %q", rc, out)
	}

	e.install(e.plugin("demo", "1.0.0", "exec:echo"))

	rc, out, _ = e.do("plugin", "check")
	if rc != 0 || !strings.Contains(out, "demo 1.0.0 (enabled)") ||
		!strings.Contains(out, "plugins checked: 1") {
		t.Errorf("one plugin: exit %d, stdout %q", rc, out)
	}

	out = e.must("plugin", "check", "--json")

	var doc struct {
		Schema  int  `json:"schema"`
		OK      bool `json:"ok"`
		Plugins []struct {
			ID string `json:"id"`
		} `json:"plugins"`
		Orphans   []any `json:"orphans"`
		Leftovers []any `json:"leftovers"`
	}

	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}

	if doc.Schema != 1 || !doc.OK || len(doc.Plugins) != 1 || doc.Plugins[0].ID != "demo" ||
		doc.Orphans == nil || doc.Leftovers == nil {
		t.Errorf("json: %+v", doc)
	}

	// An entry that is not a plugin is reported.
	if err := os.MkdirAll(filepath.Join(e.vars["TOTE_DATA_DIR"], "plugins", "stray"),
		0o755); err != nil {
		t.Fatal(err)
	}

	rc, out, _ = e.do("plugin", "check")
	if rc != exitFail || !strings.Contains(out, "stray") ||
		!strings.Contains(out, "no install record") || strings.Contains(out, "no_lock_record") {
		t.Errorf("stray entry: exit %d, stdout %q", rc, out)
	}
}
