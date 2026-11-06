package ui

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/config"
)

// update rewrites the reference frames of testdata instead of comparing with them. Review the
// result by hand (git diff internal/ui/testdata) before it is committed.
var update = flag.Bool("update", false, "rewrite the reference frames in testdata")

// dataSet names the data files a frame is drawn from.
type dataSet int

const (
	// dataFull holds the four fixtures, with emoji, CJK and hostile text.
	dataFull dataSet = iota

	// dataPlain holds the fixtures without the records whose text is not ASCII.
	dataPlain

	// dataEmpty holds no file at all.
	dataEmpty

	// dataBroken holds a connections file with a syntax error and valid other files.
	dataBroken

	// dataConfig holds a config.toml that sets some settings, with a comment.
	dataConfig

	// dataBadConfig holds a config.toml with a syntax error.
	dataBadConfig

	// dataVia holds one connection that cannot take another login.
	dataVia
)

// frameCase is one screen to draw: the data, the keys that lead to it and the options.
//
// Sizes replaces the size matrix when it is set, and each of them is a reference frame. Check
// lists more sizes that are checked with the rules of checkScreen and not kept. Keys entries
// starting with "type:" are typed text, the others are key names as in keyMsg.
type frameCase struct {
	name  string
	data  dataSet
	start Start
	keys  []string
	sizes [][2]int
	check [][2]int
	setup func(*Options)
}

// The sizes every frame is checked at.
var (
	// matrixWidths are the terminal widths of the matrix.
	matrixWidths = []int{60, 80, 100, 120}

	// matrixHeights are the terminal heights of the matrix.
	matrixHeights = []int{12, 24, 40}

	// goldenSizes are the sizes that are also kept as reference frames: compact, standard,
	// tall and wide.
	goldenSizes = [][2]int{{60, 12}, {80, 24}, {100, 40}, {120, 24}}

	// promptSizes are the sizes of the frames of the questions: compact and standard.
	promptSizes = [][2]int{{60, 12}, {80, 24}}

	// tinySizes are below the smallest usable terminal or right at its edge.
	tinySizes = [][2]int{{20, 5}, {23, 7}, {24, 6}, {24, 7}}

	// minimumSizes are the minimum and two sizes a little above it, where the compact layout is
	// the tightest.
	minimumSizes = [][2]int{{24, 7}, {26, 8}, {36, 10}}

	// smallSizes are the sizes where the too small screen changes tier, and a few in each tier.
	smallSizes = [][2]int{
		{1, 1},
		{5, 2},
		{11, 4},
		{12, 3},
		{19, 6},
		{20, 5},
		{20, 6},
		{23, 7},
		{23, 30},
		{24, 6},
		{24, 7},
		{26, 5},
		{26, 6},
		{120, 6},
		{60, 3},
	}

	// wideSizes is a terminal wider than every cap of ui.width, to see the margin of the page.
	wideSizes = [][2]int{{200, 30}}
)

// widthSetup returns the setup of a frame or a rig whose page has a given ui.width.
//
// Parameters:
//   - value: "auto", "120" or "160"
func widthSetup(value string) func(*Options) {
	return func(o *Options) { o.Settings.UI.Width = value }
}

