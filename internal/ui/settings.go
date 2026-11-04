package ui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/BurntSushi/toml"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// maxConfigSize is the largest config.toml read for the screen of settings, as in the loader.
const maxConfigSize = 64 << 10

// settingKind says how a row of the settings screen is changed.
type settingKind int

const (
	// settingChoice is one value picked from a short list.
	settingChoice settingKind = iota

	// settingBool is a yes or no switch.
	settingBool

	// settingText is a free text checked by the rules of the setting.
	settingText

	// settingInfo is a value that the settings cannot change, such as the colors in use.
	settingInfo

	// settingAction is a row that runs something, such as the check for an update.
	settingAction
)

// settingApply says when a change of a setting takes effect.
type settingApply int

const (
	// applyNone is for a row that is not stored, such as the colors in use.
	applyNone settingApply = iota

	// applyNow is a setting that the running interface follows as soon as it is written.
	applyNow

	// applyRestart is a setting that is only read when tote starts.
	applyRestart
)

// settingDef describes one row of the settings screen. The table settingDefs is the one registry
// of the settings that the screen, the toasts and the tests draw from.
//
// ID names the row in the catalog: ui_set_<id> is its name, ui_setb_<id> the short description
// shown on the row, ui_setd_<id> the full one and ui_setn_<id> an optional extra note (see the
// methods that return them). Table and Key say where the value lives in config.toml, an empty
// Table being the root of the file; the default is the one of config.DefaultSettings, see
// defaultValue. Choices are the stored values of a choice, in the order they are offered, the
// empty string standing for a key that is absent. Envs are the variables that override the file;
// when one of them is set the row is read-only. Apply says when a change takes effect and Note is
// true when the setting has an extra note.
type settingDef struct {
	id      string
	table   string
	key     string
	kind    settingKind
	choices []string
	envs    []string
	apply   settingApply
	note    bool
}

// settingDefs lists the rows of the settings screen in the order they are shown.
var settingDefs = []settingDef{
	{
		id: "lang", key: "lang", kind: settingChoice, choices: []string{"", "en", "fr"},
		envs: []string{"TOTE_LANG"}, apply: applyNow,
	},
	{id: "editor", key: "editor", kind: settingText, apply: applyNow, note: true},
	{id: "repos_root", key: "repos_root", kind: settingText, apply: applyNow, note: true},
	{
		id: "after_disconnect", table: "connect", key: "after_disconnect", kind: settingChoice,
		choices: []string{"menu", "exit"}, apply: applyNow,
	},
	{
		id: "keepalive", table: "connect", key: "keepalive", kind: settingBool, apply: applyNow,
		note: true,
	},
	{
		id: "after_open", table: "repos", key: "after_open", kind: settingChoice,
		choices: []string{"return", "quit", "cd"}, apply: applyNow,
	},
	{
		id: "theme", table: "ui", key: "theme", kind: settingChoice,
		choices: []string{"auto", "dark", "light", "mono"}, apply: applyNow,
	},
	{
		id: "welcome", table: "ui", key: "welcome", kind: settingChoice,
		choices: []string{"box", "compact", "off"}, apply: applyNow,
	},
	{
		id: "compact", table: "ui", key: "compact", kind: settingChoice,
		choices: []string{"auto", "always", "never"}, apply: applyNow,
	},
	{
		id: "width", table: "ui", key: "width", kind: settingChoice,
		choices: []string{"auto", "120", "160"}, apply: applyNow,
	},
	{id: "mouse", table: "ui", key: "mouse", kind: settingBool, apply: applyNow, note: true},
	{id: "check", table: "update", key: "check", kind: settingBool, apply: applyRestart},
	{id: "colors", kind: settingInfo, envs: []string{"TOTE_COLOR", "NO_COLOR"}, note: true},
	{id: "update", kind: settingAction, note: true},
}

// findSetting looks a setting up by its identifier.
//
// Parameters:
//   - id: identifier of the setting, such as "keepalive"
//
// Returns the setting and true, or the zero value and false when the table has none.
func findSetting(id string) (settingDef, bool) {
	for _, d := range settingDefs {
		if d.id == id {
			return d, true
		}
	}

	return settingDef{}, false
}

