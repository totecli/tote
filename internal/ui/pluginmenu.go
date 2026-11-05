package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// newPluginMenu returns the list of the actions that apply to a plugin in its present state. The
// menu is read from the files when it opens, so it never offers an action the state refuses.
//
// Parameters:
//   - e: shared environment, which gives the registry
//   - id: plugin id
func newPluginMenu(e *env, id string) overlay {
	items := []pickItem{{label: e.t("ui_plug_menu_details"), value: strconv.Itoa(int(actDetails))}}
	add := func(act pluginAct, key string) {
		items = append(items, pickItem{label: e.t(key), value: strconv.Itoa(int(act))})
	}

	seen := ""

	if p, ok := e.Plugins.Recheck(id); ok {
		installedMenu(p, add)

		seen = string(p.State)
	} else {
		add(actInstall, "ui_plug_menu_install")
		add(actCheck, "ui_plug_menu_check")
	}

	return newPick(sanitize(id), items, 0, func(_ *env, value string) tea.Cmd {
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil
		}

		return msgCmd(pluginActMsg{Act: pluginAct(n), ID: id, Seen: seen})
	})
}

// installedMenu lists the actions of an installed plugin, after the details.
//
// Parameters:
//   - p: the plugin as it was just read
//   - add: appends an entry to the menu
func installedMenu(p plugin.Plugin, add func(pluginAct, string)) {
	switch p.State {
	case plugin.StateEnabled, plugin.StateMissingDependency:
		add(actToggle, "ui_plug_menu_disable")
	case plugin.StateDisabled:
		add(actToggle, "ui_plug_menu_enable")
	case plugin.StateNeedsConsent:
		add(actAllow, "ui_plug_menu_allow")
		add(actToggle, "ui_plug_menu_enable")
	case plugin.StateQuarantined:
		add(actClear, "ui_plug_menu_clear")
	}

	if p.Manifest != nil && len(p.Manifest.Settings) > 0 {
		add(actSettings, "ui_plug_menu_settings")
	}

	if p.Record.Kind == plugin.KindLocal || p.Record.Kind == plugin.KindBundled {
		add(actUpdate, "ui_plug_menu_update")
	}

	add(actCheck, "ui_plug_menu_check")
	add(actRemove, "ui_plug_menu_remove")
	add(actPurge, "ui_plug_menu_purge")
}

// newPluginInfo returns the overlay that shows everything about a plugin, installed or shipped
// with tote.
//
// Parameters:
//   - e: shared environment, which gives the registry
//   - id: plugin id
func newPluginInfo(e *env, id string) overlay {
	if p, ok := e.Plugins.Recheck(id); ok {
		title, fields, warn := installedFields(e, p)

		return &infoOverlay{title: title, fields: fields, warn: warn}
	}

	snap := e.Plugins.Load()

	for _, a := range e.Plugins.Available(snap, e.Tr.Lang()) {
		if a.ID == id {
			return &infoOverlay{
				title:  headOf(a.ID, a.Version, a.Title),
				fields: availableFields(e, a),
			}
		}
	}

	return &infoOverlay{title: sanitize(id), fields: []infoField{{
		label: e.t("cli_plugin_label_state"), value: e.codeText(plugin.CodeNotInstalled,
			[]string{id}, nil),
	}}}
}

// headOf joins the id, the version and the title of a plugin into the title of a box.
//
// Parameters:
//   - id: plugin id
//   - version: version, may be empty
//   - title: title in the language of the interface, may be empty
func headOf(id, version, title string) string {
	line := id
	if version != "" {
		line += " " + version
	}

	if title != "" {
		line += " - " + title
	}

	return sanitize(line)
}

// fieldSink collects the lines of the details of a plugin.
type fieldSink struct {
	e   *env
	out []infoField
}

// add appends a line when its value is not empty.
//
// Parameters:
//   - label: catalog key of the label, or "" for a line that continues the one above
//   - value: the value
func (f *fieldSink) add(label, value string) {
	if value == "" {
		return
	}

	text := ""
	if label != "" {
		text = f.e.t(label)
	}

	f.out = append(f.out, infoField{label: text, value: sanitize(value)})
}

// list appends a titled list: the first value on the line of the label and the others under it.
// An empty list gives the word none.
//
// Parameters:
//   - label: catalog key of the label
//   - values: the values, already clean
func (f *fieldSink) list(label string, values []string) {
	if len(values) == 0 {
		f.add(label, f.e.t("cli_plugin_none"))

		return
	}

	for i, v := range values {
		if i == 0 {
			f.add(label, v)

			continue
		}

		f.add("", v)
	}
}

