package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/shell"
)

// longAliases writes forty aliases a00 to a39 in the sandbox of a rig and reloads the data, so
// that a list of aliases has to scroll.
//
// Parameters:
//   - g: rig to fill
func longAliases(g *rig) {
	g.t.Helper()

	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i := range 40 {
		fmt.Fprintf(&b, "\n[[alias]]\nname = \"a%02d\"\ncmd = \"echo %d\"\n", i, i)
	}

	writeFile(g.t, filepath.Join(g.r.e.Paths.Config, "aliases.toml"), b.String())
	g.send(reloadMsg{})
}

// TestReviewDoubleClickWindowIsFourHundredMilliseconds pins the length of the double click
// window in absolute time. The other tests move the clock by doubleClickWindow itself, so a
// window of five seconds, or of any other length, passed them all. The documentation promises
// 400 ms: 399 ms is a double click and 401 ms is not.
//
// Parameters:
//   - t: test to report to
func TestReviewDoubleClickWindowIsFourHundredMilliseconds(t *testing.T) {
	tests := []struct {
		name   string
		gap    time.Duration
		double bool
	}{
		{"at once", 0, true},
		{"a quarter of a second", 250 * time.Millisecond, true},
		{"just inside", 399 * time.Millisecond, true},
		{"just outside", 401 * time.Millisecond, false},
		{"half a second", 500 * time.Millisecond, false},
		{"a second", time.Second, false},
		{"a minute", time.Minute, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, clk := mouseRig(t, 100, 30, StartConnections, nil)

			clickOn(g, "staging")
			clk.advance(tt.gap)
			clickOn(g, "staging")

			if _, got := seenActivate(g); got != tt.double {
				t.Errorf("second click after %v: double click %v, want %v", tt.gap, got, tt.double)
			}
		})
	}
}

// TestReviewScrollBetweenTwoClicksIsNoDoubleClick checks that two clicks on the same line of the
// terminal are not a double click when the wheel moved another record under that line between
// them. Both clicks fall within 400 ms and on the same line, so only the identity of the record
// tells them apart: without it the second click would connect to, or insert the command of, a
// record that was never clicked before.
//
// Parameters:
//   - t: test to report to
func TestReviewScrollBetweenTwoClicksIsNoDoubleClick(t *testing.T) {
	g, clk := mouseRig(t, 100, 20, StartAliases, nil)
	longAliases(g)

	l := g.r.scr.(*listScreen)
	y, x := lineOf(g, "a02")
	first := stripStyles(g.view()[y])

	g.click(x, y)
	clk.advance(50 * time.Millisecond)
	g.send(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})

	second := stripStyles(g.view()[y])
	if second == first {
		t.Fatalf("the wheel left the same record on line %d: %q", y, second)
	}

	clk.advance(50 * time.Millisecond)
	g.click(x, y)

	if a, ok := seenActivate(g); ok || g.Quit {
		t.Errorf("a click on another record under the same line activated %+v (quit %v)", a, g.Quit)
	}

	want := regexp.MustCompile(`a\d\d`).FindString(second)
	if it := l.selected(); it == nil || it.Key != want {
		t.Errorf("selected = %+v, want %s, the record now on line %d: %q", it, want, y, second)
	}
}

// TestReviewHitEdges checks the first and the last cell of every hit of a screen or of an overlay
// and the two cells around it. The hit tests of the other tests click on the middle of a text,
// so a hit one cell too wide or one cell too narrow passed all of them.
//
// Parameters:
//   - t: test to report to
func TestReviewHitEdges(t *testing.T) {
	opens := []struct {
		name  string
		start Start
		keys  []string
	}{
		{"home", StartHome, nil},
		{"connections", StartConnections, nil},
		{"aliases", StartAliases, nil},
		{"config", StartConfig, nil},
		{"palette", StartHome, []string{"ctrl+k"}},
		{"form", StartConnections, []string{"n"}},
		{"confirm", StartConnections, []string{"d"}},
		{"pick", StartConnections, []string{"U"}},
	}

	for _, size := range [][2]int{{60, 14}, {100, 30}, {121, 30}, {160, 40}} {
		for _, o := range opens {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], o.name), func(t *testing.T) {
				g, _ := mouseRig(t, size[0], size[1], o.start, nil)
				g.press(o.keys...)

				_, hits := g.r.compose(true)
				if len(hits) == 0 {
					t.Fatal("the screen has no hit")
				}

				for _, h := range hits {
					for _, x := range []int{h.x, h.x + h.w - 1} {
						if got, ok := g.r.hitAt(x, h.y); !ok || got != h {
							t.Errorf("cell %d,%d of %+v gives %+v, %v", x, h.y, h, got, ok)
						}
					}

					for _, x := range []int{h.x - 1, h.x + h.w} {
						if got, ok := g.r.hitAt(x, h.y); ok && got == h {
							t.Errorf("cell %d,%d outside %+v is still on it", x, h.y, h)
						}
					}
				}
			})
		}
	}
}

