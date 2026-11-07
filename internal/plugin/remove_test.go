package plugin

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// snapshot lists everything below a directory with its type, mode and size, links unfollowed, so
// that two snapshots can be compared to prove that nothing changed.
//
// Parameters:
//   - t: test to report to
//   - root: directory to list
func snapshot(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}

		line := fmt.Sprintf("%s %s %d", path, info.Mode(), info.Size())
		if info.Mode()&fs.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			line += " -> " + target
		}

		out = append(out, line)

		return nil
	})
	must(t, err)

	sort.Strings(out)

	return out
}

// sameSnapshot fails the test when two snapshots differ.
//
// Parameters:
//   - t: test to report to
//   - before: first snapshot
//   - after: second snapshot
func sameSnapshot(t *testing.T, before, after []string) {
	t.Helper()

	if strings.Join(before, "\n") == strings.Join(after, "\n") {
		return
	}

	in := map[string]bool{}
	for _, s := range before {
		in[s] = true
	}

	for _, s := range after {
		if !in[s] {
			t.Errorf("appeared: %s", s)
		}

		delete(in, s)
	}

	for s := range in {
		t.Errorf("gone: %s", s)
	}
}

// removeLab installs "demo" with a settings file and state, cache and host directories, and a
// second plugin "demo-x" with the same, so that a removal can be checked against its neighbour.
//
// Parameters:
//   - t: test to report to
func removeLab(t *testing.T) (*lab, *Registry) {
	t.Helper()

	l := newLab(t)
	l.fakeBin("tool")

	for _, id := range []string{"demo", "demo-x"} {
		l.plug(id, []string{"exec:tool"}, cmdWith("go", ""))

		settings, _ := l.paths.PluginSettingsFile(id)
		l.writeFile(settings, "k = 1\n")

		for _, get := range []func(string) (string, error){
			l.paths.PluginStateDir,
			l.paths.PluginCacheDir, l.paths.PluginHostDir,
		} {
			dir, _ := get(id)
			l.writeFile(filepath.Join(dir, "f"), "data")
		}
	}

	return l, l.reg(nil)
}

// TestRemoveBadIDs is the regression of the shell version: an argument that is not a plugin id
// fails with bad_id before any file system call, and nothing is touched, whatever it points to.
//
// Parameters:
//   - t: test to report to
func TestRemoveBadIDs(t *testing.T) {
	l, r := removeLab(t)

	// What the old verb would have deleted for each of these.
	l.writeFile(filepath.Join(l.paths.Data, "data", "keep"), "x")
	l.writeFile(filepath.Join(l.paths.PluginsDir(), "sub", "keep"), "x")

	ids := []string{
		"../data", "a/b", ".", "..", "", "Demo", "DEMO", ".staging", ".mutex", "-x",
		"a b", "a\x00b", "demo/", "/demo", "demo/../demo", "../../etc", "demo\n", "\u00e9",
		strings.Repeat("a", 200), "sub/keep", "./demo", "~", "$HOME", "demo*",
	}

	before := snapshot(t, l.root)

	for _, id := range ids {
		if _, err := r.Remove(id, RemoveOptions{Purge: true}); asError(t, err).Code != CodeBadID ||
			ExitOf(err) != ExitUsage {
			t.Errorf("Remove(%q) = %v", id, err)
		}

		if _, err := r.Disable(id); asError(t, err).Code != CodeBadID {
			t.Errorf("Disable(%q) = %v", id, err)
		}

		if _, err := r.Enable(id, Grant{Prompt: true}); asError(t, err).Code != CodeBadID {
			t.Errorf("Enable(%q) = %v", id, err)
		}

		if _, err := r.Grant(id, Grant{Prompt: true}); asError(t, err).Code != CodeBadID {
			t.Errorf("Grant(%q) = %v", id, err)
		}

		if _, err := r.Allow(id); asError(t, err).Code != CodeBadID {
			t.Errorf("Allow(%q) = %v", id, err)
		}
	}

	sameSnapshot(t, before, snapshot(t, l.root))
}

