package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// sgr matches the only escape sequences the interface is allowed to draw with.
var sgr = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// checkScreen verifies the rules that every frame must follow.
//
// Parameters:
//   - t: test to report to
//   - name: description of the frame for the messages
//   - screen: rendered frame
//   - w: width of the terminal
//   - h: height of the terminal
//   - ascii: true when the frame must hold nothing but ASCII
//
// The frame must have exactly h lines, none wider than w cells under the wide character rule or
// the grapheme rule, hold no escape sequence but styles, and no control character.
func checkScreen(t *testing.T, name, screen string, w, h int, ascii bool) {
	t.Helper()

	lines := strings.Split(screen, "\n")

	if len(lines) != h {
		t.Errorf("%s: %d lines, want %d", name, len(lines), h)
	}

	for i, l := range lines {
		if gw, wc := ansi.StringWidth(l), ansi.StringWidthWc(l); gw > w || wc > w {
			t.Errorf("%s: line %d is %d/%d cells wide, limit %d: %q", name, i, wc, gw, w,
				stripStyles(l))
		}

		bare := sgr.ReplaceAllString(l, "")

		for _, r := range bare {
			if unsafeRune(r) {
				t.Errorf("%s: line %d holds %U: %q", name, i, r, bare)
				break
			}

			if ascii && r > 0x7e {
				t.Errorf("%s: line %d holds the non-ASCII %U in ASCII mode: %q", name, i, r, bare)
				break
			}
		}
	}
}

// viewCase is one frame to draw. The French cases run in the ASCII modes too, where the catalog
// text is folded to ASCII.
type viewCase struct {
	name  string
	start Start
	keys  []string
	toast string
	setup func(*Options)
}

