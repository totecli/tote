package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// playPalette plays a list of steps on an open palette: "type:text" types text, "paste:text"
// pastes it and any other step is a key name.
//
// Parameters:
//   - g: rig with the palette open
//   - steps: what to do, in order
func playPalette(g *rig, steps []string) {
	g.t.Helper()

	for _, s := range steps {
		switch text, ok := strings.CutPrefix(s, "type:"); {
		case ok:
			g.typeText(text)
		case strings.HasPrefix(s, "paste:"):
			g.send(tea.PasteMsg{Content: strings.TrimPrefix(s, "paste:")})
		default:
			g.press(s)
		}
	}
}

// TestPalettePreview plays key sequences on the palette and checks its state after each one: what
// was typed, what the input line shows, whether it previews an entry, which entry is selected and
// whether the palette is still open.
//
// Parameters:
//   - t: test to report to
func TestPalettePreview(t *testing.T) {
	tests := []struct {
		name    string
		steps   []string
		typed   string
		input   string
		preview bool
		sel     string
		closed  bool
	}{
		{name: "nothing yet", steps: nil, typed: "", input: "", sel: "prod"},
		{
			name: "typing only filters", steps: []string{"type:c st"}, typed: "c st",
			input: "c st", sel: "staging",
		},
		{
			name: "down previews the second entry", steps: []string{"type:c ", "down"},
			typed: "c ", input: "c staging", preview: true, sel: "staging",
		},
		{
			name: "down twice", steps: []string{"type:c ", "down", "down"}, typed: "c ",
			input: "c bastion", preview: true, sel: "bastion",
		},
		{
			name: "up back to the first", steps: []string{"type:c ", "down", "up"}, typed: "c ",
			input: "c prod", preview: true, sel: "prod",
		},
		{
			name: "up at the top still previews", steps: []string{"type:c ", "up"}, typed: "c ",
			input: "c prod", preview: true, sel: "prod",
		},
		{
			name: "ctrl+n and ctrl+p", steps: []string{"type:c ", "ctrl+n", "ctrl+n", "ctrl+p"},
			typed: "c ", input: "c staging", preview: true, sel: "staging",
		},
		{
			name: "end and home", steps: []string{"type:c ", "end"}, typed: "c ",
			input: "c hostile", preview: true, sel: "hostile",
		},
		{
			name: "home", steps: []string{"type:c ", "end", "home"}, typed: "c ",
			input: "c prod", preview: true, sel: "prod",
		},
		{
			name: "page keys", steps: []string{"type:c ", "pgdown"}, typed: "c ",
			input: "c hostile", preview: true, sel: "hostile",
		},
		{
			name: "no prefix", steps: []string{"type:conn", "down"}, typed: "conn",
			input: "staging", preview: true, sel: "staging",
		},
		{
			name: "action prefix", steps: []string{"type:>", "down"}, typed: ">",
			input: ">New repository", preview: true, sel: "New repository",
		},
		{
			name: "tab keeps the previewed name", steps: []string{"type:c ", "down", "tab"},
			typed: "c staging", input: "c staging", sel: "staging",
		},
		{
			name:  "tab without a preview completes the selected entry",
			steps: []string{"type:c st", "tab"}, typed: "c staging", input: "c staging",
			sel: "staging",
		},
		{
			name: "tab twice is stable", steps: []string{"type:c ", "down", "tab", "tab"},
			typed: "c staging", input: "c staging", sel: "staging",
		},
		{
			name: "tab on an action", steps: []string{"type:>", "down", "tab"},
			typed: ">New repository", input: ">New repository", sel: "New repository",
		},
		{
			name: "tab with no result", steps: []string{"type:zzzz", "tab"}, typed: "zzzz",
			input: "zzzz", sel: "",
		},
		{
			name: "tab keeps a letter prefix in normal form", steps: []string{"type:C  ", "tab"},
			typed: "C prod", input: "C prod", sel: "prod",
		},
		{name: "a character restores the typed text first", steps: []string{
			"type:c ", "down",
			"type:b",
		}, typed: "c b", input: "c b", sel: "bastion"},
		{name: "backspace restores the typed text first", steps: []string{
			"type:c st", "down",
			"backspace",
		}, typed: "c s", input: "c s", sel: "staging"},
		{
			name: "ctrl+u clears the typed text", steps: []string{"type:c st", "down", "ctrl+u"},
			typed: "", input: "", sel: "prod",
		},
		{name: "ctrl+w deletes a word of the typed text", steps: []string{
			"type:c st", "down",
			"ctrl+w",
		}, typed: "c ", input: "c ", sel: "prod"},
		{name: "paste applies to the typed text", steps: []string{
			"type:c ", "down",
			"paste:ba",
		}, typed: "c ba", input: "c ba", sel: "bastion"},
		{
			name: "first esc restores", steps: []string{"type:c ", "down", "esc"}, typed: "c ",
			input: "c ", sel: "staging",
		},
		{
			name: "second esc closes", steps: []string{"type:c ", "down", "esc", "esc"},
			typed: "c ", input: "c ", sel: "staging", closed: true,
		},
		{
			name: "esc closes at once without a preview", steps: []string{"type:c ", "esc"},
			typed: "c ", input: "c ", sel: "prod", closed: true,
		},
		{
			name: "ctrl+c is the first esc", steps: []string{"type:c ", "down", "ctrl+c"},
			typed: "c ", input: "c ", sel: "staging",
		},
		{
			name: "ctrl+c twice closes", steps: []string{"type:c ", "down", "ctrl+c", "ctrl+c"},
			typed: "c ", input: "c ", sel: "staging", closed: true,
		},
		{
			name: "ctrl+k closes while previewing", steps: []string{"type:c ", "down", "ctrl+k"},
			typed: "c ", input: "c staging", preview: true, sel: "staging", closed: true,
		},
		{name: "down again after a restore previews again", steps: []string{
			"type:c ", "down",
			"esc", "down",
		}, typed: "c ", input: "c bastion", preview: true, sel: "bastion"},
		{
			name: "enter runs the previewed entry", steps: []string{"type:c ", "down", "enter"},
			typed: "c ", input: "c staging", preview: true, sel: "staging", closed: true,
		},
		{name: "enter runs the first entry when nothing was moved", steps: []string{
			"type:c ",
			"enter",
		}, typed: "c ", input: "c ", sel: "prod", closed: true},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t))
		p := openPalette(g)

		playPalette(g, tt.steps)

		open := len(g.r.overlays) == 1

		if open == tt.closed {
			t.Errorf("%s: palette open = %v", tt.name, open)
		}

		if p.flt.String() != tt.typed || p.inputText() != tt.input || p.preview != tt.preview {
			t.Errorf("%s: typed %q input %q preview %v; want %q %q %v", tt.name, p.flt.String(),
				p.inputText(), p.preview, tt.typed, tt.input, tt.preview)
		}

		if got := top(p); got != tt.sel {
			t.Errorf("%s: selected %q, want %q", tt.name, got, tt.sel)
		}

		if open && tt.input != "" && !strings.Contains(g.text(), tt.input) {
			t.Errorf("%s: the input line does not show %q:\n%s", tt.name, tt.input, g.text())
		}
	}
}

