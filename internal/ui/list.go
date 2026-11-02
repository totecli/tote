package ui

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// listScreen is the list of one kind with its filter, its selection and its detail panel.
//
// Items are all the records of the kind and Shown the indexes of those that match the filter,
// best match first. Sel indexes Shown and Off is the first visible row. Natural holds the
// preferred width of each column, measured once per load. ReadOnly is the sanitized reason the
// file cannot be written, empty when it can. Users holds, by connection key, the login that the
// keys "u" and "U" chose for the next connection instead of the stored one; it lives as long as
// the list.
type listScreen struct {
	kind     kind
	items    []item
	shown    []int
	sel      int
	off      int
	flt      filter
	natural  []int
	readOnly string
	users    map[string]string
	nav      tea.Msg
}

// newList builds the list of a kind from the current data.
//
// Parameters:
//   - e: shared environment
//   - k: kind to list
func newList(e *env, k kind) *listScreen {
	s := &listScreen{kind: k, users: map[string]string{}}
	s.load(e, "")

	return s
}

// load reads the items again and applies the filter.
//
// Parameters:
//   - e: shared environment
//   - selectKey: raw key to select afterwards; empty keeps the selected record when it is still
//     there, else the same position
func (s *listScreen) load(e *env, selectKey string) {
	keep := selectKey
	if keep == "" {
		if it := s.selected(); it != nil {
			keep = it.Key
		}
	}

	prev := s.sel
	s.items = e.Data.items(s.kind)
	s.readOnly = e.Data.readOnlyText(s.kind)
	s.measure(e)
	s.refilter()

	s.sel = max(0, min(prev, len(s.shown)-1))

	for i, idx := range s.shown {
		if keep != "" && s.items[idx].Key == keep {
			s.sel = i
		}
	}

	s.off = scrollTo(s.sel, s.off, s.rows(e), len(s.shown))
}

// measure computes the preferred width of every column.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) measure(e *env) {
	cols := s.kind.columns()
	s.natural = make([]int, len(cols))

	for i, c := range cols {
		w := e.Metric.width(e.t(c.head))

		for _, it := range s.items {
			if i < len(it.Cells) {
				w = max(w, e.Metric.width(it.Cells[i]))
			}
		}

		s.natural[i] = min(w, c.max)
	}
}

// refilter recomputes Shown from the filter text and puts the cursor on the best match.
func (s *listScreen) refilter() {
	s.shown = matchItems(s.items, s.flt.String())
	s.sel, s.off = 0, 0
}

// selected returns the item under the cursor, nil when the list is empty.
func (s *listScreen) selected() *item {
	if s.sel < 0 || s.sel >= len(s.shown) {
		return nil
	}

	return &s.items[s.shown[s.sel]]
}

// panelShown reports whether the detail panel is on screen.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) panelShown(e *env) bool {
	return e.Layout == layoutFull && e.Height >= panelHeight && s.selected() != nil
}

// headLines returns the number of lines above the rows.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) headLines(e *env) int {
	n := 2
	if e.Layout == layoutFull {
		n = 5
	}

	if s.readOnly != "" {
		n++
	}

	return n
}

// rows returns the number of rows that fit between the heading and the panel.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) rows(e *env) int {
	n := e.Height - 2 - s.headLines(e)
	if s.panelShown(e) {
		n -= panelRows
	}

	return max(1, n)
}

// Typing implements screen.
func (s *listScreen) Typing() bool {
	return s.flt.typing
}

// Reload implements screen.
//
// Parameters:
//   - e: shared environment
//   - selectKey: raw key to select, empty to keep the selection
func (s *listScreen) Reload(e *env, selectKey string) screen {
	s.load(e, selectKey)

	return s
}

// Status implements screen.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) Status(e *env) statusInfo {
	return statusFor(e, s.selected(), s.panelShown(e), s.userOf(s.selected()))
}

// userOf returns the login chosen for the next connection of an item.
//
// Parameters:
//   - it: item, nil for none
//
// Returns an empty string for the stored login, and for anything but a connection.
func (s *listScreen) userOf(it *item) string {
	if it == nil || it.Kind != kindConn {
		return ""
	}

	return s.users[it.Key]
}

