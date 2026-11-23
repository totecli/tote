package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/totecli/tote/internal/plugin"
)

// areaWidth is the width the editing engine of a multiline field is given: wider than the
// longest line a value can hold, plugin.MaxMultilineBytes characters of two cells, so that the
// engine never wraps a line itself and its up and down moves go from one line to the next. The
// field wraps the lines when it draws them, at the width of the form.
const areaWidth = 2*plugin.MaxMultilineBytes + 8

// newMultiField returns an empty multiline text field.
//
// Parameters:
//   - id: name of the field in the record
//   - label: catalog key of the label
//   - hint: catalog key of the help text, empty for none
//   - rows: rows the field shows, brought within plugin.MinMultilineRows and
//     plugin.MaxMultilineRows, plugin.DefaultMultilineRows when it is 0 or less
//   - limit: most characters accepted
func newMultiField(id, label, hint string, rows, limit int) *formField {
	if rows <= 0 {
		rows = plugin.DefaultMultilineRows
	}

	rows = min(max(rows, plugin.MinMultilineRows), plugin.MaxMultilineRows)

	return &formField{
		ID: id, Label: label, Hint: hint, Type: fieldMulti, Area: newArea(), Rows: rows,
		Limit: limit,
	}
}

// newArea returns the editing engine of a multiline field: no prompt, no line numbers, no limit
// of its own (editArea applies the limits of a value) and only the keys of areaBindings. The
// clipboard keys are off, like Ctrl-V in newInput: their handlers start a helper program found on
// PATH. Text pasted by the terminal arrives as a tea.PasteMsg and works as usual.
func newArea() textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.MaxWidth = 0
	ta.KeyMap = areaKeyMap()
	ta.SetVirtualCursor(false)
	ta.SetWidth(areaWidth)
	ta.SetHeight(1)

	return ta
}

// areaKeyMap returns the keys of the editing engine of a multiline field: the ones of the
// default key map that edit the text or move the cursor along it, Enter for a new line, and
// nothing else. Up and down are left to the field, which moves along the rows it draws, see
// areaStep. The Delete key deletes forward and Ctrl-D is left to the quit key, as in newInput.
func areaKeyMap() textarea.KeyMap {
	km := textarea.DefaultKeyMap()
	off := key.NewBinding()

	km.InsertNewline = key.NewBinding(key.WithKeys("enter"))
	km.DeleteCharacterForward = key.NewBinding(key.WithKeys("delete"))
	km.LineNext, km.LinePrevious, km.PageUp, km.PageDown = off, off, off, off
	km.Paste, km.CopySelection, km.SelectAll = off, off, off
	km.SelectCharacterForward, km.SelectCharacterBackward = off, off
	km.SelectWordForward, km.SelectWordBackward = off, off
	km.SelectLineUp, km.SelectLineDown = off, off
	km.UppercaseWordForward, km.LowercaseWordForward = off, off
	km.CapitalizeWordForward, km.TransposeCharacterBackward = off, off

	return km
}

// areaBindings lists the bindings of the editing engine of a multiline field that act on the text
// or on the cursor.
//
// Parameters:
//   - km: key map of the engine
func areaBindings(km textarea.KeyMap) []key.Binding {
	return []key.Binding{
		km.CharacterForward, km.CharacterBackward, km.WordForward, km.WordBackward,
		km.DeleteWordBackward, km.DeleteWordForward, km.DeleteAfterCursor,
		km.DeleteBeforeCursor, km.DeleteCharacterBackward, km.DeleteCharacterForward,
		km.LineStart, km.LineEnd, km.InputBegin, km.InputEnd, km.InsertNewline,
	}
}

// cleanLines makes text typed or pasted safe for a multiline field.
//
// Parameters:
//   - s: text received from the terminal
//
// Returns s where every line break became a line feed, a tab became a space and every other
// control character, escape, bidirectional control or invalid byte was dropped.
func cleanLines(s string) string {
	return cleanText(plugin.NormalizeLines(s), true)
}

