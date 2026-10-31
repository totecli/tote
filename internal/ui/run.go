package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/shell"
)

// ErrInterrupted is returned by Run when the interface was stopped by SIGINT.
var ErrInterrupted = errors.New("interrupted")

// Start is the screen shown when the interface opens.
type Start int

const (
	// StartHome opens the welcome page.
	StartHome Start = iota

	// StartConnections opens the list of connections.
	StartConnections

	// StartRepos opens the list of repositories.
	StartRepos

	// StartAliases opens the list of aliases.
	StartAliases

	// StartFuncs opens the list of functions.
	StartFuncs

	// StartConfig opens the settings.
	StartConfig

	// StartPlugins opens the plugins screen, or the welcome page when plugins are off.
	StartPlugins
)

// renderFPS is the most frames per second that Bubble Tea draws. Its renderer wakes up at this
// rate for as long as the program runs, even when nothing changes, and on a Go runtime every
// wake-up costs the same whatever the screen holds (about 0.03 percent of a core per frame per
// second on the machine where it was measured). The default of 60 made an idle interface cost
// 1.5 percent of a core; 15 keeps the cost near a quarter of that and delays a change by 67 ms
// at most, which a launcher driven by keys and clicks does not show.
const renderFPS = 15

// wrapModel lets a test put a model around the root of Run, to count what the program does. It is
// nil in production.
var wrapModel func(tea.Model) tea.Model

// Options configures Run.
//
// Paths, Settings and Printer are required. Version is shown in the welcome box, Home is the
// home directory used to expand "~" in repository paths and User the name in the greeting.
// Environ is the process environment, os.Environ() when nil; it decides the color profile and
// is given to the programs that are started. ASCII forces the ASCII glyphs and Start picks the
// first screen.
//
// The remaining fields are seams for tests and are nil in production: Input and Output replace
// the terminal, Runner the process runner, Now the clock, TmuxFeatures the tmux query, Probe the
// "ssh -G" lookup (it runs a command and returns its standard output) and Width and Height give
// the size of a terminal that is not one.
//
// UpdateNotice, when set, is called once in the background after the interface has started. It
// returns the version to announce in the status line, or an empty string for none, and its
// context ends when the interface closes. CheckUpdate is the check that the key "U" runs: unlike
// UpdateNotice it always asks the release server, and it reports how the installed version
// compares with the latest release. TmuxClipboard is the seam for the value of the tmux option
// set-clipboard, which tells whether tmux lets the interface set the clipboard, and Clipboard the
// one for the programs that put a text selected with the mouse in the clipboard, wl-copy, xclip
// and xsel by default.
//
// NoWrapper is true when the shell wrapper is not active (no TOTE_HANDOFF): the interface then
// never quits to hand a command line or a directory to the shell, it shows a message with the
// line that enables the wrapper and stays open.
//
// Plugins is the plugin registry. Nil means that plugins are off: the welcome page has no plugin
// entry and the plugin screens cannot be reached.
type Options struct {
	Paths        config.Paths
	Settings     config.Settings
	Printer      *i18n.Printer
	Version      string
	Home         string
	User         string
	Environ      []string
	ASCII        bool
	Start        Start
	Input        io.Reader
	Output       io.Writer
	Runner       Runner
	Now          func() time.Time
	TmuxFeatures func() string
	Probe        shell.Prober
	Width        int
	Height       int
	UpdateNotice func(ctx context.Context) string
	CheckUpdate  func(ctx context.Context) (UpdateCheck, error)

	TmuxClipboard func() string
	Clipboard     ClipboardFunc
	NoWrapper     bool
	Plugins       PluginHost
}

// UpdateCheck is the answer of Options.CheckUpdate.
//
// Status is "available" when a newer release exists, "current" when the installed version is the
// latest, "newer" when it is ahead of the latest release, "dev" for a development build and
// "managed" when a package manager owns the program, in which case Command is what to run to
// update it. Current and Latest are version numbers.
type UpdateCheck struct {
	Status  string
	Current string
	Latest  string
	Command string
}

