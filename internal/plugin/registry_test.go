package plugin

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// load opens a registry over the sandbox and loads it.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
//   - id: plugin id to return
func load(t *testing.T, l *lab, id string) Plugin {
	t.Helper()

	p, ok := l.registry(nil).Load().Get(id)
	if !ok {
		t.Fatalf("plugin %q is not in the snapshot", id)
	}

	return p
}

// TestLoadEnabled checks the happy path: a verified, consented, compatible plugin is enabled and
// carries everything a caller needs.
//
// Parameters:
//   - t: test to report to
func TestLoadEnabled(t *testing.T) {
	l := newLab(t)
	rec := l.install("demo", demoFiles("demo", ""))

	snap := l.registry(nil).Load()

	p, ok := snap.Get("demo")
	if !ok || p.State != StateEnabled || len(p.Reasons) != 0 {
		t.Fatalf("state = %s, reasons = %v", p.State, p.Reasons)
	}

	if !p.Installed || p.Manifest == nil || p.Manifest.ID != "demo" || p.Header.ID != "demo" {
		t.Errorf("plugin = %+v", p)
	}

	if p.Dir != l.treeDir(rec) || !slices.Equal(p.Caps, []string{"exec:tool"}) ||
		p.Surface != rec.SurfaceSHA256 || p.Grant != GrantToken(p.Caps, p.Surface) {
		t.Errorf("dir %q caps %v surface %q grant %q", p.Dir, p.Caps, p.Surface, p.Grant)
	}

	if len(snap.Problems) != 0 || len(snap.Orphans) != 0 || len(p.Stale) != 0 {
		t.Errorf("problems %v orphans %v stale %v", snap.Problems, snap.Orphans, p.Stale)
	}

	if got := snap.Count(); got != (Counts{Total: 1, Enabled: 1}) {
		t.Errorf("Count = %+v", got)
	}

	if en := snap.Enabled(); len(en) != 1 || en[0].ID != "demo" {
		t.Errorf("Enabled = %v", en)
	}

	if _, ok := snap.Get("nope"); ok {
		t.Error("Get found a plugin that is not there")
	}
}

// TestLoadEmpty checks that nothing installed, and not even a plugins directory, is no plugin and
// no problem.
//
// Parameters:
//   - t: test to report to
func TestLoadEmpty(t *testing.T) {
	snap := newLab(t).registry(nil).Load()

	if len(snap.Plugins()) != 0 || len(snap.Problems) != 0 || len(snap.Orphans) != 0 {
		t.Errorf("snapshot = %+v", snap)
	}
}

