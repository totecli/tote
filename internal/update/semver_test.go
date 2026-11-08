package update

import (
	"errors"
	"testing"
)

// TestParseVersion checks the strict parser on valid and invalid text.
//
// Parameters:
//   - t: test to report to
func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"1.0.0", "1.0.0", true},
		{"0.0.1", "0.0.1", true},
		{"10.20.30", "10.20.30", true},
		{"1.1.0-beta.1", "1.1.0-beta.1", true},
		{"1.1.0-beta.10", "1.1.0-beta.10", true},
		{"1.0.0-0", "1.0.0-0", true},
		{"1.0.0-x-y.z", "1.0.0-x-y.z", true},
		{"", "", false},
		{"dev", "", false},
		{"v1.0.0", "", false},
		{"1.0", "", false},
		{"1.0.0.0", "", false},
		{"01.0.0", "", false},
		{"1.00.0", "", false},
		{"1.0.0-", "", false},
		{"1.0.0-beta..1", "", false},
		{"1.0.0-01", "", false},
		{"1.0.0-beta_1", "", false},
		{"1.0.0+build", "", false},
		{"1.0.0-beta+build", "", false},
		{"-1.0.0", "", false},
		{"+1.0.0", "", false},
		{"1.0.0 ", "", false},
		{" 1.0.0", "", false},
		{"1.0.0\n", "", false},
		{"99999999999999999999.0.0", "", false},
		{"1.0.0/../x", "", false},
		{"1.0.\u0660", "", false},
	}

	for _, tt := range tests {
		v, err := ParseVersion(tt.in)
		if (err == nil) != tt.ok {
			t.Errorf("ParseVersion(%q) error = %v, want ok = %v", tt.in, err, tt.ok)
			continue
		}

		if !tt.ok {
			if !errors.Is(err, ErrVersion) {
				t.Errorf("ParseVersion(%q) error %v does not wrap ErrVersion", tt.in, err)
			}

			continue
		}

		if v.String() != tt.want {
			t.Errorf("ParseVersion(%q).String() = %q, want %q", tt.in, v.String(), tt.want)
		}
	}
}

// TestCompareVersions checks the precedence rules, including the ones the plans rely on.
//
// Parameters:
//   - t: test to report to
func TestCompareVersions(t *testing.T) {
	// Each row is lower than the next one: the whole list is the order of the SemVer example.
	ordered := []string{
		"0.9.9",
		"1.0.0-0",
		"1.0.0-1",
		"1.0.0-2",
		"1.0.0-10",
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.10",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
		"1.0.1-beta.1",
		"1.0.1",
		"1.1.0-beta.1",
		"1.1.0-beta.2",
		"1.1.0-beta.10",
		"1.1.0",
		"1.10.0",
		"2.0.0",
		"10.0.0",
	}

	for i, a := range ordered {
		va, err := ParseVersion(a)
		if err != nil {
			t.Fatal(err)
		}

		if c := va.Compare(va); c != 0 {
			t.Errorf("%s.Compare(%s) = %d, want 0", a, a, c)
		}

		for j := i + 1; j < len(ordered); j++ {
			vb, err := ParseVersion(ordered[j])
			if err != nil {
				t.Fatal(err)
			}

			if c := va.Compare(vb); c != -1 {
				t.Errorf("%s.Compare(%s) = %d, want -1", a, ordered[j], c)
			}

			if c := vb.Compare(va); c != 1 {
				t.Errorf("%s.Compare(%s) = %d, want 1", ordered[j], a, c)
			}
		}
	}
}

// TestCompareHugeIdentifiers checks that numeric identifiers beyond 64 bits compare without
// overflow.
//
// Parameters:
//   - t: test to report to
func TestCompareHugeIdentifiers(t *testing.T) {
	small, _ := ParseVersion("1.0.0-99999999999999999999")
	big, _ := ParseVersion("1.0.0-100000000000000000000")

	if small.Compare(big) != -1 || big.Compare(small) != 1 {
		t.Error("huge numeric identifiers do not compare numerically")
	}
}

// TestChannelOf checks that only a pre-release is on the beta channel.
//
// Parameters:
//   - t: test to report to
func TestChannelOf(t *testing.T) {
	for in, want := range map[string]Channel{
		"1.0.0":        ChannelStable,
		"1.1.0-beta.1": ChannelBeta,
		"2.0.0-rc.1":   ChannelBeta,
	} {
		v, err := ParseVersion(in)
		if err != nil {
			t.Fatal(err)
		}

		if got := ChannelOf(v); got != want {
			t.Errorf("ChannelOf(%s) = %s, want %s", in, got, want)
		}

		if v.Prerelease() != (want == ChannelBeta) {
			t.Errorf("Prerelease(%s) = %v", in, v.Prerelease())
		}
	}
}
