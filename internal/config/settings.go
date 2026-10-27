package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"

	"github.com/totecli/tote/internal/store"
)

// schemaVersion is the only config.toml schema understood by this version of tote.
const schemaVersion = 1

// maxSettingsSize is the largest config.toml read, in bytes.
//
// The file holds a handful of short keys, so the cap only stops a corrupted or hostile file
// from filling the memory or from nesting values deep enough to overflow the TOML parser.
const maxSettingsSize = 64 << 10

// ParseError reports a config file that cannot be decoded, with the position of the problem.
//
// File is the path given to LoadSettings. Line and Column are 1-based, and 0 when the position
// is unknown. Msg is the message of the TOML library, or of the check that found the problem.
// Unwrap returns the validation error behind Msg when there is one, so errors.Is keeps working.
type ParseError struct {
	File   string
	Line   int
	Column int
	Msg    string
	err    error
}

// Error implements error.
func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Msg)
	}

	return e.File + ": " + e.Msg
}

// Unwrap returns the validation error that Msg describes, or nil for a syntax error.
func (e *ParseError) Unwrap() error { return e.err }

// Settings is the content of config.toml.
//
// Schema is the file format version and must be 1; an absent key counts as 1. Lang is "en", "fr" or
// empty to follow the environment, Editor is a command line split on spaces and never run through a
// shell, and ReposRoot is the default directory scanned for repositories. The remaining fields are
// grouped by table: Connect, Repos, Shell, Update and UI. Shell.CompatMN is false by default
// because tote does not migrate from mn on purpose: there is no compatibility function to keep
// alive, and Shell.ExtraNames lists additional function names to expose. Plugins holds the
// [plugins.<id>] tables keyed by plugin id; it is nil when the file has none, and an id without a
// table is enabled.
type Settings struct {
	Schema    int                     `toml:"schema"`
	Lang      string                  `toml:"lang"`
	Editor    string                  `toml:"editor"`
	ReposRoot string                  `toml:"repos_root"`
	Connect   ConnectConfig           `toml:"connect"`
	Repos     ReposConfig             `toml:"repos"`
	Shell     ShellConfig             `toml:"shell"`
	Update    UpdateConfig            `toml:"update"`
	UI        UIConfig                `toml:"ui"`
	Plugins   map[string]PluginConfig `toml:"plugins"`
}

// maxPrefixMin is the largest value of connect.prefix_min: a record name has at most 64 bytes, so
// a prefix of more characters could never be typed.
const maxPrefixMin = 64

// ConnectConfig is the [connect] table.
//
// PrefixMin is the minimum number of characters before a prefix connects (1 to 64) and
// AfterDisconnect is either "menu" (the default) or "exit". It tells the full-screen interface
// what to do when a connection ends: "menu" goes back to the list and "exit" closes the interface
// without printing anything. The command line is not concerned, since "tote co NAME" replaces
// itself by the connection and has nothing left to do afterwards. Keepalive, true by default,
// lets both the interface and "tote co NAME" add ssh keepalive options to a plain ssh connection
// whose ssh configuration has none of its own; it must be a boolean.
type ConnectConfig struct {
	PrefixMin       int    `toml:"prefix_min"`
	AfterDisconnect string `toml:"after_disconnect"`
	Keepalive       bool   `toml:"keepalive"`
}

// ReposConfig is the [repos] table.
//
// AfterOpen is what happens once a repository is opened: "return", "quit" or "cd".
type ReposConfig struct {
	AfterOpen string `toml:"after_open"`
}

// ShellConfig is the [shell] table.
//
// CompatMN enables a compatibility function named mn and ExtraNames lists extra function names.
type ShellConfig struct {
	CompatMN   bool     `toml:"compat_mn"`
	ExtraNames []string `toml:"extra_names"`
}

// UpdateConfig is the [update] table.
//
// Check enables the periodic, asynchronous check for a new version.
type UpdateConfig struct {
	Check bool `toml:"check"`
}

