package ui

import (
	"errors"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// after tells what the interface does once a program it started has returned.
//
// The zero value returns to the list. Quit ends the interface; Action and Payload are then the
// hand-off for the wrapper, such as a change of directory. Quit only applies when the program
// returned 0, unless Tolerant is set: then any end that does not need the pause (an exit status
// below 126, as ssh gives back that of the last remote command) counts, while a failure to start
// or to connect leaves the user on the list. Via is set for a connection whose via is a program
// of its own: when that program cannot be found, the message explains what a via must be. Plug is
// the id of the plugin whose program ran, which words the message of the end.
type after struct {
	Quit     bool
	Tolerant bool
	Via      bool
	Plug     string
	Action   shell.Action
	Payload  string
}

// navigate handles the messages that change the screen, open an overlay or touch the data.
//
// Parameters:
//   - msg: one of openKindMsg, goHomeMsg, addMsg, editMsg, deleteMsg, reloadMsg, wroteMsg,
//     openPaletteMsg, activateMsg, editorMsg, openConfigMsg, openHelpMsg, commandMsg, copyMsg,
//     viewMsg, pickUserMsg or openPluginsMsg
func (r *root) navigate(msg tea.Msg) tea.Cmd {
	e := r.e

	r.moved()

	switch m := msg.(type) {
	case openKindMsg:
		r.scr = newList(e, m.Kind)
	case goHomeMsg:
		sel := 0

		switch l := r.scr.(type) {
		case *listScreen:
			sel = int(l.kind)
		case *pluginsScreen:
			sel = len(allKinds)
		case *configScreen:
			sel = e.homeCount() - 1
		}

		r.scr = newHomeAt(e, sel)
	case openPluginsMsg:
		r.scr = newPlugins(e, "")
	case addMsg:
		return r.openForm(m.Kind, "", false)
	case editMsg:
		return r.openForm(m.Kind, m.Key, true)
	case deleteMsg:
		if err := e.Data.err(m.Kind); err != nil {
			return r.say(e.Data.readOnlyText(m.Kind), true)
		}

		return r.push(newConfirm(m.Kind, m.Key))
	case openPaletteMsg:
		return r.push(newPalette())
	case openConfigMsg:
		return r.openConfig()
	case openHelpMsg:
		return r.push(newHelp(r.topic()))
	case commandMsg:
		return r.commandMode(m)
	case copyMsg:
		return r.copyRecord(m)
	case viewMsg:
		return r.viewRecord(m)
	case pickUserMsg:
		return r.pickUser(m)
	case reloadMsg:
		e.Data.reload()
		r.scr = r.scr.Reload(e, "")

		return r.say(e.t("ui_reloaded"), false)
	case wroteMsg:
		return r.wrote(m)
	case refreshMsg:
		e.Data.reloadKind(m.Kind)
		r.scr = r.scr.Reload(e, "")

		return nil
	case activateMsg:
		return r.activate(m)
	case editorMsg:
		return r.openInEditor(m.Key)
	}

	return nil
}

// openForm opens the form of a new or an existing record.
//
// Parameters:
//   - k: kind of the record
//   - key: raw key of the record to edit
//   - edit: true to edit the record at key, false to create one
func (r *root) openForm(k kind, key string, edit bool) tea.Cmd {
	e := r.e

	if e.Data.err(k) != nil {
		return r.say(e.Data.readOnlyText(k), true)
	}

	if !edit {
		return r.push(newForm(k, nil))
	}

	rec, ok := e.Data.record(k, key)
	if !ok {
		return r.say(e.t("cli_not_found", e.Data.kindName(k), sanitize(key)), true)
	}

	return r.push(newForm(k, rec))
}

// wrote reloads a collection after a successful write and refreshes the screen.
//
// Parameters:
//   - m: kind, key and operation of the write
func (r *root) wrote(m wroteMsg) tea.Cmd {
	e := r.e
	e.Data.reloadKind(m.Kind)

	if m.Kind != kindConn {
		r.stale = true
	}

	sel := m.Key
	text := e.t("ui_saved", e.Data.kindName(m.Kind), sanitize(m.Key))

	if m.Op == opDelete {
		sel = ""
		text = e.t("cli_removed", e.Data.kindName(m.Kind), sanitize(m.Key))
	}

	r.scr = r.scr.Reload(e, sel)

	return r.say(text, false)
}

// activate runs what Enter means for a record: connect, change directory or insert a command.
//
// Parameters:
//   - m: kind and key of the record
func (r *root) activate(m activateMsg) tea.Cmd {
	e := r.e

	rec, ok := e.Data.record(m.Kind, m.Key)
	if !ok {
		return r.say(e.t("cli_not_found", e.Data.kindName(m.Kind), sanitize(m.Key)), true)
	}

	switch v := rec.(type) {
	case store.Connection:
		return r.connect(v, m.User)
	case store.Repo:
		path, err := r.repoPath(v)
		if err != "" {
			return r.say(err, true)
		}

		return r.leaveWith(shell.ChangeDir, path)
	case store.Alias:
		return r.leaveWith(shell.Insert, v.Cmd)
	case store.Func:
		return r.leaveWith(shell.Insert, v.Name)
	}

	return nil
}

// connect starts the program of a connection. A record that needs review, or that fails its
// checks, is refused with a message and nothing is run.
//
// Parameters:
//   - c: connection to open
//   - user: login to connect as instead of the stored one, empty for the stored one
func (r *root) connect(c store.Connection, user string) tea.Cmd {
	e := r.e

	argv, why := r.argvOf(c, user)
	if why != "" {
		return r.say(why, true)
	}

	next := after{Via: shell.ViaProgram(c) != ""}
	if e.Settings.Connect.AfterDisconnect == "exit" {
		next.Quit, next.Tolerant = true, true
	}

	return r.startConnection(argv, next)
}

// argvOf builds the command of a connection.
//
// Parameters:
//   - c: connection to open
//   - user: login to connect as instead of the stored one, empty for the stored one
//
// Returns the command, or the translated reason it cannot be built.
func (r *root) argvOf(c store.Connection, user string) ([]string, string) {
	e := r.e

	argv, err := shell.ConnectArgv(c, user)

	switch {
	case errors.Is(err, shell.ErrNeedsReview):
		return nil, e.t("cli_needs_review", sanitize(c.Name))
	case errors.Is(err, shell.ErrUserOverride):
		return nil, e.t("ui_user_refused", sanitize(c.Name))
	case err != nil:
		return nil, sanitize(err.Error())
	}

	return argv, ""
}

// startConnection starts a connection. When keepalive options are to be asked for, the lookup
// runs first, off the event loop, and the connection starts from its answer; the signal guard is
// armed meanwhile so that no key starts a second one.
//
// Parameters:
//   - argv: command of the connection
//   - next: what to do when it returns
func (r *root) startConnection(argv []string, next after) tea.Cmd {
	wait := r.withKeepalive(argv, useRun, next)
	if wait == nil {
		return r.startExec(argv, next)
	}

	r.guard.begin()

	return wait
}

// useCommand does what was asked of a command whose keepalive options are known.
//
// Parameters:
//   - m: the complete command and what to do with it
func (r *root) useCommand(m probedMsg) tea.Cmd {
	switch m.Use {
	case useCopy:
		return r.copyText(shell.PrettyCommand(m.Argv))
	case useInsert:
		return r.leaveWith(shell.Insert, shell.PrettyCommand(m.Argv))
	}

	return r.runExec(m.Argv, m.Next)
}

// repoPath resolves the directory of a repository.
//
// Parameters:
//   - v: repository
//
// Returns the absolute path, or a translated message when the path is relative or is not a
// directory.
func (r *root) repoPath(v store.Repo) (string, string) {
	e := r.e
	path := shell.ExpandHome(v.Path, e.Home)

	if !filepath.IsAbs(path) {
		return "", e.t("cli_path_not_absolute")
	}

	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return "", e.t("cli_dir_not_found", sanitize(path))
	}

	return path, ""
}

