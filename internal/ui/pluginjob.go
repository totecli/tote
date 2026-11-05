package ui

import (
	"context"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// busyWidth is the widest the box of the wait for a job gets, in cells.
const busyWidth = 48

// pluginJob is the background job that the interface waits for.
//
// Seq tells it from an older one, Cancel ends its context and Write tells that it changes
// something, so that quitting while it runs asks first. A job that is cancelled leaves its answer
// to be dropped when it comes.
type pluginJob struct {
	seq    int
	op     jobOp
	id     string
	cancel context.CancelFunc
	write  bool
	stop   bool
}

// busyKeys maps a job to the catalog key of the sentence that says what it is doing.
var busyKeys = map[jobOp]string{
	jobPlan:    "ui_plug_busy_plan",
	jobCommit:  "ui_plug_busy_commit",
	jobAllow:   "ui_plug_busy_allow",
	jobEnable:  "ui_plug_busy_enable",
	jobGrant:   "ui_plug_busy_grant",
	jobDisable: "ui_plug_busy_disable",
	jobRemove:  "ui_plug_busy_remove",
	jobCheck:   "ui_plug_busy_check",
}

// busyOverlay is the overlay that waits for a background job. Nothing else can be done while it is
// open; Esc gives up a job that can be given up, one that only reads or copies into the staging
// area, and a job that changes the installed plugins cannot be cancelled.
type busyOverlay struct {
	seq    int
	text   string
	cancel bool
}

// newBusy returns the overlay that waits for a job.
//
// Parameters:
//   - e: shared environment, for the text
//   - j: the job
func newBusy(e *env, j pluginJob) overlay {
	return &busyOverlay{seq: j.seq, text: e.t(busyKeys[j.op], sanitize(j.id)), cancel: j.stop}
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (b *busyOverlay) Init(_ *env) tea.Cmd {
	return nil
}

// Update implements overlay: Esc gives up a job that can be given up.
//
// Parameters:
//   - e: shared environment, unused
//   - msg: key press; anything else is ignored
func (b *busyOverlay) Update(_ *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	if k, ok := msg.(tea.KeyPressMsg); ok && b.cancel && k.String() == "esc" {
		return b, msgCmd(jobCancelMsg{Seq: b.seq}), false
	}

	return b, nil, false
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (b *busyOverlay) View(e *env, maxW, maxH int) []string {
	w := max(4, min(maxW, busyWidth))
	lines := wrapWords(e, b.text, w-4, max(1, maxH-2))

	rows := make([]string, len(lines))
	for i, l := range lines {
		rows[i] = " " + e.Theme.Text.Render(l)
	}

	return floating(e, e.Theme.Bold.Render(e.t("ui_plug_busy_title")), rows, w)
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (b *busyOverlay) Keys(e *env) []key.Binding {
	if !b.cancel {
		return nil
	}

	return []key.Binding{
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", e.t("ui_help_cancel"))),
	}
}

// startJob runs a piece of work off the event loop and shows the overlay that waits for it.
//
// Parameters:
//   - j: the job, its seq filled in here
//   - run: the work; it gets a context that Esc ends for a job that can be cancelled, and must not
//     touch the model
//
// Returns the command, or nil when a job is already running.
func (r *root) startJob(j pluginJob, run func(ctx context.Context) pluginDoneMsg) tea.Cmd {
	if r.job != nil {
		return nil
	}

	r.jobSeq++

	parent := r.ctx
	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithCancel(parent)
	j.seq, j.cancel = r.jobSeq, cancel
	r.job = &j

	if j.write {
		r.e.writing(1)
	}

	show := r.push(newBusy(r.e, j))

	return tea.Batch(show, func() tea.Msg {
		m := run(ctx)
		m.Seq, m.Op, m.ID = j.seq, j.op, j.id

		return m
	})
}

// dropBusy removes the overlay that waits for a job from the stack.
//
// Parameters:
//   - seq: the job
func (r *root) dropBusy(seq int) {
	r.overlays = slices.DeleteFunc(r.overlays, func(ov overlay) bool {
		b, ok := ov.(*busyOverlay)

		return ok && b.seq == seq
	})
	r.moved()
}

// cancelJob gives up the job that is running, when it can be given up.
//
// Parameters:
//   - seq: the job the overlay waits for
func (r *root) cancelJob(seq int) tea.Cmd {
	j := r.job
	if j == nil || j.seq != seq || !j.stop {
		return nil
	}

	j.cancel()
	r.job = nil
	r.dropBusy(seq)

	return r.say(r.e.t("cli_plugin_declined"), false)
}

// closeOverlays lets the overlays that hold something outside the interface give it back, when
// the interface ends: a package waiting in the staging area is deleted.
func (r *root) closeOverlays() {
	for _, ov := range r.overlays {
		if c, ok := ov.(interface{ abort() }); ok {
			c.abort()
		}
	}

	if r.job != nil {
		r.job.cancel()
	}

	r.endRun()
}

// fieldsSaved hands the end of a save to the form that started it, wherever it is in the stack: the
// help or another box may have been opened over it meanwhile.
//
// Parameters:
//   - m: the end of the save
func (r *root) fieldsSaved(m fieldsSavedMsg) tea.Cmd {
	r.e.pluginsChanged()

	for i, ov := range r.overlays {
		if f, ok := ov.(*fieldsForm); !ok || f != m.Form {
			continue
		}

		next, cmd, done := ov.Update(r.e, m)

		if done {
			r.overlays = slices.Delete(r.overlays, i, i+1)
			r.moved()
		} else {
			r.overlays[i] = next
		}

		return cmd
	}

	return nil
}

// failToast shows the failure of an operation of the plugin layer and keeps it on screen long
// enough to be read.
//
// Parameters:
//   - err: the error
func (r *root) failToast(err error) tea.Cmd {
	return r.showToast(toastMsg{Text: r.e.errorText(err), Err: true, Hold: true})
}

// doPlugin handles a request for an action on a plugin.
//
// Parameters:
//   - m: the action and the id of the plugin
func (r *root) doPlugin(m pluginActMsg) tea.Cmd {
	e := r.e
	if !e.pluginsOn() || r.job != nil {
		return nil
	}

	switch m.Act {
	case actDetails:
		return r.push(newPluginInfo(e, m.ID))
	case actMenu:
		return r.push(newPluginMenu(e, m.ID))
	case actInstall:
		return r.install(m.ID)
	case actUpdate:
		return r.plan(plugin.InstallRequest{ID: m.ID, Update: true, Lang: e.Tr.Lang()}, m.ID)
	case actCheck:
		return r.checkPlugin(m.ID)
	}

	p, ok := e.Plugins.Recheck(m.ID)
	if !ok {
		r.e.pluginsChanged()
		r.reloadPlugins(m.ID)

		return r.showToast(toastMsg{Text: e.codeText(plugin.CodeNotInstalled, []string{m.ID},
			nil), Err: true})
	}

	if m.Act == actToggle && m.Seen != "" && toggleKey(m.Seen) != toggleKey(string(p.State)) {
		r.e.pluginsChanged()
		r.reloadPlugins(m.ID)

		return r.showToast(toastMsg{Text: e.t("ui_plug_changed", sanitize(m.ID)), Err: true})
	}

	switch m.Act {
	case actToggle:
		return r.toggle(p)
	case actAllow:
		return r.allow(p.ID, false)
	case actSettings:
		return r.openSettings(p)
	case actRemove, actPurge:
		return r.push(newAskRemove(e, p.ID, m.Act == actPurge))
	case actClear:
		return r.startJob(pluginJob{op: jobEnable, id: p.ID, write: true},
			func(context.Context) pluginDoneMsg {
				res, err := e.Plugins.Enable(p.ID, plugin.Grant{})

				return pluginDoneMsg{Res: res, Err: err}
			})
	}

	return nil
}

// install stages a plugin shipped with tote.
//
// Parameters:
//   - id: plugin id
func (r *root) install(id string) tea.Cmd {
	e := r.e

	if _, installed := e.Plugins.Recheck(id); installed {
		return r.say(e.t("ui_plug_installed_already"), false)
	}

	return r.plan(plugin.InstallRequest{Source: id, Lang: e.Tr.Lang()}, id)
}

// plan starts the copy of a package into the staging area.
//
// Parameters:
//   - req: what to install or to update
//   - id: plugin id, for the wait
func (r *root) plan(req plugin.InstallRequest, id string) tea.Cmd {
	host := r.e.Plugins

	return r.startJob(pluginJob{op: jobPlan, id: id, stop: true},
		func(ctx context.Context) pluginDoneMsg {
			st, err := host.Plan(ctx, req)

			return pluginDoneMsg{Staged: st, Err: err}
		})
}

// checkPlugin starts the examination of a plugin.
//
// Parameters:
//   - id: plugin id
func (r *root) checkPlugin(id string) tea.Cmd {
	host, lang := r.e.Plugins, r.e.Tr.Lang()

	return r.startJob(pluginJob{op: jobCheck, id: id, stop: true},
		func(ctx context.Context) pluginDoneMsg {
			res, err := host.Check(ctx, plugin.CheckRequest{Target: id, Lang: lang})

			return pluginDoneMsg{Res: res, Err: err}
		})
}

// toggle enables a plugin that is not enabled, with the consent it needs, and disables one that
// is.
//
// Parameters:
//   - p: the plugin, read just now
func (r *root) toggle(p plugin.Plugin) tea.Cmd {
	host := r.e.Plugins

	switch p.State {
	case plugin.StateEnabled, plugin.StateMissingDependency:
		return r.startJob(pluginJob{op: jobDisable, id: p.ID, write: true},
			func(context.Context) pluginDoneMsg {
				res, err := host.Disable(p.ID)

				return pluginDoneMsg{Res: res, Err: err}
			})
	}

	return r.allow(p.ID, true)
}

// allow builds the view of what an installed plugin asks for. The answer opens the consent
// overlay, or, for a plugin that asks nothing, enables it at once.
//
// Parameters:
//   - id: plugin id
//   - enable: true to enable the plugin once it is allowed, false to record the consent only
func (r *root) allow(id string, enable bool) tea.Cmd {
	host, lang := r.e.Plugins, r.e.Tr.Lang()

	job := pluginJob{op: jobAllow, id: id, stop: true}

	return r.startJob(job, func(context.Context) pluginDoneMsg {
		d, err := host.Allow(id)
		if err != nil {
			return pluginDoneMsg{Err: err}
		}

		if p, ok := host.Recheck(id); ok && p.Manifest != nil {
			d.Title = r.e.fold(p.Catalog.Text(lang, "title", d.Title))
			d.Description = r.e.fold(p.Catalog.Text(lang, "description", d.Description))
		}

		return pluginDoneMsg{Res: allowed{diff: d, enable: enable}}
	})
}

// allowed is the answer of a job that built the view of an installed plugin.
type allowed struct {
	diff   plugin.Diff
	enable bool
}

// granted handles the consent: the staged package is committed, or the installed plugin is
// allowed and, when asked, enabled.
//
// Parameters:
//   - m: the consent
func (r *root) granted(m pluginGrantMsg) tea.Cmd {
	host := r.e.Plugins
	g := plugin.Grant{Prompt: true, Token: m.Token}

	if m.Staged != nil {
		return r.startJob(pluginJob{op: jobCommit, id: m.ID, write: true},
			func(context.Context) pluginDoneMsg {
				res, err := m.Staged.Commit(g)

				return pluginDoneMsg{Res: res, Err: err}
			})
	}

	if m.Enable {
		return r.startJob(pluginJob{op: jobEnable, id: m.ID, write: true},
			func(context.Context) pluginDoneMsg {
				res, err := host.Enable(m.ID, g)

				return pluginDoneMsg{Res: res, Err: err}
			})
	}

	return r.startJob(pluginJob{op: jobGrant, id: m.ID, write: true},
		func(context.Context) pluginDoneMsg {
			res, err := host.Grant(m.ID, g)

			return pluginDoneMsg{Res: res, Err: err}
		})
}

// removePlugin deletes a plugin after the question was answered with yes.
//
// Parameters:
//   - m: the plugin and whether its settings and data go too
func (r *root) removePlugin(m pluginRemoveMsg) tea.Cmd {
	host := r.e.Plugins

	return r.startJob(pluginJob{op: jobRemove, id: m.ID, write: true},
		func(context.Context) pluginDoneMsg {
			res, err := host.Remove(m.ID, plugin.RemoveOptions{Purge: m.Purge})

			return pluginDoneMsg{Res: res, Err: err}
		})
}

// jobDone handles the end of a background job: the overlay that waited goes away and the result is
// shown. The answer of a job that was given up is dropped, and a package it staged is deleted.
//
// Parameters:
//   - m: the answer
func (r *root) jobDone(m pluginDoneMsg) tea.Cmd {
	j := r.job
	if j == nil || j.seq != m.Seq {
		if m.Staged != nil {
			_ = m.Staged.Abort()
		}

		return nil
	}

	r.job = nil
	j.cancel()

	if j.write {
		r.e.writing(-1)
	}

	r.dropBusy(j.seq)
	r.e.pluginsChanged()

	if m.Err != nil {
		if m.Staged != nil {
			_ = m.Staged.Abort()
		}

		r.reloadPlugins(m.ID)

		return r.failToast(m.Err)
	}

	cmd := r.jobResult(m)
	r.reloadPlugins(m.ID)

	return cmd
}

// reloadPlugins reads the plugins again when the plugins screen is the one on show.
//
// Parameters:
//   - id: plugin to select afterwards
func (r *root) reloadPlugins(id string) {
	if _, here := r.scr.(*pluginsScreen); here {
		r.scr = r.scr.Reload(r.e, id)
	}
}

// jobResult shows the outcome of a job that worked.
//
// Parameters:
//   - m: the answer
func (r *root) jobResult(m pluginDoneMsg) tea.Cmd {
	e := r.e
	id := sanitize(m.ID)

	switch m.Op {
	case jobPlan:
		return r.staged(m.Staged)
	case jobAllow:
		a := m.Res.(allowed)

		switch {
		case !a.diff.NeedsConsent && a.enable:
			return r.granted(pluginGrantMsg{ID: a.diff.ID, Enable: true})
		case !a.diff.NeedsConsent:
			return r.say(e.t("ui_plug_nothing_to_allow", id), false)
		}

		return r.push(newConsent(e, a.diff, plugin.ViewAllow, nil, a.enable))
	case jobCheck:
		return r.push(newCheckReport(e, m.Res.(*plugin.CheckResult)))
	case jobCommit:
		return r.say(r.installedText(m.Res.(plugin.InstallResult)), false)
	case jobEnable:
		res := m.Res.(plugin.EnableResult)

		return r.showToast(toastMsg{Text: r.enabledText(id, res), Hold: res.ConfigFile != ""})
	case jobGrant:
		if m.Res.(plugin.GrantResult).Already {
			return r.say(e.t("ui_plug_nothing_to_allow", id), false)
		}

		return r.say(e.t("ui_plug_allowed", id), false)
	case jobDisable:
		return r.say(r.disabledText(id, m.Res.(plugin.DisableResult)), false)
	case jobRemove:
		return r.say(r.removedText(id, m.Res.(plugin.RemoveResult)), false)
	}

	return nil
}

// staged shows what a staged package will do. A package that asks for no consent is installed at
// once; otherwise the consent overlay asks.
//
// Parameters:
//   - st: the staged package
func (r *root) staged(st *plugin.Staged) tea.Cmd {
	d := st.Diff()
	mode := plugin.ViewInstall

	if d.Update {
		mode = plugin.ViewUpdate
	}

	if !d.NeedsConsent {
		return r.granted(pluginGrantMsg{ID: d.ID, Staged: st})
	}

	return r.push(newConsent(r.e, d, mode, st, false))
}

// installedText words the end of an install or an update.
//
// Parameters:
//   - res: what the commit did
func (r *root) installedText(res plugin.InstallResult) string {
	e := r.e
	id, state := sanitize(res.ID), e.pstate(string(res.State))

	switch {
	case res.Updated && res.ConsentedBy == "":
		return e.t("plugin_updated_quiet", id, sanitize(res.OldVersion), sanitize(res.Version))
	case res.Updated:
		return e.t("cli_plugin_updated", id, sanitize(res.OldVersion), sanitize(res.Version),
			state)
	}

	return e.t("plugin_installed", id, sanitize(res.Version), state)
}

// enabledText words the end of an enable.
//
// Parameters:
//   - id: plugin id, sanitized
//   - res: what the enable did
func (r *root) enabledText(id string, res plugin.EnableResult) string {
	e := r.e

	if res.ConfigFile != "" {
		return e.t("cli_plugin_config_layout_enable", sanitize(res.ConfigFile), id)
	}

	text := e.t("cli_plugin_enabled", id)
	if res.Already {
		text = e.t("cli_plugin_already_enabled", id)
	}

	if res.State != plugin.StateEnabled {
		text += " (" + e.pstate(string(res.State)) + ")"
	}

	return text
}

// disabledText words the end of a disable.
//
// Parameters:
//   - id: plugin id, sanitized
//   - res: what the disable did
func (r *root) disabledText(id string, res plugin.DisableResult) string {
	e := r.e

	text := e.t("cli_plugin_disabled", id)
	if res.Already {
		text = e.t("cli_plugin_already_disabled", id)
	}

	if res.Killed > 0 {
		text += " " + e.t("cli_plugin_stopped", res.Killed)
	}

	return text
}

// removedText words the end of a removal.
//
// Parameters:
//   - id: plugin id, sanitized
//   - res: what the removal did
func (r *root) removedText(id string, res plugin.RemoveResult) string {
	e := r.e
	text := e.t("cli_plugin_removed", id)

	if res.Killed > 0 {
		text += " " + e.t("cli_plugin_stopped", res.Killed)
	}

	if len(res.Kept) > 0 {
		words := make([]string, len(res.Kept))
		for i, k := range res.Kept {
			words[i] = e.pword("plugin_left_", k)
		}

		text += " " + e.t("cli_plugin_removed_kept", strings.Join(words, ", "))
	}

	return text
}

// newAskRemove returns the question that comes before a plugin is removed. It lists what goes, and
// "No" is the default answer.
//
// Parameters:
//   - e: shared environment, for the texts
//   - id: plugin id
//   - purge: true when the settings and the data go too
func newAskRemove(e *env, id string, purge bool) overlay {
	details := []string{e.t("cli_plugin_goes_base")}
	if purge {
		details = append(details, e.t("cli_plugin_goes_purge"))
	}

	return &ask{
		title:   e.t("ui_plug_remove_title"),
		text:    e.t("ui_plug_remove_text", sanitize(id)),
		details: details,
		yes:     pluginRemoveMsg{ID: id, Purge: purge},
	}
}
