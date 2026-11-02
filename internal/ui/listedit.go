package ui

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// listWidth is the widest the box of the list editor gets, in cells.
const listWidth = 56

// listEditedMsg gives the items of a list back to the form that opened the editor. ID is the id of
// the field.
type listEditedMsg struct {
	ID    string
	Items []string
}

// listItemMsg is the answer of the prompt that reads one item. Index is the item to replace, or
// -1 for a new item that goes after the one under the cursor.
type listItemMsg struct {
	Index int
	Text  string
}

// listEditor is the overlay that edits a list of texts: it adds, changes, removes and reorders the
// items. Nothing leaves the editor until the user applies the list; Esc drops the draft, after the
// question when it differs from the list the editor opened with.
//
// ID is the id of the field the items go back to and Base the list as the editor opened. Check
// gives the translated reason one item is refused and CheckAll the one for the whole list, both ""
// to accept. Cur is the item under the cursor, Off the first visible one and Msg the reason the
// last apply was refused.
type listEditor struct {
	title    string
	id       string
	items    []string
	base     []string
	cur      int
	off      int
	msg      string
	check    func(e *env, item string) string
	checkAll func(e *env, items []string) string
}

// newListEditor returns the overlay that edits a list of texts.
//
// Parameters:
//   - title: translated title of the box
//   - id: id of the field that gets the items back
//   - items: the list as it is
//   - check: gives the reason an item is refused, "" to accept it; may be nil
//   - checkAll: gives the reason the list is refused, "" to accept it; may be nil
func newListEditor(title, id string, items []string, check func(*env, string) string,
	checkAll func(*env, []string) string,
) overlay {
	return &listEditor{
		title: title, id: id, items: slices.Clone(items), base: slices.Clone(items),
		check: check, checkAll: checkAll,
	}
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (l *listEditor) Init(_ *env) tea.Cmd {
	return nil
}

// Dirty implements dirtier: it reports that the draft differs from the list the editor opened with.
func (l *listEditor) Dirty() bool {
	return !slices.Equal(l.items, l.base)
}

// rows returns the number of items that fit in a box.
//
// Parameters:
//   - maxH: most lines the box may take
func (l *listEditor) rows(maxH int) int {
	return max(1, min(max(1, len(l.items)), maxH-2-l.extra()))
}

// extra is the number of lines under the items: the reason the last apply was refused.
func (l *listEditor) extra() int {
	if l.msg != "" {
		return 1
	}

	return 0
}

// move changes the item under the cursor and scrolls to keep it visible.
//
// Parameters:
//   - by: number of items to move, negative to go up
//   - rows: number of visible items
func (l *listEditor) move(by, rows int) {
	l.cur = max(0, min(l.cur+by, len(l.items)-1))
	l.off = scrollTo(l.cur, l.off, rows, len(l.items))
}

// Update implements overlay.
//
// Parameters:
//   - e: shared environment
//   - msg: key press, click, wheel, the answer of the prompt or a dismissal; other messages are
//     ignored
func (l *listEditor) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	rows := l.rows(e.Height - 2)

	switch m := msg.(type) {
	case dismissMsg:
		return l, nil, true
	case listItemMsg:
		l.put(m)
	case itemClickMsg:
		if by, ok := pageClick(m.ID, pageOf(l.off, rows, len(l.items))); ok {
			l.cur, l.off = scrollRows(l.cur, l.off, rows, len(l.items), by)

			return l, nil, false
		}

		if m.ID < 0 || m.ID >= len(l.items) {
			return l, nil, false
		}

		l.move(m.ID-l.cur, rows)

		if m.Double {
			return l, l.prompt(l.cur), false
		}
	case wheelMsg:
		l.cur, l.off = scrollRows(l.cur, l.off, rows, len(l.items), m.Lines)
	case tea.KeyPressMsg:
		return l.key(e, m, rows)
	}

	return l, nil, false
}

// key handles a key press.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
//   - rows: number of visible items
func (l *listEditor) key(e *env, k tea.KeyPressMsg, rows int) (overlay, tea.Cmd, bool) {
	switch {
	case k.String() == "ctrl+s":
		return l.apply(e)
	case key.Matches(k, e.Keys.Close):
		return l, nil, true
	case key.Matches(k, e.Keys.Up):
		l.move(-1, rows)
	case key.Matches(k, e.Keys.Down):
		l.move(1, rows)
	case key.Matches(k, e.Keys.PageUp):
		l.move(-rows, rows)
	case key.Matches(k, e.Keys.PageDown):
		l.move(rows, rows)
	case key.Matches(k, e.Keys.Home):
		l.move(-len(l.items), rows)
	case key.Matches(k, e.Keys.End):
		l.move(len(l.items), rows)
	case key.Matches(k, e.Keys.New):
		return l, l.prompt(-1), false
	case key.Matches(k, e.Keys.Edit), key.Matches(k, e.Keys.Change):
		if len(l.items) > 0 {
			return l, l.prompt(l.cur), false
		}

		return l, l.prompt(-1), false
	case key.Matches(k, e.Keys.Delete):
		l.drop(rows)
	case key.Matches(k, e.Keys.Shift):
		l.shift(k.String() == "K" || k.String() == "shift+up", rows)
	}

	return l, nil, false
}