// TestStates drives the registry through every state, each time from a healthy install, and
// checks the state and the code of the reason that explains it.
//
// Parameters:
//   - t: test to report to
func TestStates(t *testing.T) {
	tests := []struct {
		name   string
		files  map[string]string
		raw    bool
		setup  func(l *lab, rec LockRecord)
		mutate func(o *Options)
		state  State
		code   string
	}{
		{"healthy", nil, false, nil, nil, StateEnabled, ""},
		{"disabled by config", nil, false, func(l *lab, _ LockRecord) {
			l.writeConfig("[plugins.demo]\nenabled = false\n")
		}, nil, StateDisabled, ReasonConfigOff},
		{"enabled by config", nil, false, func(l *lab, _ LockRecord) {
			l.writeConfig("[plugins.demo]\nenabled = true\n")
		}, nil, StateEnabled, ""},
		{"disabled by the lock flag", nil, false, func(l *lab, _ LockRecord) {
			must(t, l.lock().SetDisabled("demo", true))
		}, nil, StateDisabled, ReasonLockOff},
		{
			"disabled for the session", nil, false, nil, func(o *Options) { o.NoPlugins = true },
			StateDisabled, ReasonSessionOff,
		},
		{"never consented", nil, false, func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.ConsentedAt, r.ConsentedBy = "", "" }))
		}, nil, StateNeedsConsent, ReasonNotConsented},
		{"capability not consented", nil, false, func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.Caps = nil }))
		}, nil, StateNeedsConsent, ReasonCapsEscalated},
		{"surface changed", nil, false, func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.SurfaceSHA256 = sum256("other") }))
		}, nil, StateNeedsConsent, ReasonEscalated},
		{"more consent than needed", nil, false, func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) {
				r.Caps = append(r.Caps, "net:example.com")
			}))
		}, nil, StateEnabled, ""},
		{"quarantined", nil, false, func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) {
				r.Quarantined, r.Failures, r.LastReason = true, 3, FailCrash
				r.LastFailureAt = "2026-11-03T11:00:00Z"
			}))
		}, nil, StateQuarantined, ReasonQuarantined},
		{
			"required program missing", demoFiles("demo", "\n[[requires]]\nbin = \"absent\"\n"),
			false, nil, nil, StateMissingDependency, ReasonMissingDep,
		},
		{
			"required program found", demoFiles("demo", "\n[[requires]]\nbin = \"present\"\n"),
			false, func(l *lab, _ LockRecord) { l.fakeBin("present") }, nil, StateEnabled, "",
		},
		{
			"optional program missing", demoFiles("demo",
				"\n[[requires]]\nbin = \"absent\"\noptional = true\n"), false, nil, nil,
			StateEnabled,
			"",
		},
		{
			"tote range excludes the host",
			map[string]string{"plugin.toml": strings.Replace(
				demoFiles("demo", "")["plugin.toml"], `tote = ">=1.0.0, <2"`, `tote = ">=9.0.0"`,
				1)},
			true, nil, nil, StateIncompatible, CodeToteRange,
		},
		{
			"needs a feature tote lacks",
			map[string]string{"plugin.toml": strings.Replace(
				demoFiles("demo", "")["plugin.toml"], "tier =",
				"requires_features = [\"exec.health\"]\ntier =", 1)},
			true, nil, nil, StateIncompatible, CodeFeatureMissing,
		},
		{
			"manifest not valid",
			map[string]string{"plugin.toml": strings.Replace(
				demoFiles("demo", "")["plugin.toml"], `license = "MIT"`, `licence = "MIT"`, 1)},
			true, nil, nil, StateBroken, ReasonManifestInvalid,
		},
		{
			"manifest of another id",
			map[string]string{
				"plugin.toml": demoFiles("other", "")["plugin.toml"],
			},
			true, nil, nil, StateBroken, ReasonManifestInvalid,
		},
		{
			"no manifest",
			map[string]string{"README": "x"},
			true, nil, nil, StateBroken,
			ReasonManifestInvalid,
		},
		{"extra file in a declarative package", func() map[string]string {
			f := demoFiles("demo", "")
			f["run.sh*"] = "#!/bin/sh\n"

			return f
		}(), false, nil, nil, StateBroken, ReasonPackageInvalid},
		{"lock says another version", nil, false, func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.Version = "2.0.0" }))
		}, nil, StateBroken, ReasonLockVersion},
		{"lock says another tier", nil, false, func(l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.Tier = TierExec }))
		}, nil, StateBroken, ReasonLockTier},
		{"other user owns the tree", nil, false, nil, func(o *Options) {
			o.Getuid = func() int { return os.Getuid() + 1 }
		}, StateBroken, CodeTreeOwner},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)

			files := tt.files
			if files == nil {
				files = demoFiles("demo", "")
			}

			var rec LockRecord

			if tt.raw {
				rec = installRaw(t, l, "demo", files)
			} else {
				rec = l.install("demo", files)
			}

			if tt.setup != nil {
				tt.setup(l, rec)
			}

			p, ok := l.registry(tt.mutate).Load().Get("demo")
			if !ok {
				t.Fatal("not in the snapshot")
			}

			if p.State != tt.state {
				t.Fatalf("state = %s (%v), want %s", p.State, p.Reasons, tt.state)
			}

			if tt.code != "" && !hasReason(p.Reasons, tt.code) {
				t.Errorf("reasons = %v, want %s", reasonCodes(p.Reasons), tt.code)
			}

			if tt.state == StateEnabled && len(p.Reasons) != 0 {
				t.Errorf("an enabled plugin has reasons %v", p.Reasons)
			}

			if p.State.Runnable() != (tt.state == StateEnabled) {
				t.Error("Runnable disagrees with the state")
			}
		})
	}
}