// TestRemove checks a removal without purge: the plugin is unrunnable, its tree and record are
// gone, the neighbour is untouched, and what the user owns (settings, state, cache) is kept and
// reported.
//
// Parameters:
//   - t: test to report to
func TestRemove(t *testing.T) {
	l, r := removeLab(t)

	rec, _ := l.lock().Get("demo")
	neighbour, _ := l.lock().Get("demo-x")
	before := snapshot(t, filepath.Join(l.paths.PluginsDir(), "demo-x"))

	res, err := r.Remove("demo", RemoveOptions{})
	if err != nil || res.Link || len(res.Leftovers) != 0 || res.ConfigFile != "" {
		t.Fatalf("Remove = %+v %v", res, err)
	}

	want := []string{LeftSettings, LeftState, LeftCache, LeftHost}
	if strings.Join(res.Kept, ",") != strings.Join(want, ",") {
		t.Errorf("Kept = %v", res.Kept)
	}

	if _, ok := l.lock().Get("demo"); ok {
		t.Error("the record is still there")
	}

	if got, _ := l.lock().Get("demo-x"); got.TreeSHA256 != neighbour.TreeSHA256 {
		t.Errorf("the neighbour changed: %+v", got)
	}

	if !gone(filepath.Join(l.paths.PluginsDir(), "demo")) || !gone(l.treeDir(rec)) {
		t.Error("the tree is still there")
	}

	if names := stagingNames(t, l); len(names) != 0 {
		t.Errorf("staging = %v", names)
	}

	sameSnapshot(t, before, snapshot(t, filepath.Join(l.paths.PluginsDir(), "demo-x")))

	for _, get := range []func(string) (string, error){
		l.paths.PluginSettingsFile,
		l.paths.PluginStateDir, l.paths.PluginCacheDir, l.paths.PluginHostDir,
	} {
		if path, _ := get("demo"); gone(path) {
			t.Errorf("%s went without purge", path)
		}
	}

	if _, ok := r.Recheck("demo"); ok {
		t.Error("the registry still knows the plugin")
	}

	if _, err := r.Remove("demo", RemoveOptions{}); asError(t, err).Code != CodeNotInstalled {
		t.Errorf("second Remove = %v", err)
	}

	// An install of the same id starts from nothing and finds its old settings.
	installDir(t, r, demoSource(l, "demo", "1.0.0"))

	if p, ok := r.Recheck("demo"); !ok || p.State != StateEnabled {
		t.Errorf("reinstall = %v %+v", ok, p)
	}
}

// TestRemovePurge checks the purge: everything of the plugin goes, the keys of its table in
// config.toml included, and nothing of the neighbour does.
//
// Parameters:
//   - t: test to report to
func TestRemovePurge(t *testing.T) {
	l, r := removeLab(t)

	l.writeConfig("lang = \"fr\"\n\n[plugins.demo]\nenabled = false\n\n" +
		"[plugins.demo-x]\nenabled = false\n")

	before := snapshot(t, filepath.Join(l.paths.PluginsDir(), "demo-x"))

	res, err := r.Remove("demo", RemoveOptions{Purge: true})
	if err != nil || len(res.Leftovers) != 0 || len(res.Kept) != 0 || res.ConfigFile != "" {
		t.Fatalf("Remove = %+v %v", res, err)
	}

	for _, get := range []func(string) (string, error){
		l.paths.PluginSettingsFile,
		l.paths.PluginStateDir, l.paths.PluginCacheDir, l.paths.PluginHostDir,
	} {
		if path, _ := get("demo"); !gone(path) {
			t.Errorf("%s is still there", path)
		}

		if path, _ := get("demo-x"); gone(path) {
			t.Errorf("%s of the neighbour went", path)
		}
	}

	sameSnapshot(t, before, snapshot(t, filepath.Join(l.paths.PluginsDir(), "demo-x")))

	data, _ := os.ReadFile(l.paths.ConfigFile())
	text := string(data)

	if !r.configEnabled("demo") || !strings.Contains(text, "lang") ||
		!strings.Contains(text, "[plugins.demo-x]") {
		t.Errorf("config.toml = %q", text)
	}

	if strings.Count(text, "enabled = false") != 1 {
		t.Errorf("config.toml = %q", text)
	}

	if r.configEnabled("demo-x") {
		t.Errorf("the neighbour was enabled: %q", text)
	}
}

