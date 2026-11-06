package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// gftRigWith is gftRig with a change of the sandbox before the rig is built.
//
// Parameters:
//   - t: running test
//   - mutate: change to make to the sandbox
func gftRigWith(t *testing.T, mutate func(*sandbox)) (*rig, *plugLab, string) {
	t.Helper()

	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	log := l.runTools()
	l.installGft()

	sb.Opts.Start = StartRepos
	mutate(sb)

	return newRig(t, sb), l, log
}

// burst delivers key presses to the model one after the other without playing the commands they
// return, which is what keys that arrive together do, then plays the commands.
//
// Parameters:
//   - keys: key names in order
func (g *rig) burst(keys ...string) {
	g.t.Helper()

	var cmds []tea.Cmd

	for _, k := range keys {
		_, cmd := g.r.Update(keyMsg(k))
		cmds = append(cmds, cmd)
	}

	for _, c := range cmds {
		for _, m := range collectLate(c, g.late) {
			g.send(m)
		}
	}
}

// openDrop opens the form of "Drop a stash" from the menu of the first repository.
func (g *rig) openDrop() {
	g.t.Helper()
	g.repoActions()
	g.press("down", "down", "down", "down", "down", "enter")

	if _, ok := g.top().(*fieldsForm); !ok {
		g.t.Fatalf("Enter opened %T:\n%s", g.top(), g.text())
	}
}

// TestPlugKeysDuringHandoff checks that keys which arrive in the same burst as the choice of an
// action do not reach the list under the menu: Enter there would open the repository and end tote
// with a change of directory, killing the run that was being started.
func TestPlugKeysDuringHandoff(t *testing.T) {
	g, _, log := gftRig(t)
	g.repoActions()
	g.burst("down", "down", "down", "down", "down", "enter", "3", "enter")

	if _, ok := g.top().(*fieldsForm); !ok {
		t.Fatalf("the burst led to %T:\n%s", g.top(), g.text())
	}

	if g.Quit || !g.r.result.Empty() || strings.Contains(g.toastText(), "wrapper") {
		t.Errorf("the burst left the list (quit %v, result %+v, toast %q)", g.Quit, g.r.result,
			g.toastText())
	}

	if g.r.e.Handing {
		t.Error("the hand-off is still raised once the form is shown")
	}

	g.press("esc")
	g.openDrop()
	g.typeText("3")
	g.press("enter")

	if _, ok := g.top().(*runAsk); !ok {
		t.Fatalf("the form led to %T", g.top())
	}

	g.burst("l", "enter", "enter")
	g.settled()

	if got := readLog(t, log); len(got) != 1 || got[0] != "stash drop 3 --yes" {
		t.Errorf("log %q", got)
	}

	if g.Quit || !g.r.result.Empty() || g.r.e.Handing {
		t.Errorf("the second Enter went on (quit %v, result %+v, handing %v)", g.Quit,
			g.r.result, g.r.e.Handing)
	}
}

// TestPlugHandoffEnds checks that every way a flow can end lowers the hand-off, so that no key is
// ever swallowed for good, and that the quit keys work while it is raised.
func TestPlugHandoffEnds(t *testing.T) {
	g, _, _ := gftRig(t)

	g.press("tab", "esc")

	if g.r.e.Handing {
		t.Error("closing the menu raised the hand-off")
	}

	g.repoActions()
	g.press("enter")

	if g.r.e.Handing {
		t.Error("a refused action raised the hand-off")
	}

	g.press("esc")
	g.startShow("-bad/x", "")
	g.press("enter")

	if g.r.e.Handing {
		t.Errorf("a refused form raised the hand-off:\n%s", g.text())
	}

	g.press("esc")

	g.r.e.Handing = true
	g.press("ctrl+d")

	if !g.Quit {
		t.Error("Ctrl-D does not quit while the hand-off is raised")
	}
}

