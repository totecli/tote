package plugin

import (
	"strings"
)

// grantHashLen is the number of hexadecimal digits of the surface hash kept in a grant token.
const grantHashLen = 12

// Groups of capabilities, in the order of the consent view.
const (
	GroupTTY     = "tty"
	GroupExec    = "exec"
	GroupNetwork = "network"
	GroupFiles   = "files"
	GroupEnv     = "environment"
	GroupHost    = "host"
)

// EffectiveCaps computes the capabilities that consent and escalation work on: the declared
// permissions plus the derived ones. derived:code is added for the exec tier, derived:tty when any
// entry that tote runs has output = "terminal".
//
// Parameters:
//   - m: validated manifest
//
// Returns the sorted, unique capability strings.
func EffectiveCaps(m *Manifest) []string {
	caps := append([]string(nil), m.Permissions...)

	if m.Tier == TierExec {
		caps = append(caps, CapDerivedCode)
	}

	if usesTerminal(m) {
		caps = append(caps, CapDerivedTTY)
	}

	return NormalizeCaps(caps)
}

// usesTerminal reports whether any contribution that tote runs hands the terminal over.
//
// Parameters:
//   - m: manifest
func usesTerminal(m *Manifest) bool {
	sets := [][]Entry{m.Commands, m.ItemActions, m.Actions}
	for _, l := range m.Lists {
		sets = append(sets, l.Actions)
	}

	for _, set := range sets {
		for _, e := range set {
			if e.Output == "terminal" {
				return true
			}
		}
	}

	return false
}

// ValidCap checks a capability string as stored in the lock: a permission of the closed
// vocabulary or one of the two derived capabilities.
//
// Parameters:
//   - s: capability string
func ValidCap(s string) bool {
	if s == CapDerivedCode || s == CapDerivedTTY {
		return true
	}

	_, perr := ParsePermission(s)

	return perr == nil
}

// GrantToken builds the token that --grant must equal: the sorted effective capabilities joined by
// commas, an at sign, and the first 12 hexadecimal digits of the surface hash. It binds both what
// the plugin may reach and what it runs.
//
// Parameters:
//   - caps: effective capabilities, in any order
//   - surface: surface_sha256 in hexadecimal
func GrantToken(caps []string, surface string) string {
	if len(surface) > grantHashLen {
		surface = surface[:grantHashLen]
	}

	return strings.Join(NormalizeCaps(caps), ",") + "@" + surface
}

// CapGroup tells which group of the consent view a capability belongs to.
//
// Parameters:
//   - c: capability string
//
// Returns one of the Group constants, or an empty string for a string outside the vocabulary.
func CapGroup(c string) string {
	switch {
	case c == CapDerivedTTY:
		return GroupTTY
	case c == CapDerivedCode || strings.HasPrefix(c, "exec:"):
		return GroupExec
	case strings.HasPrefix(c, "net:"):
		return GroupNetwork
	case strings.HasPrefix(c, "fs:"):
		return GroupFiles
	case strings.HasPrefix(c, "env:"):
		return GroupEnv
	case strings.HasPrefix(c, "host:"):
		return GroupHost
	}

	return ""
}

// Enforced tells whether tote itself enforces a capability or the consent view can only declare
// it. Every consent line says which.
//
// Parameters:
//   - c: capability string
//   - tier: tier of the plugin
//
// Returns true for the capabilities tote enforces: exec: and env: for a declarative plugin,
// host:read:repos and host:open-url for the request or the key tote handles, and the two derived
// capabilities. net: and fs: are declared only for both tiers, exec: and env: for the exec tier.
func Enforced(c, tier string) bool {
	switch {
	case c == CapDerivedCode || c == CapDerivedTTY:
		return true
	case c == PermReadRepos || c == PermOpenURL:
		return true
	case strings.HasPrefix(c, "exec:") || strings.HasPrefix(c, "env:"):
		return tier == TierDeclarative
	}

	return false
}

// Broad tells whether a capability is marked broad at consent: file access to the home, the ssh
// directory or anywhere, a wildcard network host, and a sensitive environment variable.
//
// Parameters:
//   - c: capability string
func Broad(c string) bool {
	p, perr := ParsePermission(c)
	if perr != nil {
		return false
	}

	switch p.Kind {
	case PermFSRead, PermFSWrite:
		return p.Arg == "home" || p.Arg == "ssh" || p.Arg == "any"
	case PermNet:
		return isWildcardHost(p.Arg)
	case PermEnv:
		return SensitiveEnv(p.Arg)
	}

	return false
}
