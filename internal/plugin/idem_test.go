package plugin

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// treeSnapshot reads every file below a directory, by relative path, with its mode, so that a
// test can tell that nothing was written, replaced or created.
//
// Parameters:
//   - t: test to report to
//   - root: directory to read
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()

	out := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		info, ierr := d.Info()

		if ierr != nil {
			return ierr
		}

		entry := info.Mode().String()
		if !d.IsDir() {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}

			entry += " " + string(data) + info.ModTime().String()
		}

		out[rel] = entry

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// equalTrees reports whether two snapshots are equal.
//
// Parameters:
//   - a: first snapshot
//   - b: second snapshot
func equalTrees(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if b[k] != v {
			return false
		}
	}

	return true
}

// assertUnchanged plans the install again, checks that the package is the installed one, that it
// asks nothing, that Commit answers without a grant and that neither the data, nor the state, nor
// the lock changed by a byte.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
//   - r: registry
//   - req: request of the second install
func assertUnchanged(t *testing.T, l *lab, r *Registry, req InstallRequest) Plugin {
	t.Helper()

	before := treeSnapshot(t, l.root)
	lockBefore, _ := os.ReadFile(l.paths.LockFile())

	st := plan(t, r, req)

	cur, same := st.AlreadyInstalled()
	if !same {
		t.Fatalf("AlreadyInstalled = false, diff = %+v", st.Diff())
	}

	if d := st.Diff(); d.NeedsConsent || len(d.Why) != 0 || !d.Update {
		t.Errorf("diff = %+v", d)
	}

	res, err := st.Commit(Grant{})
	if err != nil || !res.Unchanged || res.Updated || res.ID != cur.ID || res.State != cur.State {
		t.Fatalf("result = %+v, %v", res, err)
	}

	if _, err := st.Commit(Grant{}); asError(t, err).Code != CodeStale {
		t.Errorf("second commit = %v", err)
	}

	if names := stagingNames(t, l); len(names) != 0 {
		t.Errorf("staging = %v", names)
	}

	lockAfter, _ := os.ReadFile(l.paths.LockFile())
	if !bytes.Equal(lockBefore, lockAfter) {
		t.Errorf("the lock changed:\n%s\n%s", lockBefore, lockAfter)
	}

	if after := treeSnapshot(t, l.root); !equalTrees(before, after) {
		t.Errorf("the files changed:\n before %v\n after %v", before, after)
	}

	return cur
}

