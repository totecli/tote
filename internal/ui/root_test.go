package ui

import (
	"image/color"
	"os"
	"strings"
	"syscall"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/totecli/tote/internal/shell"
)

// probeOverlay is an overlay that records what it receives and closes on the key "x".
type probeOverlay struct {
	got  []tea.Msg
	init bool
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (p *probeOverlay) Init(*env) tea.Cmd {
	p.init = true

	return nil
}

// Update implements overlay.
//
// Parameters:
//   - e: shared environment, unused
//   - msg: message to record
func (p *probeOverlay) Update(_ *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	p.got = append(p.got, msg)

	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "x" {
		return p, toastCmd("probe closed", false), true
	}

	return p, nil, false
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (p *probeOverlay) View(e *env, maxW, maxH int) []string {
	return boxed(e, "probe", []string{" content "}, min(maxW, 24))[:min(maxH, 3)]
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (p *probeOverlay) Keys(e *env) []key.Binding {
	return []key.Binding{e.Keys.Close}
}

// TestHomeNavigation checks the welcome page: rendering, movement and opening a list.
//
// Parameters:
//   - t: test to report to
func TestHomeNavigation(t *testing.T) {
	g := newRig(t, newSandbox(t))

	for _, want := range []string{
		"tote", "1.2.3", "Welcome, tester", "Connections", "Repos",
		"Aliases", "Functions", "6", "open", "quit",
	} {
		if !strings.Contains(g.text(), want) {
			t.Errorf("home lacks %q:\n%s", want, g.text())
		}
	}

	h := g.r.scr.(*homeScreen)

	g.press("down", "down")

	if h.sel != 2 {
		t.Fatalf("after two downs sel = %d, want 2", h.sel)
	}

	g.press("up", "up", "up")

	if h.sel != homeEntries-1 {
		t.Errorf("up wraps to the last entry: sel = %d, want %d", h.sel, homeEntries-1)
	}

	g.press("home")

	if h.sel != 0 {
		t.Errorf("home: sel = %d", h.sel)
	}

	g.press("end")

	if h.sel != homeEntries-1 {
		t.Errorf("end: sel = %d", h.sel)
	}

	g.press("j", "j")

	if h.sel != 1 {
		t.Errorf("down wraps to the first entry: sel = %d, want 1", h.sel)
	}

	g.press("enter")

	l, ok := g.r.scr.(*listScreen)
	if !ok || l.kind != kindRepo {
		t.Fatalf("enter opened %T, want the repository list", g.r.scr)
	}

	g.press("esc")

	if h, ok := g.r.scr.(*homeScreen); !ok || h.sel != int(kindRepo) {
		t.Errorf("esc went back to %+v, want the welcome page on the repositories", g.r.scr)
	}

	g.press("3")

	if l, ok := g.r.scr.(*listScreen); !ok || l.kind != kindAlias {
		t.Errorf("key 3 opened %T", g.r.scr)
	}

	g.send(goHomeMsg{})
	g.press(",")

	if _, ok := g.r.scr.(*configScreen); !ok {
		t.Errorf("the comma key opened %T, want the settings", g.r.scr)
	}

	g.send(goHomeMsg{})
	g.press("9", "0", "x")

	if _, ok := g.r.scr.(*homeScreen); !ok {
		t.Errorf("an unknown key left the welcome page: %T", g.r.scr)
	}

	if g.Quit {
		t.Error("an unknown key quit")
	}
}

// TestHomeWelcomeSettings checks the three welcome settings.
//
// Parameters:
//   - t: test to report to
func TestHomeWelcomeSettings(t *testing.T) {
	tests := []struct {
		welcome string
		want    string
		absent  string
	}{
		{"box", "Welcome, tester", ""},
		{"compact", "6 conn", "Welcome"},
		{"off", "Connections", "Welcome"},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Settings.UI.Welcome = tt.welcome }))

		if !strings.Contains(g.text(), tt.want) {
			t.Errorf("welcome %s lacks %q:\n%s", tt.welcome, tt.want, g.text())
		}

		if tt.absent != "" && strings.Contains(g.text(), tt.absent) {
			t.Errorf("welcome %s shows %q", tt.welcome, tt.absent)
		}
	}
}

