package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/plugin"
	"github.com/totecli/tote/internal/plugin/bundled"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
	"github.com/totecli/tote/internal/ui"
	"github.com/totecli/tote/internal/update"
)

const (
	// exitOK is the exit code of a command that succeeded.
	exitOK = 0

	// exitFail is the exit code of a command that failed at run time.
	exitFail = 1

	// exitUsage is the exit code of a command line that is wrong.
	exitUsage = 2

	// exitAuth is the exit code of a plugin command that needs a sign-in tote cannot give.
	exitAuth = 3

	// exitRefused is the exit code of a plugin command refused by a state or a policy: a plugin
	// that cannot run, a --grant token that differs, a call from a plugin or --no-plugins.
	exitRefused = 4

	// exitIntegrity is the exit code of a plugin command that found bytes that are not the
	// expected ones: a checksum, an archive or a tree.
	exitIntegrity = 5

	// exitExec is the exit code when the program to run cannot be started.
	exitExec = 127

	// maxAnswer is the most bytes read as the answer to a confirmation, so that an endless
	// input such as a character device cannot exhaust the memory.
	maxAnswer = 256
)

// app is one run of the tote command line, with everything it touches from the outside.
//
// Stdin, Stdout and Stderr are the standard streams, Getenv reads the environment and Environ
// lists it for a program started with Exec, Home is the home directory and Version the release
// string. Exec replaces the process by the given program, IsTerminal tells whether an output is
// a terminal and IsInputTerminal does the same for an input; tests replace all of them. The
// remaining fields cache the resolved paths, the settings and the printer, which are computed on
// first use so that "tote version" never reads a file; answers is the reader of the replies to
// confirmations, shared so that two questions in a row read two lines. UI runs the full-screen
// interface and returns what the shell should do afterwards; tests replace it, and ascii is set
// by --ascii.
//
// Executable returns the path of the running program, Geteuid the effective user id and Context
// the context that a Ctrl-C cancels, for the update and uninstall verbs; a nil field means the
// real one. Updater is the template of the update.Updater that "tote update" fills in: tests
// give it a fake Run, a fixed OS and Arch, and keys.
//
// For "tote doctor", Now is the clock, LookPath finds a program in PATH, TermSize gives the size
// of the terminal, TmuxFeatures the features that the tmux client reports, TmuxClipboard the
// value of the tmux option set-clipboard and Stty restores the line discipline of a terminal
// input; a nil field means the real one. Probe runs "ssh -G" for
// the keepalive options of "tote co NAME"; nil means the real lookup. The OS and Arch of Updater
// also give the platform that the doctor reports, so that its output can be fixed in a test.
type app struct {
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	Getenv          func(string) string
	Environ         func() []string
	Home            string
	Version         string
	Exec            func(argv []string, env []string) error
	IsTerminal      func(io.Writer) bool
	IsInputTerminal func(io.Reader) bool
	UI              func(ui.Options) (ui.Result, error)
	Executable      func() (string, error)
	Geteuid         func() int
	Context         func() (context.Context, context.CancelFunc)
	Updater         update.Updater
	Now             func() time.Time
	LookPath        func(string) (string, error)
	TermSize        func() (int, int, bool)
	TmuxFeatures    func() string
	TmuxClipboard   func() string
	Stty            func(io.Reader) error
	Probe           shell.Prober
	Bundled         plugin.BundledSet
	PluginTransport plugin.RoundTripper

	ascii        bool
	paths        config.Paths
	pathsErr     error
	pathsDone    bool
	settings     config.Settings
	settingsErr  error
	settingsWarn []config.Warning
	settingsDone bool
	warned       bool
	warnedKeys   bool
	printer      *i18n.Printer
	answers      *bufio.Reader
	noPlugins    bool
	registry     *plugin.Registry
}

