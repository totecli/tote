package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Plugin identifier and tree name limits, in bytes.
const (
	pluginIDMin   = 2
	pluginIDMax   = 32
	pluginTreeLen = 32
	pluginIDShown = 40
)

// ErrInvalidPluginID is wrapped by every error about a plugin identifier that breaks the grammar.
var ErrInvalidPluginID = errors.New("invalid plugin id")

// ErrInvalidPluginTree is wrapped by every error about a tree directory name that is not 32 hex
// digits.
var ErrInvalidPluginTree = errors.New("invalid plugin tree name")

// PluginConfig is one [plugins.<id>] table of config.toml.
//
// Enabled is nil when the key is absent, which means enabled: only an explicit false disables the
// plugin. The kill switch kept in the lock file is not part of config.toml and is not read here.
type PluginConfig struct {
	Enabled *bool `toml:"enabled"`
}

// pluginIDError ties a rejected plugin identifier to the reason, for Validate and for the reason of
// the warning that LoadSettingsWarn gives.
type pluginIDError struct {
	id  string
	err error
}

// Error implements error.
func (e *pluginIDError) Error() string {
	return "plugins." + quoteShort(e.id) + ": " + e.err.Error()
}

// Unwrap returns the grammar error, which wraps ErrInvalidPluginID.
func (e *pluginIDError) Unwrap() error { return e.err }

// quoteShort quotes s for an error message, cutting a long value so that a hostile identifier does
// not flood the output. The quoting escapes control and bidirectional characters.
//
// Parameters:
//   - s: text to show, possibly hostile
func quoteShort(s string) string {
	if len(s) > pluginIDShown {
		return strconv.Quote(s[:pluginIDShown]) + "..."
	}

	return strconv.Quote(s)
}

// ValidPluginID checks a plugin identifier against the grammar of API 1.
//
// The grammar is ^[a-z0-9][a-z0-9-]*$ with 2 to 32 bytes. It is the only definition of the
// grammar: internal/plugin imports it, because this package cannot import internal/plugin. The
// reserved names (core verbs, schema record fields, official ids) are a separate rule that needs
// the list of CLI verbs, so it lives in the plugin manifest validator, not here: a [plugins.<id>]
// table for a reserved but valid id is accepted by config.toml.
//
// Parameters:
//   - id: candidate identifier, never trimmed or normalised
//
// Returns nil when id is acceptable, else an error wrapping ErrInvalidPluginID and saying why.
func ValidPluginID(id string) error {
	if len(id) < pluginIDMin || len(id) > pluginIDMax {
		return fmt.Errorf("%w %s: length is %d bytes, must be %d to %d", ErrInvalidPluginID,
			quoteShort(id), len(id), pluginIDMin, pluginIDMax)
	}

	for i := 0; i < len(id); i++ {
		c := id[i]
		digit := c >= '0' && c <= '9'
		lower := c >= 'a' && c <= 'z'
		if !digit && !lower && (c != '-' || i == 0) {
			return fmt.Errorf("%w %s: byte %d is not allowed, expected [a-z0-9][a-z0-9-]*",
				ErrInvalidPluginID, quoteShort(id), i)
		}
	}

	return nil
}

// validTreeName checks the name of a tree directory: the first 32 hex digits of a tree hash.
//
// Parameters:
//   - tree: candidate directory name
//
// Returns nil for exactly 32 lowercase hexadecimal digits, else an error wrapping
// ErrInvalidPluginTree.
func validTreeName(tree string) error {
	if len(tree) != pluginTreeLen {
		return fmt.Errorf("%w %s: length is %d bytes, must be %d", ErrInvalidPluginTree,
			quoteShort(tree), len(tree), pluginTreeLen)
	}

	for i := 0; i < len(tree); i++ {
		c := tree[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("%w %s: byte %d is not a lowercase hex digit", ErrInvalidPluginTree,
				quoteShort(tree), i)
		}
	}

	return nil
}

// pluginJoin checks a plugin identifier and joins it below a base directory.
//
// Parameters:
//   - id: plugin identifier, checked with ValidPluginID before any path is built
//   - base: directory that holds the plugin entry
//   - rest: elements appended after the identifier
//
// Returns the path, or the error of ValidPluginID.
func pluginJoin(id, base string, rest ...string) (string, error) {
	if err := ValidPluginID(id); err != nil {
		return "", err
	}

	return filepath.Join(append([]string{base, id}, rest...)...), nil
}

// PluginDir returns the directory that holds every installed tree of one plugin.
//
// Parameters:
//   - id: plugin identifier, checked with ValidPluginID
//
// Returns <Data>/plugins/<id>, or an error wrapping ErrInvalidPluginID. No file system call is
// made.
func (p Paths) PluginDir(id string) (string, error) {
	return pluginJoin(id, p.PluginsDir())
}

