package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// outside builds a directory that holds what removeTree must never touch: a read-only directory
// with a file in it, and a file on its own. It returns their paths and a function that checks that
// both are intact.
//
// Parameters:
//   - t: test to report to
//   - root: sandbox directory
func outside(t *testing.T, root string) (dir, file string, intact func()) {
	t.Helper()

	dir = filepath.Join(root, "outside", "dir")
	file = filepath.Join(root, "outside", "file")

	must(t, os.MkdirAll(dir, 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "inner"), []byte("inner"), 0o600))
	must(t, os.WriteFile(file, []byte("file"), 0o640))
	must(t, os.Chmod(dir, 0o510))

	t.Cleanup(func() { restoreWrite(root) })

	return dir, file, func() {
		t.Helper()

		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0o510 {
			t.Errorf("the outside directory changed: %v %v", info, err)
		}

		if data, err := os.ReadFile(filepath.Join(dir, "inner")); err != nil ||
			string(data) != "inner" {
			t.Errorf("the file in the outside directory changed: %q %v", data, err)
		}

		finfo, err := os.Stat(file)
		if err != nil || finfo.Mode().Perm() != 0o640 {
			t.Errorf("the outside file changed: %v %v", finfo, err)
		}

		if data, _ := os.ReadFile(file); string(data) != "file" {
			t.Errorf("the outside file holds %q", data)
		}
	}
}

// gone reports whether nothing is left at a path, links included.
//
// Parameters:
//   - path: location
func gone(path string) bool {
	_, err := os.Lstat(path)

	return errors.Is(err, os.ErrNotExist)
}

// TestRemoveTreeHostileLayouts deletes trees that were built to make a recursive delete leave its
// tree, and checks both that the tree is gone and that what lies outside is exactly as it was.
//
// Parameters:
//   - t: test to report to
func TestRemoveTreeHostileLayouts(t *testing.T) {
	layouts := map[string]func(t *testing.T, tree, dir, file string){
		"link to an outside directory": func(t *testing.T, tree, dir, _ string) {
			must(t, os.Symlink(dir, filepath.Join(tree, "d")))
		},
		"link to an outside file": func(t *testing.T, tree, _, file string) {
			must(t, os.Symlink(file, filepath.Join(tree, "f")))
		},
		"link to the parent": func(t *testing.T, tree, _, _ string) {
			must(t, os.Symlink("..", filepath.Join(tree, "up")))
		},
		"link to itself": func(t *testing.T, tree, _, _ string) {
			must(t, os.Symlink(".", filepath.Join(tree, "self")))
		},
		"link loop": func(t *testing.T, tree, _, _ string) {
			must(t, os.Symlink("b", filepath.Join(tree, "a")))
			must(t, os.Symlink("a", filepath.Join(tree, "b")))
		},
		"link to a missing path": func(t *testing.T, tree, _, _ string) {
			must(t, os.Symlink("/nonexistent/x", filepath.Join(tree, "dangling")))
		},
		"link to the root": func(t *testing.T, tree, _, _ string) {
			must(t, os.Symlink("/", filepath.Join(tree, "root")))
		},
		"deep link": func(t *testing.T, tree, dir, _ string) {
			deep := filepath.Join(tree, "a", "b", "c")
			must(t, os.MkdirAll(deep, 0o700))
			must(t, os.Symlink(dir, filepath.Join(deep, "out")))
		},
		"hard link": func(t *testing.T, tree, _, file string) {
			must(t, os.Link(file, filepath.Join(tree, "hard")))
		},
		"fifo": func(t *testing.T, tree, _, _ string) {
			must(t, syscall.Mkfifo(filepath.Join(tree, "pipe"), 0o600))
		},
		"read-only directories": func(t *testing.T, tree, _, _ string) {
			sub := filepath.Join(tree, "ro", "deeper")
			must(t, os.MkdirAll(sub, 0o700))
			must(t, os.WriteFile(filepath.Join(sub, "f"), []byte("x"), 0o400))
			must(t, os.Chmod(sub, 0o500))
			must(t, os.Chmod(filepath.Dir(sub), 0o500))
		},
		"awkward names": func(t *testing.T, tree, _, _ string) {
			for _, n := range []string{"-rf", "...", "a b", "line\nbreak", "\x1b[31m", "*", ".x"} {
				must(t, os.WriteFile(filepath.Join(tree, n), []byte("x"), 0o600))
			}
		},
		"many entries": func(t *testing.T, tree, _, _ string) {
			for i := range 3 * wipeBatch {
				must(t, os.WriteFile(filepath.Join(tree, fmt.Sprintf("f%04d", i)), []byte("x"),
					0o600))
			}
		},
		"empty": func(t *testing.T, tree, _, _ string) {},
	}

	for name, build := range layouts {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir, file, intact := outside(t, root)

			tree := filepath.Join(root, "work", "tree")
			must(t, os.MkdirAll(tree, 0o700))
			build(t, tree, dir, file)

			must(t, removeTree(tree))

			if !gone(tree) {
				t.Error("the tree is still there")
			}

			if _, err := os.Stat(filepath.Dir(tree)); err != nil {
				t.Errorf("the parent went: %v", err)
			}

			intact()
		})
	}
}

