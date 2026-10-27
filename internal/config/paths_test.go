package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// envMap returns a getenv function backed by a map.
//
// Parameters:
//   - m: variables visible to the returned function
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestResolve checks XDG precedence, ignored values and TOTE_*_DIR overrides.
//
// Parameters:
//   - t: test to report to
func TestResolve(t *testing.T) {
	const home = "/home/u"

	tests := []struct {
		name string
		env  map[string]string
		want Paths
	}{
		{
			name: "defaults from home",
			want: Paths{
				Config: "/home/u/.config/tote",
				Data:   "/home/u/.local/share/tote",
				State:  "/home/u/.local/state/tote",
				Cache:  "/home/u/.cache/tote",
			},
		},
		{
			name: "xdg variables win over home",
			env: map[string]string{
				"XDG_CONFIG_HOME": "/x/c", "XDG_DATA_HOME": "/x/d",
				"XDG_STATE_HOME": "/x/s", "XDG_CACHE_HOME": "/x/k",
				"XDG_RUNTIME_DIR": "/run/user/1000",
			},
			want: Paths{
				Config: "/x/c/tote", Data: "/x/d/tote", State: "/x/s/tote", Cache: "/x/k/tote",
				Runtime: "/run/user/1000/tote",
			},
		},
		{
			name: "empty and relative xdg variables are ignored",
			env: map[string]string{
				"XDG_CONFIG_HOME": "", "XDG_DATA_HOME": "rel/data",
				"XDG_STATE_HOME": "./state", "XDG_CACHE_HOME": "cache",
				"XDG_RUNTIME_DIR": "run",
			},
			want: Paths{
				Config: "/home/u/.config/tote",
				Data:   "/home/u/.local/share/tote",
				State:  "/home/u/.local/state/tote",
				Cache:  "/home/u/.cache/tote",
			},
		},
		{
			name: "tote overrides win over xdg and are not suffixed",
			env: map[string]string{
				"XDG_CONFIG_HOME": "/x/c", "TOTE_CONFIG_DIR": "/t/config/",
				"TOTE_DATA_DIR": "/t/data", "TOTE_STATE_DIR": "/t/state",
				"TOTE_CACHE_DIR": "/t/cache",
			},
			want: Paths{
				Config: "/t/config", Data: "/t/data", State: "/t/state", Cache: "/t/cache",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(envMap(tt.env), home)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestResolveErrors checks the rejection of a bad home and of a relative override.
//
// Parameters:
//   - t: test to report to
func TestResolveErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		home string
		want string
	}{
		{name: "empty home", home: "", want: "not an absolute path"},
		{name: "relative home", home: "home/u", want: "not an absolute path"},
		{
			name: "relative override",
			env:  map[string]string{"TOTE_DATA_DIR": "data"},
			home: "/home/u",
			want: "TOTE_DATA_DIR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(envMap(tt.env), tt.home)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Resolve() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// TestFilePaths checks the standard file and directory names.
//
// Parameters:
//   - t: test to report to
func TestFilePaths(t *testing.T) {
	p := Paths{Config: "/c", Data: "/d", State: "/s", Cache: "/k"}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"config", p.ConfigFile(), "/c/config.toml"},
		{"connections", p.ConnectionsFile(), "/c/connections.toml"},
		{"repos", p.ReposFile(), "/c/repos.toml"},
		{"aliases", p.AliasesFile(), "/c/aliases.toml"},
		{"funcs", p.FuncsFile(), "/c/funcs.toml"},
		{"plugins", p.PluginsDir(), "/d/plugins"},
		{"bin", p.BinDir(), "/d/bin"},
		{"receipt", p.ReceiptFile(), "/s/install.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

// TestDefault checks that Default honours the environment.
//
// Parameters:
//   - t: test to report to
func TestDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{
		"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
		"XDG_RUNTIME_DIR", "TOTE_CONFIG_DIR", "TOTE_DATA_DIR", "TOTE_STATE_DIR", "TOTE_CACHE_DIR",
	} {
		t.Setenv(k, "")
	}

	p, err := Default()
	if err != nil {
		t.Fatalf("Default() error = %v", err)
	}
	if want := filepath.Join(home, ".config", "tote"); p.Config != want {
		t.Errorf("Config = %q, want %q", p.Config, want)
	}
	if p.Runtime != "" {
		t.Errorf("Runtime = %q, want empty", p.Runtime)
	}
}

// TestDefaultNoHome checks that Default fails when no home directory can be found.
//
// Parameters:
//   - t: test to report to
func TestDefaultNoHome(t *testing.T) {
	t.Setenv("HOME", "")

	if _, err := Default(); err == nil {
		t.Error("Default() error = nil, want an error")
	}
}

// TestRuntimeDirTemp checks the fallback to a private temporary directory.
//
// Parameters:
//   - t: test to report to
func TestRuntimeDirTemp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	dir, err := Paths{}.RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir() error = %v", err)
	}
	if filepath.Dir(dir) != tmp {
		t.Errorf("RuntimeDir() = %q, want a child of %q", dir, tmp)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("mode = %v, want 0700", perm)
	}
}

// TestRuntimeDirTempFails checks the error when the temporary directory is unusable.
//
// Parameters:
//   - t: test to report to
func TestRuntimeDirTempFails(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

	if _, err := (Paths{}).RuntimeDir(); err == nil {
		t.Error("RuntimeDir() error = nil, want an error")
	}
}

// TestRuntimeDirXDG checks creation, reuse and rejection of XDG runtime directories.
//
// Parameters:
//   - t: test to report to
func TestRuntimeDirXDG(t *testing.T) {
	t.Run("created with mode 0700", checkRuntimeCreated)
	t.Run("existing private directory is accepted", checkRuntimeExisting)

	for _, mode := range []os.FileMode{0o770, 0o707, 0o777, 0o750} {
		t.Run("rejects mode "+mode.String(), func(t *testing.T) { checkRuntimeMode(t, mode) })
	}

	t.Run("rejects a regular file", checkRuntimeFile)
	t.Run("rejects a symbolic link", checkRuntimeSymlink)
	t.Run("fails when the parent is a file", checkRuntimeParentFile)
}

// checkRuntimeCreated checks that a missing runtime directory is created with mode 0700.
//
// Parameters:
//   - t: test to report to
func checkRuntimeCreated(t *testing.T) {
	rt := filepath.Join(t.TempDir(), "run", "tote")

	dir, err := Paths{Runtime: rt}.RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir() error = %v", err)
	}
	if dir != rt {
		t.Errorf("RuntimeDir() = %q, want %q", dir, rt)
	}

	info, err := os.Stat(rt)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("mode = %v, want 0700", perm)
	}
}

// checkRuntimeExisting checks that an existing private runtime directory is accepted.
//
// Parameters:
//   - t: test to report to
func checkRuntimeExisting(t *testing.T) {
	rt := filepath.Join(t.TempDir(), "tote")
	if err := os.Mkdir(rt, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := (Paths{Runtime: rt}).RuntimeDir(); err != nil {
		t.Errorf("RuntimeDir() error = %v", err)
	}
}

// checkRuntimeMode checks that a runtime directory open to group or others is rejected.
//
// Parameters:
//   - t: test to report to
//   - mode: permission bits given to the existing directory
func checkRuntimeMode(t *testing.T, mode os.FileMode) {
	rt := filepath.Join(t.TempDir(), "tote")
	if err := os.Mkdir(rt, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(rt, mode); err != nil {
		t.Fatal(err)
	}

	_, err := Paths{Runtime: rt}.RuntimeDir()
	if err == nil || !strings.Contains(err.Error(), "group or others") {
		t.Errorf("RuntimeDir() error = %v, want a permission error", err)
	}
}

// checkRuntimeFile checks that a regular file in place of the runtime directory is rejected.
//
// Parameters:
//   - t: test to report to
func checkRuntimeFile(t *testing.T) {
	rt := filepath.Join(t.TempDir(), "tote")
	if err := os.WriteFile(rt, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := (Paths{Runtime: rt}).RuntimeDir(); err == nil {
		t.Error("RuntimeDir() error = nil, want an error")
	}
}

// checkRuntimeSymlink checks that a symbolic link in place of the runtime directory is rejected.
//
// Parameters:
//   - t: test to report to
func checkRuntimeSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "tote")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if _, err := (Paths{Runtime: link}).RuntimeDir(); err == nil {
		t.Error("RuntimeDir() error = nil, want an error")
	}
}

// checkRuntimeParentFile checks the failure when the parent of the runtime directory is a file.
//
// Parameters:
//   - t: test to report to
func checkRuntimeParentFile(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := (Paths{Runtime: filepath.Join(parent, "tote")}).RuntimeDir(); err == nil {
		t.Error("RuntimeDir() error = nil, want an error")
	}
}
