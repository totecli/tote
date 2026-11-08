package plugin

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/config"
)

// lab is a sandbox for the registry tests: a home, the four tote directories and a directory of
// fake programs, all below one temporary directory. Nothing outside it is touched.
type lab struct {
	t     *testing.T
	root  string
	home  string
	paths config.Paths
	bin   string
	env   map[string]string
}

// newLab creates a sandbox with a fake PATH that holds only its own program directory.
//
// Parameters:
//   - t: test to report to; the sandbox is removed with it
func newLab(t *testing.T) *lab {
	t.Helper()

	root := t.TempDir()

	// Installed trees are read-only; give the write bit back before the temporary directory is
	// removed. Cleanups run last in, first out, so this one runs before the removal.
	t.Cleanup(func() { restoreWrite(root) })

	l := &lab{
		t: t, root: root, home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin"),
		env: map[string]string{},
	}

	for _, d := range []string{l.home, l.bin} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	l.env["PATH"] = l.bin
	l.env["TOTE_CONFIG_DIR"] = filepath.Join(root, "config")
	l.env["TOTE_DATA_DIR"] = filepath.Join(root, "data")
	l.env["TOTE_STATE_DIR"] = filepath.Join(root, "state")
	l.env["TOTE_CACHE_DIR"] = filepath.Join(root, "cache")

	paths, err := config.Resolve(func(k string) string { return l.env[k] }, l.home)
	if err != nil {
		t.Fatal(err)
	}

	l.paths = paths

	return l
}

// restoreWrite gives every directory below root the owner write bit, so that it can be removed.
//
// Parameters:
//   - root: directory to open up
func restoreWrite(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o700)
		}

		return nil
	})
}

// registry opens a registry over the sandbox.
//
// Parameters:
//   - mutate: changes the options before the registry is opened; may be nil
func (l *lab) registry(mutate func(*Options)) *Registry {
	o := Options{
		Paths: l.paths, Host: testHost(), Home: l.home, Cwd: filepath.Join(l.root, "cwd"),
		Lookup: func(k string) (string, bool) { v, ok := l.env[k]; return v, ok },
	}

	if mutate != nil {
		mutate(&o)
	}

	return Open(o)
}

// fakeBin creates a program in the fake PATH directory.
//
// Parameters:
//   - name: program name
func (l *lab) fakeBin(name string) {
	l.t.Helper()

	if err := os.WriteFile(filepath.Join(l.bin, name), []byte("#!/bin/sh\n"), 0o700); err != nil {
		l.t.Fatal(err)
	}
}

// lock opens the lock of the sandbox.
func (l *lab) lock() *Lock {
	l.t.Helper()

	lk, err := OpenLock(l.paths.LockFile())
	if err != nil {
		l.t.Fatal(err)
	}

	return lk
}

// demoFiles builds the manifest of a small declarative plugin that runs "tool".
//
// Parameters:
//   - id: plugin id
//   - extra: tables added after the command, such as a [[requires]]
func demoFiles(id, extra string) map[string]string {
	src := decl("", []string{"exec:tool"}, cmdBody+extra)

	return map[string]string{"plugin.toml": strings.Replace(src, `id = "demo"`,
		`id = "`+id+`"`, 1)}
}

// install writes a tree for a plugin, read-only and in the place the registry looks for it,
// and records it in the lock as installed and consented. It returns the record that was written.
//
// Parameters:
//   - id: plugin id
//   - files: files of the package by slash separated path; a path ending in "*" gets the exec bit
//     after the star is dropped
func (l *lab) install(id string, files map[string]string) LockRecord {
	l.t.Helper()

	m, rep := Decode([]byte(files["plugin.toml"]), DecodeOptions{
		Host: testHost(), WantID: id,
		Official: true,
	})
	if m == nil {
		l.t.Fatalf("fixture manifest refused: %v", rep.Issues)
	}

	idDir, err := l.paths.PluginDir(id)
	if err != nil {
		l.t.Fatal(err)
	}

	build := filepath.Join(idDir, ".build")
	l.writeTree(build, files)

	tree, err := HashTree(build)
	if err != nil {
		l.t.Fatal(err)
	}

	dest, err := l.paths.PluginTree(id, tree.ID())
	if err != nil {
		l.t.Fatal(err)
	}

	if err := os.Rename(build, dest); err != nil {
		l.t.Fatal(err)
	}

	now := time.Date(2026, 11, 3, 10, 15, 0, 0, time.UTC).Format(time.RFC3339)
	caps := EffectiveCaps(m)
	surface := SurfaceSHA256(m)

	rec := LockRecord{
		ID: id, Version: m.Version, Tier: m.Tier, Kind: KindBundled,
		TreeSHA256: tree.SHA256, SurfaceSHA256: surface, InstalledAt: now, UpdatedAt: now,
		Caps: caps, ConsentedAt: now, ConsentedBy: ConsentPrompt,
	}

	if err := l.lock().Put(rec); err != nil {
		l.t.Fatal(err)
	}

	return rec
}

// writeTree creates a package directory with the modes of an installed tree: files 0400, programs
// 0500, directories 0500.
//
// Parameters:
//   - dir: directory to create
//   - files: files by slash separated path; a path ending in "*" is a program
func (l *lab) writeTree(dir string, files map[string]string) {
	l.t.Helper()

	var dirs []string

	for name, content := range files {
		mode := os.FileMode(0o400)
		if strings.HasSuffix(name, "*") {
			name, mode = strings.TrimSuffix(name, "*"), 0o500
		}

		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			l.t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			l.t.Fatal(err)
		}

		if err := os.Chmod(path, mode); err != nil {
			l.t.Fatal(err)
		}
	}

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, path)
		}

		return nil
	})

	// Deepest first, so that a parent is closed after its children.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })

	for _, d := range dirs {
		if err := os.Chmod(d, 0o500); err != nil {
			l.t.Fatal(err)
		}
	}
}

// treeDir returns the directory of the installed tree of a plugin.
//
// Parameters:
//   - rec: its lock record
func (l *lab) treeDir(rec LockRecord) string {
	l.t.Helper()

	dir, err := l.paths.PluginTree(rec.ID, TreeID(rec.TreeSHA256))
	if err != nil {
		l.t.Fatal(err)
	}

	return dir
}

// writeFile writes a file of the sandbox, creating its directory.
//
// Parameters:
//   - path: location
//   - content: new content
func (l *lab) writeFile(path, content string) {
	l.t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		l.t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		l.t.Fatal(err)
	}
}

// writeConfig writes config.toml.
//
// Parameters:
//   - content: new content
func (l *lab) writeConfig(content string) {
	l.t.Helper()

	l.writeFile(l.paths.ConfigFile(), content)
}

// open gives a path the write bit so that a test can alter an installed file, as a tamperer with
// the rights of the user would.
//
// Parameters:
//   - path: file or directory
func (l *lab) open(path string) {
	l.t.Helper()

	if err := os.Chmod(path, 0o700); err != nil {
		l.t.Fatal(err)
	}
}

// reasonCodes lists the codes of reasons.
//
// Parameters:
//   - rs: reasons
func reasonCodes(rs []Reason) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Code
	}

	return out
}

// hasReason reports whether a reason with a code is in the list.
//
// Parameters:
//   - rs: reasons
//   - code: code to look for
func hasReason(rs []Reason, code string) bool {
	for _, r := range rs {
		if r.Code == code {
			return true
		}
	}

	return false
}
