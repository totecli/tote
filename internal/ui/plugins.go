package ui

import (
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// pluginRow is one line of the plugins screen: an installed plugin, or a plugin shipped with tote
// that is not installed yet.
//
// Every text is sanitized. State is the stable state of the plugin or "available". Inst is the
// plugin as one Load saw it, nil for an available one, and Avail the description of the available
// plugin, nil for an installed one.
type pluginRow struct {
	id      string
	title   string
	version string
	state   string
	origin  string
	desc    string
	tier    string
	caps    int
	reasons []plugin.Reason
	inst    *plugin.Plugin
	avail   *plugin.Available
}

// stateAvailable is the state shown for a plugin shipped with tote that is not installed.
const stateAvailable = "available"

// pluginsScreen is the screen of the plugins: the installed ones and the ones shipped with tote,
// with a filter, a selection and a detail panel.
//
// Rows are all the plugins and Items the same, prepared for the filter. Shown indexes the rows
// that match, Sel indexes Shown and Off is the first visible row. Natural holds the preferred
// width of each column. Problems are the failures that concern every plugin, such as a lock that
// does not parse, and Off tells that the plugins are off for this run.
type pluginsScreen struct {
	rows     []pluginRow
	items    []item
	shown    []int
	sel      int
	off      int
	flt      filter
	natural  []int
	problems []plugin.Reason
	session  bool
}

// pluginHeads are the catalog keys of the headings of the columns.
var pluginHeads = [...]string{"header_name", "header_version", "header_state", "header_description"}

// pluginMaxWidths are the widest each column gets.
var pluginMaxWidths = [...]int{24, 12, 22, 200}

// newPlugins builds the plugins screen from the registry.
//
// Parameters:
//   - e: shared environment, which gives the registry
//   - selectID: plugin to select, empty for the first
func newPlugins(e *env, selectID string) *pluginsScreen {
	s := &pluginsScreen{}
	s.load(e, selectID)

	return s
}

// load reads the plugins again and applies the filter.
//
// Parameters:
//   - e: shared environment
//   - selectID: id to select afterwards; empty keeps the selected plugin when it is still there,
//     else the same position
func (s *pluginsScreen) load(e *env, selectID string) {
	keep := selectID
	if keep == "" {
		if r := s.selected(); r != nil {
			keep = r.id
		}
	}

	prev := s.sel
	snap := e.Plugins.Load()
	s.rows = pluginRows(e, snap, e.Plugins.Available(snap, e.Tr.Lang()))
	s.problems = snap.Problems
	s.session = false

	for _, r := range s.rows {
		for _, why := range r.reasons {
			s.session = s.session || why.Code == plugin.ReasonSessionOff
		}
	}

	if e.Plug != nil {
		e.Plug.counts = snap.Count()
	}

	s.items = make([]item, len(s.rows))
	for i, r := range s.rows {
		s.items[i] = item{
			Key: r.id, Names: []string{r.id, strings.ToLower(r.title)},
			Text: strings.ToLower(r.id + " " + r.title + " " + r.desc + " " + r.state),
		}
	}

	s.measure(e)
	s.refilter()

	s.sel = max(0, min(prev, len(s.shown)-1))

	for i, idx := range s.shown {
		if keep != "" && s.rows[idx].id == keep {
			s.sel = i
		}
	}

	s.off = scrollTo(s.sel, s.off, s.capacity(e), len(s.shown))
}

// pluginRows builds the rows of the installed plugins and of the ones shipped with tote, by id.
//
// Parameters:
//   - e: shared environment, for the language and the words
//   - snap: what Load found
//   - avail: the plugins shipped with tote that are not installed
func pluginRows(e *env, snap plugin.Snapshot, avail []plugin.Available) []pluginRow {
	var rows []pluginRow

	for _, p := range snap.Plugins() {
		p := p
		r := pluginRow{
			id: p.ID, state: string(p.State), reasons: p.Reasons, inst: &p,
			version: sanitize(p.Record.Version), tier: p.Record.Tier, caps: len(p.Caps),
			origin: sanitize(p.Record.Origin),
		}

		if r.version == "" {
			r.version = sanitize(p.Header.Version)
		}

		if r.tier == "" {
			r.tier = p.Header.Tier
		}

		if r.origin == "" {
			r.origin = e.pword("plugin_kind_", p.Record.Kind)
		}

		r.title = sanitize(p.Header.Title)

		if p.Manifest != nil {
			r.title = sanitize(e.ptext(p.Catalog, "title", p.Manifest.Title))
			desc := e.ptext(p.Catalog, "description", p.Manifest.Description)
			r.desc = sanitize(firstLine(desc))
		}

		rows = append(rows, r)
	}

	for _, av := range avail {
		av := av
		rows = append(rows, pluginRow{
			id: av.ID, title: sanitize(av.Title),
			version: sanitize(av.Version), state: stateAvailable,
			origin: e.pword("plugin_kind_", plugin.KindBundled), tier: av.Tier,
			desc: sanitize(firstLine(av.Description)), caps: len(av.Caps), avail: &av,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].id < rows[j].id })

	return rows
}

// firstLine gives the first line of a text.
//
// Parameters:
//   - s: text, possibly on several lines
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")

	return line
}

