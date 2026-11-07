package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// stagingNames lists the entries of the staging directory of a sandbox.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
func stagingNames(t *testing.T, l *lab) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(l.paths.PluginsDir(), ".staging"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		t.Fatal(err)
	}

	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}

	return out
}

// demoSource writes the source directory of a small declarative plugin and puts its program in the
// fake PATH.
//
// Parameters:
//   - l: sandbox
//   - id: plugin id
//   - version: plugin version
func demoSource(l *lab, id, version string) string {
	l.fakeBin("tool")

	return l.srcDir(id+"-"+version, map[string]string{
		"plugin.toml": manifest(id, version, []string{"exec:tool"}, cmdWith("go", "")),
		"README.md":   "# demo\n",
	})
}

// TestInstallLocalDirectory installs a directory and checks every piece of the result: the
// record, the read-only tree, the directories of the plugin, an empty staging directory and an
// untouched source.
//
// Parameters:
//   - t: test to report to
func TestInstallLocalDirectory(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")
	r := l.reg(nil)

	before, _ := os.Stat(filepath.Join(src, "plugin.toml"))

	st := plan(t, r, InstallRequest{Source: src})
	d := st.Diff()

	if !d.NeedsConsent || len(d.Why) != 1 || d.Why[0] != WhyNew || d.Update ||
		d.Class != OriginLocal || d.Source != src || d.Kind != KindLocal ||
		d.Tier != TierDeclarative {
		t.Errorf("diff = %+v", d)
	}

	want := GrantToken(d.Caps, d.SurfaceSHA256)
	if d.Grant != want || !strings.Contains(d.Grant, "@") {
		t.Errorf("grant %q, want %q", d.Grant, want)
	}

	if len(d.Commands) != 1 || d.Commands[0].ID != "go" || len(d.Missing) != 0 {
		t.Errorf("commands %+v missing %+v", d.Commands, d.Missing)
	}

	if names := stagingNames(t, l); len(names) != 1 || !strings.HasPrefix(names[0], stagePrefix) {
		t.Errorf("staging before commit = %v", names)
	}

	if p, ok := r.Recheck("demo"); ok && p.Installed {
		t.Fatal("Plan installed something")
	}

	res, err := st.Commit(Grant{Token: d.Grant})
	if err != nil {
		t.Fatal(err)
	}

	if res.State != StateEnabled || res.ConsentedBy != ConsentGrant || res.Updated ||
		len(res.Leftovers) != 0 || len(res.Warnings) != 0 {
		t.Errorf("result = %+v", res)
	}

	rec, ok := l.lock().Get("demo")
	if !ok || rec.Kind != KindLocal || rec.Source != src || rec.ConsentedBy != ConsentGrant ||
		rec.SurfaceSHA256 != d.SurfaceSHA256 || rec.TreeSHA256 != d.TreeSHA256 || rec.Disabled ||
		rec.InstalledAt == "" || rec.InstalledAt != rec.UpdatedAt || len(rec.Caps) == 0 {
		t.Fatalf("record = %+v", rec)
	}

	tree := l.treeDir(rec)
	checkMode(t, tree, 0o500)
	checkMode(t, filepath.Join(tree, "plugin.toml"), 0o400)
	checkMode(t, filepath.Join(tree, "README.md"), 0o400)

	for _, dir := range []func(string) (string, error){
		l.paths.PluginStateDir,
		l.paths.PluginCacheDir,
	} {
		path, _ := dir("demo")
		checkMode(t, path, 0o700)
	}

	if names := stagingNames(t, l); len(names) != 0 {
		t.Errorf("staging after commit = %v", names)
	}

	after, _ := os.Stat(filepath.Join(src, "plugin.toml"))
	if !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		t.Error("the source was touched")
	}

	if _, err := st.Commit(Grant{Prompt: true}); asError(t, err).Code != CodeStale {
		t.Errorf("second Commit = %v, want stale", err)
	}

	if err := st.Abort(); err != nil {
		t.Errorf("Abort after Commit = %v", err)
	}
}

