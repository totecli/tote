package plugin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Kinds of Leftover.
const (
	LeftStaging    = "staging"
	LeftTrash      = "trash"
	LeftUnknown    = "unknown"
	LeftTreeOld    = "old_tree"
	LeftTreeOrphan = "orphan_tree"
	LeftSettings   = "settings"
	LeftState      = "state"
	LeftCache      = "cache"
	LeftHost       = "host"
)

// Limits of the housekeeping listings.
const (
	maxStagingEntries = 1024
	maxLeftovers      = 256
)

// Leftover is something on disk that no installed plugin owns, found without reading its content.
//
// Kind is one of the Left constants: an install in progress or abandoned (staging), a tree
// waiting for deletion (trash), an entry of staging that is neither (unknown), a tree that is not
// the current one of an installed plugin (old_tree), a tree of an id with no record (orphan_tree,
// what a crash after C1 leaves), the settings file, state, cache or host directory of an id that
// is not installed. ID is empty for the staging kinds. Name is cleaned. Age is the time since the
// entry was last changed.
type Leftover struct {
	Kind string
	ID   string
	Name string
	Age  time.Duration
}

// CleanupReport is what Cleanup did: the entries it deleted and the ones it could not, both by
// name below the plugins directory, and whether it gave up because another tote was changing
// plugins.
type CleanupReport struct {
	Removed []string
	Failed  []string
	Busy    bool
}

// Leftovers lists what Diagnose reports besides the plugins themselves: the staging directory
// and its trash, the old trees of installed plugins, the orphan trees of ids with no record, and
// the settings, state, cache and host directories of ids that are not installed. It only lists
// directories and takes no lock; it opens no file and runs nothing.
//
// A lock that does not parse leaves the entries that depend on it unclassified: only the staging
// directory is listed then, and the error says why.
//
// Returns the leftovers sorted by kind then name, cut at 256, and an *Error store_corrupt when
// the lock could not be read.
func (r *Registry) Leftovers() ([]Leftover, error) {
	var (
		out []Leftover
		err error
	)

	pd, perr := r.resolvePlugins(false)
	if perr != nil {
		return nil, nil
	}

	out = append(out, r.stagingLeftovers(pd)...)

	installed, lerr := r.installedSet()
	if lerr != nil {
		err = lerr
	} else {
		out = append(out, r.treeLeftovers(pd, installed)...)
		out = append(out, r.dirLeftovers(installed)...)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}

		return out[i].Name < out[j].Name
	})

	if len(out) > maxLeftovers {
		out = out[:maxLeftovers]
	}

	return out, err
}

// installedSet reads the lock and returns the current tree of every valid record by id.
func (r *Registry) installedSet() (map[string]string, error) {
	lk, err := OpenLock(r.opts.Paths.LockFile())
	if err != nil || lk.Err() != nil {
		if err == nil {
			err = lk.Err()
		}

		return nil, wrapError(CodeStoreCorrupt, err, r.opts.Paths.LockFile())
	}

	out := map[string]string{}

	for _, id := range lk.IDs() {
		out[id] = ""
	}

	for _, rec := range lk.Records() {
		out[rec.ID] = TreeID(rec.TreeSHA256)
	}

	return out, nil
}

// stagingLeftovers lists the entries of the staging directory.
//
// Parameters:
//   - pd: the plugins directory
func (r *Registry) stagingLeftovers(pd pluginsDir) []Leftover {
	var out []Leftover

	if r.checkDir(pd.Staging) != nil {
		return nil
	}

	entries, _ := listDir(pd.Staging, maxStagingEntries)
	for _, e := range entries {
		kind := LeftUnknown

		switch {
		case strings.HasPrefix(e.Name(), trashPrefix):
			kind = LeftTrash
		case strings.HasPrefix(e.Name(), stagePrefix):
			kind = LeftStaging
		}

		out = append(out, Leftover{
			Kind: kind, Name: CleanLine(e.Name(), issueArgRunes),
			Age: ageOf(e),
		})
	}

	return out
}

// ageOf returns the time since a directory entry was last changed, 0 when it cannot be told.
//
// Parameters:
//   - e: entry from a directory listing
func ageOf(e fs.DirEntry) time.Duration {
	info, err := e.Info()
	if err != nil {
		return 0
	}

	return time.Since(info.ModTime())
}

// treeLeftovers lists the trees that are not the current tree of an installed plugin.
//
// Parameters:
//   - pd: the plugins directory
//   - installed: current tree of every record by id
func (r *Registry) treeLeftovers(pd pluginsDir, installed map[string]string) []Leftover {
	var out []Leftover

	ids, _ := listDir(pd.Real, maxPluginDirs)
	for _, de := range ids {
		id := de.Name()
		if !validID(id) || !de.IsDir() {
			continue
		}

		current, isInstalled := installed[id]
		if isInstalled && current == "" {
			// The record is invalid, so which tree is current cannot be told.
			continue
		}

		trees, _ := listDir(filepath.Join(pd.Real, id), MaxTreeEntries)

		for _, t := range trees {
			if t.Name() == current && isInstalled {
				continue
			}

			kind := LeftTreeOld
			if !isInstalled {
				kind = LeftTreeOrphan
			}

			out = append(out, Leftover{
				Kind: kind, ID: id,
				Name: CleanLine(t.Name(), issueArgRunes), Age: ageOf(t),
			})
		}
	}

	return out
}

