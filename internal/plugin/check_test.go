package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// chkSeq numbers the source directories of the check tests, so that two calls of one test never
// share files.
var chkSeq atomic.Int64

// chkLab is a sandbox whose temporary directory is inside the sandbox, so that the copy a check
// makes is visible to the test and nothing is made outside it.
//
// Parameters:
//   - t: test to report to
func chkLab(t *testing.T) *lab {
	t.Helper()

	l := newLab(t)
	tmp := filepath.Join(l.root, "tmp")

	must(t, os.MkdirAll(tmp, 0o700))
	t.Setenv("TMPDIR", tmp)

	return l
}

// chkDir checks a source directory built from files and returns the result.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
//   - files: files of the package by path; a path ending in a star is a program
//   - mutate: changes the request; may be nil
func chkDir(t *testing.T, l *lab, files map[string]string,
	mutate func(*CheckRequest),
) *CheckResult {
	t.Helper()

	req := CheckRequest{Target: l.srcDir("pkg"+itoa(int(chkSeq.Add(1))), files)}
	if mutate != nil {
		mutate(&req)
	}

	res, err := l.reg(nil).Check(context.Background(), req)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	return res
}

// chkPlugin builds the files of a declarative plugin with a manifest and the extra files.
//
// Parameters:
//   - perms: permission entries
//   - body: tables after the header
//   - extra: more files by path
func chkPlugin(perms []string, body string, extra map[string]string) map[string]string {
	files := map[string]string{"plugin.toml": decl("", perms, body)}
	for k, v := range extra {
		files[k] = v
	}

	return files
}

// find returns the first finding with a code.
//
// Parameters:
//   - res: result to search
//   - code: stable code
func (c *CheckResult) find(code string) *Finding {
	for i := range c.Findings {
		if c.Findings[i].Code == code {
			return &c.Findings[i]
		}
	}

	return nil
}

