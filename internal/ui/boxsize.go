package ui

const (
	// boxShare is the share of the terminal, in percent, that a box may grow to when its content
	// asks for more room than the base size: of the width for a list and the palette, and of the
	// height for the palette.
	boxShare = 60

	// pickMaxWidth is the widest a list of choices grows to on a very wide terminal, in cells.
	pickMaxWidth = 110

	// paletteMaxWidth is the widest the palette grows to on a very wide terminal, in cells.
	paletteMaxWidth = 160

	// paletteMaxHeight is the tallest the palette grows to on a very tall terminal, in lines.
	paletteMaxHeight = 40

	// rowMargins is the number of cells of an item row that are not its text: the lead, the
	// marker and the space after it, and the space at the right.
	rowMargins = 4

	// borderCells is the number of cells that the two sides of a box take.
	borderCells = 2

	// titleMargins is the number of cells that a box keeps around a title set in its top border,
	// the borders included, so that the title is not cut: see boxWith.
	titleMargins = 6

	// infoMargins is the number of cells around the information line of a list, the borders
	// included: a space on each side.
	infoMargins = 4

	// infoIndent is the number of cells of the information line that a box keeps blank at each
	// end.
	infoIndent = infoMargins / 2
)

// boxWidth returns the width of a box that adapts to the terminal: as wide as its content needs,
// at least the base width, at most boxShare percent of the terminal or the limit, and never above
// maxW.
//
// Parameters:
//   - base: width of the box when the content is short, in cells
//   - need: width that the content asks for, borders included, 0 when unknown
//   - limit: widest the box grows to, in cells
//   - screen: width of the terminal in cells, 0 when unknown, which gives the base width
//   - maxW: widest the box may be, whatever the rest
func boxWidth(base, need, limit, screen, maxW int) int {
	w := max(base, min(need, limit, screen*boxShare/100))

	return max(4, min(maxW, w))
}

// paletteBoxHeight returns the number of lines of the palette box: at least paletteHeight, up to
// boxShare percent of maxH and paletteMaxHeight, and never above maxH.
//
// Parameters:
//   - maxH: most lines the box may take
func paletteBoxHeight(maxH int) int {
	grown := min(paletteMaxHeight, maxH*boxShare/100)

	return min(maxH, max(paletteHeight, grown))
}
