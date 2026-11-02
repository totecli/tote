package ui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// homeScreen is the welcome page: the boxed welcome and the menu of the four kinds, followed by
// the plugins when they are on, then the settings.
//
// Sel is the index of the selected entry: the position in allKinds, then the plugins when they are
// on, then the settings. Off is the index of the first entry shown when the terminal is too short
// for the whole menu, which then scrolls like a list.
type homeScreen struct {
	sel int
	off int
}

// homeEntries is the number of entries of the menu when the plugins are off: the four kinds, then
// the settings. The digits open the kinds in menu order, "1" to "4", and "5" the plugins when they
// are on. The settings have no digit, they open with the comma key, so that an entry added later
// never moves a digit, see env.homeCount and digitEntry.
var homeEntries = len(allKinds) + 1

// homeCount returns the number of entries of the menu: one more when the plugins are on.
func (e *env) homeCount() int {
	if e.pluginsOn() {
		return homeEntries + 1
	}

	return homeEntries
}

// newHomeAt returns the welcome screen with an entry selected, counting the entries the plugins
// add when they are on.
//
// Parameters:
//   - e: shared environment
//   - sel: index of the selected entry; out of range values are brought back into range
func newHomeAt(e *env, sel int) *homeScreen {
	return &homeScreen{sel: max(0, min(sel, e.homeCount()-1))}
}

// newHome returns the welcome screen.
//
// Parameters:
//   - sel: index of the selected entry; out of range values are brought back into range
func newHome(sel int) *homeScreen {
	return &homeScreen{sel: max(0, min(sel, homeEntries-1))}
}

// open returns the command that opens the entry at an index of the menu.
//
// Parameters:
//   - e: shared environment
//   - i: index of the entry
func (s *homeScreen) open(e *env, i int) tea.Cmd {
	switch {
	case i < len(allKinds):
		return msgCmd(openKindMsg{Kind: allKinds[i]})
	case e.pluginsOn() && i == len(allKinds):
		return msgCmd(openPluginsMsg{})
	}

	return msgCmd(openConfigMsg{})
}

// Update implements screen.
//
// Parameters:
//   - e: shared environment
//   - msg: message to handle
func (s *homeScreen) Update(e *env, msg tea.Msg) (screen, tea.Cmd) {
	if c, ok := msg.(itemClickMsg); ok {
		rows, n := s.menuRows(e), e.homeCount()

		page := pageOf(scrollTo(s.sel, s.off, rows, n), rows, n)

		if by, more := pageClick(c.ID, page); more {
			s.sel, s.off = scrollRows(s.sel, s.off, rows, n, by)

			return s, nil
		}

		if c.ID < 0 || c.ID >= n {
			return s, nil
		}

		s.sel = c.ID
		s.follow(e)

		return s, s.open(e, c.ID)
	}

	if w, ok := msg.(wheelMsg); ok {
		s.sel = clampInt(s.sel+max(-1, min(1, w.Lines)), 0, e.homeCount()-1)
		s.follow(e)

		return s, nil
	}

	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}

	n := e.homeCount()

	switch {
	case key.Matches(press, e.Keys.Up):
		s.sel = (s.sel + n - 1) % n
	case key.Matches(press, e.Keys.Down):
		s.sel = (s.sel + 1) % n
	case key.Matches(press, e.Keys.Home):
		s.sel = 0
	case key.Matches(press, e.Keys.End):
		s.sel = n - 1
	case key.Matches(press, e.Keys.Open):
		return s, s.open(e, s.sel)
	case key.Matches(press, e.Keys.Reload):
		return s, msgCmd(reloadMsg{})
	case key.Matches(press, e.Keys.Notice):
		return s, msgCmd(noticeMsg{})
	case key.Matches(press, e.Keys.Check):
		return s, msgCmd(checkMsg{})
	case key.Matches(press, e.Keys.Jump):
		if i, ok := digitEntry(press.String(), n-1); ok {
			s.sel = i
			s.follow(e)

			return s, s.open(e, i)
		}
	}

	s.follow(e)

	return s, nil
}

