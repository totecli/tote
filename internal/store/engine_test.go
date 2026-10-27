package store

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestReviewKeysAreCaseSensitive checks that Get, Replace, Delete and Add compare keys exactly.
//
// Parameters:
//   - t: test to report to
func TestReviewKeysAreCaseSensitive(t *testing.T) {
	path := engWrite(t, "schema = 1\n\n[[rec]]\nname = \"Alpha\"\n")
	c := engOpen(t, path)

	if _, ok := c.Get("alpha"); ok {
		t.Error("Get(alpha) found Alpha")
	}
	if _, ok := c.Get("ALPHA"); ok {
		t.Error("Get(ALPHA) found Alpha")
	}
	if got, ok := c.Get("Alpha"); !ok || got.Name != "Alpha" {
		t.Errorf("Get(Alpha) = %v, %v", got, ok)
	}
	if got := c.Line("alpha"); got != 0 {
		t.Errorf("Line(alpha) = %d, want 0", got)
	}
	if err := c.Replace("alpha", testRec{Name: "alpha"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Replace(alpha) = %v, want ErrNotFound", err)
	}
	if err := c.Delete("ALPHA"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(ALPHA) = %v, want ErrNotFound", err)
	}
	if err := c.Add(testRec{Name: "alpha"}); err != nil {
		t.Errorf("Add(alpha) next to Alpha = %v, want nil", err)
	}
	if err := c.Replace("Alpha", testRec{Name: "alpha"}); !errors.Is(err, ErrExists) {
		t.Errorf("rename Alpha to alpha = %v, want ErrExists", err)
	}
}

// TestReviewTempFileBesideTarget checks that the temporary file lives in the directory of the
// resolved target, so that the rename stays on one file system and is atomic.
//
// Parameters:
//   - t: test to report to
func TestReviewTempFileBesideTarget(t *testing.T) {
	root := engRoot(t)
	realDir := filepath.Join(root, "real")
	linkDir := filepath.Join(root, "links")
	for _, d := range []string{realDir, linkDir} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	realFile := filepath.Join(realDir, "recs.toml")
	engExternal(t, realFile, engFixture)
	link := filepath.Join(linkDir, "recs.toml")
	if err := os.Symlink(realFile, link); err != nil {
		t.Fatal(err)
	}
	wantDir, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatal(err)
	}

	var from, to string
	orig := renameFile
	renameFile = func(oldpath, newpath string) error {
		from, to = oldpath, newpath
		return orig(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = orig })

	if err := engOpen(t, link).Add(testRec{Name: "z"}); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(from) != wantDir {
		t.Errorf("temporary file in %s, want %s", filepath.Dir(from), wantDir)
	}
	if filepath.Dir(to) != wantDir || filepath.Base(to) != "recs.toml" {
		t.Errorf("rename target %s, want recs.toml in %s", to, wantDir)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", fi, err)
	}
}

// TestReviewLockFileBesideTarget checks the name, the mode and the survival of the lock file.
//
// Parameters:
//   - t: test to report to
func TestReviewLockFileBesideTarget(t *testing.T) {
	path := engWrite(t, engFixture)
	c := engOpen(t, path)

	for _, name := range []string{"x", "y"} {
		if err := c.Add(testRec{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if m := engMode(t, path+".lock"); m != 0o600 {
		t.Errorf("lock file mode = %o, want 600", m)
	}
	if err := c.Delete("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Errorf("lock file gone after a write: %v", err)
	}
	engNoTemp(t, filepath.Dir(path))
}

// TestReviewReadOnlyDirectory checks that a write into a directory that cannot be written fails
// cleanly: an error, the file unchanged and no temporary file left behind.
//
// Parameters:
//   - t: test to report to
func TestReviewReadOnlyDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recs.toml")
	engExternal(t, path, engFixture)
	c := engOpen(t, path)

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		t.Skip("the directory is writable anyway, probably running as root")
	}

	if err := c.Add(testRec{Name: "x"}); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Errorf("Add without the lock file = %v, want a lock error", err)
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(testRec{Name: "y"}); err != nil {
		t.Fatalf("Add = %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	err := c.Add(testRec{Name: "z"})
	if err == nil || !strings.Contains(err.Error(), "temporary file") {
		t.Errorf("Add with the lock file but no room for a temporary file = %v", err)
	}
	if _, ok := c.Get("z"); ok {
		t.Error("z is in the collection after a failed write")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := engKeys(engOpen(t, path).List()); len(got) != 4 || got[3] != "y" {
		t.Errorf("keys on disk = %v, want a b c y", got)
	}
	engNoTemp(t, dir)
}

// TestReviewHelperProcess is the body of the child processes of TestReviewProcessesShareFile.
//
// It does nothing unless the environment asks for it.
//
// Parameters:
//   - t: test to report to
func TestReviewHelperProcess(t *testing.T) {
	path := os.Getenv("TOTE_REVIEW_PATH")
	if path == "" {
		t.Skip("child process body, run by TestReviewProcessesShareFile")
	}
	worker := os.Getenv("TOTE_REVIEW_WORKER")
	n, err := strconv.Atoi(os.Getenv("TOTE_REVIEW_COUNT"))
	if err != nil {
		t.Fatal(err)
	}
	if err := addMany(path, atoiOrZero(worker), n); err != nil {
		t.Fatal(err)
	}
}

// atoiOrZero converts a decimal string to an int and gives 0 when it is not a number.
//
// Parameters:
//   - s: text to convert
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}

	return n
}

// TestReviewProcessesShareFile checks the lock between real processes, not only goroutines.
//
// Parameters:
//   - t: test to report to
func TestReviewProcessesShareFile(t *testing.T) {
	path := engWrite(t, "schema = 1\n")
	const workers, each = 4, 12

	cmds := make([]*exec.Cmd, workers)
	for w := range workers {
		cmd := exec.Command(os.Args[0], "-test.run=^TestReviewHelperProcess$")
		cmd.Env = append(os.Environ(), "TOTE_REVIEW_PATH="+path,
			fmt.Sprintf("TOTE_REVIEW_WORKER=%d", w), fmt.Sprintf("TOTE_REVIEW_COUNT=%d", each))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[w] = cmd
	}
	for w, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Errorf("worker %d: %v", w, err)
		}
	}

	c := engOpen(t, path)
	if c.Err() != nil {
		t.Fatalf("file unreadable after the run: %v", c.Err())
	}
	if got := len(c.List()); got != workers*each {
		t.Errorf("%d records on disk, want %d", got, workers*each)
	}
	engNoTemp(t, filepath.Dir(path))
}
