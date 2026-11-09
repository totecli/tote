package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/totecli/tote/internal/plugin"
)

const (
	// stateAvailable is the state shown for a plugin shipped with tote that is not installed.
	stateAvailable = "available"

	// listDescRunes is the most runes of the description in the columns of the list.
	listDescRunes = 70
)

// reasonDoc is the JSON form of a reason: the stable code, its arguments and the message in the
// language of the run.
type reasonDoc struct {
	Code    string   `json:"code"`
	Args    []string `json:"args"`
	Message string   `json:"message"`
}

// pluginDoc is the JSON form of a plugin in "plugin list --json" and the base of "plugin info".
type pluginDoc struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Version     string             `json:"version"`
	Tier        string             `json:"tier"`
	Kind        string             `json:"kind"`
	Origin      string             `json:"origin"`
	State       string             `json:"state"`
	Reasons     []reasonDoc        `json:"reasons"`
	Enabled     bool               `json:"enabled"`
	Consented   bool               `json:"consented"`
	Caps        []string           `json:"caps"`
	Grant       string             `json:"grant"`
	Contributes plugin.Contributes `json:"contributes"`
}

// pluginRow is one plugin of the list: the JSON object and the strings of the columns.
type pluginRow struct {
	doc  pluginDoc
	text string
	desc string
}

// reasonDocs renders reasons for JSON.
//
// Parameters:
//   - rs: reasons, may be empty
func (a *app) reasonDocs(rs []plugin.Reason) []reasonDoc {
	out := make([]reasonDoc, 0, len(rs))

	for _, r := range rs {
		out = append(out, reasonDoc{
			Code: r.Code, Args: orEmpty(r.Args),
			Message: a.reasonText(r),
		})
	}

	return out
}

// orEmpty gives a list that JSON prints as [] and not as null.
//
// Parameters:
//   - list: list that may be nil
func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}

	return list
}

// originOf gives the origin of an installed plugin: the owner/repo of a release, else the kind.
//
// Parameters:
//   - rec: lock record
func originOf(rec plugin.LockRecord) string {
	if rec.Origin != "" {
		return rec.Origin
	}

	return rec.Kind
}

// installedRow builds the row of an installed plugin.
//
// Parameters:
//   - p: plugin as one Load saw it
func (a *app) installedRow(p plugin.Plugin) pluginRow {
	lang := a.tr().Lang()
	doc := pluginDoc{
		ID: p.ID, Version: p.Record.Version, Tier: p.Record.Tier, Kind: p.Record.Kind,
		Origin: p.Record.Origin, State: string(p.State), Reasons: a.reasonDocs(p.Reasons),
		Enabled: p.State == plugin.StateEnabled, Consented: p.Record.Consented(),
		Caps: orEmpty(p.Caps), Grant: p.Grant, Contributes: plugin.ContributesOf(p.Manifest),
	}

	if doc.Version == "" {
		doc.Version = p.Header.Version
	}

	if doc.Tier == "" {
		doc.Tier = p.Header.Tier
	}

	doc.Title = p.Header.Title
	desc := ""

	if p.Manifest != nil {
		doc.Title = p.Catalog.Text(lang, "title", p.Manifest.Title)
		desc = p.Catalog.Text(lang, "description", p.Manifest.Description)
	}

	text := originOf(p.Record)
	if p.Record.Origin == "" {
		text = a.word("plugin_kind_", p.Record.Kind)
	}

	return pluginRow{doc: doc, text: text, desc: desc}
}

// availableRow builds the row of a plugin shipped with tote that is not installed.
//
// Parameters:
//   - av: description of the embedded plugin
func (a *app) availableRow(av plugin.Available) pluginRow {
	return pluginRow{
		doc: pluginDoc{
			ID: av.ID, Title: av.Title, Version: av.Version, Tier: av.Tier,
			Kind: plugin.KindBundled, State: stateAvailable, Reasons: []reasonDoc{},
			Caps: orEmpty(av.Caps), Grant: av.Grant, Contributes: av.Contributes,
		},
		text: a.word("plugin_kind_", plugin.KindBundled),
		desc: av.Description,
	}
}

