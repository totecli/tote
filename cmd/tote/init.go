package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// records lists the records of a data file without ever failing.
//
// A file that cannot be read or parsed gives no record and a one line comment for the shell, so
// that a typo in a data file never breaks the start of a terminal.
//
// Parameters:
//   - a: running app
//   - path: data file
//   - open: opens the collection of the kind
//   - notes: comment lines for the shell, to which a line is added on failure
//
// Returns the records in file order.
func records[T store.Record](
	a *app, path string, open func(string) (*store.Collection[T], error), notes *[]string,
) []T {
	c, err := open(path)
	if err == nil {
		err = c.Err()
	}

	if err != nil {
		reason := sanitize(err.Error())
		*notes = append(*notes, a.t("cli_init_skipped", filepath.Base(path), reason))

		return nil
	}

	return c.List()
}

// extraNames lists the extra function names that the settings ask for.
//
// Parameters:
//   - s: settings of config.toml
//
// Returns shell.extra_names, with mn added when shell.compat_mn is on.
func extraNames(s config.Settings) []string {
	extra := slices.Clone(s.Shell.ExtraNames)
	if s.Shell.CompatMN && !slices.Contains(extra, "mn") {
		extra = append(extra, "mn")
	}

	return extra
}

// loadDefs reads every data file for the definitions part of the init output.
//
// Returns the definitions, the comment lines to append to the output and an error when the
// directories cannot be resolved.
func (a *app) loadDefs() (shell.Defs, []string, error) {
	paths, err := a.cfgPaths()
	if err != nil {
		return shell.Defs{}, nil, err
	}

	var notes []string

	settings := a.cfg()
	a.warnSettings()

	if a.settingsErr != nil {
		reason := sanitize(a.settingsErr.Error())
		notes = append(notes, a.t("cli_init_skipped", filepath.Base(paths.ConfigFile()), reason))
	}

	defs := shell.Defs{
		Aliases:    records(a, paths.AliasesFile(), store.OpenAliases, &notes),
		Repos:      records(a, paths.ReposFile(), store.OpenRepos, &notes),
		Funcs:      records(a, paths.FuncsFile(), store.OpenFuncs, &notes),
		BinDir:     paths.BinDir(),
		ExtraNames: extraNames(settings),
	}

	return defs, notes, nil
}

// runInit implements "tote init [bash|zsh] [--defs]".
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the exit code of the verb.
func (a *app) runInit(args []string) int {
	o, err := parseOptions(args, []opt{{long: "defs"}})
	if err == nil {
		err = wantArgs(o.pos, 0, 1)
	}

	if err != nil {
		return a.usageErr(err, "init")
	}

	if o.help {
		return a.verbHelp("init")
	}

	kind, rc := a.shellKind(o.pos)
	if kind == "" {
		return rc
	}

	defs, notes, err := a.loadDefs()
	if err != nil {
		return a.fail(err)
	}

	text := shell.Init(kind, defs)
	if o.set["defs"] {
		text = shell.Definitions(kind, defs)
	}

	fmt.Fprint(a.Stdout, text)

	if len(notes) > 0 {
		fmt.Fprintln(a.Stdout, strings.Join(notes, "\n"))
	}

	return exitOK
}

// shellKind picks the shell to write for, from the argument or from $SHELL.
//
// Parameters:
//   - pos: positional arguments of init, at most one
//
// Returns the kind, or an empty kind and the exit code to return.
func (a *app) shellKind(pos []string) (shell.Kind, int) {
	var (
		kind shell.Kind
		err  error
	)

	if len(pos) == 1 {
		kind, err = shell.ParseKind(pos[0])
	} else {
		kind, err = shell.DetectKind(a.Getenv("SHELL"))
	}

	if err == nil {
		return kind, exitOK
	}

	name := ""

	switch {
	case len(pos) == 1:
		name = pos[0]
	case a.Getenv("SHELL") != "":
		name = strings.TrimPrefix(filepath.Base(a.Getenv("SHELL")), "-")
	default:
		missing := &optError{Key: "cli_missing_argument", Args: []any{"arg_shell"}}

		return "", a.usageErr(missing, "init")
	}

	a.failMsg(exitUsage, "cli_unsupported_shell", name)
	a.say("cli_try_help", "tote help init")

	return "", exitUsage
}
