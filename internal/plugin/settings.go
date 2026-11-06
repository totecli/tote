package plugin

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/totecli/tote/internal/store"
)

// Where an effective setting value comes from.
const (
	SourceEnvironment = "environment"
	SourceFile        = "file"
	SourceDefault     = "default"
)

// Codes of a value that was ignored or refused, a setting or a form field, and of a settings file
// that was ignored as a whole. Each has a message plugin_reject_<code> in both catalogs.
const (
	RejectType    = "type"
	RejectLength  = "length"
	RejectText    = "text"
	RejectPattern = "pattern"
	RejectRange   = "range"
	RejectChoice  = "choice"
	RejectItems   = "items"
	RejectPath    = "path"
	RejectMissing = "missing"
	RejectSyntax  = "syntax"
	RejectSchema  = "schema"
	RejectShape   = "shape"
	RejectRead    = "read"

	RejectRequired = "required"
	RejectMaxLen   = "max_len"
	RejectBool     = "bool"
	RejectUTF8     = "utf8"
	RejectControl  = "control"
	RejectUnknown  = "unknown"
)

// Limits of a settings file and of a path value.
const (
	maxSettingsFile = 1 << 20
	maxPathBytes    = 4096
	settingsSchema  = 1
)

// settingsShape bounds the structure of plugins/<id>.toml before the TOML library sees it. The
// file is flat: one key per setting, a list being the only nesting.
var settingsShape = store.ShapeLimits{
	MaxBytes: maxSettingsFile, MaxNesting: 3, MaxKeyParts: 2,
	MaxHeaders: 16, MaxKeys: 1024,
}

// SettingRejection is a value that was ignored: where it came from, why, and the value itself,
// cleaned and cut. The settings screen, check and info show it.
type SettingRejection struct {
	Source string
	Code   string
	Raw    string
}

// SettingValue is the effective value of one setting.
//
// Value is a string, a bool, an int64 or a []string according to Type. Source says where it comes
// from. Rejected lists the values of higher precedence that were ignored because they do not fit
// the declared type, so that the user sees why a value did not apply.
type SettingValue struct {
	ID       string
	Type     string
	Value    any
	Source   string
	Rejected []SettingRejection
}

// SettingsProblem is a settings file that was ignored as a whole: a code, the 1-based line when
// the syntax error has one, and the cleaned message of the TOML library.
type SettingsProblem struct {
	Code string
	Line int
	Msg  string
}

// SettingsView is the effective settings of a plugin, in the order of the manifest.
//
// Unknown lists the keys of the settings file that no setting declares, sorted. Problem is nil
// unless the file exists and could not be used, in which case none of its values applied and a
// write must not replace it.
type SettingsView struct {
	Values  []SettingValue
	Unknown []string
	File    string
	Exists  bool
	Problem *SettingsProblem
}

// SettingsInput is where the settings of a plugin come from.
//
// File is plugins/<id>.toml, empty for none. Lookup reads the environment of tote, nil meaning
// os.LookupEnv; a variable that is set but empty counts as unset. Home expands a leading "~" of a
// path value.
type SettingsInput struct {
	PluginID string
	File     string
	Lookup   func(string) (string, bool)
	Home     string
}

// hostKeyPrefix starts the root keys of plugins/<id>.toml that belong to tote itself. A setting id
// starts with a letter, so a key of this kind never collides with a setting, and a later release
// can keep its own data there; this release neither reads nor reports them, and keeps them when
// it saves.
const hostKeyPrefix = "_"

// EffectiveSettings resolves the settings of a plugin: the environment first, then the file, then
// the default of the manifest, or the zero value of the type when there is none.
//
// Every value is checked against the declared type, limits, pattern and choices. An invalid value
// is ignored for that setting and the next source applies; the rejection is recorded. A settings
// file that does not parse, is too large or has another schema is ignored whole with a problem.
// The variable of a setting is TOTE_PLUGIN_<ID>_<SETTING>: a bool is 1, true, yes or on, or 0,
// false, no or off in any case; an int is decimal; a list is comma-separated and trimmed, the
// empty text being the empty list; an enum is one of its choices. The effective value always
// satisfies its setting, even when no source does.
//
// Parameters:
//   - m: validated manifest
//   - in: sources of the values
//
// Returns the view. Nothing is written and nothing is executed.
func EffectiveSettings(m *Manifest, in SettingsInput) SettingsView {
	view := SettingsView{File: in.File}

	file, problem, exists := readSettingsFile(in.File)
	view.Exists, view.Problem = exists, problem

	lookup := in.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}

	known := map[string]bool{"schema": true}

	for i := range m.Settings {
		s := &m.Settings[i]
		known[s.ID] = true

		view.Values = append(view.Values, resolveSetting(s, in, lookup, file))
	}

	for key := range file {
		if !known[key] && !strings.HasPrefix(key, hostKeyPrefix) {
			view.Unknown = append(view.Unknown, CleanLine(key, issueArgRunes))
		}
	}

	sort.Strings(view.Unknown)

	return view
}

