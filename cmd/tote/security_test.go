package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReviewSecurityPurgeKeepsForeignFiles checks that --purge never removes a file tote did
// not create, even when a TOTE_DATA_DIR override points at a directory shared with other tools.
//
// With TOTE_DATA_DIR set to the directory that holds the user's own bin directory, the entry
// "bin" is removed with everything below it, foreign files included. The test currently fails.
//
// Parameters:
//   - t: test to report to
func TestReviewSecurityPurgeKeepsForeignFiles(t *testing.T) {
	h, _ := uninstallSetup(t)

	shared := filepath.Join(h.dir, "home", ".local")
	foreign := filepath.Join(shared, "bin", "mytool")

	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(foreign, []byte("precious"), 0o755); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_DATA_DIR"] = shared

	h.run("uninstall", "--yes", "--purge")

	if !exists(foreign) {
		t.Errorf("--purge removed %s, a file that tote did not create", foreign)
	}
}

// TestReviewSecurityRefusalBeforeRemoval checks that a purge that will be refused is refused
// before anything is removed, so that a failed uninstall leaves the install as it was.
//
// A configuration directory that is a symbolic link (a dotfiles manager) makes --purge fail,
// but only after the program, the receipt and the shell line are gone. The test currently fails.
//
// Parameters:
//   - t: test to report to
func TestReviewSecurityRefusalBeforeRemoval(t *testing.T) {
	h, target := uninstallSetup(t)

	real := filepath.Join(h.dir, "dotfiles")
	if err := os.Rename(h.vars["TOTE_CONFIG_DIR"], real); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(real, h.vars["TOTE_CONFIG_DIR"]); err != nil {
		t.Fatal(err)
	}

	code := h.run("uninstall", "--yes", "--purge", "--shell")
	if code == 0 {
		t.Fatalf("code 0, want a refusal")
	}

	rc := filepath.Join(h.app.Home, ".bashrc")
	data, _ := os.ReadFile(rc)

	if !exists(target) || !strings.Contains(string(data), "tote init") {
		t.Errorf("refused with code %d but the program or the shell line is already gone", code)
	}
}
