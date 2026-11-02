package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// health is the role that colors a cell: the state of a plugin is drawn in the role of its health.
type health int

const (
	// healthNone leaves the cell in the color of the text.
	healthNone health = iota

	// healthOK is the role of what works.
	healthOK

	// healthWarn is the role of what needs attention.
	healthWarn

	// healthErr is the role of what is broken.
	healthErr

	// healthMuted is the role of what the user turned off.
	healthMuted
)

// cell is one column of a row: its text, its width in cells, its alignment, whether it is drawn
// with the secondary color, whether it is a column heading, drawn in bold with the secondary
// color, whether it is drawn in bold, such as the name of a setting, and whether a text that is too
// long is cut at its start instead of its end, such as a path, whose last folders are the ones
// that tell it from the others. Health colors the cell with a role, in the rows that are not
// selected.
type cell struct {
	text    string
	w       int
	right   bool
	muted   bool
	head    bool
	bold    bool
	keepEnd bool
	health  health
}

// columnGap is the number of spaces between two columns.
const columnGap = 2

// renderRow draws one row of a list or a menu.
//
// Parameters:
//   - e: shared environment
//   - selected: true for the row under the cursor, drawn with the selection style
//   - flagged: true for a record that cannot be used, marked with the warning glyph
//   - cells: columns of the row; the text is plain and is cut with an ellipsis or padded to
//     the width of its cell
//   - w: width of the row in cells
//
// Returns a line of at most w cells. The selection starts at the left edge of the row, the blank
// cells in front of the marker included as in the mockups, and never paints the last two cells
// of the line: they stay unstyled, so that a terminal that counts a wide character differently
// from the renderer cannot wrap the selected line and hide its last character.
func (e *env) renderRow(selected, flagged bool, cells []cell, w int) string {
	return e.drawRow(selected, flagged, cells, w, spareColumns)
}

// renderBoxRow draws one row of a list inside a box, such as the palette or a list of choices.
// It is renderRow without the unpainted cells at the right: a box never touches the edge of the
// terminal, so nothing can wrap, and the selection runs from one side of the box to the other as
// in the mockups.
//
// Parameters:
//   - e: shared environment
//   - selected: true for the row under the cursor, drawn with the selection style
//   - flagged: true for a record that cannot be used, marked with the warning glyph
//   - cells: columns of the row
//   - w: width of the row in cells
func (e *env) renderBoxRow(selected, flagged bool, cells []cell, w int) string {
	return e.drawRow(selected, flagged, cells, w, 0)
}

// drawRow draws a row for renderRow and renderBoxRow.
//
// Parameters:
//   - e: shared environment
//   - selected: true for the row under the cursor
//   - flagged: true for a record that cannot be used
//   - cells: columns of the row
//   - w: width of the row in cells
//   - spare: cells at the right that the selection leaves unpainted
func (e *env) drawRow(selected, flagged bool, cells []cell, w, spare int) string {
	g, th := e.Glyphs, e.Theme
	lead := e.lead()
	w -= e.indent()
	inner := max(0, w-spare)
	marker := "  "

	switch {
	case selected:
		marker = g.marker + " "
	case flagged:
		marker = g.warn + " "
	}

	if selected {
		body := e.Metric.pad(lead+marker+e.joinCells(cells, false), e.indent()+inner,
			g.ellipsis)

		return th.Selected.Render(body) + spaces(w-inner)
	}

	mk := th.Text.Render(marker)
	if flagged {
		mk = th.Err.Render(marker)
	}

	return lead + mk + e.clip(e.joinCells(cells, true), max(0, inner-2))
}

// joinCells lays out the cells of a row side by side.
//
// Parameters:
//   - cells: columns of the row
//   - styled: true to style each cell, false for plain text
func (e *env) joinCells(cells []cell, styled bool) string {
	var b strings.Builder

	for i, c := range cells {
		if i > 0 {
			b.WriteString(spaces(columnGap))
		}

		text := e.Metric.fit(c.text, c.w, e.Glyphs.ellipsis)
		if c.keepEnd {
			text = e.Metric.fitEnd(c.text, c.w, e.Glyphs.ellipsis)
		}

		gap := spaces(c.w - e.Metric.width(text))

		if c.right {
			text = gap + text
		} else {
			text += gap
		}

		switch {
		case !styled:
			b.WriteString(text)
		case c.head:
			b.WriteString(e.Theme.Heading.Render(text))
		case c.health != healthNone:
			b.WriteString(e.healthStyle(c.health).Render(text))
		case c.muted:
			b.WriteString(e.Theme.Muted.Render(text))
		case c.bold:
			b.WriteString(e.Theme.Bold.Render(text))
		default:
			b.WriteString(e.Theme.Text.Render(text))
		}
	}

	return b.String()
}

// columnWidths decides how many columns fit and how wide each one is.
//
// Parameters:
//   - natural: width each column would like, the most important column first
//   - avail: cells available for the columns and the gaps between them
//
// Returns the widths of the columns that are kept. Columns are dropped from the end until the
// rest fits; the last kept column may shrink to 12 cells, or to its natural width when that is
// smaller, and the first column is cut when it alone is too wide. When every column has its
// natural width and room is left, the room is shared between the columns, see growColumns. The
// last column, the description, then takes whatever is still left, a few cells of rounding on a
// narrow page and most of the room on a very wide one where the other columns have reached twice
// their natural width and are never stretched further, so that the table always fills the page
// and a long description is shown whole.
func columnWidths(natural []int, avail int) []int {
	for k := len(natural); k >= 1; k-- {
		fixed := columnGap * (k - 1)
		for _, w := range natural[:k-1] {
			fixed += w
		}

		room := avail - fixed
		last := natural[k-1]

		if k == 1 {
			return []int{max(0, min(last, avail))}
		}

		if room >= min(last, 12) {
			out := append([]int(nil), natural[:k-1]...)
			out = append(out, min(last, room))

			if room > last {
				growColumns(out, room-last)
				out[k-1] += avail - columnGap*(k-1) - totalWidth(out)
			}

			return out
		}
	}

	return nil
}

// growColumns shares spare room between columns, so that a wide terminal is not left with a
// narrow table and an empty right half.
//
// Parameters:
//   - widths: widths of the columns, changed in place
//   - spare: cells to share; each column takes a part proportional to its width and never grows
//     beyond twice its width
func growColumns(widths []int, spare int) {
	total := totalWidth(widths)
	if total == 0 {
		return
	}

	orig := append([]int(nil), widths...)
	given := 0

	for i, w := range orig {
		extra := min(spare*w/total, w)
		widths[i] += extra
		given += extra
	}

	for i := 0; given < spare && i < len(widths); i++ {
		if widths[i] > 0 && widths[i] < 2*orig[i] {
			widths[i]++
			given++
		}
	}
}

// totalWidth adds up the widths of columns, the gaps between them not included.
//
// Parameters:
//   - widths: widths to add
func totalWidth(widths []int) int {
	total := 0
	for _, w := range widths {
		total += w
	}

	return total
}

// healthStyle returns the style of a role.
//
// Parameters:
//   - t: role of a cell
func (e *env) healthStyle(t health) lipgloss.Style {
	switch t {
	case healthOK:
		return e.Theme.OK
	case healthWarn:
		return e.Theme.Warn
	case healthErr:
		return e.Theme.Err
	case healthMuted:
		return e.Theme.Muted
	}

	return e.Theme.Text
}
