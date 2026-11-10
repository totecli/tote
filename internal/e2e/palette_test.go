package e2e

import (
	"strings"
	"testing"
)

// paletteInput returns the text of the input line of the open palette: what follows the prompt on
// the first line of the box.
//
// Parameters:
//   - s: what the terminal shows
func paletteInput(s snap) string {
	for _, l := range strings.Split(s.Text, "\n") {
		if _, rest, ok := strings.Cut(l, "\u2502 \u276f "); ok {
			return strings.TrimSpace(strings.Trim(rest, "\u2502 \u258f"))
		}
	}

	return ""
}

// TestPalettePreview drives the palette of the real binary through the states of its preview: the
// arrow keys show the selected entry in the input, the footer names the keys of the state, Esc
// puts the typed text back (the hint here, since nothing was typed) before it closes, and Tab keeps
// the entry as the query.
//
// Parameters:
//   - t: test to report to
func TestPalettePreview(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{W: 110, H: 30}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.key("C-k")
	tm.waitText("Palette")
	tm.waitText("esc close")

	placeholder := paletteInput(tm.look())
	if !strings.HasPrefix(placeholder, "search all") {
		t.Errorf("the input of a palette that was just opened shows %q, want the hint", placeholder)
	}

	tm.key("Down")
	tm.waitText("esc restore")

	if got := paletteInput(tm.look()); got != "bravo" {
		t.Errorf("after Down the input shows %q, want bravo", got)
	}

	tm.key("Escape")
	tm.waitText("esc close")
	tm.waitText("Palette")

	if got := paletteInput(tm.look()); got != placeholder {
		t.Errorf("after Esc the input shows %q, want the typed text (none) back", got)
	}

	tm.key("Down", "Tab")
	tm.waitGone("esc restore")
	tm.waitGone("alpha")

	if got := paletteInput(tm.look()); got != "fail" {
		t.Errorf("after Tab the input shows %q, want the entry as the query", got)
	}

	tm.key("Escape")
	tm.waitGone("Palette")
	tm.quit()
}