// checkMode fails the test when a path does not have the exact permission bits.
//
// Parameters:
//   - t: test to report to
//   - path: file or directory
//   - want: expected permission bits
func checkMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != want || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		t.Errorf("%s has mode %v, want %v", path, info.Mode(), want)
	}
}

// TestInstallConsent checks the grant rules of Commit: none gives consent_needed (exit 2), a
// token that differs gives grant_mismatch (exit 4), neither spends the staged package, and a
// prompt or the right token install.
//
// Parameters:
//   - t: test to report to
func TestInstallConsent(t *testing.T) {
	l := newLab(t)
	r := l.reg(nil)
	st := plan(t, r, InstallRequest{Source: demoSource(l, "demo", "1.0.0")})

	_, err := st.Commit(Grant{})
	if pe := asError(t, err); pe.Code != CodeConsentNeeded || ExitOf(err) != ExitUsage ||
		len(pe.Args) != 1 || pe.Args[0] != st.Diff().Grant {
		t.Errorf("no grant: %v exit %d", err, ExitOf(err))
	}

	for _, g := range []Grant{{Token: "exec:other@000000000000"}, {Token: "x", Prompt: true}} {
		_, err = st.Commit(g)
		if pe := asError(t, err); pe.Code != CodeGrantMismatch || ExitOf(err) != ExitPolicy {
			t.Errorf("grant %+v: %v exit %d", g, err, ExitOf(err))
		}
	}

	if rec, ok := l.lock().Get("demo"); ok {
		t.Fatalf("a refused grant installed %+v", rec)
	}

	if names := stagingNames(t, l); len(names) != 1 {
		t.Fatalf("a refused grant spent the staged package: %v", names)
	}

	res, err := st.Commit(Grant{Prompt: true, Token: st.Diff().Grant})
	if err != nil || res.ConsentedBy != ConsentPrompt {
		t.Errorf("prompt: %+v %v", res, err)
	}
}

// TestAbort checks that Abort deletes the staging directory, that it can be called twice and that
// a package that was aborted cannot be committed.
//
// Parameters:
//   - t: test to report to
func TestAbort(t *testing.T) {
	l := newLab(t)
	r := l.reg(nil)
	st := plan(t, r, InstallRequest{Source: demoSource(l, "demo", "1.0.0")})

	if err := st.Abort(); err != nil {
		t.Fatal(err)
	}

	if err := st.Abort(); err != nil {
		t.Fatal(err)
	}

	if names := stagingNames(t, l); len(names) != 0 {
		t.Errorf("staging = %v", names)
	}

	if _, err := st.Commit(Grant{Prompt: true}); asError(t, err).Code != CodeStale {
		t.Errorf("Commit after Abort = %v", err)
	}

	if _, ok := l.lock().Get("demo"); ok {
		t.Error("an aborted install is in the lock")
	}
}

// TestInstallIsTheCopy checks that what is installed is what was hashed and consented, not what
// the source holds at the time of the commit.
//
// Parameters:
//   - t: test to report to
func TestInstallIsTheCopy(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")
	r := l.reg(nil)
	st := plan(t, r, InstallRequest{Source: src})

	evil := manifest("demo", "1.0.0", []string{"exec:tool", "net:evil.example"}, cmdWith("go", ""))
	must(t, os.WriteFile(filepath.Join(src, "plugin.toml"), []byte(evil), 0o644))

	res, err := st.Commit(Grant{Prompt: true})
	if err != nil {
		t.Fatal(err)
	}

	rec, _ := l.lock().Get("demo")
	for _, c := range rec.Caps {
		if strings.Contains(c, "evil") {
			t.Errorf("caps %v follow the source after the consent", rec.Caps)
		}
	}

	if res.State != StateEnabled {
		t.Errorf("state %s %v", res.State, res.Reasons)
	}
}

