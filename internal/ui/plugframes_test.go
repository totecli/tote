package ui

import (
	"slices"
	"strings"
)

// pluginSizes are the sizes of the frames of the plugin screens: standard, compact and the
// smallest terminal that the interface takes.
var pluginSizes = [][2]int{{80, 24}, {40, 12}, {24, 7}}

// consentSizes are the sizes of the frames of the consent: the standard ones, plus the width of a
// narrow window and the one of a large terminal, where the box is cut or at its widest.
var consentSizes = append(slices.Clone(pluginSizes), [2]int{60, 12}, [2]int{120, 40})

// checkedBesides returns the other sizes of the matrix, where the frames of the plugin screens are
// checked and not kept.
//
// Parameters:
//   - kept: the sizes whose frames are kept
func checkedBesides(kept [][2]int) [][2]int {
	var out [][2]int

	for _, w := range matrixWidths {
		for _, h := range matrixHeights {
			if !slices.Contains(kept, [2]int{w, h}) {
				out = append(out, [2]int{w, h})
			}
		}
	}

	return append(out, tinySizes[:3]...)
}

// pluginLooks lists the ways a plugin frame is drawn: English, French and ASCII with the French
// words.
var pluginLooks = stateLooks

// pluginFrames lists the frames of the plugins screen, of its overlays and of the forms of the
// settings, at the sizes of pluginSizes. Every key sequence only reads: the sandbox is shared by
// all the frames, so none of them installs, enables or writes anything.
//
// In the sandbox the plugins are, in order of their ids, demo (enabled, with settings), gft (not
// installed), ghost (missing dependency), jumpy (quarantined), notes (disabled), pager (consent
// needed) and torn (broken).
func pluginFrames() []frameCase {
	downs := func(n int, rest ...string) []string {
		keys := make([]string, n)
		for i := range keys {
			keys[i] = "down"
		}

		return append(keys, rest...)
	}

	cases := []struct {
		name  string
		start Start
		keys  []string
		looks bool
	}{
		{"plugins", StartPlugins, nil, true},
		{"pluginsmiddle", StartPlugins, downs(3), false},
		{"pluginsend", StartPlugins, []string{"end"}, false},
		{"pluginsghost", StartPlugins, downs(2), false},
		{"pluginsavailable", StartPlugins, downs(1), false},
		{"pluginsfilter", StartPlugins, []string{"/", "type:no"}, false},
		{"pluginsnone", StartPlugins, []string{"/", "type:zzz"}, false},
		{"pluginsmenu", StartPlugins, []string{"tab"}, true},
		{"pluginsmenuquarantine", StartPlugins, downs(3, "tab"), false},
		{"pluginsmenuavailable", StartPlugins, downs(1, "tab"), false},
		{"pluginsinfo", StartPlugins, []string{"enter"}, true},
		{"pluginsinfoghost", StartPlugins, downs(2, "enter"), false},
		{"pluginsinfoavailable", StartPlugins, downs(1, "enter"), false},
		{"pluginsconsent", StartPlugins, downs(5, "a"), true},
		{"pluginsconsentbutton", StartPlugins, downs(5, "a", "tab"), false},
		{"pluginsinstall", StartPlugins, downs(1, "i"), true},
		{"pluginsremove", StartPlugins, []string{"d"}, true},
		{"pluginscheck", StartPlugins, downs(4, "c"), true},
		{"pluginscheckwarn", StartPlugins, []string{"c"}, false},
		{"pluginssettings", StartPlugins, []string{"s"}, true},
		{"pluginssettingsmid", StartPlugins, []string{"s", "down", "down"}, false},
		{"pluginssettingslist", StartPlugins, []string{"s", "down", "down", "down", "down"}, false},
		{
			"pluginslistedit", StartPlugins,
			[]string{"s", "down", "down", "down", "down", "enter"},
			true,
		},
		{
			"pluginsprompt", StartPlugins,
			[]string{"s", "down", "down", "down", "down", "enter", "n"},
			false,
		},
		{"pluginshelp", StartPlugins, []string{"?"}, false},
		{"homeplugins", StartHome, downs(4), true},
		{"homepluginsfirst", StartHome, nil, false},
	}

	var out []frameCase

	for _, c := range cases {
		looks := []struct {
			suffix string
			setup  func(*Options)
		}{pluginLooks[0]}
		if c.looks {
			looks = pluginLooks
		}

		for _, look := range looks {
			kept := pluginSizes
			if strings.HasPrefix(c.name, "pluginsconsent") && c.name != "pluginsconsentbutton" {
				kept = consentSizes
			}

			out = append(out, frameCase{
				name: c.name + look.suffix, data: dataPlugins,
				start: c.start, keys: c.keys, sizes: kept, check: checkedBesides(kept),
				setup: look.setup,
			})
		}
	}

	return out
}
