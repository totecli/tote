package plugin

import (
	"context"
	"os"
	"slices"
	"sync"

	"github.com/totecli/tote/internal/config"
)

// Stable codes of the install, update, enable, disable and remove verbs. Like the codes of
// errors.go they name a failure, never a message: the host renders the text and maps the code to
// an exit code with ExitClass.
const (
	CodeNotInstalled      = "not_installed"
	CodeBadSource         = "bad_source"
	CodeSourceUnsupported = "source_unsupported"
	CodeSourceRequired    = "source_required"
	CodeNoEmbedded        = "no_embedded"
	CodeSourceUnsafe      = "source_unsafe"
	CodeSourceLimits      = "source_limits"
	CodeSourceRead        = "source_read"
	CodeManifestInvalid   = "manifest_invalid"
	CodeSourceChanged     = "source_changed"
	CodeDowngrade         = "downgrade"
	CodePrerelease        = "prerelease"
	CodeConsentNeeded     = "consent_needed"
	CodeGrantMismatch     = "grant_mismatch"
	CodeTreeCollision     = "tree_collision"
	CodeStoreCorrupt      = "store_corrupt"
	CodeStoreWritable     = "store_writable"
	CodeEnableRefused     = "enable_refused"
	CodeFromPlugin        = "from_plugin"
	CodeDisk              = "disk"
	CodeIDMismatch        = "id_mismatch"
)

// InstallRequest asks for an install or an update.
//
// Source is what the user typed: a directory or an archive file (it starts with /, ./, ../ or
// ~/), the bare id of a bundled plugin, a release URL of github.com or the owner/repo@tag
// shorthand. ID is the plugin an update is for; with Update set and no Source, a local plugin is
// read again from the path on record and a bundled one from the embedded copy, while a release
// plugin needs a Source (source_required). Update makes a missing plugin an error
// (not_installed); without it, an id that is already installed is updated all the same, as
// section 7.4 says. Force allows what the update rules refuse: another origin, a version that is
// not newer and a pre-release. SHA256 is the pinned digest of the archive: a release needs it, for
// an install and for an update (hash_required, hash_malformed), an archive file checks it when
// given, and a directory and a bundled plugin have no archive, so it is refused for them. Lang
// picks the language of the title and the description in the diff when the package translates
// them; empty gives English.
type InstallRequest struct {
	Source string
	ID     string
	SHA256 string
	Lang   string
	Update bool
	Force  bool
}

// lang gives the language of the texts of a request: English unless the request names another.
func (req InstallRequest) lang() string {
	if req.Lang == "" {
		return "en"
	}

	return req.Lang
}

// Staged is a package that was copied, validated and hashed in the staging directory and waits
// for the user's decision. It holds nothing but its staging directory, which Commit turns into
// the installed tree and Abort deletes. It is safe for use by several goroutines.
type Staged struct {
	r       *Registry
	pd      pluginsDir
	mu      sync.Mutex
	dir     string
	spent   bool
	tree    *Tree
	pm      *parsedManifest
	sp      sourceSpec
	origin  string
	archive string
	base    *LockRecord
	same    *Plugin
	lang    string
	diff    Diff
	fresh   []string
}

// Diff returns what the install will do, for the consent view or for --dry-run. It reads nothing
// from disk.
func (s *Staged) Diff() Diff {
	d := s.diff
	d.Caps = slices.Clone(d.Caps)

	return d
}

// validID reports whether a text is inside the grammar of plugin ids.
//
// Parameters:
//   - s: candidate
func validID(s string) bool { return config.ValidPluginID(s) == nil }

// guardChange refuses a change of the installed plugins from inside a plugin: when the variable
// TOTE_PLUGIN_ID is set, or when the process runs in a session recorded in plugin-procs. It is a
// guard against an honest or sloppy plugin and against a script's accident, not a boundary: a
// descendant that called setsid again has left the session, and the variable can be removed.
func (r *Registry) guardChange() error {
	if v, ok := r.lookup("TOTE_PLUGIN_ID"); ok && v != "" {
		return newError(CodeFromPlugin)
	}

	if InPluginSession(r.opts.Paths) {
		return newError(CodeFromPlugin)
	}

	return nil
}

