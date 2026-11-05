package ui

import (
	"context"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// pluginStart names a contribution of a plugin to start: the plugin, the table and the id of the
// contribution, the saved repository it is about (empty for a command) and its label for the
// messages.
type pluginStart struct {
	Plugin string
	Table  string
	Entry  string
	Repo   string
	Label  string
}

// pluginStartMsg asks the root to start a contribution: it reads the contribution, asks for its
// form, prepares it, asks for the confirmation and runs it.
type pluginStartMsg struct {
	Start pluginStart
}

// pluginAskedMsg is the answer of the command that read a contribution: the contribution itself,
// the catalog of its plugin for the words of the form and the starting value of each field.
type pluginAskedMsg struct {
	Start    pluginStart
	Entry    plugin.Entry
	Cat      plugin.Catalog
	Defaults map[string]string
	Err      error
}

// pluginPreparedMsg is the answer of the command that prepared a contribution that has no form,
// or the end of a form whose values were accepted.
type pluginPreparedMsg struct {
	Start pluginStart
	Prep  plugin.Prepared
	Err   error
}

// pluginGoMsg is the go-ahead: the contribution was prepared and, when it asks, confirmed.
type pluginGoMsg struct {
	Start pluginStart
	Prep  plugin.Prepared
}

// openedMsg is the end of the request to open an address.
type openedMsg struct {
	Start pluginStart
	Prep  plugin.Prepared
	Out   plugin.Outcome
}

// isMono reports whether the plugins must be asked for plain text: the theme draws no color, so
// a program gets NO_COLOR and its colors are dropped from the output.
func (e *env) isMono() bool {
	return e.Theme.Mode == modeNeutral
}

// callOf builds the call that runs a contribution with the values of its form.
//
// Parameters:
//   - st: the contribution
//   - fields: the fields of its form, in order
//   - v: the values the form holds, nil for a contribution without a form
func (e *env) callOf(st pluginStart, fields []plugin.Field, v *fieldValues) plugin.Call {
	call := plugin.Call{
		Plugin: st.Plugin, Table: st.Table, Entry: st.Entry, Repo: st.Repo,
		Lang: e.Tr.Lang(), Mono: e.isMono(),
	}

	if v == nil {
		return call
	}

	call.Values = map[string]string{}
	for _, f := range fields {
		call.Values[f.ID] = v.Text(f.ID)
	}

	return call
}

// startPlugin begins the run of a contribution. The contribution is read from the files first,
// off the event loop, since its form needs the starting values that the repository gives.
//
// Parameters:
//   - m: the contribution to start
func (r *root) startPlugin(m pluginStartMsg) tea.Cmd {
	e := r.e
	if !e.pluginsOn() {
		r.settle()

		return nil
	}

	if r.run != nil || r.opening || r.guard.running() {
		r.settle()

		return r.say(e.t("ui_plug_run_wait"), true)
	}

	st, host := m.Start, e.Plugins
	call := e.callOf(st, nil, nil)
	e.Handing = true

	return func() tea.Msg {
		p, ok := host.Recheck(st.Plugin)
		if !ok || p.Manifest == nil || !p.State.Runnable() {
			_, err := host.Prepare(call)
			if err == nil {
				err = &plugin.Error{Code: plugin.CodeNotRunnable, Args: []string{st.Plugin}}
			}

			return pluginAskedMsg{Start: st, Err: err}
		}

		entries := p.Manifest.Commands
		if st.Table == plugin.TableItemAction {
			entries = p.Manifest.ItemActions
		}

		for _, en := range entries {
			if en.ID != st.Entry {
				continue
			}

			asked := pluginAskedMsg{Start: st, Entry: en, Cat: p.Catalog}
			if len(en.Fields) > 0 {
				asked.Defaults, asked.Err = host.Defaults(call)
			}

			return asked
		}

		return pluginAskedMsg{Start: st, Err: &plugin.Error{
			Code: plugin.CodeNoEntry,
			Args: []string{st.Table, st.Entry},
		}}
	}
}

// asked handles the contribution that was read: a failure is shown, a contribution with a form
// opens it, any other is prepared at once.
//
// Parameters:
//   - m: the contribution and its starting values
func (r *root) asked(m pluginAskedMsg) tea.Cmd {
	if m.Err != nil {
		r.settle()

		return r.failToast(m.Err)
	}

	if len(m.Entry.Fields) == 0 {
		return r.prepare(m.Start, nil, nil)
	}

	r.settle()

	return r.push(r.runForm(m))
}

// settle ends the hand-off of a plugin flow: the step that was waited for has shown its overlay or
// its message, so keys go to the interface again.
func (r *root) settle() {
	r.e.Handing = false
}

// prepare asks the registry to check a call and build the run, off the event loop: it reads the
// files again and may ask git for the remote of a repository.
//
// Parameters:
//   - st: the contribution
//   - fields: its fields, for the values
//   - v: the values of its form, nil when it has none
func (r *root) prepare(st pluginStart, fields []plugin.Field, v *fieldValues) tea.Cmd {
	e := r.e
	call := e.callOf(st, fields, v)
	host := e.Plugins
	e.Handing = true

	return func() tea.Msg {
		p, err := host.Prepare(call)

		return pluginPreparedMsg{Start: st, Prep: p, Err: err}
	}
}

// runForm returns the form that asks for the fields of a contribution. Enter on the last field
// prepares the run; a refusal of the registry (a value that starts with a dash, a plugin turned
// off meanwhile) is shown under the form, which stays open.
//
// Parameters:
//   - m: the contribution and its starting values
func (r *root) runForm(m pluginAskedMsg) *fieldsForm {
	e := r.e
	st, fields, host := m.Start, m.Entry.Fields, e.Plugins
	base := m.Entry.Table + "." + m.Entry.ID
	specs := commandSpecs(e, fields, m.Cat, base, m.Defaults)

	var prepared plugin.Prepared

	title := sanitize(st.Plugin) + e.colon() + " " + sanitize(st.Label)

	form := newFieldsForm(title, specs, "", "", func(v fieldValues) string {
		p, err := host.Prepare(e.callOf(st, fields, &v))
		if err != nil {
			return e.errorText(err)
		}

		prepared = p

		return ""
	})

	form.runs, form.wide = true, true
	form.after = func() tea.Cmd {
		return e.handoff(pluginPreparedMsg{Start: st, Prep: prepared})
	}

	return form
}

// prepared handles a run that is ready: it asks for the confirmation when the contribution says
// so or when it opens an address, else it starts it.
//
// Parameters:
//   - m: the run, or the reason it cannot be built
func (r *root) prepared(m pluginPreparedMsg) tea.Cmd {
	r.settle()

	if m.Err != nil {
		return r.failToast(m.Err)
	}

	if m.Prep.IsOpen() || m.Prep.Confirm.Required {
		return r.push(newRunAsk(r.e, m.Start, m.Prep))
	}

	return r.launch(m.Start, m.Prep)
}

// launch starts a run that was prepared and, when it asks, confirmed.
//
// Parameters:
//   - st: the contribution
//   - p: the prepared run
func (r *root) launch(st pluginStart, p plugin.Prepared) tea.Cmd {
	r.settle()

	switch {
	case r.run != nil || r.opening || r.guard.running():
		return r.say(r.e.t("ui_plug_run_wait"), true)
	case p.Confirm.Required && !p.Confirmed:
		return r.showToast(toastMsg{
			Text: r.e.t("ui_plug_run_unasked", sanitize(st.Plugin)),
			Err:  true, Hold: true,
		})
	case p.IsOpen():
		return r.openAddress(st, p)
	case !dirExists(p.Dir):
		return r.say(r.e.t("ui_plug_run_nodir", sanitize(st.Plugin), sanitize(p.Dir)), true)
	case p.IsTerminal():
		return r.runTerminal(st, p)
	}

	return r.runCaptured(st, p)
}

// dirExists reports whether a directory is still there.
//
// Parameters:
//   - dir: path to look at
func dirExists(dir string) bool {
	info, err := os.Stat(dir)

	return err == nil && info.IsDir()
}

// pauseOf maps the pause policy of a contribution to the one of the exec runner.
//
// Parameters:
//   - name: always, error or never; anything else is always, the default of the plugins
func pauseOf(name string) PausePolicy {
	if p, ok := ParsePause(name); ok {
		return p
	}

	return PauseAlways
}

// runTerminal hands the terminal to the program of a contribution through the exec path of the
// connections: the same banner, mode reset, signal guard and pause. It differs in three things
// that a plugin needs: the working directory and the environment come from the registry, the
// keys typed meanwhile are discarded before and after, so that nothing typed while a program
// ran is replayed into the interface, and the pause follows the contribution (always unless it
// says otherwise).
//
// The state of the plugin is checked right here and once more in the exec command, immediately
// before the program starts.
//
// Parameters:
//   - st: the contribution
//   - p: the prepared run
func (r *root) runTerminal(st pluginStart, p plugin.Prepared) tea.Cmd {
	if g := r.e.Plugins.Guard(p); g != nil {
		return r.failToast(g)
	}

	r.guard.begin()

	return tea.Exec(r.terminalCommand(st, p))
}

// terminalCommand builds the command that hands the terminal to the program of a contribution,
// and the function that turns its end into a message. The command refuses to start when the gate
// finds the plugin changed since the run was prepared.
//
// Parameters:
//   - st: the contribution
//   - p: the prepared run
func (r *root) terminalCommand(st pluginStart, p plugin.Prepared) (*execCommand,
	tea.ExecCallback,
) {
	spec := RunSpec{Argv: p.Argv, Env: p.Env, Dir: p.Dir, Pause: pauseOf(p.Pause), Flush: true}
	host := r.e.Plugins

	cmd, done := r.prepareSpec(spec, sanitize(p.Display), after{Plug: st.Plugin})
	cmd.gate = func() error {
		if g := host.Guard(p); g != nil {
			return g
		}

		return nil
	}

	return cmd, done
}

// openAddress asks the platform to open the address of a contribution, after the user said yes.
// The registry waits for the opener a few seconds, so it runs off the event loop.
//
// Parameters:
//   - st: the contribution
//   - p: the prepared run
func (r *root) openAddress(st pluginStart, p plugin.Prepared) tea.Cmd {
	e := r.e
	host := e.Plugins
	r.opening = true

	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	return tea.Batch(r.say(e.t("ui_plug_run_opening", sanitize(st.Plugin)), false),
		func() tea.Msg {
			return openedMsg{Start: st, Prep: p, Out: host.Run(ctx, p, nil)}
		})
}

// opened handles the end of the request to open an address.
//
// Parameters:
//   - m: the outcome
func (r *root) opened(m openedMsg) tea.Cmd {
	r.opening = false

	text, bad := r.e.outcomeText(m.Start.Plugin, m.Out, m.Prep.Timeout)
	if !bad {
		text = r.e.t("ui_plug_run_opened", sanitize(m.Start.Plugin))
	}

	return r.showToast(toastMsg{Text: text, Err: bad, Hold: bad})
}

// plugOutcome gives the message for the end of a program that ran in the terminal.
//
// Parameters:
//   - id: the plugin
//   - res: what the program did
func (e *env) plugOutcome(id string, res RunResult) (string, bool) {
	who := sanitize(id)

	switch {
	case res.Err != nil:
		return e.t("ui_plug_run_nostart", who, sanitize(res.Err.Error())), true
	case res.Signal != "":
		return e.t("ui_plug_run_signal", who, sanitize(strings.TrimPrefix(res.Signal,
			"signal "))), true
	case res.ExitCode != 0:
		return e.t("ui_plug_run_failed", who, res.ExitCode), true
	}

	return e.t("ui_plug_run_done", who), false
}

// outcomeText gives the message for the end of a captured run or of a request to open an address.
//
// Parameters:
//   - id: the plugin
//   - out: how the run ended
//   - limit: the time limit the run had, for the message of a timeout
//
// Returns the message, in the language of the interface and clean, and true when it is a failure.
func (e *env) outcomeText(id string, out plugin.Outcome, limit time.Duration) (string, bool) {
	who := sanitize(id)

	switch out.Status {
	case plugin.OutcomeOK:
		return e.t("ui_plug_run_done", who), false
	case plugin.OutcomeCancelled:
		return e.t("ui_plug_run_stopped", who), true
	case plugin.OutcomeTimeout:
		return e.t("ui_plug_run_timeout", who, limit.Round(time.Second).String()), true
	case plugin.OutcomeSignaled:
		return e.t("ui_plug_run_signal", who, sigWord(out.Signal)), true
	case plugin.OutcomeFailed:
		if out.Err != nil && out.Err.Code == plugin.CodeOutputLimit {
			return e.errorText(out.Err), true
		}

		if out.Err != nil {
			return who + e.colon() + " " + e.errorText(out.Err), true
		}

		return e.t("ui_plug_run_failed", who, out.Exit), true
	}

	return e.refusalText(who, out.Err), true
}

// sigWord gives the name of a signal as the plugin package reports it, such as TERM or SIG10, in
// the form SIGTERM.
//
// Parameters:
//   - name: short name of the signal
func sigWord(name string) string {
	name = sanitize(name)
	if strings.HasPrefix(name, "SIG") {
		return name
	}

	return "SIG" + name
}

// refusalText words a run that did not start, with the plugin first.
//
// Parameters:
//   - who: the plugin, clean
//   - err: why it did not start, may be nil
func (e *env) refusalText(who string, err *plugin.Error) string {
	if err == nil {
		return e.t("ui_plug_run_nostart", who, "")
	}

	switch err.Code {
	case plugin.CodeBusy:
		return who + e.colon() + " " + e.t("ui_plug_run_busy")
	case plugin.CodeStartFailed:
		return e.t("ui_plug_run_nostart", who, causeText(err.Err))
	}

	return who + e.colon() + " " + e.errorText(err)
}
