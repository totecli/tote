package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/i18n"
)

// typist is implemented by an overlay that reads text, such as a form or the palette: the keys
// that open the help or the settings are text there.
type typist interface {
	Typing() bool
}

// helpAllowed reports whether a key may open the help. F1 may open it anywhere but over the help
// itself; "?" only where it is not a character of some text.
//
// Parameters:
//   - k: key pressed
func (r *root) helpAllowed(k tea.KeyPressMsg) bool {
	n := len(r.overlays)
	if n == 0 {
		return k.String() == "f1" || !r.scr.Typing()
	}

	top := r.overlays[n-1]
	if _, isHelp := top.(*helpOverlay); isHelp {
		return false
	}

	if k.String() == "f1" {
		return true
	}

	t, ok := top.(typist)

	return !ok || !t.Typing()
}

// topic names the screen or the overlay the help is opened from, which decides the section the
// help shows first.
func (r *root) topic() string {
	if n := len(r.overlays); n > 0 {
		switch r.overlays[n-1].(type) {
		case *form, *fieldsForm:
			return "form"
		case *consentOverlay:
			return "consent"
		case *listEditor:
			return "listedit"
		case *paletteOverlay:
			return "palette"
		case *confirm, *ask:
			return "confirm"
		case *infoOverlay:
			return "info"
		case *pickOverlay:
			return "pick"
		case *promptOverlay:
			return "prompt"
		}
	}

	switch s := r.scr.(type) {
	case *homeScreen:
		return "home"
	case *configScreen:
		return "config"
	case *pluginsScreen:
		if s.flt.typing {
			return "filter"
		}

		return "plugins"
	case *listScreen:
		if s.flt.typing {
			return "filter"
		}

		return kindSlug[s.kind]
	}

	return "global"
}

// openConfig shows the settings screen, unless it is already there.
func (r *root) openConfig() tea.Cmd {
	if _, here := r.scr.(*configScreen); !here {
		r.scr = newConfig(r.e, "")
		r.moved()
	}

	return nil
}

// settingWrote handles the end of a write of config.toml: it shows the error, or applies the new
// value to the running interface and reads the screen again.
//
// Parameters:
//   - m: the setting and its new value
func (r *root) settingWrote(m settingWroteMsg) tea.Cmd {
	e := r.e

	if m.Err != nil {
		// A failed write ends the steps that were waiting behind it and asks for nothing more.
		e.stepEnded(m)

		return r.say(m.Err.Error(), true)
	}

	d, _ := findSetting(m.ID)

	value := m.Value
	if m.Reset {
		value = defaultValue(d)
	}

	next := d.with(e.Settings, value)

	if s, err := config.LoadSettings(e.Paths.ConfigFile()); err == nil {
		next = s
	}

	// The selection stays where the user put it: a key pressed while the write ran has moved it.
	cmd := r.applySettings(next)
	r.scr = r.scr.Reload(e, "")

	key := "ui_set_saved"
	if m.Reset {
		key = "ui_set_reset"
	}

	say := r.say(settingToast(e, key, d), false)
	r.toast.reword = func() string { return settingToast(e, key, d) }

	return tea.Batch(cmd, say, e.stepEnded(m))
}

// toastMinName is the fewest cells the name of a setting may be cut to in a toast: a shorter
// name says nothing, so the status line cuts the end of the message instead.
const toastMinName = 6

// settingToast words the message that follows a change of a setting, cutting the name of the
// setting, not the state that comes after it, when the line is too short for both. A name that
// would shrink below toastMinName cells is kept whole and the status line cuts the message at its
// end instead, since "S..." says nothing.
//
// Parameters:
//   - e: shared environment
//   - key: catalog key of the message, "ui_set_saved" or "ui_set_reset"; both take the name and
//     the state
//   - d: setting that changed
func settingToast(e *env, key string, d settingDef) string {
	state := d.applyText(e)
	room := e.Width - e.indent() - e.Metric.width(e.t(key, "", state))
	name := e.t(d.nameKey())

	if room >= toastMinName {
		name = e.Metric.fit(name, room, e.Glyphs.ellipsis)
	}

	return e.t(key, name, state)
}

// applySettings makes the running interface follow new settings: the language, the color scheme
// and the layout change at once, and the other settings are read where they are used.
//
// Parameters:
//   - s: the settings that are now in force
//
// Returns the command that asks the terminal for its background when the scheme became
// automatic, or became a dark or a light one that the terminal may contradict, see themeClash.
func (r *root) applySettings(s config.Settings) tea.Cmd {
	e := r.e
	old := e.Settings
	e.Settings = s

	if lang := config.EffectiveLang(s, envLookup(e.Environ)); lang != e.Tr.Lang() {
		e.Tr = printerFor(i18n.New(lang), e.Glyphs.ascii)

		e.Keys = newKeymap(e.Tr, e.Glyphs)
		e.Data.tr = e.Tr
		e.pluginWords(lang)
	}

	r.resize(e.Cols, e.Height)

	if old.UI.Theme == s.UI.Theme {
		return nil
	}

	r.pickMode()
	r.rebuildTheme()

	if r.auto {
		r.waiting = true

		return tea.Batch(tea.RequestBackgroundColor, tick(backgroundWait,
			func(time.Time) tea.Msg { return backgroundTimeoutMsg{} }))
	}

	if r.forced() {
		return tea.RequestBackgroundColor
	}

	return nil
}

// settingsFileText describes in one sanitized line why config.toml cannot be used.
//
// Parameters:
//   - e: shared environment
//   - err: error of the loader of the settings
func settingsFileText(e *env, err error) string {
	var perr *config.ParseError
	if errors.As(err, &perr) && errors.Unwrap(perr) == nil {
		return e.t("ui_readonly_syntax", sanitize(filepath.Base(perr.File)), perr.Line,
			perr.Column, sanitize(perr.Msg))
	}

	return e.t("ui_readonly_config", settingsReason(e, err))
}

// settingsReason writes why config.toml is refused on one line, with the name of the file
// instead of its path, which would push the reason out of the screen.
//
// Parameters:
//   - e: shared environment, for the path of the file
//   - err: error of the loader of the settings; a joined error gives one reason per line
func settingsReason(e *env, err error) string {
	path := e.Paths.ConfigFile()
	text := strings.ReplaceAll(err.Error(), path, filepath.Base(path))
	text = strings.ReplaceAll(text, "\n"+filepath.Base(path)+": ", "; ")
	text = strings.TrimPrefix(text, filepath.Base(path)+": ")

	return sanitize(strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " "))
}
