package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	// buildDir is the temporary directory that holds the binary under test.
	buildDir string

	// buildOnce builds the binary at most once per test run.
	buildOnce sync.Once

	// buildErr is the failure of the build, if any.
	buildErr error
)

// TestMain removes the binary under test and its directory when the tests are over.
//
// Parameters:
//   - m: test set to run
func TestMain(m *testing.M) {
	code := m.Run()

	if buildDir != "" {
		// The directory holds the binary only: two plain removals, nothing recursive.
		_ = os.Remove(filepath.Join(buildDir, "tote"))
		_ = os.Remove(buildDir)
	}

	os.Exit(code)
}

// toteBin returns the directory that holds the binary tote, built from the sources of the
// repository on the first call.
//
// Parameters:
//   - t: running test, skipped or failed when the binary cannot be built
func toteBin(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tote-e2e-bin-")
		if err != nil {
			buildErr = err
			return
		}

		buildDir = dir

		goBin, err := exec.LookPath("go")
		if err != nil {
			buildErr = err
			return
		}

		cmd := exec.Command(goBin, "build", "-o", filepath.Join(dir, "tote"),
			"github.com/totecli/tote/cmd/tote")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})

	if buildErr != nil {
		t.Fatalf("cannot build the binary under test: %v", buildErr)
	}

	return buildDir
}