// TestCheckRules plants each defect of section 8.3 in a package and checks that check reports it,
// with its code, its severity and its scope, and that the result fails as --strict says it must.
//
// Parameters:
//   - t: test to report to
func TestCheckRules(t *testing.T) {
	sshBody := "[[command]]\nid = \"go\"\nlabel = \"Go\"\nrun = [\"ssh\", \"{x}\"]\n" +
		"output = \"pager\"\n[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n"
	gitBody := "[[command]]\nid = \"go\"\nlabel = \"Go\"\nrun = [\"git\", \"-c\", \"a=b\", " +
		"\"status\"]\noutput = \"pager\"\n"
	shBody := "[[command]]\nid = \"go\"\nlabel = \"Go\"\nrun = [\"sh\", \"-c\", \"{x}\"]\n" +
		"output = \"pager\"\n[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n"
	frAll := chkFrench(t)

	tests := []struct {
		name    string
		files   map[string]string
		code    string
		sev     Severity
		scope   string
		loose   bool
		strict  bool
		pathHas string
	}{
		{
			"shell as program", chkPlugin([]string{"exec:sh"}, shBody, nil), CodeArgvShell,
			SeverityError, ScopeManifest, true, true, "permissions",
		},
		{
			"risky flag", chkPlugin([]string{"exec:git"}, gitBody, nil), CodeArgvRiskyFlag,
			SeverityWarning, ScopeManifest, false, true, "command[0]",
		},
		{
			"ssh without double dash", chkPlugin([]string{"exec:ssh"}, sshBody, nil),
			CodeArgvSSHDashDash, SeverityError, ScopeManifest, true, true, "command[0]",
		},
		{
			"unanchored pattern", chkPlugin(tool, strings.Replace(cmdBody, "label = \"X\"\n",
				"label = \"X\"\npattern = \"[0-9]+\"\n", 1), nil), CodePatternUnanchored,
			SeverityError, ScopeManifest, true, true, "command[0].field[0]",
		},
		{
			"extra file", chkPlugin(tool, cmdBody, map[string]string{"bin/x": "#!/bin/sh\n"}),
			CodeDeclarativeExtra, SeverityError, ScopePackage, true, true, "bin/x",
		},
		{"reserved table", chkPlugin(tool, cmdBody+"\n[[hook]]\nid = \"h\"\n",
			nil), CodeReserved, SeverityError, ScopeManifest, true, true, ""},
		{
			"reserved key", chkPlugin(tool, strings.Replace(cmdBody, "label = \"X\"\n",
				"label = \"X\"\nsecret = true\n", 1), nil), CodeReserved, SeverityError,
			ScopeManifest, true, true, "",
		},
		{
			"unknown key", chkPlugin(tool, cmdBody+"colour = 1\n", nil), CodeUnknownKey,
			SeverityError, ScopeManifest, true, true, "",
		},
		{
			"unused exec", chkPlugin([]string{"exec:tool", "exec:other"}, cmdBody, nil),
			CodeExecUnused, SeverityWarning, ScopeManifest, false, true, "",
		},
		{
			"reserved key letter", chkPlugin(tool, strings.Replace(cmdBody, "output = \"pager\"\n",
				"output = \"pager\"\nkey = \"l\"\n", 1), nil), CodeKeyReserved, SeverityWarning,
			ScopeManifest, false, true, "",
		},
		{
			"locale key missing", chkPlugin(tool, cmdBody, map[string]string{
				"locales/fr.toml": "\"title\" = \"T\"\n",
			}), CodeLocaleMissingKey, SeverityWarning,
			ScopeLocale, false, true, "locales.fr.",
		},
		{
			"locale key extra", chkPlugin(tool, cmdBody, map[string]string{
				"locales/fr.toml": frAll + "\"command.nope.label\" = \"X\"\n",
			}),
			CodeLocaleExtraKey, SeverityWarning, ScopeLocale, false, true, "locales.fr.",
		},
		{
			"locale placeholder", chkPlugin(tool, cmdBody, map[string]string{
				"locales/fr.toml": frAll + "\"command.go.label\" = \"{nope}\"\n",
			}),
			CodeLocaleInvalid, SeverityError, ScopeLocale, true, true, "locales.fr.",
		},
		{
			"locale syntax", chkPlugin(tool, cmdBody, map[string]string{
				"locales/fr.toml": "not toml [",
			}), CodeLocaleInvalid, SeverityError, ScopeLocale,
			true, true, "locales.fr",
		},
		{"program not installed", chkPlugin(tool, cmdBody+"[[requires]]\nbin = \"nothere\"\n",
			nil), ReasonMissingDep, SeverityWarning, ScopeEnv, false, false, "requires.nothere"},
		{
			"optional program not installed", chkPlugin(tool,
				cmdBody+"[[requires]]\nbin = \"nothere\"\noptional = true\n", nil),
			ReasonOptionalDepAbsent, SeverityWarning, ScopeEnv, false, false, "requires.nothere",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := chkLab(t)

			res := chkDir(t, l, tt.files, func(r *CheckRequest) { r.Strict = false })

			f := res.find(tt.code)
			if f == nil {
				t.Fatalf("no %s in %+v", tt.code, res.Findings)
			}

			if f.Severity != tt.sev || f.Scope != tt.scope {
				t.Errorf("finding %+v want severity %d scope %s", *f, tt.sev, tt.scope)
			}

			if !strings.Contains(f.Path, tt.pathHas) {
				t.Errorf("path %q want it to hold %q", f.Path, tt.pathHas)
			}

			if res.Failed() != tt.loose {
				t.Errorf("Failed() = %v want %v", res.Failed(), tt.loose)
			}

			res = chkDir(t, l, tt.files, func(r *CheckRequest) { r.Strict = true })

			if res.Failed() != tt.strict {
				t.Errorf("strict Failed() = %v want %v", res.Failed(), tt.strict)
			}

			if !res.Strict {
				t.Error("the result lost the strict flag")
			}
		})
	}
}