// trimLines gives the value of a multiline field: the spaces at the end of each line and the
// blank lines before the first line and after the last one are dropped. The spaces at the start
// of a line are kept, so that an indented line stays indented.
//
// Parameters:
//   - s: text of the field
func trimLines(s string) string {
	lines := strings.Split(s, "\n")

	for i, l := range lines {
		lines[i] = strings.TrimRightFunc(l, unicode.IsSpace)
	}

	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return strings.Join(lines, "\n")
}

// fitLines cuts text about to be inserted in a multiline field so that the value stays within
// its limits: plugin.MaxMultilineBytes bytes, plugin.MaxMultilineLines lines and Limit
// characters. These are the limits that the registry checks, so the user cannot type a value
// that it would refuse for its size.
//
// Parameters:
//   - add: clean text to insert
//
// Returns the longest start of add that fits, "" when nothing fits.
func (fd *formField) fitLines(add string) string {
	v := fd.Area.Value()
	bytes := plugin.MaxMultilineBytes - len(v)
	runes := fd.Limit - utf8.RuneCountInString(v)
	lines := plugin.MaxMultilineLines - strings.Count(v, "\n") - 1

	for i, r := range add {
		if r == '\n' {
			lines--
		}

		bytes -= utf8.RuneLen(r)
		runes--

		if lines < 0 || bytes < 0 || runes < 0 {
			return add[:i]
		}
	}

	return add
}

// editArea gives a message to the editing engine of a multiline field and keeps the result safe.
//
// Parameters:
//   - msg: key press or paste; anything else is ignored
//
// Returns true when the text changed. As in a one line field, only the editing keys of the
// engine work, plus printable text, and a key with Ctrl, Alt or another modifier that the engine
// does not bind is dropped. Typed and pasted text goes through cleanLines, then fitLines; Enter
// adds a line while the value has room for it. The cursor is moved to the edge of a grapheme
// when an edit left it inside one.
func (fd *formField) editArea(msg tea.Msg) bool {
	if fd.Locked != "" {
		return false
	}

	switch m := msg.(type) {
	case tea.KeyPressMsg:
		bound := false

		for _, b := range areaBindings(fd.Area.KeyMap) {
			bound = bound || key.Matches(m, b)
		}

		switch {
		case key.Matches(m, fd.Area.KeyMap.InsertNewline):
			if fd.fitLines("\n") == "" {
				return false
			}
		case !bound:
			if m.Mod&^tea.ModShift != 0 {
				return false
			}

			m.Text = fd.fitLines(cleanLines(m.Text))
			if m.Text == "" {
				return false
			}
		}

		msg = m
	case tea.PasteMsg:
		m.Content = fd.fitLines(cleanLines(m.Content))
		if m.Content == "" {
			return false
		}

		msg = m
	default:
		return false
	}

	before, row, col := fd.Area.Value(), fd.Area.Line(), fd.Area.Column()

	if k, ok := msg.(tea.KeyPressMsg); ok && key.Matches(k, fd.Area.KeyMap.DeleteCharacterForward) {
		msg = fd.deleteForward(k)
	}

	fd.Area, _ = fd.Area.Update(msg)
	fd.snapArea(row, col)

	return fd.Area.Value() != before
}

// deleteForward returns the message to give the engine for the Delete key. The engine joins the
// next line after deleting a character when the cursor is then at the end of its line, so one
// Delete on the last character of a line would remove it and also join the next line. In the
// middle of a line the cursor is moved one rune forward and a Backspace is sent, which deletes
// exactly one rune and joins lines only at the start of a line. At the end of a line the key goes
// on as it is: there it joins the next line, which is what Delete does there.
//
// Parameters:
//   - k: Delete key press
func (fd *formField) deleteForward(k tea.KeyPressMsg) tea.Msg {
	lines := strings.Split(fd.Area.Value(), "\n")
	row, col := fd.Area.Line(), fd.Area.Column()

	if row >= len(lines) || col >= utf8.RuneCountInString(lines[row]) {
		return k
	}

	fd.Area.SetCursorColumn(col + 1)

	return tea.KeyPressMsg{Code: tea.KeyBackspace}
}

