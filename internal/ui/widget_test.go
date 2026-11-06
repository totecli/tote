package ui

import (
	"strings"
	"testing"
)

// TestNewInput checks the text field seam.
//
// Parameters:
//   - t: test to report to
func TestNewInput(t *testing.T) {
	in := newInput("name", 12)

	if in.Placeholder != "name" || in.CharLimit != 12 || in.Prompt != "" {
		t.Errorf("input = %+v", in)
	}

	if len(in.KeyMap.Paste.Keys()) != 0 {
		t.Error("the clipboard paste binding is on; pasting goes through the terminal")
	}
}

// TestWelcomeBoxIsRectangular checks the width of every line of the welcome box.
//
// Parameters:
//   - t: test to report to
func TestWelcomeBoxIsRectangular(t *testing.T) {
	for _, env := range [][]string{{"TOTE_COLOR=truecolor"}, {"TOTE_COLOR=none", "LANG=C"}} {
		for _, w := range []int{70, 72, 80, 100, 160} {
			g := newRig(t, newSandbox(t).with(func(o *Options) {
				o.Width, o.Height = w, 40
				o.Environ = append(o.Environ, env...)
			}))

			box := welcomeBox(g.r.e)
			want := w

			if len(box) != 7 {
				t.Errorf("width %d: %d lines", w, len(box))
			}

			for i, l := range box {
				if got := g.r.e.Metric.width(l); got != want {
					t.Errorf("width %d line %d is %d cells, want %d: %q", w, i, got, want,
						stripStyles(l))
				}
			}
		}
	}
}

// TestKeymapHelp checks that every binding shown in a footer has a key label and a text in both
// languages, and none is missing a translation.
//
// Parameters:
//   - t: test to report to
func TestKeymapHelp(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		for _, ascii := range []bool{true, false} {
			km := newKeymap(newPrinter(lang), newGlyphs(ascii))

			for _, b := range []struct {
				name string
				help string
				desc string
			}{
				{"Nav", km.Nav.Help().Key, km.Nav.Help().Desc},
				{"Open", km.Open.Help().Key, km.Open.Help().Desc},
				{"Back", km.Back.Help().Key, km.Back.Help().Desc},
				{"Quit", km.Quit.Help().Key, km.Quit.Help().Desc},
				{"Filter", km.Filter.Help().Key, km.Filter.Help().Desc},
				{"Palette", km.Palette.Help().Key, km.Palette.Help().Desc},
				{"New", km.New.Help().Key, km.New.Help().Desc},
				{"Edit", km.Edit.Help().Key, km.Edit.Help().Desc},
				{"Delete", km.Delete.Help().Key, km.Delete.Help().Desc},
				{"Editor", km.Editor.Help().Key, km.Editor.Help().Desc},
				{"Tabs", km.Tabs.Help().Key, km.Tabs.Help().Desc},
				{"Apply", km.Apply.Help().Key, km.Apply.Help().Desc},
				{"Clear", km.Clear.Help().Key, km.Clear.Help().Desc},
				{"Reload", km.Reload.Help().Key, km.Reload.Help().Desc},
				{"Close", km.Close.Help().Key, km.Close.Help().Desc},
			} {
				if b.help == "" || b.desc == "" || strings.HasPrefix(b.desc, "!") {
					t.Errorf("%s %v %s: key %q desc %q", lang, ascii, b.name, b.help, b.desc)
				}
			}
		}
	}
}