// TestReviewHitsDoNotOverlap checks that no two hits of a frame share a cell, so that the order
// in which hitAt looks at them cannot matter.
//
// Parameters:
//   - t: test to report to
func TestReviewHitsDoNotOverlap(t *testing.T) {
	opens := []struct {
		name  string
		start Start
		keys  []string
	}{
		{"home", StartHome, nil},
		{"connections", StartConnections, nil},
		{"repos", StartRepos, nil},
		{"aliases", StartAliases, nil},
		{"funcs", StartFuncs, nil},
		{"config", StartConfig, nil},
		{"filter", StartConnections, []string{"/"}},
		{"palette", StartHome, []string{"ctrl+k"}},
		{"form", StartConnections, []string{"n"}},
		{"confirm", StartConnections, []string{"d"}},
		{"help", StartConnections, []string{"?"}},
		{"details", StartConnections, []string{"v"}},
		{"pick", StartConnections, []string{"U"}},
	}

	for _, size := range [][2]int{{40, 10}, {60, 12}, {80, 24}, {100, 30}, {160, 40}} {
		for _, o := range opens {
			g, _ := mouseRig(t, size[0], size[1], o.start, nil)
			g.press(o.keys...)

			_, hits := g.r.compose(true)

			for i, a := range hits {
				for _, b := range hits[i+1:] {
					if a.y == b.y && a.x < b.x+b.w && b.x < a.x+a.w {
						t.Errorf("%dx%d %s: hits %+v and %+v share cells", size[0], size[1], o.name,
							a, b)
					}
				}
			}
		}
	}
}

// TestReviewFooterClicksOnAWideTerminal checks that a click on a hint of the footer presses its
// key when the page has a margin, and in an overlay that is centered. The footer tests of the
// other files run at 120 columns, where the margin is zero, so a footer hit that forgot the
// margin passed all of them.
//
// Parameters:
//   - t: test to report to
func TestReviewFooterClicksOnAWideTerminal(t *testing.T) {
	for _, cols := range []int{121, 140, 160, 200} {
		t.Run(fmt.Sprintf("%d", cols), func(t *testing.T) {
			g, _ := mouseRig(t, cols, 30, StartAliases, widthSetup("120"))

			clickFooter(g, "n new")

			if _, ok := g.r.overlayTop().(*form); !ok {
				t.Fatalf("the hint n did not open the form: %T", g.r.overlayTop())
			}

			clickFooter(g, "esc cancel")

			if g.r.overlayTop() != nil {
				t.Fatal("the hint esc did not close the form")
			}

			clickFooter(g, "ctrl+k")

			if _, ok := g.r.overlayTop().(*paletteOverlay); !ok {
				t.Fatalf("the hint ctrl+k did not open the palette: %T", g.r.overlayTop())
			}

			clickFooter(g, "esc close")

			if g.r.overlayTop() != nil {
				t.Fatal("the hint esc did not close the palette")
			}

			g2, _ := mouseRig(t, cols, 30, StartHome, nil)
			clickFooter(g2, g2.r.e.Glyphs.enter+" open")

			if _, ok := g2.r.scr.(*listScreen); !ok {
				t.Errorf("the hint enter did not open the selected entry: %T", g2.r.scr)
			}
		})
	}
}

// TestReviewTinyScreenIgnoresTheWheel checks that the wheel does nothing on a terminal that is too
// small to draw the interface: a click finds no hit there, but the wheel is not a hit, so only
// the check of the layout keeps it from scrolling a list nobody can see.
//
// Parameters:
//   - t: test to report to
func TestReviewTinyScreenIgnoresTheWheel(t *testing.T) {
	g, _ := mouseRig(t, 100, 20, StartAliases, nil)
	longAliases(g)

	l := g.r.scr.(*listScreen)

	g.resize(minWidth-1, minHeight+1)

	if g.r.e.Layout != layoutTiny {
		t.Fatalf("a %dx%d terminal has the layout %v, want the tiny one", g.r.e.Width, g.r.e.Height,
			g.r.e.Layout)
	}

	for range 4 {
		g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	}

	if l.sel != 0 || l.off != 0 {
		t.Errorf("the wheel scrolled a list behind the too small message: sel %d off %d", l.sel,
			l.off)
	}
}

