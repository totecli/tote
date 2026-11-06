package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
)

// keyRig builds a rig on a screen of a 100 by 30 terminal with a clock the test moves.
//
// Parameters:
//   - t: running test
//   - start: first screen
//
// Returns the rig and the clock.
func keyRig(t *testing.T, start Start) (*rig, *clock) {
	t.Helper()

	return mouseRig(t, 100, 30, start, nil)
}

// playKeys presses keys written as in rig.press, plus "type:text" for typed text, "tick" to
// move the clock past the window of the second Ctrl-C, "busy" to start a program in the guard
// and "writing" to start a write that has not ended.
//
// Parameters:
//   - g: rig to drive
//   - clk: clock of the rig
//   - keys: steps in order
func playKeys(g *rig, clk *clock, keys ...string) {
	g.t.Helper()

	for _, k := range keys {
		switch text, typed := strings.CutPrefix(k, "type:"); {
		case typed:
			g.typeText(text)
		case k == "tick":
			clk.advance(quitWindow + time.Second)
		case k == "busy":
			g.r.guard.busy.Store(1)
		case k == "writing":
			g.r.e.writing(1)
		default:
			g.press(k)
		}
	}
}

// screenName names the kind of the screen of the root, for the tables.
//
// Parameters:
//   - r: root to look at
func screenName(r *root) string {
	switch r.scr.(type) {
	case *homeScreen:
		return "home"
	case *listScreen:
		return "list"
	case *configScreen:
		return "config"
	}

	return "other"
}

