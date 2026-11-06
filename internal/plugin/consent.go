package plugin

import "slices"

// Origin classes of a plugin, as the consent view names them: "official" only for a release of
// totecli/<id> for an official id, checked here and never taken from the manifest.
const (
	OriginOfficial  = "official"
	OriginCommunity = "community"
	OriginLocal     = "local"
	OriginBundled   = "bundled"
)

// Why a consent is asked, the codes of Diff.Why.
const (
	WhyNew        = "new"
	WhyUnconsent  = "not_consented"
	WhyCaps       = "caps_added"
	WhySurface    = "surface"
	WhyUnverified = "unverified"
	WhyForced     = "forced"
)

// Warnings of a Diff, besides the missing programs and the findings of the manifest.
const (
	WarnSourceChanged = "source_changed"
	WarnDowngrade     = "downgrade"
	WarnSameVersion   = "same_version"
	WarnPrerelease    = "prerelease"
)

// Grant is the user's consent, given to Commit, Enable and Grant. Prompt is true when the user
// accepted in the consent view. Token is the string of --grant, which must equal Diff.Grant. Both
// can be set; a token that does not match is refused whatever Prompt says.
type Grant struct {
	Token  string
	Prompt bool
}

// Diff is what an install or an update will do, as plain data for the consent view. The text of
// every field that came from the plugin is cleaned, but it is still the plugin's own words: the
// view labels it as such.
//
// Caps are the effective capabilities of the new version. For an update, Added are the ones the
// consent on record does not cover (an escalation), Removed the ones that are gone and Unchanged
// the number of the others. SurfaceChanges are the changes of the command surface that ask for a
// new consent; CommandChanges is the full diff of the commands, shown but not asked. Missing are
// the required programs that PATH does not have. NeedsConsent says that Commit refuses without a
// grant, Why says why, and Grant is the token that --grant must equal. OldVersion is empty for a
// new install. Program is the program of an exec plugin (the bin of [exec], else the first
// [[platform]] bin) and empty for a declarative one.
type Diff struct {
	ID             string
	Title          string
	Description    string
	Version        string
	OldVersion     string
	Tier           string
	Program        string
	Kind           string
	Origin         string
	Class          string
	Source         string
	TreeSHA256     string
	ArchiveSHA256  string
	SurfaceSHA256  string
	Update         bool
	Caps           []string
	Added          []string
	Removed        []string
	Unchanged      int
	Commands       []CommandRef
	CommandChanges []CommandChange
	SurfaceChanges []SurfaceChange
	Missing        []Requires
	Warnings       []Reason
	Issues         []Issue
	NeedsConsent   bool
	Why            []string
	Grant          string
}

// originClass names how far a plugin can be trusted by where it came from.
//
// Parameters:
//   - kind: release, local or bundled
//   - origin: lower case owner/repo of a release, else empty
//   - id: id of the plugin, as the manifest or the lock record carries it
func originClass(kind, origin, id string) string {
	switch {
	case kind == KindBundled:
		return OriginBundled
	case isOfficialOrigin(origin, id):
		return OriginOfficial
	case kind == KindLocal:
		return OriginLocal
	}

	return OriginCommunity
}

// OriginClass names how far a plugin can be trusted by where it came from, for the command line.
//
// Parameters:
//   - kind: KindRelease, KindLocal or KindBundled
//   - origin: lower case owner/repo of a release, else empty
//   - id: id of the plugin
//
// Returns one of OriginOfficial, OriginCommunity, OriginLocal and OriginBundled.
func OriginClass(kind, origin, id string) string { return originClass(kind, origin, id) }

// capsDelta compares the capabilities on record with those of a new version.
//
// Parameters:
//   - old: consented capabilities
//   - next: effective capabilities of the new version
//
// Returns the capabilities that old does not cover (the escalation), those of old that next no
// longer has, and the number of the capabilities of next that old has.
func capsDelta(old, next []string) (added, removed []string, unchanged int) {
	added = Escalation(old, next)

	for _, c := range NormalizeCaps(old) {
		if !slices.Contains(next, c) {
			removed = append(removed, c)
		}
	}

	for _, c := range next {
		if slices.Contains(old, c) {
			unchanged++
		}
	}

	return added, removed, unchanged
}

// checkGrant decides whether a grant covers what the install asks.
//
// Parameters:
//   - d: the diff the user saw
//   - g: consent given
//
// Returns the consented_by value to record ("" when no consent is needed, so the one on record
// stays), or an *Error: grant_mismatch when the token differs from the diff (exit 4, so a script
// breaks instead of widening), consent_needed when none was given (exit 2). A token is checked
// only when consent is asked: a refreshed surface of a non-escalating update cannot widen
// anything, and a script that passes the token of the old version must not break on it.
func checkGrant(d Diff, g Grant) (string, error) {
	if !d.NeedsConsent {
		return "", nil
	}

	if g.Token != "" && g.Token != d.Grant {
		return "", newError(CodeGrantMismatch, d.Grant)
	}

	switch {
	case g.Prompt:
		return ConsentPrompt, nil
	case g.Token != "":
		return ConsentGrant, nil
	}

	return "", newError(CodeConsentNeeded, d.Grant)
}

// programOf names the program of an exec plugin for the consent view.
//
// Parameters:
//   - m: validated manifest
//
// Returns the bin of [exec], else the bin of the first [[platform]] table, else an empty string,
// which is also the answer for a declarative plugin.
func programOf(m *Manifest) string {
	switch {
	case m.Tier != TierExec || m.Exec == nil:
		return ""
	case m.Exec.Bin != "":
		return m.Exec.Bin
	case len(m.Builds) > 0:
		return m.Builds[0].Bin
	}

	return ""
}
