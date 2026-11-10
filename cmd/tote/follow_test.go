package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/totecli/tote/internal/store"
	"github.com/totecli/tote/internal/update"
)

// sealTree writes a file in a directory and makes the tree read-only the way an installed plugin
// is: directories 0500 and files 0400.
//
// Parameters:
//   - t: running test
//   - dir: directory that holds the tree
//   - rel: path of the file inside the tree
func sealTree(t *testing.T, dir, rel string) {
	t.Helper()

	file := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(file, 0o400); err != nil {
		t.Fatal(err)
	}

	for d := filepath.Dir(file); d != filepath.Dir(dir); d = filepath.Dir(d) {
		if err := os.Chmod(d, 0o500); err != nil {
			t.Fatal(err)
		}
	}
}

// TestUninstallPurgeReadOnlyTree checks that --purge deletes the read-only tree of an installed
// plugin, which a plain recursive delete refuses, and then removes the program.
//
// Parameters:
//   - t: test to report to
func TestUninstallPurgeReadOnlyTree(t *testing.T) {
	h, target := uninstallSetup(t)
	t.Cleanup(func() { makeWritable(h.dir) })

	tree := filepath.Join(h.vars["TOTE_DATA_DIR"], "plugins", "gft")
	sealTree(t, tree, "0123456789abcdef/locales/fr.toml")

	if code := h.run("uninstall", "--yes", "--purge"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	for _, p := range []string{target, tree, h.vars["TOTE_DATA_DIR"]} {
		if exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
}

// TestUninstallFailedPurgeKeepsProgram checks that a purge that cannot finish tries every
// directory, names what failed, exits with an error and keeps the program, so that the
// uninstall can be run again; once the cause is fixed the second run finishes the job.
//
// Parameters:
//   - t: test to report to
func TestUninstallFailedPurgeKeepsProgram(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove entries of a read-only directory")
	}

	h, target := uninstallSetup(t)
	t.Cleanup(func() { makeWritable(h.dir) })

	state := h.vars["TOTE_STATE_DIR"]
	stuck := filepath.Join(state, "plugins", "gft", "last")

	if err := os.MkdirAll(filepath.Dir(stuck), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(stuck, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A state directory that cannot be changed makes the removal of its entries fail.
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatal(err)
	}

	rc := h.run("uninstall", "--yes", "--purge", "--shell")
	if rc != exitFail {
		t.Fatalf("code %d, want %d; stderr %q", rc, exitFail, h.errb.String())
	}

	errs := h.errb.String()
	for _, want := range []string{
		filepath.Join(state, "plugins"), "is kept", "run tote uninstall again",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errs)
		}
	}

	if strings.Contains(h.out.String(), "tote is removed") {
		t.Errorf("success message after a failure:\n%s", h.out.String())
	}

	if !exists(target) {
		t.Error("the program was removed although the purge failed")
	}

	// The other directories and the startup file were handled all the same.
	if exists(h.path("connections.toml")) || exists(h.vars["TOTE_CACHE_DIR"]) {
		t.Error("a directory after the failing one was not purged")
	}

	startup := filepath.Join(h.app.Home, ".bashrc")
	if data, _ := os.ReadFile(startup); string(data) != "export A=1\n" {
		t.Errorf(".bashrc = %q", data)
	}

	if err := os.Chmod(state, 0o700); err != nil {
		t.Fatal(err)
	}

	if code := h.run("uninstall", "--yes", "--purge"); code != 0 {
		t.Fatalf("second run: code %d, stderr %q", code, h.errb.String())
	}

	if exists(target) || exists(state) {
		t.Error("the second run left the program or the state directory")
	}
}

