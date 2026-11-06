package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// diag runs Diagnose over a sandbox.
//
// Parameters:
//   - l: sandbox
//   - strict: the --strict flag
func diag(l *lab, strict bool) Diagnosis {
	return l.reg(nil).Diagnose(context.Background(), strict)
}

// codes lists the codes of findings.
//
// Parameters:
//   - fs: findings
func codes(fs []Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Code
	}

	return out
}

// TestDiagnoseHealthy checks that a sandbox with healthy plugins and nothing else gives no
// finding, no leftover and no failure, and that the results carry no argv: the diagnosis is a
// summary, not one check per contribution.
//
// Parameters:
//   - t: test to report to
func TestDiagnoseHealthy(t *testing.T) {
	l := chkLab(t)
	l.install("demo", demoFiles("demo", ""))
	l.install("other", demoFiles("other", ""))

	d := diag(l, true)

	if d.Failed() || len(d.Plugins) != 2 || len(d.Leftovers) != 0 || len(d.Orphans) != 0 ||
		len(d.Findings) != 0 || len(d.Problems) != 0 {
		t.Errorf("diagnosis %+v", d)
	}

	for _, p := range d.Plugins {
		if p.State != StateEnabled || len(p.Contributions) != 0 || p.Kind != TargetInstalled {
			t.Errorf("%s: %+v", p.ID, p)
		}
	}
}

// TestDiagnoseReportsLeftovers checks everything the diagnosis lists besides the plugins: orphan
// trees, staging directories, old trees, the settings of an uninstalled id, entries that are
// not plugins, and a plugin-procs that names a plugin which is not installed. Nothing is deleted.
//
// Parameters:
//   - t: test to report to
func TestDiagnoseReportsLeftovers(t *testing.T) {
	l := chkLab(t)
	rec := l.install("demo", demoFiles("demo", ""))

	base := l.paths.PluginsDir()
	idDir := filepath.Dir(l.treeDir(rec))

	must(t, os.Mkdir(filepath.Join(base, ".staging"), 0o700))
	must(t, os.Mkdir(filepath.Join(base, ".staging", "stage-1"), 0o700))
	must(t, os.Mkdir(filepath.Join(base, ".staging", "trash-1"), 0o700))
	must(t, os.Mkdir(filepath.Join(idDir, sum256("old")[:32]), 0o500))
	must(t, os.MkdirAll(filepath.Join(base, "ghost", sum256("orphan")[:32]), 0o700))
	must(t, os.Mkdir(filepath.Join(base, "Not A Plugin"), 0o700))

	l.writeFile(filepath.Join(l.paths.Config, "plugins", "gone.toml"), "schema = 1\n")
	l.writeFile(l.paths.ProcsFile(), "gone 4242 -\ngone 4243 -\ndemo 4244 -\n")
	must(t, os.Chmod(l.paths.ProcsFile(), 0o600))

	reg := l.reg(nil)
	before := snapshotFiles(t, l.root)
	d := reg.Diagnose(context.Background(), true)
	after := snapshotFiles(t, l.root)

	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("the diagnosis changed the files:\n%s\n--\n%s", strings.Join(before, "\n"),
			strings.Join(after, "\n"))
	}

	kinds := map[string]bool{}
	for _, lf := range d.Leftovers {
		kinds[lf.Kind+":"+lf.ID] = true
	}

	for _, want := range []string{
		LeftStaging + ":", LeftTrash + ":", LeftTreeOld + ":demo",
		LeftTreeOrphan + ":ghost", LeftSettings + ":gone",
	} {
		if !kinds[want] {
			t.Errorf("no leftover %s in %+v", want, d.Leftovers)
		}
	}

	if len(d.Orphans) != 1 || d.Orphans[0].Name != "Not A Plugin" {
		t.Errorf("orphans %+v", d.Orphans)
	}

	if got := codes(d.Findings); len(got) != 1 || got[0] != CodeProcsOrphan ||
		d.Findings[0].Args[0] != "gone" {
		t.Errorf("findings %v", d.Findings)
	}

	// The tree of ghost has no record: the plugin is broken, and the check fails for it alone.
	for _, p := range d.Plugins {
		if p.ID == "ghost" != p.Failed() {
			t.Errorf("%s: Failed() = %v", p.ID, p.Failed())
		}
	}

	must(t, os.Chmod(l.paths.ProcsFile(), 0o644))

	if d = diag(l, false); !d.Failed() || d.Findings[0].Code != CodeProcsUnsafe {
		t.Errorf("an unsafe plugin-procs is an error: %+v", d.Findings)
	}
}

// TestDiagnoseProblems checks that a lock that does not parse is a problem of every plugin and
// fails the diagnosis, and that it is not also reported as a leftover error.
//
// Parameters:
//   - t: test to report to
func TestDiagnoseProblems(t *testing.T) {
	l := chkLab(t)
	l.install("demo", demoFiles("demo", ""))
	l.writeFile(l.paths.LockFile(), "this is [ not toml")

	d := diag(l, false)

	if !d.Failed() || len(d.Problems) != 1 || d.Problems[0].Code != ReasonLockUnreadable ||
		len(d.Findings) != 0 {
		t.Errorf("problems %+v findings %+v", d.Problems, d.Findings)
	}

	if len(d.Plugins) != 1 || d.Plugins[0].State != StateBroken || !d.Plugins[0].Failed() {
		t.Errorf("plugins %+v", d.Plugins)
	}
}

// TestDiagnoseFindsDefectsOfPlugins checks what the diagnosis lists per plugin: a manifest that
// needs a reserved feature, a deprecated key, and a setting value that was refused.
//
// Parameters:
//   - t: test to report to
func TestDiagnoseFindsDefectsOfPlugins(t *testing.T) {
	l := chkLab(t)

	installRaw(t, l, "needy", map[string]string{"plugin.toml": strings.Replace(decl("", tool,
		cmdBody+"\n[[hook]]\nid = \"h\"\n"), `id = "demo"`, `id = "needy"`, 1)})

	saved := deprecatedKeys
	deprecatedKeys = map[string]string{"command.legacy": "run"}

	defer func() { deprecatedKeys = saved }()

	legacy := strings.Replace(cmdBody, "output = \"pager\"\n", "output = \"pager\"\nlegacy = 1\n", 1)
	old := strings.Replace(decl("", tool, legacy), `id = "demo"`, `id = "old"`, 1)
	installRaw(t, l, "old", map[string]string{"plugin.toml": old})

	l.install("fine", demoFiles("fine", "[[setting]]\nid = \"n\"\ntype = \"int\"\nlabel = \"N\"\n"))
	l.writeFile(filepath.Join(l.paths.Config, "plugins", "fine.toml"), "schema = 1\nn = \"x\"\n")

	d := diag(l, false)
	byID := map[string]*CheckResult{}

	for _, p := range d.Plugins {
		byID[p.ID] = p
	}

	if r := byID["needy"]; r == nil || !r.Has(CodeReserved) || !r.Failed() ||
		r.State != StateBroken {
		t.Errorf("needy: %+v", r)
	}

	if r := byID["old"]; r == nil || !r.Has(CodeDeprecatedKey) {
		t.Errorf("old: %+v", r)
	}

	if r := byID["fine"]; r == nil || !r.Has(CodeSettingsIgnore) || len(r.Rejected) != 1 ||
		r.Failed() {
		t.Errorf("fine: %+v", r)
	}

	if !d.Failed() {
		t.Error("a broken plugin fails the diagnosis")
	}
}
