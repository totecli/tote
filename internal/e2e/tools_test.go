package e2e

import (
	"os"
	"os/exec"
	"testing"
)

// toolsRequired tells whether a missing external program is a failure and not a skip: it is
// when CI is "true" (set by GitHub Actions) or TOTE_REQUIRE_TOOLS is "1", so that a runner that
// lacks a tool cannot go green by silently skipping the tests that need it.
//
// Returns true when the environment demands the tools.
func toolsRequired() bool {
	return os.Getenv("CI") == "true" || os.Getenv("TOTE_REQUIRE_TOOLS") == "1"
}

// requireTool returns the path of a program, and skips the test when it is not installed, unless
// toolsRequired says that the environment must provide it, in which case the test fails.
//
// Parameters:
//   - t: running test
//   - name: program to look up in PATH
//
// Returns the path of the program.
func requireTool(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		if toolsRequired() {
			t.Fatalf("%s is not installed and the environment forbids skipping (CI, "+
				"TOTE_REQUIRE_TOOLS)", name)
		}

		t.Skipf("%s is not installed", name)
	}

	return path
}