// Update implements screen.
//
// Parameters:
//   - e: shared environment
//   - msg: message to handle
func (s *listScreen) Update(e *env, msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.PasteMsg:
		if s.flt.typing {
			s.flt.insert(m.Content)
			s.refilter()
		}
	case itemClickMsg:
		return s.clicked(e, m)
	case wheelMsg:
		s.sel, s.off = scrollRows(s.sel, s.off, s.rows(e), len(s.shown), m.Lines)
	case tea.KeyPressMsg:
		if s.flt.typing {
			return s.typed(e, m)
		}

		return s.pressed(e, m)
	}

	return s, nil
}

// clicked handles a click on a row: it selects the row, and a double click opens it as Enter
// does, which also ends the typing of the filter. A click on a "N more" line scrolls by a page.
//
// Parameters:
//   - e: shared environment
//   - m: the click; an ID that is not a row or a "N more" line is ignored
func (s *listScreen) clicked(e *env, m itemClickMsg) (screen, tea.Cmd) {
	rows, n := s.rows(e), len(s.shown)

	if by, ok := pageClick(m.ID, pageOf(scrollTo(s.sel, s.off, rows, n), rows, n)); ok {
		s.sel, s.off = scrollRows(s.sel, s.off, rows, n, by)

		return s, nil
	}

	if m.ID < 0 || m.ID >= len(s.shown) {
		return s, nil
	}

	s.move(e, m.ID)

	if !m.Double {
		return s, nil
	}

	s.flt.typing = false

	return s, s.activate()
}

// move changes the selection and scrolls to keep it visible.
//
// Parameters:
//   - e: shared environment
//   - to: new selected row, brought back into range
func (s *listScreen) move(e *env, to int) {
	s.sel = max(0, min(to, len(s.shown)-1))
	s.off = scrollTo(s.sel, s.off, s.rows(e), len(s.shown))
}

// navigate handles the keys that move the cursor, in both modes.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
//
// Returns true when the key was a movement.
func (s *listScreen) navigate(e *env, k tea.KeyPressMsg) bool {
	switch {
	case key.Matches(k, e.Keys.Up):
		s.move(e, s.sel-1)
	case key.Matches(k, e.Keys.Down):
		s.move(e, s.sel+1)
	case key.Matches(k, e.Keys.PageUp):
		s.move(e, s.sel-s.rows(e))
	case key.Matches(k, e.Keys.PageDown):
		s.move(e, s.sel+s.rows(e))
	case key.Matches(k, e.Keys.Home):
		s.move(e, 0)
	case key.Matches(k, e.Keys.End):
		s.move(e, len(s.shown)-1)
	default:
		return false
	}

	return true
}

// typed handles a key while the filter reads text.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (s *listScreen) typed(e *env, k tea.KeyPressMsg) (screen, tea.Cmd) {
	// Letters belong to the filter here, so only the arrow keys move the cursor.
	switch k.String() {
	case "up":
		s.move(e, s.sel-1)
	case "down":
		s.move(e, s.sel+1)
	case "pgup":
		s.move(e, s.sel-s.rows(e))
	case "pgdown":
		s.move(e, s.sel+s.rows(e))
	case "esc":
		s.flt.clear()
		s.refilter()
	case "enter":
		// Enter only applies the filter, like its hint says: a second Enter uses the record. A
		// connection whose name or alias is exactly the text gets the cursor, so that the second
		// Enter connects to it even if other rows match.
		s.flt.typing = false

		if i := s.exact(); i >= 0 {
			s.move(e, i)
		}
	case "backspace":
		s.flt.backspace()
		s.refilter()
	case "ctrl+u":
		s.flt.text = nil
		s.refilter()
	case "ctrl+w":
		s.flt.deleteWord()
		s.refilter()
	default:
		if t := k.Key().Text; t != "" {
			s.flt.insert(t)
			s.refilter()
		}
	}

	return s, nil
}

// exact looks for the connection whose name or alias is exactly the text of the filter.
//
// Returns its position among the shown rows, or -1 when the text is not an exact handle or the
// list is not the connections.
func (s *listScreen) exact() int {
	q := strings.ToLower(strings.TrimSpace(s.flt.String()))
	if s.kind != kindConn || q == "" {
		return -1
	}

	for i, idx := range s.shown {
		if slices.Contains(s.items[idx].Names, q) {
			return i
		}
	}

	return -1
}