// TestQuitStateMachine plays key sequences on the welcome page, the lists, the settings screen
// and the overlays, and checks whether the interface quit, what is on screen afterwards and the
// depth of the stack of overlays.
//
// Parameters:
//   - t: test to report to
func TestQuitStateMachine(t *testing.T) {
	tests := []struct {
		name     string
		start    Start
		keys     []string
		quit     bool
		screen   string
		overlays int
		toast    string
	}{
		{"home ctrl+c", StartHome, []string{"ctrl+c"}, false, "home", 0, "Press ctrl+c again"},
		{"home ctrl+c twice", StartHome, []string{"ctrl+c", "ctrl+c"}, true, "home", 0, ""},
		{
			"home ctrl+c late", StartHome,
			[]string{"ctrl+c", "tick", "ctrl+c"},
			false, "home", 0,
			"Press ctrl+c again",
		},
		{
			"home ctrl+c then a key", StartHome,
			[]string{"ctrl+c", "down", "ctrl+c"},
			false,
			"home", 0, "Press ctrl+c again",
		},
		{"home key clears the hint", StartHome, []string{"ctrl+c", "down"}, false, "home", 0, ""},
		{"home ctrl+d", StartHome, []string{"ctrl+d"}, true, "home", 0, ""},
		{"home q", StartHome, []string{"q"}, true, "home", 0, ""},
		{"home esc", StartHome, []string{"esc"}, false, "home", 0, ""},
		{"list ctrl+c goes home", StartConnections, []string{"ctrl+c"}, false, "home", 0, ""},
		{
			"list ctrl+c twice", StartConnections,
			[]string{"ctrl+c", "ctrl+c"},
			false, "home", 0,
			"Press ctrl+c again",
		},
		{
			"list ctrl+c three times", StartConnections,
			[]string{"ctrl+c", "ctrl+c", "ctrl+c"},
			true, "home", 0, "",
		},
		{"list q", StartConnections, []string{"q"}, true, "list", 0, ""},
		{"list ctrl+d", StartConnections, []string{"ctrl+d"}, true, "list", 0, ""},
		{"repos q", StartRepos, []string{"q"}, true, "list", 0, ""},
		{"aliases q", StartAliases, []string{"q"}, true, "list", 0, ""},
		{"funcs q", StartFuncs, []string{"q"}, true, "list", 0, ""},
		{"filter q is text", StartConnections, []string{"/", "q"}, false, "list", 0, ""},
		{
			"filter ctrl+c", StartConnections,
			[]string{"/", "type:ab", "ctrl+c"},
			false, "list", 0,
			"",
		},
		{"filter ctrl+d", StartConnections, []string{"/", "ctrl+d"}, true, "list", 0, ""},
		{"settings q", StartConfig, []string{"q"}, true, "config", 0, ""},
		{"settings ctrl+c goes home", StartConfig, []string{"ctrl+c"}, false, "home", 0, ""},
		{"settings ctrl+d", StartConfig, []string{"ctrl+d"}, true, "config", 0, ""},
		{"help q", StartHome, []string{"?", "q"}, false, "home", 1, ""},
		{"help ctrl+c", StartHome, []string{"?", "ctrl+c"}, false, "home", 0, ""},
		{"help esc", StartHome, []string{"?", "esc"}, false, "home", 0, ""},
		{"help ctrl+d", StartHome, []string{"?", "ctrl+d"}, true, "home", 1, ""},
		{"palette q is text", StartConnections, []string{"ctrl+k", "q"}, false, "list", 1, ""},
		{
			"palette ctrl+c", StartConnections,
			[]string{"ctrl+k", "type:ab", "ctrl+c"},
			false,
			"list", 0, "",
		},
		{"confirm q", StartConnections, []string{"d", "q"}, false, "list", 1, ""},
		{"confirm ctrl+c", StartConnections, []string{"d", "ctrl+c"}, false, "list", 0, ""},
		{"info q", StartConnections, []string{"v", "q"}, false, "list", 1, ""},
		{"pick q", StartConnections, []string{"U", "q"}, false, "list", 1, ""},
		{"pick ctrl+c", StartConnections, []string{"U", "ctrl+c"}, false, "list", 0, ""},
		{"settings pick q", StartConfig, []string{"enter", "q"}, false, "config", 1, ""},
		{
			"settings pick ctrl+c", StartConfig,
			[]string{"enter", "ctrl+c"},
			false, "config", 0,
			"",
		},
		{"form clean esc", StartConnections, []string{"n", "esc"}, false, "list", 0, ""},
		{"form clean ctrl+c", StartConnections, []string{"n", "ctrl+c"}, false, "list", 0, ""},
		{"form clean ctrl+d", StartConnections, []string{"n", "ctrl+d"}, true, "list", 1, ""},
		{"form q is text", StartConnections, []string{"n", "q"}, false, "list", 1, ""},
		{"form dirty esc", StartConnections, []string{"n", "type:x", "esc"}, false, "list", 2, ""},
		{
			"form dirty ctrl+c", StartConnections,
			[]string{"n", "type:x", "ctrl+c"},
			false, "list",
			2, "",
		},
		{
			"form dirty esc enter", StartConnections,
			[]string{"n", "type:x", "esc", "enter"},
			false,
			"list", 1, "",
		},
		{
			"form dirty esc esc", StartConnections,
			[]string{"n", "type:x", "esc", "esc"},
			false,
			"list", 1, "",
		},
		{
			"form dirty esc ctrl+c", StartConnections,
			[]string{"n", "type:x", "esc", "ctrl+c"},
			false, "list", 1, "",
		},
		{
			"form dirty esc n", StartConnections,
			[]string{"n", "type:x", "esc", "n"},
			false, "list",
			1, "",
		},
		{
			"form dirty esc y", StartConnections,
			[]string{"n", "type:x", "esc", "y"},
			false, "list",
			0, "",
		},
		{
			"form dirty esc Y", StartConnections,
			[]string{"n", "type:x", "esc", "Y"},
			false, "list",
			0, "",
		},
		{
			"form dirty esc right enter", StartConnections,
			[]string{"n", "type:x", "esc", "right", "enter"},
			false, "list", 0, "",
		},
		{
			"form dirty ctrl+c y", StartConnections,
			[]string{"n", "type:x", "ctrl+c", "y"},
			false,
			"list", 0, "",
		},
		{
			"form typed then erased", StartConnections,
			[]string{"n", "type:x", "backspace", "esc"},
			false, "list", 0, "",
		},
		{"form edit untouched", StartConnections, []string{"e", "esc"}, false, "list", 0, ""},
		{
			"form edit changed", StartConnections,
			[]string{"e", "type:x", "esc"},
			false, "list", 2,
			"",
		},
		{
			"form dirty ctrl+d", StartConnections,
			[]string{"n", "type:x", "ctrl+d"},
			false, "list",
			2, "",
		},
		{
			"form dirty ctrl+d y", StartConnections,
			[]string{"n", "type:x", "ctrl+d", "y"},
			true,
			"list", 1, "",
		},
		{
			"form dirty ctrl+d enter", StartConnections,
			[]string{"n", "type:x", "ctrl+d", "enter"},
			false, "list", 1, "",
		},
		{
			"form dirty ctrl+d esc", StartConnections,
			[]string{"n", "type:x", "ctrl+d", "esc"},
			false, "list", 1, "",
		},
		{
			"form dirty ctrl+d twice", StartConnections,
			[]string{"n", "type:x", "ctrl+d", "ctrl+d"},
			false, "list", 2, "",
		},
		{
			"form dirty ctrl+d ctrl+c", StartConnections,
			[]string{"n", "type:x", "ctrl+d", "ctrl+c"},
			false, "list", 1, "",
		},
		{
			"settings prompt clean esc", StartConfig,
			[]string{"down", "enter", "esc"},
			false,
			"config", 0, "",
		},
		{
			"settings prompt dirty esc", StartConfig,
			[]string{"down", "enter", "type:x", "esc"},
			false, "config", 2, "",
		},
		{
			"settings prompt dirty ctrl+c y", StartConfig,
			[]string{"down", "enter", "type:x", "ctrl+c", "y"},
			false, "config", 0, "",
		},
		{"running q", StartConnections, []string{"busy", "q"}, false, "list", 0, ""},
		{"running ctrl+d", StartConnections, []string{"busy", "ctrl+d"}, false, "list", 0, ""},
		{"running esc", StartConnections, []string{"busy", "esc"}, false, "list", 0, ""},
		{"running ctrl+c", StartConnections, []string{"busy", "ctrl+c"}, true, "list", 0, ""},
		{"write q", StartHome, []string{"writing", "q"}, false, "home", 1, ""},
		{"write ctrl+d", StartHome, []string{"writing", "ctrl+d"}, false, "home", 1, ""},
		{"write ctrl+d n", StartHome, []string{"writing", "ctrl+d", "n"}, false, "home", 0, ""},
		{"write ctrl+d y", StartHome, []string{"writing", "ctrl+d", "y"}, true, "home", 0, ""},
		{
			"write ctrl+d enter", StartHome,
			[]string{"writing", "ctrl+d", "enter"},
			false, "home",
			0, "",
		},
		{
			"write ctrl+c twice", StartHome,
			[]string{"writing", "ctrl+c", "ctrl+c"},
			false, "home",
			1, "",
		},
		{
			"write palette quit", StartConnections,
			[]string{"writing", "ctrl+k", "type:quit", "enter"},
			false, "list", 1, "",
		},
		{
			"palette quit", StartConnections,
			[]string{"ctrl+k", "type:quit", "enter"},
			true, "list",
			0, "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, clk := keyRig(t, tt.start)
			playKeys(g, clk, tt.keys...)

			if g.Quit != tt.quit {
				t.Errorf("quit = %v, want %v", g.Quit, tt.quit)
			}

			if got := screenName(g.r); got != tt.screen {
				t.Errorf("screen = %s, want %s", got, tt.screen)
			}

			if got := len(g.r.overlays); got != tt.overlays {
				t.Errorf("overlays = %d, want %d", got, tt.overlays)
			}

			if got := g.toastText(); !strings.Contains(got, tt.toast) ||
				(tt.toast == "" && got != "") {
				t.Errorf("toast = %q, want %q", g.toastText(), tt.toast)
			}
		})
	}
}