// measure computes the preferred width of every column.
//
// Parameters:
//   - e: shared environment
func (s *pluginsScreen) measure(e *env) {
	s.natural = make([]int, len(pluginHeads))

	for i, h := range pluginHeads {
		w := e.Metric.width(e.t(h))

		for _, r := range s.rows {
			w = max(w, e.Metric.width(s.cellText(e, r, i)))
		}

		s.natural[i] = min(w, pluginMaxWidths[i])
	}
}

// cellText gives the text of one column of a row.
//
// Parameters:
//   - e: shared environment
//   - r: the row
//   - col: index of the column
func (s *pluginsScreen) cellText(e *env, r pluginRow, col int) string {
	switch col {
	case 0:
		return r.id
	case 1:
		return r.version
	case 2:
		return e.Glyphs.dot + " " + e.pstate(r.state)
	}

	return r.desc
}

// refilter recomputes Shown from the filter text and puts the cursor on the best match.
func (s *pluginsScreen) refilter() {
	s.shown = matchItems(s.items, s.flt.String())
	s.sel, s.off = 0, 0
}

// selected returns the row under the cursor, nil when there is none.
func (s *pluginsScreen) selected() *pluginRow {
	if s.sel < 0 || s.sel >= len(s.shown) {
		return nil
	}

	return &s.rows[s.shown[s.sel]]
}

// panelShown reports whether the detail panel is on screen.
//
// Parameters:
//   - e: shared environment
func (s *pluginsScreen) panelShown(e *env) bool {
	return e.Layout == layoutFull && e.Height >= panelHeight && s.selected() != nil
}

// banners returns the lines above the rows that tell what concerns every plugin.
//
// Parameters:
//   - e: shared environment
func (s *pluginsScreen) banners(e *env) []string {
	var out []string

	room := e.Width - e.indent()

	if s.session {
		out = append(out, e.lead()+e.Theme.Warn.Render(e.Metric.fit(e.Glyphs.warn+" "+
			e.reasonText(plugin.Reason{Code: plugin.ReasonSessionOff}), room, e.Glyphs.ellipsis)))
	}

	if len(s.problems) > 0 {
		text := e.Glyphs.warn + " " + e.reasonText(s.problems[0])
		if n := len(s.problems) - 1; n > 0 {
			text += " (" + e.t("cli_plugin_more", n) + ")"
		}

		out = append(out, e.lead()+e.Theme.Err.Render(e.Metric.fit(text, room, e.Glyphs.ellipsis)))
	}

	return out
}

// headLines returns the number of lines above the rows.
//
// Parameters:
//   - e: shared environment
func (s *pluginsScreen) headLines(e *env) int {
	n := 2
	if e.Layout == layoutFull {
		n = 5
	}

	return n + len(s.banners(e))
}

