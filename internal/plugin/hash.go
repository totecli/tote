package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// Limits of an installed tree, from section 7.3 of the specification: they keep a hash cheap, so
// that every start can verify every plugin.
const (
	MaxTreeBytes   = 16 << 20
	MaxTreeEntries = 512
	MaxTreeDepth   = 6
	MaxTreePath    = 255
)

// Modes of an installed tree: the host makes it read-only at commit.
const (
	treeDirMode  = fs.FileMode(0o500)
	treeFileMode = fs.FileMode(0o400)
	treeExecMode = fs.FileMode(0o500)
)

// treeIDLen is the number of hexadecimal digits of tree_sha256 that name the tree directory.
const treeIDLen = 32

// Codes of the findings about a tree. They are the reasons of the broken state, see state.go.
const (
	CodeTreeMissing = "tree_missing"
	CodeTreeSymlink = "tree_symlink"
	CodeTreeSpecial = "tree_special"
	CodeTreeName    = "tree_name"
	CodeTreeLimit   = "tree_limit"
	CodeTreeOwner   = "tree_owner"
	CodeTreePerms   = "tree_perms"
	CodeTreeHash    = "tree_hash"
	CodeTreeIO      = "tree_io"
)

// segmentRe matches one path segment of a package: the grammar of the archive entries of 7.3.
var segmentRe = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]*$`)

// TreeError reports a tree that cannot be hashed or must not be trusted.
//
// Code is one of the CodeTree constants and Path the relative path of the entry concerned, empty
// for the tree itself. The text is for logs: the host builds the message from the code.
type TreeError struct {
	Code string
	Path string
	Err  error
}

// Error implements error.
func (e *TreeError) Error() string {
	msg := e.Code
	if e.Path != "" {
		msg += ": " + strconv.Quote(CleanLine(e.Path, issueArgRunes))
	}

	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}

	return msg
}

// Unwrap returns the underlying system error, if any.
func (e *TreeError) Unwrap() error { return e.Err }

// TreeFile is one regular file of a tree as the canonical listing holds it.
type TreeFile struct {
	Path   string
	Size   int64
	SHA256 string
	Exec   bool
}

// Tree is the verified content of a package directory: its tree_sha256 and its files in
// canonical order.
//
// Data keeps the bytes of the manifest and of the locale files that were hashed, so that they can
// be parsed from the very bytes the hash covers and not from a second read.
type Tree struct {
	SHA256 string
	Files  []TreeFile
	data   map[string][]byte
}

// ID returns the first 32 hexadecimal digits of the tree hash, the name of its directory.
func (t *Tree) ID() string { return TreeID(t.SHA256) }

// TreeID returns the directory name of a tree from its full hash.
//
// Parameters:
//   - sum: tree_sha256 in hexadecimal; a shorter text is returned whole
func TreeID(sum string) string {
	if len(sum) > treeIDLen {
		return sum[:treeIDLen]
	}

	return sum
}

// keepFunc tells which files of a tree keep their bytes, and the largest size that is kept.
type keepFunc func(rel string) (limit int, ok bool)

// treeEntry is one directory or regular file met by the walk, with the identity that
// Lstat gave.
type treeEntry struct {
	Rel string
	Dir bool
	ID  statID
}

// treeScan is the result of walking a tree without reading any file: every entry sorted by
// relative path, the root included first with the empty path.
type treeScan struct {
	entries []treeEntry
}

// HashTree walks a package directory and computes its tree_sha256.
//
// The hash is the sha256 of the canonical listing: for each regular file, in byte order of the
// slash separated relative path, the bytes path, NUL, decimal size, NUL, the sha256 of the content
// in lower case hexadecimal, NUL, "x" for a file with an owner execute bit or "-" otherwise, and a
// line feed. Directories are not listed, so an empty directory changes nothing, and neither do
// timestamps and owners. The exec bit is the only mode bit that counts, because it decides what
// can run. A file is opened without following links and must be the file the walk saw.
//
// Parameters:
//   - root: directory of the package, not a symbolic link
//
// Returns the tree, or a *TreeError: tree_missing, tree_symlink and tree_special for entries that
// must not exist, tree_name for a segment outside the grammar of the archive entries, tree_limit
// beyond 16 MiB, 512 entries, depth 6 or 255 bytes of path, tree_io when a file changes while it
// is read. No mode and no owner is required: see treeProblems for the installed tree.
func HashTree(root string) (*Tree, error) {
	sc, terr := scanTree(root)
	if terr != nil {
		return nil, terr
	}

	t, terr := sc.hash(root, nil)
	if terr != nil {
		return nil, terr
	}

	return t, nil
}

// scanTree walks a directory with Lstat only and checks what must hold before any byte is read.
//
// Parameters:
//   - root: directory of the package
//
// Returns the scan, or a *TreeError.
func scanTree(root string) (*treeScan, *TreeError) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, ioError(CodeTreeMissing, "", err)
	}

	id, ok := statIDOf(info)

	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, &TreeError{Code: CodeTreeSymlink}
	case !info.IsDir():
		return nil, &TreeError{Code: CodeTreeSpecial}
	case !ok:
		return nil, &TreeError{Code: CodeTreeIO, Err: errors.New("no file identity")}
	}

	sc := &treeScan{entries: []treeEntry{{Rel: "", Dir: true, ID: id}}}
	budget := MaxTreeBytes

	if terr := sc.walk(root, "", 0, &budget); terr != nil {
		return nil, terr
	}

	sort.Slice(sc.entries, func(i, j int) bool { return sc.entries[i].Rel < sc.entries[j].Rel })

	return sc, nil
}

// ioError wraps a system error in a TreeError, mapping a missing file to the given code and any
// other failure to tree_io.
//
// Parameters:
//   - code: code for a file that does not exist
//   - rel: relative path of the entry
//   - err: system error
func ioError(code, rel string, err error) *TreeError {
	if errors.Is(err, fs.ErrNotExist) {
		return &TreeError{Code: code, Path: rel, Err: err}
	}

	return &TreeError{Code: CodeTreeIO, Path: rel, Err: err}
}

// walk reads one directory and recurses.
//
// Parameters:
//   - root: directory of the package
//   - rel: relative path of the directory being read, empty for the root
//   - depth: number of segments of rel
//   - budget: bytes still allowed for the files of the tree; updated here
func (sc *treeScan) walk(root, rel string, depth int, budget *int) *TreeError {
	names, err := readNames(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ioError(CodeTreeMissing, rel, err)
	}

	for _, name := range names {
		path := name
		if rel != "" {
			path = rel + "/" + name
		}

		if terr := sc.visit(root, path, depth+1, budget); terr != nil {
			return terr
		}
	}

	return nil
}

// readNames lists the names in a directory, bounded so that a hostile directory cannot flood the
// memory before the entry limit is checked.
//
// Parameters:
//   - dir: directory to read
func readNames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	names, err := f.Readdirnames(MaxTreeEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	return names, nil
}

// visit checks one entry and adds it to the scan.
//
// Parameters:
//   - root: directory of the package
//   - rel: relative path of the entry
//   - depth: number of segments of rel
//   - budget: bytes still allowed for the files of the tree; updated here
func (sc *treeScan) visit(root, rel string, depth int, budget *int) *TreeError {
	if terr := checkName(rel, depth); terr != nil {
		return terr
	}

	if len(sc.entries) > MaxTreeEntries {
		return &TreeError{Code: CodeTreeLimit, Path: rel, Err: errors.New("entries")}
	}

	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ioError(CodeTreeMissing, rel, err)
	}

	id, ok := statIDOf(info)
	if !ok {
		return &TreeError{Code: CodeTreeIO, Path: rel, Err: errors.New("no file identity")}
	}

	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return &TreeError{Code: CodeTreeSymlink, Path: rel}
	case info.IsDir():
		sc.entries = append(sc.entries, treeEntry{Rel: rel, Dir: true, ID: id})

		return sc.walk(root, rel, depth, budget)
	case !info.Mode().IsRegular():
		return &TreeError{Code: CodeTreeSpecial, Path: rel}
	}

	*budget -= int(min(info.Size(), int64(MaxTreeBytes)+1))
	if info.Size() > MaxTreeBytes || *budget < 0 {
		return &TreeError{Code: CodeTreeLimit, Path: rel, Err: errors.New("bytes")}
	}

	sc.entries = append(sc.entries, treeEntry{Rel: rel, ID: id})

	return nil
}

// checkName checks the last segment of a relative path and the limits of its depth and length.
//
// Parameters:
//   - rel: relative path with slashes
//   - depth: number of segments of rel
func checkName(rel string, depth int) *TreeError {
	name := rel[strings.LastIndexByte(rel, '/')+1:]

	switch {
	case depth > MaxTreeDepth:
		return &TreeError{Code: CodeTreeLimit, Path: rel, Err: errors.New("depth")}
	case len(rel) > MaxTreePath:
		return &TreeError{Code: CodeTreeLimit, Path: rel, Err: errors.New("path")}
	case name == "." || name == ".." || !segmentRe.MatchString(name):
		return &TreeError{Code: CodeTreeName, Path: rel}
	}

	return nil
}

// sameAs reports whether two scans saw the same entries with the same identities.
//
// Parameters:
//   - other: scan to compare with
func (sc *treeScan) sameAs(other *treeScan) bool {
	if other == nil || len(sc.entries) != len(other.entries) {
		return false
	}

	for i := range sc.entries {
		if sc.entries[i] != other.entries[i] {
			return false
		}
	}

	return true
}

// hash reads the files of a scan and computes the canonical hash.
//
// Parameters:
//   - root: directory of the package
//   - keep: tells whether the bytes of a file are kept in the tree, and their size limit; nil
//     keeps none
func (sc *treeScan) hash(root string, keep keepFunc) (*Tree, *TreeError) {
	t := &Tree{}
	sum := sha256.New()

	for _, e := range sc.entries {
		if e.Dir {
			continue
		}

		tf, content, terr := hashFile(root, e, keep)
		if terr != nil {
			return nil, terr
		}

		if content != nil {
			if t.data == nil {
				t.data = map[string][]byte{}
			}

			t.data[e.Rel] = content
		}

		t.Files = append(t.Files, tf)
		sum.Write([]byte(tf.Path + "\x00" + strconv.FormatInt(tf.Size, 10) + "\x00" +
			tf.SHA256 + "\x00" + execFlag(tf.Exec) + "\n"))
	}

	t.SHA256 = hex.EncodeToString(sum.Sum(nil))

	return t, nil
}

// execFlag is the last field of a line of the canonical listing.
//
// Parameters:
//   - exec: true for a file with an owner execute bit
func execFlag(exec bool) string {
	if exec {
		return "x"
	}

	return "-"
}

// hashFile hashes one file through a descriptor opened without following links, and checks that
// it is the file the walk saw.
//
// Parameters:
//   - root: directory of the package
//   - e: entry from the walk
//   - keep: tells whether the bytes of a file are kept and their size limit
//
// Returns the listing entry, the bytes when kept, or a *TreeError.
func hashFile(root string, e treeEntry, keep keepFunc) (TreeFile, []byte, *TreeError) {
	path := filepath.Join(root, filepath.FromSlash(e.Rel))

	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return TreeFile{}, nil, &TreeError{Code: CodeTreeSymlink, Path: e.Rel}
		}

		return TreeFile{}, nil, ioError(CodeTreeMissing, e.Rel, err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return TreeFile{}, nil, ioError(CodeTreeMissing, e.Rel, err)
	}

	id, ok := statIDOf(info)
	if !ok || !info.Mode().IsRegular() || id.Dev != e.ID.Dev || id.Ino != e.ID.Ino ||
		id.Size != e.ID.Size {
		return TreeFile{}, nil, &TreeError{
			Code: CodeTreeIO, Path: e.Rel,
			Err: errors.New("file changed during the walk"),
		}
	}

	h := sha256.New()

	var content *bytes.Buffer

	src := io.Reader(io.LimitReader(f, e.ID.Size+1))

	if keep != nil {
		if limit, kept := keep(e.Rel); kept && e.ID.Size <= int64(limit) {
			content = &bytes.Buffer{}
			src = io.TeeReader(src, content)
		}
	}

	n, err := io.Copy(h, src)
	if err != nil {
		return TreeFile{}, nil, ioError(CodeTreeMissing, e.Rel, err)
	}

	if n != e.ID.Size {
		return TreeFile{}, nil, &TreeError{
			Code: CodeTreeIO, Path: e.Rel,
			Err: errors.New("file changed while it was read"),
		}
	}

	tf := TreeFile{
		Path: e.Rel, Size: n, SHA256: hex.EncodeToString(h.Sum(nil)),
		Exec: e.ID.Mode.Perm()&0o100 != 0,
	}

	if content != nil {
		return tf, content.Bytes(), nil
	}

	return tf, nil, nil
}

// TreeProblem is one finding about the owner or the mode of an installed tree.
type TreeProblem struct {
	Code string
	Path string
}

// problems checks the owner and the mode of every entry of an installed tree.
//
// Every entry must belong to the user. A directory is 0500, a regular file 0400 or, when it is a
// program, 0500; any other bit, setuid, setgid and sticky included, is wrong. Only the first
// entry of each kind of problem is reported, so a tree rewritten with the wrong mode costs one
// reason.
//
// Parameters:
//   - uid: user id the tree must belong to
func (sc *treeScan) problems(uid uint32) []TreeProblem {
	var out []TreeProblem

	seen := map[string]bool{}
	add := func(code, rel string) {
		if !seen[code] {
			seen[code] = true
			out = append(out, TreeProblem{code, rel})
		}
	}

	for _, e := range sc.entries {
		if e.ID.UID != uid {
			add(CodeTreeOwner, e.Rel)
		}

		perm := e.ID.Mode.Perm()
		special := e.ID.Mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0

		ok := perm == treeFileMode || perm == treeExecMode
		if e.Dir {
			ok = perm == treeDirMode
		}

		if !ok || special {
			add(CodeTreePerms, e.Rel)
		}
	}

	return out
}
