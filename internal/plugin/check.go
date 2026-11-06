package plugin

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
)

// Kinds of target that Check understands.
const (
	TargetInstalled = "installed"
	TargetDirectory = "directory"
	TargetArchive   = "archive"
	TargetBundled   = "bundled"
)

// Scopes of a Finding: where the finding comes from. The first four describe the package, and
// --strict promotes their warnings; the others describe the machine or the request, and a package
// is not at fault for them.
const (
	ScopeManifest = "manifest"
	ScopePackage  = "package"
	ScopeLocale   = "locale"
	ScopePin      = "pin"
	ScopeState    = "state"
	ScopeEnv      = "environment"
	ScopeSettings = "settings"
	ScopeRequest  = "request"
)

// maxTargetRunes is the most runes of the target of a check that the result keeps: a path of the
// user is whole up to the length of a long path, and only a hostile name is cut.
const maxTargetRunes = 1024

// Codes of the findings that Check adds to those of the manifest, the package and the locales.
const (
	CodePinMismatch    = "pin_mismatch"
	CodeSetUnknown     = "set_unknown"
	CodeSettingsIgnore = "settings_ignored"
	CodeSettingsFile   = "settings_file"
	CodeSettingsKey    = "settings_unknown_key"
	CodeProcsUnsafe    = "procs_unsafe"
	CodeProcsOrphan    = "procs_orphan"
	CodeLeftover       = "leftover"
)

// CheckRequest asks Check to examine one plugin.
//
// Target is an installed plugin id, the id of a bundled plugin that is not installed, or a
// directory or an archive file written with a leading /, ./, ../ or ~/. Strict makes the result
// fail on a warning about the package. Official lets a directory use an official id, which a plain
// install refuses; the bundled copy always may. Lang picks the language of the texts shown
// (confirmation, hints). Set gives values to form fields by id, for every contribution that has the
// field, and Repo names a saved repository whose data fills the {repo.*} placeholders; with
// neither, a contribution that needs them is shown as its template with the reason it cannot be
// resolved.
type CheckRequest struct {
	Target   string
	Strict   bool
	Official bool
	Lang     string
	Set      map[string]string
	Repo     string
	noArgv   bool
}

// Finding is one thing Check found: a stable code, where it was found and its arguments. It is
// data, never a message; the host renders text from Code and Args.
//
// Scope is one of the Scope constants. Path, Line and Column locate it in the manifest or the
// locale file (Line is 0 when unknown). Incompatible marks an error that makes the plugin
// incompatible with this tote rather than invalid.
type Finding struct {
	Scope        string   `json:"scope"`
	Code         string   `json:"code"`
	Severity     Severity `json:"severity"`
	Path         string   `json:"path,omitempty"`
	Line         int      `json:"line,omitempty"`
	Column       int      `json:"column,omitempty"`
	Args         []string `json:"args,omitempty"`
	Incompatible bool     `json:"incompatible,omitempty"`
}

// Requirement is a [[requires]] entry and what the lookup on PATH found. The hint is the
// plugin's text, cleaned: it is shown as suggested by the plugin and never run.
type Requirement struct {
	Bin      string `json:"bin"`
	Hint     string `json:"hint,omitempty"`
	Optional bool   `json:"optional"`
	Found    bool   `json:"found"`
	Path     string `json:"path,omitempty"`
}

// SettingNote is a value of a setting that was ignored, as check shows it.
type SettingNote struct {
	Setting string `json:"setting"`
	Source  string `json:"source"`
	Code    string `json:"code"`
	Raw     string `json:"raw,omitempty"`
}