// capacity returns the number of rows that fit between the heading and the panel.
//
// Parameters:
//   - e: shared environment
func (s *pluginsScreen) capacity(e *env) int {
	n := e.Height - 2 - s.headLines(e)
	if s.panelShown(e) {
		n -= panelRows
	}

	return max(1, n)
}

// Typing implements screen.
func (s *pluginsScreen) Typing() bool {
	return s.flt.typing
}

// Reload implements screen.
//
// Parameters:
//   - e: shared environment
//   - selectKey: id of the plugin to select, empty to keep the selection
func (s *pluginsScreen) Reload(e *env, selectKey string) screen {
	s.load(e, selectKey)

	return s
}

// Status implements screen: when the panel is not on screen, the first reason the selected plugin
// is not enabled.
//
// Parameters:
//   - e: shared environment
func (s *pluginsScreen) Status(e *env) statusInfo {
	r := s.selected()
	if r == nil || s.panelShown(e) {
		return statusInfo{}
	}

	return statusInfo{Command: s.reasonLine(e, *r)}
}

// reasonLine gives the sentence under a plugin in the panel: the first reason it is not enabled,
// or that it can be installed.
//
// Parameters:
//   - e: shared environment
//   - r: the row
func (s *pluginsScreen) reasonLine(e *env, r pluginRow) string {
	switch {
	case r.state == stateAvailable:
		return e.t("ui_plug_available", sanitize(e.Keys.Install.Help().Key))
	case len(r.reasons) > 0:
		return strings.TrimSpace(e.reasonText(r.reasons[0]) + " " + hintOf(e, r))
	}

	return ""
}

// hintOf gives the hint of the program a plugin shipped with tote misses, labelled as the words of
// the plugin. The hint of any other plugin stays in its details, where it is not mistaken for a
// word of tote.
//
// Parameters:
//   - e: shared environment
//   - r: the row
//
// Returns the labelled hint, or an empty string when there is none to show.
func hintOf(e *env, r pluginRow) string {
	p := r.inst
	if p == nil || p.Record.Kind != plugin.KindBundled || len(r.reasons) == 0 ||
		r.reasons[0].Code != plugin.ReasonMissingDep {
		return ""
	}

	for _, m := range p.Missing {
		if hint := e.ptext(p.Catalog, "requires."+m.Bin+".hint", m.Hint); hint != "" {
			return e.t("plugin_hint_label", sanitize(hint))
		}
	}

	return ""
}

// Update implements screen.
//
// Parameters:
//   - e: shared environment
//   - msg: message to handle
func (s *pluginsScreen) Update(e *env, msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.PasteMsg:
		if s.flt.typing {
			s.flt.insert(m.Content)
			s.refilter()
		}
	case itemClickMsg:
		return s.clicked(e, m)
	case wheelMsg:
		s.sel, s.off = scrollRows(s.sel, s.off, s.capacity(e), len(s.shown), m.Lines)
	case tea.KeyPressMsg:
		if s.flt.typing {
			return s.typed(e, m)
		}

		return s.pressed(e, m)
	}

	return s, nil
}

// clicked handles a click on a row: it selects the row, and a double click opens its details. A
// click on a "N more" line scrolls by a page.
//
// Parameters:
//   - e: shared environment
//   - m: the click
func (s *pluginsScreen) clicked(e *env, m itemClickMsg) (screen, tea.Cmd) {
	rows, n := s.capacity(e), len(s.shown)

	if by, ok := pageClick(m.ID, pageOf(scrollTo(s.sel, s.off, rows, n), rows, n)); ok {
		s.sel, s.off = scrollRows(s.sel, s.off, rows, n, by)

		return s, nil
	}

	if m.ID < 0 || m.ID >= n {
		return s, nil
	}

	s.move(e, m.ID)

	if !m.Double {
		return s, nil
	}

	s.flt.typing = false

	return s, s.act(actDetails)
}

