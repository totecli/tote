package ui

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// emptyStyle matches a style that is set and reset at once, which a cut of a styled line leaves
// for every style of the part that was cut away.
var emptyStyle = regexp.MustCompile("\x1b\\[[0-9;:]+m(\x1b\\[m)")

// hoverState says what the pointer rests on. It is the one place that holds the hovered zone.
//
// Known is true while the position of the pointer is known and a hover is allowed: it becomes true
// at the first plain move of the pointer, or at a click, a release that ended no drag or a notch
// of the wheel, and false again when the terminal loses the focus, when a drag starts, when the
// mouse is off or a program owns the terminal. At is the cell of the pointer. Hits are the hits
// of the frame as it is now, valid while Fresh is true: they are drawn once after each change
// and not again for the moves of the pointer, so that a move costs a search and no drawing.
// Over is true when the pointer is on an element that reacts to the mouse and Cur is that hit,
// the same one that a click there would act on. Painting reads Over and Cur, nothing else.
type hoverState struct {
	known bool
	at    cellPos
	hits  []hit
	fresh bool
	over  bool
	cur   hit
}

// reset forgets the pointer.
//
// Returns true when an element was hovered, so that the frame has to be drawn again.
func (h *hoverState) reset() bool {
	had := h.over
	*h = hoverState{}

	return had
}

// hoverOf finds the element under a cell among the hits of a frame.
//
// Parameters:
//   - hits: hits in drawing order, see root.composeFrame
//   - at: the cell
//
// Returns the hit that is drawn on top at the cell, as hitAt does, and true when it is something
// to highlight: a hint of the footer that presses a key, or a zone of the screen or of the overlay
// that is an item. The "N more" lines scroll and are not items, so they are not highlighted.
func hoverOf(hits []hit, at cellPos) (hit, bool) {
	h, found := topHit(hits, at)

	return h, found && (h.press != "" || h.id >= 0)
}

// glide handles a move of the pointer with no button down, which is what the terminal reports in
// all-motion mode. It looks for the element under the pointer in the hits of the frame, which are
// drawn only when something changed since they were last drawn, and does nothing else: a move that
// stays on the same element changes no state and no frame, and the cached view stays. It never
// moves the selection, opens, runs or reads anything.
//
// Parameters:
//   - m: the motion
//
// Returns the command of the release that was lost, if there is one, and true when the message was
// dealt with here; a motion with a button down is a drag and is left to the caller. A terminal
// reports a move with a button down as a drag, so a plain move while the button was last seen down
// says that its release happened where the terminal could not report it, outside of the window: it
// is taken as the release, see lostRelease.
func (r *root) glide(m tea.MouseMotionMsg) (tea.Cmd, bool) {
	if m.Button != tea.MouseNone {
		return nil, false
	}

	var cmd tea.Cmd

	h := &r.hover

	if r.sel.held {
		cmd, r.shown = r.lostRelease(), nil
	}

	if !r.mouseReady() {
		if h.reset() {
			r.shown = nil
		}

		return cmd, true
	}

	at := cellPos{x: m.X, y: m.Y}
	if h.known && h.at == at {
		return cmd, true
	}

	if !h.fresh {
		h.hits, h.fresh = r.composeFrame(true).hits, true
	}

	was, had := h.cur, h.over
	h.known, h.at = true, at
	h.cur, h.over = hoverOf(h.hits, at)

	if h.over != had || h.cur != was {
		r.shown = nil
	}

	return cmd, true
}

// track notes where the pointer is when a message other than a motion says so: a click, a notch
// of the wheel, or the release of a button that ended no drag.
//
// Parameters:
//   - msg: the message that was just handled
func (r *root) track(msg tea.Msg) {
	var m tea.Mouse

	switch ev := msg.(type) {
	case tea.MouseClickMsg:
		m = ev.Mouse()
	case tea.MouseWheelMsg:
		m = ev.Mouse()
	case tea.MouseReleaseMsg:
		if r.sel.drag {
			return
		}

		m = ev.Mouse()
	default:
		return
	}

	r.hover.known, r.hover.at = true, cellPos{x: m.X, y: m.Y}
}

// rehover works out again what is under the pointer after a message changed something: a scroll,
// a resize, a new screen, an overlay that opened or closed, a key. The pointer did not move, but
// what is under it may have. Nothing is drawn while the pointer is not known, so a session that
// never uses the mouse pays nothing.
func (r *root) rehover() {
	h := &r.hover
	h.fresh = false

	if !h.known {
		return
	}

	if !r.mouseReady() {
		h.reset()

		return
	}

	h.hits, h.fresh = r.composeFrame(true).hits, true
	h.cur, h.over = hoverOf(h.hits, h.at)
}