// resolveSetting applies the precedence to one setting.
//
// Parameters:
//   - s: setting of the manifest
//   - in: sources of the values
//   - lookup: environment lookup
//   - file: decoded settings file, nil when there is none
func resolveSetting(s *Setting, in SettingsInput, lookup func(string) (string, bool),
	file map[string]any,
) SettingValue {
	out := SettingValue{ID: s.ID, Type: s.Type}
	chk := checker{s: s, home: in.Home}

	if raw, ok := lookup(SettingEnvName(in.PluginID, s.ID)); ok && raw != "" {
		v, code := chk.fromEnv(raw)
		if code == "" {
			out.Value, out.Source = v, SourceEnvironment

			return out
		}

		out.Rejected = append(out.Rejected, SettingRejection{
			SourceEnvironment, code,
			CleanLine(raw, issueArgRunes),
		})
	}

	if raw, ok := file[s.ID]; ok {
		v, code := chk.check(raw)
		if code == "" {
			out.Value, out.Source = v, SourceFile

			return out
		}

		out.Rejected = append(out.Rejected, SettingRejection{
			SourceFile, code,
			CleanLine(fmt.Sprint(raw), issueArgRunes),
		})
	}

	out.Value, out.Source = s.zeroOrDefault(), SourceDefault

	return out
}

// zeroOrDefault returns the default of a setting, or the zero value of its type: "", false, 0,
// the first choice of an enum, "" for a path and the empty list. An int zero outside the bounds
// is moved to the nearest bound, so that the effective value always satisfies the setting.
func (s *Setting) zeroOrDefault() any {
	if s.HasDefault {
		if l, ok := s.Default.([]string); ok {
			return append([]string{}, l...)
		}

		return s.Default
	}

	switch s.Type {
	case "bool":
		return false
	case "int":
		lo, hi := s.bounds()

		return min(max(int64(0), lo), hi)
	case "enum":
		if len(s.Choices) > 0 {
			return s.Choices[0]
		}
	case "list":
		return []string{}
	}

	return ""
}

// checker validates a value against one setting.
type checker struct {
	s    *Setting
	home string
}

// fromEnv parses the text of an environment variable to the type of the setting and checks it.
//
// Parameters:
//   - raw: value of the variable, not empty
func (c checker) fromEnv(raw string) (any, string) {
	switch c.s.Type {
	case "bool":
		switch strings.ToLower(raw) {
		case "1", "true", "yes", "on":
			return true, ""
		case "0", "false", "no", "off":
			return false, ""
		}

		return nil, RejectType
	case "int":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, RejectType
		}

		return c.check(n)
	case "list":
		return c.check(splitList(raw))
	}

	return c.check(raw)
}

// splitList cuts a comma-separated text and trims each item. An item that is empty after the trim
// stays in the result, so that the check rejects "a,,b" instead of hiding it.
//
// Parameters:
//   - raw: text of the variable
func splitList(raw string) []any {
	parts := strings.Split(raw, ",")
	out := make([]any, len(parts))

	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}

	return out
}

// check validates a decoded value, a TOML value or one built from the environment.
//
// Parameters:
//   - v: string, bool, int64 or []any of strings
//
// Returns the normalised value, or a nil value and the code of the first rule it breaks.
func (c checker) check(v any) (any, string) {
	switch c.s.Type {
	case "string":
		return c.text(v)
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, RejectType
		}

		return b, ""
	case "int":
		n, ok := v.(int64)

		lo, hi := c.s.bounds()
		switch {
		case !ok:
			return nil, RejectType
		case n < lo || n > hi:
			return nil, RejectRange
		}

		return n, ""
	case "enum":
		s, ok := v.(string)

		switch {
		case !ok:
			return nil, RejectType
		case !containsString(c.s.Choices, s):
			return nil, RejectChoice
		}

		return s, ""
	case "path":
		return c.path(v)
	}

	return c.list(v)
}