// CheckResult is what Check found about one plugin.
//
// Kind is one of the Target constants. State and Reasons are set for an installed plugin only.
// Findings are in this order: the manifest, the package files, the locales, the pin, the state,
// the programs and the settings. Contributions list the argv and the directory of every
// contribution. The hashes are those an install of the same files would record: ArchiveSHA256 is
// set for an archive and for an installed release, and Grant is the token that --grant must
// equal. Strict is the --strict flag of the request; Failed applies it.
type CheckResult struct {
	Target        string         `json:"target"`
	Kind          string         `json:"kind"`
	ID            string         `json:"id,omitempty"`
	Version       string         `json:"version,omitempty"`
	Tier          string         `json:"tier,omitempty"`
	Title         string         `json:"title,omitempty"`
	State         State          `json:"state,omitempty"`
	Reasons       []Reason       `json:"reasons,omitempty"`
	Strict        bool           `json:"strict"`
	Findings      []Finding      `json:"findings"`
	Contributions []Contribution `json:"contributions,omitempty"`
	Requires      []Requirement  `json:"requires,omitempty"`
	Rejected      []SettingNote  `json:"rejected,omitempty"`
	ArchiveSHA256 string         `json:"archive_sha256,omitempty"`
	TreeSHA256    string         `json:"tree_sha256,omitempty"`
	SurfaceSHA256 string         `json:"surface_sha256,omitempty"`
	Caps          []string       `json:"caps,omitempty"`
	Grant         string         `json:"grant,omitempty"`
	Files         []TreeFile     `json:"-"`
}

// Errors counts the findings of severity error.
func (c *CheckResult) Errors() int { return c.count(SeverityError, false) }

// Warnings counts the findings of severity warning.
func (c *CheckResult) Warnings() int { return c.count(SeverityWarning, false) }

// Failed tells whether the check fails: any error, and under --strict any warning about the
// package itself. A warning about the machine (a program that is not installed) or about the
// user's settings never fails a check, because the package is not at fault for it.
func (c *CheckResult) Failed() bool {
	if c.Errors() > 0 {
		return true
	}

	return c.Strict && c.count(SeverityWarning, true) > 0
}

// Has tells whether a finding carries a code.
//
// Parameters:
//   - code: stable code to look for
func (c *CheckResult) Has(code string) bool {
	return slices.ContainsFunc(c.Findings, func(f Finding) bool { return f.Code == code })
}

// count counts findings of one severity.
//
// Parameters:
//   - sev: severity to count
//   - intrinsic: true to count only the findings about the package
func (c *CheckResult) count(sev Severity, intrinsic bool) int {
	n := 0

	for _, f := range c.Findings {
		if f.Severity == sev && (!intrinsic || f.intrinsic()) {
			n++
		}
	}

	return n
}

// intrinsic tells whether a finding is about the package, as opposed to the machine, the state
// or the request.
func (f Finding) intrinsic() bool {
	switch f.Scope {
	case ScopeManifest, ScopePackage, ScopeLocale, ScopePin:
		return true
	}

	return false
}

// findings turns the issues of the manifest, the package or the locales into findings.
//
// Parameters:
//   - scope: scope of all of them
//   - issues: issues to convert
func findings(scope string, issues []Issue) []Finding {
	out := make([]Finding, 0, len(issues))

	for _, i := range issues {
		out = append(out, Finding{
			Scope: scope, Code: i.Code, Severity: i.Severity,
			Path: i.Path, Line: i.Line, Column: i.Column, Args: i.Args,
			Incompatible: i.Incompatible,
		})
	}

	return out
}

// errorFinding turns an *Error of the install pipeline into a finding of the package.
//
// Parameters:
//   - e: the error
func errorFinding(e *Error) Finding {
	return Finding{Scope: ScopePackage, Code: e.Code, Severity: SeverityError, Args: e.Args}
}