// TestInstallTamperedStaging checks that a staged copy altered between Plan and Commit is not
// installed: the hash is checked again under the mutex.
//
// Parameters:
//   - t: test to report to
func TestInstallTamperedStaging(t *testing.T) {
	l := newLab(t)
	r := l.reg(nil)
	st := plan(t, r, InstallRequest{Source: demoSource(l, "demo", "1.0.0")})

	stage := filepath.Join(l.paths.PluginsDir(), ".staging", stagingNames(t, l)[0])
	must(t, os.WriteFile(filepath.Join(stage, "plugin.toml"), []byte("api = 1\n"), 0o600))

	_, err := st.Commit(Grant{Prompt: true})
	if pe := asError(t, err); pe.Code != CodeTreeHash || ExitOf(err) != ExitIntegrity {
		t.Fatalf("Commit = %v", err)
	}

	if _, ok := l.lock().Get("demo"); ok {
		t.Error("a tampered copy was installed")
	}

	if names := stagingNames(t, l); len(names) != 0 {
		t.Errorf("staging = %v", names)
	}
}

// TestClassifySource checks the forms of a source: a path starts with a slash, ./, ../ or ~/, a
// bare id is a bundled plugin, and the release forms and archives are recognised but not
// installable yet.
//
// Parameters:
//   - t: test to report to
func TestClassifySource(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")
	must(t, os.MkdirAll(l.cwd(), 0o700))
	must(t, os.Symlink(src, filepath.Join(l.cwd(), "link")))
	must(t, os.WriteFile(filepath.Join(l.root, "demo.tar.gz"), []byte("x"), 0o600))
	must(t, os.Symlink(src, filepath.Join(l.home, "plug")))

	r := l.reg(nil)

	tests := []struct {
		name string
		src  string
		code string
	}{
		{"empty", "", CodeBadSource},
		{"bare id, nothing embedded", "demo", CodeNoEmbedded},
		{"upper case word", "Demo", CodeBadSource},
		{
			"release url without a pin", "https://github.com/o/r/releases/download/1.0.0/p.tar.gz",
			CodeHashRequired,
		},
		{"http url", "http://example.com/p.tar.gz", CodeBadSource},
		{"url of another host", "https://example.com/p.tar.gz", CodeBadSource},
		{"shorthand without a pin", "owner/repo@1.0.0", CodeHashRequired},
		{"shorthand without tag", "owner/repo", CodeBadSource},
		{"file that is not an archive", filepath.Join(l.root, "demo.tar.gz"), CodeArchiveFormat},
		{"missing path", filepath.Join(l.root, "absent"), CodeBadSource},
		{"control character", "/tmp/a\x01b", CodeBadSource},
		{"relative to the working directory", "./link", ""},
		{"home", "~/plug", ""},
		{"absolute", src, ""},
		{"with dots", src + "/../" + filepath.Base(src), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := r.Plan(context.Background(), InstallRequest{Source: tt.src})

			if tt.code == "" {
				if err != nil {
					t.Fatal(err)
				}

				if got := st.Diff().Source; got != filepath.Clean(got) || !filepath.IsAbs(got) {
					t.Errorf("recorded source %q is not a clean absolute path", got)
				}

				_ = st.Abort()

				return
			}

			if err == nil {
				_ = st.Abort()

				t.Fatal("accepted")
			}

			if got := asError(t, err).Code; got != tt.code {
				t.Errorf("code = %s, want %s", got, tt.code)
			}
		})
	}

	if got := ExitClass(CodeSourceUnsupported); got != ExitUsage {
		t.Errorf("source_unsupported exits %d", got)
	}
}

// TestCopierFoldsCase checks the case rule of a source directory on its own, so that it is covered
// on file systems that cannot hold two names which differ in case.
//
// Parameters:
//   - t: test to report to
func TestCopierFoldsCase(t *testing.T) {
	c := &copier{ctx: context.Background()}
	folded := map[string]bool{}

	if err := c.check("README", srcEntry{Name: "README"}, 1, folded); err != nil {
		t.Fatal(err)
	}

	err := c.check("readme", srcEntry{Name: "readme"}, 1, folded)
	if err == nil {
		t.Fatal("two names that differ in case were accepted")
	}

	if pe := asError(t, err); pe.Code != CodeSourceUnsafe || pe.Args[len(pe.Args)-1] != UnsafeCase {
		t.Errorf("error = %v, want %s %s", err, CodeSourceUnsafe, UnsafeCase)
	}

	// The same name in another directory is a different entry.
	if err := c.check("dir/README", srcEntry{Name: "README"}, 2, map[string]bool{}); err != nil {
		t.Errorf("same name in another directory: %v", err)
	}
}

