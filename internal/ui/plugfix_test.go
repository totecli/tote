package ui

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/plugin"
	"github.com/totecli/tote/internal/plugin/bundled"
)

// dataPlugins is the data set of the frames of the plugin screens: the four fixtures, a plugin in
// each state and the plugin that ships with tote and is not installed.
const dataPlugins dataSet = dataVia + 1

// plugLab is a registry over the sandbox of a test, with the plugins of the fixtures installed
// through the same functions as the command line.
//
// Env is the environment the registry reads (the settings variables, PATH) and Src the directory
// that holds the sources of the packages.
type plugLab struct {
	t   *testing.T
	sb  *sandbox
	reg *plugin.Registry
	env map[string]string
	src string
}

// demoSettings is the settings part of the manifest of the plugin "demo": one setting of each
// type.
const demoSettings = `
[[setting]]
id = "owner"
type = "string"
label = "Owner"
description = "Who owns the notes."
pattern = "^[a-z0-9-]+$"
max_len = 12
default = "someone"

[[setting]]
id = "verbose"
type = "bool"
label = "Verbose"

[[setting]]
id = "limit"
type = "int"
label = "Limit"
min = 1
max = 50
default = 10

[[setting]]
id = "mode"
type = "enum"
label = "Mode"
choices = ["fast", "safe"]

[[setting]]
id = "tags"
type = "list"
label = "Tags"
pattern = "^[a-z]+$"
max_items = 3
default = ["x"]
`

// plugManifest builds the manifest of a declarative plugin with one command that runs "tool".
//
// Parameters:
//   - id: plugin id
//   - desc: description
//   - extra: tables after the command
func plugManifest(id, desc, extra string) string {
	title := strings.ToUpper(id[:1]) + id[1:]

	return `api = 1
id = "` + id + `"
version = "1.0.0"
tote = ">=1.0.0, <2"
title = "` + title + `"
description = "` + desc + `"
license = "MIT"
tier = "declarative"
permissions = ["exec:tool"]

[[command]]
id = "go"
label = "Go"
run = ["tool", "{x}"]
output = "pager"

[[command.field]]
id = "x"
type = "text"
label = "X"
` + extra
}

// openUp gives the owner write bit back to every directory below a root, so that the temporary
// directory of a test can be removed after read-only trees were installed in it.
//
// Parameters:
//   - root: directory to open up
func openUp(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o700)
		}

		return nil
	})
}

// newPlugLab opens a registry over the paths of a sandbox, with the plugin that ships with tote
// and an empty PATH, and sets it as the plugin host of the sandbox.
//
// Parameters:
//   - t: running test
//   - sb: sandbox whose paths the registry uses
func newPlugLab(t *testing.T, sb *sandbox) *plugLab {
	t.Helper()

	l := &plugLab{t: t, sb: sb, env: map[string]string{}, src: filepath.Join(sb.Dir, "src")}
	l.env["PATH"] = filepath.Join(sb.Dir, "bin")

	t.Cleanup(func() { openUp(sb.Dir) })

	l.reg = l.open(false)
	sb.Opts.Plugins = l.reg

	return l
}

// open opens a registry over the paths of the sandbox.
//
// Parameters:
//   - off: true for a run where every plugin is off
func (l *plugLab) open(off bool) *plugin.Registry {
	return plugin.Open(plugin.Options{
		Paths:     l.sb.Opts.Paths,
		Host:      plugin.NewHost(l.sb.Opts.Version),
		Home:      l.sb.Opts.Home,
		Bundled:   bundled.Set{},
		Now:       l.sb.Opts.Now,
		NoPlugins: off,
		Lookup: func(k string) (string, bool) {
			v, ok := l.env[k]

			return v, ok
		},
	})
}

// source writes the files of a package below the source directory.
//
// Parameters:
//   - id: plugin id
//   - files: files by slash separated path
//
// Returns the directory.
func (l *plugLab) source(id string, files map[string]string) string {
	l.t.Helper()

	dir := filepath.Join(l.src, id)

	for rel, content := range files {
		writeFile(l.t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}

	return dir
}

// install installs a plugin from a source directory with the consent of the user, as the screen
// does, and fails the test on an error.
//
// Parameters:
//   - id: plugin id
//   - manifest: content of plugin.toml
func (l *plugLab) install(id, manifest string) {
	l.t.Helper()

	dir := l.source(id, map[string]string{"plugin.toml": manifest})

	st, err := l.reg.Plan(context.Background(), plugin.InstallRequest{Source: dir})
	if err != nil {
		l.t.Fatalf("Plan(%s): %v", id, err)
	}

	if _, err := st.Commit(plugin.Grant{Prompt: true}); err != nil {
		l.t.Fatalf("Commit(%s): %v", id, err)
	}

	l.edit(id, func(r *plugin.LockRecord) { r.Source = "/srv/plugins/" + id })
}

// edit changes the lock record of an installed plugin.
//
// Parameters:
//   - id: plugin id
//   - fn: change to make
func (l *plugLab) edit(id string, fn func(*plugin.LockRecord)) {
	l.t.Helper()

	lk, err := plugin.OpenLock(l.sb.Opts.Paths.LockFile())
	if err != nil {
		l.t.Fatal(err)
	}

	if err := lk.Update(id, fn); err != nil {
		l.t.Fatal(err)
	}
}

// fixtures installs a plugin in each state: demo (enabled, with one setting of each type), notes
// (disabled), pager (consent needed), jumpy (quarantined), ghost (missing dependency) and torn
// (broken). The plugin gft ships with tote and stays not installed.
func (l *plugLab) fixtures() {
	l.t.Helper()

	l.install("demo", plugManifest("demo", "Shows notes about the work.", demoSettings))
	l.install("notes", plugManifest("notes", "Keeps short notes.", ""))
	l.install("pager", plugManifest("pager", "Reads long output page by page.", ""))
	l.install("jumpy", plugManifest("jumpy", "Jumps between places.", ""))
	l.install("ghost", plugManifest("ghost", "Needs a program that is not here.",
		"\n[[requires]]\nbin = \"ghost-tool\"\n"+
			"hint = \"Install ghost-tool with your package manager.\"\n"))
	l.install("torn", plugManifest("torn", "Its files were changed.", ""))

	if _, err := l.reg.Disable("notes"); err != nil {
		l.t.Fatal(err)
	}

	l.edit("pager", func(r *plugin.LockRecord) { r.ConsentedAt, r.ConsentedBy = "", "" })
	l.edit("jumpy", func(r *plugin.LockRecord) {
		r.Quarantined, r.Failures, r.LastReason, r.LastFailureAt = true, 3, "crash",
			"2026-11-03T09:00:00Z"
	})
	l.edit("torn", func(r *plugin.LockRecord) { r.TreeSHA256 = strings.Repeat("0", 64) })
}

// states returns the state of each installed plugin by id.
func (l *plugLab) states() map[string]string {
	out := map[string]string{}

	for _, p := range l.reg.Load().Plugins() {
		out[p.ID] = string(p.State)
	}

	return out
}
