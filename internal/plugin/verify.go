package plugin

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// maxReasonRunes is the length of an argument copied into a reason.
const maxReasonRunes = 4 * issueArgRunes

// localeFiles are the files of a package that hold translations, with the language of each.
var localeFiles = map[string]string{"locales/en.toml": "en", "locales/fr.toml": "fr"}

// evaluate decides the state of one plugin: it gathers every failing check into Facts and lets
// Resolve rank them. Nothing here executes the plugin or a program it names.
//
// Parameters:
//   - ctx: inputs of the pass
//   - id: plugin id
func (r *Registry) evaluate(ctx *loadContext, id string) Plugin {
	p := Plugin{ID: id}

	var f Facts

	r.killSwitches(ctx, id, &p, &f)

	if p.Installed {
		r.verify(ctx, &p, &f)
	}

	p.State, p.Reasons = Resolve(f)
	p.facts = f

	return p
}

// killSwitches reads the three ways a plugin is switched off, the lock record and the two files
// whose failure makes everything broken.
//
// Parameters:
//   - ctx: inputs of the pass
//   - id: plugin id
//   - p: plugin being evaluated; Installed and Record are set here
//   - f: failing checks; updated here
func (r *Registry) killSwitches(ctx *loadContext, id string, p *Plugin, f *Facts) {
	if r.opts.NoPlugins {
		f.Disabled = append(f.Disabled, Reason{Code: ReasonSessionOff})
	}

	if ctx.cfgErr == nil && !ctx.cfg.PluginEnabled(id) {
		f.Disabled = append(f.Disabled, Reason{Code: ReasonConfigOff})
	}

	if ctx.cfgErr != nil {
		f.Broken = append(f.Broken, Reason{ReasonConfigUnreadable, []string{errText(ctx.cfgErr)}})
	}

	rec, ok := ctx.records[id]

	switch {
	case ctx.lockErr != nil:
		f.Broken = append(f.Broken, Reason{ReasonLockUnreadable, []string{errText(ctx.lockErr)}})
	case ctx.invalid[id] != nil:
		f.Broken = append(f.Broken, Reason{
			ReasonLockRecordInvalid,
			[]string{CleanLine(ctx.invalid[id].Error(), maxReasonRunes)},
		})
	case !ok:
		f.Broken = append(f.Broken, Reason{Code: ReasonNoLockRecord})
	default:
		p.Installed, p.Record = true, rec

		if rec.Disabled {
			f.Disabled = append(f.Disabled, Reason{Code: ReasonLockOff})
		}
	}
}

// verify checks the installed tree of a plugin against its record and, when the tree is the one
// that was pinned, reads and checks its manifest.
//
// Parameters:
//   - ctx: inputs of the pass
//   - p: plugin with its record
//   - f: failing checks; updated here
func (r *Registry) verify(ctx *loadContext, p *Plugin, f *Facts) {
	rec := p.Record

	dir, err := r.opts.Paths.PluginTree(p.ID, TreeID(rec.TreeSHA256))
	if err != nil {
		f.Broken = append(f.Broken, Reason{Code: CodeTreeName})

		return
	}

	p.Dir = dir
	ctx.markSeen(dir)

	if reason, ok := r.checkIDDir(ctx, p, filepath.Dir(dir)); !ok {
		f.Broken = append(f.Broken, reason)

		return
	}

	entry, terr := r.treeOf(dir)
	if terr != nil {
		f.Broken = append(f.Broken, treeReason(terr.Code, terr.Path))

		return
	}

	for _, pr := range entry.scan.problems(ctx.uid) {
		f.Broken = append(f.Broken, treeReason(pr.Code, pr.Path))
	}

	if entry.tree.SHA256 != rec.TreeSHA256 {
		f.Broken = append(f.Broken, Reason{
			CodeTreeHash,
			[]string{TreeID(rec.TreeSHA256), TreeID(entry.tree.SHA256)},
		})

		return
	}

	r.manifestOf(ctx, p, entry, f)
}