// Check examines one plugin and reports what an install or a load would find, without running
// anything. It reads the manifest, the package files, the locale files and the settings, looks the
// required programs up on PATH, and computes the argv and the directory of every contribution.
// The only program it can start is git, run by tote itself to derive {repo.*} for a saved
// repository; nothing the plugin names is ever started.
//
// An installed plugin is examined in place, from the tree that Load verified. A directory is
// copied into a private temporary directory by the rules of an install (regular files and
// directories only, the limits of section 7.3) and examined there, so that the hashes are those an
// install would record and nothing in the source can reach outside. The temporary directory is
// deleted before the call returns.
//
// Parameters:
//   - ctx: cancels the copy of a directory
//   - req: what to examine, see CheckRequest
//
// Returns the result, with the findings in it, or an *Error: bad_source for an empty or unusable
// target or a directory that is not there, source_unsupported for a release URL,
// not_installed for an id that is neither installed nor bundled, bad_id for a malformed id, disk
// when the temporary copy fails. Findings are not errors: a plugin full of them is a result.
func (r *Registry) Check(ctx context.Context, req CheckRequest) (*CheckResult, error) {
	text := req.Target

	switch {
	case text == "":
		return nil, newError(CodeBadSource, "")
	case isLocalPath(text):
		sp, err := r.classifyPath(sourceSpec{Text: text})

		var pe *Error
		if errors.As(err, &pe) && pe.Code == CodeSourceUnsupported && sp.Archive {
			return r.checkArchive(ctx, req, sp)
		}

		if err != nil {
			return nil, err
		}

		return r.checkDirectory(ctx, req, sp.Path)
	case strings.Contains(text, "://") || shorthandRe.MatchString(text):
		return nil, newError(CodeSourceUnsupported, KindRelease, text)
	case !validID(text):
		return nil, newError(CodeBadID, text)
	}

	if p, lc, ok := r.recheck(text); ok {
		return r.checkInstalled(p, lc, req), nil
	}

	if r.opts.Bundled != nil {
		if fsys, _, _, _, ok := r.opts.Bundled.Lookup(text); ok && fsys != nil {
			return r.checkBundled(ctx, req, text)
		}
	}

	return nil, newError(CodeNotInstalled, text)
}

// newResult starts a result for a request.
//
// Parameters:
//   - req: the request
//   - kind: one of the Target constants
func newResult(req CheckRequest, kind string) *CheckResult {
	return &CheckResult{
		Target: CleanLine(req.Target, maxTargetRunes), Kind: kind,
		Strict: req.Strict, Findings: []Finding{},
	}
}

// lang gives the language of the texts of a request: English unless the request names another.
func (req CheckRequest) lang() string {
	if req.Lang == "" {
		return "en"
	}

	return req.Lang
}

// checkInstalled examines an installed plugin from the state that Recheck computed. A tree that
// failed its verification has no manifest to lint, so the result holds the reasons the state
// function gave.
//
// Parameters:
//   - p: the plugin
//   - lc: inputs of the pass that produced p
//   - req: the request
func (r *Registry) checkInstalled(p Plugin, lc *loadContext, req CheckRequest) *CheckResult {
	res := newResult(req, TargetInstalled)
	res.ID, res.State, res.Reasons = p.ID, p.State, p.Reasons
	res.Version, res.Tier, res.Title = p.Header.Version, p.Header.Tier, p.Header.Title
	res.Findings = append(res.Findings, findings(ScopeManifest, p.Issues)...)
	res.Findings = append(res.Findings, stateFindings(p, res.Findings)...)
	res.TreeSHA256, res.ArchiveSHA256 = p.Record.TreeSHA256, p.Record.ArchiveSHA256

	if p.Manifest == nil {
		return res
	}

	res.Caps, res.SurfaceSHA256, res.Grant = p.Caps, p.Surface, p.Grant
	res.Version, res.Tier, res.Title = p.Manifest.Version, p.Manifest.Tier, p.Manifest.Title

	if tc, terr := r.treeOf(p.Dir); terr == nil {
		res.Files = tc.tree.Files
		res.Findings = append(res.Findings, r.localeFindings(p.Manifest, tc.tree, req)...)
	}

	r.describe(res, p, lc, req)

	return res
}

// stateFindings turns the reasons of a plugin that cannot be used into findings: a broken or
// incompatible plugin is an error. The other states (disabled, needs-consent, quarantined,
// missing-dependency) are the user's to resolve and are shown as the state, not as a defect. A
// reason whose code is the code of an error that the manifest findings report already is the same
// defect seen twice (the state of an installed plugin repeats what its manifest says), so it is
// left out.
//
// Parameters:
//   - p: the plugin
//   - have: findings of the manifest, already in the result
func stateFindings(p Plugin, have []Finding) []Finding {
	var out []Finding

	seen := map[string]bool{}

	for _, f := range have {
		if f.Severity == SeverityError {
			seen[f.Code] = true
		}
	}

	for _, rs := range p.facts.Broken {
		if !seen[rs.Code] {
			out = append(out, Finding{
				Scope: ScopeState, Code: rs.Code, Severity: SeverityError,
				Args: rs.Args,
			})
		}
	}

	for _, rs := range p.facts.Incompatible {
		if !seen[rs.Code] {
			out = append(out, Finding{
				Scope: ScopeState, Code: rs.Code, Severity: SeverityError,
				Args: rs.Args, Incompatible: true,
			})
		}
	}

	return out
}