// TestPalettePreviewRuns checks which entry Enter runs after the previewing keys: the selected
// one, whatever the input line shows, and that a record and an action both work.
//
// Parameters:
//   - t: test to report to
func TestPalettePreviewRuns(t *testing.T) {
	tests := []struct {
		name  string
		steps []string
		check func(t *testing.T, g *rig)
	}{
		{"previewed connection", []string{"type:c ", "down", "enter"}, func(t *testing.T, g *rig) {
			var got activateMsg

			for _, m := range g.Seen {
				if a, ok := m.(activateMsg); ok {
					got = a
				}
			}

			if got.Kind != kindConn || got.Key != "staging" {
				t.Errorf("activateMsg = %+v, want the connection staging", got)
			}
		}},
		{"completed then run", []string{"type:c ", "down", "tab", "enter"}, func(t *testing.T,
			g *rig,
		) {
			var got activateMsg

			for _, m := range g.Seen {
				if a, ok := m.(activateMsg); ok {
					got = a
				}
			}

			if got.Key != "staging" {
				t.Errorf("activateMsg = %+v, want staging", got)
			}
		}},
		{"restored then run", []string{"type:c ", "down", "esc", "enter"}, func(t *testing.T,
			g *rig,
		) {
			var got activateMsg

			for _, m := range g.Seen {
				if a, ok := m.(activateMsg); ok {
					got = a
				}
			}

			if got.Key != "staging" {
				t.Errorf("activateMsg = %+v: Enter runs the selected entry even after Esc", got)
			}
		}},
		{"previewed action", []string{"type:>", "end", "enter"}, func(t *testing.T, g *rig) {
			if !g.Quit {
				t.Errorf("the last action is Quit tote, but the interface is open")
			}
		}},
		{"previewed edit", []string{"type:a ", "down", "ctrl+e"}, func(t *testing.T, g *rig) {
			var got editMsg

			for _, m := range g.Seen {
				if e, ok := m.(editMsg); ok {
					got = e
				}
			}

			if got.Kind != kindAlias || got.Key != "gs" {
				t.Errorf("editMsg = %+v, want the alias gs", got)
			}
		}},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t))
		openPalette(g)
		playPalette(g, tt.steps)
		tt.check(t, g)
	}
}

