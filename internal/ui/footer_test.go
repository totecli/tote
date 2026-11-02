package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

// helpBinding builds a binding that only has a help text.
//
// Parameters:
//   - label: key label shown in the footer
//   - desc: description shown after it
//   - keys: keys that trigger it
func helpBinding(label, desc string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, desc))
}

// TestFooterWidths checks, at every width, that the footer is never wider than the screen, under
// either width rule, and that it is never empty when there is room for a key.
//
// Parameters:
//   - t: test to report to
func TestFooterWidths(t *testing.T) {
	for _, start := range []Start{StartRepos, StartConnections, StartHome} {
		for _, lang := range []string{"en", "fr"} {
			for _, ascii := range []bool{false, true} {
				s := newSandbox(t).with(func(o *Options) {
					o.Start, o.Printer, o.ASCII = start, newPrinter(lang), ascii
				})
				g := newRig(t, s)
				e := g.r.e
				bs := g.r.scr.Keys(e)

				for _, short := range []bool{false, true} {
					for w := 150; w >= 1; w-- {
						line := footerLine(e, bs, short, w)

						if got := checkWidth(line, w); got != "" {
							t.Errorf("%d/%s/%v/%v at %d: %s", start, lang, ascii, short, w, got)
						}

						if w >= 12 && len(bs) > 0 && stripStyles(line) == "" {
							t.Errorf("%d/%s/%v/%v at %d: empty", start, lang, ascii, short, w)
						}
					}
				}
			}
		}
	}
}

// checkWidth reports a styled line that is wider than a limit under either width rule.
//
// Parameters:
//   - line: styled line
//   - w: limit in cells
//
// Returns an empty string when the line fits.
func checkWidth(line string, w int) string {
	plain := stripStyles(line)

	if gw, wc := ansi.StringWidth(plain), ansi.StringWidthWc(plain); gw > w || wc > w {
		return fmt.Sprintf("%d/%d cells, limit %d: %q", wc, gw, w, plain)
	}

	return ""
}

// TestFooterDropOrder checks which hints go first when the width shrinks: the last ones that are
// not the primary action or a key that leaves the screen.
//
// Parameters:
//   - t: test to report to
func TestFooterDropOrder(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	bs := []key.Binding{
		helpBinding("a", "alpha", "a"),
		helpBinding("b", "bravo", "b"),
		helpBinding("c", "charlie", "c"),
		e.Keys.Quit,
		helpBinding("d", "delta", "d"),
		helpBinding("e", "echo", "e"),
	}

	tests := []struct {
		width int
		want  []string
		not   []string
	}{
		{200, []string{"a alpha", "b bravo", "c charlie", "q quit", "d delta", "e echo"}, nil},
		{50, []string{"a alpha", "b bravo", "c charlie", "q quit"}, []string{"echo"}},
		{36, []string{"a alpha", "q quit"}, []string{"echo", "delta", "charlie"}},
		{24, []string{"a", "q"}, []string{"alpha", "quit", "echo"}},
	}

	for _, tt := range tests {
		got := stripStyles(footerLine(e, bs, false, tt.width))

		for _, w := range tt.want {
			if !strings.Contains(got, w) {
				t.Errorf("width %d: %q lacks %q", tt.width, got, w)
			}
		}

		for _, n := range tt.not {
			if strings.Contains(got, n) {
				t.Errorf("width %d: %q still has %q", tt.width, got, n)
			}
		}
	}

	// The primary action stays after everything else is gone.
	if got := stripStyles(footerLine(e, bs, false, 3)); !strings.Contains(got, "a") {
		t.Errorf("narrow footer = %q", got)
	}
}

