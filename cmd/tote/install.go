package main

import (
	"fmt"

	"github.com/totecli/tote/internal/plugin"
)

// installOpts lists the options of the install and update verbs.
var installOpts = []opt{
	{long: "sha256", value: true},
	{long: "grant", value: true},
	{long: "dry-run"},
	{long: "force"},
}

// commandDoc is the JSON form of a command a plugin runs.
type commandDoc struct {
	Table     string   `json:"table"`
	ID        string   `json:"id"`
	Run       []string `json:"run,omitempty"`
	Open      string   `json:"open,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	AllowDash []string `json:"allow_dash,omitempty"`
	Env       []string `json:"env,omitempty"`
}

// diffDoc is the JSON form of what an install or an update will do.
type diffDoc struct {
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	Version       string       `json:"version"`
	OldVersion    string       `json:"old_version,omitempty"`
	Tier          string       `json:"tier"`
	Kind          string       `json:"kind"`
	Origin        string       `json:"origin"`
	Class         string       `json:"class"`
	Source        string       `json:"source"`
	ArchiveSHA256 string       `json:"archive_sha256,omitempty"`
	TreeSHA256    string       `json:"tree_sha256"`
	SurfaceSHA256 string       `json:"surface_sha256"`
	Update        bool         `json:"update"`
	Caps          []string     `json:"caps"`
	Added         []string     `json:"added"`
	Removed       []string     `json:"removed"`
	Unchanged     int          `json:"unchanged"`
	Commands      []commandDoc `json:"commands"`
	Missing       []string     `json:"missing"`
	Warnings      []reasonDoc  `json:"warnings"`
	NeedsConsent  bool         `json:"needs_consent"`
	Why           []string     `json:"why"`
	Grant         string       `json:"grant"`
}

// dryDoc is the JSON form of --dry-run.
type dryDoc struct {
	Schema int     `json:"schema"`
	OK     bool    `json:"ok"`
	DryRun bool    `json:"dry_run"`
	Plugin diffDoc `json:"plugin"`

	AlreadyInstalled bool `json:"already_installed"`
}

// installDoc is the JSON form of an install or an update that was done.
type installDoc struct {
	Schema      int         `json:"schema"`
	OK          bool        `json:"ok"`
	ID          string      `json:"id"`
	Version     string      `json:"version"`
	OldVersion  string      `json:"old_version,omitempty"`
	Updated     bool        `json:"updated"`
	State       string      `json:"state"`
	Reasons     []reasonDoc `json:"reasons"`
	ConsentedBy string      `json:"consented_by"`
	Warnings    []reasonDoc `json:"warnings"`
	Leftovers   []string    `json:"leftovers"`

	AlreadyInstalled bool `json:"already_installed"`
}

// commandDocs converts the commands of a diff.
//
// Parameters:
//   - cmds: commands a plugin runs
func commandDocs(cmds []plugin.CommandRef) []commandDoc {
	out := make([]commandDoc, 0, len(cmds))

	for _, c := range cmds {
		out = append(out, commandDoc{
			Table: c.Table, ID: c.ID, Run: c.Run, Open: c.Open,
			Cwd: c.Cwd, AllowDash: c.AllowDash, Env: c.Env,
		})
	}

	return out
}

// diffToDoc converts a diff for JSON.
//
// Parameters:
//   - d: what will be done
func (a *app) diffToDoc(d plugin.Diff) diffDoc {
	missing := make([]string, 0, len(d.Missing))
	for _, m := range d.Missing {
		missing = append(missing, m.Bin)
	}

	return diffDoc{
		ID: d.ID, Title: d.Title, Version: d.Version, OldVersion: d.OldVersion, Tier: d.Tier,
		Kind: d.Kind, Origin: d.Origin, Class: d.Class, Source: d.Source,
		ArchiveSHA256: d.ArchiveSHA256, TreeSHA256: d.TreeSHA256, SurfaceSHA256: d.SurfaceSHA256,
		Update: d.Update, Caps: orEmpty(d.Caps), Added: orEmpty(d.Added),
		Removed: orEmpty(d.Removed), Unchanged: d.Unchanged, Commands: commandDocs(d.Commands),
		Missing: missing, Warnings: a.reasonDocs(d.Warnings), NeedsConsent: d.NeedsConsent,
		Why: orEmpty(d.Why), Grant: d.Grant,
	}
}

// pluginInstall implements "tote plugin install <source>" and "tote plugin update <id>
// [<source>]".
//
// The package is copied, checked and hashed first, and nothing is installed until the consent is
// given: on a terminal by answering the question, otherwise with --grant and the token that
// --dry-run prints.
//
// Parameters:
//   - args: arguments after the verb
//   - update: true for update, false for install
//
// Returns the exit code of the verb.
func (a *app) pluginInstall(args []string, update bool) int {
	lo, hi, name := 1, 1, "arg_source"
	if update {
		hi, name = 2, "arg_id"
	}

	v, ok, rc := a.pluginParse(args, installOpts, lo, hi, name)
	if !ok {
		return rc
	}

	req := plugin.InstallRequest{
		SHA256: v.o.vals["sha256"], Force: v.o.set["force"],
		Update: update, Lang: a.tr().Lang(),
	}

	if update {
		req.ID = v.o.pos[0]

		if len(v.o.pos) > 1 {
			req.Source = v.o.pos[1]
		}
	} else {
		req.Source = v.o.pos[0]
	}

	reg, err := a.pluginRegistry()
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	ctx, cancel := a.context()
	defer cancel()

	st, err := reg.Plan(ctx, req)
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	defer func() { _ = st.Abort() }()

	d := st.Diff()
	mode, ask := plugin.ViewInstall, "cli_plugin_ask_install"

	if d.Update {
		mode, ask = plugin.ViewUpdate, "cli_plugin_ask_update"
	}

	if cur, same := st.AlreadyInstalled(); same {
		return a.alreadyInstalled(d, cur, v.o.set["dry-run"], v.js)
	}

	pinned := req.SHA256 != ""

	if v.o.set["dry-run"] {
		return a.dryRun(d, mode, pinned, v.js)
	}

	g, ok, rc := a.consent(ctx, d, mode, v.o.vals["grant"], pinned, ask)
	if !ok {
		return rc
	}

	res, err := st.Commit(g)
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	return a.installed(res, v.js)
}

// alreadyInstalled reports an install that asks for what is installed already, the same plugin
// from the same source at the same version with the same tree hash. Nothing was asked and nothing
// changed: the store and the lock were not touched, with --dry-run or without. The plugin may well
// not be enabled, and then the reasons and the way out follow, as after an install. A plugin that
// cannot be used as it is (it needs consent, is broken or is incompatible) exits with 4, so that
// "install && use" does not go on; disabled, quarantined and missing-program plugins exit with 0.
//
// Parameters:
//   - d: what the install would have done
//   - cur: the plugin as it is installed
//   - dry: true for --dry-run
//   - js: true for JSON
func (a *app) alreadyInstalled(d plugin.Diff, cur plugin.Plugin, dry, js bool) int {
	rc := exitOK

	switch cur.State {
	case plugin.StateNeedsConsent, plugin.StateBroken, plugin.StateIncompatible:
		rc = exitRefused
	}

	if js {
		var doc any

		if dry {
			doc = dryDoc{
				Schema: pluginSchema, OK: rc == exitOK, DryRun: true,
				Plugin: a.diffToDoc(d), AlreadyInstalled: true,
			}
		} else {
			doc = installDoc{
				Schema: pluginSchema, OK: rc == exitOK, ID: cur.ID,
				Version: d.Version, State: string(cur.State), Reasons: a.reasonDocs(cur.Reasons),
				Warnings: []reasonDoc{}, Leftovers: []string{}, AlreadyInstalled: true,
			}
		}

		if err := writeJSON(a.Stdout, doc); err != nil {
			return a.fail(err)
		}

		return rc
	}

	fmt.Fprintln(a.Stdout, a.t("plugin_already_installed", sanitize(cur.ID), sanitize(d.Version),
		a.stateWord(string(cur.State))))

	a.notEnabled(cur.ID, cur.State, cur.Reasons)

	return rc
}

// dryRun prints what an install or an update would do and the token that accepts it, and changes
// nothing.
//
// Parameters:
//   - d: what will be done
//   - mode: one of the View constants of the plugin package
//   - pinned: true when the user typed --sha256
//   - js: true for JSON
func (a *app) dryRun(d plugin.Diff, mode string, pinned, js bool) int {
	if js {
		doc := dryDoc{Schema: pluginSchema, OK: true, DryRun: true, Plugin: a.diffToDoc(d)}
		if err := writeJSON(a.Stdout, doc); err != nil {
			return a.fail(err)
		}

		return exitOK
	}

	a.writeView(a.Stdout, d, mode, pinned)

	if d.NeedsConsent {
		fmt.Fprintln(a.Stdout)
		a.whyLines(a.Stdout, d.Why)
		fmt.Fprintln(a.Stdout, a.t("consent_code_line", d.Grant))
	}

	fmt.Fprintln(a.Stdout)
	fmt.Fprintln(a.Stdout, a.t("cli_plugin_dry_run"))

	if d.NeedsConsent {
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_dry_hint", d.Grant))
	}

	return exitOK
}

// installed reports an install or an update that was done.
//
// Parameters:
//   - res: result of the commit
//   - js: true for JSON
func (a *app) installed(res plugin.InstallResult, js bool) int {
	if js {
		doc := installDoc{
			Schema: pluginSchema, OK: true, ID: res.ID, Version: res.Version,
			OldVersion: res.OldVersion, Updated: res.Updated, State: string(res.State),
			Reasons: a.reasonDocs(res.Reasons), ConsentedBy: res.ConsentedBy,
			Warnings: a.reasonDocs(res.Warnings), Leftovers: orEmpty(res.Leftovers),
		}

		if err := writeJSON(a.Stdout, doc); err != nil {
			return a.fail(err)
		}

		return exitOK
	}

	state := a.stateWord(string(res.State))

	switch {
	case res.Updated && res.ConsentedBy == "":
		fmt.Fprintln(a.Stdout, a.t("plugin_updated_quiet", res.ID, res.OldVersion, res.Version))
	case res.Updated:
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_updated", res.ID, res.OldVersion, res.Version,
			state))
	default:
		fmt.Fprintln(a.Stdout, a.t("plugin_installed", res.ID, res.Version, state))
	}

	a.notEnabled(res.ID, res.State, res.Reasons)

	for _, w := range res.Warnings {
		a.say("cli_plugin_problem", a.reasonText(w))
	}

	for _, l := range res.Leftovers {
		a.say("cli_plugin_leftover", sanitize(l))
	}

	return exitOK
}

// notEnabled lists why a plugin is not enabled and what to do about it. It prints nothing for an
// enabled plugin.
//
// Parameters:
//   - id: plugin id
//   - state: state of the plugin
//   - reasons: why it is in that state
func (a *app) notEnabled(id string, state plugin.State, reasons []plugin.Reason) {
	if state == plugin.StateEnabled {
		return
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_not_enabled", sanitize(id), a.stateWord(string(state))))

	for _, r := range reasons {
		fmt.Fprintln(a.Stdout, "  - "+a.reasonText(r))
	}

	if fix, ok := a.fixText(string(state), id); ok {
		fmt.Fprintln(a.Stdout, "  "+a.t("cli_plugin_fix", fix))
	}
}
