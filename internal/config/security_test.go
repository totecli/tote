package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// reviewEnv builds a getenv function from a map.
//
// Parameters:
//   - m: variables to expose
func reviewEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestReviewSecOverrides checks relative and dotted TOTE_*_DIR values.
//
// Parameters:
//   - t: test to report to
func TestReviewSecOverrides(t *testing.T) {
	for _, v := range []string{"rel", "./rel", "../x", "~/x", " /x", "$HOME/x"} {
		_, err := Resolve(reviewEnv(map[string]string{"TOTE_DATA_DIR": v}), "/home/u")
		if err == nil {
			t.Errorf("override %q accepted", v)
		}
	}
	p, err := Resolve(reviewEnv(map[string]string{"TOTE_CONFIG_DIR": "/a/b/../../etc//"}), "/home/u")
	if err != nil || p.Config != "/etc" {
		t.Errorf("dotted override: %q %v", p.Config, err)
	}
	// XDG values that are relative are ignored, never joined to the working directory.
	p, err = Resolve(reviewEnv(map[string]string{
		"XDG_CONFIG_HOME": "../evil",
		"XDG_RUNTIME_DIR": "run",
	}), "/home/u")
	if err != nil || p.Config != "/home/u/.config/tote" || p.Runtime != "" {
		t.Errorf("relative xdg: %+v %v", p, err)
	}
	if _, err := Resolve(reviewEnv(nil), "home"); err == nil {
		t.Error("relative home accepted")
	}
}

// TestReviewSecRuntimeDir checks the ownership, mode and symlink checks of RuntimeDir.
//
// Parameters:
//   - t: test to report to
func TestReviewSecRuntimeDir(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	base := t.TempDir()

	// Missing: created 0700 even under umask 0.
	p := Paths{Runtime: filepath.Join(base, "run", "tote")}
	dir, err := p.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Errorf("created mode %v", info.Mode().Perm())
	}

	// Pre-created by someone with a loose mode.
	loose := filepath.Join(base, "loose")
	if err := os.Mkdir(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (Paths{Runtime: loose}).RuntimeDir(); err == nil {
		t.Error("0755 runtime dir accepted")
	}
	if info, _ := os.Stat(loose); info.Mode().Perm() != 0o755 {
		t.Error("RuntimeDir chmod-ed an existing directory")
	}

	// Symlink to a good directory.
	link := filepath.Join(base, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (Paths{Runtime: link}).RuntimeDir(); err == nil {
		t.Error("symlinked runtime dir accepted")
	}

	// Dangling symlink and a regular file in the way.
	dangling := filepath.Join(base, "dangling")
	if err := os.Symlink(filepath.Join(base, "nowhere"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := (Paths{Runtime: dangling}).RuntimeDir(); err == nil {
		t.Error("dangling symlink accepted")
	}
	if _, err := os.Lstat(filepath.Join(base, "nowhere")); err == nil {
		t.Error("dangling symlink target was created")
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Paths{Runtime: file}).RuntimeDir(); err == nil {
		t.Error("regular file accepted")
	}

	// Fallback: private, unique, not shared between calls.
	t.Setenv("TMPDIR", base)
	a, err := (Paths{}).RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := (Paths{}).RuntimeDir()
	if a == b {
		t.Error("fallback directory reused")
	}
	if info, _ := os.Stat(a); info.Mode().Perm() != 0o700 {
		t.Errorf("fallback mode %v", info.Mode().Perm())
	}
	t.Logf("each RuntimeDir() call with no XDG_RUNTIME_DIR leaves a new directory behind: %s %s",
		a, b)
}

// TestReviewSecSettingsBadPaths checks directory and garbage inputs of LoadSettings.
//
// Parameters:
//   - t: test to report to
func TestReviewSecSettingsBadPaths(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadSettings(dir); err == nil {
		t.Error("directory accepted as config file")
	}
	path := filepath.Join(dir, "config.toml")
	bodies := []string{"\x00", "lang = \"\\u0000\"", "[[[[", "editor = ", "a = \"\xc3\x28\""}
	for _, body := range bodies {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSettings(path); err == nil && body != "lang = \"\\u0000\"" {
			t.Errorf("garbage %q accepted", body)
		}
	}
	// A symlink to a missing file is treated like a missing file: defaults.
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadSettings(link); err != nil || s.Schema != 1 {
		t.Errorf("dangling link: %v", err)
	}
}

// TestReviewSecEditorNoShell checks that the editor is split on spaces and never shell-expanded.
//
// Parameters:
//   - t: test to report to
func TestReviewSecEditorNoShell(t *testing.T) {
	argv := EffectiveEditor(Settings{Editor: "vim; rm x $(id) `id`"}, reviewEnv(nil))
	if len(argv) != 5 || argv[0] != "vim;" {
		t.Errorf("argv %q", argv)
	}
	argv = EffectiveEditor(Settings{}, reviewEnv(map[string]string{"VISUAL": "\t\n "}))
	if len(argv) != 1 || argv[0] != "vim" {
		t.Errorf("blank VISUAL: %q", argv)
	}
}

// TestReviewSecFIFOConfig checks that LoadSettings refuses a FIFO instead of waiting on it.
//
// Parameters:
//   - t: test to report to
func TestReviewSecFIFOConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := LoadSettings(path)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as config")
		}
	case <-time.After(2 * time.Second):
		f, _ := os.OpenFile(path, os.O_WRONLY, 0)
		if f != nil {
			_ = f.Close()
		}
		t.Fatal("LoadSettings hangs forever on a FIFO")
	}
}

// TestReviewSecDeepNestingConfig checks that LoadSettings survives a hostile deeply nested file.
//
// Parameters:
//   - t: test to report to
func TestReviewSecDeepNestingConfig(t *testing.T) {
	if p := os.Getenv("TOTE_REVIEW_CHILD"); p != "" {
		_, _ = LoadSettings(p)
		return
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	n := 3000000
	body := "x = " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestReviewSecDeepNestingConfig$")
	cmd.Env = append(os.Environ(), "TOTE_REVIEW_CHILD="+p)
	out, err := cmd.CombinedOutput()
	if err != nil {
		head := string(out)
		if len(head) > 120 {
			head = head[:120]
		}
		t.Fatalf("a %d byte config.toml kills the process: %v: %s", len(body), err, head)
	}
}

// TestReviewSecExtraNames checks that hostile shell.extra_names entries are dropped with a warning.
//
// Parameters:
//   - t: test to report to
func TestReviewSecExtraNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[shell]\nextra_names = [\"x; rm -rf ~\", \"$(id)\", \"a b\", \"\", \"-n\"]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, warns, err := LoadSettingsWarn(path)
	if err != nil || len(warns) != 1 || warns[0].Key != "shell.extra_names" {
		t.Fatalf("warnings = %+v, error %v, want one warning on shell.extra_names", warns, err)
	}
	if len(s.Shell.ExtraNames) != 0 {
		t.Fatalf("hostile function names accepted: %q", s.Shell.ExtraNames)
	}
}
