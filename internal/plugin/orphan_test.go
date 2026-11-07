package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// otherTree is a tree name that is valid and belongs to nobody.
const otherTree = "0123456789abcdef0123456789abcdef"

// leftoverLab builds the layout Diagnose has to describe: an installed plugin with a current tree
// and an old one, a tree with no record, a stage, a trash, an unknown entry in staging, and the
// settings, state, cache and host directories of an id that is not installed.
//
// Parameters:
//   - t: test to report to
func leftoverLab(t *testing.T) (*lab, *Registry) {
	t.Helper()

	l := newLab(t)
	l.fakeBin("tool")
	l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))

	idDir, _ := l.paths.PluginDir("demo")
	l.writeTree(filepath.Join(idDir, otherTree), map[string]string{"plugin.toml": "x"})

	ghost, _ := l.paths.PluginDir("ghost")
	l.writeTree(filepath.Join(ghost, otherTree), map[string]string{"plugin.toml": "x"})

	staging := filepath.Join(l.paths.PluginsDir(), ".staging")
	l.writeTree(filepath.Join(staging, "stage-0011"), map[string]string{
		"plugin.toml": "x",
		"sub/f":       "x",
	})
	l.writeTree(filepath.Join(staging, "trash-0022"), map[string]string{
		"plugin.toml": "x",
		"sub/f":       "x",
	})
	l.writeFile(filepath.Join(staging, "weird"), "x")

	settings, _ := l.paths.PluginSettingsFile("gone")
	l.writeFile(settings, "k = 1\n")

	for _, get := range []func(string) (string, error){
		l.paths.PluginStateDir,
		l.paths.PluginCacheDir, l.paths.PluginHostDir,
	} {
		dir, _ := get("gone")
		l.writeFile(filepath.Join(dir, "f"), "x")
	}

	return l, l.reg(nil)
}

// kindsOf counts leftovers by kind.
//
// Parameters:
//   - ls: leftovers
func kindsOf(ls []Leftover) map[string]int {
	out := map[string]int{}
	for _, x := range ls {
		out[x.Kind]++
	}

	return out
}

// TestLeftovers checks the classification, the order and that nothing is changed by listing.
//
// Parameters:
//   - t: test to report to
func TestLeftovers(t *testing.T) {
	l, r := leftoverLab(t)
	before := snapshot(t, l.root)

	ls, err := r.Leftovers()
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]int{
		LeftStaging: 1, LeftTrash: 1, LeftUnknown: 1, LeftTreeOld: 1,
		LeftTreeOrphan: 1, LeftSettings: 1, LeftState: 1, LeftCache: 1, LeftHost: 1,
	}

	got := kindsOf(ls)
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s = %d, want %d (%+v)", k, got[k], n, ls)
		}
	}

	if len(ls) != len(want) {
		t.Errorf("leftovers = %+v", ls)
	}

	for _, x := range ls {
		switch x.Kind {
		case LeftTreeOld, LeftTreeOrphan:
			if x.Name != otherTree || x.ID == "" {
				t.Errorf("tree leftover = %+v", x)
			}
		case LeftSettings, LeftState, LeftCache, LeftHost:
			if x.ID != "gone" {
				t.Errorf("leftover = %+v", x)
			}
		}

		if x.Age < 0 || x.Age > time.Hour {
			t.Errorf("age of %+v", x)
		}
	}

	for i := 1; i < len(ls); i++ {
		if ls[i-1].Kind > ls[i].Kind {
			t.Errorf("not sorted: %+v", ls)

			break
		}
	}

	sameSnapshot(t, before, snapshot(t, l.root))
}

// TestLeftoversCleansNames checks that a name with control bytes, as a hostile layout might have,
// never reaches the listing with them.
//
// Parameters:
//   - t: test to report to
func TestLeftoversCleansNames(t *testing.T) {
	l, r := leftoverLab(t)
	staging := filepath.Join(l.paths.PluginsDir(), ".staging")

	l.writeFile(filepath.Join(staging, "trash-\x1b]0;owned\x07\nx"), "x")

	ls, err := r.Leftovers()
	if err != nil {
		t.Fatal(err)
	}

	for _, x := range ls {
		for _, c := range []byte(x.Name) {
			if c < 0x20 || c == 0x7f {
				t.Errorf("name %q holds a control byte", x.Name)
			}
		}
	}
}

