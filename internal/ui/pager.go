package ui

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/totecli/tote/internal/plugin"
)

const (
	// pagerWidth is the widest the box of the output of a program is drawn.
	pagerWidth = 120

	// pagerMaxLines is the most lines of output the pager keeps. A program that writes more is cut
	// there, as the registry cuts it at 8 MiB, and the pager says so.
	pagerMaxLines = 200000

	// pagerMaxLine is the most bytes the pager keeps of one line.
	pagerMaxLine = 32 << 10

	// pagerTab is the distance between two tab stops.
	pagerTab = 8

	// sgrReset is the sequence that ends every style.
	sgrReset = "\x1b[0m"
)

// pagerText is the output of a program as the pager holds it: the finished lines, the line that
// is still being written, and the notices that the run added, such as the truncation. Every chunk
// goes through plugin.FilterOutput again on its way in, so that a raw escape, an operating system
// command, a cursor move, a bidirectional override, a NUL or any other control character cannot
// reach the terminal even if the producer let one through; only the colors and styles of the
// whitelist survive, and only when Sgr is true.
//
// Width, Wrapped and Done are the cache of the lines wrapped for one width: a resize wraps them
// again, new lines are wrapped once.
type pagerText struct {
	sgr     bool
	lines   []string
	cur     string
	notes   []string
	cut     bool
	width   int
	wrapped []string
	done    int
	tail    []string
}

// newPagerText returns an empty pager text.
//
// Parameters:
//   - sgr: true to keep the colors and styles of the whitelist, false for the mono theme
func newPagerText(sgr bool) *pagerText {
	return &pagerText{sgr: sgr}
}

// add appends a piece of output: finished lines, and perhaps the start of the next one.
//
// Parameters:
//   - chunk: text the registry emitted, any bytes
func (t *pagerText) add(chunk string) {
	chunk = plugin.FilterOutput(chunk, t.sgr)

	for chunk != "" {
		i := strings.IndexByte(chunk, '\n')
		if i < 0 {
			t.cur = t.extend(t.cur, chunk)

			return
		}

		t.cur = t.extend(t.cur, chunk[:i])
		chunk = chunk[i+1:]

		if len(t.lines) >= pagerMaxLines {
			t.cut, t.cur = true, ""

			continue
		}

		t.lines, t.cur = append(t.lines, t.cur), ""
	}
}

// extend adds text to a line up to pagerMaxLine bytes, cutting on a rune boundary, and records that
// the output was cut when bytes had to be left out.
//
// Parameters:
//   - line: the line so far
//   - more: text to add
func (t *pagerText) extend(line, more string) string {
	room := pagerMaxLine - len(line)
	if room <= 0 {
		t.cut = t.cut || more != ""

		return line
	}

	if len(more) > room {
		t.cut = true
		more = more[:room]
		for len(more) > 0 && !utf8.ValidString(more) {
			more = more[:len(more)-1]
		}
	}

	return line + more
}

// note adds a notice under the output, such as the truncation of a huge output.
//
// Parameters:
//   - text: the notice, plain
func (t *pagerText) note(text string) {
	t.notes = append(t.notes, text)
}

// fit works out the rows of the text for a width.
//
// Parameters:
//   - e: shared environment, for the metric
//   - w: cells per row
func (t *pagerText) fit(e *env, w int) {
	w = max(1, w)

	if w != t.width {
		t.width, t.wrapped, t.done = w, nil, 0
	}

	for ; t.done < len(t.lines); t.done++ {
		t.wrapped = append(t.wrapped, e.wrapSGR(t.lines[t.done], w)...)
	}

	t.tail = t.tail[:0]

	if t.cur != "" && !onlySGR(t.cur) {
		t.tail = append(t.tail, e.wrapSGR(t.cur, w)...)
	}
}

// onlySGR reports whether a text holds nothing but color sequences, such as the reset that the
// filter adds at the end of a chunk. The line that is still being written shows no row for it.
//
// Parameters:
//   - s: text of the filter
func onlySGR(s string) bool {
	for s != "" {
		n, _ := sgrAt(s)
		if n == 0 {
			return false
		}

		s = s[n:]
	}

	return true
}

// total returns the number of rows after fit, the notices not counted.
func (t *pagerText) total() int {
	return len(t.wrapped) + len(t.tail)
}

