package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// fieldSet is what every form shares: the fields, the one that has the keys, the keys that move
// between them and the box they are drawn in.
//
// Fields are the lines in order and Cur the one that has the keys. Msg is the reason the last save
// failed, Problem the reason the form as typed would be refused as a whole (a name that is taken)
// and Submitted becomes true once the user asked to save, which shows the errors of the fields that
// were not visited. Base holds the text of every field as the form opened, so that Dirty can tell
// whether anything was typed or changed. Recheck checks every field again after a change and
// Entered runs when a field gets the keys, before the check; both may be nil. Wide gives the labels
// up to half of the box instead of formLabelMax cells, for a form whose labels explain the field.
type fieldSet struct {
	fields    []*formField
	cur       int
	msg       string
	problem   string
	submitted bool
	base      []string
	recheck   func(e *env)
	entered   func(e *env)
	wide      bool
}

// formKey is what a key press asks of the form that owns the fields.
type formKey int

const (
	// fkNone means the fieldSet handled the key, or ignored it.
	fkNone formKey = iota

	// fkSubmit asks to save: Ctrl-S, or Enter on the last field.
	fkSubmit

	// fkCancel asks to close the form without saving.
	fkCancel

	// fkOpen asks to edit the list of the field that has the keys.
	fkOpen
)

// rescan checks the fields again with the check of the owner.
//
// Parameters:
//   - e: shared environment
func (f *fieldSet) rescan(e *env) {
	if f.recheck != nil {
		f.recheck(e)
	}
}

// enter tells the owner that a field got the keys.
//
// Parameters:
//   - e: shared environment
func (f *fieldSet) enter(e *env) {
	if f.entered != nil {
		f.entered(e)
	}
}

// navigate handles the keys every form has: Esc cancels, Ctrl-S saves, Tab and the arrows move
// between the fields, and Enter goes to the next field, saves on the last one, or opens the editor
// of a list. Any other key goes to the field that has the keys.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (f *fieldSet) navigate(e *env, k tea.KeyPressMsg) formKey {
	switch k.String() {
	case "esc":
		return fkCancel
	case "ctrl+s":
		return fkSubmit
	case "tab", "down":
		f.move(e, 1)
	case "shift+tab", "up":
		f.move(e, -1)
	case "enter":
		switch {
		case f.fields[f.cur].Type == fieldList:
			return fkOpen
		case f.cur == len(f.fields)-1:
			return fkSubmit
		}

		f.move(e, 1)
	default:
		f.fieldKey(e, k)
	}

	return fkNone
}

// texts returns the text of every field, in order.
func (f *fieldSet) texts() []string {
	out := make([]string, len(f.fields))

	for i, fd := range f.fields {
		out[i] = fd.text()
	}

	return out
}

// Dirty implements dirtier: it reports that a field differs from what the form opened with.
func (f *fieldSet) Dirty() bool {
	return !slices.Equal(f.texts(), f.base)
}

// typed gives a message to the field that has the keys and checks the form when the text changed.
//
// Parameters:
//   - e: shared environment
//   - msg: key press or paste
func (f *fieldSet) typed(e *env, msg tea.Msg) {
	if fd := f.fields[f.cur]; fd.edit(msg) {
		fd.Touched = true
		f.changed(e)
	}
}

// changed forgets the last save failure and checks the form again.
//
// Parameters:
//   - e: shared environment
func (f *fieldSet) changed(e *env) {
	f.msg = ""
	f.rescan(e)
}

// fieldKey handles a key meant for the field that has the keys.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (f *fieldSet) fieldKey(e *env, k tea.KeyPressMsg) {
	fd := f.fields[f.cur]

	if fd.Locked != "" {
		return
	}

	switch fd.Type {
	case fieldChoice:
		step := 0

		switch k.String() {
		case "left", "h":
			step = len(fd.Values) - 1
		case "right", "l", "space", " ":
			step = 1
		}

		if step != 0 {
			fd.Pick = (fd.Pick + step) % len(fd.Values)
			fd.Touched = true
			f.changed(e)
		}
	case fieldToggle:
		switch k.String() {
		case "left", "right", "h", "l", "space", " ", "x":
			fd.On = !fd.On
			fd.Touched = true
			f.changed(e)
		}
	default:
		f.typed(e, k)
	}
}

