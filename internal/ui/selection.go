package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// cellPos is a cell of the terminal.
type cellPos struct {
	x int
	y int
}

// selection is the text selected with the mouse.
//
// Held is true from the press of the left button to its release. Press is the cell of the press
// brought inside the terminal and Origin the cell as the terminal reported it, which is what a
// click uses. Drag becomes true when the pointer moves to another cell while the button is held:
// from then on Anchor, which is where the press was, and Focus, which is where the pointer is,
// bound the selected text. Frozen holds the lines of the frame, without styles, as they were when
// the drag began, and Status the line of that frame that the toasts use, which is left out when
// checking that nothing changed. The highlight stays after the release until a key is pressed,
// the terminal is resized or the frame under it changes.
type selection struct {
	held   bool
	press  cellPos
	origin cellPos
	drag   bool
	anchor cellPos
	focus  cellPos
	frozen []string
	status int
}

// gridCell is a grapheme cluster of a line: its text, the first cell it takes and how many cells.
type gridCell struct {
	text string
	x    int
	w    int
}

// gridRow is a line of the frame cut into grapheme clusters, with the part of it that is content.
//
// From and To bound the clusters that are content: the blanks at both ends are left out, and so
// are the glyphs of the border of a box when the line is a row of one. Rule is true for the top
// or the bottom border of a box or a rule inside it, whose content is its title if it has one.
type gridRow struct {
	cells []gridCell
	from  int
	to    int
	rule  bool
}

// selRow is what a selection takes of one line.
//
// Y is the line, X0 and X1 the cells that are highlighted, X1 being the first cell after them,
// and Lo the first cell of the content of the line. Natural is true when the selection starts at
// or before the content of a line of text, so that its indentation counts; the title in a border
// does not count. Text is the selected clusters and Empty is true when nothing of the content is
// selected.
type selRow struct {
	y       int
	x0      int
	x1      int
	lo      int
	natural bool
	text    string
	empty   bool
}

// clampCell brings a cell of a mouse event inside the terminal.
//
// Parameters:
//   - x: column, which may be past the edges when the pointer left the window
//   - y: row
func (r *root) clampCell(x, y int) cellPos {
	return cellPos{x: max(0, min(x, r.e.Cols-1)), y: max(0, min(y, r.e.Height-1))}
}

// plainLines removes the styles of the lines of a frame.
//
// Parameters:
//   - lines: styled lines
func plainLines(lines []string) []string {
	out := make([]string, len(lines))

	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}

	return out
}

// gridOf cuts a plain line into grapheme clusters and finds its content.
//
// Parameters:
//   - plain: line without styles
//
// Returns the clusters, measured with the metric of the renderer so that a wide character or an
// emoji is one cluster that takes its cells together. The content leaves out the blanks at both
// ends. A line that starts and ends with the sides of a box, or with its corners or tees, also
// leaves those out, and the rule of a top or a bottom border with them, so that only the text
// inside the box, its title included, is content. This reads the glyphs of the line and not the
// box that drew it: in the ASCII set a line of text that starts and ends with a bar loses them.
func (e *env) gridOf(plain string) gridRow {
	var cells []gridCell

	x := 0

	for s := plain; s != ""; {
		c, w := ansi.FirstGraphemeCluster(s, e.Metric.method)
		if c == "" {
			break
		}

		s = s[len(c):]

		if w == 0 {
			if n := len(cells); n > 0 {
				cells[n-1].text += c
			}

			continue
		}

		cells = append(cells, gridCell{text: c, x: x, w: w})
		x += w
	}

	row := gridRow{cells: cells, to: len(cells)}
	row.trim("")

	if row.to-row.from < 2 {
		return row
	}

	g := e.Glyphs
	left, right := cells[row.from].text, cells[row.to-1].text

	switch {
	case (left == g.tl || left == g.bl || left == g.ml) &&
		(right == g.tr || right == g.br || right == g.mr):
		row.from++
		row.to--
		row.rule = true
		row.trim(g.h)
	case left == g.v && right == g.v:
		row.from++
		row.to--
		row.trim("")
	}

	if row.isMore(g) {
		row.to = row.from
	}

	return row
}

// isMore reports whether the content of the line is a "N more" line: an arrow, a blank and a
// number, which tell that a list goes on. It is chrome and not text, like a border.
//
// Parameters:
//   - set: glyph set of the interface
func (g *gridRow) isMore(set glyphs) bool {
	if g.to-g.from < 3 {
		return false
	}

	arrow, gap, digit := g.cells[g.from].text, g.cells[g.from+1].text, g.cells[g.from+2].text

	return (arrow == set.above || arrow == set.below) && gap == " " && len(digit) == 1 &&
		digit[0] >= '0' && digit[0] <= '9'
}

