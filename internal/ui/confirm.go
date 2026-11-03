package ui

import (
	"cmp"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/store"
)

// confirmWidth is the widest the box of a confirmation gets, in cells.
const confirmWidth = 56

const (
	// confirmNo is the zone of the button "No".
	confirmNo = iota

	// confirmYes is the zone of the button "Yes".
	confirmYes
)

// confirm is the overlay that asks before a record is deleted.
//
// Key is the raw key of the record and Yes tells which button has the focus. The focus starts on
// "No", so that Enter alone never deletes anything.
type confirm struct {
	kind kind
	key  string
	yes  bool
}

// newConfirm returns the overlay that asks to confirm the deletion of a record.
//
// Parameters:
//   - k: kind of the record
//   - key: raw key of the record to delete. The overlay deletes it with removeCmd, which
//     reports the outcome to the root, and keeps "No" as the default answer
func newConfirm(k kind, key string) overlay {
	return &confirm{kind: k, key: key}
}

// confirmKey returns the catalog key of the question asked for a kind.
//
// Parameters:
//   - k: kind of the record
func confirmKey(k kind) string {
	return [...]string{
		"ui_confirm_connection", "ui_confirm_repo", "ui_confirm_alias", "ui_confirm_func",
	}[k]
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (c *confirm) Init(_ *env) tea.Cmd {
	return nil
}

// Update implements overlay: y deletes, n and Esc keep the record, the arrow keys and Tab change
// the button and Enter picks the one that has the focus.
//
// Parameters:
//   - e: shared environment, which gives the data
//   - msg: key press or dismissal; other messages are ignored
func (c *confirm) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return c, nil, true
	case itemClickMsg:
		switch m.ID {
		case confirmNo:
			return c, nil, true
		case confirmYes:
			return c, removeCmd(e, c.kind, c.key), true
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "y", "Y":
			return c, removeCmd(e, c.kind, c.key), true
		case "n", "N", "esc":
			return c, nil, true
		case "enter":
			if c.yes {
				return c, removeCmd(e, c.kind, c.key), true
			}

			return c, nil, true
		case "left", "right", "h", "l", "tab", "shift+tab":
			c.yes = !c.yes
		}
	}

	return c, nil, false
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (c *confirm) View(e *env, maxW, maxH int) []string {
	box, _ := c.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: a click on a button answers.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (c *confirm) Zones(e *env, maxW, maxH int) []zone {
	_, zs := c.draw(e, maxW, maxH)

	return zs
}

// draw lays out the box of the confirmation.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed; a box that does not have the height for all its lines drops the
//     blank line, then the target, before it shortens the question, see questionBox
//
// Returns the lines of the box and the zones of the two buttons, counted from its top left
// corner.
func (c *confirm) draw(e *env, maxW, maxH int) ([]string, []zone) {
	w := min(maxW, confirmWidth)
	name := sanitize(c.key)

	// The name is cut to what is left of one line, so that the short question is never the one
	// that loses it.
	room := max(1, w-4-e.Metric.width(e.t("ui_confirm_short", "")))

	return questionBox(e, w, maxH, e.t("ui_confirm_title"), e.t(confirmKey(c.kind), name),
		e.t("ui_confirm_short", e.Metric.fit(name, room, e.Glyphs.ellipsis)), c.details(e), c.yes)
}

// questionBox lays out a box that asks a question and offers the two buttons.
//
// The lines are the question, the details, a blank line and the buttons. When the height does not
// leave room for them all, the lines go in this order: the second of two details, the blank line,
// the second line of the question, then the details. The buttons and one line of the question
// always stay, and the buttons are drawn tighter when the width asks for it, see choiceRow.
//
// Parameters:
//   - e: shared environment
//   - maxW: width of the box, at least 4
//   - maxH: most lines allowed, borders included
//   - title: title of the box
//   - full: the question in full, wrapped on at most two lines
//   - short: the question in a few words, the name of the record and the question mark, used
//     when the full one needs more than two lines or when only one line is left for it; empty to
//     use the full one cut
//   - details: muted lines under the question, each wrapped on at most detailRows rows
//   - yes: true when "Yes" has the focus
//
// Returns the lines of the box and the zones of the two buttons, counted from its top left
// corner.
func questionBox(e *env, maxW, maxH int, title, full, short string, details []string,
	yes bool,
) ([]string, []zone) {
	w := max(4, maxW)
	inner := w - 4
	rows := max(1, maxH-2)
	pad := func(s string) string { return " " + e.clipPad(s, inner) + " " }

	brief := wrapWords(e, cmp.Or(short, full), inner, 2)

	text := wrapWords(e, full, inner, 8)
	if len(text) > 2 {
		text = brief
	}

	groups := make([][]string, len(details))
	for i, extra := range details {
		groups[i] = wrapWords(e, extra, inner, detailRows)
	}

	spacer := 1

	for 1+len(text)+lineCount(groups)+spacer > rows {
		switch {
		case len(groups) > 1:
			groups = groups[:1]
		case spacer > 0:
			spacer = 0
		case len(brief) < len(text):
			text = brief
		case len(groups) > 0:
			groups = nil
		case len(text) > 1:
			text = brief[:1]
		default:
			rows = 1 + len(text)
		}
	}

	var lines []string

	for _, l := range text {
		lines = append(lines, pad(e.Theme.Text.Render(l)))
	}

	for _, g := range groups {
		for _, extra := range g {
			lines = append(lines, pad(e.Theme.Muted.Render(extra)))
		}
	}

	if spacer > 0 {
		lines = append(lines, pad(""))
	}

	row, zs := choiceRow(e, yes, inner)
	lines = append(lines, pad(row))

	for i := range zs {
		zs[i].y = len(lines)
		zs[i].x += 2
	}

	return floating(e, e.Theme.Bold.Render(title), lines, w), zs
}

// detailRows is the most rows one detail of a question gets when it is wrapped.
const detailRows = 3

// lineCount counts the lines of groups of lines.
//
// Parameters:
//   - groups: the groups
func lineCount(groups [][]string) int {
	n := 0
	for _, g := range groups {
		n += len(g)
	}

	return n
}

// details returns the lines that tell what is about to disappear.
//
// Parameters:
//   - e: shared environment, which gives the record
//
// Returns the main value of the record (target, path, command or script) and, for a function, the
// reminder that its script file stays. A record that is gone gives nothing.
func (c *confirm) details(e *env) []string {
	rec, ok := e.Data.record(c.kind, c.key)
	if !ok {
		return nil
	}

	switch r := rec.(type) {
	case store.Connection:
		return []string{sanitize(r.Target)}
	case store.Repo:
		return []string{sanitize(r.Path)}
	case store.Alias:
		return []string{sanitize(r.Cmd)}
	case store.Func:
		return []string{sanitize(r.Script), e.t("ui_confirm_script_kept")}
	}

	return nil
}

// choiceRow draws the two answers of a question side by side, the one with the focus highlighted.
//
// Parameters:
//   - e: shared environment
//   - yes: true when "Yes" has the focus
//   - inner: cells available; when the buttons do not fit with three cells between them, the gap
//     shrinks to one, then the buttons lose the spaces inside their brackets
//
// Returns the line and the zones of the buttons, with the cells counted from the start of the
// line and the line of the zones left at 0.
func choiceRow(e *env, yes bool, inner int) (string, []zone) {
	return buttonRow(e, e.t("ui_choice_no"), e.t("ui_choice_yes"), yes, inner, false)
}

// buttonRow draws two buttons side by side, the one with the focus highlighted.
//
// Parameters:
//   - e: shared environment
//   - left: label of the first button, the one that cancels
//   - right: label of the second button
//   - second: true when the second button has the focus
//   - inner: cells available; when the buttons do not fit with three cells between them, the gap
//     shrinks to one, then the buttons lose the spaces inside their brackets
//   - shrink: true to cut the labels, each to its share of the room, when the buttons still do
//     not fit, so that both stay whole buttons
//
// Returns the line and the zones of the buttons, with the cells counted from the start of the
// line and the line of the zones left at 0.
func buttonRow(e *env, left, right string, second bool, inner int, shrink bool) (string, []zone) {
	no := "[ " + left + " ]"
	ok := "[ " + right + " ]"
	gap := 3

	if e.Metric.width(no)+gap+e.Metric.width(ok) > inner {
		gap = 1
	}

	if e.Metric.width(no)+gap+e.Metric.width(ok) > inner {
		no, ok = "["+left+"]", "["+right+"]"
	}

	if over := e.Metric.width(no) + gap + e.Metric.width(ok) - inner; shrink && over > 0 {
		room := max(2, inner-4-gap)
		half := room / 2
		lw := min(e.Metric.width(left), max(1, half))
		rw := min(e.Metric.width(right), max(1, room-lw))
		no = "[" + e.Metric.fit(left, lw, e.Glyphs.ellipsis) + "]"
		ok = "[" + e.Metric.fit(right, rw, e.Glyphs.ellipsis) + "]"
	}

	nw, yw := e.Metric.width(no), e.Metric.width(ok)
	lead := max(0, (inner-nw-gap-yw)/2)

	zs := []zone{
		{x: lead, w: nw, id: confirmNo},
		{x: lead + nw + gap, w: yw, id: confirmYes},
	}

	if second {
		no, ok = e.Theme.Text.Render(no), e.Theme.Selected.Render(ok)
	} else {
		no, ok = e.Theme.Selected.Render(no), e.Theme.Text.Render(ok)
	}

	return spaces(lead) + no + spaces(gap) + ok, zs
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (c *confirm) Keys(e *env) []key.Binding {
	return confirmBindings(e)
}

// confirmBindings lists the keys of the confirmation.
//
// Parameters:
//   - e: shared environment
func confirmBindings(e *env) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", e.t("ui_help_yes"))),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", e.t("ui_help_no"))),
		key.NewBinding(key.WithKeys("left", "right", "h", "l"),
			key.WithHelp("h/l", e.t("ui_help_choose"))),
		e.Keys.Apply,
	}
}
