package ui

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// hoverFrameCase is a screen with the pointer on one element, kept as a reference frame.
type hoverFrameCase struct {
	name string
	hoverCase
	size [2]int
}

// hoverFrameCases lists the reference frames of the hover: a row of a list, an entry of the
// welcome page, a row of the palette, a hint of the footer, a button, a row of the settings and a
// row of a picker, and the row of a list on a compact terminal, on one wider than a page capped at
// 120 cells and on one whose page is as wide as itself.
func hoverFrameCases() []hoverFrameCase {
	return []hoverFrameCase{
		{
			"hoverlist",
			hoverCase{start: StartConnections, target: "staging.example.com"},
			[2]int{80, 24},
		},
		{
			"hoverlistcompact",
			hoverCase{start: StartConnections, target: "bastion.example.com"},
			[2]int{60, 12},
		},
		{"hoverlistwide", hoverCase{
			start: StartConnections, target: "staging.example.com",
			width: "120",
		}, [2]int{200, 40}},
		{"hoverlistauto", hoverCase{
			start: StartConnections, target: "staging.example.com",
			width: "auto",
		}, [2]int{200, 40}},
		{"hoverhome", hoverCase{start: StartHome, target: "Repos"}, [2]int{80, 24}},
		{"hoverpalette", hoverCase{
			start: StartConnections, keys: []string{"ctrl+k"},
			target: "staging",
		}, [2]int{80, 24}},
		{
			"hoverfooter",
			hoverCase{start: StartConnections, target: "ctrl+k", footer: true},
			[2]int{80, 24},
		},
		{"hoverconfirm", hoverCase{
			start: StartConnections, keys: []string{"d"},
			target: "[ Yes ]",
		}, [2]int{80, 24}},
		{"hoverconfig", hoverCase{start: StartConfig, target: "Mouse"}, [2]int{80, 24}},
		{"hoverpick", hoverCase{
			start: StartConfig, keys: []string{"enter"},
			target: "English",
		}, [2]int{80, 24}},
	}
}

// hoverFrame draws a screen with the pointer on an element.
//
// Parameters:
//   - t: running test
//   - s: sandbox holding the data
//   - c: the screen and the element
//   - m: the mode to draw in
//   - size: width and height of the terminal
//
// Returns the rig, the cell of the pointer and the frame with the hover.
func hoverFrame(t *testing.T, s *sandbox, c hoverFrameCase, m hoverMode,
	size [2]int,
) (*rig, cellPos, string) {
	t.Helper()

	o := s.with(func(o *Options) {
		o.Start = c.start
		o.Width, o.Height = size[0], size[1]
		o.Environ = append(append([]string(nil), o.Environ...), m.env)
		o.Settings.UI.Theme = m.theme
		o.ASCII = m.ascii

		if c.width != "" {
			o.Settings.UI.Width = c.width
		}
	})

	g := newRig(t, o)
	g.press(c.keys...)

	p := pointAt(g, c.hoverCase)

	return g, p, g.r.render()
}

// escaped writes a styled line so that its sequences can be read in a reference file.
//
// Parameters:
//   - line: styled line
func escaped(line string) string {
	return strings.TrimRight(strings.ReplaceAll(line, "\x1b", "\\e"), " ")
}

// TestHoverFrames draws the reference screens with the pointer on an element in every mode and
// compares them with testdata. A file holds the frame as text, which the hover leaves as it is, and
// for each mode the line under the pointer with its escape sequences written out, and the line of
// the selected element for comparison, so that the weaker look can be read against the stronger.
// go test -run TestHoverFrames -update rewrites the files; read the diff.
//
// Parameters:
//   - t: test to report to
func TestHoverFrames(t *testing.T) {
	full, plain := frameSandbox(t, dataFull), frameSandbox(t, dataPlain)

	for _, c := range hoverFrameCases() {
		t.Run(c.name, func(t *testing.T) {
			var golden bytes.Buffer

			for i, m := range hoverModes {
				s := full
				if m.ascii {
					s = plain
				}

				g, p, got := hoverFrame(t, s, c, m, c.size)
				checkScreen(t, c.name+" "+m.name, got, c.size[0], c.size[1], m.ascii)

				lines := strings.Split(got, "\n")

				if i == 0 {
					fmt.Fprintf(&golden, "== %dx%d pointer %d,%d\n%s\n", c.size[0], c.size[1], p.x,
						p.y, trimFrame(got))
				}

				fmt.Fprintf(&golden, "-- %s\nhovered %d: %s\n", m.name, p.y, escaped(lines[p.y]))

				for y, l := range lines {
					if y != p.y && g.r.e.Theme.selected(l) {
						fmt.Fprintf(&golden, "selected %d: %s\n", y, escaped(l))

						break
					}
				}
			}

			compareGolden(t, filepath.Join("testdata", c.name+".golden"), golden.Bytes())
		})
	}
}