// move changes the selection and scrolls to keep it visible.
//
// Parameters:
//   - e: shared environment
//   - to: new selected row, brought back into range
func (s *pluginsScreen) move(e *env, to int) {
	s.sel = max(0, min(to, len(s.shown)-1))
	s.off = scrollTo(s.sel, s.off, s.capacity(e), len(s.shown))
}

// typed handles a key while the filter reads text.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (s *pluginsScreen) typed(e *env, k tea.KeyPressMsg) (screen, tea.Cmd) {
	switch k.String() {
	case "up":
		s.move(e, s.sel-1)
	case "down":
		s.move(e, s.sel+1)
	case "pgup":
		s.move(e, s.sel-s.capacity(e))
	case "pgdown":
		s.move(e, s.sel+s.capacity(e))
	case "esc":
		s.flt.clear()
		s.refilter()
	case "enter":
		s.flt.typing = false
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

// pressed handles a key while the screen reads commands. A letter opens a menu, a question or a
// view; none of them changes anything by itself.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (s *pluginsScreen) pressed(e *env, k tea.KeyPressMsg) (screen, tea.Cmd) {
	switch {
	case key.Matches(k, e.Keys.Up):
		s.move(e, s.sel-1)
	case key.Matches(k, e.Keys.Down):
		s.move(e, s.sel+1)
	case key.Matches(k, e.Keys.PageUp):
		s.move(e, s.sel-s.capacity(e))
	case key.Matches(k, e.Keys.PageDown):
		s.move(e, s.sel+s.capacity(e))
	case key.Matches(k, e.Keys.Home):
		s.move(e, 0)
	case key.Matches(k, e.Keys.End):
		s.move(e, len(s.shown)-1)
	case key.Matches(k, e.Keys.Filter):
		s.flt.typing = true
	case key.Matches(k, e.Keys.Back):
		if s.flt.String() != "" {
			s.flt.clear()
			s.refilter()

			return s, nil
		}

		return s, msgCmd(goHomeMsg{})
	case key.Matches(k, e.Keys.Reload):
		return s, msgCmd(reloadMsg{})
	case key.Matches(k, e.Keys.Open):
		return s, s.act(actDetails)
	case key.Matches(k, e.Keys.Toggle):
		return s, s.act(actToggle)
	case key.Matches(k, e.Keys.Allow):
		return s, s.act(actAllow)
	case key.Matches(k, e.Keys.Setup):
		return s, s.act(actSettings)
	case key.Matches(k, e.Keys.Install):
		return s, s.act(actInstall)
	case key.Matches(k, e.Keys.Delete):
		return s, s.act(actRemove)
	case key.Matches(k, e.Keys.Verify):
		return s, s.act(actCheck)
	case key.Matches(k, e.Keys.Menu):
		return s, s.act(actMenu)
	}

	return s, nil
}

// act returns the command that asks for an action on the selected plugin.
//
// Parameters:
//   - a: the action
func (s *pluginsScreen) act(a pluginAct) tea.Cmd {
	r := s.selected()
	if r == nil {
		return nil
	}

	return msgCmd(pluginActMsg{Act: a, ID: r.id, Seen: r.state})
}

// Keys implements screen.
//
// Parameters:
//   - e: shared environment
func (s *pluginsScreen) Keys(e *env) []key.Binding {
	if s.flt.typing {
		return filterBindings(e)
	}

	return pluginsBindings(e, s.selected(), false)
}