// move gives the keys to another field, wrapping around the ends.
//
// Parameters:
//   - e: shared environment
//   - step: 1 for the next field, -1 for the previous one
func (f *fieldSet) move(e *env, step int) {
	f.jump(e, (f.cur+step+len(f.fields))%len(f.fields))
}

// jump gives the keys to a field, as Tab does for the next one.
//
// Parameters:
//   - e: shared environment
//   - i: index of the field
func (f *fieldSet) jump(e *env, i int) {
	f.fields[f.cur].blur()
	f.cur = i
	f.fields[f.cur].focus()
	f.enter(e)
	f.rescan(e)
}

// focusID gives the keys to the field with an identifier, if the form has it.
//
// Parameters:
//   - e: shared environment
//   - id: identifier of the field, such as "name"
func (f *fieldSet) focusID(e *env, id string) {
	for i, fd := range f.fields {
		if fd.ID == id {
			f.focusOn(e, i)

			return
		}
	}
}

// focusOn gives the keys to a field without moving the cursor of the others.
//
// Parameters:
//   - e: shared environment
//   - i: index of the field
func (f *fieldSet) focusOn(e *env, i int) {
	if i == f.cur {
		return
	}

	f.fields[f.cur].blur()
	f.cur = i
	f.fields[i].focus()
	f.rescan(e)
}

// notes returns the lines under a field: the reason it is refused, or the help text while it has
// the keys.
//
// Parameters:
//   - e: shared environment
//   - fd: field to describe
//   - focused: true when the field has the keys
//   - indent: spaces in front of the lines
//   - w: cells available for the text
func (f *fieldSet) notes(e *env, fd *formField, focused bool, indent string, w int) []string {
	var out []string

	switch {
	case focused && (fd.Locked != "" || fd.Note != ""):
		if fd.Locked != "" {
			for _, l := range wrapWords(e, fd.Locked, w, 2) {
				out = append(out, indent+e.Theme.Muted.Render(l))
			}
		}

		if fd.Note != "" {
			for _, l := range wrapWords(e, e.Glyphs.warn+" "+fd.Note, w, 2) {
				out = append(out, indent+e.Theme.Warn.Render(l))
			}
		}
	case fd.Err != "" && (fd.Touched || f.submitted):
		for _, l := range wrapWords(e, e.Glyphs.warn+" "+fd.Err, w, 2) {
			out = append(out, indent+e.Theme.Err.Render(l))
		}
	case focused && fd.hintKey() != "":
		for _, l := range wrapWords(e, fd.words(e, fd.hintKey()), w, 2) {
			out = append(out, indent+e.Theme.Muted.Render(l))
		}
	}

	return out
}

// messages returns the lines for the failure of the last save, or the clash of the record.
//
// Parameters:
//   - e: shared environment
//   - inner: cells inside the box
//   - most: most lines to return; the text is wrapped on that many lines, cut with an ellipsis
//     when it needs more, and left out when it is 0
func (f *fieldSet) messages(e *env, inner, most int) []string {
	text := f.msg
	if text == "" {
		text = f.problem
	}

	if text == "" {
		return nil
	}

	var out []string

	for _, l := range wrapWords(e, e.Glyphs.warn+" "+sanitize(text), inner-2, min(3, most)) {
		out = append(out, " "+e.Theme.Err.Render(l))
	}

	return out
}

