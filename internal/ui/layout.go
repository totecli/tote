package ui

import "github.com/totecli/tote/internal/config"

const (
	// minWidth is the narrowest terminal that can show the interface.
	minWidth = 24

	// minHeight is the shortest terminal that can show the interface.
	minHeight = 7

	// neverWidth is the narrowest terminal that keeps the full layout for ui.compact "never".
	neverWidth = 40

	// neverHeight is the shortest terminal that keeps the full layout for ui.compact "never": its
	// headings leave three rows to a list there.
	neverHeight = 10

	// fullWidth is the narrowest terminal that shows the welcome box in automatic mode.
	fullWidth = 70

	// fullHeight is the shortest terminal that shows the welcome box in automatic mode.
	fullHeight = 20

	// panelHeight is the shortest terminal that shows the detail panel under a list.
	panelHeight = 24

	// pageWidthNarrow is the widest the page is drawn for ui.width "120".
	pageWidthNarrow = 120

	// pageWidthWide is the widest the page is drawn for ui.width "160".
	pageWidthWide = 160

	// spareColumns is the number of cells at the right of a row that the selection never paints,
	// so that a terminal that counts a character wider than the renderer cannot wrap the line.
	spareColumns = 2
)

// pageGeometry decides how wide the page is and where it starts. It is the one place that turns
// the ui.width setting into a width: the page is the welcome box, the lists, the detail panel, the
// status line and the footer.
//
// Parameters:
//   - cols: width of the terminal in cells
//   - setting: value of ui.width, "auto", "120" or "160"; anything else counts as "auto"
//
// Returns the width of the page, which is the terminal width up to the cap of the setting (120
// or 160 cells, no cap for "auto"), and the number of blank cells at its left, half of what the
// terminal has beyond the page, rounded down. A terminal narrower than the cap gets a page as
// wide as itself and no margin. The overlays are centered on the terminal, which is the center of
// the page, so that everything shares one rule.
func pageGeometry(cols int, setting string) (int, int) {
	w := cols

	switch setting {
	case "120":
		w = min(cols, pageWidthNarrow)
	case "160":
		w = min(cols, pageWidthWide)
	}

	return w, max(0, (cols-w)/2)
}

// layoutMode is how much of the interface fits on the terminal.
type layoutMode int

const (
	// layoutFull shows the welcome box, the column headings and the detail panel.
	layoutFull layoutMode = iota

	// layoutCompact shows a one-line header and drops the decoration.
	layoutCompact

	// layoutTiny only shows a message asking for a bigger terminal.
	layoutTiny
)

// chooseLayout decides the layout for a terminal size.
//
// Parameters:
//   - w: width in cells
//   - h: height in rows
//   - ui: the [ui] settings; compact "auto" switches to the compact layout below 70 columns or
//     20 rows, "always" and welcome "compact" always use it, and "never" keeps the full layout
//     down to neverWidth columns and neverHeight rows, where the headings of the full layout
//     would leave no row to the lists
//
// Returns layoutTiny below minWidth columns or minHeight rows whatever the settings say.
func chooseLayout(w, h int, ui config.UIConfig) layoutMode {
	switch {
	case w < minWidth || h < minHeight:
		return layoutTiny
	case ui.Compact == "always" || ui.Welcome == "compact":
		return layoutCompact
	case ui.Compact == "never":
		if w < neverWidth || h < neverHeight {
			return layoutCompact
		}

		return layoutFull
	case w < fullWidth || h < fullHeight:
		return layoutCompact
	}

	return layoutFull
}
