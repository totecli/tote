package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

const (
	// paletteWidth is the widest the palette box is drawn.
	paletteWidth = 76

	// paletteHeight is the height of the palette box when the screen is tall enough.
	paletteHeight = 16

	// minSubWidth is the fewest cells of the target, path or command that a row of the palette
	// shows at its right; below it the detail is left out.
	minSubWidth = 8

	// paletteChrome is the number of box lines that are not result rows: two borders, the line
	// of the query and the rule under it.
	paletteChrome = 4
)

// scopeLabelKeys are the catalog keys of the names shown at the right of the query when a
// prefix narrows the search.
var scopeLabelKeys = map[scope]string{
	scopeConn: "ui_short_connections", scopeRepo: "ui_short_repos",
	scopeAlias: "ui_short_aliases", scopeFunc: "ui_short_funcs",
	scopeAction: "ui_pal_scope_actions",
}

// paletteOverlay is the command palette, opened with Ctrl-K from any screen.
//
// Entries are built once, when the palette opens. Flt is what the user typed, which alone drives
// the search: Sc is the scope that its prefix chose and Hits the indexes of the entries that
// match, best first. Sel indexes Hits and Off is the first visible row. Preview is true while the
// input line shows the label of the selected entry instead of what was typed: the arrow keys and
// a click turn it on, Tab keeps the shown text as the new query, Esc (once), a typed character
// and the other editing keys turn it off and show Flt again. The list is never filtered by the
// preview.
type paletteOverlay struct {
	entries []entry
	built   bool
	flt     filter
	sc      scope
	hits    []int
	sel     int
	off     int
	preview bool
}

// newPalette returns the overlay of the command palette, opened with Ctrl-K from any screen.
//
// It searches the connections, repositories, aliases and functions and a few actions. The
// prefixes "c ", "r ", "a " and "d " narrow the search to one kind and ">" to the actions. The
// arrow keys move the selection and show its name in the input line, Tab keeps that name as the
// query, Enter runs the selected entry like the list would, Ctrl-E edits it, and Esc puts the
// typed text back before it closes the palette.
func newPalette() overlay {
	return &paletteOverlay{}
}

// Init implements overlay: it builds the entries.
//
// Parameters:
//   - e: shared environment
func (p *paletteOverlay) Init(e *env) tea.Cmd {
	p.ensure(e)

	return nil
}

// ensure builds the entries the first time they are needed.
//
// Parameters:
//   - e: shared environment
func (p *paletteOverlay) ensure(e *env) {
	if p.built {
		return
	}

	p.built = true
	p.entries = buildEntries(e)
	p.refilter()
}

// refilter recomputes the hits from the query and puts the cursor on the best one.
func (p *paletteOverlay) refilter() {
	var rest string

	p.sc, rest = parseScope(p.flt.String())
	p.hits = rank(p.entries, p.sc, rest)
	p.sel, p.off = 0, 0
}

// paletteTight tells whether a box is too short to keep the line between the query and the
// results: it would leave only one result row.
//
// Parameters:
//   - maxH: most lines the box may take
func paletteTight(maxH int) bool {
	return min(maxH, paletteHeight) < paletteChrome+2
}

// rowsFor returns the number of result rows of a box.
//
// Parameters:
//   - maxH: most lines the box may take; a box shorter than paletteChrome+2 lines gives up the
//     line between the query and the results
func rowsFor(maxH int) int {
	chrome := paletteChrome
	if paletteTight(maxH) {
		chrome--
	}

	return max(1, min(maxH, paletteHeight)-chrome)
}

// selected returns the entry under the cursor, nil when nothing matches.
func (p *paletteOverlay) selected() *entry {
	if p.sel < 0 || p.sel >= len(p.hits) {
		return nil
	}

	return &p.entries[p.hits[p.sel]]
}

// move changes the selection and scrolls to keep it visible.
//
// Parameters:
//   - e: shared environment, which gives the size of the box
//   - to: new selected row, brought back into range
func (p *paletteOverlay) move(e *env, to int) {
	p.sel = max(0, min(to, len(p.hits)-1))
	p.off = scrollTo(p.sel, p.off, rowsFor(e.Height-2), len(p.hits))
}