// UIConfig is the [ui] table.
//
// Theme is "auto", "dark", "light" or "mono" and Welcome is "box", "compact" or "off". Compact
// tells when the one-line header replaces the welcome box: "auto" below 70 columns or 20 rows,
// "always", or "never" to keep the box on any terminal that is not too small to draw anything.
// Width is how wide the page is drawn: "auto" (the default) for the whole terminal, or "120" or
// "160" to cap the page at that many cells; on a wider terminal a capped page is centered. It is a
// string so that it reads and writes like the other choices; an absent key counts as "auto". Mouse
// turns the mouse support of the interface on (the default) or off; with it off the interface
// never asks the terminal for mouse events, so that the terminal selects text as usual.
type UIConfig struct {
	Theme   string `toml:"theme"`
	Welcome string `toml:"welcome"`
	Compact string `toml:"compact"`
	Width   string `toml:"width"`
	Mouse   bool   `toml:"mouse"`
}

// DefaultSettings returns the settings used when config.toml is missing or incomplete.
func DefaultSettings() Settings {
	return Settings{
		Schema:  schemaVersion,
		Connect: ConnectConfig{PrefixMin: 2, AfterDisconnect: "menu", Keepalive: true},
		Repos:   ReposConfig{AfterOpen: "return"},
		Update:  UpdateConfig{Check: true},
		UI: UIConfig{
			Theme: "auto", Welcome: "box", Compact: "auto", Width: "auto",
			Mouse: true,
		},
	}
}

// Kinds of Warning.
const (
	WarnUnknownKey   = "unknown_key"
	WarnInvalidValue = "invalid_value"
)

// Warning reports one setting of config.toml that tote does not use, while the rest of the file
// applies.
//
// Key is the dotted path of the setting, Line its 1-based line or 0 when it could not be found,
// Kind is WarnUnknownKey or WarnInvalidValue, and Msg is the English reason of an invalid value,
// empty for an unknown key. The setting keeps its default value.
type Warning struct {
	Key  string
	Line int
	Kind string
	Msg  string
}

// LoadSettings reads config.toml on top of the default settings.
//
// Parameters:
//   - path: location of config.toml; a missing file is not an error
//
// Returns the settings of LoadSettingsWarn and its error, and drops the warnings. Callers that
// can tell the user use LoadSettingsWarn.
func LoadSettings(path string) (Settings, error) {
	s, _, err := LoadSettingsWarn(path)

	return s, err
}

// LoadSettingsWarn reads config.toml on top of the default settings and reports what it ignored.
//
// Only a file that cannot be understood at all is refused: a syntax error, a schema other than 1,
// a structure too deep to decode, a plugins key that is not a table, or a file that is not a
// regular file or is larger than 64 KiB. Anything else is read key by key. An unknown key, or a
// value of a known key that has the wrong type or is not allowed (an enum value, a number below
// its bound), becomes a Warning and the key keeps its default; the other keys apply. A file
// written by a newer tote therefore still works, with its new keys ignored. An entry of
// [plugins.<id>] whose id breaks the grammar is ignored; one that is not a table, or whose enabled
// is not a boolean, disables the plugin, because a switch that cannot be read fails closed.
//
// Parameters:
//   - path: location of config.toml; a missing file is not an error
//
// Returns the defaults and no warning when the file does not exist. Otherwise it returns the
// settings and the warnings ordered by line, or an error carrying the file name: a *ParseError
// with the line and the column for a syntax error or a schema problem.
func LoadSettingsWarn(path string) (Settings, []Warning, error) {
	s := DefaultSettings()

	data, err := readSettings(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultSettings(), nil, nil
		}

		return Settings{}, nil, err
	}

	if se := store.CheckShape(data, store.ShapeUser); se != nil {
		return Settings{}, nil, &ParseError{File: path, Line: se.Line, Column: 1, Msg: se.Error()}
	}

	var doc map[string]any

	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			line, col := store.SyntaxPosition(data, perr.Position)

			return Settings{}, nil, &ParseError{
				File: path, Line: line, Column: col, Msg: perr.Message,
			}
		}

		return Settings{}, nil, fmt.Errorf("read %s: %w", path, err)
	}

	if err := checkPluginsTable(path, md, data); err != nil {
		return Settings{}, nil, err
	}

	if err := checkSchema(path, doc, data); err != nil {
		return Settings{}, nil, err
	}

	w := &reader{data: data}
	w.table(reflect.ValueOf(&s).Elem(), doc, "")
	w.plugins(&s, doc)

	defaults := DefaultSettings()

	reported := map[string]bool{}

	for _, fe := range s.fieldErrors() {
		if reported[fe.key] {
			continue
		}

		reported[fe.key] = true
		w.warn(fe.key, WarnInvalidValue, fe.err.Error())
		resetKey(reflect.ValueOf(&s).Elem(), reflect.ValueOf(defaults), fe.key)
	}

	sort.SliceStable(w.warnings, func(i, j int) bool {
		a, b := w.warnings[i], w.warnings[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}

		return a.Key < b.Key
	})

	return s, w.warnings, nil
}