// chkFrench is a French catalog for cmdBody that is complete, so that a test adds exactly the
// defect it plants.
//
// Parameters:
//   - t: test to report to
func chkFrench(t *testing.T) string {
	t.Helper()

	m := mustDecode(t, decl("", tool, cmdBody))

	var b strings.Builder

	for _, k := range TranslatableKeys(m) {
		b.WriteString("\"" + k + "\" = \"FR " + k + "\"\n")
	}

	return b.String()
}

// TestCheckCleanPackage checks that a correct package has no finding about itself, with and
// without --strict, and that its parity is checked in both languages.
//
// Parameters:
//   - t: test to report to
func TestCheckCleanPackage(t *testing.T) {
	l := chkLab(t)

	files := chkPlugin(tool, cmdBody, map[string]string{
		"locales/fr.toml": chkFrench(t),
		"README.md":       "demo\n", "LICENSE": "MIT\n",
	})

	for _, strict := range []bool{false, true} {
		res := chkDir(t, l, files, func(r *CheckRequest) { r.Strict = strict })

		for _, f := range res.Findings {
			if f.intrinsic() {
				t.Errorf("strict=%v: finding %+v", strict, f)
			}
		}

		if res.Failed() {
			t.Errorf("strict=%v: Failed()", strict)
		}
	}
}

// TestCheckStrictIgnoresTheMachine checks that --strict promotes the warnings about the package
// only: a missing program and a rejected setting are the user's, not the package's.
//
// Parameters:
//   - t: test to report to
func TestCheckStrictIgnoresTheMachine(t *testing.T) {
	l := chkLab(t)

	res := chkDir(t, l, chkPlugin(tool, cmdBody+"[[requires]]\nbin = \"nothere\"\n", nil),
		func(r *CheckRequest) { r.Strict = true })

	if res.Warnings() == 0 || res.Errors() != 0 || res.Failed() {
		t.Errorf("warnings %d errors %d failed %v", res.Warnings(), res.Errors(), res.Failed())
	}

	if len(res.Requires) != 1 || res.Requires[0].Found || res.Requires[0].Bin != "nothere" {
		t.Errorf("requires %+v", res.Requires)
	}
}

// TestCheckHashesMatchInstall checks that the hashes of a directory are those an install of the
// same files records, for a declarative plugin and for an exec plugin whose program has no execute
// bit in the source: the check applies the exec bit the way the install does.
//
// Parameters:
//   - t: test to report to
func TestCheckHashesMatchInstall(t *testing.T) {
	declFiles := chkPlugin(tool, cmdBody, nil)
	execFiles := map[string]string{
		"plugin.toml": execM("", []string{"exec:tool"}, ""),
		"bin/demo":    "#!/bin/sh\nexit 0\n",
	}

	for name, files := range map[string]map[string]string{
		"declarative": declFiles,
		"exec":        execFiles,
	} {
		t.Run(name, func(t *testing.T) {
			l := chkLab(t)
			r := l.reg(nil)

			dir := l.srcDir("pkg", files)

			res, err := r.Check(context.Background(), CheckRequest{Target: dir})
			if err != nil {
				t.Fatal(err)
			}

			if res.Failed() || res.Kind != TargetDirectory {
				t.Fatalf("kind %q findings %+v", res.Kind, res.Findings)
			}

			st := plan(t, r, InstallRequest{Source: dir})
			d := st.Diff()

			if res.TreeSHA256 != d.TreeSHA256 || res.SurfaceSHA256 != d.SurfaceSHA256 ||
				res.Grant != d.Grant || strings.Join(res.Caps, ",") != strings.Join(d.Caps, ",") {
				t.Errorf("check %s %s %s\ninstall %s %s %s", res.TreeSHA256, res.SurfaceSHA256,
					res.Grant, d.TreeSHA256, d.SurfaceSHA256, d.Grant)
			}

			if res.ArchiveSHA256 != "" || res.ID != "demo" || res.Version != "1.0.0" {
				t.Errorf("archive %q id %q version %q", res.ArchiveSHA256, res.ID, res.Version)
			}

			_, err = st.Commit(Grant{Token: res.Grant})
			must(t, err)

			again, err := r.Check(context.Background(), CheckRequest{Target: "demo"})
			if err != nil {
				t.Fatal(err)
			}

			if again.Kind != TargetInstalled || again.State != StateEnabled ||
				again.TreeSHA256 != res.TreeSHA256 || again.Grant != res.Grant ||
				again.SurfaceSHA256 != res.SurfaceSHA256 {
				t.Errorf("installed check: %+v", again)
			}
		})
	}
}