// newApp builds the production app on the real process environment.
//
// Parameters:
//   - stdout: destination of normal output
//   - stderr: destination of errors and hints
func newApp(stdout, stderr io.Writer) *app {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return &app{
		Stdin:           os.Stdin,
		Stdout:          stdout,
		Stderr:          stderr,
		Getenv:          os.Getenv,
		Environ:         os.Environ,
		Home:            home,
		Version:         version,
		Exec:            execProgram,
		IsTerminal:      isTerminal,
		IsInputTerminal: isInputTerminal,
		UI:              ui.Run,
		Executable:      os.Executable,
		Geteuid:         os.Geteuid,
		Context:         notifyContext,
		Now:             time.Now,
		LookPath:        exec.LookPath,
		TermSize:        termSize,
		Stty:            sttySane,
		Bundled:         bundled.Set{},
	}
}

// execProgram replaces the current process by a program found on PATH.
//
// Parameters:
//   - argv: command and arguments, not empty
//   - env: environment of the new program
//
// Returns an error when the program is not found or cannot be started; on success it never
// returns.
func execProgram(argv []string, env []string) error {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}

	return syscall.Exec(path, argv, env)
}

// isCharDevice reports whether a file is a terminal-like device other than the null device.
//
// Parameters:
//   - f: open file
func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}

	null, err := os.Stat(os.DevNull)

	return err != nil || !os.SameFile(info, null)
}

// errWriter remembers the first error of the writer it wraps, so that a command that prints
// with fmt.Fprintln still learns that its output was lost, for instance on a full disk.
type errWriter struct {
	w   io.Writer
	err error
}

// Write implements io.Writer.
//
// Parameters:
//   - p: bytes to write
func (e *errWriter) Write(p []byte) (int, error) {
	n, err := e.w.Write(p)
	if err != nil && e.err == nil {
		e.err = err
	}

	return n, err
}

// isTerminal reports whether an output is a terminal.
//
// Parameters:
//   - w: output to test; anything but an *os.File is not a terminal, and an errWriter is looked
//     through
func isTerminal(w io.Writer) bool {
	if e, ok := w.(*errWriter); ok {
		w = e.w
	}

	f, ok := w.(*os.File)

	return ok && isCharDevice(f)
}

// isInputTerminal reports whether an input is a terminal.
//
// Parameters:
//   - r: input to test; anything but an *os.File is not a terminal
func isInputTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)

	return ok && isCharDevice(f)
}

// cfgPaths resolves the tote directories once.
//
// Returns the paths, or the error of config.Resolve.
func (a *app) cfgPaths() (config.Paths, error) {
	if !a.pathsDone {
		a.paths, a.pathsErr = config.Resolve(a.Getenv, a.Home)
		a.pathsDone = true
	}

	return a.paths, a.pathsErr
}

// cfg loads config.toml once.
//
// Returns the settings, or the defaults when the file cannot be used; the reason is kept in
// settingsErr so that the caller decides whether to tell the user.
func (a *app) cfg() config.Settings {
	if a.settingsDone {
		return a.settings
	}

	a.settingsDone = true
	a.settings = config.DefaultSettings()

	paths, err := a.cfgPaths()
	if err != nil {
		return a.settings
	}

	s, warns, err := config.LoadSettingsWarn(paths.ConfigFile())
	if err != nil {
		a.settingsErr = err
		return a.settings
	}

	a.settings, a.settingsWarn = s, warns

	return s
}

// cfgWarn loads the settings and tells the user once on stderr what was ignored: the whole file
// when it is unusable, else each key that was not used.
//
// Returns the settings, the defaults when config.toml is unusable.
func (a *app) cfgWarn() config.Settings {
	s := a.cfg()

	if a.settingsErr != nil && !a.warned {
		a.warned = true
		fmt.Fprintln(a.Stderr, "tote: "+a.t("cli_config_ignored", sanitize(a.settingsErr.Error())))
	}

	a.warnSettings()

	return s
}

// warnSettings prints on stderr, once, one line for each setting of config.toml that is not used
// while the others apply: the line of the file, the key and why.
func (a *app) warnSettings() {
	if a.warnedKeys {
		return
	}

	a.warnedKeys = true

	for _, w := range a.settingsWarn {
		where := "config.toml"
		if w.Line > 0 {
			where += ":" + strconv.Itoa(w.Line)
		}

		if w.Kind == config.WarnInvalidValue {
			fmt.Fprintln(a.Stderr, "tote: "+a.t("cli_config_invalid_value", where, sanitize(w.Msg)))
		} else {
			fmt.Fprintln(a.Stderr, "tote: "+a.t("cli_config_unknown_key", where, sanitize(w.Key)))
		}
	}
}