// trim moves the bounds of the content in over blanks and over a rule glyph.
//
// Parameters:
//   - rule: glyph to skip as well as the blanks, empty for none
func (g *gridRow) trim(rule string) {
	skip := func(c gridCell) bool {
		return strings.TrimSpace(c.text) == "" || (rule != "" && c.text == rule)
	}

	for g.from < g.to && skip(g.cells[g.from]) {
		g.from++
	}

	for g.to > g.from && skip(g.cells[g.to-1]) {
		g.to--
	}
}

// selRows works out what a selection takes of each line.
//
// Parameters:
//   - plain: the lines of the frame without styles
//   - a: one end of the selection
//   - b: the other end, which may come first in reading order
//
// Returns one row per line from the first to the last. The selection is linear: the first line
// is taken from its cell, the last up to its cell, included, and the lines between whole. A
// cluster is taken whole as soon as one of its cells is, so that a wide character or an emoji is
// never split, and only the content of a line is taken.
func (e *env) selRows(plain []string, a, b cellPos) []selRow {
	if b.y < a.y || (b.y == a.y && b.x < a.x) {
		a, b = b, a
	}

	var out []selRow

	for y := a.y; y <= b.y && y < len(plain); y++ {
		from, to := 0, e.Cols
		if y == a.y {
			from = a.x
		}

		if y == b.y {
			to = b.x + 1
		}

		out = append(out, e.selRow(y, e.gridOf(plain[y]), from, to))
	}

	return out
}

// selRow takes the content of a line that lies between two cells.
//
// Parameters:
//   - y: the line
//   - row: the line cut into clusters
//   - from: first cell asked for
//   - to: first cell not asked for
func (e *env) selRow(y int, row gridRow, from, to int) selRow {
	out := selRow{y: y, empty: true}
	if row.from >= row.to {
		return out
	}

	out.lo = row.cells[row.from].x

	first, last := -1, -1

	for i := row.from; i < row.to; i++ {
		c := row.cells[i]
		if c.x < to && c.x+c.w > from {
			if first < 0 {
				first = i
			}

			last = i
		}
	}

	if first < 0 {
		return out
	}

	var b strings.Builder

	for i := first; i <= last; i++ {
		b.WriteString(row.cells[i].text)
	}

	out.x0 = row.cells[first].x
	out.x1 = row.cells[last].x + row.cells[last].w
	out.natural = first == row.from && !row.rule
	out.text = b.String()
	out.empty = false

	return out
}

