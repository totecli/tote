package e2e

import (
	"strings"
	"testing"
)

// installDemo installs the neutral plugin "demo" with the command line, so that the interface has
// something to show.
//
// Parameters:
//   - t: running test
//   - sb: sandbox
//   - src: directory of the source of the plugin
func installDemo(t *testing.T, sb *sandbox, src string) {
	t.Helper()

	grant := "exec:echo@" + grantOf(t, sb, src)

	out, errs, rc := sb.run(t, "plugin", "install", src, "--grant", grant)
	if rc != 0 {
		t.Fatalf("install: exit %d, stdout %q, stderr %q", rc, out, errs)
	}
}

// grantOf returns the hash part of the consent token of a package.
//
// Parameters:
//   - t: running test
//   - sb: sandbox
//   - src: directory of the source of the plugin
func grantOf(t *testing.T, sb *sandbox, src string) string {
	t.Helper()

	out, errs, rc := sb.run(t, "plugin", "check", src)
	if rc != 0 {
		t.Fatalf("check: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	i := strings.Index(out, "exec:echo@")
	if i < 0 {
		t.Fatalf("no grant token in %q", out)
	}

	return strings.TrimPrefix(strings.Fields(out[i:])[0], "exec:echo@")
}

// TestPluginsScreenBinary drives the plugins screen of the real binary: the list, the toggle that
// the command line sees, the removal question that is answered no, the smallest terminal and the
// exit that restores the terminal.
//
// Parameters:
//   - t: test to report to
func TestPluginsScreenBinary(t *testing.T) {
	t.Parallel()

	sb, src := pluginSandbox(t)
	installDemo(t, sb, src)

	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote", "plugin")

	tm.waitText("Plugins")
	tm.waitText("A neutral demo plugin.")
	tm.waitText("space disable")

	tm.key("Space")
	tm.waitText(`plugin "demo" disabled`)
	tm.waitText("space enable")

	if out, _, _ := sb.run(t, "plugin", "list", "--format", "tsv"); !strings.Contains(out,
		"demo\t1.0.0\tdisabled") {
		t.Errorf("the command line does not see the toggle: %q", out)
	}

	tm.key("Space")
	tm.waitText(`plugin "demo" enabled`)

	tm.key("d")
	tm.waitText("Remove")
	tm.key("n")
	tm.waitGone("Remove plugin")

	if out, _, _ := sb.run(t, "plugin", "list", "--format", "tsv"); !strings.Contains(out,
		"demo\t1.0.0\tenabled") {
		t.Errorf("the plugin did not stay installed: %q", out)
	}

	tm.resize(24, 7)
	tm.waitText("demo")
	tm.resize(100, 30)
	tm.waitText("A neutral demo plugin.")

	tm.key("Escape")
	tm.waitText("Welcome")
	tm.key("q")

	if code := tm.waitDone(); code != 0 {
		t.Errorf("exit status %d, want 0; screen:\n%s", code, tm.look().Text)
	}

	tm.restored()
}

// TestPluginsConsentBinary drives the consent of the plugin that ships with tote: the dialog shows
// what is allowed, Cancel has the focus, Esc installs nothing, and the answer Allow installs it.
//
// Parameters:
//   - t: test to report to
func TestPluginsConsentBinary(t *testing.T) {
	t.Parallel()

	sb, _ := pluginSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote", "plugin")

	tm.waitText("Plugins")
	tm.waitText("gft")

	tm.key("i")
	tm.waitText("Install plugin gft")
	tm.waitText("Cancel")
	tm.key("Escape")
	tm.waitGone("Install plugin gft")

	if out, _, _ := sb.run(t, "plugin", "list", "--format", "tsv"); strings.Contains(out,
		"gft\t1.0.0\tenabled") {
		t.Errorf("gft was installed by Esc: %q", out)
	}

	tm.key("i")
	tm.waitText("Install plugin gft")
	tm.key("Tab")
	tm.key("Enter")
	tm.waitText("installed plugin gft")

	out, _, _ := sb.run(t, "plugin", "list", "--format", "tsv")
	if !strings.Contains(out, "gft\t") {
		t.Errorf("gft is not installed after the consent: %q", out)
	}

	tm.key("q")

	if code := tm.waitDone(); code != 0 {
		t.Errorf("exit status %d, want 0; screen:\n%s", code, tm.look().Text)
	}

	tm.restored()
}
