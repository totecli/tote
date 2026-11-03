package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// openSettings shows the form of the settings of a plugin.
//
// Parameters:
//   - p: the plugin as it was just read
func (r *root) openSettings(p plugin.Plugin) tea.Cmd {
	e := r.e
	if p.Manifest == nil || len(p.Manifest.Settings) == 0 {
		return r.say(e.t("ui_plug_no_settings", sanitize(p.ID)), false)
	}

	return r.push(newSettingsForm(e, p))
}

// newSettingsForm returns the form that edits the settings of a plugin. Each [[setting]] is a
// field of the generic form; a list is edited in the list editor. The form writes only through
// the registry, which edits the keys that changed in the settings file and nothing else.
//
// A setting that an environment variable sets is shown as it is and cannot be changed here. A
// value of the file that was ignored is said under its field, and a settings file that cannot be
// used, or has a key no setting declares, is said at the bottom of the form.
//
// Parameters:
//   - e: shared environment, which gives the registry
//   - p: the plugin as it was just read, with a manifest
func newSettingsForm(e *env, p plugin.Plugin) *fieldsForm {
	view := e.Plugins.Settings(p)
	seen := map[string]plugin.SettingValue{}

	for _, sv := range view.Values {
		seen[sv.ID] = sv
	}

	specs := make([]fieldSpec, 0, len(p.Manifest.Settings))
	for _, s := range p.Manifest.Settings {
		specs = append(specs, settingSpec(e, p, s, seen[s.ID]))
	}

	host := e.Plugins
	id := sanitize(p.ID)

	form := newFieldsForm(e.t("ui_plug_settings_title", id), specs,
		e.t("ui_plug_settings_saved", id), settingsBanner(e, view),
		func(v fieldValues) string {
			if err := host.SaveSettings(p, settingValues(p.Manifest.Settings, v)); err != nil {
				return e.t("ui_plug_set_notsaved", e.errorText(err))
			}

			return ""
		})

	// The labels of a plugin are sentences of up to sixty characters: give them the wide column.
	form.wide = true

	return form
}

// settingsBanner words what is wrong with the settings file as a whole.
//
// Parameters:
//   - e: shared environment
//   - view: the settings as the registry read them
func settingsBanner(e *env, view plugin.SettingsView) string {
	switch {
	case view.Problem != nil:
		return e.findingText(plugin.Finding{
			Scope: plugin.ScopeSettings,
			Code:  plugin.CodeSettingsFile, Args: []string{view.Problem.Code},
		})
	case len(view.Unknown) > 0:
		return e.findingText(plugin.Finding{
			Scope: plugin.ScopeSettings,
			Code:  plugin.CodeSettingsKey, Args: []string{view.Unknown[0]},
		})
	}

	return ""
}

// settingSpec describes the field of one setting.
//
// Parameters:
//   - e: shared environment
//   - p: the plugin, for its catalog
//   - s: the setting
//   - sv: its effective value
func settingSpec(e *env, p plugin.Plugin, s plugin.Setting, sv plugin.SettingValue) fieldSpec {
	key := "setting." + s.ID
	home := e.Home

	spec := fieldSpec{
		ID:    s.ID,
		Label: e.ptext(p.Catalog, key+".label", s.Label),
		Hint:  e.ptext(p.Catalog, key+".description", s.Description),
		Value: plugin.SettingText(sv.Value),
		Max:   s.MaxLen,
		Note:  settingNote(e, s.ID, sv),
	}

	if sv.Source == plugin.SourceEnvironment {
		spec.Locked = e.t("ui_plug_set_env", plugin.SettingEnvName(p.ID, s.ID))
	}

	switch s.Type {
	case "bool":
		spec.Type = fieldToggle
	case "enum":
		spec.Type, spec.Choices = fieldChoice, s.Choices

		for _, c := range s.Choices {
			spec.Names = append(spec.Names, e.ptext(p.Catalog, key+".choice."+c, c))
		}
	case "list":
		spec.Type = fieldList
		spec.Items, _ = sv.Value.([]string)
		spec.Check = func(e *env, item string) string {
			return e.pword("plugin_reject_", plugin.CheckSettingItem(s, item))
		}
		spec.CheckList = func(e *env, items []string) string {
			return rejectText(e, s, plugin.CheckSettingItems(s, items))
		}
	default:
		spec.Placeholder = plugin.SettingText(plugin.SettingDefault(s))
		spec.Check = func(e *env, raw string) string {
			if raw == "" {
				return ""
			}

			_, code := plugin.CheckSettingText(s, home, raw)

			return rejectText(e, s, code)
		}
	}

	return spec
}

// rejectText words the code of a rule that a value of a setting breaks. The range and the number
// of items say their bound.
//
// Parameters:
//   - e: shared environment
//   - s: the setting
//   - code: one of the Reject constants, "" for none
func rejectText(e *env, s plugin.Setting, code string) string {
	switch {
	case code == "":
		return ""
	case code == plugin.RejectRange && s.HasMin && s.HasMax:
		return e.t("ui_rule_range_between", s.Min, s.Max)
	case code == plugin.RejectRange && s.HasMin:
		return e.t("ui_rule_range_min", s.Min)
	case code == plugin.RejectRange && s.HasMax:
		return e.t("ui_rule_range_max", s.Max)
	case code == plugin.RejectItems && s.MaxItems > 0:
		return e.t("ui_rule_items_max", s.MaxItems)
	}

	return e.pword("plugin_reject_", code)
}

// settingNote says that a value of higher precedence than the one shown was ignored.
//
// Parameters:
//   - e: shared environment
//   - id: setting id
//   - sv: the effective value, which lists what was ignored
func settingNote(e *env, id string, sv plugin.SettingValue) string {
	if len(sv.Rejected) == 0 {
		return ""
	}

	return ignoredText(e, id, sv.Rejected[0])
}

// ignoredText tells that a value was ignored, why, and the value itself.
//
// Parameters:
//   - e: shared environment
//   - id: setting id
//   - r: the value that was ignored
func ignoredText(e *env, id string, r plugin.SettingRejection) string {
	text := e.findingText(plugin.Finding{
		Scope: plugin.ScopeSettings,
		Code:  plugin.CodeSettingsIgnore, Args: []string{id, r.Source, r.Code},
	})

	if raw := sanitize(r.Raw); raw != "" {
		text += " " + e.t("ui_plug_rejected_value", e.Metric.fit(raw, rejectedWidth,
			e.Glyphs.ellipsis))
	}

	return text
}

// rejectedWidth is the most cells of an ignored value that are shown next to the reason.
const rejectedWidth = 24

// settingValues turns the values of the form into the values to write: the settings the user
// changed, each in the type of its setting. An empty text gives the default back.
//
// Parameters:
//   - settings: the settings of the manifest
//   - v: the values of the form
func settingValues(settings []plugin.Setting, v fieldValues) map[string]any {
	out := map[string]any{}

	for _, s := range settings {
		if !v.Changed(s.ID) {
			continue
		}

		raw := v.Text(s.ID)

		switch {
		case s.Type == "bool":
			out[s.ID] = raw == "true"
		case s.Type == "list":
			out[s.ID] = v.Items(s.ID)
		case raw == "":
			out[s.ID] = plugin.SettingDefault(s)
		case s.Type == "int":
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				out[s.ID] = raw

				continue
			}

			out[s.ID] = n
		default:
			out[s.ID] = raw
		}
	}

	return out
}