// TestQuitTiny checks that a terminal too small to show a question keeps the three ways to quit
// direct, and drops every other key.
//
// Parameters:
//   - t: test to report to
func TestQuitTiny(t *testing.T) {
	for _, k := range []string{"q", "ctrl+d", "ctrl+c"} {
		g, _ := mouseRig(t, minWidth-1, minHeight+1, StartHome, nil)
		g.press(k)

		if !g.Quit {
			t.Errorf("%s did not quit a tiny terminal", k)
		}
	}

	g, _ := mouseRig(t, minWidth-1, minHeight+1, StartHome, nil)
	g.press("x", "esc", "down")

	if g.Quit {
		t.Error("another key quit a tiny terminal")
	}
}

// TestQuitWindow checks that the second Ctrl-C counts within two seconds and not after.
//
// Parameters:
//   - t: test to report to
func TestQuitWindow(t *testing.T) {
	g, clk := keyRig(t, StartHome)

	g.press("ctrl+c")
	clk.advance(quitWindow)
	g.press("ctrl+c")

	if !g.Quit {
		t.Error("a second Ctrl-C right at the end of the window did not quit")
	}

	g, clk = keyRig(t, StartHome)

	g.press("ctrl+c")
	clk.advance(quitWindow + time.Millisecond)
	g.press("ctrl+c")

	if g.Quit {
		t.Error("a second Ctrl-C after the window quit")
	}
}

