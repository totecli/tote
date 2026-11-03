package ui

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/totecli/tote/internal/plugin"
)

// consentWidth is the widest the box of the consent gets, in cells.
const consentWidth = 84

// consentShort is how many rows more than consentSpacer a box needs to give a long field, such as
// the source, more than one row.
const consentShort = 4

// consentSpacer is the height of the box from which a blank line separates the text from the
// buttons.
const consentSpacer = 8

// consentOverlay is the overlay that shows what a plugin asks for, and asks the user to allow it.
//
// Diff is what the install, the update or the allow screen will do and Mode which of the three it
// is. Staged is the package that waits in the staging area, nil for a plugin that is already
// installed; the overlay owns it until the consent is given or refused, and deletes it when it is
// refused. Enable tells that the plugin is enabled once it is allowed. Yes tells which button has
// the focus; it starts on Cancel, so that Enter alone never allows anything. Off is the first
// visible line of the text; Rows and Total are recorded by the last View for the scroll keys.
type consentOverlay struct {
	diff   plugin.Diff
	mode   string
	staged *plugin.Staged
	enable bool
	yes    bool
	off    int
	rows   int
	total  int
}

// newConsent returns the overlay that asks for the consent of a plugin.
//
// Parameters:
//   - e: shared environment, unused
//   - d: what will be done
//   - mode: one of the View constants of the plugin package
//   - st: the staged package, nil for an installed plugin
//   - enable: true to enable the plugin once it is allowed
func newConsent(_ *env, d plugin.Diff, mode string, st *plugin.Staged, enable bool) overlay {
	return &consentOverlay{diff: d, mode: mode, staged: st, enable: enable}
}

// abort deletes the staged package, if the overlay still holds one.
func (c *consentOverlay) abort() {
	if c.staged != nil {
		_ = c.staged.Abort()
		c.staged = nil
	}
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (c *consentOverlay) Init(_ *env) tea.Cmd {
	return nil
}

// refuse closes the overlay without consent and says that nothing changed.
//
// Parameters:
//   - e: shared environment, for the text
func (c *consentOverlay) refuse(e *env) (overlay, tea.Cmd, bool) {
	c.abort()

	return c, toastCmd(e.t("cli_plugin_declined"), false), true
}

// accept closes the overlay with the consent. The staged package goes to the root with it.
func (c *consentOverlay) accept() (overlay, tea.Cmd, bool) {
	m := pluginGrantMsg{ID: c.diff.ID, Staged: c.staged, Enable: c.enable, Token: c.diff.Grant}
	c.staged = nil

	return c, msgCmd(m), true
}

// answer gives the answer of the button that has the focus.
//
// Parameters:
//   - e: shared environment
func (c *consentOverlay) answer(e *env) (overlay, tea.Cmd, bool) {
	if c.yes {
		return c.accept()
	}

	return c.refuse(e)
}

// Update implements overlay: Esc and n refuse, the arrow keys, Tab and h and l change the button
// and Enter picks the one that has the focus. There is no key that allows by itself.
//
// Parameters:
//   - e: shared environment
//   - msg: key press, click, wheel or dismissal; other messages are ignored
func (c *consentOverlay) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return c.refuse(e)
	case wheelMsg:
		c.off = clampInt(c.off+m.Lines, 0, maxOffset(c.rows, c.total))
	case itemClickMsg:
		switch m.ID {
		case confirmNo:
			return c.refuse(e)
		case confirmYes:
			return c.accept()
		}

		if by, ok := pageClick(m.ID, pageOf(c.off, c.rows, c.total)); ok {
			c.off = clampInt(c.off+by, 0, maxOffset(c.rows, c.total))
		}
	case tea.KeyPressMsg:
		return c.key(e, m)
	}

	return c, nil, false
}

// key handles a key press.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (c *consentOverlay) key(e *env, k tea.KeyPressMsg) (overlay, tea.Cmd, bool) {
	page := pageOf(c.off, c.rows, c.total)

	switch s := k.String(); {
	case s == "esc" || s == "n" || s == "N":
		return c.refuse(e)
	case s == "enter":
		return c.answer(e)
	case s == "left" || s == "right" || s == "h" || s == "l" || s == "tab" || s == "shift+tab":
		c.yes = !c.yes
	case key.Matches(k, e.Keys.Up):
		c.off--
	case key.Matches(k, e.Keys.Down):
		c.off++
	case key.Matches(k, e.Keys.PageUp):
		c.off -= page
	case key.Matches(k, e.Keys.PageDown):
		c.off += page
	case key.Matches(k, e.Keys.Home):
		c.off = 0
	case key.Matches(k, e.Keys.End):
		c.off = maxOffset(c.rows, c.total)
	}

	c.off = clampInt(c.off, 0, maxOffset(c.rows, c.total))

	return c, nil, false
}