// openLockForWrite opens plugins.lock for a change. A lock that does not parse fails closed:
// nothing is installed over a record that cannot be read.
func (r *Registry) openLockForWrite() (*Lock, error) {
	path := r.opts.Paths.LockFile()

	lk, err := OpenLock(path)
	if err != nil {
		return nil, wrapError(CodeStoreCorrupt, err, path)
	}

	if err := lk.Err(); err != nil {
		return nil, wrapError(CodeStoreCorrupt, err, path)
	}

	return lk, nil
}

// Plan copies a package into staging, validates it as Load validates an installed one, computes
// its hashes and capabilities, applies the update rules and builds the Diff. It changes nothing
// that is installed and runs nothing: the files are read, hashed and parsed.
//
// A directory and a bundled plugin are copied by os.Root with the rules of section 7.3 (regular
// files and directories only, names and sizes limited). An archive file and a release are read
// into a file of the staging directory while they are hashed; a release is downloaded from
// github.com only. The digest is compared with the pin before anything is parsed, and the archive
// is then extracted with the same rules. The hash and the consent are those of the copy, and a
// source that changes afterwards changes nothing. Plan does not hold the plugin mutex while the
// user decides: Commit takes it and checks again that the installed record is still the one the
// Diff was computed against, which keeps a prompt that stays open from blocking every other
// change.
//
// Parameters:
//   - ctx: cancels the copy
//   - req: what to install
//
// Returns the staged package, which the caller must Commit or Abort, or an *Error: bad_source,
// hash_required, hash_malformed, hash_mismatch, platform_hash, tag_mismatch, the net_* codes,
// too_large, origin_redirect, archive_format, archive_unsafe, archive_limits, no_embedded,
// source_unsafe, source_limits, manifest_invalid or the code of the first manifest error,
// source_changed, downgrade, prerelease, not_installed, store_corrupt, tree_hash for a bundled
// plugin that does not match its pin, from_plugin, disk. Whatever the failure, the staging
// directory and the downloaded file are deleted.
func (r *Registry) Plan(ctx context.Context, req InstallRequest) (*Staged, error) {
	if err := r.guardChange(); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lk, err := r.openLockForWrite()
	if err != nil {
		return nil, err
	}

	sp, err := r.resolveSource(req, lk)
	if err != nil {
		return nil, err
	}

	if err := sp.setPin(req.SHA256); err != nil {
		return nil, err
	}

	fresh := r.missingStore()

	pd, err := r.resolvePlugins(true)
	if err != nil {
		dropDirs(fresh)

		return nil, err
	}

	// A machine that never installed a plugin has nothing to sweep, and sweeping would create the
	// mutex file, which cannot be removed again without a race.
	if len(fresh) == 0 {
		r.tidy(pd)
	}

	dir, err := newStage(pd)
	if err != nil {
		dropDirs(fresh)

		return nil, err
	}

	st, err := r.stage(ctx, req, sp, pd, dir, lk)
	if err != nil {
		_ = removeTree(dir)

		dropDirs(fresh)

		return nil, err
	}

	st.fresh = fresh

	return st, nil
}

// resolveSource turns a request into a classified source. An update with no source reads it from
// the record.
//
// Parameters:
//   - req: the request
//   - lk: the lock
func (r *Registry) resolveSource(req InstallRequest, lk *Lock) (sourceSpec, error) {
	text := req.Source

	if req.ID != "" && !validID(req.ID) {
		return sourceSpec{}, newError(CodeBadID, req.ID)
	}

	// An update of a plugin that is not there is refused before anything is read or downloaded.
	if _, ok := lk.Get(req.ID); req.Update && req.ID != "" && !ok && lk.Invalid()[req.ID] == nil {
		return sourceSpec{}, newError(CodeNotInstalled, req.ID)
	}

	if text == "" {
		if !req.Update || req.ID == "" {
			return sourceSpec{}, newError(CodeBadSource, "")
		}

		rec, ok := lk.Get(req.ID)
		if !ok {
			return sourceSpec{}, newError(CodeNotInstalled, req.ID)
		}

		switch rec.Kind {
		case KindLocal:
			text = rec.Source
		case KindBundled:
			text = rec.ID
		default:
			return sourceSpec{}, newError(CodeSourceRequired, req.ID, rec.Source)
		}
	}

	sp, err := r.classify(text)
	if err != nil {
		return sp, err
	}

	if sp.Kind == KindBundled && req.ID != "" && req.ID != sp.ID {
		return sp, newError(CodeIDMismatch, req.ID, sp.ID)
	}

	return sp, nil
}