// pluginRows lists the installed plugins and the ones shipped with tote, by id.
//
// Parameters:
//   - reg: the registry
//
// Returns the rows, and the problems that concern every plugin.
func (a *app) pluginRows(reg *plugin.Registry) ([]pluginRow, []plugin.Reason) {
	snap := reg.Load()

	var rows []pluginRow

	for _, p := range snap.Plugins() {
		rows = append(rows, a.installedRow(p))
	}

	for _, av := range reg.Available(snap, a.tr().Lang()) {
		rows = append(rows, a.availableRow(av))
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].doc.ID < rows[j].doc.ID })

	return rows, snap.Problems
}

// firstLine gives the first line of a text, cut.
//
// Parameters:
//   - s: text, possibly on several lines
//   - n: most runes
func firstLine(s string, n int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")

	return plugin.CleanLine(line, n)
}

// pluginList implements "tote plugin [list] [--format table|tsv|json | --json]".
//
// Parameters:
//   - args: arguments after list, or after plugin
//
// Returns the exit code of the verb: 0 whatever the states of the plugins.
func (a *app) pluginList(args []string) int {
	v, ok, rc := a.pluginParse(args, []opt{{long: "format", value: true}}, 0, 0)
	if !ok {
		return rc
	}

	format, err := a.listFormat(v.o)
	if err != nil {
		if v.js {
			return a.usageJSON(err)
		}

		return a.usageErr(err, "plugin")
	}

	reg, err := a.pluginRegistry()
	if err != nil {
		return a.pluginFail(err, format == formatJSON)
	}

	rows, problems := a.pluginRows(reg)

	for _, pr := range problems {
		a.say("cli_plugin_problem", a.reasonText(pr))
	}

	return a.renderPluginList(rows, format)
}

// listFormat picks the output format of the plugin list, with the rules of the other lists.
//
// Parameters:
//   - o: parsed options
func (a *app) listFormat(o options) (string, error) {
	f := o.vals["format"]

	if o.set["json"] {
		if f != "" && f != formatJSON {
			return "", &optError{Key: "cli_format_conflict", Args: []any{sanitize(f)}}
		}

		return formatJSON, nil
	}

	switch f {
	case formatTable, formatTSV, formatJSON:
		return f, nil
	case "":
		if a.IsTerminal(a.Stdout) {
			return formatTable, nil
		}

		return formatTSV, nil
	}

	return "", &optError{Key: "cli_invalid_value", Args: []any{"format", f}}
}

// renderPluginList prints the rows in a format.
//
// Parameters:
//   - rows: plugins to show
//   - format: one of the format constants
func (a *app) renderPluginList(rows []pluginRow, format string) int {
	switch format {
	case formatJSON:
		docs := make([]any, len(rows))
		for i, r := range rows {
			docs[i] = r.doc
		}

		if err := writeJSON(a.Stdout, listDoc{noun: "plugins", records: docs}); err != nil {
			return a.fail(err)
		}
	case formatTSV:
		cells := make([][]string, len(rows))

		for i, r := range rows {
			raw := r.doc.Origin
			if raw == "" {
				raw = r.doc.Kind
			}

			cells[i] = []string{
				r.doc.ID, r.doc.Version, r.doc.State, raw,
				firstLine(r.desc, listDescRunes), strconv.FormatBool(r.doc.Enabled),
			}
		}

		writeTSV(a.Stdout, cells)
	default:
		heads := []string{
			a.t("header_name"), a.t("header_version"), a.t("header_state"),
			a.t("header_origin"), a.t("header_description"),
		}
		cells := make([][]string, len(rows))

		for i, r := range rows {
			cells[i] = []string{
				r.doc.ID, r.doc.Version, a.stateWord(r.doc.State), r.text,
				firstLine(r.desc, listDescRunes),
			}
		}

		writeTable(a.Stdout, heads, cells)
	}

	return exitOK
}

// requireDoc is the JSON form of a required program.
type requireDoc struct {
	Bin      string `json:"bin"`
	Hint     string `json:"hint,omitempty"`
	Optional bool   `json:"optional"`
	Found    bool   `json:"found"`
	Path     string `json:"path,omitempty"`
}

