package plugin

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// TestParsePermission checks the closed permission vocabulary, one stable code per refusal.
//
// Parameters:
//   - t: test to report to
func TestParsePermission(t *testing.T) {
	good := map[string]string{
		"exec:git":                        PermExec,
		"exec:docker-compose":             PermExec,
		"exec:g++":                        PermExec,
		"net:api.github.com":              PermNet,
		"net:*.github.com":                PermNet,
		"net:a-b.example.org":             PermNet,
		"fs:read:home":                    PermFSRead,
		"fs:write:tmp":                    PermFSWrite,
		"fs:read:any":                     PermFSRead,
		"env:MY_MODE":                     PermEnv,
		"env:_X1":                         PermEnv,
		"env:GITHUB_TOKEN":                PermEnv,
		"host:read:repos":                 PermReadRepos,
		"host:open-url":                   PermOpenURL,
		"fs:write:" + "config":            PermFSWrite,
		"exec:" + strings.Repeat("a", 64): PermExec,
	}

	for in, kind := range good {
		p, perr := ParsePermission(in)
		if perr != nil || p.Kind != kind || p.Raw != in {
			t.Errorf("ParsePermission(%q) = %+v, %v; want kind %s", in, p, perr, kind)
		}
	}

	bad := []struct {
		in   string
		code string
	}{
		{"", CodePermInvalid},
		{strings.Repeat("a", 129), CodePermInvalid},
		{"exec:git\n", CodePermInvalid},
		{"exec:g\x00t", CodePermInvalid},
		{"exec:" + strings.Repeat("a", 65), CodePermInvalid},
		{"exec:", CodePermInvalid},
		{"exec:/bin/git", CodePermInvalid},
		{"exec:../git", CodePermInvalid},
		{"exec:-git", CodePermInvalid},
		{"exec:git status", CodePermInvalid},
		{"net:localhost", CodePermInvalid},
		{"net:*", CodePermInvalid},
		{"net:*.com", CodePermInvalid},
		{"net:Example.com", CodePermInvalid},
		{"net:example.com:443", CodePermInvalid},
		{"net:user@example.com", CodePermInvalid},
		{"net:1.2.3.4", CodePermInvalid},
		{"net:example.com/path", CodePermInvalid},
		{"net:-a.example.com", CodePermInvalid},
		{"net:a..example.com", CodePermInvalid},
		{"net:a.*.example.com", CodePermInvalid},
		{"net:**.example.com", CodePermInvalid},
		{"net:" + strings.Repeat("a", 64) + ".com", CodePermInvalid},
		{"net:" + strings.Repeat("a.", 130) + "com", CodePermInvalid},
		{"fs:read:/etc", CodePermInvalid},
		{"fs:read:", CodePermInvalid},
		{"fs:write:HOME", CodePermInvalid},
		{"env:lower", CodePermInvalid},
		{"env:", CodePermInvalid},
		{"env:A-B", CodePermInvalid},
		{"env:" + strings.Repeat("A", 65), CodePermInvalid},
		{"fs:execute:home", CodePermUnknown},
		{"fs:home", CodePermUnknown},
		{"exec", CodePermUnknown},
		{"EXEC:git", CodePermUnknown},
		{"root", CodePermUnknown},
		{"host:read:repo", CodePermUnknown},
		{"host:open-url ", CodePermUnknown},
		{"hook:intercept", CodePermRefused},
		{"host:write:settings", CodePermRefused},
		{"env:PATH", CodePermRefused},
		{"env:LD_PRELOAD", CodePermRefused},
		{"env:DYLD_LIBRARY_PATH", CodePermRefused},
		{"env:GIT_SSH_COMMAND", CodePermRefused},
		{"env:BASH_FUNC_x%%", CodePermInvalid},
		{"env:TOTE_TOKEN", CodeReserved},
		{"env:TOTE_ANY", CodePermRefused},
		{"host:read:connections", CodeReserved},
		{"host:handoff", CodeReserved},
		{"host:clipboard", CodeReserved},
		{"host:notify", CodeReserved},
		{"host:editor", CodeReserved},
	}

	for _, tt := range bad {
		if _, perr := ParsePermission(tt.in); perr == nil || perr.Code != tt.code {
			t.Errorf("ParsePermission(%q) = %v, want %s", tt.in, perr, tt.code)
		}
	}
}

