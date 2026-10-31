package ui

const (
	// minMoreRows is the fewest rows a window needs to spend one of them on a "N more" line at
	// each end and still show two items. Below it the window shows items only: three items are
	// worth more than one item between two lines.
	minMoreRows = 4

	// zoneMoreUp is the identifier of the zone of the "N more" line above a window.
	zoneMoreUp = -1

	// zoneMoreDown is the identifier of the zone of the "N more" line below a window.
	zoneMoreDown = -2
)

// window is the part of a list that a block of rows shows.
//
// Off is the index of the first item shown and Count the number of items shown. Above and Below
// are the numbers of items hidden before and after them. Top and Bottom tell that a "N more" line
// takes the first or the last row of the block; the line is one of the rows, so the items and the
// lines together never take more rows than the block has. A line exists only when items are
// hidden on its side and the block has at least minMoreRows rows.
type window struct {
	off    int
	count  int
	above  int
	below  int
	top    bool
	bottom bool
}

// clampInt brings a number into a range.
//
// Parameters:
//   - v: number to bring back
//   - lo: smallest value allowed
//   - hi: largest value allowed; when it is below lo, lo wins
func clampInt(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// maxOffset returns the largest first item a window of rows rows can have: the one that shows
// the last item on the last row.
//
// Parameters:
//   - rows: rows of the block, "N more" lines included
//   - n: number of items
func maxOffset(rows, n int) int {
	rows = max(1, rows)

	switch {
	case n <= rows:
		return 0
	case rows < minMoreRows:
		return n - rows
	}

	return n - rows + 1
}

// windowOf works out what a block of rows shows of a list.
//
// Parameters:
//   - off: wanted first item, brought back into range
//   - rows: rows of the block, "N more" lines included
//   - n: number of items
//
// Returns the window. A list that fits shows everything and has no line. Otherwise the hidden
// items are counted by a line that takes a row of the block, at the top when some are above and at
// the bottom when some are below, so that the user always knows how much there is to see.
func windowOf(off, rows, n int) window {
	rows = max(1, rows)

	if n <= rows {
		return window{count: max(0, n)}
	}

	off = clampInt(off, 0, maxOffset(rows, n))

	if rows < minMoreRows {
		return window{off: off, count: rows, above: off, below: n - off - rows}
	}

	w := window{off: off, above: off, top: off > 0}
	room := rows

	if w.top {
		room--
	}

	if off+room < n {
		w.bottom = true
		room--
	}

	w.count = room
	w.below = n - off - room

	return w
}

// scrollTo keeps an item visible.
//
// Parameters:
//   - sel: selected item
//   - off: current first visible item
//   - rows: rows of the block, "N more" lines included
//   - n: number of items
//
// Returns the new first visible item, with the selected item on a row of its own and never on a
// "N more" line. The window never goes past the point where the last item is on the last row.
func scrollTo(sel, off, rows, n int) int {
	rows = max(1, rows)
	if n <= rows {
		return 0
	}

	sel = clampInt(sel, 0, n-1)
	last := maxOffset(rows, n)
	off = clampInt(off, 0, last)

	if sel < off {
		return sel
	}

	if w := windowOf(off, rows, n); sel < w.off+w.count {
		return off
	}

	// Start from the smallest first item that can show sel, and step over the end of a block
	// where the line below goes away.
	least := sel - rows + 2
	if rows < minMoreRows {
		least = sel - rows + 1
	}

	off = clampInt(max(off, least), 0, last)

	for off < last {
		if w := windowOf(off, rows, n); sel < w.off+w.count {
			break
		}

		off++
	}

	return off
}

// scrollRows scrolls a list by moving its window, and keeps the selected item inside it.
//
// Parameters:
//   - sel: selected item
//   - off: first visible item
//   - rows: rows of the block, "N more" lines included
//   - n: number of items
//   - lines: items to scroll by, negative to go up
//
// Returns the new selection and the new first visible item. The window never goes past the point
// where the last item is on the last row, and an empty list gives zeros.
func scrollRows(sel, off, rows, n, lines int) (int, int) {
	if n <= 0 {
		return 0, 0
	}

	rows = max(1, rows)
	off = scrollTo(sel, off, rows, n)
	off = clampInt(off+lines, 0, maxOffset(rows, n))
	w := windowOf(off, rows, n)
	sel = clampInt(sel, w.off, w.off+w.count-1)

	return min(sel, n-1), off
}

// pageOf returns the number of items that a page of a block of rows scrolls by: the items the
// window shows less one, so that the last item seen stays in view.
//
// Parameters:
//   - off: first visible item
//   - rows: rows of the block, "N more" lines included
//   - n: number of items
func pageOf(off, rows, n int) int {
	return max(1, windowOf(off, rows, n).count-1)
}

// moreLine draws the muted line that says how many items are hidden.
//
// Parameters:
//   - glyph: arrow that points the way, the "above" or the "below" glyph
//   - n: number of hidden items
func (e *env) moreLine(glyph string, n int) string {
	return e.Theme.Muted.Render(glyph + " " + e.t("ui_more", n))
}

// stack puts the "N more" lines around the drawn items of a window.
//
// Parameters:
//   - e: shared environment
//   - w: the window the items were drawn for
//   - items: the drawn items, w.count of them, from w.off
//   - lead: text in front of a "N more" line, so that it lines up with the text of the items
//   - x: first cell of the rows, for the zones
//   - width: width of the rows, for the zones
//
// Returns the lines and the zones: one per item, whose identifier is the index of the item, and
// one per "N more" line, whose identifier is zoneMoreUp or zoneMoreDown. The zones are counted
// from the first line returned.
func (w window) stack(e *env, items []string, lead string, x, width int) ([]string, []zone) {
	out := make([]string, 0, len(items)+2)
	zs := make([]zone, 0, len(items)+2)

	if w.top {
		out = append(out, lead+e.moreLine(e.Glyphs.above, w.above))
		zs = append(zs, zone{y: 0, x: x, w: width, id: zoneMoreUp})
	}

	for i := range items {
		zs = append(zs, zone{y: len(out) + i, x: x, w: width, id: w.off + i})
	}

	out = append(out, items...)

	if w.bottom {
		zs = append(zs, zone{y: len(out), x: x, w: width, id: zoneMoreDown})
		out = append(out, lead+e.moreLine(e.Glyphs.below, w.below))
	}

	return out, zs
}

// onlyMore keeps the zones of the "N more" lines, for a block whose items are not clickable.
//
// Parameters:
//   - zs: zones as stack returns them
func onlyMore(zs []zone) []zone {
	out := zs[:0]

	for _, z := range zs {
		if z.id < 0 {
			out = append(out, z)
		}
	}

	return out
}

// pageClick tells how far a click on a "N more" line scrolls.
//
// Parameters:
//   - id: identifier of the zone that was clicked
//   - page: number of items in a page
//
// Returns the number of items to scroll by, negative to go up, and true; or zero and false when
// the zone is not a "N more" line.
func pageClick(id, page int) (int, bool) {
	switch id {
	case zoneMoreUp:
		return -page, true
	case zoneMoreDown:
		return page, true
	}

	return 0, false
}
