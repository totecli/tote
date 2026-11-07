package plugin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/totecli/tote/internal/store"
)

// Points of an install where a test can make the next step fail or kill the process. The names
// are those of section 7.4: staged is the end of the copy and the validation, c1 is the rename of
// the tree into place, c2 the single write of the lock record that switches the version and the
// consent, c3 the creation of the state and cache directories and c4 the removal of the previous
// tree.
const (
	PointStaged    = "staged"
	PointC1Before  = "c1.before"
	PointC1Renamed = "c1.renamed"
	PointC1After   = "c1.after"
	PointC2Before  = "c2.before"
	PointC2After   = "c2.after"
	PointC3Before  = "c3.before"
	PointC3After   = "c3.after"
	PointC4Before  = "c4.before"
	PointC4After   = "c4.after"
)

// PointRemoveStop is the point of Remove between the kill switch and the stop of the processes.
// Its error is ignored: a removal does not fail for a test's sake.
const PointRemoveStop = "remove.stop"

// installHook is called at every point of an install. It does nothing in production; tests replace
// it to return an error (an I/O failure) or to end the process (a crash), like renameFile in
// internal/store.
var installHook = func(string) error { return nil }

// ReasonStepFailed is the warning for a step after the switch that failed. The install itself
// stands: from the lock write on, nothing can undo it.
const ReasonStepFailed = "step_failed"

// ReasonPluginDir is the warning for a state or cache directory that could not be prepared.
const ReasonPluginDir = "plugin_dir"

// InstallResult is what a Commit did.
//
// State and Reasons are the state of the plugin right after, from a fresh read: a plugin that is
// not enabled says why. ConsentedBy is who gave the consent this commit recorded, empty when the
// earlier consent of a non-escalating update was kept. Warnings are the steps after the switch
// that failed. Leftovers are the paths that could not be deleted and wait for the next cleanup.
// Unchanged is true when the package was the installed one already (Staged.AlreadyInstalled): the
// store and the lock were not touched, and State and Reasons are those read when it was staged.
type InstallResult struct {
	ID          string
	Version     string
	OldVersion  string
	Updated     bool
	State       State
	Reasons     []Reason
	ConsentedBy string
	Warnings    []Reason
	Leftovers   []string
	Unchanged   bool
}

// Commit installs the staged package once the user consented.
//
// The consent is checked first against the Diff the user saw (see Grant): none gives
// consent_needed, a token that differs gives grant_mismatch, and in both cases the package stays
// staged and Commit can be called again. Then, under the plugin mutex (waiting up to 10 seconds,
// then busy, which also leaves the package staged), the commit protocol of section 7.4 runs and
// nothing is ever overwritten:
//
//   - C1 renames the staging directory to <Data>/plugins/<id>/<tree32>. If that directory already
//     exists, it is accepted only when its full hash equals the staged one (an orphan of an
//     earlier crash is reused), else the install stops with tree_collision.
//   - C2 writes the lock record in one atomic write: the switch of the version, the tree, the
//     capabilities, the surface and the consent. It is compared with the record the Diff was
//     computed against, so another change in between gives stale.
//   - C3 creates the state and cache directories.
//   - C4 moves the previous tree to staging and deletes it; a failure is a leftover.
//
// A failure before C2 leaves the previous version running and removes the tree this commit put in
// place. A crash after C1 leaves an orphan tree that nothing runs and a retry reuses; after C2 the
// new version runs and the old tree waits for the next cleanup. After a failed Commit the package
// is spent, except for the errors named above. A package that AlreadyInstalled reports is not
// committed: no consent is checked, nothing is written, and the result is Unchanged.
//
// Parameters:
//   - g: the consent
//
// Returns the result, or an *Error: consent_needed, grant_mismatch, busy, stale, tree_hash when
// the staged copy changed, tree_collision, store_corrupt, disk.
func (s *Staged) Commit(g Grant) (InstallResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.spent {
		return InstallResult{}, newError(CodeStale, s.diff.ID)
	}

	if s.same != nil {
		s.spent = true
		_ = s.discard()

		return InstallResult{
			ID: s.diff.ID, Version: s.diff.Version, State: s.same.State,
			Reasons: s.same.Reasons, Unchanged: true,
		}, nil
	}

	by, err := checkGrant(s.diff, g)
	if err != nil {
		return InstallResult{}, err
	}

	res, err := s.apply(by)

	var pe *Error
	if err != nil && errors.As(err, &pe) && pe.Code == CodeBusy {
		return res, err
	}

	s.spent = true
	if err != nil {
		_ = s.discard()

		return res, err
	}

	// The store holds a plugin now: what the plan created belongs to it.
	s.fresh = nil

	return res, nil
}

