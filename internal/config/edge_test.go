package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReviewPrefixMinBoundary checks that prefix_min = 1, the lowest legal value, is accepted.
//
// Parameters:
//   - t: test to report to
func TestReviewPrefixMinBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[connect]\nprefix_min = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings = %v", err)
	}
	if s.Connect.PrefixMin != 1 {
		t.Errorf("PrefixMin = %d, want 1", s.Connect.PrefixMin)
	}
}

// TestReviewRuntimeDirFallbackName checks the name and the mode of the temporary fallback.
//
// Parameters:
//   - t: test to report to
func TestReviewRuntimeDirFallbackName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	dir, err := Paths{}.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != tmp || !strings.HasPrefix(filepath.Base(dir), "tote-") {
		t.Errorf("RuntimeDir = %s, want a tote-* directory in %s", dir, tmp)
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("fallback directory: %v, %v", fi, err)
	}
}