// TestNormalizePermissions checks the list rules: sort, duplicates, count and error index.
//
// Parameters:
//   - t: test to report to
func TestNormalizePermissions(t *testing.T) {
	got, perr := NormalizePermissions([]string{"net:b.example.com", "exec:git", "env:MY_MODE"})
	if perr != nil || !slices.Equal(got, []string{"env:MY_MODE", "exec:git", "net:b.example.com"}) {
		t.Errorf("NormalizePermissions = %v, %v", got, perr)
	}

	if _, perr := NormalizePermissions([]string{"exec:git", "exec:git"}); perr == nil ||
		perr.Code != CodePermInvalid || perr.Reason != "duplicate" || perr.Index != 1 {
		t.Errorf("duplicate = %v", perr)
	}

	if _, perr := NormalizePermissions([]string{"exec:git", "bogus"}); perr == nil || perr.Index != 1 {
		t.Errorf("bad entry = %v", perr)
	}

	many := make([]string, 65)
	for i := range many {
		many[i] = "exec:p" + itoa(i)
	}

	if _, perr := NormalizePermissions(many); perr == nil || perr.Code != CodeLimitExceeded {
		t.Errorf("65 entries = %v", perr)
	}

	if _, perr := NormalizePermissions(many[:64]); perr != nil {
		t.Errorf("64 entries = %v", perr)
	}

	if got, perr := NormalizePermissions(nil); perr != nil || len(got) != 0 {
		t.Errorf("empty list = %v, %v", got, perr)
	}
}

// TestEscalationMatrix checks the escalation table: false exactly when the new set is a subset of
// the old one up to the wildcard rule.
//
// Parameters:
//   - t: test to report to
func TestEscalationMatrix(t *testing.T) {
	tests := []struct {
		name string
		old  []string
		next []string
		want []string
	}{
		{"same", []string{"exec:git"}, []string{"exec:git"}, nil},
		{"both empty", nil, nil, nil},
		{"drop", []string{"exec:git", "net:a.example.com"}, []string{"exec:git"}, nil},
		{"drop all", []string{"exec:git"}, nil, nil},
		{"add", []string{"exec:git"}, []string{"exec:git", "exec:gh"}, []string{"exec:gh"}},
		{"add to empty", nil, []string{"env:MY_MODE"}, []string{"env:MY_MODE"}},
		{"order and duplicates", []string{"b:x", "a:x"}, []string{"a:x", "b:x", "a:x"}, nil},
		{"swap", []string{"exec:git"}, []string{"exec:gh"}, []string{"exec:gh"}},
		{
			"wildcard covers subdomain",
			[]string{"net:*.example.com"},
			[]string{"net:api.example.com"},
			nil,
		},
		{
			"wildcard covers deep subdomain",
			[]string{"net:*.example.com"},
			[]string{"net:a.b.example.com"},
			nil,
		},
		{
			"wildcard does not cover apex",
			[]string{"net:*.example.com"},
			[]string{"net:example.com"},
			[]string{"net:example.com"},
		},
		{
			"wildcard does not cover lookalike",
			[]string{"net:*.example.com"},
			[]string{"net:badexample.com"},
			[]string{"net:badexample.com"},
		},
		{
			"wildcard does not cover sibling domain",
			[]string{"net:*.example.com"},
			[]string{"net:api.example.org"},
			[]string{"net:api.example.org"},
		},
		{
			"host does not cover wildcard",
			[]string{"net:api.example.com"},
			[]string{"net:*.example.com"},
			[]string{"net:*.example.com"},
		},
		{
			"wildcard does not cover wildcard",
			[]string{"net:*.example.com"},
			[]string{"net:*.api.example.com"},
			[]string{"net:*.api.example.com"},
		},
		{
			"wildcard does not cover itself by rule but by equality",
			[]string{"net:*.example.com"},
			[]string{"net:*.example.com"},
			nil,
		},
		{
			"write does not imply read",
			[]string{"fs:write:home"},
			[]string{"fs:read:home"},
			[]string{"fs:read:home"},
		},
		{
			"any does not imply home",
			[]string{"fs:read:any"},
			[]string{"fs:read:home"},
			[]string{"fs:read:home"},
		},
		{
			"derived code added",
			[]string{"exec:git"},
			[]string{"exec:git", CapDerivedCode},
			[]string{CapDerivedCode},
		},
		{"derived tty added", nil, []string{CapDerivedTTY}, []string{CapDerivedTTY}},
		{
			"several added sorted",
			[]string{"exec:a"},
			[]string{"net:z.example.com", "exec:b", "exec:a"},
			[]string{"exec:b", "net:z.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Escalation(tt.old, tt.next)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Escalation = %v, want %v", got, tt.want)
			}

			if Escalates(tt.old, tt.next) != (len(tt.want) > 0) {
				t.Errorf("Escalates = %v for %v", len(tt.want) <= 0, tt.want)
			}
		})
	}
}

