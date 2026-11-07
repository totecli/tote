package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Names in the staging directory: an install in progress and a tree waiting for deletion.
const (
	stagePrefix = "stage-"
	trashPrefix = "trash-"
)

// staleStage is how long an install may stay in staging before a cleanup treats it as abandoned.
const staleStage = time.Hour

// mutexWait is how long a change waits for the plugin mutex, a variable so that tests can shorten
// it.
var mutexWait = 10 * time.Second

// mutexPoll is the pause between two attempts to take the mutex.
const mutexPoll = 10 * time.Millisecond

// pluginsDir is the directory that holds the installed plugins, resolved and checked.
//
// Real is the plugins directory with its symbolic links resolved: every path the host builds for
// a change is a direct child of it, so a link in the configured path is followed once, here, and
// never again. Staging is Real/.staging.
type pluginsDir struct {
	Real    string
	Staging string
}

// ownedBy reports whether a file belongs to a user and cannot be written by anyone else.
//
// Parameters:
//   - info: result of Lstat
//   - uid: user id
func ownedBy(info fs.FileInfo, uid int) bool {
	id, ok := statIDOf(info)

	return ok && int(id.UID) == uid && info.Mode().Perm()&0o022 == 0
}

// ownedByUser reports whether a file belongs to a user, whoever can write it.
//
// Parameters:
//   - info: result of Lstat
//   - uid: user id
func ownedByUser(info fs.FileInfo, uid int) bool {
	id, ok := statIDOf(info)

	return ok && int(id.UID) == uid
}

// resolvePlugins resolves the plugins directory and its staging directory, creating them 0700 when
// create is true. The plugins directory must be a real directory of the user that no one else can
// write, and so must the staging directory when it is created or used: a staging directory that
// is a link would let a rename put a tree anywhere.
//
// Parameters:
//   - create: true to create what is missing
//
// Returns the directories, or an *Error: disk when they cannot be created, store_writable when
// others can write them, store_corrupt when they are not what they must be.
func (r *Registry) resolvePlugins(create bool) (pluginsDir, error) {
	base := r.opts.Paths.PluginsDir()

	if create {
		if err := os.MkdirAll(base, 0o700); err != nil {
			return pluginsDir{}, wrapError(CodeDisk, err, base)
		}
	}

	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return pluginsDir{}, wrapError(CodeDisk, err, base)
	}

	pd := pluginsDir{Real: resolved, Staging: filepath.Join(resolved, ".staging")}

	if err := r.checkDir(pd.Real); err != nil {
		return pluginsDir{}, err
	}

	if create {
		if err := os.Mkdir(pd.Staging, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return pluginsDir{}, wrapError(CodeDisk, err, pd.Staging)
		}

		if err := r.checkDir(pd.Staging); err != nil {
			return pluginsDir{}, err
		}
	}

	return pd, nil
}

// missingStore lists, from the top, the directories that the plugin store does not have yet: the
// missing parents of the plugins directory, the plugins directory and its staging directory.
func (r *Registry) missingStore() []string {
	base := r.opts.Paths.PluginsDir()

	var out []string

	for dir := base; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil || filepath.Dir(dir) == dir {
			break
		}

		out = append([]string{dir}, out...)
	}

	staging := filepath.Join(base, ".staging")
	if _, err := os.Lstat(staging); err != nil {
		out = append(out, staging)
	}

	return out
}

// dropDirs removes directories that this process created, deepest first. A directory that is not
// empty stays: removal is never recursive.
//
// Parameters:
//   - dirs: directories, from the top
func dropDirs(dirs []string) {
	for i := len(dirs) - 1; i >= 0; i-- {
		if os.Remove(dirs[i]) != nil {
			return
		}
	}
}

// checkDir checks that a directory of the host is a real directory of the user that no one else
// can write.
//
// Parameters:
//   - dir: directory, looked at with Lstat
//
// Returns nil, an *Error store_writable for a directory of the user that others can write, which
// chmod go-w fixes, store_corrupt for anything else that is not right, or disk.
func (r *Registry) checkDir(dir string) error {
	info, err := os.Lstat(dir)

	switch {
	case err != nil:
		return wrapError(CodeDisk, err, dir)
	case !info.IsDir() || !ownedBy(info, r.uid()):
		if info.IsDir() && ownedByUser(info, r.uid()) {
			return newError(CodeStoreWritable, dir, dir)
		}

		return newError(CodeStoreCorrupt, dir)
	}

	return nil
}

// randName returns a name that no other process picks: the prefix and 16 random hex digits.
//
// Parameters:
//   - prefix: stagePrefix or trashPrefix
func randName(prefix string) string {
	var b [8]byte

	// Since Go 1.24 the system source of randomness cannot fail: Read always fills the buffer.
	_, _ = rand.Read(b[:])

	return prefix + hex.EncodeToString(b[:])
}

// newStage creates a staging directory for one install, mode 0700.
//
// Parameters:
//   - pd: the plugins directory
func newStage(pd pluginsDir) (string, error) {
	var last error

	for range 8 {
		dir := filepath.Join(pd.Staging, randName(stagePrefix))

		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return dir, nil
		}

		last = err
	}

	return "", wrapError(CodeDisk, last, pd.Staging)
}

// changeLock is the exclusive lock on .mutex, held for the commit of an install, an update or a
// removal. It is an flock, so the kernel drops it when the process dies.
type changeLock struct {
	f *os.File
}

// lockChanges takes the plugin mutex, polling until the wait is over.
//
// Parameters:
//   - pd: the plugins directory
//   - wait: how long to poll; 0 tries once
//
// Returns the lock, or an *Error: busy when another tote holds it, store_corrupt when .mutex is a
// link or not a private file of the user.
func (r *Registry) lockChanges(pd pluginsDir, wait time.Duration) (*changeLock, error) {
	path := filepath.Join(pd.Real, ".mutex")

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, wrapError(CodeStoreCorrupt, err, path)
	}

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !ownedBy(info, r.uid()) {
		_ = f.Close()

		return nil, newError(CodeStoreCorrupt, path)
	}

	deadline := time.Now().Add(wait)

	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &changeLock{f: f}, nil
		}

		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()

			return nil, wrapError(CodeStoreCorrupt, err, path)
		}

		if !time.Now().Before(deadline) {
			_ = f.Close()

			return nil, newError(CodeBusy)
		}

		time.Sleep(mutexPoll)
	}
}

// release drops the lock. It is safe to call twice.
func (l *changeLock) release() {
	if l == nil || l.f == nil {
		return
	}

	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}

// now returns the time of a record, in UTC.
func (r *Registry) now() time.Time {
	if r.opts.Now != nil {
		return r.opts.Now().UTC()
	}

	return time.Now().UTC()
}