// TestListFilter checks typing in the filter, the ranking and the way out of it.
//
// Parameters:
//   - t: test to report to
func TestListFilter(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	l := g.r.scr.(*listScreen)

	if len(l.shown) != 6 || l.Typing() {
		t.Fatalf("start: %d shown, typing %v", len(l.shown), l.Typing())
	}

	g.press("/")

	if !l.Typing() {
		t.Fatal("/ did not start the filter")
	}

	g.press("q")

	if g.Quit || l.flt.String() != "q" {
		t.Errorf("q while typing: quit %v, filter %q", g.Quit, l.flt.String())
	}

	g.press("backspace")
	g.typeText("stag")

	if len(l.shown) != 1 || l.selected().Key != "staging" {
		t.Fatalf("filter st shows %d records, selected %v", len(l.shown), l.selected())
	}

	if !strings.Contains(g.text(), "1/6") || !strings.Contains(g.text(), "/ stag") {
		t.Errorf("filter line or counter missing:\n%s", g.text())
	}

	g.press("ctrl+u")

	if len(l.shown) != 6 || l.flt.String() != "" {
		t.Errorf("ctrl+u: %d shown, filter %q", len(l.shown), l.flt.String())
	}

	g.typeText("prod stag")
	g.press("ctrl+w")

	if l.flt.String() != "prod " {
		t.Errorf("ctrl+w left %q", l.flt.String())
	}

	g.press("esc")

	if l.Typing() || l.flt.String() != "" || len(l.shown) != 6 {
		t.Errorf("esc: typing %v, filter %q, %d shown", l.Typing(), l.flt.String(), len(l.shown))
	}

	g.press("/")
	g.send(tea.PasteMsg{Content: "ba\nstion"})

	if l.flt.String() != "bastion" || len(l.shown) != 1 {
		t.Errorf("paste: filter %q, %d shown", l.flt.String(), len(l.shown))
	}

	g.press("enter")

	if l.Typing() || l.flt.String() != "bastion" {
		t.Errorf("enter: typing %v filter %q", l.Typing(), l.flt.String())
	}

	if g.r.guard.busy.Load() != 0 {
		t.Error("enter on the single match started the connection: it only applies the filter")
	}

	g.press("enter")

	if g.r.guard.busy.Load() != 1 {
		t.Error("a second enter on the single match did not start the connection")
	}

	g.send(execDoneMsg{Label: "ssh", Result: RunResult{}})
	g.press("esc")

	if l.flt.String() != "" {
		t.Errorf("esc outside typing left the filter %q", l.flt.String())
	}

	g.typeText("zzz")
	g.send(tea.PasteMsg{Content: "ignored"})
}

// TestListNoResults checks the messages of an empty list.
//
// Parameters:
//   - t: test to report to
func TestListNoResults(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartAliases }))

	g.press("/")
	g.typeText("zzz")

	if !strings.Contains(g.text(), `No result for "zzz"`) {
		t.Errorf("no result message missing:\n%s", g.text())
	}

	g.press("enter", "up", "down", "pgup", "pgdown", "esc")

	writeFile(t, g.r.e.Paths.AliasesFile(), "schema = 1\n")
	g.press("ctrl+r")

	if !strings.Contains(g.text(), "Nothing here yet") {
		t.Errorf("empty message missing:\n%s", g.text())
	}

	if g.toastText() != "data reloaded" {
		t.Errorf("toast = %q", g.toastText())
	}

	g.press("e", "d", "o", "enter")

	if g.Quit || len(g.r.overlays) != 0 {
		t.Errorf("keys on an empty list: quit %v, overlays %d", g.Quit, len(g.r.overlays))
	}
}

// TestListMovement checks the cursor keys and the scrolling of a long list.
//
// Parameters:
//   - t: test to report to
func TestListMovement(t *testing.T) {
	s := newSandbox(t)

	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i := 0; i < 40; i++ {
		b.WriteString("\n[[alias]]\nname = \"a" + string(rune('a'+i/26)) + string(rune('a'+i%26)) +
			"\"\ncmd = \"true\"\n")
	}

	writeFile(t, s.Opts.Paths.AliasesFile(), b.String())

	g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases; o.Height = 20 }))
	l := g.r.scr.(*listScreen)
	rows := l.rows(g.r.e)

	g.press("pgdown")

	if l.sel != rows {
		t.Errorf("pgdown: sel %d, want %d", l.sel, rows)
	}

	g.press("end")

	if l.sel != 39 || l.off != maxOffset(rows, 40) {
		t.Errorf("end: sel %d off %d", l.sel, l.off)
	}

	if !strings.Contains(g.text(), "abn") {
		t.Errorf("the last record is not visible:\n%s", g.text())
	}

	g.press("G", "g")

	if l.sel != 0 || l.off != 0 {
		t.Errorf("home: sel %d off %d", l.sel, l.off)
	}

	g.press("j", "j", "k", "ctrl+f", "ctrl+b", "up", "up")

	if l.sel != 0 {
		t.Errorf("sel = %d, want 0", l.sel)
	}
}

