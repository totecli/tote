package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// minTitle is the fewest cells a title keeps when it is cut to fit the top border of a box; a title
// that would keep fewer is left out.
const minTitle = 4

// boxed draws a rounded box around lines.
//
// Parameters:
//   - e: shared environment, for the glyphs, the border style and the metric
//   - title: text set in the top border, already styled, empty for none; it is left out when it
//     does not fit
//   - lines: content lines, styled; each is cut or padded to the inside of the box
//   - w: total width of the box in cells, at least 4
//
// Returns the lines of the box, each exactly w cells wide.
func boxed(e *env, title string, lines []string, w int) []string {
	return boxWith(e, e.Theme.Border, title, lines, w)
}

// floating draws the rounded box of an overlay, which has the accent color for its border.
//
// Parameters:
//   - e: shared environment, for the glyphs, the border style and the metric
//   - title: text set in the top border, already styled, empty for none
//   - lines: content lines, styled
//   - w: total width of the box in cells, at least 4
//
// Returns the lines of the box, each exactly w cells wide.
func floating(e *env, title string, lines []string, w int) []string {
	return boxWith(e, e.Theme.Accent, title, lines, w)
}

// boxWith draws a rounded box around lines with a given border style.
//
// Parameters:
//   - e: shared environment, for the glyphs and the metric
//   - st: style of the border
//   - title: text set in the top border, already styled, empty for none; it is cut with an
//     ellipsis when it does not fit, and left out when fewer than minTitle cells of it would show
//   - lines: content lines, styled; each is cut or padded to the inside of the box
//   - w: total width of the box in cells, at least 4
//
// Returns the lines of the box, each exactly w cells wide.
func boxWith(e *env, st lipgloss.Style, title string, lines []string, w int) []string {
	w = max(w, 4)
	inner := w - 2
	g := e.Glyphs

	rest := inner - 1
	head := ""

	if tw := e.Metric.width(title); title != "" && tw > rest-3 && rest-3 >= minTitle {
		title = e.Metric.method.Truncate(title, rest-3, e.Glyphs.ellipsis)
	} else if title != "" && tw > rest-3 {
		title = ""
	}

	if title != "" {
		head = " " + title + " "
		rest -= e.Metric.width(title) + 2
	}

	out := make([]string, 0, len(lines)+2)
	out = append(out, st.Render(g.tl+g.h)+head+st.Render(strings.Repeat(g.h, rest)+g.tr))

	for _, l := range lines {
		out = append(out, st.Render(g.v)+e.clipPad(l, inner)+st.Render(g.v))
	}

	return append(out, st.Render(g.bl+strings.Repeat(g.h, inner)+g.br))
}

// clip cuts a styled line to a width.
//
// Parameters:
//   - s: styled text
//   - w: most cells to keep
func (e *env) clip(s string, w int) string {
	if e.Metric.width(s) <= w {
		return s
	}

	return e.Metric.method.Truncate(s, max(0, w), "")
}

// clipPad cuts or pads a styled line to exactly a width.
//
// Parameters:
//   - s: styled text
//   - w: cells wanted
func (e *env) clipPad(s string, w int) string {
	s = e.clip(s, w)

	return s + spaces(w-e.Metric.width(s))
}

// fitLines forces a slice of lines to an exact height.
//
// Parameters:
//   - lines: lines to adjust
//   - h: number of lines wanted; extra lines are dropped from the end and missing ones are
//     filled with empty lines
func fitLines(lines []string, h int) []string {
	if h <= 0 {
		return nil
	}

	if len(lines) >= h {
		return lines[:h]
	}

	return append(lines, make([]string, h-len(lines))...)
}
