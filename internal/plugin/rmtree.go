package plugin

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

// Limits of removeTree: how deep and how large a tree it agrees to delete. A tree that goes past
// them is hostile or broken, and the entry is left for the next cleanup instead of being chased.
const (
	maxWipeDepth   = 128
	maxWipeEntries = 1 << 21
	wipeBatch      = 256
)

// errWipeSwapped is returned when a directory is not the one that was looked at a moment ago.
var errWipeSwapped = errors.New("directory replaced while it was removed")

// errWipeLimit is returned when a tree is deeper or larger than removeTree accepts.
var errWipeLimit = errors.New("tree too deep or too large to remove")

// removeTree deletes a file, a link or a directory tree that belongs to the host, and nothing
// else. It is the only recursive delete of this package.
//
// The algorithm never follows a symbolic link and never leaves the tree:
//
//  1. The path is looked at with Lstat. A missing path is done. Anything that is not a directory,
//     a symbolic link included, is unlinked as the entry it is: the target of a link is not
//     touched.
//  2. A directory is opened as an os.Root and compared with the Lstat result (os.SameFile), so
//     that a link swapped in between the two calls is refused. From then on every operation goes
//     through the root, which cannot leave the directory.
//  3. Each directory is made writable (mode 0700, since an installed tree is read-only), read in
//     batches of names and emptied entry by entry. Every name is looked at with Lstat of the
//     root: a directory is opened as a sub-root, compared again with SameFile, emptied, and
//     removed; every other entry (file, symbolic link, FIFO, socket, device) is unlinked, never
//     opened, so a hard link only loses one of its names and a link to the outside loses itself.
//  4. Depth is bounded by maxWipeDepth and the number of entries by maxWipeEntries, which stops a
//     tree that a running program keeps refilling.
//
// The first failure stops the walk and is returned: what is left stays for the next cleanup. The
// path is the caller's responsibility: it must be a direct child of a directory the host trusts,
// checked before.
//
// Parameters:
//   - path: file, link or directory to remove
func removeTree(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !info.IsDir() {
		return os.Remove(path)
	}

	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}

	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = root.Close()

		return errWipeSwapped
	}

	w := &wiper{}
	werr := w.wipe(root, 0)
	cerr := root.Close()

	if werr != nil {
		return werr
	}

	if cerr != nil {
		return cerr
	}

	return os.Remove(path)
}

// wiper counts what removeTree has deleted, to stop a tree that never ends.
type wiper struct {
	count int
}

// wipe empties a directory.
//
// Parameters:
//   - root: the directory, opened as a root
//   - depth: number of directories above it
func (w *wiper) wipe(root *os.Root, depth int) error {
	if depth > maxWipeDepth {
		return errWipeLimit
	}

	// A directory that cannot be made writable is reported by the first removal that fails.
	_ = root.Chmod(".", 0o700)

	for {
		names, err := readBatch(root)
		if err != nil {
			return err
		}

		if len(names) == 0 {
			return nil
		}

		for _, name := range names {
			if err := w.entry(root, name, depth); err != nil {
				return err
			}
		}
	}
}

// readBatch reads the next names of a directory, at most wipeBatch of them.
//
// Parameters:
//   - root: the directory, opened as a root
func readBatch(root *os.Root) ([]string, error) {
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	names, err := f.Readdirnames(wipeBatch)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	return names, nil
}

// entry removes one name of a directory.
//
// Parameters:
//   - root: the directory
//   - name: a single path element, never "." or ".."
//   - depth: number of directories above root
func (w *wiper) entry(root *os.Root, name string, depth int) error {
	w.count++
	if w.count > maxWipeEntries {
		return errWipeLimit
	}

	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !info.IsDir() {
		return root.Remove(name)
	}

	// A directory the owner closed (a plugin runs as the user and can do it in its own state) is
	// opened up first, so that it can be read. The root confines the change, and the check of the
	// same file below still catches a swap.
	_ = root.Chmod(name, 0o700)

	sub, err := root.OpenRoot(name)
	if err != nil {
		return err
	}

	opened, err := sub.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = sub.Close()

		return errWipeSwapped
	}

	werr := w.wipe(sub, depth+1)
	_ = sub.Close()

	if werr != nil {
		return werr
	}

	return root.Remove(name)
}