// TestPlugAccelerator checks that the key of an action runs it from the menu, and that the
// menu shows the key, the plugin and the dots of an action that asks something.
func TestPlugAccelerator(t *testing.T) {
	g, _, log := gftRig(t)
	g.repoActions()

	for _, want := range []string{"s  List stashes", "Drop a stash\u2026", "gft"} {
		if !strings.Contains(g.text(), want) {
			t.Errorf("the menu lacks %q:\n%s", want, g.text())
		}
	}

	g.press("s")
	g.settled()

	if got := readLog(t, log); len(got) != 1 || got[0] != "stash list" {
		t.Errorf("log %q", got)
	}
}

// TestPlugAcceleratorKeepsListKeys checks that an accelerator never takes a key of the list.
func TestPlugAcceleratorKeepsListKeys(t *testing.T) {
	g, _, log := gftRig(t)
	m := g.repoActions()
	m.offers[1].Key = "j"

	g.press("j")

	if m.cur != 1 || len(readLog(t, log)) != 0 {
		t.Errorf("j moved to %d (log %q)", m.cur, readLog(t, log))
	}
}

// TestPlugReasonAndToast checks that a repository without a GitHub remote says so in plain
// words in both languages, and that the reason held on the status line goes when the cursor moves.
func TestPlugReasonAndToast(t *testing.T) {
	for lang, want := range map[string]string{
		"en": "has no GitHub remote",
		"fr": "n\u2019a pas de remote GitHub",
	} {
		g, _, _ := gftRigWith(t, func(sb *sandbox) { sb.Opts.Printer = newPrinter(lang) })
		g.repoActions()

		if strings.Contains(g.text(), "(empty)") || !strings.Contains(g.text(), want) {
			t.Errorf("%s: the reason reads:\n%s", lang, g.text())
		}

		g.press("enter")

		if !strings.Contains(g.toastText(), want) {
			t.Errorf("%s: toast %q", lang, g.toastText())
		}

		g.press("down", "down", "down")

		if g.toastText() != "" {
			t.Errorf("%s: the reason stayed on the status line: %q", lang, g.toastText())
		}
	}
}

// TestPlugMissingProgram checks what the menu says when the program of the plugin is gone, and
// the words of a run refused for the same reason.
func TestPlugMissingProgram(t *testing.T) {
	g, l, _ := gftRig(t)
	l.env["PATH"] = filepath.Join(l.sb.Dir, "nowhere")

	g.press("tab")

	if g.top() != nil {
		t.Fatalf("Tab opened %T", g.top())
	}

	if got := g.toastText(); !strings.Contains(got, "gft") ||
		!strings.Contains(got, "not installed") {
		t.Errorf("toast %q", got)
	}

	e := g.r.e
	text := e.errorText(&plugin.Error{
		Code: plugin.CodeNotRunnable,
		Args: []string{"gft", "missing-dependency", "missing_dependency"},
	})

	if text != "plugin gft cannot run (missing dependency)" {
		t.Errorf("not runnable reads %q", text)
	}
}

// TestPlugRunFormKeys checks the footer of a form that asks the arguments of a run: it names the
// key that runs, never saves and says nothing of Tab on a form of one field.
func TestPlugRunFormKeys(t *testing.T) {
	g, _, _ := gftRig(t)
	g.openDrop()

	foot := g.footerText()
	if !strings.Contains(foot, "run") || strings.Contains(foot, "save") ||
		strings.Contains(foot, "tab") {
		t.Errorf("one field: %q", foot)
	}

	g.press("esc")
	g.startShow("demo/proj", "")

	g.press("shift+tab", "shift+tab")

	if foot = g.footerText(); !strings.Contains(foot, "next") ||
		!strings.Contains(foot, "ctrl+s run") || strings.Contains(foot, "save") {
		t.Errorf("first field: %q", foot)
	}

	g.press("tab", "tab", "tab", "tab")

	if foot = g.footerText(); !strings.Contains(foot, "run") {
		t.Errorf("last field: %q", foot)
	}
}

