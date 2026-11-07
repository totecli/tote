package plugin

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
)

// Codes of an archive that cannot be extracted. They are integrity failures: the first argument
// is the entry, the second the reason.
const (
	CodeArchiveFormat = "archive_format"
	CodeArchiveUnsafe = "archive_unsafe"
	CodeArchiveLimits = "archive_limits"
)

// More reasons of an archive_unsafe error, beside the UnsafeSymlink, UnsafeSpecial, UnsafeName
// and UnsafeCase of a directory source. Reasons of an archive_format error are ArchiveMagic,
// ArchiveTruncated and ArchiveCorrupt, and ArchiveTrailing for data after the archive.
const (
	UnsafeLink       = "link"
	UnsafeAbsolute   = "absolute"
	UnsafeParent     = "parent"
	UnsafeDuplicate  = "duplicate"
	UnsafeConflict   = "conflict"
	UnsafePAX        = "pax"
	ArchiveMagic     = "magic"
	ArchiveCorrupt   = "corrupt"
	ArchiveTrailing  = "trailing"
	ArchiveTruncated = "truncated"
)

// streamSlack is what a tar stream may hold beyond the file contents: headers, padding and the
// end of the archive.
const streamSlack = 4 << 20

// errStreamCap is returned by the counting reader when the decompressed stream is too long.
var errStreamCap = errors.New("decompressed stream too long")

// capReader reads at most left bytes and then fails, which stops a decompression bomb whatever
// the archive says about itself.
type capReader struct {
	r    io.Reader
	left int64
}

// Read implements io.Reader.
//
// Parameters:
//   - p: buffer to fill
func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errStreamCap
	}

	if int64(len(p)) > c.left {
		p = p[:c.left]
	}

	n, err := c.r.Read(p)
	c.left -= int64(n)

	return n, err
}

// diskError marks a failure to write to the staging directory, which is not the archive's fault.
type diskError struct {
	err error
}

// Error implements error.
func (d diskError) Error() string { return d.err.Error() }

// Unwrap returns the system error.
func (d diskError) Unwrap() error { return d.err }

// diskWriter tags the errors of a write.
type diskWriter struct {
	w io.Writer
}

// Write implements io.Writer.
//
// Parameters:
//   - p: bytes to write
func (d diskWriter) Write(p []byte) (int, error) {
	n, err := d.w.Write(p)
	if err != nil {
		return n, diskError{err}
	}

	return n, nil
}

// unpacker extracts gzip compressed tar archives into a staging directory under the rules of
// section 7.3. It writes through an os.Root, so that a mistake in its own checks still cannot
// leave the directory, and it keeps its names and counters between archives, so that the program
// of a platform lands in the same tree as the package and a path in both is an error.
type unpacker struct {
	ctx      context.Context
	dst      *os.Root
	kinds    map[string]fs.FileMode
	explicit map[string]bool
	folded   map[string]string
	entries  int
	bytes    int64
}

// newUnpacker creates an extractor for a staging directory.
//
// Parameters:
//   - ctx: cancels the extraction
//   - dst: the empty staging directory, opened as a root
func newUnpacker(ctx context.Context, dst *os.Root) *unpacker {
	return &unpacker{
		ctx: ctx, dst: dst, kinds: map[string]fs.FileMode{},
		explicit: map[string]bool{}, folded: map[string]string{},
	}
}

// unpackFile extracts an archive file and hashes the bytes it reads while it does, so that the
// caller can compare the digest of what was extracted, and not only of what was once written to
// the file, with the one it expects.
//
// Parameters:
//   - path: the archive, whose digest was already checked
//
// Returns the sha256 of the archive as it was read, or an *Error.
func (u *unpacker) unpackFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", wrapError(CodeDisk, err, path)
	}

	defer func() { _ = f.Close() }()

	h := sha256.New()

	if err := u.run(io.TeeReader(f, h)); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// run extracts one archive. Any violation aborts and names the entry; the caller deletes the
// staging directory.
//
// The archive must be one gzip stream, found by its magic bytes, that holds one tar archive and
// nothing after it: a second stream, trailing bytes, a truncated stream and non-zero data after
// the end of the tar are errors. The decompressed stream is counted while it is read.
//
// Parameters:
//   - src: the compressed archive
func (u *unpacker) run(src io.Reader) error {
	br := bufio.NewReader(src)

	magic, err := br.Peek(2)
	if err != nil || magic[0] != 0x1f || magic[1] != 0x8b {
		return newError(CodeArchiveFormat, "", ArchiveMagic)
	}

	gz, err := gzip.NewReader(br)
	if err != nil {
		return formatError(err)
	}

	defer func() { _ = gz.Close() }()

	gz.Multistream(false)

	stream := &capReader{r: gz, left: MaxTreeBytes + streamSlack}
	tr := tar.NewReader(stream)

	for {
		if err := u.ctx.Err(); err != nil {
			return err
		}

		hdr, err := tr.Next()
		if errors.Is(err, tar.ErrInsecurePath) && hdr != nil {
			err = nil
		}

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return formatError(err)
		}

		if err := u.entry(tr, hdr); err != nil {
			return err
		}
	}

	return u.finish(stream, br)
}

