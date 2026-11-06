package plugin

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrVersion is wrapped by every error about a plugin version that is not a strict Semantic
// Version.
var ErrVersion = errors.New("invalid version")

// ErrRange is wrapped by every error about a tote range that breaks the grammar.
var ErrRange = errors.New("invalid version range")

// Version and range limits.
const (
	maxVersionLen = 64
	maxRangeLen   = 128
	maxComparator = 8
)

// Version is a parsed Semantic Version 2.0.0 such as 1.1.0, 1.1.0-beta.1 or 1.1.0+build.5.
//
// Pre holds the dot separated pre-release identifiers, empty for a stable release. Build holds the
// build metadata identifiers, which never take part in precedence. There is no "v" prefix.
type Version struct {
	Major uint64
	Minor uint64
	Patch uint64
	Pre   []string
	Build []string
}

// ParseVersion parses a strict Semantic Version. The module keeps its own parser: the plugin
// package does not import internal/update.
//
// Parameters:
//   - s: text such as 1.2.3, 1.2.3-beta.10 or 1.2.3+meta; no "v" prefix, no leading zero in a
//     number or a numeric pre-release identifier, no empty identifier, at most 64 bytes
//
// Returns the version, or an error wrapping ErrVersion.
func ParseVersion(s string) (Version, error) {
	fail := fmt.Errorf("%w: %q", ErrVersion, CleanLine(s, issueArgRunes))

	if len(s) > maxVersionLen {
		return Version{}, fail
	}

	rest, build, hasBuild := strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(rest, "-")

	nums, ok := parseCore(core, 3)
	if !ok {
		return Version{}, fail
	}

	v := Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}

	if hasPre {
		v.Pre = strings.Split(pre, ".")
		for _, id := range v.Pre {
			if !validPreIdentifier(id) {
				return Version{}, fail
			}
		}
	}

	if hasBuild {
		v.Build = strings.Split(build, ".")
		for _, id := range v.Build {
			if !validBuildIdentifier(id) {
				return Version{}, fail
			}
		}
	}

	return v, nil
}

// parseCore parses the dot separated numbers of a version.
//
// Parameters:
//   - core: text such as 1.2.3
//   - want: exact number of parts
//
// Returns the numbers and true, or false for a wrong count, a sign, a leading zero, a non-digit or
// an overflow.
func parseCore(core string, want int) ([3]uint64, bool) {
	var nums [3]uint64

	parts := strings.Split(core, ".")
	if len(parts) != want {
		return nums, false
	}

	for i, p := range parts {
		n, ok := parseNumber(p)
		if !ok {
			return nums, false
		}

		nums[i] = n
	}

	return nums, true
}

// parseNumber parses a version number: digits only, "0" or starting with 1 to 9.
//
// Parameters:
//   - s: text to parse
func parseNumber(s string) (uint64, bool) {
	if !allDigits(s) || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}

	n, err := strconv.ParseUint(s, 10, 64)

	return n, err == nil
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

// validBuildIdentifier reports whether a build metadata identifier is valid: ASCII letters, digits
// and hyphens, not empty. Leading zeros are allowed.
//
// Parameters:
//   - id: one dot separated identifier
func validBuildIdentifier(id string) bool {
	if id == "" {
		return false
	}

	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '-' {
			return false
		}
	}

	return true
}

// validPreIdentifier reports whether a pre-release identifier is valid: a build identifier whose
// numeric form has no leading zero.
//
// Parameters:
//   - id: one dot separated identifier
func validPreIdentifier(id string) bool {
	return validBuildIdentifier(id) && (!allDigits(id) || id == "0" || id[0] != '0')
}

// String returns the canonical text of the version, build metadata included.
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}

	if len(v.Build) > 0 {
		s += "+" + strings.Join(v.Build, ".")
	}

	return s
}

// Prerelease reports whether the version has a pre-release part.
func (v Version) Prerelease() bool { return len(v.Pre) > 0 }