// installedFields lists the lines of the details of an installed plugin.
//
// Parameters:
//   - e: shared environment
//   - p: the plugin as it was just read
//
// Returns the title of the box, the lines and the warning to show under them.
func installedFields(e *env, p plugin.Plugin) (string, []infoField, string) {
	f := &fieldSink{e: e}
	lang := e.Tr.Lang()
	title := p.Header.Title

	if p.Manifest != nil {
		title = e.ptext(p.Catalog, "title", p.Manifest.Title)
		f.add("cli_plugin_label_description", firstLine(e.ptext(p.Catalog, "description",
			p.Manifest.Description)))
	}

	f.add("cli_plugin_label_tier", e.pword("plugin_tier_", p.Record.Tier))
	f.add("cli_plugin_label_kind", e.pword("plugin_kind_", p.Record.Kind))
	f.add("cli_plugin_label_source", p.Record.Source)
	f.add("cli_plugin_label_origin", p.Record.Origin)
	f.add("cli_plugin_label_state", e.pstate(string(p.State)))
	stateLines(e, f, p)

	if p.Manifest != nil {
		f.add("cli_plugin_label_license", p.Manifest.License)
		f.add("cli_plugin_label_homepage", p.Manifest.Homepage)
	}

	f.list("cli_plugin_label_declared", p.Caps)

	if p.Record.Consented() {
		f.list("cli_plugin_label_granted", p.Record.Caps)
	}

	requireLines(e, f, p)
	pieceLines(e, f, plugin.PiecesOf(p.Manifest, p.Catalog, lang))
	settingLines(e, f, p)

	if p.Record.Failures > 0 {
		f.add("cli_plugin_label_failure", e.t("cli_plugin_failure_line",
			e.pword("plugin_fail_", p.Record.LastReason), strconv.Itoa(p.Record.Failures),
			sanitize(p.Record.LastFailureAt)))
	}

	return headOf(p.ID, p.Record.Version, title), f.out, ""
}

// stateLines adds the reasons of a state that is not enabled, the way out and the warnings.
//
// Parameters:
//   - e: shared environment
//   - f: the lines so far
//   - p: the plugin
func stateLines(e *env, f *fieldSink, p plugin.Plugin) {
	for _, r := range p.Reasons {
		f.add("", "- "+e.reasonText(r))
	}

	if fix, ok := e.pfix(string(p.State), p.ID); ok && len(p.Reasons) > 0 {
		f.add("", e.t("cli_plugin_fix", fix))
	}

	for _, w := range p.Warnings {
		f.add("", e.Glyphs.warn+" "+e.reasonText(w))
	}
}

// requireLines adds the programs a plugin needs and whether PATH has them. The hint of a program
// that is missing is the plugin's own words and is labelled so.
//
// Parameters:
//   - e: shared environment
//   - f: the lines so far
//   - p: the plugin
func requireLines(e *env, f *fieldSink, p plugin.Plugin) {
	if p.Manifest == nil || len(p.Manifest.Requires) == 0 {
		return
	}

	missing := map[string]bool{}
	for _, m := range append(append([]plugin.Requires(nil), p.Missing...), p.Absent...) {
		missing[m.Bin] = true
	}

	label := "cli_plugin_label_requires"

	for _, rq := range p.Manifest.Requires {
		switch {
		case !missing[rq.Bin]:
			f.add(label, "- "+rq.Bin)
		case rq.Optional:
			f.add(label, "- "+e.t("cli_plugin_req_optional", sanitize(rq.Bin)))
		default:
			f.add(label, "- "+e.t("cli_plugin_req_missing", sanitize(rq.Bin)))
		}

		label = ""

		hint := e.ptext(p.Catalog, "requires."+rq.Bin+".hint", rq.Hint)
		if missing[rq.Bin] && hint != "" {
			f.add("", "  "+e.t("plugin_hint_label", sanitize(hint)))
		}
	}
}

// pieceLines adds what a plugin contributes.
//
// Parameters:
//   - e: shared environment
//   - f: the lines so far
//   - items: the contributions
func pieceLines(e *env, f *fieldSink, items []plugin.Piece) {
	lines := make([]string, 0, len(items))

	for _, it := range items {
		table := e.pword("plugin_table_", it.Table)

		if it.Label == "" {
			lines = append(lines, "- "+e.t("cli_plugin_item_bare", table, sanitize(it.ID)))

			continue
		}

		lines = append(lines, "- "+e.t("cli_plugin_item_line", table, sanitize(it.ID),
			sanitize(it.Label)))
	}

	f.list("cli_plugin_label_contributions", lines)
}