// TestQuitAskDefaultsToNo checks that the question about quitting and the one about discarding
// start on "No", name what is lost, and take their answer from the buttons.
//
// Parameters:
//   - t: test to report to
func TestQuitAskDefaultsToNo(t *testing.T) {
	g, clk := keyRig(t, StartConnections)
	playKeys(g, clk, "n", "type:x", "ctrl+d")

	a, ok := g.top().(*ask)
	if !ok || a.focus {
		t.Fatalf("the question is %T focus %v, want an ask on No", g.top(), ok && a.focus)
	}

	for _, want := range []string{"Quit?", "not saved", "[ No ]", "[ Yes ]"} {
		if !strings.Contains(g.text(), want) {
			t.Errorf("%q is not in the question:\n%s", want, g.text())
		}
	}

	clickOn(g, "[ No ]")

	if len(g.r.overlays) != 1 || g.Quit {
		t.Fatalf("No: overlays %d quit %v", len(g.r.overlays), g.Quit)
	}

	g.press("ctrl+d")
	clickOn(g, "[ Yes ]")

	if !g.Quit {
		t.Error("Yes did not quit")
	}
}

// TestDiscardKeepsTheText checks that answering no leaves the form as it was, text and focus.
//
// Parameters:
//   - t: test to report to
func TestDiscardKeepsTheText(t *testing.T) {
	g, clk := keyRig(t, StartConnections)
	playKeys(g, clk, "n", "type:web", "tab", "type:web.example.com", "esc", "enter")

	f := g.frm()

	if f.fields[0].text() != "web" || f.fields[1].text() != "web.example.com" || f.cur != 1 {
		t.Errorf("the form changed: %q %q focus %d", f.fields[0].text(), f.fields[1].text(), f.cur)
	}

	playKeys(g, clk, "esc", "y")

	if len(g.r.overlays) != 0 {
		t.Error("yes did not discard")
	}

	if _, found := g.r.e.Data.record(kindConn, "web"); found {
		t.Error("the discarded form wrote a record")
	}
}

// TestMouseNeverDiscards checks that a click outside a form with changes keeps it, and that the
// hint "esc" of the footer asks like the key does.
//
// Parameters:
//   - t: test to report to
func TestMouseNeverDiscards(t *testing.T) {
	g, clk := keyRig(t, StartConnections)
	playKeys(g, clk, "n", "type:web")

	for _, p := range [][2]int{{0, 0}, {99, 29}, {50, 0}, {0, 15}} {
		g.click(p[0], p[1])
		g.click(p[0], p[1])
	}

	if len(g.r.overlays) != 1 || g.frm().fields[0].text() != "web" {
		t.Fatalf("a click outside lost the form: %d overlays", len(g.r.overlays))
	}

	clickFooter(g, "esc")

	if _, ok := g.top().(*ask); !ok {
		t.Fatalf("the esc hint did not ask: %T", g.top())
	}

	clickOn(g, "[ No ]")

	if len(g.r.overlays) != 1 || g.frm().fields[0].text() != "web" {
		t.Error("No lost the form")
	}
}

