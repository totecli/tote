package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// overlay is a layer drawn over the current screen: a form, a confirmation, the palette.
//
// The root keeps a stack of overlays. The one on top receives every key and every message that
// is not handled by the root itself; the screen below receives nothing until the overlay is
// done. Init runs once when the overlay is pushed. Update returns the overlay to keep, a command
// and a done flag; when done is true the root pops it and runs the command. View returns the
// lines of the box, none wider than maxW cells and no more than maxH lines; the root centers
// the box on the screen. Keys returns the bindings that the footer lists while the overlay is
// open.
//
// An overlay writes records through env.Data and reports it with wroteCmd, so the root reloads
// the collections and refreshes the screen. It can show a message with toastCmd and open
// another overlay with an openOverlayMsg.
type overlay interface {
	Init(e *env) tea.Cmd
	Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool)
	View(e *env, maxW, maxH int) []string
	Keys(e *env) []key.Binding
}
