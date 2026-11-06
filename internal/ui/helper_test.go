package ui

import (
	"context"
	"errors"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/i18n"
)

const (
	// fixtureConns holds six connections: a plain one, one with a custom via, one that needs
	// review, one with hostile text and one that is only reachable through an IPv6 target.
	fixtureConns = `schema = 1

[[connection]]
name = "prod"
target = "prod.example.com"
user = "deploy"
aliases = ["p"]
desc = "Production web"

[[connection]]
name = "staging"
target = "staging.example.com"
user = "deploy"

[[connection]]
name = "bastion"
via = "mosh"
target = "bastion.example.com"
user = "ops"
ip = "10.0.0.9"

[[connection]]
name = "legacy"
target = "old.example.com"
needs_review = true

[[connection]]
name = "emoji"
target = "fun.example.com"
desc = "rocket \U0001F680 heart \u2764\uFE0F dev \U0001F468\u200D\U0001F4BB \u65E5\u672C\u8A9E"

[[connection]]
name = "hostile"
target = "evil.example.com"
desc = "a\u001b[31mb\u202Ec\u009dd"
`

	// fixtureAliases holds two aliases.
	fixtureAliases = `schema = 1

[[alias]]
name = "ll"
cmd = "ls -la"
desc = "Long list"

[[alias]]
name = "gs"
cmd = "git status"
`

	// fixtureConfig sets three settings and carries a comment that a write must keep.
	fixtureConfig = `schema = 1

# my own note
editor = "nano -w"  # trailing note
repos_root = "/srv/work"

[connect]
after_disconnect = "exit"

[ui]
theme = "dark"
`

	// fixtureViaConns holds one connection whose command has no place for another login.
	fixtureViaConns = `schema = 1

[[connection]]
name = "jump"
via = "tsh ssh"
target = "jump.example.com"
user = "ops"
`

	// fixtureFuncs holds two functions.
	fixtureFuncs = `schema = 1

[[func]]
name = "hello"
kind = "source"
script = "~/bin/hello.sh"
managed = false

[[func]]
name = "tool"
desc = "A tool"
kind = "exec"
script = "/opt/tool/tool"
managed = true
`
)

// sandbox is a throw-away set of data files and the options that point the interface at them.
//
// Dir is the root, Repo an existing directory registered as the repository "proj" and Opts the
// options to give to newRoot or Run.
type sandbox struct {
	Dir  string
	Repo string
	Opts Options
}

// fakeRunner is a Runner that records what it was asked to start and returns a fixed result.
//
// Calls holds the specs it received, Res the result it gives and Out the text it writes to the
// standard output of the program.
type fakeRunner struct {
	mu    sync.Mutex
	Calls []RunSpec
	Res   RunResult
	Out   string
}

// Run implements Runner.
//
// Parameters:
//   - spec: program to run
func (f *fakeRunner) Run(spec RunSpec) RunResult {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Calls = append(f.Calls, spec)

	if f.Out != "" && spec.Stdout != nil {
		_, _ = io.WriteString(spec.Stdout, f.Out)
	}

	return f.Res
}

// calls returns a copy of the recorded specs.
func (f *fakeRunner) calls() []RunSpec {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]RunSpec(nil), f.Calls...)
}

// noProbe is the "ssh -G" lookup of the tests that do not care about it: it fails, so that no
// option is ever added to a command and no real ssh is run.
//
// Parameters:
//   - ctx: unused
//   - argv: unused
//   - environ: unused
func noProbe(context.Context, []string, []string) (string, error) {
	return "", errors.New("no ssh in tests")
}

// settle plays the lookup that comes before a connection and returns the command that starts it,
// which is what a connection gave in the tests that were written before the lookup existed.
//
// Parameters:
//   - g: model that receives the answer of the lookup
//   - cmd: command returned for a connection
func settle(g *rig, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}

	msg := cmd()

	p, ok := msg.(probedMsg)
	if !ok {
		return func() tea.Msg { return msg }
	}

	_, next := g.r.Update(p)

	return next
}

// clock is a clock that only moves when told to.
type clock struct {
	t time.Time
}

// now returns the current fake time.
func (c *clock) now() time.Time { return c.t }

