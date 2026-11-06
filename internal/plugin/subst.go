package plugin

import (
	"strings"
	"unicode/utf8"
)

// Placeholder name limits.
const (
	maxSegmentLen = 32
	maxNameLen    = 96
)

// PlaceholderKind classifies a placeholder by the scope it reads from.
type PlaceholderKind int

// The kinds of placeholder of section 3.5.
const (
	KindField PlaceholderKind = iota + 1
	KindSetting
	KindRepo
	KindItemID
	KindItemData
	KindCwd
	KindHome
)

// repoMembers lists the members of {repo.*}.
var repoMembers = []string{"path", "name", "remote", "slug", "branch"}

// Placeholder is one parsed {name} or {name?} of a template.
//
// Name is the dotted name without braces and without the question mark. Member is the part after
// the kind: the setting id, the repo member or the item data key; it is empty for a field, {cwd},
// {home} and {item.id}. Optional is true for {name?}.
type Placeholder struct {
	Name     string
	Member   string
	Kind     PlaceholderKind
	Optional bool
}

// Part is one piece of a template: a literal text, or a placeholder when Holder is not nil.
type Part struct {
	Text   string
	Holder *Placeholder
}

// Template is a parsed value template. Substitution is a later step: this type only carries the
// grammar and the classification of the placeholders.
type Template struct {
	Parts []Part
}

// TemplateError reports why a template does not parse.
//
// Reason is one of "stray_open" (a "{" that does not open a placeholder), "stray_close" (a "}"
// that closes nothing), "empty" ({} or {?}), "unterminated" (no closing brace), "bad_name" (a name
// that breaks the dotted grammar) and "unknown" (a well-formed name that is not a placeholder of
// the API). Offset is the byte position in the template.
type TemplateError struct {
	Reason string
	Offset int
}

// Error implements error.
func (e *TemplateError) Error() string {
	return "template: " + e.Reason
}

// ParseTemplate parses a value template.
//
// Parameters:
//   - s: text with {name} and {name?} placeholders, where name is dotted segments of
//     ^[a-z][a-z0-9_]*$; "{{" and "}}" are literal braces; a stray brace, an empty {}, an unknown
//     name or a nested brace is an error
//
// Returns the template, or a *TemplateError. The names that classify are: a field id, setting.<id>,
// repo.path name remote slug branch, item.id, item.data.<key>, cwd and home.
func ParseTemplate(s string) (Template, *TemplateError) {
	var (
		t   Template
		lit strings.Builder
	)

	flush := func() {
		if lit.Len() > 0 {
			t.Parts = append(t.Parts, Part{Text: lit.String()})
			lit.Reset()
		}
	}

	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '{':
			if i+1 < len(s) && s[i+1] == '{' {
				lit.WriteByte('{')

				i++

				continue
			}

			end := strings.IndexByte(s[i+1:], '}')
			if end < 0 {
				return Template{}, &TemplateError{"unterminated", i}
			}

			h, perr := parsePlaceholder(s[i+1:i+1+end], i)
			if perr != nil {
				return Template{}, perr
			}

			flush()

			t.Parts = append(t.Parts, Part{Holder: &h})
			i += end + 1
		case '}':
			if i+1 < len(s) && s[i+1] == '}' {
				lit.WriteByte('}')

				i++

				continue
			}

			return Template{}, &TemplateError{"stray_close", i}
		default:
			lit.WriteByte(c)
		}
	}

	flush()

	return t, nil
}

// parsePlaceholder parses the text between braces.
//
// Parameters:
//   - body: text between "{" and "}"
//   - offset: byte position of the opening brace, for errors
func parsePlaceholder(body string, offset int) (Placeholder, *TemplateError) {
	name, optional := strings.CutSuffix(body, "?")
	if name == "" {
		return Placeholder{}, &TemplateError{"empty", offset}
	}

	if strings.ContainsAny(name, "{}") {
		return Placeholder{}, &TemplateError{"stray_open", offset}
	}

	if !validDottedName(name) {
		return Placeholder{}, &TemplateError{"bad_name", offset}
	}

	h, ok := classify(name)
	if !ok {
		return Placeholder{}, &TemplateError{"unknown", offset}
	}

	h.Optional = optional

	return h, nil
}