// scrolled keeps a number of rows, scrolling so that the focused rows are in view. When rows are
// out of view it spends the first and the last row on a "N more" line, as the lists do, which
// counts the fields that are hidden.
//
// Parameters:
//   - e: shared environment
//   - rows: all the rows
//   - firsts: index in rows of the first row of each field
//   - cur: index of the focused field
//   - focusEnd: index after the last row of the focused field
//   - h: rows to keep
//
// Returns the rows to draw, the index in rows of the first one that is not a "N more" line, and 1
// when a "N more" line is the first row drawn, else 0. A block too small to spare the rows shows
// no line. The first row of the focused field, which holds its label and what is typed, is
// always among the rows drawn, even when its note below it is then cut.
func (f *fieldSet) scrolled(e *env, rows []string, firsts []int, cur, focusEnd,
	h int,
) ([]string, int, int) {
	n := len(rows)
	if n <= h {
		return rows, 0, 0
	}

	if h < minMoreRows {
		s := min(windowStart(n, focusEnd, h), firsts[cur])

		return rows[s : s+h], s, 0
	}

	inner := h - 2
	s := min(windowStart(n, focusEnd, inner), firsts[cur])

	switch {
	case s == 0:
		inner = h - 1
	case s+inner >= n:
		inner = h - 1
		s = n - inner
	}

	end := func(i int) int {
		if i+1 < len(firsts) {
			return firsts[i+1]
		}

		return n
	}

	var above, below int

	for i, first := range firsts {
		if end(i) <= s {
			above++
		}

		if first >= s+inner {
			below++
		}
	}

	out := make([]string, 0, h)
	lead, lift := spaces(3), 0

	if s > 0 {
		out = append(out, lead+e.moreLine(e.Glyphs.above, max(1, above)))
		lift = 1
	}

	out = append(out, rows[s:s+inner]...)

	if s+inner < n {
		out = append(out, lead+e.moreLine(e.Glyphs.below, max(1, below)))
	}

	return out, s, lift
}

// Typing reports that the form reads text, so that "?" and the other keys of the screens belong
// to the field.
func (f *fieldSet) Typing() bool {
	return true
}

// windowStart gives the first row that scrolled keeps.
//
// Parameters:
//   - n: number of rows
//   - focusEnd: index after the last row of the focused field
//   - h: rows to keep
//
// Returns 0 when everything fits.
func windowStart(n, focusEnd, h int) int {
	if n <= h {
		return 0
	}

	return min(n-h, max(0, focusEnd-h))
}

// layout lays out the box of a form.
//
// Parameters:
//   - e: shared environment
//   - title: translated title of the box
//   - maxW: widest line allowed
//   - maxH: most lines allowed
//
// Returns the lines of the box and the zones of the fields that are on them, counted from its top
// left corner.
func (f *fieldSet) layout(e *env, title string, maxW, maxH int) ([]string, []zone) {
	w := max(4, min(maxW, formWidth))
	inner := w - 2

	labels := make([]string, len(f.fields))
	labelW := 0

	for i, fd := range f.fields {
		labels[i] = fd.words(e, fd.Label)
		labelW = max(labelW, e.Metric.width(labels[i]))
	}

	if f.wide {
		labelW = min(labelW, max(1, inner/2))
	} else {
		labelW = min(labelW, formLabelMax, max(1, inner/3))
	}
	valW := max(1, inner-3-labelW-2)
	note := spaces(3 + labelW + 1)

	var rows []string

	focusEnd := 0
	firsts := make([]int, len(f.fields))

	for i, fd := range f.fields {
		firsts[i] = len(rows)
		marker, label := "  ", e.Theme.Muted
		if i == f.cur {
			marker, label = e.Theme.Title.Render(e.Glyphs.marker)+" ", e.Theme.Key
		}

		rows = append(rows, " "+marker+label.Render(e.Metric.pad(labels[i], labelW,
			e.Glyphs.ellipsis))+" "+fd.view(e, valW, i == f.cur))

		rows = append(rows, f.notes(e, fd, i == f.cur, note, valW)...)

		if i == f.cur {
			focusEnd = len(rows)
		}
	}

	msgs := f.messages(e, inner, max(0, maxH-2-formMinRows))
	h := max(1, maxH-2-len(msgs))
	rows, start, lift := f.scrolled(e, rows, firsts, f.cur, focusEnd, h)

	var zs []zone

	for i, first := range firsts {
		if y := first - start + lift; y >= lift && y < len(rows) {
			zs = append(zs, zone{y: 1 + y, x: 1, w: inner, id: i})
		}
	}

	rows = append(rows, msgs...)

	return floating(e, e.Theme.Bold.Render(title), rows, w), zs
}