// tr returns the printer of the language chosen by the environment and the settings.
func (a *app) tr() *i18n.Printer {
	if a.printer == nil {
		a.printer = i18n.New(config.EffectiveLang(a.cfg(), a.Getenv))
	}

	return a.printer
}

// t translates a catalog key.
//
// Parameters:
//   - key: catalog key
//   - args: values for its Printf directives
func (a *app) t(key string, args ...any) string {
	return a.tr().T(key, args...)
}

// say prints a prefixed line on stderr.
//
// Parameters:
//   - key: catalog key of the message
//   - args: values for its Printf directives
func (a *app) say(key string, args ...any) {
	fmt.Fprintln(a.Stderr, "tote: "+a.t(key, args...))
}

// failMsg reports an error given as a catalog message.
//
// Parameters:
//   - code: exit code to return
//   - key: catalog key of the message
//   - args: values for its Printf directives
//
// Returns code.
func (a *app) failMsg(code int, key string, args ...any) int {
	a.say("cli_error", a.t(key, args...))

	return code
}

// fail reports an error value.
//
// Parameters:
//   - err: error to show
//
// Returns exitFail.
func (a *app) fail(err error) int {
	a.say("cli_error", a.errorText(err))

	return exitFail
}

// readOnly reports why a collection cannot be written.
//
// Parameters:
//   - err: the Err of the collection
//
// Returns exitFail.
func (a *app) readOnly(err error) int {
	var perr *store.ParseError

	if errors.As(err, &perr) {
		a.say("cli_readonly_syntax", perr.File, perr.Line, perr.Column, a.parseText(perr.Msg))
		return exitFail
	}

	return a.fail(err)
}

// usageErr reports a misuse of the command line and points to the help.
//
// Parameters:
//   - err: error of parseOptions or wantArgs
//   - verb: command whose help is suggested
//
// Returns exitUsage.
func (a *app) usageErr(err error, verb string) int {
	var oerr *optError

	if !errors.As(err, &oerr) {
		return a.fail(err)
	}

	args := a.optArgs(oerr)

	a.failMsg(exitUsage, oerr.Key, args...)
	a.say("cli_try_help", "tote help "+verb)

	return exitUsage
}

// optArgs prepares the values of an option error for its message: a value that names an
// argument or a kind of record by its catalog key is translated, any other string is sanitized.
//
// Parameters:
//   - e: error whose Args are prepared
func (a *app) optArgs(e *optError) []any {
	args := make([]any, len(e.Args))

	for i, v := range e.Args {
		args[i] = v

		s, ok := v.(string)
		if !ok {
			continue
		}

		args[i] = sanitize(s)

		if strings.HasPrefix(s, "arg_") || strings.HasPrefix(s, "kind_") {
			args[i] = a.t(s)
		}
	}

	return args
}

// notFound reports a missing record and suggests the closest name.
//
// Parameters:
//   - label: catalog key of the kind of record
//   - name: what the user typed
//   - names: names that exist
//
// Returns exitFail.
func (a *app) notFound(label, name string, names []string) int {
	a.say("cli_error", a.t("cli_not_found", a.t(label), sanitize(name)))
	a.didYouMean(name, names)

	return exitFail
}

// didYouMean prints a suggestion when a name is close to what the user typed.
//
// Parameters:
//   - word: what the user typed
//   - names: names that exist
func (a *app) didYouMean(word string, names []string) {
	if s, ok := suggest(word, names); ok {
		a.say("cli_did_you_mean", sanitize(s))
	}
}

// confirm asks the user to confirm a removal.
//
// Parameters:
//   - label: catalog key of the kind of record
//   - name: record to remove
//
// Returns true when the user agreed, else false with the exit code to return: exitUsage when
// there is no terminal to ask on and exitFail when the answer was no.
func (a *app) confirm(label, name string) (bool, int) {
	return a.askYes(a.t("cli_confirm_remove", a.t(label), sanitize(name)))
}