// checkSchema refuses a file whose schema key is not the integer 1.
//
// Parameters:
//   - path: file name for the message
//   - doc: decoded document
//   - data: document text, used to find the line
//
// Returns nil when the key is absent or 1, else a *ParseError at the line of the key.
func checkSchema(path string, doc map[string]any, data []byte) error {
	v, ok := doc["schema"]
	if !ok {
		return nil
	}

	n, isInt := v.(int64)
	if isInt && n == schemaVersion {
		return nil
	}

	msg := "schema must be the integer 1, only schema 1 is supported"
	if isInt {
		msg = fmt.Sprintf("schema is %d, only schema 1 is supported", n)
	}

	line, col := keyLine(data, []string{"schema"})

	return &ParseError{File: path, Line: line, Column: col, Msg: msg}
}

// reader walks a decoded document and applies it to the settings, key by key.
//
// data is the document text, used to find the line of a warning, and warnings collects what was
// ignored.
type reader struct {
	data     []byte
	warnings []Warning
}

// warn records one ignored setting.
//
// Parameters:
//   - key: dotted path of the setting
//   - kind: WarnUnknownKey or WarnInvalidValue
//   - msg: English reason, empty for an unknown key
func (r *reader) warn(key, kind, msg string) {
	line, _ := keyLine(r.data, strings.Split(key, "."))
	r.warnings = append(r.warnings, Warning{Key: key, Line: line, Kind: kind, Msg: msg})
}

// table applies one decoded table to a struct, matching keys with the toml tags of its fields.
//
// The schema key and the plugins tables are read elsewhere and skipped here.
//
// Parameters:
//   - v: struct to fill, addressable
//   - doc: decoded table
//   - prefix: dotted path of the table, empty for the root
func (r *reader) table(v reflect.Value, doc map[string]any, prefix string) {
	fields := map[string]reflect.Value{}
	for i := 0; i < v.NumField(); i++ {
		fields[v.Type().Field(i).Tag.Get("toml")] = v.Field(i)
	}

	for _, name := range slices.Sorted(maps.Keys(doc)) {
		key := prefix + name
		field, known := fields[name]

		switch {
		case prefix == "" && (name == "schema" || name == "plugins"):
		case !known:
			r.warn(key, WarnUnknownKey, "")
		case field.Kind() == reflect.Struct:
			sub, isTable := doc[name].(map[string]any)
			if !isTable {
				r.warn(key, WarnInvalidValue, key+" must be a table, found "+typeName(doc[name]))

				continue
			}

			r.table(field, sub, key+".")
		default:
			if msg := assign(field, doc[name]); msg != "" {
				r.warn(key, WarnInvalidValue, key+" "+msg)
			}
		}
	}
}

// assign stores a decoded value in a field of the settings when the type fits.
//
// Parameters:
//   - field: addressable string, int, bool or []string field
//   - value: decoded TOML value
//
// Returns an empty string when the value was stored, else the reason it was not, which starts
// with a verb so that the key can be put in front.
func assign(field reflect.Value, value any) string {
	switch field.Kind() {
	case reflect.String:
		if s, ok := value.(string); ok {
			field.SetString(s)

			return ""
		}
	case reflect.Int:
		if n, ok := value.(int64); ok && int64(int(n)) == n {
			field.SetInt(n)

			return ""
		}
	case reflect.Bool:
		if b, ok := value.(bool); ok {
			field.SetBool(b)

			return ""
		}
	case reflect.Slice:
		if list, ok := value.([]any); ok {
			out := make([]string, len(list))
			for i, e := range list {
				s, isString := e.(string)
				if !isString {
					return "must be a list of strings, found " + typeName(e) + " in it"
				}
				out[i] = s
			}
			field.Set(reflect.ValueOf(out))

			return ""
		}
	}

	return "must be " + kindName(field) + ", found " + typeName(value)
}