// origin says where the effective value of a setting comes from.
type origin int

const (
	// originDefault is the built-in value.
	originDefault origin = iota

	// originFile is a key of config.toml.
	originFile

	// originEnv is an environment variable.
	originEnv
)

// source tells where the effective value of a setting comes from.
//
// Name is the variable of an environment origin. Locked is true when the variable overrides the
// file, so that changing the file would have no effect.
type source struct {
	origin origin
	name   string
	locked bool
}

// nameKey returns the catalog key of the name of the setting.
func (d settingDef) nameKey() string {
	return "ui_set_" + d.id
}

// briefKey returns the catalog key of the short description shown on the row of the setting.
func (d settingDef) briefKey() string {
	return "ui_setb_" + d.id
}

// descKey returns the catalog key of the full description of the setting.
func (d settingDef) descKey() string {
	return "ui_setd_" + d.id
}

// noteKey returns the catalog key of the extra note of the setting, empty when it has none. The
// note says what the description does not, and never when a change applies, which comes from
// Apply.
func (d settingDef) noteKey() string {
	if !d.note {
		return ""
	}

	return "ui_setn_" + d.id
}

// applyText says in a few words when a change of the setting takes effect.
//
// Parameters:
//   - e: shared environment
//
// Returns "applied now" or "after restart", and an empty string for a row that is not stored.
func (d settingDef) applyText(e *env) string {
	switch d.apply {
	case applyNow:
		return e.t("ui_apply_now")
	case applyRestart:
		return e.t("ui_apply_restart")
	}

	return ""
}

// path returns the name of the setting as written in config.toml, such as "ui.compact".
func (d settingDef) path() string {
	if d.table == "" {
		return d.key
	}

	return d.table + "." + d.key
}

// writable reports whether the row is stored in config.toml.
func (d settingDef) writable() bool {
	return d.key != "" && d.kind != settingInfo && d.kind != settingAction
}

// value returns the value a setting has in a set of settings, as the text that config.toml
// holds. A boolean gives "true" or "false" and an absent language gives the empty string.
//
// Parameters:
//   - s: settings to read
func (d settingDef) value(s config.Settings) string {
	switch d.id {
	case "lang":
		return s.Lang
	case "editor":
		return s.Editor
	case "repos_root":
		return s.ReposRoot
	case "after_disconnect":
		return s.Connect.AfterDisconnect
	case "keepalive":
		return strconv.FormatBool(s.Connect.Keepalive)
	case "after_open":
		return s.Repos.AfterOpen
	case "theme":
		return s.UI.Theme
	case "welcome":
		return s.UI.Welcome
	case "compact":
		return s.UI.Compact
	case "width":
		return s.UI.Width
	case "mouse":
		return strconv.FormatBool(s.UI.Mouse)
	case "check":
		return strconv.FormatBool(s.Update.Check)
	}

	return ""
}

// with returns a copy of the settings where the setting has a value.
//
// Parameters:
//   - s: settings to start from
//   - v: new value, as config.Settings stores it; "true" turns a boolean on
func (d settingDef) with(s config.Settings, v string) config.Settings {
	switch d.id {
	case "lang":
		s.Lang = v
	case "editor":
		s.Editor = v
	case "repos_root":
		s.ReposRoot = v
	case "after_disconnect":
		s.Connect.AfterDisconnect = v
	case "keepalive":
		s.Connect.Keepalive = v == "true"
	case "after_open":
		s.Repos.AfterOpen = v
	case "theme":
		s.UI.Theme = v
	case "welcome":
		s.UI.Welcome = v
	case "compact":
		s.UI.Compact = v
	case "width":
		s.UI.Width = v
	case "mouse":
		s.UI.Mouse = v == "true"
	case "check":
		s.Update.Check = v == "true"
	}

	return s
}

// fileState is what config.toml holds, read for the settings screen.
//
// Present lists the settings that the file sets, by path. Err is the reason the file cannot be
// used, nil when it is missing or valid: the screen then shows the settings and refuses to
// change them, like the lists do for a file with a syntax error.
type fileState struct {
	present map[string]bool
	err     error
}