// TestFooterEdges covers an empty list, disabled bindings, wide characters and tiny widths.
//
// Parameters:
//   - t: test to report to
func TestFooterEdges(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	off := helpBinding("x", "hidden", "x")
	off.SetEnabled(false)

	tests := []struct {
		name  string
		bs    []key.Binding
		width int
		want  string
		not   string
	}{
		{"none", nil, 40, "", ""},
		{"zero width", []key.Binding{e.Keys.Quit}, 0, "", ""},
		{"disabled", []key.Binding{off, e.Keys.Quit}, 40, "q quit", "hidden"},
		{
			"no label",
			[]key.Binding{key.NewBinding(key.WithKeys("z")), e.Keys.Quit},
			40,
			"q quit", "",
		},
		{"wide", []key.Binding{
			helpBinding("\u65e5\u672c", "\u8a9e\u8a9e\u8a9e", "a"),
			e.Keys.Quit,
		}, 14, "", ""},
	}

	for _, tt := range tests {
		line := footerLine(e, tt.bs, false, tt.width)
		got := stripStyles(line)

		if msg := checkWidth(line, tt.width); msg != "" {
			t.Errorf("%s: %s", tt.name, msg)
		}

		if tt.want != "" && !strings.Contains(got, tt.want) {
			t.Errorf("%s: %q lacks %q", tt.name, got, tt.want)
		}

		if tt.name == "none" || tt.name == "zero width" {
			if got != "" {
				t.Errorf("%s: %q, want nothing", tt.name, got)
			}
		}

		if tt.not != "" && strings.Contains(got, tt.not) {
			t.Errorf("%s: %q holds %q", tt.name, got, tt.not)
		}
	}

	// Hostile help text is neutralized before it is measured.
	bad := helpBinding("\x1b[31mk", "d\x07esc", "k")

	if got := stripStyles(footerLine(e, []key.Binding{bad}, false, 40)); strings.ContainsAny(got,
		"\x1b\x07") {
		t.Errorf("footer holds control characters: %q", got)
	}

	// Every width from 1 to 30 with the wide hint.
	wide := []key.Binding{helpBinding("\u65e5\u672c", "\u8a9e\u8a9e\u8a9e", "a"), e.Keys.Quit}

	for w := 1; w <= 30; w++ {
		if msg := checkWidth(footerLine(e, wide, false, w), w); msg != "" {
			t.Errorf("wide at %d: %s", w, msg)
		}
	}
}

// TestFooterPaletteAndFilter checks the footers of the overlay and of the typing state.
//
// Parameters:
//   - t: test to report to
func TestFooterPaletteAndFilter(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))

	g.press("ctrl+k")

	last := g.footerText()
	for _, want := range []string{"open", "esc close", "choose"} {
		if !strings.Contains(last, want) {
			t.Errorf("palette footer %q lacks %q", last, want)
		}
	}

	g.typeText("zzzz")

	if last := g.footerText(); strings.Contains(last, "open") ||
		strings.Contains(last, "ctrl+e") {
		t.Errorf("palette footer offers open or edit with no result: %q", last)
	}
}

// TestFooterKeepsThePalette checks that the key of the palette is one of the last to go, so that
// the other actions can always be found, and that on a list at 80 columns it is fully shown.
//
// Parameters:
//   - t: test to report to
func TestFooterKeepsThePalette(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	e := g.r.e

	bs := []key.Binding{
		helpBinding("a", "alpha", "a"),
		helpBinding("b", "bravo", "b"),
		helpBinding("c", "charlie", "c"),
		e.Keys.Palette,
		e.Keys.Quit,
	}

	for _, width := range []int{36, 30, 24} {
		got := stripStyles(footerLine(e, bs, false, width))

		if !strings.Contains(got, "ctrl+k") || !strings.Contains(got, "q") ||
			strings.Contains(got, "charlie") {
			t.Errorf("width %d: %q", width, got)
		}
	}

	g.resize(80, 24)

	if text := g.text(); !strings.Contains(text, "ctrl+k palette") {
		t.Errorf("the palette hint is clipped at 80x24:\n%s", text)
	}
}