// advance moves the fake time forward.
//
// Parameters:
//   - d: duration to add
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// writeFile creates a file and its directory.
//
// Parameters:
//   - t: running test
//   - path: file to create
//   - content: text to store
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newSandbox writes the fixtures below a temporary directory.
//
// Parameters:
//   - t: running test
//
// Returns options for a 100 by 30 terminal with a UTF-8 locale, true color, the English catalog
// and a fake runner. Nothing outside t.TempDir() is read or written.
func newSandbox(t *testing.T) *sandbox {
	t.Helper()

	dir := t.TempDir()
	repo := filepath.Join(dir, "work", "proj")

	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := filepath.Join(dir, "config")
	writeFile(t, filepath.Join(cfg, "connections.toml"), fixtureConns)
	writeFile(t, filepath.Join(cfg, "aliases.toml"), fixtureAliases)
	writeFile(t, filepath.Join(cfg, "funcs.toml"), fixtureFuncs)
	writeFile(t, filepath.Join(cfg, "repos.toml"), "schema = 1\n\n[[repo]]\nname = \"proj\"\n"+
		"path = \""+repo+"\"\ndesc = \"This tool\"\n\n[[repo]]\nname = \"docs\"\n"+
		"path = \"~/missing/docs\"\n")

	clk := &clock{t: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)}

	return &sandbox{Dir: dir, Repo: repo, Opts: Options{
		Paths: config.Paths{
			Config:  cfg,
			Data:    filepath.Join(dir, "data"),
			State:   filepath.Join(dir, "state"),
			Cache:   filepath.Join(dir, "cache"),
			Runtime: filepath.Join(dir, "run"),
		},
		Settings: config.DefaultSettings(),
		Printer:  i18n.New("en"),
		Version:  "1.2.3",
		Home:     filepath.Join(dir, "home"),
		User:     "tester",
		Environ: []string{
			"TERM=xterm-256color", "LANG=en_US.UTF-8",
			"TOTE_COLOR=truecolor", "HOME=" + filepath.Join(dir, "home"),
		},
		Runner:       &fakeRunner{},
		Probe:        noProbe,
		Now:          clk.now,
		TmuxFeatures: func() string { return "" },
		Width:        100,
		Height:       30,
	}}
}

// with returns a copy of the sandbox whose options were changed by a function.
//
// Parameters:
//   - f: function that edits the options
func (s *sandbox) with(f func(*Options)) *sandbox {
	c := *s
	f(&c.Opts)

	return &c
}

// rig is a root model driven by scripted messages.
//
// Quit becomes true when a command returned tea.QuitMsg and Seen holds every message that the
// commands produced, in order.
type rig struct {
	t    *testing.T
	r    *root
	Quit bool
	Seen []tea.Msg
	late chan tea.Msg
}

// newRig builds a root from a sandbox, as Run does.
//
// Parameters:
//   - t: running test
//   - s: sandbox giving the options
func newRig(t *testing.T, s *sandbox) *rig {
	t.Helper()

	o := s.Opts.withDefaults()

	g := &rig{t: t, r: newRoot(o, io.Discard), late: make(chan tea.Msg, 16)}

	// The count of the plugins is read after the first frame in a run; a test starts with it read.
	for _, m := range collect(g.r.e.Plug.fetch()) {
		g.send(m)
	}

	return g
}

// cmdTimeout is how long a command may run before the rig gives up on it. The timers of the
// interface are switched off in TestMain, so only a command that really waits, such as a check
// for an update held by a test, reaches it.
const cmdTimeout = time.Second

// collect runs a command and returns the messages it produces, batches flattened. A command
// that does not answer within cmdTimeout, such as a timer, produces nothing.
//
// Parameters:
//   - cmd: command to run, may be nil
func collect(cmd tea.Cmd) []tea.Msg {
	return collectLate(cmd, nil)
}

// collectLate is collect that hands the answer of a slow command to a channel instead of
// dropping it, so that a test can wait for a write that outlasts cmdTimeout on a loaded machine.
//
// Parameters:
//   - cmd: command to run, may be nil
//   - late: receives the message of a command that answered after the timeout, may be nil
func collectLate(cmd tea.Cmd, late chan<- tea.Msg) []tea.Msg {
	if cmd == nil {
		return nil
	}

	ch := make(chan tea.Msg)
	done := make(chan struct{})

	go func() {
		m := cmd()

		select {
		case ch <- m:
		case <-done:
			if late != nil {
				select {
				case late <- m:
				default:
				}
			}
		}
	}()

	select {
	case m := <-ch:
		if b, ok := m.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, collectLate(c, late)...)
			}

			return out
		}

		return []tea.Msg{m}
	case <-time.After(cmdTimeout):
		close(done)

		return nil
	}
}

