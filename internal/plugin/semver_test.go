package plugin

import (
	"errors"
	"strings"
	"testing"
)

// TestParseVersion checks the strict Semantic Version grammar.
//
// Parameters:
//   - t: test to report to
func TestParseVersion(t *testing.T) {
	good := map[string]string{
		"0.0.0":              "0.0.0",
		"1.2.3":              "1.2.3",
		"10.20.30":           "10.20.30",
		"1.1.0-beta.1":       "1.1.0-beta.1",
		"1.0.0-0":            "1.0.0-0",
		"1.0.0-alpha-1.x-y":  "1.0.0-alpha-1.x-y",
		"1.0.0+build.5":      "1.0.0+build.5",
		"1.0.0-rc.1+sha.abc": "1.0.0-rc.1+sha.abc",
		"1.0.0+001":          "1.0.0+001",
	}

	for in, want := range good {
		v, err := ParseVersion(in)
		if err != nil || v.String() != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %q", in, v, err, want)
		}
	}

	bad := []string{
		"", "1", "1.2", "1.2.3.4", "v1.2.3", "01.2.3", "1.02.3", "1.2.03", "1.2.-3", "1.2.3-",
		"1.2.3+", "1.2.3-01", "1.2.3-a..b", "1.2.3-.a", "1.2.3-a.", "1.2.3+a..b", "1.2.3-a_b",
		" 1.2.3", "1.2.3 ", "1.2.x", "a.b.c", "1.2.3-beta+", "99999999999999999999.0.0",
		strings.Repeat("1", 65), "1.0.0-" + strings.Repeat("a", 60),
	}

	for _, in := range bad {
		if _, err := ParseVersion(in); !errors.Is(err, ErrVersion) {
			t.Errorf("ParseVersion(%q) err = %v, want ErrVersion", in, err)
		}
	}
}

// TestVersionCompare checks the precedence rules of Semantic Version 2.0.0, section 11.
//
// Parameters:
//   - t: test to report to
func TestVersionCompare(t *testing.T) {
	// The spec example chain: each version is below the next one.
	chain := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0", "10.0.0",
	}

	for i := range chain {
		for j := range chain {
			a, _ := ParseVersion(chain[i])
			b, _ := ParseVersion(chain[j])

			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}

			if got := a.Compare(b); got != want {
				t.Errorf("%s.Compare(%s) = %d, want %d", chain[i], chain[j], got, want)
			}
		}
	}

	a, _ := ParseVersion("1.0.0+a")
	b, _ := ParseVersion("1.0.0+b")

	if a.Compare(b) != 0 {
		t.Error("build metadata took part in the precedence")
	}

	if !(Version{Pre: []string{"x"}}).Prerelease() || (Version{}).Prerelease() {
		t.Error("Prerelease is wrong")
	}
}

// TestParseRange checks the tote range grammar, its normal form and the limits.
//
// Parameters:
//   - t: test to report to
func TestParseRange(t *testing.T) {
	good := map[string]string{
		">=1.0.0":           ">=1.0.0",
		">=1.0.0, <2":       ">=1.0.0, <2.0.0",
		">=1.0.0,<2.0.0":    ">=1.0.0, <2.0.0",
		"  >= 1 ,  < 2  ":   ">=1.0.0, <2.0.0",
		"1.2":               "=1.2.0",
		"=1.2.3":            "=1.2.3",
		">1, <=3.4":         ">1.0.0, <=3.4.0",
		">=0":               ">=0.0.0",
		"<2.0.0":            "<2.0.0",
		">=1.1, <1.1.5, >0": ">=1.1.0, <1.1.5, >0.0.0",
	}

	for in, want := range good {
		r, err := ParseRange(in)
		if err != nil || r.String() != want {
			t.Errorf("ParseRange(%q) = %q, %v; want %q", in, r.String(), err, want)
		}
	}

	bad := []string{
		"", " ", ",", ">=1,", ",>=1", ">=1,,<2", ">=", "1.x", "*", "^1.0.0", "~1.0.0",
		">=1.0.0 || <0.5", ">=1.0.0-beta", ">=1.0.0+b", "v1.0.0", ">=v1", "==1", "=>1", "!=1",
		">= 1 2", "1.2.3.4", ">=01", "latest", ">=1.0.0 <2", strings.Repeat("1,", 8) + "1",
		strings.Repeat(">=1.0.0, ", 20),
	}

	for _, in := range bad {
		if _, err := ParseRange(in); !errors.Is(err, ErrRange) {
			t.Errorf("ParseRange(%q) err = %v, want ErrRange", in, err)
		}
	}

	if _, err := ParseRange(strings.Repeat("1,", 7) + "1"); err != nil {
		t.Errorf("eight comparators refused: %v", err)
	}
}