// Result tells the caller what the shell should do when the interface is closed.
//
// Action is shell.None, shell.Insert (Payload is the command line to put at the prompt) or
// shell.ChangeDir (Payload is the directory). Reload is true when a record that the shell
// definitions are built from was written, so that the wrapper loads them again.
type Result struct {
	Action  shell.Action
	Payload string
	Reload  bool
}

// Handoff converts the result into the message for the wrapper.
func (r Result) Handoff() shell.Handoff {
	a := r.Action
	if a == "" {
		a = shell.None
	}

	return shell.Handoff{Action: a, Reload: r.Reload, Payload: r.Payload}
}

// Empty reports whether the shell has nothing to do.
func (r Result) Empty() bool {
	return (r.Action == "" || r.Action == shell.None) && !r.Reload
}

// withDefaults fills the optional fields of the options.
func (o Options) withDefaults() Options {
	if o.Printer == nil {
		o.Printer = i18n.New("en")
	}

	if o.Environ == nil {
		o.Environ = os.Environ()
	}

	if o.Now == nil {
		o.Now = time.Now
	}

	if o.Runner == nil {
		o.Runner = osRunner{}
	}

	if o.Probe == nil {
		o.Probe = shell.SSHProbe
	}

	if o.TmuxFeatures == nil {
		o.TmuxFeatures = tmuxFeatures
	}

	if o.TmuxClipboard == nil {
		o.TmuxClipboard = tmuxClipboard
	}

	if o.Clipboard == nil {
		o.Clipboard = copyWithTools
	}

	if o.User == "" {
		get := envLookup(o.Environ)
		o.User = get("USER")

		if o.User == "" {
			o.User = get("LOGNAME")
		}
	}

	return o
}

// printerFor gives the printer of the glyph set in use: with the plain ASCII glyphs the messages
// are folded to ASCII too, whatever the cause of the plain glyphs is.
//
// Parameters:
//   - p: printer of the language
//   - ascii: true when the plain ASCII glyph set is in use
func printerFor(p *i18n.Printer, ascii bool) *i18n.Printer {
	if ascii {
		return p.ASCII()
	}

	return p
}

// newRoot builds the model of the program without starting it.
//
// Parameters:
//   - o: options with their defaults filled
//   - out: output used to detect the color profile
func newRoot(o Options, out io.Writer) *root {
	get := envLookup(o.Environ)
	profile := detectProfile(o.Environ, out, o.TmuxFeatures)
	glyph := newGlyphs(useASCII(o.ASCII, get))
	printer := printerFor(o.Printer, glyph.ascii)

	r := &root{
		runner:    o.Runner,
		guard:     newGuard(o.Now),
		delay:     execDelay,
		profile:   profile,
		panicKey:  debugPanic(get),
		mousable:  mouseTerm(get),
		noWrapper: o.NoWrapper,
	}

	r.e = &env{
		Tr:       printer,
		Glyphs:   glyph,
		Metric:   newMetric(),
		Keys:     newKeymap(printer, glyph),
		Settings: o.Settings,
		Paths:    o.Paths,
		Data:     newDataset(printer, o.Paths, o.Home),
		Home:     o.Home,
		User:     o.User,
		Version:  o.Version,
		Environ:  o.Environ,
		Probe:    o.Probe,
		Now:      o.Now,
		Writes:   new(atomic.Int32),
	}

	r.ctx = context.Background()
	r.check = o.CheckUpdate
	r.tmuxClip = o.TmuxClipboard
	r.clip = o.Clipboard
	r.e.Profile = profile

	if o.Plugins != nil {
		r.e.Plugins = o.Plugins
		r.e.Plug = &pluginCache{host: o.Plugins, lang: printer.Lang()}
		r.e.pluginWords(printer.Lang())
	}

	r.pickMode()
	r.waiting = r.auto
	r.rebuildTheme()
	r.resize(o.Width, o.Height)

	r.scr = startScreen(r.e, o.Start)

	return r
}

