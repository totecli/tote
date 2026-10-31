package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	// wheelLines is the number of rows that one notch of the mouse wheel scrolls.
	wheelLines = 3

	// doubleClickWindow is the longest time between two clicks on the same row for them to count
	// as a double click.
	doubleClickWindow = 400 * time.Millisecond
)

// zone is a span of one line of a screen or of an overlay that reacts to a click.
//
// Y is the line and X the first cell, counted from the top left corner of the block that drew
// it (the page for a screen, the box for an overlay), W the number of cells and ID what the
// screen or overlay calls the thing under the mouse: the index of a record, of an entry, of a
// field or of a button. A screen gives its zones from the same function that draws its lines, so
// a frame and its hit-testing cannot drift apart.
type zone struct {
	y  int
	x  int
	w  int
	id int
}

// hit is a zone placed on the terminal: Y and X are absolute cells. A hit with a Press is a hint
// of the footer and presses that key; any other hit is a zone of the screen or of the overlay on
// top, whose ID is given back to it.
type hit struct {
	y     int
	x     int
	w     int
	id    int
	press string
}

// itemClickMsg tells the screen or the overlay on top that the zone with an ID was clicked.
// Double is true for the second click of a double click.
type itemClickMsg struct {
	ID     int
	Double bool
}

// wheelMsg tells the screen or the overlay on top to scroll by a number of rows: negative to go
// up and positive to go down.
type wheelMsg struct {
	Lines int
}

// clickMark remembers the last click, to tell a double click.
//
// At is the time of the click, Depth the number of overlays then, X and Y the cell and ID the zone
// it hit. Valid is true while a second click on the same zone, within doubleClickWindow, is a
// double click: a key, the wheel, a change of screen or of overlay all end it. Settled is true
// when the click pressed a hint of the footer, or when what is on screen changed after it: a click
// on the same cell within the window is then the tail of a double click that has done its work,
// and it is dropped instead of acting on what the first one revealed.
type clickMark struct {
	at      time.Time
	depth   int
	x       int
	y       int
	id      int
	valid   bool
	settled bool
}

// zonedScreen is implemented by the screens that react to a click. Zones returns the same
// zones as the lines that View draws for the same height.
type zonedScreen interface {
	Zones(e *env, h int) []zone
}

// zonedOverlay is implemented by the overlays that react to a click. Zones returns the zones of
// the box that View draws for the same limits.
type zonedOverlay interface {
	Zones(e *env, maxW, maxH int) []zone
}

// shiftZones moves zones down.
//
// Parameters:
//   - zs: zones to move
//   - by: number of lines to add to each one
func shiftZones(zs []zone, by int) []zone {
	for i := range zs {
		zs[i].y += by
	}

	return zs
}

// clipZones drops the zones that are not on one of the first n lines.
//
// Parameters:
//   - zs: zones to filter
//   - n: number of lines that are drawn
func clipZones(zs []zone, n int) []zone {
	out := zs[:0]

	for _, z := range zs {
		if z.y >= 0 && z.y < n {
			out = append(out, z)
		}
	}

	return out
}

// keyPress builds the key press for the name of a simple key.
//
// Parameters:
//   - name: "esc", "enter", "tab", "ctrl+" and a lowercase letter, or a single printable ASCII
//     character
//
// Returns the key press and true, or the zero value and false for any other name.
func keyPress(name string) (tea.KeyPressMsg, bool) {
	switch name {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}, true
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}, true
	}

	if len(name) == 6 && name[:5] == "ctrl+" && name[5] >= 'a' && name[5] <= 'z' {
		return tea.KeyPressMsg{Code: rune(name[5]), Mod: tea.ModCtrl}, true
	}

	if len(name) == 1 && name[0] > ' ' && name[0] < 0x7f {
		return tea.KeyPressMsg{Code: rune(name[0]), Text: name}, true
	}

	return tea.KeyPressMsg{}, false
}

// mouseOn reports whether the interface asks the terminal for mouse events: the setting ui.mouse
// is on and the terminal can report them, see mouseTerm.
func (r *root) mouseOn() bool {
	return r.e.Settings.UI.Mouse && r.mousable
}