// markSeen records that a tree directory is in use, so that the cache keeps it.
//
// Parameters:
//   - dir: tree directory
func (c *loadContext) markSeen(dir string) {
	// The map is shared by the workers of one pass.
	c.mu.Lock()
	c.seen[dir] = true
	c.mu.Unlock()
}

// treeReason builds the reason of a finding about a tree.
//
// Parameters:
//   - code: one of the CodeTree constants
//   - rel: relative path of the entry concerned, empty for the tree itself
func treeReason(code, rel string) Reason {
	if rel == "" {
		return Reason{Code: code}
	}

	return Reason{code, []string{CleanLine(rel, maxReasonRunes)}}
}

// writableReason builds the reason of a directory of the store that others can write.
//
// Parameters:
//   - dir: the directory
func writableReason(dir string) Reason {
	clean := CleanLine(dir, maxReasonRunes)

	return Reason{CodeStoreWritable, []string{clean, clean}}
}

// checkIDDir checks the directory <Data>/plugins/<id> that holds the trees: a real directory, not
// a link, owned by the user and not writable by anyone else. It also lists the trees that are not
// the current one.
//
// Parameters:
//   - ctx: inputs of the pass
//   - p: plugin; Stale is set here
//   - dir: the directory
//
// Returns the reason and false when the directory cannot be trusted.
func (r *Registry) checkIDDir(ctx *loadContext, p *Plugin, dir string) (Reason, bool) {
	info, err := os.Lstat(dir)
	if err != nil {
		return Reason{Code: CodeTreeMissing}, false
	}

	id, ok := statIDOf(info)

	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return Reason{Code: CodeTreeSymlink}, false
	case !info.IsDir():
		return Reason{Code: CodeTreeSpecial}, false
	case !ok || id.UID != ctx.uid:
		return Reason{Code: CodeTreeOwner}, false
	case info.Mode().Perm()&0o022 != 0:
		return writableReason(dir), false
	}

	// Whoever can write the plugins directory can rename the id directory away.
	if parent, err := os.Stat(filepath.Dir(dir)); err == nil {
		pid, pok := statIDOf(parent)

		switch {
		case !pok || pid.UID != ctx.uid:
			return Reason{Code: CodeTreeOwner}, false
		case parent.Mode().Perm()&0o022 != 0:
			return writableReason(filepath.Dir(dir)), false
		}
	}

	current := filepath.Base(p.Dir)

	entries, _ := listDir(dir, MaxTreeEntries)
	for _, e := range entries {
		if e.Name() != current {
			p.Stale = append(p.Stale, CleanLine(e.Name(), issueArgRunes))
		}
	}

	slices.Sort(p.Stale)

	return Reason{}, true
}

// treeOf scans a tree and returns its verified content, from the cache when no entry changed.
//
// Parameters:
//   - dir: tree directory
func (r *Registry) treeOf(dir string) (*treeCache, *TreeError) {
	sc, terr := scanTree(dir)
	if terr != nil {
		return nil, terr
	}

	var hit *treeCache

	r.mu.Lock()
	if cached := r.cache[dir]; cached != nil && cached.scan.sameAs(sc) {
		hit = &treeCache{scan: sc, tree: cached.tree, parsed: cached.parsed}
	}
	r.mu.Unlock()

	if hit != nil {
		// The identities are the fingerprint, but the owner and the mode are checked on the
		// fresh scan, so the cache never hides a chmod.
		return hit, nil
	}

	tree, terr := sc.hash(dir, keepPackageText)
	if terr != nil {
		return nil, terr
	}

	r.mu.Lock()
	r.cache[dir] = &treeCache{scan: sc, tree: tree}
	r.mu.Unlock()

	return &treeCache{scan: sc, tree: tree}, nil
}

