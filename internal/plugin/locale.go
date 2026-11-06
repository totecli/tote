package plugin

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/totecli/tote/internal/store"
)

// localeLangs lists the languages a plugin can translate its texts to. English is the language of
// the manifest and only serves to label choices; only these two are read in 1.0.
var localeLangs = []string{"en", "fr"}

// Locale limits.
const (
	maxLocaleKeyBytes   = 160
	maxLocaleValueBytes = 1024
	maxLocaleDepth      = 8
)

// flattenLocale flattens a table of translations, whose keys may be dotted, nested or quoted, to
// one level of dotted keys.
//
// Parameters:
//   - c: collector that receives the issues
//   - path: issue path of the table, empty for a locale file
//   - m: decoded table
//   - prefix: dotted key of the table inside the file, with a trailing dot, empty at the top
//   - depth: nesting level, to stop a hostile document
//   - out: receives the flat keys; a value that fails a check is left out
func flattenLocale(
	c *collector,
	path string,
	m map[string]any,
	prefix string,
	depth int,
	out map[string]string,
) {
	for _, k := range sortedKeys(m) {
		key := prefix + k
		at := joinKey(path, k)

		switch v := m[k].(type) {
		case string:
			switch {
			case len(key) > maxLocaleKeyBytes || len(v) > maxLocaleValueBytes:
				c.add(CodeLimitExceeded, at, "bytes", itoa(maxLocaleValueBytes))
			case !shownText(key) || !shownText(v):
				c.add(CodeTextInvalid, at)
			default:
				out[key] = v
			}
		case map[string]any:
			if depth >= maxLocaleDepth {
				c.add(CodeLimitExceeded, at, "depth", itoa(maxLocaleDepth))

				continue
			}

			flattenLocale(c, at, v, key+".", depth+1, out)
		default:
			c.add(CodeTypeInvalid, at, "string", typeName(v))
		}
	}
}

// ParseLocale reads a locales/<lang>.toml file.
//
// Parameters:
//   - data: content of the file, at most 128 KiB, a TOML document of strings whose keys follow the
//     grammar of section 3.7; dotted, nested and quoted keys are all accepted and flattened
//
// Returns the flat table of translations and the issues found. Any issue is an error: the caller
// does not use the table then. The size, syntax and encoding problems are locale_invalid; the
// problems of one entry keep the code of the generic readers.
func ParseLocale(data []byte) (map[string]string, []Issue) {
	c := &collector{}

	if len(data) > MaxLocaleSize {
		c.add(CodeLocaleInvalid, "", "size", itoa(MaxLocaleSize))

		return nil, c.issues
	}

	if !utf8.Valid(data) {
		c.add(CodeLocaleInvalid, "", "utf8")

		return nil, c.issues
	}

	if se := store.CheckShape(data, localeShape); se != nil {
		c.issues = append(c.issues, shapeIssue(se))

		return nil, c.issues
	}

	var raw map[string]any

	if _, err := toml.Decode(string(data), &raw); err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			line, col := store.SyntaxPosition(data, perr.Position)

			c.issues = append(c.issues, Issue{
				Code: CodeLocaleInvalid, Line: line, Column: col, Args: []string{"syntax"},
			})
		} else {
			c.add(CodeLocaleInvalid, "", "syntax")
		}

		return nil, c.issues
	}

	c.pos = indexPositions(string(data))
	out := map[string]string{}
	flattenLocale(c, "", raw, "", 0, out)

	return out, c.issues
}

// localeKey is one translatable text of a manifest: its key, its English literal, its limit and the
// names its placeholders may use.
//
// literal is empty for the texts that have no English literal (the label of a choice), which is
// why a plugin that has choices ships an English file. words marks the keywords, translated as one
// comma separated string.
type localeKey struct {
	key     string
	literal string
	runes   int
	words   bool
	scope   Scope
}

// localeKeys derives the translatable texts of a manifest.
//
// Parameters:
//   - m: validated manifest
func localeKeys(m *Manifest) []localeKey {
	var out []localeKey

	add := func(key, literal string, runes int, scope Scope) {
		if literal != "" {
			out = append(out, localeKey{key: key, literal: literal, runes: runes, scope: scope})
		}
	}

	if m.Title != "" {
		add("title", m.Title, MaxTitleRunes, Scope{})
	}

	add("description", m.Description, MaxDescRunes, Scope{})

	for _, r := range m.Requires {
		add("requires."+r.Bin+".hint", r.Hint, MaxDescRunes, Scope{})
	}

	for _, set := range [][]Entry{m.Commands, m.ItemActions, m.Actions} {
		for i := range set {
			out = append(out, entryKeys(m, &set[i], set[i].Table+"."+set[i].ID)...)
		}
	}

	for i := range m.Lists {
		out = append(out, listKeys(m, &m.Lists[i])...)
	}

	for i := range m.Settings {
		out = append(out, settingKeys(m, &m.Settings[i])...)
	}

	return out
}

