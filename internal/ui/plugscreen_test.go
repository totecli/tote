package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// plugRig builds a rig on the plugins screen over the fixtures and plays some keys.
//
// Parameters:
//   - t: running test
//   - keys: key names to press first
//
// Returns the rig and the lab that owns the registry.
func plugRig(t *testing.T, keys ...string) (*rig, *plugLab) {
	t.Helper()

	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.fixtures()

	sb.Opts.Start = StartPlugins
	g := newRig(t, sb)
	g.press(keys...)

	return g, l
}

// screen returns the plugins screen.
func (g *rig) screen() *pluginsScreen {
	g.t.Helper()

	s, ok := g.r.scr.(*pluginsScreen)
	if !ok {
		g.t.Fatalf("the screen is %T, not the plugins screen", g.r.scr)
	}

	return s
}

// pick moves the cursor of the plugins screen to a plugin.
//
// Parameters:
//   - id: plugin id
func (g *rig) pick(id string) {
	g.t.Helper()
	g.press("home")

	for range 20 {
		if r := g.screen().selected(); r != nil && r.id == id {
			return
		}

		g.press("down")
	}

	g.t.Fatalf("plugin %s is not in the list:\n%s", id, g.text())
}

// TestPlugLabStates checks that the fixtures give the states the frames and the tests rely on.
//
// Parameters:
//   - t: test to report to
func TestPlugLabStates(t *testing.T) {
	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.fixtures()

	want := map[string]string{
		"demo": "enabled", "notes": "disabled", "pager": "needs-consent",
		"jumpy": "quarantined", "ghost": "missing-dependency", "torn": "broken",
	}

	if got := l.states(); !reflect.DeepEqual(got, want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
}

// TestPluginsRows checks the row of each plugin: the id, the version and the state as a word,
// with the bullet, and the panel of the plugin under the cursor.
//
// Parameters:
//   - t: test to report to
func TestPluginsRows(t *testing.T) {
	tests := []struct {
		id    string
		state string
		panel string
	}{
		{"demo", "enabled", "Shows notes about the work."},
		{"gft", "available", "not installed, press i to install it"},
		{"ghost", "missing dependency", "ghost-tool"},
		{"jumpy", "quarantined", "Jumps between places."},
		{"notes", "disabled", "Keeps short notes."},
		{"pager", "consent needed", "Reads long output page by page."},
		{"torn", "broken", ""},
	}

	g, _ := plugRig(t)

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			g.t = t
			g.pick(tt.id)

			var row string

			for _, l := range strings.Split(g.text(), "\n") {
				if strings.Contains(l, " "+tt.id+"  ") && strings.Contains(l, "1.0.0") {
					row = l

					break
				}
			}

			if !strings.Contains(row, "\u25cf "+tt.state) {
				t.Errorf("row of %s = %q, want the state %q with its bullet", tt.id, row, tt.state)
			}

			if !strings.Contains(g.text(), tt.panel) {
				t.Errorf("panel of %s lacks %q:\n%s", tt.id, tt.panel, g.text())
			}
		})
	}
}

// TestPluginsFooterByState checks that the footer offers the keys that apply to the plugin under
// the cursor and none that would do nothing.
//
// Parameters:
//   - t: test to report to
func TestPluginsFooterByState(t *testing.T) {
	tests := []struct {
		id     string
		want   []string
		absent []string
	}{
		{
			"demo",
			[]string{"space disable", "s settings", "d remove", "c check", "tab actions"},
			[]string{"a allow", "i install"},
		},
		{"notes", []string{"space enable", "d remove"}, []string{"s settings", "a allow"}},
		{"pager", []string{"space enable", "a allow"}, []string{"i install"}},
		{"gft", []string{"i install", "c check"}, []string{"space", "d remove", "a allow"}},
		{"torn", []string{"d remove", "c check"}, []string{"space"}},
	}

	g, _ := plugRig(t)
	g.resize(140, 30)

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			g.t = t
			g.pick(tt.id)

			footer := g.footerText()

			for _, w := range tt.want {
				if !strings.Contains(footer, w) {
					t.Errorf("footer of %s lacks %q: %q", tt.id, w, footer)
				}
			}

			for _, a := range tt.absent {
				if strings.Contains(footer, a) {
					t.Errorf("footer of %s shows %q: %q", tt.id, a, footer)
				}
			}
		})
	}
}

// TestPluginsFilter checks that the filter keeps the plugins whose id, title, description or
// state matches, and that Esc clears it before it leaves the screen.
//
// Parameters:
//   - t: test to report to
func TestPluginsFilter(t *testing.T) {
	g, _ := plugRig(t)

	g.press("/")
	g.typeText("quar")
	g.press("enter")

	if rows := g.screen().shown; len(rows) != 1 || g.screen().rows[rows[0]].id != "jumpy" {
		t.Fatalf("shown = %v after the filter quar", rows)
	}

	g.press("esc")

	if g.screen().flt.String() != "" || len(g.screen().shown) != 7 {
		t.Errorf("Esc did not clear the filter: %q, %d rows", g.screen().flt.String(),
			len(g.screen().shown))
	}

	g.press("esc")

	if _, ok := g.r.scr.(*homeScreen); !ok {
		t.Errorf("the second Esc went to %T, want the home page", g.r.scr)
	}

	if h := g.r.scr.(*homeScreen); h.sel != 4 {
		t.Errorf("the home page selects entry %d, want the plugins (4)", h.sel)
	}
}