// TestListTabs checks the movement between kinds with the arrows and h and l.
//
// Parameters:
//   - t: test to report to
func TestListTabs(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	kindOf := func() kind { return g.r.scr.(*listScreen).kind }

	g.press("l")

	if kindOf() != kindRepo {
		t.Errorf("l: %d", kindOf())
	}

	g.press("right", "right", "right")

	if kindOf() != kindConn {
		t.Errorf("right wraps: %d", kindOf())
	}

	g.press("left")

	if kindOf() != kindFunc {
		t.Errorf("left wraps: %d", kindOf())
	}

	g.press("h")

	if kindOf() != kindAlias {
		t.Errorf("h: %d", kindOf())
	}
}

// TestActivate checks what Enter does for each kind.
//
// Parameters:
//   - t: test to report to
func TestActivate(t *testing.T) {
	s := newSandbox(t)

	tests := []struct {
		start  Start
		down   int
		action shell.Action
		load   string
	}{
		{StartRepos, 0, shell.ChangeDir, s.Repo},
		{StartAliases, 0, shell.Insert, "ls -la"},
		{StartAliases, 1, shell.Insert, "git status"},
		{StartFuncs, 0, shell.Insert, "hello"},
	}

	for _, tt := range tests {
		g := newRig(t, s.with(func(o *Options) { o.Start = tt.start }))

		for i := 0; i < tt.down; i++ {
			g.press("down")
		}

		g.press("enter")

		if !g.Quit || g.r.result.Action != tt.action || g.r.result.Payload != tt.load {
			t.Errorf("start %d: quit %v, result %+v, want %s %q", tt.start, g.Quit, g.r.result,
				tt.action, tt.load)
		}
	}
}

// TestActivateRefusals checks the records that Enter refuses to use.
//
// Parameters:
//   - t: test to report to
func TestActivateRefusals(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.ReposFile(), "schema = 1\n\n[[repo]]\nname = \"gone\"\n"+
		"path = \"/tote-no-such-dir/x\"\n\n[[repo]]\nname = \"rel\"\npath = \"rel/dir\"\n")

	g := newRig(t, s.with(func(o *Options) { o.Start = StartRepos }))

	g.press("enter")

	if g.Quit || !strings.Contains(g.toastText(), "directory not found") || !g.r.toast.err {
		t.Errorf("missing directory: quit %v, toast %q", g.Quit, g.toastText())
	}

	g.press("down", "enter")

	if g.Quit || !strings.Contains(g.toastText(), "absolute") {
		t.Errorf("relative path: quit %v, toast %q", g.Quit, g.toastText())
	}

	c := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	c.press("/")
	c.typeText("legacy")
	c.press("enter", "enter")

	if c.Quit || !strings.Contains(c.toastText(), "needs review") || c.r.guard.busy.Load() != 0 {
		t.Errorf("needs review: quit %v, toast %q, busy %d", c.Quit, c.toastText(),
			c.r.guard.busy.Load())
	}

	c.send(activateMsg{Kind: kindConn, Key: "ghost"})

	if !strings.Contains(c.toastText(), "not found") {
		t.Errorf("unknown record: toast %q", c.toastText())
	}

	c.send(editorMsg{Key: "ghost"})

	if !strings.Contains(c.toastText(), "not found") {
		t.Errorf("unknown repository: toast %q", c.toastText())
	}
}

