package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// themeMode is the color scheme in use.
//
// The neutral mode draws no color at all: the default foreground, bold for emphasis and reverse
// video for the selection. It is used while the background of the terminal is unknown, when the
// terminal has no colors and for ui.theme = "mono".
type themeMode int

const (
	// modeNeutral draws without color.
	modeNeutral themeMode = iota

	// modeDark is the palette for a dark background.
	modeDark

	// modeLight is the palette for a light background.
	modeLight
)

// tone is one color given explicitly for each profile, so that no value is ever converted
// automatically. An empty string means the default foreground of the terminal.
//
// ansi is a number from 0 to 15, c256 a number from 0 to 255 and rgb a #RRGGBB value.
type tone struct {
	ansi string
	c256 string
	rgb  string
}

// color returns the value of a tone for a profile.
//
// Parameters:
//   - p: color profile of the terminal
func (t tone) color(p colorprofile.Profile) color.Color {
	return lipgloss.Complete(p)(lipgloss.Color(t.ansi), lipgloss.Color(t.c256),
		lipgloss.Color(t.rgb))
}

// palette lists the colors of one scheme.
//
// Text and Muted are the body and the secondary text, Marker the cursor of the filter and the
// accent of titles below true color, Accent the pink of the border of the overlays, Err, OK and
// Warn the status colors, Sel and SelBG the colors of the selected row, Hover the background of
// what the pointer rests on, a step away from the background of the terminal and well short of
// SelBG, and From and To the two ends of the gradient. This file is the only place where a color
// value is written.
type palette struct {
	text   tone
	muted  tone
	marker tone
	accent tone
	err    tone
	ok     tone
	warn   tone
	sel    tone
	selBG  tone
	hover  tone
	from   tone
	to     tone
}

// darkPalette is the scheme for a dark background (night, #15131A). The contrasts against it
// are at least 4.5 for text and 3 for the other elements.
var darkPalette = palette{
	text:   tone{"", "255", "#ECE8F0"},
	muted:  tone{"", "245", "#8B8697"},
	marker: tone{"11", "208", "#EA6A0A"},
	accent: tone{"13", "162", "#DB2777"},
	err:    tone{"9", "203", "#F87171"},
	ok:     tone{"10", "114", "#4ADE80"},
	warn:   tone{"11", "214", "#FBBF24"},
	sel:    tone{"", "255", "#F4D9E2"},
	selBG:  tone{"", "89", "#3A2230"},
	hover:  tone{"", "235", "#221F29"},
	from:   tone{"", "", "#EA6A0A"},
	to:     tone{"", "", "#EC4899"},
}

// lightPalette is the scheme for a light background (chalk, #FBF8F4), with the darker variants
// that keep the contrast of text at 4.5 or more.
var lightPalette = palette{
	text:   tone{"", "233", "#15131A"},
	muted:  tone{"", "241", "#5F5A6B"},
	marker: tone{"1", "130", "#B84F00"},
	accent: tone{"5", "125", "#B0175D"},
	err:    tone{"1", "160", "#B91C1C"},
	ok:     tone{"2", "28", "#15803D"},
	warn:   tone{"5", "130", "#92400E"},
	sel:    tone{"", "52", "#5A0F33"},
	selBG:  tone{"", "224", "#F6D5E3"},
	hover:  tone{"", "255", "#F2EDE7"},
	from:   tone{"", "", "#B84F00"},
	to:     tone{"", "", "#B0175D"},
}

