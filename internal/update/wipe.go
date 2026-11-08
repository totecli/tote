package update

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// Limits of wipeEntry: how deep and how large a tree it agrees to delete. A tree that goes past
// them is hostile or broken, and the entry is reported instead of being chased.
const (
	maxWipeDepth   = 128
	maxWipeEntries = 1 << 21
)

// errWipeSwapped is returned when a directory is not the one that was looked at a moment ago.
var errWipeSwapped = errors.New("directory replaced while it was removed")

// errWipeLimit is returned when a tree is deeper or larger than wipeEntry accepts.
var errWipeLimit = errors.New("tree too deep or too large to remove")

// wiper removes entries below one directory, counting them to stop a tree that never ends.
//
// errs holds one error for each entry that could not be removed, which are reported together.
type wiper struct {
	count int
	errs  []error
}

// wipeEntry removes one entry of a directory the way the installed plugin trees need.
//
// An installed plugin tree is read-only (directories 0500, files 0400), so a plain os.RemoveAll
// fails on it. The walk never follows a symbolic link and never leaves the tree:
//
//  1. The name is looked at with Lstat of the root. A missing name is done. Anything that is not
//     a directory, a symbolic link included, is unlinked as the entry it is.
//  2. A directory is made writable (mode 0700) through the root, opened as a sub-root and compared
//     with the Lstat result (os.SameFile), so that a link swapped in between is refused. From then
//     on every operation goes through the sub-root, which cannot leave the directory.
//  3. The names are read, then each one is removed the same way. The walk goes on after an entry
//     that fails, so that one stuck file does not keep the rest of the data in place; a directory
//     that still holds a failed entry is kept.
//
// Parameters:
//   - parent: the directory that holds the entry, opened as a root
//   - name: a single path element, never "." or ".."
//   - shown: the full path of the entry, quoted in the errors
//
// Returns nil when the entry is gone, or the errors of everything that could not be removed.
func wipeEntry(parent *os.Root, name, shown string) error {
	w := &wiper{}
	w.entry(parent, name, shown, 0)

	return errors.Join(w.errs...)
}

// entry removes one name of a directory and records the failure of any entry below it.
//
// Parameters:
//   - root: the directory
//   - name: a single path element
//   - shown: the path to quote in an error
//   - depth: number of directories above root
//
// Returns true when the entry is gone.
func (w *wiper) entry(root *os.Root, name, shown string, depth int) bool {
	w.count++
	if w.count > maxWipeEntries {
		return w.fail(shown, errWipeLimit)
	}

	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}

	if err != nil {
		return w.fail(shown, err)
	}

	if !info.IsDir() {
		return w.removed(root, name, shown)
	}

	if depth > maxWipeDepth {
		return w.fail(shown, errWipeLimit)
	}

	// A directory that cannot be made writable is reported by the first removal that fails.
	_ = root.Chmod(name, 0o700)

	sub, err := root.OpenRoot(name)
	if err != nil {
		return w.fail(shown, err)
	}

	opened, err := sub.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = sub.Close()

		return w.fail(shown, errWipeSwapped)
	}

	clean := w.empty(sub, shown, depth+1)
	_ = sub.Close()

	if !clean {
		return false
	}

	return w.removed(root, name, shown)
}

// empty removes everything a directory holds.
//
// Parameters:
//   - sub: the directory, opened as a root
//   - shown: the path of the directory to quote in an error
//   - depth: number of directories above sub
//
// Returns true when the directory is empty afterwards.
func (w *wiper) empty(sub *os.Root, shown string, depth int) bool {
	f, err := sub.Open(".")
	if err != nil {
		return w.fail(shown, err)
	}

	names, err := f.Readdirnames(-1)
	_ = f.Close()

	if err != nil && !errors.Is(err, io.EOF) {
		return w.fail(shown, err)
	}

	clean := true

	for _, n := range names {
		if !w.entry(sub, n, shown+string(os.PathSeparator)+n, depth) {
			clean = false
		}
	}

	return clean
}

// removed unlinks a name that is a file, a link or an empty directory.
//
// Parameters:
//   - root: the directory
//   - name: the name to remove
//   - shown: the path to quote in an error
//
// Returns true on success.
func (w *wiper) removed(root *os.Root, name, shown string) bool {
	err := root.Remove(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return w.fail(shown, err)
	}

	return true
}

// fail records the failure of one entry.
//
// Parameters:
//   - shown: the path to quote
//   - err: what went wrong
//
// Returns false, so that a caller can return it.
func (w *wiper) fail(shown string, err error) bool {
	w.errs = append(w.errs, fmt.Errorf("%s: %w", shown, unwrapPath(err)))

	return false
}