// openInEditor opens a repository in the editor of the settings, then does what repos.after_open
// says: return to the list, quit, or quit and change directory.
//
// Parameters:
//   - key: raw name of the repository
func (r *root) openInEditor(key string) tea.Cmd {
	e := r.e

	rec, ok := e.Data.record(kindRepo, key)
	if !ok {
		return r.say(e.t("cli_not_found", e.Data.kindName(kindRepo), sanitize(key)), true)
	}

	path, msg := r.repoPath(rec.(store.Repo))
	if msg != "" {
		return r.say(msg, true)
	}

	argv := append(config.EffectiveEditor(e.Settings, envLookup(e.Environ)), path)

	next := after{}

	switch e.Settings.Repos.AfterOpen {
	case "quit":
		next.Quit = true
	case "cd":
		next = after{Quit: true, Action: shell.ChangeDir, Payload: path}
	}

	return r.startExec(argv, next)
}

// prepareExec builds the command that hands the terminal to a program, and the function that
// turns its end into a message.
//
// Parameters:
//   - argv: program and arguments
//   - next: what to do when the program returns
func (r *root) prepareExec(argv []string, next after) (*execCommand, tea.ExecCallback) {
	spec := RunSpec{Argv: argv, Env: r.e.Environ}

	return r.prepareSpec(spec, sanitize(shell.PrettyCommand(argv)), next)
}