// settingDoc is the JSON form of a setting with its effective value.
type settingDoc struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Value  any    `json:"value"`
	Source string `json:"source"`
}

// pathsDoc is the JSON form of the places a plugin lives in.
type pathsDoc struct {
	Tree     string `json:"tree,omitempty"`
	Settings string `json:"settings,omitempty"`
	State    string `json:"state,omitempty"`
	Cache    string `json:"cache,omitempty"`
	Host     string `json:"host,omitempty"`
}

// hashesDoc is the JSON form of the hashes on record.
type hashesDoc struct {
	Archive string `json:"archive_sha256,omitempty"`
	Tree    string `json:"tree_sha256,omitempty"`
	Surface string `json:"surface_sha256,omitempty"`
}

// failureDoc is the JSON form of the last failure of a plugin.
type failureDoc struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	At     string `json:"at"`
}

// infoDoc is the JSON form of "plugin info": the list object, then the details.
type infoDoc struct {
	Schema int  `json:"schema"`
	OK     bool `json:"ok"`
	pluginDoc
	Description string         `json:"description"`
	Items       []plugin.Piece `json:"contributions"`
	License     string         `json:"license,omitempty"`
	Homepage    string         `json:"homepage,omitempty"`
	Source      string         `json:"source,omitempty"`
	Granted     []string       `json:"granted_caps"`
	Requires    []requireDoc   `json:"requires"`
	Settings    []settingDoc   `json:"settings"`
	Paths       pathsDoc       `json:"paths"`
	Hashes      hashesDoc      `json:"hashes"`
	Failure     *failureDoc    `json:"last_failure,omitempty"`
	Warnings    []reasonDoc    `json:"warnings"`
}

// pluginInfo implements "tote plugin info <id> [--json]".
//
// Parameters:
//   - args: arguments after info
//
// Returns the exit code of the verb: 0, or 1 when the plugin is neither installed nor shipped
// with tote.
func (a *app) pluginInfo(args []string) int {
	v, ok, rc := a.pluginParse(args, nil, 1, 1, "arg_id")
	if !ok {
		return rc
	}

	id := v.o.pos[0]

	reg, err := a.pluginRegistry()
	if err != nil {
		return a.pluginFail(err, v.js)
	}

	snap := reg.Load()

	if p, found := snap.Get(id); found {
		return a.printInfo(a.installedInfo(reg, p), v.js, a.installedLines)
	}

	for _, av := range reg.Available(snap, a.tr().Lang()) {
		if av.ID == id {
			doc := infoDoc{
				Schema: pluginSchema, OK: true, pluginDoc: a.availableRow(av).doc,
				Description: av.Description, Items: orNoItems(av.Items), Granted: []string{},
				Requires: []requireDoc{}, Settings: []settingDoc{}, Warnings: []reasonDoc{},
			}

			return a.printInfo(doc, v.js, a.availableLines)
		}
	}

	return a.pluginFail(&plugin.Error{Code: plugin.CodeNotInstalled, Args: []string{id}}, v.js)
}

// printInfo prints the details of a plugin.
//
// Parameters:
//   - doc: details
//   - js: true for JSON
//   - lines: writes the human form
func (a *app) printInfo(doc infoDoc, js bool, lines func(infoDoc)) int {
	if js {
		if err := writeJSON(a.Stdout, doc); err != nil {
			return a.fail(err)
		}

		return exitOK
	}

	lines(doc)

	return exitOK
}