// TestLeftoversBrokenLock checks that a lock that does not parse leaves the trees unclassified,
// reports why, and still lists the staging directory.
//
// Parameters:
//   - t: test to report to
func TestLeftoversBrokenLock(t *testing.T) {
	l, r := leftoverLab(t)
	l.writeFile(l.paths.LockFile(), "[[plugin]\n")

	ls, err := r.Leftovers()
	if asError(t, err).Code != CodeStoreCorrupt {
		t.Errorf("error = %v", err)
	}

	got := kindsOf(ls)
	if got[LeftTreeOld]+got[LeftTreeOrphan]+got[LeftSettings] != 0 || got[LeftStaging] != 1 {
		t.Errorf("leftovers = %+v", ls)
	}
}

// TestLeftoversNothing checks a sandbox with no plugins directory at all.
//
// Parameters:
//   - t: test to report to
func TestLeftoversNothing(t *testing.T) {
	l := newLab(t)

	ls, err := l.reg(nil).Leftovers()
	if err != nil || len(ls) != 0 {
		t.Errorf("Leftovers = %+v %v", ls, err)
	}

	if rep, err := l.reg(nil).Cleanup(); err != nil || len(rep.Removed)+len(rep.Failed) != 0 {
		t.Errorf("Cleanup = %+v %v", rep, err)
	}
}

// TestCleanup checks what a cleanup deletes and what it keeps: the trash, the old tree and the
// abandoned stage go; the fresh stage, the unknown entry, the orphan tree and the files of the user
// stay; a second cleanup finds nothing.
//
// Parameters:
//   - t: test to report to
func TestCleanup(t *testing.T) {
	l, r := leftoverLab(t)
	staging := filepath.Join(l.paths.PluginsDir(), ".staging")

	old := time.Now().Add(-2 * staleStage)
	must(t, os.Chtimes(filepath.Join(staging, "stage-0011"), old, old))

	l.writeTree(filepath.Join(staging, "stage-fresh"), map[string]string{"plugin.toml": "x"})

	rep, err := r.Cleanup()
	if err != nil || rep.Busy || len(rep.Failed) != 0 {
		t.Fatalf("Cleanup = %+v %v", rep, err)
	}

	if len(rep.Removed) != 3 {
		t.Errorf("removed = %v", rep.Removed)
	}

	idDir, _ := l.paths.PluginDir("demo")
	ghost, _ := l.paths.PluginDir("ghost")
	settings, _ := l.paths.PluginSettingsFile("gone")

	for path, wantGone := range map[string]bool{
		filepath.Join(staging, "stage-0011"):  true,
		filepath.Join(staging, "trash-0022"):  true,
		filepath.Join(idDir, otherTree):       true,
		filepath.Join(staging, "stage-fresh"): false,
		filepath.Join(staging, "weird"):       false,
		filepath.Join(ghost, otherTree):       false,
		settings:                              false,
	} {
		if gone(path) != wantGone {
			t.Errorf("%s gone = %v, want %v", path, gone(path), wantGone)
		}
	}

	rec, _ := l.lock().Get("demo")
	if gone(l.treeDir(rec)) {
		t.Error("the current tree went")
	}

	if rep, err := r.Cleanup(); err != nil || len(rep.Removed) != 0 {
		t.Errorf("second Cleanup = %+v %v", rep, err)
	}

	if p, _ := r.Recheck("demo"); p.State != StateEnabled {
		t.Errorf("state = %s", p.State)
	}
}

