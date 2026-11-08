package plugin

import (
	"regexp"
	"slices"
	"strings"
	"time"
)

// Keys each table of contributions accepts, from section 3.6. [[action]] takes the execution keys
// minus run and open, [[list.action]] the ones of an item action minus target.
const (
	commandKeys = "id label description run open cwd env confirm output pause timeout allow_dash " +
		"key keywords field"
	itemActionKeys = "id label description run open cwd env confirm output pause timeout " +
		"allow_dash target key field"
	actionKeys = "id label description cwd env confirm output pause timeout allow_dash target " +
		"key keywords field"
	listActionKeys = "id label description run open cwd env confirm output pause timeout " +
		"allow_dash key field"
)

// Timeout bounds, from section 3.6 and 3.7.
const (
	MaxEntryTimeout  = 30 * time.Minute
	MaxListTimeout   = 120 * time.Second
	DefaultEntryTime = 5 * time.Minute
	DefaultListTime  = 30 * time.Second
)

// Limits chosen for the keys the specification does not number.
const (
	maxEnvEntries   = 16
	maxAllowDash    = 16
	maxConfirmRunes = 200
	maxOpenBytes    = 2048
	maxFieldBytes   = 1024
	maxMaxLen       = 1024
	defaultMaxLen   = 256
	defaultItems    = 100
	maxItemsLimit   = 500
	minWidth        = 4
	maxWidth        = 80
)

// timeoutRe matches the timeout syntax: digits and the unit s or m.
var timeoutRe = regexp.MustCompile(`^[0-9]+(s|m)$`)

// ParseTimeout parses a timeout such as 30s or 5m.
//
// Parameters:
//   - s: digits followed by s or m
//
// Returns the duration and true, or false for another form, a zero value or a number too large to
// hold.
func ParseTimeout(s string) (time.Duration, bool) {
	if !timeoutRe.MatchString(s) {
		return 0, false
	}

	unit := time.Second
	if strings.HasSuffix(s, "m") {
		unit = time.Minute
	}

	digits := s[:len(s)-1]
	if len(digits) > 6 {
		return 0, false
	}

	var n int64

	for i := 0; i < len(digits); i++ {
		n = n*10 + int64(digits[i]-'0')
	}

	if n == 0 {
		return 0, false
	}

	return time.Duration(n) * unit, true
}

// keySet turns a space separated list of keys into a set.
//
// Parameters:
//   - keys: key names separated by spaces
func keySet(keys string) map[string]bool {
	set := map[string]bool{}
	for _, k := range strings.Fields(keys) {
		set[k] = true
	}

	return set
}

// entries reads one table of contributions.
//
// Parameters:
//   - n: root of the manifest
//   - table: name of the table
//   - keys: space separated keys the table accepts
func (x *extractor) entries(n *node, table, keys string) []Entry {
	var out []Entry

	allow := keySet(keys)

	for _, t := range n.tables(table) {
		out = append(out, x.entry(t, table, allow))
		t.finish()
	}

	return out
}

// entry reads one contribution.
//
// Parameters:
//   - n: table of the contribution
//   - table: name of the table, which decides the values some keys accept
//   - allow: keys the table accepts; the others are left unread and reported as unknown
func (x *extractor) entry(n *node, table string, allow map[string]bool) Entry {
	start := len(x.c.issues)
	e := Entry{Table: table, Path: n.path}

	e.ID, _ = n.str("id", strOpt{required: true, nonEmpty: true})
	e.Label, _ = n.str("label", strOpt{required: true, visible: true, runes: MaxLabelRunes})
	e.Description, _ = n.str("description", strOpt{runes: MaxDescRunes})

	if allow["run"] {
		e.Run, _ = n.strs("run", listOpt{limit: MaxArgvElems, item: strOpt{bytes: MaxArgvElemBytes}})
	}

	if allow["open"] {
		e.Open, _ = n.str("open", strOpt{nonEmpty: true, bytes: maxOpenBytes})
	}

	e.Cwd, _ = n.str("cwd", strOpt{nonEmpty: true, bytes: MaxArgvElemBytes})
	e.Env = x.env(n)
	e.Confirm = x.confirm(n)
	e.Output = x.output(n, table)
	e.Pause = x.pause(n)
	e.Timeout = x.timeout(n, MaxEntryTimeout)
	e.AllowDash, _ = n.strs("allow_dash",
		listOpt{limit: maxAllowDash, item: strOpt{nonEmpty: true, bytes: maxNameLen}})

	if allow["target"] {
		e.Target = x.target(n, table)
	}

	e.Key, _ = n.str("key", strOpt{nonEmpty: true, bytes: 1})

	if allow["keywords"] {
		e.Keywords, _ = n.strs("keywords", listOpt{
			limit: MaxKeywords,
			item:  strOpt{nonEmpty: true, runes: MaxKeywordRunes},
		})
	}

	e.Fields = x.fields(n)

	if len(x.c.issues) > start {
		e.broken = true
	}

	return e
}