// TestReviewQuitWindowIsTwoSeconds pins the length of the window of the second Ctrl-C in absolute
// time. TestQuitWindow moves the clock by quitWindow itself, so a window of twenty seconds, or one
// that never ends, passed it. The message says that a second Ctrl-C within two seconds quits.
//
// Parameters:
//   - t: test to report to
func TestReviewQuitWindowIsTwoSeconds(t *testing.T) {
	tests := []struct {
		name string
		gap  time.Duration
		quit bool
	}{
		{"at once", 0, true},
		{"one second", time.Second, true},
		{"just inside", 1900 * time.Millisecond, true},
		{"just outside", 2100 * time.Millisecond, false},
		{"five seconds", 5 * time.Second, false},
		{"a minute", time.Minute, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, clk := keyRig(t, StartHome)

			g.press("ctrl+c")
			clk.advance(tt.gap)
			g.press("ctrl+c")

			if g.Quit != tt.quit {
				t.Errorf("a second Ctrl-C after %v: quit %v, want %v", tt.gap, g.Quit, tt.quit)
			}
		})
	}
}

// TestReviewRankCategoryBeatsWordMatches checks the order of a query that is also the start of
// a category: a connection whose own name has the query at the start of a word, such as "my-conn"
// for "conn", stays among the connections in the order of the file, instead of falling behind
// them, and the names that merely contain the query come after all of them.
//
// Parameters:
//   - t: test to report to
func TestReviewRankCategoryBeatsWordMatches(t *testing.T) {
	conn := func(name string) entry {
		return entry{
			Scope: scopeConn, Names: []string{name}, Name: name, Low: "connections",
			Text: name + " connections",
		}
	}
	alias := func(name string) entry {
		return entry{
			Scope: scopeAlias, Names: []string{name}, Name: name, Low: "aliases",
			Text: name + " aliases",
		}
	}

	entries := []entry{
		conn("prod"),
		conn("my-conn"),
		alias("reconnect"),
		conn("staging"),
		alias("connect"),
		alias("db-conn"),
		conn("conn"),
	}

	want := []string{"conn", "connect", "prod", "my-conn", "staging", "db-conn", "reconnect"}

	var got []string

	for _, i := range rank(entries, scopeAll, "conn") {
		got = append(got, entries[i].Name)
	}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rank for conn = %v, want %v", got, want)
	}
}

// TestReviewResizeClearsTheSelectionWhateverTheText checks that a resize forgets a selection even
// when the text under the highlighted cells is the same in the new frame. TestSelectionClears
// resizes in width, which changes the columns, so the selection also went away because the
// content under it differed; a change of height alone keeps the top of a list as it was, and the
// highlight would then stay on cells that no longer mean what the user dragged over.
//
// Parameters:
//   - t: test to report to
func TestReviewResizeClearsTheSelectionWhateverTheText(t *testing.T) {
	var copied []string

	g, _ := selectionRig(t, StartConnections, &copied, nil)
	y, x := lineOf(g, "prod")
	before := strings.Join(g.view()[:y+1], "\n")

	drag(g, cellPos{x, y}, cellPos{x + 3, y})

	if !strings.Contains(strings.Join(markedScreen(g), ""), "[[") {
		t.Fatal("no highlight to clear")
	}

	g.resize(100, 31)

	if after := strings.Join(g.view()[:y+1], "\n"); stripStyles(after) != stripStyles(before) {
		t.Fatalf("the top of the screen changed with the height, the test needs it to stay")
	}

	if got := strings.Join(markedScreen(g), "\n"); strings.Contains(got, "[[") {
		t.Errorf("the highlight survived a resize:\n%s", got)
	}

	if g.r.sel.drag {
		t.Error("the selection is still held after a resize")
	}
}

