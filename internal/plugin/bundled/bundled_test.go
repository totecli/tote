package bundled_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/plugin"
	"github.com/totecli/tote/internal/plugin/bundled"
)

// registry opens a registry over a sandbox, with the bundled set.
//
// Parameters:
//   - t: test to report to
//   - set: bundled set to use
func registry(t *testing.T, set plugin.BundledSet) *plugin.Registry {
	t.Helper()

	root := t.TempDir()
	t.Setenv("TMPDIR", root)

	env := map[string]string{
		"PATH":            filepath.Join(root, "bin"),
		"TOTE_CONFIG_DIR": filepath.Join(root, "config"),
		"TOTE_DATA_DIR":   filepath.Join(root, "data"),
		"TOTE_STATE_DIR":  filepath.Join(root, "state"),
		"TOTE_CACHE_DIR":  filepath.Join(root, "cache"),
	}

	paths, err := config.Resolve(func(k string) string { return env[k] }, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}

	host := plugin.NewHost("1.0.0")
	host.OS, host.Arch = "linux", "amd64"

	return plugin.Open(plugin.Options{
		Paths: paths, Host: host, Bundled: set,
		Home: filepath.Join(root, "home"), Cwd: root,
		Lookup: func(k string) (string, bool) { v, ok := env[k]; return v, ok },
	})
}

// TestBundledPins is the gate of the bundled plugins: each one passes plugin check --strict as
// the embedded files install, and its version, tree hash and surface hash are the ones pinned in
// pins.toml. A file, a version or a command cannot change without a visible edit of the pin.
//
// Parameters:
//   - t: test to report to
func TestBundledPins(t *testing.T) {
	set := bundled.Set{}
	r := registry(t, set)

	pins, err := bundled.Pins()
	if err != nil {
		t.Fatal(err)
	}

	if len(pins) == 0 {
		t.Fatal("no bundled plugin")
	}

	for _, pin := range pins {
		t.Run(pin.ID, func(t *testing.T) {
			res, err := r.Check(context.Background(), plugin.CheckRequest{
				Target: pin.ID,
				Strict: true,
			})
			if err != nil {
				t.Fatal(err)
			}

			if res.Kind != plugin.TargetBundled {
				t.Errorf("kind %q", res.Kind)
			}

			// A program that is not installed on the machine that runs the test is the machine's
			// finding, not the package's: it is listed and never fails the check.
			for _, f := range res.Findings {
				if f.Scope != plugin.ScopeEnv {
					t.Errorf("finding %s %s %s %v", f.Scope, f.Code, f.Path, f.Args)
				}
			}

			if res.Failed() {
				t.Error("plugin check --strict fails")
			}

			if res.Version != pin.Version || res.TreeSHA256 != pin.Tree ||
				res.SurfaceSHA256 != pin.Surface {
				t.Errorf("the pin of %s is out of date; pins.toml should read\n"+
					"version = %q\ntree_sha256 = %q\nsurface_sha256 = %q", pin.ID, res.Version,
					res.TreeSHA256, res.SurfaceSHA256)
			}
		})
	}
}

// TestBundledIDs checks that the directories below plugins/ and the pins name the same plugins,
// so that a plugin cannot be embedded without a pin or pinned without files, and that no Go edit
// is needed to add one: the ids are discovered from the directory, never listed here.
//
// Parameters:
//   - t: test to report to
func TestBundledIDs(t *testing.T) {
	set := bundled.Set{}
	ids := set.IDs()

	entries, err := os.ReadDir("plugins")
	if err != nil {
		t.Fatal(err)
	}

	var dirs []string

	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("plugins/%s is not a directory", e.Name())

			continue
		}

		dirs = append(dirs, e.Name())
	}

	if len(dirs) == 0 {
		t.Fatal("no bundled plugin directory")
	}

	if !slices.Equal(dirs, ids) {
		t.Errorf("plugins/ holds %v and pins.toml pins %v", dirs, ids)
	}

	for _, id := range ids {
		fsys, version, tree, surface, ok := set.Lookup(id)
		if !ok || fsys == nil || version == "" || tree == "" || surface == "" {
			t.Fatalf("Lookup(%q) = %v %q %q %q", id, ok, version, tree, surface)
		}

		if _, err := fs.Stat(fsys, "plugin.toml"); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}

	if _, _, _, _, ok := set.Lookup("nope"); ok {
		t.Error("Lookup found an unknown id")
	}

	if len(ids) > 0 {
		if _, _, _, _, ok := set.Lookup("../" + ids[0]); ok {
			t.Error("Lookup accepted a path")
		}
	}

	if !slices.IsSorted(ids) {
		t.Errorf("ids %v are not in alphabetical order, which vendor.sh keeps", ids)
	}
}
