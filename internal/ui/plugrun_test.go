package ui

import (
	"os"
	"strings"
	"testing"
)

// readLog returns the lines the fake programs recorded.
//
// Parameters:
//   - t: running test
//   - log: path of the log file
func readLog(t *testing.T, log string) []string {
	t.Helper()

	b, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		t.Fatal(err)
	}

	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// repoActions opens the menu of the actions of the first repository and returns it.
func (g *rig) repoActions() *actionMenu {
	g.t.Helper()
	g.press("tab")

	m, ok := g.top().(*actionMenu)
	if !ok {
		g.t.Fatalf("Tab opened %T:\n%s", g.top(), g.text())
	}

	return m
}

// settled waits until the run that the interface shows has ended.
func (g *rig) settled() {
	g.t.Helper()
	g.await("the run to end", func() bool { return g.r.run == nil })
}

// TestGftDropStash plays the whole way of a record action with a fake gft: select the
// repository, open the menu, pick "Drop a stash", answer the number, read the exact command in
// the question, run it and read the end on the status line.
func TestGftDropStash(t *testing.T) {
	g, _, log := gftRig(t)

	m := g.repoActions()
	if len(m.offers) != 7 {
		t.Fatalf("%d actions, want 7:\n%s", len(m.offers), g.text())
	}

	for _, want := range []string{"Show the latest release", "Drop a stash", "Actions: proj"} {
		if !strings.Contains(g.text(), want) {
			t.Errorf("the menu lacks %q:\n%s", want, g.text())
		}
	}

	if m.why[0] == "" || m.why[5] != "" {
		t.Errorf("release must be greyed, drop must not: %q", m.why)
	}

	g.press("down", "down", "down", "down", "down", "enter")

	if _, ok := g.top().(*fieldsForm); !ok || !strings.Contains(g.text(), "Stash number") {
		t.Fatalf("Enter opened %T:\n%s", g.top(), g.text())
	}

	g.typeText("3")
	g.press("enter")

	a, ok := g.top().(*runAsk)
	if !ok {
		t.Fatalf("the form led to %T:\n%s", g.top(), g.text())
	}

	for _, want := range []string{
		"Drop stash 3 of proj?", "gft stash drop 3 --yes", "Cancel",
		"Run",
	} {
		if !strings.Contains(g.text(), want) {
			t.Errorf("the question lacks %q:\n%s", want, g.text())
		}
	}

	if a.yes {
		t.Error("the question starts on yes")
	}

	g.press("y")

	if _, still := g.top().(*runAsk); !still || len(readLog(t, log)) != 0 {
		t.Fatalf("y answered the question (log %v)", readLog(t, log))
	}

	g.press("enter")

	if g.top() != nil && len(readLog(t, log)) != 0 {
		t.Fatalf("Enter on Cancel ran the program: %v", readLog(t, log))
	}

	g.press("tab")
	g.press("down", "down", "down", "down", "down", "enter")
	g.typeText("3")
	g.press("enter", "tab", "enter")
	g.settled()

	if got := readLog(t, log); len(got) != 1 || got[0] != "stash drop 3 --yes" {
		t.Fatalf("log %q", got)
	}

	if !strings.Contains(g.toastText(), "plugin gft: done") {
		t.Errorf("toast %q", g.toastText())
	}

	if !strings.Contains(g.text(), "gft: stash drop") {
		t.Errorf("the pager lacks the output:\n%s", g.text())
	}

	g.press("esc")

	if g.top() != nil {
		t.Errorf("Esc left %T open", g.top())
	}
}

// TestGftRefusedAction checks that an action that cannot run stays in the menu with its reason
// and starts nothing.
func TestGftRefusedAction(t *testing.T) {
	g, _, log := gftRig(t)
	g.repoActions()
	g.press("enter")

	if _, ok := g.top().(*actionMenu); !ok {
		t.Fatalf("Enter on a greyed action led to %T", g.top())
	}

	if !strings.Contains(g.toastText(), "gft:") || len(readLog(t, log)) != 0 {
		t.Errorf("toast %q, log %q", g.toastText(), readLog(t, log))
	}
}

// startShow opens the form of the command "Show a GitHub release" from the palette and fills
// it, leaving the last field.
//
// Parameters:
//   - slug: value of the first field
//   - tag: value of the second field
func (g *rig) startShow(slug, tag string) {
	g.t.Helper()
	g.press("ctrl+k")
	g.typeText("show a github")
	g.press("enter")

	if _, ok := g.top().(*fieldsForm); !ok {
		g.t.Fatalf("the palette led to %T:\n%s", g.top(), g.text())
	}

	g.typeText(slug)
	g.press("tab")
	g.typeText(tag)
	g.press("tab")
}