// TestCheckGrantToken checks that the token a check prints is the one that --grant accepts, and
// that it changes when a command changes.
//
// Parameters:
//   - t: test to report to
func TestCheckGrantToken(t *testing.T) {
	l := chkLab(t)

	a := chkDir(t, l, chkPlugin(tool, cmdBody, nil), nil)
	b := chkDir(t, l, chkPlugin(tool, strings.Replace(cmdBody, `"{x}"`, `"{x}", "--more"`, 1),
		nil), nil)

	if !strings.HasPrefix(a.Grant, "exec:tool@") || a.Grant == b.Grant ||
		strings.Split(a.Grant, "@")[0] != strings.Split(b.Grant, "@")[0] {
		t.Errorf("tokens %q %q", a.Grant, b.Grant)
	}
}

// TestCheckInvalidManifest checks that a manifest that fails still gives a result: the findings
// with their lines, the tree hash and no capabilities.
//
// Parameters:
//   - t: test to report to
func TestCheckInvalidManifest(t *testing.T) {
	l := chkLab(t)

	res := chkDir(t, l, map[string]string{"plugin.toml": decl("", nil, "[[command]]\nid = 1\n")},
		nil)

	if res.Errors() == 0 || !res.Failed() || res.TreeSHA256 == "" || res.Grant != "" ||
		res.Caps != nil {
		t.Errorf("result %+v", res)
	}

	if f := res.find(CodeTypeInvalid); f == nil || f.Line == 0 {
		t.Errorf("finding %+v", f)
	}

	res = chkDir(t, l, map[string]string{"README.md": "x\n"}, nil)

	if f := res.find(CodeManifestMissing); f == nil || f.Path != "plugin.toml" || !res.Failed() {
		t.Errorf("no manifest: %+v", res.Findings)
	}
}

// TestCheckTargets checks the targets a check refuses, with the code of each refusal.
//
// Parameters:
//   - t: test to report to
func TestCheckTargets(t *testing.T) {
	l := chkLab(t)
	r := l.reg(nil)

	tests := []struct {
		target string
		code   string
	}{
		{"", CodeBadSource},
		{"./nowhere", CodeBadSource},
		{"https://github.com/o/r/releases/download/1.0.0/plugin-r.tar.gz", CodeSourceUnsupported},
		{"o/r@1.0.0", CodeSourceUnsupported},
		{"Bad_ID", CodeBadID},
		{"ghost", CodeNotInstalled},
	}

	for _, tt := range tests {
		_, err := r.Check(context.Background(), CheckRequest{Target: tt.target})

		if pe := asError(t, err); pe.Code != tt.code {
			t.Errorf("%q: %s want %s", tt.target, pe.Code, tt.code)
		}
	}
}