// pressed handles a key while the list reads commands.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (s *listScreen) pressed(e *env, k tea.KeyPressMsg) (screen, tea.Cmd) {
	if s.navigate(e, k) {
		return s, nil
	}

	it := s.selected()

	switch {
	case key.Matches(k, e.Keys.Filter):
		s.flt.typing = true
	case key.Matches(k, e.Keys.Back):
		if s.flt.String() != "" {
			s.flt.clear()
			s.refilter()

			return s, nil
		}

		return s.goTo(goHomeMsg{})
	case key.Matches(k, e.Keys.Open):
		return s, s.activate()
	case key.Matches(k, e.Keys.New):
		return s.goTo(addMsg{Kind: s.kind})
	case key.Matches(k, e.Keys.Edit) && it != nil:
		return s.goTo(editMsg{Kind: s.kind, Key: it.Key})
	case key.Matches(k, e.Keys.Delete) && it != nil:
		return s.goTo(deleteMsg{Kind: s.kind, Key: it.Key})
	case key.Matches(k, e.Keys.Editor) && it != nil && s.kind == kindRepo:
		return s, msgCmd(editorMsg{Key: it.Key})
	case key.Matches(k, e.Keys.Menu) && it != nil && s.kind == kindRepo && e.pluginsOn():
		return s, e.handoff(repoMenuMsg{Key: it.Key})
	case key.Matches(k, e.Keys.User) && it != nil && s.kind == kindConn:
		return s, s.cycleUser(e, it)
	case key.Matches(k, e.Keys.Pick) && it != nil && s.kind == kindConn:
		return s.goTo(pickUserMsg{Key: it.Key, Cur: s.users[it.Key]})
	case key.Matches(k, e.Keys.Command) && it != nil && s.kind == kindConn:
		return s, msgCmd(commandMsg{Key: it.Key, User: s.users[it.Key]})
	case key.Matches(k, e.Keys.Copy) && it != nil:
		return s, msgCmd(copyMsg{Kind: s.kind, Key: it.Key, User: s.userOf(it)})
	case key.Matches(k, e.Keys.View) && it != nil:
		return s.goTo(viewMsg{Kind: s.kind, Key: it.Key, User: s.userOf(it)})
	case key.Matches(k, e.Keys.Left):
		return s.goTo(openKindMsg{Kind: allKinds[(int(s.kind)+len(allKinds)-1)%len(allKinds)]})
	case key.Matches(k, e.Keys.Right):
		return s.goTo(openKindMsg{Kind: allKinds[(int(s.kind)+1)%len(allKinds)]})
	case key.Matches(k, e.Keys.Reload):
		return s, msgCmd(reloadMsg{})
	}

	return s, nil
}

// goTo asks the root to change the screen or to open an overlay. The root does it before the next
// key, which a message sent through a command would not: keys that come in the same burst, such as
// text typed ahead, would otherwise reach this screen once more.
//
// Parameters:
//   - m: message that navigate handles
func (s *listScreen) goTo(m tea.Msg) (screen, tea.Cmd) {
	s.nav = m

	return s, nil
}

// takeNav implements navigator.
func (s *listScreen) takeNav() tea.Msg {
	m := s.nav
	s.nav = nil

	return m
}

// activate returns the command for Enter on the selected item.
func (s *listScreen) activate() tea.Cmd {
	it := s.selected()
	if it == nil {
		return nil
	}

	return msgCmd(activateMsg{Kind: s.kind, Key: it.Key, User: s.userOf(it)})
}

// Keys implements screen.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) Keys(e *env) []key.Binding {
	if s.flt.typing {
		return filterBindings(e)
	}

	return listBindings(e, s.kind, s.readOnly != "", len(s.shown) == 0)
}

// filterBindings lists the keys of a list while its filter reads text.
//
// Parameters:
//   - e: shared environment
func filterBindings(e *env) []key.Binding {
	return []key.Binding{e.Keys.Apply, e.Keys.Clear, e.Keys.Nav}
}

