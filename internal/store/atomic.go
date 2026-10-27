package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
)

// renameFile is the rename step of writeAtomic, a variable so that tests can make it fail.
var renameFile = os.Rename

// resolveTarget finds the real file a data path stands for and makes sure its directory exists.
//
// A missing parent directory is created with mode 0700. A symlink is followed to its final
// target so that the write replaces the target and leaves the link alone. A path that does not
// exist yet resolves to its name inside the resolved parent directory.
//
// Parameters:
//   - path: data file path as given to Open, possibly a symlink or not yet existing
//
// Returns the resolved path, or an error when a directory cannot be created or the path is a
// dangling symlink.
func resolveTarget(path string) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	if info, lerr := os.Lstat(path); lerr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("resolve %s: dangling symlink", path)
		}

		// Another writer created the file between the two calls.
		if resolved, err = filepath.EvalSymlinks(path); err != nil {
			return "", fmt.Errorf("resolve %s: %w", path, err)
		}
		return resolved, nil
	}

	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}

	return filepath.Join(realDir, filepath.Base(path)), nil
}

// writeAtomic replaces a file with new content so that readers see the old or the new bytes.
//
// The content goes to a temporary file created in the same directory, which is synced, renamed
// over the target, and followed by a sync of the directory. A new file gets mode 0600; an
// existing file keeps its mode, and the target itself is never chmod-ed. A target without the
// owner write bit is refused, because the rename would bypass the protection its owner set. A
// content larger than maxFileSize is refused, because readFile would not read it back. The
// temporary file is removed on every error path.
//
// Parameters:
//   - path: resolved target path, see resolveTarget
//   - data: complete new content
//
// Returns an error when any step fails, when the target is read-only or when the content passes
// the size cap (wrapping ErrTooBig), in which case the target is unchanged.
func writeAtomic(path string, data []byte) error {
	if len(data) > maxFileSize {
		return fmt.Errorf("%w: %s would be %d bytes, the limit is %d", ErrTooBig,
			filepath.Base(path), len(data), maxFileSize)
	}

	perm := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
		if perm&0o200 == 0 {
			return fmt.Errorf("write %s: file is read-only", path)
		}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	name := tmp.Name()

	err = fillTemp(tmp, data, perm)
	if err == nil {
		err = renameFile(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("write %s: %w", path, err)
	}

	return syncDir(dir)
}

// fillTemp writes, sets the mode of, syncs and closes a temporary file.
//
// Parameters:
//   - f: open temporary file, always closed by the call
//   - data: bytes to write
//   - perm: mode to give to the file
func fillTemp(f *os.File, data []byte, perm os.FileMode) error {
	_, err := f.Write(data)
	if err == nil {
		err = f.Chmod(perm)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}

	return err
}

// syncDir flushes a directory so that a rename inside it survives a crash.
//
// Parameters:
//   - dir: directory path
//
// Returns an error unless the file system simply does not support syncing a directory.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}

	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	if err != nil && !errors.Is(err, syscall.EINVAL) {
		return fmt.Errorf("sync directory: %w", err)
	}

	return nil
}

// sameRecords reports whether two record lists hold equal values in the same order.
//
// It behaves like reflect.DeepEqual except that a nil and an empty slice or map are equal,
// because a field that is empty is written nowhere and decodes back as nil.
//
// Parameters:
//   - a: first list
//   - b: second list
func sameRecords[T Record](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameValue(reflect.ValueOf(a[i]), reflect.ValueOf(b[i])) {
			return false
		}
	}

	return true
}

// sameValue compares two values deeply, treating empty and nil collections as equal.
//
// Parameters:
//   - a: first value
//   - b: second value
func sameValue(a, b reflect.Value) bool {
	if a.Kind() != b.Kind() {
		return false
	}

	switch a.Kind() {
	case reflect.Invalid:
		return true
	case reflect.Slice, reflect.Array:
		return sameElems(a, b)
	case reflect.Struct:
		for i := range a.NumField() {
			if !sameValue(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return sameValue(a.Elem(), b.Elem())
	case reflect.Map:
		return sameMap(a, b)
	}

	return sameScalar(a, b)
}

// sameElems compares two slices or arrays element by element.
//
// Parameters:
//   - a: first slice or array
//   - b: second value of the same kind
func sameElems(a, b reflect.Value) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := range a.Len() {
		if !sameValue(a.Index(i), b.Index(i)) {
			return false
		}
	}

	return true
}

// sameMap compares two maps, an empty map being equal to a nil one.
//
// Parameters:
//   - a: first map
//   - b: second map
func sameMap(a, b reflect.Value) bool {
	if a.Len() != b.Len() {
		return false
	}
	for _, k := range a.MapKeys() {
		other := b.MapIndex(k)
		if !other.IsValid() || !sameValue(a.MapIndex(k), other) {
			return false
		}
	}

	return true
}

// sameScalar compares two values of a basic kind.
//
// Parameters:
//   - a: first value
//   - b: second value of the same kind
//
// Returns false for kinds that a record cannot sensibly hold, such as channels and functions.
func sameScalar(a, b reflect.Value) bool {
	switch {
	case a.CanInt():
		return a.Int() == b.Int()
	case a.CanUint():
		return a.Uint() == b.Uint()
	case a.CanFloat():
		return a.Float() == b.Float()
	case a.CanComplex():
		return a.Complex() == b.Complex()
	}

	switch a.Kind() {
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.String:
		return a.String() == b.String()
	}

	return false
}

// sameValueOf compares two records with the rules of sameRecords.
//
// Parameters:
//   - a: first record
//   - b: second record
func sameValueOf[T Record](a, b T) bool {
	return sameValue(reflect.ValueOf(a), reflect.ValueOf(b))
}