// startScreen builds the first screen.
//
// Parameters:
//   - e: shared environment
//   - s: screen asked for
func startScreen(e *env, s Start) screen {
	switch s {
	case StartConnections:
		return newList(e, kindConn)
	case StartRepos:
		return newList(e, kindRepo)
	case StartAliases:
		return newList(e, kindAlias)
	case StartFuncs:
		return newList(e, kindFunc)
	case StartConfig:
		return newConfig(e, "")
	case StartPlugins:
		if e.pluginsOn() {
			return newPlugins(e, "")
		}
	}

	return newHome(0)
}

// interruptTo wraps the delivery of messages so that an interrupt ends the program through
// its ordinary quit. Bubble Tea ends a program that returns an error by killing it, which
// closes the input reader while its goroutine is still waking up, a data race in the library; an
// ordinary quit waits for the reader first.
//
// Parameters:
//   - send: delivers a message to the program, usually Program.Send
func interruptTo(send func(tea.Msg)) func(tea.Msg) {
	return func(msg tea.Msg) {
		if _, ok := msg.(tea.InterruptMsg); ok {
			msg = interruptMsg{}
		}

		send(msg)
	}
}

// Run shows the interface and returns when the user leaves it.
//
// Parameters:
//   - o: options
//
// Returns what the shell should do; Reload is set whatever way the interface was left, as soon
// as a record that the shell definitions are built from was written. The error is
// ErrInterrupted after a SIGINT, or the reason the interface could not run. The terminal is
// restored in every case, a panic included.
func Run(o Options) (res Result, err error) {
	o = o.withDefaults()

	out := o.Output
	if out == nil {
		out = os.Stdout
	}

	m := newRoot(o, out)

	opts := []tea.ProgramOption{
		tea.WithoutSignalHandler(),
		tea.WithColorProfile(m.profile),
		tea.WithEnvironment(o.Environ),
		tea.WithFPS(renderFPS),
	}

	if o.Input != nil {
		opts = append(opts, tea.WithInput(o.Input))
	}

	if o.Output != nil {
		opts = append(opts, tea.WithOutput(o.Output))
	}

	if o.Width > 0 && o.Height > 0 {
		opts = append(opts, tea.WithWindowSize(o.Width, o.Height))
	}

	var model tea.Model = m
	if wrapModel != nil {
		model = wrapModel(m)
	}

	prog := tea.NewProgram(model, opts...)
	stop := m.guard.watch(interruptTo(prog.Send))

	defer stop()

	noticeCtx, endNotice := context.WithCancel(context.Background())
	defer endNotice()

	m.ctx = noticeCtx

	hangup := make(chan os.Signal, 1)
	signal.Notify(hangup, syscall.SIGHUP)

	defer signal.Stop(hangup)

	go func() {
		select {
		case <-hangup:
			prog.Quit()
		case <-noticeCtx.Done():
		}
	}()

	if o.UpdateNotice != nil {
		go func() {
			if v := o.UpdateNotice(noticeCtx); v != "" {
				prog.Send(updateMsg{Version: v})
			}
		}()
	}

	defer func() {
		if rec := recover(); rec != nil {
			prog.Kill()
			_ = ResetTerminal(out)
			err = fmt.Errorf("ui: crash: %v", rec)
		}
	}()

	_, runErr := prog.Run()

	m.closeOverlays()

	switch {
	case errors.Is(runErr, tea.ErrProgramPanic):
		_ = ResetTerminal(out)
		return Result{}, runErr
	case errors.Is(runErr, tea.ErrInterrupted):
		return Result{Reload: m.stale}, ErrInterrupted
	case runErr != nil:
		return Result{}, runErr
	case m.interrupted:
		return Result{Reload: m.stale}, ErrInterrupted
	}

	res = m.result
	res.Reload = m.stale

	return res, nil
}