// TestPlugChoiceInView checks that the picked value of a choice is always on the row, whatever
// its width and whichever value is picked.
func TestPlugChoiceInView(t *testing.T) {
	g, _, _ := gftRig(t)
	e := g.r.e
	names := []string{
		"Release info", "All releases", "Assets", "Asset for this system",
		"Release notes",
	}
	fd := newChoiceField("view", "View", []string{"a", "b", "c", "d", "e"}, names, nil)
	fd.Raw = true

	for pick := range names {
		fd.Pick = pick

		for w := 14; w <= 90; w++ {
			row := stripStyles(fd.view(e, w, true))

			if e.Metric.width(row) != w {
				t.Fatalf("pick %d width %d: the row is %d cells", pick, w, e.Metric.width(row))
			}

			name := names[pick]
			if e.Metric.width(name)+2 > w {
				name = name[:w-2]
			}

			if !strings.Contains(row, name) {
				t.Fatalf("pick %d width %d: %q not in %q", pick, w, name, row)
			}
		}
	}
}

// TestChoiceSpan checks which values of a choice are drawn.
func TestChoiceSpan(t *testing.T) {
	widths := []int{6, 6, 6, 6, 6}

	tests := []struct {
		name        string
		pick, w     int
		first, last int
	}{
		{"all fit", 2, 40, 0, 4},
		{"first", 0, 20, 0, 1},
		{"middle", 2, 20, 1, 2},
		{"last", 4, 20, 3, 4},
		{"too narrow", 3, 4, 3, 3},
	}

	for _, tc := range tests {
		first, last := choiceSpan(widths, tc.pick, tc.w, 1)
		if first != tc.first || last != tc.last {
			t.Errorf("%s: %d..%d want %d..%d", tc.name, first, last, tc.first, tc.last)
		}
	}

	if first, last := choiceSpan(nil, 0, 10, 1); first != 0 || last != -1 {
		t.Errorf("no value: %d..%d", first, last)
	}
}

// TestPlugRunFormLabels checks that the labels of a run form keep their hint up to half the box.
func TestPlugRunFormLabels(t *testing.T) {
	g, _, _ := gftRig(t)
	g.startShow("demo/proj", "")

	if !strings.Contains(g.text(), "Tag (empty for the latest)") {
		t.Errorf("the label is cut:\n%s", g.text())
	}
}

// TestPlugPagerTopOnSuccess checks that a run that worked leaves the pager at the top of its
// output, and one that failed at the end, where the error is.
func TestPlugPagerTopOnSuccess(t *testing.T) {
	g, _, _ := gftRig(t)
	g.startShow("many/x", "")
	g.press("enter")
	g.settled()

	v, ok := g.top().(*runView)
	if !ok || v.off != 0 || v.follow || !strings.Contains(g.text(), "gft: many/x") {
		t.Fatalf("the pager of a good run: %T off %d follow %v:\n%s", g.top(), v.off, v.follow,
			g.text())
	}

	g.press("esc")
	g.startShow("fail/x", "")
	g.press("enter")
	g.settled()

	if _, ok = g.top().(*runView); !ok || !strings.Contains(g.text(), "boom") {
		t.Fatalf("the pager of a failed run: %T:\n%s", g.top(), g.text())
	}
}

// TestPlugRunClearsToast checks that the reason of a failure held on the status line gives way
// to the status of the next run.
func TestPlugRunClearsToast(t *testing.T) {
	g, _, _ := gftRig(t)
	g.startShow("fail/x", "")
	g.press("enter")
	g.settled()

	if !strings.Contains(g.toastText(), "failed") {
		t.Fatalf("toast %q", g.toastText())
	}

	g.press("esc")
	g.startShow("hang/x", "")
	g.press("enter")

	if g.toastText() != "" || !strings.Contains(g.text(), "plugin gft: running") {
		t.Errorf("toast %q, screen:\n%s", g.toastText(), g.text())
	}

	g.press("esc", "esc")
	g.settled()
}