// step runs the hook at a point of the install.
//
// Parameters:
//   - point: one of the Point constants
func step(point string) error {
	if err := installHook(point); err != nil {
		return wrapError(CodeDisk, err, point)
	}

	return nil
}

// apply runs the commit protocol under the mutex.
//
// Parameters:
//   - by: consented_by to record, empty to keep the consent on record
func (s *Staged) apply(by string) (InstallResult, error) {
	r := s.r
	m := s.pm.m
	res := InstallResult{
		ID: m.ID, Version: m.Version, Updated: s.base != nil,
		ConsentedBy: by,
	}

	if s.base != nil {
		res.OldVersion = s.base.Version
	}

	mu, err := r.lockChanges(s.pd, mutexWait)
	if err != nil {
		return res, err
	}

	defer mu.release()

	lk, err := r.openLockForWrite()
	if err != nil {
		return res, err
	}

	if err := s.checkBase(lk); err != nil {
		return res, err
	}

	if err := s.verifyStage(); err != nil {
		return res, err
	}

	if err := step(PointC1Before); err != nil {
		return res, err
	}

	dest, placed, err := s.place()
	if err != nil {
		return res, err
	}

	if err := s.switchTo(lk, by, dest, placed); err != nil {
		return res, err
	}

	r.finish(s, &res)

	return res, nil
}

// checkBase checks under the mutex that the record is still the one the Diff was computed
// against: absent for a new install, unchanged in version, tree, capabilities and consent for an
// update.
//
// Parameters:
//   - lk: the lock, read just now
func (s *Staged) checkBase(lk *Lock) error {
	id := s.pm.m.ID
	rec, found := lk.Get(id)

	switch {
	case lk.Invalid()[id] != nil:
		return newError(CodeStoreCorrupt, id)
	case s.base == nil && found:
		return newError(CodeStale, id)
	case s.base != nil && (!found || !sameBase(rec, *s.base)):
		return newError(CodeStale, id)
	}

	return nil
}

// sameBase reports whether a record is still the one the Diff was computed against.
//
// Parameters:
//   - cur: the record now
//   - base: the record at Plan
func sameBase(cur, base LockRecord) bool {
	return cur.TreeSHA256 == base.TreeSHA256 && cur.Version == base.Version &&
		cur.ConsentedAt == base.ConsentedAt && slices.Equal(cur.Caps, base.Caps)
}

// verifyStage hashes the staging directory again: a copy that changed since Plan, by a process of
// the same user, is not what the user consented to.
func (s *Staged) verifyStage() error {
	sc, terr := scanTree(s.dir)
	if terr != nil {
		return treeFailure(terr)
	}

	got, terr := sc.hash(s.dir, nil)
	if terr != nil {
		return treeFailure(terr)
	}

	if got.SHA256 != s.tree.SHA256 {
		return newError(CodeTreeHash, TreeID(s.tree.SHA256), TreeID(got.SHA256))
	}

	return nil
}

