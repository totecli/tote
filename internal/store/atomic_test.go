package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// engListDir returns the sorted names of the entries of a directory.
//
// Parameters:
//   - t: test to report to
//   - dir: directory to list
func engListDir(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)

	return names
}

// engMode returns the permission bits of a path, following symlinks.
//
// Parameters:
//   - t: test to report to
//   - path: file or directory
func engMode(t *testing.T, path string) os.FileMode {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	return info.Mode().Perm()
}

// TestWriteAtomicNewFile checks the content, the mode and the leftovers of a first write.
//
// Parameters:
//   - t: test to report to
func TestWriteAtomicNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.toml")

	if err := writeAtomic(path, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello\n" {
		t.Fatalf("content = %q, %v", got, err)
	}
	if m := engMode(t, path); m != 0o600 {
		t.Errorf("mode = %o, want 600", m)
	}
	if names := engListDir(t, dir); !reflect.DeepEqual(names, []string{"data.toml"}) {
		t.Errorf("directory holds %v, want only data.toml", names)
	}
}

// TestWriteAtomicKeepsMode checks that an existing file keeps its permission bits.
//
// Parameters:
//   - t: test to report to
func TestWriteAtomicKeepsMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o640, 0o644, 0o604, 0o666} {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.toml")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}

		if err := writeAtomic(path, []byte("new")); err != nil {
			t.Fatal(err)
		}

		got, _ := os.ReadFile(path)
		if string(got) != "new" {
			t.Errorf("mode %o: content = %q", mode, got)
		}
		if m := engMode(t, path); m != mode {
			t.Errorf("mode %o became %o", mode, m)
		}
	}
}

// TestWriteAtomicRenameFails checks that a failed rename leaves no temporary file behind.
//
// Parameters:
//   - t: test to report to
func TestWriteAtomicRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.toml")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := renameFile
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { renameFile = orig })

	err := writeAtomic(path, []byte("new"))
	if err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("error = %v, want the injected failure", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "old" {
		t.Errorf("content = %q, want the old content", got)
	}
	if names := engListDir(t, dir); !reflect.DeepEqual(names, []string{"data.toml"}) {
		t.Errorf("directory holds %v, want only data.toml", names)
	}
}

// TestWriteAtomicMissingDir checks the error for a directory that does not exist.
//
// Parameters:
//   - t: test to report to
func TestWriteAtomicMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "data.toml")
	if err := writeAtomic(path, []byte("x")); err == nil {
		t.Fatal("writeAtomic in a missing directory succeeded")
	}
}

// engRoot returns a temporary directory with its symlinks resolved.
//
// Parameters:
//   - t: test to report to
func engRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return root
}

