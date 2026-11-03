package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// askWidth is the widest the box of a question gets, in cells.
const askWidth = 52

// dirtier is implemented by an overlay that holds something the user typed or changed, so that
// closing it would lose work. The root asks before it closes such an overlay with Esc or
// Ctrl-C, and before the interface quits.
type dirtier interface {
	Dirty() bool
}

// ask is the overlay that asks a yes or no question whose answer has no record behind it: whether
// to discard what was typed in an overlay, whether to quit while something is not finished and
// whether a setting goes back to its default.
//
// Title and Text are the translated heading and sentence. Yes is the message sent when the answer
// is yes, and Focus tells which button has the focus. The focus starts on "No", so that Enter
// alone never loses anything: Enter on "No", "n" and Esc all keep things as they are. Details are
// muted lines under the sentence, such as the list of what a removal deletes.
type ask struct {
	title   string
	text    string
	details []string
	yes     tea.Msg
	focus   bool
}

// discardMsg tells the root to close the overlay on top of the stack without keeping what was
// typed in it. The question that precedes it has already closed.
type discardMsg struct{}

// quitMsg tells the root to end the interface after the question about quitting was answered
// with yes.
type quitMsg struct{}

// newAskDiscard returns the question that comes before an overlay with changes is closed.
//
// Parameters:
//   - e: shared environment, for the texts
func newAskDiscard(e *env) overlay {
	return &ask{title: e.t("ui_discard_title"), text: e.t("ui_discard_text"), yes: discardMsg{}}
}

// newAskQuit returns the question that comes before the interface quits with work in progress.
//
// Parameters:
//   - e: shared environment, for the texts
//   - textKey: catalog key of the sentence that says what would be lost
func newAskQuit(e *env, textKey string) overlay {
	return &ask{title: e.t("ui_quit_title"), text: e.t(textKey), yes: quitMsg{}}
}

// resetSettingMsg tells the root to remove a setting from config.toml, which gives its default
// back, after the question was answered with yes.
type resetSettingMsg struct {
	ID string
}

// newAskReset returns the question that comes before a setting goes back to its default, which
// loses the value the user set.
//
// Parameters:
//   - e: shared environment, for the texts
//   - d: setting that would be reset
func newAskReset(e *env, d settingDef) overlay {
	return &ask{
		title: e.t("ui_reset_title"),
		text:  e.t("ui_reset_text", e.t(d.nameKey())),
		yes:   resetSettingMsg{ID: d.id},
	}
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (a *ask) Init(_ *env) tea.Cmd {
	return nil
}

// Update implements overlay: y answers yes, n and Esc answer no, the arrow keys and Tab change
// the button and Enter picks the one that has the focus.
//
// Parameters:
//   - e: shared environment, unused
//   - msg: key press or click; other messages are ignored
func (a *ask) Update(_ *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return a, nil, true
	case itemClickMsg:
		switch m.ID {
		case confirmNo:
			return a, nil, true
		case confirmYes:
			return a, msgCmd(a.yes), true
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "y", "Y":
			return a, msgCmd(a.yes), true
		case "n", "N", "esc":
			return a, nil, true
		case "enter":
			if a.focus {
				return a, msgCmd(a.yes), true
			}

			return a, nil, true
		case "left", "right", "h", "l", "tab", "shift+tab":
			a.focus = !a.focus
		}
	}

	return a, nil, false
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (a *ask) View(e *env, maxW, maxH int) []string {
	box, _ := a.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: a click on a button answers.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (a *ask) Zones(e *env, maxW, maxH int) []zone {
	_, zs := a.draw(e, maxW, maxH)

	return zs
}

// draw lays out the box of the question.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed; a box that is short drops the blank line, see questionBox
//
// Returns the lines of the box and the zones of the two buttons, counted from its top left
// corner.
func (a *ask) draw(e *env, maxW, maxH int) ([]string, []zone) {
	w := min(maxW, askWidth)

	return questionBox(e, w, maxH, a.title, a.text, "", a.details, a.focus)
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (a *ask) Keys(e *env) []key.Binding {
	return confirmBindings(e)
}