// TestInstallHostileSource feeds Plan with directories that break the rules of section 7.3 and
// checks that each is refused with its code and the entry, and that nothing is left in staging or
// in the plugins directory. A canary file outside proves that nothing was written beyond.
//
// Parameters:
//   - t: test to report to
func TestInstallHostileSource(t *testing.T) {
	base := map[string]string{"plugin.toml": manifest("demo", "1.0.0", []string{"exec:tool"},
		cmdWith("go", ""))}

	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		code  string
		arg   string
	}{
		{"symlink to a file", func(t *testing.T, dir string) {
			must(t, os.Symlink("/etc/passwd", filepath.Join(dir, "README")))
		}, CodeSourceUnsafe, "README"},
		{"symlink to a directory", func(t *testing.T, dir string) {
			must(t, os.Symlink(t.TempDir(), filepath.Join(dir, "locales")))
		}, CodeSourceUnsafe, "locales"},
		{"dangling symlink", func(t *testing.T, dir string) {
			must(t, os.Symlink("/nonexistent", filepath.Join(dir, "LICENSE")))
		}, CodeSourceUnsafe, "LICENSE"},
		{"fifo", func(t *testing.T, dir string) {
			must(t, syscall.Mkfifo(filepath.Join(dir, "README"), 0o600))
		}, CodeSourceUnsafe, "README"},
		{"space in a name", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "a b"), nil, 0o600))
		}, CodeSourceUnsafe, "a b"},
		{"leading dash", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "-rf"), nil, 0o600))
		}, CodeSourceUnsafe, "-rf"},
		{"non ascii name", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "r\u00e9sum\u00e9"), nil, 0o600))
		}, CodeSourceUnsafe, ""},
		{"names that differ in case", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "README"), nil, 0o600))
			must(t, os.WriteFile(filepath.Join(dir, "readme"), nil, 0o600))

			// A case-insensitive file system (the default on macOS) folds the two names into one
			// file, so the directory cannot hold the hostile layout. TestCopierFoldsCase checks
			// the rule without a file system.
			if _, err := os.Lstat(filepath.Join(dir, "Readme")); err == nil {
				t.Skip("this file system cannot hold two names that differ in case")
			}
		}, CodeSourceUnsafe, ""},
		{"too deep", func(t *testing.T, dir string) {
			deep := filepath.Join(dir, "a", "b", "c", "d", "e", "f")
			must(t, os.MkdirAll(deep, 0o755))
			must(t, os.WriteFile(filepath.Join(deep, "g"), nil, 0o600))
		}, CodeSourceLimits, LimitDepth},
		{"too many entries", func(t *testing.T, dir string) {
			for i := range MaxTreeEntries + 1 {
				must(t, os.WriteFile(filepath.Join(dir, "f"+strings.Repeat("a", 3)+itoa(i)), nil,
					0o600))
			}
		}, CodeSourceLimits, LimitEntries},
		{"file over the limit", func(t *testing.T, dir string) {
			f := filepath.Join(dir, "README")
			must(t, os.WriteFile(f, nil, 0o600))
			must(t, os.Truncate(f, MaxTreeBytes+1))
		}, CodeSourceLimits, LimitBytes},
		{"files over the limit together", func(t *testing.T, dir string) {
			for _, n := range []string{"README", "LICENSE"} {
				f := filepath.Join(dir, n)
				must(t, os.WriteFile(f, nil, 0o600))
				must(t, os.Truncate(f, MaxTreeBytes/2+1))
			}
		}, CodeSourceLimits, LimitBytes},
		{"path too long", func(t *testing.T, dir string) {
			long := dir
			for _, c := range "abcde" {
				long = filepath.Join(long, strings.Repeat(string(c), 56))
			}

			must(t, os.MkdirAll(long, 0o755))
			must(t, os.WriteFile(filepath.Join(long, "f"), nil, 0o600))
		}, CodeSourceLimits, LimitPath},
		{"a program in a declarative package", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"), 0o755))
		}, CodeDeclarativeExtra, "run.sh"},
		{"no manifest", func(t *testing.T, dir string) {
			must(t, os.Remove(filepath.Join(dir, "plugin.toml")))
		}, CodeManifestInvalid, manifestName},
		{"manifest over the limit", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "plugin.toml"),
				[]byte(strings.Repeat("#", MaxManifestSize+10)), 0o600))
		}, CodeManifestTooLarge, ""},
		{"reserved id", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(manifest("help", "1.0.0",
				[]string{"exec:tool"}, cmdWith("go", ""))), 0o600))
		}, CodeIDInvalid, ""},
		{"official id from a directory", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(manifest("gft", "1.0.0",
				[]string{"exec:tool"}, cmdWith("go", ""))), 0o600))
		}, CodeIDInvalid, ""},
		{"refused permission", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(manifest("demo", "1.0.0",
				[]string{"exec:tool", "hook:intercept"}, cmdWith("go", ""))), 0o600))
		}, CodePermRefused, ""},
		{"exec on tote", func(t *testing.T, dir string) {
			must(t, os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(manifest("demo", "1.0.0",
				[]string{"exec:tote"}, cmdWith("go", ""))), 0o600))
		}, CodeExecSelf, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			l.fakeBin("tool")

			canary := filepath.Join(l.root, "canary")
			must(t, os.WriteFile(canary, []byte("alive"), 0o600))

			dir := l.srcDir("hostile", base)
			tt.setup(t, dir)

			pe := planErr(t, l.reg(nil), InstallRequest{Source: dir})
			if pe.Code != tt.code {
				t.Fatalf("code = %s (%v), want %s", pe.Code, pe, tt.code)
			}

			if tt.arg != "" && (len(pe.Args) == 0 ||
				!strings.Contains(strings.Join(pe.Args, " "), tt.arg)) {
				t.Errorf("args = %v, want one with %q", pe.Args, tt.arg)
			}

			if names := stagingNames(t, l); len(names) != 0 {
				t.Errorf("staging = %v", names)
			}

			entries, _ := os.ReadDir(l.paths.PluginsDir())
			for _, e := range entries {
				if !strings.HasPrefix(e.Name(), ".") {
					t.Errorf("the plugins directory holds %s", e.Name())
				}
			}

			if data, err := os.ReadFile(canary); err != nil || string(data) != "alive" {
				t.Error("canary changed")
			}

			if _, ok := l.lock().Get("demo"); ok {
				t.Error("a refused package is in the lock")
			}
		})
	}
}