// installedInfo gathers the details of an installed plugin.
//
// Parameters:
//   - reg: the registry
//   - p: the plugin
func (a *app) installedInfo(reg *plugin.Registry, p plugin.Plugin) infoDoc {
	row := a.installedRow(p)
	rec := p.Record

	doc := infoDoc{
		Schema: pluginSchema, OK: true, pluginDoc: row.doc, Description: row.desc,
		Items:  orNoItems(plugin.PiecesOf(p.Manifest, p.Catalog, a.tr().Lang())),
		Source: rec.Source, Granted: orEmpty(rec.Caps), Requires: []requireDoc{},
		Settings: []settingDoc{}, Warnings: a.reasonDocs(p.Warnings),
		Hashes: hashesDoc{
			Archive: rec.ArchiveSHA256, Tree: rec.TreeSHA256,
			Surface: rec.SurfaceSHA256,
		},
	}

	if rec.Failures > 0 || rec.LastReason != "" {
		doc.Failure = &failureDoc{
			Reason: rec.LastReason, Count: rec.Failures,
			At: rec.LastFailureAt,
		}
	}

	if m := p.Manifest; m != nil {
		doc.License, doc.Homepage = sanitize(m.License), sanitize(m.Homepage)
		doc.Requires = a.requirements(p)
		doc.Settings = a.settingDocs(reg, p)
		doc.Warnings = append(doc.Warnings, a.settingWarnings(reg.Settings(p))...)
	}

	doc.Paths = a.pluginPaths(p)

	return doc
}

// orNoItems gives a list that JSON prints as [] and not as null.
//
// Parameters:
//   - items: contributions that may be nil
func orNoItems(items []plugin.Piece) []plugin.Piece {
	if items == nil {
		return []plugin.Piece{}
	}

	return items
}

// settingWarnings reports what the plugin layer ignored in the settings of a plugin: values that
// were rejected, keys it does not know and a file it could not read. They are the same findings
// that "plugin check" shows.
//
// Parameters:
//   - view: settings of the plugin as the plugin layer read them
func (a *app) settingWarnings(view plugin.SettingsView) []reasonDoc {
	var fs []plugin.Finding

	for _, sv := range view.Values {
		for _, rej := range sv.Rejected {
			fs = append(fs, plugin.Finding{
				Scope: plugin.ScopeSettings,
				Code:  plugin.CodeSettingsIgnore, Args: []string{sv.ID, rej.Source, rej.Code},
			})
		}
	}

	if view.Problem != nil {
		fs = append(fs, plugin.Finding{
			Scope: plugin.ScopeSettings,
			Code:  plugin.CodeSettingsFile, Args: []string{view.Problem.Code},
		})
	}

	for _, key := range view.Unknown {
		fs = append(fs, plugin.Finding{
			Scope: plugin.ScopeSettings,
			Code:  plugin.CodeSettingsKey, Args: []string{key},
		})
	}

	out := make([]reasonDoc, 0, len(fs))
	for _, f := range fs {
		out = append(out, reasonDoc{
			Code: f.Code, Args: orEmpty(f.Args),
			Message: a.findingText(f),
		})
	}

	return out
}

// requirements lists the required programs of a plugin and whether PATH has them.
//
// Parameters:
//   - p: plugin with a manifest
func (a *app) requirements(p plugin.Plugin) []requireDoc {
	out := make([]requireDoc, 0, len(p.Manifest.Requires))

	for _, rq := range p.Manifest.Requires {
		d := requireDoc{
			Bin: rq.Bin, Optional: rq.Optional,
			Hint: p.Catalog.Text(a.tr().Lang(), "requires."+rq.Bin+".hint", rq.Hint),
		}

		d.Path, d.Found = plugin.LookBin(a.Getenv("PATH"), rq.Bin, plugin.Avoid{})
		out = append(out, d)
	}

	return out
}

// settingDocs lists the effective settings of a plugin.
//
// Parameters:
//   - reg: the registry
//   - p: plugin with a manifest
func (a *app) settingDocs(reg *plugin.Registry, p plugin.Plugin) []settingDoc {
	out := []settingDoc{}

	for _, sv := range reg.Settings(p).Values {
		out = append(out, settingDoc{ID: sv.ID, Type: sv.Type, Value: sv.Value, Source: sv.Source})
	}

	return out
}

// pluginPaths gives the places a plugin lives in.
//
// Parameters:
//   - p: the plugin
func (a *app) pluginPaths(p plugin.Plugin) pathsDoc {
	d := pathsDoc{Tree: p.Dir}

	paths, err := a.cfgPaths()
	if err != nil {
		return d
	}

	d.Settings, _ = paths.PluginSettingsFile(p.ID)
	d.State, _ = paths.PluginStateDir(p.ID)
	d.Cache, _ = paths.PluginCacheDir(p.ID)
	d.Host, _ = paths.PluginHostDir(p.ID)

	return d
}