// bodyLines lays out the consent view for a width: the lines under the title, each marker at the
// start and the words wrapped under their first word.
//
// Parameters:
//   - e: shared environment
//   - inner: cells available inside the box, the margin included
//   - fieldRows: most rows a field such as the source gets, so that a long path does not push the
//     name and the access of the plugin out of a short box
func (c *consentOverlay) bodyLines(e *env, inner, fieldRows int) []string {
	var out []string

	view := plugin.NewConsentView(c.diff, c.mode, false)

	for _, l := range view.Lines {
		if l.Level == plugin.LevelTitle {
			continue
		}

		if l.Level == plugin.LevelGap && len(out) == 0 {
			continue
		}

		start, body := e.viewParts(l)
		room := inner - e.Metric.width(start)

		if room < minViewRoom {
			out = append(out, e.consentStyle(l.Level).Render(e.Metric.fit(start+body, inner,
				e.Glyphs.ellipsis)))

			continue
		}

		rows := helpWrapLines
		if l.Level == plugin.LevelField {
			rows = fieldRows
		}

		parts := wrapWords(e, body, room, rows)
		if len(parts) == 0 {
			parts = []string{""}
		}

		hang := spaces(e.Metric.width(start))

		for i, p := range parts {
			lead := hang
			if i == 0 {
				lead = start
			}

			out = append(out, e.consentStyle(l.Level).Render(lead+p))
		}
	}

	return out
}

// minViewRoom is the least cells a line of the consent view keeps for its words once the marker at
// its start is taken; a line with less is cut instead of wrapped.
const minViewRoom = 8

// consentStyle gives the style of a line of the consent view by its level.
//
// Parameters:
//   - level: one of the Level constants of the plugin package
func (e *env) consentStyle(level string) lipgloss.Style {
	switch level {
	case plugin.LevelHead:
		return e.Theme.Bold
	case plugin.LevelStrong, plugin.LevelWarn, plugin.LevelAdd:
		return e.Theme.Warn
	case plugin.LevelDrop, plugin.LevelNote, plugin.LevelQuote, plugin.LevelCode:
		return e.Theme.Muted
	case plugin.LevelCmd:
		return e.Theme.Key
	}

	return e.Theme.Text
}

// title gives the title of the box, the first line of the view.
//
// Parameters:
//   - e: shared environment
func (c *consentOverlay) title(e *env) string {
	for _, l := range plugin.NewConsentView(c.diff, c.mode, false).Lines {
		if l.Level == plugin.LevelTitle {
			return e.viewText(l)
		}
	}

	return sanitize(c.diff.ID)
}

// buttonKey gives the catalog key of the label of the button that allows.
func (c *consentOverlay) buttonKey() string {
	switch c.mode {
	case plugin.ViewUpdate:
		return "consent_btn_update"
	case plugin.ViewAllow:
		return "consent_btn_allow"
	}

	return "consent_btn_install"
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (c *consentOverlay) View(e *env, maxW, maxH int) []string {
	box, _ := c.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: a click on a button answers and a click on a "N more" line
// scrolls by a page.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (c *consentOverlay) Zones(e *env, maxW, maxH int) []zone {
	_, zs := c.draw(e, maxW, maxH)

	return zs
}

// draw lays out the box of the consent: the text, which scrolls, and the buttons under it, which
// stay on the last row of the box whatever its height.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
//
// Returns the lines of the box and the zones of the buttons and of the "N more" lines, counted
// from its top left corner.
func (c *consentOverlay) draw(e *env, maxW, maxH int) ([]string, []zone) {
	w := max(8, min(maxW, consentWidth))
	inner := w - 4
	height := max(2, maxH-2)

	fieldRows := helpWrapLines
	if height < consentSpacer+consentShort {
		fieldRows = 1
	}

	all := c.bodyLines(e, inner, fieldRows)

	// A title that is cut hides the name of the plugin, so the name opens the text.
	if e.Metric.width(c.title(e)) > w-6 {
		all = slices.Insert(all, 0, e.Theme.Bold.Render(e.Metric.fit(sanitize(c.diff.ID), inner,
			e.Glyphs.ellipsis)))
	}

	spacer := 0
	if height >= consentSpacer {
		spacer = 1
	}

	c.rows = max(1, min(len(all), height-1-spacer))
	c.total = len(all)

	win := windowOf(c.off, c.rows, c.total)
	c.off = win.off

	items := make([]string, win.count)
	for i := range items {
		items[i] = " " + e.clipPad(all[win.off+i], inner)
	}

	lines, zs := win.stack(e, items, " ", 1, w-2)
	lines = fitLines(lines, c.rows)
	zs = shiftZones(onlyMore(zs), 1)

	for range spacer {
		lines = append(lines, "")
	}

	row, bz := buttonRow(e, e.t("consent_btn_cancel"), e.t(c.buttonKey()), c.yes, inner, true)

	for _, z := range bz {
		z.y, z.x = 1+len(lines), z.x+2
		zs = append(zs, z)
	}

	lines = append(lines, " "+row)

	return floating(e, e.Theme.Bold.Render(c.title(e)), lines, w), zs
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (c *consentOverlay) Keys(e *env) []key.Binding {
	return consentBindings(e)
}

// consentBindings lists the keys of the consent.
//
// Parameters:
//   - e: shared environment
func consentBindings(e *env) []key.Binding {
	return []key.Binding{
		e.Keys.Apply,
		key.NewBinding(key.WithKeys("left", "right", "h", "l", "tab", "shift+tab"),
			key.WithHelp("h/l", e.t("ui_help_choose"))),
		key.NewBinding(key.WithKeys("esc", "n"), key.WithHelp("esc", e.t("ui_help_cancel"))),
		e.Keys.Scroll,
	}
}