// kindName describes the type a field takes, for a message.
//
// Parameters:
//   - field: string, int, bool or []string field
func kindName(field reflect.Value) string {
	switch field.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Int:
		return "an integer"
	case reflect.Bool:
		return "true or false"
	}

	return "a list of strings"
}

// typeName names the TOML type of a decoded value, for a message.
//
// Parameters:
//   - v: value decoded into an any
func typeName(v any) string {
	switch v.(type) {
	case string:
		return "a string"
	case int64:
		return "an integer"
	case float64:
		return "a float"
	case bool:
		return "a boolean"
	case []any, []map[string]any:
		return "a list"
	case map[string]any:
		return "a table"
	}

	return "a date or time"
}

// plugins applies the [plugins.<id>] tables of the document.
//
// Parameters:
//   - s: settings to fill
//   - doc: decoded document, whose plugins key is a table (checkPluginsTable)
func (r *reader) plugins(s *Settings, doc map[string]any) {
	tables, _ := doc["plugins"].(map[string]any)
	if len(tables) == 0 {
		return
	}

	s.Plugins = map[string]PluginConfig{}

	for _, id := range slices.Sorted(maps.Keys(tables)) {
		key := "plugins." + id

		if err := ValidPluginID(id); err != nil {
			r.warn(key, WarnInvalidValue, (&pluginIDError{id: id, err: err}).Error())

			continue
		}

		off := false
		cfg := PluginConfig{}

		table, isTable := tables[id].(map[string]any)
		if !isTable {
			r.warn(key, WarnInvalidValue, key+" must be a table, found "+typeName(tables[id]))
			cfg.Enabled = &off
			s.Plugins[id] = cfg

			continue
		}

		for _, name := range slices.Sorted(maps.Keys(table)) {
			if name != "enabled" {
				r.warn(key+"."+name, WarnUnknownKey, "")

				continue
			}

			if b, ok := table[name].(bool); ok {
				cfg.Enabled = &b

				continue
			}

			r.warn(key+".enabled", WarnInvalidValue,
				key+".enabled must be true or false, found "+typeName(table[name])+
					", the plugin stays off")
			cfg.Enabled = &off
		}

		s.Plugins[id] = cfg
	}
}

// resetKey puts the default value back in the field a dotted key designates.
//
// Parameters:
//   - v: settings struct to change, addressable
//   - def: default settings struct
//   - key: dotted path made of toml tags
func resetKey(v, def reflect.Value, key string) {
	for part, rest, more := strings.Cut(key, "."); ; part, rest, more = strings.Cut(rest, ".") {
		found := false

		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).Tag.Get("toml") == part {
				v, def, found = v.Field(i), def.Field(i), true

				break
			}
		}

		if !found {
			return
		}

		if !more {
			v.Set(def)

			return
		}
	}
}

