package plugin

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// Reasons of a source_unsafe error and names of the limits of a source_limits error.
const (
	UnsafeSymlink = "symlink"
	UnsafeSpecial = "special"
	UnsafeName    = "name"
	UnsafeCase    = "case"
	UnsafeChanged = "changed"
	LimitEntries  = "entries"
	LimitDepth    = "depth"
	LimitPath     = "path"
	LimitBytes    = "bytes"
)

// srcEntry is one entry met in a source directory.
//
// Kind holds the type bits of the mode, as an Lstat reports them: a symbolic link is never
// followed, so it shows as such. Info is kept to compare with the file that is opened later.
type srcEntry struct {
	Name string
	Kind fs.FileMode
	Size int64
	Info fs.FileInfo
}

// srcTree is a tree of files that can be copied into a staging directory: a directory on disk
// read through os.Root, or the embedded files of a bundled plugin.
type srcTree interface {
	list(rel string) ([]srcEntry, error)
	copyFile(rel string, e srcEntry, dst io.Writer) (int64, error)
}

// dirSource reads a directory of the user without ever leaving it: os.Root rejects a path that
// escapes, and every entry is looked at with Lstat before it is opened.
type dirSource struct {
	root *os.Root
}

// dotIfEmpty turns the root of a tree into the name os.Root expects for the directory itself.
//
// Parameters:
//   - rel: slash separated relative path, empty for the root
func dotIfEmpty(rel string) string {
	if rel == "" {
		return "."
	}

	return rel
}

// list reads one directory, at most MaxTreeEntries+1 names, and looks at every entry.
//
// Parameters:
//   - rel: slash separated relative path of the directory, empty for the root
func (d dirSource) list(rel string) ([]srcEntry, error) {
	f, err := d.root.Open(dotIfEmpty(rel))
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	names, err := f.Readdirnames(MaxTreeEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	out := make([]srcEntry, 0, len(names))

	for _, name := range names {
		path := name
		if rel != "" {
			path = rel + "/" + name
		}

		info, err := d.root.Lstat(path)
		if err != nil {
			return nil, err
		}

		out = append(out, srcEntry{
			Name: name, Kind: info.Mode().Type(), Size: info.Size(),
			Info: info,
		})
	}

	return out, nil
}

// copyFile copies a regular file. The file is opened without blocking, so that a FIFO put in the
// place of the file cannot stall the install, and it must be the file that list saw.
//
// Parameters:
//   - rel: slash separated relative path of the file
//   - e: entry from list
//   - dst: destination of the bytes
func (d dirSource) copyFile(rel string, e srcEntry, dst io.Writer) (int64, error) {
	f, err := d.root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, err
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return 0, err
	}

	if !info.Mode().IsRegular() || !os.SameFile(info, e.Info) {
		return 0, errChanged
	}

	return io.Copy(dst, io.LimitReader(f, e.Size+1))
}

// errChanged tells that a file is not the one that was listed, or changed while it was copied.
var errChanged = errors.New("source changed while it was copied")

// fsSource reads the embedded files of a bundled plugin.
type fsSource struct {
	fsys fs.FS
}

// list reads one directory of the embedded files.
//
// Parameters:
//   - rel: slash separated relative path of the directory, empty for the root
func (s fsSource) list(rel string) ([]srcEntry, error) {
	list, err := fs.ReadDir(s.fsys, dotIfEmpty(rel))
	if err != nil {
		return nil, err
	}

	if len(list) > MaxTreeEntries+1 {
		list = list[:MaxTreeEntries+1]
	}

	out := make([]srcEntry, 0, len(list))

	for _, de := range list {
		info, err := de.Info()
		if err != nil {
			return nil, err
		}

		out = append(out, srcEntry{
			Name: de.Name(), Kind: de.Type(), Size: info.Size(),
			Info: info,
		})
	}

	return out, nil
}

// copyFile copies an embedded file.
//
// Parameters:
//   - rel: slash separated relative path of the file
//   - e: entry from list
//   - dst: destination of the bytes
func (s fsSource) copyFile(rel string, e srcEntry, dst io.Writer) (int64, error) {
	f, err := s.fsys.Open(rel)
	if err != nil {
		return 0, err
	}

	defer func() { _ = f.Close() }()

	return io.Copy(dst, io.LimitReader(f, e.Size+1))
}

// faultWriter remembers the error of the destination, so that a failure to write is told apart
// from a failure to read the source when a copy stops.
type faultWriter struct {
	w   io.Writer
	err error
}