// keepPackageText selects the files whose bytes are read while hashing: the manifest and the
// locale files, with their size limits.
//
// Parameters:
//   - rel: relative path of a file
func keepPackageText(rel string) (int, bool) {
	if rel == manifestName {
		return MaxManifestSize, true
	}

	if _, ok := localeFiles[rel]; ok {
		return MaxLocaleSize, true
	}

	return 0, false
}

// manifestOf reads the manifest from the bytes that were hashed and runs the checks that need it:
// the compatibility, the consent, the quarantine and the required programs.
//
// Parameters:
//   - ctx: inputs of the pass
//   - p: plugin; Header, Manifest and the derived fields are set here
//   - entry: verified tree
//   - f: failing checks; updated here
func (r *Registry) manifestOf(ctx *loadContext, p *Plugin, entry *treeCache, f *Facts) {
	rec := p.Record
	official := rec.Kind == KindBundled || isOfficialOrigin(rec.Origin, p.ID)

	pm := entry.parsed
	if pm == nil || pm.official != official {
		pm = r.parse(p.ID, entry.tree, official)

		r.mu.Lock()
		if c := r.cache[p.Dir]; c != nil && c.tree == entry.tree {
			c.parsed = pm
		}
		r.mu.Unlock()
	}

	p.Header, p.Issues, p.Warnings = pm.rep.Header, pm.rep.Issues, slices.Clone(pm.warnings)

	if pm.m == nil {
		classifyManifest(pm.rep, f)

		return
	}

	p.Manifest, p.Catalog = pm.m, pm.catalog

	if pm.m.Version != rec.Version {
		f.Broken = append(f.Broken, Reason{ReasonLockVersion, []string{rec.Version, pm.m.Version}})
	}

	if pm.m.Tier != rec.Tier {
		f.Broken = append(f.Broken, Reason{ReasonLockTier, []string{rec.Tier, pm.m.Tier}})
	}

	if len(pm.pkg) > 0 {
		first := pm.pkg[0]
		f.Broken = append(f.Broken, Reason{ReasonPackageInvalid, []string{
			first.Code,
			CleanLine(first.Path, maxReasonRunes), itoa(len(pm.pkg)),
		}})
	}

	consent(p, f)

	if rec.Quarantined {
		f.Quarantined = append(f.Quarantined, Reason{
			ReasonQuarantined,
			[]string{rec.LastReason, itoa(rec.Failures)},
		})
	}

	r.dependencies(ctx, p, f)
}

// parse reads a manifest and the locale files of a verified tree.
//
// Parameters:
//   - id: plugin id, which the manifest must carry
//   - tree: verified tree with the bytes of the manifest and of the locale files
//   - official: true when the plugin may use an official id
func (r *Registry) parse(id string, tree *Tree, official bool) *parsedManifest {
	pm := &parsedManifest{official: official}

	data, ok := tree.data[manifestName]
	if !ok {
		pm.rep = missingManifest(tree)

		return pm
	}

	pm.m, pm.rep = Decode(data, DecodeOptions{
		Host: r.opts.Host, WantID: id, Official: official, Installed: true,
	})
	if pm.m == nil {
		return pm
	}

	r.finishParse(pm, tree)

	return pm
}

// finishParse runs the checks that need the whole tree on a manifest that decoded: the package
// content, the execute bits and the locale files. The same code reads an installed tree and a
// staged one, so that an install refuses what Load would call broken.
//
// Parameters:
//   - pm: result of the decoding, with a manifest; the package findings, the warnings and the
//     catalog are set here
//   - tree: hashed tree with the bytes of the manifest and of the locale files
func (r *Registry) finishParse(pm *parsedManifest, tree *Tree) {
	files := make([]string, len(tree.Files))
	for i, tf := range tree.Files {
		files[i] = tf.Path
	}

	pm.pkg = append(CheckPackage(pm.m, files, r.opts.Host),
		CheckExecBits(pm.m, tree.Files, r.opts.Host)...)

	locales := map[string]map[string]string{}

	for _, rel := range sortedKeys(localeFiles) {
		raw, ok := tree.data[rel]
		if !ok {
			continue
		}

		table, issues := ParseLocale(raw)
		if len(issues) > 0 {
			pm.warnings = append(pm.warnings, Reason{
				ReasonLocaleInvalid,
				[]string{localeFiles[rel], issues[0].Code},
			})

			continue
		}

		locales[localeFiles[rel]] = table
	}

	pm.catalog = NewCatalog(MergeLocales(pm.m, locales))
}

