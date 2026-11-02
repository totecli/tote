package ui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
)

// minDescribed is the fewest hints a footer keeps with their descriptions: below it the keys
// alone, which allow more hints in the same width, are shown instead.
const minDescribed = 3

// maxKeysOnly is the most hints a footer of bare keys shows: more would be a wall of keys that say
// nothing, and the compact mockups show five.
const maxKeysOnly = 7

// hint is one entry of the footer: the label of a key and what it does.
//
// Press is the name of the key that a click on the hint presses, empty when the hint is not one
// key (such as "up/down" or "1-5"), see clickKey. Pinned hints are the primary action, the keys
// that leave the screen, the key of the palette, which is how every other action is found, and
// the key of the help, which is how every key is found; they are the last to go when the line is
// too narrow. The key of the primary action is pinned wherever it stands in the line, so that it
// can follow the keys that move.
type hint struct {
	key    string
	desc   string
	press  string
	pinned bool
}

// span is the place of a hint on the footer line: the cells it covers, counted from the start of
// the line, and the key that a click on it presses, empty for none.
type span struct {
	x     int
	w     int
	press string
}

// footerLine renders the key help line from bindings.
//
// Parameters:
//   - e: shared environment, for the theme, the glyphs and the metric
//   - bs: bindings in order of importance; those without a key label, or disabled, are skipped
//   - short: when true only the keys are shown, as in the compact layout
//   - width: cells available
//
// Returns the line, always at most width cells. When it is too wide, hints are dropped from the
// end, the primary action and the keys that quit or close being the last to go. The
// descriptions are shown while at least minDescribed hints fit with them; below that the
// descriptions are dropped, so that more keys fit, and as a last resort the first key is cut.
func footerLine(e *env, bs []key.Binding, short bool, width int) string {
	line, _ := footerDraw(e, bs, short, width)

	return line
}

// footerDraw renders the key help line and says where each hint is on it. The mouse hit-testing
// reads the spans, so the line and the places of its hints always come from this one function.
//
// Parameters:
//   - e: shared environment, for the theme, the glyphs and the metric
//   - bs: bindings in order of importance, as for footerLine
//   - short: when true only the keys are shown, as in the compact layout
//   - width: cells available
//
// Returns the same line as footerLine and one span per hint shown, in order. A hint that is cut
// to fit is given with the cells that remain of it.
func footerDraw(e *env, bs []key.Binding, short bool, width int) (string, []span) {
	hints := hintsOf(e, bs)
	if len(hints) == 0 || width <= 0 {
		return "", nil
	}

	if !short {
		if kept, ok := footerFit(e, hints, false, width, min(minDescribed, len(hints))); ok {
			return joinHelp(e, kept, false)
		}
	}

	if kept, ok := footerFit(e, hints, true, width, 1); ok {
		return joinHelp(e, kept, true)
	}

	plain := e.Metric.fit(plainHelp(e, hints[:1], true), width, e.Glyphs.ellipsis)
	lead := e.indent()

	return e.Theme.Muted.Render(plain), []span{{
		x: lead, w: max(0, e.Metric.width(plain)-lead),
		press: hints[0].press,
	}}
}

// clickKey tells which key a click on a hint of the footer presses.
//
// Parameters:
//   - b: the binding of the hint
//   - label: key label shown for it
//   - enter: label of the Enter key in the glyph set in use
//
// Returns the first key of the binding that is the label itself ("esc", "n", "ctrl+k", "q"), or
// "enter" when the label is the Enter glyph. A hint whose label is several keys ("up/down",
// "1-5", "h/l") or an arrow is not a single key, so it gives an empty string and a click on it
// does nothing. Only the keys that keyPress can build are returned.
func clickKey(b key.Binding, label, enter string) string {
	for _, k := range b.Keys() {
		if k == label || (k == "enter" && label == enter) {
			if _, ok := keyPress(k); ok {
				return k
			}
		}
	}

	return ""
}