// ensureIDDir makes sure <Data>/plugins/<id> is a real directory of the user, creating it.
//
// Parameters:
//   - idDir: the directory
func (r *Registry) ensureIDDir(idDir string) error {
	_, err := os.Lstat(idDir)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.Mkdir(idDir, 0o700); err != nil {
			return wrapError(CodeDisk, err, idDir)
		}

		return nil
	case err != nil:
		return wrapError(CodeDisk, err, idDir)
	}

	return r.checkDir(idDir)
}

// place is C1: the tree is made read-only and renamed into its directory, or an identical tree
// that is already there is reused.
//
// Returns the directory of the tree, whether this call put it there, or an *Error. When it
// returns an error the staging directory is still where it was.
func (s *Staged) place() (string, bool, error) {
	id := s.pm.m.ID
	idDir := filepath.Join(s.pd.Real, id)

	if err := s.r.ensureIDDir(idDir); err != nil {
		return "", false, err
	}

	dest := filepath.Join(idDir, s.tree.ID())

	_, err := os.Lstat(dest)
	if err == nil {
		return s.reuse(dest)
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return "", false, wrapError(CodeDisk, err, dest)
	}

	// The top directory stays writable until it has moved: a directory cannot change parent
	// without write permission on itself.
	if err := sealTree(s.dir, false); err != nil {
		return "", false, wrapError(CodeDisk, err, s.dir)
	}

	if err := os.Rename(s.dir, dest); err != nil {
		return "", false, wrapError(CodeDisk, err, dest)
	}

	// The staging directory is the tree now.
	s.dir = ""

	return dest, true, nil
}

// reuse accepts a tree that is already in place when its full hash is the staged one. The name of
// a tree decides nothing; the bytes do.
//
// Parameters:
//   - dest: the existing directory
func (s *Staged) reuse(dest string) (string, bool, error) {
	info, err := os.Lstat(dest)
	if err != nil || !info.IsDir() {
		return "", false, newError(CodeTreeCollision, filepath.Base(dest))
	}

	sc, terr := scanTree(dest)
	if terr != nil {
		return "", false, newError(CodeTreeCollision, filepath.Base(dest))
	}

	got, terr := sc.hash(dest, nil)
	if terr != nil || got.SHA256 != s.tree.SHA256 {
		return "", false, newError(CodeTreeCollision, filepath.Base(dest))
	}

	if err := sealTree(dest, true); err != nil {
		return "", false, wrapError(CodeDisk, err, dest)
	}

	// The staged copy is a duplicate now.
	_ = s.dropStage()

	return dest, false, nil
}