// prepareSpec builds the command that hands the terminal to a program described by a spec, and
// the function that turns its end into a message.
//
// Parameters:
//   - spec: program, environment, directory, pause policy and flush
//   - label: sanitized command, shown in the banner and the pause line
//   - next: what to do when the program returns
func (r *root) prepareSpec(spec RunSpec, label string, next after) (*execCommand,
	tea.ExecCallback,
) {
	e := r.e
	banner := "  " + e.Glyphs.arrow + " " + label
	cmd := newExecCommand(r.runner, spec, banner, label, e.Tr, e.Now, r.delay)
	cmd.via = next.Via
	cmd.stop = r.guard.termPending
	cmd.waker = r.guard.waker

	return cmd, func(err error) tea.Msg {
		return execDoneMsg{
			Label: label, Result: cmd.res, Restore: err, After: next,
			Denied: cmd.denied,
		}
	}
}

// startExec returns the command that hands the terminal to a program. The signal guard is
// armed here, before Bubble Tea gets the command, so that no Ctrl-C can arrive unguarded.
//
// Parameters:
//   - argv: program and arguments
//   - next: what to do when the program returns
func (r *root) startExec(argv []string, next after) tea.Cmd {
	r.guard.begin()

	return r.runExec(argv, next)
}

// runExec returns the command that hands the terminal to a program; the signal guard must
// already be armed.
//
// Parameters:
//   - argv: program and arguments
//   - next: what to do when the program returns
func (r *root) runExec(argv []string, next after) tea.Cmd {
	cmd, done := r.prepareExec(argv, next)

	return tea.Exec(cmd, done)
}

// execDone handles the return of a program. What the program was started for (quit, change
// directory) only happens when it ran and returned 0, or ended normally when the program was a
// connection; a failed editor leaves the list in place.
//
// Parameters:
//   - m: outcome of the program
func (r *root) execDone(m execDoneMsg) tea.Cmd {
	e := r.e

	r.guard.end()

	if r.guard.takeTerm() {
		return tea.Quit
	}

	if m.After.Quit && m.Restore == nil &&
		(m.Result == (RunResult{}) || (m.After.Tolerant && !pauseNeeded(m.Result))) {
		return r.leaveWith(m.After.Action, m.After.Payload)
	}

	if m.Denied != nil {
		return r.failToast(m.Denied)
	}

	if m.Restore != nil {
		return r.say(sanitize(m.Restore.Error()), true)
	}

	if m.After.Plug != "" {
		text, bad := e.plugOutcome(m.After.Plug, m.Result)

		return r.showToast(toastMsg{Text: text, Err: bad, Hold: bad})
	}

	if text := describeExit(e.Tr, m.Label, m.Result, m.After.Via); text != "" {
		return r.say(text, pauseNeeded(m.Result))
	}

	return nil
}
