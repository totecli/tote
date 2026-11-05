package ui

import (
	"slices"
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// repoMenuMsg asks the root to open the menu of the actions that plugins offer on a saved
// repository.
type repoMenuMsg struct {
	Key string
}

// repoMenuReadyMsg is the answer of the command that read the actions of a repository: what each
// enabled plugin offers on it, and whether it can run now. Blocked lists, when there is no offer,
// the plugins that have actions on a repository but cannot run because a program they need is not
// installed.
type repoMenuReadyMsg struct {
	Key     string
	Offers  []plugin.Offer
	Blocked []plugin.Plugin
}

// actionMenu is the menu of the actions that plugins offer on one repository. An action that
// cannot run now stays in the list, muted, with the reason at its right; Enter on it shows the
// reason in full and starts nothing. It is a list of choices like the others, so it scrolls, takes
// clicks and fits a terminal of 24 lines by 7.
//
// Repo is the raw name of the repository and Offers the actions in the order of the list. Why is
// the reason each one cannot run, empty when it can, and Held is true while the status line still
// shows a reason that Enter asked for.
type actionMenu struct {
	*pickOverlay
	repo   string
	offers []plugin.Offer
	why    []string
	held   bool
}

// newActionMenu returns the menu of the actions of a repository.
//
// Parameters:
//   - e: shared environment, for the words of the reasons
//   - repo: raw name of the repository
//   - offers: the actions, at least one
func newActionMenu(e *env, repo string, offers []plugin.Offer) *actionMenu {
	m := &actionMenu{repo: repo, offers: offers, why: make([]string, len(offers))}
	items := make([]pickItem, len(offers))
	lead := slices.ContainsFunc(offers, func(o plugin.Offer) bool { return o.Key != "" })

	for i, o := range offers {
		label := sanitize(e.fold(o.Label))
		if o.Form || o.Confirm {
			label += e.Glyphs.ellipsis
		}

		if lead {
			label = e.Metric.pad(sanitize(o.Key), 1, e.Glyphs.ellipsis) + "  " + label
		}

		items[i] = pickItem{label: label, value: strconv.Itoa(i), note: sanitize(o.Plugin)}

		if !o.Available && o.Why != nil {
			m.why[i] = e.errorText(&plugin.Error{Code: o.Why.Code, Args: o.Why.Args})
			items[i].off, items[i].note = true, m.why[i]
		}
	}

	m.pickOverlay = &pickOverlay{
		title: e.t("ui_plug_act_title", sanitize(repo)), items: items,
		done: func(e *env, value string) tea.Cmd {
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || n >= len(offers) {
				return nil
			}

			o := offers[n]

			return e.handoff(pluginStartMsg{Start: pluginStart{
				Plugin: o.Plugin, Table: o.Table,
				Entry: o.ID, Repo: repo, Label: sanitize(e.fold(o.Label)),
			}})
		},
	}

	return m
}

// Update implements overlay: everything is the list of choices, except that an action that cannot
// run is not started: its reason is shown instead.
//
// Parameters:
//   - e: shared environment
//   - msg: key press, click, wheel or dismissal
func (m *actionMenu) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	before := m.cur

	switch k := msg.(type) {
	case tea.KeyPressMsg:
		if i := m.current(); key.Matches(k, e.Keys.Apply) && i >= 0 && m.why[i] != "" {
			return m, m.refuse(e), false
		}

		if n := m.accelerated(e, k); n >= 0 {
			m.cur = n
			m.off = scrollTo(n, m.off, m.rows(e.Height-2), len(m.items))

			if m.why[n] != "" {
				return m, m.refuse(e), false
			}

			return m, m.done(e, m.items[n].value), true
		}
	case itemClickMsg:
		if k.Double && k.ID >= 0 && k.ID < len(m.why) && m.why[k.ID] != "" {
			m.pickOverlay.Update(e, itemClickMsg{ID: k.ID})
			m.cur = k.ID

			return m, m.refuse(e), false
		}
	}

	_, cmd, done := m.pickOverlay.Update(e, msg)

	if m.held && m.cur != before {
		m.held = false

		return m, tea.Batch(cmd, msgCmd(toastMsg{})), done
	}

	return m, cmd, done
}

