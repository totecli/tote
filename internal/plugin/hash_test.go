package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// writeFiles creates a plain directory of files for the hash tests. A name ending in "*" gets the
// exec bit.
//
// Parameters:
//   - t: test to report to
//   - files: content by slash separated path
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()

	for name, content := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, "*") {
			name, mode = strings.TrimSuffix(name, "*"), 0o755
		}

		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// sum256 is the hex sha256 of a string.
//
// Parameters:
//   - s: text to hash
func sum256(s string) string {
	h := sha256.Sum256([]byte(s))

	return hex.EncodeToString(h[:])
}

// TestHashTreeCanonical checks the hash against the canonical listing of section 7.1 written out
// by hand: path, NUL, size, NUL, sha256 of the content, NUL, x or -, line feed, in byte order of
// the whole path, so that "a-b" comes before "a/b" although a directory walk meets "a" first.
//
// Parameters:
//   - t: test to report to
func TestHashTreeCanonical(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"plugin.toml":     "api = 1\n",
		"bin/run*":        "#!/bin/sh\n",
		"a/b":             "inner",
		"a-b":             "dash",
		"locales/en.toml": "",
	})

	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	want := "a-b\x00" + "4\x00" + sum256("dash") + "\x00-\n" +
		"a/b\x00" + "5\x00" + sum256("inner") + "\x00-\n" +
		"bin/run\x00" + "10\x00" + sum256("#!/bin/sh\n") + "\x00x\n" +
		"locales/en.toml\x00" + "0\x00" + sum256("") + "\x00-\n" +
		"plugin.toml\x00" + "8\x00" + sum256("api = 1\n") + "\x00-\n"

	tree, err := HashTree(dir)
	if err != nil {
		t.Fatal(err)
	}

	if tree.SHA256 != sum256(want) {
		t.Errorf("tree hash = %s, want %s", tree.SHA256, sum256(want))
	}

	if len(tree.Files) != 5 || tree.Files[0].Path != "a-b" || tree.Files[1].Path != "a/b" ||
		!tree.Files[2].Exec || tree.Files[3].Exec {
		t.Errorf("files = %+v", tree.Files)
	}

	if len(tree.ID()) != 32 || tree.ID() != tree.SHA256[:32] || TreeID("abc") != "abc" {
		t.Errorf("ID = %q", tree.ID())
	}
}

// TestHashTreeDeterministic checks that the hash depends on the names, the contents and the exec
// bit only: the creation order, the owner write bit, the timestamps and the empty directories do
// not change it, and the same files in two places give the same hash.
//
// Parameters:
//   - t: test to report to
func TestHashTreeDeterministic(t *testing.T) {
	files := map[string]string{"plugin.toml": "x", "b/c": "y", "b/a*": "z", "locales/fr.toml": "w"}

	first, err := HashTree(writeFiles(t, files))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		dir := writeFiles(t, files)

		if i == 3 {
			past := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := os.Chtimes(filepath.Join(dir, "plugin.toml"), past, past); err != nil {
				t.Fatal(err)
			}

			if err := os.MkdirAll(filepath.Join(dir, "extra", "empty"), 0o755); err != nil {
				t.Fatal(err)
			}
		}

		if i == 4 {
			if err := os.Chmod(filepath.Join(dir, "plugin.toml"), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		got, err := HashTree(dir)
		if err != nil {
			t.Fatal(err)
		}

		if got.SHA256 != first.SHA256 {
			t.Errorf("run %d: hash %s differs from %s", i, got.SHA256, first.SHA256)
		}
	}
}

// TestHashTreeTamper checks that each way of changing a tree changes the hash: one byte, a new
// file, a removed file, a rename, a file that becomes a program and a program that stops being one.
//
// Parameters:
//   - t: test to report to
func TestHashTreeTamper(t *testing.T) {
	base := map[string]string{"plugin.toml": "manifest", "bin/run*": "program", "doc": "text"}

	tests := []struct {
		name   string
		change func(dir string) error
	}{
		{"modified byte", func(d string) error {
			return os.WriteFile(filepath.Join(d, "doc"), []byte("tExt"), 0o644)
		}},
		{"same size, other byte", func(d string) error {
			return os.WriteFile(filepath.Join(d, "plugin.toml"), []byte("manifesu"), 0o644)
		}},
		{"added file", func(d string) error {
			return os.WriteFile(filepath.Join(d, "extra"), nil, 0o644)
		}},
		{"added empty file in a subdirectory", func(d string) error {
			if err := os.Mkdir(filepath.Join(d, "sub"), 0o755); err != nil {
				return err
			}

			return os.WriteFile(filepath.Join(d, "sub", "x"), nil, 0o644)
		}},
		{"removed file", func(d string) error { return os.Remove(filepath.Join(d, "doc")) }},
		{"renamed file", func(d string) error {
			return os.Rename(filepath.Join(d, "doc"), filepath.Join(d, "docs"))
		}},
		{"exec bit set", func(d string) error { return os.Chmod(filepath.Join(d, "doc"), 0o755) }},
		{"exec bit cleared", func(d string) error {
			return os.Chmod(filepath.Join(d, "bin", "run"), 0o644)
		}},
	}

	ref, err := HashTree(writeFiles(t, base))
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeFiles(t, base)
			if err := tt.change(dir); err != nil {
				t.Fatal(err)
			}

			got, err := HashTree(dir)
			if err != nil {
				t.Fatal(err)
			}

			if got.SHA256 == ref.SHA256 {
				t.Error("the hash did not change")
			}
		})
	}
}