// kindBindings lists the keys that only a kind of list has, starting with the one that opens a
// record.
//
// Parameters:
//   - e: shared environment
//   - k: kind of the list
func kindBindings(e *env, k kind) []key.Binding {
	open := e.Keys.Open
	open.SetHelp(e.Glyphs.enter, e.t("ui_help_open_"+kindSlug[k]))

	switch k {
	case kindConn:
		return []key.Binding{open, e.Keys.User, e.Keys.Pick, e.Keys.Command}
	case kindRepo:
		if e.pluginsOn() {
			return []key.Binding{open, e.Keys.Editor, e.Keys.Menu}
		}

		return []key.Binding{open, e.Keys.Editor}
	}

	return []key.Binding{open}
}

// commonListBindings lists the keys that every list has apart from the one that opens a record.
//
// Parameters:
//   - e: shared environment
func commonListBindings(e *env) []key.Binding {
	return []key.Binding{
		e.Keys.Filter, e.Keys.New, e.Keys.Edit, e.Keys.Delete, e.Keys.Copy, e.Keys.View,
		e.Keys.Back, e.Keys.Nav, e.Keys.Tabs, e.Keys.Reload,
	}
}

// listBindings lists the keys of a list that reads commands, the most important first, in the
// order of the mockups: open, edit, delete, new, then the filter.
//
// Parameters:
//   - e: shared environment
//   - k: kind of the list
//   - readOnly: true when the file cannot be written, which leaves out the keys that write
//   - empty: true when no record is shown, which leaves out the keys that act on the record under
//     the cursor, so that the footer offers nothing that would do nothing
func listBindings(e *env, k kind, readOnly, empty bool) []key.Binding {
	kb := kindBindings(e, k)

	var bs []key.Binding

	switch {
	case empty && readOnly:
	case empty:
		bs = []key.Binding{e.Keys.New, e.Keys.Filter}
	case readOnly:
		bs = []key.Binding{kb[0], e.Keys.Filter}
	default:
		bs = []key.Binding{kb[0], e.Keys.Edit, e.Keys.Delete, e.Keys.New, e.Keys.Filter}
	}

	if !empty {
		bs = append(bs, kb[1:]...)
		bs = append(bs, e.Keys.Copy, e.Keys.View)
	}

	bs = append(bs, e.Keys.Back, e.Keys.Palette, e.Keys.Quit)

	if !empty {
		bs = append(bs, e.Keys.Nav)
	}

	return append(bs, e.Keys.Tabs, e.Keys.Reload, e.Keys.Help)
}

// View implements screen.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines to return
func (s *listScreen) View(e *env, h int) []string {
	lines, _ := s.draw(e, h)

	return lines
}

// Zones implements zonedScreen: the visible rows can be clicked.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines of the page
func (s *listScreen) Zones(e *env, h int) []zone {
	_, zs := s.draw(e, h)

	return zs
}

// draw lays out the page of the list.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines to return
//
// Returns exactly h lines and the zones of the rows that are on them.
func (s *listScreen) draw(e *env, h int) ([]string, []zone) {
	var lines []string

	lines = append(lines, s.titleLine(e))

	if e.Layout == layoutFull {
		lines = append(lines, "")
	}

	lines = append(lines, s.filterLine(e))

	if s.readOnly != "" {
		lines = append(lines, e.lead()+e.Theme.Err.Render(e.Metric.fit(
			e.Glyphs.warn+" "+s.readOnly, e.Width-e.indent(), e.Glyphs.ellipsis)))
	}

	if e.Layout == layoutFull {
		lines = append(lines, "", s.headingLine(e))
	}

	body, zs := s.rowLines(e, s.rows(e))
	zs = shiftZones(zs, len(lines))
	lines = append(lines, body...)

	if s.panelShown(e) {
		lines = fitLines(lines, h-panelRows)
		zs = clipZones(zs, len(lines))
		lines = append(lines, detailPanel(e, *s.selected(), e.Width, s.userOf(s.selected()))...)
	}

	lines = fitLines(lines, h)

	return lines, clipZones(zs, len(lines))
}

// titleLine draws the first line: the logo, the name of the program and of the kind, and the
// number of records at the right.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) titleLine(e *env) string {
	count := strconv.Itoa(len(s.items))

	if s.flt.String() != "" {
		count = strconv.Itoa(len(s.shown)) + "/" + count
	}

	return crumbLine(e, e.t(s.kind.titleKey()), count)
}

