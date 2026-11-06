package plugin

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/totecli/tote/internal/config"
)

// CodeVersionInvalid reports a plugin version that is not a strict Semantic Version.
const CodeVersionInvalid = "version_invalid"

var (
	// licenseRe matches the shape of an SPDX identifier or expression. The SPDX list itself is
	// not consulted.
	licenseRe = regexp.MustCompile(`^[A-Za-z0-9.+-]+( (AND|OR|WITH) [A-Za-z0-9.+-]+)*$`)

	// contributionIDRe matches the id of a contribution.
	contributionIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

	// snakeIDRe matches the id of a setting, a field or a column.
	snakeIDRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

	// binSegmentRe matches one segment of a path inside a package.
	binSegmentRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// reservedFieldIDs lists the ids that no form field may take: they name the other scopes.
var reservedFieldIDs = []string{"repo", "conn", "setting", "item", "cwd", "home"}

// validator checks a manifest that extract has read.
//
// perms is the set of declared permissions and execIdx maps each exec: program to the index of
// its permission, to report the ones nothing uses. settings maps the setting ids to their types and
// patterns caches the verdict on each distinct pattern.
type validator struct {
	c        *collector
	m        *Manifest
	opts     DecodeOptions
	perms    map[string]bool
	execIdx  map[string]int
	execUsed map[string]bool
	settings map[string]string
	patterns map[string]compiledPattern
}

// validate runs the semantic rules over a manifest.
//
// Parameters:
//   - c: collector that receives the issues
//   - m: manifest read by extract
//   - opts: how the manifest is read
func validate(c *collector, m *Manifest, opts DecodeOptions) {
	v := &validator{
		c: c, m: m, opts: opts, perms: map[string]bool{}, execIdx: map[string]int{},
		execUsed: map[string]bool{}, settings: map[string]string{},
	}

	for _, s := range m.Settings {
		v.settings[s.ID] = s.Type
	}

	v.identity()
	v.permissions()
	v.counts()
	v.requires()
	v.tierShape()
	v.settingRules()
	v.contributions()
	v.unusedExec()
}

// identity checks the id, the version, the license and the homepage.
func (v *validator) identity() {
	m := v.m

	if m.ID != "" {
		v.pluginID()
	}

	if m.Version != "" {
		if _, err := ParseVersion(m.Version); err != nil {
			v.c.add(CodeVersionInvalid, "version", m.Version)
		}
	}

	if m.License != "" && !licenseRe.MatchString(m.License) {
		v.c.add(CodeValueInvalid, "license", m.License)
	}

	if m.Homepage != "" && !validHomepage(m.Homepage) {
		v.c.add(CodeValueInvalid, "homepage", m.Homepage)
	}

	if len(m.Platforms) > 1 && m.Platforms[0] == m.Platforms[1] {
		v.c.add(CodeValueInvalid, "platforms", m.Platforms[0])
	}
}

// pluginID checks the id against the grammar, the reserved names and the expected id.
func (v *validator) pluginID() {
	id := v.m.ID

	if err := config.ValidPluginID(id); err != nil {
		v.c.add(CodeIDInvalid, "id", id, "grammar")

		return
	}

	switch reason := ReservedID(id); {
	case v.opts.Installed:
		// A reservation added after the install never breaks the plugin (API 1, section 3.3).
	case reason == ReservedOfficial && !v.opts.Official && !isOfficialOrigin(v.opts.Origin, id):
		v.c.add(CodeIDInvalid, "id", id, ReservedOfficial)
	case reason != "" && reason != ReservedOfficial:
		v.c.add(CodeIDInvalid, "id", id, reason)
	}

	if v.opts.WantID != "" && id != v.opts.WantID {
		v.c.add(CodeIDInvalid, "id", id, "mismatch", v.opts.WantID)
	}
}

// validHomepage checks that a homepage is an https URL without user information.
//
// Parameters:
//   - s: URL text, at most 256 bytes
func validHomepage(s string) bool {
	if strings.ContainsAny(s, " \\") {
		return false
	}

	u, err := url.Parse(s)

	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
		u.Opaque == ""
}

// permissions checks every permission entry and records the declared set.
func (v *validator) permissions() {
	m := v.m
	seen := map[string]bool{}
	bad := false

	for i, s := range m.Permissions {
		path := "permissions[" + itoa(i) + "]"

		p, perr := ParsePermission(s)
		if perr != nil {
			v.c.add(perr.Code, path, s, perr.Reason)

			bad = true

			continue
		}

		if seen[s] {
			v.c.add(CodePermInvalid, path, s, "duplicate")

			bad = true

			continue
		}

		seen[s] = true
		v.perms[s] = true

		if p.Kind == PermExec {
			v.execIdx[p.Arg] = i

			v.execPermission(path, p.Arg)
		}
	}

	if !bad {
		m.Permissions = slices.Clone(m.Permissions)
		slices.Sort(m.Permissions)
	}
}

// execPermission applies the rules that bind an exec: permission: tote itself is never allowed,
// and a shell or launcher in a declarative plugin is a lint error.
//
// Parameters:
//   - path: issue path of the permission
//   - name: program name
func (v *validator) execPermission(path, name string) {
	switch {
	case v.isSelf(name):
		v.c.add(CodeExecSelf, path, name)
	case v.m.Tier == TierDeclarative && IsShellName(name):
		v.c.add(CodeArgvShell, path, name)
	}
}

// isSelf reports whether a program name is tote or the base name of the running binary. The
// comparison ignores case, because a case-insensitive file system resolves Tote to tote.
//
// Parameters:
//   - name: program name
func (v *validator) isSelf(name string) bool {
	if strings.EqualFold(name, "tote") {
		return true
	}

	for _, s := range v.opts.Host.Self {
		if s != "" && strings.EqualFold(name, s) {
			return true
		}
	}

	return false
}

// counts checks the number of contributions.
func (v *validator) counts() {
	m := v.m
	n := len(m.Commands) + len(m.ItemActions) + len(m.Actions) + len(m.Lists) + len(m.Settings)

	if n > MaxContributions {
		v.c.add(CodeLimitExceeded, "", "contributions", itoa(MaxContributions))
	}
}

// requires checks the [[requires]] entries.
func (v *validator) requires() {
	seen := map[string]bool{}

	for i, r := range v.m.Requires {
		path := "requires[" + itoa(i) + "]"

		if r.Bin == "" {
			continue
		}

		if !execNameRe.MatchString(r.Bin) {
			v.c.add(CodeValueInvalid, path+".bin", r.Bin)
		}

		if seen[r.Bin] {
			v.c.add(CodeIDDuplicate, path+".bin", r.Bin)
		}

		seen[r.Bin] = true
	}
}

// tierShape checks the rules that tie the tables to the tier: a declarative plugin has no program
// of its own, an exec plugin has exactly one way to name it.
func (v *validator) tierShape() {
	m := v.m

	if m.Tier == TierDeclarative {
		v.noTable(m.Exec != nil, "exec")
		v.noTable(len(m.Builds) > 0, "platform")
		v.noTable(len(m.Actions) > 0, "action")
		v.noTable(len(m.Lists) > 0, "list")

		return
	}

	v.execShape()
}

// noTable reports a table that the declarative tier does not have.
//
// Parameters:
//   - present: true when the manifest has the table
//   - table: name of the table
func (v *validator) noTable(present bool, table string) {
	if present {
		v.c.add(CodeTierMismatch, table, table, TierDeclarative)
	}
}

// execShape checks [exec] and [[platform]] of the exec tier.
func (v *validator) execShape() {
	m := v.m
	single := m.Exec != nil && m.Exec.Bin != ""

	switch {
	case single && len(m.Builds) > 0:
		v.c.add(CodeKeyConflict, "exec.bin", "bin", "platform")
	case !single && len(m.Builds) == 0:
		v.c.add(CodeKeyMissing, "exec.bin")
	}

	if single && (!validPackagePath(m.Exec.Bin) || binIsData(m.Exec.Bin)) {
		v.c.add(CodeValueInvalid, "exec.bin", m.Exec.Bin)
	}

	seen := map[string]bool{}

	for i, b := range m.Builds {
		path := "platform[" + itoa(i) + "]"

		if b.Bin != "" && (!validPackagePath(b.Bin) || binIsData(b.Bin)) {
			v.c.add(CodeValueInvalid, path+".bin", b.Bin)
		}

		key := b.OS + "/" + b.Arch
		if seen[key] {
			v.c.add(CodeIDDuplicate, path, key)
		}

		seen[key] = true
	}

	v.modes()
}

// modes checks that the request kinds in [exec].modes cover the tables that need them.
func (v *validator) modes() {
	m := v.m
	if m.Exec == nil || len(m.Exec.Modes) == 0 {
		return
	}

	if len(m.Lists) > 0 && !slices.Contains(m.Exec.Modes, "list") {
		v.c.add(CodeValueInvalid, "exec.modes", "list")
	}

	if len(m.Actions) > 0 && !slices.Contains(m.Exec.Modes, "run") {
		v.c.add(CodeValueInvalid, "exec.modes", "run")
	}
}

// binIsData reports whether a program path names a file that the host parses as data, the
// manifest or a locale file: such a file must not also be a program.
//
// Parameters:
//   - p: path as written in the manifest, already valid under validPackagePath
func binIsData(p string) bool {
	lower := strings.ToLower(p)

	return lower == "plugin.toml" || strings.HasPrefix(lower, "locales/")
}

// validPackagePath checks a program path inside a package: relative, no "..", no backslash, each
// segment in [A-Za-z0-9._-]+ and not starting with "-", at most 6 segments.
//
// Parameters:
//   - p: path as written in the manifest
func validPackagePath(p string) bool {
	if p == "" || len(p) > MaxBinBytes || strings.HasPrefix(p, "/") {
		return false
	}

	segs := strings.Split(p, "/")
	if len(segs) > MaxBinDepth {
		return false
	}

	for _, s := range segs {
		if s == "." || s == ".." || strings.HasPrefix(s, "-") || !binSegmentRe.MatchString(s) {
			return false
		}
	}

	return true
}

// unusedExec warns about the exec: permissions of a declarative plugin that no command uses.
func (v *validator) unusedExec() {
	if v.m.Tier != TierDeclarative {
		return
	}

	for _, name := range sortedKeys(v.execIdx) {
		if !v.execUsed[name] {
			v.c.warn(CodeExecUnused, "permissions["+itoa(v.execIdx[name])+"]", name)
		}
	}
}