// TestConnectStartsProgram checks that a connection arms the signal guard and that its end is
// handled.
//
// Parameters:
//   - t: test to report to
func TestConnectStartsProgram(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))

	g.press("enter")

	if g.r.guard.busy.Load() != 1 {
		t.Fatal("the guard was not armed")
	}

	g.send(execDoneMsg{Label: "ssh prod", Result: RunResult{ExitCode: 255}})

	if g.r.guard.busy.Load() != 0 {
		t.Error("the guard stayed armed")
	}

	if !strings.Contains(g.toastText(), "255") || !g.r.toast.err {
		t.Errorf("toast = %q err %v", g.toastText(), g.r.toast.err)
	}

	g.send(execDoneMsg{Label: "ssh prod", Result: RunResult{ExitCode: 3}})

	if !strings.Contains(g.toastText(), "3") || g.r.toast.err {
		t.Errorf("exit 3: toast %q err %v", g.toastText(), g.r.toast.err)
	}

	g.send(toastMsg{Text: "x"})
	g.send(execDoneMsg{Label: "ssh prod", Result: RunResult{}})

	if g.toastText() != "x" {
		t.Errorf("a clean exit changed the toast: %q", g.toastText())
	}

	g.send(execDoneMsg{Label: "ssh prod", Restore: os.ErrClosed})

	if !g.r.toast.err {
		t.Error("a restore error is not reported")
	}

	g.r.guard.begin()
	g.r.guard.classify(syscall.SIGTERM)
	g.send(execDoneMsg{Label: "ssh prod"})

	if !g.Quit {
		t.Error("a SIGTERM received during the program did not quit")
	}
}

// TestPrepareExec checks the command built for a program.
//
// Parameters:
//   - t: test to report to
func TestPrepareExec(t *testing.T) {
	run := &fakeRunner{Res: RunResult{ExitCode: 4}}
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Runner = run }))
	g.r.delay = 0

	cmd, done := g.r.prepareExec([]string{"ssh", "-l", "u", "--", "h"}, after{Quit: true})
	cmd.SetStdin(strings.NewReader(""))
	cmd.SetStdout(&strings.Builder{})
	cmd.SetStderr(&strings.Builder{})

	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	msg, ok := done(nil).(execDoneMsg)
	if !ok || msg.Result.ExitCode != 4 || !msg.After.Quit || msg.Label != "ssh -l u -- h" {
		t.Errorf("callback gave %+v", msg)
	}

	if got := run.calls(); len(got) != 1 || got[0].Argv[0] != "ssh" {
		t.Errorf("runner calls = %+v", got)
	}
}

// TestEditor checks the repository editor with each repos.after_open setting.
//
// Parameters:
//   - t: test to report to
func TestEditor(t *testing.T) {
	tests := []struct {
		after   string
		quit    bool
		action  shell.Action
		payload bool
	}{
		{"return", false, "", false},
		{"quit", true, "", false},
		{"cd", true, shell.ChangeDir, true},
	}

	for _, tt := range tests {
		s := newSandbox(t)
		g := newRig(t, s.with(func(o *Options) {
			o.Start = StartRepos
			o.Settings.Repos.AfterOpen = tt.after
			o.Environ = append(o.Environ, "EDITOR=myeditor --wait")
		}))

		g.press("o")

		if g.r.guard.busy.Load() != 1 {
			t.Errorf("%s: the editor was not started", tt.after)
		}

		next := after{}

		switch tt.after {
		case "quit":
			next.Quit = true
		case "cd":
			next = after{Quit: true, Action: shell.ChangeDir, Payload: s.Repo}
		}

		g.send(execDoneMsg{Label: "myeditor", After: next})

		if g.Quit != tt.quit || g.r.result.Action != tt.action ||
			(g.r.result.Payload != "") != tt.payload {
			t.Errorf("%s: quit %v result %+v", tt.after, g.Quit, g.r.result)
		}
	}
}

// TestReadOnlyKinds checks that a file with a syntax error cannot be changed from the list.
//
// Parameters:
//   - t: test to report to
func TestReadOnlyKinds(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n[[alias\n")

	g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))

	for _, k := range []string{"n", "e", "d"} {
		g.press(k)
	}

	if !strings.Contains(g.text(), "syntax error") || !strings.Contains(g.text(), "read-only") {
		t.Errorf("the reason is not shown:\n%s", g.text())
	}

	g.send(addMsg{Kind: kindAlias})

	if !g.r.toast.err || !strings.Contains(g.toastText(), "syntax error") {
		t.Errorf("add on a read-only file: toast %q", g.toastText())
	}

	g.send(deleteMsg{Kind: kindAlias, Key: "ll"})

	if !g.r.toast.err {
		t.Error("delete on a read-only file was not refused")
	}

	keys := g.r.scr.Keys(g.r.e)

	for _, b := range keys {
		if b.Help().Key == "n" || b.Help().Key == "d" {
			t.Errorf("a read-only list advertises %q", b.Help().Key)
		}
	}

	if !strings.Contains(g.text(), "Nothing to show") {
		t.Errorf("empty text for an unreadable file missing:\n%s", g.text())
	}

	g.send(editMsg{Kind: kindConn, Key: "ghost"})

	if !strings.Contains(g.toastText(), "not found") {
		t.Errorf("edit of an unknown record: %q", g.toastText())
	}
}

