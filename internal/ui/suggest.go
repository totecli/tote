package ui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/plugin"
)

// suggestLimit is the most listings one field of one form starts. The list of an argv that was
// listed before comes from the memory of the form and does not count.
const suggestLimit = 5

// suggestPhase says what the suggestions of a form are doing, or what they last did.
type suggestPhase int

const (
	// sugIdle means there is nothing to say.
	sugIdle suggestPhase = iota

	// sugPreparing means the registry builds the argv, which may ask git for the repository.
	sugPreparing

	// sugRunning means the program of the suggestions runs.
	sugRunning

	// sugReady means a list was loaded and the picker was shown.
	sugReady

	// sugEmpty means the program ran and listed nothing.
	sugEmpty

	// sugWaiting means an earlier field that the argv names is not valid yet, or a value it needs
	// is empty: nothing was started.
	sugWaiting

	// sugFailed means the run did not give a list; the reason is in the text.
	sugFailed

	// sugLimit means the field already started suggestLimit listings.
	sugLimit

	// sugStopped means Esc stopped the run.
	sugStopped

	// sugNoMatch means a list was loaded and no value of it holds what is typed in the field, so
	// that the picker shows the whole list.
	sugNoMatch
)

// suggestState is what a form that asks the arguments of a run knows about the suggestions of its
// fields.
//
// Memo holds the lists that were loaded, by the exact argv that gave them, and it is never written
// anywhere; Starts counts the listings started by field. Argv is the argv of the run that goes,
// cleaned and redacted for the status line, and Text the translated reason of a wait or a failure.
// Spin and Secs are moved on by the tick of the run.
type suggestState struct {
	start  pluginStart
	fields []plugin.Field
	memo   map[string]plugin.ChoicesResult
	starts map[string]int
	phase  suggestPhase
	field  string
	argv   string
	text   string
	shown  int
	hidden int
	more   int
	spin   int
	secs   int
}

// newSuggest returns the suggestions of a form, or nil when no field of the contribution has any.
//
// Parameters:
//   - st: the contribution the form asks the arguments of
//   - fields: its fields
func newSuggest(st pluginStart, fields []plugin.Field) *suggestState {
	if !slices.ContainsFunc(fields, func(f plugin.Field) bool {
		return f.Type == "text" && len(f.ChoicesFrom) > 0
	}) {
		return nil
	}

	return &suggestState{
		start: st, fields: fields, memo: map[string]plugin.ChoicesResult{},
		starts: map[string]int{},
	}
}

// busy reports whether a listing is being prepared or runs.
func (s *suggestState) busy() bool {
	return s != nil && (s.phase == sugPreparing || s.phase == sugRunning)
}

// settle forgets what the last listing said, once the user went on with the form.
func (s *suggestState) settle() {
	if s != nil && !s.busy() {
		s.phase, s.text, s.argv = sugIdle, "", ""
	}
}

// begin records that a listing of a field is being prepared.
//
// Parameters:
//   - field: id of the field
func (s *suggestState) begin(field string) {
	s.phase, s.field, s.text, s.argv = sugPreparing, field, "", ""
	s.spin, s.secs = 0, 0
}

// argvKey is the key of the memory of a form: the field and the exact argv of the run, program
// included. The field is part of it because the list was filtered by the pattern and the
// max_len of that field, and another field with the same argv may have other rules.
//
// Parameters:
//   - field: id of the field
//   - p: the prepared run
func argvKey(field string, p plugin.Prepared) string {
	return field + "\x00" + strings.Join(p.Argv, "\x00")
}

// shownArgv writes the argv of a run as the status line shows it: cleaned, with the secrets of the
// known shapes hidden.
//
// Parameters:
//   - p: the prepared run
func shownArgv(p plugin.Prepared) string {
	return sanitize(plugin.NewRedactor(nil).Line(p.Display))
}

// midFit shortens a plain string to a width by cutting its middle, so that both its start, which
// names the program, and its end, which holds the values, stay in view.
//
// Parameters:
//   - s: plain text, which must not contain escape sequences
//   - w: available cells
func (e *env) midFit(s string, w int) string {
	if w <= 0 {
		return ""
	}

	total := e.Metric.width(s)
	if total <= w {
		return s
	}

	mark := e.Glyphs.ellipsis
	if w <= e.Metric.width(mark)+2 {
		return e.Metric.fit(s, w, mark)
	}

	keep := w - e.Metric.width(mark)
	head := (keep + 1) / 2

	return e.Metric.cut(s, head) + mark + e.Metric.slice(s, total-(keep-head), total)
}

