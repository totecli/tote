package ui

import "testing"

// TestHighlightSpaces checks that the highlight covers the spaces of a span as it covers the
// letters, with or without underline.
//
// Parameters:
//   - t: test to report to
func TestHighlightSpaces(t *testing.T) {
	for _, mode := range []themeMode{modeNeutral, modeDark} {
		e := bareEnv(t, false)
		th := newTheme(mode, e.Theme.Profile)
		got := marked(th.highlight("a  b"))

		if got != "[[a  b]]" {
			t.Errorf("mode %d: marked = %q, want one span", mode, got)
		}
	}
}