// TestWrote checks the reaction to a write made by an overlay.
//
// Parameters:
//   - t: test to report to
func TestWrote(t *testing.T) {
	s := newSandbox(t)
	g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))

	extra := "\n[[alias]]\nname = \"zz\"\ncmd = \"x\"\n"
	writeFile(t, s.Opts.Paths.AliasesFile(), fixtureAliases+extra)
	g.send(wroteMsg{Kind: kindAlias, Key: "zz", Op: opAdd})

	l := g.r.scr.(*listScreen)

	if len(l.items) != 3 || l.selected().Key != "zz" {
		t.Errorf("after a write: %d items, selected %v", len(l.items), l.selected())
	}

	if g.toastText() != `saved alias "zz"` || !g.r.stale {
		t.Errorf("toast %q, stale %v", g.toastText(), g.r.stale)
	}

	writeFile(t, s.Opts.Paths.AliasesFile(), fixtureAliases)
	g.send(wroteMsg{Kind: kindAlias, Key: "zz", Op: opDelete})

	if len(l.items) != 2 || !strings.Contains(g.toastText(), "removed") {
		t.Errorf("after a delete: %d items, toast %q", len(l.items), g.toastText())
	}

	g.send(finishMsg{Result: Result{Action: shell.Insert, Payload: "ls"}})

	if !g.Quit || !g.r.result.Reload || g.r.result.Payload != "ls" {
		t.Errorf("finish: quit %v result %+v", g.Quit, g.r.result)
	}

	c := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	c.send(wroteMsg{Kind: kindConn, Key: "prod", Op: opEdit})
	c.send(finishMsg{})

	if c.r.result.Reload {
		t.Error("a connection write asked the shell to reload its definitions")
	}
}

// TestOverlayStack checks that the top overlay gets the keys, hides the screen from them and
// closes itself.
//
// Parameters:
//   - t: test to report to
func TestOverlayStack(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	p := &probeOverlay{}

	g.send(openOverlayMsg{Overlay: p})

	if !p.init || len(g.r.overlays) != 1 {
		t.Fatalf("overlay not pushed: init %v, depth %d", p.init, len(g.r.overlays))
	}

	if !strings.Contains(g.text(), "probe") || !strings.Contains(g.text(), "esc close") {
		t.Errorf("the overlay or its footer is not drawn:\n%s", g.text())
	}

	g.press("q", "down", "n")

	if g.Quit || g.r.scr.(*listScreen).sel != 0 || len(g.r.overlays) != 1 {
		t.Errorf("keys reached the screen: quit %v sel %d", g.Quit, g.r.scr.(*listScreen).sel)
	}

	g.press("ctrl+k")

	if len(g.r.overlays) != 1 {
		t.Error("ctrl+k opened a second palette over an overlay")
	}

	if len(p.got) != 4 {
		t.Errorf("overlay received %d messages, want 4", len(p.got))
	}

	g.press("x")

	if len(g.r.overlays) != 0 || g.toastText() != "probe closed" {
		t.Errorf("after x: depth %d toast %q", len(g.r.overlays), g.toastText())
	}

	g.send(openOverlayMsg{Overlay: p})
	g.press("ctrl+d")

	if !g.Quit {
		t.Error("ctrl+d did not quit over an overlay")
	}
}

// TestToast checks the expiry of messages.
//
// Parameters:
//   - t: test to report to
func TestToast(t *testing.T) {
	g := newRig(t, newSandbox(t))

	g.send(toastMsg{Text: "first"})
	first := g.r.toast.id
	g.send(toastMsg{Text: "second", Err: true})

	if g.r.toast.id == first || g.toastText() != "second" {
		t.Fatalf("toast = %+v", g.r.toast)
	}

	g.send(toastExpireMsg{ID: first})

	if g.toastText() != "second" {
		t.Error("an old timer removed a newer message")
	}

	if !strings.Contains(g.text(), "second") {
		t.Errorf("toast not drawn:\n%s", g.text())
	}

	g.send(toastExpireMsg{ID: g.r.toast.id})

	if g.toastText() != "" {
		t.Error("the timer did not remove the message")
	}
}