// prompt opens the box that reads one item.
//
// Parameters:
//   - at: index of the item to change, -1 for a new item
func (l *listEditor) prompt(at int) tea.Cmd {
	initial := ""
	if at >= 0 && at < len(l.items) {
		initial = l.items[at]
	}

	pr := newPrompt(l.title, "", initial, func(e *env, text string) string {
		if text == "" || l.check == nil {
			return ""
		}

		return l.check(e, text)
	}, func(_ *env, text string) tea.Cmd {
		if text == "" {
			return nil
		}

		return msgCmd(listItemMsg{Index: at, Text: text})
	})

	return msgCmd(openOverlayMsg{Overlay: pr})
}

// put stores an item the prompt gave back.
//
// Parameters:
//   - m: the item and where it goes
func (l *listEditor) put(m listItemMsg) {
	l.msg = ""

	if m.Index >= 0 && m.Index < len(l.items) {
		l.items[m.Index] = m.Text

		return
	}

	at := min(len(l.items), l.cur+1)
	if len(l.items) == 0 {
		at = 0
	}

	l.items = slices.Insert(l.items, at, m.Text)
	l.cur = at
}

// drop removes the item under the cursor from the draft.
//
// Parameters:
//   - rows: number of visible items
func (l *listEditor) drop(rows int) {
	if len(l.items) == 0 {
		return
	}

	l.msg = ""
	l.items = slices.Delete(l.items, l.cur, l.cur+1)
	l.move(0, rows)
}

// shift moves the item under the cursor up or down, and the cursor with it.
//
// Parameters:
//   - up: true to move it up
//   - rows: number of visible items
func (l *listEditor) shift(up bool, rows int) {
	to := l.cur + 1
	if up {
		to = l.cur - 1
	}

	if to < 0 || to >= len(l.items) {
		return
	}

	l.items[l.cur], l.items[to] = l.items[to], l.items[l.cur]
	l.msg = ""
	l.move(to-l.cur, rows)
}

// apply checks the list and gives it back to the form that opened the editor.
//
// Parameters:
//   - e: shared environment
func (l *listEditor) apply(e *env) (overlay, tea.Cmd, bool) {
	if l.checkAll != nil {
		if reason := l.checkAll(e, l.items); reason != "" {
			l.msg = reason

			return l, nil, false
		}
	}

	return l, msgCmd(listEditedMsg{ID: l.id, Items: slices.Clone(l.items)}), true
}

// Typing reports that the editor takes letters as commands of its own, so that the keys of the
// screens stay out of it.
func (l *listEditor) Typing() bool {
	return true
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (l *listEditor) View(e *env, maxW, maxH int) []string {
	box, _ := l.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: the visible items can be clicked.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (l *listEditor) Zones(e *env, maxW, maxH int) []zone {
	_, zs := l.draw(e, maxW, maxH)

	return zs
}

// draw lays out the box of the editor.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
//
// Returns the lines of the box and the zones of the items, counted from its top left corner.
func (l *listEditor) draw(e *env, maxW, maxH int) ([]string, []zone) {
	w := max(4, min(maxW, listWidth))
	inner := w - 2
	rows := l.rows(maxH)
	box := *e
	box.Layout = layoutCompact

	var (
		lines []string
		zs    []zone
	)

	if len(l.items) == 0 {
		lines = []string{box.renderBoxRow(false, false, []cell{{
			text: e.t("cli_plugin_none"),
			w:    max(1, inner-4), muted: true,
		}}, inner)}
	} else {
		l.off = scrollTo(l.cur, l.off, rows, len(l.items))
		win := windowOf(l.off, rows, len(l.items))
		l.off = win.off

		items := make([]string, 0, win.count)

		for i := win.off; i < win.off+win.count; i++ {
			items = append(items, box.renderBoxRow(i == l.cur, false,
				[]cell{{text: sanitize(l.items[i]), w: max(1, inner-4)}}, inner))
		}

		lines, zs = win.stack(e, items, spaces(box.indent()+2), 1, inner)
		zs = shiftZones(zs, 1)
	}

	if l.msg != "" {
		lines = append(lines, " "+e.Theme.Err.Render(e.Metric.fit(e.Glyphs.warn+" "+l.msg, inner-1,
			e.Glyphs.ellipsis)))
	}

	return floating(e, e.Theme.Bold.Render(e.Metric.fit(l.title, max(1, w-8), e.Glyphs.ellipsis)),
		lines, w), zs
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (l *listEditor) Keys(e *env) []key.Binding {
	return editorBindings(e)
}

// editorBindings lists the keys of the list editor.
//
// Parameters:
//   - e: shared environment
func editorBindings(e *env) []key.Binding {
	edit := e.Keys.Edit
	edit.SetKeys("e", "enter")

	return []key.Binding{
		edit, e.Keys.New, e.Keys.Delete, e.Keys.Shift,
		key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", e.t("ui_help_apply_list"))),
		e.Keys.Close, e.Keys.Nav,
	}
}