// readFileState reads config.toml.
//
// Parameters:
//   - path: location of config.toml; a missing file means that nothing is set
//
// Returns the state. The file is checked with the loader of the settings, so that what the
// screen refuses is exactly what the program refuses at start. The bytes decoded here are read
// again and checked for a hostile structure before the TOML library sees them, since the file
// may have been replaced since the loader read it.
func readFileState(path string) fileState {
	st := fileState{present: map[string]bool{}}

	if _, err := config.LoadSettings(path); err != nil {
		st.err = err

		return st
	}

	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			st.err = err
		}

		return st
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxConfigSize))
	if err != nil {
		st.err = err

		return st
	}

	if se := store.CheckShape(data, store.ShapeUser); se != nil {
		st.err = se

		return st
	}

	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		st.err = err

		return st
	}

	for _, d := range settingDefs {
		if d.writable() && hasKey(doc, d) {
			st.present[d.path()] = true
		}
	}

	return st
}

// hasKey reports whether a decoded config.toml sets a setting.
//
// Parameters:
//   - doc: the decoded file
//   - d: setting to look for
func hasKey(doc map[string]any, d settingDef) bool {
	scope := doc

	if d.table != "" {
		sub, ok := doc[d.table].(map[string]any)
		if !ok {
			return false
		}

		scope = sub
	}

	_, ok := scope[d.key]

	return ok
}

// supportedLang reports whether a locale names a language of the interface: its part before
// "_", "." or "@" is "en" or "fr", ignoring case.
//
// Parameters:
//   - locale: value of a locale variable or of TOTE_LANG
func supportedLang(locale string) bool {
	v := strings.ToLower(locale)

	if i := strings.IndexAny(v, "_.@"); i >= 0 {
		v = v[:i]
	}

	return v == "en" || v == "fr"
}

// localeVar returns the first of the variables that choose the language by locale whose value
// names a supported language, "" when none does.
//
// Parameters:
//   - get: environment lookup
func localeVar(get func(string) string) string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if supportedLang(get(name)) {
			return name
		}
	}

	return ""
}

// envDecides reports whether the value of an override variable changes anything: TOTE_LANG only
// counts when it names a supported language, as in config.EffectiveLang, and TOTE_COLOR when it
// names a color profile.
//
// Parameters:
//   - name: variable
//   - value: its value, possibly empty
func envDecides(name, value string) bool {
	switch name {
	case "TOTE_LANG":
		return supportedLang(value)
	case "TOTE_COLOR":
		_, ok := parseColorName(value)

		return ok || colorWord(value) == "always"
	}

	return value != ""
}

// sourceOf finds where the effective value of a setting comes from.
//
// Parameters:
//   - e: shared environment, for the environment variables
//   - st: what config.toml holds
//   - d: setting to look up
//
// Returns the origin. A variable that overrides the file locks the row; a variable that only
// fills in when the file says nothing (the locale for the language, $VISUAL and $EDITOR for the
// editor) does not.
func sourceOf(e *env, st fileState, d settingDef) source {
	get := envLookup(e.Environ)

	for _, name := range d.envs {
		if envDecides(name, get(name)) {
			return source{origin: originEnv, name: name, locked: true}
		}
	}

	switch d.id {
	case "lang":
		if e.Settings.Lang != "" || st.present["lang"] {
			return source{origin: originFile}
		}

		if name := localeVar(get); name != "" {
			return source{origin: originEnv, name: name}
		}
	case "editor":
		if strings.TrimSpace(e.Settings.Editor) != "" {
			return source{origin: originFile}
		}

		for _, name := range []string{"VISUAL", "EDITOR"} {
			if strings.TrimSpace(get(name)) != "" {
				return source{origin: originEnv, name: name}
			}
		}
	default:
		if st.present[d.path()] {
			return source{origin: originFile}
		}
	}

	return source{}
}

