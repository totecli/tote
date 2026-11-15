package ui

import (
	"strings"
	"testing"
)

// TestActionMenuUnavailableKeepsPlugin checks that an action that cannot run keeps its plugin as
// the note and gives its reason once, on the status line, while selected, and that Enter and a
// double click on it start nothing.
//
// Parameters:
//   - t: test to report to
func TestActionMenuUnavailableKeepsPlugin(t *testing.T) {
	g, _, log := gftRig(t)
	m := g.repoActions()

	const reason = "has no GitHub remote"

	if !m.items[0].off || m.items[0].note != "gft" || m.why[0] == "" {
		t.Fatalf("the first action: off %v note %q why %q", m.items[0].off, m.items[0].note,
			m.why[0])
	}

	if m.items[3].off || m.items[3].note != "gft" {
		t.Errorf("the stash action: off %v note %q", m.items[3].off, m.items[3].note)
	}

	text := g.text()
	if n := strings.Count(text, reason); n != 1 {
		t.Fatalf("the reason shows %d times, want once on the status line:\n%s", n, text)
	}

	status := strings.Split(text, "\n")
	for _, l := range status {
		if strings.Contains(l, "Show the latest release") && strings.Contains(l, reason) {
			t.Errorf("the reason is in the row: %q", l)
		}
	}

	if m.Status(g.r.e).Command != m.why[0] {
		t.Errorf("status %q, want %q", m.Status(g.r.e).Command, m.why[0])
	}

	g.press("down", "down", "down")

	if strings.Contains(g.text(), reason) || m.Status(g.r.e).Command != "" {
		t.Errorf("the reason stays on a row that can run:\n%s", g.text())
	}

	g.press("home", "enter")

	if _, ok := g.top().(*actionMenu); !ok || !strings.Contains(g.toastText(), reason) {
		t.Errorf("Enter on the greyed row: top %T, toast %q", g.top(), g.toastText())
	}

	g.press("down", "up")

	y, x := lineOf(g, "List the release assets")
	g.click(x, y)
	g.click(x, y)

	if m.cur != 1 || !strings.Contains(g.toastText(), reason) || len(readLog(t, log)) != 0 {
		t.Errorf("double click: cur %d toast %q log %q", m.cur, g.toastText(), readLog(t, log))
	}
}