// pluginsBindings lists the keys of the plugins screen, the most important first, for the plugin
// under the cursor: the keys that do not apply to its state are left out, so that the footer
// offers nothing that would do nothing.
//
// Parameters:
//   - e: shared environment
//   - r: the plugin under the cursor, nil for an empty list
//   - all: true for every key of the screen, as the help lists them
func pluginsBindings(e *env, r *pluginRow, all bool) []key.Binding {
	open := e.Keys.Open
	open.SetHelp(e.Glyphs.enter, e.t("ui_help_details"))

	toggle := e.Keys.Toggle
	remove := e.Keys.Delete
	remove.SetHelp(remove.Help().Key, e.t("ui_help_remove"))

	var bs []key.Binding

	switch {
	case all:
		bs = []key.Binding{
			open, toggle, e.Keys.Allow, e.Keys.Setup, e.Keys.Install, remove,
			e.Keys.Verify, e.Keys.Menu, e.Keys.Filter,
		}
	case r == nil:
		bs = []key.Binding{e.Keys.Filter}
	case r.state == stateAvailable:
		bs = []key.Binding{open, e.Keys.Install, e.Keys.Verify, e.Keys.Menu, e.Keys.Filter}
	default:
		toggle.SetHelp(toggle.Help().Key, e.t(toggleKey(r.state)))

		bs = []key.Binding{open}
		if togglable(r.state) {
			bs = append(bs, toggle)
		}

		if plugin.State(r.state) == plugin.StateNeedsConsent {
			bs = append(bs, e.Keys.Allow)
		}

		if r.inst != nil && r.inst.Manifest != nil && len(r.inst.Manifest.Settings) > 0 {
			bs = append(bs, e.Keys.Setup)
		}

		bs = append(bs, remove, e.Keys.Verify, e.Keys.Menu, e.Keys.Filter)
	}

	bs = append(bs, e.Keys.Back, e.Keys.Palette, e.Keys.Quit)

	if r != nil || all {
		bs = append(bs, e.Keys.Nav)
	}

	return append(bs, e.Keys.Reload, e.Keys.Help)
}

// togglable reports whether the space key can turn a plugin in a state on or off: a broken or an
// incompatible plugin cannot be enabled.
//
// Parameters:
//   - state: state of the plugin
func togglable(state string) bool {
	switch plugin.State(state) {
	case plugin.StateBroken, plugin.StateIncompatible:
		return false
	}

	return true
}

// toggleKey gives the catalog key of what the space key does to a plugin in a state.
//
// Parameters:
//   - state: state of the plugin
func toggleKey(state string) string {
	switch plugin.State(state) {
	case plugin.StateEnabled, plugin.StateMissingDependency:
		return "ui_help_disable"
	}

	return "ui_help_enable"
}

// View implements screen.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines to return
func (s *pluginsScreen) View(e *env, h int) []string {
	lines, _ := s.draw(e, h)

	return lines
}

// Zones implements zonedScreen: the visible rows can be clicked.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines of the page
func (s *pluginsScreen) Zones(e *env, h int) []zone {
	_, zs := s.draw(e, h)

	return zs
}

// draw lays out the page of the plugins.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines to return
//
// Returns exactly h lines and the zones of the rows that are on them.
func (s *pluginsScreen) draw(e *env, h int) ([]string, []zone) {
	lines := []string{crumbLine(e, e.t("ui_plugins_title"), s.countText())}

	if e.Layout == layoutFull {
		lines = append(lines, "")
	}

	lines = append(lines, filterLineOf(e, s.flt))
	lines = append(lines, s.banners(e)...)

	if e.Layout == layoutFull {
		lines = append(lines, "", s.headingLine(e))
	}

	body, zs := s.rowLines(e, s.capacity(e))
	zs = shiftZones(zs, len(lines))
	lines = append(lines, body...)

	if s.panelShown(e) {
		lines = fitLines(lines, h-panelRows)
		zs = clipZones(zs, len(lines))
		lines = append(lines, s.panel(e, *s.selected())...)
	}

	lines = fitLines(lines, h)

	return lines, clipZones(zs, len(lines))
}

