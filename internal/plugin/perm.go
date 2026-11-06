package plugin

import (
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Permission grammar limits.
const (
	maxPermissions = 64
	maxPermLen     = 128
	maxHostLen     = 253
	maxLabelLen    = 63
)

// Kinds of permission entry.
const (
	PermExec      = "exec"
	PermNet       = "net"
	PermFSRead    = "fs:read"
	PermFSWrite   = "fs:write"
	PermEnv       = "env"
	PermReadRepos = "host:read:repos"
	PermOpenURL   = "host:open-url"
)

// Derived capabilities, added to the declared ones by EffectiveCaps.
const (
	CapDerivedCode = "derived:code"
	CapDerivedTTY  = "derived:tty"
)

var (
	// execNameRe matches the program name of an exec: permission: a name, never a path.
	execNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

	// envNameRe matches the variable name of an env: permission.
	envNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

	// hostLabelRe matches one label of a net: host.
	hostLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// fsZones lists the zones of an fs: permission.
var fsZones = []string{"home", "cwd", "repo", "config", "ssh", "tmp", "any"}

// reservedPerms lists the permissions that are reserved and not available in 1.0.
var reservedPerms = []string{
	"host:read:connections", "host:handoff", "host:clipboard", "host:notify", "host:editor",
}

// refusedPerms lists the permissions that are never grantable in API 1.
var refusedPerms = []string{"hook:intercept", "host:write:settings"}

// Permission is one parsed permission entry.
//
// Kind is one of the Perm constants; Arg is the program, host, zone or variable name, and empty
// for the fixed host entries. Raw is the text as written.
type Permission struct {
	Raw  string
	Kind string
	Arg  string
}

// PermError explains why a permission entry was refused. Code is one of CodePermUnknown,
// CodePermInvalid, CodePermRefused, CodeReserved or CodeLimitExceeded; Reason is a short slug for
// the catalog; Index is the position of the entry in the list given to NormalizePermissions, and
// -1 when the error comes from ParsePermission alone.
type PermError struct {
	Code   string
	Reason string
	Index  int
}

// ParsePermission parses one permission entry against the closed vocabulary.
//
// Parameters:
//   - s: entry as written, at most 128 bytes: exec:<bin>, net:<host>, fs:read:<zone>,
//     fs:write:<zone>, env:<NAME>, host:read:repos or host:open-url
//
// Returns the permission, or a *PermError with a nil Permission on failure: perm_unknown for a
// string outside the vocabulary, perm_invalid for a known family with a bad argument,
// perm_refused for hook:intercept, host:write:settings and the blocked environment names, and
// reserved_not_available for the reserved host entries and env:TOTE_TOKEN.
func ParsePermission(s string) (Permission, *PermError) {
	if s == "" || len(s) > maxPermLen || !plainText(s) {
		return Permission{}, &PermError{CodePermInvalid, "form", -1}
	}

	switch {
	case slices.Contains(refusedPerms, s):
		return Permission{}, &PermError{CodePermRefused, "api1", -1}
	case slices.Contains(reservedPerms, s):
		return Permission{}, &PermError{CodeReserved, "reserved", -1}
	case s == PermReadRepos || s == PermOpenURL:
		return Permission{Raw: s, Kind: s}, nil
	}

	family, arg, ok := splitPerm(s)
	if !ok {
		return Permission{}, &PermError{CodePermUnknown, "family", -1}
	}

	if reason := checkPermArg(family, arg); reason != "" {
		code := CodePermInvalid

		switch reason {
		case "blocked":
			code = CodePermRefused
		case "reserved":
			code = CodeReserved
		}

		return Permission{}, &PermError{code, reason, -1}
	}

	return Permission{Raw: s, Kind: family, Arg: arg}, nil
}

// splitPerm splits an entry into its family and its argument.
//
// Parameters:
//   - s: entry as written
//
// Returns the family (one of the Perm kinds), the argument and true, or false for an unknown
// family.
func splitPerm(s string) (family, arg string, ok bool) {
	for _, f := range []string{PermFSRead, PermFSWrite, PermExec, PermNet, PermEnv} {
		if rest, found := strings.CutPrefix(s, f+":"); found {
			return f, rest, true
		}
	}

	return "", "", false
}

// checkPermArg checks the argument of a permission family.
//
// Parameters:
//   - family: one of the Perm kinds
//   - arg: text after the family
//
// Returns an empty string when the argument is fine, else a reason slug.
func checkPermArg(family, arg string) string {
	switch family {
	case PermExec:
		if !execNameRe.MatchString(arg) {
			return "name"
		}
	case PermNet:
		if !validNetHost(arg) {
			return "host"
		}
	case PermFSRead, PermFSWrite:
		if !slices.Contains(fsZones, arg) {
			return "zone"
		}
	case PermEnv:
		switch {
		case arg == "TOTE_TOKEN":
			return "reserved"
		case !envNameRe.MatchString(arg):
			return "name"
		case BlockedEnvName(arg):
			return "blocked"
		}
	}

	return ""
}

// validNetHost checks the host of a net: permission: lowercase labels, at least two of them, an
// optional leading "*.", no port, no IP literal, no userinfo; "*" and "*.com" are invalid.
//
// Parameters:
//   - host: text after "net:"
func validNetHost(host string) bool {
	if len(host) > maxHostLen {
		return false
	}

	labels := strings.Split(strings.TrimPrefix(host, "*."), ".")
	if len(labels) < 2 {
		return false
	}

	for _, l := range labels {
		if len(l) > maxLabelLen || !hostLabelRe.MatchString(l) {
			return false
		}
	}

	return !allDigits(labels[len(labels)-1])
}

// isWildcardHost reports whether a net: host is a "*.domain" wildcard.
//
// Parameters:
//   - host: text after "net:"
func isWildcardHost(host string) bool { return strings.HasPrefix(host, "*.") }

// NormalizePermissions validates a list of permission entries and returns it sorted.
//
// Parameters:
//   - list: entries as written, at most 64
//
// Returns the sorted entries, or the *PermError of the first entry that is refused, with its
// index. A duplicate, a list over 64 entries and an unknown entry are all refused.
func NormalizePermissions(list []string) ([]string, *PermError) {
	if len(list) > maxPermissions {
		return nil, &PermError{CodeLimitExceeded, "count", maxPermissions}
	}

	seen := map[string]bool{}

	for i, s := range list {
		if _, perr := ParsePermission(s); perr != nil {
			perr.Index = i

			return nil, perr
		}

		if seen[s] {
			return nil, &PermError{CodePermInvalid, "duplicate", i}
		}

		seen[s] = true
	}

	out := slices.Clone(list)
	sort.Strings(out)

	return out, nil
}

// NormalizeCaps sorts a capability set and drops duplicates. It does not validate.
//
// Parameters:
//   - caps: capability strings, permissions and derived ones
func NormalizeCaps(caps []string) []string {
	out := slices.Clone(caps)
	sort.Strings(out)

	return slices.Compact(out)
}

// Covers reports whether a capability set already grants one capability: it holds it, or it holds
// net:*.D and the capability is net:H with H a strict subdomain of D. Nothing else is implied,
// not even fs:write for fs:read: the rule may ask too often, never too rarely.
//
// Parameters:
//   - have: capability set, in any order
//   - p: capability to look for
func Covers(have []string, p string) bool {
	if slices.Contains(have, p) {
		return true
	}

	host, ok := strings.CutPrefix(p, "net:")
	if !ok || isWildcardHost(host) {
		return false
	}

	for _, h := range have {
		domain, found := strings.CutPrefix(h, "net:*.")
		if found && strings.HasSuffix(host, "."+domain) && len(host) > len(domain)+1 {
			return true
		}
	}

	return false
}

// Escalation computes the capabilities that a new version adds to what the user consented to.
//
// Parameters:
//   - old: consented effective capabilities
//   - next: effective capabilities of the new version
//
// Returns the sorted capabilities of next that old does not cover. The result is empty exactly
// when next is a subset of old up to the wildcard rule, so a dropped capability never asks.
func Escalation(old, next []string) []string {
	var added []string

	for _, p := range NormalizeCaps(next) {
		if !Covers(old, p) {
			added = append(added, p)
		}
	}

	return added
}

// Escalates reports whether a new version asks for more than the user consented to, by
// capabilities alone. The command surface is a separate input, see SurfaceEscalations.
//
// Parameters:
//   - old: consented effective capabilities
//   - next: effective capabilities of the new version
func Escalates(old, next []string) bool { return len(Escalation(old, next)) > 0 }

// sensitiveEnvWords and sensitiveEnvPrefixes mark the environment names that carry a credential.
var (
	sensitiveEnvWords = []string{
		"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "API_KEY", "PRIVATE",
	}
	sensitiveEnvPrefixes = []string{"AWS_", "AZURE_", "GOOGLE_", "GH_", "GITHUB_", "GITLAB_"}
)

// SensitiveEnv reports whether an environment variable name looks like a credential.
//
// Parameters:
//   - name: variable name without the env: prefix
//
// Returns true for SSH_AUTH_SOCK, names that contain TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL,
// API_KEY or PRIVATE, and names that start with AWS_, AZURE_, GOOGLE_, GH_, GITHUB_ or GITLAB_.
func SensitiveEnv(name string) bool {
	if name == "SSH_AUTH_SOCK" {
		return true
	}

	for _, w := range sensitiveEnvWords {
		if strings.Contains(name, w) {
			return true
		}
	}

	for _, p := range sensitiveEnvPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	return false
}