// theme is the set of styles of the interface.
//
// Text, Muted, Bold, Title, Marker, Border, Accent, Heading, Hint, Selected, Err, OK, Warn, Key,
// Foot and Cursor are ready to render plain strings. Foot draws the keys of the footer, in the
// muted color like the rest of the line as in the mockups, and in bold only where there is no
// color to tell them from their descriptions. Cursor is the block of a text cursor, in reverse
// video so that it shows on an empty field with or without color. Selected styles the selected
// row, arrow included, and Highlight the text selected with the mouse, which is reverse video so
// that it shows with or without color. Where the selected row is in reverse video too, with 16
// colors or none, the highlight is underlined as well, see highlight, so that it never looks like
// the selected row.
// Hover is the look of what the pointer rests on, see hover: a background a step off the one of
// the terminal with 256 colors and true color, and an underline with 16 colors and none, so that
// it shows without any color and stays well short of the selected row. It is the one role that is
// not a lipgloss style: it is added to text that already has its styles, so it is a bare run of
// attributes.
// Border draws the frames of the page and Accent the frame of an overlay, Heading the column
// headings and Hint a hint that stands where text is typed. The gradient colors are set only when
// the profile is true color: below it, titles use one solid color. Mode and Profile tell how the
// theme was built. Underline is true when the highlight must be underlined, see highlight, and
// SelOpen is the start of the sequence that the selected row begins with, which tells it from the
// rest, see selected.
type theme struct {
	Text      lipgloss.Style
	Muted     lipgloss.Style
	Bold      lipgloss.Style
	Title     lipgloss.Style
	Marker    lipgloss.Style
	Border    lipgloss.Style
	Accent    lipgloss.Style
	Heading   lipgloss.Style
	Hint      lipgloss.Style
	Selected  lipgloss.Style
	Highlight lipgloss.Style
	Err       lipgloss.Style
	OK        lipgloss.Style
	Warn      lipgloss.Style
	Key       lipgloss.Style
	Foot      lipgloss.Style
	Cursor    lipgloss.Style
	Hover     ansi.Style
	Mode      themeMode
	Profile   colorprofile.Profile
	underline bool
	selOpen   string
	from      color.Color
	to        color.Color
}

// newTheme builds the styles of a scheme for a color profile.
//
// Parameters:
//   - mode: scheme to use; a profile without colors always gives the neutral mode
//   - p: color profile of the terminal
//
// Returns the theme. With 16 colors the text keeps the default foreground and the selection is
// drawn in reverse video; with 256 colors and true color the selection has its own colors.
func newTheme(mode themeMode, p colorprofile.Profile) theme {
	base := lipgloss.NewStyle()
	th := theme{Mode: mode, Profile: p}

	if !colors(p) {
		th.Mode = modeNeutral
	}

	if th.Mode == modeNeutral {
		return th.neutral(base)
	}

	pal := darkPalette
	if mode == modeLight {
		pal = lightPalette
	}

	fg := func(t tone) lipgloss.Style { return base.Foreground(t.color(p)) }

	th.Text = fg(pal.text)
	th.Muted = fg(pal.muted)
	th.Bold = th.Text.Bold(true)
	th.Title = fg(pal.marker).Bold(true)
	th.Marker = fg(pal.marker).Bold(true)
	th.Border = th.Muted
	th.Accent = fg(pal.accent)
	th.Heading = th.Muted.Bold(true)
	th.Hint = th.Muted.Italic(true)
	th.Err = fg(pal.err)
	th.OK = fg(pal.ok)
	th.Warn = fg(pal.warn)
	th.Key = th.Text.Bold(true)
	th.Foot = th.Muted
	th.Cursor = base.Reverse(true)
	th.Selected = base.Bold(true).Reverse(true)
	th.Highlight = base.Reverse(true)
	th.Hover = ansi.NewStyle().Underline(true)
	th.underline = true

	if p >= colorprofile.ANSI256 {
		th.underline = false
		th.Selected = base.Bold(true).Foreground(pal.sel.color(p)).Background(pal.selBG.color(p))
		th.Marker = th.Selected.Foreground(pal.marker.color(p))
		th.Hover = ansi.NewStyle().BackgroundColor(pal.hover.color(p))
	} else {
		th.Marker = th.Selected
	}

	if p == colorprofile.TrueColor {
		th.from, th.to = pal.from.color(p), pal.to.color(p)
	}

	th.selOpen = opening(th.Selected)

	return th
}

// opening returns the start of the sequence that a style puts in front of a text.
//
// Parameters:
//   - st: style to look at
//
// Returns what comes before the text when the style renders one, or an empty string for a style
// that draws nothing.
func opening(st lipgloss.Style) string {
	out := st.Render("x")

	return out[:strings.Index(out, "x")]
}

