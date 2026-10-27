package store

import "slices"

const (
	// TableConnection is the array of tables name of the connections file.
	TableConnection = "connection"

	// TableAlias is the array of tables name of the aliases file.
	TableAlias = "alias"

	// TableRepo is the array of tables name of the repositories file.
	TableRepo = "repo"

	// TableFunc is the array of tables name of the functions file.
	TableFunc = "func"
)

const (
	// KindExec marks a function that is an executable found on PATH.
	KindExec = "exec"

	// KindSource marks a function whose script is sourced into the interactive shell.
	KindSource = "source"
)

// Connection is one saved remote host.
//
// Via is the command that opens the session: empty means "ssh", a bare verb receives the
// optionally user-prefixed target as last argument, and a template may use the placeholders
// {user@}, {user}, {host}, {ip} and {name}. User empty means that ssh_config or the command
// decides. Aliases are extra names that select the same record. IP is informational and never
// used to connect. NeedsReview is set by the importer on an ambiguous record: it passes
// validation but the interface must refuse to connect until the user edits it. Every field
// except Name is omitted from the file when it has its zero value.
type Connection struct {
	Name        string   `toml:"name"`
	Via         string   `toml:"via,omitempty"`
	Target      string   `toml:"target,omitempty"`
	User        string   `toml:"user,omitempty"`
	Aliases     []string `toml:"aliases,omitempty"`
	IP          string   `toml:"ip,omitempty"`
	Desc        string   `toml:"desc,omitempty"`
	NeedsReview bool     `toml:"needs_review,omitempty"`
}

// Alias is one shell alias: Name expands to the command line Cmd. Desc is omitted from the file
// when empty.
type Alias struct {
	Name string `toml:"name"`
	Cmd  string `toml:"cmd"`
	Desc string `toml:"desc,omitempty"`
}

// Repo is one saved repository. Path is stored verbatim, a leading "~" included, and is never
// expanded or evaluated by this package. Desc is omitted from the file when empty.
type Repo struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
	Desc string `toml:"desc,omitempty"`
}

// Func is one shell function or executable that the program exposes. Kind is KindExec or
// KindSource, Script is the path of the script, and Managed records that the script belongs to
// the program. In 1.0 the flag is stored and shown and has no other effect; a later release may
// act only on scripts under the bin folder of the data directory. Managed is always written so
// that a hand editor sees the switch. Desc is omitted from the file when empty.
type Func struct {
	Name    string `toml:"name"`
	Desc    string `toml:"desc,omitempty"`
	Kind    string `toml:"kind"`
	Script  string `toml:"script"`
	Managed bool   `toml:"managed"`
}

// Clone returns a copy of the connection that shares no slice with it.
func (c Connection) Clone() Connection {
	c.Aliases = slices.Clone(c.Aliases)

	return c
}

// Key returns the name of the connection.
func (c Connection) Key() string {
	return c.Name
}

// Validate checks every field of the connection and returns the first error found. The error
// wraps ErrInvalidField.
func (c Connection) Validate() error {
	if err := ValidConnectionName(c.Name); err != nil {
		return field("connection name", err)
	}

	found, err := viaPlaceholders(c.Via)
	if err != nil {
		return field("connection via", err)
	}

	if err := validTarget(c.Target, found); err != nil {
		return field("connection target", err)
	}

	if err := validUser(c.User); err != nil {
		return field("connection user", err)
	}

	if err := validAliases(c.Aliases, c.Name); err != nil {
		return field("connection aliases", err)
	}

	if err := validIP(c.IP); err != nil {
		return field("connection ip", err)
	}

	return field("connection desc", ValidDesc(c.Desc))
}

// Key returns the name of the alias.
func (a Alias) Key() string {
	return a.Name
}

// Validate checks every field of the alias and returns the first error found. The error wraps
// ErrInvalidField.
func (a Alias) Validate() error {
	if err := ValidAliasName(a.Name); err != nil {
		return field("alias name", err)
	}

	if err := validLine(a.Cmd); err != nil {
		return field("alias cmd", err)
	}

	return field("alias desc", ValidDesc(a.Desc))
}

// Key returns the name of the repository.
func (r Repo) Key() string {
	return r.Name
}

// Validate checks every field of the repository and returns the first error found. The error
// wraps ErrInvalidField.
func (r Repo) Validate() error {
	if err := ValidRepoName(r.Name); err != nil {
		return field("repo name", err)
	}

	if err := validLine(r.Path); err != nil {
		return field("repo path", err)
	}

	return field("repo desc", ValidDesc(r.Desc))
}

// Key returns the name of the function.
func (f Func) Key() string {
	return f.Name
}

// Validate checks every field of the function and returns the first error found. The error wraps
// ErrInvalidField.
func (f Func) Validate() error {
	if err := ValidFuncName(f.Name); err != nil {
		return field("func name", err)
	}

	if f.Kind != KindExec && f.Kind != KindSource {
		return field("func kind", invalid("must be %q or %q", KindExec, KindSource))
	}

	if err := validLine(f.Script); err != nil {
		return field("func script", err)
	}

	return field("func desc", ValidDesc(f.Desc))
}

// OpenConnections opens the connections collection stored in a file.
//
// Parameters:
//   - path: file of the collection, which may not exist yet
//
// Returns what Open returns for this record kind.
func OpenConnections(path string) (*Collection[Connection], error) {
	return Open[Connection](path, TableConnection)
}

// OpenAliases opens the aliases collection stored in a file.
//
// Parameters:
//   - path: file of the collection, which may not exist yet
//
// Returns what Open returns for this record kind.
func OpenAliases(path string) (*Collection[Alias], error) {
	return Open[Alias](path, TableAlias)
}

// OpenRepos opens the repositories collection stored in a file.
//
// Parameters:
//   - path: file of the collection, which may not exist yet
//
// Returns what Open returns for this record kind.
func OpenRepos(path string) (*Collection[Repo], error) {
	return Open[Repo](path, TableRepo)
}

// OpenFuncs opens the functions collection stored in a file.
//
// Parameters:
//   - path: file of the collection, which may not exist yet
//
// Returns what Open returns for this record kind.
func OpenFuncs(path string) (*Collection[Func], error) {
	return Open[Func](path, TableFunc)
}
