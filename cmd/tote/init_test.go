package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// bashSyntax runs bash -n on a script in a sandbox environment.
//
// Parameters:
//   - t: running test
//   - script: shell text to check
//
// Returns the output of bash, empty when the syntax is valid.
func bashSyntax(t *testing.T, script string) string {
	t.Helper()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	cmd := exec.Command(bash, "-n")
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}
	cmd.Stdin = strings.NewReader(script)

	out, _ := cmd.CombinedOutput()

	return string(out)
}

// TestInitBash checks the bash output: wrapper, definitions, and a clean bash -n.
//
// Parameters:
//   - t: test to report to
func TestInitBash(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	if code := h.run("init", "bash"); code != 0 || h.errb.Len() != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	out := h.out.String()
	if !strings.HasPrefix(out, "tote() {") {
		t.Errorf("the output does not start with the wrapper: %.40q", out)
	}

	for _, want := range []string{
		"alias ll='ls -la'", "alias gs='git status'", "alias proj='tote repo open proj'",
		"alias docs='tote repo open docs'", "hello() { source", "__TOTE_DEFS=",
		"complete -o default -F __tote_complete tote",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}

	if msg := bashSyntax(t, out); msg != "" {
		t.Errorf("bash -n: %s", msg)
	}

	if strings.Contains(out, "# tote:") {
		t.Errorf("unexpected skip comment in %q", out)
	}
}

// TestInitDefs checks that --defs leaves the wrapper out.
//
// Parameters:
//   - t: test to report to
func TestInitDefs(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	if code := h.run("init", "--defs", "bash"); code != 0 {
		t.Fatalf("code %d", code)
	}

	if strings.Contains(h.out.String(), "tote() {") || !strings.Contains(h.out.String(), "alias ll=") {
		t.Errorf("defs output = %q", h.out.String())
	}

	if msg := bashSyntax(t, h.out.String()); msg != "" {
		t.Errorf("bash -n: %s", msg)
	}
}

// TestInitZsh checks the zsh output, which cannot be syntax checked without zsh.
//
// Parameters:
//   - t: test to report to
func TestInitZsh(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	if code := h.run("init", "zsh"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if !strings.Contains(h.out.String(), "emulate -L zsh") ||
		!strings.Contains(h.out.String(), "compdef _tote") {
		t.Errorf("not a zsh output: %.200q", h.out.String())
	}
}

// TestInitFromEnvironment checks the shell guessed from $SHELL and the refusals.
//
// Parameters:
//   - t: test to report to
func TestInitFromEnvironment(t *testing.T) {
	h := newHarness(t)

	h.vars["SHELL"] = "/usr/bin/zsh"

	if code := h.run("init"); code != 0 || !strings.Contains(h.out.String(), "compdef") {
		t.Errorf("zsh from $SHELL: code %d", code)
	}

	h.vars["SHELL"] = "/bin/fish"

	if code := h.run("init"); code != 2 || h.out.Len() != 0 ||
		!strings.Contains(h.errb.String(), `unsupported shell "fish"`) {
		t.Errorf("fish from $SHELL: code %d, stdout %q, stderr %q", code, h.out.String(), h.errb.String())
	}

	if code := h.run("init", "fish"); code != 2 || h.out.Len() != 0 ||
		!strings.Contains(h.errb.String(), `unsupported shell "fish" (want bash or zsh)`) {
		t.Errorf("init fish: code %d, stdout %q, stderr %q", code, h.out.String(), h.errb.String())
	}

	if code := h.run("init", "bash", "zsh"); code != 2 {
		t.Errorf("two shells: code %d, want 2", code)
	}
}

// TestInitBrokenFiles checks that read-only collections are skipped with a comment line and
// never make init fail.
//
// Parameters:
//   - t: test to report to
func TestInitBrokenFiles(t *testing.T) {
	h := newHarness(t)
	h.fixtures()
	h.write("aliases.toml", "schema = 1\n[[alias]\n")
	h.write("config.toml", "lang = \n")

	if code := h.run("init", "bash"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	out := h.out.String()

	if strings.Contains(out, "alias ll=") || !strings.Contains(out, "alias proj=") {
		t.Errorf("only the broken file must be skipped: %q", out)
	}

	for _, want := range []string{"# tote: aliases.toml skipped:", "# tote: config.toml skipped:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}

	if strings.Count(out, "\n# tote: aliases.toml") != 1 {
		t.Errorf("the note must be one line: %q", out)
	}

	if msg := bashSyntax(t, out); msg != "" {
		t.Errorf("bash -n: %s", msg)
	}

	if h.read("aliases.toml") != "schema = 1\n[[alias]\n" {
		t.Error("init changed a data file")
	}
}

// TestInitConfigNames checks shell.extra_names and shell.compat_mn.
//
// Parameters:
//   - t: test to report to
func TestInitConfigNames(t *testing.T) {
	h := newHarness(t)
	h.write("config.toml", "schema = 1\n\n[shell]\ncompat_mn = true\nextra_names = [\"tt\"]\n")

	if code := h.run("init", "bash"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	for _, want := range []string{"tt() { tote \"$@\"; }", "mn() { tote \"$@\"; }"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("output lacks %q", want)
		}
	}
}

// TestInitNoFiles checks init on an empty sandbox, where no directory exists yet.
//
// Parameters:
//   - t: test to report to
func TestInitNoFiles(t *testing.T) {
	h := newHarness(t)

	if code := h.run("init", "bash"); code != 0 || h.errb.Len() != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if _, err := os.Stat(h.vars["TOTE_CONFIG_DIR"]); err == nil {
		t.Error("init created the config directory")
	}
}