// env reads the env table of a contribution: variable names to templates.
//
// Parameters:
//   - n: table of the contribution
func (x *extractor) env(n *node) map[string]string {
	t, ok := n.table("env")
	if !ok {
		return nil
	}

	if len(t.m) > maxEnvEntries {
		x.c.add(CodeLimitExceeded, t.path, "count", itoa(maxEnvEntries))
	}

	out := map[string]string{}

	for _, k := range sortedKeys(t.m) {
		if v, ok := t.str(k, strOpt{bytes: MaxArgvElemBytes}); ok {
			out[k] = v
		}
	}

	return out
}

// confirm reads the confirm key, a boolean or a text.
//
// Parameters:
//   - n: table of the contribution
func (x *extractor) confirm(n *node) Confirm {
	v, ok := n.raw("confirm")
	if !ok {
		return Confirm{}
	}

	if b, isBool := v.(bool); isBool {
		return Confirm{Set: b}
	}

	s, ok := n.checkStr(n.at("confirm"), v, strOpt{nonEmpty: true, runes: maxConfirmRunes})
	if !ok {
		return Confirm{}
	}

	return Confirm{Set: true, Text: s}
}

// output reads the output key of a contribution.
//
// Parameters:
//   - n: table of the contribution
//   - table: name of the table: terminal belongs to the declarative tier and to the entries that
//     tote runs, and is reserved for an [[action]] and for the exec tier
func (x *extractor) output(n *node, table string) string {
	allowed := []string{"pager", "silent"}
	reserved := []string{"table", "pane", "stream"}

	if table != "action" && x.tier == TierDeclarative {
		allowed = append(allowed, "terminal")
	} else {
		reserved = append(reserved, "terminal")
	}

	return x.choice(n, "output", allowed, reserved)
}

// pause reads the pause key of a contribution.
//
// Parameters:
//   - n: table of the contribution
func (x *extractor) pause(n *node) string {
	return x.choice(n, "pause", []string{"always", "error", "never"}, nil)
}

// target reads the target key of a contribution.
//
// Parameters:
//   - n: table of the contribution
//   - table: name of the table: an item_action takes repo only, an action none or repo
func (x *extractor) target(n *node, table string) string {
	allowed := []string{"repo"}
	if table == "action" {
		allowed = []string{"none", "repo"}
	}

	return x.choice(n, "target", allowed, []string{"connection"})
}

// choice reads an optional string that must be one of a list.
//
// Parameters:
//   - n: table that holds the key
//   - key: key name
//   - allowed: accepted values
//   - reserved: values that are reserved for a later release
//
// Returns the value, or an empty string when the key is absent or refused.
func (x *extractor) choice(n *node, key string, allowed, reserved []string) string {
	s, ok := n.str(key, strOpt{nonEmpty: true})
	if !ok {
		return ""
	}

	switch {
	case slices.Contains(allowed, s):
		return s
	case slices.Contains(reserved, s):
		x.c.add(CodeReserved, n.at(key), s)
	default:
		x.c.add(CodeValueInvalid, n.at(key), s)
	}

	return ""
}

// timeout reads a timeout key.
//
// Parameters:
//   - n: table that holds the key
//   - limit: longest accepted duration
func (x *extractor) timeout(n *node, limit time.Duration) string {
	s, ok := n.str("timeout", strOpt{nonEmpty: true})
	if !ok {
		return ""
	}

	d, valid := ParseTimeout(s)

	switch {
	case !valid:
		x.c.add(CodeValueInvalid, n.at("timeout"), s)
	case d > limit:
		x.c.add(CodeLimitExceeded, n.at("timeout"), "max", limit.String())
	default:
		return s
	}

	return ""
}

// fields reads the form fields of a contribution.
//
// Parameters:
//   - n: table of the contribution
func (x *extractor) fields(n *node) []Field {
	tables := n.tables("field")
	if len(tables) > MaxFields {
		x.c.add(CodeLimitExceeded, n.at("field"), "count", itoa(MaxFields))
	}

	out := make([]Field, 0, len(tables))

	for _, t := range tables {
		out = append(out, x.field(t))
		t.finish()
	}

	return out
}

// field reads one form field.
//
// Parameters:
//   - n: table of the field
func (x *extractor) field(n *node) Field {
	start := len(x.c.issues)
	f := Field{Path: n.path}

	f.ID, _ = n.str("id", strOpt{required: true, nonEmpty: true})
	f.Type = x.fieldType(n)
	f.Label, _ = n.str("label", strOpt{required: true, visible: true, runes: MaxLabelRunes})
	f.Hint, _ = n.str("hint", strOpt{runes: MaxDescRunes})
	f.Required, _ = n.boolean("required")

	n.reserve("secret", "remember", "choices_from")

	x.fieldDefault(n, &f)
	x.fieldTyped(n, &f)

	if len(x.c.issues) > start {
		f.broken = true
	}

	return f
}