// readSettings reads config.toml, which must be a regular file of at most maxSettingsSize bytes.
//
// The file is opened without blocking so that a FIFO in its place gives an error instead of a
// hang.
//
// Parameters:
//   - path: location of config.toml, symlinks are followed
//
// Returns the content, an error that wraps fs.ErrNotExist when the file is missing, or another
// error naming the file.
func readSettings(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}

		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read %s: not a regular file", path)
	}

	data, err := io.ReadAll(io.LimitReader(f, maxSettingsSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxSettingsSize {
		return nil, fmt.Errorf("read %s: file larger than %d bytes", path, maxSettingsSize)
	}

	return data, nil
}

// fieldError is one invalid value found by fieldErrors: the dotted key and the reason.
type fieldError struct {
	key string
	err error
}

// fieldErrors lists the values of the known keys that are out of bounds or not allowed.
//
// Returns the numeric bound, the enum values and the extra names, in the order of the file
// layout. The schema and the plugin identifiers are not part of it, see Validate.
func (s Settings) fieldErrors() []fieldError {
	var errs []fieldError

	if s.Connect.PrefixMin < 1 || s.Connect.PrefixMin > maxPrefixMin {
		errs = append(errs, fieldError{
			"connect.prefix_min",
			fmt.Errorf("connect.prefix_min is %d, must be between 1 and %d",
				s.Connect.PrefixMin, maxPrefixMin),
		})
	}

	if lang := languagePart(s.Lang); lang != "" && lang != "en" && lang != "fr" {
		errs = append(errs, fieldError{
			"lang",
			fmt.Errorf("lang is %q, allowed values: en, fr, or empty", s.Lang),
		})
	}

	enums := []struct {
		field   string
		value   string
		allowed []string
	}{
		{"connect.after_disconnect", s.Connect.AfterDisconnect, []string{"exit", "menu"}},
		{"repos.after_open", s.Repos.AfterOpen, []string{"return", "quit", "cd"}},
		{"ui.theme", s.UI.Theme, []string{"auto", "dark", "light", "mono"}},
		{"ui.welcome", s.UI.Welcome, []string{"box", "compact", "off"}},
		{"ui.compact", s.UI.Compact, []string{"auto", "always", "never"}},
		{"ui.width", s.UI.Width, []string{"auto", "120", "160"}},
	}
	for _, e := range enums {
		if !slices.Contains(e.allowed, e.value) {
			errs = append(errs, fieldError{e.field, fmt.Errorf("%s is %q, allowed values: %s",
				e.field, e.value, strings.Join(e.allowed, ", "))})
		}
	}

	seen := map[string]bool{}
	for _, name := range s.Shell.ExtraNames {
		if err := store.ValidFuncName(name); err != nil {
			errs = append(errs, fieldError{
				"shell.extra_names",
				fmt.Errorf("shell.extra_names has %q: %w", name, err),
			})
		} else if seen[name] {
			errs = append(errs, fieldError{
				"shell.extra_names",
				fmt.Errorf("shell.extra_names lists %q twice", name),
			})
		}
		seen[name] = true
	}

	return errs
}

// Validate checks the schema version, the enum values, the numeric bounds and the extra names.
//
// This is the strict check, used on settings built in memory. LoadSettingsWarn does not refuse a
// file for these faults: it turns each into a Warning and keeps the default.
//
// Returns nil when the settings are valid, else an error naming every invalid field with its
// allowed values. Each entry of shell.extra_names must be a valid function name, see
// store.ValidFuncName, and appear once. Each key of plugins must pass ValidPluginID, and the error
// for a bad key wraps ErrInvalidPluginID.
func (s Settings) Validate() error {
	var errs []error

	if s.Schema != schemaVersion {
		errs = append(errs, fmt.Errorf("schema is %d, only schema 1 is supported", s.Schema))
	}

	for _, fe := range s.fieldErrors() {
		errs = append(errs, fe.err)
	}

	for _, id := range slices.Sorted(maps.Keys(s.Plugins)) {
		if err := ValidPluginID(id); err != nil {
			errs = append(errs, &pluginIDError{id: id, err: err})
		}
	}

	return errors.Join(errs...)
}

// EffectiveEditor returns the editor command line as an argv slice.
//
// The editor is the first non-blank of the editor setting, $VISUAL and $EDITOR, else "vim". It is
// split on white space only: no quoting, no expansion and no shell is involved.
//
// Parameters:
//   - s: settings holding the optional editor command
//   - getenv: environment lookup, usually os.Getenv
//
// Returns a non-empty argv slice.
func EffectiveEditor(s Settings, getenv func(string) string) []string {
	for _, candidate := range []string{s.Editor, getenv("VISUAL"), getenv("EDITOR")} {
		if argv := strings.Fields(candidate); len(argv) > 0 {
			return argv
		}
	}

	return []string{"vim"}
}

// EffectiveLang returns the interface language, either "en" or "fr".
//
// The order is TOTE_LANG, the lang setting, then LC_ALL, LC_MESSAGES and LANG. A value counts when
// its language part (before '_', '.' or '@') is "en" or "fr", ignoring case; other values are
// skipped. English is the default.
//
// Parameters:
//   - s: settings holding the optional language
//   - getenv: environment lookup, usually os.Getenv
func EffectiveLang(s Settings, getenv func(string) string) string {
	candidates := []string{
		getenv("TOTE_LANG"), s.Lang, getenv("LC_ALL"), getenv("LC_MESSAGES"), getenv("LANG"),
	}
	for _, c := range candidates {
		if lang := languagePart(c); lang == "en" || lang == "fr" {
			return lang
		}
	}

	return "en"
}

// languagePart extracts the lowercase language code from a locale string such as "fr_FR.UTF-8".
//
// Parameters:
//   - locale: locale or language value, possibly empty
func languagePart(locale string) string {
	if i := strings.IndexAny(locale, "_.@"); i >= 0 {
		locale = locale[:i]
	}

	return strings.ToLower(locale)
}