// PluginTree returns the directory of one verified tree, the value of TOTE_PLUGIN_DIR.
//
// Parameters:
//   - id: plugin identifier, checked with ValidPluginID
//   - tree: first 32 lowercase hex digits of the tree hash
//
// Returns <Data>/plugins/<id>/<tree>, or an error wrapping ErrInvalidPluginID or
// ErrInvalidPluginTree.
func (p Paths) PluginTree(id, tree string) (string, error) {
	if err := validTreeName(tree); err != nil {
		return "", err
	}

	return pluginJoin(id, p.PluginsDir(), tree)
}

// PluginSettingsFile returns the file holding the setting values of one plugin.
//
// Parameters:
//   - id: plugin identifier, checked with ValidPluginID
//
// Returns <Config>/plugins/<id>.toml, or an error wrapping ErrInvalidPluginID. The file belongs
// to the user and may be a symbolic link into a dotfiles repository.
func (p Paths) PluginSettingsFile(id string) (string, error) {
	path, err := pluginJoin(id, filepath.Join(p.Config, "plugins"))
	if err != nil {
		return "", err
	}

	return path + ".toml", nil
}

// PluginStateDir returns the directory a plugin may write its state to, TOTE_PLUGIN_STATE.
//
// Parameters:
//   - id: plugin identifier, checked with ValidPluginID
//
// Returns <State>/plugins/<id>, or an error wrapping ErrInvalidPluginID. The host never writes
// there, see PluginHostDir.
func (p Paths) PluginStateDir(id string) (string, error) {
	return pluginJoin(id, filepath.Join(p.State, "plugins"))
}

// PluginHostDir returns the directory the host keeps its own files for one plugin in, such as
// last.log.
//
// Parameters:
//   - id: plugin identifier, checked with ValidPluginID
//
// Returns <State>/plugins-host/<id>, or an error wrapping ErrInvalidPluginID. It is a sibling of
// the plugin-writable state directory on purpose: a plugin must not be able to turn a host file
// into a symbolic link.
func (p Paths) PluginHostDir(id string) (string, error) {
	return pluginJoin(id, filepath.Join(p.State, "plugins-host"))
}

// PluginCacheDir returns the disposable directory a plugin may write to, TOTE_PLUGIN_CACHE.
//
// Parameters:
//   - id: plugin identifier, checked with ValidPluginID
//
// Returns <Cache>/plugins/<id>, or an error wrapping ErrInvalidPluginID.
func (p Paths) PluginCacheDir(id string) (string, error) {
	return pluginJoin(id, filepath.Join(p.Cache, "plugins"))
}

// LockFile returns the path of plugins.lock, the record of installed plugins.
func (p Paths) LockFile() string { return filepath.Join(p.State, "plugins.lock") }

// ProcsFile returns the path of plugin-procs, the list of running plugin process groups.
func (p Paths) ProcsFile() string { return filepath.Join(p.State, "plugin-procs") }

// StagingDir returns the directory of installs in progress and of trees waiting for deletion.
//
// The name starts with a dot, so it can never be a plugin identifier.
func (p Paths) StagingDir() string { return filepath.Join(p.PluginsDir(), ".staging") }

// MutexFile returns the lock file that serialises installs, updates and removals.
//
// The name starts with a dot, so it can never be a plugin identifier.
func (p Paths) MutexFile() string { return filepath.Join(p.PluginsDir(), ".mutex") }

// PluginEnabled reports whether config.toml leaves a plugin enabled.
//
// Parameters:
//   - id: plugin identifier; an unknown or uninstalled id has no table and counts as enabled
//
// Returns false only for an explicit enabled = false in [plugins.<id>]. This is one input of the
// plugin state; the kill switch in the lock file and the session switch are separate inputs.
func (s Settings) PluginEnabled(id string) bool {
	c := s.Plugins[id]

	return c.Enabled == nil || *c.Enabled
}

// NoPlugins reports whether the session switch TOTE_NO_PLUGINS turns every plugin off.
//
// Parameters:
//   - getenv: environment lookup, usually os.Getenv
//
// Returns true for any non-empty value, including "0": the switch is a kill switch, so an
// ambiguous value fails closed. The --no-plugins flag is read by the command line, not here.
func NoPlugins(getenv func(string) string) bool {
	return getenv("TOTE_NO_PLUGINS") != ""
}

// checkPluginsTable rejects a plugins key that is not a table.
//
// "plugins = false" looks like a global switch, and reading it as a table with no entry would make
// it a switch that does nothing. A silent switch is the worst kind for a security setting, so it
// is one of the few faults that refuse the file.
//
// Parameters:
//   - path: file name for the message
//   - md: metadata of the decode that just succeeded
//   - data: document text, used to find the line
//
// Returns nil when plugins is absent or a table, else a *ParseError at the line of the key.
func checkPluginsTable(path string, md toml.MetaData, data []byte) error {
	kind := md.Type("plugins")
	if kind == "" || kind == "Hash" {
		return nil
	}

	line, col := keyLine(data, []string{"plugins"})

	return &ParseError{
		File: path, Line: line, Column: col,
		Msg: "plugins must be a table of [plugins.<id>] tables, found " + kind,
	}
}
