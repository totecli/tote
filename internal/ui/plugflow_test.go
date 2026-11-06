package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/totecli/tote/internal/plugin"
)

// stagingLeft counts the installs that wait in the staging area of a sandbox.
//
// Parameters:
//   - t: running test
//   - l: the lab
func stagingLeft(t *testing.T, l *plugLab) int {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(l.sb.Opts.Paths.PluginsDir(), ".staging"))
	if err != nil {
		return 0
	}

	return len(entries)
}

// TestPluginsToggle checks the space key: a plugin that is allowed turns on and off at once, one
// that needs the consent of the user asks first and stays as it was when the user says no.
//
// Parameters:
//   - t: test to report to
func TestPluginsToggle(t *testing.T) {
	g, l := plugRig(t)

	g.pick("notes")
	g.press("space")

	if st := l.states()["notes"]; st != "enabled" {
		t.Fatalf("notes is %s after space, want enabled (toast %q)", st, g.toastText())
	}

	if !strings.Contains(g.toastText(), `plugin "notes" enabled`) {
		t.Errorf("toast = %q", g.toastText())
	}

	g.press("space")

	if st := l.states()["notes"]; st != "disabled" {
		t.Errorf("notes is %s after the second space, want disabled", st)
	}

	g.pick("pager")
	g.press("space")

	if _, ok := g.top().(*consentOverlay); !ok {
		t.Fatalf("space on a plugin that needs consent opened %T:\n%s", g.top(), g.text())
	}

	if st := l.states()["pager"]; st != "needs-consent" {
		t.Errorf("pager changed to %s before the answer", st)
	}

	g.press("esc")

	if g.top() != nil || l.states()["pager"] != "needs-consent" {
		t.Errorf("Esc left %T on top and pager %s", g.top(), l.states()["pager"])
	}

	if !strings.Contains(g.toastText(), "nothing changed") {
		t.Errorf("toast = %q, want the cancellation", g.toastText())
	}

	g.press("space", "tab", "enter")

	if st := l.states()["pager"]; st != "enabled" {
		t.Errorf("pager is %s after the consent, want enabled (toast %q)", st, g.toastText())
	}
}

// TestPluginsToggleRefused checks that a plugin that cannot be enabled says why and changes
// nothing.
//
// Parameters:
//   - t: test to report to
func TestPluginsToggleRefused(t *testing.T) {
	g, l := plugRig(t)

	g.pick("torn")
	g.press("space")

	if g.top() != nil {
		t.Errorf("space on a broken plugin opened %T", g.top())
	}

	if st := l.states()["torn"]; st != "broken" {
		t.Errorf("torn is %s", st)
	}

	if g.toastText() == "" || !g.r.toast.err {
		t.Errorf("toast = %q (error %v), want the reason", g.toastText(), g.r.toast.err)
	}
}

// TestConsentButtons checks how the consent is given and refused: the focus starts on Cancel, no
// letter allows, Esc and n and Ctrl-C refuse, Tab then Enter allows, and so does a click.
//
// Parameters:
//   - t: test to report to
func TestConsentButtons(t *testing.T) {
	tests := []struct {
		name  string
		keys  []string
		click string
		want  string
	}{
		{"enter keeps the focus on cancel", []string{"enter"}, "", "needs-consent"},
		{"y is no shortcut", []string{"y", "enter"}, "", "needs-consent"},
		{"escape", []string{"esc"}, "", "needs-consent"},
		{"n", []string{"n"}, "", "needs-consent"},
		{"capital n", []string{"N"}, "", "needs-consent"},
		{"control c", []string{"ctrl+c"}, "", "needs-consent"},
		{"tab then enter", []string{"tab", "enter"}, "", "enabled"},
		{"right then enter", []string{"right", "enter"}, "", "enabled"},
		{"l then enter", []string{"l", "enter"}, "", "enabled"},
		{"tab twice is cancel again", []string{"tab", "tab", "enter"}, "", "needs-consent"},
		{"click on allow", nil, "[ Allow ]", "enabled"},
		{"click on cancel", nil, "[ Cancel ]", "needs-consent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, l := plugRig(t)
			g.resize(100, 30)
			g.pick("pager")
			g.press("a")

			if _, ok := g.top().(*consentOverlay); !ok {
				t.Fatalf("a opened %T", g.top())
			}

			if tt.click != "" {
				pos := g.footerAtBox(tt.click)
				g.click(pos.x+2, pos.y)
			}

			g.press(tt.keys...)

			if tt.want == "enabled" {
				// "a" records the consent only, so the plugin ends enabled when nothing else
				// keeps it off.
				tt.want = l.states()["pager"]
				if tt.want != "enabled" {
					t.Fatalf("pager is %s after the consent", tt.want)
				}

				return
			}

			if got := l.states()["pager"]; got != tt.want {
				t.Errorf("pager is %s, want %s", got, tt.want)
			}

			if g.top() != nil {
				t.Errorf("%T is still open", g.top())
			}
		})
	}
}

