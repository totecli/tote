package plugin

import (
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/totecli/tote/internal/config"
)

// Limits of the enumeration of installed plugins.
const (
	maxPluginDirs = 1024
	loadWorkers   = 4
	maxReasonArgs = 8
	officialOwner = "totecli/"
	manifestName  = "plugin.toml"
)

// Options configures a Registry.
//
// Paths are the directories of tote. Host describes the running tote (version, platform, announced
// features), see NewHost. NoPlugins is the session switch (--no-plugins or TOTE_NO_PLUGINS): every
// plugin is then disabled for this run. Lookup reads the environment, nil meaning os.LookupEnv; it
// supplies PATH and the TOTE_PLUGIN_* settings. Home expands a leading "~" of a path setting. Cwd
// is the working directory, whose own files no program lookup may return (see Avoid); empty means
// os.Getwd at Load. Getuid returns the user id that must own every installed tree, nil meaning
// os.Getuid. Bin is the absolute path of the tote binary, given to programs as TOTE_BIN. Bundled
// is the set of plugins shipped with tote, nil when there is none. Now is the clock of the
// records the registry writes, nil meaning time.Now. Transport carries the downloads of release
// plugins, nil meaning the transport of section 7.2 (system roots, TLS 1.2, proxy from the
// environment); tests inject one that reaches an httptest server.
type Options struct {
	Paths     config.Paths
	Host      Host
	NoPlugins bool
	Lookup    func(string) (string, bool)
	Home      string
	Cwd       string
	Getuid    func() int
	Bin       string
	Bundled   BundledSet
	Now       func() time.Time
	Transport RoundTripper
}

// Registry reads the installed plugins and decides their state. It keeps only a cache of what it
// verified, in memory: the lock, config.toml and the trees are read again by every Load.
//
// A Registry is safe for use by several goroutines. Two Registries over the same directories see
// each other's changes through the files, which is how a disable made by one stops the other.
type Registry struct {
	opts  Options
	mu    sync.Mutex
	cache map[string]*treeCache
	rt    *runState
}

// treeCache is what the registry verified for one tree directory: the scan whose identities are
// the fingerprint, the hash and, once the hash matched the lock, the manifest read from those
// bytes.
type treeCache struct {
	scan   *treeScan
	tree   *Tree
	parsed *parsedManifest
}

// parsedManifest is the result of reading the manifest of a verified tree.
type parsedManifest struct {
	official bool
	m        *Manifest
	rep      Report
	catalog  Catalog
	warnings []Reason
	pkg      []Issue
}

// Open creates a registry. It does no I/O. The base name of Options.Bin is added to the names
// that exec: refuses, so that the host cannot forget to declare its own name.
//
// Parameters:
//   - o: options, see Options
func Open(o Options) *Registry {
	if base := filepath.Base(o.Bin); o.Bin != "" && !slices.Contains(o.Host.Self, base) {
		o.Host.Self = append(slices.Clone(o.Host.Self), base)
	}

	return &Registry{opts: o, cache: map[string]*treeCache{}, rt: newRunState()}
}

// Plugin is an installed plugin as one Load saw it.
//
// Installed tells that a valid lock record exists, Record being the zero value otherwise. Header
// is the identity read leniently from the manifest and Manifest the validated manifest, nil unless
// the tree was verified against its pinned hash and the manifest is valid and compatible. Issues
// are the findings of the manifest, warnings included. Dir is the tree directory, Caps the
// effective capabilities, Surface the surface_sha256 computed from the manifest and Grant the
// token that --grant must equal. Missing and Absent are the required and the optional programs
// that were not found. Warnings are findings that do not change the state. Stale lists the
// directories of the plugin that are not its current tree. Nothing in a Plugin may be modified.
type Plugin struct {
	ID        string
	State     State
	Reasons   []Reason
	Warnings  []Reason
	Installed bool
	Record    LockRecord
	Header    Header
	Manifest  *Manifest
	Issues    []Issue
	Catalog   Catalog
	Dir       string
	Caps      []string
	Surface   string
	Grant     string
	Missing   []Requires
	Absent    []Requires
	Stale     []string
	facts     Facts
}

// Kinds of directory entry that no plugin owns.
const (
	OrphanName   = "name"
	OrphanFile   = "file"
	OrphanRecord = "record"
)

// Orphan is an entry that is not a plugin: a name in the plugins directory outside the grammar of
// ids, a file there, or a lock record whose id is outside the grammar. Name is cleaned.
type Orphan struct {
	Kind string
	Name string
}