// fieldLabel gives the start of a detail line: the name of the field and its punctuation.
//
// Parameters:
//   - key: catalog key of the name
func (a *app) fieldLabel(key string) string {
	return a.t(key) + a.colon() + " "
}

// field prints one detail line when its value is not empty.
//
// Parameters:
//   - key: catalog key of the name of the field
//   - value: its value
func (a *app) field(key, value string) {
	if value != "" {
		fmt.Fprintln(a.Stdout, a.fieldLabel(key)+sanitize(value))
	}
}

// listLines prints a titled list of values, or the word none.
//
// Parameters:
//   - key: catalog key of the title
//   - items: values, already clean
func (a *app) listLines(key string, items []string) {
	if len(items) == 0 {
		fmt.Fprintln(a.Stdout, a.fieldLabel(key)+a.t("cli_plugin_none"))

		return
	}

	fmt.Fprintln(a.Stdout, a.t(key)+a.colon())

	for _, it := range items {
		fmt.Fprintln(a.Stdout, "  - "+sanitize(it))
	}
}

// installedLines prints the details of an installed plugin.
//
// Parameters:
//   - d: details
func (a *app) installedLines(d infoDoc) {
	a.headLine(d)
	a.field("cli_plugin_label_description", firstLine(d.Description, 200))
	a.field("cli_plugin_label_tier", a.word("plugin_tier_", d.Tier))
	a.field("cli_plugin_label_kind", a.word("plugin_kind_", d.Kind))
	a.field("cli_plugin_label_source", d.Source)
	a.field("cli_plugin_label_origin", d.Origin)
	fmt.Fprintln(a.Stdout, a.fieldLabel("cli_plugin_label_state")+a.stateWord(d.State))
	a.stateReasons(d)
	a.field("cli_plugin_label_license", d.License)
	a.field("cli_plugin_label_homepage", d.Homepage)
	a.listLines("cli_plugin_label_declared", d.Caps)
	a.listLines("cli_plugin_label_granted", d.Granted)
	a.field("cli_plugin_label_grant", d.Grant)
	a.requireLines(d.Requires)
	a.itemLines(d.Items)
	a.settingLines(d.Settings)
	a.pathLines(d.Paths)
	a.hashLines(d.Hashes)

	if d.Failure != nil {
		line := a.t("cli_plugin_failure_line", a.word("plugin_fail_", d.Failure.Reason),
			strconv.Itoa(d.Failure.Count), d.Failure.At)
		fmt.Fprintln(a.Stdout, a.fieldLabel("cli_plugin_label_failure")+line)
	}
}

// availableLines prints the details of a plugin shipped with tote that is not installed.
//
// Parameters:
//   - d: details
func (a *app) availableLines(d infoDoc) {
	a.headLine(d)
	a.field("cli_plugin_label_description", firstLine(d.Description, 200))
	a.field("cli_plugin_label_tier", a.word("plugin_tier_", d.Tier))
	a.field("cli_plugin_label_kind", a.word("plugin_kind_", d.Kind))
	fmt.Fprintln(a.Stdout, a.fieldLabel("cli_plugin_label_state")+a.stateWord(d.State))
	a.listLines("cli_plugin_label_declared", d.Caps)
	a.field("cli_plugin_label_grant", d.Grant)
	a.itemLines(d.Items)
	fmt.Fprintln(a.Stdout, a.t("cli_plugin_available", d.ID))
}

// headLine prints the first line of the details: the id, the version and the title.
//
// Parameters:
//   - d: details
func (a *app) headLine(d infoDoc) {
	line := d.ID + " " + d.Version
	if d.Title != "" {
		line += " - " + d.Title
	}

	fmt.Fprintln(a.Stdout, sanitize(line))
}

// stateReasons prints the reasons of a state that is not enabled, with the way out.
//
// Parameters:
//   - d: details
func (a *app) stateReasons(d infoDoc) {
	for _, r := range d.Reasons {
		fmt.Fprintln(a.Stdout, "  - "+sanitize(r.Message))
	}

	if fix, ok := a.fixText(d.State, d.ID); ok && len(d.Reasons) > 0 {
		fmt.Fprintln(a.Stdout, "  "+a.t("cli_plugin_fix", fix))
	}

	for _, w := range d.Warnings {
		fmt.Fprintln(a.Stdout, "  ! "+sanitize(w.Message))
	}
}