// sealTree makes a tree read-only: directories 0500, files 0400 and programs 0500. The children
// are done before their parent, so that no directory is closed before its entries are changed.
// It only reads the tree through an os.Root and changes modes, which is idempotent.
//
// Parameters:
//   - dir: the tree
//   - top: true to close the top directory too
func sealTree(dir string, top bool) error {
	sc, terr := scanTree(dir)
	if terr != nil {
		return terr
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	for i := len(sc.entries) - 1; i >= 0; i-- {
		e := sc.entries[i]
		if e.Rel == "" && !top {
			continue
		}

		mode := treeFileMode

		switch {
		case e.Dir:
			mode = treeDirMode
		case e.ID.Mode.Perm()&0o100 != 0:
			mode = treeExecMode
		}

		if err := root.Chmod(dotIfEmpty(e.Rel), mode); err != nil {
			return err
		}
	}

	return nil
}

// switchTo runs C1 (the end of it) and C2: the top directory is closed, then the lock record
// replaces the old one. When anything fails before the record is certain to be written, the tree
// this commit put in place is removed, so no orphan is left by a failure that is not a crash.
//
// Parameters:
//   - lk: the lock
//   - by: consented_by to record, empty to keep the consent on record
//   - dest: the directory of the tree
//   - placed: true when this commit created the directory
func (s *Staged) switchTo(lk *Lock, by, dest string, placed bool) error {
	undo := func(cause error) error {
		if placed {
			if refers, known := s.recordRefers(); known && !refers {
				_ = removeTree(dest)

				// A directory of an id that holds nothing else goes too; os.Remove refuses a
				// directory that still has a tree in it.
				_ = os.Remove(filepath.Dir(dest))
			}
		}

		return cause
	}

	if placed {
		if err := step(PointC1Renamed); err != nil {
			return undo(err)
		}

		if err := closeTop(dest); err != nil {
			return undo(wrapError(CodeDisk, err, dest))
		}
	}

	for _, point := range []string{PointC1After, PointC2Before} {
		if err := step(point); err != nil {
			return undo(err)
		}
	}

	if err := s.writeRecord(lk, by); err != nil {
		// The write may have landed before it reported the failure; the tree is removed only when
		// the record is known not to refer to it.
		if refers, known := s.recordRefers(); known && refers {
			return nil
		}

		return undo(err)
	}

	return nil
}

// closeTop makes the top directory of a tree read-only, after the rename.
//
// Parameters:
//   - dest: the directory of the tree
func closeTop(dest string) error {
	parent, err := os.OpenRoot(filepath.Dir(dest))
	if err != nil {
		return err
	}

	defer func() { _ = parent.Close() }()

	return parent.Chmod(filepath.Base(dest), treeDirMode)
}

// recordRefers reads the lock again and tells whether the record of the plugin points at the
// staged tree.
//
// Returns whether it does, and whether the answer is known: a lock that cannot be read leaves it
// unknown, and nothing is deleted then.
func (s *Staged) recordRefers() (refers, known bool) {
	lk, err := OpenLock(s.r.opts.Paths.LockFile())
	if err != nil || lk.Err() != nil {
		return false, false
	}

	rec, found := lk.Get(s.pm.m.ID)

	return found && rec.TreeSHA256 == s.tree.SHA256, true
}

// writeRecord is C2, the one write that switches the plugin.
//
// A new plugin is created and never replaces a record. An update is compared with the record the
// Diff was computed against inside the write, and keeps what the user set since the install: the
// time of the install, the kill switch and, when no consent was asked, the consent. The failure
// counters and the quarantine are cleared, because a new version starts clean.
//
// Parameters:
//   - lk: the lock
//   - by: consented_by to record, empty to keep the consent on record
func (s *Staged) writeRecord(lk *Lock, by string) error {
	m := s.pm.m
	now := s.r.now().Format(lockTimeFormat)

	archive := ""
	if s.sp.Kind == KindRelease {
		archive = s.archive
	}

	next := LockRecord{
		ID: m.ID, Version: m.Version, Tier: m.Tier, Kind: s.sp.Kind,
		Source: s.recordSource(), Origin: s.origin, ArchiveSHA256: archive,
		TreeSHA256:    s.tree.SHA256,
		SurfaceSHA256: s.diff.SurfaceSHA256, InstalledAt: now, UpdatedAt: now,
		Caps: slices.Clone(s.diff.Caps), ConsentedAt: now, ConsentedBy: by,
	}

	if s.base == nil {
		return writeError(lk.Create(next))
	}

	return writeError(lk.Change(m.ID, func(cur *LockRecord) error {
		if !sameBase(*cur, *s.base) {
			return errStaleBase
		}

		next.InstalledAt, next.Disabled = cur.InstalledAt, cur.Disabled

		if by == "" {
			next.ConsentedAt, next.ConsentedBy = cur.ConsentedAt, cur.ConsentedBy
		}

		*cur = next

		return nil
	}))
}

// errStaleBase tells that the record changed between the plan and the write.
var errStaleBase = errors.New("lock record changed")

// writeError maps the error of a lock write to a code.
//
// Parameters:
//   - err: error of Create or Change
func writeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errStaleBase), errors.Is(err, store.ErrExists),
		errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrNotFound):
		return wrapError(CodeStale, err)
	case errors.Is(err, store.ErrBusy):
		return wrapError(CodeBusy, err)
	case errors.Is(err, store.ErrReadOnly):
		return wrapError(CodeStoreCorrupt, err)
	}

	var pe *fs.PathError

	path := ""
	if errors.As(err, &pe) {
		path = pe.Path
	}

	return wrapError(CodeDisk, err, path)
}