// TestRemovePurgeConfigLayout checks that a purge succeeds when config.toml says enabled = false in
// a layout the editor will not touch, and then names the file.
//
// Parameters:
//   - t: test to report to
func TestRemovePurgeConfigLayout(t *testing.T) {
	for name, content := range map[string]string{
		"dotted":       "plugins.demo.enabled = false\n",
		"inline table": "[plugins]\ndemo = { enabled = false }\n",
	} {
		t.Run(name, func(t *testing.T) {
			l, r := removeLab(t)
			l.writeConfig(content)

			res, err := r.Remove("demo", RemoveOptions{Purge: true})
			if err != nil {
				t.Fatal(err)
			}

			if (res.ConfigFile != "") == r.configEnabled("demo") {
				t.Errorf("ConfigFile %q with enabled = %v", res.ConfigFile, r.configEnabled("demo"))
			}

			if _, ok := l.lock().Get("demo"); ok {
				t.Error("the record is still there")
			}
		})
	}
}

// TestRemoveSymlinkedID checks a plugin entry that is a link to a directory somewhere else, as a
// user may have made with a dotfiles repository: the link is unlinked, the directory it pointed
// to is not touched.
//
// Parameters:
//   - t: test to report to
func TestRemoveSymlinkedID(t *testing.T) {
	l, r := removeLab(t)

	real := filepath.Join(l.root, "elsewhere", "demo-real")
	must(t, os.MkdirAll(filepath.Dir(real), 0o700))
	must(t, os.Rename(filepath.Join(l.paths.PluginsDir(), "demo"), real))
	must(t, os.Symlink(real, filepath.Join(l.paths.PluginsDir(), "demo")))
	l.writeFile(filepath.Join(l.root, "elsewhere", "keep"), "x")

	before := snapshot(t, filepath.Join(l.root, "elsewhere"))

	res, err := r.Remove("demo", RemoveOptions{Purge: true})
	if err != nil || !res.Link || len(res.Leftovers) != 0 {
		t.Fatalf("Remove = %+v %v", res, err)
	}

	if !gone(filepath.Join(l.paths.PluginsDir(), "demo")) {
		t.Error("the link is still there")
	}

	sameSnapshot(t, before, snapshot(t, filepath.Join(l.root, "elsewhere")))

	if _, ok := l.lock().Get("demo"); ok {
		t.Error("the record is still there")
	}
}

// TestRemoveSymlinkedIDWithoutRecord checks the same for a link that has no record, which is
// reached through the existence of the entry alone.
//
// Parameters:
//   - t: test to report to
func TestRemoveSymlinkedIDWithoutRecord(t *testing.T) {
	l := newLab(t)
	r := l.reg(nil)

	victim := filepath.Join(l.root, "victim")
	l.writeFile(filepath.Join(victim, "f"), "x")
	must(t, os.MkdirAll(l.paths.PluginsDir(), 0o700))
	must(t, os.Symlink(victim, filepath.Join(l.paths.PluginsDir(), "ghost")))

	before := snapshot(t, victim)

	res, err := r.Remove("ghost", RemoveOptions{Purge: true})
	if err != nil || !res.Link {
		t.Fatalf("Remove = %+v %v", res, err)
	}

	sameSnapshot(t, before, snapshot(t, victim))

	if !gone(filepath.Join(l.paths.PluginsDir(), "ghost")) {
		t.Error("the link is still there")
	}
}

