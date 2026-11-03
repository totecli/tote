package ui

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// fieldType says how a field of a form is edited.
type fieldType int

const (
	// fieldText is a single line of text.
	fieldText fieldType = iota

	// fieldChoice is one value picked from a short list.
	fieldChoice

	// fieldToggle is a yes or no switch.
	fieldToggle

	// fieldList is a list of texts, edited in a box of its own.
	fieldList
)

// formField is one line of a form.
//
// ID names the field in the record, Label, Hint and Default are catalog keys: the label shown
// at the left, the help shown under the field while it has the focus and the grey text shown
// while a text field is empty. A choice has Values, the stored values, Names, the catalog keys of
// what is shown, and Hints, one catalog key per value that replaces Hint; Pick is the chosen
// index. A toggle keeps its state in On. Input is the editing engine of a text field: it holds
// the text and the cursor, while the field draws them itself. Touched becomes true once the user
// changed the field or left it, Err is the localized reason the value is refused, empty when it
// is accepted, and Off is the first cell of the text that is shown when the text is longer than
// the room.
//
// A field of a plugin has its texts literally: Raw tells that Label, Hint, Default and Names are
// the words to show, already clean, not catalog keys. Items is the content of a list field and
// Locked, when it is not empty, is the translated reason the field cannot be changed. Note is a
// translated remark about the value, shown in the warning color while the field has the keys.
type formField struct {
	ID      string
	Label   string
	Hint    string
	Default string
	Type    fieldType
	Input   textinput.Model
	Values  []string
	Names   []string
	Hints   []string
	Pick    int
	On      bool
	Touched bool
	Err     string
	Off     int
	Raw     bool
	Items   []string
	Locked  string
	Note    string
}

// newTextField returns an empty text field.
//
// Parameters:
//   - id: name of the field in the record
//   - label: catalog key of the label
//   - hint: catalog key of the help text, empty for none
//   - limit: most characters accepted
func newTextField(id, label, hint string, limit int) *formField {
	in := newInput("", limit)
	in.SetVirtualCursor(false)

	return &formField{ID: id, Label: label, Hint: hint, Input: in}
}

// newChoiceField returns a field that picks one value among a few.
//
// Parameters:
//   - id: name of the field in the record
//   - label: catalog key of the label
//   - values: stored values, the first one is chosen at the start
//   - names: catalog key of the text shown for each value
//   - hints: catalog key of the help text of each value
func newChoiceField(id, label string, values, names, hints []string) *formField {
	return &formField{
		ID: id, Label: label, Type: fieldChoice, Values: values, Names: names,
		Hints: hints,
	}
}

// newToggleField returns a yes or no field, set to no.
//
// Parameters:
//   - id: name of the field in the record
//   - label: catalog key of the label
//   - hint: catalog key of the help text
func newToggleField(id, label, hint string) *formField {
	return &formField{ID: id, Label: label, Hint: hint, Type: fieldToggle}
}

// words gives a text of the field in the language of the interface.
//
// Parameters:
//   - e: shared environment
//   - s: catalog key, or the text itself when the field is Raw
func (fd *formField) words(e *env, s string) string {
	if fd.Raw {
		return sanitize(s)
	}

	return e.t(s)
}

// text returns the text of a field, or the chosen value of a choice. A toggle gives "true" or
// "false" and a list its items, one per line.
func (fd *formField) text() string {
	switch fd.Type {
	case fieldList:
		return strings.Join(fd.Items, "\n")
	case fieldChoice:
		return fd.Values[fd.Pick]
	case fieldToggle:
		if fd.On {
			return "true"
		}

		return "false"
	}

	return strings.TrimSpace(fd.Input.Value())
}

// setText puts a text in a text field and the cursor at its end.
//
// Parameters:
//   - s: new content
func (fd *formField) setText(s string) {
	fd.Input.SetValue(s)
	fd.Input.CursorEnd()
	fd.Off = 0
}

// hintKey returns the catalog key of the help text to show for the current state.
func (fd *formField) hintKey() string {
	if fd.Type == fieldChoice && fd.Pick < len(fd.Hints) {
		return fd.Hints[fd.Pick]
	}

	return fd.Hint
}

// focus gives the field the keys.
func (fd *formField) focus() {
	if fd.Type == fieldText {
		fd.Input.Focus()
	}
}