// finish runs the steps after the switch. Nothing here can undo the install, so a failure is a
// warning or a leftover, never an error.
//
// Parameters:
//   - s: the committed package
//   - res: result being built
func (r *Registry) finish(s *Staged, res *InstallResult) {
	warn := func(point string, err error) {
		res.Warnings = append(res.Warnings, Reason{ReasonStepFailed, []string{point, errText(err)}})
	}

	id := s.pm.m.ID

	if err := step(PointC2After); err != nil {
		warn(PointC2After, err)
	}

	if err := step(PointC3Before); err != nil {
		warn(PointC3Before, err)
	}

	res.Warnings = append(res.Warnings, r.ensurePluginDirs(id)...)

	if err := step(PointC3After); err != nil {
		warn(PointC3After, err)
	}

	if err := step(PointC4Before); err != nil {
		warn(PointC4Before, err)
	}

	if s.base != nil && TreeID(s.base.TreeSHA256) != s.tree.ID() {
		if left := r.retire(s.pd, id, TreeID(s.base.TreeSHA256)); left != "" {
			res.Leftovers = append(res.Leftovers, left)
		}
	}

	if err := step(PointC4After); err != nil {
		warn(PointC4After, err)
	}

	if p, ok := r.Recheck(id); ok {
		res.State, res.Reasons = p.State, p.Reasons
	}
}

// ensurePluginDirs is C3: it creates the state and cache directories of a plugin, mode 0700. The
// directories belong to the plugin, which can write there, so an existing entry is looked at and
// never changed.
//
// Parameters:
//   - id: plugin id
func (r *Registry) ensurePluginDirs(id string) []Reason {
	var warnings []Reason

	for _, get := range []func(string) (string, error){
		r.opts.Paths.PluginStateDir,
		r.opts.Paths.PluginCacheDir,
	} {
		dir, err := get(id)
		if err != nil {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
			warnings = append(warnings, Reason{ReasonPluginDir, []string{dir, errText(err)}})

			continue
		}

		err = os.Mkdir(dir, 0o700)

		switch {
		case err == nil:
		case errors.Is(err, fs.ErrExist):
			if info, lerr := os.Lstat(dir); lerr != nil || !info.IsDir() {
				warnings = append(warnings,
					Reason{ReasonPluginDir, []string{dir, "not_a_directory"}})
			}
		default:
			warnings = append(warnings, Reason{ReasonPluginDir, []string{dir, errText(err)}})
		}
	}

	return warnings
}

// retire is C4: the previous tree is moved into staging and deleted. A failure leaves an entry
// that the next cleanup removes.
//
// Parameters:
//   - pd: the plugins directory
//   - id: plugin id
//   - tree: name of the previous tree
//
// Returns the path that is left, empty when everything was deleted.
func (r *Registry) retire(pd pluginsDir, id, tree string) string {
	if _, err := r.opts.Paths.PluginTree(id, tree); err != nil {
		return ""
	}

	old := filepath.Join(pd.Real, id, tree)

	info, err := os.Lstat(old)
	if err != nil {
		return ""
	}

	if info.IsDir() {
		// Moving a directory to another parent needs write permission on it.
		if parent, perr := os.OpenRoot(filepath.Dir(old)); perr == nil {
			_ = parent.Chmod(tree, 0o700)
			_ = parent.Close()
		}
	}

	trash := filepath.Join(pd.Staging, randName(trashPrefix))

	if err := os.Rename(old, trash); err != nil {
		return old
	}

	if err := removeTree(trash); err != nil {
		return trash
	}

	return ""
}