// Snapshot is the state of every installed plugin at the moment of a Load, sorted by id. It is a
// value: later changes on disk are seen by the next Load, never by an existing snapshot.
//
// Problems are the failures that concern every plugin and make them all broken, such as a lock or
// a config.toml that does not parse.
type Snapshot struct {
	plugins  []Plugin
	index    map[string]int
	Orphans  []Orphan
	Problems []Reason
}

// Plugins returns the plugins sorted by id, as a copy of the list.
func (s Snapshot) Plugins() []Plugin { return append([]Plugin(nil), s.plugins...) }

// Get returns the plugin with an id.
//
// Parameters:
//   - id: plugin id
func (s Snapshot) Get(id string) (Plugin, bool) {
	i, ok := s.index[id]
	if !ok {
		return Plugin{}, false
	}

	return s.plugins[i], true
}

// Enabled returns the plugins whose state is enabled, the only ones that contribute.
func (s Snapshot) Enabled() []Plugin {
	var out []Plugin

	for _, p := range s.plugins {
		if p.State.Runnable() {
			out = append(out, p)
		}
	}

	return out
}

// Counts summarises a snapshot.
type Counts struct {
	Total     int
	Enabled   int
	Attention int
}

// Count returns the number of plugins, of enabled ones and of those that need attention: every
// state except enabled and disabled, because a disabled plugin is what the user asked for.
func (s Snapshot) Count() Counts {
	c := Counts{Total: len(s.plugins)}

	for _, p := range s.plugins {
		switch p.State {
		case StateEnabled:
			c.Enabled++
		case StateDisabled:
		default:
			c.Attention++
		}
	}

	return c
}

// loadContext is what one Load or Recheck reads once and shares between the plugins.
type loadContext struct {
	cfg     config.Settings
	cfgErr  error
	lockErr error
	records map[string]LockRecord
	invalid map[string]error
	lockIDs []string
	uid     uint32
	pathEnv string
	avoid   Avoid
	mu      sync.Mutex
	seen    map[string]bool
}

// Load reads config.toml, plugins.lock and the plugins directory, verifies and hashes every
// installed tree, reads the manifests from the hashed bytes and decides the state of every
// plugin. It runs nothing and starts no process: hashing and looking a program up on PATH are
// reads. Every call reads the files again; only the hash of a tree and the manifest read from it
// are reused, while the size, owner, mode, times and inode of every entry stay as they were.
//
// A lock or a config.toml that does not parse fails closed: every plugin on disk is broken and
// Problems says why. The work for each plugin runs in parallel, which keeps the start-up budget
// of 20 ms for three plugins.
func (r *Registry) Load() Snapshot {
	ctx := r.context()

	ids, orphans := r.enumerate(ctx)

	plugins := make([]Plugin, len(ids))
	sem := make(chan struct{}, loadWorkers)

	var wg sync.WaitGroup

	for i, id := range ids {
		wg.Add(1)

		sem <- struct{}{}

		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			plugins[i] = r.evaluate(ctx, id)
		}()
	}

	wg.Wait()

	r.mu.Lock()
	for dir := range r.cache {
		if !ctx.seen[dir] {
			delete(r.cache, dir)
		}
	}
	r.mu.Unlock()

	snap := Snapshot{plugins: plugins, index: map[string]int{}, Orphans: orphans}
	for i, p := range plugins {
		snap.index[p.ID] = i
	}

	snap.Problems = ctx.problems()

	return snap
}

// Recheck decides the state of one plugin from files read at this moment. It is the check that
// runs before every decision to start a program: a disable, a remove or an edit made in another
// terminal is seen here, never a stale snapshot.
//
// Parameters:
//   - id: plugin id; an id outside the grammar is never found
//
// Returns the plugin and true, or false when nothing on disk or in the lock carries this id.
func (r *Registry) Recheck(id string) (Plugin, bool) {
	p, _, ok := r.recheck(id)

	return p, ok
}

// recheck is Recheck that also returns the inputs of the pass, which the environment builder
// needs.
//
// Parameters:
//   - id: plugin id
func (r *Registry) recheck(id string) (Plugin, *loadContext, bool) {
	if config.ValidPluginID(id) != nil {
		return Plugin{}, nil, false
	}

	ctx := r.context()

	_, inLock := ctx.records[id]
	if !inLock && ctx.invalid[id] == nil && !r.hasDir(id) {
		return Plugin{}, nil, false
	}

	return r.evaluate(ctx, id), ctx, true
}

// hasDir reports whether the plugins directory holds an entry with this name.
//
// Parameters:
//   - id: plugin id
func (r *Registry) hasDir(id string) bool {
	_, err := os.Lstat(filepath.Join(r.opts.Paths.PluginsDir(), id))

	return err == nil
}