// TestPlugQuitAsksDuringRun checks that quitting with a program running asks first, and that
// answering no leaves the program alone.
func TestPlugQuitAsksDuringRun(t *testing.T) {
	g, _, _ := gftRig(t)
	g.startShow("hang/x", "")
	g.press("enter")

	if g.r.run == nil {
		t.Fatal("no run started")
	}

	g.press("ctrl+d")

	if _, ok := g.top().(*ask); !ok || g.Quit || !strings.Contains(g.text(), "still running") {
		t.Fatalf("Ctrl-D with a run: %T quit %v:\n%s", g.top(), g.Quit, g.text())
	}

	g.press("enter")

	if g.Quit || g.r.run == nil {
		t.Errorf("No quit anyway (quit %v)", g.Quit)
	}

	g.press("esc", "esc")
	g.settled()
}

// TestPlugMonoPager checks that the output of a program carries no color in the neutral theme
// and keeps it otherwise.
func TestPlugMonoPager(t *testing.T) {
	for _, mono := range []bool{false, true} {
		g, _, _ := gftRigWith(t, func(sb *sandbox) {
			sb.Opts.Settings.UI.Theme = "dark"

			if mono {
				sb.Opts.Environ = append(sb.Opts.Environ, "NO_COLOR=1", "TOTE_COLOR=")
			}
		})

		if g.r.e.isMono() != mono {
			t.Fatalf("mono %v: the theme is %v", mono, g.r.e.Theme.Mode)
		}

		g.startShow("color/x", "")
		g.press("enter")
		g.settled()

		v, ok := g.top().(*runView)
		if !ok {
			t.Fatalf("mono %v: %T", mono, g.top())
		}

		v.text.fit(g.r.e, 80)

		var found bool

		for i := 0; i < v.text.total(); i++ {
			found = found || strings.Contains(v.text.row(i), "\x1b")
		}

		if found == mono {
			t.Errorf("mono %v: color in the rows is %v", mono, found)
		}
	}
}

// TestPlugPagerNotices checks that a cut output shows one translated notice, whether the
// registry or the pager cut it.
func TestPlugPagerNotices(t *testing.T) {
	g, _, _ := gftRig(t)
	e := g.r.e

	run := &plugRun{
		seq: 7, start: pluginStart{Plugin: "gft", Label: "x"},
		ch: make(chan runItem, 1),
	}
	run.view = newRunView(e, run)
	g.r.run = run

	g.r.runEvents(runEventsMsg{Seq: 7, Events: []plugin.Event{
		{Type: plugin.EventOutput, Text: "one\n"},
		{Type: plugin.EventNotice, Text: "[output truncated]"},
	}})

	if !run.view.text.cut || len(run.view.text.notes) != 0 {
		t.Errorf("cut %v notes %v", run.view.text.cut, run.view.text.notes)
	}

	g.r.run = nil
	lines, _ := run.view.draw(e, 80, 10)
	text := stripStyles(strings.Join(lines, "\n"))

	if !strings.Contains(text, "output cut here") || strings.Contains(text, "truncated") {
		t.Errorf("notices:\n%s", text)
	}
}

// TestPagerLongLine checks that a line which is cut at the byte limit says so.
func TestPagerLongLine(t *testing.T) {
	p := newPagerText(false)
	p.add(strings.Repeat("y", pagerMaxLine/2))

	if p.cut {
		t.Fatal("half a line was reported as cut")
	}

	p.add(strings.Repeat("y", pagerMaxLine))

	if !p.cut || len(p.cur) != pagerMaxLine {
		t.Errorf("cut %v, kept %d bytes", p.cut, len(p.cur))
	}

	p = newPagerText(false)
	p.add(strings.Repeat("z", pagerMaxLine+5) + "\nnext\n")

	if !p.cut || len(p.lines) != 2 {
		t.Errorf("cut %v lines %d", p.cut, len(p.lines))
	}
}

// TestPagerStyleCost checks that a line made of thousands of style sequences costs no more rows
// than its text: the style is opened again once per row, not once per sequence.
func TestPagerStyleCost(t *testing.T) {
	g, _, _ := gftRig(t)
	e := g.r.e
	p := newPagerText(true)
	line := strings.Repeat("\x1b[1ma", 3200) + "\n"
	in := 0

	for range 64 {
		p.add(line)

		in += len(line)
	}

	p.fit(e, 20)

	out := 0

	for i := 0; i < p.total(); i++ {
		out += len(p.row(i))
	}

	if out > 3*in {
		t.Errorf("%d bytes of rows for %d bytes of output", out, in)
	}
}

