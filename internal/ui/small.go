package ui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	// smallFullHeight is the shortest terminal that gets the whole block of the too small screen.
	smallFullHeight = 6

	// smallFullWidth is the narrowest terminal that gets the whole block of the too small screen.
	// The block is for the terminals just under the minimum, which are narrow or short, so the
	// width is a little under minWidth: a narrow and tall terminal still gets the hint wrapped.
	smallFullWidth = 20

	// smallMidHeight is the shortest terminal that gets more than the message.
	smallMidHeight = 3

	// smallMidWidth is the narrowest terminal that gets more than the message.
	smallMidWidth = 12
)

// tooSmall draws the screen that asks for a bigger terminal.
//
// The block is centered on the whole terminal, not on the page, and is drawn again at each size.
// Above smallFullHeight rows and smallFullWidth columns it holds the brand line, the message, the
// size of the terminal against the size needed, and the hint to make the window bigger, wrapped
// on as many rows as the height leaves, the blank row above the hint being given up before the
// hint is cut. Down to
// smallMidHeight rows and smallMidWidth columns it keeps the message and the size line. Below
// that it is the message alone on the first row, cut to the width.
//
// Returns exactly Height lines, none wider than Cols.
func (e *env) tooSmall() []string {
	switch {
	case e.Height >= smallFullHeight && e.Cols >= smallFullWidth:
		block := []string{e.smallBrand(), "", e.smallTitle(), e.sizeLine()}

		// The blank row above the hint goes first when the hint would otherwise be cut.
		if hint, whole := e.smallHint(e.Height - len(block) - 1); whole {
			return e.centered(append(append(block, ""), hint...))
		}

		if hint, whole := e.smallHint(e.Height - len(block)); whole {
			return e.centered(append(block, hint...))
		}

		hint, _ := e.smallHint(1)

		return e.centered(append(append(block, ""), hint...))
	case e.Height >= smallMidHeight && e.Cols >= smallMidWidth:
		return e.centered([]string{e.smallTitle(), e.sizeLine()})
	}

	return fitLines([]string{e.Metric.fit(e.t("ui_too_small"), e.Cols, e.Glyphs.ellipsis)},
		e.Height)
}

// centered puts lines in the middle of the terminal.
//
// Parameters:
//   - lines: styled lines, each at most Cols cells wide; an empty line stays empty
//
// Returns Height lines where each line has its own left margin, half of the cells it leaves
// free, rounded down, and the block has the blank rows above it, half of the rows it leaves
// free, rounded down.
func (e *env) centered(lines []string) []string {
	out := make([]string, 0, e.Height)
	out = append(out, make([]string, max(0, (e.Height-len(lines))/2))...)

	for _, l := range lines {
		if l != "" {
			l = spaces((e.Cols-e.Metric.width(l))/2) + l
		}

		out = append(out, l)
	}

	return fitLines(out, e.Height)
}

// smallBrand draws the mark and the name of the program as the header of the lists does.
func (e *env) smallBrand() string {
	return e.Theme.Title.Render(e.Glyphs.prompt) + " " + e.Theme.gradient("tote")
}

// smallTitle draws the message that the terminal is too small, cut to the width.
func (e *env) smallTitle() string {
	text := e.Metric.fit(e.t("ui_too_small"), e.Cols, e.Glyphs.ellipsis)

	return e.Theme.Warn.Bold(true).Render(text)
}

// smallHint draws the advice to make the window bigger or to quit, with the key of the keymap.
//
// Parameters:
//   - room: rows left for the hint
//
// Returns the hint on one line when it fits the width, else wrapped at the spaces when the rows
// left allow it, and true; else the hint on one line cut with an ellipsis, and false.
func (e *env) smallHint(room int) ([]string, bool) {
	text := e.t("ui_too_small_hint", e.Keys.Quit.Help().Key)
	rows := e.wrap(text, e.Cols)
	whole := len(rows) <= room

	if !whole {
		rows = []string{e.Metric.fit(text, e.Cols, e.Glyphs.ellipsis)}
	}

	for i, r := range rows {
		rows[i] = e.Theme.Muted.Render(r)
	}

	return rows, whole
}

// wrap breaks a plain text into lines at its spaces.
//
// Parameters:
//   - text: plain text
//   - w: most cells in a line
//
// Returns the lines, each at most w cells wide. The text is broken greedily, then again at the
// narrowest width that gives the same number of lines, so that the lines are of about the same
// length and the last one is not a single word. A word wider than w is cut with an ellipsis on a
// line of its own.
func (e *env) wrap(text string, w int) []string {
	rows := e.greedy(text, w)
	if len(rows) < 2 {
		return rows
	}

	longest := 0
	for _, word := range strings.Fields(text) {
		longest = max(longest, e.Metric.width(word))
	}

	best := rows

	for ww := w - 1; ww >= longest; ww-- {
		next := e.greedy(text, ww)
		if len(next) > len(rows) {
			break
		}

		best = next
	}

	return best
}

// greedy breaks a plain text into lines at its spaces, filling each line before it starts the
// next.
//
// Parameters:
//   - text: plain text
//   - w: most cells in a line
func (e *env) greedy(text string, w int) []string {
	var rows []string

	line := ""

	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case e.Metric.width(line)+1+e.Metric.width(word) <= w:
			line += " " + word
		default:
			rows = append(rows, e.Metric.fit(line, w, e.Glyphs.ellipsis))
			line = word
		}
	}

	return append(rows, e.Metric.fit(line, w, e.Glyphs.ellipsis))
}

// sizeLine draws the size of the terminal against the size it needs.
//
// The dimension that is too small is flagged and the other one is in the text color; the sizes
// that are needed are muted. A line too wide for the terminal loses its words first, "needs"
// giving way to the arrow, then the needed size, so that the size of the terminal always shows.
func (e *env) sizeLine() string {
	th := e.Theme
	narrow, short := th.Text, th.Text

	if e.Cols < minWidth {
		narrow = th.flagged()
	}

	if e.Height < minHeight {
		short = th.flagged()
	}

	now := e.dims(e.Cols, e.Height, narrow, short, th.Text)
	need := e.dims(minWidth, minHeight, th.Muted, th.Muted, th.Muted)

	for _, link := range []string{e.t("ui_too_small_needs"), e.Glyphs.arrow} {
		line := e.t("ui_too_small_size", now, " "+th.Muted.Render(link), need)
		if e.Metric.width(line) <= e.Cols {
			return line
		}
	}

	return e.clip(now, e.Cols)
}

// dims draws a size as width, multiplication sign and height.
//
// Parameters:
//   - w: width in cells
//   - h: height in rows
//   - sw: style of the width
//   - sh: style of the height
//   - sx: style of the multiplication sign
func (e *env) dims(w, h int, sw, sh, sx lipgloss.Style) string {
	return sw.Render(strconv.Itoa(w)) + " " + sx.Render(e.Glyphs.times) + " " +
		sh.Render(strconv.Itoa(h))
}