// footerAtBox finds a text inside the box of the overlay on top.
//
// Parameters:
//   - sub: text to find
func (g *rig) footerAtBox(sub string) cellPos {
	g.t.Helper()

	for y, l := range g.view() {
		plain := stripStyles(l)

		if i := strings.Index(plain, sub); i >= 0 {
			return cellPos{x: len([]rune(plain[:i])), y: y}
		}
	}

	g.t.Fatalf("%q is not on the screen:\n%s", sub, g.text())

	return cellPos{}
}

// TestPluginsInstall checks the install of the plugin that ships with tote: the staged copy is
// deleted when the user says no, and the plugin appears installed when the user says yes.
//
// Parameters:
//   - t: test to report to
func TestPluginsInstall(t *testing.T) {
	g, l := plugRig(t)

	g.pick("gft")
	g.press("i")

	c, ok := g.top().(*consentOverlay)
	if !ok {
		t.Fatalf("i opened %T:\n%s", g.top(), g.text())
	}

	if !strings.Contains(g.text(), "Install plugin gft") || c.yes {
		t.Errorf("consent = %+v:\n%s", c, g.text())
	}

	if n := stagingLeft(t, l); n != 1 {
		t.Errorf("%d installs wait in the staging area, want 1", n)
	}

	g.press("esc")

	if n := stagingLeft(t, l); n != 0 {
		t.Errorf("%d installs are left in the staging area after Esc", n)
	}

	if _, installed := l.reg.Recheck("gft"); installed {
		t.Fatal("gft was installed although the user said no")
	}

	g.press("i", "tab", "enter")

	if _, installed := l.reg.Recheck("gft"); !installed {
		t.Fatalf("gft was not installed (toast %q)", g.toastText())
	}

	if !strings.Contains(g.toastText(), "installed plugin gft 1.0.0") {
		t.Errorf("toast = %q", g.toastText())
	}

	if row := g.screen().rows; len(row) != 7 {
		t.Errorf("%d rows after the install, want 7", len(row))
	}

	g.press("i")

	if !strings.Contains(g.toastText(), "already installed") {
		t.Errorf("installing twice: toast %q", g.toastText())
	}
}

// TestPluginsInstallQuit checks that a package waiting in the staging area is deleted when the
// interface ends with the consent still open.
//
// Parameters:
//   - t: test to report to
func TestPluginsInstallQuit(t *testing.T) {
	g, l := plugRig(t)

	g.pick("gft")
	g.press("i")

	if n := stagingLeft(t, l); n != 1 {
		t.Fatalf("%d installs wait in the staging area, want 1", n)
	}

	g.r.closeOverlays()

	if n := stagingLeft(t, l); n != 0 {
		t.Errorf("%d installs are left in the staging area", n)
	}
}

// TestPluginsRemove checks the removal: a question lists what goes with No as the answer, and
// "Remove with its data" deletes the settings too.
//
// Parameters:
//   - t: test to report to
func TestPluginsRemove(t *testing.T) {
	g, l := plugRig(t)

	g.pick("notes")
	g.press("d")

	a, ok := g.top().(*ask)
	if !ok {
		t.Fatalf("d opened %T", g.top())
	}

	if a.focus || !strings.Contains(g.text(), "its installed files") {
		t.Errorf("question = %+v:\n%s", a, g.text())
	}

	g.press("enter")

	if _, ok := l.states()["notes"]; !ok {
		t.Fatal("notes was removed by Enter on No")
	}

	g.press("d", "n")

	if _, ok := l.states()["notes"]; !ok {
		t.Fatal("notes was removed by n")
	}

	g.press("d", "y")

	if _, ok := l.states()["notes"]; ok {
		t.Fatalf("notes is still installed (toast %q)", g.toastText())
	}

	if !strings.Contains(g.toastText(), `removed plugin "notes"`) {
		t.Errorf("toast = %q", g.toastText())
	}

	file, _ := l.sb.Opts.Paths.PluginSettingsFile("demo")
	writeFile(t, file, "schema = 1\nlimit = 20\n")

	g.pick("demo")
	g.press("tab", "down", "down", "down", "down", "down", "down", "enter")

	if !strings.Contains(g.text(), "its settings, state") {
		t.Fatalf("the purge question lacks the list of data:\n%s", g.text())
	}

	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}} {
		g.resize(size[0], size[1])

		if got := g.text(); !strings.Contains(got, "config.toml") {
			t.Errorf("the purge question is cut at %dx%d:\n%s", size[0], size[1], got)
		}
	}

	g.resize(80, 24)
	g.press("tab", "enter")

	if _, err := os.Stat(file); err == nil {
		t.Errorf("the settings file is still there after the removal with data")
	}
}