// TestInstallIdempotent checks that installing what is installed already changes nothing and
// asks nothing, for a directory, a bundled plugin and a release, whatever the state of the
// installed plugin, and that every other request keeps the rules of an update: another version
// updates, another source is source_changed, the same version with other files is refused as not
// newer, --force and an update are not idempotent, and a tree that was modified is not "installed".
//
// Parameters:
//   - t: test to report to
func TestInstallIdempotent(t *testing.T) {
	plain := surfaceBase("", "", "")

	t.Run("directory", func(t *testing.T) {
		l := newLab(t)
		r, dir := seeded(t, l, plain)

		cur := assertUnchanged(t, l, r, InstallRequest{Source: dir})
		if cur.State != StateEnabled || cur.Record.Version != "1.0.0" {
			t.Errorf("plugin = %s %+v", cur.State, cur.Record)
		}

		// A request with a language and an id names the same package.
		assertUnchanged(t, l, r, InstallRequest{Source: dir, ID: "demo", Lang: "fr"})
	})

	t.Run("states", func(t *testing.T) {
		l := newLab(t)
		r, dir := seeded(t, l, plain)

		if _, err := r.Disable("demo"); err != nil {
			t.Fatal(err)
		}

		cur := assertUnchanged(t, l, r, InstallRequest{Source: dir})
		if cur.State != StateDisabled {
			t.Errorf("state = %s", cur.State)
		}

		must(t, l.lock().Update("demo", func(rec *LockRecord) {
			rec.ConsentedAt, rec.ConsentedBy = "", ""
		}))

		cur = assertUnchanged(t, l, r, InstallRequest{Source: dir})
		if cur.State != StateDisabled || !hasReason(cur.Reasons, ReasonNotConsented) {
			t.Errorf("state = %s %v", cur.State, cur.Reasons)
		}
	})

	t.Run("bundled", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")

		b := bundle(t, l, map[string]string{"plugin.toml": manifest("demo", "1.0.0",
			[]string{"exec:tool"}, cmdWith("go", ""))})
		r := l.reg(func(o *Options) { o.Bundled = b })

		st := plan(t, r, InstallRequest{Source: "demo"})
		if _, same := st.AlreadyInstalled(); same {
			t.Fatal("a new install is not already installed")
		}

		if _, err := st.Commit(Grant{Prompt: true}); err != nil {
			t.Fatal(err)
		}

		assertUnchanged(t, l, r, InstallRequest{Source: "demo"})
	})

	t.Run("release", func(t *testing.T) {
		e := newRelEnv(t)
		data := demoArchive(t, "1.0.0")
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", data)

		e.install(url, data)

		cur := assertUnchanged(t, e.l, e.r, InstallRequest{Source: url, SHA256: sumOf(data)})
		if cur.Record.Kind != KindRelease || cur.Record.Origin != "o/r" {
			t.Errorf("record = %+v", cur.Record)
		}

		// Another tag of the same repository is an update, not the same install.
		data2 := demoArchive(t, "1.1.0")
		url2 := e.publish("o", "r", "1.1.0", "demo.tar.gz", data2)

		st := plan(t, e.r, InstallRequest{Source: url2, SHA256: sumOf(data2)})
		if _, same := st.AlreadyInstalled(); same || !st.Diff().Update {
			t.Errorf("another version: %+v", st.Diff())
		}

		// Another repository is another source.
		other := e.publish("p", "q", "1.0.0", "demo.tar.gz", data)
		pe := e.planCode(InstallRequest{Source: other, SHA256: sumOf(data)})
		if pe.Code != CodeSourceChanged {
			t.Errorf("another source = %v", pe)
		}

		// A record that names another origin for the same URL is not the package that is asked
		// for, even with the same files.
		must(t, e.l.lock().Update("demo", func(rec *LockRecord) { rec.Origin = "p/q" }))

		pe = e.planCode(InstallRequest{Source: url, SHA256: sumOf(data)})
		if pe.Code != CodeSourceChanged {
			t.Errorf("another recorded origin = %v", pe)
		}
	})

	t.Run("a record that disagrees with the files", func(t *testing.T) {
		l := newLab(t)
		r, dir := seeded(t, l, plain)

		// The tree hash covers the version, so the record is the thing that is wrong: the install
		// is the installed package, and the state of the plugin tells what is wrong with it.
		must(t, l.lock().Update("demo", func(rec *LockRecord) { rec.Version = "9.9.9" }))

		cur := assertUnchanged(t, l, r, InstallRequest{Source: dir})
		if cur.State != StateBroken || !hasReason(cur.Reasons, ReasonLockVersion) {
			t.Errorf("state = %s %v", cur.State, cur.Reasons)
		}
	})

	t.Run("warning arguments", func(t *testing.T) {
		l := newLab(t)
		r, dir := seeded(t, l, plain)
		rec, _ := l.lock().Get("demo")

		tests := []struct {
			name string
			text string
			code string
			args []string
		}{
			{"older", withVersion(plain, "0.9.0"), WarnDowngrade, []string{"0.9.0", "1.0.0"}},
			{"same", plain, WarnSameVersion, []string{"1.0.0"}},
			{
				"pre-release", withVersion(plain, "2.0.0-rc.1"), WarnPrerelease,
				[]string{"2.0.0-rc.1"},
			},
		}

		for _, tt := range tests {
			rewrite(t, dir, tt.text)

			st := plan(t, r, InstallRequest{Source: dir, Force: true})

			var got []string

			for _, w := range st.Diff().Warnings {
				if w.Code == tt.code {
					got = w.Args
				}
			}

			if !reflect.DeepEqual(got, tt.args) {
				t.Errorf("%s: %s args = %q, want %q", tt.name, tt.code, got, tt.args)
			}

			must(t, st.Abort())
		}

		// The source warning names where the plugin came from, then where this one comes from.
		other := l.srcDir("other", map[string]string{"plugin.toml": plain})
		st := plan(t, r, InstallRequest{Source: other, Force: true})

		var got []string

		for _, w := range st.Diff().Warnings {
			if w.Code == WarnSourceChanged {
				got = w.Args
			}
		}

		if want := []string{rec.Source, other}; !reflect.DeepEqual(got, want) {
			t.Errorf("source_changed args = %q, want %q", got, want)
		}

		must(t, st.Abort())
	})

	t.Run("not the same", func(t *testing.T) {
		l := newLab(t)
		r, dir := seeded(t, l, plain)
		rec, _ := l.lock().Get("demo")

		other := l.srcDir("other", map[string]string{"plugin.toml": plain})
		if pe := planErr(t, r, InstallRequest{Source: other}); pe.Code != CodeSourceChanged {
			t.Errorf("same files, another directory = %v", pe)
		}

		st := plan(t, r, InstallRequest{Source: other, Force: true})
		_, same := st.AlreadyInstalled()
		if same || !hasReason(st.Diff().Warnings, WarnSourceChanged) {
			t.Errorf("forced from another source: %+v", st.Diff())
		}

		must(t, st.Abort())

		st = plan(t, r, InstallRequest{Source: dir, Force: true})
		_, same = st.AlreadyInstalled()
		if same || !hasReason(st.Diff().Warnings, WarnSameVersion) || !st.Diff().NeedsConsent {
			t.Errorf("forced: %+v", st.Diff())
		}

		must(t, st.Abort())

		upd := InstallRequest{Source: dir, ID: "demo", Update: true}
		if pe := planErr(t, r, upd); pe.Code != CodeDowngrade {
			t.Errorf("update of the same package = %v", pe)
		}

		rewrite(t, dir, withVersion(plain, "1.0.0")+"\n# another file with the same version\n")

		if pe := planErr(t, r, InstallRequest{Source: dir}); pe.Code != CodeDowngrade {
			t.Errorf("same version, other files = %v", pe)
		}

		rewrite(t, dir, withVersion(plain, "0.9.0"))

		if pe := planErr(t, r, InstallRequest{Source: dir}); pe.Code != CodeDowngrade {
			t.Errorf("older = %v", pe)
		}

		rewrite(t, dir, plain)

		// The installed tree was modified: the plugin is broken, so it is not "installed" as
		// such and the old rule answers.
		tree := l.treeDir(rec)
		f := filepath.Join(tree, "plugin.toml")
		l.open(tree)
		l.open(f)

		data, _ := os.ReadFile(f)
		data[len(data)-2] ^= 0x01
		must(t, os.WriteFile(f, data, 0o400))

		if pe := planErr(t, r, InstallRequest{Source: dir}); pe.Code != CodeDowngrade {
			t.Errorf("modified tree = %v", pe)
		}
	})
}