// bundledPin is the compiled-in pin of a bundled plugin.
type bundledPin struct {
	version string
	tree    string
	surface string
}

// stage fills a staging directory from the source and inspects what was copied.
//
// Parameters:
//   - ctx: cancels the copy
//   - req: the request
//   - sp: the classified source
//   - pd: the plugins directory
//   - dir: the staging directory, empty
//   - lk: the lock
func (r *Registry) stage(ctx context.Context, req InstallRequest, sp sourceSpec, pd pluginsDir,
	dir string, lk *Lock,
) (*Staged, error) {
	dst, err := os.OpenRoot(dir)
	if err != nil {
		return nil, wrapError(CodeDisk, err, dir)
	}

	defer func() { _ = dst.Close() }()

	want := req.ID
	if sp.Kind == KindBundled {
		want = sp.ID
	}

	// A bundled copy may use any official id. A release may use one only when its repository is
	// totecli/<id> for that very id, which Decode checks against the id of the manifest it reads
	// (DecodeOptions.Origin): the id is not known here when the request carries none.
	official := sp.Kind == KindBundled

	pin, archive, err := r.stageSource(ctx, sp, dir, dst, want, official)
	if err != nil {
		return nil, err
	}

	if err := installHook(PointStaged); err != nil {
		return nil, wrapError(CodeDisk, err, PointStaged)
	}

	// An update of an installed plugin keeps the id it has even if a later release reserved it,
	// except an official id, which only the shipped copy and the releases of totecli may use.
	_, known := lk.Get(want)
	installed := known && ReservedID(want) != ReservedOfficial

	tree, pm, err := r.inspect(dst, dir, want, official, sp.Origin, installed)
	if err != nil {
		return nil, err
	}

	if sp.Kind == KindRelease && pm.m.Version != sp.Ref.Tag {
		return nil, newError(CodeTagMismatch, sp.Ref.Tag, pm.m.Version)
	}

	if pin != nil && (pm.m.Version != pin.version || tree.SHA256 != pin.tree ||
		SurfaceSHA256(pm.m) != pin.surface) {
		return nil, newError(CodeTreeHash, KindBundled, pm.m.ID)
	}

	st := &Staged{
		r: r, pd: pd, dir: dir, tree: tree, pm: pm, sp: sp, origin: sp.Origin,
		archive: archive, lang: req.lang(),
	}

	if err := st.compare(lk, req); err != nil {
		return nil, err
	}

	return st, nil
}

// stageSource puts the files of the source into the staging directory.
//
// Parameters:
//   - ctx: cancels the copy, the download and the extraction
//   - sp: the classified source
//   - dir: the staging directory
//   - dst: the staging directory, opened as a root
//   - want: id the manifest must carry, empty for any
//   - official: true when the plugin may use an official id
//
// Returns the pin of a bundled plugin (nil for the other sources) and the digest of the archive
// (empty for a directory and a bundled plugin), or an *Error.
func (r *Registry) stageSource(ctx context.Context, sp sourceSpec, dir string, dst *os.Root,
	want string, official bool,
) (*bundledPin, string, error) {
	if sp.Kind == KindRelease || sp.Archive {
		sum, err := r.unpackSource(ctx, sp, dir, dst, want, official)

		return nil, sum, err
	}

	src, pin, closeSrc, err := r.openSource(sp)
	if err != nil {
		return nil, "", err
	}

	defer closeSrc()

	if err := (&copier{ctx: ctx, src: src, dst: dst}).run(); err != nil {
		return nil, "", err
	}

	return pin, "", nil
}