// fieldType reads the type of a field, which decides the keys it accepts.
//
// Parameters:
//   - n: table of the field
func (x *extractor) fieldType(n *node) string {
	s, ok := n.str("type", strOpt{required: true, nonEmpty: true})
	if !ok {
		return ""
	}

	switch s {
	case "text", "choice", "bool":
		return s
	case "connection", "connections", "repo", "path":
		x.c.add(CodeReserved, n.at("type"), s)
	default:
		x.c.add(CodeValueInvalid, n.at("type"), s)
	}

	return ""
}

// fieldDefault reads the default of a field: a template for text and choice, a boolean for bool.
//
// Parameters:
//   - n: table of the field
//   - f: field being read
func (x *extractor) fieldDefault(n *node, f *Field) {
	if f.Type == "bool" {
		if b, ok := n.boolean("default"); ok {
			f.HasDefault = true
			f.Default = "false"

			if b {
				f.Default = "true"
			}
		}

		return
	}

	if s, ok := n.str("default", strOpt{bytes: maxFieldBytes}); ok {
		f.HasDefault = true
		f.Default = s
	}
}

// fieldTyped reads the keys that belong to one field type and reports them on the others.
//
// Parameters:
//   - n: table of the field
//   - f: field being read
func (x *extractor) fieldTyped(n *node, f *Field) {
	x.only(n, f.Type, "text", "pattern", "max_len")
	x.only(n, f.Type, "choice", "choices")
	x.only(n, f.Type, "bool", "flag")

	switch f.Type {
	case "text":
		f.Pattern, _ = n.str("pattern", strOpt{nonEmpty: true, bytes: MaxPatternBytes})
		f.MaxLen = defaultMaxLen

		if v, ok := n.integer("max_len", 1, maxMaxLen); ok {
			f.MaxLen = int(v)
		}
	case "choice":
		f.Choices, _ = n.strs("choices", listOpt{limit: MaxChoices, item: strOpt{runes: MaxChoiceRunes}})
	case "bool":
		f.Flag, _ = n.str("flag", strOpt{required: true, nonEmpty: true, bytes: MaxFlagBytes})
	}
}

// only reports the keys that a field type does not accept.
//
// Parameters:
//   - n: table of the field
//   - typ: type of the field or setting, empty when it is unknown
//   - want: space separated types that accept the keys
//   - keys: key names
func (x *extractor) only(n *node, typ, want string, keys ...string) {
	if typ == "" || slices.Contains(strings.Fields(want), typ) {
		return
	}

	for _, k := range keys {
		if _, ok := n.raw(k); ok {
			x.c.add(CodeKeyConflict, n.at(k), k, typ)
		}
	}
}

// lists reads the [[list]] tables of the exec tier.
//
// Parameters:
//   - n: root of the manifest
func (x *extractor) lists(n *node) []List {
	var out []List

	for _, t := range n.tables("list") {
		out = append(out, x.list(t))
		t.finish()
	}

	return out
}

// list reads one [[list]].
//
// Parameters:
//   - n: table of the list
func (x *extractor) list(n *node) List {
	l := List{Path: n.path}

	l.ID, _ = n.str("id", strOpt{required: true, nonEmpty: true})
	l.Label, _ = n.str("label", strOpt{required: true, visible: true, runes: MaxLabelRunes})
	l.Description, _ = n.str("description", strOpt{runes: MaxDescRunes})
	l.Keywords, _ = n.strs("keywords", listOpt{
		limit: MaxKeywords,
		item:  strOpt{nonEmpty: true, runes: MaxKeywordRunes},
	})
	l.Timeout = x.timeout(n, MaxListTimeout)

	columns := n.tables("column")

	switch raw, present := n.m["column"]; {
	case !present:
		x.c.add(CodeKeyMissing, n.at("column"))
	case emptyArray(raw):
		x.c.add(CodeValueInvalid, n.at("column"), "empty")
	}

	if len(columns) > MaxColumns {
		x.c.add(CodeLimitExceeded, n.at("column"), "count", itoa(MaxColumns))
	}

	for _, t := range columns {
		l.Columns = append(l.Columns, x.column(t))
		t.finish()
	}

	actions := n.tables("action")
	if len(actions) > MaxListActions {
		x.c.add(CodeLimitExceeded, n.at("action"), "count", itoa(MaxListActions))
	}

	allow := keySet(listActionKeys)

	for _, t := range actions {
		l.Actions = append(l.Actions, x.entry(t, "list.action", allow))
		t.finish()
	}

	return l
}

// emptyArray tells whether a decoded value is an array with no element.
//
// Parameters:
//   - v: decoded value
func emptyArray(v any) bool {
	switch arr := v.(type) {
	case []any:
		return len(arr) == 0
	case []map[string]any:
		return len(arr) == 0
	}

	return false
}

// column reads one [[list.column]].
//
// Parameters:
//   - n: table of the column
func (x *extractor) column(n *node) Column {
	var c Column

	c.ID, _ = n.str("id", strOpt{required: true, nonEmpty: true})
	c.Label, _ = n.str("label", strOpt{required: true, visible: true, runes: MaxLabelRunes})

	if v, ok := n.integer("width", minWidth, maxWidth); ok {
		c.Width = int(v)
	}

	c.Align = x.choice(n, "align", []string{"left", "right"}, nil)

	return c
}