// TestBackground checks the choice of the palette from the background of the terminal.
//
// Parameters:
//   - t: test to report to
func TestBackground(t *testing.T) {
	dark := tea.BackgroundColorMsg{Color: color.RGBA{A: 255}}
	light := tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}

	tests := []struct {
		name  string
		theme string
		color string
		msg   tea.Msg
		mode  themeMode
	}{
		{"auto waits", "auto", "truecolor", nil, modeNeutral},
		{"auto dark", "auto", "truecolor", dark, modeDark},
		{"auto light", "auto", "truecolor", light, modeLight},
		{"auto timeout", "auto", "truecolor", backgroundTimeoutMsg{}, modeDark},
		{"auto without colors", "auto", "none", nil, modeNeutral},
		{"fixed dark", "dark", "truecolor", light, modeDark},
		{"fixed light", "light", "256", nil, modeLight},
		{"mono", "mono", "truecolor", dark, modeNeutral},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Settings.UI.Theme = tt.theme
			o.Environ = append(o.Environ, "TOTE_COLOR="+tt.color)
		}))

		start := tt.theme == "auto" && tt.color != "none"
		asks := start || ((tt.theme == "dark" || tt.theme == "light") && tt.color != "none")

		if g.r.waiting != start {
			t.Errorf("%s: waiting = %v at start", tt.name, g.r.waiting)
		}

		if cmd := g.r.Init(); (cmd != nil) != asks {
			t.Errorf("%s: Init gave %v, asks %v", tt.name, cmd, asks)
		}

		if tt.msg != nil {
			g.send(tt.msg)
		}

		if g.r.e.Theme.Mode != tt.mode || (tt.msg != nil && g.r.waiting) {
			t.Errorf("%s: mode %d waiting %v, want mode %d", tt.name, g.r.e.Theme.Mode,
				g.r.waiting, tt.mode)
		}
	}
}

// TestResize checks that the layout follows the size of the terminal at every message.
//
// Parameters:
//   - t: test to report to
func TestResize(t *testing.T) {
	g := newRig(t, newSandbox(t))

	steps := []struct {
		w, h int
		mode layoutMode
		text string
	}{
		{100, 30, layoutFull, "Welcome"},
		{60, 30, layoutCompact, "6 conn"},
		{100, 15, layoutCompact, "6 conn"},
		{24, 7, layoutCompact, "6 conn"},
		{23, 30, layoutTiny, "Terminal too small"},
		{100, 6, layoutTiny, "Terminal too small"},
		{24, 6, layoutTiny, "Terminal too small"},
		{120, 40, layoutFull, "Welcome"},
	}

	for _, st := range steps {
		g.resize(st.w, st.h)

		if g.r.e.Layout != st.mode || !strings.Contains(g.text(), st.text) {
			t.Errorf("%dx%d: layout %d, want %d:\n%s", st.w, st.h, g.r.e.Layout, st.mode, g.text())
		}

		if got := len(g.view()); got != st.h {
			t.Errorf("%dx%d: %d lines", st.w, st.h, got)
		}
	}

	g.r.e.Width = 0

	if g.r.render() != "" {
		t.Error("an unknown size must render nothing")
	}
}

// TestModeReport checks that the terminal reporting grapheme clustering changes the metric.
//
// Parameters:
//   - t: test to report to
func TestModeReport(t *testing.T) {
	g := newRig(t, newSandbox(t))

	g.send(tea.ModeReportMsg{Mode: ansi.ModeFocusEvent, Value: ansi.ModeSet})

	if g.r.e.Metric.method != ansi.WcWidth {
		t.Error("an unrelated mode changed the metric")
	}

	g.send(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeNotRecognized})

	if g.r.e.Metric.method != ansi.WcWidth {
		t.Error("an unknown mode 2027 changed the metric")
	}

	g.send(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})

	if g.r.e.Metric.method != ansi.GraphemeWidth {
		t.Error("mode 2027 did not switch to the grapheme rule")
	}
}