// TestPagerWrapStyle checks that a style which is on at the end of a row is closed there and
// opened again, as one sequence, on the next row.
func TestPagerWrapStyle(t *testing.T) {
	g, _, _ := gftRig(t)
	e := g.r.e

	got := e.wrapSGR("\x1b[1;31mAAAAAAAA\x1b[0mB", 4)
	want := []string{"\x1b[1;31mAAAA\x1b[0m", "\x1b[1;31mAAAA\x1b[0m", "B"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("rows %q want %q", got, want)
	}

	got = e.wrapSGR("\x1b[1m\x1b[3m\x1b[22mxy", 1)

	if len(got) != 2 || !strings.HasSuffix(got[1], "\x1b[0m") {
		t.Errorf("rows %q", got)
	}
}

// TestSgrAt checks the parser of color sequences of the pager on its own: only the whitelist is a
// sequence.
func TestSgrAt(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"\x1b[1;31mx", 7},
		{"\x1b[0m", 4},
		{"\x1b[m", 3},
		{"\x1b[53mx", 0},
		{"\x1b[8mx", 0},
		{"\x1b[9999mx", 0},
		{"\x1b[38;5;1mx", 0},
		{"\x1b[2Jx", 0},
		{"\x1b]0;t\x07", 0},
		{"\x1b[", 0},
	}

	for _, tc := range tests {
		if n, _ := sgrAt(tc.in); n != tc.want {
			t.Errorf("sgrAt(%q) = %d want %d", tc.in, n, tc.want)
		}
	}
}

// TestPlugAskExactCommand checks that the question shows the command as it will run: the spaces
// inside an argument stay, and a box too small for the whole command refuses to answer yes.
func TestPlugAskExactCommand(t *testing.T) {
	g, _, _ := gftRig(t)
	e := g.r.e
	st := pluginStart{Plugin: "gft", Label: "Say"}

	a := newRunAsk(e, st, plugin.Prepared{Display: "gft say 'a   b' --end", Dir: "/usr"}).(*runAsk)
	box, _ := a.draw(e, 80, 24)

	if text := stripStyles(strings.Join(box, "\n")); !strings.Contains(text, "'a   b'") || a.cut {
		t.Errorf("the command was changed (cut %v):\n%s", a.cut, text)
	}

	long := "gft say " + strings.Repeat("x", 90) + " --tail-marker-END"
	a = newRunAsk(e, st, plugin.Prepared{Display: long}).(*runAsk)

	if box, _ = a.draw(e, 24, 7); !a.cut {
		t.Fatalf("a command of %d cells fitted a box of 24x7:\n%s", len(long),
			strings.Join(box, "\n"))
	}

	a.yes = true

	if _, cmd, done := a.Update(e, keyMsg("enter")); cmd != nil || done {
		t.Error("Run was accepted although the command was cut")
	}

	if _, _ = a.draw(e, 100, 30); a.cut {
		t.Error("the same command does not fit a box of 100x30")
	}

	if _, cmd, done := a.Update(e, keyMsg("enter")); cmd == nil || !done {
		t.Error("Run was refused although the whole command is shown")
	}
}

// TestWrapExact checks that cutting a command into lines loses nothing.
func TestWrapExact(t *testing.T) {
	g, _, _ := gftRig(t)
	e := g.r.e

	for _, s := range []string{
		"gft stash drop 3 --yes", "a  b   c", strings.Repeat("ab ", 20),
		strings.Repeat("x", 55), "\u65e5\u672c\u8a9e \u65e5\u672c\u8a9e", "tail ",
	} {
		for w := 1; w <= 30; w++ {
			lines := wrapExact(e, s, w)

			if strings.Join(lines, "") != s {
				t.Fatalf("%q at %d: %q", s, w, lines)
			}

			for _, l := range lines {
				if e.Metric.width(l) > w && w >= 2 {
					t.Fatalf("%q at %d: line %q is %d cells", s, w, l, e.Metric.width(l))
				}
			}
		}
	}
}