// entryKeys derives the translatable texts of one contribution.
//
// Parameters:
//   - m: manifest, for the scope of the placeholders
//   - e: contribution
//   - base: key prefix such as command.show or list.latest.action.notes
func entryKeys(m *Manifest, e *Entry, base string) []localeKey {
	scope := scopeFor(m, e, e.Table == "list.action")
	out := []localeKey{{key: base + ".label", literal: e.Label, runes: MaxLabelRunes, scope: scope}}

	if e.Description != "" {
		out = append(out, localeKey{
			key: base + ".description", literal: e.Description,
			runes: MaxDescRunes, scope: scope,
		})
	}

	if len(e.Keywords) > 0 {
		out = append(out, localeKey{
			key: base + ".keywords", literal: strings.Join(e.Keywords, ","),
			runes: MaxKeywords * (MaxKeywordRunes + 1), words: true,
		})
	}

	if e.Confirm.Text != "" {
		out = append(out, localeKey{
			key: base + ".confirm", literal: e.Confirm.Text,
			runes: maxConfirmRunes, scope: scope,
		})
	}

	for _, f := range e.Fields {
		fb := base + ".field." + f.ID
		out = append(out, localeKey{
			key: fb + ".label", literal: f.Label, runes: MaxLabelRunes,
			scope: scope,
		})

		if f.Hint != "" {
			out = append(out, localeKey{
				key: fb + ".hint", literal: f.Hint, runes: MaxDescRunes,
				scope: scope,
			})
		}

		for _, ch := range f.Choices {
			out = append(out, localeKey{key: fb + ".choice." + choiceKey(ch), runes: MaxLabelRunes})
		}
	}

	return out
}

// choiceKey gives the catalog key segment of a choice value: the value itself, or "_" for the
// empty value.
//
// Parameters:
//   - value: static choice
func choiceKey(value string) string {
	if value == "" {
		return "_"
	}

	return value
}

// listKeys derives the translatable texts of a [[list]].
//
// Parameters:
//   - m: manifest, for the scope of the placeholders
//   - l: list
func listKeys(m *Manifest, l *List) []localeKey {
	base := "list." + l.ID
	out := []localeKey{{key: base + ".label", literal: l.Label, runes: MaxLabelRunes}}

	if l.Description != "" {
		out = append(out, localeKey{
			key: base + ".description", literal: l.Description,
			runes: MaxDescRunes,
		})
	}

	if len(l.Keywords) > 0 {
		out = append(out, localeKey{
			key: base + ".keywords", literal: strings.Join(l.Keywords, ","),
			runes: MaxKeywords * (MaxKeywordRunes + 1), words: true,
		})
	}

	for _, c := range l.Columns {
		out = append(out, localeKey{
			key: base + ".column." + c.ID, literal: c.Label,
			runes: MaxLabelRunes,
		})
	}

	for i := range l.Actions {
		out = append(out, entryKeys(m, &l.Actions[i], base+".action."+l.Actions[i].ID)...)
	}

	return out
}

// settingKeys derives the translatable texts of a [[setting]].
//
// Parameters:
//   - m: manifest, unused but kept so that every deriver has the same shape
//   - s: setting
func settingKeys(_ *Manifest, s *Setting) []localeKey {
	base := "setting." + s.ID
	out := []localeKey{{key: base + ".label", literal: s.Label, runes: MaxLabelRunes}}

	if s.Description != "" {
		out = append(out, localeKey{
			key: base + ".description", literal: s.Description,
			runes: MaxDescRunes,
		})
	}

	for _, ch := range s.Choices {
		out = append(out, localeKey{key: base + ".choice." + ch, runes: MaxLabelRunes})
	}

	return out
}

// TranslatableKeys lists the keys that a locale of the manifest may hold and, for a language other
// than English, should hold. It is what the parity test of the bundled plugins compares with the
// shipped files.
//
// Parameters:
//   - m: validated manifest
//
// Returns the sorted keys: title, description, <table>.<id>.label|description|keywords|confirm,
// the field texts and choice labels, setting.<id>.choice.<value>, list.<id>.column.<cid> and
// requires.<bin>.hint, for what the manifest declares.
func TranslatableKeys(m *Manifest) []string {
	keys := localeKeys(m)
	out := make([]string, len(keys))

	for i, k := range keys {
		out[i] = k.key
	}

	sort.Strings(out)

	return slices.Compact(out)
}

