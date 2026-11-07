package plugin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/store"
)

// removeGrace is how long remove waits for the processes of a plugin to stop.
const removeGrace = 3 * time.Second

// RemoveOptions are the options of Remove: Purge also deletes the settings file and the state,
// cache and host directories of the plugin.
type RemoveOptions struct {
	Purge bool
}

// RemoveResult is what Remove did.
//
// Killed is the number of process groups that were signalled. Leftovers are the paths that could
// not be deleted; they wait for the next cleanup and are not an error of the removal. Kept lists
// what a removal without Purge left in place: the settings file and the state and cache
// directories, by kind. Link tells that the plugin entry was a symbolic link: it was unlinked,
// the directory it pointed to was not touched. ConfigFile is the path of config.toml when the
// enabled = false of the plugin could not be cleared because of the layout of the file.
type RemoveResult struct {
	Killed     int
	Leftovers  []string
	Kept       []string
	Link       bool
	ConfigFile string
}

// Remove deletes an installed plugin. The id is checked against the grammar before any file
// system call, so ../data and a/b fail with bad_id and nothing is touched: the shell version of
// this verb deleted whatever the argument named.
//
// The steps, under the plugin mutex (which waits up to 10 seconds, then busy):
//
//  1. The disabled flag of the record is set, so that no launch can start from now on, and the
//     processes of the plugin are stopped, as Disable does, waiting up to 3 seconds.
//  2. The lock record is deleted, which makes the plugin unrunnable at once.
//  3. <Data>/plugins/<id> is looked at with Lstat inside the plugins directory resolved with
//     EvalSymlinks, so it is a direct child of a directory the host trusts. It is renamed into
//     the staging directory, and the renamed entry is deleted by removeTree, which never follows a
//     link and never leaves the entry. A symbolic link in the place of the directory is unlinked,
//     not followed. A deletion that fails leaves the entry in staging for the next cleanup.
//  4. The enabled = false of [plugins.<id>] in config.toml goes, purge or not: a plugin that is
//     installed again is a new install, with a new consent, and starts enabled. The table is
//     edited, not deleted, so an empty header can stay.
//  5. With Purge, the settings file and the state, cache and host directories (each the direct
//     child of its resolved parent, deleted the same way) go too.
//
// The state that decides is read under the mutex, after it was taken: an install that commits at
// the same time cannot be seen half done, and the record it wrote is the one that is deleted.
//
// The settings file belongs to the user and may be a link into a dotfiles repository: the link is
// unlinked and the repository file stays.
//
// Parameters:
//   - id: plugin id
//   - o: options
//
// Returns the result, or an *Error: bad_id, from_plugin, not_installed, busy, store_corrupt when
// the lock cannot be written, disk.
func (r *Registry) Remove(id string, o RemoveOptions) (RemoveResult, error) {
	var res RemoveResult

	if err := config.ValidPluginID(id); err != nil {
		return res, wrapError(CodeBadID, err, id)
	}

	if err := r.guardChange(); err != nil {
		return res, err
	}

	// A first look decides whether there is anything to take the mutex for, and whether the
	// plugins directory may be created for it. It decides nothing else.
	lk, err := r.openLockForWrite()
	if err != nil {
		return res, err
	}

	_, known := lk.Get(id)
	known = known || lk.Invalid()[id] != nil

	pd, err := r.resolvePlugins(known)
	if err != nil {
		if known {
			return res, err
		}

		return res, newError(CodeNotInstalled, id)
	}

	if !known && !entryExists(filepath.Join(pd.Real, id)) {
		return res, newError(CodeNotInstalled, id)
	}

	mu, err := r.lockChanges(pd, mutexWait)
	if err != nil {
		return res, err
	}

	defer mu.release()

	lk, err = r.openLockForWrite()
	if err != nil {
		return res, err
	}

	_, installed := lk.Get(id)
	invalid := lk.Invalid()[id] != nil

	if !installed && !invalid && !entryExists(filepath.Join(pd.Real, id)) {
		return res, newError(CodeNotInstalled, id)
	}

	// The kill switch goes first: a launch that registers its process while the others are being
	// stopped then fails its own verification instead of outliving the removal. If the record
	// cannot be written the removal goes on; the deletion below is the stronger switch.
	if installed || invalid {
		_ = lk.SetDisabled(id, true)
	}

	_ = installHook(PointRemoveStop)

	res.Killed, _ = r.StopPlugin(id, removeGrace)

	if installed || invalid {
		if err := lk.Delete(id); err != nil && !errors.Is(err, store.ErrNotFound) {
			return res, writeError(err)
		}
	}

	r.removeEntry(pd, id, &res)

	// The switch belongs to the installation that is gone, not to the settings that stay.
	_ = store.PatchTable(r.opts.Paths.ConfigFile(), pluginTable(id), nil, []string{"enabled"})

	if !r.configEnabled(id) {
		res.ConfigFile = r.opts.Paths.ConfigFile()
	}

	if o.Purge {
		r.purge(id, &res)
	} else {
		res.Kept = r.keptKinds(id)
	}

	return res, nil
}

