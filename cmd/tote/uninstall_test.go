package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/update"
)

// uninstallSetup builds a harness that runs as a fake installed program, with a receipt, data
// in the four directories, a plugin tree, a file that is not tote's and a startup file.
//
// Parameters:
//   - t: running test
//
// Returns the harness and the path of the fake program.
func uninstallSetup(t *testing.T) (*harness, string) {
	t.Helper()

	h := newHarness(t)

	// The directories are named tote, as the defaults are: a directory chosen with another name
	// may be shared, and then --purge does not remove the directories it holds.
	for _, k := range []string{
		"TOTE_CONFIG_DIR", "TOTE_DATA_DIR", "TOTE_STATE_DIR", "TOTE_CACHE_DIR",
	} {
		h.vars[k] = filepath.Join(h.vars[k], "tote")
	}

	target := installFake(h, newTestMirror(t, newTestSigner(1)), "1.0.0")

	writeReceipt(h, update.Receipt{InstallMethod: "script", Path: target, Version: "1.0.0"})
	h.fixtures()
	h.write("notes.txt", "mine")

	rcText := "export A=1\neval \"$(tote init bash)\"\n"
	files := map[string]string{
		h.path("docs/guide.md"): "doc",
		filepath.Join(h.vars["TOTE_DATA_DIR"], "plugins", "p", "plugin.toml"): "x",
		filepath.Join(h.vars["TOTE_DATA_DIR"], "bin", "script.sh"):            "x",
		filepath.Join(h.vars["TOTE_CACHE_DIR"], update.CacheFile):             "{}",
		filepath.Join(h.app.Home, ".bashrc"):                                  rcText,
	}

	for p, text := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return h, target
}

// exists reports whether a path exists.
//
// Parameters:
//   - p: path to test
func exists(p string) bool {
	_, err := os.Lstat(p)

	return err == nil
}

// TestUninstallManaged checks that an apt or brew install is refused with the command to run.
//
// Parameters:
//   - t: test to report to
func TestUninstallManaged(t *testing.T) {
	for method, command := range map[string]string{
		"apt":  "sudo apt remove tote",
		"brew": "brew uninstall tote",
	} {
		h, target := uninstallSetup(t)
		writeReceipt(h, update.Receipt{InstallMethod: method, Path: target})

		if code := h.run("uninstall", "--yes", "--purge", "--shell"); code != 1 {
			t.Errorf("%s: code %d", method, code)
		}

		if !strings.Contains(h.errb.String(), command) {
			t.Errorf("%s: stderr %q lacks %q", method, h.errb.String(), command)
		}

		if !exists(target) || !exists(h.path("connections.toml")) {
			t.Errorf("%s: something was removed", method)
		}
	}
}

// TestUninstallNeedsConfirmation checks that nothing is removed without a yes.
//
// Parameters:
//   - t: test to report to
func TestUninstallNeedsConfirmation(t *testing.T) {
	tests := []struct {
		name  string
		tty   bool
		stdin string
		args  []string
		code  int
	}{
		{"no terminal, no --yes", false, "", nil, 2},
		{"answer no", true, "n\n", nil, 1},
		{"empty answer", true, "\n", nil, 1},
		{"end of input", true, "", nil, 1},
		{"junk answer", true, "yes please\n", nil, 1},
		{"purge: second answer no", true, "y\nn\n", []string{"--purge"}, 1},
		{"purge: no second answer", true, "y\n", []string{"--purge"}, 1},
	}

	for _, tt := range tests {
		h, target := uninstallSetup(t)
		h.ttyIn, h.stdin = tt.tty, tt.stdin

		if code := h.run(append([]string{"uninstall"}, tt.args...)...); code != tt.code {
			t.Errorf("%s: code %d, stderr %q", tt.name, code, h.errb.String())
		}

		receipt := filepath.Join(h.vars["TOTE_STATE_DIR"], "install.json")

		for _, p := range []string{target, h.path("connections.toml"), receipt} {
			if !exists(p) {
				t.Errorf("%s: %s was removed", tt.name, p)
			}
		}
	}
}

