package update

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// writeFileAtomic replaces a file so that a reader sees the old or the new content, never a
// mix.
//
// The data goes to a temporary file beside the target, which is synced and renamed over it, and
// the directory is synced after. The temporary file is removed when a step fails. When tote
// runs as root and replaces a file, the new file keeps the owner of the old one, so that a
// command run with sudo leaves no file owned by root in the home of a user.
//
// Parameters:
//   - path: target file, in an existing directory
//   - data: complete new content
//   - perm: mode of the new file
//
// Returns an error when a step fails, in which case the target is unchanged.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	old, _ := os.Stat(path)

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}

	name := tmp.Name()

	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(perm)
	}

	if err == nil {
		err = keepOwner(name, old)
	}

	if err == nil {
		err = tmp.Sync()
	}

	if cerr := tmp.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = os.Rename(name, path)
	}

	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("write %s: %w", path, err)
	}

	syncDir(dir)

	return nil
}

// syncDir flushes a directory so that a rename survives a crash.
//
// Parameters:
//   - dir: directory to sync; a failure is ignored because some file systems refuse it
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}

	_ = d.Sync()
	_ = d.Close()
}

// keepOwner gives a new file the owner and group of the file it replaces, when running as root.
//
// Parameters:
//   - path: new file
//   - old: information on the replaced file, nil when there was none
//
// Returns nil when nothing needs to change, else the error of the change of owner.
func keepOwner(path string, old os.FileInfo) error {
	if old == nil || os.Geteuid() != 0 {
		return nil
	}

	st, ok := old.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	return os.Chown(path, int(st.Uid), int(st.Gid))
}
