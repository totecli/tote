package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// TestHighlight checks that the highlight covers exactly a span of cells and keeps the rest of the
// line, its styles included, in place.
//
// Parameters:
//   - t: test to report to
func TestHighlight(t *testing.T) {
	e := bareEnv(t, false)
	th := e.Theme

	line := th.Muted.Render("alpha ") + th.Bold.Render("beta") + th.Text.Render(" gamma")

	row := selRow{y: 0, x0: 6, x1: 10, text: "beta"}
	got := e.highlight(line, row)

	if m := marked(got); m != "alpha [[beta]] gamma" {
		t.Errorf("marked = %q", m)
	}

	if plain := stripStyles(got); plain != "alpha beta gamma" {
		t.Errorf("plain = %q, want the line unchanged", plain)
	}

	if !strings.HasSuffix(got, th.Text.Render(" gamma")) {
		t.Errorf("the part after the span lost its style: %q", got)
	}

	if e.Metric.width(got) != e.Metric.width(line) {
		t.Errorf("width %d, want %d", e.Metric.width(got), e.Metric.width(line))
	}
}

// TestHighlightRole checks that the highlight is reverse video for every profile, underlined as
// well where the selected row is in reverse video too, and so never looks like the selected row.
//
// Parameters:
//   - t: test to report to
func TestHighlightRole(t *testing.T) {
	tests := []struct {
		name    string
		profile colorprofile.Profile
		mode    themeMode
		under   bool
	}{
		{"none", colorprofile.NoTTY, modeDark, true},
		{"ascii", colorprofile.Ascii, modeDark, true},
		{"16 colors", colorprofile.ANSI, modeDark, true},
		{"256 colors", colorprofile.ANSI256, modeDark, false},
		{"true color dark", colorprofile.TrueColor, modeDark, false},
		{"true color light", colorprofile.TrueColor, modeLight, false},
	}

	for _, tt := range tests {
		th := newTheme(tt.mode, tt.profile)
		got := th.highlight("x")

		colored := strings.Contains(got, "\x1b[38") || strings.Contains(got, "\x1b[48")
		if !strings.Contains(got, "7") || colored {
			t.Errorf("%s: highlight = %q, want reverse video without colors", tt.name, got)
		}

		if under := strings.Contains(got, "4"); under != tt.under {
			t.Errorf("%s: underlined = %v, want %v (%q)", tt.name, under, tt.under, got)
		}

		if got == th.Selected.Render("x") {
			t.Errorf("%s: the highlight looks like the selected row", tt.name)
		}
	}
}

// TestHighlightWide checks the highlight of a styled line that holds wide characters, an emoji
// and a letter with a combining mark, under both width rules: the span takes whole graphemes, the
// rest of the line keeps its width and its text, and the cells before and after do not move.
//
// Parameters:
//   - t: test to report to
func TestHighlightWide(t *testing.T) {
	const (
		cjk    = "ab\u65E5\u672C\u8A9E \U0001F680 x"
		accent = "cafe\u0301 ok"
	)

	tests := []struct {
		name string
		line string
		a, b cellPos
		want string
	}{
		{
			"first half of a wide character", cjk,
			cellPos{2, 0},
			cellPos{2, 0},
			"ab[[\u65E5]]\u672C\u8A9E \U0001F680 x",
		},
		{
			"second half of a wide character", cjk,
			cellPos{3, 0},
			cellPos{3, 0},
			"ab[[\u65E5]]\u672C\u8A9E \U0001F680 x",
		},
		{
			"two wide characters", cjk,
			cellPos{3, 0},
			cellPos{4, 0},
			"ab[[\u65E5\u672C]]\u8A9E \U0001F680 x",
		},
		{
			"wide characters up to the emoji", cjk,
			cellPos{6, 0},
			cellPos{9, 0},
			"ab\u65E5\u672C[[\u8A9E \U0001F680]] x",
		},
		{
			"the emoji by its second cell", cjk,
			cellPos{10, 0},
			cellPos{10, 0},
			"ab\u65E5\u672C\u8A9E [[\U0001F680]] x",
		},
		{"a combining mark", accent, cellPos{3, 0}, cellPos{3, 0}, "caf[[e\u0301]] ok"},
		{
			"around a combining mark", accent,
			cellPos{2, 0},
			cellPos{4, 0},
			"ca[[fe\u0301 ]]ok",
		},
	}

	for _, rule := range []ansi.Method{ansi.WcWidth, ansi.GraphemeWidth} {
		for _, tt := range tests {
			e := bareEnv(t, false)
			e.Metric = metric{method: rule}
			th := e.Theme

			plain := stripStyles(tt.line)
			styled := th.Muted.Render("ab") + th.Bold.Render(strings.TrimPrefix(plain, "ab"))

			if tt.line == accent {
				styled = th.Text.Render(plain)
			}

			rows := e.selRows([]string{plain}, tt.a, tt.b)
			if len(rows) != 1 || rows[0].empty {
				t.Fatalf("%s: rows = %+v", tt.name, rows)
			}

			got := e.highlight(styled, rows[0])

			if m := marked(got); m != tt.want {
				t.Errorf("%s: marked = %q, want %q", tt.name, m, tt.want)
			}

			if stripStyles(got) != plain {
				t.Errorf("%s: plain = %q, want the line unchanged", tt.name, stripStyles(got))
			}

			if e.Metric.width(got) != e.Metric.width(styled) {
				t.Errorf("%s: width %d, want %d", tt.name, e.Metric.width(got),
					e.Metric.width(styled))
			}
		}
	}
}
