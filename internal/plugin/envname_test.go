package plugin

import (
	"strings"
	"testing"
)

// TestSettingEnvNameInjective enumerates every plugin id and setting id that the grammars allow
// over a small alphabet and checks that no two pairs read the same variable: the name is public
// and cannot change after the release.
//
// Parameters:
//   - t: test to report to
func TestSettingEnvNameInjective(t *testing.T) {
	var ids, settings []string

	for _, id := range words("a0-", 4) {
		if id[0] != '-' {
			ids = append(ids, id)
		}
	}

	for _, s := range words("a0_", 4) {
		if snakeIDRe.MatchString(s) && !strings.Contains(s, "__") {
			settings = append(settings, s)
		}
	}

	seen := map[string]string{}

	for _, id := range ids {
		for _, s := range settings {
			name := SettingEnvName(id, s)
			if prev, dup := seen[name]; dup {
				t.Fatalf("%s is read by %s and by %s/%s", name, prev, id, s)
			}

			seen[name] = id + "/" + s
		}
	}

	if len(seen) < 1000 {
		t.Errorf("only %d pairs were enumerated", len(seen))
	}
}

// TestSettingEnvNameExamples checks the names of the documented examples.
//
// Parameters:
//   - t: test to report to
func TestSettingEnvNameExamples(t *testing.T) {
	tests := []struct{ id, setting, want string }{
		{"demo", "greeting", "TOTE_PLUGIN_DEMO_GREETING"},
		{"my", "tool_x", "TOTE_PLUGIN_MY_TOOL_X"},
		{"my-tool", "x", "TOTE_PLUGIN_MY__TOOL_X"},
		{"a--b", "x", "TOTE_PLUGIN_A____B_X"},
	}

	for _, tt := range tests {
		if got := SettingEnvName(tt.id, tt.setting); got != tt.want {
			t.Errorf("SettingEnvName(%q, %q) = %q, want %q", tt.id, tt.setting, got, tt.want)
		}
	}
}

// words lists every string of 1 to n characters over an alphabet.
//
// Parameters:
//   - alphabet: characters to combine
//   - n: longest length
func words(alphabet string, n int) []string {
	out := []string{""}
	last := []string{""}

	for i := 0; i < n; i++ {
		var next []string

		for _, w := range last {
			for _, c := range alphabet {
				next = append(next, w+string(c))
			}
		}

		out = append(out, next...)
		last = next
	}

	return out[1:]
}