// TestHashTreeModeBitsOtherThanExec checks that a mode change other than the exec bit leaves the
// hash alone, because the hash says what runs and the mode check is a separate finding.
//
// Parameters:
//   - t: test to report to
func TestHashTreeModeBitsOtherThanExec(t *testing.T) {
	dir := writeFiles(t, map[string]string{"plugin.toml": "m"})

	a, _ := HashTree(dir)

	if err := os.Chmod(filepath.Join(dir, "plugin.toml"), 0o666); err != nil {
		t.Fatal(err)
	}

	b, _ := HashTree(dir)
	if a.SHA256 != b.SHA256 {
		t.Error("a write bit changed the hash")
	}
}

// TestHashTreeRefuses checks the entries that must not exist in a tree: links of both kinds,
// pipes and sockets, names outside the grammar of the archive entries, and a tree that is not a
// directory. Each is refused with its own code and the path of the entry.
//
// Parameters:
//   - t: test to report to
func TestHashTreeRefuses(t *testing.T) {
	tests := []struct {
		name string
		make func(t *testing.T, dir string)
		code string
		path string
	}{
		{"symlink to a file", func(t *testing.T, d string) {
			must(t, os.Symlink("plugin.toml", filepath.Join(d, "link")))
		}, CodeTreeSymlink, "link"},
		{"symlink to outside", func(t *testing.T, d string) {
			must(t, os.Symlink("/etc/passwd", filepath.Join(d, "link")))
		}, CodeTreeSymlink, "link"},
		{"dangling symlink", func(t *testing.T, d string) {
			must(t, os.Symlink("nowhere", filepath.Join(d, "link")))
		}, CodeTreeSymlink, "link"},
		{"symlink to a directory", func(t *testing.T, d string) {
			must(t, os.Symlink("sub", filepath.Join(d, "link")))
		}, CodeTreeSymlink, "link"},
		{"symlink in a subdirectory", func(t *testing.T, d string) {
			must(t, os.Symlink("../plugin.toml", filepath.Join(d, "sub", "link")))
		}, CodeTreeSymlink, "sub/link"},
		{"named pipe", func(t *testing.T, d string) {
			must(t, syscall.Mkfifo(filepath.Join(d, "pipe"), 0o600))
		}, CodeTreeSpecial, "pipe"},
		{"space in a name", func(t *testing.T, d string) {
			must(t, os.WriteFile(filepath.Join(d, "a b"), nil, 0o600))
		}, CodeTreeName, "a b"},
		{"line feed in a name", func(t *testing.T, d string) {
			must(t, os.WriteFile(filepath.Join(d, "a\nb"), nil, 0o600))
		}, CodeTreeName, "a\nb"},
		{"leading dash", func(t *testing.T, d string) {
			must(t, os.WriteFile(filepath.Join(d, "-rf"), nil, 0o600))
		}, CodeTreeName, "-rf"},
		{"non ASCII name", func(t *testing.T, d string) {
			must(t, os.WriteFile(filepath.Join(d, "caf\u00e9"), nil, 0o600))
		}, CodeTreeName, "caf\u00e9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{"plugin.toml": "m", "sub/x": "y"})
			tt.make(t, dir)

			_, err := HashTree(dir)

			var terr *TreeError
			if !errors.As(err, &terr) {
				t.Fatalf("error = %v, want a *TreeError", err)
			}

			if terr.Code != tt.code || terr.Path != tt.path {
				t.Errorf("got %s at %q, want %s at %q", terr.Code, terr.Path, tt.code, tt.path)
			}
		})
	}

	t.Run("root is a symlink", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{"plugin.toml": "m"})
		link := filepath.Join(t.TempDir(), "tree")
		must(t, os.Symlink(dir, link))

		assertTreeCode(t, link, CodeTreeSymlink)
	})

	t.Run("root is a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "tree")
		must(t, os.WriteFile(file, nil, 0o600))

		assertTreeCode(t, file, CodeTreeSpecial)
	})

	t.Run("root is missing", func(t *testing.T) {
		assertTreeCode(t, filepath.Join(t.TempDir(), "gone"), CodeTreeMissing)
	})
}