// settingLines adds the effective settings of a plugin with the place each value comes from.
//
// Parameters:
//   - e: shared environment
//   - f: the lines so far
//   - p: the plugin
func settingLines(e *env, f *fieldSink, p plugin.Plugin) {
	if p.Manifest == nil || len(p.Manifest.Settings) == 0 {
		return
	}

	label := "cli_plugin_label_settings"
	view := e.Plugins.Settings(p)

	var warnings []string

	for _, sv := range view.Values {
		f.add(label, "- "+e.t("cli_plugin_setting_line", sanitize(sv.ID), sanitize(sv.Type),
			sanitize(plugin.SettingText(sv.Value)), e.pword("plugin_source_", sv.Source)))

		label = ""

		for _, rej := range sv.Rejected {
			warnings = append(warnings, ignoredText(e, sv.ID, rej))
		}
	}

	if view.Problem != nil {
		warnings = append(warnings, e.findingText(plugin.Finding{
			Scope: plugin.ScopeSettings,
			Code:  plugin.CodeSettingsFile, Args: []string{view.Problem.Code},
		}))
	}

	for _, key := range view.Unknown {
		warnings = append(warnings, e.findingText(plugin.Finding{
			Scope: plugin.ScopeSettings,
			Code:  plugin.CodeSettingsKey, Args: []string{sanitize(key)},
		}))
	}

	if len(warnings) > 0 {
		f.list("ui_plug_label_warnings", warnings)
	}
}

// availableFields lists the lines of the details of a plugin shipped with tote.
//
// Parameters:
//   - e: shared environment
//   - a: the plugin
func availableFields(e *env, a plugin.Available) []infoField {
	f := &fieldSink{e: e}

	f.add("cli_plugin_label_description", firstLine(a.Description))
	f.add("cli_plugin_label_tier", e.pword("plugin_tier_", a.Tier))
	f.add("cli_plugin_label_kind", e.pword("plugin_kind_", "bundled"))
	f.add("cli_plugin_label_state", e.pstate(stateAvailable))
	f.list("cli_plugin_label_declared", a.Caps)
	pieceLines(e, f, a.Items)
	f.add("", e.t("cli_plugin_available_hint", sanitize(a.ID)))

	return f.out
}

// newCheckReport returns the overlay that shows what the examination of a plugin found.
//
// Parameters:
//   - e: shared environment
//   - res: the result of the check
func newCheckReport(e *env, res *plugin.CheckResult) overlay {
	f := &fieldSink{e: e}
	name := cleanName(res)

	switch errs, warns := res.Errors(), res.Warnings(); {
	case errs == 0 && warns == 0:
		f.add("ui_plug_verdict", e.t("cli_plugin_check_ok", name))
	default:
		f.add("ui_plug_verdict", e.t("cli_plugin_check_summary", name, errs, warns))
	}

	for _, fd := range res.Findings {
		word := e.t("cli_plugin_severity_warning")
		if fd.Severity == plugin.SeverityError {
			word = e.t("cli_plugin_severity_error")
		}

		f.out = append(f.out, infoField{label: word, value: sanitize(e.findingText(fd))})
	}

	label := "cli_plugin_label_requires"

	for _, rq := range res.Requires {
		switch {
		case rq.Found:
			f.add(label, e.t("cli_plugin_req_found", sanitize(rq.Bin), sanitize(rq.Path)))
		case rq.Optional:
			f.add(label, e.t("cli_plugin_req_optional", sanitize(rq.Bin)))
		default:
			f.add(label, e.t("cli_plugin_req_missing", sanitize(rq.Bin)))
		}

		label = ""

		if !rq.Found && rq.Hint != "" {
			f.add("", "  "+e.t("plugin_hint_label", sanitize(rq.Hint)))
		}
	}

	lines := make([]string, 0, len(res.Contributions))

	for _, c := range res.Contributions {
		lines = append(lines, "- "+e.t("cli_plugin_item_line", e.pword("plugin_table_", c.Table),
			sanitize(c.ID), sanitize(c.Display)))
	}

	if len(lines) > 0 {
		f.list("cli_plugin_label_contributions", lines)
	}

	warn := ""
	if res.Failed() {
		warn = e.t("cli_plugin_check_summary", name, res.Errors(), res.Warnings())
	}

	return &infoOverlay{title: headOf(res.ID, res.Version, res.Title), fields: f.out, warn: warn}
}

// cleanName gives the name a check result is shown under: the plugin id, else the target.
//
// Parameters:
//   - res: the result
func cleanName(res *plugin.CheckResult) string {
	name := res.ID
	if name == "" {
		name = res.Target
	}

	return sanitize(strings.TrimSpace(name))
}