// crumbLine draws the first line of a list: the logo, the name of the program and of the list, and
// a count at the right.
//
// Parameters:
//   - e: shared environment
//   - name: translated name of the list, cut when the width asks for it
//   - count: text at the right edge
func crumbLine(e *env, name, count string) string {
	g := e.Glyphs
	left := g.prompt + " tote " + g.crumb + " "
	room := e.Width - spareColumns - 1 - e.Metric.width(count) - 1

	name = e.Metric.fit(name, max(1, room-e.Metric.width(left)), g.ellipsis)
	gap := e.Width - spareColumns - e.Metric.width(left) - e.Metric.width(name) -
		e.Metric.width(count)

	return e.Theme.Title.Render(g.prompt) + " " + e.Theme.gradient("tote") +
		e.Theme.Muted.Render(" "+g.crumb+" ") + e.Theme.Bold.Render(name) + spaces(gap) +
		e.Theme.Muted.Render(count)
}

// filterLine draws the line of the filter: a hint, or the typed text with the cursor.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) filterLine(e *env) string {
	return filterLineOf(e, s.flt)
}

// filterLineOf draws the line of a filter: a hint, or the typed text with the cursor.
//
// Parameters:
//   - e: shared environment
//   - flt: the filter
func filterLineOf(e *env, flt filter) string {
	text := flt.String()

	if text == "" && !flt.typing {
		hint := e.Metric.fit(e.t("ui_filter_hint"), max(1, e.Width-e.indent()-2),
			e.Glyphs.ellipsis)

		return e.lead() + e.Theme.Muted.Render("/ ") + e.Theme.Hint.Render(hint)
	}

	shown := e.Metric.fitEnd(text, max(1, e.Width-e.indent()-4), e.Glyphs.ellipsis)
	cursor := ""

	if flt.typing {
		cursor = e.Theme.Marker.Render(e.Glyphs.cursor)
	}

	return e.lead() + e.Theme.Muted.Render("/ ") + e.Theme.Text.Render(shown) + cursor
}

// widths returns the widths of the visible columns.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) widths(e *env) []int {
	return columnWidths(s.natural, e.Width-e.indent()-spareColumns-2)
}

// headingLine draws the column headings.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) headingLine(e *env) string {
	cols := s.kind.columns()
	ws := s.widths(e)
	cells := make([]cell, len(ws))

	for i, w := range ws {
		cells[i] = cell{text: e.t(cols[i].head), w: w, head: true}
	}

	return e.lead() + "  " + e.clip(e.joinCells(cells, true), e.Width-e.indent()-2)
}

// rowLines draws the visible rows, or a message when there is nothing to show.
//
// Parameters:
//   - e: shared environment
//   - rows: number of rows that fit
//
// Returns the lines and the zones of the rows, counted from the first row; the message has none,
// and is wrapped on the rows when the width is short. When the list goes on above or below the
// rows, a muted "N more" line takes the first or the last row, see windowOf.
func (s *listScreen) rowLines(e *env, rows int) ([]string, []zone) {
	if len(s.shown) == 0 {
		var out []string

		text, w := s.emptyText(e), e.Width-e.indent()

		lines := e.wrap(text, w)
		if len(lines) > max(1, rows) {
			lines = wrapWords(e, text, w, max(1, rows))
		}

		for _, l := range lines {
			out = append(out, e.lead()+e.Theme.Muted.Render(l))
		}

		return out, nil
	}

	ws := s.widths(e)
	cols := s.kind.columns()
	win := windowOf(scrollTo(s.sel, s.off, rows, len(s.shown)), rows, len(s.shown))
	items := make([]string, 0, win.count)

	for i := win.off; i < win.off+win.count; i++ {
		it := s.items[s.shown[i]]
		cells := make([]cell, len(ws))

		for c, w := range ws {
			cells[c] = cell{text: it.Cells[c], w: w, muted: cols[c].head == "header_description"}
		}

		items = append(items, e.renderRow(i == s.sel, it.Warn != "", cells, e.Width))
	}

	return win.stack(e, items, e.lead()+"  ", 0, e.Width)
}

// emptyText explains an empty list.
//
// Parameters:
//   - e: shared environment
func (s *listScreen) emptyText(e *env) string {
	switch {
	case s.readOnly != "":
		return e.t("ui_empty_unreadable")
	case len(s.items) > 0:
		return e.t("ui_no_results", strings.TrimSpace(s.flt.String()))
	}

	return e.t("ui_empty")
}
