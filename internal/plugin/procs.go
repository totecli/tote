package plugin

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/totecli/tote/internal/config"
)

// Limits of the process registry.
const (
	maxProcesses    = 4
	maxProcsFile    = 1 << 20
	maxProcsEntries = 256
	procsLockWait   = 5 * time.Second
	killGrace       = 2 * time.Second
)

// runState is what a Registry keeps for running programs: how many plugin processes this tote
// has, and the cache of the git helper.
type runState struct {
	mu     sync.Mutex
	active int
	git    *gitCache
}

// newRunState creates an empty state.
func newRunState() *runState { return &runState{git: newGitCache()} }

// acquire takes one of the process slots. A fifth start is refused at once: there is no queue.
func (s *runState) acquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active >= maxProcesses {
		return false
	}

	s.active++

	return true
}

// release gives a process slot back.
func (s *runState) release() {
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
}

// ProcEntry is one line of plugin-procs: a plugin id, the session id of a process group the host
// started for it, and the start token of the leader, which tells the process from a later one that
// got the same id.
type ProcEntry struct {
	ID    string
	SID   int
	Start string
}

// procsFile is an open and locked plugin-procs file.
type procsFile struct {
	f       *os.File
	entries []ProcEntry
}

// openProcs opens <State>/plugin-procs for a change, creating it 0600, and takes an exclusive lock
// on it. The file must be a regular file of the user that nobody else can write; a link, a file of
// another user or a wider mode is refused, because the host would otherwise signal the process
// groups that file names.
//
// Parameters:
//   - path: location of the file
//   - uid: user id that must own it
func openProcs(path string, uid int) (*procsFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w: open plugin-procs: %w", ErrHostFile, err)
	}

	pf := &procsFile{f: f}

	if err := pf.check(uid); err != nil {
		_ = f.Close()

		return nil, err
	}

	if err := lockFile(f); err != nil {
		_ = f.Close()

		return nil, err
	}

	if err := pf.read(); err != nil {
		_ = f.Close()

		return nil, err
	}

	return pf, nil
}

// check verifies that the open file is a private regular file of the user.
//
// Parameters:
//   - uid: user id that must own it
func (pf *procsFile) check(uid int) error {
	info, err := pf.f.Stat()
	if err != nil {
		return err
	}

	id, ok := statIDOf(info)
	if !ok || !info.Mode().IsRegular() || int(id.UID) != uid || info.Mode().Perm()&0o077 != 0 ||
		id.Nlink != 1 {
		return fmt.Errorf("%w: plugin-procs is not a private regular file of the user", ErrHostFile)
	}

	return nil
}

// lockFile takes an exclusive lock, giving up after a few seconds.
//
// Parameters:
//   - f: open file
func lockFile(f *os.File) error {
	deadline := time.Now().Add(procsLockWait)

	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}

		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return err
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%w: plugin-procs is locked", ErrHostFile)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// read parses the file. A line that does not parse is dropped, so a damaged file loses entries but
// never stops a run.
func (pf *procsFile) read() error {
	data, err := io.ReadAll(io.LimitReader(pf.f, maxProcsFile))
	if err != nil {
		return err
	}

	pf.entries = parseProcs(string(data))

	return nil
}

// parseProcs reads the lines "<id> <sid> <start>" followed by any further fields. A later release
// may append a field to a line; this one keeps the entry, so that the processes it names are still
// stopped, and ignores the tail (a rewrite of the file then writes the three known fields).
//
// Parameters:
//   - text: content of the file
func parseProcs(text string) []ProcEntry {
	var out []ProcEntry

	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || config.ValidPluginID(f[0]) != nil || !validToken(f[2]) {
			continue
		}

		sid, err := strconv.Atoi(f[1])
		if err != nil || sid <= 1 {
			continue
		}

		out = append(out, ProcEntry{ID: f[0], SID: sid, Start: f[2]})

		if len(out) >= maxProcsEntries {
			break
		}
	}

	return out
}