// finish reads what follows the tar archive: zeros up to the end of the gzip stream, and then
// nothing.
//
// Parameters:
//   - stream: the decompressed stream
//   - br: the compressed archive, positioned at the end of the gzip stream
func (u *unpacker) finish(stream io.Reader, br *bufio.Reader) error {
	buf := make([]byte, 32<<10)

	for {
		n, err := stream.Read(buf)
		if bytes.Count(buf[:n], []byte{0}) != n {
			return newError(CodeArchiveFormat, "", ArchiveTrailing)
		}

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return formatError(err)
		}
	}

	if _, err := br.Peek(1); !errors.Is(err, io.EOF) {
		return newError(CodeArchiveFormat, "", ArchiveTrailing)
	}

	return nil
}

// formatError turns a failure to read the archive into an archive_format or archive_limits error.
//
// Parameters:
//   - err: error of the gzip or tar reader
func formatError(err error) error {
	switch {
	case errors.Is(err, errStreamCap):
		return newError(CodeArchiveLimits, "", LimitBytes)
	case errors.Is(err, io.ErrUnexpectedEOF):
		return wrapError(CodeArchiveFormat, err, "", ArchiveTruncated)
	}

	return wrapError(CodeArchiveFormat, err, "", ArchiveCorrupt)
}

// entry handles one header of the archive.
//
// Parameters:
//   - tr: the tar reader, positioned at the contents of the entry
//   - hdr: the header
func (u *unpacker) entry(tr *tar.Reader, hdr *tar.Header) error {
	shown := hdr.Name

	if reason := entryType(hdr); reason != "" {
		return newError(CodeArchiveUnsafe, shown, reason)
	}

	// A global extended header that passed entryType only carries notes, such as the commit that
	// git archive records. The tar reader does not apply it to the entries that follow.
	if hdr.Typeflag == tar.TypeXGlobalHeader {
		return nil
	}

	u.entries++

	if u.entries > MaxTreeEntries {
		return newError(CodeArchiveLimits, shown, LimitEntries)
	}

	dir := hdr.Typeflag == tar.TypeDir

	name, err := entryName(hdr.Name, dir)
	if err != nil {
		return err
	}

	if name == "" {
		return nil
	}

	if dir {
		return u.mkdir(name)
	}

	return u.file(tr, name, hdr.Size)
}

// entryType refuses what is not a regular file or a directory, and the extended headers that
// change a path or lay a file out in a way tote does not read. A global extended header is
// accepted when none of its records is one of those: it names nothing and is skipped.
//
// Parameters:
//   - hdr: the header
//
// Returns the reason, empty for an entry that is acceptable.
func entryType(hdr *tar.Header) string {
	for k := range hdr.PAXRecords {
		if k == "path" || k == "linkpath" || strings.HasPrefix(k, "GNU.sparse") {
			return UnsafePAX
		}
	}

	switch hdr.Typeflag {
	case tar.TypeReg, tar.TypeDir, tar.TypeXGlobalHeader:
		return ""
	case tar.TypeSymlink:
		return UnsafeSymlink
	case tar.TypeLink:
		return UnsafeLink
	}

	return UnsafeSpecial
}

// entryName checks the name of an entry against the rules of 7.3 and returns it in the form it
// is created under.
//
// Parameters:
//   - raw: name as the archive holds it
//   - dir: true for a directory entry
//
// Returns the slash separated relative path, empty for the root of the archive, or an *Error:
// archive_unsafe or archive_limits.
func entryName(raw string, dir bool) (string, error) {
	unsafe := func(reason string) error { return newError(CodeArchiveUnsafe, raw, reason) }

	switch {
	case raw == "" || strings.ContainsRune(raw, 0) || strings.Contains(raw, "\\"):
		return "", unsafe(UnsafeName)
	case strings.HasPrefix(raw, "/"):
		return "", unsafe(UnsafeAbsolute)
	}

	name := strings.TrimPrefix(raw, "./")

	if strings.HasSuffix(name, "/") {
		if !dir {
			return "", unsafe(UnsafeName)
		}

		name = strings.TrimSuffix(name, "/")
	}

	if name == "." || name == "" {
		if !dir {
			return "", unsafe(UnsafeName)
		}

		return "", nil
	}

	if len(name) > MaxTreePath {
		return "", newError(CodeArchiveLimits, raw, LimitPath)
	}

	parts := strings.Split(name, "/")

	for _, p := range parts {
		switch {
		case p == "..":
			return "", unsafe(UnsafeParent)
		case p == "." || !segmentRe.MatchString(p):
			return "", unsafe(UnsafeName)
		}
	}

	if len(parts) > MaxTreeDepth {
		return "", newError(CodeArchiveLimits, raw, LimitDepth)
	}

	return name, nil
}

