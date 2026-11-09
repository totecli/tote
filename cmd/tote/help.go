package main

import (
	"fmt"
	"runtime"
	"slices"
)

// command is one top-level verb of tote.
//
// names lists every spelling of the verb, the first being the canonical one, and label is how
// the help shows them. desc is the catalog key of the one line description and usage lists the
// catalog keys of the usage lines. A hidden command is neither listed, completed nor suggested.
type command struct {
	names  []string
	label  string
	run    func(*app, []string) int
	desc   string
	usage  []string
	hidden bool
}

// commands returns every verb of tote in the order of the help.
//
// A function and not a variable: the verbs refer to methods that print this list.
func commands() []command {
	return []command{
		{
			names: []string{"help"}, label: "help", run: (*app).runHelp,
			desc: "help_cmd_help", usage: []string{"usage_help"},
		},
		{
			names: []string{"version"}, label: "version", run: (*app).runVersion,
			desc: "help_cmd_version", usage: []string{"usage_version"},
		},
		{
			names: []string{"init"}, label: "init", run: (*app).runInit,
			desc: "help_cmd_init", usage: []string{"usage_init"},
		},
		{
			names: []string{"co", "conn", "connections", "connexions"}, label: "co, connections",
			run: (*app).runConn, desc: "help_cmd_co",
			usage: []string{
				"usage_co_ui", "usage_co_list", "usage_co_connect", "usage_co_add", "usage_co_rm",
			},
		},
		{
			names: []string{"alias", "aliases", "a"}, label: "alias, a", run: (*app).runAlias,
			desc:  "help_cmd_alias",
			usage: []string{"usage_alias_list", "usage_alias_add", "usage_alias_rm"},
		},
		{
			names: []string{"repos", "repo", "r"}, label: "repos, repo, r", run: (*app).runRepos,
			desc: "help_cmd_repos",
			usage: []string{
				"usage_repos_list", "usage_repos_open", "usage_repos_add", "usage_repos_rm",
				"usage_repos_root",
			},
		},
		{
			names: []string{"funcs", "func", "f"}, label: "funcs, f", run: (*app).runFuncs,
			desc:  "help_cmd_funcs",
			usage: []string{"usage_funcs_list", "usage_funcs_add", "usage_funcs_rm"},
		},
		{
			names: []string{"plugin", "plugins"}, label: "plugin, plugins", run: (*app).runPlugin,
			desc: "help_cmd_plugin",
			usage: []string{
				"usage_plugin_list", "usage_plugin_info", "usage_plugin_install",
				"usage_plugin_update", "usage_plugin_enable", "usage_plugin_disable",
				"usage_plugin_remove", "usage_plugin_check", "usage_plugin_source",
				"usage_plugin_hash", "usage_plugin_argv", "usage_plugin_flags",
				"usage_plugin_exit",
			},
		},
		{
			names: []string{"config"}, label: "config", run: (*app).runConfig,
			desc:  "help_cmd_config",
			usage: []string{"usage_config_ui", "usage_config_path"},
		},
		{
			names: []string{"doctor"}, label: "doctor", run: (*app).runDoctor,
			desc:  "help_cmd_doctor",
			usage: []string{"usage_doctor", "usage_doctor_reset", "usage_doctor_exit"},
		},
		{
			names: []string{"update"}, label: "update", run: (*app).runUpdate,
			desc:  "help_cmd_update",
			usage: []string{"usage_update", "usage_update_options", "usage_update_exit"},
		},
		{
			names: []string{"uninstall"}, label: "uninstall", run: (*app).runUninstall,
			desc: "help_cmd_uninstall",
			usage: []string{
				"usage_uninstall", "usage_uninstall_yes", "usage_uninstall_purge",
				"usage_uninstall_dry",
			},
		},
		{names: []string{"__complete"}, run: (*app).runComplete, hidden: true},
	}
}

// findCommand looks a verb up by any of its spellings.
//
// Parameters:
//   - name: what the user typed, matched exactly
//
// Returns the command and true, or the zero command and false.
func findCommand(name string) (command, bool) {
	for _, c := range commands() {
		if slices.Contains(c.names, name) {
			return c, true
		}
	}

	return command{}, false
}

// commandNames lists the spellings worth suggesting for a mistyped verb.
//
// Returns every spelling of a visible command longer than two letters.
func commandNames() []string {
	var out []string

	for _, c := range commands() {
		if c.hidden {
			continue
		}

		for _, n := range c.names {
			if len(n) > 2 {
				out = append(out, n)
			}
		}
	}

	return out
}

// rootHelp prints the list of verbs on stdout.
func (a *app) rootHelp() {
	cmds := commands()
	width := 0

	for _, c := range cmds {
		width = max(width, len(c.label))
	}

	w := a.Stdout
	fmt.Fprintln(w, a.t("help_tagline"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, a.t("help_usage_header"))
	fmt.Fprintln(w, "  "+a.t("help_usage_root"))
	fmt.Fprintln(w, "  "+a.t("help_usage_ui"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, a.t("help_commands_header"))

	for _, c := range cmds {
		if !c.hidden {
			fmt.Fprintf(w, "  %-*s  %s\n", width, c.label, a.t(c.desc))
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, a.t("help_options_header"))
	fmt.Fprintln(w, "  "+a.t("help_opt_ascii"))
	fmt.Fprintln(w, "  "+a.t("help_opt_noplugins"))
	fmt.Fprintln(w, "  "+a.t("help_opt_color"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, a.t("help_more"))
}

// verbHelp prints the description and the usage lines of one verb on stdout.
//
// Parameters:
//   - name: any spelling of the verb
//
// Returns exitOK, or exitUsage when the verb does not exist.
func (a *app) verbHelp(name string) int {
	c, ok := findCommand(name)
	if !ok || c.hidden {
		return a.unknownCommand(name)
	}

	fmt.Fprintln(a.Stdout, a.t(c.desc))
	fmt.Fprintln(a.Stdout)
	fmt.Fprintln(a.Stdout, a.t("help_usage_header"))

	for _, key := range c.usage {
		fmt.Fprintln(a.Stdout, "  "+a.t(key))
	}

	return exitOK
}

// runHelp implements "tote help [command]".
//
// Parameters:
//   - args: arguments after help
//
// Returns the exit code of the verb.
func (a *app) runHelp(args []string) int {
	o, err := parseOptions(args, nil)
	if err != nil {
		return a.usageErr(err, "help")
	}

	if err := wantArgs(o.pos, 0, 1); err != nil {
		return a.usageErr(err, "help")
	}

	if len(o.pos) == 0 {
		a.rootHelp()
		return exitOK
	}

	return a.verbHelp(o.pos[0])
}

// versionInfo is the JSON form of "tote version --json".
type versionInfo struct {
	Schema  int    `json:"schema"`
	Version string `json:"version"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// runVersion implements "tote version [--json]".
//
// Parameters:
//   - args: arguments after version
//
// Returns the exit code of the verb.
func (a *app) runVersion(args []string) int {
	o, err := parseOptions(args, []opt{{long: "json"}})
	if err == nil {
		err = wantArgs(o.pos, 0, 0)
	}

	if err != nil {
		return a.usageErr(err, "version")
	}

	switch {
	case o.help:
		return a.verbHelp("version")
	case o.set["json"]:
		info := versionInfo{listSchema, a.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH}
		if err := writeJSON(a.Stdout, info); err != nil {
			return a.fail(err)
		}
	default:
		fmt.Fprintln(a.Stdout, "tote "+a.Version)
	}

	return exitOK
}