// localeFindings checks the locale files of a package against its manifest: that each parses,
// that each value fits the limits and the placeholders of the entry it translates, and the
// parity of the keys. A syntax or size problem is an error. Parity problems are warnings, and
// errors under --strict.
//
// Parameters:
//   - m: validated manifest
//   - tree: hashed tree, which kept the bytes of the locale files
//   - req: the request
func (r *Registry) localeFindings(m *Manifest, tree *Tree, req CheckRequest) []Finding {
	var out []Finding

	tables := map[string]map[string]string{}

	for _, rel := range sortedKeys(localeFiles) {
		raw, ok := tree.data[rel]
		if !ok {
			continue
		}

		table, issues := ParseLocale(raw)
		if len(issues) > 0 {
			for i := range issues {
				issues[i].Path = joinKey("locales."+localeFiles[rel], issues[i].Path)
			}

			out = append(out, findings(ScopeLocale, issues)...)

			continue
		}

		tables[localeFiles[rel]] = table
	}

	return append(out, findings(ScopeLocale, CheckLocales(m, MergeLocales(m, tables),
		req.Strict))...)
}

// catalogOf builds the catalog of a package from its locale files, for the texts the check shows.
// A file that does not parse is left out, as Load leaves it out.
//
// Parameters:
//   - m: validated manifest
//   - tree: hashed tree
func catalogOf(m *Manifest, tree *Tree) Catalog {
	tables := map[string]map[string]string{}

	for _, rel := range sortedKeys(localeFiles) {
		if table, issues := ParseLocale(tree.data[rel]); len(issues) == 0 && tree.data[rel] != nil {
			tables[localeFiles[rel]] = table
		}
	}

	return NewCatalog(MergeLocales(m, tables))
}

// describe adds what the check shows besides the findings of the files: the required programs,
// the settings that were rejected and the argv and directory of every contribution.
//
// Parameters:
//   - res: result being filled, with its manifest findings in place
//   - p: the plugin, installed or built from a directory
//   - lc: inputs of the pass
//   - req: the request
func (r *Registry) describe(res *CheckResult, p Plugin, lc *loadContext, req CheckRequest) {
	m := p.Manifest

	for _, rq := range m.Requires {
		path, found := LookBin(lc.pathEnv, rq.Bin, lc.avoid)

		res.Requires = append(res.Requires, Requirement{
			Bin:      rq.Bin,
			Hint:     p.Catalog.Text(req.lang(), "requires."+rq.Bin+".hint", rq.Hint),
			Optional: rq.Optional, Found: found, Path: path,
		})

		if !found {
			code, sev := ReasonMissingDep, SeverityWarning
			if rq.Optional {
				code = ReasonOptionalDepAbsent
			}

			res.Findings = append(res.Findings, Finding{
				Scope: ScopeEnv, Code: code,
				Severity: sev, Path: "requires." + rq.Bin, Args: []string{rq.Bin},
			})
		}
	}

	res.Findings = append(res.Findings, r.settingFindings(res, p)...)

	if !req.noArgv {
		res.Contributions, res.Findings = r.contributions(res, p, lc, req)
	}
}

