package ui

// stateSizes are the sizes of the frames of the states of the palette and of the settings: compact,
// standard and wide.
var stateSizes = [][2]int{{60, 12}, {80, 24}, {110, 30}}

// stateLooks lists the ways a state is drawn: English, French and ASCII with the French words.
var stateLooks = []struct {
	suffix string
	setup  func(*Options)
}{
	{"", nil},
	{"french", func(o *Options) { o.Printer = newPrinter("fr") }},
	{"ascii", func(o *Options) { o.Printer, o.ASCII = newPrinter("fr"), true }},
}

// stateFrames lists the frames of the palette in each state of its preview and of the settings
// screen in the middle of its list and on a switch and a choice, whose value is framed by the
// angle quotes that the left and right keys change, each one in English, in French and in ASCII,
// at the three sizes of stateSizes.
func stateFrames() []frameCase {
	states := []struct {
		name  string
		start Start
		data  dataSet
		keys  []string
	}{
		{"palettetop", StartConnections, dataFull, []string{"ctrl+k"}},
		{"palettepreview", StartConnections, dataFull, []string{"ctrl+k", "down", "down", "down"}},
		{
			"palettepreviewquery", StartConnections, dataFull,
			[]string{"ctrl+k", "type:pro", "down"},
		},
		{
			"palettepreviewscope", StartConnections, dataFull,
			[]string{"ctrl+k", "type:r ", "down"},
		},
		{
			"palettepreviewtab", StartConnections, dataFull,
			[]string{"ctrl+k", "type:pr", "down", "tab"},
		},
		{
			"palettepreviewscopetab", StartConnections, dataFull,
			[]string{"ctrl+k", "type:r ", "down", "tab"},
		},
		{
			"palettepreviewesc", StartConnections, dataFull,
			[]string{"ctrl+k", "type:pro", "down", "esc"},
		},
		{
			"palettepreviewtyped", StartConnections, dataFull,
			[]string{"ctrl+k", "type:pro", "down", "type:j"},
		},
		{
			"palettepreviewmiddle", StartConnections, dataFull,
			[]string{"ctrl+k", "pgdown", "down"},
		},
		{"palettepreviewend", StartConnections, dataFull, []string{"ctrl+k", "end"}},
		{
			"palettepreviewhome", StartConnections, dataFull,
			[]string{"ctrl+k", "end", "home"},
		},
		{"configmiddle", StartConfig, dataConfig, []string{"down", "down", "down", "down", "down"}},
		{"configend", StartConfig, dataConfig, []string{"end"}},
		{"configarrowsbool", StartConfig, dataConfig, []string{"down", "down", "down", "down"}},
		{
			"configarrowschoice", StartConfig, dataConfig,
			[]string{"down", "down", "down", "down", "down", "down"},
		},
		{
			"configwidth", StartConfig, dataConfig,
			[]string{"down", "down", "down", "down", "down", "down", "down", "down", "down"},
		},
	}

	var out []frameCase

	for _, st := range states {
		for _, look := range stateLooks {
			out = append(out, frameCase{
				name: st.name + look.suffix, data: st.data,
				start: st.start, keys: st.keys, sizes: stateSizes, setup: look.setup,
			})
		}
	}

	return out
}

// clashFrames lists the frames of the warning that the theme row of the settings screen shows
// when the dark scheme is set and the terminal answered with a light background, in English, in
// French and in ASCII with the French words, at the three sizes of stateSizes. The frames are
// drawn with colors, since without them the scheme is neutral and there is nothing to warn about.
func clashFrames() []frameCase {
	keys := []string{"down", "down", "down", "down", "down", "down", "bg:light"}
	tint := func(o *Options) { o.Environ = append(o.Environ, "TOTE_COLOR=truecolor") }

	var out []frameCase

	for _, look := range stateLooks {
		out = append(out, frameCase{
			name: "configclash" + look.suffix, data: dataConfig, start: StartConfig, keys: keys,
			sizes: stateSizes,
			setup: func(o *Options) {
				tint(o)

				if look.setup != nil {
					look.setup(o)
				}
			},
		})
	}

	return out
}