// TestResetAsks checks that the letter d on the settings screen asks before a setting goes back
// to its default, and that only yes writes the file.
//
// Parameters:
//   - t: test to report to
func TestResetAsks(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) { o.Start = StartConfig })
	writeFile(t, s.Opts.Paths.ConfigFile(), fixtureConfig)

	before := readFile(t, s.Opts.Paths.ConfigFile())
	g := newRig(t, s)

	g.press("down", "down", "d")

	a, ok := g.top().(*ask)
	if !ok || a.focus {
		t.Fatalf("d did not ask on No: %T", g.top())
	}

	if !strings.Contains(g.text(), "Reset to default?") {
		t.Errorf("the question is not shown:\n%s", g.text())
	}

	g.press("enter")

	if len(g.r.overlays) != 0 || readFile(t, s.Opts.Paths.ConfigFile()) != before {
		t.Fatal("Enter on No changed something")
	}

	g.press("d", "y")
	g.await("the reset", func() bool { return readFile(t, s.Opts.Paths.ConfigFile()) != before })
}

// TestWritesAreCounted checks that a write of a setting counts as pending from the moment its
// command is made until it ends.
//
// Parameters:
//   - t: test to report to
func TestWritesAreCounted(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) { o.Start = StartConfig })
	g := newRig(t, s)
	e := g.r.e

	if e.pendingWrites() {
		t.Fatal("a write is pending at the start")
	}

	cmd := writeSettingCmd(e, settingDefs[0], "fr", false)

	if !e.pendingWrites() {
		t.Error("a write that was not run yet is not pending")
	}

	g.press("ctrl+d")

	if g.Quit || len(g.r.overlays) != 1 {
		t.Errorf("quit during a write: quit %v overlays %d", g.Quit, len(g.r.overlays))
	}

	_ = cmd()

	if e.pendingWrites() {
		t.Error("a write that ended is still pending")
	}
}

// TestPaletteQuitFollowsTheRule checks that the palette action "Quit tote" asks like Ctrl-D.
//
// Parameters:
//   - t: test to report to
func TestPaletteQuitFollowsTheRule(t *testing.T) {
	g, clk := keyRig(t, StartConnections)
	playKeys(g, clk, "writing", "ctrl+k", "type:quit", "enter")

	if g.Quit {
		t.Fatal("the palette quit with a write in progress")
	}

	if _, ok := g.top().(*ask); !ok {
		t.Fatalf("no question: %T", g.top())
	}
}

// TestNoRBinding checks that the letter R is bound nowhere: not in the key map, not in the
// bindings that any screen or overlay lists, not in the source of the package as a key of a
// binding or a case of a switch on a key, and that pressing it does nothing on a screen that
// does not read text. The scope prefix "r " of the palette is typed text.
//
// Parameters:
//   - t: test to report to
func TestNoRBinding(t *testing.T) {
	isR := func(k string) bool { return k == "r" || k == "R" }

	e := newRig(t, newSandbox(t)).r.e
	v := reflect.ValueOf(e.Keys)

	for i := range v.NumField() {
		b, ok := v.Field(i).Interface().(key.Binding)
		if ok && slices.ContainsFunc(b.Keys(), isR) {
			t.Errorf("the key map binds R in %s: %v", v.Type().Field(i).Name, b.Keys())
		}
	}

	for _, lang := range []string{"en", "fr"} {
		for _, s := range surfaces(t, lang, false) {
			for _, b := range s.keys {
				if slices.ContainsFunc(b.Keys(), isR) {
					t.Errorf("%s %s binds R: %v", lang, s.name, b.Keys())
				}
			}
		}
	}

	for _, s := range surfaces(t, "en", false) {
		g := s.build(100, 30)

		if readsText(g) {
			continue
		}

		before, depth, name := g.text(), len(g.r.overlays), screenName(g.r)

		g.press("r", "R")

		if g.Quit || g.text() != before || len(g.r.overlays) != depth || screenName(g.r) != name {
			t.Errorf("%s: r or R did something:\n%s", s.name, g.text())
		}

		if n := len(g.r.runner.(*fakeRunner).calls()); n != 0 {
			t.Errorf("%s: r or R started %d programs", s.name, n)
		}
	}

	scanKeyLiterals(t, isR)
}

// readsText reports whether the screen or the overlay on top reads text, where letters are
// characters.
//
// Parameters:
//   - g: rig to look at
func readsText(g *rig) bool {
	if top := g.top(); top != nil {
		t, ok := top.(typist)

		return ok && t.Typing()
	}

	return g.r.scr.Typing()
}