// validDottedName checks the grammar of a placeholder name: dotted segments of ^[a-z][a-z0-9_]*$.
//
// Parameters:
//   - name: text to test, without braces
func validDottedName(name string) bool {
	if len(name) > maxNameLen {
		return false
	}

	for _, seg := range strings.Split(name, ".") {
		if !validSnake(seg) {
			return false
		}
	}

	return true
}

// validSnake checks one segment against ^[a-z][a-z0-9_]*$ with at most 32 bytes.
//
// Parameters:
//   - s: text to test
func validSnake(s string) bool {
	if s == "" || len(s) > maxSegmentLen || s[0] < 'a' || s[0] > 'z' {
		return false
	}

	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}

	return true
}

// classify gives the kind of a well-formed placeholder name.
//
// Parameters:
//   - name: dotted name that passed validDottedName
//
// Returns the placeholder, or false when the name is not one of the API: an unknown repo member,
// {item} alone, {repo} alone, or a dotted name with an unknown first segment.
func classify(name string) (Placeholder, bool) {
	head, rest, dotted := strings.Cut(name, ".")

	if !dotted {
		switch head {
		case "cwd":
			return Placeholder{Name: name, Kind: KindCwd}, true
		case "home":
			return Placeholder{Name: name, Kind: KindHome}, true
		case "repo", "setting", "item":
			return Placeholder{}, false
		}

		return Placeholder{Name: name, Kind: KindField}, true
	}

	switch head {
	case "setting":
		if validSnake(rest) {
			return Placeholder{Name: name, Member: rest, Kind: KindSetting}, true
		}
	case "repo":
		for _, m := range repoMembers {
			if rest == m {
				return Placeholder{Name: name, Member: rest, Kind: KindRepo}, true
			}
		}
	case "item":
		if rest == "id" {
			return Placeholder{Name: name, Kind: KindItemID}, true
		}

		if key, ok := strings.CutPrefix(rest, "data."); ok && validSnake(key) {
			return Placeholder{Name: name, Member: key, Kind: KindItemData}, true
		}
	}

	return Placeholder{}, false
}

// Placeholders lists the placeholders of the template in order, repeats included.
func (t Template) Placeholders() []Placeholder {
	var out []Placeholder

	for _, p := range t.Parts {
		if p.Holder != nil {
			out = append(out, *p.Holder)
		}
	}

	return out
}

// Literal returns the text of a template that holds no placeholder, with the doubled braces
// resolved, and true; else an empty string and false.
func (t Template) Literal() (string, bool) {
	var b strings.Builder

	for _, p := range t.Parts {
		if p.Holder != nil {
			return "", false
		}

		b.WriteString(p.Text)
	}

	return b.String(), true
}

// Whole returns the placeholder of a template that is exactly one placeholder with no text around
// it, and true; else the zero value and false. A list value must be a whole element.
func (t Template) Whole() (Placeholder, bool) {
	if len(t.Parts) == 1 && t.Parts[0].Holder != nil {
		return *t.Parts[0].Holder, true
	}

	return Placeholder{}, false
}

// Scope tells which names a template may use in one place of a manifest.
//
// Fields maps the ids of the form fields in scope to their types, Settings maps every declared
// setting id to its type, and Item is true inside a [[list.action]], where {item.id} and
// {item.data.<key>} exist. {repo.*}, {cwd} and {home} are in scope everywhere.
type Scope struct {
	Fields   map[string]string
	Settings map[string]string
	Item     bool
}

// Check tells whether a placeholder exists in the scope.
//
// Parameters:
//   - p: placeholder returned by ParseTemplate
//
// Returns an empty string when it exists, CodePlaceholderUnknown when a field or a setting of that
// name is not declared, and CodePlaceholderScope when the name is known but unavailable here
// ({item.*} outside a [[list.action]]).
func (s Scope) Check(p Placeholder) string {
	switch p.Kind {
	case KindField:
		if _, ok := s.Fields[p.Name]; !ok {
			return CodePlaceholderUnknown
		}
	case KindSetting:
		if _, ok := s.Settings[p.Member]; !ok {
			return CodePlaceholderUnknown
		}
	case KindItemID, KindItemData:
		if !s.Item {
			return CodePlaceholderScope
		}
	}

	return ""
}