// openSource opens the files to copy.
//
// Parameters:
//   - sp: the classified source
//
// Returns the tree of files, the pin of a bundled plugin (nil for a directory), a function that
// releases the source, or an *Error.
func (r *Registry) openSource(sp sourceSpec) (srcTree, *bundledPin, func(), error) {
	switch sp.Kind {
	case KindLocal:
		root, err := os.OpenRoot(sp.Path)
		if err != nil {
			return nil, nil, nil, wrapError(CodeBadSource, err, sp.Text)
		}

		return dirSource{root: root}, nil, func() { _ = root.Close() }, nil
	case KindBundled:
		if r.opts.Bundled == nil {
			return nil, nil, nil, newError(CodeNoEmbedded, sp.ID)
		}

		files, version, tree, surface, ok := r.opts.Bundled.Lookup(sp.ID)
		if !ok || files == nil {
			return nil, nil, nil, newError(CodeNoEmbedded, sp.ID)
		}

		return fsSource{fsys: files}, &bundledPin{version, tree, surface}, func() {}, nil
	}

	return nil, nil, nil, newError(CodeSourceUnsupported, sp.Kind, sp.Text)
}

// ManifestError carries every finding about a package that was refused, so that the host can list
// them with their positions. It is the underlying error of an *Error whose code is the first
// finding's code.
type ManifestError struct {
	Issues []Issue
}

// Error implements error.
func (e *ManifestError) Error() string {
	if len(e.Issues) == 0 {
		return "invalid package"
	}

	return e.Issues[0].Error()
}

// rejection builds the *Error for a package that cannot be installed. The code is the first
// finding's when the specification names it, else manifest_invalid.
//
// Parameters:
//   - issues: findings of severity error, in report order
func rejection(issues []Issue) *Error {
	code := CodeManifestInvalid

	var args []string

	if len(issues) > 0 {
		first := issues[0]
		args = []string{first.Path, itoa(len(issues))}

		if slices.Contains(namedIssueCodes, first.Code) {
			code = first.Code
		}
	}

	return wrapError(code, &ManifestError{Issues: issues}, args...)
}

// namedIssueCodes are the manifest codes that the failure table of the specification gives to an
// install; the others are finer causes of manifest_invalid.
var namedIssueCodes = []string{
	CodeIDInvalid, CodePermUnknown, CodePermRefused,
	CodeAPIUnsupported, CodeToteRange, CodePlatformNone, CodeReserved, CodeDeclarativeExtra,
	CodeExecSelf, CodeExecUncovered, CodePermMissing, CodeManifestTooLarge, CodeBinMissing,
}

// compare finds the installed plugin of the same id, applies the update rules and builds the
// diff.
//
// Parameters:
//   - lk: the lock
//   - req: the request
func (s *Staged) compare(lk *Lock, req InstallRequest) error {
	m := s.pm.m

	if _, bad := lk.Invalid()[m.ID]; bad {
		return newError(CodeStoreCorrupt, m.ID)
	}

	rec, found := lk.Get(m.ID)
	if req.Update && !found {
		return newError(CodeNotInstalled, m.ID)
	}

	var warnings []Reason

	cur := Plugin{}

	if found {
		s.base = &rec
		cur, _ = s.r.Recheck(m.ID)

		if !req.Update && !req.Force && s.identical(rec, cur) {
			s.same = &cur
			s.diff = s.makeDiff(cur, true, nil)
			s.diff.NeedsConsent, s.diff.Why = false, nil

			return nil
		}

		var err error

		warnings, err = s.rules(rec, req.Force)
		if err != nil {
			return err
		}
	}

	s.diff = s.makeDiff(cur, found, warnings)

	return nil
}