// TestUninstallLeftAlone checks that a line that runs tote init but that --shell does not delete
// is named in the plan and in the summary, as one to remove by hand, and stays in the file.
//
// Parameters:
//   - t: test to report to
func TestUninstallLeftAlone(t *testing.T) {
	h, target := uninstallSetup(t)
	rc := filepath.Join(h.app.Home, ".bashrc")
	text := "export A=1\n" +
		"eval \"$(tote init bash)\"\n" +
		"eval \"$(tote init bash)\" && echo ready\n" +
		"source <(tote init bash)\n" +
		"echo \"tote init bash\"\n"

	if err := os.WriteFile(rc, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	left := func(n int) string { return fmt.Sprintf("left alone, remove by hand: %s:%d", rc, n) }

	if code := h.run("uninstall", "--dry-run", "--shell"); code != 0 {
		t.Fatalf("dry run: code %d, stderr %q", code, h.errb.String())
	}

	out := h.out.String()
	for _, want := range []string{
		"the line that loads tote, in " + rc + " (line 2)", left(3), left(5),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}

	for _, line := range []int{2, 4} {
		if strings.Contains(out, left(line)) {
			t.Errorf("line %d is deleted by --shell, it is not left alone:\n%s", line, out)
		}
	}

	if code := h.run("uninstall", "--yes", "--shell"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	// The two deleted lines took the numbers of the next ones with them: the summary counts again.
	// The plan comes first and is printed with the numbers of the file as it was.
	_, out, _ = strings.Cut(h.out.String(), "removed ")
	if !strings.Contains(out, left(2)) || !strings.Contains(out, left(3)) ||
		strings.Contains(out, left(5)) || exists(target) {
		t.Errorf("summary:\n%s", out)
	}

	data, _ := os.ReadFile(rc)
	want := "export A=1\neval \"$(tote init bash)\" && echo ready\necho \"tote init bash\"\n"

	if string(data) != want {
		t.Errorf(".bashrc = %q, want %q", data, want)
	}

	h2, _ := uninstallSetup(t)
	h2.vars["TOTE_LANG"] = "fr"

	if err := os.WriteFile(filepath.Join(h2.app.Home, ".bashrc"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	h2.run("uninstall", "--dry-run")

	french := "laiss\u00e9e telle quelle, \u00e0 retirer \u00e0 la main"
	if !strings.Contains(h2.out.String(), french) {
		t.Errorf("french plan:\n%s", h2.out.String())
	}
}

// TestLoadLinesAgree checks that the line finder of the doctor and the ones of the uninstall
// give the same answer: every line the doctor reports is either deleted by --shell or named as
// one to remove by hand, and a line none of them reports is not about tote.
//
// Parameters:
//   - t: test to report to
func TestLoadLinesAgree(t *testing.T) {
	tests := []struct {
		line    string
		loads   bool
		mention bool
	}{
		{`eval "$(tote init bash)"`, true, false},
		{`  eval "$(tote init zsh)"  # tote`, true, false},
		{`source <(tote init bash)`, true, false},
		{`if command -v tote >/dev/null; then eval "$(tote init bash)"; fi`, true, false},
		{`command -v tote && eval "$(tote init bash)"`, true, false},
		{`eval "$(tote init bash)" && echo ready`, false, true},
		{`echo "tote init bash"`, false, true},
		{`for f in a; do eval "$(tote init bash)"; echo $f; done`, false, true},
		{`# eval "$(tote init bash)"`, false, false},
		{`export PATH="$PATH:$HOME/tote"`, false, false},
		{`eval "$(mytote init bash)"`, false, false},
		{`eval "$(tote init fish)"`, false, false},
		{`alias t="tote"`, false, false},
	}

	dir := t.TempDir()

	for i, tt := range tests {
		path := filepath.Join(dir, fmt.Sprintf("rc%d", i))
		if err := os.WriteFile(path, []byte(tt.line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		lines, err := update.ShellLines(path)
		if err != nil {
			t.Fatal(err)
		}

		left, err := update.ShellMentions(path)
		if err != nil {
			t.Fatal(err)
		}

		if got := len(lines) == 1; got != tt.loads {
			t.Errorf("%q: ShellLines = %v, want loads %v", tt.line, lines, tt.loads)
		}

		if got := len(left) == 1; got != tt.mention {
			t.Errorf("%q: ShellMentions = %v, want mention %v", tt.line, left, tt.mention)
		}

		found := len(findLoadLines([]string{path})) == 1
		if want := tt.loads || tt.mention; found != want {
			t.Errorf("%q: findLoadLines found it = %v, want %v", tt.line, found, want)
		}
	}
}

// TestTooBigIsTranslated checks that a write refused for its size reads in the language of the
// run, with the name of the file, and not as the English error of the store.
//
// Parameters:
//   - t: test to report to
func TestTooBigIsTranslated(t *testing.T) {
	err := fmt.Errorf("%w: aliases.toml would be 4194305 bytes, the limit is 4194304",
		store.ErrTooBig)

	for lang, want := range map[string]string{
		"en": "aliases.toml would grow too large to be read back, nothing was written",
		"fr": "aliases.toml deviendrait trop gros",
	} {
		h := newHarness(t)
		h.vars["TOTE_LANG"] = lang

		if got := h.app.errorText(err); !strings.Contains(got, want) {
			t.Errorf("%s: %q lacks %q", lang, got, want)
		}
	}

	// Reached through the verb: the error of the store goes through the same text.
	h := newHarness(t)
	if code := h.app.fail(err); code != exitFail {
		t.Errorf("exit %d", code)
	}
}

// TestViaQuoteIsTranslated checks the refusal of a quote in a via, in both languages.
//
// Parameters:
//   - t: test to report to
func TestViaQuoteIsTranslated(t *testing.T) {
	for lang, want := range map[string]string{
		"en": "quotes are not interpreted: the via is split on spaces",
		"fr": "les guillemets ne sont pas interpr\u00e9t\u00e9s",
	} {
		h := newHarness(t)
		h.vars["TOTE_LANG"] = lang

		code := h.run("co", "add", "web", "--target", "host.example.org", "--via", `ssh -o 'A B'`)
		if code != exitFail || !strings.Contains(h.errb.String(), want) {
			t.Errorf("%s: code %d, stderr %q", lang, code, h.errb.String())
		}
	}
}

// TestPluginConfigNote checks the note about a config.toml that could not be edited: a file
// without the write permission is called read-only, any other file is said to be in a layout
// that tote does not rewrite.
//
// Parameters:
//   - t: test to report to
func TestPluginConfigNote(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
		verb string
		want string
		deny string
	}{
		{"read-only, remove", 0o400, "purge", "is read-only", "does not rewrite"},
		{"read-only, enable", 0o400, "enable", "is read-only", "does not rewrite"},
		{"read-only, disable", 0o400, "disable", "is read-only", "does not rewrite"},
		{"layout, remove", 0o600, "purge", "does not rewrite", "read-only"},
		{"layout, enable", 0o600, "enable", "does not rewrite", "read-only"},
		{"layout, disable", 0o600, "disable", "does not rewrite", "read-only"},
	}

	for _, tt := range tests {
		h := newHarness(t)
		h.write("config.toml", "schema = 1\n")

		if err := os.Chmod(h.path("config.toml"), tt.mode); err != nil {
			t.Fatal(err)
		}

		h.app.configNote(tt.verb, h.path("config.toml"), "demo")

		got := h.errb.String()
		if !strings.Contains(got, tt.want) || strings.Contains(got, tt.deny) ||
			!strings.Contains(got, "plugins.demo") {
			t.Errorf("%s: stderr %q", tt.name, got)
		}

		if err := os.Chmod(h.path("config.toml"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPluginRemoveReadOnlyConfig checks "plugin remove" with a read-only config.toml that says
// enabled = false for the plugin: the note says the file is read-only, and the exit stays 0.
//
// Parameters:
//   - t: test to report to
func TestPluginRemoveReadOnlyConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only file")
	}

	e := newPluginEnv(t)
	e.install(e.plugin("demo", "1.0.0", "exec:echo"))
	e.write("config.toml", "schema = 1\n\n[plugins.demo]\nenabled = false\n")

	if err := os.Chmod(e.path("config.toml"), 0o400); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(e.path("config.toml"), 0o600) })

	rc, _, errs := e.do("plugin", "remove", "demo", "--yes")
	if rc != 0 || !strings.Contains(errs, "is read-only") ||
		strings.Contains(errs, "does not rewrite") {
		t.Errorf("exit %d, stderr %q", rc, errs)
	}
}

// TestPluginCheckSharedDir checks that "plugin check" without an argument reports a plugins
// directory that others can write once, as a problem of every plugin, with the chmod hint, also
// when no plugin is installed.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckSharedDir(t *testing.T) {
	e := newPluginEnv(t)
	e.install(e.plugin("demo", "1.0.0", "exec:echo"))
	e.install(e.plugin("other", "1.0.0", "exec:echo"))

	dir := filepath.Join(e.vars["TOTE_DATA_DIR"], "plugins")
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}

	rc, out, _ := e.do("plugin", "check")
	if rc != exitFail || strings.Count(out, "can be written by other users") != 1 ||
		!strings.Contains(out, "chmod go-w "+e.norm(dir)) ||
		!strings.Contains(out, "plugins checked: 2") || strings.Contains(out, "no problem found") {
		t.Errorf("exit %d, stdout:\n%s", rc, out)
	}

	var doc struct {
		OK       bool `json:"ok"`
		Problems []struct {
			Code string `json:"code"`
		} `json:"problems"`
		Plugins []struct {
			Findings []struct {
				Code string `json:"code"`
			} `json:"findings"`
		} `json:"plugins"`
	}

	rc, out, _ = e.do("plugin", "check", "--json")
	if err := json.Unmarshal([]byte(out), &doc); err != nil || rc != exitFail {
		t.Fatalf("exit %d, %v:\n%s", rc, err, out)
	}

	if doc.OK || len(doc.Problems) != 1 || doc.Problems[0].Code != "store_writable" {
		t.Errorf("json problems: %+v", doc)
	}

	for _, p := range doc.Plugins {
		if len(p.Findings) != 0 {
			t.Errorf("the finding is repeated inside a plugin: %+v", p.Findings)
		}
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	if rc, out, _ = e.do("plugin", "check"); rc != 0 || strings.Contains(out, "other users") {
		t.Errorf("after chmod: exit %d, stdout:\n%s", rc, out)
	}

	// No plugin installed: the directory alone is reported.
	e2 := newPluginEnv(t)
	dir2 := filepath.Join(e2.vars["TOTE_DATA_DIR"], "plugins")

	if err := os.MkdirAll(dir2, 0o777); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir2, 0o777); err != nil {
		t.Fatal(err)
	}

	rc, out, _ = e2.do("plugin", "check")
	if rc != exitFail || !strings.Contains(out, "chmod go-w "+e2.norm(dir2)) ||
		!strings.Contains(out, "plugins checked: 0") {
		t.Errorf("no plugin: exit %d, stdout:\n%s", rc, out)
	}
}

// TestPluginCheckSharedDirBelowLink checks that the unsafe plugins directory is still reported
// once when the data directory is spelled through a link, as the temporary directory of macOS is:
// the finding of the plugin layer and the one of the verb must be recognised as the same.
//
// Parameters:
//   - t: test to report to
func TestPluginCheckSharedDirBelowLink(t *testing.T) {
	e := newPluginEnv(t)

	target := filepath.Join(e.dir, "target")
	link := filepath.Join(e.dir, "link")

	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symbolic link: %v", err)
	}

	e.vars["TOTE_DATA_DIR"] = filepath.Join(link, "data")

	e.install(e.plugin("demo", "1.0.0", "exec:echo"))

	dir := filepath.Join(link, "data", "plugins")
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}

	rc, out, _ := e.do("plugin", "check")
	if rc != exitFail || strings.Count(out, "can be written by other users") != 1 ||
		!strings.Contains(out, "chmod go-w "+e.norm(dir)) {
		t.Errorf("exit %d, stdout:\n%s", rc, out)
	}
}

// TestNotifyContextHangup checks that the closing of the terminal cancels a long operation like a
// termination signal does, so that a download is cleaned up.
//
// Parameters:
//   - t: test to report to
func TestNotifyContextHangup(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM} {
		ctx, stop := notifyContext()

		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			stop()
			t.Fatal(err)
		}

		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Errorf("%v did not cancel the context", sig)
		}

		stop()

		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("%v: error %v", sig, ctx.Err())
		}
	}
}