// MergeLocales combines the [i18n.<lang>] tables of the manifest with the locale files, the file
// winning for a key defined twice.
//
// Parameters:
//   - m: manifest
//   - files: flat tables read with ParseLocale, by language; may be nil
//
// Returns a new table per language that has one source.
func MergeLocales(m *Manifest, files map[string]map[string]string) map[string]map[string]string {
	out := map[string]map[string]string{}

	for _, src := range []map[string]map[string]string{m.I18n, files} {
		for lang, kv := range src {
			if out[lang] == nil {
				out[lang] = map[string]string{}
			}

			for k, v := range kv {
				out[lang][k] = v
			}
		}
	}

	return out
}

// CheckLocales checks the translations of a plugin against its manifest.
//
// Every value is checked: valid text, the length limit of its kind, the keyword form, and the
// placeholders it uses must exist in the contribution it belongs to (locale_invalid). Then parity:
// a key outside TranslatableKeys is locale_extra_key, and a key missing is locale_missing_key,
// where English only needs the labels of choices (the one text with no literal in the manifest) and
// any other language needs all keys.
//
// Parameters:
//   - m: validated manifest
//   - locales: merged translations by language, see MergeLocales; English is checked even when
//     absent, so that a plugin with choices cannot forget its English file
//   - strict: true to report the parity problems as errors, false for warnings; the value problems
//     are always errors
//
// Returns the issues, sorted by language then key, with the path locales.<lang>.<key>.
func CheckLocales(m *Manifest, locales map[string]map[string]string, strict bool) []Issue {
	c := &collector{}
	keys := localeKeys(m)
	byKey := make(map[string]localeKey, len(keys))

	for _, k := range keys {
		byKey[k.key] = k
	}

	langs := sortedKeys(locales)
	if !slices.Contains(langs, "en") {
		langs = append([]string{"en"}, langs...)
	}

	sev := c.warn
	if strict {
		sev = c.add
	}

	for _, lang := range langs {
		kv := locales[lang]
		base := "locales." + lang

		for _, key := range sortedKeys(kv) {
			k, known := byKey[key]
			if !known {
				sev(CodeLocaleExtraKey, joinKey(base, key), key)

				continue
			}

			checkLocaleValue(c, joinKey(base, key), kv[key], k)
		}

		for _, k := range keys {
			_, have := kv[k.key]
			needed := lang != "en" || k.literal == ""

			if needed && !have {
				sev(CodeLocaleMissingKey, joinKey(base, k.key), k.key)
			}
		}
	}

	return c.issues
}

// checkLocaleValue checks one translated text.
//
// Parameters:
//   - c: collector that receives the issues
//   - path: issue path of the value
//   - value: translated text
//   - k: key it translates
func checkLocaleValue(c *collector, path, value string, k localeKey) {
	if utf8.RuneCountInString(value) > k.runes {
		c.add(CodeLocaleInvalid, path, "length", itoa(k.runes))

		return
	}

	if k.words {
		words := strings.Split(value, ",")
		if len(words) > MaxKeywords {
			c.add(CodeLocaleInvalid, path, "keywords", itoa(MaxKeywords))

			return
		}

		for _, w := range words {
			if utf8.RuneCountInString(strings.TrimSpace(w)) > MaxKeywordRunes {
				c.add(CodeLocaleInvalid, path, "keyword", itoa(MaxKeywordRunes))

				return
			}
		}

		return
	}

	t, terr := ParseTemplate(value)
	if terr != nil {
		c.add(CodeLocaleInvalid, path, "placeholder", terr.Reason)

		return
	}

	for _, p := range t.Placeholders() {
		if code := k.scope.Check(p); code != "" {
			c.add(CodeLocaleInvalid, path, "placeholder", p.Name)

			return
		}
	}
}

// Catalog resolves the texts of a plugin for a language.
//
// It holds the merged translations of a plugin; the manifest literal is given at each lookup,
// because it already lives in the manifest.
type Catalog struct {
	byLang map[string]map[string]string
}

// NewCatalog builds a Catalog.
//
// Parameters:
//   - locales: merged translations by language, see MergeLocales; may be nil
func NewCatalog(locales map[string]map[string]string) Catalog {
	return Catalog{byLang: locales}
}

// Text looks a key up: the requested language, then English, then the literal of the manifest. A
// missing key is never an error.
//
// Parameters:
//   - lang: requested language such as "fr"; unknown languages fall back to English
//   - key: translation key, see TranslatableKeys
//   - literal: English text from the manifest, returned when no catalog has the key
//
// Returns the text, cleaned as a single line. Placeholders are left in place: expanding them is
// the job of the substitution step.
func (c Catalog) Text(lang, key, literal string) string {
	for _, l := range []string{lang, "en"} {
		if v, ok := c.byLang[l][key]; ok {
			return CleanLine(v, maxLocaleValueBytes)
		}
	}

	return CleanLine(literal, maxLocaleValueBytes)
}