// paintHover draws the hover look on the element under the pointer.
//
// Parameters:
//   - lines: styled lines of the frame, changed in place
func (r *root) paintHover(lines []string) []string {
	h := r.hover
	if !h.over || h.cur.y < 0 || h.cur.y >= len(lines) || !r.mouseReady() {
		return lines
	}

	e := r.e
	x1 := h.cur.x + h.cur.w

	if h.cur.press == "" && len(r.overlays) == 0 {
		x1 = min(x1, e.Margin+e.Width-spareColumns)
	}

	lines[h.cur.y] = e.hoverSpan(lines[h.cur.y], h.cur.x, x1)

	return lines
}

// hoverSpan draws the hover look on cells of a styled line.
//
// Parameters:
//   - line: styled line
//   - x0: first cell
//   - x1: first cell after the span
//
// Returns the line with the span in the hover look and the rest as it was. With a background (256
// colors, true color) the whole span is tinted, blanks included, so that a row shows as a band,
// and a line that is shorter than the span is filled with blanks as far as its end, as the selected
// row is. Without a background the text is underlined, each run of text on its own: two blanks or
// more end a run, so that the gap between two columns is not underlined. A span drawn as the
// selected row is left alone, and so is a line that the change would make wider or narrower.
func (e *env) hoverSpan(line string, x0, x1 int) string {
	th := e.Theme
	x1 = min(x1, e.Cols)

	if x1 <= x0 || len(th.Hover) == 0 {
		return line
	}

	orig := line

	if w := e.Metric.width(line); !th.underline && w < x1 {
		line += ansi.ResetStyle + spaces(x1-w)
	}

	mid := tidy(e.Metric.slice(line, x0, x1))
	if mid == "" || th.selected(mid) {
		return orig
	}

	spans := [][2]int{{x0, x1}}
	if th.underline {
		spans = e.textRuns(ansi.Strip(mid), x0)
	}

	if len(spans) == 0 {
		return orig
	}

	var b strings.Builder

	pos := 0

	for _, sp := range spans {
		if head := tidy(e.Metric.slice(line, pos, sp[0])); head != "" {
			b.WriteString(head)

			if styled(head) {
				b.WriteString(ansi.ResetStyle)
			}
		}

		b.WriteString(th.hover(tidy(e.Metric.slice(line, sp[0], sp[1]))))

		pos = sp[1]
	}

	b.WriteString(tidy(e.Metric.slice(line, pos, e.Cols)))

	if out := b.String(); e.Metric.width(out) == e.Metric.width(line) {
		return out
	}

	return orig
}

// textRuns finds the runs of text of a plain span: from its first character that is not a blank
// to its last, cut where two blanks or more follow one another.
//
// Parameters:
//   - plain: text without styles
//   - x0: column of the first cell of the text
//
// Returns the first cell of each run and the first cell after it, in columns of the line.
func (e *env) textRuns(plain string, x0 int) [][2]int {
	var (
		out        [][2]int
		start, end = -1, 0
	)

	for _, c := range e.gridOf(plain).cells {
		if strings.TrimSpace(c.text) == "" {
			continue
		}

		if start >= 0 && c.x-end >= 2 {
			out = append(out, [2]int{x0 + start, x0 + end})
			start = -1
		}

		if start < 0 {
			start = c.x
		}

		end = c.x + c.w
	}

	if start >= 0 {
		out = append(out, [2]int{x0 + start, x0 + end})
	}

	return out
}

// tidy removes what cutting a styled line leaves behind: every style of the part that was cut away
// is put again at the start of what stays, most of them closed at once, since nothing they style
// is there.
//
// Parameters:
//   - s: styled text from metric.slice
//
// Returns the text with the same look: a style that is closed at once is dropped, since the reset
// that closes it also ends what came before, a run of resets is one reset and a reset at the very
// start is dropped.
func tidy(s string) string {
	s = emptyStyle.ReplaceAllString(s, "$1")

	for strings.Contains(s, ansi.ResetStyle+ansi.ResetStyle) {
		s = strings.ReplaceAll(s, ansi.ResetStyle+ansi.ResetStyle, ansi.ResetStyle)
	}

	return strings.TrimPrefix(s, ansi.ResetStyle)
}

// styled reports whether a style is still on at the end of a styled text: the last sequence in it
// is not a reset.
//
// Parameters:
//   - s: styled text
func styled(s string) bool {
	i := strings.LastIndex(s, "\x1b[")

	return i >= 0 && !strings.HasPrefix(s[i:], ansi.ResetStyle) &&
		!strings.HasPrefix(s[i:], "\x1b[0m")
}