// installRaw installs a tree without validating its manifest, for the fixtures that are meant to
// be refused, and records it as consented.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
//   - id: plugin id
//   - files: files of the package
func installRaw(t *testing.T, l *lab, id string, files map[string]string) LockRecord {
	t.Helper()

	idDir, _ := l.paths.PluginDir(id)
	build := filepath.Join(idDir, ".build")
	l.writeTree(build, files)

	tree, err := HashTree(build)
	must(t, err)

	dest, _ := l.paths.PluginTree(id, tree.ID())
	must(t, os.Rename(build, dest))

	now := "2026-11-03T10:15:00Z"
	rec := LockRecord{
		ID: id, Version: "1.0.0", Tier: TierDeclarative, Kind: KindBundled,
		TreeSHA256: tree.SHA256, SurfaceSHA256: sum256("surface"), InstalledAt: now,
		UpdatedAt: now, Caps: []string{"exec:tool"}, ConsentedAt: now, ConsentedBy: ConsentPrompt,
	}

	must(t, l.lock().Put(rec))

	return rec
}

// TestDirectoryWithoutRecord checks that a plugin directory the lock does not know is broken, and
// that a record without a directory is broken too.
//
// Parameters:
//   - t: test to report to
func TestDirectoryWithoutRecord(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", ""))
	l.install("stray", demoFiles("stray", ""))
	must(t, l.lock().Delete("stray"))

	other := l.install("lost", demoFiles("lost", ""))
	l.open(filepath.Dir(l.treeDir(other)))
	l.open(l.treeDir(other))
	must(t, os.Remove(filepath.Join(l.treeDir(other), "plugin.toml")))
	must(t, os.Remove(l.treeDir(other)))

	snap := l.registry(nil).Load()

	stray, _ := snap.Get("stray")
	if stray.State != StateBroken || !hasReason(stray.Reasons, ReasonNoLockRecord) ||
		stray.Manifest != nil {
		t.Errorf("stray: %s %v", stray.State, stray.Reasons)
	}

	lost, _ := snap.Get("lost")
	if lost.State != StateBroken || !hasReason(lost.Reasons, CodeTreeMissing) {
		t.Errorf("lost: %s %v", lost.State, lost.Reasons)
	}

	if demo, _ := snap.Get("demo"); demo.State != StateEnabled {
		t.Errorf("demo: %s", demo.State)
	}

	if got := snap.Count(); got.Total != 3 || got.Enabled != 1 || got.Attention != 2 {
		t.Errorf("Count = %+v", got)
	}
}

// TestOrphansAndStaleTrees checks the entries of the plugins directory that are not plugins and
// the trees of a plugin that are not its current one.
//
// Parameters:
//   - t: test to report to
func TestOrphansAndStaleTrees(t *testing.T) {
	l := newLab(t)
	rec := l.install("demo", demoFiles("demo", ""))

	base := l.paths.PluginsDir()
	must(t, os.Mkdir(filepath.Join(base, "Not A Plugin"), 0o700))
	must(t, os.WriteFile(filepath.Join(base, "notes"), nil, 0o600))
	must(t, os.Mkdir(filepath.Join(base, ".staging"), 0o700))
	must(t, os.WriteFile(filepath.Join(base, ".mutex"), nil, 0o600))
	must(t, os.Mkdir(filepath.Join(filepath.Dir(l.treeDir(rec)), sum256("old")[:32]), 0o500))

	snap := l.registry(nil).Load()

	kinds := map[string]string{}
	for _, o := range snap.Orphans {
		kinds[o.Name] = o.Kind
	}

	if len(kinds) != 2 || kinds["Not A Plugin"] != OrphanName || kinds["notes"] != OrphanFile {
		t.Errorf("orphans = %v", snap.Orphans)
	}

	if _, ok := snap.Get("notes"); ok || len(snap.Plugins()) != 1 {
		t.Errorf("plugins = %v", snap.Plugins())
	}

	p, _ := snap.Get("demo")
	if p.State != StateEnabled || len(p.Stale) != 1 || p.Stale[0] != sum256("old")[:32] {
		t.Errorf("state %s stale %v", p.State, p.Stale)
	}
}