// neutral fills a theme with styles that use no color.
//
// Parameters:
//   - base: empty style
func (t theme) neutral(base lipgloss.Style) theme {
	t.Text, t.Muted, t.Border, t.Accent = base, base, base, base
	t.Bold = base.Bold(true)
	t.Heading = t.Bold
	t.Hint = base.Italic(true)
	t.Title = t.Bold
	t.Key = t.Bold
	t.Foot = t.Bold
	t.Cursor = base.Reverse(true)
	t.Err = t.Bold
	t.OK, t.Warn = base, t.Bold
	t.Selected = base.Bold(true).Reverse(true)
	t.Highlight = base.Reverse(true)
	t.Hover = ansi.NewStyle().Underline(true)
	t.underline = true
	t.Marker = t.Selected
	t.selOpen = opening(t.Selected)

	return t
}

// flagged returns the style of a value that is wrong and must stand out, such as the dimension
// of a terminal that is too small.
//
// Returns the Warn role in bold, and underlined too where the theme has no colors to tell it
// (16 colors and none), so that it never relies on the color alone.
func (t theme) flagged() lipgloss.Style {
	st := t.Warn.Bold(true)
	if t.underline {
		st = st.Underline(true)
	}

	return st
}

// highlight renders text selected with the mouse.
//
// Parameters:
//   - s: plain text
//
// Returns the text in reverse video, and underlined where the selected row is in reverse video
// too. Underlining is not done by the Highlight style, whose renderer styles the characters one by
// one and leaves the spaces out.
func (t theme) highlight(s string) string {
	if t.underline {
		return ansi.NewStyle().Reverse(true).Underline(true).Styled(s)
	}

	return t.Highlight.Render(s)
}

// hover puts the hover look on a span of a styled line.
//
// Parameters:
//   - span: text to draw, with its own styles
//
// Returns the span with the hover attributes added at its start and again after each reset inside
// it, so that they reach the whole span, and ended by a reset. A background that the span sets
// itself wins, which is why a selected row is left alone, see selected.
func (t theme) hover(span string) string {
	if len(t.Hover) == 0 {
		return span
	}

	open := t.Hover.String()
	span = strings.ReplaceAll(span, ansi.ResetStyle, ansi.ResetStyle+open)
	span = strings.ReplaceAll(span, "\x1b[0m", "\x1b[0m"+open)
	span = strings.TrimSuffix(span, open)

	if !strings.HasSuffix(span, ansi.ResetStyle) && !strings.HasSuffix(span, "\x1b[0m") {
		span += ansi.ResetStyle
	}

	return open + span
}

// selected reports whether a styled span has text drawn as the selected row or button, which the
// hover leaves as it is: the selection is the stronger of the two.
//
// Parameters:
//   - span: styled text, which may begin with the sequences that put it in the style it is in
//
// Returns true when a character of the span is drawn while the selection style is on. The styles
// are followed from the start of the span, so that a selected button that comes before the span on
// the line, whose style was closed by then, does not count.
func (t theme) selected(span string) bool {
	if t.selOpen == "" {
		return false
	}

	on := false

	for s := span; s != ""; {
		if !strings.HasPrefix(s, "\x1b[") {
			if on {
				return true
			}

			s = s[1:]

			continue
		}

		end := 2
		for end < len(s) && (s[end] < 0x40 || s[end] > 0x7e) {
			end++
		}

		if end >= len(s) {
			return false
		}

		switch s[:end+1] {
		case ansi.ResetStyle, "\x1b[0m":
			on = false
		case t.selOpen:
			on = true
		}

		s = s[end+1:]
	}

	return false
}

// gradient renders a word with the colors blending letter by letter from the first to the
// second end of the gradient.
//
// Parameters:
//   - s: plain text
//
// Returns the text in the title style when the profile is below true color.
func (t theme) gradient(s string) string {
	if t.from == nil || s == "" {
		return t.Title.Render(s)
	}

	runes := []rune(s)
	cols := lipgloss.Blend1D(len(runes), t.from, t.to)
	out := make([]byte, 0, len(s)*8)

	for i, r := range runes {
		out = append(out, t.Title.Foreground(cols[i]).Render(string(r))...)
	}

	return string(out)
}

// bag renders one line of the bag logo, the lines going from the first to the second end of
// the gradient.
//
// Parameters:
//   - s: plain text of the line
//   - i: line index, from 0 to n-1
//   - n: number of lines
func (t theme) bag(s string, i, n int) string {
	if t.from == nil || n < 1 {
		return t.Title.Render(s)
	}

	return t.Title.Foreground(lipgloss.Blend1D(n, t.from, t.to)[i]).Render(s)
}
