package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/ui"
	"github.com/totecli/tote/internal/update"
)

// fixture is a healthy installation in a temporary directory, which the tests then break.
//
// home is the home directory, vars the environment, tools the programs LookPath finds and env
// what the checks read. Nothing of the machine of the person who runs the tests is looked at.
type fixture struct {
	t     *testing.T
	home  string
	vars  map[string]string
	tools map[string]string
	env   diagEnv
}

// newFixture builds a healthy installation: the four directories with mode 0700, no data file,
// the wrapper active, the programs found, a UTF-8 terminal of 120 columns by 40 rows and a
// private hand-off directory.
//
// Parameters:
//   - t: running test
func newFixture(t *testing.T) *fixture {
	t.Helper()

	// The app resolves the executable, so the home directory must be a real path: on macOS the
	// temporary directory is below /var, a link to /private/var, and "~" would not match.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	f := &fixture{t: t, home: filepath.Join(root, "home")}

	for _, d := range []string{
		".config/tote", ".local/share/tote", ".local/state/tote", ".cache/tote", "run", "tmp",
		"bin",
	} {
		f.mkdir(d, 0o700)
	}

	f.tools = map[string]string{}

	for _, n := range []string{"tote", "ssh", "git"} {
		f.write("bin/"+n, "#!/bin/sh\n", 0o755)
		f.tools[n] = filepath.Join(f.home, "bin", n)
	}

	f.vars = map[string]string{
		"TERM":            "xterm-256color",
		"LANG":            "en_US.UTF-8",
		"SHELL":           "/bin/bash",
		"TOTE_SHELL":      "bash",
		"XDG_RUNTIME_DIR": filepath.Join(f.home, "run"),
		"TMPDIR":          filepath.Join(f.home, "tmp"),
	}

	f.env = diagEnv{
		Getenv:      func(k string) string { return f.vars[k] },
		Home:        f.home,
		Version:     "1.2.3",
		GOOS:        "linux",
		GOARCH:      "amd64",
		Exe:         f.tools["tote"],
		Now:         time.Now(),
		UID:         os.Getuid(),
		Keys:        func() ([]update.PublicKey, error) { return []update.PublicKey{{}}, nil },
		LookPath:    f.lookPath,
		Marker:      filepath.Join(f.home, "no-marker"),
		Term:        ui.Terminal{Color: "truecolor", UTF8: true},
		TmuxClip:    func() string { return "" },
		Interactive: true,
		Width:       120,
		Height:      40,
		SizeOK:      true,
	}

	return f
}

// lookPath finds one of the programs of the fixture.
//
// Parameters:
//   - name: program name
func (f *fixture) lookPath(name string) (string, error) {
	if p, ok := f.tools[name]; ok {
		return p, nil
	}

	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// path joins a path to the home directory.
//
// Parameters:
//   - rel: path below the home directory
func (f *fixture) path(rel string) string {
	return filepath.Join(f.home, rel)
}

// mkdir creates a directory with an exact mode, whatever the umask is.
//
// Parameters:
//   - rel: path below the home directory
//   - mode: permission bits
func (f *fixture) mkdir(rel string, mode os.FileMode) {
	f.t.Helper()

	p := f.path(rel)
	if err := os.MkdirAll(p, 0o700); err != nil {
		f.t.Fatal(err)
	}

	if err := os.Chmod(p, mode); err != nil {
		f.t.Fatal(err)
	}
}

// write creates a file with an exact mode, and its directory when needed.
//
// Parameters:
//   - rel: path below the home directory
//   - text: content
//   - mode: permission bits
func (f *fixture) write(rel, text string, mode os.FileMode) {
	f.t.Helper()

	p := f.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		f.t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		f.t.Fatal(err)
	}

	if err := os.Chmod(p, mode); err != nil {
		f.t.Fatal(err)
	}
}

// conf writes a file of the config directory with mode 0600.
//
// Parameters:
//   - name: file name such as connections.toml
//   - text: content
func (f *fixture) conf(name, text string) {
	f.t.Helper()
	f.write(".config/tote/"+name, text, 0o600)
}

// age sets the modification time of a file in the past.
//
// Parameters:
//   - rel: path below the home directory
//   - d: how long ago
func (f *fixture) age(rel string, d time.Duration) {
	f.t.Helper()

	at := f.env.Now.Add(-d)
	if err := os.Chtimes(f.path(rel), at, at); err != nil {
		f.t.Fatal(err)
	}
}

// run runs every check.
func (f *fixture) run() []finding {
	f.t.Helper()

	return diagnose(f.env)
}

// get returns the finding of a check.
//
// Parameters:
//   - found: findings of a run
//   - id: stable name of the check
func get(t *testing.T, found []finding, id string) finding {
	t.Helper()

	for _, x := range found {
		if x.ID == id {
			return x
		}
	}

	t.Fatalf("no check named %q in %v", id, ids(found))

	return finding{}
}

// ids lists the names of the checks of a run.
//
// Parameters:
//   - found: findings of a run
func ids(found []finding) []string {
	out := make([]string, len(found))

	for i, x := range found {
		out[i] = x.ID
	}

	return out
}

// text flattens a finding into the keys and values it holds, for a search in a test.
//
// Parameters:
//   - f: finding
func text(f finding) string {
	parts := []string{flat(f.Msg), flat(f.Fix)}

	for _, d := range f.Details {
		parts = append(parts, flat(d))
	}

	return strings.Join(parts, "\n")
}

// flat writes a message as its key followed by its values.
//
// Parameters:
//   - m: message
func flat(m msg) string {
	out := []string{m.Key}

	for _, a := range m.Args {
		if n, ok := a.(msg); ok {
			out = append(out, flat(n))
		} else {
			out = append(out, fmt.Sprint(a))
		}
	}

	return strings.Join(out, " ")
}