// maxCwdBytes is the longest directory a template may produce.
const maxCwdBytes = 4096

// Item is the row of a list that a [[list.action]] runs on: its id and its data, both set by the
// plugin that produced the row and therefore untrusted.
type Item struct {
	ID   string
	Data map[string]string
}

// FieldValue is the submitted value of a form field after validation. Text is what a {field}
// placeholder gives: the value, or for a bool its flag when true and the empty text when false.
// Type is the field type, which decides the dash rule.
type FieldValue struct {
	Text string
	Type string
}

// Values holds what the placeholders of one call read: the validated fields, the effective
// settings, the repository and the row, the working directory and the home. A nil Repo or Item
// leaves the matching placeholders unresolved.
type Values struct {
	Fields   map[string]FieldValue
	Settings map[string]SettingValue
	Repo     *RepoInfo
	Item     *Item
	Cwd      string
	Home     string
}

// resolve gives the value of a placeholder.
//
// Parameters:
//   - p: placeholder returned by ParseTemplate
//
// Returns the values and true when the placeholder is a list (a list setting), where each value is
// one element; otherwise one value, which is empty when the placeholder is unresolved.
func (v *Values) resolve(p Placeholder) ([]string, bool) {
	switch p.Kind {
	case KindField:
		return []string{v.Fields[p.Name].Text}, false
	case KindSetting:
		return settingText(v.Settings[p.Member])
	case KindRepo:
		if v.Repo != nil {
			return []string{v.Repo.member(p.Member)}, false
		}
	case KindItemID:
		if v.Item != nil {
			return []string{v.Item.ID}, false
		}
	case KindItemData:
		if v.Item != nil {
			return []string{v.Item.Data[p.Member]}, false
		}
	case KindCwd:
		return []string{v.Cwd}, false
	case KindHome:
		return []string{v.Home}, false
	}

	return []string{""}, false
}

// settingText gives the text of an effective setting value.
//
// Parameters:
//   - sv: setting value, the zero value when the setting does not exist
func settingText(sv SettingValue) ([]string, bool) {
	switch x := sv.Value.(type) {
	case string:
		return []string{x}, false
	case bool:
		if x {
			return []string{"true"}, false
		}

		return []string{"false"}, false
	case int64:
		return []string{itoa64(x)}, false
	case []string:
		return x, true
	}

	return []string{""}, false
}

// ProducedIssue checks a piece of text that substitution produced and returns the rule it breaks:
// "utf8" (not valid UTF-8), "control" (a control character of any kind, NUL, line feed, carriage
// return and tab included, or a bidi, zero-width or other format character) or "length", or an
// empty string when it is fine.
//
// Parameters:
//   - s: produced text
//   - limit: longest allowed length in bytes
func ProducedIssue(s string, limit int) string {
	if len(s) > limit {
		return RejectLength
	}

	if !utf8.ValidString(s) {
		return RejectUTF8
	}

	for _, r := range s {
		if unsafeRune(r) || r == '\t' {
			return RejectControl
		}
	}

	return ""
}

// ExpandArgv substitutes the placeholders of one element of a run template.
//
// Parameters:
//   - t: parsed element
//   - v: values of the call
//   - allow: names of the placeholders that allow_dash lists
//
// Returns the elements it produces, none when an optional placeholder is empty, many when the
// element is a whole list setting, and otherwise one. An *Error says why the contribution is
// unavailable (an empty placeholder that is not optional), why a value is refused (the dash rule)
// or why the produced text is not acceptable.
func ExpandArgv(t Template, v *Values, allow map[string]bool) ([]string, *Error) {
	if h, whole := t.Whole(); whole {
		if vals, list := v.resolve(h); list {
			return expandList(h, vals, v, allow)
		}
	}

	var b strings.Builder

	for _, part := range t.Parts {
		if part.Holder == nil {
			b.WriteString(part.Text)

			continue
		}

		vals, list := v.resolve(*part.Holder)

		switch {
		case list:
			return nil, newError(CodeProducedInvalid, part.Holder.Name, "list")
		case vals[0] == "" && part.Holder.Optional:
			return nil, nil
		case vals[0] == "":
			return nil, newError(CodeUnavailable, part.Holder.Name, "empty")
		case len(vals[0]) > MaxArgvElemBytes:
			return nil, newError(CodeProducedInvalid, "argv", "length")
		}

		b.WriteString(vals[0])
	}

	out := b.String()

	if rule := ProducedIssue(out, MaxArgvElemBytes); rule != "" {
		return nil, newError(CodeProducedInvalid, "argv", rule)
	}

	if err := dashCheck(t, out, v, allow); err != nil {
		return nil, err
	}

	return []string{out}, nil
}

