package main

import (
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// connClash refuses a connection whose name or alias is already a name or an alias of another
// connection, because a handle must designate one host only. The rule lives in store.SharedName
// so that the interface applies the very same one.
//
// Parameters:
//   - a: running app, unused
//   - c: loaded connections
//   - rec: connection about to be added
//
// Returns nil when no name is shared, otherwise the error naming the shared word and its owner.
// A name or an alias that tote keeps for its command line is refused first. A name equal to the
// name of an existing connection is left to the store, which reports it as an existing
// connection.
func connClash(_ *app, c *store.Collection[store.Connection], rec store.Connection) *optError {
	for _, w := range append([]string{rec.Name}, rec.Aliases...) {
		if store.ReservedRecordName(w) {
			return &optError{Key: "cli_reserved_name", Args: []any{sanitize(w)}}
		}
	}

	if word, owner, found := store.SharedName(c.List(), "", rec); found {
		return nameTaken(word, "kind_connection", owner)
	}

	return nil
}

// nameTaken builds the error of a name that another record already holds.
//
// Parameters:
//   - name: the shared word
//   - label: catalog key of the kind of the owner
//   - owner: name of the record that holds it
func nameTaken(name, label, owner string) *optError {
	return &optError{Key: "cli_name_taken", Args: []any{sanitize(name), label, sanitize(owner)}}
}

// holds tells whether a collection of another kind has a record of a name.
//
// Parameters:
//   - a: running app
//   - s: description of the other kind
//   - name: name to look for
//
// Returns false when the file cannot be read, because the clash cannot be proved.
func holds[T store.Record](a *app, s spec[T], name string) bool {
	paths, err := a.cfgPaths()
	if err != nil {
		return false
	}

	c, err := s.open(s.file(paths))
	if err != nil || c.Err() != nil {
		return false
	}

	_, ok := c.Get(name)

	return ok
}

// shellClash refuses a name that the shell cannot give to a new alias, repository or function:
// a reserved word, or a name that another of the three kinds already uses. The generated code
// would skip such a record silently. An alias or a repository also may not take a word of the
// command line of tote, see store.ReservedRecordName.
//
// Parameters:
//   - a: running app
//   - c: collection of the kind being added to, unused
//   - rec: record about to be added
//
// Returns nil when the name is free, otherwise the error naming the problem.
func shellClash[T store.Record](a *app, _ *store.Collection[T], rec T) *optError {
	name := rec.Key()

	if shell.Reserved(name) {
		return &optError{Key: "cli_reserved_name", Args: []any{sanitize(name)}}
	}

	switch any(rec).(type) {
	case store.Alias:
		if store.ReservedRecordName(name) {
			return &optError{Key: "cli_reserved_name", Args: []any{sanitize(name)}}
		}

		return firstTaken(a, name, holdsRepo, holdsFunc)
	case store.Repo:
		if store.ReservedRecordName(name) {
			return &optError{Key: "cli_reserved_name", Args: []any{sanitize(name)}}
		}

		return firstTaken(a, name, holdsAlias, holdsFunc)
	}

	return firstTaken(a, name, holdsAlias, holdsRepo)
}

// repoCheck refuses a repository whose name clashes or whose path depends on the current
// directory.
//
// Parameters:
//   - a: running app
//   - c: loaded repositories
//   - rec: repository about to be added
//
// Returns nil when the repository may be stored.
func repoCheck(a *app, c *store.Collection[store.Repo], rec store.Repo) *optError {
	if !shell.AbsOrHome(rec.Path) {
		return &optError{Key: "cli_path_not_absolute"}
	}

	return shellClash(a, c, rec)
}

// funcCheck refuses a function whose name clashes or whose source script would be searched in
// PATH and in the current directory by the shell.
//
// Parameters:
//   - a: running app
//   - c: loaded functions
//   - rec: function about to be added
//
// Returns nil when the function may be stored.
func funcCheck(a *app, c *store.Collection[store.Func], rec store.Func) *optError {
	if rec.Kind == store.KindSource && !shell.ValidScript(rec.Script) {
		return &optError{Key: "cli_script_not_absolute"}
	}

	return shellClash(a, c, rec)
}

// holdsAlias tells whether an alias has a name.
//
// Parameters:
//   - a: running app
//   - name: name to look for
func holdsAlias(a *app, name string) (string, bool) {
	return "kind_alias", holds(a, aliasSpec(), name)
}

// holdsRepo tells whether a repository has a name.
//
// Parameters:
//   - a: running app
//   - name: name to look for
func holdsRepo(a *app, name string) (string, bool) {
	return "kind_repo", holds(a, repoSpec(), name)
}

// holdsFunc tells whether a function has a name.
//
// Parameters:
//   - a: running app
//   - name: name to look for
func holdsFunc(a *app, name string) (string, bool) {
	return "kind_func", holds(a, funcSpec(), name)
}

// firstTaken asks each lookup in turn whether it holds a name.
//
// Parameters:
//   - a: running app
//   - name: name to look for
//   - lookups: functions that return the catalog key of their kind and whether they hold name
//
// Returns the error for the first kind that holds the name, or nil.
func firstTaken(a *app, name string, lookups ...func(*app, string) (string, bool)) *optError {
	for _, look := range lookups {
		if label, ok := look(a, name); ok {
			return nameTaken(name, label, name)
		}
	}

	return nil
}