// must fails the test on an error.
//
// Parameters:
//   - t: test to report to
//   - err: error to check
func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

// assertTreeCode checks that hashing a path fails with a code.
//
// Parameters:
//   - t: test to report to
//   - path: tree root
//   - code: expected code
func assertTreeCode(t *testing.T, path, code string) {
	t.Helper()

	_, err := HashTree(path)

	var terr *TreeError
	if !errors.As(err, &terr) || terr.Code != code {
		t.Errorf("HashTree(%s) = %v, want %s", filepath.Base(path), err, code)
	}
}

// TestHashTreeLimits checks the bounds that keep the cost of a hash small: the number of entries,
// the depth, the length of a path, the size of a file and the size of the whole tree. A sparse
// file stands for the large ones, and none of them is read: the size is checked on the entry
// first, so the test allocates nothing.
//
// Parameters:
//   - t: test to report to
func TestHashTreeLimits(t *testing.T) {
	t.Run("entries", func(t *testing.T) {
		dir := t.TempDir()
		for i := 0; i < MaxTreeEntries+1; i++ {
			must(t, os.WriteFile(filepath.Join(dir, "f"+itoa(i)), nil, 0o600))
		}

		assertTreeCode(t, dir, CodeTreeLimit)
	})

	t.Run("exactly the limit of entries", func(t *testing.T) {
		dir := t.TempDir()
		for i := 0; i < MaxTreeEntries; i++ {
			must(t, os.WriteFile(filepath.Join(dir, "f"+itoa(i)), nil, 0o600))
		}

		if _, err := HashTree(dir); err != nil {
			t.Errorf("512 entries refused: %v", err)
		}
	})

	t.Run("depth", func(t *testing.T) {
		dir := t.TempDir()
		deep := filepath.Join(dir, "a", "b", "c", "d", "e", "f", "g")
		must(t, os.MkdirAll(deep, 0o755))

		assertTreeCode(t, dir, CodeTreeLimit)
	})

	t.Run("exactly the depth", func(t *testing.T) {
		dir := t.TempDir()
		deep := filepath.Join(dir, "a", "b", "c", "d", "e")
		must(t, os.MkdirAll(deep, 0o755))
		must(t, os.WriteFile(filepath.Join(deep, "f"), nil, 0o600))

		if _, err := HashTree(dir); err != nil {
			t.Errorf("depth 6 refused: %v", err)
		}
	})

	t.Run("path length", func(t *testing.T) {
		dir := t.TempDir()
		seg := times("x", 60)
		path := filepath.Join(dir, seg, seg, seg, seg, seg)
		must(t, os.MkdirAll(path, 0o755))
		must(t, os.WriteFile(filepath.Join(path, "f"), nil, 0o600))

		assertTreeCode(t, dir, CodeTreeLimit)
	})

	t.Run("one large file", func(t *testing.T) {
		dir := t.TempDir()
		big := filepath.Join(dir, "big")
		must(t, os.WriteFile(big, nil, 0o600))
		must(t, os.Truncate(big, MaxTreeBytes+1))

		assertTreeCode(t, dir, CodeTreeLimit)
	})

	t.Run("total size", func(t *testing.T) {
		dir := t.TempDir()

		for _, name := range []string{"a", "b"} {
			path := filepath.Join(dir, name)
			must(t, os.WriteFile(path, nil, 0o600))
			must(t, os.Truncate(path, MaxTreeBytes/2+1))
		}

		assertTreeCode(t, dir, CodeTreeLimit)
	})
}