// entryExists reports whether a path exists, without following a link.
//
// Parameters:
//   - path: path to look at
func entryExists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

// removeEntry moves <plugins>/<id> into staging and deletes it.
//
// Parameters:
//   - pd: the plugins directory
//   - id: plugin id
//   - res: result being built
func (r *Registry) removeEntry(pd pluginsDir, id string, res *RemoveResult) {
	path := filepath.Join(pd.Real, id)

	info, err := os.Lstat(path)
	if err != nil {
		return
	}

	res.Link = info.Mode()&fs.ModeSymlink != 0

	if res.Link {
		// A link is the entry; its target is somebody else's.
		if err := os.Remove(path); err != nil {
			res.Leftovers = append(res.Leftovers, path)
		}

		return
	}

	if err := r.resolveStaging(pd); err != nil {
		res.Leftovers = append(res.Leftovers, path)

		return
	}

	trash := filepath.Join(pd.Staging, randName(trashPrefix))

	if err := os.Rename(path, trash); err != nil {
		res.Leftovers = append(res.Leftovers, path)

		return
	}

	if err := removeTree(trash); err != nil {
		res.Leftovers = append(res.Leftovers, trash)
	}
}

// resolveStaging makes sure the staging directory exists and can be trusted.
//
// Parameters:
//   - pd: the plugins directory
func (r *Registry) resolveStaging(pd pluginsDir) error {
	if err := os.Mkdir(pd.Staging, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return wrapError(CodeDisk, err, pd.Staging)
	}

	return r.checkDir(pd.Staging)
}

// purge deletes what remove keeps: the settings file and the state, cache and host directories.
//
// Parameters:
//   - id: plugin id
//   - res: result being built
func (r *Registry) purge(id string, res *RemoveResult) {
	p := r.opts.Paths

	targets := []struct {
		dir    string
		suffix string
	}{
		{filepath.Join(p.Config, "plugins"), ".toml"},
		{filepath.Join(p.State, "plugins"), ""},
		{filepath.Join(p.Cache, "plugins"), ""},
		{filepath.Join(p.State, "plugins-host"), ""},
	}

	for _, t := range targets {
		parent, err := filepath.EvalSymlinks(t.dir)
		if err != nil {
			continue
		}

		path := filepath.Join(parent, id+t.suffix)
		if err := removeTree(path); err != nil {
			res.Leftovers = append(res.Leftovers, path)
		}
	}
}

// keptKinds lists what exists for a plugin and that remove leaves in place.
//
// Parameters:
//   - id: plugin id
func (r *Registry) keptKinds(id string) []string {
	p := r.opts.Paths

	var out []string

	for _, k := range []struct {
		kind string
		get  func(string) (string, error)
	}{
		{LeftSettings, p.PluginSettingsFile},
		{LeftState, p.PluginStateDir},
		{LeftCache, p.PluginCacheDir},
		{LeftHost, p.PluginHostDir},
	} {
		if path, err := k.get(id); err == nil && entryExists(path) {
			out = append(out, k.kind)
		}
	}

	return out
}