// TestPlugAskButtons checks that left and right choose a button and stay on it.
func TestPlugAskButtons(t *testing.T) {
	g, _, _ := gftRig(t)
	e := g.r.e
	a := newRunAsk(e, pluginStart{Plugin: "gft"}, plugin.Prepared{Display: "gft"}).(*runAsk)

	for _, tc := range []struct {
		key string
		yes bool
	}{
		{"left", false},
		{"h", false},
		{"right", true},
		{"l", true},
		{"right", true},
		{"h", false},
		{"tab", true},
		{"tab", false},
		{"shift+tab", true},
	} {
		a.Update(e, keyMsg(tc.key))

		if a.yes != tc.yes {
			t.Errorf("after %s: yes is %v", tc.key, a.yes)
		}
	}
}

// TestPlugLanguageChange checks what the plugins add to the interface follows a change of
// language: the commands of the palette and the range of the jump keys.
func TestPlugLanguageChange(t *testing.T) {
	g, _, _ := gftRig(t)
	e := g.r.e

	s := e.Settings
	s.Lang = "fr"
	g.r.applySettings(s)

	for _, m := range collect(e.Plug.fetch()) {
		g.send(m)
	}

	g.press("esc")

	if foot := g.footerText(); !strings.Contains(foot, "1-5") {
		t.Errorf("footer after the change: %q", foot)
	}

	g.press("ctrl+k")
	g.typeText("release")

	if text := g.text(); !strings.Contains(text, "Afficher une release GitHub") {
		t.Errorf("the palette kept the old language:\n%s", text)
	}
}

// TestPlugOutcomeTexts checks the sentence of every way a run can end, and whether it is shown as
// a failure.
func TestPlugOutcomeTexts(t *testing.T) {
	g, _, _ := gftRig(t)
	e := g.r.e

	tests := []struct {
		name string
		out  plugin.Outcome
		bad  bool
		want string
	}{
		{"done", plugin.Outcome{Status: plugin.OutcomeOK}, false, "plugin demo: done"},
		{"stopped", plugin.Outcome{Status: plugin.OutcomeCancelled}, true, "plugin demo: stopped"},
		{
			"timeout",
			plugin.Outcome{Status: plugin.OutcomeTimeout},
			true,
			"plugin demo: stopped after 5s",
		},
		{
			"failed",
			plugin.Outcome{Status: plugin.OutcomeFailed, Exit: 3},
			true,
			"plugin demo: failed, exit status 3",
		},
		{
			"named signal",
			plugin.Outcome{Status: plugin.OutcomeSignaled, Signal: "TERM"},
			true,
			"plugin demo: ended on signal SIGTERM",
		},
		{
			"numbered signal",
			plugin.Outcome{Status: plugin.OutcomeSignaled, Signal: "SIG10"},
			true,
			"plugin demo: ended on signal SIG10",
		},
		{
			"too much output",
			plugin.Outcome{
				Status: plugin.OutcomeFailed,
				Err:    &plugin.Error{Code: plugin.CodeOutputLimit, Args: []string{"demo"}},
			},
			true,
			"demo sent too much output and was stopped",
		},
		{
			"busy",
			plugin.Outcome{
				Status: plugin.OutcomeRefused,
				Err:    &plugin.Error{Code: plugin.CodeBusy, Args: []string{"demo"}},
			},
			true,
			"demo: too many plugin programs are running, wait for one to end",
		},
		{
			"not started",
			plugin.Outcome{
				Status: plugin.OutcomeNotStarted,
				Err:    &plugin.Error{Code: plugin.CodeStartFailed, Args: []string{"demo"}},
			},
			true,
			"plugin demo: could not start",
		},
		{
			"refused",
			plugin.Outcome{
				Status: plugin.OutcomeRefused,
				Err: &plugin.Error{Code: plugin.CodeNotRunnable, Args: []string{
					"demo",
					"disabled",
				}},
			},
			true, "demo: plugin demo cannot run (disabled)",
		},
	}

	for _, tc := range tests {
		got, bad := e.outcomeText("demo", tc.out, 5*time.Second)

		if bad != tc.bad || got != tc.want && !strings.HasPrefix(got, tc.want) {
			t.Errorf("%s: %q bad %v, want %q bad %v", tc.name, got, bad, tc.want, tc.bad)
		}
	}

	if strings.Count(func() string { s, _ := e.outcomeText("demo", tests[6].out, 0); return s }(),
		"demo") != 1 {
		t.Error("the id is said twice")
	}

	got, _ := e.plugOutcome("demo", RunResult{Signal: "signal 12"})
	if !strings.HasSuffix(got, "signal 12") || strings.Contains(got, "signal signal") {
		t.Errorf("terminal signal: %q", got)
	}
}