// TestRangeSatisfies checks the satisfaction table, including the pre-release rule and "dev".
//
// Parameters:
//   - t: test to report to
func TestRangeSatisfies(t *testing.T) {
	tests := []struct {
		rng  string
		host string
		want bool
	}{
		{">=1.0.0, <2", "1.0.0", true},
		{">=1.0.0, <2", "1.9.9", true},
		{">=1.0.0, <2", "2.0.0", false},
		{">=1.0.0, <2", "0.9.9", false},
		{">=1.1.0", "1.1.0-beta.1", true},
		{"<1.1.0", "1.1.0-beta.1", false},
		{">1.1.0", "1.1.0-beta.1", false},
		{"=1.2", "1.2.0", true},
		{"=1.2", "1.2.1", false},
		{"1.2.3", "1.2.3+build", true},
		{"<=1.2.3", "1.2.3", true},
		{"<=1.2.3", "1.2.4", false},
		{">0", "0.0.1", true},
		{">0", "0.0.0", false},
		{">=1, <1", "1.0.0", false},
		{">=1", "dev", true},
		{"<0.0.1", "dev", true},
		{">=1.0.0", "0.0.0-dev", true},
		{">=1.0.0", "0.0.0-20261109205113-159407959e94", true},
		{">=1.0.0", "1.2.3-0.20261109205113-159407959e94", true},
		{">=1.0.0", "1.2.3-rc.0.20261109205113-159407959e94", true},
		{">=1.0.0", "0.0.1", false},
		{">=1.0.0", "0.0.0-rc.1", false},
		{">=1.0.0", "0.0.0-20261109205113", false},
	}

	for _, tt := range tests {
		r, err := ParseRange(tt.rng)
		if err != nil {
			t.Fatalf("ParseRange(%q): %v", tt.rng, err)
		}

		got, err := r.Allows(tt.host)
		if err != nil || got != tt.want {
			t.Errorf("%q allows %q = %v, %v; want %v", tt.rng, tt.host, got, err, tt.want)
		}
	}

	r, _ := ParseRange(">=1")
	if _, err := r.Allows("not a version"); !errors.Is(err, ErrVersion) {
		t.Errorf("Allows(garbage) err = %v, want ErrVersion", err)
	}
}

// TestIsDevVersion checks which host versions count as a development build.
//
// Parameters:
//   - t: test to report to
func TestIsDevVersion(t *testing.T) {
	tests := map[string]bool{
		"dev": true, "0.0.0-dev": true, "0.0.0-20261109205113-159407959e94": true,
		"2.0.0-0.20261109205113-159407959e94": true, "": false, "1.0.0": false,
		"1.0.0-dev": false, "0.0.0": false, "0.0.0-20261109205113-15940795": false,
		"0.0.0-20261109205113-159407959E94": false, "0.0.0-2026110920511-159407959e94": false,
	}

	for host, want := range tests {
		if got := IsDevVersion(host); got != want {
			t.Errorf("IsDevVersion(%q) = %v, want %v", host, got, want)
		}
	}
}

// FuzzParseRange checks that the range and version parsers never panic and that a parsed range
// reparses from its normal form to the same range.
//
// Parameters:
//   - f: fuzz target
func FuzzParseRange(f *testing.F) {
	for _, s := range []string{">=1.0.0, <2", "", "^1", "=1.2.3", ">= 1 ,< 2", "1.0.0-beta", "\x00"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseVersion(s)

		r, err := ParseRange(s)
		if err != nil {
			return
		}

		again, err := ParseRange(r.String())
		if err != nil || again.String() != r.String() {
			t.Fatalf("normal form %q of %q does not reparse: %v", r.String(), s, err)
		}
	})
}
