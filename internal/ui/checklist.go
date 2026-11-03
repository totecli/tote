package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// checkItem is one line of a checklist: a label and whether it is ticked. A checklist always
// starts with every line cleared, whatever Checked says when it is built.
type checkItem struct {
	Label   string
	Checked bool
}

// checklist is the widget of checkboxes used by the screens that ask which parts to act on. It
// is not an overlay: the screen or overlay that owns it forwards the keys and draws its lines.
// Up and Down move the cursor, Space ticks the line, "a" ticks or clears all of them and Enter
// confirms the choice, which Done then reports. Nothing is ticked at the start, so that a screen
// that acts on the ticked lines never acts on a line the user did not choose.
type checklist struct {
	items []checkItem
	cur   int
	done  bool
}

// newChecklist returns a checklist.
//
// Parameters:
//   - items: lines to show; the labels are drawn as they are, so they must be sanitized. The
//     Checked flags are ignored: every line starts cleared
func newChecklist(items []checkItem) checklist {
	out := append([]checkItem(nil), items...)

	for i := range out {
		out[i].Checked = false
	}

	return checklist{items: out}
}

// Update handles a key.
//
// Parameters:
//   - e: shared environment
//   - msg: message to handle; only key presses matter
//
// Returns the updated checklist.
func (c checklist) Update(e *env, msg tea.Msg) checklist {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return c
	}

	if k.String() == "enter" {
		c.done = true

		return c
	}

	if len(c.items) == 0 {
		return c
	}

	c.items = append([]checkItem(nil), c.items...)
	c.done = false

	switch {
	case key.Matches(k, e.Keys.Up):
		c.cur = max(0, c.cur-1)
	case key.Matches(k, e.Keys.Down):
		c.cur = min(len(c.items)-1, c.cur+1)
	case k.String() == "space" || k.String() == " ":
		c.items[c.cur].Checked = !c.items[c.cur].Checked
	case k.String() == "a":
		all := true
		for _, it := range c.items {
			all = all && it.Checked
		}

		for i := range c.items {
			c.items[i].Checked = !all
		}
	}

	return c
}

// Done reports whether Enter was pressed, which confirms the choice. Any other key clears it.
func (c checklist) Done() bool {
	return c.done
}

// Checked returns the indexes of the ticked lines.
func (c checklist) Checked() []int {
	var out []int

	for i, it := range c.items {
		if it.Checked {
			out = append(out, i)
		}
	}

	return out
}

// View draws the lines of the checklist.
//
// Parameters:
//   - e: shared environment
//   - w: width in cells
//
// Returns one line per item, each at most w cells.
func (c checklist) View(e *env, w int) []string {
	out := make([]string, len(c.items))

	for i, it := range c.items {
		box := "[ ]"
		if it.Checked {
			box = "[x]"
		}

		out[i] = e.renderRow(i == c.cur, false, []cell{
			{text: box, w: 3, muted: true},
			{text: it.Label, w: max(1, w-e.indent()-spareColumns-2-3-columnGap)},
		}, w)
	}

	return out
}