// expandList expands a whole list setting into its elements.
//
// Parameters:
//   - h: the placeholder
//   - vals: items of the list
//   - v: values of the call
//   - allow: names of the placeholders that allow_dash lists
func expandList(h Placeholder, vals []string, v *Values, allow map[string]bool) ([]string, *Error) {
	var out []string

	for _, item := range vals {
		if item == "" {
			continue
		}

		if rule := ProducedIssue(item, MaxArgvElemBytes); rule != "" {
			return nil, newError(CodeProducedInvalid, h.Name, rule)
		}

		if err := dashCheck(Template{Parts: []Part{{Holder: &h}}}, item, v, allow); err != nil {
			return nil, err
		}

		out = append(out, item)
	}

	return out, nil
}

// dashCheck applies the dash rule of section 3.5: an element that starts with '-' after
// substitution, whose template does not, is refused, except when its first placeholder is named in
// allow_dash, is a choice field with static choices, or is the flag of a bool field.
//
// Parameters:
//   - t: parsed element
//   - out: element after substitution
//   - v: values of the call
//   - allow: names of the placeholders that allow_dash lists
func dashCheck(t Template, out string, v *Values, allow map[string]bool) *Error {
	if !strings.HasPrefix(out, "-") || len(t.Parts) == 0 || t.Parts[0].Holder == nil {
		return nil
	}

	h := t.Parts[0].Holder

	if allow[h.Name] {
		return nil
	}

	if h.Kind == KindField {
		if typ := v.Fields[h.Name].Type; typ == "choice" || typ == "bool" {
			return nil
		}
	}

	return newError(CodeDashRefused, h.Name)
}

// ExpandScalar substitutes the placeholders of a template that must give one value: a working
// directory, an environment value, a confirmation text or a URL. The dash rule does not apply.
//
// Parameters:
//   - t: parsed template
//   - v: values of the call
//   - limit: longest allowed result in bytes
//
// Returns the text and true, or false when an optional placeholder was empty (the value is
// dropped). An *Error says why an empty placeholder that is not optional makes the value
// unavailable, or why the text is not acceptable. A list setting cannot be used here.
func ExpandScalar(t Template, v *Values, limit int) (string, bool, *Error) {
	var b strings.Builder

	for _, part := range t.Parts {
		if part.Holder == nil {
			b.WriteString(part.Text)

			continue
		}

		vals, list := v.resolve(*part.Holder)

		switch {
		case list:
			return "", false, newError(CodeProducedInvalid, part.Holder.Name, "list")
		case vals[0] == "" && part.Holder.Optional:
			return "", false, nil
		case vals[0] == "":
			return "", false, newError(CodeUnavailable, part.Holder.Name, "empty")
		case len(vals[0]) > limit:
			return "", false, newError(CodeProducedInvalid, "value", "length")
		}

		b.WriteString(vals[0])
	}

	out := b.String()

	if rule := ProducedIssue(out, limit); rule != "" {
		return "", false, newError(CodeProducedInvalid, "value", rule)
	}

	return out, true, nil
}

// ExpandLoose substitutes a template for display or for a default: an unresolved placeholder, a
// list or a text that is not acceptable gives the empty text for that placeholder, never an error.
//
// Parameters:
//   - t: parsed template
//   - v: values of the call
func ExpandLoose(t Template, v *Values) string {
	var b strings.Builder

	for _, part := range t.Parts {
		if part.Holder == nil {
			b.WriteString(part.Text)

			continue
		}

		vals, list := v.resolve(*part.Holder)
		if list || ProducedIssue(vals[0], MaxArgvElemBytes) != "" {
			continue
		}

		b.WriteString(vals[0])
	}

	return b.String()
}