// row returns one row after fit.
//
// Parameters:
//   - i: index, from 0 to total-1
func (t *pagerText) row(i int) string {
	if i < len(t.wrapped) {
		return t.wrapped[i]
	}

	return t.tail[i-len(t.wrapped)]
}

// wrapSGR cuts a line of output into rows of at most w cells. The line holds plain text and the
// color sequences of the whitelist, nothing else: a style that is on at the end of a row is closed
// there and opened again at the start of the next, so that a row never leaks a style into the box
// around it. A tab becomes spaces up to the next stop, and every grapheme goes through sanitize,
// so that a character that the terminals measure differently is shown as a question mark.
//
// Parameters:
//   - line: one line of output without its line break
//   - w: cells per row; at least 1
//
// Returns at least one row.
func (e *env) wrapSGR(line string, w int) []string {
	w = max(1, w)

	var (
		rows  []string
		row   strings.Builder
		state sgrState
		used  int
	)

	end := func() {
		if state.on() {
			row.WriteString(sgrReset)
		}

		rows = append(rows, row.String())
		row.Reset()
		row.WriteString(state.seq())

		used = 0
	}

	for len(line) > 0 {
		if line[0] == 0x1b {
			n, params := sgrAt(line)
			if n == 0 {
				if used+1 > w {
					end()
				}

				row.WriteByte('?')
				used++

				line = line[1:]

				continue
			}

			row.WriteString(line[:n])
			state = state.apply(params)
			line = line[n:]

			continue
		}

		run := line
		if i := strings.IndexByte(line, 0x1b); i >= 0 {
			run = line[:i]
		}

		line = line[len(run):]

		for _, g := range e.clusters(expandTabs(run, &used, w)) {
			if used+g.w > w {
				end()
			}

			row.WriteString(g.text)

			used += g.w
		}
	}

	if used > 0 || len(rows) == 0 {
		if state.on() {
			row.WriteString(sgrReset)
		}

		rows = append(rows, row.String())
	}

	return rows
}

// cluster is a grapheme of the output and the cells it takes.
type cluster struct {
	text string
	w    int
}

// clusters splits plain text into graphemes that every terminal measures the same.
//
// Parameters:
//   - s: plain text without an escape
//
// Returns the graphemes of sanitize(s) with their width; a wide one never exceeds the row.
func (e *env) clusters(s string) []cluster {
	s = sanitize(s)

	var out []cluster

	for len(s) > 0 {
		c, cw := ansi.FirstGraphemeCluster(s, e.Metric.method)
		if c == "" {
			break
		}

		s = s[len(c):]
		out = append(out, cluster{text: c, w: cw})
	}

	return out
}

// expandTabs replaces the tabs of a run of text by spaces up to the next stop.
//
// Parameters:
//   - run: plain text, perhaps with tabs
//   - used: cells already taken on the row
//   - w: cells per row; a stop never goes past it
func expandTabs(run string, used *int, w int) string {
	if !strings.Contains(run, "\t") {
		return run
	}

	var b strings.Builder

	col := *used

	for _, r := range run {
		if r != '\t' {
			b.WriteRune(r)

			col++

			continue
		}

		n := pagerTab - col%pagerTab
		b.WriteString(strings.Repeat(" ", min(n, max(1, w))))

		col += n
	}

	return b.String()
}

// sgrAt reads a color sequence at the start of a text.
//
// Parameters:
//   - s: text that starts with ESC
//
// Returns its length and its parameters, or zero when the text does not start with "ESC [ params
// m" whose parameters are all in the whitelist of the registry.
func sgrAt(s string) (int, string) {
	if len(s) < 3 || s[1] != '[' {
		return 0, ""
	}

	for i := 2; i < len(s) && i < 64; i++ {
		c := s[i]

		switch {
		case c >= '0' && c <= '9', c == ';':
		case c == 'm':
			if plugin.FilterOutput(s[:i+1], true) == s[:i+1]+sgrReset {
				return i + 1, s[2:i]
			}

			return 0, ""
		default:
			return 0, ""
		}
	}

	return 0, ""
}

// sgrState is the style that is on at a point of a line: the four attributes of the whitelist and
// the color of the text. It is what a row opens again after a break, so that a line of thousands
// of sequences costs no more than one of a few.
type sgrState struct {
	bold      bool
	faint     bool
	italic    bool
	underline bool
	color     string
}

// on reports whether any style is on.
func (s sgrState) on() bool {
	return s.bold || s.faint || s.italic || s.underline || s.color != ""
}