// askYes asks a yes or no question on the terminal, the answer being no by default.
//
// Parameters:
//   - prompt: question to print on stderr, without the trailing space
//
// Returns true when the user agreed, else false with the exit code to return: exitUsage when
// there is no terminal to ask on and exitFail when the answer was no.
func (a *app) askYes(prompt string) (bool, int) {
	return a.askYesIn(context.Background(), prompt, "cli_aborted")
}

// askYesIn is askYes with a context that interrupts the wait for the answer and the catalog key
// of the line that tells the answer was no.
//
// Parameters:
//   - ctx: cancelled by Ctrl-C while the question is open
//   - prompt: question to print on stderr, without the trailing space
//   - declined: catalog key of the line printed when the answer is not yes
//
// Returns true when the user agreed, else false with the exit code to return: exitUsage when
// there is no terminal to ask on, exitInterrupted after Ctrl-C and exitFail when the answer was
// no.
func (a *app) askYesIn(ctx context.Context, prompt, declined string) (bool, int) {
	if !a.IsInputTerminal(a.Stdin) {
		return false, a.failMsg(exitUsage, "cli_refuse_no_terminal")
	}

	fmt.Fprint(a.Stderr, prompt+" ")

	line, ok := a.readAnswer(ctx)
	if !ok {
		fmt.Fprintln(a.Stderr)

		return false, exitInterrupted
	}

	answer := strings.ToLower(strings.TrimSpace(line))

	yes := answer == "y" || answer == "yes"
	if a.tr().Lang() == "fr" {
		yes = yes || answer == "o" || answer == "oui"
	}

	if !yes {
		a.say(declined)
		return false, exitFail
	}

	return true, exitOK
}

// readAnswer reads one line of the answer to a question. The read of a context that can be
// cancelled runs beside the wait, so that Ctrl-C ends the wait at once.
//
// Parameters:
//   - ctx: cancelled by Ctrl-C
//
// Returns the line, or false when the context ended first.
func (a *app) readAnswer(ctx context.Context) (string, bool) {
	if a.answers == nil {
		a.answers = bufio.NewReader(io.LimitReader(a.Stdin, maxAnswer))
	}

	if ctx.Done() == nil {
		line, _ := a.answers.ReadString('\n')

		return line, true
	}

	lines := make(chan string, 1)

	go func() {
		line, _ := a.answers.ReadString('\n')
		lines <- line
	}()

	select {
	case line := <-lines:
		return line, true
	case <-ctx.Done():
		return "", false
	}
}

// run executes one command line and turns a failed write on standard output into a failure.
//
// Parameters:
//   - args: command line without the program name
//
// Returns the exit code of the process, exitFail when the command succeeded but its output
// could not be written.
func (a *app) run(args []string) int {
	out := a.Stdout
	ew := &errWriter{w: out}
	a.Stdout = ew

	defer func() { a.Stdout = out }()

	rc := a.dispatch(args)
	if rc == exitOK && ew.err != nil {
		return a.fail(ew.err)
	}

	return rc
}

// dispatch finds the command named by the first argument and runs it.
//
// Parameters:
//   - args: command line without the program name
//
// Returns the exit code of the command.
func (a *app) dispatch(args []string) int {
	for len(args) > 0 && (args[0] == "--ascii" || args[0] == "--no-plugins") {
		if args[0] == "--ascii" {
			a.ascii = true
		} else {
			a.noPlugins = true
		}

		args = args[1:]
	}

	if len(args) == 0 {
		if a.interactive() {
			return a.runUI(ui.StartHome)
		}

		a.rootHelp()

		return exitOK
	}

	switch args[0] {
	case "-h", "--help":
		return a.runHelp(args[1:])
	case "-v", "--version":
		return a.runVersion(args[1:])
	}

	cmd, ok := findCommand(args[0])
	if !ok {
		return a.unknownCommand(args[0])
	}

	return cmd.run(a, args[1:])
}

// unknownCommand reports a first argument that is no command.
//
// Parameters:
//   - word: the argument
//
// Returns exitUsage.
func (a *app) unknownCommand(word string) int {
	if strings.HasPrefix(word, "-") {
		a.failMsg(exitUsage, "cli_unknown_option", word)
	} else {
		a.failMsg(exitUsage, "cli_unknown_command", word)
		a.didYouMean(word, commandNames())
	}

	a.say("cli_try_help", "tote help")

	return exitUsage
}