// TestHashTreeChangesWhileRead checks that a file that grows between the walk and the read is not
// hashed as if nothing happened.
//
// Parameters:
//   - t: test to report to
func TestHashTreeChangesWhileRead(t *testing.T) {
	dir := writeFiles(t, map[string]string{"plugin.toml": "short"})

	sc, terr := scanTree(dir)
	if terr != nil {
		t.Fatal(terr)
	}

	must(t, os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte("much longer now"), 0o644))

	if _, terr := sc.hash(dir, nil); terr == nil || terr.Code != CodeTreeIO {
		t.Errorf("hash of a changed file: %v, want %s", terr, CodeTreeIO)
	}
}

// TestHashTreeKeepsTheHashedBytes checks that the manifest and the locale files of a tree are
// returned with the hash, within their size limit, and that other files are not kept.
//
// Parameters:
//   - t: test to report to
func TestHashTreeKeepsTheHashedBytes(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"plugin.toml": "m", "doc": "d",
		"locales/en.toml": "e",
	})

	sc, _ := scanTree(dir)

	tree, terr := sc.hash(dir, keepPackageText)
	if terr != nil {
		t.Fatal(terr)
	}

	if string(tree.data["plugin.toml"]) != "m" || string(tree.data["locales/en.toml"]) != "e" ||
		tree.data["doc"] != nil {
		t.Errorf("kept = %v", tree.data)
	}
}

// TestTreeProblems checks the owner and the mode rules of an installed tree: 0500 directories,
// 0400 files and 0500 programs, every entry owned by the user, no special bit.
//
// Parameters:
//   - t: test to report to
func TestTreeProblems(t *testing.T) {
	uid := uint32(os.Getuid())

	tests := []struct {
		name   string
		change func(dir string) error
		uid    uint32
		want   []string
	}{
		{"clean", func(string) error { return nil }, uid, nil},
		{
			"file writable", func(d string) error { return os.Chmod(d+"/plugin.toml", 0o600) }, uid,
			[]string{CodeTreePerms},
		},
		{
			"file world readable", func(d string) error {
				return os.Chmod(d+"/plugin.toml",
					0o444)
			},
			uid,
			[]string{CodeTreePerms},
		},
		{
			"program 0700", func(d string) error { return os.Chmod(d+"/bin/run", 0o700) }, uid,
			[]string{CodeTreePerms},
		},
		{
			"program 0755", func(d string) error { return os.Chmod(d+"/bin/run", 0o755) }, uid,
			[]string{CodeTreePerms},
		},
		{
			"directory 0700", func(d string) error { return os.Chmod(d+"/bin", 0o700) }, uid,
			[]string{CodeTreePerms},
		},
		{
			"root writable", func(d string) error { return os.Chmod(d, 0o700) }, uid,
			[]string{CodeTreePerms},
		},
		{
			"setgid directory", func(d string) error {
				return os.Chmod(d+"/bin",
					0o500|os.ModeSetgid)
			},
			uid,
			[]string{CodeTreePerms},
		},
		{
			"sticky file", func(d string) error {
				return os.Chmod(d+"/plugin.toml",
					0o400|os.ModeSticky)
			},
			uid,
			[]string{CodeTreePerms},
		},
		{
			"other owner", func(string) error { return nil }, uid + 1,
			[]string{CodeTreeOwner},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			dir := filepath.Join(l.root, "tree")
			l.writeTree(dir, map[string]string{"plugin.toml": "m", "bin/run*": "p"})

			if err := tt.change(dir); err != nil {
				t.Fatal(err)
			}

			sc, terr := scanTree(dir)
			if terr != nil {
				t.Fatal(terr)
			}

			var got []string
			for _, p := range sc.problems(tt.uid) {
				got = append(got, p.Code)
			}

			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("problems = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTreeErrorText checks that the text of a tree error quotes the path, so that a hostile name
// cannot add a line to a log.
//
// Parameters:
//   - t: test to report to
func TestTreeErrorText(t *testing.T) {
	e := &TreeError{Code: CodeTreeName, Path: "a\x1b[2Jb\nc", Err: errors.New("bad")}

	got := e.Error()
	if strings.ContainsAny(got, "\x1b\n") {
		t.Errorf("Error() = %q holds a raw control character", got)
	}

	if !errors.Is(e, e.Err) {
		t.Error("Unwrap does not return the cause")
	}
}