// TestRemoveSymlinkedPluginsDir checks a plugins directory that is itself a link: the removal
// happens inside the directory it resolves to and nowhere else, and the link and the neighbours of
// the target stay.
//
// Parameters:
//   - t: test to report to
func TestRemoveSymlinkedPluginsDir(t *testing.T) {
	l, r := removeLab(t)

	real := filepath.Join(l.root, "elsewhere", "plugins-real")
	must(t, os.MkdirAll(filepath.Dir(real), 0o700))
	must(t, os.Rename(l.paths.PluginsDir(), real))
	must(t, os.Symlink(real, l.paths.PluginsDir()))
	l.writeFile(filepath.Join(l.root, "elsewhere", "keep"), "x")
	l.writeFile(filepath.Join(real, "not-a-plugin", "keep"), "x")

	neighbour := snapshot(t, filepath.Join(real, "demo-x"))

	res, err := r.Remove("demo", RemoveOptions{})
	if err != nil {
		t.Fatalf("Remove = %+v %v", res, err)
	}

	if !gone(filepath.Join(real, "demo")) {
		t.Error("the plugin is still in the real directory")
	}

	if gone(filepath.Join(real, "not-a-plugin", "keep")) || gone(filepath.Join(l.root, "elsewhere",
		"keep")) {
		t.Error("something else went")
	}

	if info, err := os.Lstat(l.paths.PluginsDir()); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the link to the plugins directory changed: %v", err)
	}

	sameSnapshot(t, neighbour, snapshot(t, filepath.Join(real, "demo-x")))
}

// TestRemovePurgeHostileDirectories checks a purge over a settings file that is a link to a
// dotfiles repository, a state directory that is a link to a directory of the user, and a cache
// directory that has links inside to the outside: links are unlinked, targets stay.
//
// Parameters:
//   - t: test to report to
func TestRemovePurgeHostileDirectories(t *testing.T) {
	l, r := removeLab(t)

	dots := filepath.Join(l.root, "dotfiles")
	l.writeFile(filepath.Join(dots, "demo.toml"), "mine = true\n")
	l.writeFile(filepath.Join(dots, "state-target", "keep"), "x")
	l.writeFile(filepath.Join(dots, "cache-target", "keep"), "x")

	settings, _ := l.paths.PluginSettingsFile("demo")
	state, _ := l.paths.PluginStateDir("demo")
	cache, _ := l.paths.PluginCacheDir("demo")

	must(t, os.Rename(settings, filepath.Join(l.root, "settings-moved")))
	must(t, os.Symlink(filepath.Join(dots, "demo.toml"), settings))

	must(t, os.Rename(state, filepath.Join(l.root, "state-moved")))
	must(t, os.Symlink(filepath.Join(dots, "state-target"), state))

	must(t, os.Symlink(filepath.Join(dots, "cache-target"), filepath.Join(cache, "out")))
	must(t, os.Symlink(dots, filepath.Join(cache, "dots")))
	must(t, os.Symlink("/", filepath.Join(cache, "slash")))

	before := snapshot(t, dots)

	res, err := r.Remove("demo", RemoveOptions{Purge: true})
	if err != nil || len(res.Leftovers) != 0 {
		t.Fatalf("Remove = %+v %v", res, err)
	}

	sameSnapshot(t, before, snapshot(t, dots))

	for _, path := range []string{settings, state, cache} {
		if !gone(path) {
			t.Errorf("%s is still there", path)
		}
	}
}

// TestRemovePurgeParentIsALink checks that the directory of a kind is resolved once: a state
// directory that is a link to a directory elsewhere has the entry of the plugin deleted inside
// the target, and nothing else of it.
//
// Parameters:
//   - t: test to report to
func TestRemovePurgeParentIsALink(t *testing.T) {
	l, r := removeLab(t)

	parent := filepath.Join(l.paths.State, "plugins")
	real := filepath.Join(l.root, "elsewhere", "state-plugins")

	must(t, os.MkdirAll(filepath.Dir(real), 0o700))
	must(t, os.Rename(parent, real))
	must(t, os.Symlink(real, parent))

	neighbour := snapshot(t, filepath.Join(real, "demo-x"))

	if _, err := r.Remove("demo", RemoveOptions{Purge: true}); err != nil {
		t.Fatal(err)
	}

	if !gone(filepath.Join(real, "demo")) {
		t.Error("the state of the plugin is still there")
	}

	sameSnapshot(t, neighbour, snapshot(t, filepath.Join(real, "demo-x")))

	if info, err := os.Lstat(parent); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the link changed: %v", err)
	}
}