// TestPluginsHomeEntries checks the plugins row of the welcome page: it exists only when the
// host is set, the digits move by one, and the row opens the screen.
//
// Parameters:
//   - t: test to report to
func TestPluginsHomeEntries(t *testing.T) {
	t.Run("without the host", func(t *testing.T) {
		g := newRig(t, newSandbox(t))

		if got := g.text(); strings.Contains(got, "Plugins") {
			t.Errorf("the home page shows the plugins without a host:\n%s", got)
		}

		g.press("5")

		if _, ok := g.r.scr.(*homeScreen); !ok {
			t.Errorf("5 opened %T, want the welcome page: Settings has no digit", g.r.scr)
		}

		g.press(",")

		if _, ok := g.r.scr.(*configScreen); !ok {
			t.Errorf("the comma key opened %T, want the settings", g.r.scr)
		}
	})

	t.Run("with the host", func(t *testing.T) {
		sb := newSandbox(t)
		newPlugLab(t, sb).fixtures()

		g := newRig(t, sb)

		if got := g.text(); !strings.Contains(got, "Plugins") || !strings.Contains(got, "! 6") {
			t.Errorf("the home page lacks the plugins row with its count and mark:\n%s", got)
		}

		if got := g.footerText(); !strings.Contains(got, "1-5 jump") {
			t.Errorf("footer = %q, want the digits 1-5", got)
		}

		g.press("5")

		if _, ok := g.r.scr.(*pluginsScreen); !ok {
			t.Fatalf("5 opened %T, want the plugins", g.r.scr)
		}

		g.press("esc")
		g.press("6")

		if _, ok := g.r.scr.(*homeScreen); !ok {
			t.Errorf("6 opened %T, want the welcome page to stay: Settings has no digit", g.r.scr)
		}

		g.press(",")

		if _, ok := g.r.scr.(*configScreen); !ok {
			t.Errorf("the comma key opened %T, want the settings", g.r.scr)
		}

		g.press("esc")

		if h := g.r.scr.(*homeScreen); h.sel != 5 {
			t.Errorf("the home page selects entry %d after the settings, want 5", h.sel)
		}
	})
}

// TestPluginsPalette checks the palette action that opens the plugins, which exists only with the
// host.
//
// Parameters:
//   - t: test to report to
func TestPluginsPalette(t *testing.T) {
	g := newRig(t, newSandbox(t))
	g.press("ctrl+k")
	g.typeText("plugins")

	if strings.Contains(g.text(), "Go to\u2026 Plugins") {
		t.Errorf("the palette offers the plugins without a host:\n%s", g.text())
	}

	sb := newSandbox(t)
	newPlugLab(t, sb).fixtures()

	g = newRig(t, sb)
	g.press("ctrl+k")
	g.typeText("plugins")

	if !strings.Contains(g.text(), "Go to\u2026 Plugins") {
		t.Fatalf("the palette lacks the plugins action:\n%s", g.text())
	}

	g.press("enter")

	if _, ok := g.r.scr.(*pluginsScreen); !ok {
		t.Errorf("the action opened %T", g.r.scr)
	}
}

// TestPluginsSessionOff checks the banner of a run where the plugins are off.
//
// Parameters:
//   - t: test to report to
func TestPluginsSessionOff(t *testing.T) {
	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.fixtures()

	sb.Opts.Plugins = l.open(true)
	sb.Opts.Start = StartPlugins

	g := newRig(t, sb)

	if !g.screen().session {
		t.Errorf("the screen does not know that the plugins are off for this run")
	}
}

// TestPluginsMouse checks the mouse on the screen: a click selects a row, a double click opens
// its details, and the wheel moves the cursor.
//
// Parameters:
//   - t: test to report to
func TestPluginsMouse(t *testing.T) {
	g, _ := plugRig(t)
	g.resize(100, 30)

	pos := g.rowAt("notes")
	g.click(pos.x, pos.y)

	if r := g.screen().selected(); r == nil || r.id != "notes" {
		t.Fatalf("the click selected %+v, want notes", r)
	}

	g.click(pos.x, pos.y)

	if _, ok := g.top().(*infoOverlay); !ok {
		t.Errorf("the second click opened %T, want the details", g.top())
	}

	g.press("esc")
	g.resize(60, 8)
	g.press("home")

	before := *g.screen()

	g.send(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})

	if after := g.screen(); after.sel == before.sel && after.off == before.off {
		t.Errorf("the wheel did not move the list: cursor %d, offset %d", after.sel, after.off)
	}
}

// rowAt finds the cell of the first letter of a plugin id on the screen.
//
// Parameters:
//   - id: plugin id
func (g *rig) rowAt(id string) cellPos {
	g.t.Helper()

	for y, l := range g.view() {
		plain := stripStyles(l)

		if i := strings.Index(plain, " "+id+"  "); i >= 0 && strings.Contains(plain, "1.0.0") {
			return cellPos{x: i + 1, y: y}
		}
	}

	g.t.Fatalf("%s is not on the screen:\n%s", id, g.text())

	return cellPos{}
}
