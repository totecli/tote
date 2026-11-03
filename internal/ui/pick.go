package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// pickWidth is the widest the box of a list of choices gets, in cells.
const pickWidth = 48

// pickItem is one line of a list of choices.
//
// Label is the translated text, Value what the choice gives back and Note a short muted text at
// the right, such as "current". Off draws the choice muted: the caller has to refuse it.
type pickItem struct {
	label string
	value string
	note  string
	off   bool
}

// pickOverlay is the overlay that asks for one value among a few.
//
// Cur is the item under the cursor and Off the first visible one. Done turns the value of the
// chosen item into the command to run; the overlay closes when an item is chosen or on Esc.
type pickOverlay struct {
	title string
	items []pickItem
	cur   int
	off   int
	done  func(e *env, value string) tea.Cmd
}

// newPick returns the overlay that picks one value in a list.
//
// Parameters:
//   - title: translated title of the box
//   - items: choices in order; at least one
//   - cur: index of the item that has the cursor at the start, brought back into range
//   - done: called with the value of the chosen item, returns the command to run
func newPick(title string, items []pickItem, cur int, done func(*env, string) tea.Cmd) overlay {
	return &pickOverlay{
		title: title, items: items, cur: max(0, min(cur, len(items)-1)),
		done: done,
	}
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (p *pickOverlay) Init(_ *env) tea.Cmd {
	return nil
}

// rows returns the number of items that fit in a box.
//
// Parameters:
//   - maxH: most lines the box may take
func (p *pickOverlay) rows(maxH int) int {
	return max(1, min(len(p.items), maxH-2))
}

// Update implements overlay.
//
// Parameters:
//   - e: shared environment
//   - msg: key press or dismissal; other messages are ignored
func (p *pickOverlay) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return p, nil, true
	case itemClickMsg:
		rows := p.rows(e.Height - 2)

		if by, ok := pageClick(m.ID, pageOf(p.off, rows, len(p.items))); ok {
			p.cur, p.off = scrollRows(p.cur, p.off, rows, len(p.items), by)

			return p, nil, false
		}

		if m.ID < 0 || m.ID >= len(p.items) {
			return p, nil, false
		}

		p.move(m.ID-p.cur, rows)

		if m.Double {
			return p, p.done(e, p.items[p.cur].value), true
		}
	case wheelMsg:
		p.cur, p.off = scrollRows(p.cur, p.off, p.rows(e.Height-2), len(p.items), m.Lines)
	case tea.KeyPressMsg:
		rows := p.rows(e.Height - 2)

		switch {
		case key.Matches(m, e.Keys.Close):
			return p, nil, true
		case key.Matches(m, e.Keys.Apply):
			return p, p.done(e, p.items[p.cur].value), true
		case key.Matches(m, e.Keys.Up):
			p.move(-1, rows)
		case key.Matches(m, e.Keys.Down):
			p.move(1, rows)
		case key.Matches(m, e.Keys.Home):
			p.move(-len(p.items), rows)
		case key.Matches(m, e.Keys.End):
			p.move(len(p.items), rows)
		}
	}

	return p, nil, false
}

// move changes the item under the cursor and scrolls to keep it visible.
//
// Parameters:
//   - by: number of items to move, negative to go up
//   - rows: number of visible items
func (p *pickOverlay) move(by, rows int) {
	p.cur = max(0, min(p.cur+by, len(p.items)-1))
	p.off = scrollTo(p.cur, p.off, rows, len(p.items))
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (p *pickOverlay) View(e *env, maxW, maxH int) []string {
	box, _ := p.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: the visible items can be clicked.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (p *pickOverlay) Zones(e *env, maxW, maxH int) []zone {
	_, zs := p.draw(e, maxW, maxH)

	return zs
}

// minNote is the fewest cells a note beside a choice keeps; a box too narrow for that shows the
// labels alone.
const minNote = 3

// draw lays out the box of the choices.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
//
// Returns the lines of the box and the zones of the items, counted from its top left corner. A
// list longer than the box has a muted "N more" line on its first or last row.
func (p *pickOverlay) draw(e *env, maxW, maxH int) ([]string, []zone) {
	w := max(4, min(maxW, pickWidth))
	inner := w - 2
	rows := p.rows(maxH)
	win := windowOf(scrollTo(p.cur, p.off, rows, len(p.items)), rows, len(p.items))
	p.off = win.off

	noteW, labelW := 0, 0
	for _, it := range p.items {
		noteW = max(noteW, e.Metric.width(it.note))
		labelW = max(labelW, e.Metric.width(it.label))
	}

	// The notes give way to the labels: a note that cannot keep minNote cells is left out.
	if noteW = min(noteW, inner-4-columnGap-labelW); noteW < minNote {
		noteW = 0
	}

	box := *e
	box.Layout = layoutCompact

	items := make([]string, 0, win.count)

	for i := win.off; i < win.off+win.count; i++ {
		it := p.items[i]
		cells := []cell{{text: it.label, w: max(1, inner-4), muted: it.off}}

		if noteW > 0 {
			cells[0].w = max(1, inner-4-noteW-columnGap)
			cells = append(cells, cell{text: it.note, w: noteW, muted: true})
		}

		items = append(items, box.renderBoxRow(i == p.cur, false, cells, inner))
	}

	lines, zs := win.stack(e, items, spaces(box.indent()+2), 1, inner)

	return floating(e, e.Theme.Bold.Render(p.title), lines, w), shiftZones(zs, 1)
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (p *pickOverlay) Keys(e *env) []key.Binding {
	return pickBindings(e)
}

// pickBindings lists the keys of a list of choices.
//
// Parameters:
//   - e: shared environment
func pickBindings(e *env) []key.Binding {
	choose := e.Keys.Apply
	choose.SetHelp(e.Glyphs.enter, e.t("ui_help_choose"))

	return []key.Binding{choose, e.Keys.Close, e.Keys.Nav}
}