// TestReviewQuestionFocusIsDrawn checks that the answer which has the focus in a confirmation or
// a question is drawn with the style of the selection, and the other one is not. The reference
// frames keep only the text, so the frame of the question with the focus on "Yes" is the same
// file as the one with the focus on "No", and a focus that nothing shows passed every test.
//
// Parameters:
//   - t: test to report to
func TestReviewQuestionFocusIsDrawn(t *testing.T) {
	tests := []struct {
		name  string
		start Start
		keys  []string
	}{
		{"delete a record", StartConnections, []string{"d"}},
		{"discard a form", StartConnections, []string{"n", "type:x", "esc"}},
		{"quit with a dirty form", StartConnections, []string{"n", "type:x", "ctrl+d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, clk := keyRig(t, tt.start)
			playKeys(g, clk, tt.keys...)

			th := g.r.e.Theme
			no, yes := "[ No ]", "[ Yes ]"

			if th.Selected.Render(no) == no || th.Selected.Render(no) == th.Text.Render(no) {
				t.Fatalf("the selection style %q looks like the text", th.Selected.Render(no))
			}

			screen := func() string { return strings.Join(g.view(), "\n") }

			if s := screen(); !strings.Contains(s, th.Selected.Render(no)) ||
				strings.Contains(s, th.Selected.Render(yes)) {
				t.Errorf("the focus starts on No, which should be drawn selected:\n%q", s)
			}

			g.press("right")

			if s := screen(); !strings.Contains(s, th.Selected.Render(yes)) ||
				strings.Contains(s, th.Selected.Render(no)) {
				t.Errorf("after right the focus is on Yes, which should be drawn selected:\n%q", s)
			}
		})
	}
}

// TestReviewClipboardLimitsAreTheDocumentedOnes pins the two limits of the copy of a selection
// in absolute terms: a clipboard program is given two seconds, and a text of more than 100 KiB is
// not sent to the terminal as an OSC 52 sequence. The other tests compare with the constants
// themselves, so a limit ten times too generous passed them all, and the interface would freeze
// for twenty seconds on a clipboard program that hangs.
//
// Parameters:
//   - t: test to report to
func TestReviewClipboardLimitsAreTheDocumentedOnes(t *testing.T) {
	if clipToolTimeout != 2*time.Second {
		t.Errorf("clipToolTimeout = %v, want 2s", clipToolTimeout)
	}

	if maxOSC != 100*1024 {
		t.Errorf("maxOSC = %d, want 100 KiB", maxOSC)
	}
}

// TestReviewClipboardToolIsNeverRunFromTheCurrentDirectory checks that a clipboard program is
// looked up in the absolute directories of PATH only. A repository can hold a file called xclip,
// and a PATH with ".", an empty entry or a relative one would run it when the user selects text
// with the mouse. No other test puts a relative entry in PATH, so the check could be removed
// without any test noticing.
//
// Parameters:
//   - t: test to report to
func TestReviewClipboardToolIsNeverRunFromTheCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := "#!/bin/sh\ncat > /dev/null\necho ran > '" + marker + "'\n"

	for _, p := range []string{filepath.Join(dir, "xclip"), filepath.Join(dir, "bin", "xclip")} {
		writeFile(t, p, script)

		if err := os.Chmod(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	t.Chdir(dir)

	relative := []string{".", "./bin", "bin", "", ":bin:.", "bin:" + filepath.Join(dir, "none")}

	for _, path := range relative {
		if got, ok := shell.LookProgram(path, "xclip"); ok {
			t.Errorf("shell.LookProgram(%q) found %q, a program of a relative directory", path, got)
		}

		environ := []string{"PATH=" + path, "DISPLAY=:0"}
		if got := copyWithTools(context.Background(), environ, "text"); got != "" {
			t.Errorf("copyWithTools with PATH %q used %q", path, got)
		}
	}

	if _, err := os.Stat(marker); err == nil {
		t.Error("a program of the current directory ran")
	}

	want := filepath.Join(dir, "bin", "xclip")
	if got, ok := shell.LookProgram("bin:"+filepath.Join(dir, "bin"), "xclip"); !ok || got != want {
		t.Errorf("LookProgram with an absolute entry = %q, %v; want %q", got, ok, want)
	}
}

// TestReviewDetailsScroll checks the scrolling of the details of a record that does not fit in the
// box. The end of TestDetails does that too, but it skips itself with "the details fit" for the
// fixture it uses, so the keys and the wheel of the details never ran in the suite.
//
// Parameters:
//   - t: test to report to
func TestReviewDetailsScroll(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.Width, o.Height = 60, 12
	})

	writeFile(t, s.Opts.Paths.ConnectionsFile(), "schema = 1\n\n[[connection]]\nname = \"big\"\n"+
		"target = \"big.example.com\"\nuser = \"deploy\"\naliases = [\"b\", \"bb\"]\n"+
		"desc = \""+strings.Repeat("word ", 80)+"\"\n")

	g := newRig(t, s)
	g.press("v")

	o, ok := g.top().(*infoOverlay)
	if !ok {
		t.Fatalf("v opened %T", g.top())
	}

	g.r.render()

	if o.total <= o.rows {
		t.Fatalf("the details fit in the box: %d lines of %d rows, the test needs more", o.total,
			o.rows)
	}

	last := maxOffset(o.rows, o.total)

	if !strings.Contains(g.text(), "more") {
		t.Errorf("the details do not say that more lines follow:\n%s", g.text())
	}

	g.press("down")

	if o.off != 1 {
		t.Errorf("off = %d after down, want 1", o.off)
	}

	g.press("up", "up")

	if o.off != 0 {
		t.Errorf("off = %d after up, want 0", o.off)
	}

	g.press("end")

	if o.off != last {
		t.Errorf("off = %d after end, want %d", o.off, last)
	}

	g.press("home")
	g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})

	if want := min(wheelLines, last); o.off != want {
		t.Errorf("off = %d after one notch, want %d", o.off, want)
	}
}

