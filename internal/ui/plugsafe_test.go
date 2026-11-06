package ui

import (
	"context"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// holdHost is a plugin host whose writes wait for a signal, to show a job that is still running.
type holdHost struct {
	PluginHost
	gate chan struct{}
}

// SaveSettings implements PluginHost: it waits for the gate.
//
// Parameters:
//   - p: the plugin
//   - values: the values to write
func (h *holdHost) SaveSettings(p plugin.Plugin, values map[string]any) error {
	<-h.gate

	return h.PluginHost.SaveSettings(p, values)
}

// Disable implements PluginHost: it waits for the gate.
//
// Parameters:
//   - id: plugin id
func (h *holdHost) Disable(id string) (plugin.DisableResult, error) {
	<-h.gate

	return h.PluginHost.Disable(id)
}

// Remove implements PluginHost: it waits for the gate.
//
// Parameters:
//   - id: plugin id
//   - o: what to remove
func (h *holdHost) Remove(id string, o plugin.RemoveOptions) (plugin.RemoveResult, error) {
	<-h.gate

	return h.PluginHost.Remove(id, o)
}

// holdRig opens the plugins screen over a host whose writes wait.
//
// Parameters:
//   - t: running test
//
// Returns the rig, the lab and the host.
func holdRig(t *testing.T) (*rig, *plugLab, *holdHost) {
	t.Helper()

	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.fixtures()

	h := &holdHost{PluginHost: l.reg, gate: make(chan struct{})}
	sb.Opts.Plugins = h
	sb.Opts.Start = StartPlugins

	return newRig(t, sb), l, h
}

// TestConsentTokenBinding checks that the consent of an installed plugin is bound to what the
// user saw: a plugin that asks for more while the overlay is open is not allowed unseen.
//
// Parameters:
//   - t: test to report to
func TestConsentTokenBinding(t *testing.T) {
	g, l := plugRig(t)

	g.pick("pager")
	g.press("a")

	if _, ok := g.top().(*consentOverlay); !ok {
		t.Fatalf("a opened %T", g.top())
	}

	wider := strings.Replace(plugManifest("pager", "Reads long output page by page.", ""),
		`permissions = ["exec:tool"]`, `permissions = ["exec:tool", "exec:other"]`, 1)
	wider = strings.Replace(wider, `version = "1.0.0"`, `version = "1.1.0"`, 1)
	dir := l.source("pagerwide", map[string]string{"plugin.toml": wider})

	st, err := l.reg.Plan(context.Background(), plugin.InstallRequest{
		Source: dir, Update: true,
		Force: true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if _, err := st.Commit(plugin.Grant{Prompt: true}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	l.edit("pager", func(r *plugin.LockRecord) { r.ConsentedAt, r.ConsentedBy = "", "" })

	g.press("tab", "enter")

	if st := l.states()["pager"]; st == "enabled" || st == "disabled" {
		t.Errorf("pager is %s after a consent to something else", st)
	}

	if !g.r.toast.err {
		t.Errorf("toast = %q, want the refusal", g.toastText())
	}
}

// TestSettingsSaveHeld checks that a save that runs is a pending write, and that the form stays
// until the save ends.
//
// Parameters:
//   - t: test to report to
func TestSettingsSaveHeld(t *testing.T) {
	g, l, h := holdRig(t)

	g.pick("demo")
	g.press("s", "backspace", "ctrl+s")

	f, ok := g.top().(*fieldsForm)
	if !ok || !f.saving {
		t.Fatalf("top = %T, saving %v", g.top(), ok && f.saving)
	}

	if !g.r.e.pendingWrites() {
		t.Errorf("a save that runs is not a pending write")
	}

	g.send(dismissMsg{})

	if g.top() != f {
		t.Errorf("the form went away while it saved: %T", g.top())
	}

	close(h.gate)
	g.await("the save to end", func() bool { return g.top() == nil && !g.r.e.pendingWrites() })

	if _, err := os.Stat(l.settingsFile()); err != nil {
		t.Errorf("the settings were not written: %v", err)
	}
}

// TestPluginsWritesAreCounted checks that a disable and a removal count as writes while they run.
//
// Parameters:
//   - t: test to report to
func TestPluginsWritesAreCounted(t *testing.T) {
	tests := []struct {
		name string
		do   func(g *rig)
	}{
		{"disable", func(g *rig) { g.pick("demo"); g.press("space") }},
		{"remove", func(g *rig) { g.pick("notes"); g.press("d", "y") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _, h := holdRig(t)
			tt.do(g)

			if _, ok := g.top().(*busyOverlay); !ok {
				t.Fatalf("top = %T, want the wait box", g.top())
			}

			if !g.r.e.pendingWrites() {
				t.Errorf("the job is not counted as a write")
			}

			close(h.gate)
			g.await("the job to end", func() bool { return g.top() == nil })

			if g.r.e.pendingWrites() {
				t.Errorf("the job is still counted after its end")
			}
		})
	}
}

// TestPluginsRemoveKeepsData checks that a removal without the data leaves the settings file.
//
// Parameters:
//   - t: test to report to
func TestPluginsRemoveKeepsData(t *testing.T) {
	g, l := plugRig(t)
	writeFile(t, l.settingsFile(), "schema = 1\nlimit = 20\n")

	g.pick("demo")
	g.press("d", "y")

	if _, ok := l.states()["demo"]; ok {
		t.Fatalf("demo is still installed (toast %q)", g.toastText())
	}

	if _, err := os.Stat(l.settingsFile()); err != nil {
		t.Errorf("a removal without the data deleted the settings: %v", err)
	}
}

// TestPluginsToggleMissingDependency checks that space turns a plugin off when it is on but lacks
// a program.
//
// Parameters:
//   - t: test to report to
func TestPluginsToggleMissingDependency(t *testing.T) {
	g, l := plugRig(t)

	if st := l.states()["ghost"]; st != "missing-dependency" {
		t.Fatalf("ghost is %s", st)
	}

	g.pick("ghost")
	g.press("space")

	if st := l.states()["ghost"]; st != "disabled" {
		t.Errorf("ghost is %s after space, want disabled (toast %q)", st, g.toastText())
	}
}

// TestPluginsNoSelection checks that every key of the screen is harmless when the filter shows no
// plugin.
//
// Parameters:
//   - t: test to report to
func TestPluginsNoSelection(t *testing.T) {
	g, l := plugRig(t, "/", "type:zzz", "enter")
	before := l.states()

	g.press("space", "a", "s", "i", "c", "d", "tab", "enter")

	if g.top() != nil {
		t.Errorf("a key opened %T on an empty list", g.top())
	}

	if got := l.states(); len(got) != len(before) {
		t.Errorf("states = %v, were %v", got, before)
	}
}

// TestPluginsKeyActions checks what the letter keys of the screen ask for.
//
// Parameters:
//   - t: test to report to
func TestPluginsKeyActions(t *testing.T) {
	g, _ := plugRig(t)

	g.pick("demo")
	g.press("d")

	a, ok := g.top().(*ask)
	if !ok {
		t.Fatalf("d opened %T", g.top())
	}

	if m, ok := a.yes.(pluginRemoveMsg); !ok || m.Purge {
		t.Errorf("d asks for %+v, want a removal that keeps the data", a.yes)
	}

	g.press("esc", "a")

	if g.top() != nil {
		t.Errorf("a on a plugin that is allowed opened %T", g.top())
	}

	if st := g.screen().selected(); st == nil || st.state != "enabled" {
		t.Errorf("a changed the plugin: %+v", st)
	}
}

// TestPluginsPasteOutsideFilter checks that a paste does not reach the filter when it is not being
// typed.
//
// Parameters:
//   - t: test to report to
func TestPluginsPasteOutsideFilter(t *testing.T) {
	g, _ := plugRig(t)
	n := len(g.screen().shown)

	g.send(tea.PasteMsg{Content: "zzz"})

	if got := len(g.screen().shown); got != n {
		t.Errorf("a paste changed the list from %d to %d plugins", n, got)
	}
}

// TestConsentDismissAborts checks that a consent that holds a staged package deletes it when the
// interface dismisses the overlay.
//
// Parameters:
//   - t: test to report to
func TestConsentDismissAborts(t *testing.T) {
	g, l := plugRig(t)

	g.pick("gft")
	g.press("i")

	c, ok := g.top().(*consentOverlay)
	if !ok {
		t.Fatalf("i opened %T:\n%s", g.top(), g.text())
	}

	if stagingLeft(t, l) == 0 {
		t.Fatal("nothing is staged while the consent is open")
	}

	if _, _, done := c.Update(g.r.e, dismissMsg{}); !done {
		t.Error("dismissMsg did not close the consent")
	}

	if n := stagingLeft(t, l); n != 0 {
		t.Errorf("%d packages are still staged after the dismissal", n)
	}
}

// TestPluginsLateAnswer checks that the answer of a job that was given up is dropped even when
// another job runs, and that the package it staged is deleted.
//
// Parameters:
//   - t: test to report to
func TestPluginsLateAnswer(t *testing.T) {
	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.fixtures()

	host := &slowHost{PluginHost: l.reg, gate: make(chan struct{})}
	sb.Opts.Plugins = host
	sb.Opts.Start = StartPlugins

	g := newRig(t, sb)
	g.pick("gft")
	g.press("i")

	first := g.r.job.seq

	g.press("esc")
	g.press("i")

	if g.r.job == nil || g.r.job.seq == first {
		t.Fatalf("the second job did not start: %+v", g.r.job)
	}

	dir := l.source("late", map[string]string{"plugin.toml": plugManifest("late", "Late.", "")})

	st, err := l.reg.Plan(context.Background(), plugin.InstallRequest{Source: dir})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	second := g.r.job.seq

	g.send(pluginDoneMsg{Seq: first, Op: jobPlan, ID: "late", Staged: st})

	if g.r.job == nil || g.r.job.seq != second {
		t.Errorf("a late answer ended the job that runs: %+v", g.r.job)
	}

	if _, ok := g.top().(*consentOverlay); ok {
		t.Errorf("a late answer opened the consent")
	}

	if n := stagingLeft(t, l); n != 0 {
		t.Errorf("%d packages are still staged after the late answer", n)
	}

	close(host.gate)
	g.await("the job to end", func() bool { return g.r.job == nil })
}

// loadCounter is a plugin host that counts the reads of every plugin.
type loadCounter struct {
	PluginHost
	loads int
}

// Load implements PluginHost: it counts the call.
func (h *loadCounter) Load() plugin.Snapshot {
	h.loads++

	return h.PluginHost.Load()
}

// TestHomeCountIsNotReadByAFrame checks that drawing the welcome page reads no plugin: the count
// comes from a command, and a change makes the next command read it again.
//
// Parameters:
//   - t: test to report to
func TestHomeCountIsNotReadByAFrame(t *testing.T) {
	sb := newSandbox(t)
	newPlugLab(t, sb).fixtures()

	h := &loadCounter{PluginHost: sb.Opts.Plugins}
	sb.Opts.Plugins = h

	g := newRig(t, sb)
	g.await("the count", func() bool { _, known := g.r.e.Plug.summary(); return known })

	if h.loads == 0 {
		t.Fatal("the count was never read")
	}

	g.r.e.pluginsChanged()

	before := h.loads

	for range 3 {
		_ = g.r.render()
	}

	if h.loads != before {
		t.Errorf("a frame read the plugins: %d reads, were %d", h.loads, before)
	}

	if !strings.Contains(g.text(), "Plugins") {
		t.Errorf("the plugins row is gone:\n%s", g.text())
	}

	g.send(tea.KeyPressMsg{Code: 'x', Text: "x"})
	g.await("the count again", func() bool { _, known := g.r.e.Plug.summary(); return known })

	if h.loads != before+1 {
		t.Errorf("the count was read %d times after a change, want 1", h.loads-before)
	}
}

// TestPluginMenuUpdate checks that Update is offered for a plugin that has a source to read again
// and not for a release.
//
// Parameters:
//   - t: test to report to
func TestPluginMenuUpdate(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{plugin.KindLocal, true},
		{plugin.KindBundled, true},
		{plugin.KindRelease, false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := false

			installedMenu(plugin.Plugin{
				State:  plugin.StateEnabled,
				Record: plugin.LockRecord{Kind: tt.kind},
			}, func(a pluginAct, _ string) {
				got = got || a == actUpdate
			})

			if got != tt.want {
				t.Errorf("update offered = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPaletteOpensPluginsAfterFunctions checks the place of the plugins action in the palette.
//
// Parameters:
//   - t: test to report to
func TestPaletteOpensPluginsAfterFunctions(t *testing.T) {
	sb := newSandbox(t)
	newPlugLab(t, sb)

	g := newRig(t, sb)

	var labels []string

	for _, en := range buildEntries(g.r.e) {
		if en.Scope == scopeAction {
			labels = append(labels, en.Name)
		}
	}

	at := slices.Index(labels, g.r.e.t("ui_pal_act_open_funcs"))
	if at < 0 || at+1 >= len(labels) || labels[at+1] != g.r.e.t("ui_pal_act_open_plugins") {
		t.Errorf("actions = %q, want the plugins right after the functions", labels)
	}
}

// TestPluginErrorsForTheInterface checks the texts of the errors that tell to force: the
// interface has no such option, and a source that cannot be read says so.
//
// Parameters:
//   - t: test to report to
func TestPluginErrorsForTheInterface(t *testing.T) {
	g, _ := plugRig(t)
	e := g.r.e

	for _, code := range []string{
		plugin.CodeDowngrade, plugin.CodePrerelease,
		plugin.CodeSourceChanged,
	} {
		got := e.codeText(code, []string{"a", "b", "c"}, nil)
		if !strings.Contains(got, "command line") {
			t.Errorf("%s = %q, want a pointer to the command line", code, got)
		}
	}

	got := e.codeText(plugin.CodeBadSource, []string{"/srv/x"}, fs.ErrPermission)
	if !strings.Contains(got, "permission denied") || strings.Contains(got, "release URL") {
		t.Errorf("unreadable source = %q", got)
	}
}

// TestSettingsRejectionsSayTheBound checks that the range and the number of items name their limit.
//
// Parameters:
//   - t: test to report to
func TestSettingsRejectionsSayTheBound(t *testing.T) {
	g, l, _ := settingsRig(t, nil)
	p, _ := l.reg.Recheck("demo")

	by := map[string]plugin.Setting{}
	for _, s := range p.Manifest.Settings {
		by[s.ID] = s
	}

	tests := []struct {
		id, code, want string
	}{
		{"limit", plugin.RejectRange, "must be between 1 and 50"},
		{"tags", plugin.RejectItems, "at most 3 items"},
		{"owner", plugin.RejectPattern, "does not match the pattern"},
		{"owner", "", ""},
	}

	for _, tt := range tests {
		if got := rejectText(g.r.e, by[tt.id], tt.code); got != tt.want {
			t.Errorf("%s %q = %q, want %q", tt.id, tt.code, got, tt.want)
		}
	}
}

// TestSettingsRefusedSaveIsSaid checks that a save the registry refuses is told as not saved.
//
// Parameters:
//   - t: test to report to
func TestSettingsRefusedSaveIsSaid(t *testing.T) {
	g, _, f := settingsRig(t, func(l *plugLab) {
		writeFile(t, l.settingsFile(), "schema = 1\nlimit = = 3\n")
	})

	g.press("backspace", "ctrl+s")
	g.await("the save to end", func() bool { return !f.saving })

	if !strings.HasPrefix(f.msg, "Not saved:") {
		t.Errorf("message = %q, want the refusal", f.msg)
	}

	if g.top() != f {
		t.Errorf("the form closed on a refused save: %T", g.top())
	}
}

// TestRemovePurgeQuestionIsWhole checks that the list of what a removal with data deletes is
// shown in full.
//
// Parameters:
//   - t: test to report to
func TestRemovePurgeQuestionIsWhole(t *testing.T) {
	g, _ := plugRig(t)
	g.resize(80, 24)
	g.pick("notes")
	g.send(pluginActMsg{ID: "notes", Act: actPurge})

	flat := strings.Join(strings.Fields(strings.NewReplacer("\u2502", " ").Replace(g.text())), " ")
	if !strings.Contains(flat, "its table in config.toml") {
		t.Errorf("the list of what goes is cut:\n%s", g.text())
	}
}

// TestPanelTitleIsNotRepeated checks that the border of the panel shows the title of a plugin
// only when it adds something to the id.
//
// Parameters:
//   - t: test to report to
func TestPanelTitleIsNotRepeated(t *testing.T) {
	g, _ := plugRig(t)
	g.resize(100, 30)
	g.pick("gft")

	if got := g.text(); strings.Contains(got, "gft gft") || !strings.Contains(got, " gft ") {
		t.Errorf("panel of gft:\n%s", got)
	}
}
