package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/plugin"
)

// enableDoc is the JSON form of "plugin enable".
type enableDoc struct {
	Schema     int         `json:"schema"`
	OK         bool        `json:"ok"`
	ID         string      `json:"id"`
	State      string      `json:"state"`
	Reasons    []reasonDoc `json:"reasons"`
	Already    bool        `json:"already"`
	Consented  bool        `json:"consented"`
	ConfigFile string      `json:"config_file,omitempty"`
}

// disableDoc is the JSON form of "plugin disable".
type disableDoc struct {
	Schema     int    `json:"schema"`
	OK         bool   `json:"ok"`
	ID         string `json:"id"`
	Already    bool   `json:"already"`
	Killed     int    `json:"killed"`
	ConfigFile string `json:"config_file,omitempty"`
}

// removeDoc is the JSON form of "plugin remove".
type removeDoc struct {
	Schema     int      `json:"schema"`
	OK         bool     `json:"ok"`
	ID         string   `json:"id"`
	Killed     int      `json:"killed"`
	Kept       []string `json:"kept"`
	Leftovers  []string `json:"leftovers"`
	Link       bool     `json:"link"`
	ConfigFile string   `json:"config_file,omitempty"`
}

// pluginEnable implements "tote plugin enable <id> [--grant TOKEN]".
//
// A plugin that waits for consent is enabled only with it: on a terminal the user reads what the
// plugin asks for and answers, otherwise --grant must carry the token that "plugin info" prints.
//
// Parameters:
//   - args: arguments after enable
//
// Returns the exit code of the verb.
func (a *app) pluginEnable(args []string) int {
	v, ok, rc := a.pluginParse(args, []opt{{long: "grant", value: true}}, 1, 1, "arg_id")
	if !ok {
		return rc
	}

	id := v.o.pos[0]

	reg, err := a.pluginRegistry()
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	g := plugin.Grant{Token: v.o.vals["grant"]}

	if g.Token == "" && !a.sessionOff() {
		if d, aerr := reg.Allow(id); aerr == nil {
			var ok bool

			d = a.localized(reg, d)

			if g, ok, rc = a.consent(context.Background(), d, plugin.ViewAllow, "", false,
				"cli_plugin_ask_allow"); !ok {
				return rc
			}
		}
	}

	res, err := reg.Enable(id, g)
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	if v.js {
		doc := enableDoc{
			Schema: pluginSchema, OK: true, ID: id, State: string(res.State),
			Reasons: a.reasonDocs(res.Reasons), Already: res.Already, Consented: res.Consented,
			ConfigFile: res.ConfigFile,
		}
		if werr := writeJSON(a.Stdout, doc); werr != nil {
			return a.fail(werr)
		}

		return exitOK
	}

	key := "cli_plugin_enabled"
	if res.Already {
		key = "cli_plugin_already_enabled"
	}

	if res.ConfigFile != "" {
		a.configNote("enable", res.ConfigFile, id)
	} else {
		fmt.Fprintln(a.Stdout, a.t(key, sanitize(id)))
	}

	a.notEnabled(id, res.State, res.Reasons)

	return exitOK
}

// localized puts the title and the description of a plugin in the language of the run when the
// plugin translates them.
//
// Parameters:
//   - reg: the registry
//   - d: what the allow screen shows
func (a *app) localized(reg *plugin.Registry, d plugin.Diff) plugin.Diff {
	p, found := reg.Load().Get(d.ID)
	if !found || p.Manifest == nil {
		return d
	}

	lang := a.tr().Lang()
	d.Title = p.Catalog.Text(lang, "title", d.Title)
	d.Description = p.Catalog.Text(lang, "description", d.Description)

	return d
}

// pluginDisable implements "tote plugin disable <id>": the plugin is off at once, its settings,
// data and consent stay.
//
// Parameters:
//   - args: arguments after disable
//
// Returns the exit code of the verb.
func (a *app) pluginDisable(args []string) int {
	v, ok, rc := a.pluginParse(args, nil, 1, 1, "arg_id")
	if !ok {
		return rc
	}

	id := v.o.pos[0]

	reg, err := a.pluginRegistry()
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	res, err := reg.Disable(id)
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	if v.js {
		doc := disableDoc{
			Schema: pluginSchema, OK: true, ID: id, Already: res.Already,
			Killed: res.Killed, ConfigFile: res.ConfigFile,
		}
		if werr := writeJSON(a.Stdout, doc); werr != nil {
			return a.fail(werr)
		}

		return exitOK
	}

	if res.Already {
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_already_disabled", sanitize(id)))
	} else {
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_disabled", sanitize(id)))
	}

	if res.Killed > 0 {
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_stopped", res.Killed))
	}

	if res.ConfigFile != "" {
		a.configNote("disable", res.ConfigFile, id)
	}

	return exitOK
}

