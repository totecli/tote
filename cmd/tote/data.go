package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// descOpt is the --desc option shared by every add verb.
var descOpt = opt{long: "desc", value: true}

// aliasView is the JSON form of an alias.
type aliasView struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
	Desc string `json:"desc"`
}

// repoView is the JSON form of a repository.
type repoView struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Desc string `json:"desc"`
}

// funcView is the JSON form of a function.
type funcView struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Script  string `json:"script"`
	Managed bool   `json:"managed"`
	Desc    string `json:"desc"`
}

// aliasSpec describes the aliases for the generic verbs.
func aliasSpec() spec[store.Alias] {
	return spec[store.Alias]{
		command:  "alias",
		label:    "kind_alias",
		noun:     "aliases",
		heads:    []string{"header_name", "header_command", "header_description"},
		file:     config.Paths.AliasesFile,
		open:     store.OpenAliases,
		cells:    func(r store.Alias) []string { return []string{r.Name, r.Cmd, r.Desc} },
		view:     func(r store.Alias) any { return aliasView{r.Name, r.Cmd, r.Desc} },
		addOpts:  []opt{descOpt},
		build:    buildAlias,
		check:    shellClash[store.Alias],
		cmdWords: 1,
	}
}

// repoSpec describes the repositories for the generic verbs.
func repoSpec() spec[store.Repo] {
	return spec[store.Repo]{
		command: "repos",
		label:   "kind_repo",
		noun:    "repos",
		heads:   []string{"header_name", "header_path", "header_description"},
		file:    config.Paths.ReposFile,
		open:    store.OpenRepos,
		cells:   func(r store.Repo) []string { return []string{r.Name, r.Path, r.Desc} },
		view:    func(r store.Repo) any { return repoView{r.Name, r.Path, r.Desc} },
		addOpts: []opt{descOpt},
		build:   buildRepo,
		check:   repoCheck,
		prepare: underReposRoot,
	}
}

// funcSpec describes the functions for the generic verbs.
func funcSpec() spec[store.Func] {
	return spec[store.Func]{
		command: "funcs",
		label:   "kind_func",
		noun:    "funcs",
		heads: []string{
			"header_name", "header_kind", "header_script", "header_managed",
			"header_description",
		},
		file: config.Paths.FuncsFile,
		open: store.OpenFuncs,
		cells: func(r store.Func) []string {
			return []string{r.Name, r.Kind, r.Script, strconv.FormatBool(r.Managed), r.Desc}
		},
		view: func(r store.Func) any {
			return funcView{r.Name, r.Kind, r.Script, r.Managed, r.Desc}
		},
		addOpts: []opt{
			{long: "script", value: true},
			{long: "kind", value: true},
			descOpt,
			{long: "managed"},
		},
		build: buildFunc,
		check: funcCheck,
	}
}

// buildAlias makes an alias out of "add NAME COMMAND..." arguments.
//
// Parameters:
//   - o: parsed options and positional arguments
//
// Returns the alias, whose command is the positional words joined by spaces, or an *optError.
func buildAlias(o options) (store.Alias, error) {
	if err := wantArgs(o.pos, 2, -1, "arg_name", "arg_command"); err != nil {
		return store.Alias{}, err
	}

	return store.Alias{
		Name: o.pos[0],
		Cmd:  strings.Join(o.pos[1:], " "),
		Desc: o.vals["desc"],
	}, nil
}

// buildRepo makes a repository out of "add NAME PATH" arguments.
//
// Parameters:
//   - o: parsed options and positional arguments
//
// Returns the repository, or an *optError.
func buildRepo(o options) (store.Repo, error) {
	if err := wantArgs(o.pos, 2, 2, "arg_name", "arg_path"); err != nil {
		return store.Repo{}, err
	}

	return store.Repo{Name: o.pos[0], Path: o.pos[1], Desc: o.vals["desc"]}, nil
}