// dirLeftovers lists the settings file and the state, cache and host directories of ids that are
// not installed. A removal without --purge leaves them on purpose; the listing only tells.
//
// Parameters:
//   - installed: ids that have a record
func (r *Registry) dirLeftovers(installed map[string]string) []Leftover {
	p := r.opts.Paths

	var out []Leftover

	for _, d := range []struct {
		kind   string
		dir    string
		suffix string
	}{
		{LeftSettings, filepath.Join(p.Config, "plugins"), ".toml"},
		{LeftState, filepath.Join(p.State, "plugins"), ""},
		{LeftCache, filepath.Join(p.Cache, "plugins"), ""},
		{LeftHost, filepath.Join(p.State, "plugins-host"), ""},
	} {
		entries, _ := listDir(d.dir, maxPluginDirs)
		for _, e := range entries {
			id := strings.TrimSuffix(e.Name(), d.suffix)
			if _, ok := installed[id]; ok || !validID(id) {
				continue
			}

			out = append(out, Leftover{
				Kind: d.kind, ID: id,
				Name: CleanLine(e.Name(), issueArgRunes), Age: ageOf(e),
			})
		}
	}

	return out
}

// Cleanup deletes what a finished or crashed change left behind: the trees in the trash, the
// installs in staging that nothing touched for an hour, and the trees that are not the current
// tree of an installed plugin. Trees of ids with no record are not touched: a retry of the
// install reuses them, and Diagnose lists them. Settings, state and cache are never touched.
//
// It runs under the plugin mutex without waiting, so it never delays a command: when another tote
// holds the mutex the report says Busy. Nothing outside the staging directory and the trees of
// installed plugins is deleted, and every deletion is removeTree.
//
// Returns the report, or an *Error when the plugins directory cannot be used.
func (r *Registry) Cleanup() (CleanupReport, error) {
	pd, err := r.resolvePlugins(false)
	if err != nil {
		return CleanupReport{}, nil
	}

	mu, err := r.lockChanges(pd, 0)
	if err != nil {
		var pe *Error
		if errors.As(err, &pe) && pe.Code == CodeBusy {
			return CleanupReport{Busy: true}, nil
		}

		return CleanupReport{}, err
	}

	defer mu.release()

	return r.sweep(pd), nil
}

// tidy is Cleanup for the start of a change: best effort, silent.
//
// Parameters:
//   - pd: the plugins directory
func (r *Registry) tidy(pd pluginsDir) {
	mu, err := r.lockChanges(pd, 0)
	if err != nil {
		return
	}

	defer mu.release()

	r.sweep(pd)
}

// sweep deletes the leftovers. The caller holds the mutex.
//
// Parameters:
//   - pd: the plugins directory
func (r *Registry) sweep(pd pluginsDir) CleanupReport {
	var rep CleanupReport

	drop := func(path string) {
		name, _ := filepath.Rel(pd.Real, path)

		if err := removeTree(path); err != nil {
			rep.Failed = append(rep.Failed, name)

			return
		}

		rep.Removed = append(rep.Removed, name)
	}

	if r.checkDir(pd.Staging) == nil {
		entries, _ := listDir(pd.Staging, maxStagingEntries)
		for _, e := range entries {
			path := filepath.Join(pd.Staging, e.Name())

			switch {
			case strings.HasPrefix(e.Name(), trashPrefix):
				drop(path)
			case strings.HasPrefix(e.Name(), stagePrefix) && ageOf(e) > staleStage:
				drop(path)
			}
		}
	}

	lk, err := OpenLock(r.opts.Paths.LockFile())
	if err != nil || lk.Err() != nil {
		return rep
	}

	for _, rec := range lk.Records() {
		r.sweepTrees(pd, rec, drop)
	}

	return rep
}

// sweepTrees deletes the trees of one installed plugin that are not its current tree.
//
// Parameters:
//   - pd: the plugins directory
//   - rec: the record
//   - drop: deletes a path and reports it
func (r *Registry) sweepTrees(pd pluginsDir, rec LockRecord, drop func(string)) {
	idDir := filepath.Join(pd.Real, rec.ID)

	if info, err := os.Lstat(idDir); err != nil || !info.IsDir() {
		return
	}

	trees, _ := listDir(idDir, MaxTreeEntries)
	for _, t := range trees {
		if t.Name() == TreeID(rec.TreeSHA256) {
			continue
		}

		// Only a tree name is deleted; any other entry is somebody's and stays.
		if _, err := r.opts.Paths.PluginTree(rec.ID, t.Name()); err == nil {
			drop(filepath.Join(idDir, t.Name()))
		}
	}
}
