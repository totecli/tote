package ui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
)

// newInput returns the single-line text input used by the forms and the palette.
//
// Parameters:
//   - placeholder: grey text shown while the input is empty; it must be translated
//   - limit: most characters accepted, zero or less for no limit
//
// Returns an input without prompt, with Ctrl-D left to the quit key (the Delete key deletes
// forward) and with Ctrl-V turned off: its handler reads the system
// clipboard through a helper program found on PATH, which a hostile PATH could abuse. Text
// pasted by the terminal arrives as a tea.PasteMsg and works as usual.
func newInput(placeholder string, limit int) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.CharLimit = limit
	in.KeyMap.Paste = key.NewBinding()
	in.KeyMap.DeleteCharacterForward = key.NewBinding(key.WithKeys("delete"))

	return in
}
