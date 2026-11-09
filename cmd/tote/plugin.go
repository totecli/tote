package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/totecli/tote/internal/plugin"
	"github.com/totecli/tote/internal/ui"
)

const (
	// pluginSchema is the version of the JSON objects that the plugin verbs print.
	pluginSchema = 1

	// codeInterrupted is the code of the JSON error object of a run stopped by SIGINT or SIGTERM.
	codeInterrupted = "interrupted"

	// codeUsage is the code of the JSON error object of a command line that is wrong.
	codeUsage = "usage"

	// codeNotFound is the code of the JSON error object of a name that matches no record.
	codeNotFound = "not_found"

	// codeFailure is the code of the JSON error object of a failure that has no plugin code.
	codeFailure = "failure"

	// maxIssueLines is the most findings of a refused package that the command line lists.
	maxIssueLines = 10
)

// pluginVerbs lists the subverbs of "tote plugin", in the order of the help. The verb run is
// reserved but not built yet: it is not here, so it is an unknown subverb.
var pluginVerbs = []string{
	"list", "info", "install", "update", "enable", "disable", "remove", "check",
}

// errorDoc is the JSON form of a failed plugin command.
type errorDoc struct {
	Schema int       `json:"schema"`
	OK     bool      `json:"ok"`
	Error  errorBody `json:"error"`
}

// errorBody is the error of an errorDoc: the stable code, the message in the language of the run
// and the exit code of the process.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Exit    int    `json:"exit"`
}

// runPlugin implements "tote plugin [list|info|install|update|enable|disable|remove|check]". With
// no argument it opens the plugins screen on a terminal and prints the list otherwise.
//
// Parameters:
//   - args: arguments after plugin
//
// Returns the exit code of the verb: 0 done, 1 failure, 2 usage, 3 authentication, 4 refused,
// 5 integrity.
func (a *app) runPlugin(args []string) int {
	if len(args) == 0 && a.interactive() {
		return a.runUI(ui.StartPlugins)
	}

	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return a.pluginList(args)
	}

	switch args[0] {
	case "list", "ls":
		return a.pluginList(args[1:])
	case "info":
		return a.pluginInfo(args[1:])
	case "install":
		return a.pluginInstall(args[1:], false)
	case "update":
		return a.pluginInstall(args[1:], true)
	case "enable":
		return a.pluginEnable(args[1:])
	case "disable":
		return a.pluginDisable(args[1:])
	case "remove":
		return a.pluginRemove(args[1:])
	case "check":
		return a.pluginCheck(args[1:])
	}

	err := &optError{Key: "cli_unknown_subcommand", Args: []any{args[0]}}

	if slices.Contains(args, "--json") {
		return a.usageJSON(err)
	}

	a.failMsg(exitUsage, err.Key, sanitize(args[0]))
	a.didYouMean(args[0], pluginVerbs)
	a.say("cli_try_help", "tote help plugin")

	return exitUsage
}

// sessionOff reports whether every plugin is off for this run: --no-plugins, or the variable
// TOTE_NO_PLUGINS set to something other than empty, 0 or false.
func (a *app) sessionOff() bool {
	if a.noPlugins {
		return true
	}

	switch strings.ToLower(a.Getenv("TOTE_NO_PLUGINS")) {
	case "", "0", "false", "no", "off":
		return false
	}

	return true
}

// pluginRegistry opens the plugin registry of the run once. Opening does no I/O.
//
// Returns the registry, or the error of the path resolution.
func (a *app) pluginRegistry() (*plugin.Registry, error) {
	if a.registry != nil {
		return a.registry, nil
	}

	paths, err := a.cfgPaths()
	if err != nil {
		return nil, err
	}

	var bin string

	if a.Executable != nil {
		bin, _ = a.Executable()
	} else {
		bin, _ = os.Executable()
	}

	lookup := func(key string) (string, bool) {
		v := a.Getenv(key)

		return v, v != ""
	}

	a.registry = plugin.Open(plugin.Options{
		Paths:     paths,
		Host:      plugin.NewHost(a.Version, filepath.Base(bin)),
		NoPlugins: a.sessionOff(),
		Lookup:    lookup,
		Home:      a.Home,
		Getuid:    a.geteuid,
		Bin:       bin,
		Bundled:   a.Bundled,
		Now:       a.Now,
		Transport: a.PluginTransport,
	})

	return a.registry, nil
}

// geteuid gives the effective user id of the process, the real one unless a test replaced it.
func (a *app) geteuid() int {
	if a.Geteuid != nil {
		return a.Geteuid()
	}

	return os.Geteuid()
}

// pluginVerb is what every plugin verb needs once its command line is read.
//
// o holds the parsed options and js tells that --json was given.
type pluginVerb struct {
	o  options
	js bool
}

// pluginParse reads the command line of a plugin verb.
//
// Parameters:
//   - args: arguments after the verb
//   - specs: options the verb accepts besides --json
//   - lo: fewest positional arguments
//   - hi: most positional arguments
//   - names: catalog keys of the argument names for the missing argument message
//
// Returns the parsed command line and true, or false and the exit code to return (a usage error,
// or 0 after the help was printed).
func (a *app) pluginParse(args []string, specs []opt, lo, hi int, names ...string) (pluginVerb,
	bool, int,
) {
	v := pluginVerb{js: slices.Contains(args, "--json")}

	o, err := parseOptions(args, append(specs, opt{long: "json"}))
	if err == nil && !o.help {
		err = wantArgs(o.pos, lo, hi, names...)
	}

	if err != nil {
		if v.js {
			return v, false, a.usageJSON(err)
		}

		return v, false, a.usageErr(err, "plugin")
	}

	if o.help {
		return v, false, a.verbHelp("plugin")
	}

	v.o = o

	return v, true, exitOK
}