// identical tells whether the staged package is what is installed already: the same id (the caller
// looked the record up by it), recorded from the same source (origin and path or release URL, none
// of the looser rules of sameSource), with the same tree hash, and the installed tree readable (its
// manifest read). Such an install has nothing to do and nothing to ask: Plan marks it and the host
// says so (AlreadyInstalled). The kind and the version are not compared: a valid record's kind
// follows from its source and origin (a path, a URL or nothing), and the tree hash covers the
// manifest and so the version. How well the installed tree is doing (permissions, a lock record
// that disagrees with the manifest) is not decided here: it is the state of the plugin, which the
// host reports.
//
// Parameters:
//   - rec: the installed record
//   - cur: the installed plugin as Recheck sees it
func (s *Staged) identical(rec LockRecord, cur Plugin) bool {
	return cur.Manifest != nil && rec.Origin == s.origin && rec.Source == s.recordSource() &&
		rec.TreeSHA256 == s.tree.SHA256
}

// AlreadyInstalled tells that an install asks for what is installed already, in which case the
// package changes nothing and asks no consent (Commit returns an unchanged result and leaves the
// store and the lock alone). It is true when the same plugin is recorded from the same source at
// the same version with the same tree hash, and neither an update nor --force asked for the work.
//
// Returns the installed plugin as Recheck saw it when the package was staged, and true, or the zero
// value and false.
func (s *Staged) AlreadyInstalled() (Plugin, bool) {
	if s.same == nil {
		return Plugin{}, false
	}

	return *s.same, true
}

// recordSource is the source as the lock records it: the path of a local directory or archive,
// the release URL of a release, empty for a bundled plugin.
func (s *Staged) recordSource() string {
	switch s.sp.Kind {
	case KindLocal:
		return s.sp.Path
	case KindRelease:
		return s.sp.Ref.URL()
	}

	return ""
}

// rules applies the update rules of section 7.4: the origin must be the same, the version must be
// newer and must not be a pre-release. With force the violations are warnings.
//
// Parameters:
//   - rec: the installed record
//   - force: true to accept what the rules refuse
//
// Returns the warnings, or an *Error: source_changed, downgrade or prerelease.
func (s *Staged) rules(rec LockRecord, force bool) ([]Reason, error) {
	var warnings []Reason

	refuse := func(code string, args ...string) error {
		if !force {
			return newError(code, args...)
		}

		warnings = append(warnings, Reason{code, args})

		return nil
	}

	id := s.pm.m.ID
	if !sameSource(rec, s.sp.Kind, s.origin, s.recordSource(), id) {
		from, to := sourceLabel(rec.Source, rec.Kind), sourceLabel(s.recordSource(), s.sp.Kind)

		if !force {
			return nil, newError(WarnSourceChanged, id, from, to)
		}

		warnings = append(warnings, Reason{WarnSourceChanged, []string{from, to}})
	}

	oldV, oerr := ParseVersion(rec.Version)
	newV, nerr := ParseVersion(s.pm.m.Version)

	if oerr == nil && nerr == nil && newV.Compare(oldV) <= 0 {
		if !force {
			return nil, newError(CodeDowngrade, s.pm.m.Version, rec.Version)
		}

		if newV.Compare(oldV) == 0 {
			warnings = append(warnings, Reason{WarnSameVersion, []string{s.pm.m.Version}})
		} else {
			warnings = append(warnings, Reason{WarnDowngrade, []string{
				s.pm.m.Version,
				rec.Version,
			}})
		}
	}

	if nerr == nil && newV.Prerelease() {
		if err := refuse(WarnPrerelease, s.pm.m.Version); err != nil {
			return nil, err
		}
	}

	return warnings, nil
}

// sourceLabel names where a plugin came from in a message: the path or the URL, or the kind for
// a plugin that has no source of its own (a bundled one).
//
// Parameters:
//   - source: source as the lock records it, possibly empty
//   - kind: kind of the source
func sourceLabel(source, kind string) string {
	if source == "" {
		return kind
	}

	return source
}

// sameSource tells whether a new version comes from where the installed one came from. The kind
// and the origin must be equal, and a local plugin must come from the same path. A release is the
// same source whatever its tag and asset, since every version has its own URL. A release of
// totecli/<id> replaces the bundled copy of that id.
//
// Parameters:
//   - rec: the installed record
//   - kind: kind of the new source
//   - origin: lower case owner/repo of the new source, empty unless it is a release
//   - source: source as it would be recorded
//   - id: plugin id
func sameSource(rec LockRecord, kind, origin, source, id string) bool {
	if rec.Kind == KindBundled && kind == KindRelease && isOfficialOrigin(origin, id) {
		return true
	}

	if kind == KindRelease {
		return rec.Kind == KindRelease && rec.Origin == origin
	}

	return rec.Kind == kind && rec.Origin == origin && rec.Source == source
}