// Update implements overlay.
//
// Parameters:
//   - e: shared environment
//   - msg: message to handle
func (p *paletteOverlay) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	p.ensure(e)

	switch m := msg.(type) {
	case dismissMsg:
		return p, nil, true
	case tea.PasteMsg:
		p.edit(func() { p.flt.insert(m.Content) })
	case itemClickMsg:
		rows := rowsFor(e.Height - 2)

		if by, ok := pageClick(m.ID, pageOf(p.off, rows, len(p.hits))); ok {
			p.sel, p.off = scrollRows(p.sel, p.off, rows, len(p.hits), by)

			return p, nil, false
		}

		if m.ID < 0 || m.ID >= len(p.hits) {
			return p, nil, false
		}

		p.step(e, m.ID)

		if m.Double {
			return p.run(e, false)
		}
	case wheelMsg:
		p.sel, p.off = scrollRows(p.sel, p.off, rowsFor(e.Height-2), len(p.hits), m.Lines)
	case tea.KeyPressMsg:
		return p.pressed(e, m)
	}

	return p, nil, false
}

// pressed handles a key press.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (p *paletteOverlay) pressed(e *env, k tea.KeyPressMsg) (overlay, tea.Cmd, bool) {
	rows := rowsFor(e.Height - 2)

	switch k.String() {
	case "esc":
		if p.preview {
			p.preview = false

			return p, nil, false
		}

		return p, nil, true
	case "ctrl+k":
		return p, nil, true
	case "enter":
		return p.run(e, false)
	case "ctrl+e":
		return p.run(e, true)
	case "tab":
		p.complete()
	case "up", "ctrl+p":
		p.step(e, p.sel-1)
	case "down", "ctrl+n":
		p.step(e, p.sel+1)
	case "pgup":
		p.step(e, p.sel-rows)
	case "pgdown":
		p.step(e, p.sel+rows)
	case "home":
		p.step(e, 0)
	case "end":
		p.step(e, len(p.hits)-1)
	case "backspace", "ctrl+h":
		p.edit(p.flt.backspace)
	case "ctrl+u":
		p.edit(func() { p.flt.text = nil })
	case "ctrl+w":
		p.edit(p.flt.deleteWord)
	default:
		if t := k.Key().Text; t != "" {
			p.edit(func() { p.flt.insert(t) })
		}
	}

	return p, nil, false
}

// edit changes what the user typed. A preview in progress is dropped first, so that the change
// applies to the typed text and not to the name that was shown, and the search starts again.
//
// Parameters:
//   - change: edits the filter
func (p *paletteOverlay) edit(change func()) {
	p.preview = false
	change()
	p.refilter()
}

// step moves the selection to a row and shows its name in the input line.
//
// Parameters:
//   - e: shared environment
//   - to: new selected row, brought back into range
func (p *paletteOverlay) step(e *env, to int) {
	p.move(e, to)
	p.preview = p.selected() != nil
}

// complete keeps the name of the selected entry as the query, which searches again: the typed
// text is replaced by the name, behind the scope prefix when there is one. Nothing happens when
// no entry is selected.
func (p *paletteOverlay) complete() {
	en := p.selected()
	if en == nil {
		return
	}

	p.edit(func() { p.flt.text = []rune(scopePrefix(p.flt.String()) + en.Name) })
}

// inputText returns the text the input line shows: the name of the selected entry behind the
// scope prefix while previewing, else what was typed.
func (p *paletteOverlay) inputText() string {
	if en := p.selected(); p.preview && en != nil {
		return scopePrefix(p.flt.String()) + en.Name
	}

	return p.flt.String()
}

