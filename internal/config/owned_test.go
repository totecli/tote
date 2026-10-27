package config

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// ownedPaths calls every exported accessor of Paths that returns a path and collects the results,
// so that a new accessor is seen by the test without anyone remembering to add it.
//
// Parameters:
//   - t: running test
//   - p: paths to ask
//
// Returns the method names with the paths they returned. A method with a signature this
// function does not know fails the test: whoever adds it decides what purging does with it.
func ownedPaths(t *testing.T, p Paths) map[string]string {
	t.Helper()

	// Not a path of a file that tote keeps.
	skip := map[string]bool{"Owned": true, "RuntimeDir": true}
	tree := strings.Repeat("a", pluginTreeLen)
	out := map[string]string{}
	v := reflect.ValueOf(p)

	for i := 0; i < v.NumMethod(); i++ {
		name := v.Type().Method(i).Name
		if skip[name] {
			continue
		}

		m := v.Method(i)
		sig := m.Type()

		var res []reflect.Value

		switch {
		case sig.NumIn() == 0 && sig.NumOut() == 1:
			res = m.Call(nil)
		case sig.NumIn() == 1 && sig.NumOut() == 2:
			res = m.Call([]reflect.Value{reflect.ValueOf("x1")})
		case sig.NumIn() == 2 && sig.NumOut() == 2:
			res = m.Call([]reflect.Value{reflect.ValueOf("x1"), reflect.ValueOf(tree)})
		default:
			t.Fatalf("Paths.%s has a signature that the test does not know: decide what a purge "+
				"does with it", name)
		}

		if len(res) == 2 && !res[1].IsNil() {
			t.Fatalf("Paths.%s(\"x1\") failed: %v", name, res[1].Interface())
		}

		out[name] = res[0].String()
	}

	return out
}

// TestOwnedCoversEveryAccessor checks that each path that a Paths method gives is inside an entry
// of Owned, so that "tote uninstall --purge" cannot leave a file of tote behind, and that every
// entry of Owned belongs to a directory of Paths.
//
// Parameters:
//   - t: test to report to
func TestOwnedCoversEveryAccessor(t *testing.T) {
	p := Paths{Config: "/c/tote", Data: "/d/tote", State: "/s/tote", Cache: "/k/tote"}
	paths := ownedPaths(t, p)

	if len(paths) < 15 {
		t.Fatalf("only %d accessors found: %v", len(paths), paths)
	}

	for name, got := range paths {
		covered := false

		for _, d := range p.Owned() {
			rel, err := filepath.Rel(d.Dir, got)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}

			first, _, _ := strings.Cut(rel, string(filepath.Separator))
			if slices.Contains(d.Entries, first) {
				covered = true
			}
		}

		if !covered {
			t.Errorf("Paths.%s = %s is not covered by Paths.Owned", name, got)
		}
	}

	dirs := []string{p.Config, p.Data, p.State, p.Cache}
	for i, d := range p.Owned() {
		if d.Dir != dirs[i] {
			t.Errorf("Owned()[%d].Dir = %s, want %s", i, d.Dir, dirs[i])
		}

		if len(d.Entries) == 0 {
			t.Errorf("no entry for %s", d.Dir)
		}
	}
}

// TestOwnedList pins the names purged in each directory, among them the lock files, the files of
// the plugins and the names kept for the planned releases, and checks that nothing else is there,
// in particular no folder of the user's such as docs.
//
// Parameters:
//   - t: test to report to
func TestOwnedList(t *testing.T) {
	p := Paths{Config: "/c/tote", Data: "/d/tote", State: "/s/tote", Cache: "/k/tote"}

	want := map[string][]string{
		"/c/tote": {
			"config.toml", "config.toml.lock", "connections.toml", "connections.toml.lock",
			"repos.toml", "repos.toml.lock", "aliases.toml", "aliases.toml.lock",
			"funcs.toml", "funcs.toml.lock", "snippets.toml", "snippets.toml.lock",
			"tunnels.toml", "tunnels.toml.lock", "ports.toml", "ports.toml.lock", "plugins",
		},
		"/d/tote": {"plugins", "bin"},
		"/s/tote": {
			"install.json", "plugins.lock", "plugins.lock.lock", "plugin-procs", "plugins",
			"plugins-host",
		},
		"/k/tote": {"update-check.json", "plugins"},
	}

	for _, d := range p.Owned() {
		got := slices.Clone(d.Entries)
		slices.Sort(got)

		w := slices.Clone(want[d.Dir])
		slices.Sort(w)

		if !slices.Equal(got, w) {
			t.Errorf("%s: entries %v, want %v", d.Dir, got, w)
		}

		if slices.Contains(d.Entries, "docs") {
			t.Errorf("%s: docs is the user's folder, not tote's", d.Dir)
		}
	}
}