// accelerated returns the action that a key is the accelerator of, -1 when it is none. The keys
// of the list itself win, and when two actions ask for the same key the first one has it.
//
// Parameters:
//   - e: shared environment, for the key map
//   - k: key pressed
func (m *actionMenu) accelerated(e *env, k tea.KeyPressMsg) int {
	for _, b := range []key.Binding{
		e.Keys.Up, e.Keys.Down, e.Keys.Home, e.Keys.End, e.Keys.Apply,
		e.Keys.Close,
	} {
		if key.Matches(k, b) {
			return -1
		}
	}

	for i, o := range m.offers {
		if o.Key != "" && o.Key == k.String() {
			return i
		}
	}

	return -1
}

// current returns the position of the choice under the cursor.
func (m *actionMenu) current() int {
	if m.cur < 0 || m.cur >= len(m.items) {
		return -1
	}

	return m.cur
}

// refuse returns the command that shows why the choice under the cursor cannot run.
//
// Parameters:
//   - e: shared environment
func (m *actionMenu) refuse(e *env) tea.Cmd {
	o := m.offers[m.current()]
	m.held = true

	text := sanitize(o.Plugin) + e.colon() + " " + m.why[m.current()]

	return msgCmd(toastMsg{Text: text, Err: true, Hold: true})
}

// Status implements statuser: the status line gives the reason that the action under the cursor
// cannot run, in full.
//
// Parameters:
//   - e: shared environment, unused
func (m *actionMenu) Status(_ *env) statusInfo {
	if i := m.current(); i >= 0 {
		return statusInfo{Command: m.why[i]}
	}

	return statusInfo{}
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (m *actionMenu) Keys(e *env) []key.Binding {
	return pickBindings(e)
}

// repoMenu reads what plugins offer on a repository, off the event loop since it reads the
// plugins again and may ask git for the remote.
//
// Parameters:
//   - m: the repository
func (r *root) repoMenu(m repoMenuMsg) tea.Cmd {
	e := r.e
	if !e.pluginsOn() {
		r.settle()

		return nil
	}

	host, lang := e.Plugins, e.Tr.Lang()

	return func() tea.Msg {
		snap := host.Load()
		ready := repoMenuReadyMsg{Key: m.Key, Offers: host.RepoOffers(snap, m.Key, lang)}

		if len(ready.Offers) == 0 {
			ready.Blocked = blockedPlugins(snap)
		}

		return ready
	}
}

// blockedPlugins lists the plugins of a snapshot that have an action on a repository and are held
// back by a program that is not installed.
//
// Parameters:
//   - snap: what Load found
func blockedPlugins(snap plugin.Snapshot) []plugin.Plugin {
	var out []plugin.Plugin

	for _, p := range snap.Plugins() {
		if p.State != plugin.StateMissingDependency || p.Manifest == nil || len(p.Missing) == 0 {
			continue
		}

		if slices.ContainsFunc(p.Manifest.ItemActions, func(en plugin.Entry) bool {
			return en.Target == "repo"
		}) {
			out = append(out, p)
		}
	}

	return out
}

// menuReady opens the menu of the actions of a repository, or says that there are none.
//
// Parameters:
//   - m: what the plugins offer
func (r *root) menuReady(m repoMenuReadyMsg) tea.Cmd {
	r.settle()

	if len(m.Offers) == 0 {
		if len(m.Blocked) > 0 {
			p, rq := m.Blocked[0], m.Blocked[0].Missing[0]
			why := r.e.errorText(&plugin.Error{
				Code: plugin.CodeDepMissing,
				Args: []string{rq.Bin, rq.Hint},
			})

			return r.showToast(toastMsg{
				Text: sanitize(p.ID) + r.e.colon() + " " + why, Err: true,
				Hold: true,
			})
		}

		return r.say(r.e.t("ui_plug_act_none"), false)
	}

	if len(r.overlays) > 0 {
		if _, again := r.overlays[len(r.overlays)-1].(*actionMenu); again {
			return nil
		}
	}

	return r.push(newActionMenu(r.e, m.Key, m.Offers))
}