// scanKeyLiterals reads the source of the package and reports the string literals that name a
// key in a call that builds a binding, or in a case of a switch on the text of a key press.
//
// Parameters:
//   - t: running test
//   - bad: tells whether a key name is forbidden
func scanKeyLiterals(t *testing.T, bad func(string) bool) {
	t.Helper()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}

		tree, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		report := func(n ast.Node, lit *ast.BasicLit) {
			if lit.Kind == token.STRING && bad(strings.Trim(lit.Value, "\"")) {
				t.Errorf("%s: %s is a key name", fset.Position(n.Pos()), lit.Value)
			}
		}

		ast.Inspect(tree, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				name := ""

				switch fn := x.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					name = fn.Sel.Name
				}

				if name == "WithKeys" || name == "bind" || name == "plain" {
					for _, a := range x.Args {
						if lit, ok := a.(*ast.BasicLit); ok {
							report(x, lit)
						}
					}
				}
			case *ast.SwitchStmt:
				call, ok := x.Tag.(*ast.CallExpr)
				if !ok {
					return true
				}

				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "String" {
					return true
				}

				for _, c := range x.Body.List {
					for _, e := range c.(*ast.CaseClause).List {
						if lit, ok := e.(*ast.BasicLit); ok {
							report(c, lit)
						}
					}
				}
			}

			return true
		})
	}
}

// snapshotDir reads every file below a directory.
//
// Parameters:
//   - t: running test
//   - dir: directory to read, which may not exist
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()

	out := map[string]string{}

	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		b, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatal(rerr)
		}

		out[p] = string(b)

		return nil
	})

	return out
}

// snapshotAll reads every file of the configuration, the data and the state of a rig, so that a
// letter that installs, removes, enables or disables a plugin shows. The staging area is left out:
// an install is staged, and nothing is installed until the consent is given. So is the tree of the
// fixture "torn", whose record names a tree that is not the one on disk: the housekeeping that
// starts every change deletes a tree that is not the current one of its plugin.
//
// Parameters:
//   - t: running test
//   - g: rig to read
func snapshotAll(t *testing.T, g *rig) map[string]string {
	t.Helper()

	out := map[string]string{}

	sep := string(filepath.Separator)
	skipped := []string{sep + ".staging" + sep, sep + "plugins" + sep + "torn" + sep}

	for _, dir := range []string{g.r.e.Paths.Config, g.r.e.Paths.Data, g.r.e.Paths.State} {
		for p, s := range snapshotDir(t, dir) {
			if !slices.ContainsFunc(skipped, func(part string) bool {
				return strings.Contains(p, part)
			}) {
				out[p] = s
			}
		}
	}

	return out
}

// changedFiles lists the files that differ between two snapshots.
//
// Parameters:
//   - before: first snapshot
//   - after: second snapshot
func changedFiles(before, after map[string]string) []string {
	var out []string

	for p, s := range after {
		if old, ok := before[p]; !ok || old != s {
			out = append(out, p)
		}
	}

	for p := range before {
		if _, ok := after[p]; !ok {
			out = append(out, p)
		}
	}

	slices.Sort(out)

	return out
}