// makeDiff builds the Diff.
//
// Parameters:
//   - cur: the installed plugin as Recheck sees it, the zero value for a new install
//   - found: true when a record exists
//   - warnings: findings of the update rules that force accepted
func (s *Staged) makeDiff(cur Plugin, found bool, warnings []Reason) Diff {
	m := s.pm.m
	caps := EffectiveCaps(m)
	surface := SurfaceSHA256(m)

	cat := catalogOf(m, s.tree)

	d := Diff{
		ID: m.ID, Title: cat.Text(s.lang, "title", m.Title),
		Description: cat.Text(s.lang, "description", m.Description), Version: m.Version,
		Tier: m.Tier, Program: programOf(m), Kind: s.sp.Kind, Origin: s.origin,
		Class:  originClass(s.sp.Kind, s.origin, m.ID),
		Source: s.recordSource(), TreeSHA256: s.tree.SHA256, ArchiveSHA256: s.archive,
		SurfaceSHA256: surface,
		Update:        found, Caps: caps, Commands: Commands(m), Warnings: warnings,
		Issues: s.pm.rep.Warnings(), Grant: GrantToken(caps, surface),
	}

	d.Warnings = append(d.Warnings, s.pm.warnings...)

	d.Missing = s.r.missing(m)
	for _, rq := range d.Missing {
		d.Warnings = append(d.Warnings, Reason{ReasonMissingDep, []string{rq.Bin}})
	}

	if !found {
		d.Added, d.Unchanged = caps, 0
		d.NeedsConsent, d.Why = true, []string{WhyNew}

		return d
	}

	rec := *s.base
	d.OldVersion = rec.Version
	d.Added, d.Removed, d.Unchanged = capsDelta(rec.Caps, caps)

	if cur.Manifest != nil {
		d.SurfaceChanges = SurfaceEscalations(cur.Manifest, m)
		d.CommandChanges = DiffCommands(cur.Manifest, m)
	}

	switch {
	case !rec.Consented():
		d.Why = append(d.Why, WhyUnconsent)
	case len(d.Added) > 0:
		d.Why = append(d.Why, WhyCaps)
	}

	if len(d.SurfaceChanges) > 0 {
		d.Why = append(d.Why, WhySurface)
	}

	if cur.Manifest == nil {
		d.Why = append(d.Why, WhyUnverified)
	}

	// A refusal that force turned into a warning is never silent: the user sees it.
	if len(warnings) > 0 {
		d.Why = append(d.Why, WhyForced)
	}

	d.NeedsConsent = len(d.Why) > 0

	return d
}

// missing lists the required programs that PATH does not have. A lookup is a stat: nothing runs.
//
// Parameters:
//   - m: manifest
func (r *Registry) missing(m *Manifest) []Requires {
	pathEnv, _ := r.lookup("PATH")
	avoid := r.avoid()

	var out []Requires

	for _, rq := range m.Requires {
		if rq.Optional {
			continue
		}

		if _, found := LookBin(pathEnv, rq.Bin, avoid); !found {
			out = append(out, rq)
		}
	}

	return out
}

// Abort deletes the staging directory. It is safe to call after Commit and more than once. A
// plugins directory that the plan had to create on a machine without plugins, and that is still
// empty, goes with it, so a dry run leaves nothing behind.
//
// Returns the error of the deletion, which leaves the directory for the next cleanup.
func (s *Staged) Abort() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.discard()

	dropDirs(s.fresh)
	s.fresh = nil

	return err
}

// discard marks the package spent and deletes what is left of its staging directory.
func (s *Staged) discard() error {
	s.spent = true

	return s.dropStage()
}

// dropStage deletes the staging directory, if there is still one.
func (s *Staged) dropStage() error {
	dir := s.dir
	s.dir = ""

	if dir == "" {
		return nil
	}

	return removeTree(dir)
}