// TestRequiredProgramLookup checks the lookup of a required program: a relative PATH entry and
// the working directory are never searched, a program inside the plugin tree or the plugins
// directory is not an answer, a file without an exec bit is not a program, and the lookup runs
// nothing.
//
// Parameters:
//   - t: test to report to
func TestRequiredProgramLookup(t *testing.T) {
	canary := ""

	run := func(t *testing.T, setup func(l *lab, rec LockRecord) string) State {
		l := newLab(t)
		files := demoFiles("demo", "\n[[requires]]\nbin = \"tool2\"\n")
		rec := l.install("demo", files)
		canary = filepath.Join(l.root, "canary")

		l.env["PATH"] = setup(l, rec)

		return load(t, l, "demo").State
	}

	script := "#!/bin/sh\ntouch " + "%s" + "\n"

	t.Run("found", func(t *testing.T) {
		got := run(t, func(l *lab, _ LockRecord) string {
			l.writeFile(filepath.Join(l.bin, "tool2"), strings.Replace(script, "%s", canary, 1))
			must(t, os.Chmod(filepath.Join(l.bin, "tool2"), 0o700))

			return l.bin
		})

		if got != StateEnabled {
			t.Errorf("state %s", got)
		}

		if _, err := os.Stat(canary); err == nil {
			t.Error("the lookup ran the program")
		}
	})

	t.Run("no exec bit", func(t *testing.T) {
		got := run(t, func(l *lab, _ LockRecord) string {
			l.writeFile(filepath.Join(l.bin, "tool2"), "x")

			return l.bin
		})

		if got != StateMissingDependency {
			t.Errorf("state %s", got)
		}
	})

	t.Run("relative entry", func(t *testing.T) {
		got := run(t, func(l *lab, _ LockRecord) string {
			l.writeFile(filepath.Join(l.bin, "tool2"), "x")
			must(t, os.Chmod(filepath.Join(l.bin, "tool2"), 0o700))

			// The entries resolve to the program if they are followed.
			t.Chdir(l.root)

			return "bin:./bin:."
		})

		if got != StateMissingDependency {
			t.Errorf("state %s", got)
		}
	})

	t.Run("working directory", func(t *testing.T) {
		got := run(t, func(l *lab, _ LockRecord) string {
			cwd := filepath.Join(l.root, "cwd")
			l.writeFile(filepath.Join(cwd, "tool2"), "x")
			must(t, os.Chmod(filepath.Join(cwd, "tool2"), 0o700))

			return cwd
		})

		if got != StateMissingDependency {
			t.Errorf("state %s", got)
		}
	})

	t.Run("working directory above the entry", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", "\n[[requires]]\nbin = \"tool2\"\n"))
		l.writeFile(filepath.Join(l.bin, "tool2"), "x")
		must(t, os.Chmod(filepath.Join(l.bin, "tool2"), 0o700))

		// A user working in their home still finds the programs below it.
		reg := l.registry(func(o *Options) { o.Cwd = l.root })
		if p, _ := reg.Load().Get("demo"); p.State != StateEnabled {
			t.Errorf("state %s", p.State)
		}
	})

	t.Run("inside the plugins directory", func(t *testing.T) {
		got := run(t, func(l *lab, rec LockRecord) string {
			dir := filepath.Join(l.paths.PluginsDir(), "demo")
			l.open(dir)
			l.open(l.treeDir(rec))
			l.writeFile(filepath.Join(dir, "tool2"), "x")
			must(t, os.Chmod(filepath.Join(dir, "tool2"), 0o700))
			must(t, os.Chmod(l.treeDir(rec), 0o500))

			return dir
		})

		if got != StateMissingDependency {
			t.Errorf("state %s", got)
		}
	})

	t.Run("symlink into the plugins directory", func(t *testing.T) {
		got := run(t, func(l *lab, rec LockRecord) string {
			target := filepath.Join(l.paths.PluginsDir(), "hidden")
			l.writeFile(target, "x")
			must(t, os.Chmod(target, 0o700))
			must(t, os.Symlink(target, filepath.Join(l.bin, "tool2")))

			return l.bin
		})

		if got != StateMissingDependency {
			t.Errorf("state %s", got)
		}
	})

	t.Run("empty PATH", func(t *testing.T) {
		got := run(t, func(*lab, LockRecord) string { return "" })

		if got != StateMissingDependency {
			t.Errorf("state %s", got)
		}
	})
}