// TestUninstallDefault checks that a confirmed uninstall removes the program and the receipt
// and nothing else, and tells what it keeps.
//
// Parameters:
//   - t: test to report to
func TestUninstallDefault(t *testing.T) {
	h, target := uninstallSetup(t)
	h.ttyIn, h.stdin = true, "y\n"

	if code := h.run("uninstall"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if exists(target) || exists(filepath.Join(h.vars["TOTE_STATE_DIR"], "install.json")) {
		t.Error("the program or its receipt is still there")
	}

	if exists(h.vars["TOTE_STATE_DIR"]) {
		t.Error("the empty state directory is still there")
	}

	for _, p := range []string{
		h.path("connections.toml"), h.path("aliases.toml"), h.path("notes.txt"),
		filepath.Join(h.vars["TOTE_DATA_DIR"], "plugins", "p", "plugin.toml"),
		filepath.Join(h.app.Home, ".bashrc"),
	} {
		if !exists(p) {
			t.Errorf("%s was removed", p)
		}
	}

	out := h.out.String()

	for _, want := range []string{
		"the program " + target, "the install receipt", "kept: the line that loads tote, in " +
			filepath.Join(h.app.Home, ".bashrc") + " (line 2)", "are kept; add --purge", "tote is removed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}

	if !strings.Contains(h.errb.String(), "remove tote? [y/N]") {
		t.Errorf("prompt: %q", h.errb.String())
	}
}

// TestUninstallShell checks --shell: only the loading line goes.
//
// Parameters:
//   - t: test to report to
func TestUninstallShell(t *testing.T) {
	h, target := uninstallSetup(t)

	if code := h.run("uninstall", "--yes", "--shell"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	data, _ := os.ReadFile(filepath.Join(h.app.Home, ".bashrc"))
	if string(data) != "export A=1\n" || exists(target) {
		t.Errorf(".bashrc = %q", data)
	}

	if !exists(h.path("connections.toml")) {
		t.Error("data was removed")
	}
}

// TestUninstallPurge checks --purge: tote's own files go, the user's other files stay with
// their directory, and the confirmations are separate.
//
// Parameters:
//   - t: test to report to
func TestUninstallPurge(t *testing.T) {
	h, target := uninstallSetup(t)
	h.ttyIn, h.stdin = true, "y\ny\n"

	if code := h.run("uninstall", "--purge"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if exists(target) {
		t.Error("the program is still there")
	}

	for _, p := range []string{
		h.path("connections.toml"), h.path("aliases.toml"), h.path("repos.toml"), h.path("funcs.toml"),
		filepath.Join(h.vars["TOTE_DATA_DIR"]), filepath.Join(h.vars["TOTE_CACHE_DIR"]),
		h.vars["TOTE_STATE_DIR"],
	} {
		if exists(p) {
			t.Errorf("%s is still there", p)
		}
	}

	// tote 1.0 never creates a docs folder, so one in the config directory is the user's.
	if !exists(h.path("notes.txt")) || !exists(h.path("docs/guide.md")) ||
		!exists(h.vars["TOTE_CONFIG_DIR"]) {
		t.Error("a file that is not tote's, or its directory, was removed")
	}

	if got := strings.Count(h.errb.String(), "[y/N]"); got != 2 {
		t.Errorf("%d prompts: %q", got, h.errb.String())
	}

	if !strings.Contains(h.out.String(), "kept "+h.vars["TOTE_CONFIG_DIR"]) ||
		!strings.Contains(h.out.String(), "cannot be undone") {
		t.Errorf("stdout:\n%s", h.out.String())
	}

	if !exists(filepath.Join(h.app.Home, ".bashrc")) {
		t.Error("the startup file was touched without --shell")
	}
}

// TestUninstallDryRun checks that --dry-run prints the plan, exits 0, asks nothing, and removes
// nothing, with or without a terminal and with every other option.
//
// Parameters:
//   - t: test to report to
func TestUninstallDryRun(t *testing.T) {
	tests := []struct {
		name string
		tty  bool
		args []string
	}{
		{"no terminal", false, []string{"--dry-run"}},
		{"terminal", true, []string{"--dry-run"}},
		{"purge and shell", false, []string{"--dry-run", "--purge", "--shell"}},
		{"yes", false, []string{"--yes", "--purge", "--dry-run"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, target := uninstallSetup(t)
			h.ttyIn = tt.tty

			if code := h.run(append([]string{"uninstall"}, tt.args...)...); code != 0 {
				t.Fatalf("code %d, stderr %q", code, h.errb.String())
			}

			if !strings.Contains(h.out.String(), target) {
				t.Errorf("the plan does not name the program:\n%s", h.out.String())
			}

			if strings.Contains(h.errb.String(), "[y/N]") {
				t.Errorf("a question was asked: %q", h.errb.String())
			}

			receipt := filepath.Join(h.vars["TOTE_STATE_DIR"], "install.json")

			for _, p := range []string{
				target, h.path("connections.toml"), receipt, filepath.Join(h.app.Home, ".bashrc"),
			} {
				if !exists(p) {
					t.Errorf("%s was removed", p)
				}
			}
		})
	}
}

// TestUninstallFrench checks the French answers and messages.
//
// Parameters:
//   - t: test to report to
func TestUninstallFrench(t *testing.T) {
	h, target := uninstallSetup(t)
	h.vars["TOTE_LANG"] = "fr"
	h.ttyIn, h.stdin = true, "oui\n"

	if code := h.run("uninstall"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if exists(target) || !strings.Contains(h.errb.String(), "retirer tote\u00a0? [o/N]") ||
		!strings.Contains(h.out.String(), "tote est retir\u00e9") {
		t.Errorf("stdout %q, stderr %q", h.out.String(), h.errb.String())
	}
}

// TestUninstallForeignReceipt checks that a receipt of another program is kept.
//
// Parameters:
//   - t: test to report to
func TestUninstallForeignReceipt(t *testing.T) {
	h, target := uninstallSetup(t)
	writeReceipt(h, update.Receipt{InstallMethod: "script", Path: "/elsewhere/tote"})

	if code := h.run("uninstall", "--yes"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if exists(target) || !exists(filepath.Join(h.vars["TOTE_STATE_DIR"], "install.json")) {
		t.Error("wrong files removed")
	}

	if !strings.Contains(h.out.String(), "describes another program") {
		t.Errorf("stdout %q", h.out.String())
	}
}

// TestUninstallNotWritable checks that a directory without write access stops everything
// before any change, and says what to run.
//
// Parameters:
//   - t: test to report to
func TestUninstallNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}

	h, target := uninstallSetup(t)
	dir := filepath.Dir(target)

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if code := h.run("uninstall", "--yes", "--purge", "--shell"); code != 1 {
		t.Errorf("code %d", code)
	}

	if e := h.errb.String(); !strings.Contains(e, "cannot remove "+target) ||
		!strings.Contains(e, "sudo rm") {
		t.Errorf("stderr %q", h.errb.String())
	}

	if !exists(target) || !exists(h.path("connections.toml")) ||
		!exists(filepath.Join(h.vars["TOTE_STATE_DIR"], "install.json")) {
		t.Error("something was removed")
	}
}

// TestUninstallTwice checks that a second run, from a program that still exists, finds the
// data kept and does not fail.
//
// Parameters:
//   - t: test to report to
func TestUninstallTwice(t *testing.T) {
	h, target := uninstallSetup(t)

	if code := h.run("uninstall", "--yes"); code != 0 {
		t.Fatalf("first run: code %d", code)
	}

	// The program is gone; a copy elsewhere runs the second time.
	if err := os.WriteFile(target, []byte(fakeBinary("1.0.0")), 0o755); err != nil {
		t.Fatal(err)
	}

	if code := h.run("uninstall", "--yes"); code != 0 {
		t.Errorf("second run: code %d, stderr %q", code, h.errb.String())
	}

	if exists(target) {
		t.Error("the second copy is still there")
	}
}

// TestDataDirsAreTotesOwn checks the names that --purge may delete: nothing outside the four
// directories of tote, and no path.
//
// Parameters:
//   - t: test to report to
func TestDataDirsAreTotesOwn(t *testing.T) {
	h, _ := uninstallSetup(t)

	paths, err := h.app.cfgPaths()
	if err != nil {
		t.Fatal(err)
	}

	if got := update.CacheFile; !slices.ContainsFunc(dataDirs(paths), func(d update.DataDir) bool {
		return d.Dir == paths.Cache && slices.Contains(d.Entries, got)
	}) {
		t.Errorf("the cache entries lack %s", got)
	}

	dirs := dataDirs(paths)
	if len(dirs) != 4 {
		t.Fatalf("%d directories", len(dirs))
	}

	for _, d := range dirs {
		if d.Dir != paths.Config && d.Dir != paths.Data && d.Dir != paths.Cache && d.Dir != paths.State {
			t.Errorf("unexpected directory %s", d.Dir)
		}

		for _, e := range d.Entries {
			if e == "" || strings.ContainsRune(e, '/') || strings.HasPrefix(e, "..") {
				t.Errorf("unsafe entry %q in %s", e, d.Dir)
			}
		}
	}
}

// TestUninstallPurgeCoversPlugins plays a purge in a sandbox where plugins were installed and
// consented, and checks that nothing of them survives, the consent record included, together
// with the lock files and the names kept for later releases, while the files of the user stay.
//
// Parameters:
//   - t: test to report to
func TestUninstallPurgeCoversPlugins(t *testing.T) {
	h, _ := uninstallSetup(t)
	h.vars["TOTE_LANG"] = "en"

	conf, data := h.vars["TOTE_CONFIG_DIR"], h.vars["TOTE_DATA_DIR"]
	state, cache := h.vars["TOTE_STATE_DIR"], h.vars["TOTE_CACHE_DIR"]

	gone := []string{
		filepath.Join(state, "plugins.lock"), filepath.Join(state, "plugins.lock.lock"),
		filepath.Join(state, "plugin-procs"), filepath.Join(state, "plugins", "gft", "last"),
		filepath.Join(state, "plugins-host", "gft", "last.log"),
		filepath.Join(cache, "plugins", "gft", "x"), filepath.Join(data, "plugins", ".mutex"),
		filepath.Join(data, "plugins", ".staging", "x"),
		filepath.Join(conf, "plugins", "gft.toml"), filepath.Join(conf, "plugins", "gft.toml.lock"),
		filepath.Join(conf, "config.toml"), filepath.Join(conf, "config.toml.lock"),
		filepath.Join(conf, "snippets.toml"), filepath.Join(conf, "tunnels.toml.lock"),
		filepath.Join(conf, "ports.toml"), filepath.Join(conf, "ports.toml.lock"),
	}

	for _, p := range gone {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if code := h.run("uninstall", "--yes", "--purge"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	for _, p := range gone {
		if exists(p) {
			t.Errorf("%s survived the purge", p)
		}
	}

	for _, p := range []string{
		filepath.Join(conf, "plugins"), filepath.Join(state, "plugins"),
		filepath.Join(cache, "plugins"), filepath.Join(state, "plugins-host"), state, cache, data,
	} {
		if exists(p) {
			t.Errorf("%s survived the purge", p)
		}
	}

	if !exists(h.path("notes.txt")) || !exists(h.path("docs/guide.md")) {
		t.Error("a file of the user was removed")
	}
}

// TestUninstallPurgePlanListsPlugins checks that the plan printed before the confirmation names
// the consent record and the plugin directories, so that the user sees what the purge deletes.
//
// Parameters:
//   - t: test to report to
func TestUninstallPurgePlanListsPlugins(t *testing.T) {
	h, _ := uninstallSetup(t)
	h.vars["TOTE_LANG"] = "en"

	lock := filepath.Join(h.vars["TOTE_STATE_DIR"], "plugins.lock")
	if err := os.WriteFile(lock, []byte("schema = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Without --yes and without a terminal the questions are refused, so only the plan is printed.
	h.run("uninstall", "--purge")

	for _, want := range []string{
		"your plugins and the access you granted them", lock,
		filepath.Join(h.vars["TOTE_DATA_DIR"], "plugins"),
	} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("plan lacks %q:\n%s", want, h.out.String())
		}
	}

	if !exists(lock) {
		t.Error("the plan removed the consent record")
	}
}

// TestUninstallCompletion checks the options that the shell completes for uninstall.
//
// Parameters:
//   - t: test to report to
func TestUninstallCompletion(t *testing.T) {
	h := newHarness(t)

	want := []string{"--yes", "--shell", "--purge", "--dry-run"}
	if got := names(completeLines(t, h, "uninstall", "")); !slices.Equal(got, want) {
		t.Errorf("uninstall options = %v, want %v", got, want)
	}
}