// hintsOf turns bindings into hints, in the order given.
//
// Parameters:
//   - e: shared environment, for the bindings that must stay
//   - bs: bindings, the most important first
//
// Returns the enabled bindings that have a key label. The first one is pinned, and so is any
// binding that has the keys of Quit, Close, Back, Clear, Palette, Help or Open, or the key that
// saves a form.
func hintsOf(e *env, bs []key.Binding) []hint {
	exits := [][]string{
		e.Keys.Quit.Keys(), e.Keys.Close.Keys(), e.Keys.Back.Keys(), e.Keys.Clear.Keys(),
		e.Keys.Palette.Keys(), e.Keys.Help.Keys(), e.Keys.Open.Keys(),
		{"ctrl+s"},
	}

	var out []hint

	for _, b := range bs {
		h := b.Help()
		if !b.Enabled() || h.Key == "" {
			continue
		}

		pinned := len(out) == 0 || slices.ContainsFunc(exits, func(k []string) bool {
			return slices.Equal(k, b.Keys())
		})

		out = append(out, hint{
			key: sanitize(h.Key), desc: sanitize(h.Desc), pinned: pinned,
			press: clickKey(b, h.Key, e.Glyphs.enter),
		})
	}

	return out
}

// footerFit drops hints until the line fits.
//
// Parameters:
//   - e: shared environment
//   - hints: all hints, the most important first
//   - keysOnly: true to leave the descriptions out
//   - width: cells available
//   - least: fewest hints that are worth showing; fewer than that gives up
//
// Returns the hints kept, in their original order, and true when the line fits. Hints that are
// not pinned go first, from the last to the first, then the pinned ones in the same order. A line
// of bare keys also keeps at most maxKeysOnly hints.
func footerFit(e *env, hints []hint, keysOnly bool, width, least int) ([]hint, bool) {
	var order []int

	for _, pinned := range []bool{false, true} {
		for i := len(hints) - 1; i >= 0; i-- {
			if hints[i].pinned == pinned {
				order = append(order, i)
			}
		}
	}

	gone := map[int]bool{}

	for n := len(hints); n >= least && n > 0; n-- {
		kept := make([]hint, 0, n)

		for i, h := range hints {
			if !gone[i] {
				kept = append(kept, h)
			}
		}

		if (!keysOnly || n <= maxKeysOnly) &&
			e.Metric.width(plainHelp(e, kept, keysOnly)) <= width {
			return kept, true
		}

		gone[order[len(hints)-n]] = true
	}

	return nil, false
}

// plainHelp joins hints without styles, to measure them; joinHelp gives the same text styled.
//
// Parameters:
//   - e: shared environment
//   - hints: hints to join
//   - keysOnly: true to leave the descriptions out
func plainHelp(e *env, hints []hint, keysOnly bool) string {
	var b strings.Builder

	b.WriteString(e.lead())

	for i, h := range hints {
		if i > 0 {
			b.WriteString(" " + e.Glyphs.sep + " ")
		}

		b.WriteString(h.key)

		if !keysOnly && h.desc != "" {
			b.WriteString(" " + h.desc)
		}
	}

	return b.String()
}

// joinHelp joins hints into a styled line.
//
// Parameters:
//   - e: shared environment
//   - hints: hints to join
//   - keysOnly: when true the descriptions are left out
//
// Returns the line and the span of each hint: its first cell, counted from the start of the
// line, and its width.
func joinHelp(e *env, hints []hint, keysOnly bool) (string, []span) {
	var b strings.Builder

	b.WriteString(e.lead())

	sep := " " + e.Glyphs.sep + " "
	at := e.indent()
	spans := make([]span, 0, len(hints))

	for i, h := range hints {
		if i > 0 {
			b.WriteString(e.Theme.Muted.Render(sep))

			at += e.Metric.width(sep)
		}

		w := e.Metric.width(h.key)

		b.WriteString(e.Theme.Foot.Render(h.key))

		if !keysOnly && h.desc != "" {
			b.WriteString(e.Theme.Muted.Render(" " + h.desc))

			w += 1 + e.Metric.width(h.desc)
		}

		spans = append(spans, span{x: at, w: w, press: h.press})
		at += w
	}

	return b.String(), spans
}