// seq returns the one sequence that turns the style on again, empty when none is on.
func (s sgrState) seq() string {
	var parts []string

	for _, a := range []struct {
		on   bool
		code string
	}{{s.bold, "1"}, {s.faint, "2"}, {s.italic, "3"}, {s.underline, "4"}} {
		if a.on {
			parts = append(parts, a.code)
		}
	}

	if s.color != "" {
		parts = append(parts, s.color)
	}

	if len(parts) == 0 {
		return ""
	}

	return "\x1b[" + strings.Join(parts, ";") + "m"
}

// apply works out the style that is on after a color sequence.
//
// Parameters:
//   - params: its parameters, all of them in the whitelist of the registry
func (s sgrState) apply(params string) sgrState {
	for _, p := range strings.Split(params, ";") {
		switch p {
		case "", "0":
			s = sgrState{}
		case "1":
			s.bold = true
		case "2":
			s.faint = true
		case "3":
			s.italic = true
		case "4":
			s.underline = true
		case "22":
			s.bold, s.faint = false, false
		case "23":
			s.italic = false
		case "24":
			s.underline = false
		case "39":
			s.color = ""
		default:
			s.color = p
		}
	}

	return s
}

// pagerView is the scrolling part of a pager: the position in the rows of a pagerText.
//
// Off is the first row shown and Follow tells that the view sticks to the end of a text that is
// still growing; scrolling up lets go of it, and going back to the end takes it again. Rows and
// Total are recorded by the last draw for the scroll keys.
type pagerView struct {
	text   *pagerText
	off    int
	rows   int
	total  int
	follow bool
}

// last returns the largest first row.
func (p *pagerView) last() int {
	return maxOffset(p.rows, p.total)
}

// scroll applies a key to the position. It returns true when the key scrolls.
//
// Parameters:
//   - e: shared environment, for the key map
//   - k: key pressed
func (p *pagerView) scroll(e *env, k tea.KeyPressMsg) bool {
	page := max(1, p.rows-1)

	switch {
	case key.Matches(k, e.Keys.Up):
		p.move(-1)
	case key.Matches(k, e.Keys.Down):
		p.move(1)
	case key.Matches(k, e.Keys.PageUp):
		p.move(-page)
	case key.Matches(k, e.Keys.PageDown):
		p.move(page)
	case key.Matches(k, e.Keys.Home):
		p.off, p.follow = 0, false
	case key.Matches(k, e.Keys.End):
		p.off, p.follow = p.last(), true
	default:
		return false
	}

	return true
}

// move scrolls by a number of rows and lets go of, or takes again, the end of the text.
//
// Parameters:
//   - by: rows to move, negative to go up
func (p *pagerView) move(by int) {
	p.off = clampInt(p.off+by, 0, p.last())
	p.follow = p.off >= p.last()
}

// draw lays out the rows that fit in a box.
//
// Parameters:
//   - e: shared environment
//   - w: width of the box
//   - h: height of the box, borders included
//
// Returns the content lines, each one cell in from the border, and the zones of the "N more"
// lines, counted from the first content line.
func (p *pagerView) draw(e *env, w, h int) ([]string, []zone) {
	inner := w - 4
	p.text.fit(e, inner)

	notes := make([]string, 0, len(p.text.notes))
	for _, n := range p.text.notes {
		notes = append(notes, e.Metric.fit(sanitize(n), inner, e.Glyphs.ellipsis))
	}

	if p.text.cut {
		notes = append(notes, e.Metric.fit(e.t("ui_plug_pager_cut"), inner, e.Glyphs.ellipsis))
	}

	p.total = p.text.total() + len(notes)
	p.rows = max(1, h-2)

	if p.follow {
		p.off = p.last()
	}

	p.off = clampInt(p.off, 0, p.last())
	win := windowOf(p.off, p.rows, p.total)
	p.off = win.off

	items := make([]string, win.count)

	for i := range items {
		idx := win.off + i

		if idx < p.text.total() {
			items[i] = " " + e.clipPad(p.text.row(idx), inner)

			continue
		}

		items[i] = " " + e.Theme.Muted.Render(e.clipPad(notes[idx-p.text.total()], inner))
	}

	lines, zs := win.stack(e, items, " ", 1, w-2)

	return fitLines(lines, p.rows), onlyMore(zs)
}