// Settings resolves the effective settings of a plugin: environment, then plugins/<id>.toml, then
// the default. It reads the file and the environment at this moment and writes nothing.
//
// Parameters:
//   - p: plugin with a manifest; a plugin without one has no settings
func (r *Registry) Settings(p Plugin) SettingsView {
	if p.Manifest == nil {
		return SettingsView{}
	}

	file, err := r.opts.Paths.PluginSettingsFile(p.ID)
	if err != nil {
		return SettingsView{}
	}

	return EffectiveSettings(p.Manifest, SettingsInput{
		PluginID: p.ID, File: file,
		Lookup: r.opts.Lookup, Home: r.opts.Home,
	})
}

// context reads the two files every state depends on and fixes the inputs of one pass.
func (r *Registry) context() *loadContext {
	o := r.opts
	ctx := &loadContext{
		records: map[string]LockRecord{}, invalid: map[string]error{},
		seen: map[string]bool{},
	}

	ctx.cfg, ctx.cfgErr = config.LoadSettings(o.Paths.ConfigFile())

	if lock, err := OpenLock(o.Paths.LockFile()); err != nil {
		ctx.lockErr = err
	} else if err := lock.Err(); err != nil {
		ctx.lockErr = err
	} else {
		ctx.lockIDs = lock.IDs()
		ctx.invalid = lock.Invalid()

		for _, rec := range lock.Records() {
			ctx.records[rec.ID] = rec
		}
	}

	uid := os.Getuid
	if o.Getuid != nil {
		uid = o.Getuid
	}

	// A negative id never matches an owner, whatever the platform reports.
	ctx.uid = math.MaxUint32
	if u := uid(); u >= 0 && u <= math.MaxUint32 {
		ctx.uid = uint32(u)
	}

	lookup := o.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}

	ctx.pathEnv, _ = lookup("PATH")

	ctx.avoid = r.avoid()

	return ctx
}

// avoid returns the places a program lookup must never return: the plugins directory with
// everything below it, and the files directly in the working directory.
func (r *Registry) avoid() Avoid {
	o := r.opts

	cwd := o.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	base := o.Paths.PluginsDir()
	avoid := Avoid{Under: []string{base}, Exactly: []string{cwd}}

	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		avoid.Under = append(avoid.Under, resolved)
	}

	return avoid
}

// problems lists the failures that concern every plugin.
func (c *loadContext) problems() []Reason {
	var out []Reason

	if c.lockErr != nil {
		out = append(out, Reason{ReasonLockUnreadable, []string{errText(c.lockErr)}})
	}

	if c.cfgErr != nil {
		out = append(out, Reason{ReasonConfigUnreadable, []string{errText(c.cfgErr)}})
	}

	return out
}

// errText cleans the text of an error so that it can be an argument of a reason.
//
// Parameters:
//   - err: error from a file read, which may quote file content
func errText(err error) string { return CleanLine(err.Error(), 4*issueArgRunes) }

// enumerate lists the ids to evaluate: the records of the lock, valid or not, and the directories
// of the plugins directory whose name is an id.
//
// Parameters:
//   - ctx: inputs of the pass
//
// Returns the ids sorted and unique, and the entries that are not plugins.
func (r *Registry) enumerate(ctx *loadContext) ([]string, []Orphan) {
	set := map[string]bool{}

	var orphans []Orphan

	for _, id := range ctx.lockIDs {
		if config.ValidPluginID(id) == nil {
			set[id] = true
		} else {
			orphans = append(orphans, Orphan{OrphanRecord, CleanLine(id, issueArgRunes)})
		}
	}

	names, _ := listDir(r.opts.Paths.PluginsDir(), maxPluginDirs)

	for _, e := range names {
		switch {
		case strings.HasPrefix(e.Name(), "."):
		case config.ValidPluginID(e.Name()) != nil:
			orphans = append(orphans, Orphan{OrphanName, CleanLine(e.Name(), issueArgRunes)})
		case !e.IsDir() && e.Type()&fs.ModeSymlink == 0:
			orphans = append(orphans, Orphan{OrphanFile, e.Name()})
		default:
			set[e.Name()] = true
		}
	}

	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	return ids, orphans
}

// listDir reads at most limit entries of a directory, in no particular order.
//
// Parameters:
//   - dir: directory to read
//   - limit: largest number of entries to read
//
// Returns the entries, or the error of opening the directory; a missing directory has none.
func listDir(dir string, limit int) ([]fs.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	// A read error after some entries still leaves the entries that were read.
	entries, _ := f.ReadDir(limit)

	return entries, nil
}
