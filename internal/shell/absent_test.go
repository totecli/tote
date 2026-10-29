package shell

import (
	"os/exec"
	"strings"
	"testing"
)

// TestWrapperWithoutBinary checks that the function prints one clear message and returns 127
// when no tote binary can be found, instead of letting the shell suggest unrelated packages.
//
// Parameters:
//   - t: test to report to
func TestWrapperWithoutBinary(t *testing.T) {
	for _, k := range []Kind{Bash, Zsh} {
		t.Run(string(k), func(t *testing.T) {
			path, err := exec.LookPath(string(k))
			if err != nil {
				t.Skipf("%s is not installed", k)
			}

			cmd := exec.Command(path, "-c", Wrapper(k)+"\ntote co x\necho rc=$?")
			cmd.Env = []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}

			var stderr strings.Builder

			cmd.Stderr = &stderr

			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}

			if strings.TrimSpace(string(out)) != "rc=127" ||
				stderr.String() != "tote: binary not found in PATH\n" {
				t.Errorf("stdout %q, stderr %q", out, stderr.String())
			}
		})
	}
}
