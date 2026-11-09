package main

import (
	"fmt"

	"github.com/totecli/tote/internal/ui"
)

// runConfig implements "tote config path", and "tote config" alone, which opens the settings
// screen of the interface on a terminal.
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the exit code of the verb.
func (a *app) runConfig(args []string) int {
	o, err := parseOptions(args, nil)
	if err != nil {
		return a.usageErr(err, "config")
	}

	if o.help {
		return a.verbHelp("config")
	}

	if len(o.pos) == 0 && a.interactive() {
		return a.runUI(ui.StartConfig)
	}

	if err := wantArgs(o.pos, 1, 1, "arg_name"); err != nil {
		return a.usageErr(err, "config")
	}

	if o.pos[0] != "path" {
		a.failMsg(exitUsage, "cli_unknown_subcommand", o.pos[0])
		a.say("cli_try_help", "tote help config")

		return exitUsage
	}

	paths, err := a.cfgPaths()
	if err != nil {
		return a.fail(err)
	}

	fmt.Fprintln(a.Stdout, sanitize(paths.Config))

	return exitOK
}
