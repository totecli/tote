package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// suggestTickMsg moves the spinner of the suggestions run of the generation Gen on.
type suggestTickMsg struct {
	Gen int
}

// secsMax is the most seconds that the loading row counts, and sets the room that it keeps at the
// right of the row, so that it does not move when the count goes from one digit to two.
const secsMax = 99

// waitInfoH is the fewest lines of room for which the loading box keeps the line of the argv: the
// two borders, the row of the spinner and the argv. The shortest terminal (7 lines) leaves 5 to
// the box, so the argv always shows.
const waitInfoH = 4

// suggestWait is the box that stands for the list of suggestions while its program runs: the title
// of the list, one muted row with the spinner and a word, the argv under it and, once a second has
// passed, the seconds the run has taken. The root takes it away when the run ends, whatever the
// outcome, so that no empty box stays open.
type suggestWait struct {
	form  *fieldsForm
	title string
}

// newSuggestWait returns the loading box of the suggestions of a field.
//
// Parameters:
//   - e: shared environment
//   - f: the form that asked
//   - field: id of the field
func newSuggestWait(e *env, f *fieldsForm, field string) *suggestWait {
	_, label := f.fieldWords(e, field)

	return &suggestWait{form: f, title: e.t("ui_choices_title", e.fold(label))}
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (w *suggestWait) Init(_ *env) tea.Cmd {
	return nil
}

// Update implements overlay: Esc and a dismissal stop the run, which closes the box, and every
// other key is dropped, since what is typed in the field would not be seen and the second Ctrl-L
// must not start a second program.
//
// Parameters:
//   - e: shared environment
//   - msg: key press or dismissal; other messages are ignored
func (w *suggestWait) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return w, msgCmd(suggestStopMsg{Form: w.form}), true
	case tea.KeyPressMsg:
		if key.Matches(m, e.Keys.Close) {
			return w, msgCmd(suggestStopMsg{Form: w.form}), true
		}
	}

	return w, nil, false
}

// width returns the width of the box, which grows like a list of choices with the title and the
// argv.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
func (w *suggestWait) width(e *env, maxW int) int {
	p := &pickOverlay{title: w.title, info: w.form.sug.argv}
	secs := e.Metric.width(e.t("ui_choices_wait_secs", secsMax))
	need := p.wanted(e, e.Metric.width(w.word(e)), secs)

	return boxWidth(pickWidth, need, pickMaxWidth, e.Cols, maxW)
}

// word words the row: the frame of the spinner and what the box waits for.
//
// Parameters:
//   - e: shared environment
func (w *suggestWait) word(e *env) string {
	return e.spinner(w.form.sug.spin) + " " + e.t("ui_choices_wait")
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed, the box is as tall as one row and the line of the argv
func (w *suggestWait) View(e *env, maxW, maxH int) []string {
	s := w.form.sug
	bw := w.width(e, maxW)
	inner := bw - borderCells

	secsW := e.Metric.width(e.t("ui_choices_wait_secs", secsMax))
	secs := ""

	if s.secs > 0 {
		secs = e.t("ui_choices_wait_secs", min(s.secs, secsMax))
	}

	box := *e
	box.Layout = layoutCompact

	cells := []cell{{text: w.word(e), w: max(1, inner-rowMargins), muted: true}}

	if noteW := min(secsW, inner-rowMargins-columnGap-minNote); noteW >= minNote {
		cells[0].w = max(1, inner-rowMargins-noteW-columnGap)
		cells = append(cells, cell{text: secs, w: noteW, muted: true, right: true})
	}

	lines := []string{box.renderBoxRow(false, false, cells, inner)}

	if maxH >= waitInfoH {
		info := e.midFit(s.argv, inner-infoIndent)
		lines = append(lines, " "+e.Theme.Muted.Render(info))
	}

	return floating(e, e.Theme.Bold.Render(w.title), lines, bw)
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (w *suggestWait) Keys(e *env) []key.Binding {
	stop := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", e.t("ui_help_cancel")))

	return []key.Binding{stop}
}