// settingFindings reports the settings values that were ignored and the settings file that could
// not be used. They are about the user's files, not about the package, so --strict ignores them.
//
// Parameters:
//   - res: result being filled; the rejected values are added to it
//   - p: the plugin
func (r *Registry) settingFindings(res *CheckResult, p Plugin) []Finding {
	var out []Finding

	view := r.Settings(p)

	for _, sv := range view.Values {
		for _, rej := range sv.Rejected {
			res.Rejected = append(res.Rejected, SettingNote{
				Setting: sv.ID, Source: rej.Source,
				Code: rej.Code, Raw: rej.Raw,
			})

			out = append(out, Finding{
				Scope: ScopeSettings, Code: CodeSettingsIgnore,
				Severity: SeverityWarning, Path: "setting." + sv.ID,
				Args: []string{sv.ID, rej.Source, rej.Code},
			})
		}
	}

	if view.Problem != nil {
		out = append(out, Finding{
			Scope: ScopeSettings, Code: CodeSettingsFile,
			Severity: SeverityWarning, Line: view.Problem.Line,
			Args: []string{view.Problem.Code},
		})
	}

	for _, key := range view.Unknown {
		out = append(out, Finding{
			Scope: ScopeSettings, Code: CodeSettingsKey,
			Severity: SeverityWarning, Args: []string{key},
		})
	}

	return out
}

// checkDirectory examines a directory of the user.
//
// Parameters:
//   - ctx: cancels the copy
//   - req: the request
//   - path: absolute, cleaned directory
func (r *Registry) checkDirectory(ctx context.Context, req CheckRequest, path string) (*CheckResult,
	error,
) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, wrapError(CodeBadSource, err, req.Target)
	}

	defer func() { _ = root.Close() }()

	return r.checkStaged(ctx, req, dirSource{root: root}, TargetDirectory, req.Official, nil)
}

// checkArchive examines an archive file of the user: it is extracted by the rules of an install
// into a private temporary directory and examined there, and the result carries the sha256 of the
// archive, which an author publishes next to the release as <asset>.sha256.
//
// Parameters:
//   - ctx: cancels the extraction
//   - req: the request
//   - sp: the classified archive source
func (r *Registry) checkArchive(ctx context.Context, req CheckRequest, sp sourceSpec) (*CheckResult,
	error,
) {
	res := newResult(req, TargetArchive)

	tmp, err := os.MkdirTemp("", "tote-check-")
	if err != nil {
		return nil, wrapError(CodeDisk, err, os.TempDir())
	}

	defer func() { _ = removeTree(tmp) }()

	dst, err := os.OpenRoot(tmp)
	if err != nil {
		return nil, wrapError(CodeDisk, err, tmp)
	}

	defer func() { _ = dst.Close() }()

	sum, err := r.unpackSource(ctx, sp, tmp, dst, "", req.Official)
	if err != nil {
		var pe *Error
		if !errors.As(err, &pe) || pe.Code == CodeDisk {
			return nil, err
		}

		res.Findings = append(res.Findings, errorFinding(pe))

		return res, nil
	}

	res.ArchiveSHA256 = sum

	out, err := r.examineStaged(res, dst, tmp, req, req.Official, nil)

	// The hash of an archive that cannot be installed is not one to publish.
	if out != nil && out.Errors() > 0 {
		out.ArchiveSHA256 = ""
	}

	return out, err
}

// checkBundled examines the embedded copy of a bundled plugin that is not installed, and checks it
// against its pin: the version, the tree hash and the surface hash must be those compiled in.
//
// Parameters:
//   - ctx: cancels the copy
//   - req: the request
//   - id: bundled plugin id
func (r *Registry) checkBundled(ctx context.Context, req CheckRequest, id string) (*CheckResult,
	error,
) {
	fsys, version, tree, surface, _ := r.opts.Bundled.Lookup(id)

	return r.checkStaged(ctx, req, fsSource{fsys: fsys}, TargetBundled, true,
		&bundledPin{version: version, tree: tree, surface: surface})
}

