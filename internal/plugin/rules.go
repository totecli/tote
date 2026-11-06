package plugin

import (
	"strconv"
	"strings"
)

// FieldRule checks the text of a form field the way a run does and names the rule it breaks.
//
// The rules are the ones that Prepare applies, in the same order: the size, the characters, the
// required rule, max_len and the pattern for a text, membership for a choice and a boolean word
// for a bool. The dash rule is not applied here, it belongs to the substitution of a command.
//
// Parameters:
//   - f: field of a validated manifest
//   - raw: text of the form: the value, "true" or "false" for a bool
//
// Returns the rule: "required", "max_len", "pattern", "choice", "bool", "length", "utf8" or
// "control", or an empty string when the value is accepted.
func FieldRule(f Field, raw string) string {
	if _, err := checkField(&f, raw); err != nil && len(err.Args) > 1 {
		return err.Args[1]
	}

	return ""
}

// SettingDefault returns the value a setting has when nothing sets it: its default, else the zero
// value of its type.
//
// Parameters:
//   - s: setting of a validated manifest
//
// Returns a string, a bool, an int64 or a []string according to the type of the setting.
func SettingDefault(s Setting) any {
	return s.zeroOrDefault()
}

// SettingText gives the text a form shows for a value of a setting: the text itself, "true" or
// "false", the decimal number, and the items of a list joined by a comma and a space.
//
// Parameters:
//   - v: string, bool, int64 or []string, as SettingValue holds it
func SettingText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case []string:
		return strings.Join(x, ", ")
	}

	return ""
}

// CheckSettingText checks a value typed in a form for a setting of one line: a string, an int,
// an enum or a path, or a bool as "true" or "false".
//
// Parameters:
//   - s: setting of a validated manifest; a list is checked with CheckSettingItems
//   - home: home directory, which expands a leading "~" of a path that must exist
//   - raw: text of the form
//
// Returns the value in the type of the setting and an empty code, or a nil value and the code of
// the first rule it breaks (the Reject constants).
func CheckSettingText(s Setting, home, raw string) (any, string) {
	c := checker{s: &s, home: home}

	switch s.Type {
	case "bool":
		switch strings.ToLower(raw) {
		case "true":
			return true, ""
		case "false":
			return false, ""
		}

		return nil, RejectType
	case "int":
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, RejectType
		}

		return c.check(n)
	case "list":
		return nil, RejectType
	}

	return c.check(raw)
}

// CheckSettingItem checks one item that is typed for a list setting: a text that is not empty,
// within max_len and the pattern.
//
// Parameters:
//   - s: setting of a validated manifest, of type list
//   - item: text of the item
//
// Returns an empty code when the item is accepted, else the code of the first rule it breaks.
func CheckSettingItem(s Setting, item string) string {
	if item == "" {
		return RejectText
	}

	c := checker{s: &s}
	_, code := c.text(item)

	return code
}

// CheckSettingItems checks the items of a list setting as a whole: the number of items, then each
// item.
//
// Parameters:
//   - s: setting of a validated manifest, of type list
//   - items: items of the list
//
// Returns an empty code when the list is accepted, else the code of the first rule it breaks.
func CheckSettingItems(s Setting, items []string) string {
	anys := make([]any, len(items))
	for i, it := range items {
		anys[i] = it
	}

	c := checker{s: &s}
	_, code := c.list(anys)

	return code
}
