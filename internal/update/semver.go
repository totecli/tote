package update

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrVersion reports a string that is not a strict Semantic Version.
var ErrVersion = errors.New("invalid version")

// Version is a parsed Semantic Version 2.0.0 such as 1.1.0 or 1.1.0-beta.1.
//
// Versions carry no "v" prefix and no build metadata, so neither is accepted. Pre holds the
// dot separated pre-release identifiers, empty for a stable release.
type Version struct {
	Major uint64
	Minor uint64
	Patch uint64
	Pre   []string
}

// ParseVersion parses a strict Semantic Version.
//
// Parameters:
//   - s: text such as 1.2.3 or 1.2.3-beta.10; no "v" prefix (the release code strips the one
//     of a tag), no build metadata, no leading zero in a number, no empty identifier
//
// Returns the version, or an error wrapping ErrVersion.
func ParseVersion(s string) (Version, error) {
	core, pre, hasPre := strings.Cut(s, "-")

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%w: %q", ErrVersion, s)
	}

	var nums [3]uint64

	for i, p := range parts {
		n, err := parseNumber(p)
		if err != nil {
			return Version{}, fmt.Errorf("%w: %q", ErrVersion, s)
		}

		nums[i] = n
	}

	v := Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}
	if !hasPre {
		return v, nil
	}

	v.Pre = strings.Split(pre, ".")
	for _, id := range v.Pre {
		if !validIdentifier(id) {
			return Version{}, fmt.Errorf("%w: %q", ErrVersion, s)
		}
	}

	return v, nil
}

// parseNumber parses a version number without a sign, a leading zero or a non-digit.
//
// Parameters:
//   - s: digits only, "0" or starting with 1 to 9
//
// Returns the number, or an error for any other text or an overflow.
func parseNumber(s string) (uint64, error) {
	if s == "" || (len(s) > 1 && s[0] == '0') || !allDigits(s) {
		return 0, ErrVersion
	}

	return strconv.ParseUint(s, 10, 64)
}

// allDigits reports whether a non-empty string is made of ASCII digits only.
//
// Parameters:
//   - s: string to test
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return s != ""
}

// validIdentifier reports whether a pre-release identifier is valid.
//
// Parameters:
//   - id: one dot separated identifier; ASCII letters, digits and hyphens, not empty, and a
//     purely numeric one has no leading zero
func validIdentifier(id string) bool {
	if id == "" {
		return false
	}

	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '-' {
			return false
		}
	}

	return !allDigits(id) || id == "0" || id[0] != '0'
}

// String returns the canonical text of the version.
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}

	return s
}

// Prerelease reports whether the version has a pre-release part.
func (v Version) Prerelease() bool {
	return len(v.Pre) > 0
}

// Compare orders two versions by the precedence rules of Semantic Versioning.
//
// A pre-release sorts before the release it precedes, numeric identifiers sort numerically and
// before alphanumeric ones, and alphanumeric identifiers sort in ASCII order, so that beta.2 is
// below beta.10 and 1.1.0-beta.1 is below 1.1.0.
//
// Parameters:
//   - o: version to compare with
//
// Returns -1 when v is below o, 0 when they are equal and 1 when v is above o.
func (v Version) Compare(o Version) int {
	if c := cmpUint(v.Major, o.Major); c != 0 {
		return c
	}

	if c := cmpUint(v.Minor, o.Minor); c != 0 {
		return c
	}

	if c := cmpUint(v.Patch, o.Patch); c != 0 {
		return c
	}

	switch {
	case len(v.Pre) == 0 && len(o.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(o.Pre) == 0:
		return -1
	}

	for i := 0; i < len(v.Pre) && i < len(o.Pre); i++ {
		if c := cmpIdentifier(v.Pre[i], o.Pre[i]); c != 0 {
			return c
		}
	}

	return cmpUint(uint64(len(v.Pre)), uint64(len(o.Pre)))
}

// cmpUint orders two numbers.
//
// Parameters:
//   - a: first number
//   - b: second number
//
// Returns -1, 0 or 1.
func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}

	return 0
}

// cmpIdentifier orders two pre-release identifiers.
//
// Parameters:
//   - a: first identifier
//   - b: second identifier
//
// Returns -1, 0 or 1. Numeric identifiers have no leading zero, so a longer one is a larger
// one and no conversion can overflow.
func cmpIdentifier(a, b string) int {
	an, bn := allDigits(a), allDigits(b)

	switch {
	case an && bn:
		if len(a) != len(b) {
			return cmpUint(uint64(len(a)), uint64(len(b)))
		}

		return strings.Compare(a, b)
	case an:
		return -1
	case bn:
		return 1
	}

	return strings.Compare(a, b)
}

// Channel names a stream of releases.
type Channel string

const (
	// ChannelStable is the default channel: releases without a pre-release part.
	ChannelStable Channel = "stable"

	// ChannelBeta also offers pre-releases such as 1.1.0-beta.1.
	ChannelBeta Channel = "beta"
)

// ChannelOf returns the channel a version is published on.
//
// Parameters:
//   - v: parsed version
//
// Returns ChannelBeta for a pre-release and ChannelStable otherwise.
func ChannelOf(v Version) Channel {
	if v.Prerelease() {
		return ChannelBeta
	}

	return ChannelStable
}