// invalidSetting tells why a value is refused for a setting.
//
// Parameters:
//   - e: shared environment, for the printer and the current settings
//   - d: setting to change
//   - v: new value as config.toml would store it
//
// Returns an empty string when the value is accepted, else the translated reason. The value is
// first checked by the rules of the setting that the loader does not have (the editor and the
// repositories root are free text), then the whole set of settings is validated by the loader
// itself, so that the screen can never write what the next start would refuse.
func invalidSetting(e *env, d settingDef, v string) string {
	if !utf8.ValidString(v) || strings.ContainsFunc(v, unsafeRune) {
		return e.t("ui_invalid_control")
	}

	switch d.id {
	case "editor":
		if v != "" && len(strings.Fields(v)) == 0 {
			return e.t("ui_invalid_empty")
		}
	case "repos_root":
		if v != "" && !shell.AbsOrHome(v) {
			return e.t("cli_path_not_absolute")
		}
	}

	if d.kind == settingChoice && !slices.Contains(d.choices, v) {
		return e.t("ui_invalid_choice")
	}

	if err := d.with(e.Settings, v).Validate(); err != nil {
		return sanitize(err.Error())
	}

	return ""
}

// storeValue turns a value into what PatchTable writes.
//
// Parameters:
//   - d: setting to write
//   - v: value as config.Settings stores it
//
// Returns the value to set, and false when the key must be removed from the file instead: an
// empty text, which stands for the default.
func storeValue(d settingDef, v string) (any, bool) {
	switch {
	case d.kind == settingBool:
		return v == "true", true
	case v == "":
		return nil, false
	}

	return v, true
}

// settingWroteMsg reports the end of a write of config.toml.
//
// ID names the setting, Value is the value written (the default after a reset), Reset is true
// when the key was removed, and Err is nil on success, else a *userError.
type settingWroteMsg struct {
	ID    string
	Value string
	Reset bool
	Err   error
}

// writeSetting edits one key of config.toml, keeping the comments and everything else in it.
//
// Parameters:
//   - tr: printer for the message of an error
//   - paths: directories of tote
//   - d: setting to write
//   - v: new value; with reset the key is removed whatever v is
//   - reset: true to remove the key, which gives the default back
//
// Returns nil on success, else a *userError whose message is ready to show.
func writeSetting(tr *i18n.Printer, paths config.Paths, d settingDef, v string, reset bool) error {
	set := map[string]any{}

	var unset []string

	if val, ok := storeValue(d, v); ok && !reset {
		set[d.key] = val
	} else {
		unset = []string{d.key}
	}

	err := store.PatchTable(paths.ConfigFile(), d.table, set, unset)
	if err == nil {
		return nil
	}

	var perr *store.ParseError

	switch {
	case errors.Is(err, store.ErrBusy):
		return &userError{msg: tr.T("ui_busy"), err: err}
	case errors.Is(err, store.ErrLayout):
		return &userError{msg: tr.T("ui_set_layout"), err: err}
	case errors.As(err, &perr):
		file := sanitize(filepath.Base(perr.File))

		return &userError{msg: tr.T("ui_readonly_syntax", file, perr.Line, perr.Column,
			sanitize(perr.Msg)), err: err}
	}

	return &userError{msg: sanitize(err.Error()), err: err}
}

// writeSettingCmd returns the command that writes a setting off the event loop, since the write
// may wait for the lock of the file.
//
// Parameters:
//   - e: shared environment
//   - d: setting to write
//   - v: new value
//   - reset: true to remove the key
//
// Returns a command that yields a settingWroteMsg.
func writeSettingCmd(e *env, d settingDef, v string, reset bool) tea.Cmd {
	tr, paths := e.Tr, e.Paths

	e.writing(1)

	return func() tea.Msg {
		defer e.writing(-1)

		err := writeSetting(tr, paths, d, v, reset)

		return settingWroteMsg{ID: d.id, Value: v, Reset: reset, Err: err}
	}
}

// defaultValue returns the value a setting has when config.toml does not set it.
//
// Parameters:
//   - d: setting to look up
func defaultValue(d settingDef) string {
	return d.value(config.DefaultSettings())
}

// updateTimeout is the longest an update check started from the interface may take.
const updateTimeout = 20 * time.Second

// checkContext returns the context of an update check.
//
// Parameters:
//   - parent: context of the interface, which ends when the interface closes
func checkContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, updateTimeout)
}