// TestInstallExecTier checks the modes of an exec package: only the declared program keeps an
// execute bit, so a package that arrives with every file executable hashes exactly like one where
// only the program is, and the installed program is 0500.
//
// Parameters:
//   - t: test to report to
func TestInstallExecTier(t *testing.T) {
	m := strings.Replace(execM("", []string{"exec:tool"}, ""), `id = "demo"`, `id = "ex"`, 1)

	l := newLab(t)
	r := l.reg(nil)

	loose := l.srcDir("loose", map[string]string{
		"plugin.toml": m, "bin/demo*": "#!/bin/sh\n",
		"extra.sh*": "#!/bin/sh\n", "README*": "x\n",
	})
	tight := l.srcDir("tight", map[string]string{
		"plugin.toml": m, "bin/demo*": "#!/bin/sh\n",
		"extra.sh": "#!/bin/sh\n", "README": "x\n",
	})

	a := plan(t, r, InstallRequest{Source: loose})
	b := plan(t, r, InstallRequest{Source: tight})

	if a.Diff().TreeSHA256 != b.Diff().TreeSHA256 {
		t.Error("the hash follows execute bits that the manifest does not declare")
	}

	if err := b.Abort(); err != nil {
		t.Fatal(err)
	}

	d := a.Diff()
	if d.Tier != TierExec || !contains(d.Caps, CapDerivedCode) {
		t.Errorf("diff = %+v", d)
	}

	res, err := a.Commit(Grant{Prompt: true})
	if err != nil {
		t.Fatal(err)
	}

	rec, _ := l.lock().Get("ex")
	tree := l.treeDir(rec)

	checkMode(t, filepath.Join(tree, "bin", "demo"), 0o500)
	checkMode(t, filepath.Join(tree, "bin"), 0o500)
	checkMode(t, filepath.Join(tree, "extra.sh"), 0o400)
	checkMode(t, filepath.Join(tree, "README"), 0o400)

	if res.State != StateEnabled {
		t.Errorf("state %s %v", res.State, res.Reasons)
	}
}

