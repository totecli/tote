package plugin

import (
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Setting types of API 1.
var settingTypes = []string{"string", "bool", "int", "enum", "path", "list"}

// settings reads the [[setting]] tables.
//
// Parameters:
//   - n: root of the manifest
func (x *extractor) settings(n *node) []Setting {
	var out []Setting

	for _, t := range n.tables("setting") {
		out = append(out, x.setting(t))
		t.finish()
	}

	return out
}

// setting reads one [[setting]].
//
// Parameters:
//   - n: table of the setting
func (x *extractor) setting(n *node) Setting {
	start := len(x.c.issues)
	s := Setting{Path: n.path}

	s.ID, _ = n.str("id", strOpt{required: true, nonEmpty: true})
	s.Type = x.settingType(n)
	s.Label, _ = n.str("label", strOpt{required: true, visible: true, runes: MaxLabelRunes})
	s.Description, _ = n.str("description", strOpt{runes: MaxDescRunes})

	x.only(n, s.Type, "string list", "pattern", "max_len")
	x.only(n, s.Type, "int", "min", "max")
	x.only(n, s.Type, "enum", "choices")
	x.only(n, s.Type, "path", "must_exist")
	x.only(n, s.Type, "list", "max_items")

	x.settingKeys(n, &s)
	x.settingDefault(n, &s)

	if len(x.c.issues) > start {
		s.broken = true
	}

	return s
}

// settingType reads the type of a setting.
//
// Parameters:
//   - n: table of the setting
func (x *extractor) settingType(n *node) string {
	s, ok := n.str("type", strOpt{required: true, nonEmpty: true})
	if !ok {
		return ""
	}

	switch {
	case slices.Contains(settingTypes, s):
		return s
	case s == "dir" || s == "secret_ref":
		x.c.add(CodeReserved, n.at("type"), s)
	default:
		x.c.add(CodeValueInvalid, n.at("type"), s)
	}

	return ""
}

// settingKeys reads the keys that belong to one setting type.
//
// Parameters:
//   - n: table of the setting
//   - s: setting being read
func (x *extractor) settingKeys(n *node, s *Setting) {
	switch s.Type {
	case "string", "list":
		s.Pattern, _ = n.str("pattern", strOpt{nonEmpty: true, bytes: MaxPatternBytes})
		s.MaxLen = defaultMaxLen

		if v, ok := n.integer("max_len", 1, maxMaxLen); ok {
			s.MaxLen = int(v)
		}
	case "int":
		s.Min, s.HasMin = n.integer("min", math.MinInt32, math.MaxInt32)
		s.Max, s.HasMax = n.integer("max", math.MinInt32, math.MaxInt32)
	case "enum":
		var ok bool

		s.Choices, ok = n.strs("choices", listOpt{
			limit: MaxChoices,
			item:  strOpt{visible: true, runes: MaxChoiceRunes},
		})

		switch {
		case !n.has("choices"):
			x.c.add(CodeKeyMissing, n.at("choices"))
		case ok && len(s.Choices) == 0:
			x.c.add(CodeValueInvalid, n.at("choices"), "empty")
		}
	case "path":
		s.MustExist, _ = n.boolean("must_exist")
	}

	if s.Type == "list" {
		s.MaxItems = defaultItems

		if v, ok := n.integer("max_items", 1, maxItemsLimit); ok {
			s.MaxItems = int(v)
		}
	}
}

// settingDefault reads the default of a setting, which must have the type of the setting.
//
// Parameters:
//   - n: table of the setting
//   - s: setting being read
func (x *extractor) settingDefault(n *node, s *Setting) {
	if !n.has("default") {
		return
	}

	switch s.Type {
	case "string", "path", "enum":
		if v, ok := n.str("default", strOpt{bytes: maxFieldBytes}); ok {
			s.Default, s.HasDefault = v, true
		}
	case "bool":
		if v, ok := n.boolean("default"); ok {
			s.Default, s.HasDefault = v, true
		}
	case "int":
		if v, ok := n.integer("default", math.MinInt32, math.MaxInt32); ok {
			s.Default, s.HasDefault = v, true
		}
	case "list":
		if v, ok := n.strs("default", listOpt{
			limit: maxItemsLimit,
			item:  strOpt{bytes: maxMaxLen},
		}); ok {
			s.Default, s.HasDefault = v, true
		}
	default:
		n.raw("default")
	}
}

// SettingEnvName returns the name of the environment variable that overrides a setting.
//
// Parameters:
//   - id: plugin id
//   - setting: setting id, which never holds two consecutive underscores (settingID refuses them)
//
// Returns TOTE_PLUGIN_<ID>_<SETTING> in upper case, with each hyphen of the plugin id written as
// two underscores. The mapping is injective: an id holds no underscore, so a double underscore
// belongs to the id and a single one separates the id from the setting, and the setting id starts
// with a letter and holds no double underscore. The plugin my-tool with the setting x reads
// TOTE_PLUGIN_MY__TOOL_X, the plugin my with the setting tool_x reads TOTE_PLUGIN_MY_TOOL_X. The
// variable is read from tote's own environment and needs no permission.
func SettingEnvName(id, setting string) string {
	return "TOTE_PLUGIN_" + strings.ToUpper(strings.ReplaceAll(id, "-", "__")) + "_" +
		strings.ToUpper(setting)
}

// settingRules checks the settings against each other and against their own limits.
func (v *validator) settingRules() {
	seen := map[string]bool{}

	for i := range v.m.Settings {
		s := &v.m.Settings[i]

		v.settingID(s, seen)

		re := v.pattern(s.Path+".pattern", s.Pattern)

		v.settingChoices(s)
		v.settingBounds(s)
		v.settingValue(s, re)
	}
}

// reservedSettingIDs lists the setting ids that are reserved for a later version: token_env and
// token_cmd are refused for every setting. The id schema is refused for good, by settingID, because
// it is the root key that holds the version of the settings file plugins/<id>.toml, so a setting of
// that name could never be stored. The other root keys of that file that start with an underscore
// belong to tote; a setting id starts with a letter, so none of them can collide.
var reservedSettingIDs = []string{"token_env", "token_cmd"}

// settingID checks the grammar, the reserved ids and the uniqueness of a setting id. The grammar
// is that of the snake ids without a double underscore, which SettingEnvName relies on.
//
// Parameters:
//   - s: setting
//   - seen: ids already used; updated here
func (v *validator) settingID(s *Setting, seen map[string]bool) {
	if s.ID == "" {
		return
	}

	switch {
	case !snakeIDRe.MatchString(s.ID), strings.Contains(s.ID, "__"):
		v.c.add(CodeIDInvalid, s.Path+".id", s.ID, "setting")
	case s.ID == "schema":
		v.c.add(CodeIDInvalid, s.Path+".id", s.ID, "settingfile")
	case slices.Contains(reservedSettingIDs, s.ID):
		v.c.add(CodeReserved, s.Path+".id", s.ID)
	case seen[s.ID]:
		v.c.add(CodeIDDuplicate, s.Path+".id", s.ID)
	}

	seen[s.ID] = true
}

// settingChoices checks that the values of an enum are distinct.
//
// Parameters:
//   - s: setting
func (v *validator) settingChoices(s *Setting) {
	seen := map[string]bool{}

	for i, ch := range s.Choices {
		if seen[ch] {
			v.c.add(CodeValueInvalid, s.Path+".choices["+itoa(i)+"]", ch, "duplicate")
		}

		seen[ch] = true
	}
}

// settingBounds checks that the bounds of an int are ordered.
//
// Parameters:
//   - s: setting
func (v *validator) settingBounds(s *Setting) {
	lo, hi := s.bounds()
	if lo > hi {
		v.c.add(CodeValueInvalid, s.Path+".min", itoa64(lo), itoa64(hi))
	}
}

// bounds returns the effective bounds of an int setting, the defaults when a bound is absent.
func (s *Setting) bounds() (lo, hi int64) {
	lo, hi = math.MinInt32, math.MaxInt32

	if s.HasMin {
		lo = s.Min
	}

	if s.HasMax {
		hi = s.Max
	}

	return lo, hi
}

// settingValue checks the default of a setting against the limits of its own type.
//
// Parameters:
//   - s: setting
//   - re: compiled pattern, nil when there is none
func (v *validator) settingValue(s *Setting, re *regexp.Regexp) {
	if !s.HasDefault {
		return
	}

	path := s.Path + ".default"

	switch d := s.Default.(type) {
	case string:
		v.stringDefault(s, path, d, re)
	case int64:
		if lo, hi := s.bounds(); d < lo || d > hi {
			v.c.add(CodeValueInvalid, path, itoa64(d))
		}
	case []string:
		if len(d) > s.MaxItems {
			v.c.add(CodeLimitExceeded, path, "count", itoa(s.MaxItems))
		}

		for _, item := range d {
			v.stringDefault(s, path, item, re)
		}
	}
}

// stringDefault checks one default string against the length, the pattern and the choices of a
// setting. The empty string is always accepted: it is the zero value.
//
// Parameters:
//   - s: setting
//   - path: issue path of the default
//   - d: default text or list item
//   - re: compiled pattern, nil when there is none
func (v *validator) stringDefault(s *Setting, path, d string, re *regexp.Regexp) {
	switch {
	case s.Type == "enum":
		if !containsString(s.Choices, d) {
			v.c.add(CodeValueInvalid, path, d)
		}
	case d == "":
	case s.Type == "path":
	case utf8.RuneCountInString(d) > s.MaxLen, re != nil && !re.MatchString(d):
		v.c.add(CodeValueInvalid, path, d)
	}
}