// subsetUpToWildcard is the reference model of the escalation rule, written independently of
// Covers: p is allowed when it is in have, or when a wildcard of have names a strict parent.
//
// Parameters:
//   - have: consented set
//   - p: capability to test
func subsetUpToWildcard(have []string, p string) bool {
	for _, h := range have {
		if h == p {
			return true
		}
	}

	if !strings.HasPrefix(p, "net:") || strings.HasPrefix(p, "net:*.") {
		return false
	}

	labels := strings.Split(strings.TrimPrefix(p, "net:"), ".")
	for i := 1; i < len(labels); i++ {
		parent := "net:*." + strings.Join(labels[i:], ".")
		if slices.Contains(have, parent) {
			return true
		}
	}

	return false
}

// TestEscalationProperty compares Escalates with the reference model over random capability sets.
// The pool holds hosts, wildcards and other entries that share suffixes, to hit the edge cases.
//
// Parameters:
//   - t: test to report to
func TestEscalationProperty(t *testing.T) {
	pool := []string{
		"exec:git", "exec:gh", "env:MY_MODE", "fs:read:home", "fs:write:home", CapDerivedCode,
		"net:example.com", "net:a.example.com", "net:b.a.example.com", "net:*.example.com",
		"net:*.a.example.com", "net:badexample.com", "net:example.org", "net:*.org", "net:x.org",
		"net:*.b.a.example.com", "net:com",
	}

	rng := rand.New(rand.NewSource(1))

	pick := func() []string {
		var out []string

		for _, p := range pool {
			if rng.Intn(3) == 0 {
				out = append(out, p)
			}
		}

		rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })

		return out
	}

	for i := 0; i < 5000; i++ {
		old, next := pick(), pick()

		want := false

		for _, p := range next {
			if !subsetUpToWildcard(old, p) {
				want = true
			}
		}

		if got := Escalates(old, next); got != want {
			t.Fatalf("Escalates(%v, %v) = %v, model says %v", old, next, got, want)
		}

		// Reflexivity and monotonicity: a set never escalates against itself or a superset.
		if Escalates(next, next) || Escalates(append(slices.Clone(next), old...), next) {
			t.Fatalf("a set escalated against itself or a superset: %v", next)
		}
	}
}

// FuzzPermission checks that the parser never panics and that an accepted entry reparses to the
// same permission and is a valid capability.
//
// Parameters:
//   - f: fuzz target
func FuzzPermission(f *testing.F) {
	for _, s := range []string{
		"exec:git", "net:*.github.com", "fs:read:home", "env:PATH", "host:open-url", "", "net:*",
		"exec:\x00", "env:TOTE_TOKEN", "host:handoff",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		p, perr := ParsePermission(s)
		if perr != nil {
			if perr.Code == "" || ValidCap(s) && s != CapDerivedCode && s != CapDerivedTTY {
				t.Fatalf("refused %q yet ValidCap says it is valid", s)
			}

			return
		}

		again, perr := ParsePermission(p.Raw)
		if perr != nil || again != p || !ValidCap(s) {
			t.Fatalf("accepted %q does not round trip: %+v %v", s, again, perr)
		}
	})
}

// TestSensitiveEnv checks the credential heuristic used at consent.
//
// Parameters:
//   - t: test to report to
func TestSensitiveEnv(t *testing.T) {
	for _, n := range []string{
		"SSH_AUTH_SOCK", "GITHUB_TOKEN", "MY_SECRET", "DB_PASSWORD", "AWS_REGION", "GH_HOST",
		"GITLAB_X", "AZURE_X", "GOOGLE_X", "OPENAI_API_KEY", "PRIVATE_X", "X_CREDENTIAL", "PASSWD_X",
	} {
		if !SensitiveEnv(n) {
			t.Errorf("SensitiveEnv(%q) = false", n)
		}
	}

	for _, n := range []string{"EDITOR", "LANG", "TERM", "NO_COLOR", "PAGER"} {
		if SensitiveEnv(n) {
			t.Errorf("SensitiveEnv(%q) = true", n)
		}
	}
}
