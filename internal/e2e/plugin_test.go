package e2e

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pluginManifest is a neutral declarative plugin with one command.
const pluginManifest = `api = 1
id = "demo"
version = "1.0.0"
tote = ">=1.0.0, <2"
title = "Demo"
description = "A neutral demo plugin."
license = "MIT"
tier = "declarative"
permissions = ["exec:echo"]

[[command]]
id = "hello"
label = "Say hello"
run = ["echo", "hello"]
output = "pager"
`

// openUp gives the owner write permission on a tree, so that the installed read-only plugin trees
// of a test can be removed with its temporary directory.
//
// Parameters:
//   - root: directory to open up
func openUp(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			_ = os.Chmod(path, 0o700)
		} else if d.Type().IsRegular() {
			_ = os.Chmod(path, 0o600)
		}

		return nil
	})
}

// run runs the binary under test with the environment of the sandbox, without a terminal.
//
// Parameters:
//   - t: running test
//   - sb: sandbox
//   - args: command line without the program name
//
// Returns standard output, standard error and the exit code.
func (sb *sandbox) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()

	cmd := exec.Command(sb.tote(), args...)
	cmd.Env = sb.Env()
	cmd.Dir = sb.Home

	var out, errb bytes.Buffer

	cmd.Stdout, cmd.Stderr = &out, &errb

	err := cmd.Run()

	var ee *exec.ExitError

	switch {
	case err == nil:
		return out.String(), errb.String(), 0
	case errors.As(err, &ee):
		return out.String(), errb.String(), ee.ExitCode()
	}

	t.Fatal(err)

	return "", "", -1
}

// pluginSandbox makes a sandbox with the source of a plugin in it.
//
// Parameters:
//   - t: running test
//
// Returns the sandbox and the directory of the plugin.
func pluginSandbox(t *testing.T) (*sandbox, string) {
	t.Helper()

	sb := newSandbox(t)
	t.Cleanup(func() { openUp(sb.Dir) })

	src := filepath.Join(sb.Home, "plugins", "demo")
	sb.write(t, filepath.Join(src, "plugin.toml"), pluginManifest)

	return sb, src
}