// TestRemoveTreeTop checks the entry itself: a missing path is done, a file and a link are
// unlinked as what they are, and the target of a link stays whatever it is.
//
// Parameters:
//   - t: test to report to
func TestRemoveTreeTop(t *testing.T) {
	root := t.TempDir()
	dir, file, intact := outside(t, root)
	work := filepath.Join(root, "work")

	must(t, os.MkdirAll(work, 0o700))

	t.Run("missing", func(t *testing.T) {
		must(t, removeTree(filepath.Join(work, "missing")))
	})

	t.Run("file", func(t *testing.T) {
		path := filepath.Join(work, "file")
		must(t, os.WriteFile(path, []byte("x"), 0o600))
		must(t, removeTree(path))

		if !gone(path) {
			t.Error("the file is still there")
		}
	})

	t.Run("link to a directory", func(t *testing.T) {
		path := filepath.Join(work, "link-dir")
		must(t, os.Symlink(dir, path))
		must(t, removeTree(path))

		if !gone(path) {
			t.Error("the link is still there")
		}
	})

	t.Run("link to a file", func(t *testing.T) {
		path := filepath.Join(work, "link-file")
		must(t, os.Symlink(file, path))
		must(t, removeTree(path))

		if !gone(path) {
			t.Error("the link is still there")
		}
	})

	t.Run("link to a link", func(t *testing.T) {
		first, second := filepath.Join(work, "first"), filepath.Join(work, "second")
		must(t, os.Symlink(dir, first))
		must(t, os.Symlink(first, second))
		must(t, removeTree(second))

		if !gone(second) || gone(first) {
			t.Error("only the last link was to go")
		}
	})

	t.Run("dangling link", func(t *testing.T) {
		path := filepath.Join(work, "dangling")
		must(t, os.Symlink("/nonexistent/x", path))
		must(t, removeTree(path))

		if !gone(path) {
			t.Error("the link is still there")
		}
	})

	t.Run("through a linked parent", func(t *testing.T) {
		// The caller must pass a path whose parent is trusted. A parent that is a link is
		// resolved by the kernel like for any path; the entry below it is what is deleted.
		parent := filepath.Join(work, "parent")
		target := filepath.Join(root, "target")

		must(t, os.MkdirAll(filepath.Join(target, "entry"), 0o700))
		must(t, os.WriteFile(filepath.Join(target, "entry", "f"), []byte("x"), 0o600))
		must(t, os.WriteFile(filepath.Join(target, "sibling"), []byte("x"), 0o600))
		must(t, os.Symlink(target, parent))
		must(t, removeTree(filepath.Join(parent, "entry")))

		if !gone(filepath.Join(target, "entry")) || gone(filepath.Join(target, "sibling")) {
			t.Error("the wrong entries went")
		}
	})

	intact()
}

// TestRemoveTreeLimits checks that a tree deeper than the bound is refused and left in place,
// without leaving the tree, and that one just inside the bound goes.
//
// Parameters:
//   - t: test to report to
func TestRemoveTreeLimits(t *testing.T) {
	build := func(root string, depth int) string {
		parts := make([]string, depth)
		for i := range parts {
			parts[i] = "d"
		}

		tree := filepath.Join(root, "work", "tree")
		must(t, os.MkdirAll(filepath.Join(append([]string{tree}, parts...)...), 0o700))

		return tree
	}

	t.Run("inside the bound", func(t *testing.T) {
		root := t.TempDir()
		_, _, intact := outside(t, root)
		tree := build(root, maxWipeDepth-2)

		must(t, removeTree(tree))

		if !gone(tree) {
			t.Error("the tree is still there")
		}

		intact()
	})

	t.Run("past the bound", func(t *testing.T) {
		root := t.TempDir()
		_, _, intact := outside(t, root)
		tree := build(root, maxWipeDepth+20)

		if err := removeTree(tree); !errors.Is(err, errWipeLimit) {
			t.Fatalf("removeTree = %v", err)
		}

		if gone(tree) {
			t.Error("a tree that was refused is gone")
		}

		intact()
	})
}

// TestRemoveTreeClosedDirectory checks a directory without any permission bit, which a plugin can
// make in its own tree: the delete opens it up inside the root, removes it, and touches nothing
// outside.
//
// Parameters:
//   - t: test to report to
func TestRemoveTreeClosedDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root enters a directory of mode 000")
	}

	root := t.TempDir()
	_, _, intact := outside(t, root)

	tree := filepath.Join(root, "work", "tree")
	locked := filepath.Join(tree, "locked")

	must(t, os.MkdirAll(filepath.Join(locked, "deeper"), 0o700))
	must(t, os.WriteFile(filepath.Join(locked, "f"), []byte("x"), 0o600))
	must(t, os.WriteFile(filepath.Join(tree, "other"), []byte("x"), 0o600))
	must(t, os.Chmod(filepath.Join(locked, "deeper"), 0))
	must(t, os.Chmod(locked, 0))

	must(t, removeTree(tree))

	if !gone(tree) {
		t.Error("a closed directory stopped the delete")
	}

	intact()
}