// TestPluginsCheck checks the report of the check: the verdict, the findings with their severity
// and the contributions.
//
// Parameters:
//   - t: test to report to
func TestPluginsCheck(t *testing.T) {
	g, _ := plugRig(t)

	g.pick("notes")
	g.press("c")

	if _, ok := g.top().(*infoOverlay); !ok {
		t.Fatalf("c opened %T", g.top())
	}

	for _, want := range []string{"notes: no problem found", "contributes", "command go"} {
		if !strings.Contains(g.text(), want) {
			t.Errorf("report lacks %q:\n%s", want, g.text())
		}
	}

	g.press("esc")
	g.pick("demo")
	g.press("c")

	for _, want := range []string{"errors 0, warnings 2", "warning"} {
		if !strings.Contains(g.text(), want) {
			t.Errorf("report of demo lacks %q:\n%s", want, g.text())
		}
	}
}

// TestPluginsDetails checks what the details show for each kind of plugin.
//
// Parameters:
//   - t: test to report to
func TestPluginsDetails(t *testing.T) {
	tests := []struct {
		id   string
		want []string
	}{
		{"demo", []string{
			"Shows notes about the work.", "declared access", "exec:tool",
			"access you allowed", "settings", "owner (string) = someone, from default",
		}},
		{"ghost", []string{
			"missing dependency", "ghost-tool: not found",
			"suggested by the plugin, not checked by tote",
		}},
		{"jumpy", []string{"quarantined", "last failure", "crash", "fix:"}},
		{"pager", []string{"consent needed", "fix:"}},
		{"gft", []string{
			"available",
			"it ships with tote: install it with tote plugin install gft",
		}},
	}

	g, _ := plugRig(t)
	g.resize(110, 40)

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			g.t = t
			g.pick(tt.id)
			g.press("enter")

			for _, w := range tt.want {
				if !strings.Contains(g.text(), w) {
					t.Errorf("details of %s lack %q:\n%s", tt.id, w, g.text())
				}
			}

			g.press("esc")
		})
	}

	// A plugin nobody allowed has no list of what was allowed.
	g.pick("pager")
	g.press("enter")

	if strings.Contains(g.text(), "access you allowed") {
		t.Errorf("details of pager list access that was never allowed:\n%s", g.text())
	}
}

// TestPluginsMenu checks the action menu: its entries follow the state, and an entry does what
// the key does.
//
// Parameters:
//   - t: test to report to
func TestPluginsMenu(t *testing.T) {
	tests := []struct {
		id     string
		want   []string
		absent []string
	}{
		{
			"demo",
			[]string{"Details", "Disable", "Settings", "Update", "Check", "Remove"},
			[]string{"Enable", "Allow", "Clear the quarantine", "Install"},
		},
		{"pager", []string{"Allow", "Enable"}, []string{"Disable", "Settings"}},
		{"ghost", []string{"Disable", "Check"}, []string{"Enable", "Allow"}},
		{"jumpy", []string{"Clear the quarantine"}, []string{"Enable", "Disable"}},
		{"gft", []string{"Details", "Install", "Check"}, []string{"Remove", "Update"}},
	}

	g, l := plugRig(t)

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			g.t = t
			g.pick(tt.id)
			g.press("tab")

			text := g.text()
			for _, w := range tt.want {
				if !strings.Contains(text, w) {
					t.Errorf("menu of %s lacks %q:\n%s", tt.id, w, text)
				}
			}

			for _, a := range tt.absent {
				if strings.Contains(text, a) {
					t.Errorf("menu of %s shows %q:\n%s", tt.id, a, text)
				}
			}

			g.press("esc")
		})
	}

	g.pick("jumpy")
	g.press(".")
	g.press("enter")

	if g.top() == nil {
		t.Fatalf("Enter on Details closed the menu without opening the details")
	}

	g.press("esc", ".", "down", "enter")

	if st := l.states()["jumpy"]; st != "enabled" {
		t.Errorf("jumpy is %s after clearing the quarantine (toast %q)", st, g.toastText())
	}
}