// contains reports whether a list holds a string.
//
// Parameters:
//   - list: list to search
//   - s: string to find
func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}

	return false
}

// TestInstallMissingProgram checks that a required program that PATH lacks is a warning of the
// diff and that the plugin installs as missing-dependency, with the hint kept as data.
//
// Parameters:
//   - t: test to report to
func TestInstallMissingProgram(t *testing.T) {
	l := newLab(t)
	r := l.reg(nil)

	src := l.srcDir("dep", map[string]string{"plugin.toml": manifest("dep", "1.0.0",
		[]string{"exec:tool"},
		cmdWith("go", "")+"\n[[requires]]\nbin = \"tool\"\nhint = \"get it\"\n")})

	st := plan(t, r, InstallRequest{Source: src})
	d := st.Diff()

	if len(d.Missing) != 1 || d.Missing[0].Bin != "tool" || d.Missing[0].Hint != "get it" ||
		!hasReason(d.Warnings, ReasonMissingDep) {
		t.Errorf("diff = %+v", d)
	}

	res, err := st.Commit(Grant{Prompt: true})
	if err != nil || res.State != StateMissingDependency {
		t.Errorf("result = %+v %v", res, err)
	}
}

// TestInstallCancelled checks that a cancelled context stops the copy and leaves nothing.
//
// Parameters:
//   - t: test to report to
func TestInstallCancelled(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if st, err := l.reg(nil).Plan(ctx, InstallRequest{Source: src}); err == nil {
		_ = st.Abort()

		t.Fatal("Plan ran with a cancelled context")
	}

	if names := stagingNames(t, l); len(names) != 0 {
		t.Errorf("staging = %v", names)
	}
}

// TestInstallBundled installs an embedded plugin by extraction, checks the pin, and refuses a
// copy that does not match it.
//
// Parameters:
//   - t: test to report to
func TestInstallBundled(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")

	files := map[string]string{"plugin.toml": manifest("demo", "1.0.0", []string{"exec:tool"},
		cmdWith("go", ""))}
	b := bundle(t, l, files)
	r := l.reg(func(o *Options) { o.Bundled = b })

	st := plan(t, r, InstallRequest{Source: "demo"})
	d := st.Diff()

	if d.Class != OriginBundled || d.Kind != KindBundled || d.Source != "" || !d.NeedsConsent {
		t.Errorf("diff = %+v", d)
	}

	res, err := st.Commit(Grant{Prompt: true})
	if err != nil || res.State != StateEnabled {
		t.Fatalf("result = %+v %v", res, err)
	}

	rec, _ := l.lock().Get("demo")
	if rec.Kind != KindBundled || rec.Source != "" || rec.Origin != "" || rec.ArchiveSHA256 != "" {
		t.Errorf("record = %+v", rec)
	}

	if pe := planErr(t, r, InstallRequest{Source: "absent"}); pe.Code != CodeNoEmbedded ||
		ExitOf(pe) != ExitUsage {
		t.Errorf("unknown bundled id: %v", pe)
	}

	if pe := planErr(t, r, InstallRequest{Source: "demo", ID: "other"}); pe.Code != CodeIDMismatch {
		t.Errorf("id mismatch: %v", pe)
	}

	l2 := newLab(t)
	l2.fakeBin("tool")

	tampered := *b
	tampered.tree = strings.Repeat("0", 64)

	r2 := l2.reg(func(o *Options) { o.Bundled = &tampered })
	if pe := planErr(t, r2, InstallRequest{Source: "demo"}); pe.Code != CodeTreeHash ||
		ExitOf(pe) != ExitIntegrity {
		t.Errorf("pin mismatch: %v", pe)
	}

	wrongSurface := *b
	wrongSurface.surface = strings.Repeat("0", 64)

	r3 := l2.reg(func(o *Options) { o.Bundled = &wrongSurface })
	if pe := planErr(t, r3, InstallRequest{Source: "demo"}); pe.Code != CodeTreeHash {
		t.Errorf("surface pin mismatch: %v", pe)
	}

	if names := stagingNames(t, l2); len(names) != 0 {
		t.Errorf("staging after a pin mismatch = %v", names)
	}
}