// claim records a path, refusing a duplicate, a name that differs only in case from another and a
// file where a directory is.
//
// Parameters:
//   - name: slash separated relative path
//   - kind: fs.ModeDir or 0 for a file
//   - implicit: true for a parent directory that an entry below it makes
//
// Returns whether the path was already there as the same kind, or an *Error: archive_unsafe.
func (u *unpacker) claim(name string, kind fs.FileMode, implicit bool) (bool, error) {
	if have, ok := u.kinds[name]; ok {
		switch {
		case have != kind:
			return false, newError(CodeArchiveUnsafe, name, UnsafeConflict)
		case kind != fs.ModeDir || (!implicit && u.explicit[name]):
			return false, newError(CodeArchiveUnsafe, name, UnsafeDuplicate)
		}

		u.explicit[name] = u.explicit[name] || !implicit

		return true, nil
	}

	key := strings.ToLower(name)
	if other, ok := u.folded[key]; ok && other != name {
		return false, newError(CodeArchiveUnsafe, name, UnsafeCase)
	}

	u.kinds[name] = kind
	u.folded[key] = name
	u.explicit[name] = !implicit

	return false, nil
}

// parents creates the directories above a path.
//
// Parameters:
//   - name: slash separated relative path of an entry
func (u *unpacker) parents(name string) error {
	parts := strings.Split(name, "/")

	for i := 1; i < len(parts); i++ {
		if err := u.ensureDir(strings.Join(parts[:i], "/"), true); err != nil {
			return err
		}
	}

	return nil
}

// ensureDir creates a directory unless it already exists.
//
// Parameters:
//   - name: slash separated relative path
//   - implicit: true when an entry below it asks for it
func (u *unpacker) ensureDir(name string, implicit bool) error {
	had, err := u.claim(name, fs.ModeDir, implicit)
	if err != nil || had {
		return err
	}

	// A parent that only an entry below it asks for is a node of its own: it counts against the
	// limit of entries, so that the limit bounds what is created and not only what is listed.
	if implicit {
		u.entries++

		if u.entries > MaxTreeEntries {
			return newError(CodeArchiveLimits, name, LimitEntries)
		}
	}

	if err := u.dst.Mkdir(name, 0o700); err != nil {
		return wrapError(CodeDisk, err, name)
	}

	return nil
}

// mkdir creates the directory of an entry.
//
// Parameters:
//   - name: slash separated relative path
func (u *unpacker) mkdir(name string) error {
	if err := u.parents(name); err != nil {
		return err
	}

	return u.ensureDir(name, false)
}

// file creates a regular file, mode 0600, with O_EXCL. The size comes from the header, but the
// bytes written are the ones the tar reader delivers, which it bounds by that size.
//
// Parameters:
//   - tr: the tar reader, positioned at the contents
//   - name: slash separated relative path
//   - size: size announced by the header
func (u *unpacker) file(tr *tar.Reader, name string, size int64) error {
	if size < 0 || size > MaxTreeBytes || u.bytes+size > MaxTreeBytes {
		return newError(CodeArchiveLimits, name, LimitBytes)
	}

	if err := u.parents(name); err != nil {
		return err
	}

	if _, err := u.claim(name, 0, false); err != nil {
		return err
	}

	out, err := u.dst.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return wrapError(CodeDisk, err, name)
	}

	n, err := io.Copy(diskWriter{out}, tr)
	if cerr := out.Close(); err == nil && cerr != nil {
		err = diskError{cerr}
	}

	var de diskError

	switch {
	case errors.As(err, &de):
		return wrapError(CodeDisk, de.err, name)
	case err != nil:
		return u.readFailure(name, err)
	case n != size:
		return newError(CodeArchiveFormat, name, ArchiveTruncated)
	}

	u.bytes += n

	return nil
}

// readFailure reports a failure to read the contents of an entry.
//
// Parameters:
//   - name: the entry
//   - err: error of the readers
func (u *unpacker) readFailure(name string, err error) error {
	var fe *Error
	if errors.As(formatError(err), &fe) {
		fe.Args[0] = CleanLine(name, maxReasonRunes)

		return fe
	}

	return err
}