// TestPalettePreviewFooter checks that the footer lists the keys that apply in each state of the
// palette, from the key maps: Esc closes until an entry is previewed, then it restores; Tab and
// Enter need a selected entry; Ctrl-E needs a record.
//
// Parameters:
//   - t: test to report to
func TestPalettePreviewFooter(t *testing.T) {
	tests := []struct {
		name   string
		steps  []string
		want   []string
		absent []string
	}{
		{
			"start", nil,
			[]string{"esc close", "tab complete", "ctrl+e edit", "open"},
			[]string{"restore"},
		},
		{
			"previewing a record",
			[]string{"type:c ", "down"},
			[]string{"esc restore", "tab complete", "ctrl+e edit", "open"},
			[]string{"close"},
		},
		{
			"previewing an action",
			[]string{"type:>", "down"},
			[]string{"esc restore", "tab complete", "open"},
			[]string{"ctrl+e", "close"},
		},
		{
			"after tab",
			[]string{"type:c ", "down", "tab"},
			[]string{"esc close", "tab complete", "ctrl+e edit"},
			[]string{"restore"},
		},
		{
			"after esc",
			[]string{"type:c ", "down", "esc"},
			[]string{"esc close", "tab complete"},
			[]string{"restore"},
		},
		{
			"no result",
			[]string{"type:zzzz"},
			[]string{"esc close"},
			[]string{"tab", "open", "ctrl+e", "restore"},
		},
	}

	for _, tt := range tests {
		g := paletteRig(t, 110, 30, "", nil)
		playPalette(g, tt.steps)

		foot := g.footerText()

		for _, w := range tt.want {
			if !strings.Contains(foot, w) {
				t.Errorf("%s: the footer lacks %q: %q", tt.name, w, foot)
			}
		}

		for _, a := range tt.absent {
			if strings.Contains(foot, a) {
				t.Errorf("%s: the footer still has %q: %q", tt.name, a, foot)
			}
		}
	}
}

// TestPalettePreviewKeepsTheList checks that previewing never filters: the hits stay those of the
// typed text while the input line shows another name, and only Tab or a character changes them.
//
// Parameters:
//   - t: test to report to
func TestPalettePreviewKeepsTheList(t *testing.T) {
	g := newRig(t, newSandbox(t))
	p := openPalette(g)

	g.typeText("c ")

	want := slices.Clone(p.hits)

	g.press("down", "down", "end", "up", "pgup")

	if !slices.Equal(p.hits, want) {
		t.Errorf("the hits changed while previewing: %v, want %v", p.hits, want)
	}

	g.press("tab")

	if len(p.hits) >= len(want) {
		t.Errorf("tab did not filter by the completed name: %d hits", len(p.hits))
	}
}

// TestPalettePreviewStyle checks that the previewed name is drawn like typed text, followed by the
// cursor, and that the scope label at the right stays that of the typed text.
//
// Parameters:
//   - t: test to report to
func TestPalettePreviewStyle(t *testing.T) {
	g := paletteRig(t, 110, 30, "c ", nil)
	typed := g.view()

	g.press("down")
	previewed := g.view()

	find := func(lines []string, sub string) string {
		for _, l := range lines {
			if strings.Contains(stripStyles(l), sub) {
				return l
			}
		}

		t.Fatalf("no line holds %q:\n%s", sub, strings.Join(lines, "\n"))

		return ""
	}

	a, b := find(typed, "\u276f c \u258f"), find(previewed, "\u276f c staging\u258f")

	if !strings.Contains(a, "\x1b[38;2;236;232;240mc ") ||
		!strings.Contains(b, "\x1b[38;2;236;232;240mc staging") {
		t.Errorf("the previewed text is not in the style of the typed text:\n%q\n%q", a, b)
	}

	for _, l := range []string{a, b} {
		if !strings.Contains(stripStyles(l), "conn") {
			t.Errorf("the scope label is gone: %q", stripStyles(l))
		}
	}
}
