package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/ui"
	"github.com/totecli/tote/internal/update"
)

// exitInterrupted is the exit code of an interface stopped by SIGINT.
const exitInterrupted = 130

// interactive reports whether both the input and the output are terminals, which is what the
// full-screen interface needs.
func (a *app) interactive() bool {
	return a.IsTerminal(a.Stdout) && a.IsInputTerminal(a.Stdin)
}

// runUI opens the full-screen interface and leaves its result for the shell wrapper.
//
// Parameters:
//   - start: screen to open first
//
// Returns the exit code: exitInterrupted after SIGINT, exitFail when the interface could not
// run or the hand-off could not be written.
func (a *app) runUI(start ui.Start) int {
	settings := a.cfgWarn()

	if v, bad := ui.UnknownColor(a.Environ()); bad {
		fmt.Fprintln(a.Stderr, "tote: "+a.t("cli_ui_bad_color", v))
	}

	paths, err := a.cfgPaths()
	if err != nil {
		return a.fail(err)
	}

	user := a.Getenv("USER")
	if user == "" {
		user = a.Getenv("LOGNAME")
	}

	var host ui.PluginHost

	if reg, rerr := a.pluginRegistry(); rerr == nil {
		host = reg
	}

	res, err := a.UI(ui.Options{
		Paths:    paths,
		Settings: settings,
		Printer:  a.tr(),
		Version:  a.Version,
		Home:     a.Home,
		User:     user,
		Environ:  a.Environ(),
		ASCII:    a.ascii,
		Start:    start,
		Plugins:  host,

		NoWrapper:    a.Getenv("TOTE_HANDOFF") == "",
		UpdateNotice: a.statusNotice,
		CheckUpdate:  a.checkUpdate,
	})

	switch {
	case errors.Is(err, ui.ErrInterrupted):
		if !res.Empty() {
			a.leave(res)
		}

		return exitInterrupted
	case err != nil:
		return a.failMsg(exitFail, "cli_ui_failed", sanitize(err.Error()))
	}

	return a.leave(res)
}

// statusNotice finds the version that the status line announces, if a newer release exists.
//
// The result of the last check is used when there is one, and the release server is asked in
// the background only when that check is more than a day old.
//
// Parameters:
//   - ctx: ends when the interface closes
//
// Returns the version of the release, or an empty string when there is nothing to announce.
func (a *app) statusNotice(ctx context.Context) string {
	if n, ok := a.updateNotice(); ok {
		return n.Latest
	}

	n, err := a.refreshUpdateNotice(ctx)
	if err != nil || !n.Available {
		return ""
	}

	return n.Latest
}

// checkUpdate asks the release server now, for the key "U" of the interface. Unlike the check of
// the status line it ignores the age of the last check and the update.check setting, since the
// user asked for it, and it never installs anything.
//
// Parameters:
//   - ctx: ends when the interface closes or the check takes too long
//
// Returns how the installed version compares with the latest release, or an error whose text is
// the message of the catalog for the failure, in the language of the command line.
func (a *app) checkUpdate(ctx context.Context) (ui.UpdateCheck, error) {
	ic, err := a.installContext()
	if err != nil {
		return ui.UpdateCheck{}, a.updateError(fmt.Errorf("%w: %v", errExe, err), ic)
	}

	if ic.install.Managed {
		return ui.UpdateCheck{
			Status: string(update.StatusManaged), Current: a.Version,
			Command: ic.install.UpdateCommand,
		}, nil
	}

	src, err := a.updateSource()
	if err != nil {
		return ui.UpdateCheck{}, a.updateError(err, ic)
	}

	res, err := a.newUpdater(updateFlags{}, ic, src).Check(ctx)
	if err != nil {
		return ui.UpdateCheck{}, a.updateError(err, ic)
	}

	return ui.UpdateCheck{
		Status: string(res.Status), Current: res.Current, Latest: res.Latest,
	}, nil
}

// updateError turns the failure of an update check into an error that reads as a message.
//
// Parameters:
//   - err: the failure
//   - ic: description of the install
func (a *app) updateError(err error, ic installCtx) error {
	key, args := a.updateErrorMessage(err, ic)

	return errors.New(a.t(key, args...))
}

// leave passes the result of the interface to the wrapper.
//
// Parameters:
//   - res: what the shell should do
//
// Returns exitOK, or exitFail when the hand-off file is refused. Without the wrapper, or when
// the hand-off file vanished (the wrapper removes it when the interface was suspended with
// Ctrl+Z), the directory or the command line is printed so that it is not lost, and a terminal
// is told how to enable the integration.
func (a *app) leave(res ui.Result) int {
	if res.Empty() {
		return exitOK
	}

	file := a.Getenv("TOTE_HANDOFF")
	if file == "" {
		a.printUnwrapped(res)

		return exitOK
	}

	if err := shell.WriteHandoff(file, res.Handoff()); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			a.printUnwrapped(res)

			return exitOK
		}

		return a.fail(err)
	}

	return exitOK
}

// printUnwrapped shows a result the shell cannot receive.
//
// Parameters:
//   - res: what the shell should have done
//
// Only a directory change and an insertion are printed; a reload has nothing to show.
func (a *app) printUnwrapped(res ui.Result) {
	if res.Action != shell.Insert && res.Action != shell.ChangeDir {
		return
	}

	fmt.Fprintln(a.Stdout, sanitize(res.Payload))

	if a.IsTerminal(a.Stdout) {
		a.say("cli_no_wrapper")
	}
}