// checkStaged copies a source into a private temporary directory, examines the copy and removes
// it. The copy follows the rules of an install, so a source that an install would refuse is a
// finding here, and the hashes are those the install would record.
//
// Parameters:
//   - ctx: cancels the copy
//   - req: the request
//   - src: the files to examine
//   - kind: one of the Target constants
//   - official: true when the plugin may use an official id
//   - pin: compiled-in pin to compare with, nil for none
func (r *Registry) checkStaged(ctx context.Context, req CheckRequest, src srcTree, kind string,
	official bool, pin *bundledPin,
) (*CheckResult, error) {
	res := newResult(req, kind)

	tmp, err := os.MkdirTemp("", "tote-check-")
	if err != nil {
		return nil, wrapError(CodeDisk, err, os.TempDir())
	}

	defer func() { _ = removeTree(tmp) }()

	dst, err := os.OpenRoot(tmp)
	if err != nil {
		return nil, wrapError(CodeDisk, err, tmp)
	}

	defer func() { _ = dst.Close() }()

	if cerr := (&copier{ctx: ctx, src: src, dst: dst}).run(); cerr != nil {
		var pe *Error
		if !errors.As(cerr, &pe) || pe.Code == CodeDisk {
			return nil, cerr
		}

		res.Findings = append(res.Findings, errorFinding(pe))

		return res, nil
	}

	return r.examineStaged(res, dst, tmp, req, official, pin)
}

// examineStaged examines a package that was copied or extracted into a private directory and
// completes the result with what it finds.
//
// Parameters:
//   - res: result being filled
//   - dst: the directory, opened as a root
//   - tmp: the directory
//   - req: the request
//   - official: true when the plugin may use an official id
//   - pin: compiled-in pin to compare with, nil for none
func (r *Registry) examineStaged(res *CheckResult, dst *os.Root, tmp string, req CheckRequest,
	official bool, pin *bundledPin,
) (*CheckResult, error) {
	tree, pm, err := r.examine(dst, tmp, "", official, "", false)
	if err != nil {
		return nil, err
	}

	r.fill(res, tree, pm, req)

	if pm.m != nil {
		if pin != nil {
			res.Findings = append(res.Findings, pinFindings(pm.m, tree, pin)...)
		}

		lc := r.context()
		p := Plugin{
			ID: pm.m.ID, Manifest: pm.m, Catalog: catalogOf(pm.m, tree),
			Header: pm.rep.Header, Dir: tmp,
		}

		r.describe(res, p, lc, req)
	}

	return res, nil
}

// fill copies the identity, the hashes and the findings of an examined package into a result.
//
// Parameters:
//   - res: result being filled
//   - tree: hashed tree
//   - pm: parsed manifest
//   - req: the request
func (r *Registry) fill(res *CheckResult, tree *Tree, pm *parsedManifest, req CheckRequest) {
	res.TreeSHA256, res.Files = tree.SHA256, tree.Files
	res.ID, res.Version, res.Tier, res.Title = pm.rep.Header.ID, pm.rep.Header.Version,
		pm.rep.Header.Tier, pm.rep.Header.Title

	res.Findings = append(res.Findings, findings(ScopeManifest, pm.rep.Issues)...)

	if pm.m == nil {
		return
	}

	res.Findings = append(res.Findings, findings(ScopePackage, pm.pkg)...)
	res.Findings = append(res.Findings, r.localeFindings(pm.m, tree, req)...)

	res.ID, res.Version, res.Tier, res.Title = pm.m.ID, pm.m.Version, pm.m.Tier, pm.m.Title
	res.Caps = EffectiveCaps(pm.m)
	res.SurfaceSHA256 = SurfaceSHA256(pm.m)
	res.Grant = GrantToken(res.Caps, res.SurfaceSHA256)
}

// pinFindings compares an examined bundled plugin with its compiled-in pin.
//
// Parameters:
//   - m: validated manifest
//   - tree: hashed tree
//   - pin: compiled-in pin
func pinFindings(m *Manifest, tree *Tree, pin *bundledPin) []Finding {
	var out []Finding

	for _, c := range []struct{ field, want, got string }{
		{"version", pin.version, m.Version},
		{"tree_sha256", pin.tree, tree.SHA256},
		{"surface_sha256", pin.surface, SurfaceSHA256(m)},
	} {
		if c.want != c.got {
			out = append(out, Finding{
				Scope: ScopePin, Code: CodePinMismatch,
				Severity: SeverityError, Path: c.field,
				Args: []string{c.field, short(c.want), short(c.got)},
			})
		}
	}

	return out
}

// short cuts a hash to the 12 hexadecimal digits that messages show.
//
// Parameters:
//   - s: text, usually a hash
func short(s string) string {
	if len(s) > grantHashLen {
		return s[:grantHashLen]
	}

	return s
}
