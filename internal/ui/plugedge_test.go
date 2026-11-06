package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/plugin"
)

// TestPluginsToggleKeepsTheDirection checks that the space key does what the footer said when the
// state of the plugin changed meanwhile: the plugin is left alone, the list is read again and the
// user is told.
//
// Parameters:
//   - t: test to report to
func TestPluginsToggleKeepsTheDirection(t *testing.T) {
	g, l := plugRig(t)
	g.pick("demo")

	if st := g.screen().selected().state; st != "enabled" {
		t.Fatalf("demo shows %s, want enabled", st)
	}

	if _, err := l.reg.Disable("demo"); err != nil {
		t.Fatal(err)
	}

	g.press("space")

	if st := l.states()["demo"]; st != "disabled" {
		t.Errorf("demo is %s, the space key turned it on although the list said enabled", st)
	}

	if !strings.Contains(g.toastText(), "changed since the list was read") {
		t.Errorf("toast = %q", g.toastText())
	}

	if st := g.screen().selected().state; st != "disabled" {
		t.Errorf("the list still shows %s", st)
	}

	g.press("space")

	if st := l.states()["demo"]; st != "enabled" {
		t.Errorf("demo is %s after the second space, want enabled", st)
	}
}

// TestPluginsClickEdges checks the clicks of the plugins screen that fall outside the rows, and
// that a double click leaves the filter.
//
// Parameters:
//   - t: test to report to
func TestPluginsClickEdges(t *testing.T) {
	g, _ := plugRig(t)
	e := g.r.e
	s := g.screen()

	before := s.sel

	if _, cmd := s.Update(e, itemClickMsg{ID: len(s.shown) + 3}); cmd != nil || s.sel != before {
		t.Errorf("a click below the last row moved the cursor to %d or gave a command", s.sel)
	}

	if _, cmd := s.Update(e, itemClickMsg{ID: -1}); cmd != nil || s.sel != before {
		t.Errorf("a click above the first row moved the cursor to %d", s.sel)
	}

	g.press("/")

	if !s.flt.typing {
		t.Fatal("/ did not start the filter")
	}

	if _, cmd := s.Update(e, itemClickMsg{ID: 1, Double: true}); cmd == nil || s.flt.typing {
		t.Errorf("a double click: typing %v, command %v", s.flt.typing, cmd != nil)
	}
}

// TestPluginsDetailsWarn checks that the details of a plugin tell the values of its settings file
// that were ignored, with the value, and the keys it does not know.
//
// Parameters:
//   - t: test to report to
func TestPluginsDetailsWarn(t *testing.T) {
	g, l := plugRig(t)
	file, _ := l.sb.Opts.Paths.PluginSettingsFile("demo")
	writeFile(t, file, "schema = 1\nlimit = 99\nnope = 1\n")
	g.pick("demo")
	g.press("enter")

	flat := strings.Join(strings.Fields(g.text()), " ")

	for _, want := range []string{
		"warnings", "setting limit from the file was ignored",
		"(value: 99)", "nope",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the details lack %q:\n%s", want, g.text())
		}
	}
}

// TestSettingsListRow checks that a list setting shows how many items it holds, and that the
// footer tells that Enter opens the editor while the list has the keys.
//
// Parameters:
//   - t: test to report to
func TestSettingsListRow(t *testing.T) {
	g, _, f := settingsRig(t, nil)

	if !strings.Contains(g.text(), "items: 1 (x)") {
		t.Errorf("the list row does not show the count:\n%s", g.text())
	}

	if strings.Contains(g.footerText(), "edit") {
		t.Errorf("the footer offers to edit a list on a text field: %q", g.footerText())
	}

	g.press("down", "down", "down", "down")

	if f.fields[f.cur].Type != fieldList || !strings.Contains(g.footerText(), "edit") {
		t.Errorf("footer on the list = %q", g.footerText())
	}
}

// TestPluginsQuitCancelsTheJob checks that ending the interface gives up the job that is running:
// its context ends, so that a copy or a read does not go on in the background.
//
// Parameters:
//   - t: test to report to
func TestPluginsQuitCancelsTheJob(t *testing.T) {
	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.fixtures()

	host := &slowHost{PluginHost: l.reg, gate: make(chan struct{})}
	sb.Opts.Plugins = host
	sb.Opts.Start = StartPlugins

	g := newRig(t, sb)
	g.pick("gft")
	g.press("i")

	if g.r.job == nil {
		t.Fatal("the job did not start")
	}

	g.r.closeOverlays()

	g.await("the job to see the end of its context", func() bool {
		host.mu.Lock()
		defer host.mu.Unlock()

		return host.ended != nil
	})
}

// TestPluginsCommitIsAWrite checks that the commit of an install counts as a write while it runs,
// so that quitting asks first, and that a second action is not started meanwhile.
//
// Parameters:
//   - t: test to report to
func TestPluginsCommitIsAWrite(t *testing.T) {
	g, l := plugRig(t)

	dir := l.source("fresh", map[string]string{"plugin.toml": plugManifest("fresh", "Fresh.", "")})

	st, err := l.reg.Plan(context.Background(), plugin.InstallRequest{Source: dir})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	cmd := g.r.granted(pluginGrantMsg{ID: "fresh", Staged: st, Token: ""})
	if cmd == nil || g.r.job == nil || !g.r.job.write || g.r.job.stop {
		t.Fatalf("job = %+v, want a write that cannot be given up", g.r.job)
	}

	if !g.r.e.pendingWrites() {
		t.Error("the commit is not counted as a write")
	}

	if again := g.r.doPlugin(pluginActMsg{Act: actInstall, ID: "gft"}); again != nil {
		t.Error("an action started while a job runs")
	}

	for _, m := range collectLate(cmd, g.late) {
		g.send(m)
	}

	g.await("the commit to end", func() bool { return g.r.job == nil })

	if _, ok := l.states()["fresh"]; !ok {
		t.Errorf("fresh was not installed (toast %q)", g.toastText())
	}
}