// TestReviewBrokenConfigIsSaidAtStart checks that an interface which opens on a screen other than
// the settings tells at once that config.toml is ignored. The warning of the command line is gone
// with the terminal screen that it was printed on, and no test covered this message.
//
// Parameters:
//   - t: test to report to
func TestReviewBrokenConfigIsSaidAtStart(t *testing.T) {
	for _, start := range []Start{StartHome, StartConnections} {
		s := newSandbox(t).with(func(o *Options) { o.Start = start })
		writeFile(t, s.Opts.Paths.ConfigFile(), "schema = 1\nlang = \"fr\n")

		g := newRig(t, s)
		g.r.Init()

		if got := g.toastText(); !strings.Contains(got, "config.toml ignored") {
			t.Errorf("start %d: toast %q, want it to say that config.toml is ignored", start, got)
		}
	}

	s := newSandbox(t).with(func(o *Options) { o.Start = StartHome })
	g := newRig(t, s)
	g.r.Init()

	if got := g.toastText(); got != "" {
		t.Errorf("a missing config.toml gave the toast %q", got)
	}
}

// TestReviewTimersHaveTheirDurations records the delays that the interface asks its timers for.
// TestMain replaces the timers by a function that starts nothing, so no other test sees these
// numbers: a message that stays a tenth of a second, or a wait for the background color that
// lasts a minute, passed the whole suite.
//
// Parameters:
//   - t: test to report to
func TestReviewTimersHaveTheirDurations(t *testing.T) {
	var got []time.Duration

	saved := tick
	tick = func(d time.Duration, _ func(time.Time) tea.Msg) tea.Cmd {
		got = append(got, d)

		return nil
	}

	t.Cleanup(func() { tick = saved })

	tests := []struct {
		name string
		do   func(g *rig)
		want time.Duration
	}{
		{"a message", func(g *rig) { g.r.say("done", false) }, 4 * time.Second},
		{"a message to act on", func(g *rig) {
			g.r.showToast(toastMsg{Text: "act", Hold: true})
		}, 12 * time.Second},
		{"a message with its own delay", func(g *rig) {
			g.r.showToast(toastMsg{Text: "wait", Wait: 7 * time.Second})
		}, 7 * time.Second},
		{"the first Ctrl-C", func(g *rig) { g.press("ctrl+c") }, 2 * time.Second},
		{"the background color", func(g *rig) { g.r.Init() }, 300 * time.Millisecond},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartHome }))
		got = nil

		tt.do(g)

		if len(got) == 0 || got[0] != tt.want {
			t.Errorf("%s: the timers were started with %v, want %v first", tt.name, got, tt.want)
		}
	}
}

// TestConfigWarningsAreSaidAtStart checks that a config.toml with a key that is ignored, an
// unknown key or a value that is not allowed, is announced when the interface opens, since the
// line printed on the command line is gone with the terminal screen. A clean file says nothing.
//
// Parameters:
//   - t: test to report to
func TestConfigWarningsAreSaidAtStart(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{"unknown key", "schema = 1\n[shell]\nbind = true\n", "settings that are ignored"},
		{"value not allowed", "schema = 1\n[ui]\ntheme = \"neon\"\n", "settings that are ignored"},
		{"clean file", "schema = 1\n[ui]\ntheme = \"dark\"\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSandbox(t).with(func(o *Options) { o.Start = StartHome })
			writeFile(t, s.Opts.Paths.ConfigFile(), tt.file)

			g := newRig(t, s)
			g.r.Init()

			got := g.toastText()
			if (tt.want == "") != (got == "") || !strings.Contains(got, tt.want) {
				t.Errorf("toast %q, want one with %q", got, tt.want)
			}
		})
	}
}
