package update

import (
	"os"
	"os/exec"
	"testing"
)

// toolsRequired reports whether a missing tool must fail a test instead of skipping it: on CI,
// or when TOTE_REQUIRE_TOOLS=1 asks for it, so that a program the pipeline is meant to provide
// cannot go missing without anyone noticing.
//
// Parameters:
//   - getenv: reads an environment variable
func toolsRequired(getenv func(string) string) bool {
	return getenv("CI") == "true" || getenv("TOTE_REQUIRE_TOOLS") == "1"
}

// requireTool finds a program in the PATH for a test.
//
// Parameters:
//   - t: test to report to
//   - name: program name
//
// Returns the path of the program. When it is missing the test is skipped, or failed when
// toolsRequired says so.
func requireTool(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}

	if toolsRequired(os.Getenv) {
		t.Fatalf("%s is not installed, and CI=true or TOTE_REQUIRE_TOOLS=1 forbids skipping", name)
	}

	t.Skipf("%s is not installed", name)

	return ""
}

// toolFailed reports that a tool a test relies on did not work: a skip normally, a failure when
// toolsRequired says so.
//
// Parameters:
//   - t: test to report to
//   - format: message format
//   - args: message arguments
func toolFailed(t *testing.T, format string, args ...any) {
	t.Helper()

	if toolsRequired(os.Getenv) {
		t.Fatalf(format, args...)
	}

	t.Skipf(format, args...)
}

// TestToolsRequired checks which environments forbid skipping a test for a missing tool.
//
// Parameters:
//   - t: test to report to
func TestToolsRequired(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"nothing set", nil, false},
		{"CI true", map[string]string{"CI": "true"}, true},
		{"CI other value", map[string]string{"CI": "no"}, false},
		{"explicit one", map[string]string{"TOTE_REQUIRE_TOOLS": "1"}, true},
		{"explicit zero", map[string]string{"TOTE_REQUIRE_TOOLS": "0"}, false},
	}

	for _, tt := range tests {
		if got := toolsRequired(func(k string) string { return tt.env[k] }); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}