// pluginRemove implements "tote plugin remove <id> [--yes] [--purge]".
//
// The id is checked against the grammar before anything else, so that ../data fails with a usage
// error and no file system call. The question lists what goes and defaults to no; without a
// terminal and without --yes the command exits 2.
//
// Parameters:
//   - args: arguments after remove
//
// Returns the exit code of the verb.
func (a *app) pluginRemove(args []string) int {
	v, ok, rc := a.pluginParse(args, []opt{{long: "yes"}, {long: "purge"}}, 1, 1, "arg_id")
	if !ok {
		return rc
	}

	id := v.o.pos[0]

	if verr := config.ValidPluginID(id); verr != nil {
		return a.pluginFail(&plugin.Error{Code: plugin.CodeBadID, Args: []string{id}, Err: verr},
			v.js)
	}

	reg, err := a.pluginRegistry()
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	if _, found := reg.Recheck(id); !found {
		return a.pluginFail(&plugin.Error{Code: plugin.CodeNotInstalled, Args: []string{id}},
			v.js)
	}

	purge := v.o.set["purge"]

	if !v.o.set["yes"] {
		if !a.IsInputTerminal(a.Stdin) {
			return a.pluginFail(&plugin.Error{
				Code: plugin.CodeConfirmNeeded,
				Args: []string{id, "remove"},
			}, v.js)
		}

		a.removeGoes(id, purge)

		ask := a.t("cli_confirm_remove", a.t("kind_plugin"), sanitize(id))

		if yes, code := a.askYesIn(context.Background(), ask, "cli_plugin_declined"); !yes {
			return code
		}
	}

	res, err := reg.Remove(id, plugin.RemoveOptions{Purge: purge})
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	return a.removedReport(id, res, v.js)
}

// removeGoes tells what a removal deletes.
//
// Parameters:
//   - id: plugin id
//   - purge: true when --purge was given
func (a *app) removeGoes(id string, purge bool) {
	fmt.Fprintln(a.Stderr, a.t("cli_plugin_remove_goes", sanitize(id)))
	fmt.Fprintln(a.Stderr, "  - "+a.t("cli_plugin_goes_base"))

	if purge {
		fmt.Fprintln(a.Stderr, "  - "+a.t("cli_plugin_goes_purge"))
	}
}

// removedReport reports a removal.
//
// Parameters:
//   - id: plugin id
//   - res: what the removal did
//   - js: true for JSON
func (a *app) removedReport(id string, res plugin.RemoveResult, js bool) int {
	if js {
		doc := removeDoc{
			Schema: pluginSchema, OK: true, ID: id, Killed: res.Killed,
			Kept: orEmpty(res.Kept), Leftovers: orEmpty(res.Leftovers), Link: res.Link,
			ConfigFile: res.ConfigFile,
		}
		if err := writeJSON(a.Stdout, doc); err != nil {
			return a.fail(err)
		}

		return exitOK
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_removed", sanitize(id)))

	if res.Killed > 0 {
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_stopped", res.Killed))
	}

	if len(res.Kept) > 0 {
		words := make([]string, len(res.Kept))
		for i, k := range res.Kept {
			words[i] = a.word("plugin_left_", k)
		}

		fmt.Fprintln(a.Stdout, a.t("cli_plugin_removed_kept", strings.Join(words, ", ")))
	}

	for _, l := range res.Leftovers {
		a.say("cli_plugin_leftover", sanitize(l))
	}

	if res.ConfigFile != "" {
		a.configNote("purge", res.ConfigFile, id)
	}

	return exitOK
}

// configNote says why config.toml could not be edited for a plugin. A file without the write
// permission is reported as such; any other file is written in a layout the editor leaves alone.
//
// Parameters:
//   - verb: enable, disable or purge, which picks the message
//   - file: path of config.toml
//   - id: plugin id
func (a *app) configNote(verb, file, id string) {
	readOnly := false
	if info, err := os.Stat(file); err == nil && info.Mode().Perm()&0o200 == 0 {
		readOnly = true
	}

	var key string

	switch {
	case verb == "enable" && readOnly:
		key = "cli_plugin_config_readonly_enable"
	case verb == "enable":
		key = "cli_plugin_config_layout_enable"
	case verb == "disable" && readOnly:
		key = "cli_plugin_config_readonly_disable"
	case verb == "disable":
		key = "cli_plugin_config_layout_disable"
	case readOnly:
		key = "cli_plugin_config_readonly_purge"
	default:
		key = "cli_plugin_config_layout_purge"
	}

	a.say(key, sanitize(file), sanitize(id))
}
