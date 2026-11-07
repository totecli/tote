package plugin

import (
	"errors"
	"reflect"
	"sort"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/store"
)

// SaveSettings writes the values of some settings of an installed plugin to its settings file,
// plugins/<id>.toml, in one atomic edit of the keys that change. Comments, unknown keys and the
// layout of the rest of the file stay as they are.
//
// A value that equals the default of its setting removes the key instead of writing it, so that a
// later default of the plugin applies again. Every value is checked against its setting here, not
// in the caller: the type, the limits, the pattern, the choices and the path rules decide, and
// nothing is written when one is refused. The file is the user's, the plugin never writes it.
//
// Parameters:
//   - p: the plugin as one Load saw it, the one whose settings were shown; the write is refused
//     with stale when its files are no longer the ones that p was read from
//   - values: the new value of each setting to change, by setting id: a string, a bool, an int64
//     or a []string according to its type. A setting that is not listed is not touched
//
// Returns nil, or an *Error: bad_id, from_plugin, not_installed, stale (the plugin changed or was
// removed), input_invalid (the setting and the code of the rule it breaks), settings_file when
// the file cannot be edited (its code is a Reject constant), busy when another tote holds the
// file, or disk.
func (r *Registry) SaveSettings(p Plugin, values map[string]any) error {
	if err := config.ValidPluginID(p.ID); err != nil {
		return wrapError(CodeBadID, err, p.ID)
	}

	if err := r.guardChange(); err != nil {
		return err
	}

	cur, ok := r.Recheck(p.ID)

	switch {
	case !ok || !cur.Installed:
		return newError(CodeNotInstalled, p.ID)
	case cur.Manifest == nil || cur.Record.TreeSHA256 != p.Record.TreeSHA256:
		return newError(CodeStale, p.ID)
	}

	file, err := r.opts.Paths.PluginSettingsFile(p.ID)
	if err != nil {
		return wrapError(CodeBadID, err, p.ID)
	}

	set, unset, perr := r.settingsEdit(cur, values)
	if perr != nil {
		return perr
	}

	switch werr := store.PatchTable(file, "", set, unset); {
	case werr == nil:
		return nil
	case errors.Is(werr, store.ErrBusy):
		return wrapError(CodeBusy, werr)
	case errors.Is(werr, store.ErrLayout):
		return wrapError(CodeSettingsFile, werr, RejectShape)
	default:
		var pe *store.ParseError
		if errors.As(werr, &pe) {
			return wrapError(CodeSettingsFile, werr, RejectSyntax)
		}

		return wrapError(CodeDisk, werr, file)
	}
}

// settingsEdit turns the values to save into the keys to set and to unset.
//
// Parameters:
//   - p: plugin with a manifest
//   - values: new value of each setting to change, by setting id
func (r *Registry) settingsEdit(p Plugin, values map[string]any) (map[string]any, []string,
	*Error,
) {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	set := map[string]any{}

	var unset []string

	for _, id := range ids {
		s := settingOf(p.Manifest, id)
		if s == nil {
			return nil, nil, newError(CodeInputInvalid, id, RejectType)
		}

		v, code := checkSaved(s, r.opts.Home, values[id])
		if code != "" {
			return nil, nil, newError(CodeInputInvalid, id, code)
		}

		if reflect.DeepEqual(v, s.zeroOrDefault()) {
			unset = append(unset, id)

			continue
		}

		set[id] = v
	}

	return set, unset, nil
}

// settingOf finds a setting of a manifest.
//
// Parameters:
//   - m: validated manifest
//   - id: setting id
func settingOf(m *Manifest, id string) *Setting {
	for i := range m.Settings {
		if m.Settings[i].ID == id {
			return &m.Settings[i]
		}
	}

	return nil
}

// checkSaved checks a value to save against its setting and brings it to the type that the file
// holds.
//
// Parameters:
//   - s: the setting
//   - home: home directory for the path rules
//   - v: string, bool, int or int64, or []string
//
// Returns the value, or a nil value and the code of the first rule it breaks.
func checkSaved(s *Setting, home string, v any) (any, string) {
	c := checker{s: s, home: home}

	switch x := v.(type) {
	case int:
		return c.check(int64(x))
	case []string:
		items := make([]any, len(x))
		for i, it := range x {
			items[i] = it
		}

		return c.check(items)
	}

	return c.check(v)
}