// TestLookBinName checks that only a name is looked up: a path, an option, an empty name and a
// name with a space are never found, even if such a file exists.
//
// Parameters:
//   - t: test to report to
func TestLookBinName(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "ok"), nil, 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "-x"), nil, 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "a b"), nil, 0o700))

	for _, name := range []string{
		"", "-x", "a b", "../ok", "sub/ok", "/bin/sh", ".", "..",
		strings.Repeat("a", 65),
	} {
		if p, found := LookBin(dir, name, Avoid{}); found {
			t.Errorf("LookBin(%q) = %q", name, p)
		}
	}

	if p, found := LookBin(dir, "ok", Avoid{}); !found || p != filepath.Join(dir, "ok") {
		t.Errorf("LookBin(ok) = %q %v", p, found)
	}

	if _, found := LookBin(dir, "ok", Avoid{Under: []string{dir}}); found {
		t.Error("a denied directory was searched")
	}

	avoid := Avoid{Under: []string{"", "relative"}, Exactly: []string{"rel"}}

	if _, found := LookBin(dir, "ok", avoid); !found {
		t.Error("empty and relative deny entries hid the program")
	}
}

// TestLoadIsReadOnly checks that Load, Recheck and Settings write nothing: the tree, the state
// and cache directories and the host directory are byte for byte what they were, the lock and
// config.toml too.
//
// Parameters:
//   - t: test to report to
func TestLoadIsReadOnly(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", ""))

	for _, d := range []string{"state/plugins/demo", "cache/plugins/demo", "state/plugins-host"} {
		must(t, os.MkdirAll(filepath.Join(l.root, d), 0o700))
	}

	l.writeConfig("[ui]\ntheme = \"auto\"\n")

	before := snapshotFiles(t, l.root)

	reg := l.registry(nil)
	snap := reg.Load()
	reg.Recheck("demo")

	p, _ := snap.Get("demo")
	reg.Settings(p)

	after := snapshotFiles(t, l.root)
	if !slices.Equal(before, after) {
		t.Errorf("files changed:\nbefore %v\nafter  %v", before, after)
	}
}

// snapshotFiles lists every file and directory below a root with its mode, size and modification
// time, sorted, so that two listings can be compared.
//
// Parameters:
//   - t: test to report to
//   - root: directory to list
func snapshotFiles(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		out = append(out, path+" "+info.Mode().String()+" "+itoa64(info.Size())+" "+
			info.ModTime().Format(time.RFC3339Nano))

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// TestHostWritesNothingInPluginDirectories is the rule of the plugins-host files: the host writes
// its files only below <State>/plugins-host/<id>. A plugin state directory that holds a symlink
// named like a host file, and a cache directory with another, must not redirect any write of the
// host, and none of the registry operations may touch the target.
//
// Parameters:
//   - t: test to report to
func TestHostWritesNothingInPluginDirectories(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", ""))

	victim := filepath.Join(l.root, "victim")
	must(t, os.WriteFile(victim, []byte("precious"), 0o600))

	stateDir, _ := l.paths.PluginStateDir("demo")
	cacheDir, _ := l.paths.PluginCacheDir("demo")

	for _, dir := range []string{stateDir, cacheDir} {
		must(t, os.MkdirAll(dir, 0o700))

		for _, name := range []string{"last.log", "demo.lock", "plugins.lock", "plugin-procs"} {
			must(t, os.Symlink(victim, filepath.Join(dir, name)))
		}
	}

	reg := l.registry(nil)
	p, _ := reg.Load().Get("demo")

	reg.Settings(p)

	must(t, l.lock().SetDisabled("demo", true))
	must(t, WriteHostFile(l.paths, "demo", "last.log", []byte("stderr tail"), os.Getuid()))

	if data, _ := os.ReadFile(victim); string(data) != "precious" {
		t.Errorf("the target of the symlinks was written: %q", data)
	}

	hostDir, _ := l.paths.PluginHostDir("demo")

	data, err := os.ReadFile(filepath.Join(hostDir, "last.log"))
	if err != nil || string(data) != "stderr tail" {
		t.Errorf("host file = %q, %v", data, err)
	}

	for _, dir := range []string{stateDir, cacheDir} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.Type()&os.ModeSymlink == 0 {
				t.Errorf("the host created %s in a plugin directory", e.Name())
			}
		}
	}

	if filepath.Dir(hostDir) == filepath.Dir(stateDir) {
		t.Errorf("host directory %s is a sibling of the plugin state, not of plugins-host", hostDir)
	}
}

