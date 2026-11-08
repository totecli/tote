package update

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sealTestTree makes a tree read-only the way an installed plugin is: directories 0500 and
// files 0400, and registers the cleanup that opens it again for the removal of the temp dir.
//
// Parameters:
//   - t: test to report to
//   - root: top of the tree
func sealTestTree(t *testing.T, root string) {
	t.Helper()

	var dirs []string

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		switch {
		case d.Type()&fs.ModeSymlink != 0:
		case d.IsDir():
			dirs = append(dirs, p)
		default:
			return os.Chmod(p, 0o400)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range slices.Backward(dirs) {
		if err := os.Chmod(p, 0o500); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o700)
			}

			return nil
		})
	})
}

// TestPurgeDirReadOnlyTree checks that a purge removes the read-only trees of installed plugins,
// with their lock and state, never follows a link out of them, and leaves nothing behind.
//
// Parameters:
//   - t: test to report to
func TestPurgeDirReadOnlyTree(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "precious"), "x")

	dir := filepath.Join(home, ".local", "share", "tote")
	tree := filepath.Join(dir, "plugins", "gft", "0123456789abcdef")

	write(t, filepath.Join(tree, "plugin.toml"), "x")
	write(t, filepath.Join(tree, "locales", "fr.toml"), "x")
	write(t, filepath.Join(tree, "locales", "deep", "en.toml"), "x")
	write(t, filepath.Join(dir, "plugins.lock"), "x")

	if err := os.Symlink(outside, filepath.Join(tree, "out")); err != nil {
		t.Fatal(err)
	}

	sealTestTree(t, filepath.Join(dir, "plugins"))

	if err := os.Chmod(filepath.Join(dir, "plugins.lock"), 0o400); err != nil {
		t.Fatal(err)
	}

	d := DataDir{Dir: dir, Entries: []string{"plugins", "plugins.lock", "absent"}}

	out, err := PurgeDir(d, home)
	if err != nil {
		t.Fatalf("PurgeDir: %v", err)
	}

	want := []string{filepath.Join(dir, "plugins"), filepath.Join(dir, "plugins.lock")}
	if !slices.Equal(out.Removed, want) || len(out.Failed) != 0 || out.Kept {
		t.Errorf("outcome = %+v, want removed %v", out, want)
	}

	if exists(dir) {
		t.Error("the data directory was left behind")
	}

	if content(t, filepath.Join(outside, "precious")) != "x" {
		t.Error("a link was followed out of the tree")
	}
}

// TestPurgeDirContinues checks that an entry that cannot be removed does not stop the others,
// and that the error names it with its full path.
//
// Parameters:
//   - t: test to report to
func TestPurgeDirContinues(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "tote")

	write(t, filepath.Join(dir, "other.toml"), "x")
	write(t, filepath.Join(dir, "after", "f"), "x")

	deep := filepath.Join(dir, "deep", strings.Repeat("d/", maxWipeDepth+2))
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	d := DataDir{Dir: dir, Entries: []string{"deep", "other.toml", "after"}}

	out, err := PurgeDir(d, home)
	if err == nil || !errors.Is(err, errWipeLimit) {
		t.Fatalf("error = %v, want errWipeLimit", err)
	}

	if !strings.Contains(err.Error(), filepath.Join(dir, "deep")) {
		t.Errorf("error %q does not name the entry", err)
	}

	wantRemoved := []string{filepath.Join(dir, "other.toml"), filepath.Join(dir, "after")}
	if !slices.Equal(out.Removed, wantRemoved) ||
		!slices.Equal(out.Failed, []string{filepath.Join(dir, "deep")}) || !out.Kept {
		t.Errorf("outcome = %+v", out)
	}

	if !exists(filepath.Join(dir, "deep")) || exists(filepath.Join(dir, "after")) {
		t.Error("the failed entry was lost or the later entry was skipped")
	}
}

// TestPurgeDirReadOnlyParent checks that when the directory itself refuses every removal, each
// entry is reported as failed and nothing is lost.
//
// Parameters:
//   - t: test to report to
func TestPurgeDirReadOnlyParent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}

	home := t.TempDir()
	dir := filepath.Join(home, "tote")

	write(t, filepath.Join(dir, "a.toml"), "x")
	write(t, filepath.Join(dir, "b.toml"), "x")

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	out, err := PurgeDir(DataDir{Dir: dir, Entries: []string{"a.toml", "b.toml"}}, home)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error = %v, want a permission error", err)
	}

	if len(out.Removed) != 0 || len(out.Failed) != 2 || !out.Kept {
		t.Errorf("outcome = %+v", out)
	}

	if !exists(filepath.Join(dir, "a.toml")) || !exists(filepath.Join(dir, "b.toml")) {
		t.Error("a file went although the directory is read-only")
	}
}

// TestPurgeDirChecksBeforeRemoving checks that a refused entry name stops the purge before
// anything is removed, so that a bad list never leaves a half done purge.
//
// Parameters:
//   - t: test to report to
func TestPurgeDirChecksBeforeRemoving(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "tote")
	write(t, filepath.Join(dir, "a.toml"), "x")

	out, err := PurgeDir(DataDir{Dir: dir, Entries: []string{"a.toml", "../x"}}, home)
	if !errors.Is(err, ErrUnsafePath) || len(out.Removed) != 0 {
		t.Errorf("PurgeDir = %+v, %v", out, err)
	}

	if !exists(filepath.Join(dir, "a.toml")) {
		t.Error("an entry was removed before the list was checked")
	}
}