// validToken checks a start token: digits, or "-" when unknown.
//
// Parameters:
//   - s: token as written in the file
func validToken(s string) bool {
	if s == "-" {
		return true
	}

	if s == "" || len(s) > 20 {
		return false
	}

	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

// write replaces the content of the file, under the lock.
func (pf *procsFile) write() error {
	var b strings.Builder

	for _, e := range pf.entries {
		b.WriteString(e.ID + " " + strconv.Itoa(e.SID) + " " + e.Start + "\n")
	}

	if err := pf.f.Truncate(0); err != nil {
		return err
	}

	_, err := pf.f.WriteAt([]byte(b.String()), 0)

	return err
}

// close unlocks and closes the file.
func (pf *procsFile) close() { _ = pf.f.Close() }

// prune drops the entries whose session no longer exists.
func (pf *procsFile) prune() {
	kept := pf.entries[:0]

	for _, e := range pf.entries {
		if sessionAlive(e.SID, e.Start) {
			kept = append(kept, e)
		}
	}

	pf.entries = kept
}

// uid returns the user id that owns the host files.
func (r *Registry) uid() int {
	if r.opts.Getuid != nil {
		return r.opts.Getuid()
	}

	return os.Getuid()
}

// trackProc records a process group that the host started for a plugin. It runs under the lock of
// plugin-procs and drops the entries of sessions that ended.
//
// Parameters:
//   - id: plugin id
//   - sid: session id of the new group, the pid of its leader
func (r *Registry) trackProc(id string, sid int) (ProcEntry, error) {
	pf, err := openProcs(r.opts.Paths.ProcsFile(), r.uid())
	if err != nil {
		return ProcEntry{}, err
	}

	defer pf.close()

	pf.prune()

	e := ProcEntry{ID: id, SID: sid, Start: startToken(sid)}
	pf.entries = append(pf.entries, e)

	if len(pf.entries) > maxProcsEntries {
		pf.entries = pf.entries[len(pf.entries)-maxProcsEntries:]
	}

	return e, pf.write()
}

// untrackProc removes a process group from plugin-procs once its leader has exited and the output
// was drained.
//
// Parameters:
//   - e: entry returned by trackProc
func (r *Registry) untrackProc(e ProcEntry) {
	pf, err := openProcs(r.opts.Paths.ProcsFile(), r.uid())
	if err != nil {
		return
	}

	defer pf.close()

	kept := pf.entries[:0]

	for _, x := range pf.entries {
		if x != e {
			kept = append(kept, x)
		}
	}

	pf.entries = kept

	_ = pf.write()
}

// Procs lists the plugin process groups now running, as plugin-procs records them, without those
// that ended.
func (r *Registry) Procs() ([]ProcEntry, error) {
	pf, err := openProcs(r.opts.Paths.ProcsFile(), r.uid())
	if err != nil {
		return nil, err
	}

	defer pf.close()

	pf.prune()

	_ = pf.write()

	return append([]ProcEntry(nil), pf.entries...), nil
}

// InPluginSession reports whether the calling process belongs to a session that plugin-procs
// records: the guard that makes tote refuse its writing verbs when a plugin started it, even when
// the plugin removed TOTE_PLUGIN_ID from the environment. A descendant that called setsid again
// leaves the session; the guard is best effort, not a boundary.
//
// Parameters:
//   - paths: tote directories
func InPluginSession(paths config.Paths) bool {
	sid, err := getsid(0)
	if err != nil {
		return false
	}

	data, err := readLimited(paths.ProcsFile(), maxProcsFile)
	if err != nil {
		return false
	}

	for _, e := range parseProcs(string(data)) {
		// A session that ended can leave its line behind, and its number can later belong to an
		// unrelated terminal; the entry counts only while the session it names is the same.
		if e.SID == sid && sessionAlive(e.SID, e.Start) {
			return true
		}
	}

	return false
}

// safeSID refuses the ids that must never be signalled: the init process, the caller's own
// process, group and session, and anything that is not a valid id. A tampered plugin-procs cannot
// make the host kill itself or the system.
//
// Parameters:
//   - sid: session or process group id from a record
func safeSID(sid int) bool {
	if sid <= 1 || sid == os.Getpid() || sid == syscall.Getpgrp() {
		return false
	}

	if own, err := getsid(0); err == nil && own == sid {
		return false
	}

	return true
}

// killGroup sends a signal to a process group.
//
// Parameters:
//   - pgid: process group id; an id that safeSID refuses is ignored
//   - sig: signal
func killGroup(pgid int, sig syscall.Signal) error {
	if !safeSID(pgid) {
		return fs.ErrInvalid
	}

	return syscall.Kill(-pgid, sig)
}

// StopPlugin stops the process groups that plugin-procs records for a plugin, whichever tote
// started them: TERM to the group and, on Linux, to every process left in the session, then KILL
// for what is still there after the grace time. An entry counts only if its start token still
// matches. A descendant that called setsid again has left the session and is not reached: this is
// best effort, not a guarantee.
//
// Parameters:
//   - id: plugin id
//   - grace: time between TERM and KILL, 0 meaning 2 seconds
//
// Returns the number of sessions that were signalled.
func (r *Registry) StopPlugin(id string, grace time.Duration) (int, error) {
	if grace <= 0 {
		grace = killGrace
	}

	pf, err := openProcs(r.opts.Paths.ProcsFile(), r.uid())
	if err != nil {
		return 0, err
	}

	pf.prune()

	var mine, others []ProcEntry

	for _, e := range pf.entries {
		switch {
		case e.ID != id:
			others = append(others, e)
		case safeSID(e.SID):
			mine = append(mine, e)
		}
	}

	pf.entries = others
	werr := pf.write()

	pf.close()

	for _, e := range mine {
		signalSession(e.SID, syscall.SIGTERM)
	}

	deadline := time.Now().Add(grace)

	for time.Now().Before(deadline) && anyAlive(mine) {
		time.Sleep(20 * time.Millisecond)
	}

	for _, e := range mine {
		signalSession(e.SID, syscall.SIGKILL)
	}

	return len(mine), werr
}

// anyAlive reports whether any recorded session still has a process.
//
// Parameters:
//   - list: entries to look at
func anyAlive(list []ProcEntry) bool {
	for _, e := range list {
		if sessionAlive(e.SID, e.Start) {
			return true
		}
	}

	return false
}

// signalSession sends a signal to a process group and, where the system can list them, to every
// process of the session.
//
// Parameters:
//   - sid: session id
//   - sig: signal
func signalSession(sid int, sig syscall.Signal) {
	_ = killGroup(sid, sig)

	for _, pid := range sessionMembers(sid) {
		if pid != os.Getpid() {
			_ = syscall.Kill(pid, sig)
		}
	}
}
