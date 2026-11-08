package plugin

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestTamper alters an installed tree in each way a user, a sync tool or a hostile process with
// the rights of the user could, and checks that the plugin is broken with the matching reason and
// that nothing of the tampered manifest reaches the snapshot.
//
// Parameters:
//   - t: test to report to
func TestTamper(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, l *lab, dir string)
		code   string
	}{
		{"modified byte", func(t *testing.T, l *lab, d string) {
			f := filepath.Join(d, "plugin.toml")
			l.open(d)
			l.open(f)

			data, _ := os.ReadFile(f)
			data[len(data)-2] ^= 0x01
			must(t, os.WriteFile(f, data, 0o400))
		}, CodeTreeHash},
		{"added file", func(t *testing.T, l *lab, d string) {
			l.open(d)
			must(t, os.WriteFile(filepath.Join(d, "evil.sh"), []byte("#!/bin/sh\n"), 0o500))
		}, CodeTreeHash},
		{"removed file", func(t *testing.T, l *lab, d string) {
			l.open(d)
			must(t, os.Remove(filepath.Join(d, "plugin.toml")))
		}, CodeTreeHash},
		{"file became writable", func(t *testing.T, l *lab, d string) {
			l.open(filepath.Join(d, "plugin.toml"))
		}, CodeTreePerms},
		{
			"directory became writable", func(t *testing.T, l *lab, d string) { l.open(d) },
			CodeTreePerms,
		},
		{"file became a program", func(t *testing.T, l *lab, d string) {
			must(t, os.Chmod(filepath.Join(d, "plugin.toml"), 0o500))
		}, CodeTreeHash},
		{"file replaced by a symlink", func(t *testing.T, l *lab, d string) {
			l.open(d)

			f := filepath.Join(d, "plugin.toml")
			data, _ := os.ReadFile(f)
			outside := filepath.Join(l.root, "outside.toml")
			must(t, os.WriteFile(outside, data, 0o600))
			must(t, os.Remove(f))
			must(t, os.Symlink(outside, f))
		}, CodeTreeSymlink},
		{"tree replaced by a symlink", func(t *testing.T, l *lab, d string) {
			moved := d + ".real"
			must(t, os.Rename(d, moved))
			must(t, os.Symlink(moved, d))
		}, CodeTreeSymlink},
		{"tree removed", func(t *testing.T, l *lab, d string) {
			l.open(d)
			must(t, os.Remove(filepath.Join(d, "plugin.toml")))
			must(t, os.Remove(d))
		}, CodeTreeMissing},
		{"pipe in the tree", func(t *testing.T, l *lab, d string) {
			l.open(d)
			must(t, syscall.Mkfifo(filepath.Join(d, "pipe"), 0o600))
		}, CodeTreeSpecial},
		{"id directory is a symlink", func(t *testing.T, l *lab, d string) {
			id := filepath.Dir(d)
			moved := id + ".real"
			must(t, os.Rename(id, moved))
			must(t, os.Symlink(moved, id))
		}, CodeTreeSymlink},
		{"id directory writable by the group", func(t *testing.T, l *lab, d string) {
			must(t, os.Chmod(filepath.Dir(d), 0o770))
		}, CodeStoreWritable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			rec := l.install("demo", demoFiles("demo", ""))

			tt.change(t, l, l.treeDir(rec))

			p := load(t, l, "demo")

			if p.State != StateBroken || !hasReason(p.Reasons, tt.code) {
				t.Fatalf("state = %s, reasons = %v, want broken with %s", p.State,
					reasonCodes(p.Reasons), tt.code)
			}

			if p.State.Runnable() {
				t.Error("a tampered plugin is runnable")
			}

			if tt.code == CodeTreeHash && (p.Manifest != nil || len(p.Caps) != 0) {
				t.Error("the manifest of a tree that failed its hash was used")
			}
		})
	}
}

// TestCacheSeesTamper checks that the fingerprint cache does not hide a change: a byte flipped
// without changing the size, with the modification time put back, is still caught because the
// change time moved, on the second Load of the same registry.
//
// Parameters:
//   - t: test to report to
func TestCacheSeesTamper(t *testing.T) {
	l := newLab(t)
	rec := l.install("demo", demoFiles("demo", ""))
	reg := l.registry(nil)

	if p, _ := reg.Load().Get("demo"); p.State != StateEnabled {
		t.Fatalf("first load: %s", p.State)
	}

	dir := l.treeDir(rec)
	file := filepath.Join(dir, "plugin.toml")

	info, _ := os.Stat(file)
	data, _ := os.ReadFile(file)
	data[10] ^= 0x01

	l.open(dir)
	l.open(file)
	must(t, os.WriteFile(file, data, 0o400))
	must(t, os.Chtimes(file, info.ModTime(), info.ModTime()))
	must(t, os.Chmod(dir, 0o500))

	p, _ := reg.Load().Get("demo")
	if p.State != StateBroken || !hasReason(p.Reasons, CodeTreeHash) {
		t.Errorf("second load: %s %v", p.State, reasonCodes(p.Reasons))
	}
}

// TestCacheReusesTheHash checks that a second Load of an untouched tree does not read the files
// again: the cached tree object comes back, and a chmod is still seen because the mode is read
// from the fresh walk.
//
// Parameters:
//   - t: test to report to
func TestCacheReusesTheHash(t *testing.T) {
	l := newLab(t)
	rec := l.install("demo", demoFiles("demo", ""))
	reg := l.registry(nil)

	reg.Load()

	reg.mu.Lock()
	first := reg.cache[l.treeDir(rec)]
	reg.mu.Unlock()

	if first == nil || first.parsed == nil {
		t.Fatal("nothing cached after Load")
	}

	reg.Load()

	reg.mu.Lock()
	second := reg.cache[l.treeDir(rec)]
	reg.mu.Unlock()

	if second != first {
		t.Error("an untouched tree was hashed again")
	}

	l.open(filepath.Join(l.treeDir(rec), "plugin.toml"))

	if p, _ := reg.Load().Get("demo"); p.State != StateBroken || !hasReason(p.Reasons, CodeTreePerms) {
		t.Errorf("a chmod behind the cache: %s %v", p.State, reasonCodes(p.Reasons))
	}
}

// TestCachePrunesRemovedTrees checks that the cache forgets a tree that is gone.
//
// Parameters:
//   - t: test to report to
func TestCachePrunesRemovedTrees(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", ""))
	reg := l.registry(nil)
	reg.Load()

	must(t, l.lock().Delete("demo"))
	reg.Load()

	reg.mu.Lock()
	defer reg.mu.Unlock()

	if len(reg.cache) != 0 {
		t.Errorf("cache holds %d entries after the record went away", len(reg.cache))
	}
}