// TestViewMatrix draws every screen at every size and in every color mode, and applies the rules
// of checkScreen to each frame.
//
// Parameters:
//   - t: test to report to
func TestViewMatrix(t *testing.T) {
	cases := []viewCase{
		{name: "home", start: StartHome},
		{name: "home down", start: StartHome, keys: []string{"down", "down"}},
		{name: "connections", start: StartConnections},
		{name: "connections last", start: StartConnections, keys: []string{"end"}},
		{name: "connections filter", start: StartConnections, keys: []string{"/", "e", "x"}},
		{
			name: "connections no result", start: StartConnections,
			keys: []string{"/", "z", "z", "z"},
		},
		{name: "repos", start: StartRepos},
		{name: "aliases", start: StartAliases, keys: []string{"down"}},
		{name: "funcs", start: StartFuncs},
		{
			name: "toast", start: StartConnections,
			toast: "a message that is rather long, long, long, long, long, long, long, long",
		},
		{
			name: "welcome compact", start: StartHome,
			setup: func(o *Options) { o.Settings.UI.Welcome = "compact" },
		},
		{
			name: "welcome off", start: StartHome,
			setup: func(o *Options) { o.Settings.UI.Welcome = "off" },
		},
		{
			name: "never compact", start: StartConnections,
			setup: func(o *Options) { o.Settings.UI.Compact = "never" },
		},
		{
			name: "french", start: StartConnections,
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{name: "home settings", start: StartHome, keys: []string{"end"}},
		{name: "config", start: StartConfig},
		{name: "config last", start: StartConfig, keys: []string{"end"}},
		{name: "config pick", start: StartConfig, keys: []string{"enter"}},
		{name: "config prompt", start: StartConfig, keys: []string{"down", "enter", "x"}},
		{
			name: "config french", start: StartConfig, keys: []string{"down"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{name: "help home", start: StartHome, keys: []string{"?"}},
		{name: "help list end", start: StartConnections, keys: []string{"?", "end"}},
		{name: "help config", start: StartConfig, keys: []string{"f1", "pgdown"}},
		{
			name: "help french", start: StartAliases, keys: []string{"?"},
			setup: func(o *Options) { o.Printer = newPrinter("fr") },
		},
		{name: "details", start: StartConnections, keys: []string{"end", "v"}},
		{name: "details repo", start: StartRepos, keys: []string{"v"}},
		{name: "pick user", start: StartConnections, keys: []string{"U"}},
		{name: "copied", start: StartConnections, keys: []string{"C"}},
		{name: "home 160", start: StartHome, setup: widthSetup("160")},
		{name: "home 120", start: StartHome, setup: widthSetup("120")},
		{name: "home auto", start: StartHome, setup: widthSetup("auto")},
		{name: "connections 160", start: StartConnections, setup: widthSetup("160")},
		{name: "connections 120", start: StartConnections, setup: widthSetup("120")},
		{name: "connections auto", start: StartConnections, setup: widthSetup("auto")},
		{name: "repos auto", start: StartRepos, setup: widthSetup("auto")},
		{
			name: "config auto", start: StartConfig, keys: []string{"down"},
			setup: widthSetup("auto"),
		},
		{
			name: "palette auto", start: StartConnections, keys: []string{"ctrl+k"},
			setup: widthSetup("auto"),
		},
		{
			name: "details 160", start: StartConnections, keys: []string{"v"},
			setup: widthSetup("160"),
		},
	}

	modes := []struct {
		name  string
		env   []string
		ascii bool
	}{
		{"truecolor", []string{"TOTE_COLOR=truecolor"}, false},
		{"256", []string{"TOTE_COLOR=256"}, false},
		{"16", []string{"TOTE_COLOR=16"}, false},
		{"none", []string{"TOTE_COLOR=none"}, false},
		{"no_color", []string{"NO_COLOR=1", "TOTE_COLOR="}, false},
		{"ascii", []string{"TOTE_COLOR=none", "LANG=C"}, true},
		{"linux console", []string{"TERM=linux"}, true},
	}

	base := newSandbox(t)
	plain := newSandbox(t)

	cut := strings.Index(fixtureConns, "[[connection]]\nname = \"emoji\"")
	writeFile(t, plain.Opts.Paths.ConnectionsFile(), fixtureConns[:cut])

	for _, m := range modes {
		src := base
		if m.ascii {
			src = plain
		}

		for _, w := range []int{60, 80, 100, 110, 120, 160, 200} {
			for _, h := range []int{12, 24, 40} {
				for _, c := range cases {
					s := src.with(func(o *Options) {
						o.Start = c.start
						o.Width, o.Height = w, h
						o.Environ = append(append([]string(nil), o.Environ...), m.env...)

						if c.setup != nil {
							c.setup(o)
						}
					})

					g := newRig(t, s)
					g.press(c.keys...)

					if c.toast != "" {
						g.r.Update(toastMsg{Text: c.toast, Err: true})
					}

					name := fmt.Sprintf("%s %s %dx%d", m.name, c.name, w, h)
					checkScreen(t, name, g.r.render(), w, h, m.ascii)
				}
			}
		}
	}
}

// TestViewSmallSizes draws the smallest and the oddest sizes: nothing may panic, wrap or
// overflow.
//
// Parameters:
//   - t: test to report to
func TestViewSmallSizes(t *testing.T) {
	starts := []Start{
		StartHome, StartConnections, StartRepos, StartAliases, StartFuncs, StartConfig,
	}

	scripts := [][]string{{"end", "/", "a"}, {"?", "end"}, {"enter", "down"}, {"v", "?"}}

	for _, start := range starts {
		for _, size := range [][2]int{
			{1, 1},
			{5, 3},
			{23, 7},
			{24, 6},
			{24, 7},
			{25, 8},
			{39, 10},
			{40, 9},
			{40, 10},
			{41, 11},
			{69, 19},
			{70, 20},
			{71, 21},
			{200, 8},
			{10, 100},
			{300, 80},
		} {
			for i, keys := range scripts {
				s := newSandbox(t).with(func(o *Options) {
					o.Start = start
					o.Width, o.Height = size[0], size[1]
				})

				g := newRig(t, s)
				g.press(keys...)

				checkScreen(t, fmt.Sprintf("start %d script %d %dx%d", start, i, size[0], size[1]),
					g.r.render(), size[0], size[1], false)
			}
		}
	}
}

// TestViewOverlay draws an overlay over every size.
//
// Parameters:
//   - t: test to report to
func TestViewOverlay(t *testing.T) {
	for _, size := range [][2]int{{24, 7}, {40, 10}, {60, 12}, {100, 24}, {120, 40}} {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Width, o.Height = size[0], size[1]
		}))

		g.send(openOverlayMsg{Overlay: &probeOverlay{}})

		name := fmt.Sprintf("overlay %dx%d", size[0], size[1])
		checkScreen(t, name, g.r.render(), size[0], size[1], false)

		if !strings.Contains(g.text(), "probe") {
			t.Errorf("%s: the overlay is not drawn:\n%s", name, g.text())
		}
	}
}