// send delivers a message to the model and plays every message that its commands produce.
//
// Parameters:
//   - msg: first message
func (g *rig) send(msg tea.Msg) {
	g.t.Helper()

	queue := []tea.Msg{msg}

	for n := 0; len(queue) > 0; n++ {
		if n > 200 {
			g.t.Fatal("message loop does not end")
		}

		m := queue[0]
		queue = queue[1:]

		if _, ok := m.(tea.QuitMsg); ok {
			g.Quit = true
			continue
		}

		g.Seen = append(g.Seen, m)
		_, cmd := g.r.Update(m)
		queue = append(queue, collectLate(cmd, g.late)...)
	}
}

// await plays the messages of slow commands until a condition holds, for a test that waits for a
// write of a file. It fails the test after thirty seconds, which only a failure takes.
//
// Parameters:
//   - what: description of the condition, for the failure
//   - cond: condition to wait for
func (g *rig) await(what string, cond func() bool) {
	g.t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for !cond() {
		if time.Now().After(deadline) {
			g.t.Fatalf("timeout waiting for %s", what)
		}

		select {
		case m := <-g.late:
			g.send(m)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// press delivers key presses written as in a key map: "enter", "esc", "ctrl+k", "a".
//
// Parameters:
//   - keys: key names in order
func (g *rig) press(keys ...string) {
	g.t.Helper()

	for _, k := range keys {
		g.send(keyMsg(k))
	}
}

// typeText delivers each character of a string as a key press.
//
// Parameters:
//   - text: characters to type
func (g *rig) typeText(text string) {
	g.t.Helper()

	for _, r := range text {
		g.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// resize delivers a window size message.
//
// Parameters:
//   - w: width in cells
//   - h: height in rows
func (g *rig) resize(w, h int) {
	g.t.Helper()
	g.send(tea.WindowSizeMsg{Width: w, Height: h})
}

// view returns the rendered screen split into lines.
func (g *rig) view() []string {
	return strings.Split(g.r.render(), "\n")
}

// text returns the rendered screen without styles.
func (g *rig) text() string {
	return stripStyles(g.r.render())
}

// footerText returns the footer of the screen as plain text: the last line of the page, or, while
// an overlay is open, the line that hangs under its box.
func (g *rig) footerText() string {
	e := g.r.e

	if n := len(g.r.overlays); n > 0 {
		block := g.r.overlays[n-1].View(e, max(1, e.Cols-4), max(1, e.Height-2))

		return stripStyles(g.r.footer(e.Metric.width(block[0])))
	}

	return stripStyles(g.r.footer(e.Width))
}

// toastText returns the current message of the status line.
func (g *rig) toastText() string {
	return g.r.toast.text
}

// keyMsg builds the key press for a key name.
//
// Parameters:
//   - name: "enter", "esc", "up", "down", "left", "right", "pgup", "pgdown", "home", "end",
//     "backspace", "tab", "space", "ctrl+x" or a single character
func keyMsg(name string) tea.KeyPressMsg {
	named := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "pgup": tea.KeyPgUp,
		"pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
		"backspace": tea.KeyBackspace, "tab": tea.KeyTab,
	}

	if code, ok := named[name]; ok {
		return tea.KeyPressMsg{Code: code}
	}

	if name == "space" {
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	}

	if rest, ok := strings.CutPrefix(name, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: []rune(rest)[0], Mod: tea.ModCtrl}
	}

	r := []rune(name)

	return tea.KeyPressMsg{Code: r[0], Text: name}
}

// stripStyles removes the escape sequences of a styled text.
//
// Parameters:
//   - s: styled text
func stripStyles(s string) string {
	return ansi.Strip(s)
}

// newPrinter returns the printer of a language.
//
// Parameters:
//   - lang: "en" or "fr"
func newPrinter(lang string) *i18n.Printer {
	return i18n.New(lang)
}

// TestMain keeps ResetTerminal away from the terminal of the person who runs the tests, and
// switches the timers of the interface off.
//
// Parameters:
//   - m: test binary to run
func TestMain(m *testing.M) {
	ttyPath = ""

	// The tests play the expiry messages themselves; a pending timer would only slow the rig.
	tick = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }

	os.Exit(m.Run())
}

// backgroundMsg builds the answer of a terminal to the query for its background color.
//
// Parameters:
//   - dark: true for a black background, false for a white one
func backgroundMsg(dark bool) tea.BackgroundColorMsg {
	if dark {
		return tea.BackgroundColorMsg{Color: color.RGBA{A: 255}}
	}

	return tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}
}