// countText gives the number at the right of the title: the installed plugins, the same number as
// the welcome page, and the ones that match out of them when the filter hides some. A plugin
// shipped with tote that is not installed is listed but not counted.
func (s *pluginsScreen) countText() string {
	total, shown := 0, 0

	for _, r := range s.rows {
		if r.state != stateAvailable {
			total++
		}
	}

	for _, i := range s.shown {
		if s.rows[i].state != stateAvailable {
			shown++
		}
	}

	if s.flt.String() != "" {
		return strconv.Itoa(shown) + "/" + strconv.Itoa(total)
	}

	return strconv.Itoa(total)
}

// widths returns the widths of the visible columns.
//
// Parameters:
//   - e: shared environment
func (s *pluginsScreen) widths(e *env) []int {
	return columnWidths(s.natural, e.Width-e.indent()-spareColumns-2)
}

// headingLine draws the column headings.
//
// Parameters:
//   - e: shared environment
func (s *pluginsScreen) headingLine(e *env) string {
	ws := s.widths(e)
	cells := make([]cell, len(ws))

	for i, w := range ws {
		cells[i] = cell{text: e.t(pluginHeads[i]), w: w, head: true}
	}

	return e.lead() + "  " + e.clip(e.joinCells(cells, true), e.Width-e.indent()-2)
}

// rowLines draws the visible rows, or a message when there is nothing to show.
//
// Parameters:
//   - e: shared environment
//   - rows: number of rows that fit
//
// Returns the lines and the zones of the rows, counted from the first row.
func (s *pluginsScreen) rowLines(e *env, rows int) ([]string, []zone) {
	if len(s.shown) == 0 {
		var out []string

		text, w := e.t("ui_plugins_empty"), e.Width-e.indent()
		if len(s.rows) > 0 {
			text = e.t("ui_no_results", strings.TrimSpace(s.flt.String()))
		}

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
	win := windowOf(scrollTo(s.sel, s.off, rows, len(s.shown)), rows, len(s.shown))
	items := make([]string, 0, win.count)

	for i := win.off; i < win.off+win.count; i++ {
		r := s.rows[s.shown[i]]
		cells := make([]cell, len(ws))

		for c, w := range ws {
			cells[c] = cell{text: s.cellText(e, r, c), w: w, muted: c == 3}
			if c == 2 {
				cells[c].health = healthOf(r.state)
			}
		}

		items = append(items, e.renderRow(i == s.sel, false, cells, e.Width))
	}

	return win.stack(e, items, e.lead()+"  ", 0, e.Width)
}

// panel draws the panel under the list for a plugin: the description, the tier, the origin and the
// number of capabilities, and the first reason it is not enabled. The identifier comes first in
// the top border and the title the plugin gives itself follows it in the secondary color, so that
// a plugin cannot pass for another by its title.
//
// Parameters:
//   - e: shared environment
//   - r: the plugin
func (s *pluginsScreen) panel(e *env, r pluginRow) []string {
	inner := e.Width - 2
	room := max(1, inner-2)

	title := e.Theme.Bold.Render(e.Metric.fit(r.id, max(1, inner-6), e.Glyphs.ellipsis))
	left := inner - 6 - e.Metric.width(r.id) - 1
	if r.title != "" && !strings.EqualFold(r.title, r.id) && left > 3 {
		title += " " + e.Theme.Muted.Render(e.Metric.fit(r.title, left, e.Glyphs.ellipsis))
	}

	meta := joinFields(e, e.pword("plugin_tiername_", r.tier), r.origin,
		e.t("ui_plug_caps", r.caps))
	reason := s.reasonLine(e, r)

	rows := []string{
		" " + e.Theme.Text.Render(e.Metric.fit(r.desc, room, e.Glyphs.ellipsis)),
		" " + e.Theme.Muted.Render(e.Metric.fit(meta, room, e.Glyphs.ellipsis)),
		"",
	}

	if reason != "" {
		style := e.healthStyle(healthOf(r.state))
		if r.state == stateAvailable {
			style = e.Theme.Muted
		}

		rows[2] = " " + style.Render(e.Metric.fit(reason, room, e.Glyphs.ellipsis))
	}

	return boxed(e, title, rows, e.Width)
}
