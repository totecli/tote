package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/totecli/tote/internal/config"
)

// ErrHostFile is wrapped by every refusal to write a host-owned file of a plugin.
var ErrHostFile = errors.New("unsafe host file")

// hostFileRe matches the name of a host-owned file, such as last.log.
var hostFileRe = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

// hostDirMode is the mode of <State>/plugins-host/<id>.
const hostDirMode = fs.FileMode(0o700)

// WriteHostFile writes a file that belongs to the host, such as last.log, for one plugin.
//
// The file lives in <State>/plugins-host/<id>, a sibling of the directory the plugin may write to,
// so that a plugin cannot turn it into a symbolic link that the host would then write through.
// The directory is created by the host with mode 0700 and checked on every call with Lstat: it
// must be a real directory, owned by the user, with exactly that mode. The file is created
// 0600 and truncated, never through the store primitives, which follow links, and opened without
// following a link in the last element. An existing file that is a link, a directory or a
// non-regular file is refused.
//
// Parameters:
//   - paths: tote directories
//   - id: plugin id, checked with config.ValidPluginID
//   - name: file name, lower case letters, digits, dots, dashes and underscores, no slash
//   - data: new content
//   - uid: user id that must own the directory and the file
//
// Returns nil, or an error wrapping ErrHostFile when a check fails, or an I/O error.
func WriteHostFile(paths config.Paths, id, name string, data []byte, uid int) error {
	dir, err := paths.PluginHostDir(id)
	if err != nil {
		return err
	}

	if !hostFileRe.MatchString(name) {
		return fmt.Errorf("%w: file name %q", ErrHostFile, CleanLine(name, issueArgRunes))
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}

	if err := os.Mkdir(dir, hostDirMode); err == nil {
		if err := os.Chmod(dir, hostDirMode); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrExist) {
		return err
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}

	st, ok := statIDOf(info)
	if !ok || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 || int(st.UID) != uid ||
		info.Mode().Perm() != hostDirMode || info.Mode()&(fs.ModeSetgid|fs.ModeSticky) != 0 {
		return fmt.Errorf("%w: %s is not a private directory of the user", ErrHostFile,
			CleanLine(dir, 4*issueArgRunes))
	}

	return writeInRoot(dir, name, data, uid, info)
}

// writeInRoot writes a file below a directory that was just checked.
//
// Parameters:
//   - dir: directory of the host files
//   - name: file name, already checked
//   - data: content
//   - uid: user id that must own the file
//   - checked: Lstat result of dir, which the opened directory must be the same as
func writeInRoot(dir, name string, data []byte, uid int, checked fs.FileInfo) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	if now, err := root.Stat("."); err != nil || !os.SameFile(checked, now) {
		return fmt.Errorf("%w: the directory changed", ErrHostFile)
	}

	// A root follows a link that stays inside it, whatever the flags say, so the last element is
	// looked at before it is opened and must be the file that is then opened.
	var seen fs.FileInfo

	switch li, err := root.Lstat(name); {
	case err == nil && !li.Mode().IsRegular():
		return fmt.Errorf("%w: %s is a link or not a regular file", ErrHostFile, name)
	case err == nil:
		seen = li
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}

	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK,
		0o600)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return fmt.Errorf("%w: %s is a link", ErrHostFile, name)
		}

		return fmt.Errorf("%w: open %s: %w", ErrHostFile, name, err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	id, ok := statIDOf(info)
	if !ok || !info.Mode().IsRegular() || int(id.UID) != uid || info.Mode().Perm()&0o077 != 0 ||
		id.Nlink != 1 || (seen != nil && !os.SameFile(seen, info)) {
		return fmt.Errorf("%w: %s is not a private regular file of the user", ErrHostFile, name)
	}

	if err := f.Truncate(0); err != nil {
		return err
	}

	_, err = f.Write(data)

	return err
}
