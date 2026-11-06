package plugin

import (
	"slices"
	"strings"
	"testing"
)

// TestEffectiveCaps checks the derived capabilities: the code tier and the terminal output.
//
// Parameters:
//   - t: test to report to
func TestEffectiveCaps(t *testing.T) {
	plain := mustDecode(t, decl("", []string{"exec:tool", "env:MY_MODE"}, cmdBody))
	if got := EffectiveCaps(plain); !slices.Equal(got, []string{"env:MY_MODE", "exec:tool"}) {
		t.Errorf("declarative caps = %v", got)
	}

	tty := mustDecode(t, decl("", []string{"exec:tool"},
		"[[command]]\nid = \"go\"\nlabel = \"Go\"\nrun = [\"tool\"]\noutput = \"terminal\"\n"))
	if got := EffectiveCaps(tty); !slices.Equal(got, []string{CapDerivedTTY, "exec:tool"}) {
		t.Errorf("terminal caps = %v", got)
	}

	code := mustDecode(t, execM("", nil,
		"[[action]]\nid = \"a\"\nlabel = \"A\"\noutput = \"pager\"\n"))
	if got := EffectiveCaps(code); !slices.Equal(got, []string{CapDerivedCode}) {
		t.Errorf("exec caps = %v", got)
	}

	// A terminal item action counts too.
	item := mustDecode(t, decl("", []string{"exec:tool", "host:read:repos"},
		"[[item_action]]\nid = \"a\"\nlabel = \"A\"\ntarget = \"repo\"\nrun = [\"tool\"]\noutput = "+
			"\"terminal\"\n"))
	if got := EffectiveCaps(item); !slices.Contains(got, CapDerivedTTY) {
		t.Errorf("item action caps = %v", got)
	}

	if !slices.IsSorted(EffectiveCaps(item)) {
		t.Error("EffectiveCaps is not sorted")
	}
}

// TestGrantToken checks the --grant token: sorted caps, an at sign, 12 hex digits.
//
// Parameters:
//   - t: test to report to
func TestGrantToken(t *testing.T) {
	hash := strings.Repeat("ab", 32)

	if got := GrantToken([]string{"net:b.example.com", "exec:git", "exec:git"}, hash); got !=
		"exec:git,net:b.example.com@abababababab" {
		t.Errorf("GrantToken = %q", got)
	}

	if got := GrantToken(nil, "abc"); got != "@abc" {
		t.Errorf("empty caps token = %q", got)
	}
}

// TestCapHelpers checks ValidCap, CapGroup, Enforced and Broad.
//
// Parameters:
//   - t: test to report to
func TestCapHelpers(t *testing.T) {
	groups := map[string]string{
		CapDerivedTTY: GroupTTY, CapDerivedCode: GroupExec, "exec:git": GroupExec,
		"net:a.example.com": GroupNetwork, "fs:read:home": GroupFiles, "env:MY_MODE": GroupEnv,
		"host:open-url": GroupHost, "host:read:repos": GroupHost, "bogus": "",
	}

	for c, want := range groups {
		if got := CapGroup(c); got != want {
			t.Errorf("CapGroup(%q) = %q, want %q", c, got, want)
		}
	}

	for _, c := range []string{CapDerivedCode, CapDerivedTTY, "exec:git", "host:open-url"} {
		if !ValidCap(c) {
			t.Errorf("ValidCap(%q) = false", c)
		}
	}

	for _, c := range []string{"", "derived:other", "exec:/bin/sh", "host:handoff"} {
		if ValidCap(c) {
			t.Errorf("ValidCap(%q) = true", c)
		}
	}

	enforced := []struct {
		c, tier string
		want    bool
	}{
		{"exec:git", TierDeclarative, true},
		{"exec:git", TierExec, false},
		{"env:MY_MODE", TierDeclarative, true},
		{"env:MY_MODE", TierExec, false},
		{"net:a.example.com", TierDeclarative, false},
		{"fs:read:home", TierExec, false},
		{"host:read:repos", TierExec, true},
		{"host:open-url", TierDeclarative, true},
		{CapDerivedCode, TierExec, true},
		{CapDerivedTTY, TierDeclarative, true},
	}

	for _, tt := range enforced {
		if got := Enforced(tt.c, tt.tier); got != tt.want {
			t.Errorf("Enforced(%q, %q) = %v", tt.c, tt.tier, got)
		}
	}

	for _, c := range []string{
		"fs:read:home", "fs:write:ssh", "fs:read:any", "net:*.example.com",
		"env:GITHUB_TOKEN",
	} {
		if !Broad(c) {
			t.Errorf("Broad(%q) = false", c)
		}
	}

	for _, c := range []string{
		"fs:read:tmp", "fs:write:cwd", "net:a.example.com", "env:MY_MODE",
		"exec:git", "bogus",
	} {
		if Broad(c) {
			t.Errorf("Broad(%q) = true", c)
		}
	}
}