// frameCases lists every screen and overlay of the interface.
func frameCases() []frameCase {
	cases := []frameCase{
		{name: "home", start: StartHome},
		{name: "homemenu", start: StartHome, keys: []string{"down", "down"}},
		{
			name: "homecompact", start: StartHome,
			setup: func(o *Options) { o.Settings.UI.Welcome = "compact" },
		},
		{
			name: "homeoff", start: StartHome,
			setup: func(o *Options) { o.Settings.UI.Welcome = "off" },
		},
		{name: "homeempty", data: dataEmpty, start: StartHome},
		{
			name: "homefrench", start: StartHome,
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},

		{name: "conn", start: StartConnections},
		{name: "connlast", start: StartConnections, keys: []string{"end"}},
		{name: "connfilter", start: StartConnections, keys: []string{"/", "type:e"}},
		{name: "connfilterdone", start: StartConnections, keys: []string{"/", "type:st", "enter"}},
		{name: "connnone", start: StartConnections, keys: []string{"/", "type:zzz"}},
		{name: "connreview", start: StartConnections, keys: []string{"down", "down", "down"}},
		{
			name: "connnever", start: StartConnections,
			setup: func(o *Options) { o.Settings.UI.Compact = "never" },
		},
		{
			name: "connalways", start: StartConnections,
			setup: func(o *Options) { o.Settings.UI.Compact = "always" },
		},
		{
			name: "connfrench", start: StartConnections,
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{name: "connerror", start: StartConnections, keys: []string{"toast"}},
		{name: "repo", start: StartRepos},
		{name: "repofilter", start: StartRepos, keys: []string{"/", "type:do"}},
		{name: "alias", start: StartAliases, keys: []string{"down"}},
		{name: "func", start: StartFuncs},
		{name: "funclast", start: StartFuncs, keys: []string{"end"}},

		{name: "formnewconn", start: StartConnections, keys: []string{"n"}},
		{name: "formnewrepo", start: StartRepos, keys: []string{"n"}},
		{name: "formnewalias", start: StartAliases, keys: []string{"n"}},
		{name: "formnewfunc", start: StartFuncs, keys: []string{"n"}},
		{
			name: "formtyped", start: StartConnections,
			keys: []string{
				"n", "type:db1", "tab", "type:db1.example.com", "tab", "tab",
				"type:ops",
			},
		},
		{name: "formerror", start: StartConnections, keys: []string{"n", "ctrl+s"}},
		{
			name: "formbad", start: StartConnections,
			keys: []string{"n", "type:a b", "tab", "type:x"},
		},
		{name: "formclash", start: StartConnections, keys: []string{
			"n", "type:prod", "tab",
			"type:x.example.com", "ctrl+s",
		}},
		{name: "formeditconn", start: StartConnections, keys: []string{"e"}},
		{name: "formeditlast", start: StartConnections, keys: []string{"end", "e"}},
		{name: "formeditrepo", start: StartRepos, keys: []string{"e"}},
		{name: "formeditalias", start: StartAliases, keys: []string{"e"}},
		{name: "formeditfunc", start: StartFuncs, keys: []string{"e"}},
		{
			name: "formfrench", start: StartConnections, keys: []string{"n", "ctrl+s"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},

		{name: "confirmconn", start: StartConnections, keys: []string{"d"}},
		{name: "confirmrepo", start: StartRepos, keys: []string{"d"}},
		{name: "confirmalias", start: StartAliases, keys: []string{"d"}},
		{name: "confirmfunc", start: StartFuncs, keys: []string{"d"}},
		{name: "confirmyes", start: StartConnections, keys: []string{"d", "right"}},
		{
			name: "confirmfrench", start: StartConnections, keys: []string{"d"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},

		{name: "palette", start: StartConnections, keys: []string{"ctrl+k"}},
		{name: "palettehome", start: StartHome, keys: []string{"ctrl+k"}},
		{name: "palettequery", start: StartConnections, keys: []string{"ctrl+k", "type:pro"}},
		{name: "palettescope", start: StartConnections, keys: []string{"ctrl+k", "type:r "}},
		{name: "paletteactions", start: StartConnections, keys: []string{"ctrl+k", "type:>"}},
		{name: "palettenone", start: StartConnections, keys: []string{"ctrl+k", "type:zzzz"}},
		{
			name: "palettefrench", start: StartConnections,
			keys:  []string{"ctrl+k", "down"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},

		{name: "emptyconn", data: dataEmpty, start: StartConnections},
		{name: "emptyrepo", data: dataEmpty, start: StartRepos},
		{name: "emptyalias", data: dataEmpty, start: StartAliases},
		{name: "emptyfunc", data: dataEmpty, start: StartFuncs},
		{name: "emptyform", data: dataEmpty, start: StartConnections, keys: []string{"n"}},
		{name: "emptypalette", data: dataEmpty, start: StartConnections, keys: []string{"ctrl+k"}},

		{name: "brokenconn", data: dataBroken, start: StartConnections},
		{name: "brokenhome", data: dataBroken, start: StartHome},
		{name: "brokennew", data: dataBroken, start: StartConnections, keys: []string{"n"}},
		{name: "brokenedit", data: dataBroken, start: StartConnections, keys: []string{"e"}},
		{name: "brokendelete", data: dataBroken, start: StartConnections, keys: []string{"d"}},
		{
			name: "brokenpalette", data: dataBroken, start: StartConnections,
			keys: []string{"ctrl+k"},
		},

		{name: "homeconfig", start: StartHome, keys: []string{"end"}},
		{name: "config", start: StartConfig},
		{name: "configfile", data: dataConfig, start: StartConfig, keys: []string{"down", "down"}},
		{name: "configlast", data: dataConfig, start: StartConfig, keys: []string{"end"}},
		{
			name: "configcolors", data: dataConfig, start: StartConfig,
			keys: []string{"end", "up", "enter"},
		},
		{
			name: "configlocked", data: dataConfig, start: StartConfig, keys: []string{"enter"},
			setup: func(o *Options) { o.Environ = append(o.Environ, "TOTE_LANG=en") },
		},
		{
			name: "configenv", data: dataConfig, start: StartConfig, keys: []string{"down"},
			setup: func(o *Options) { o.Environ = append(o.Environ, "EDITOR=nano") },
		},
		{name: "configbroken", data: dataBadConfig, start: StartConfig, keys: []string{"enter"}},
		{
			name: "configpick", data: dataConfig, start: StartConfig, keys: []string{"enter"},
			setup: func(o *Options) { o.Environ = append(o.Environ, "TOTE_LANG=") },
		},
		{
			name: "configpickdown", data: dataConfig, start: StartConfig,
			keys:  []string{"down", "down", "down", "enter", "down"},
			setup: func(o *Options) { o.Environ = append(o.Environ, "TOTE_LANG=") },
		},
		{
			name: "configprompt", data: dataConfig, start: StartConfig,
			keys: []string{"down", "enter"},
		},
		{
			name: "configpromptbad", data: dataConfig, start: StartConfig,
			keys: []string{"down", "down", "enter", "type:rel/dir"},
		},
		{
			name: "configfrench", data: dataConfig, start: StartConfig,
			keys:  []string{"down", "down", "down"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{
			name: "configpickfrench", data: dataConfig, start: StartConfig,
			keys:  []string{"down", "down", "down", "enter"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},

		{name: "help", start: StartHome, keys: []string{"?"}},
		{name: "helpscroll", start: StartHome, keys: []string{"?", "pgdown"}},
		{name: "helpend", start: StartHome, keys: []string{"?", "end"}},
		{name: "helpconn", start: StartConnections, keys: []string{"?"}},
		{name: "helprepo", start: StartRepos, keys: []string{"f1"}},
		{name: "helpconfig", start: StartConfig, keys: []string{"?"}},
		{name: "helpform", start: StartConnections, keys: []string{"n", "f1"}},
		{name: "helpfilter", start: StartConnections, keys: []string{"/", "f1"}},
		{
			name: "helpfrench", start: StartConnections, keys: []string{"?"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},

		{name: "connview", start: StartConnections, keys: []string{"v"}},
		{name: "connviewhostile", start: StartConnections, keys: []string{"end", "v"}},
		{name: "repoview", start: StartRepos, keys: []string{"v"}},
		{name: "aliasview", start: StartAliases, keys: []string{"v"}},
		{name: "funcview", start: StartFuncs, keys: []string{"v"}},
		{
			name: "connreviewview", start: StartConnections,
			keys: []string{"down", "down", "down", "v"},
		},
		{
			name: "connviewfrench", start: StartConnections, keys: []string{"v"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{name: "connuser", start: StartConnections, keys: []string{"u"}},
		{name: "connuserroot", start: StartConnections, keys: []string{"u", "u"}},
		{name: "connuserrefused", data: dataVia, start: StartConnections, keys: []string{"u"}},
		{name: "connpickrefused", data: dataVia, start: StartConnections, keys: []string{"U"}},
		{name: "connpick", start: StartConnections, keys: []string{"U"}},
		{name: "connpickother", start: StartConnections, keys: []string{"U", "end", "enter"}},
		{name: "conncopy", start: StartConnections, keys: []string{"C"}},
		{
			name: "conncopymanual", start: StartConnections, keys: []string{"C"},
			setup: func(o *Options) { o.Environ = append(o.Environ, "TERM=dumb") },
		},
		{name: "repocopy", start: StartRepos, keys: []string{"C"}},

		{name: "updatenotice", start: StartHome, keys: []string{"u"}},
		{
			name: "updatefound", start: StartHome, keys: []string{"U"},
			setup: func(o *Options) {
				o.CheckUpdate = func(context.Context) (UpdateCheck, error) {
					return UpdateCheck{Status: "available", Current: "1.2.3", Latest: "1.3.0"}, nil
				}
			},
		},
		{
			name: "updatefailed", start: StartHome, keys: []string{"U"},
			setup: func(o *Options) {
				o.CheckUpdate = func(context.Context) (UpdateCheck, error) {
					return UpdateCheck{}, errors.New("cannot reach the release server")
				}
			},
		},

		{
			name: "discard", start: StartConnections, sizes: promptSizes,
			keys: []string{"n", "type:db1", "esc"},
		},
		{
			name: "discardfrench", start: StartConnections, sizes: promptSizes,
			keys:  []string{"n", "type:db1", "esc"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{
			name: "discardascii", start: StartConnections, sizes: promptSizes,
			keys:  []string{"n", "type:db1", "esc"},
			setup: func(o *Options) { o.Printer, o.ASCII = newPrinter("fr"), true },
		},
		{
			name: "discardsetting", data: dataConfig, start: StartConfig, sizes: promptSizes,
			keys: []string{"down", "enter", "type:x", "ctrl+c"},
		},
		{
			name: "quitdirty", start: StartConnections, sizes: promptSizes,
			keys: []string{"n", "type:db1", "ctrl+d"},
		},
		{
			name: "quitdirtyfrench", start: StartConnections, sizes: promptSizes,
			keys:  []string{"n", "type:db1", "ctrl+d"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{
			name: "quitdirtyascii", start: StartConnections, sizes: promptSizes,
			keys:  []string{"n", "type:db1", "ctrl+d"},
			setup: func(o *Options) { o.Printer, o.ASCII = newPrinter("fr"), true },
		},
		{
			name: "quitwriting", start: StartHome, sizes: promptSizes,
			keys: []string{"writing", "ctrl+d"},
		},
		{
			name: "quitwritingfrench", start: StartHome, sizes: promptSizes,
			keys:  []string{"writing", "ctrl+d"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{name: "quitagain", start: StartHome, sizes: promptSizes, keys: []string{"ctrl+c"}},
		{
			name: "quitagainfrench", start: StartHome, sizes: promptSizes,
			keys:  []string{"ctrl+c"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{
			name: "quitagainascii", start: StartHome, sizes: promptSizes, keys: []string{"ctrl+c"},
			setup: func(o *Options) { o.Printer, o.ASCII = newPrinter("fr"), true },
		},
		{
			name: "resetsetting", data: dataConfig, start: StartConfig, sizes: promptSizes,
			keys: []string{"down", "down", "d"},
		},
		{
			name: "resetsettingfrench", data: dataConfig, start: StartConfig, sizes: promptSizes,
			keys:  []string{"down", "down", "d"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{
			name: "resetsettingascii", data: dataConfig, start: StartConfig, sizes: promptSizes,
			keys:  []string{"down", "down", "d"},
			setup: func(o *Options) { o.Printer, o.ASCII = newPrinter("fr"), true },
		},

		{name: "homewide120", start: StartHome, sizes: wideSizes, setup: widthSetup("120")},
		{name: "homewide160", start: StartHome, sizes: wideSizes, setup: widthSetup("160")},
		{name: "homewideauto", start: StartHome, sizes: wideSizes, setup: widthSetup("auto")},
		{name: "connwide120", start: StartConnections, sizes: wideSizes, setup: widthSetup("120")},
		{name: "connwide160", start: StartConnections, sizes: wideSizes, setup: widthSetup("160")},
		{
			name: "connwideauto", start: StartConnections, sizes: wideSizes,
			setup: widthSetup("auto"),
		},
		{
			name: "palettewide", start: StartConnections, keys: []string{"ctrl+k"},
			sizes: wideSizes, setup: widthSetup("auto"),
		},

		{
			name: "minhome", start: StartHome, keys: []string{"down", "down", "down", "down"},
			sizes: minimumSizes,
		},
		{
			name: "minhomeoff", start: StartHome, keys: []string{"down", "down"},
			sizes: minimumSizes,
			setup: func(o *Options) { o.Settings.UI.Welcome = "off" },
		},
		{
			name: "minhomenever", start: StartHome, sizes: minimumSizes,
			setup: func(o *Options) { o.Settings.UI.Compact = "never" },
		},
		{
			name: "minconn", start: StartConnections, keys: []string{"down", "down"},
			sizes: minimumSizes,
		},
		{
			name: "minconnnever", start: StartConnections, sizes: minimumSizes,
			setup: func(o *Options) { o.Settings.UI.Compact = "never" },
		},
		{
			name: "minfilter", start: StartConnections, keys: []string{"/", "type:st"},
			sizes: minimumSizes,
		},
		{name: "minempty", data: dataEmpty, start: StartRepos, sizes: minimumSizes},
		{
			name: "minconfig", data: dataConfig, start: StartConfig, keys: []string{"down", "down"},
			sizes: minimumSizes,
		},
		{
			name: "minconfigsaved", data: dataConfig, start: StartConfig,
			keys: []string{"down", "down", "down", "enter", "down", "enter"}, sizes: minimumSizes,
			setup: func(o *Options) { o.Environ = append(o.Environ, "TOTE_LANG=") },
		},
		{
			name: "minpick", data: dataConfig, start: StartConfig, keys: []string{"enter", "down"},
			sizes: minimumSizes,
			setup: func(o *Options) { o.Environ = append(o.Environ, "TOTE_LANG=") },
		},
		{
			name: "minform", start: StartConnections, keys: []string{"n", "type:ab"},
			sizes: minimumSizes,
		},
		{
			name: "minformerror", start: StartConnections, keys: []string{"n", "ctrl+s"},
			sizes: minimumSizes,
		},
		{name: "minformedit", start: StartRepos, keys: []string{"e", "tab"}, sizes: minimumSizes},
		{name: "minconfirm", start: StartConnections, keys: []string{"d"}, sizes: minimumSizes},
		{
			name: "minconfirmfrench", start: StartConnections, keys: []string{"d", "right"},
			sizes: minimumSizes, setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{
			name: "mindiscard", start: StartConnections, keys: []string{"n", "type:db1", "esc"},
			sizes: minimumSizes,
		},
		{
			name: "minpalette", start: StartConnections, keys: []string{"ctrl+k", "down"},
			sizes: minimumSizes,
		},
		{
			name: "minpalettequery", start: StartConnections, keys: []string{"ctrl+k", "type:pro"},
			sizes: minimumSizes,
		},
		{name: "minhelp", start: StartHome, keys: []string{"?", "pgdown"}, sizes: minimumSizes},
		{name: "minview", start: StartConnections, keys: []string{"v"}, sizes: minimumSizes},
		{
			name: "minfrench", start: StartHome, keys: []string{"down"}, sizes: minimumSizes,
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},

		{name: "toosmallhome", start: StartHome, sizes: tinySizes},
		{name: "toosmallconfig", start: StartConfig, sizes: tinySizes},
		{name: "toosmallhelp", start: StartHome, keys: []string{"?"}, sizes: tinySizes},
		{name: "toosmalllist", start: StartConnections, sizes: tinySizes},
		{name: "toosmallform", start: StartConnections, keys: []string{"n"}, sizes: tinySizes},
		{
			name: "toosmallpalette", start: StartConnections, keys: []string{"ctrl+k"},
			sizes: tinySizes,
		},
		{name: "toosmallsizes", start: StartHome, sizes: smallSizes},
		{
			name: "toosmallfrench", start: StartHome, sizes: smallSizes,
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{
			name: "toosmallascii", start: StartHome, sizes: smallSizes,
			setup: func(o *Options) { o.Printer, o.ASCII = newPrinter("fr"), true },
		},
		{
			name: "toosmallasciien", start: StartHome, sizes: smallSizes,
			setup: func(o *Options) { o.ASCII = true },
		},
	}

	cases = append(cases, stateFrames()...)
	cases = append(cases, clashFrames()...)
	cases = append(cases, pluginFrames()...)

	return append(cases, pluginRunFrames()...)
}

// frameMode is a combination of color profile and glyph set.
//
// ascii is true when the frame must hold nothing but ASCII.
type frameMode struct {
	name  string
	env   []string
	ascii bool
}

// frameModes lists the modes every frame is checked in. The first one is also the mode of the
// reference frames.
func frameModes() []frameMode {
	return []frameMode{
		{name: "plain", env: []string{"TOTE_COLOR=none"}},
		{name: "truecolor", env: []string{"TOTE_COLOR=truecolor"}},
		{name: "ansi", env: []string{"TOTE_COLOR=16"}},
		{name: "ascii", env: []string{"TOTE_COLOR=none", "LANG=C"}, ascii: true},
	}
}

// frameSandbox writes the data files of a data set below a temporary directory.
//
// Parameters:
//   - t: running test
//   - d: data set to write
//
// Returns a sandbox whose repositories have fixed paths, so that the frames do not depend on
// the name of the temporary directory.
func frameSandbox(t *testing.T, d dataSet) *sandbox {
	t.Helper()

	s := newSandbox(t)
	s.Opts.Home = "/home/tester"

	writeFile(t, s.Opts.Paths.ReposFile(), "schema = 1\n\n[[repo]]\nname = \"proj\"\n"+
		"path = \"/srv/work/proj\"\ndesc = \"This tool\"\n\n[[repo]]\nname = \"docs\"\n"+
		"path = \"~/missing/docs\"\n")

	switch d {
	case dataPlain:
		cut := strings.Index(fixtureConns, "[[connection]]\nname = \"emoji\"")
		writeFile(t, s.Opts.Paths.ConnectionsFile(), fixtureConns[:cut])
	case dataEmpty:
		s.Opts.Paths.Config = filepath.Join(s.Dir, "nothing")
	case dataBroken:
		writeFile(t, s.Opts.Paths.ConnectionsFile(), "schema = 1\n\n[[connection\nname = \"x\n")
	case dataConfig:
		writeFile(t, s.Opts.Paths.ConfigFile(), fixtureConfig)

		loaded, err := config.LoadSettings(s.Opts.Paths.ConfigFile())
		if err != nil {
			t.Fatal(err)
		}

		s.Opts.Settings = loaded
	case dataBadConfig:
		writeFile(t, s.Opts.Paths.ConfigFile(), "schema = 1\nlang = \"fr\n")
	case dataVia:
		writeFile(t, s.Opts.Paths.ConnectionsFile(), fixtureViaConns)
	case dataPlugins:
		newPlugLab(t, s).fixtures()
	case dataPlugRun:
		l := newPlugLab(t, s)
		l.runTools()
		l.installGft()
		writeFile(t, s.Opts.Paths.ReposFile(), "schema = 1\n\n[[repo]]\nname = \"proj\"\n"+
			"path = \"/usr\"\ndesc = \"This tool\"\n\n[[repo]]\nname = \"docs\"\n"+
			"path = \"~/missing/docs\"\n")
	}

	return s
}

// playFrame leads a rig to the screen of a frame case. The screen is drawn after every key, as the
// program does, so that an overlay knows how many rows it shows before the next key scrolls it.
//
// Parameters:
//   - g: rig to drive
//   - keys: key names, "type:text" for typed text, "writing" for a write in progress, "toast"
//     for an error message and "bg:dark" or "bg:light" for the answer of the terminal to the
//     query for its background
func playFrame(g *rig, keys []string) {
	g.t.Helper()

	for _, k := range keys {
		switch text, typed := strings.CutPrefix(k, "type:"); {
		case typed:
			g.typeText(text)
		case k == "writing":
			g.r.e.writing(1)
		case strings.HasPrefix(k, "pager:"):
			g.r.push(cannedRun(g.r.e, strings.TrimPrefix(k, "pager:")))
		case strings.HasPrefix(k, "bg:"):
			g.send(backgroundMsg(k == "bg:dark"))
		case k == "toast":
			g.send(toastMsg{
				Err:  true,
				Text: "a message that is rather long, long, long, long, long, long, long",
			})
		default:
			g.press(k)
		}

		g.r.render()
	}
}

// renderFrame draws one case at one size.
//
// Parameters:
//   - t: running test
//   - s: sandbox holding the data
//   - c: case to draw
//   - m: mode to draw it in
//   - size: width and height of the terminal
func renderFrame(t *testing.T, s *sandbox, c frameCase, m frameMode, size [2]int) string {
	t.Helper()

	o := s.with(func(o *Options) {
		o.Start = c.start
		o.Width, o.Height = size[0], size[1]
		o.Environ = append(append([]string(nil), o.Environ...), m.env...)

		if c.setup != nil {
			c.setup(o)
		}
	})

	g := newRig(t, o)
	playFrame(g, c.keys)

	frame := g.r.render()
	g.r.closeOverlays()

	return frame
}

// frameSizes returns the sizes a case is checked at.
//
// Parameters:
//   - c: case to size
func frameSizes(c frameCase) [][2]int {
	if c.sizes != nil {
		return append(slices.Clone(c.sizes), c.check...)
	}

	var out [][2]int

	for _, w := range matrixWidths {
		for _, h := range matrixHeights {
			out = append(out, [2]int{w, h})
		}
	}

	return out
}

// isGoldenSize reports whether a size is kept as a reference frame.
//
// Parameters:
//   - c: case the size belongs to
//   - size: width and height
func isGoldenSize(c frameCase, size [2]int) bool {
	if c.sizes != nil {
		return !slices.Contains(c.check, size)
	}

	for _, g := range goldenSizes {
		if g == size {
			return true
		}
	}

	return false
}

// trimFrame removes the styles and the trailing spaces of a frame.
//
// Parameters:
//   - frame: rendered screen
func trimFrame(frame string) string {
	lines := strings.Split(stripStyles(frame), "\n")

	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}

	return strings.Join(lines, "\n")
}

// TestFrames draws every screen and overlay of the interface at the sizes of the matrix, in
// every mode, and applies the rules of checkScreen: exactly the height in lines, no line wider
// than the screen under either width rule, no escape but styles, no control character and only
// ASCII in ASCII mode. The plain mode frames at the golden sizes are compared with the files of
// testdata, which -update rewrites.
//
// Parameters:
//   - t: test to report to
func TestFrames(t *testing.T) {
	sets := map[dataSet]*sandbox{}

	for _, d := range []dataSet{
		dataFull, dataPlain, dataEmpty, dataBroken, dataConfig,
		dataBadConfig, dataVia, dataPlugins, dataPlugRun,
	} {
		sets[d] = frameSandbox(t, d)
	}

	for _, c := range frameCases() {
		t.Run(c.name, func(t *testing.T) {
			var golden bytes.Buffer

			for mi, m := range frameModes() {
				d := c.data
				if m.ascii && d == dataFull {
					d = dataPlain
				}

				for _, size := range frameSizes(c) {
					frame := renderFrame(t, sets[d], c, m, size)
					name := fmt.Sprintf("%s %s %dx%d", c.name, m.name, size[0], size[1])

					checkScreen(t, name, frame, size[0], size[1], m.ascii)

					if mi == 0 && isGoldenSize(c, size) {
						fmt.Fprintf(&golden, "== %dx%d\n%s\n", size[0], size[1], trimFrame(frame))
					}
				}
			}

			compareGolden(t, filepath.Join("testdata", c.name+".golden"), golden.Bytes())
		})
	}
}

// compareGolden compares a text with a reference file, or rewrites the file with -update.
//
// Parameters:
//   - t: test to report to
//   - path: reference file
//   - got: text produced by the test
func compareGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run make golden, then review the file)", err)
	}

	if !bytes.Equal(want, got) {
		t.Errorf("%s differs from the reference frames; first difference:\n%s\n"+
			"run make golden and review the diff",
			path, firstDiff(string(want), string(got)))
	}
}

// firstDiff describes the first line where two texts differ.
//
// Parameters:
//   - want: reference text
//   - got: text produced by the test
func firstDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")

	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y string

		if i < len(a) {
			x = a[i]
		}

		if i < len(b) {
			y = b[i]
		}

		if x != y {
			return fmt.Sprintf("line %d\n  want %q\n  got  %q", i+1, x, y)
		}
	}

	return "no difference"
}

// TestFramesContent checks facts that the layout must show, independently of the reference
// frames, so that a reviewer who accepts a bad -update is still caught.
//
// Parameters:
//   - t: test to report to
func TestFramesContent(t *testing.T) {
	tests := []struct {
		name   string
		set    dataSet
		start  Start
		keys   []string
		size   [2]int
		want   []string
		absent []string
	}{
		{
			name: "form has its fields", start: StartConnections, keys: []string{"n"},
			size: [2]int{100, 30},
			want: []string{"New connection", "name", "target", "ctrl+s save"},
		},
		{
			name: "form errors stay visible", start: StartConnections,
			keys: []string{"n", "ctrl+s"}, size: [2]int{60, 12},
			want: []string{"New connection"},
		},
		{
			name: "confirm defaults to no", start: StartConnections, keys: []string{"d"},
			size: [2]int{100, 30}, want: []string{"Delete", "prod", "No", "Yes"},
		},
		{
			name: "palette lists everything", start: StartConnections, keys: []string{"ctrl+k"},
			size: [2]int{100, 40}, want: []string{"prod", "proj", "gs", "hello"},
		},
		{
			name: "empty list says so", set: dataEmpty, start: StartConnections,
			size: [2]int{100, 30}, absent: []string{"prod"},
		},
		{
			name: "broken file is read only", set: dataBroken, start: StartConnections,
			keys: []string{"n"}, size: [2]int{100, 30}, absent: []string{"New connection"},
		},
		{
			name: "too small", start: StartHome, size: [2]int{23, 10},
			want: []string{"Terminal too small"}, absent: []string{"Connections"},
		},
	}

	for _, tt := range tests {
		s := frameSandbox(t, tt.set)
		c := frameCase{name: tt.name, start: tt.start, keys: tt.keys}
		frame := renderFrame(t, s, c, frameModes()[0], tt.size)
		text := stripStyles(frame)

		for _, w := range tt.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: lacks %q:\n%s", tt.name, w, text)
			}
		}

		for _, a := range tt.absent {
			if strings.Contains(text, a) {
				t.Errorf("%s: shows %q:\n%s", tt.name, a, text)
			}
		}
	}
}

// TestFrameSandbox checks that the reference frames do not depend on the host: the sandbox
// paths must not show in any frame of the plain mode.
//
// Parameters:
//   - t: test to report to
func TestFrameSandbox(t *testing.T) {
	sets := map[dataSet]*sandbox{
		dataFull:    frameSandbox(t, dataFull),
		dataPlugins: frameSandbox(t, dataPlugins), dataPlugRun: frameSandbox(t, dataPlugRun),
	}

	for _, c := range frameCases() {
		s := sets[dataFull]
		if c.data == dataPlugins || c.data == dataPlugRun {
			s = sets[c.data]
		}

		dir := filepath.Dir(s.Opts.Paths.Config)

		for _, size := range goldenSizes {
			if strings.Contains(stripStyles(renderFrame(t, s, c, frameModes()[0], size)), dir) {
				t.Errorf("%s %dx%d shows the sandbox directory %s", c.name, size[0], size[1], dir)
			}
		}
	}
}