// TestGftFailureShowsOutput checks that a program that fails leaves its output in the pager and
// words the failure with the plugin id and the exit status.
func TestGftFailureShowsOutput(t *testing.T) {
	g, _, _ := gftRig(t)

	g.startShow("demo/proj", "fail1")
	g.press("enter")
	g.settled()

	if !strings.Contains(g.toastText(), "plugin gft: failed, exit status 3") {
		t.Errorf("toast %q", g.toastText())
	}

	for _, want := range []string{"gft: demo/proj fail1", "boom"} {
		if !strings.Contains(g.text(), want) {
			t.Errorf("the pager lacks %q:\n%s", want, g.text())
		}
	}

	if !strings.Contains(g.text(), "plugin gft: failed") {
		t.Errorf("status %q", g.text())
	}
}

// TestGftCancel checks that Esc stops a program that runs, once, and that the view stays to show
// what it wrote.
func TestGftCancel(t *testing.T) {
	g, _, _ := gftRig(t)

	g.startShow("demo/proj", "hang1")
	g.press("enter")

	if g.r.run == nil {
		t.Fatal("no run started")
	}

	if _, ok := g.top().(*runView); !ok {
		t.Fatalf("the run shows %T", g.top())
	}

	g.press("esc", "esc")
	g.settled()

	if !strings.Contains(g.toastText(), "plugin gft: stopped") {
		t.Errorf("toast %q", g.toastText())
	}

	if v, ok := g.top().(*runView); !ok || v.running {
		t.Fatalf("view after the stop: %T", g.top())
	}

	g.press("esc")

	if g.top() != nil {
		t.Errorf("Esc left %T", g.top())
	}
}

// TestGftOneRunAtATime checks that a second contribution does not start while a run shows.
func TestGftOneRunAtATime(t *testing.T) {
	g, _, log := gftRig(t)

	g.startShow("demo/proj", "hang1")
	g.press("enter")
	g.send(pluginStartMsg{Start: pluginStart{Plugin: "gft", Table: "command", Entry: "show"}})

	if !strings.Contains(g.toastText(), "already running") {
		t.Errorf("toast %q", g.toastText())
	}

	g.press("esc")
	g.settled()

	if got := readLog(t, log); len(got) != 1 {
		t.Errorf("log %q", got)
	}
}

// TestGftOpen checks the contribution that opens an address: the question shows the address,
// Cancel opens nothing, Run calls the opener.
func TestGftOpen(t *testing.T) {
	g, _, log := gftRig(t)

	open := func() {
		g.press("ctrl+k")
		g.typeText("releases page")
		g.press("enter")
		g.typeText("demo/proj")
		g.press("enter")
	}

	open()

	if _, ok := g.top().(*runAsk); !ok ||
		!strings.Contains(g.text(), "https://github.com/demo/proj/releases") {
		t.Fatalf("the question: %T\n%s", g.top(), g.text())
	}

	g.press("esc")

	if got := readLog(t, log); len(got) != 0 {
		t.Fatalf("Esc opened %q", got)
	}

	open()
	g.press("tab", "enter")
	g.await("the opener", func() bool { return len(readLog(t, log)) == 1 })

	if got := readLog(t, log); got[0] != "https://github.com/demo/proj/releases" {
		t.Errorf("opener got %q", got)
	}

	g.await("the end of the request", func() bool { return !g.r.opening })

	if !strings.Contains(g.toastText(), "address opened") {
		t.Errorf("toast %q", g.toastText())
	}
}

// TestGftTerminalCommand checks the program that owns the terminal: it goes through the exec path
// with the directory and the environment of the registry, flushes the keys typed meanwhile, waits
// for Enter after it, and is refused when the plugin was turned off after the question.
func TestGftTerminalCommand(t *testing.T) {
	run := &fakeRunner{Res: RunResult{ExitCode: 0}}
	sb := newSandbox(t)
	sb.Opts.Runner = run
	l := newPlugLab(t, sb)
	l.runTools()
	l.installGft()

	g := newRig(t, sb)
	g.r.delay = 0
	st := pluginStart{Plugin: "gft", Table: "command", Entry: "get", Label: "Download"}

	p, err := l.reg.Prepare(g.r.e.callOf(st, nil, &fieldValues{}))
	if err == nil {
		t.Fatalf("an empty form was accepted: %+v", p)
	}

	call := g.r.e.callOf(st, nil, nil)
	call.Values = map[string]string{
		"slug": "demo/proj", "tag": "", "pattern": "linux",
		"verify": "",
	}

	p, err = l.reg.Prepare(call)
	if err != nil {
		t.Fatal(err)
	}

	cmd, done := g.r.terminalCommand(st, p)
	cmd.SetStdin(strings.NewReader(""))
	cmd.SetStdout(&strings.Builder{})
	cmd.SetStderr(&strings.Builder{})

	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	calls := run.calls()
	if len(calls) != 1 || !calls[0].Flush || calls[0].Pause != PauseAlways ||
		calls[0].Dir != p.Dir || len(calls[0].Env) == 0 || calls[0].Argv[1] != "demo/proj" {
		t.Fatalf("calls %+v", calls)
	}

	if msg := done(nil).(execDoneMsg); msg.After.Plug != "gft" || msg.Denied != nil {
		t.Errorf("done %+v", msg)
	}

	if _, err := l.reg.Disable("gft"); err != nil {
		t.Fatal(err)
	}

	cmd, done = g.r.terminalCommand(st, p)
	cmd.SetStdin(strings.NewReader(""))
	cmd.SetStdout(&strings.Builder{})
	cmd.SetStderr(&strings.Builder{})

	_ = cmd.Run()

	msg := done(nil).(execDoneMsg)
	if len(run.calls()) != 1 || msg.Denied == nil {
		t.Fatalf("the disabled plugin ran: %d calls, denied %v", len(run.calls()), msg.Denied)
	}

	g.r.guard.begin()
	g.send(msg)

	if !strings.Contains(g.toastText(), "gft") || g.r.guard.running() {
		t.Errorf("toast %q", g.toastText())
	}
}