// Write writes to the destination and keeps its first error.
//
// Parameters:
//   - p: bytes to write
func (f *faultWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err != nil && f.err == nil {
		f.err = err
	}

	return n, err
}

// ioReason gives the reason of a file system error without the operation and the path, which the
// message already names.
//
// Parameters:
//   - err: error of an open, a read or a listing
func ioReason(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return errText(pe.Err)
	}

	return errText(err)
}

// copier copies a source into a staging directory under the rules of section 7.3: only regular
// files and directories, names inside the grammar of package segments, no two names that differ
// only in case, and the limits of an installed tree. The first violation aborts and names the
// entry.
type copier struct {
	ctx     context.Context
	src     srcTree
	dst     *os.Root
	entries int
	bytes   int64
}

// run copies the whole source.
func (c *copier) run() error { return c.walk("", 0) }

// walk copies one directory and what is below it.
//
// Parameters:
//   - rel: slash separated relative path of the directory, empty for the root
//   - depth: number of segments of rel
func (c *copier) walk(rel string, depth int) error {
	list, err := c.src.list(rel)
	if err != nil {
		// Only a directory that went away or became a file is a source that changed: one that
		// cannot be opened or read is a read failure, and the author must hear which one.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errChanged) ||
			errors.Is(err, syscall.ENOTDIR) {
			return wrapError(CodeSourceUnsafe, err, rel, UnsafeChanged)
		}

		return wrapError(CodeSourceRead, err, dotIfEmpty(rel), ioReason(err))
	}

	folded := map[string]bool{}

	for _, e := range list {
		path := e.Name
		if rel != "" {
			path = rel + "/" + e.Name
		}

		if err := c.ctx.Err(); err != nil {
			return err
		}

		if err := c.check(path, e, depth+1, folded); err != nil {
			return err
		}

		if err := c.put(path, e, depth+1); err != nil {
			return err
		}
	}

	return nil
}

// check applies the rules to one entry before anything is created for it.
//
// Parameters:
//   - path: slash separated relative path of the entry
//   - e: the entry
//   - depth: number of segments of path
//   - folded: lower case names already met in this directory
func (c *copier) check(path string, e srcEntry, depth int, folded map[string]bool) error {
	c.entries++

	switch {
	case depth > MaxTreeDepth:
		return newError(CodeSourceLimits, path, LimitDepth)
	case len(path) > MaxTreePath:
		return newError(CodeSourceLimits, path, LimitPath)
	case c.entries > MaxTreeEntries:
		return newError(CodeSourceLimits, path, LimitEntries)
	case e.Name == "." || e.Name == ".." || !segmentRe.MatchString(e.Name):
		return newError(CodeSourceUnsafe, path, UnsafeName)
	}

	key := strings.ToLower(e.Name)
	if folded[key] {
		return newError(CodeSourceUnsafe, path, UnsafeCase)
	}

	folded[key] = true

	switch {
	case e.Kind&fs.ModeSymlink != 0:
		return newError(CodeSourceUnsafe, path, UnsafeSymlink)
	case e.Kind != 0 && e.Kind != fs.ModeDir:
		return newError(CodeSourceUnsafe, path, UnsafeSpecial)
	}

	return nil
}

// put creates the entry in the staging directory: a directory with mode 0700 or a file with mode
// 0600, created with O_EXCL. The modes are final only for the files that are not programs: the
// program bits are set once the manifest says which files are programs.
//
// Parameters:
//   - path: slash separated relative path of the entry
//   - e: the entry, already checked
//   - depth: number of segments of path
func (c *copier) put(path string, e srcEntry, depth int) error {
	if e.Kind == fs.ModeDir {
		if err := c.dst.Mkdir(path, 0o700); err != nil {
			return wrapError(CodeDisk, err, path)
		}

		return c.walk(path, depth)
	}

	if e.Size > MaxTreeBytes || c.bytes+e.Size > MaxTreeBytes {
		return newError(CodeSourceLimits, path, LimitBytes)
	}

	out, err := c.dst.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return wrapError(CodeDisk, err, path)
	}

	w := &faultWriter{w: out}
	n, err := c.src.copyFile(path, e, w)

	cerr := out.Close()

	switch {
	case w.err != nil:
		return wrapError(CodeDisk, w.err, path)
	case errors.Is(err, errChanged) || (err == nil && n != e.Size):
		return newError(CodeSourceUnsafe, path, UnsafeChanged)
	case err != nil:
		return wrapError(CodeSourceRead, err, path, ioReason(err))
	case cerr != nil:
		return wrapError(CodeDisk, cerr, path)
	}

	c.bytes += n

	return nil
}