// TestRemoveOrphanAndInvalid checks the removals that go by the directory alone: a tree with no
// record, and a record that does not validate.
//
// Parameters:
//   - t: test to report to
func TestRemoveOrphanAndInvalid(t *testing.T) {
	t.Run("orphan tree", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")
		rec := l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
		must(t, l.lock().Delete("demo"))

		r := l.reg(nil)

		if res, err := r.Remove("demo", RemoveOptions{}); err != nil || len(res.Leftovers) != 0 {
			t.Fatalf("Remove = %+v %v", res, err)
		}

		if !gone(l.treeDir(rec)) || !gone(filepath.Dir(l.treeDir(rec))) {
			t.Error("the orphan is still there")
		}
	})

	t.Run("invalid record", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")
		rec := l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))

		data, _ := os.ReadFile(l.paths.LockFile())
		l.writeFile(l.paths.LockFile(), strings.Replace(string(data), rec.TreeSHA256, "xyz", 1))

		r := l.reg(nil)

		if _, err := r.Remove("demo", RemoveOptions{}); err != nil {
			t.Fatal(err)
		}

		if _, ok := l.lock().Get("demo"); ok || l.lock().Invalid()["demo"] != nil {
			t.Error("the record is still there")
		}

		if !gone(l.treeDir(rec)) {
			t.Error("the tree is still there")
		}
	})

	t.Run("not installed", func(t *testing.T) {
		l := newLab(t)
		r := l.reg(nil)

		before := snapshot(t, l.root)

		if _, err := r.Remove("ghost", RemoveOptions{Purge: true}); asError(t, err).Code !=
			CodeNotInstalled {
			t.Errorf("Remove = %v", err)
		}

		sameSnapshot(t, before, snapshot(t, l.root))
	})

	t.Run("lock does not parse", func(t *testing.T) {
		l, r := removeLab(t)
		l.writeFile(l.paths.LockFile(), "[[plugin]\n")

		before := snapshot(t, l.root)

		if _, err := r.Remove("demo", RemoveOptions{Purge: true}); asError(t, err).Code !=
			CodeStoreCorrupt {
			t.Errorf("Remove = %v", err)
		}

		sameSnapshot(t, before, snapshot(t, l.root))
	})
}