// selectionText joins the selected rows into the text to copy.
//
// Parameters:
//   - rows: what the selection takes of each line
//
// Returns the lines joined by newlines. The trailing blanks of each line are cut, and so are the
// empty lines at both ends. A line that is selected from the start of its content keeps its
// indentation relative to the least indented of those lines, so that the common margin of a page
// or of a box is dropped and the columns stay aligned; a line cut in the middle starts at its
// first character. Any character that is not safe to put in a command line, which a frame never
// holds, is replaced as sanitize does.
func selectionText(rows []selRow) string {
	base := -1

	for _, r := range rows {
		if !r.empty && r.natural && (base < 0 || r.lo < base) {
			base = r.lo
		}
	}

	lines := make([]string, len(rows))

	for i, r := range rows {
		if r.empty {
			continue
		}

		text := strings.TrimRight(sanitize(r.text), " ")
		if r.natural {
			text = spaces(r.lo-base) + text
		}

		lines[i] = text
	}

	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// highlight draws a span of a styled line in the highlight style.
//
// Parameters:
//   - line: styled line
//   - row: the cells to highlight and their text
//
// Returns the line with the span in reverse video and the rest as it was. The span loses its own
// styles, so that the highlight is one color whatever was under it.
func (e *env) highlight(line string, row selRow) string {
	m := e.Metric.method
	before := m.Truncate(line, row.x0, "")
	after := m.TruncateLeft(line, row.x1, "")

	return before + ansi.ResetStyle + e.Theme.highlight(row.text) + ansi.ResetStyle + after
}

// orderCells puts two cells in reading order.
//
// Parameters:
//   - a: first cell
//   - b: second cell
func orderCells(a, b cellPos) (cellPos, cellPos) {
	if b.y < a.y || (b.y == a.y && b.x < a.x) {
		return b, a
	}

	return a, b
}

// pressAt starts a selection at a press of the left button. It forgets the previous one.
//
// Parameters:
//   - x: column of the press
//   - y: row of the press
func (r *root) pressAt(x, y int) {
	r.sel = selection{held: true, press: r.clampCell(x, y), origin: cellPos{x: x, y: y}}
}

// dragTo follows the pointer while the left button is held. The first move to another cell turns
// the press into a selection, and the first selection of the session tells how to select with
// the terminal itself.
//
// Parameters:
//   - m: the motion
func (r *root) dragTo(m tea.MouseMotionMsg) tea.Cmd {
	s := &r.sel
	if !s.held || m.Button != tea.MouseLeft {
		return nil
	}

	if !r.mouseReady() {
		r.sel = selection{}

		return nil
	}

	p := r.clampCell(m.X, m.Y)

	if s.drag {
		s.focus = p

		return nil
	}

	if p == s.press {
		return nil
	}

	f := r.composeFrame(false)
	r.clicked = clickMark{}
	s.drag, s.anchor, s.focus = true, s.press, p
	s.frozen, s.status = plainLines(f.lines), f.status

	if r.hinted {
		return nil
	}

	r.hinted, r.hintPending = true, true

	return r.say(r.e.t("ui_sel_hint"), false)
}

// dropAt ends the hold of the button. A release on the cell of the press, with no move between, is
// a click, see clickAt. A selection that took some text copies it.
//
// Parameters:
//   - m: the release
func (r *root) dropAt(m tea.MouseReleaseMsg) tea.Cmd {
	s := &r.sel
	if !s.held {
		return nil
	}

	s.held = false
	if !s.drag {
		if r.mouseReady() && r.clampCell(m.X, m.Y) == s.press {
			return r.clickAt(s.origin.x, s.origin.y)
		}

		return nil
	}

	if !r.mouseReady() {
		r.sel = selection{}

		return nil
	}

	s.focus = r.clampCell(m.X, m.Y)

	rows, ok := r.selectedRows(plainLines(r.composeFrame(false).lines))

	text := ""
	if ok {
		text = selectionText(rows)
	}

	if text == "" {
		r.sel = selection{}

		return nil
	}

	return r.copySelection(text)
}

// lostRelease ends the hold of the button when the terminal never reported its release, which
// happens when it is let go outside of the window. A press that made no selection is dropped, and
// is no click, since the release was not on the cell of the press; a selection is ended where the
// pointer was last seen, and copies what it took, as a release there would.
//
// Returns the command that copies the selection, or nil.
func (r *root) lostRelease() tea.Cmd {
	s := &r.sel
	if !s.drag {
		s.held = false

		return nil
	}

	return r.dropAt(tea.MouseReleaseMsg{X: s.focus.x, Y: s.focus.y, Button: tea.MouseLeft})
}

// selectedRows works out what the selection takes of a frame.
//
// Parameters:
//   - plain: the lines of the frame without styles
//
// Returns the rows, and false when the frame is not the one the selection was made on: a line
// between its ends has other text, the status line left aside.
func (r *root) selectedRows(plain []string) ([]selRow, bool) {
	s := r.sel
	a, b := orderCells(s.anchor, s.focus)

	if b.y >= len(plain) || b.y >= len(s.frozen) {
		return nil, false
	}

	for y := a.y; y <= b.y; y++ {
		if y != s.status && plain[y] != s.frozen[y] {
			return nil, false
		}
	}

	return r.e.selRows(plain, a, b), true
}

// paintSelection highlights the selection in the lines of a frame. A selection whose frame is
// gone, or that the setting or the terminal can no longer serve, is forgotten instead.
//
// Parameters:
//   - lines: styled lines of the frame, changed in place
func (r *root) paintSelection(lines []string) []string {
	if !r.sel.drag {
		return lines
	}

	if !r.mouseOn() || r.e.Layout == layoutTiny {
		r.sel = selection{}

		return lines
	}

	plain := make([]string, len(lines))
	a, b := orderCells(r.sel.anchor, r.sel.focus)

	for y := a.y; y <= b.y && y < len(lines); y++ {
		plain[y] = ansi.Strip(lines[y])
	}

	rows, ok := r.selectedRows(plain)
	if !ok {
		r.sel = selection{}

		return lines
	}

	for _, row := range rows {
		if !row.empty && row.y < len(lines) {
			lines[row.y] = r.e.highlight(lines[row.y], row)
		}
	}

	return lines
}

// forgetSelection drops the selection and the hold of the button.
//
// Returns true when there was a highlight to remove.
func (r *root) forgetSelection() bool {
	had := r.sel.drag
	r.sel = selection{}

	return had
}