// TestResolveTargetCreatesParents checks that missing parents get mode 0700.
//
// Parameters:
//   - t: test to report to
func TestResolveTargetCreatesParents(t *testing.T) {
	root := engRoot(t)
	path := filepath.Join(root, "a", "b", "f.toml")

	got, err := resolveTarget(path)
	if err != nil || got != path {
		t.Fatalf("resolveTarget = %q, %v, want %q", got, err, path)
	}
	for _, d := range []string{"a", filepath.Join("a", "b")} {
		if m := engMode(t, filepath.Join(root, d)); m != 0o700 {
			t.Errorf("%s mode = %o, want 700", d, m)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file itself must not be created, stat error = %v", err)
	}
}

// TestResolveTargetFileSymlink checks that a symlink resolves to its target.
//
// Parameters:
//   - t: test to report to
func TestResolveTargetFileSymlink(t *testing.T) {
	root := engRoot(t)
	real := filepath.Join(root, "real.toml")
	link := filepath.Join(root, "link.toml")
	if err := os.WriteFile(real, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if got, err := resolveTarget(link); err != nil || got != real {
		t.Errorf("resolveTarget = %q, %v, want %q", got, err, real)
	}
}

// TestResolveTargetDirSymlink checks a new file inside a symlinked directory.
//
// Parameters:
//   - t: test to report to
func TestResolveTargetDirSymlink(t *testing.T) {
	root := engRoot(t)
	realDir := filepath.Join(root, "realdir")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}

	got, err := resolveTarget(filepath.Join(root, "dirlink", "new.toml"))
	if want := filepath.Join(realDir, "new.toml"); err != nil || got != want {
		t.Errorf("resolveTarget = %q, %v, want %q", got, err, want)
	}
}

// TestResolveTargetDangling checks that a dangling symlink is refused.
//
// Parameters:
//   - t: test to report to
func TestResolveTargetDangling(t *testing.T) {
	root := engRoot(t)
	link := filepath.Join(root, "dangling.toml")
	if err := os.Symlink(filepath.Join(root, "missing.toml"), link); err != nil {
		t.Fatal(err)
	}

	_, err := resolveTarget(link)
	if err == nil || !strings.Contains(err.Error(), "dangling") {
		t.Errorf("error = %v, want a dangling symlink error", err)
	}
}

// TestResolveTargetParentIsFile checks the error when a parent is a regular file.
//
// Parameters:
//   - t: test to report to
func TestResolveTargetParentIsFile(t *testing.T) {
	root := engRoot(t)
	file := filepath.Join(root, "plain")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveTarget(filepath.Join(file, "f.toml")); err == nil {
		t.Error("resolveTarget under a regular file succeeded")
	}
}

// sameFixture is a record type with maps, pointers and numbers for TestSameValue.
type sameFixture struct {
	Name  string
	Tags  []string
	Attrs map[string]int
	Next  *sameFixture
	N     int
	U     uint8
	F     float64
	B     bool
	Any   any
	Arr   [2]int
}

// TestSameValue checks the comparison used to verify an edit.
//
// Parameters:
//   - t: test to report to
func TestSameValue(t *testing.T) {
	base := func() sameFixture {
		return sameFixture{
			Name: "a", Tags: []string{"x"}, Attrs: map[string]int{"k": 1},
			Next: &sameFixture{Name: "n"}, N: 1, U: 2, F: 1.5, B: true, Any: "s",
		}
	}
	mod := func(f func(*sameFixture)) sameFixture {
		v := base()
		f(&v)
		return v
	}

	equal := []sameFixture{
		base(),
		mod(func(v *sameFixture) { v.Next.Tags = []string{} }),
		mod(func(v *sameFixture) { v.Next.Attrs = map[string]int{} }),
	}
	for i, v := range equal {
		if !sameValue(reflect.ValueOf(base()), reflect.ValueOf(v)) {
			t.Errorf("equal case %d reported different", i)
		}
	}
	if !sameValue(reflect.ValueOf(sameFixture{Tags: []string{}}), reflect.ValueOf(sameFixture{})) {
		t.Error("empty and nil slices reported different")
	}

	different := []sameFixture{
		mod(func(v *sameFixture) { v.Name = "b" }),
		mod(func(v *sameFixture) { v.Tags = []string{"y"} }),
		mod(func(v *sameFixture) { v.Tags = nil }),
		mod(func(v *sameFixture) { v.Attrs["k"] = 2 }),
		mod(func(v *sameFixture) { v.Attrs["z"] = 1 }),
		mod(func(v *sameFixture) { v.Next = nil }),
		mod(func(v *sameFixture) { v.Next.Name = "m" }),
		mod(func(v *sameFixture) { v.N = 2 }),
		mod(func(v *sameFixture) { v.U = 3 }),
		mod(func(v *sameFixture) { v.F = 2 }),
		mod(func(v *sameFixture) { v.B = false }),
		mod(func(v *sameFixture) { v.Any = 5 }),
		mod(func(v *sameFixture) { v.Any = nil }),
		mod(func(v *sameFixture) { v.Arr[1] = 1 }),
	}
	for i, v := range different {
		if sameValue(reflect.ValueOf(base()), reflect.ValueOf(v)) {
			t.Errorf("different case %d reported equal", i)
		}
	}

	ch := make(chan int)
	if sameValue(reflect.ValueOf(ch), reflect.ValueOf(ch)) {
		t.Error("channels reported equal")
	}
	if sameValue(reflect.ValueOf(1), reflect.ValueOf("1")) {
		t.Error("different kinds reported equal")
	}
}

// TestSameRecords checks the list comparison, including the Lost field that is never written.
//
// Parameters:
//   - t: test to report to
func TestSameRecords(t *testing.T) {
	a := []testRec{{Name: "a", Tags: []string{}}, {Name: "b"}}
	b := []testRec{{Name: "a"}, {Name: "b"}}
	if !sameRecords(a, b) {
		t.Error("nil and empty tags reported different")
	}
	if sameRecords(a, b[:1]) {
		t.Error("lists of different lengths reported equal")
	}
	if sameRecords(a, []testRec{{Name: "a"}, {Name: "b", Lost: "x"}}) {
		t.Error("a difference in Lost went unnoticed")
	}
	if !sameRecords[testRec](nil, []testRec{}) {
		t.Error("nil and empty lists reported different")
	}
}