// TestCleanupHostile checks a cleanup over a staging directory that is a link, a trash entry that
// is a link, and a trash tree with links inside: nothing outside is deleted.
//
// Parameters:
//   - t: test to report to
func TestCleanupHostile(t *testing.T) {
	t.Run("staging is a link", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")
		l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))

		victim := filepath.Join(l.root, "victim")
		l.writeFile(filepath.Join(victim, "trash-1", "f"), "x")
		l.writeFile(filepath.Join(victim, "stage-1", "f"), "x")
		must(t, os.Symlink(victim, filepath.Join(l.paths.PluginsDir(), ".staging")))

		before := snapshot(t, victim)

		r := l.reg(nil)
		if _, err := r.Cleanup(); err != nil {
			t.Fatal(err)
		}

		sameSnapshot(t, before, snapshot(t, victim))

		ls, _ := r.Leftovers()
		if got := kindsOf(ls); got[LeftTrash]+got[LeftStaging] != 0 {
			t.Errorf("a linked staging was listed: %+v", ls)
		}
	})

	t.Run("entries are links", func(t *testing.T) {
		l, r := leftoverLab(t)
		staging := filepath.Join(l.paths.PluginsDir(), ".staging")

		victim := filepath.Join(l.root, "victim")
		l.writeFile(filepath.Join(victim, "keep"), "x")
		must(t, os.Symlink(victim, filepath.Join(staging, "trash-link")))
		must(t, os.Symlink(victim, filepath.Join(staging, "stage-link")))
		l.open(filepath.Join(staging, "trash-0022"))
		must(t, os.Symlink(victim, filepath.Join(staging, "trash-0022", "inside")))

		idDir, _ := l.paths.PluginDir("demo")
		must(t, os.Symlink(victim, filepath.Join(idDir, "0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f")))

		before := snapshot(t, victim)

		if _, err := r.Cleanup(); err != nil {
			t.Fatal(err)
		}

		sameSnapshot(t, before, snapshot(t, victim))

		if !gone(filepath.Join(staging, "trash-link")) {
			t.Error("the link in trash is still there")
		}
	})

	t.Run("an id directory that is a link", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")
		l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))

		idDir, _ := l.paths.PluginDir("demo")
		real := filepath.Join(l.root, "elsewhere", "demo")

		must(t, os.MkdirAll(filepath.Dir(real), 0o700))
		must(t, os.Rename(idDir, real))
		must(t, os.Symlink(real, idDir))
		l.writeTree(filepath.Join(real, otherTree), map[string]string{"plugin.toml": "x"})

		before := snapshot(t, filepath.Join(l.root, "elsewhere"))

		if _, err := l.reg(nil).Cleanup(); err != nil {
			t.Fatal(err)
		}

		sameSnapshot(t, before, snapshot(t, filepath.Join(l.root, "elsewhere")))
	})
}

// TestCleanupBusy checks that a cleanup does not wait for the mutex and does not delete anything
// while another change holds it.
//
// Parameters:
//   - t: test to report to
func TestCleanupBusy(t *testing.T) {
	l, r := leftoverLab(t)

	pd, err := r.resolvePlugins(true)
	must(t, err)

	held, err := r.lockChanges(pd, time.Second)
	must(t, err)

	before := snapshot(t, l.root)
	start := time.Now()

	rep, err := r.Cleanup()
	if err != nil || !rep.Busy || len(rep.Removed) != 0 {
		t.Errorf("Cleanup = %+v %v", rep, err)
	}

	if time.Since(start) > time.Second {
		t.Error("Cleanup waited")
	}

	held.release()

	sameSnapshot(t, before, snapshot(t, l.root))
}

// TestCleanupBrokenLock checks that a lock that does not parse limits a cleanup to the staging
// directory: no tree is deleted when which one is current cannot be told.
//
// Parameters:
//   - t: test to report to
func TestCleanupBrokenLock(t *testing.T) {
	l, r := leftoverLab(t)
	l.writeFile(l.paths.LockFile(), "[[plugin]\n")

	idDir, _ := l.paths.PluginDir("demo")

	if _, err := r.Cleanup(); err != nil {
		t.Fatal(err)
	}

	if gone(filepath.Join(idDir, otherTree)) {
		t.Error("an old tree was deleted without a lock")
	}

	entries, _ := os.ReadDir(idDir)
	if len(entries) != 2 {
		t.Errorf("trees = %v", entries)
	}

	if !gone(filepath.Join(l.paths.PluginsDir(), ".staging", "trash-0022")) {
		t.Error("the trash was kept")
	}
}

// TestCleanupNamesOnly checks that a cleanup deletes a tree only when its name is a tree name: a
// file or a directory with another name in the directory of a plugin is somebody's and stays.
//
// Parameters:
//   - t: test to report to
func TestCleanupNamesOnly(t *testing.T) {
	l, r := leftoverLab(t)
	idDir, _ := l.paths.PluginDir("demo")

	l.writeFile(filepath.Join(idDir, "notes.txt"), "mine")
	l.writeFile(filepath.Join(idDir, "UPPER", "f"), "mine")

	if _, err := r.Cleanup(); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"notes.txt", "UPPER"} {
		if gone(filepath.Join(idDir, name)) {
			t.Errorf("%s went", name)
		}
	}
}