// underReposRoot lets "tote repos add NAME rel/path" mean a folder under repos_root.
//
// Parameters:
//   - a: running app, for the settings and the home directory
//   - o: parsed arguments of add
//
// Returns o with the path replaced by its place under repos_root when the path is relative, that
// is it starts with neither "/", "~" nor ".", repos_root is an absolute path or one starting with
// "~", and that folder exists there. In every other case o is returned as it is, so the path is
// checked as before and a relative one is refused.
func underReposRoot(a *app, o options) options {
	if len(o.pos) != 2 {
		return o
	}

	rel, root := o.pos[1], a.cfgWarn().ReposRoot
	if rel == "" || root == "" || !shell.AbsOrHome(root) || strings.ContainsAny(rel[:1], "/~.") {
		return o
	}

	full := filepath.Join(root, rel)

	if info, err := os.Stat(shell.ExpandHome(full, a.Home)); err != nil || !info.IsDir() {
		return o
	}

	pos := slices.Clone(o.pos)
	pos[1] = full
	o.pos = pos

	return o
}

// buildFunc makes a function out of "add NAME --script PATH" arguments.
//
// Parameters:
//   - o: parsed options and positional arguments
//
// Returns the function, of kind source unless --kind says otherwise, or an *optError.
func buildFunc(o options) (store.Func, error) {
	if err := wantArgs(o.pos, 1, 1, "arg_name"); err != nil {
		return store.Func{}, err
	}

	if !o.set["script"] {
		return store.Func{}, &optError{Key: "cli_missing_argument", Args: []any{"--script"}}
	}

	kind := o.vals["kind"]
	if kind == "" {
		kind = store.KindSource
	}

	return store.Func{
		Name:    o.pos[0],
		Desc:    o.vals["desc"],
		Kind:    kind,
		Script:  o.vals["script"],
		Managed: o.set["managed"],
	}, nil
}

// runAlias implements "tote alias".
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the exit code of the verb.
func (a *app) runAlias(args []string) int {
	return aliasSpec().dispatch(a, args)
}

// runFuncs implements "tote funcs".
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the exit code of the verb.
func (a *app) runFuncs(args []string) int {
	return funcSpec().dispatch(a, args)
}

// runRepos implements "tote repos", which adds "open" to the generic verbs.
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the exit code of the verb.
func (a *app) runRepos(args []string) int {
	if len(args) > 0 && args[0] == "open" {
		return a.repoOpen(args[1:])
	}

	return repoSpec().dispatch(a, args)
}

// repoOpen implements "tote repos open NAME".
//
// When the wrapper is there, the shell is told to change directory to the repository, whatever
// the after_open setting, which only drives the interactive screen. Without the wrapper the
// path is printed, and a terminal is told how to enable the integration. The same happens when
// the hand-off file vanished, which is what a suspension with Ctrl+Z leaves behind. A stored
// path that is not absolute is refused, because the shell would resolve it through CDPATH.
//
// Parameters:
//   - args: arguments after open
//
// Returns the exit code of the verb.
func (a *app) repoOpen(args []string) int {
	o, err := parseOptions(args, nil)
	if err != nil {
		return a.usageErr(err, "repos")
	}

	if o.help {
		return a.verbHelp("repos")
	}

	if err := wantArgs(o.pos, 1, 1, "arg_name"); err != nil {
		return a.usageErr(err, "repos")
	}

	s := repoSpec()
	c, rc := s.load(a)
	if c == nil {
		return rc
	}

	repo, ok := c.Get(o.pos[0])
	if !ok {
		return a.notFound(s.label, o.pos[0], s.names(c))
	}

	path := shell.ExpandHome(repo.Path, a.Home)
	file := a.Getenv("TOTE_HANDOFF")

	if file == "" {
		a.printPath(path)

		return exitOK
	}

	if !filepath.IsAbs(path) {
		return a.failMsg(exitFail, "cli_repo_path_relative", sanitize(path))
	}

	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return a.failMsg(exitFail, "cli_dir_not_found", sanitize(path))
	}

	msg := shell.Handoff{Action: shell.ChangeDir, Payload: path}
	if err := shell.WriteHandoff(file, msg); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			a.printPath(path)

			return exitOK
		}

		return a.fail(err)
	}

	return exitOK
}

// printPath shows a directory the shell cannot be sent to.
//
// Parameters:
//   - path: directory to print
//
// A terminal is also told how to enable the shell integration.
func (a *app) printPath(path string) {
	fmt.Fprintln(a.Stdout, sanitize(path))

	if a.IsTerminal(a.Stdout) {
		a.say("cli_no_wrapper")
	}
}