// TestViewContent checks what each layout shows, in plain ASCII so the text is exact.
//
// Parameters:
//   - t: test to report to
func TestViewContent(t *testing.T) {
	ascii := func(o *Options) { o.Environ = append(o.Environ, "TOTE_COLOR=none", "LANG=C") }

	tests := []struct {
		name   string
		w, h   int
		start  Start
		keys   []string
		want   []string
		absent []string
	}{
		{
			name: "full list has the panel", w: 100, h: 40, start: StartConnections,
			want: []string{
				"NAME", "TARGET", "USER", "DESCRIPTION", "prod", "Production web",
				"-> ssh -l deploy -- prod.example.com", "+- prod -", "> prod",
			},
		},
		{
			name: "panel from 24 rows", w: 100, h: 24, start: StartConnections,
			want: []string{"-> ssh -l deploy -- prod.example.com", "+- prod -"},
		},
		{
			name: "no panel below 24 rows", w: 100, h: 23, start: StartConnections,
			want:   []string{"NAME", "-> ssh -l deploy -- prod.example.com"},
			absent: []string{"+- prod -"},
		},
		{
			name: "compact list", w: 60, h: 30, start: StartConnections,
			want:   []string{">_ tote > Connections", "-> ssh -l deploy -- prod.example.com"},
			absent: []string{"NAME", "+- prod -"},
		},
		{
			name: "via is shown", w: 100, h: 30, start: StartConnections,
			keys: []string{"down", "down"},
			want: []string{"mosh", "via mosh", "ip 10.0.0.9"},
		},
		{
			name: "needs review", w: 100, h: 40, start: StartConnections,
			keys: []string{"/", "l", "e", "g"},
			want: []string{"> legacy", "! connection needs review", "! needs review, edit it"},
		},
		{
			name: "hostile text", w: 100, h: 40, start: StartConnections,
			keys: []string{"/", "h", "o", "s", "t"},
			want: []string{"a?[31mb?c?d"}, absent: []string{"\x1b[31m"},
		},
		{
			name: "repo panel", w: 100, h: 30, start: StartRepos,
			want: []string{"proj", "This tool", "docs", "~/missing/docs"},
		},
		{
			name: "func panel", w: 100, h: 30, start: StartFuncs,
			want: []string{"hello", "source", "tool", "~/bin/hello.sh"},
		},
		{
			name: "tiny", w: 23, h: 30, start: StartHome,
			want: []string{"Terminal too small"}, absent: []string{"Connections"},
		},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			ascii(o)
			o.Start = tt.start
			o.Width, o.Height = tt.w, tt.h
		}))

		g.press(tt.keys...)

		raw := g.r.render()
		text := stripStyles(raw)

		for _, w := range tt.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: lacks %q:\n%s", tt.name, w, text)
			}
		}

		for _, a := range tt.absent {
			if strings.Contains(text, a) || strings.Contains(sgr.ReplaceAllString(raw, ""), a) {
				t.Errorf("%s: shows %q:\n%s", tt.name, a, text)
			}
		}
	}
}

// TestViewEmojiRow checks that a row with emoji, joiners and CJK text is cut without wrapping
// at any width.
//
// Parameters:
//   - t: test to report to
func TestViewEmojiRow(t *testing.T) {
	for w := 40; w <= 130; w++ {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Width, o.Height = w, 30
		}))

		g.press("/", "e", "m", "o")
		checkScreen(t, fmt.Sprintf("emoji row %d", w), g.r.render(), w, 30, false)
	}
}

// TestFooterIsGenerated checks that the footer lists the bindings of the state.
//
// Parameters:
//   - t: test to report to
func TestFooterIsGenerated(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartRepos
		o.Width, o.Height = 120, 30
	}))

	if got := stripStyles(g.view()[len(g.view())-1]); !strings.Contains(got, "o editor") ||
		!strings.Contains(got, "go") {
		t.Errorf("repository footer = %q", got)
	}

	g.press("/")

	if got := stripStyles(g.view()[len(g.view())-1]); !strings.Contains(got, "apply") ||
		strings.Contains(got, "quit") {
		t.Errorf("filter footer = %q", got)
	}
}

// TestStatusRender checks the status line.
//
// Parameters:
//   - t: test to report to
func TestStatusRender(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	if got := (statusInfo{}).render(e, 80); got != "" {
		t.Errorf("empty status = %q", got)
	}

	full := statusInfo{Command: "-> ssh host", Branch: "main", Update: "v2 available"}
	got := stripStyles(full.render(e, 80))

	for _, want := range []string{"-> ssh host", "branch main", "v2 available"} {
		if !strings.Contains(got, want) {
			t.Errorf("status lacks %q: %q", want, got)
		}
	}

	if w := e.Metric.width(full.render(e, 20)); w > 20 {
		t.Errorf("status at 20 columns is %d cells", w)
	}

	if got := full.render(e, 3); got != "" {
		t.Errorf("status at 3 columns = %q", got)
	}

	if text, ok := commandOf(e, item{Rec: 5}, ""); text != "" || ok {
		t.Errorf("commandOf on a non-connection = %q, %v", text, ok)
	}

	if text := summaryOf(e, item{Rec: 5}, ""); text != "" {
		t.Errorf("summaryOf on an unknown record = %q", text)
	}

	for _, k := range allKinds {
		for _, it := range e.Data.items(k) {
			if summaryOf(e, it, "") == "" {
				t.Errorf("kind %d: %s has no summary", k, it.Key)
			}
		}
	}
}