// TestGlobalKeys checks the keys that work on every screen.
//
// Parameters:
//   - t: test to report to
func TestGlobalKeys(t *testing.T) {
	g := newRig(t, newSandbox(t))

	g.press("q")

	if !g.Quit {
		t.Error("q did not quit")
	}

	g = newRig(t, newSandbox(t))
	g.press("ctrl+c")

	if g.Quit {
		t.Error("a single ctrl+c quit")
	}

	g = newRig(t, newSandbox(t))
	g.press("ctrl+z")

	if g.Quit || len(g.Seen) < 2 {
		t.Errorf("ctrl+z: quit %v, seen %v", g.Quit, g.Seen)
	}

	if _, ok := g.Seen[1].(tea.SuspendMsg); !ok {
		t.Errorf("ctrl+z produced %T", g.Seen[1])
	}

	g = newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	g.press("/", "ctrl+d")

	if !g.Quit {
		t.Error("ctrl+d did not quit while typing a filter")
	}
}

// TestDebugPanic checks that the debug variable crashes on the first key and not before.
//
// Parameters:
//   - t: test to report to
func TestDebugPanic(t *testing.T) {
	for val, want := range map[string]bool{"1": true, "yes": true, "0": false, "": false} {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Environ = append(o.Environ, "TOTE_DEBUG_PANIC="+val)
		}))

		func() {
			defer func() {
				if (recover() != nil) != want {
					t.Errorf("TOTE_DEBUG_PANIC=%q: panic = %v, want %v", val, !want, want)
				}
			}()

			g.r.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			g.r.Update(keyMsg("down"))
		}()
	}
}

// TestView checks the interface contract of the view.
//
// Parameters:
//   - t: test to report to
func TestView(t *testing.T) {
	g := newRig(t, newSandbox(t))
	v := g.r.View()

	if !v.AltScreen {
		t.Error("the view does not ask for the alternate screen")
	}

	if v.Content == "" {
		t.Error("the view has no content")
	}
}

// TestListScrollMarks checks that a list that goes on has a muted "N more" line on its first or
// its last row, with the exact number of hidden rows, that the lines follow the scrolling, that
// the rows and the lines together fill the window, that a list that fits has none, and that the
// ASCII set has its own arrows.
//
// Parameters:
//   - t: test to report to
func TestListScrollMarks(t *testing.T) {
	s := newSandbox(t)

	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i := 0; i < 40; i++ {
		b.WriteString("\n[[alias]]\nname = \"a" + string(rune('a'+i/26)) + string(rune('a'+i%26)) +
			"\"\ncmd = \"true\"\n")
	}

	writeFile(t, s.Opts.Paths.AliasesFile(), b.String())

	g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases; o.Height = 20 }))
	l := g.r.scr.(*listScreen)
	e := g.r.e
	rows := l.rows(e)

	lines := func() []string {
		got, _ := l.rowLines(e, rows)
		for i := range got {
			got[i] = strings.TrimSpace(stripStyles(got[i]))
		}

		return got
	}

	up, down := e.Glyphs.above, e.Glyphs.below

	got := lines()
	if len(got) != rows || got[len(got)-1] != down+" 28 more" || strings.Contains(got[0], "more") {
		t.Errorf("top of the list: first %q, last %q (%d rows of %d)", got[0], got[len(got)-1],
			len(got), rows)
	}

	g.press("pgdown")

	got = lines()
	if len(got) != rows || !strings.HasPrefix(got[0], up+" ") ||
		!strings.HasPrefix(got[len(got)-1], down+" ") {
		t.Errorf("middle of the list: first %q, last %q", got[0], got[len(got)-1])
	}

	g.press("end")

	got = lines()
	if len(got) != rows || got[0] != up+" 28 more" || strings.Contains(got[len(got)-1], "more") {
		t.Errorf("end of the list: first %q, last %q", got[0], got[len(got)-1])
	}

	if !strings.Contains(got[len(got)-1], "abn") {
		t.Errorf("the last record is not on the last row: %q", got[len(got)-1])
	}

	for _, r := range got {
		if w := len([]rune(r)); w > e.Width {
			t.Errorf("row wider than the screen: %q", r)
		}
	}

	short := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartAliases }))
	sl := short.r.scr.(*listScreen)

	shortRows, _ := sl.rowLines(short.r.e, sl.rows(short.r.e))

	for _, r := range shortRows {
		if strings.Contains(stripStyles(r), "more") {
			t.Errorf("a list that fits has a line of hidden rows: %q", r)
		}
	}

	if a := newGlyphs(true); a.above != "^" || a.below != "v" {
		t.Errorf("ASCII marks = %q %q", a.above, a.below)
	}
}

