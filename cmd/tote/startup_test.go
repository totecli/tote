package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReviewTestsUninstallLeavesStartupFile checks that without --shell the startup file keeps
// every byte, with and without --purge: the line that loads tote is only ever removed on an
// explicit request.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsUninstallLeavesStartupFile(t *testing.T) {
	const rc = "export A=1\neval \"$(tote init bash)\"\n"

	for _, args := range [][]string{
		{"uninstall", "--yes"},
		{"uninstall", "--yes", "--purge"},
	} {
		h, target := uninstallSetup(t)

		if code := h.run(args...); code != 0 {
			t.Fatalf("%v: code %d, stderr %q", args, code, h.errb.String())
		}

		if exists(target) {
			t.Errorf("%v: the program is still there", args)
		}

		data, err := os.ReadFile(filepath.Join(h.app.Home, ".bashrc"))
		if err != nil || string(data) != rc {
			t.Errorf("%v: .bashrc = %q, %v, want it untouched", args, data, err)
		}
	}
}