// TestGftRefusals checks that every refusal of Prepare is shown with its message.
func TestGftRefusals(t *testing.T) {
	g, l, _ := gftRig(t)

	st := pluginStart{Plugin: "gft", Table: "command", Entry: "show"}
	if _, err := l.reg.Disable("gft"); err != nil {
		t.Fatal(err)
	}

	g.send(pluginStartMsg{Start: st})

	if g.toastText() == "" || !strings.Contains(g.toastText(), "gft") {
		t.Errorf("toast %q", g.toastText())
	}

	st.Entry = "nothing"
	g.send(pluginStartMsg{Start: st})

	if g.toastText() == "" {
		t.Error("an unknown contribution was not refused")
	}
}

// TestPaletteCommands checks that the commands of the enabled plugins are in the palette, found
// by their label and their keywords, and gone once the plugin is off.
func TestPaletteCommands(t *testing.T) {
	g, l, _ := gftRig(t)

	g.press("ctrl+k")
	g.typeText("assets")

	if !strings.Contains(g.text(), "Show a GitHub release") || !strings.Contains(g.text(), "gft") {
		t.Errorf("a keyword does not find the command:\n%s", g.text())
	}

	g.press("esc", "esc")

	if _, err := l.reg.Disable("gft"); err != nil {
		t.Fatal(err)
	}

	g.r.e.pluginsChanged()

	for _, m := range collect(g.r.e.Plug.fetch()) {
		g.send(m)
	}

	g.press("ctrl+k")
	g.typeText("release")

	if strings.Contains(g.text(), "Show a GitHub") {
		t.Errorf("a disabled plugin is in the palette:\n%s", g.text())
	}
}

// TestActionMenuWithoutPlugins checks the message when no plugin acts on a repository.
func TestActionMenuWithoutPlugins(t *testing.T) {
	sb := newSandbox(t)
	newPlugLab(t, sb)
	sb.Opts.Start = StartRepos
	g := newRig(t, sb)
	g.press("tab")

	if !strings.Contains(g.toastText(), "no plugin action") || g.top() != nil {
		t.Errorf("toast %q top %T", g.toastText(), g.top())
	}
}

// TestRunScreensResize checks that the form, the question, the menu and the output redraw at
// every size while they are open, down to the smallest terminal.
func TestRunScreensResize(t *testing.T) {
	g, _, _ := gftRig(t)

	check := func(what string) {
		t.Helper()

		for _, s := range [][2]int{{80, 24}, {40, 12}, {24, 7}, {100, 30}, {24, 7}} {
			g.resize(s[0], s[1])
			checkScreen(t, what, g.r.render(), s[0], s[1], false)
		}
	}

	g.press("tab")
	check("menu")
	g.press("down", "down", "down", "down", "down", "enter")
	check("form")
	g.typeText("3")
	g.press("enter")
	check("question")
	g.press("esc")
	g.startShow("demo/proj", "many1")
	g.press("enter")
	g.settled()
	check("pager")
	g.press("pgdown", "end")
	check("pager end")
}

// TestGftEndStopsRun checks that the end of the interface stops the program of a run that still
// shows, and waits for the registry to be done with it, so that no program outlives tote.
func TestGftEndStopsRun(t *testing.T) {
	g, _, _ := gftRig(t)

	g.startShow("demo/proj", "hang1")
	g.press("enter")

	run := g.r.run
	if run == nil {
		t.Fatal("no run started")
	}

	g.r.closeOverlays()

	select {
	case <-run.finished:
	default:
		t.Fatal("the registry was still running the program after the interface ended")
	}

	g.r.closeOverlays()

	if g.r.run != nil {
		t.Error("the run is still recorded")
	}
}