// missingManifest builds the report for a tree without a readable plugin.toml: too large to be
// read, or absent.
//
// Parameters:
//   - tree: verified tree
func missingManifest(tree *Tree) Report {
	for _, tf := range tree.Files {
		if tf.Path == manifestName {
			return Report{Issues: []Issue{{
				Code: CodeManifestTooLarge,
				Args: []string{itoa(MaxManifestSize)},
			}}}
		}
	}

	return Report{Issues: []Issue{{Code: CodeKeyMissing, Path: manifestName}}}
}

// classifyManifest turns the errors of a manifest that was refused into reasons: an invalid
// manifest is broken, with the code of its first error; an incompatible one lists each
// incompatibility.
//
// Parameters:
//   - rep: report of Decode
//   - f: failing checks; updated here
func classifyManifest(rep Report, f *Facts) {
	errs := rep.Errors()
	if len(errs) == 0 {
		return
	}

	if rep.Class() == ClassInvalid {
		first := errs[0]
		args := []string{first.Code, CleanLine(first.Path, maxReasonRunes), itoa(len(errs))}
		f.Broken = append(f.Broken, Reason{ReasonManifestInvalid, args})

		return
	}

	for _, e := range errs {
		if len(f.Incompatible) >= maxReasonArgs {
			break
		}

		f.Incompatible = append(f.Incompatible, Reason{e.Code, e.Args})
	}
}

// consent checks the effective capabilities and the surface of the manifest against what the user
// consented to, and sets the derived fields of the plugin.
//
// Parameters:
//   - p: plugin with a valid manifest and its record
//   - f: failing checks; updated here
func consent(p *Plugin, f *Facts) {
	rec := p.Record

	p.Caps = EffectiveCaps(p.Manifest)
	p.Surface = SurfaceSHA256(p.Manifest)
	p.Grant = GrantToken(p.Caps, p.Surface)

	if !rec.Consented() {
		f.NeedsConsent = append(f.NeedsConsent, Reason{Code: ReasonNotConsented})
	}

	if added := Escalation(rec.Caps, p.Caps); len(added) > 0 {
		if len(added) > maxReasonArgs {
			added = added[:maxReasonArgs]
		}

		f.NeedsConsent = append(f.NeedsConsent, Reason{ReasonCapsEscalated, added})
	}

	if p.Surface != rec.SurfaceSHA256 {
		f.NeedsConsent = append(f.NeedsConsent, Reason{
			ReasonEscalated,
			[]string{rec.SurfaceSHA256[:12], p.Surface[:12]},
		})
	}
}

// dependencies looks the required programs up on PATH. A required program that is absent leads to
// missing-dependency, an optional one is a warning. A lookup is a stat: nothing runs.
//
// Parameters:
//   - ctx: inputs of the pass
//   - p: plugin with a valid manifest
//   - f: failing checks; updated here
func (r *Registry) dependencies(ctx *loadContext, p *Plugin, f *Facts) {
	for _, rq := range p.Manifest.Requires {
		if _, found := LookBin(ctx.pathEnv, rq.Bin, ctx.avoid); found {
			continue
		}

		if rq.Optional {
			p.Absent = append(p.Absent, rq)
			p.Warnings = append(p.Warnings, Reason{ReasonOptionalDepAbsent, []string{rq.Bin}})

			continue
		}

		p.Missing = append(p.Missing, rq)
		f.MissingDependency = append(f.MissingDependency, Reason{
			ReasonMissingDep,
			[]string{rq.Bin},
		})
	}
}
