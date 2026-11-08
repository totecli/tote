package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
)

const (
	// maxBinarySize is the largest binary unpacked from an archive, in bytes.
	maxBinarySize = 256 << 20

	// maxExpandedSize is the most bytes read out of an archive, other files included.
	maxExpandedSize = 512 << 20

	// binaryName is the name of the binary in a release archive.
	binaryName = "tote"
)

// unpack extracts the binary of a release archive.
//
// Only one entry is ever written: a regular file named tote at the root of the archive. Any
// other entry is skipped without being written, so a hostile archive cannot create a link or a
// file elsewhere, and the sizes read are capped against a decompression bomb.
//
// Parameters:
//   - archive: path of the .tar.gz file
//   - dst: path of the file to create, which must not exist; it gets mode 0755
//
// Returns nil, or an error wrapping ErrBadArchive. The destination is removed on failure.
func unpack(archive, dst string) error {
	in, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	gz, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(io.LimitReader(gz, maxExpandedSize))
	written := false

	for {
		hdr, herr := tr.Next()
		if errors.Is(herr, io.EOF) {
			break
		}

		if herr != nil {
			cleanup(dst, written)
			return fmt.Errorf("%w: %v", ErrBadArchive, herr)
		}

		if hdr.Typeflag != tar.TypeReg || path.Clean(hdr.Name) != binaryName {
			continue
		}

		if written {
			cleanup(dst, written)
			return fmt.Errorf("%w: %s appears twice", ErrBadArchive, binaryName)
		}

		if err := writeBinary(tr, hdr.Size, dst); err != nil {
			return err
		}

		written = true
	}

	if !written {
		return fmt.Errorf("%w: no %s file", ErrBadArchive, binaryName)
	}

	return nil
}

// cleanup removes a partly written destination.
//
// Parameters:
//   - dst: file that may have been created
//   - created: whether it was
func cleanup(dst string, created bool) {
	if created {
		_ = os.Remove(dst)
	}
}

// writeBinary copies one archive entry to a new executable file and syncs it.
//
// Parameters:
//   - r: reader positioned on the content of the entry
//   - size: size announced by the header
//   - dst: file to create
func writeBinary(r io.Reader, size int64, dst string) error {
	if size <= 0 || size > maxBinarySize {
		return fmt.Errorf("%w: %s is %d bytes", ErrBadArchive, binaryName, size)
	}

	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}

	err = fillBinary(f, r, size)
	if err != nil {
		_ = os.Remove(dst)
	}

	return err
}

// fillBinary writes, sets the mode of, syncs and closes the new binary.
//
// Parameters:
//   - f: new file, always closed by the call
//   - r: reader positioned on the content of the entry
//   - size: size announced by the header
func fillBinary(f *os.File, r io.Reader, size int64) error {
	n, err := io.Copy(f, io.LimitReader(r, size))
	if err == nil && n != size {
		err = fmt.Errorf("%w: %s is truncated", ErrBadArchive, binaryName)
	}

	if err == nil {
		err = f.Chmod(binaryMode)
	}

	if err == nil {
		err = f.Sync()
	}

	if cerr := f.Close(); err == nil {
		err = cerr
	}

	return err
}