// TestRemoveStopsRunningPlugin checks that a removal ends the processes of the plugin before the
// tree goes.
//
// Parameters:
//   - t: test to report to
func TestRemoveStopsRunningPlugin(t *testing.T) {
	l, a := runSetup(t)
	b := l.reg(nil)
	pidFile := l.root + "/long.pid"

	l.script("tool", "sleep 60 &\necho $! > "+pidFile+"\nwait\n")

	done := make(chan Outcome, 1)
	p := prepare(t, a, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

	go func() { done <- a.Run(context.Background(), p, nil) }()

	waitFile(t, pidFile)
	waitProc(t, a, "demo")

	res, err := b.Remove("demo", RemoveOptions{})
	if err != nil || res.Killed != 1 {
		t.Fatalf("Remove = %+v %v", res, err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end")
	}

	waitGone(t, pidFile)

	if _, ok := b.Recheck("demo"); ok {
		t.Error("the plugin is still known")
	}
}

// TestRemoveLeavesNothingUnderRace removes a plugin while another registry installs it again: the
// end state is one of the two, never a record without a tree or a tree that no record owns.
//
// Parameters:
//   - t: test to report to
func TestRemoveLeavesNothingUnderRace(t *testing.T) {
	l, _ := removeLab(t)
	src := demoSource(l, "demo", "1.0.0")

	done := make(chan struct{})

	go func() {
		defer close(done)

		r := l.reg(nil)

		for range 10 {
			st, err := r.Plan(context.Background(), InstallRequest{Source: src, Force: true})
			if err != nil {
				continue
			}

			_, _ = st.Commit(Grant{Prompt: true})
		}
	}()

	r := l.reg(nil)

	for range 10 {
		_, _ = r.Remove("demo", RemoveOptions{})
	}

	<-done

	if _, ok := l.lock().Get("demo"); ok {
		checkInvariants(t, l, "demo", "1.0.0")
	} else {
		checkInvariants(t, l, "demo", "")
	}
}

// TestRemoveSetsTheKillSwitchFirst reads the lock at the point between the kill switch and the
// stop of the processes: the record is there and disabled, so a launch that starts in that window
// fails its verification instead of surviving the removal.
//
// Parameters:
//   - t: test to report to
func TestRemoveSetsTheKillSwitchFirst(t *testing.T) {
	l, r := removeLab(t)

	var seen, disabled bool

	withHook(t, func(point string) error {
		if point == PointRemoveStop {
			rec, ok := l.lock().Get("demo")
			seen, disabled = ok, rec.Disabled
			p, _ := l.reg(nil).Recheck("demo")

			if p.State != StateDisabled {
				t.Errorf("state at the stop = %v, want disabled", p.State)
			}
		}

		return nil
	})

	if _, err := r.Remove("demo", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}

	if !seen || !disabled {
		t.Errorf("at the stop the record was found=%v disabled=%v", seen, disabled)
	}

	if _, ok := l.lock().Get("demo"); ok {
		t.Error("the record is still there")
	}
}

// TestRemoveReadsRecordUnderMutex checks that a removal decides from the record as it is once it
// holds the mutex: an install that commits while the removal waits (the tree is in place and the
// record is written after the removal looked) is removed whole, record included.
//
// Parameters:
//   - t: test to report to
func TestRemoveReadsRecordUnderMutex(t *testing.T) {
	l := newLab(t)
	l.fakeBin("tool")

	rec := l.plug("demo", []string{"exec:tool"}, cmdWith("go", ""))
	must(t, l.lock().Delete("demo"))

	r := l.reg(nil)

	pd, err := r.resolvePlugins(false)
	must(t, err)

	mu, err := r.lockChanges(pd, time.Second)
	must(t, err)

	done := make(chan error, 1)

	go func() {
		_, err := r.Remove("demo", RemoveOptions{})
		done <- err
	}()

	time.Sleep(200 * time.Millisecond)

	must(t, l.lock().Put(rec))
	mu.release()

	select {
	case err := <-done:
		must(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the removal did not end")
	}

	if _, ok := l.lock().Get("demo"); ok {
		t.Error("the record of the plugin outlived its files")
	}

	if !gone(l.treeDir(rec)) {
		t.Error("the tree is still there")
	}
}

// TestRemoveClearsDisabledSwitch checks that disable, remove and install again gives an enabled
// plugin whether or not the removal purged, and that the neighbour keeps its switch.
//
// Parameters:
//   - t: test to report to
func TestRemoveClearsDisabledSwitch(t *testing.T) {
	for _, purge := range []bool{false, true} {
		l, r := removeLab(t)

		l.writeConfig("[plugins.demo]\nenabled = false\n\n[plugins.demo-x]\nenabled = false\n")

		res, err := r.Remove("demo", RemoveOptions{Purge: purge})
		if err != nil || res.ConfigFile != "" {
			t.Fatalf("purge %v: Remove = %+v %v", purge, res, err)
		}

		if !r.configEnabled("demo") || r.configEnabled("demo-x") {
			t.Errorf("purge %v: demo %v demo-x %v", purge, r.configEnabled("demo"),
				r.configEnabled("demo-x"))
		}

		installDir(t, r, demoSource(l, "demo", "1.0.0"))

		if p, ok := r.Recheck("demo"); !ok || p.State != StateEnabled {
			t.Errorf("purge %v: reinstall = %v %+v", purge, ok, p)
		}
	}
}
