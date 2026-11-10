package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// askSignal is a writer that tells when the question of a consent has been printed.
type askSignal struct {
	buf   bytes.Buffer
	once  sync.Once
	asked chan struct{}
}

// Write implements io.Writer.
//
// Parameters:
//   - p: bytes written to standard error
func (w *askSignal) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)

	if strings.Contains(w.buf.String(), "[y/N]") {
		w.once.Do(func() { close(w.asked) })
	}

	return n, err
}

// declined is the line that a refused consent prints.
const declined = "cancelled, nothing changed"

// withoutLicense removes the license from a manifest, which makes the package invalid.
//
// Parameters:
//   - manifest: contents of plugin.toml
func withoutLicense(manifest string) string {
	return strings.Replace(manifest, "license = \"MIT\"\n", "", 1)
}

// TestPluginInterruptAtTheQuestion checks that Ctrl-C at the consent question ends the wait at
// once, exits 130 and installs nothing, as the remove question does through the default signal.
//
// Parameters:
//   - t: test to report to
func TestPluginInterruptAtTheQuestion(t *testing.T) {
	e := newPluginEnv(t)
	dir := e.plugin("demo", "1.0.0", "exec:echo")

	stdin, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })

	sig := &askSignal{asked: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())

	t.Cleanup(cancel)

	go func() {
		<-sig.asked
		cancel()
	}()

	a := *e.app
	a.Stdin, a.Stderr = stdin, sig
	a.IsInputTerminal = func(io.Reader) bool { return true }
	a.Context = func() (context.Context, context.CancelFunc) { return ctx, func() {} }

	done := make(chan int, 1)

	go func() { done <- a.run([]string{"plugin", "install", dir}) }()

	select {
	case rc := <-done:
		if rc != exitInterrupted {
			t.Errorf("exit %d, want %d; stderr %q", rc, exitInterrupted, sig.buf.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Ctrl-C did not end the wait for the answer")
	}

	if strings.Contains(sig.buf.String(), "cancelled") {
		t.Errorf("an interrupt is not an answer: %q", sig.buf.String())
	}

	if got := e.installedIDs(); len(got) != 0 {
		t.Errorf("an interrupted install left %v", got)
	}
}

// TestPluginModifiedTree checks that a tree which no longer matches its checksum is an integrity
// failure (5) for check and enable, with the checksums in the message, and that the other
// verbs still see it.
//
// Parameters:
//   - t: test to report to
func TestPluginModifiedTree(t *testing.T) {
	e := newPluginEnv(t)
	e.install(e.plugin("demo", "1.0.0", "exec:echo"))

	trees, err := filepath.Glob(filepath.Join(e.vars["TOTE_DATA_DIR"], "plugins", "demo", "*"))
	if err != nil || len(trees) != 1 {
		t.Fatalf("trees = %v, %v", trees, err)
	}

	manifest := filepath.Join(trees[0], "plugin.toml")

	for _, p := range []string{trees[0], manifest} {
		if err := os.Chmod(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	f, err := os.OpenFile(manifest, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.WriteString("# changed\n"); err != nil {
		t.Fatal(err)
	}

	_ = f.Close()

	if err := os.Chmod(manifest, 0o400); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(trees[0], 0o500); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"plugin", "check", "demo"},
		{"plugin", "check"},
		{"plugin", "enable", "demo"},
	} {
		rc, out, errs := e.do(args...)
		if rc != exitIntegrity {
			t.Errorf("%v: exit %d, want %d; stdout %q, stderr %q", args, rc, exitIntegrity, out,
				errs)
		}
	}

	_, _, errs := e.do("plugin", "enable", "demo")
	if !strings.Contains(errs, "cannot enable demo: ") || strings.Contains(errs, "(, )") ||
		strings.Contains(errs, "expected ,") {
		t.Errorf("enable message: %q", errs)
	}

	rc, out, _ := e.do("plugin", "enable", "demo", "--json")
	if rc != exitIntegrity || !strings.Contains(out, `"exit": 5`) {
		t.Errorf("enable --json: exit %d, stdout %q", rc, out)
	}
}

// TestPluginCheckTargetIsWhole checks that a long path is shown whole, in the lines and in the
// JSON, and that the name of the sidecar file is the base name of the archive.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckTargetIsWhole(t *testing.T) {
	e := newPluginEnv(t)

	long := filepath.Join(e.src, strings.Repeat("a-very-long-directory-name-", 5))
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(long, "demo-1.0.0.tar.gz")
	data := archive(t, manifestOf("demo", "1.0.0", "exec:echo"))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	shown := e.norm(path)

	out := e.must("plugin", "check", path)
	if !strings.Contains(out, "demo-1.0.0.tar.gz.sha256") || !strings.Contains(out, ": "+shown) {
		t.Errorf("check:\n%s", out)
	}

	js := e.must("plugin", "check", path, "--json")
	if !strings.Contains(js, `"target": "`+shown+`"`) {
		t.Errorf("json:\n%s", js)
	}
}

// TestPluginCheckExitClasses checks the exit code of a check that fails: a bad archive is an
// integrity failure, a package with an error is a plain failure.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckExitClasses(t *testing.T) {
	e := newPluginEnv(t)

	bad := filepath.Join(e.src, "bad.tar.gz")
	if err := os.WriteFile(bad, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}

	noLicense := withoutLicense(manifestOf("demo", "1.0.0", "exec:echo"))

	tests := []struct {
		name   string
		target string
		exit   int
	}{
		{"bad archive", bad, exitIntegrity},
		{"package error", e.pluginText("nolic", noLicense), exitFail},
	}

	for _, tc := range tests {
		if rc, out, errs := e.do("plugin", "check", tc.target); rc != tc.exit {
			t.Errorf("%s: exit %d, want %d; stdout %q, stderr %q", tc.name, rc, tc.exit, out,
				errs)
		}
	}
}

