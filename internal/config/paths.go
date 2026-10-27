package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// appName is the directory name appended to every XDG base directory.
const appName = "tote"

// Paths holds the directories used by tote.
//
// Config is the user-authored directory, Data the installed plugins and generated scripts, State
// the install receipt and other machine-written state, Cache the disposable data and Runtime the
// per-invocation directory. Runtime is empty when XDG_RUNTIME_DIR is unset or not absolute: the
// XDG specification gives it no default, so RuntimeDir falls back to a private temporary directory.
type Paths struct {
	Config  string
	Data    string
	State   string
	Cache   string
	Runtime string
}

// xdgBase describes how one directory is resolved from the environment.
//
// The directory is taken from override when that variable is set, else from xdg when it holds an
// absolute path, else from the fallback below the home directory.
type xdgBase struct {
	override string
	xdg      string
	fallback []string
}

// Resolve computes the tote directories from the environment and the home directory.
//
// Parameters:
//   - getenv: environment lookup, usually os.Getenv; tests inject a map based function
//   - home: absolute home directory of the user, used when an XDG variable is missing
//
// Returns the resolved Paths, or an error when home is empty or relative, or when a TOTE_*_DIR
// override is not an absolute path.
func Resolve(getenv func(string) string, home string) (Paths, error) {
	if home == "" || !filepath.IsAbs(home) {
		return Paths{}, fmt.Errorf("home directory %q is not an absolute path", home)
	}

	bases := []xdgBase{
		{"TOTE_CONFIG_DIR", "XDG_CONFIG_HOME", []string{".config"}},
		{"TOTE_DATA_DIR", "XDG_DATA_HOME", []string{".local", "share"}},
		{"TOTE_STATE_DIR", "XDG_STATE_HOME", []string{".local", "state"}},
		{"TOTE_CACHE_DIR", "XDG_CACHE_HOME", []string{".cache"}},
	}

	dirs := make([]string, len(bases))
	for i, b := range bases {
		dir, err := resolveOne(getenv, home, b)
		if err != nil {
			return Paths{}, err
		}
		dirs[i] = dir
	}

	p := Paths{Config: dirs[0], Data: dirs[1], State: dirs[2], Cache: dirs[3]}
	if rt := getenv("XDG_RUNTIME_DIR"); rt != "" && filepath.IsAbs(rt) {
		p.Runtime = filepath.Join(rt, appName)
	}

	return p, nil
}

// resolveOne applies the override, XDG and home fallback rules to one base directory.
//
// Parameters:
//   - getenv: environment lookup
//   - home: validated absolute home directory
//   - b: names of the variables and the fallback path below home
//
// Returns the final directory, or an error when the override is set but not absolute.
func resolveOne(getenv func(string) string, home string, b xdgBase) (string, error) {
	if v := getenv(b.override); v != "" {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("%s=%q is not an absolute path", b.override, v)
		}

		return filepath.Clean(v), nil
	}

	// The XDG specification says relative values are invalid and must be ignored.
	if v := getenv(b.xdg); v != "" && filepath.IsAbs(v) {
		return filepath.Join(v, appName), nil
	}

	parts := append([]string{home}, b.fallback...)
	parts = append(parts, appName)

	return filepath.Join(parts...), nil
}

// Default resolves the tote directories from the real environment and the user home directory.
//
// Returns the resolved Paths, or an error when the home directory cannot be determined.
func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find home directory: %w", err)
	}

	return Resolve(os.Getenv, home)
}

// RuntimeDir returns a private directory for per-invocation files, creating it when needed.
//
// When Runtime is empty a fresh 0700 directory is created with os.MkdirTemp under os.TempDir. When
// it is set, the directory is created with mode 0700 if missing, then checked: it must be a real
// directory (not a symbolic link), owned by the current user and not accessible by group or others.
//
// Returns the directory path, or an error when it cannot be created or fails the checks.
func (p Paths) RuntimeDir() (string, error) {
	if p.Runtime == "" {
		dir, err := os.MkdirTemp("", appName+"-")
		if err != nil {
			return "", fmt.Errorf("create runtime directory: %w", err)
		}

		return dir, nil
	}

	if err := os.MkdirAll(p.Runtime, 0o700); err != nil {
		return "", fmt.Errorf("create runtime directory: %w", err)
	}

	if err := checkPrivateDir(p.Runtime); err != nil {
		return "", err
	}

	return p.Runtime, nil
}

// checkPrivateDir verifies that dir is a directory owned by the current user with mode 0700.
//
// Parameters:
//   - dir: path to inspect without following a final symbolic link
//
// Returns nil when the directory is acceptable, else an error describing the problem.
func checkPrivateDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect runtime directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("runtime path %s is not a directory", dir)
	}

	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("runtime directory %s is owned by uid %d, not by the current user",
			dir, st.Uid)
	}

	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return errors.New("runtime directory " + dir + " is accessible by group or others (" +
			perm.String() + "), expected drwx------")
	}

	return nil
}

// ConfigFile returns the path of config.toml.
func (p Paths) ConfigFile() string { return filepath.Join(p.Config, "config.toml") }

// ConnectionsFile returns the path of connections.toml.
func (p Paths) ConnectionsFile() string { return filepath.Join(p.Config, "connections.toml") }

// ReposFile returns the path of repos.toml.
func (p Paths) ReposFile() string { return filepath.Join(p.Config, "repos.toml") }

// AliasesFile returns the path of aliases.toml.
func (p Paths) AliasesFile() string { return filepath.Join(p.Config, "aliases.toml") }

// FuncsFile returns the path of funcs.toml.
func (p Paths) FuncsFile() string { return filepath.Join(p.Config, "funcs.toml") }

// PluginsDir returns the directory holding the installed plugins.
func (p Paths) PluginsDir() string { return filepath.Join(p.Data, "plugins") }

// BinDir returns the directory holding the scripts created by tote.
func (p Paths) BinDir() string { return filepath.Join(p.Data, "bin") }

// ReceiptFile returns the path of the install receipt.
func (p Paths) ReceiptFile() string { return filepath.Join(p.State, "install.json") }
