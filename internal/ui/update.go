package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
)

// showNotice tells what is known about a new release without asking the server: the version the
// status line announces, or that nothing is known and how to find out.
func (r *root) showNotice() tea.Cmd {
	e := r.e

	if v := r.status.update; v != "" {
		return r.say(e.t("ui_update_found", sanitize(v)), false)
	}

	return r.say(e.t("ui_update_none", e.Keys.Check.Help().Key), false)
}

// startCheck starts a check for a new release in the background. Only one runs at a time.
func (r *root) startCheck() tea.Cmd {
	e := r.e

	switch {
	case r.check == nil:
		return r.say(e.t("ui_update_unavailable"), true)
	case r.checking:
		return r.say(e.t("ui_update_running"), false)
	}

	r.checking = true

	check, ctx := r.check, r.ctx

	run := func() tea.Msg {
		c, cancel := checkContext(ctx)
		defer cancel()

		res, err := check(c)

		return checkedMsg{Res: res, Err: err}
	}

	return tea.Batch(r.say(e.t("ui_update_checking"), false), run)
}

// endCheck shows the outcome of a check and updates the notice of the status line.
//
// Parameters:
//   - m: the answer, or the error that stopped the check
func (r *root) endCheck(m checkedMsg) tea.Cmd {
	e := r.e
	r.checking = false

	if m.Err != nil {
		if errors.Is(m.Err, errNoUpdate) {
			return r.say(e.t("ui_update_unavailable"), true)
		}

		return r.say(e.t("ui_update_failed", sanitize(e.fold(m.Err.Error()))), true)
	}

	res := m.Res

	switch res.Status {
	case "available":
		r.status.apply(e, updateMsg{Version: res.Latest})

		return r.say(e.t("ui_update_found", sanitize(res.Latest)), false)
	case "managed":
		r.status.apply(e, updateMsg{})

		return r.say(e.t("ui_update_managed", sanitize(res.Command)), false)
	case "newer":
		r.status.apply(e, updateMsg{})

		return r.say(e.t("ui_update_newer", sanitize(res.Current), sanitize(res.Latest)), false)
	case "dev":
		r.status.apply(e, updateMsg{})

		return r.say(e.t("ui_update_dev", sanitize(res.Latest)), false)
	}

	r.status.apply(e, updateMsg{})

	return r.say(e.t("ui_update_current", sanitize(res.Current)), false)
}

// errNoUpdate is the error a build without a way to check for updates returns.
var errNoUpdate = errors.New("no update check")