// requireLines prints the required programs.
//
// Parameters:
//   - rs: requirements
func (a *app) requireLines(rs []requireDoc) {
	if len(rs) == 0 {
		return
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_label_requires")+a.colon())

	for _, r := range rs {
		switch {
		case r.Found:
			fmt.Fprintln(a.Stdout, "  - "+a.t("cli_plugin_req_found", sanitize(r.Bin),
				sanitize(r.Path)))
		case r.Optional:
			fmt.Fprintln(a.Stdout, "  - "+a.t("cli_plugin_req_optional", sanitize(r.Bin)))
		default:
			fmt.Fprintln(a.Stdout, "  - "+a.t("cli_plugin_req_missing", sanitize(r.Bin)))
		}

		if !r.Found && r.Hint != "" {
			fmt.Fprintln(a.Stdout, "    "+a.t("plugin_hint_label", sanitize(r.Hint)))
		}
	}
}

// itemLines prints what a plugin adds to tote: the table, the id and the label of each entry.
//
// Parameters:
//   - items: contributions of the plugin
func (a *app) itemLines(items []plugin.Piece) {
	if len(items) == 0 {
		fmt.Fprintln(a.Stdout,
			a.fieldLabel("cli_plugin_label_contributions")+a.t("cli_plugin_none"))

		return
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_label_contributions")+a.colon())

	for _, it := range items {
		table := a.word("plugin_table_", it.Table)

		if it.Label == "" {
			fmt.Fprintln(a.Stdout, "  - "+a.t("cli_plugin_item_bare", table, sanitize(it.ID)))

			continue
		}

		fmt.Fprintln(a.Stdout, "  - "+a.t("cli_plugin_item_line", table, sanitize(it.ID),
			sanitize(it.Label)))
	}
}

// settingLines prints the effective settings.
//
// Parameters:
//   - ss: settings
func (a *app) settingLines(ss []settingDoc) {
	if len(ss) == 0 {
		return
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_label_settings")+a.colon())

	for _, s := range ss {
		fmt.Fprintln(a.Stdout, "  - "+a.t("cli_plugin_setting_line", sanitize(s.ID),
			sanitize(s.Type), sanitize(valueText(s.Value)), a.word("plugin_source_", s.Source)))
	}
}

// valueText writes the value of a setting.
//
// Parameters:
//   - v: string, bool, int64 or list of strings
func valueText(v any) string {
	switch t := v.(type) {
	case []string:
		return strings.Join(t, ", ")
	case string:
		return t
	}

	return fmt.Sprint(v)
}

// pathLines prints the places a plugin lives in.
//
// Parameters:
//   - p: the places
func (a *app) pathLines(p pathsDoc) {
	fmt.Fprintln(a.Stdout, a.t("cli_plugin_label_paths")+a.colon())

	for _, kv := range [][2]string{
		{"cli_plugin_label_tree_dir", p.Tree},
		{"cli_plugin_label_settings_file", p.Settings},
		{"cli_plugin_label_state_dir", p.State},
		{"cli_plugin_label_cache_dir", p.Cache},
		{"cli_plugin_label_host_dir", p.Host},
	} {
		if kv[1] != "" {
			fmt.Fprintln(a.Stdout, "  "+a.fieldLabel(kv[0])+sanitize(kv[1]))
		}
	}
}

// hashLines prints the hashes on record.
//
// Parameters:
//   - h: the hashes
func (a *app) hashLines(h hashesDoc) {
	fmt.Fprintln(a.Stdout, a.t("cli_plugin_label_hashes")+a.colon())

	for _, kv := range [][2]string{
		{"cli_plugin_label_archive_sha256", h.Archive},
		{"cli_plugin_label_tree_sha256", h.Tree},
		{"cli_plugin_label_surface_sha256", h.Surface},
	} {
		if kv[1] != "" {
			fmt.Fprintln(a.Stdout, "  "+a.fieldLabel(kv[0])+kv[1])
		}
	}
}