// blur takes the keys from the field and marks it as visited.
func (fd *formField) blur() {
	if fd.Type == fieldText {
		fd.Input.Blur()
	}

	fd.Touched = true
}

// cleanTyped makes text typed or pasted safe for a one line field.
//
// Parameters:
//   - s: text received from the terminal
//
// Returns s where a line break or a tab became a space and every other control character, escape
// or invalid byte was dropped.
func cleanTyped(s string) string {
	var b strings.Builder

	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]

		switch {
		case r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r == '\r' || (r == utf8.RuneError && size == 1) || unsafeRune(r):
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

// editBindings lists the bindings of the text input that act on the text or on the cursor.
//
// Parameters:
//   - km: key map of the input
func editBindings(km textinput.KeyMap) []key.Binding {
	return []key.Binding{
		km.CharacterForward, km.CharacterBackward, km.WordForward, km.WordBackward,
		km.DeleteWordBackward, km.DeleteWordForward, km.DeleteAfterCursor,
		km.DeleteBeforeCursor, km.DeleteCharacterBackward, km.DeleteCharacterForward,
		km.LineStart, km.LineEnd,
	}
}

// edit gives a message to the text input and keeps the result safe.
//
// Parameters:
//   - msg: key press or paste; anything else is ignored
//
// Returns true when the text changed. Only the editing keys of the input work, plus printable
// text; a key with Ctrl, Alt or another modifier that the input does not bind is dropped, so
// that no stray character gets in. Typed and pasted text goes through cleanTyped. The cursor
// is moved to the edge of a grapheme when an edit left it inside one.
func (fd *formField) edit(msg tea.Msg) bool {
	if fd.Type != fieldText || fd.Locked != "" {
		return false
	}

	switch m := msg.(type) {
	case tea.KeyPressMsg:
		bound := false

		for _, b := range editBindings(fd.Input.KeyMap) {
			bound = bound || key.Matches(m, b)
		}

		if !bound {
			if m.Mod&^tea.ModShift != 0 {
				return false
			}

			m.Text = cleanTyped(m.Text)
			if m.Text == "" {
				return false
			}
		}

		msg = m
	case tea.PasteMsg:
		m.Content = cleanTyped(m.Content)
		if m.Content == "" {
			return false
		}

		msg = m
	default:
		return false
	}

	before, pos := fd.Input.Value(), fd.Input.Position()
	fd.Input, _ = fd.Input.Update(msg)
	fd.snap(pos)

	return fd.Input.Value() != before
}

// graphemeStarts returns the index in runes where each grapheme of a text starts, then the
// number of runes.
//
// Parameters:
//   - s: text to cut
func graphemeStarts(s string) []int {
	var out []int

	n := 0

	for len(s) > 0 {
		cluster, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if cluster == "" {
			break
		}

		out = append(out, n)
		n += utf8.RuneCountInString(cluster)
		s = s[len(cluster):]
	}

	return append(out, n)
}

// snap moves the cursor to the edge of the grapheme it is in, forward when it moved forward.
//
// Parameters:
//   - before: position of the cursor before the edit
func (fd *formField) snap(before int) {
	pos := fd.Input.Position()
	edges := graphemeStarts(fd.Input.Value())

	for i, edge := range edges {
		if edge == pos {
			return
		}

		if edge > pos {
			if pos > before || i == 0 {
				fd.Input.SetCursor(edge)
			} else {
				fd.Input.SetCursor(edges[i-1])
			}

			return
		}
	}

	fd.Input.SetCursor(edges[len(edges)-1])
}

// textCell is one grapheme of a text field as it is drawn.
type textCell struct {
	text  string
	w     int
	start int
}

// cells cuts the text of a field into the graphemes that are drawn.
//
// Parameters:
//   - e: shared environment, for the width rule
//
// Returns the graphemes after sanitize. A grapheme that would take no cell is drawn as a question
// mark so that the cursor can stand on it.
func (fd *formField) cells(e *env) []textCell {
	var out []textCell

	s, n := fd.Input.Value(), 0

	for len(s) > 0 {
		cluster, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if cluster == "" {
			break
		}

		shown := sanitize(cluster)
		w := e.Metric.width(shown)

		if w <= 0 {
			shown, w = "?", 1
		}

		out = append(out, textCell{text: shown, w: w, start: n})
		n += utf8.RuneCountInString(cluster)
		s = s[len(cluster):]
	}

	return out
}

// scrollWindow chooses the part of a long text that is shown.
//
// Parameters:
//   - widths: width of each cell, the last one being the empty cell after the text where the
//     cursor stands at the end
//   - cur: index of the cell under the cursor
//   - off: first cell shown before the move
//   - avail: cells available
//   - mark: width of the mark drawn where the text goes on
//
// Returns the first cell shown and the first cell not shown. The cursor cell is always inside.
// The window keeps its place while the cursor stays in it, and is pulled back when the text got
// shorter.
func scrollWindow(widths []int, cur, off, avail, mark int) (start, end int) {
	n := len(widths)
	total := 0

	for _, w := range widths {
		total += w
	}

	if total <= avail {
		return 0, n
	}

	off = max(0, min(off, cur, n-1))

	fits := func(from int) bool {
		sum := 0
		for _, w := range widths[from:] {
			sum += w
		}

		if from > 0 {
			sum += mark
		}

		return sum <= avail
	}

	for off > 0 && fits(off-1) {
		off--
	}

	for {
		used := 0
		if off > 0 {
			used = mark
		}

		end = off

		for end < n && used+widths[end] <= avail {
			used += widths[end]
			end++
		}

		if end < n {
			for end > off && used+mark > avail {
				end--
				used -= widths[end]
			}
		}

		if cur < end || off >= cur {
			return off, max(end, cur+1)
		}

		off++
	}
}

// view draws the value of the field.
//
// Parameters:
//   - e: shared environment
//   - w: cells available
//   - focused: true when the field has the keys, which shows the cursor
//
// Returns a styled line of exactly w cells.
func (fd *formField) view(e *env, w int, focused bool) string {
	var s string

	switch fd.Type {
	case fieldChoice:
		s = fd.viewChoice(e, w, focused)
	case fieldToggle:
		s = fd.viewToggle(e, focused)
	case fieldList:
		s = fd.viewList(e, w)
	default:
		s = fd.viewText(e, w, focused)
	}

	if fd.Locked != "" {
		s = e.Theme.Muted.Render(ansi.Strip(s))
	}

	return e.clipPad(s, w)
}

// viewList draws the number of the items of a list and the items on one line, or the word none.
//
// Parameters:
//   - e: shared environment
//   - w: cells available
func (fd *formField) viewList(e *env, w int) string {
	if len(fd.Items) == 0 {
		return e.Theme.Muted.Render(e.t("cli_plugin_none"))
	}

	count := e.t("ui_plug_items", len(fd.Items))
	room := w - e.Metric.width(count) - 3

	if room < 2 {
		return e.Theme.Text.Render(e.Metric.fit(count, w, e.Glyphs.ellipsis))
	}

	return e.Theme.Text.Render(count) + e.Theme.Muted.Render(" ("+e.Metric.fit(
		sanitize(strings.Join(fd.Items, ", ")), room, e.Glyphs.ellipsis)+")")
}

// viewChoice draws the values of a choice side by side, the picked one highlighted. When they do
// not all fit, the row scrolls so that the picked one is always in view, with an ellipsis at the
// side where values are left out.
//
// Parameters:
//   - e: shared environment
//   - w: cells available
//   - focused: true to draw the picked value as a selection
func (fd *formField) viewChoice(e *env, w int, focused bool) string {
	labels := make([]string, len(fd.Names))
	widths := make([]int, len(fd.Names))

	for i, name := range fd.Names {
		labels[i] = " " + fd.words(e, name) + " "
		widths[i] = e.Metric.width(labels[i])
	}

	render := func(i int, label string) string {
		switch {
		case i != fd.Pick:
			return e.Theme.Muted.Render(label)
		case focused:
			return e.Theme.Selected.Render(label)
		}

		return e.Theme.Bold.Render(label)
	}

	mark := e.Metric.width(e.Glyphs.ellipsis)
	first, last := choiceSpan(widths, fd.Pick, w, mark)

	lead, trail := first > 0, last < len(labels)-1
	room := w

	if first == last {
		// A value alone on the row has the room before the marks of its sides, and when it does not
		// fit with them it gives them up.
		if lead {
			room -= mark + 1
		}

		if trail {
			room -= mark + 1
		}

		if widths[first] > room {
			lead, trail, room = false, false, w
		}
	}

	var parts []string

	if lead {
		parts = append(parts, e.Theme.Muted.Render(e.Glyphs.ellipsis))
	}

	for i := first; i <= last; i++ {
		label := labels[i]
		if i == fd.Pick && first == last {
			label = e.Metric.fit(label, room, e.Glyphs.ellipsis)
		}

		parts = append(parts, render(i, label))
	}

	if trail {
		parts = append(parts, e.Theme.Muted.Render(e.Glyphs.ellipsis))
	}

	return strings.Join(parts, " ")
}

// choiceSpan works out which values of a choice are drawn: the picked one, then as many of the
// ones before it as fit, then as many after it as fit, each side that leaves some out taking a
// cell for its ellipsis.
//
// Parameters:
//   - widths: width of each value, spaces around it included
//   - pick: index of the picked value
//   - w: cells available
//   - mark: width of the ellipsis
//
// Returns the first and the last value drawn.
func choiceSpan(widths []int, pick, w, mark int) (int, int) {
	n := len(widths)
	if n == 0 {
		return 0, -1
	}

	pick = max(0, min(pick, n-1))

	total := n - 1
	for _, cw := range widths {
		total += cw
	}

	if total <= w {
		return 0, n - 1
	}

	first, last := pick, pick

	need := func(f, l int) int {
		u := 0
		for i := f; i <= l; i++ {
			u += widths[i] + 1
		}

		u--

		if f > 0 {
			u += mark + 1
		}

		if l < n-1 {
			u += mark + 1
		}

		return u
	}

	for {
		grew := false

		if first > 0 && need(first-1, last) <= w {
			first--
			grew = true
		}

		if last < n-1 && need(first, last+1) <= w {
			last++
			grew = true
		}

		if !grew {
			break
		}
	}

	return first, last
}

// viewToggle draws a yes or no switch.
//
// Parameters:
//   - e: shared environment
//   - focused: true to draw the box as a selection
func (fd *formField) viewToggle(e *env, focused bool) string {
	box, word := "[ ]", e.t("ui_choice_no")
	if fd.On {
		box, word = "[x]", e.t("ui_choice_yes")
	}

	if focused {
		return e.Theme.Selected.Render(box) + " " + e.Theme.Text.Render(word)
	}

	return e.Theme.Text.Render(box + " " + word)
}

// viewText draws a text field, with the cursor and a horizontal scroll when the text is longer
// than the room. It updates the scroll position of the field.
//
// Parameters:
//   - e: shared environment
//   - w: cells available
//   - focused: true to draw the cursor and the placeholder next to it
func (fd *formField) viewText(e *env, w int, focused bool) string {
	cells := fd.cells(e)

	if len(cells) == 0 {
		hold := ""
		if fd.Default != "" {
			hold = e.Metric.fit(fd.words(e, fd.Default), max(0, w-1), e.Glyphs.ellipsis)
		}

		if !focused {
			return e.Theme.Muted.Render(hold)
		}

		return e.Theme.Cursor.Render(" ") + e.Theme.Muted.Render(hold)
	}

	if !focused {
		var b strings.Builder

		for _, c := range cells {
			b.WriteString(c.text)
		}

		return e.Theme.Text.Render(e.Metric.fit(b.String(), w, e.Glyphs.ellipsis))
	}

	cur := len(cells)

	for i, c := range cells {
		if c.start >= fd.Input.Position() {
			cur = i
			break
		}
	}

	widths := make([]int, len(cells)+1)
	for i, c := range cells {
		widths[i] = c.w
	}

	widths[len(cells)] = 1
	mark := e.Metric.width(e.Glyphs.ellipsis)

	start, end := scrollWindow(widths, cur, fd.Off, w, mark)
	fd.Off = start

	var b strings.Builder

	if start > 0 {
		b.WriteString(e.Theme.Muted.Render(e.Glyphs.ellipsis))
	}

	for i := start; i < end; i++ {
		text := " "
		if i < len(cells) {
			text = cells[i].text
		}

		if i == cur {
			b.WriteString(e.Theme.Cursor.Render(text))
		} else {
			b.WriteString(e.Theme.Text.Render(text))
		}
	}

	if end < len(widths) {
		b.WriteString(e.Theme.Muted.Render(e.Glyphs.ellipsis))
	}

	return b.String()
}
