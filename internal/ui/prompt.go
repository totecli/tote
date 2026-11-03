package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// promptWidth is the widest the box of a prompt gets, in cells.
const promptWidth = 64

// promptLimit is the most characters a prompt accepts.
const promptLimit = 200

// promptOverlay is the overlay that asks for one line of text.
//
// Field holds the text and its cursor. Check returns the translated reason the text is refused,
// empty when it is accepted; Enter does nothing while there is a reason, which is shown under the
// field. Done turns the accepted text into the command to run. Initial is the text the prompt
// opened with, so that Dirty can tell whether it was changed.
type promptOverlay struct {
	title   string
	initial string
	label   string
	field   *formField
	err     string
	check   func(e *env, text string) string
	done    func(e *env, text string) tea.Cmd
}

// newPrompt returns the overlay that reads one line of text.
//
// Parameters:
//   - title: translated title of the box
//   - label: translated line above the field, empty for none
//   - initial: text in the field at the start, put there with the cursor at its end
//   - check: gives the reason a text is refused, "" to accept it
//   - done: called with the accepted text, returns the command to run
func newPrompt(title, label, initial string, check func(*env, string) string,
	done func(*env, string) tea.Cmd,
) overlay {
	fd := newTextField("text", "", "", promptLimit)
	fd.setText(initial)
	fd.focus()

	return &promptOverlay{
		title: title, label: label, initial: fd.text(), field: fd, check: check,
		done: done,
	}
}

// Init implements overlay: it checks the text as it is, which shows the reason an initial value
// is refused.
//
// Parameters:
//   - e: shared environment
func (p *promptOverlay) Init(e *env) tea.Cmd {
	p.err = p.check(e, p.field.text())

	return nil
}

// Dirty implements dirtier: it reports that the text is not the one the prompt opened with.
func (p *promptOverlay) Dirty() bool {
	return p.field.text() != p.initial
}

// Typing reports that the prompt reads text, so that "?" and the other keys of the screens
// belong to the field.
func (p *promptOverlay) Typing() bool {
	return true
}

// Update implements overlay.
//
// Parameters:
//   - e: shared environment
//   - msg: key press, paste or dismissal; other messages are ignored
func (p *promptOverlay) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return p, nil, true
	case tea.PasteMsg:
		p.typed(e, m)
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc":
			return p, nil, true
		case "enter":
			if p.err == "" {
				return p, p.done(e, p.field.text()), true
			}
		default:
			p.typed(e, m)
		}
	}

	return p, nil, false
}

// typed gives a message to the field and checks the text again when it changed.
//
// Parameters:
//   - e: shared environment
//   - msg: key press or paste
func (p *promptOverlay) typed(e *env, msg tea.Msg) {
	if p.field.edit(msg) {
		p.err = p.check(e, p.field.text())
	}
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed, unused: the box has a fixed height
func (p *promptOverlay) View(e *env, maxW, _ int) []string {
	w := max(6, min(maxW, promptWidth))
	inner := w - 4
	pad := func(s string) string { return " " + e.clipPad(s, inner) + " " }

	var lines []string

	if p.label != "" {
		lines = append(lines, pad(e.Theme.Muted.Render(e.Metric.fit(p.label, inner,
			e.Glyphs.ellipsis))))
	}

	lines = append(lines, pad(p.field.view(e, inner, true)))

	if p.err != "" {
		lines = append(lines, pad(e.Theme.Err.Render(e.Metric.fit(e.Glyphs.warn+" "+p.err, inner,
			e.Glyphs.ellipsis))))
	} else {
		lines = append(lines, pad(""))
	}

	return floating(e, e.Theme.Bold.Render(p.title), lines, w)
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (p *promptOverlay) Keys(e *env) []key.Binding {
	return promptBindings(e)
}

// promptBindings lists the keys of a prompt.
//
// Parameters:
//   - e: shared environment
func promptBindings(e *env) []key.Binding {
	save := e.Keys.Apply
	save.SetHelp(e.Glyphs.enter, e.t("ui_help_save"))

	cancel := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", e.t("ui_help_cancel")))

	return []key.Binding{save, cancel}
}