// Compare orders two versions by the precedence rules of Semantic Versioning: a pre-release sorts
// before the release it precedes, numeric identifiers sort numerically and before alphanumeric
// ones, and build metadata is ignored.
//
// Parameters:
//   - o: version to compare with
//
// Returns -1 when v is below o, 0 when they have the same precedence and 1 when v is above o.
func (v Version) Compare(o Version) int {
	for _, p := range [][2]uint64{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if c := cmpUint(p[0], p[1]); c != 0 {
			return c
		}
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

// cmpIdentifier orders two pre-release identifiers: numeric ones numerically and before the
// others, the others in ASCII order.
//
// Parameters:
//   - a: first identifier
//   - b: second identifier
//
// Returns -1, 0 or 1. A numeric identifier has no leading zero, so a longer one is a larger one
// and no conversion can overflow.
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

// Comparator is one clause of a Range: an operator and a version completed with zeros.
//
// Op is one of ">=", ">", "<=", "<" and "=".
type Comparator struct {
	Op string
	V  Version
}

// Range is a parsed tote range: comparators that must all hold.
type Range struct {
	Comps []Comparator
}

// rangeOps lists the operators, longest first so that ">=" is not read as ">".
var rangeOps = []string{">=", "<=", ">", "<", "="}

// ParseRange parses the tote range grammar of API 1.
//
// Parameters:
//   - s: comparators joined by commas, each an optional operator among >= > <= < = (none means =)
//     and a version MAJOR[.MINOR[.PATCH]] whose missing parts are zero, so that "<2" is "<2.0.0";
//     spaces are free around the operator and the commas; there is no ||, ^, ~, wildcard,
//     pre-release or build part; at most 128 bytes and 8 comparators
//
// Returns the range, or an error wrapping ErrRange.
func ParseRange(s string) (Range, error) {
	fail := fmt.Errorf("%w: %q", ErrRange, CleanLine(s, issueArgRunes))

	if len(s) > maxRangeLen {
		return Range{}, fail
	}

	parts := strings.Split(s, ",")
	if len(parts) > maxComparator {
		return Range{}, fail
	}

	var r Range

	for _, part := range parts {
		c, ok := parseComparator(strings.TrimSpace(part))
		if !ok {
			return Range{}, fail
		}

		r.Comps = append(r.Comps, c)
	}

	return r, nil
}

// parseComparator parses one clause of a range.
//
// Parameters:
//   - text: clause without surrounding spaces
func parseComparator(text string) (Comparator, bool) {
	op := "="

	for _, candidate := range rangeOps {
		if strings.HasPrefix(text, candidate) {
			op = candidate
			text = strings.TrimSpace(text[len(candidate):])

			break
		}
	}

	parts := strings.Split(text, ".")
	if len(parts) > 3 {
		return Comparator{}, false
	}

	var nums [3]uint64

	for i, p := range parts {
		n, ok := parseNumber(p)
		if !ok {
			return Comparator{}, false
		}

		nums[i] = n
	}

	return Comparator{Op: op, V: Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}}, true
}

// String returns the canonical text of the range, with the missing parts written out.
func (r Range) String() string {
	parts := make([]string, len(r.Comps))
	for i, c := range r.Comps {
		parts[i] = c.Op + c.V.String()
	}

	return strings.Join(parts, ", ")
}

// Satisfies reports whether a version is inside the range. The pre-release and build parts of v
// are ignored, so that 1.1.0-beta.1 satisfies >=1.1.0: a beta tester can try plugins written for
// the release.
//
// Parameters:
//   - v: version to test
func (r Range) Satisfies(v Version) bool {
	v = Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch}

	for _, c := range r.Comps {
		cmp := v.Compare(c.V)

		var ok bool

		switch c.Op {
		case ">=":
			ok = cmp >= 0
		case ">":
			ok = cmp > 0
		case "<=":
			ok = cmp <= 0
		case "<":
			ok = cmp < 0
		default:
			ok = cmp == 0
		}

		if !ok {
			return false
		}
	}

	return true
}

// devVersionRe matches the Go pseudo-version that a plain go build records for a checkout, in its
// three forms: vX.0.0-<time>-<commit>, vX.Y.Z-pre.0.<time>-<commit> and vX.Y.Z-0.<time>-<commit>,
// where <time> is 14 digits and <commit> 12 hexadecimal digits.
var devVersionRe = regexp.MustCompile(`[-.][0-9]{14}-[0-9a-f]{12}$`)

// IsDevVersion reports whether a host version names a development build: "dev", the "0.0.0-dev"
// that `make build` writes by default, or the pseudo-version of a build made by `go build` or
// `go install` from a checkout. None of them is a release, and none can be compared with a range.
//
// Parameters:
//   - host: version of tote as built into the binary
func IsDevVersion(host string) bool {
	return host == "dev" || host == "0.0.0-dev" || devVersionRe.MatchString(host)
}

// Allows reports whether the running tote is inside the range.
//
// Parameters:
//   - host: version of tote as built into the binary; a development build (see IsDevVersion)
//     satisfies every range
//
// Returns the answer, or an error wrapping ErrVersion when host is neither a development build nor
// a version.
func (r Range) Allows(host string) (bool, error) {
	if IsDevVersion(host) {
		return true, nil
	}

	v, err := ParseVersion(host)
	if err != nil {
		return false, err
	}

	return r.Satisfies(v), nil
}