// snapArea moves the cursor of a multiline field to the edge of the grapheme it is in, forward
// when it moved forward.
//
// Parameters:
//   - row: line of the cursor before the edit
//   - col: column of the cursor before the edit, in runes
func (fd *formField) snapArea(row, col int) {
	r, c := fd.Area.Line(), fd.Area.Column()
	lines := strings.Split(fd.Area.Value(), "\n")

	if r >= len(lines) {
		return
	}

	edges := graphemeStarts(lines[r])
	forward := r > row || (r == row && c > col)

	for i, edge := range edges {
		if edge == c {
			return
		}

		if edge > c {
			if forward || i == 0 {
				fd.Area.SetCursorColumn(edge)
			} else {
				fd.Area.SetCursorColumn(edges[i-1])
			}

			return
		}
	}

	fd.Area.SetCursorColumn(edges[len(edges)-1])
}

// areaRow is one row of a multiline field as it is drawn: the graphemes of a part of a line.
// Line is the index of the line in the text, From and To the runes of the line the row holds,
// and Last is true for the last row of its line, where the cursor stands after the text.
type areaRow struct {
	line  int
	cells []textCell
	from  int
	to    int
	last  bool
}

// holds reports whether the cursor of a multiline field is on a row.
//
// Parameters:
//   - line: line of the cursor
//   - col: column of the cursor, in runes
func (r areaRow) holds(line, col int) bool {
	return line == r.line && col >= r.from && (col < r.to || (r.last && col == r.to))
}

// areaRows wraps the text of a multiline field at the words, as it is drawn.
//
// Parameters:
//   - e: shared environment, for the width rule
//   - w: cells of text on a row
//
// Returns at least one row. A line is cut after the last space that fits, or where the row is
// full when it has no space; an empty line takes one row.
func (fd *formField) areaRows(e *env, w int) []areaRow {
	var out []areaRow

	for li, line := range strings.Split(fd.Area.Value(), "\n") {
		cells := textCells(e, line)
		end := utf8.RuneCountInString(line)
		a := 0

		for {
			b, used, space := a, 0, -1

			for b < len(cells) && used+cells[b].w <= w {
				if cells[b].text == " " {
					space = b
				}

				used += cells[b].w
				b++
			}

			switch {
			case b == a && b < len(cells):
				b++
			case b+1 < len(cells) && cells[b].text == " ":
				// The space after the last word that fits ends the row, in the cell kept for the
				// cursor, so that the next row starts with a word.
				b++
			case b < len(cells) && space > a:
				b = space + 1
			}

			row := areaRow{line: li, cells: cells[a:b], from: 0, to: end, last: b >= len(cells)}
			if a < len(cells) {
				row.from = cells[a].start
			}

			if b < len(cells) {
				row.to = cells[b].start
			}

			out = append(out, row)
			a = b

			if a >= len(cells) {
				break
			}
		}
	}

	return out
}

// areaCursor finds the cursor of a multiline field among its rows.
//
// Parameters:
//   - rows: the rows of areaRows
//
// Returns the index of the row and the cells before the cursor on it.
func (fd *formField) areaCursor(rows []areaRow) (int, int) {
	line, col := fd.Area.Line(), fd.Area.Column()

	for i, r := range rows {
		if !r.holds(line, col) {
			continue
		}

		x := 0

		for _, c := range r.cells {
			if c.start >= col {
				break
			}

			x += c.w
		}

		return i, x
	}

	return 0, 0
}

// areaText gives the cells of text on a row of a multiline field drawn w cells wide: what is left
// once the margin is drawn, less the cell where the cursor stands after the text.
//
// Parameters:
//   - w: cells of the value
func areaText(w int) int {
	return max(1, w-3)
}

// areaStep moves the cursor of a multiline field to the row above or below, as the rows are
// drawn, at the same distance from the margin or at the end of the row when it is shorter.
//
// Parameters:
//   - e: shared environment, for the width rule
//   - down: true for the row below
//
// Returns false, without moving anything, when the field is not a multiline field that can be
// changed or when the cursor is already on its first row, for up, or on its last one, for down:
// the key then belongs to the form, which goes to another field.
func (fd *formField) areaStep(e *env, down bool) bool {
	if fd.Type != fieldMulti || fd.Locked != "" {
		return false
	}

	w := fd.Wrap
	if w <= 0 {
		w = areaWidth
	}

	rows := fd.areaRows(e, w)
	cur, x := fd.areaCursor(rows)

	next := cur - 1
	if down {
		next = cur + 1
	}

	if next < 0 || next >= len(rows) {
		return false
	}

	r := rows[next]
	col, used := -1, 0

	for _, c := range r.cells {
		if used+c.w > x {
			col = c.start

			break
		}

		used += c.w
	}

	switch {
	case col >= 0:
	case r.last || len(r.cells) == 0:
		col = r.to
	default:
		col = r.cells[len(r.cells)-1].start
	}

	fd.areaGo(r.line, col)

	return true
}