// TestPauseOf checks the pause policy of a contribution, and that an unknown name pauses.
func TestPauseOf(t *testing.T) {
	for name, want := range map[string]PausePolicy{
		"": PauseAlways, "bogus": PauseAlways,
		"always": PauseAlways, "error": PauseError, "never": PauseNever,
	} {
		if got := pauseOf(name); got != want {
			t.Errorf("pauseOf(%q) = %v want %v", name, got, want)
		}
	}
}

// TestPlugLaunchGuards checks the checks that come before a program starts: the plugin is read
// again before a terminal run, a second run does not start, an unconfirmed run does not start and
// a directory that is gone is said.
func TestPlugLaunchGuards(t *testing.T) {
	g, _, _ := gftRig(t)
	st := pluginStart{Plugin: "gft", Label: "x"}
	dir := t.TempDir()

	gone := plugin.Prepared{
		PluginID: "nobody", Argv: []string{"/bin/true"}, Dir: dir,
		Output: plugin.OutputTerminal,
	}

	if cmd := g.r.runTerminal(st, gone); cmd != nil || g.r.guard.running() ||
		!strings.Contains(g.toastText(), "nobody") {
		t.Errorf("a terminal run of a plugin that is gone started (toast %q)", g.toastText())
	}

	g.r.toast.text = ""

	g.r.run = &plugRun{}
	g.r.launch(st, plugin.Prepared{Argv: []string{"/bin/true"}, Dir: dir})

	if seq := g.r.runSeq; seq != 0 || !strings.Contains(g.toastText(), "already running") {
		t.Errorf("a second run started (seq %d, toast %q)", seq, g.toastText())
	}

	g.r.run = nil
	g.r.toast.text = ""
	g.r.launch(st, plugin.Prepared{
		Argv: []string{"/bin/true"}, Dir: dir,
		Confirm: plugin.Confirmation{Required: true},
	})

	if g.r.runSeq != 0 || g.r.run != nil || !strings.Contains(g.toastText(), "not confirmed") {
		t.Errorf("an unconfirmed run started (toast %q)", g.toastText())
	}

	g.r.toast.text = ""
	g.r.launch(st, plugin.Prepared{Argv: []string{"/bin/true"}, Dir: filepath.Join(dir, "gone")})

	if g.r.runSeq != 0 || !strings.Contains(g.toastText(), "gone") ||
		!strings.Contains(g.toastText(), "directory") {
		t.Errorf("a missing directory: seq %d toast %q", g.r.runSeq, g.toastText())
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}

// TestPlugFormRefusal checks that what the registry refuses when the form is answered is shown
// under the form, which stays open.
func TestPlugFormRefusal(t *testing.T) {
	g, _, log := gftRig(t)
	g.startShow("-bad/x", "")
	g.press("enter")

	f, ok := g.top().(*fieldsForm)
	if !ok {
		t.Fatalf("the refused form led to %T:\n%s", g.top(), g.text())
	}

	if f.saving || len(readLog(t, log)) != 0 || !strings.Contains(g.text(), "dash") &&
		!strings.Contains(g.text(), "-bad") && !strings.Contains(g.text(), "not valid") {
		t.Errorf("the refusal is not shown:\n%s", g.text())
	}
}
