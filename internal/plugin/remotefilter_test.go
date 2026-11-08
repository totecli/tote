package plugin

import (
	"regexp"
	"testing"
	"time"
)

// TestSecReviewRemoteFilter pins what CleanRemote refuses: a remote helper that runs a program,
// a leading dash on the host or on the first path segment, a scheme outside https, ssh and git,
// credentials kept in the url, and a backslash that disguises the host.
//
// Parameters:
//   - t: test to report to
func TestSecReviewRemoteFilter(t *testing.T) {
	for _, raw := range []string{
		"ext::sh -c x", "EXT::sh", "file:///etc", "localhost:-x", "user@host:--upload-pack=x",
		"ssh://host/a b", "http://h/x", "ssh://h:-x/p", "-oProxyCommand=x:y", "--x:y", "a@b@c:p",
		"ftp://h/x", `https://github.com\@evil.com/o/r`,
	} {
		if out, ok := CleanRemote(raw); ok {
			t.Errorf("CleanRemote(%q) = %q, true", raw, out)
		}
	}

	if got, ok := CleanRemote("ssh://user:pw@github.com/o/r"); !ok || got != "ssh://github.com/o/r" {
		t.Errorf("credentials kept: %q %v", got, ok)
	}
}

// TestSecReviewPatternCost pins that a field pattern built to be expensive to compile (a counted
// repeat of a large Unicode class, the worst a 256-byte pattern can do) compiles in well under
// a millisecond, so a hostile manifest cannot slow Load down with its patterns.
//
// Parameters:
//   - t: test to report to
func TestSecReviewPatternCost(t *testing.T) {
	for _, pat := range []string{`^\pL{1000}$`, `^[\pL\pN\pM]{1000}$`, `^(\pL|\pN|\pS|\pP){1000}$`} {
		start := time.Now()

		re, err := regexp.Compile(pat)
		if err != nil {
			t.Fatalf("%s: %v", pat, err)
		}

		re.MatchString("a")

		if d := time.Since(start); d > 50*time.Millisecond {
			t.Errorf("%s took %v", pat, d)
		}
	}
}