// follow scrolls the menu so that the selected entry is on a row of its own.
//
// Parameters:
//   - e: shared environment
func (s *homeScreen) follow(e *env) {
	s.off = scrollTo(s.sel, s.off, s.menuRows(e), e.homeCount())
}

// menuRows returns the number of rows that the menu has under the header.
//
// Parameters:
//   - e: shared environment
func (s *homeScreen) menuRows(e *env) int {
	h := e.Height - 2

	return max(1, h-len(homeHead(e, h)))
}

// digitEntry maps the keys "1" to "4", or "5" with the plugins, to the entries of the menu that
// come before the settings. The settings have no digit.
//
// Parameters:
//   - s: key name
//   - n: number of entries that a digit reaches, that is the menu without the settings
func digitEntry(s string, n int) (int, bool) {
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' && int(s[0]-'1') < n {
		return int(s[0] - '1'), true
	}

	return 0, false
}

// Keys implements screen.
//
// Parameters:
//   - e: shared environment
func (s *homeScreen) Keys(e *env) []key.Binding {
	return homeBindings(e)
}

// homeBindings lists the keys of the welcome page. Settings comes before Jump, so that it outlasts
// it on a narrow line: the digits only repeat what the arrows and Enter do, while the comma key is
// the one shortcut to Settings.
//
// Parameters:
//   - e: shared environment
func homeBindings(e *env) []key.Binding {
	return []key.Binding{
		e.Keys.Nav, e.Keys.Open, e.Keys.Palette, e.Keys.Quit, e.Keys.Settings, e.Keys.Jump,
		e.Keys.Help, e.Keys.Notice, e.Keys.Check,
	}
}

// Status implements screen.
//
// Parameters:
//   - e: shared environment, unused
func (s *homeScreen) Status(*env) statusInfo {
	return statusInfo{}
}

// Reload implements screen.
//
// Parameters:
//   - e: shared environment, unused
//   - selectKey: ignored, the menu has no records
func (s *homeScreen) Reload(*env, string) screen {
	return s
}

// Typing implements screen: the welcome page never reads text.
func (s *homeScreen) Typing() bool {
	return false
}

// View implements screen.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines to return
func (s *homeScreen) View(e *env, h int) []string {
	lines, _ := s.draw(e, h)

	return lines
}

// Zones implements zonedScreen: a click on an entry of the menu opens it.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines of the page
func (s *homeScreen) Zones(e *env, h int) []zone {
	_, zs := s.draw(e, h)

	return zs
}

// draw lays out the welcome page.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines to return
//
// Returns exactly h lines and the zones of the entries of the menu that are on them, and of the
// "N more" lines when the menu scrolls.
func (s *homeScreen) draw(e *env, h int) ([]string, []zone) {
	head := homeHead(e, h)
	rows := max(1, h-len(head))
	n := e.homeCount()
	win := windowOf(scrollTo(s.sel, s.off, rows, n), rows, n)
	items := make([]string, 0, win.count)

	for i := win.off; i < win.off+win.count; i++ {
		items = append(items, s.entry(e, i))
	}

	body, zs := win.stack(e, items, e.lead()+"  ", 0, e.Width)
	lines := fitLines(append(head, body...), h)

	return lines, clipZones(shiftZones(zs, len(head)), len(lines))
}

// entry draws one entry of the menu.
//
// Parameters:
//   - e: shared environment
//   - i: index of the entry
func (s *homeScreen) entry(e *env, i int) string {
	label, right := e.t("ui_config_title"), e.Tr.Lang()

	switch {
	case i < len(allKinds):
		k := allKinds[i]
		label, right = e.t(k.titleKey()), strconv.Itoa(e.Data.count(k))

		if e.Data.err(k) != nil {
			right = e.Glyphs.warn
		}
	case e.pluginsOn() && i == len(allKinds):
		label, right = e.t("ui_plugins_title"), e.Glyphs.ellipsis

		if c, known := e.Plug.summary(); known {
			right = strconv.Itoa(c.Total)

			if c.Attention > 0 {
				right = e.Glyphs.warn + " " + right
			}
		}
	}

	cw := e.Metric.width(right)
	cells := []cell{
		{text: label, w: max(1, e.Width-e.indent()-spareColumns-2-columnGap-cw)},
		{text: right, w: cw, right: true},
	}

	return e.renderRow(i == s.sel, false, cells, e.Width)
}