// statusRoom gives the cells that the status line has under the box of a form.
//
// Parameters:
//   - e: shared environment
func (e *env) statusRoom() int {
	w := max(4, min(max(1, e.Cols-4), formWidth))

	return max(1, w-e.indent())
}

// minDetailCells is the fewest cells that the line of the program keeps on the status line when the
// reason of a failure needs the room.
const minDetailCells = 20

// choicesFailure words the reason a suggestions run gave no list. The line of the program, when
// there is one, comes first and is cut to fit, so that the reason at the end is not lost.
//
// Parameters:
//   - res: how the run ended
func (e *env) choicesFailure(res plugin.ChoicesResult) string {
	var args []string

	switch res.Reason {
	case plugin.ChoicesReasonExit, plugin.ChoicesReasonSignal, plugin.ChoicesReasonState:
		args = []string{sanitize(res.Arg)}
	}

	reason, ok := e.Tr.Code(i18n.PrefixChoices, res.Reason, args)
	if !ok {
		reason = res.Reason
	}

	reason = e.fold(sanitize(reason))

	if detail := e.fold(sanitize(res.Detail)); detail != "" {
		base := e.Metric.width(e.t("ui_choices_failed_detail", "", reason))
		room := max(minDetailCells, e.statusRoom()-e.Metric.width(e.Glyphs.warn)-1-base)

		return e.t("ui_choices_failed_detail", e.Metric.fit(detail, room, e.Glyphs.ellipsis), reason)
	}

	return e.t("ui_choices_failed", reason)
}

// countsText words the size of a list.
//
// Parameters:
//   - shown: values in the list
//   - hidden: entries the filters dropped
//   - more: valid entries after the limit
func (e *env) countsText(shown, hidden, more int) string {
	parts := []string{e.t("ui_choices_count", shown)}

	if hidden > 0 {
		parts = append(parts, e.t("ui_choices_hidden", hidden))
	}

	if more > 0 {
		parts = append(parts, e.t("ui_choices_more", more))
	}

	return strings.Join(parts, ", ")
}

// statusNow implements statusHolder: the status line says what the suggestions do, and writes the
// argv of a run that goes, so that the user sees what is started before the program is asked
// anything.
//
// Parameters:
//   - e: shared environment
func (f *fieldsForm) statusNow(e *env) (statusInfo, bool) {
	s := f.sug
	if s == nil || s.phase == sugIdle {
		return statusInfo{}, false
	}

	switch s.phase {
	case sugPreparing:
		return statusInfo{Command: e.t("ui_choices_preparing")}, true
	case sugRunning:
		return statusInfo{Command: e.listingLine(s, e.statusRoom())}, true
	case sugReady:
		return statusInfo{Command: e.countsText(s.shown, s.hidden, s.more)}, true
	case sugNoMatch:
		return statusInfo{Command: e.t("ui_choices_nomatch")}, true
	case sugEmpty:
		text := e.t("ui_choices_empty")
		if s.hidden > 0 {
			text += " (" + e.t("ui_choices_hidden", s.hidden) + ")"
		}

		return statusInfo{Command: text}, true
	case sugLimit:
		return statusInfo{
			Command: e.Glyphs.warn + " " + e.t("ui_choices_limit", suggestLimit), Warn: true,
		}, true
	case sugStopped:
		return statusInfo{Command: e.t("ui_choices_stopped")}, true
	}

	if s.phase == sugFailed {
		return statusInfo{Command: e.Glyphs.warn + " " + s.text, Warn: true}, true
	}

	return statusInfo{Command: s.text}, true
}

// listingLine words a listing that runs for the status line: the spinner, then the argv that goes,
// cut in the middle to fit.
//
// Parameters:
//   - s: the suggestions of the form
//   - room: cells that the line has
func (e *env) listingLine(s *suggestState, room int) string {
	lead := e.spinner(s.spin) + " "
	room -= e.Metric.width(lead) + e.Metric.width(e.t("ui_choices_loading", ""))

	return lead + e.t("ui_choices_loading", e.midFit(s.argv, max(8, room)))
}

// waitText words a listing that waits for another field or for a value.
//
// Parameters:
//   - e: shared environment
//   - name: id of the field or name of the placeholder that is not ready
func (f *fieldsForm) waitText(e *env, name string) string {
	for _, fd := range f.fields {
		if fd.ID == name {
			return e.t("ui_choices_waiting_field", e.fold(fd.words(e, fd.Label)))
		}
	}

	if name == "repo.slug" {
		return e.t("ui_choices_waiting_repo")
	}

	return e.t("ui_choices_waiting", e.fold(sanitize(name)))
}