// slowHost is a plugin host whose Plan and Enable wait for a signal, to show a job that is still
// running.
//
// Gate holds Plan until it is closed, or until its context ends. Started tells that Plan began.
type slowHost struct {
	PluginHost
	gate    chan struct{}
	mu      sync.Mutex
	started bool
	ended   error
}

// Plan implements PluginHost: it waits for the gate or for the end of the context.
//
// Parameters:
//   - ctx: ends when the user gives up
//   - req: what to install
func (h *slowHost) Plan(ctx context.Context, req plugin.InstallRequest) (*plugin.Staged, error) {
	h.mu.Lock()
	h.started = true
	h.mu.Unlock()

	select {
	case <-ctx.Done():
		h.mu.Lock()
		h.ended = ctx.Err()
		h.mu.Unlock()

		return nil, ctx.Err()
	case <-h.gate:
	}

	return h.PluginHost.Plan(ctx, req)
}

// Enable implements PluginHost: it waits for the gate.
//
// Parameters:
//   - id: plugin id
//   - g: consent
func (h *slowHost) Enable(id string, g plugin.Grant) (plugin.EnableResult, error) {
	<-h.gate

	return h.PluginHost.Enable(id, g)
}

// TestPluginsJobCancel checks a job that can be given up: while it runs the wait box is on top,
// Esc gives it up with a message, and its late answer changes nothing.
//
// Parameters:
//   - t: test to report to
func TestPluginsJobCancel(t *testing.T) {
	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.fixtures()

	host := &slowHost{PluginHost: l.reg, gate: make(chan struct{})}
	sb.Opts.Plugins = host
	sb.Opts.Start = StartPlugins

	g := newRig(t, sb)
	g.pick("gft")

	// The rig waits a second for the command, then goes on with the job still running.
	g.press("i")

	if _, ok := g.top().(*busyOverlay); !ok {
		t.Fatalf("i opened %T, want the wait box", g.top())
	}

	if !strings.Contains(g.text(), "gft") || !strings.Contains(g.footerText(), "esc") {
		t.Errorf("wait box:\n%s\nfooter %q", g.text(), g.footerText())
	}

	g.press("i")

	if len(g.r.overlays) != 1 {
		t.Errorf("a second job was started: %d overlays", len(g.r.overlays))
	}

	g.press("esc")

	if g.top() != nil || g.r.job != nil {
		t.Fatalf("Esc left %T on top and the job %v", g.top(), g.r.job)
	}

	if !strings.Contains(g.toastText(), "nothing changed") {
		t.Errorf("toast = %q", g.toastText())
	}

	g.await("the job to see the end of its context", func() bool {
		host.mu.Lock()
		defer host.mu.Unlock()

		return host.ended != nil
	})

	for len(g.late) > 0 {
		g.send(<-g.late)
	}

	if g.top() != nil || stagingLeft(t, l) != 0 {
		t.Errorf("the late answer left %T on top, %d in staging", g.top(), stagingLeft(t, l))
	}
}

// TestPluginsJobWrite checks a job that changes the installed plugins: it cannot be given up, the
// keys are held, and the screen shows the result when it ends.
//
// Parameters:
//   - t: test to report to
func TestPluginsJobWrite(t *testing.T) {
	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.fixtures()

	host := &slowHost{PluginHost: l.reg, gate: make(chan struct{})}
	sb.Opts.Plugins = host
	sb.Opts.Start = StartPlugins

	g := newRig(t, sb)
	g.pick("notes")
	g.press("space")

	if _, ok := g.top().(*busyOverlay); !ok {
		t.Fatalf("space opened %T, want the wait box", g.top())
	}

	if !g.r.e.pendingWrites() {
		t.Errorf("a write is not counted while the job runs")
	}

	g.press("esc")

	if _, ok := g.top().(*busyOverlay); !ok {
		t.Fatalf("Esc gave up a job that writes: %T", g.top())
	}

	if got := g.footerText(); strings.Contains(got, "esc") {
		t.Errorf("footer = %q, offers a way out of a job that cannot stop", got)
	}

	close(host.gate)

	g.await("the job to end", func() bool { return g.top() == nil })

	if g.r.e.pendingWrites() {
		t.Errorf("a write is still counted after the job")
	}

	if st := l.states()["notes"]; st != "enabled" {
		t.Errorf("notes is %s", st)
	}
}