// TestNoBareLetterLoses presses every letter, lower and upper case, on every screen and overlay
// that does not read text, and requires that no file of the configuration changes and that at
// most one overlay opens, which is a menu or a confirmation. The answers of a confirmation are
// the confirmation itself and are left out. The letters h and l are the left and right keys: on a
// row of the settings screen that is a switch or a choice they step the value, so config.toml (and
// the lock file next to it) may change for them there and for no other file or row;
// TestAdjustWritesOneKey checks what such a change may touch.
//
// Parameters:
//   - t: test to report to
func TestNoBareLetterLoses(t *testing.T) {
	letters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

	all := surfaces(t, "en", false)

	// The settings screen is also tried with a config.toml that sets values, on every row, since
	// a letter that resets a value has something to lose only there.
	for i := range settingDefs {
		all = append(all, surface{
			name: fmt.Sprintf("settings row %d with a file", i),
			build: func(w, h int) *rig {
				s := newSandbox(t).with(func(o *Options) {
					o.Start, o.Width, o.Height = StartConfig, w, h
				})
				writeFile(t, s.Opts.Paths.ConfigFile(), fixtureConfig)

				g := newRig(t, s)
				g.press(slices.Repeat([]string{"down"}, i)...)

				return g
			},
		})
	}

	for _, s := range all {
		probe := s.build(100, 30)
		if readsText(probe) {
			continue
		}

		switch probe.top().(type) {
		case *confirm, *ask:
			continue
		}

		for _, l := range letters {
			g := s.build(100, 30)
			before := snapshotAll(t, g)
			depth := len(g.r.overlays)

			// Only the file of settings may change, and only by a step of a switch or a choice.
			steps := strings.ContainsRune("hl", l) && steppable(g)
			cfg := g.r.e.Paths.ConfigFile()

			g.press(string(l))
			g.settle()

			after := snapshotAll(t, g)
			if steps {
				before, after = without(before, cfg, cfg+".lock"), without(after, cfg, cfg+".lock")
			}

			if !reflect.DeepEqual(after, before) {
				t.Errorf("%s: the letter %q changed %v", s.name, string(l),
					changedFiles(before, after))
			}

			if len(g.r.overlays) > depth+1 {
				t.Errorf("%s: the letter %q opened %d overlays", s.name, string(l),
					len(g.r.overlays)-depth)
			}
		}
	}
}

// steppable reports whether the screen of a rig is the settings screen on a row that the left and
// right keys change, a switch or a choice.
//
// Parameters:
//   - g: rig to read
func steppable(g *rig) bool {
	cs, ok := g.r.scr.(*configScreen)
	if !ok || g.top() != nil {
		return false
	}

	return cs.def().kind == settingBool || cs.def().kind == settingChoice
}

// without returns a copy of a snapshot of files without some of them.
//
// Parameters:
//   - snap: snapshot from snapshotDir
//   - paths: files to leave out
func without(snap map[string]string, paths ...string) map[string]string {
	out := map[string]string{}

	for p, c := range snap {
		if !slices.Contains(paths, p) {
			out[p] = c
		}
	}

	return out
}

// settle waits until no write of a setting is in progress, which the commands of the rig run
// within a second.
func (g *rig) settle() {
	g.t.Helper()

	g.await("the writes to end", func() bool { return !g.r.e.pendingWrites() })
}

// TestHelpShowsTheSafetyRules checks that the help tells, in both languages and in the ASCII
// glyph set, that Ctrl-C goes back, that Ctrl-D quits and that nothing is lost on quit, whole and
// not cut by the length of the note.
//
// Parameters:
//   - t: test to report to
func TestHelpShowsTheSafetyRules(t *testing.T) {
	tests := []struct {
		lang  string
		ascii bool
		want  []string
	}{
		{"en", false, []string{
			"[ctrl+c] goes back", "[ctrl+d] quits", "every write is immediate",
			"asked about first.",
		}},
		{"fr", false, []string{
			"[ctrl+c] revient en arri\u00e8re", "[ctrl+d] quitte",
			"\u00e9criture est imm\u00e9diate",
			"formulaire non enregistr\u00e9 est demand\u00e9 avant.",
		}},
		{"fr", true, []string{
			"[ctrl+c] revient en arriere", "[ctrl+d] quitte",
			"ecriture est immediate", "formulaire non enregistre est demande avant.",
		}},
	}

	flat := func(g *rig) string {
		return strings.Join(strings.Fields(strings.NewReplacer("\u2502", " ", "|", " ").
			Replace(g.text())), " ")
	}

	for _, tt := range tests {
		s := newSandbox(t).with(func(o *Options) {
			o.Printer, o.ASCII, o.Width, o.Height = newPrinter(tt.lang), tt.ascii, 100, 40
		})
		g := newRig(t, s)

		g.press("?")
		_ = g.text()
		g.press("end")

		text := flat(g)

		for _, w := range tt.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s ascii %v: %q is not in the help:\n%s", tt.lang, tt.ascii, w, g.text())
			}
		}

		g.press("home")

		text = flat(g)

		for _, w := range []string{"ctrl+c", "ctrl+d", "ctrl+z"} {
			if !strings.Contains(text, w) {
				t.Errorf("%s ascii %v: %q is not in the help:\n%s", tt.lang, tt.ascii, w, g.text())
			}
		}
	}
}
