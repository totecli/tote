package ui

import (
	"charm.land/bubbles/v2/key"

	"github.com/totecli/tote/internal/i18n"
)

// keymap holds every key binding of the interface.
//
// The bindings with a help text (Nav, Open, Back, Quit, Filter, Palette, New, Edit, Delete,
// Editor, Tabs, Apply, Clear, Reload, Close, Help, Settings, Jump, Notice, Check, User, Pick,
// Command, Copy, View, Change, Adjust, Default, Scroll, Dismiss, Interrupt and Exit, then Toggle,
// Allow, Setup, Install, Verify and Menu of the plugins screen) are the ones the footer and the
// help screen list; the others only match keys. Screens and overlays
// return the bindings that apply to their current state and the footer is generated from them,
// so a key is never documented by hand. Adjust is the help of the keys that Left and Right
// match on the settings screen.
//
// Quit is the bare q of the welcome page and of the top-level lists. Interrupt (Ctrl-C) cancels
// or goes up one level and Exit (Ctrl-D) quits, both from anywhere; neither is a letter, so
// neither can be typed by accident. The letter R is never bound, so that a habit of pressing it
// to go back stays harmless.
type keymap struct {
	Nav       key.Binding
	Open      key.Binding
	Back      key.Binding
	Quit      key.Binding
	Filter    key.Binding
	Palette   key.Binding
	New       key.Binding
	Edit      key.Binding
	Delete    key.Binding
	Editor    key.Binding
	Tabs      key.Binding
	Apply     key.Binding
	Clear     key.Binding
	Reload    key.Binding
	Close     key.Binding
	Help      key.Binding
	Settings  key.Binding
	Jump      key.Binding
	Notice    key.Binding
	Check     key.Binding
	User      key.Binding
	Pick      key.Binding
	Command   key.Binding
	Copy      key.Binding
	View      key.Binding
	Change    key.Binding
	Adjust    key.Binding
	Default   key.Binding
	Scroll    key.Binding
	Dismiss   key.Binding
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Home      key.Binding
	End       key.Binding
	Left      key.Binding
	Right     key.Binding
	Suspend   key.Binding
	Interrupt key.Binding
	Exit      key.Binding
	Toggle    key.Binding
	Allow     key.Binding
	Setup     key.Binding
	Install   key.Binding
	Verify    key.Binding
	Menu      key.Binding
	Shift     key.Binding
}

// newKeymap builds the bindings with their help texts in the language of the interface.
//
// Parameters:
//   - tr: printer for the descriptions shown in the footer
//   - g: glyph set, which gives the labels of the arrow keys
func newKeymap(tr *i18n.Printer, g glyphs) keymap {
	bind := func(label, desc string, keys ...string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, tr.T(desc)))
	}
	plain := func(keys ...string) key.Binding { return key.NewBinding(key.WithKeys(keys...)) }

	return keymap{
		Nav:      bind(g.upDown, "ui_help_navigate", "up", "down", "k", "j"),
		Open:     bind(g.enter, "ui_help_open", "enter"),
		Back:     bind("esc", "ui_help_back", "esc"),
		Quit:     bind("q", "ui_help_quit", "q"),
		Filter:   bind("/", "ui_help_filter", "/"),
		Palette:  bind("ctrl+k", "ui_help_palette", "ctrl+k"),
		New:      bind("n", "ui_help_new", "n"),
		Edit:     bind("e", "ui_help_edit", "e"),
		Delete:   bind("d", "ui_help_delete", "d"),
		Editor:   bind("o", "ui_help_editor", "o"),
		Tabs:     bind("h/l", "ui_help_tabs", "left", "right", "h", "l"),
		Apply:    bind(g.enter, "ui_help_apply", "enter"),
		Clear:    bind("esc", "ui_help_clear", "esc"),
		Reload:   bind("ctrl+r", "ui_help_reload", "ctrl+r"),
		Close:    bind("esc", "ui_help_close", "esc"),
		Help:     bind("?", "ui_help_help", "?", "f1"),
		Settings: bind(",", "ui_help_settings", ","),
		Jump:     bind("1-4", "ui_help_jump", "1", "2", "3", "4", "5"),
		Notice:   bind("u", "ui_help_notice", "u"),
		Check:    bind("U", "ui_help_check", "U"),
		User:     bind("u", "ui_help_user", "u"),
		Pick:     bind("U", "ui_help_pick", "U"),
		Command:  bind("c", "ui_help_command", "c"),
		Copy:     bind("C", "ui_help_copy", "C"),
		View:     bind("v", "ui_help_view", "v"),
		Change:   bind(g.enter, "ui_help_change", "enter"),
		Adjust:   bind(g.leftRight, "ui_help_adjust", "left", "right", "h", "l"),
		Default:  bind("d", "ui_help_default", "d"),
		Scroll: bind(g.upDown, "ui_help_scroll", "up", "down", "k", "j", "pgup",
			"pgdown", "ctrl+b", "ctrl+f", "home", "end", "g", "G"),
		Dismiss:   bind("esc", "ui_help_close", "esc", "?", "f1"),
		Up:        plain("up", "k"),
		Down:      plain("down", "j"),
		PageUp:    plain("pgup", "ctrl+b"),
		PageDown:  plain("pgdown", "ctrl+f"),
		Home:      plain("home", "g"),
		End:       plain("end", "G"),
		Left:      plain("left", "h"),
		Right:     plain("right", "l"),
		Suspend:   plain("ctrl+z"),
		Interrupt: bind("ctrl+c", "ui_help_interrupt", "ctrl+c"),
		Exit:      bind("ctrl+d", "ui_help_exit", "ctrl+d"),
		Toggle:    bind("space", "ui_help_toggle", "space", " "),
		Allow:     bind("a", "ui_help_allow", "a"),
		Setup:     bind("s", "ui_help_setup", "s"),
		Install:   bind("i", "ui_help_install", "i"),
		Verify:    bind("c", "ui_help_verify", "c"),
		Menu:      bind("tab", "ui_help_menu", "tab", "."),
		Shift:     bind("K/J", "ui_help_reorder", "K", "J", "shift+up", "shift+down"),
	}
}