// TestConcurrentLoad runs several Loads and Rechecks on one registry at the same time, with a
// writer switching the kill flag, to give the race detector something to see.
//
// Parameters:
//   - t: test to report to
func TestConcurrentLoad(t *testing.T) {
	l := newLab(t)

	for _, id := range []string{"aaa", "bbb", "ccc"} {
		l.install(id, demoFiles(id, ""))
	}

	reg := l.registry(nil)

	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 5 {
				snap := reg.Load()
				if len(snap.Plugins()) != 3 {
					t.Errorf("snapshot has %d plugins", len(snap.Plugins()))
				}

				reg.Recheck("bbb")
			}
		}()
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		lk, _ := OpenLock(l.paths.LockFile())
		for range 5 {
			_ = lk.SetDisabled("ccc", true)
			_ = lk.SetDisabled("ccc", false)
		}
	}()

	wg.Wait()
}

// TestExecPackage checks that an exec plugin is loaded from its manifest and its program, that the
// program is hashed with its exec bit, and that a tree without the program is broken.
//
// Parameters:
//   - t: test to report to
func TestExecPackage(t *testing.T) {
	manifest := string(readFixture(t, "ghrel/plugin.toml"))

	t.Run("with the program", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("gh")
		l.fakeBin("jq")
		installWith(t, l, "ghrel", map[string]string{
			"plugin.toml": manifest,
			"bin/ghrel*":  "#!/bin/sh\n",
		})

		p := load(t, l, "ghrel")
		if p.State != StateEnabled || p.Manifest.Tier != TierExec ||
			!slices.Contains(p.Caps, CapDerivedCode) {
			t.Errorf("state %s %v caps %v", p.State, p.Reasons, p.Caps)
		}
	})

	t.Run("without the program", func(t *testing.T) {
		l := newLab(t)
		installRaw(t, l, "ghrel", map[string]string{"plugin.toml": manifest})
		must(t, l.lock().Update("ghrel", func(r *LockRecord) { r.Tier = TierExec }))

		p := load(t, l, "ghrel")
		if p.State != StateBroken || !hasReason(p.Reasons, ReasonPackageInvalid) {
			t.Errorf("state %s %v", p.State, reasonCodes(p.Reasons))
		}
	})

	t.Run("the program lost its exec bit", func(t *testing.T) {
		l := newLab(t)
		installRaw(t, l, "ghrel", map[string]string{"plugin.toml": manifest, "bin/ghrel": "x"})
		must(t, l.lock().Update("ghrel", func(r *LockRecord) { r.Tier = TierExec }))

		p := load(t, l, "ghrel")
		if p.State != StateBroken {
			t.Errorf("state %s %v", p.State, reasonCodes(p.Reasons))
		}
	})
}

// TestOrphanRecord checks that a lock record whose id is not an id is reported and is not taken
// for a plugin.
//
// Parameters:
//   - t: test to report to
func TestOrphanRecord(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", ""))

	data, _ := os.ReadFile(l.paths.LockFile())
	l.writeFile(l.paths.LockFile(), string(data)+"\n[[plugin]]\nid = \"../data\"\n")

	snap := l.registry(nil).Load()

	if len(snap.Orphans) != 1 || snap.Orphans[0].Kind != OrphanRecord {
		t.Errorf("orphans = %v", snap.Orphans)
	}

	if _, ok := snap.Get("../data"); ok || len(snap.Plugins()) != 1 {
		t.Errorf("plugins = %v", snap.Plugins())
	}
}