// run closes the palette and sends what the selected entry means.
//
// Parameters:
//   - e: shared environment, for the hand-off of a plugin command
//   - edit: true to edit the record instead of using it; an action cannot be edited
//
// Returns the palette, the command that delivers the message, and true to close it. When
// nothing is selected, or the entry cannot be edited, the palette stays open.
func (p *paletteOverlay) run(e *env, edit bool) (overlay, tea.Cmd, bool) {
	en := p.selected()
	if en == nil || (edit && en.Scope == scopeAction) {
		return p, nil, false
	}

	switch {
	case en.Scope == scopeAction:
		if _, plug := en.Msg.(pluginStartMsg); plug {
			return p, e.handoff(en.Msg), true
		}

		return p, msgCmd(en.Msg), true
	case edit:
		return p, msgCmd(editMsg{Kind: en.Kind, Key: en.Key}), true
	}

	return p, msgCmd(activateMsg{Kind: en.Kind, Key: en.Key}), true
}

// Status implements statuser: the status line shows the command, path or branch of the entry
// under the cursor.
//
// Parameters:
//   - e: shared environment
func (p *paletteOverlay) Status(e *env) statusInfo {
	en := p.selected()
	if en == nil || en.Item == nil {
		return statusInfo{}
	}

	return statusFor(e, en.Item, false, "")
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (p *paletteOverlay) Keys(e *env) []key.Binding {
	en := p.selected()

	return paletteBindings(e, en != nil, en != nil && en.Scope != scopeAction, p.preview)
}

// Typing reports that the palette reads text, so that "?" and the other keys of the screens
// belong to the query.
func (p *paletteOverlay) Typing() bool {
	return true
}

// paletteBindings lists the keys of the palette in the order of the footer: choose, open, close
// (or restore), complete and edit.
//
// Parameters:
//   - e: shared environment
//   - canRun: true when an entry is selected, which Enter runs and Tab keeps as the query
//   - canEdit: true when the selected entry is a record, which can be edited
//   - previewing: true while the input line shows the selected entry; Esc then puts the typed
//     text back instead of closing the palette
func paletteBindings(e *env, canRun, canEdit, previewing bool) []key.Binding {
	choose := key.NewBinding(key.WithKeys("up", "down", "ctrl+p", "ctrl+n"),
		key.WithHelp(e.Glyphs.upDown, e.t("ui_help_choose")))
	run := key.NewBinding(key.WithKeys("enter"), key.WithHelp(e.Glyphs.enter,
		e.t("ui_help_open")))
	complete := key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", e.t("ui_help_complete")))
	edit := key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", e.t("ui_help_edit")))
	leave := e.Keys.Close

	if previewing {
		leave = restoreBinding(e)
	}

	run.SetEnabled(canRun)
	complete.SetEnabled(canRun)
	edit.SetEnabled(canEdit)

	return []key.Binding{choose, run, leave, complete, edit}
}

// restoreBinding is the Esc of the palette while it previews an entry: it puts the typed text
// back and does not close the palette.
//
// Parameters:
//   - e: shared environment
func restoreBinding(e *env) key.Binding {
	return key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", e.t("ui_help_restore")))
}

// View implements overlay.
//
// The box has the accent border and a bold title set in it, the query on the first line, a rule
// that joins the sides of the box, then the rows.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (p *paletteOverlay) View(e *env, maxW, maxH int) []string {
	box, _ := p.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: the visible results can be clicked.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (p *paletteOverlay) Zones(e *env, maxW, maxH int) []zone {
	_, zs := p.draw(e, maxW, maxH)

	return zs
}

// draw lays out the box of the palette.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
//
// Returns the lines of the box and the zones of the results, counted from its top left corner.
func (p *paletteOverlay) draw(e *env, maxW, maxH int) ([]string, []zone) {
	p.ensure(e)

	w := max(4, min(maxW, paletteWidth))
	inner := w - 2
	rows := rowsFor(maxH)

	lines := []string{p.inputLine(e, inner)}
	if !paletteTight(maxH) {
		lines = append(lines, "")
	}

	body, zs := p.rowLines(e, inner, rows)
	zs = shiftZones(zs, 1+len(lines))
	lines = append(lines, body...)

	box := floating(e, e.Theme.Bold.Render(e.t("ui_pal_title")), lines, w)
	g := e.Glyphs

	if !paletteTight(maxH) {
		box[2] = e.Theme.Accent.Render(g.ml + strings.Repeat(g.h, inner) + g.mr)
	}

	box = box[:min(len(box), max(1, maxH))]

	return box, clipZones(zs, len(box))
}

// inputLine draws the query with its cursor, or a hint while it is empty, and the name of the
// scope at the right when a prefix narrowed the search.
//
// Parameters:
//   - e: shared environment
//   - inner: cells inside the box
func (p *paletteOverlay) inputLine(e *env, inner int) string {
	g, th := e.Glyphs, e.Theme
	head := " " + th.Bold.Render(g.marker) + " "
	room := inner - 3 - 1

	label := ""
	if p.sc != scopeAll {
		label = sanitize(e.t(scopeLabelKeys[p.sc]))
		label = e.Metric.fit(label, max(0, room/3), g.ellipsis)
	}

	lw := e.Metric.width(label)
	if lw > 0 {
		room -= lw + 1
	}

	body := ""

	if text := p.inputText(); text == "" {
		body = th.Muted.Render(e.Metric.fit(e.t("ui_pal_hint"), max(1, room), g.ellipsis))
	} else {
		body = th.Text.Render(e.Metric.fitEnd(text, max(1, room), g.ellipsis)) +
			th.Text.Render(g.cursor)
	}

	gap := inner - 3 - e.Metric.width(body) - lw - 1

	if lw == 0 {
		return head + body
	}

	return head + body + spaces(max(1, gap)) + th.Muted.Render(label)
}

// rowLines draws the visible results, or a message when nothing matches.
//
// Parameters:
//   - e: shared environment
//   - inner: cells inside the box
//   - rows: number of rows that fit
//
// Returns the lines and the zones of the results, counted from the first row inside the box;
// the message has none. A list longer than the rows has a muted "N more" line on its first or
// last row.
func (p *paletteOverlay) rowLines(e *env, inner, rows int) ([]string, []zone) {
	if len(p.hits) == 0 {
		text := e.t("ui_pal_empty")
		if _, rest := parseScope(p.flt.String()); strings.TrimSpace(rest) != "" {
			text = e.t("ui_no_results", strings.TrimSpace(sanitize(rest)))
		}

		out := make([]string, rows)
		out[0] = " " + e.Theme.Muted.Render(e.Metric.fit(text, inner-2, e.Glyphs.ellipsis))

		return out, nil
	}

	box := *e
	box.Layout = layoutCompact

	avail := inner - box.indent() - 2 - 1
	win := windowOf(scrollTo(p.sel, p.off, rows, len(p.hits)), rows, len(p.hits))
	items := make([]string, 0, win.count)

	for i := win.off; i < win.off+win.count; i++ {
		en := &p.entries[p.hits[i]]
		items = append(items, box.renderBoxRow(i == p.sel, en.Warn != "",
			rowCells(e, en, avail), inner))
	}

	out, zs := win.stack(e, items, spaces(box.indent()+2), 1, inner)

	return append(out, make([]string, rows-len(out))...), zs
}

// isPath reports whether a text reads as a path of the file system, from the root or the home
// folder.
//
// Parameters:
//   - s: text to look at
func isPath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~")
}

// rowCells lays out the cells of a row of the palette: the label, which reads "Category > name"
// for a record, and the target, path or command of the record muted and aligned at the right.
//
// Parameters:
//   - e: shared environment
//   - en: entry to draw
//   - avail: cells for the cells and the gap between them
//
// Returns one cell, or two when the label leaves room for at least minSubWidth cells of the
// detail. The label is never cut while it fits.
func rowCells(e *env, en *entry, avail int) []cell {
	label := en.Name
	if en.Cat != "" {
		label = en.Cat + " " + e.Glyphs.crumb + " " + en.Name
	}

	lw, sw := e.Metric.width(label), e.Metric.width(en.Sub)
	room := avail - lw - columnGap

	if en.Sub == "" || room < minSubWidth {
		return []cell{{text: label, w: max(1, avail), muted: en.Nav}}
	}

	sw = min(sw, room)

	return []cell{
		{text: label, w: avail - columnGap - sw, muted: en.Nav},
		{text: en.Sub, w: sw, right: true, muted: true, keepEnd: isPath(en.Sub)},
	}
}