// TestPluginInstallPinRules checks that --sha256 is a usage error for a directory and for a
// plugin shipped with tote, which cannot be checked against it.
//
// Parameters:
//   - t: test to report to
func TestPluginInstallPinRules(t *testing.T) {
	e := newPluginEnv(t).withBundled()
	dir := e.plugin("demo", "1.0.0", "exec:echo")
	sum := strings.Repeat("a", 64)

	for _, src := range []string{dir, "gft"} {
		rc, out, errs := e.do("plugin", "install", src, "--sha256", sum, "--dry-run")
		if rc != exitUsage || out != "" || !strings.Contains(errs, "--sha256 checks a release") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", src, rc, out, errs)
		}
	}
}

// TestPluginCheckUnknownRepo checks that --repo with a name that is not saved is reported, not
// silently turned into commands that cannot be built.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckUnknownRepo(t *testing.T) {
	e := newPluginEnv(t)
	dir := e.plugin("demo", "1.0.0", "exec:echo")

	rc, out, errs := e.do("plugin", "check", dir, "--argv", "--repo", "nosuch")
	if rc != exitFail || out != "" || !strings.Contains(errs, "nosuch") {
		t.Errorf("exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	rc, out, _ = e.do("plugin", "check", dir, "--argv", "--repo", "nosuch", "--json")
	if rc != exitFail || !strings.Contains(out, `"code": "not_found"`) {
		t.Errorf("--json: exit %d, stdout %q", rc, out)
	}
}

// TestPluginEnableHints checks the hint after a refusal to enable a plugin that ships with tote
// and is not installed, and that a wrong token points to what exists for enable.
//
// Parameters:
//   - t: test to report to
func TestPluginEnableHints(t *testing.T) {
	e := newPluginEnv(t).withBundled()

	_, _, errs := e.do("plugin", "enable", "gft")
	if !strings.Contains(errs, "tote plugin install gft") {
		t.Errorf("stderr %q", errs)
	}

	e.install(e.plugin("demo", "1.0.0", "exec:echo"))
	e.must("plugin", "disable", "demo")

	_, _, errs = e.do("plugin", "enable", "demo", "--grant", "exec:echo@000000000000")
	if strings.Contains(errs, "review it with --dry-run") {
		t.Errorf("enable has no --dry-run: %q", errs)
	}
}

// TestPluginConsentWraps checks that a consent view wraps at the width of the terminal, under the
// first word of the line, and is not touched when it goes to a pipe.
//
// Parameters:
//   - t: test to report to
func TestPluginConsentWraps(t *testing.T) {
	text := "tote runs only the listed programs but those programs can do whatever you can do"

	lines := wrapText("! ", text, 40)
	if len(lines) < 2 {
		t.Fatalf("lines = %q", lines)
	}

	for i, l := range lines {
		if textWidth(l) > 40 {
			t.Errorf("line %d is %d cells wide: %q", i, textWidth(l), l)
		}

		if i > 0 && !strings.HasPrefix(l, "  ") {
			t.Errorf("line %d does not hang under the first word: %q", i, l)
		}
	}

	if got := strings.Join(strings.Fields(strings.Join(lines, " ")), " "); got != "! "+text {
		t.Errorf("words lost: %q", got)
	}

	if got := wrapText("  - ", text, 0); len(got) != 1 || got[0] != "  - "+text {
		t.Errorf("no limit: %q", got)
	}

	long := strings.Repeat("x", 100)
	for _, l := range wrapText("Source: ", long, 40) {
		if textWidth(l) > 40 {
			t.Errorf("a long word was not cut: %q", l)
		}
	}
}