// homeHead draws what is above the menu: the welcome box, or the one-line header and a blank
// line, or the header alone when the blank line would hide an entry.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines of the page
//
// Returns no line for ui.welcome "off". The box is drawn only when it leaves a row for each entry,
// which a terminal of the full layout always does but for ui.compact "never" on a short one.
func homeHead(e *env, h int) []string {
	if e.Settings.UI.Welcome == "off" {
		return nil
	}

	if e.Layout == layoutFull {
		if box := welcomeBox(e); len(box)+e.homeCount() <= h {
			return box
		}
	}

	if h >= 2+e.homeCount() {
		return []string{compactHeader(e), ""}
	}

	return []string{compactHeader(e)}
}

// welcomeBox draws the welcome box: the bag, the greeting, the tagline and the counts, with the
// name and the version in the top border.
//
// Parameters:
//   - e: shared environment
func welcomeBox(e *env) []string {
	w := e.Width
	title := e.Theme.gradient("tote") + e.Theme.Muted.Render(" "+sanitize(e.Version))
	text := w - 2 - 3 - 6 - 3

	counts := make([]string, len(allKinds))
	for i, k := range allKinds {
		counts[i] = countOrUnknown(e, k)
	}

	greeting := e.t("ui_welcome_anon")
	if user := strings.TrimSpace(sanitize(e.User)); user != "" {
		greeting = e.t("ui_welcome", user)
	}

	right := [3]string{
		e.Theme.Bold.Render(e.Metric.fit(greeting, text, "")),
		e.Theme.Muted.Render(e.Metric.fit(e.t("ui_tagline"), text, "")),
		e.Theme.Text.Render(e.Metric.fit(strings.Join(counts, " "+e.Glyphs.sep+" "),
			text, e.Glyphs.ellipsis)),
	}

	lines := []string{""}
	for i := range right {
		lines = append(lines, "   "+e.Theme.bag(e.Glyphs.logo[i], i, 3)+"   "+right[i])
	}

	lines = append(lines, "")

	return boxed(e, title, lines, w)
}

// compactHeader draws the one-line header of the compact layout: the logo, the version and the
// short counts.
//
// Parameters:
//   - e: shared environment
func compactHeader(e *env) string {
	parts := make([]string, len(allKinds))
	for i, k := range allKinds {
		parts[i] = shortCount(e, k)
	}

	whole := e.Glyphs.prompt + " tote"
	head := whole
	tail := " " + sanitize(e.Version) + " " + e.Glyphs.sep + " " +
		strings.Join(parts, " "+e.Glyphs.sep+" ")

	room := e.Width - 1
	head = e.Metric.fit(head, room, "")
	tail = e.Metric.fit(tail, room-e.Metric.width(head), e.Glyphs.ellipsis)

	if head == whole {
		return " " + e.Theme.Title.Render(e.Glyphs.prompt) + " " + e.Theme.gradient("tote") +
			e.Theme.Muted.Render(tail)
	}

	return " " + e.Theme.Title.Render(head) + e.Theme.Muted.Render(tail)
}

// countOrUnknown renders the count of a kind as in the welcome box, or its name in full behind a
// question mark when the file of the kind could not be read, so that the page never claims it is
// empty.
//
// Parameters:
//   - e: shared environment
//   - k: kind counted
func countOrUnknown(e *env, k kind) string {
	if e.Data.err(k) != nil {
		return e.t("ui_count_" + kindSlug[k] + "_unknown")
	}

	return countText(e, k, e.Data.count(k))
}

// shortCount renders the count of a kind with its short name, or a question mark when the file
// of the kind could not be read.
//
// Parameters:
//   - e: shared environment
//   - k: kind counted
func shortCount(e *env, k kind) string {
	if e.Data.err(k) != nil {
		return "? " + e.t(k.shortKey())
	}

	n := e.Data.count(k)
	key := k.shortKey()

	if plural(e.Tr.Lang(), n) == "one" {
		key = k.shortOneKey()
	}

	return strconv.Itoa(n) + " " + e.t(key)
}
