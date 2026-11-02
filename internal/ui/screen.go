package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// screen is one full page of the interface: the welcome menu or the list of a kind.
//
// The root model keeps exactly one screen and replaces it when it receives openKindMsg or
// goHomeMsg. Update handles a key or a message and returns the screen to keep and a command.
// View returns exactly h lines, each at most e.Width cells; the layout is recomputed from
// env on every call, so a resize needs no special handling. Keys returns the bindings that
// apply to the current state, from which the footer is generated, and Status the content of
// the status line. Reload is called after the collections were read again and gives the key to
// select afterwards, empty to keep the selection. Typing reports that the screen is reading
// text, so that the root does not take "q" as a command.
type screen interface {
	Update(e *env, msg tea.Msg) (screen, tea.Cmd)
	View(e *env, h int) []string
	Keys(e *env) []key.Binding
	Status(e *env) statusInfo
	Reload(e *env, selectKey string) screen
	Typing() bool
}

// navigator is implemented by a screen that asks the root to navigate during a key press. The
// root takes the message right after Update, so that the next key already meets the new screen or
// overlay.
type navigator interface {
	takeNav() tea.Msg
}

// plural gives the catalog suffix for a count in a language.
//
// Parameters:
//   - lang: "fr" or "en"
//   - n: the count
//
// Returns "one" for 1 in English and for 0 and 1 in French, else "other".
func plural(lang string, n int) string {
	if n == 1 || (lang == "fr" && n == 0) {
		return "one"
	}

	return "other"
}

// countText renders the number of records of a kind with its name, such as "9 connections".
//
// Parameters:
//   - e: shared environment
//   - k: kind counted
//   - n: number of records
func countText(e *env, k kind, n int) string {
	return e.t("ui_count_"+kindSlug[k]+"_"+plural(e.Tr.Lang(), n), n)
}

// kindSlug is the middle part of the catalog keys that exist per kind.
var kindSlug = [...]string{"connections", "repos", "aliases", "funcs"}