// TestPluginVerbs runs a plugin through every verb of the real binary: check, install with its
// consent token, list, info, disable, enable and remove.
//
// Parameters:
//   - t: test to report to
func TestPluginVerbs(t *testing.T) {
	sb, src := pluginSandbox(t)

	out, errs, rc := sb.run(t, "plugin", "check", src)
	if rc != 0 || !strings.Contains(out, "no problem found") ||
		!strings.Contains(out, "grant token: exec:echo@") {
		t.Fatalf("check: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	token := out[strings.Index(out, "exec:echo@"):]
	token = strings.Fields(token)[0]

	// Without the token and without a terminal, nothing is installed.
	if _, errs, rc = sb.run(t, "plugin", "install", src); rc != 2 ||
		!strings.Contains(errs, "consent needed") {
		t.Errorf("install without token: exit %d, stderr %q", rc, errs)
	}

	wrong := "exec:echo@000000000000"
	if _, errs, rc = sb.run(t, "plugin", "install", src, "--grant", wrong); rc != 4 ||
		!strings.Contains(errs, "does not match") {
		t.Errorf("install with a wrong token: exit %d, stderr %q", rc, errs)
	}

	if out, errs, rc = sb.run(t, "plugin", "install", src, "--dry-run"); rc != 0 ||
		!strings.Contains(out, "Install plugin demo 1.0.0?") {
		t.Errorf("dry run: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	if out, errs, rc = sb.run(t, "plugin", "list", "--format", "tsv"); rc != 0 ||
		strings.Contains(out, "demo\t") {
		t.Errorf("nothing should be installed yet: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	if out, errs, rc = sb.run(t, "plugin", "install", src, "--grant", token); rc != 0 ||
		!strings.Contains(out, "installed plugin demo 1.0.0 (enabled)") {
		t.Fatalf("install: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	if out, _, rc = sb.run(t, "plugin", "list", "--format", "tsv"); rc != 0 ||
		!strings.Contains(out, "demo\t1.0.0\tenabled") {
		t.Errorf("list: exit %d, stdout %q", rc, out)
	}

	if out, _, rc = sb.run(t, "plugin", "info", "demo"); rc != 0 ||
		!strings.Contains(out, "demo 1.0.0") {
		t.Errorf("info: exit %d, stdout %q", rc, out)
	}

	if out, _, rc = sb.run(t, "plugin", "check", "--json"); rc != 0 ||
		!strings.Contains(out, `"ok": true`) {
		t.Errorf("check all: exit %d, stdout %q", rc, out)
	}

	for _, step := range []struct {
		args []string
		want string
	}{
		{[]string{"plugin", "disable", "demo"}, "disabled"},
		{[]string{"plugin", "enable", "demo"}, "enabled"},
	} {
		if out, errs, rc = sb.run(t, step.args...); rc != 0 || !strings.Contains(out, step.want) {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", step.args, rc, out, errs)
		}
	}

	// Removing asks first; without a terminal it needs --yes.
	if _, errs, rc = sb.run(t, "plugin", "remove", "demo"); rc != 2 {
		t.Errorf("remove without --yes: exit %d, stderr %q", rc, errs)
	}

	if out, errs, rc = sb.run(t, "plugin", "remove", "demo", "--yes"); rc != 0 ||
		!strings.Contains(out, `removed plugin "demo"`) {
		t.Errorf("remove: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	if _, errs, rc = sb.run(t, "plugin", "info", "demo"); rc != 1 ||
		!strings.Contains(errs, "not installed") {
		t.Errorf("info after remove: exit %d, stderr %q", rc, errs)
	}
}

// TestPluginExitCodesBinary reaches the exit codes that need no network from the real binary and
// checks that the JSON error object is on standard output alone.
//
// Parameters:
//   - t: test to report to
func TestPluginExitCodesBinary(t *testing.T) {
	sb, _ := pluginSandbox(t)

	tests := []struct {
		name string
		args []string
		exit int
	}{
		{"list", []string{"plugin", "list"}, 0},
		{"unknown plugin", []string{"plugin", "info", "nope"}, 1},
		{"unknown subcommand", []string{"plugin", "frobnicate"}, 2},
		{"run is reserved", []string{"plugin", "run", "demo"}, 2},
		{"bad id", []string{"plugin", "remove", "../data", "--yes"}, 2},
		{"missing archive checksum", []string{
			"plugin", "install",
			"https://github.com/acme/demo/releases/download/1.0.0/demo.tar.gz",
		}, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, rc := sb.run(t, tc.args...); rc != tc.exit {
				t.Errorf("exit %d, want %d", rc, tc.exit)
			}

			if tc.exit == 0 {
				return
			}

			out, errs, rc := sb.run(t, append(tc.args, "--json")...)
			if rc != tc.exit || errs != "" || !strings.Contains(out, `"ok": false`) {
				t.Errorf("--json: exit %d, stdout %q, stderr %q", rc, out, errs)
			}
		})
	}
}

// TestPluginSessionOff checks that --no-plugins keeps enable out, with exit 4, and that the
// verbs which read still work.
//
// Parameters:
//   - t: test to report to
func TestPluginSessionOff(t *testing.T) {
	sb, src := pluginSandbox(t)

	out, _, _ := sb.run(t, "plugin", "check", src)
	token := strings.Fields(out[strings.Index(out, "exec:echo@"):])[0]

	if _, errs, rc := sb.run(t, "plugin", "install", src, "--grant", token); rc != 0 {
		t.Fatalf("install: exit %d, stderr %q", rc, errs)
	}

	if _, _, rc := sb.run(t, "plugin", "disable", "demo"); rc != 0 {
		t.Fatalf("disable: exit %d", rc)
	}

	if _, errs, rc := sb.run(t, "--no-plugins", "plugin", "enable", "demo"); rc != 4 ||
		!strings.Contains(errs, "off for this run") {
		t.Errorf("enable with --no-plugins: exit %d, stderr %q", rc, errs)
	}

	if out, _, rc := sb.run(t, "--no-plugins", "plugin", "list", "--format", "tsv"); rc != 0 ||
		!strings.Contains(out, "demo\t1.0.0\tdisabled") {
		t.Errorf("list with --no-plugins: exit %d, stdout %q", rc, out)
	}
}