// TestInstallOfficialIDOnlyBundled checks that an official id installs from the embedded copy and
// from nowhere else.
//
// Parameters:
//   - t: test to report to
func TestInstallOfficialIDOnlyBundled(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")

	files := map[string]string{"plugin.toml": manifest("gft", "1.0.0", []string{"exec:tool"},
		cmdWith("go", ""))}
	b := bundle(t, l, files)
	r := l.reg(func(o *Options) { o.Bundled = b })

	st := plan(t, r, InstallRequest{Source: "gft"})
	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}

	dir := l.srcDir("gft-copy", files)
	req := InstallRequest{Source: dir, ID: "gft", Update: true, Force: true}
	if pe := planErr(t, r, req); pe.Code != CodeIDInvalid {
		t.Errorf("a directory claimed the id gft: %v", pe)
	}
}

// TestPlanNeedsAHealthyLock checks that a lock that does not parse stops Plan before anything is
// copied: nothing is installed over a record that cannot be read.
//
// Parameters:
//   - t: test to report to
func TestPlanNeedsAHealthyLock(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")
	l.writeFile(l.paths.LockFile(), "schema = 1\n[[plugin]\n")

	pe := planErr(t, l.reg(nil), InstallRequest{Source: src})
	if pe.Code != CodeStoreCorrupt {
		t.Errorf("code = %s", pe.Code)
	}

	if names := stagingNames(t, l); len(names) != 0 {
		t.Errorf("staging = %v", names)
	}
}

// TestInstallNoClockDependence checks that the records use the clock of the options.
//
// Parameters:
//   - t: test to report to
func TestInstallNoClockDependence(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	r := l.reg(func(o *Options) { o.Now = func() time.Time { return at } })
	installDir(t, r, src)

	rec, _ := l.lock().Get("demo")
	if rec.InstalledAt != "2026-01-02T03:04:05Z" || rec.ConsentedAt != rec.InstalledAt {
		t.Errorf("times = %s %s", rec.InstalledAt, rec.ConsentedAt)
	}
}

// TestPlanAbortLeavesFreshMachineEmpty checks that a plan that is dropped, as a dry run does,
// creates nothing on a machine that never installed a plugin, and that an install that goes
// through keeps the store.
//
// Parameters:
//   - t: test to report to
func TestPlanAbortLeavesFreshMachineEmpty(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")
	r := l.reg(nil)

	st := plan(t, r, InstallRequest{Source: src})

	if _, err := os.Stat(l.paths.PluginsDir()); err != nil {
		t.Fatalf("the plan has no staging area: %v", err)
	}

	must(t, st.Abort())

	if _, err := os.Lstat(l.paths.Data); !os.IsNotExist(err) {
		t.Errorf("a dry run left %s behind: %v", l.paths.Data, err)
	}

	st = plan(t, r, InstallRequest{Source: src})

	if _, err := st.Commit(Grant{Token: st.Diff().Grant}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	must(t, st.Abort())

	for _, name := range []string{".staging", ".mutex", "demo"} {
		if _, err := os.Lstat(filepath.Join(l.paths.PluginsDir(), name)); err != nil {
			t.Errorf("%s after an install: %v", name, err)
		}
	}
}