// TestCheckHostileDirectory checks that a directory an install would refuse gives a finding and
// not a crash, and that nothing is left behind.
//
// Parameters:
//   - t: test to report to
func TestCheckHostileDirectory(t *testing.T) {
	l := chkLab(t)
	good := chkPlugin(tool, cmdBody, nil)

	dir := l.srcDir("evil", good)
	must(t, os.Symlink("/etc/passwd", filepath.Join(dir, "LICENSE")))

	res, err := l.reg(nil).Check(context.Background(), CheckRequest{Target: dir})
	if err != nil {
		t.Fatal(err)
	}

	if f := res.find(CodeSourceUnsafe); f == nil || f.Args[1] != UnsafeSymlink ||
		f.Severity != SeverityError || !res.Failed() {
		t.Errorf("symlink: %+v", res.Findings)
	}

	long := l.srcDir("long", good)
	for i := 0; i < MaxTreeEntries+1; i++ {
		must(t, os.WriteFile(filepath.Join(long, "README"+itoa(i)), []byte("x"), 0o600))
	}

	res, err = l.reg(nil).Check(context.Background(), CheckRequest{Target: long})
	if err != nil {
		t.Fatal(err)
	}

	if f := res.find(CodeSourceLimits); f == nil || f.Args[1] != LimitEntries {
		t.Errorf("entries: %+v", res.Findings)
	}

	left, _ := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "tote-check-*"))
	if len(left) != 0 {
		t.Errorf("temporary directories left behind: %v", left)
	}
}

// TestCheckRunsNothing checks that a check starts none of the programs a plugin names, for a
// directory and for an installed plugin: a canary program would leave a file.
//
// Parameters:
//   - t: test to report to
func TestCheckRunsNothing(t *testing.T) {
	l := chkLab(t)
	l.useSystemTools()

	canary := filepath.Join(l.root, "canary")
	l.script("tool", "echo ran > "+canary+"\n")

	r := l.reg(nil)

	dirRes := chkDir(t, l, chkPlugin(tool, cmdBody, nil), func(q *CheckRequest) {
		q.Set = map[string]string{"x": "1"}
	})

	if dirRes.Contributions[0].Program == "" {
		t.Fatalf("the canary program was not found: %+v", dirRes.Contributions)
	}

	l.install("demo", chkPlugin(tool, cmdBody, nil))

	if _, err := r.Check(context.Background(), CheckRequest{Target: "demo"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(canary); err == nil {
		t.Error("check started the program of the plugin")
	}
}

// TestCheckArchive checks an archive file: its sha256 is the one an install would pin, its
// hashes and grant token are those of the same files as a directory, and an archive that an install
// would refuse gives a finding and not an error.
//
// Parameters:
//   - t: test to report to
func TestCheckArchive(t *testing.T) {
	l := chkLab(t)
	r := l.reg(nil)

	data := demoArchive(t, "1.0.0")
	path := filepath.Join(l.root, "plugin-demo.tar.gz")
	must(t, os.WriteFile(path, data, 0o600))

	res, err := r.Check(context.Background(), CheckRequest{Target: path})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if res.Kind != TargetArchive || res.Failed() || res.ID != "demo" ||
		res.ArchiveSHA256 != sumOf(data) {
		t.Fatalf("kind %q failed %v id %q archive %q", res.Kind, res.Failed(), res.ID,
			res.ArchiveSHA256)
	}

	st := plan(t, r, InstallRequest{Source: path})
	d := st.Diff()
	_ = st.Abort()

	if res.TreeSHA256 != d.TreeSHA256 || res.Grant != d.Grant ||
		res.ArchiveSHA256 != d.ArchiveSHA256 {
		t.Errorf("check %s %s, install %s %s", res.TreeSHA256, res.Grant, d.TreeSHA256, d.Grant)
	}

	bad := filepath.Join(l.root, "bad.tar.gz")
	must(t, os.WriteFile(bad, []byte("not an archive"), 0o600))

	res, err = r.Check(context.Background(), CheckRequest{Target: bad})
	if err != nil || !res.Failed() || !res.Has(CodeArchiveFormat) {
		t.Errorf("bad archive: %v, %+v", err, res)
	}

	hostile := filepath.Join(l.root, "hostile.tar.gz")
	must(t, os.WriteFile(hostile,
		tgz(t, file("../evil", "x"), file("plugin.toml", "api = 1\n")), 0o600))

	res, err = r.Check(context.Background(), CheckRequest{Target: hostile})
	if err != nil || !res.Failed() || !res.Has(CodeArchiveUnsafe) {
		t.Errorf("hostile archive: %v, %+v", err, res)
	}
}