// usageJSON prints the JSON error object of a command line that is wrong.
//
// Parameters:
//   - err: error of parseOptions or wantArgs
//
// Returns exitUsage.
func (a *app) usageJSON(err error) int {
	msg := err.Error()

	var oerr *optError
	if errors.As(err, &oerr) {
		msg = a.t(oerr.Key, a.optArgs(oerr)...)
	}

	return a.reportJSON(codeUsage, sanitize(msg), exitUsage)
}

// reportJSON prints the JSON error object of a failed command and returns its exit code. Nothing
// goes to standard error: a script reads the object, and a non-zero exit means that any other
// output is to be ignored.
//
// Parameters:
//   - code: stable code of the failure
//   - msg: message in the language of the run
//   - exit: exit code of the process
func (a *app) reportJSON(code, msg string, exit int) int {
	doc := errorDoc{Schema: pluginSchema, Error: errorBody{Code: code, Message: msg, Exit: exit}}

	if err := writeJSON(a.Stdout, doc); err != nil {
		return exitFail
	}

	return exit
}

// describe turns an error of the plugin layer into the code, the message and the exit code of
// the command line. A run stopped by SIGINT or SIGTERM is the code "interrupted" with exit 130,
// whatever layer noticed it. A refused package is described by its first finding, with its own
// arguments, because the error carries the umbrella arguments.
//
// Parameters:
//   - err: error to describe
func (a *app) describe(err error) (code, msg string, exit int) {
	if errors.Is(err, context.Canceled) {
		return codeInterrupted, a.t("cli_plugin_interrupted"), exitInterrupted
	}

	var pe *plugin.Error

	if !errors.As(err, &pe) {
		return codeFailure, sanitize(scrubURLs(err.Error())), exitFail
	}

	exit = plugin.ExitOf(err)

	var me *plugin.ManifestError

	if errors.As(err, &me) && len(me.Issues) > 0 {
		first := me.Issues[0]
		msg = a.issueText(first.Path, first.Line, first.Code, first.Args)

		if len(me.Issues) > 1 {
			msg += " " + a.t("cli_plugin_more", len(me.Issues)-1)
		}

		return pe.Code, msg, exit
	}

	return pe.Code, a.errText(pe.Code, pe.Args, pe.Err), exit
}

// pluginFail reports an error of the plugin layer.
//
// Parameters:
//   - err: the error
//   - js: true for the JSON object on standard output, false for a line on standard error
//
// Returns the exit code of the error class.
func (a *app) pluginFail(err error, js bool) int {
	code, msg, exit := a.describe(err)

	if js {
		return a.reportJSON(code, msg, exit)
	}

	a.say("cli_error", msg)

	var me *plugin.ManifestError

	if errors.As(err, &me) {
		a.listIssues(me.Issues)
	}

	a.failHint(err)

	return exit
}

// listIssues prints the findings that refused a package, after the first one, which the error
// line already shows.
//
// Parameters:
//   - issues: findings of severity error, in report order
func (a *app) listIssues(issues []plugin.Issue) {
	for i, is := range issues {
		if i == 0 {
			continue
		}

		if i > maxIssueLines {
			fmt.Fprintln(a.Stderr, "  "+a.t("cli_plugin_more", len(issues)-i))

			return
		}

		fmt.Fprintln(a.Stderr, "  "+a.issueText(is.Path, is.Line, is.Code, is.Args))
	}
}

// failHint adds the line that tells what to do next after an error of the plugin layer.
//
// Parameters:
//   - err: the error that was reported
func (a *app) failHint(err error) {
	var pe *plugin.Error
	if !errors.As(err, &pe) {
		return
	}

	switch pe.Code {
	case plugin.CodeNotInstalled:
		if a.isAvailable(arg(pe.Args, 0)) {
			fmt.Fprintln(a.Stderr, "tote: "+a.t("cli_plugin_available_hint", arg(pe.Args, 0)))

			return
		}

		fmt.Fprintln(a.Stderr, "tote: "+a.t("cli_plugin_not_installed_hint"))
		a.suggestPlugin(arg(pe.Args, 0))
	case plugin.CodeEnableRefused:
		if slices.Contains(pe.Args, plugin.ReasonSessionOff) {
			a.say("cli_plugin_fix", a.t("plugin_fix_session_off"))
		}
	}
}

// isAvailable reports whether a plugin ships with tote and is not installed.
//
// Parameters:
//   - id: plugin id
func (a *app) isAvailable(id string) bool {
	reg, err := a.pluginRegistry()
	if err != nil {
		return false
	}

	for _, av := range reg.Available(reg.Load(), a.tr().Lang()) {
		if av.ID == id {
			return true
		}
	}

	return false
}

// suggestPlugin proposes the installed plugin whose id is the closest to a word.
//
// Parameters:
//   - word: what the user typed
func (a *app) suggestPlugin(word string) {
	reg, err := a.pluginRegistry()
	if err != nil {
		return
	}

	var ids []string

	for _, p := range reg.Load().Plugins() {
		ids = append(ids, p.ID)
	}

	a.didYouMean(word, ids)
}

// colon gives the punctuation that follows a label: a colon, preceded by a no-break space in
// French.
func (a *app) colon() string {
	if a.tr().Lang() == "fr" {
		return "\u00a0:"
	}

	return ":"
}