// suggest asks the root to list the suggestions of the field that has the keys. The values of the
// form go with the call; the registry ignores the ones of the field itself and of the fields after
// it.
//
// Parameters:
//   - e: shared environment
func (f *fieldsForm) suggest(e *env) tea.Cmd {
	s := f.sug
	if s == nil || s.busy() {
		return nil
	}

	v := f.values()
	fd := f.fields[f.cur]
	s.begin(fd.ID)

	return msgCmd(suggestAskMsg{Form: f, Field: fd.ID, Call: e.callOf(s.start, s.fields, &v)})
}

// suggestKey handles the keys of a form that has suggestions before the keys of every form: Esc
// stops a listing that goes and keeps the form open, and any key but Ctrl-L that comes after a
// listing ended clears what it said.
//
// Parameters:
//   - k: key pressed
//
// Returns the command and true when the key was handled here.
func (f *fieldsForm) suggestKey(k tea.KeyPressMsg) (tea.Cmd, bool) {
	s := f.sug
	if s == nil {
		return nil, false
	}

	if k.String() == "esc" && s.busy() {
		return msgCmd(suggestStopMsg{Form: f}), true
	}

	if k.String() != "ctrl+l" {
		s.settle()
	}

	return nil, false
}

// suggestBinding is the footer hint of the key that lists the suggestions of a field.
//
// Parameters:
//   - e: shared environment
func suggestBinding(e *env) key.Binding {
	return key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", e.t("ui_help_suggest")))
}

// picked fills the field with the value that the user picked in the list of the suggestions. The
// field stays free text: the value goes through the same check as a typed one.
//
// Parameters:
//   - e: shared environment
//   - m: the pick
func (f *fieldsForm) picked(e *env, m suggestPickedMsg) {
	for i, fd := range f.fields {
		if fd.ID != m.Field || !fd.Suggest || fd.Locked != "" {
			continue
		}

		if i != f.cur {
			f.focusOn(e, i)
		}

		fd.setText(m.Value)
		fd.Touched = true
		f.sug.settle()
		f.changed(e)

		return
	}
}

// fieldWords gives the text typed in a field and its label, which is the id of the field when the
// form has no such field.
//
// Parameters:
//   - e: shared environment
//   - field: id of the field
func (f *fieldsForm) fieldWords(e *env, field string) (typed, label string) {
	label = field

	for _, fd := range f.fields {
		if fd.ID == field {
			typed, label = fd.text(), fd.words(e, fd.Label)
		}
	}

	return typed, label
}

// pickList builds the picker of a list of suggestions: the values, each with its label as a note,
// narrowed to the ones that hold what is typed in the field, ignoring the case.
//
// Parameters:
//   - e: shared environment
//   - field: id of the field
//   - res: the list
//
// Returns the picker, and false when the typed text matches no value, in which case the picker
// shows the whole list.
func (f *fieldsForm) pickList(e *env, field string, res plugin.ChoicesResult) (overlay, bool) {
	typed, label := f.fieldWords(e, field)
	items := make([]pickItem, 0, len(res.Items))
	cur := 0
	low := strings.ToLower(typed)

	for _, c := range res.Items {
		if low != "" && !strings.Contains(strings.ToLower(c.Value), low) {
			continue
		}

		if c.Value == typed {
			cur = len(items)
		}

		items = append(items, pickItem{
			label: e.fold(sanitize(c.Value)), value: c.Value, note: e.fold(sanitize(c.Label)),
		})
	}

	matched := len(items) > 0
	if !matched {
		cur = 0

		for _, c := range res.Items {
			items = append(items, pickItem{
				label: e.fold(sanitize(c.Value)), value: c.Value, note: e.fold(sanitize(c.Label)),
			})
		}
	}

	title := e.t("ui_choices_title", e.fold(label))

	if !matched {
		title += " (" + e.t("ui_choices_unfiltered") + ")"
	}

	// The counts go on a line of their own under the list: the title sits in the border and is cut
	// to the width of the box.
	var counts []string

	if res.Hidden > 0 {
		counts = append(counts, e.t("ui_choices_hidden", res.Hidden))
	}

	if res.More > 0 {
		counts = append(counts, e.t("ui_choices_more", res.More))
	}

	pick := newPick(title, items, cur, func(_ *env, value string) tea.Cmd {
		return msgCmd(suggestPickedMsg{Form: f, Field: field, Value: value})
	})

	if po, ok := pick.(*pickOverlay); ok {
		po.info = strings.Join(counts, ", ")
	}

	return pick, matched
}
