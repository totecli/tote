package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/store"
)

// TestUserCycle checks the key u: it goes through the stored login, root, the login of the
// person and the logins of the other connections, comes back to the stored one, says each one,
// and changes the command that the status line shows.
//
// Parameters:
//   - t: test to report to
func TestUserCycle(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))

	want := []struct {
		toast string
		cmd   string
	}{
		{"root", "ssh -l root -- prod.example.com"},
		{"tester", "ssh -l tester -- prod.example.com"},
		{"ops", "ssh -l ops -- prod.example.com"},
		{"stored user", "ssh -l deploy -- prod.example.com"},
		{"root", "ssh -l root -- prod.example.com"},
	}

	for i, w := range want {
		g.press("u")

		if !strings.Contains(g.toastText(), w.toast) {
			t.Errorf("press %d: toast %q, want %q", i+1, g.toastText(), w.toast)
		}

		if got := g.text(); !strings.Contains(got, "\u2192 "+w.cmd) {
			t.Errorf("press %d: the command shown is not %q:\n%s", i+1, w.cmd, got)
		}
	}

	// The choice belongs to the connection it was made on and is gone after a reload.
	g.press("down")

	if got := g.text(); strings.Contains(got, "-l root") {
		t.Errorf("the next connection inherited the login:\n%s", got)
	}
}

// TestUserRefused checks that a connection that cannot take another login says so.
//
// Parameters:
//   - t: test to report to
func TestUserRefused(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.ConnectionsFile(), fixtureViaConns)

	g := newRig(t, s.with(func(o *Options) { o.Start = StartConnections }))
	g.press("u")

	if !g.r.toast.err || !strings.Contains(g.toastText(), "cannot take another user") {
		t.Errorf("toast = %q", g.toastText())
	}

	g.press("U")

	if g.top() != nil || !strings.Contains(g.toastText(), "cannot take another user") {
		t.Errorf("U opened %T on a connection that cannot take it", g.top())
	}
}

// TestUserChoicesLimit checks the list of logins: each once, the stored login left out, and no
// more than maxUsers.
//
// Parameters:
//   - t: test to report to
func TestUserChoicesLimit(t *testing.T) {
	s := newSandbox(t)

	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i := range 30 {
		letter := string(rune('a' + i%26))
		second := string(rune('a' + i/26))

		b.WriteString("\n[[connection]]\nname = \"c" + letter + second +
			"\"\ntarget = \"h.example.com\"\nuser = \"u" + letter + "\"\n")
	}

	writeFile(t, s.Opts.Paths.ConnectionsFile(), b.String())

	e := newRig(t, s).r.e
	c := store.Connection{Name: "x", Target: "x.example.com", User: "ua"}
	got := userChoices(e, c)

	if len(got) != maxUsers || got[0] != "" {
		t.Errorf("%d choices, first %q: %q", len(got), got[0], got)
	}

	seen := map[string]bool{}

	for _, u := range got {
		if seen[u] || u == "ua" {
			t.Errorf("login %q is repeated or is the stored one: %q", u, got)
		}

		seen[u] = true
	}

	if got[1] != "root" || got[2] != "tester" {
		t.Errorf("root and the person come first: %q", got)
	}
}

// TestUserPick checks the key U: the list of logins connects on Enter with the one chosen, the
// last entry asks for another one that must be valid, and esc connects to nothing.
//
// Parameters:
//   - t: test to report to
func TestUserPick(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	g.press("U")

	p, ok := g.top().(*pickOverlay)
	if !ok {
		t.Fatalf("U opened %T", g.top())
	}

	first := p.items[0]
	if first.label != "stored user" || first.note != "deploy" || p.items[1].value != "root" {
		t.Errorf("items = %+v", p.items)
	}

	e := g.r.e
	_, cmd, done := p.Update(e, keyMsg("down"))

	if cmd != nil || done {
		t.Fatal("moving in the pick did something")
	}

	_, cmd, done = p.Update(e, keyMsg("enter"))
	if !done || cmd == nil {
		t.Fatal("enter did not choose")
	}

	m, ok := cmd().(activateMsg)
	if !ok || m.User != "root" || m.Key != "prod" || m.Kind != kindConn {
		t.Errorf("chose %+v", m)
	}

	// Esc connects to nothing.
	g.press("esc")
	g.press("U", "esc")

	if g.top() != nil || g.Quit {
		t.Errorf("esc left %T", g.top())
	}

	// Another login.
	g.press("U", "end", "enter")

	pr, ok := g.top().(*promptOverlay)
	if !ok {
		t.Fatalf("the last entry opened %T", g.top())
	}

	g.typeText("bad user")

	if pr.err == "" {
		t.Error("a login with a space was accepted")
	}

	g.press("enter")

	if g.top() == nil {
		t.Error("enter on a refused login closed the prompt")
	}

	for range len("bad user") {
		g.press("backspace")
	}

	g.typeText("admin")

	if pr.err != "" {
		t.Errorf("a plain login was refused: %q", pr.err)
	}

	_, cmd, done = pr.Update(e, keyMsg("enter"))
	if !done {
		t.Fatal("enter did not accept")
	}

	if m, ok := cmd().(activateMsg); !ok || m.User != "admin" {
		t.Errorf("chose %+v", m)
	}
}

// TestUserConnects checks that the login of u and U is the one ssh is started with.
//
// Parameters:
//   - t: test to report to
func TestUserConnects(t *testing.T) {
	run := &fakeRunner{}
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.Runner = run
	}))

	g.r.delay = 0

	next := settle(g, g.r.activate(activateMsg{Kind: kindConn, Key: "prod", User: "root"}))

	for _, m := range collect(next) {
		if strings.Contains(fmt.Sprintf("%T", m), "exec") {
			g.send(playExec(t, func() tea.Msg { return m }, "\n"))
		}
	}

	calls := run.calls()
	if len(calls) != 1 || strings.Join(calls[0].Argv, " ") != "ssh -l root -- prod.example.com" {
		t.Errorf("calls = %+v", calls)
	}
}

// TestDetails checks the key v: it shows every field of a record, with the exact command for a
// connection, closes with v or esc, scrolls, and shows the reason a record cannot be used.
//
// Parameters:
//   - t: test to report to
func TestDetails(t *testing.T) {
	tests := []struct {
		start Start
		keys  []string
		want  []string
	}{
		{StartConnections, []string{"v"}, []string{
			"prod.example.com", "deploy", "Production web",
			"ssh -l deploy -- prod.example.com",
		}},
		{StartConnections, []string{"u", "v"}, []string{"ssh -l root -- prod.example.com", "root"}},
		{StartConnections, []string{"down", "down", "down", "v"}, []string{"old.example.com"}},
		{StartRepos, []string{"v"}, []string{"This tool"}},
		{StartAliases, []string{"down", "v"}, []string{"git status"}},
		{StartFuncs, []string{"v"}, []string{"script"}},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = tt.start
			o.Height = 40
		}))
		g.press(tt.keys...)

		if _, ok := g.top().(*infoOverlay); !ok {
			t.Errorf("start %d %v: opened %T", tt.start, tt.keys, g.top())
			continue
		}

		for _, w := range tt.want {
			if !strings.Contains(g.text(), w) {
				t.Errorf("start %d %v: the details lack %q:\n%s", tt.start, tt.keys, w, g.text())
			}
		}

		g.press("v")

		if g.top() != nil {
			t.Errorf("start %d: v did not close the details", tt.start)
		}

		g.press("v", "esc")

		if g.top() != nil {
			t.Errorf("start %d: esc did not close the details", tt.start)
		}
	}
}