// mouseReady reports whether a mouse event is for the interface now: the mouse is on, no program
// owns the terminal and the terminal is big enough to draw something.
func (r *root) mouseReady() bool {
	return r.mouseOn() && !r.guard.running() && r.e.Layout != layoutTiny
}

// hitAt finds what is under a cell of the terminal.
//
// Parameters:
//   - x: column, 0 for the first
//   - y: row, 0 for the first
//
// Returns the hit and true. The hits are the ones of the frame that render draws, see compose;
// the last one wins, so what is drawn on top is found first.
func (r *root) hitAt(x, y int) (hit, bool) {
	_, hits := r.compose(true)

	return topHit(hits, cellPos{x: x, y: y})
}

// topHit finds the hit that is drawn on top at a cell. A click and the hover both search with it,
// so that what is highlighted is what a click there acts on.
//
// Parameters:
//   - hits: hits in drawing order, see root.composeFrame
//   - at: the cell
//
// Returns the last hit that covers the cell and true, or the zero value and false for a cell that
// nothing covers.
func topHit(hits []hit, at cellPos) (hit, bool) {
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		if h.y == at.y && at.x >= h.x && at.x < h.x+h.w {
			return h, true
		}
	}

	return hit{}, false
}

// click handles the press of the left button. It acts on nothing yet: a press can be the start of
// a drag that selects text, which must not press a hint or open a screen on its way. The click
// happens at the release, see clickAt. Presses with a modifier key, with another button, and
// presses while a program runs are ignored.
//
// Parameters:
//   - m: the press
func (r *root) click(m tea.MouseClickMsg) tea.Cmd {
	if !r.mouseReady() || m.Button != tea.MouseLeft || m.Mod != 0 {
		return nil
	}

	r.disarm()
	r.pressAt(m.X, m.Y)

	return nil
}

// clickAt handles a click: a press and a release of the left button on the same cell, with no
// move between.
//
// A click on a hint of the footer presses its key. A click on a zone of the screen, or of the
// overlay on top, is given to it, as a double click when it is the second click on the same zone
// within doubleClickWindow and nothing happened in between, see clickMark. A click anywhere else,
// outside an overlay in particular, does nothing: it never closes an overlay, so that nothing typed
// in a form is lost by a stray click. The click that follows a double click's first on the same
// cell is dropped when the first one changed what is on screen.
//
// Parameters:
//   - x: column of the click
//   - y: row of the click
func (r *root) clickAt(x, y int) tea.Cmd {
	if r.e.Handing {
		return nil
	}

	now := r.e.Now()
	last := r.clicked

	if last.settled && last.x == x && last.y == y && now.Sub(last.at) <= doubleClickWindow {
		return nil
	}

	h, ok := r.hitAt(x, y)
	if !ok {
		r.clicked = clickMark{}

		return nil
	}

	if h.press != "" {
		r.clicked = clickMark{at: now, x: x, y: y, settled: true}

		k, _ := keyPress(h.press)
		_, cmd := r.key(k)

		return cmd
	}

	double := last.valid && last.depth == len(r.overlays) && last.y == h.y && last.id == h.id &&
		now.Sub(last.at) <= doubleClickWindow

	r.clicked = clickMark{at: now, depth: len(r.overlays), x: x, y: y, id: h.id, valid: !double}

	return r.forward(itemClickMsg{ID: h.id, Double: double})
}

// moved tells that what is on screen changed: a screen opened or an overlay was pushed or
// popped. A double click cannot go on across it, and a click on the cell of the last click right
// after it is dropped, see clickMark.
func (r *root) moved() {
	if r.clicked.at.IsZero() {
		return
	}

	r.clicked.valid = false
	r.clicked.settled = true
}

// wheel handles the mouse wheel: the screen, or the overlay on top, scrolls by wheelLines rows
// per notch wherever the pointer is.
//
// Parameters:
//   - m: the wheel event; only the vertical wheel counts
func (r *root) wheel(m tea.MouseWheelMsg) tea.Cmd {
	if !r.mouseReady() {
		return nil
	}

	r.forgetSelection()
	r.disarm()
	r.clicked.valid = false

	switch m.Button {
	case tea.MouseWheelUp:
		return r.forward(wheelMsg{Lines: -wheelLines})
	case tea.MouseWheelDown:
		return r.forward(wheelMsg{Lines: wheelLines})
	}

	return nil
}
