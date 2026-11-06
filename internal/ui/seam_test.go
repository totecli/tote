package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSeamCommands checks the commands that overlays use to report a write.
//
// Parameters:
//   - t: test to report to
func TestSeamCommands(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	if m, ok := wroteCmd(kindAlias, "ll", opEdit)().(wroteMsg); !ok || m.Key != "ll" ||
		m.Op != opEdit || m.Kind != kindAlias {
		t.Errorf("wroteCmd gave %+v", m)
	}

	if m, ok := removeCmd(e, kindAlias, "ll")().(wroteMsg); !ok || m.Op != opDelete {
		t.Errorf("removeCmd gave %+v", m)
	}

	// A record that is gone refreshes the list besides telling why.
	batch, _ := removeCmd(e, kindAlias, "ll")().(tea.BatchMsg)

	var toasted, refreshed bool

	for _, c := range batch {
		switch m := c().(type) {
		case toastMsg:
			toasted = m.Err && strings.Contains(m.Text, "not found")
		case refreshMsg:
			refreshed = m.Kind == kindAlias
		}
	}

	if !toasted || !refreshed {
		t.Errorf("removing a missing record: toast %v, refresh %v", toasted, refreshed)
	}

	if m, ok := toastCmd("hi", true)().(toastMsg); !ok || m.Text != "hi" || !m.Err {
		t.Errorf("toastCmd gave %+v", m)
	}
}

// TestHomeReload checks that the welcome page ignores a reload but still shows new counts.
//
// Parameters:
//   - t: test to report to
func TestHomeReload(t *testing.T) {
	s := newSandbox(t)
	g := newRig(t, s)
	h := g.r.scr

	if h.Reload(g.r.e, "x") != h {
		t.Error("Reload replaced the welcome page")
	}

	writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n")
	g.press("ctrl+r")

	if !strings.Contains(g.text(), "reloaded") && g.toastText() == "" {
		t.Error("ctrl+r on the welcome page did nothing")
	}

	if g.r.e.Data.count(kindAlias) != 0 {
		t.Error("the data was not read again")
	}
}

// TestConnectInvalid checks that a record that fails the checks of the store is refused with
// a message and nothing is started.
//
// Parameters:
//   - t: test to report to
func TestConnectInvalid(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.ConnectionsFile(), "schema = 1\n\n[[connection]]\nname = \"bad\"\n"+
		"target = \"-oProxyCommand=x\"\n")

	g := newRig(t, s.with(func(o *Options) { o.Start = StartConnections }))

	if it := g.r.scr.(*listScreen).selected(); it == nil || it.Warn == "" {
		t.Fatalf("the invalid record has no warning: %+v", it)
	}

	g.press("enter")

	if g.r.guard.busy.Load() != 0 || !g.r.toast.err || g.Quit {
		t.Errorf("busy %d, toast %q err %v", g.r.guard.busy.Load(), g.toastText(), g.r.toast.err)
	}
}

// TestSlice checks the cutting of cells out of a styled line under both width rules.
//
// Parameters:
//   - t: test to report to
func TestSlice(t *testing.T) {
	g := newRig(t, newSandbox(t))
	m := g.r.e.Metric
	styled := g.r.e.Theme.Title.Render("hello world")

	tests := []struct {
		from, to int
		want     string
	}{
		{0, 5, "hello"},
		{6, 11, "world"},
		{3, 8, "lo wo"},
		{0, 50, "hello world"},
		{5, 5, ""},
		{7, 3, ""},
	}

	for _, tt := range tests {
		if got := stripStyles(m.slice(styled, tt.from, tt.to)); got != tt.want {
			t.Errorf("slice(%d, %d) = %q, want %q", tt.from, tt.to, got, tt.want)
		}
	}
}

// TestTypedArrows checks that the arrow keys move the cursor while the filter reads text.
//
// Parameters:
//   - t: test to report to
func TestTypedArrows(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	l := g.r.scr.(*listScreen)

	g.press("/", "down", "down", "up")

	if l.sel != 1 || !l.Typing() {
		t.Errorf("sel %d typing %v", l.sel, l.Typing())
	}

	g.press("pgdown")

	if l.sel != 5 {
		t.Errorf("pgdown: sel %d", l.sel)
	}

	g.press("pgup")

	if l.sel != 0 {
		t.Errorf("pgup: sel %d", l.sel)
	}

	g.press("j", "k")

	if l.flt.String() != "jk" {
		t.Errorf("j and k must be typed, not move: %q", l.flt.String())
	}
}