// TestBackgroundFallback checks the palette chosen when the terminal does not answer the query
// for its background: COLORFGBG when it is readable, else the dark palette, which is what most
// terminals have. An answer that comes later still replaces the guess.
//
// Parameters:
//   - t: test to report to
func TestBackgroundFallback(t *testing.T) {
	tests := []struct {
		name string
		fgbg string
		mode themeMode
	}{
		{"not set", "", modeDark},
		{"dark terminal", "15;0", modeDark},
		{"light terminal", "0;15", modeLight},
		{"white", "0;7", modeLight},
		{"three fields, dark", "15;default;0", modeDark},
		{"bright black is dark", "15;8", modeDark},
		{"garbage", "light", modeDark},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Settings.UI.Theme = "auto"
			o.Environ = append(o.Environ, "TOTE_COLOR=truecolor", "COLORFGBG="+tt.fgbg)
		}))

		g.send(backgroundTimeoutMsg{})

		if g.r.e.Theme.Mode != tt.mode || g.r.waiting {
			t.Errorf("%s: mode %d waiting %v, want mode %d", tt.name, g.r.e.Theme.Mode,
				g.r.waiting, tt.mode)
		}
	}

	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Settings.UI.Theme = "auto"
		o.Environ = append(o.Environ, "TOTE_COLOR=truecolor")
	}))

	g.send(backgroundTimeoutMsg{})
	g.send(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})

	if g.r.e.Theme.Mode != modeLight {
		t.Errorf("a late answer left mode %d, want the light palette", g.r.e.Theme.Mode)
	}

	g = newRig(t, newSandbox(t).with(func(o *Options) {
		o.Settings.UI.Theme = "auto"
		o.Environ = append(o.Environ, "TOTE_COLOR=truecolor")
	}))

	g.send(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	g.send(backgroundTimeoutMsg{})

	if g.r.e.Theme.Mode != modeLight {
		t.Errorf("a timeout after the answer left mode %d, want the light palette",
			g.r.e.Theme.Mode)
	}
}

// TestDigitEntry checks that a digit reaches only the entries that come before the settings, so
// that an entry added later never moves a digit.
//
// Parameters:
//   - t: test to report to
func TestDigitEntry(t *testing.T) {
	tests := []struct {
		name string
		key  string
		n    int
		want int
		ok   bool
	}{
		{"first kind", "1", 4, 0, true},
		{"last kind", "4", 4, 3, true},
		{"no digit for the settings", "5", 4, 0, false},
		{"plugins with the host", "5", 5, 4, true},
		{"no digit for the settings with the host", "6", 5, 0, false},
		{"zero", "0", 4, 0, false},
		{"a letter", "x", 4, 0, false},
		{"two characters", "12", 4, 0, false},
		{"empty", "", 4, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := digitEntry(tt.key, tt.n)
			if got != tt.want || ok != tt.ok {
				t.Errorf("digitEntry(%q, %d) = %d, %v, want %d, %v", tt.key, tt.n, got, ok,
					tt.want, tt.ok)
			}
		})
	}
}

// TestTypedAhead checks that keys which arrive in the same burst as a key that opens a form or
// another list reach the new screen: the program does not run the commands of the first
// key until it has handled the others, so the list must not meet them again.
//
// Parameters:
//   - t: test to report to
func TestTypedAhead(t *testing.T) {
	for _, c := range []struct {
		name  string
		keys  string
		check func(t *testing.T, g *rig)
	}{
		{"n then a letter", "nm", func(t *testing.T, g *rig) {
			f, ok := g.top().(*form)
			if !ok || f.texts()[0] != "m" {
				t.Errorf("top %T:\n%s", g.top(), g.text())
			}
		}},
		{"n then q", "nq", func(t *testing.T, g *rig) {
			if g.Quit || g.top() == nil {
				t.Errorf("quit %v, top %T: q reached the list", g.Quit, g.top())
			}
		}},
		{"l then l", "ll", func(t *testing.T, g *rig) {
			if l, ok := g.r.scr.(*listScreen); !ok || l.kind != kindAlias {
				t.Errorf("screen %T: two l must move two kinds", g.r.scr)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))

			var late []tea.Cmd

			for _, r := range c.keys {
				_, cmd := g.r.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
				late = append(late, cmd)
			}

			for _, cmd := range late {
				if cmd != nil {
					g.send(cmd())
				}
			}

			c.check(t, g)
		})
	}
}