// areaGo puts the cursor of a multiline field on a line and a column.
//
// Parameters:
//   - line: index of the line
//   - col: column, in runes
func (fd *formField) areaGo(line, col int) {
	fd.Area.MoveToBegin()

	for range line {
		fd.Area.CursorDown()
	}

	fd.Area.SetCursorColumn(col)
}

// viewArea draws a multiline field on its rows. A margin at the left shows where the field is,
// with an arrow at the top or at the bottom when the text goes on above or below the rows shown.
// The rows scroll so that the cursor stays in view.
//
// Parameters:
//   - e: shared environment
//   - w: cells available
//   - focused: true to draw the cursor
//   - most: most rows to draw; the field draws Rows rows when it has room
//
// Returns the rows, each exactly w cells. It updates the scroll position of the field and the
// width the up and down keys follow.
func (fd *formField) viewArea(e *env, w int, focused bool, most int) []string {
	tw := areaText(w)
	fd.Wrap = tw

	rows := fd.areaRows(e, tw)
	vis := max(1, min(fd.Rows, most))
	cur, _ := fd.areaCursor(rows)

	if focused {
		if cur < fd.Top {
			fd.Top = cur
		}

		if cur >= fd.Top+vis {
			fd.Top = cur - vis + 1
		}
	}

	fd.Top = max(0, min(fd.Top, len(rows)-vis))

	empty := fd.Area.Value() == ""
	out := make([]string, vis)

	for i := range vis {
		mark := e.Glyphs.v

		switch {
		case i == 0 && fd.Top > 0:
			mark = e.Glyphs.above
		case i == vis-1 && fd.Top+vis < len(rows):
			mark = e.Glyphs.below
		}

		var body string

		switch at := fd.Top + i; {
		case empty && i == 0:
			body = fd.areaHold(e, tw, focused)
		case at < len(rows):
			body = fd.areaLine(e, rows[at], focused && at == cur)
		}

		s := e.Theme.Muted.Render(mark) + " " + body
		if fd.Locked != "" {
			s = e.Theme.Muted.Render(ansi.Strip(s))
		}

		out[i] = e.clipPad(s, w)
	}

	return out
}

// areaHold draws the first row of an empty multiline field: the cursor when it has the keys,
// then the grey placeholder, if any.
//
// Parameters:
//   - e: shared environment
//   - w: cells of text on the row
//   - focused: true to draw the cursor
func (fd *formField) areaHold(e *env, w int, focused bool) string {
	hold := ""
	if fd.Default != "" {
		hold = e.Metric.fit(fd.words(e, fd.Default), max(0, w-1), e.Glyphs.ellipsis)
	}

	if !focused {
		return e.Theme.Muted.Render(hold)
	}

	return e.Theme.Cursor.Render(" ") + e.Theme.Muted.Render(hold)
}

// areaLine draws one row of a multiline field.
//
// Parameters:
//   - e: shared environment
//   - r: the row
//   - cursor: true when the cursor is on the row
func (fd *formField) areaLine(e *env, r areaRow, cursor bool) string {
	col := fd.Area.Column()

	var b, plain strings.Builder

	flush := func() {
		if plain.Len() > 0 {
			b.WriteString(e.Theme.Text.Render(plain.String()))
			plain.Reset()
		}
	}

	shown := false

	for _, c := range r.cells {
		if cursor && !shown && c.start >= col {
			flush()
			b.WriteString(e.Theme.Cursor.Render(c.text))

			shown = true

			continue
		}

		plain.WriteString(c.text)
	}

	flush()

	if cursor && !shown {
		b.WriteString(e.Theme.Cursor.Render(" "))
	}

	return b.String()
}