// text checks a string value: valid single-line text, the length limit and the pattern. The empty
// string is always accepted, it is the zero value.
//
// Parameters:
//   - v: value to check
func (c checker) text(v any) (any, string) {
	s, ok := v.(string)
	if !ok {
		return nil, RejectType
	}

	if !plainText(s) {
		return nil, RejectText
	}

	if utf8.RuneCountInString(s) > c.s.MaxLen {
		return nil, RejectLength
	}

	if s != "" && c.s.Pattern != "" {
		re, err := regexp.Compile(c.s.Pattern)
		if err != nil || !re.MatchString(s) {
			return nil, RejectPattern
		}
	}

	return s, ""
}

// path checks a path value: empty, "~", or an absolute path or one below "~/", without control
// characters and within 4096 bytes, existing when the setting asks for it.
//
// Parameters:
//   - v: value to check
func (c checker) path(v any) (any, string) {
	s, ok := v.(string)
	if !ok {
		return nil, RejectType
	}

	switch {
	case s == "":
		return s, ""
	case len(s) > maxPathBytes:
		return nil, RejectLength
	case !plainText(s):
		return nil, RejectText
	case s != "~" && !strings.HasPrefix(s, "~/") && !filepath.IsAbs(s):
		return nil, RejectPath
	}

	if c.s.MustExist {
		if _, err := os.Stat(expandHome(s, c.home)); err != nil {
			return nil, RejectMissing
		}
	}

	return s, ""
}

// expandHome replaces a leading "~" with the home directory.
//
// Parameters:
//   - p: path, possibly "~" or starting with "~/"
//   - home: home directory; when empty the path is returned as it is
func expandHome(p, home string) string {
	if home == "" {
		return p
	}

	if p == "~" {
		return home
	}

	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}

	return p
}

// list checks a list value: at most max_items items, each a non-empty valid text within max_len
// and matching the pattern.
//
// Parameters:
//   - v: []any of strings, as the TOML library and splitList build it
func (c checker) list(v any) (any, string) {
	items, ok := v.([]any)
	if !ok {
		return nil, RejectType
	}

	if len(items) == 1 && items[0] == "" {
		return []string{}, ""
	}

	if len(items) > c.s.MaxItems {
		return nil, RejectItems
	}

	out := make([]string, 0, len(items))

	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			return nil, RejectType
		}

		if s == "" {
			return nil, RejectText
		}

		if _, code := c.text(s); code != "" {
			return nil, code
		}

		out = append(out, s)
	}

	return out, ""
}

// readSettingsFile reads and decodes plugins/<id>.toml.
//
// The file belongs to the user and may be a symbolic link into a dotfiles repository, so links are
// followed. It must be a regular file; it is opened without blocking, read up to 1 MiB, checked
// for pathological structure and then decoded.
//
// Parameters:
//   - path: location of the file, empty for none
//
// Returns the top-level table, the problem that made the file unusable, and whether the file
// exists. A missing file is not a problem.
func readSettingsFile(path string) (map[string]any, *SettingsProblem, bool) {
	if path == "" {
		return nil, nil, false
	}

	data, err := readLimited(path, maxSettingsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, false
	}

	if err != nil {
		return nil, &SettingsProblem{Code: RejectRead}, true
	}

	if se := store.CheckShape(data, settingsShape); se != nil {
		return nil, &SettingsProblem{Code: RejectShape, Line: se.Line}, true
	}

	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		p := &SettingsProblem{Code: RejectSyntax}

		var perr toml.ParseError
		if errors.As(err, &perr) {
			p.Line, _ = store.SyntaxPosition(data, perr.Position)
			p.Msg = CleanLine(perr.Message, issueArgRunes)
		}

		return nil, p, true
	}

	if v, ok := doc["schema"]; ok && v != int64(settingsSchema) {
		return nil, &SettingsProblem{Code: RejectSchema}, true
	}

	return doc, nil, true
}

// readLimited reads a regular file of at most limit bytes without blocking on a FIFO.
//
// Parameters:
//   - path: file to read, symbolic links are followed
//   - limit: largest size in bytes
//
// Returns the content, an error wrapping fs.ErrNotExist for a missing file, or another error.
func readLimited(path string, limit int) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}

	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}

	if len(data) > limit {
		return nil, errors.New("file too large")
	}

	return data, nil
}
