package ui

// glyphs is the set of symbols the interface draws with.
//
// Every symbol that is not plain ASCII is declared here, with its ASCII replacement, and no
// widget writes a glyph literal. The ASCII set is used for TERM=linux or dumb, a locale that is
// not UTF-8, and the --ascii option. Marker points at the selected row, Sep separates the items
// of a line, UpDown, LeftRight and Enter label the navigation keys in the footer, Prev and Next
// are the angle quotes around the value of a setting that the left and right keys change, Arrow
// precedes an exact command, Cursor ends the text of the filter and Warn flags a record that
// cannot be used. Above and Below are the arrows of the "N more" lines that tell a list goes on
// beyond its rows. Prompt is the logo of the program, Tl to Br and H and V draw boxes, Ml and Mr
// are the tees where a rule meets the sides of a box, and Logo is the bag of the welcome box:
// three lines of six cells. Times is the multiplication sign between the width and the height of
// a size. Dot is the bullet in front of the state of a plugin, which a role colors.
// Spin holds the frames of the spinner of a plugin that runs.
type glyphs struct {
	ascii     bool
	marker    string
	ellipsis  string
	sep       string
	upDown    string
	leftRight string
	prev      string
	next      string
	above     string
	below     string
	enter     string
	arrow     string
	cursor    string
	warn      string
	prompt    string
	crumb     string
	tl        string
	tr        string
	bl        string
	br        string
	h         string
	v         string
	ml        string
	mr        string
	logo      [3]string
	times     string
	dot       string
	spin      [4]string
}

// newGlyphs returns the symbols of a terminal.
//
// Parameters:
//   - ascii: true to get the plain ASCII set
func newGlyphs(ascii bool) glyphs {
	if ascii {
		return glyphs{
			ascii: true, marker: ">", ellipsis: "...", sep: "-", upDown: "up/down",
			leftRight: "left/right", prev: "<", next: ">", above: "^", below: "v",
			enter: "enter", arrow: "->", cursor: "_", warn: "!", prompt: ">_", crumb: ">",
			tl: "+", tr: "+", bl: "+", br: "+", h: "-", v: "|", ml: "+", mr: "+",
			logo: [3]string{" .--. ", ".+--+.", "' >_ '"}, times: "x", dot: "*",
			spin: [4]string{"|", "/", "-", "\\"},
		}
	}

	return glyphs{
		marker: "\u276F", ellipsis: "\u2026", sep: "\u00B7", upDown: "\u2191\u2193",
		leftRight: "\u2190\u2192", prev: "\u2039", next: "\u203A",
		above: "\u2191", below: "\u2193",
		enter: "\u23CE", arrow: "\u2192", cursor: "\u258F", warn: "!", prompt: "\u276F_",
		crumb: "\u203A",
		tl:    "\u256D", tr: "\u256E", bl: "\u2570", br: "\u256F", h: "\u2500", v: "\u2502",
		ml: "\u251C", mr: "\u2524",
		logo: [3]string{
			" \u256D\u2500\u2500\u256E ", "\u256D\u2534\u2500\u2500\u2534\u256E",
			"\u2570 \u276F_ \u256F",
		},
		times: "\u00D7", dot: "\u25CF",
		spin: [4]string{"\u280B", "\u2839", "\u283C", "\u2827"},
	}
}
