package update

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReviewTestsPurgeLinks checks that a link among the entries of a data directory is unlinked
// and never followed, also when its target is gone: the data it points at stays, and a dangling
// link does not stay behind in the directory.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsPurgeLinks(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(t.TempDir(), "data")
	outside := filepath.Join(t.TempDir(), "keep")

	for _, p := range []string{dir, outside} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	precious := filepath.Join(outside, "precious.txt")
	if err := os.WriteFile(precious, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(dir, "plugins")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(filepath.Join(outside, "gone"), filepath.Join(dir, "bin")); err != nil {
		t.Fatal(err)
	}

	out, err := PurgeDir(DataDir{Dir: dir, Entries: []string{"plugins", "bin"}}, home)
	if err != nil {
		t.Fatal(err)
	}

	if len(out.Removed) != 2 || out.Kept {
		t.Errorf("outcome = %+v, want both links removed and the directory gone", out)
	}

	if _, err := os.Stat(precious); err != nil {
		t.Errorf("the target of a link was followed: %v", err)
	}

	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("the data directory is still there: %v", err)
	}
}

// TestReviewTestsRemoveShellLinesUntouched checks that a startup file with no loading line is
// not rewritten: same file, same bytes, so a read-only or managed file is never disturbed.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsRemoveShellLinesUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	text := "export A=1\n# eval \"$(tote init bash)\"\necho \"eval $(tote init bash)\"\n"

	if err := os.WriteFile(path, []byte(text), 0o640); err != nil {
		t.Fatal(err)
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	n, err := RemoveShellLines(path)
	if n != 0 || err != nil {
		t.Fatalf("RemoveShellLines = %d, %v, want 0, nil", n, err)
	}

	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Errorf("the file was replaced: %v", err)
	}

	if data, _ := os.ReadFile(path); string(data) != text {
		t.Errorf("content = %q", data)
	}
}
