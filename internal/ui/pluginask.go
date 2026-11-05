package ui

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// runAskWidth is the widest the box of the question before a run gets, in cells.
const runAskWidth = 72

// runAsk is the overlay that asks before a contribution runs: it names the plugin and the
// contribution, asks the question of the manifest (or the generic one) and shows the exact command
// and the directory it runs in, or the address it opens. The focus starts on Cancel and the
// letter y does nothing, so that Enter alone never runs anything and no bare letter confirms.
//
// Start is the contribution and Prep the run that Yes releases; Yes tells which button has the
// focus. Cut is true when the last draw had no room for the whole command: the run is then refused,
// since a command that cannot be read is not one to say yes to.
type runAsk struct {
	start pluginStart
	prep  plugin.Prepared
	yes   bool
	cut   bool
}

// newRunAsk returns the question that comes before a run.
//
// Parameters:
//   - e: shared environment, unused
//   - st: the contribution
//   - p: the prepared run; Yes sets Confirmed on it
func newRunAsk(_ *env, st pluginStart, p plugin.Prepared) overlay {
	return &runAsk{start: st, prep: p}
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (a *runAsk) Init(_ *env) tea.Cmd {
	return nil
}

// answer returns the command that releases the run, with the confirmation recorded.
//
// Parameters:
//   - e: shared environment, for the hand-off to the run
func (a *runAsk) answer(e *env) tea.Cmd {
	p := a.prep
	p.Confirmed = true

	return e.handoff(pluginGoMsg{Start: a.start, Prep: p})
}

// Update implements overlay: left and right choose a button and stay on it, Tab changes the
// button, Enter picks the one that has the focus, n and Esc cancel. There is no key that answers
// yes by itself.
//
// Parameters:
//   - e: shared environment, for the hand-off to the run
//   - msg: key press, click or dismissal; other messages are ignored
func (a *runAsk) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return a, nil, true
	case itemClickMsg:
		switch m.ID {
		case confirmNo:
			return a, nil, true
		case confirmYes:
			return a.pick(e)
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "n", "N", "esc":
			return a, nil, true
		case "enter":
			if a.yes {
				return a.pick(e)
			}

			return a, nil, true
		case "left", "h":
			a.yes = false
		case "right", "l":
			a.yes = true
		case "tab", "shift+tab":
			a.yes = !a.yes
		}
	}

	return a, nil, false
}

// pick answers yes, unless the command could not be shown whole.
//
// Parameters:
//   - e: shared environment, for the hand-off to the run
func (a *runAsk) pick(e *env) (overlay, tea.Cmd, bool) {
	if a.cut {
		return a, nil, false
	}

	return a, a.answer(e), true
}

// wrapExact breaks a command into lines of at most a width without changing it: every character
// stays, the runs of spaces included, and a line ends after a space when it can.
//
// Parameters:
//   - e: shared environment, for the width rule
//   - s: plain text
//   - w: cells per line
//
// Returns the lines, which put end to end give s again.
func wrapExact(e *env, s string, w int) []string {
	var lines []string

	for s != "" {
		if e.Metric.width(s) <= w {
			return append(lines, s)
		}

		head := e.Metric.cut(s, w)
		if head == "" {
			_, n := utf8.DecodeRuneInString(s)
			head = s[:n]
		}

		if i := strings.LastIndexByte(head, ' '); i > 0 {
			head = head[:i+1]
		}

		lines = append(lines, head)
		s = s[len(head):]
	}

	return lines
}

// body works out the lines of the question: the title, the question, then what will run.
//
// Parameters:
//   - e: shared environment
//
// Returns the title, the question, the details (the directory and the environment) and the command
// or the address, which is the line that stays when the box is short.
func (a *runAsk) body(e *env) (string, string, []string, string) {
	p := a.prep
	title := sanitize(a.start.Plugin) + e.colon() + " " + sanitize(a.start.Label)

	if p.IsOpen() {
		return title, e.t("ui_plug_ask_open"), nil, sanitize(p.OpenURL)
	}

	question := e.t("ui_plug_ask_run", sanitize(a.start.Label))
	if p.Confirm.Text != "" {
		question = sanitize(e.fold(p.Confirm.Text))
	}

	var details []string

	if p.Dir != "" {
		details = append(details, e.t("ui_plug_ask_dir", sanitize(p.Dir)))
	}

	if len(p.EnvTable) > 0 {
		details = append(details, e.t("ui_plug_ask_env", sanitize(strings.Join(p.EnvTable, " "))))
	}

	return title, question, details, e.Glyphs.arrow + " " + sanitize(p.Display)
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (a *runAsk) View(e *env, maxW, maxH int) []string {
	box, _ := a.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: a click on a button answers.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (a *runAsk) Zones(e *env, maxW, maxH int) []zone {
	_, zs := a.draw(e, maxW, maxH)

	return zs
}

// draw lays out the box. What is shown is the command or the address first of all: when the box is
// short, the blank line goes, then the directory and the environment, then the question is cut to
// one line and then left out, and the command takes every row that is left, so that what runs is
// never hidden by the words around it.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
//
// Returns the lines of the box and the zones of the two buttons, counted from its top left
// corner.
func (a *runAsk) draw(e *env, maxW, maxH int) ([]string, []zone) {
	w := max(4, min(maxW, runAskWidth))
	inner := w - 4
	rows := max(2, maxH-2)
	title, question, details, command := a.body(e)

	a.cut = false
	text := wrapWords(e, question, inner, 3)
	run := wrapExact(e, command, inner)
	extra := make([][]string, len(details))

	for i, d := range details {
		extra[i] = wrapWords(e, d, inner, 2)
	}

	spacer := 1

	for 1+len(text)+len(run)+lineCount(extra)+spacer > rows {
		switch {
		case spacer > 0:
			spacer = 0
		case len(extra) > 0:
			extra = extra[:len(extra)-1]
		case len(text) > 1:
			text = text[:1]
		case len(text) > 0:
			text = nil
		case len(run) > max(1, rows-1):
			a.cut = true
			run = append(run[:max(1, rows-1)-1], e.Metric.fit(e.t("ui_plug_ask_small"), inner,
				e.Glyphs.ellipsis))
		default:
			rows = 1 + len(run)
		}
	}

	pad := func(s string) string { return " " + e.clipPad(s, inner) + " " }

	var lines []string

	for _, l := range text {
		lines = append(lines, pad(e.Theme.Text.Render(l)))
	}

	for _, l := range run {
		lines = append(lines, pad(e.Theme.Bold.Render(l)))
	}

	for _, g := range extra {
		for _, l := range g {
			lines = append(lines, pad(e.Theme.Muted.Render(l)))
		}
	}

	if spacer > 0 {
		lines = append(lines, pad(""))
	}

	right := e.t("ui_plug_btn_run")
	if a.prep.IsOpen() {
		right = e.t("ui_plug_btn_open")
	}

	row, zs := buttonRow(e, e.t("ui_plug_btn_cancel"), right, a.yes, inner, true)
	lines = append(lines, pad(row))

	for i := range zs {
		zs[i].y = len(lines)
		zs[i].x += 2
	}

	return floating(e, e.Theme.Bold.Render(title), lines, w), zs
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (a *runAsk) Keys(e *env) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("left", "right", "h", "l", "tab"),
			key.WithHelp("h/l", e.t("ui_help_choose"))),
		e.Keys.Apply,
		key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("esc", e.t("ui_help_cancel"))),
	}
}
