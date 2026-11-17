package ui

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// suggestAskMsg is the request of a form whose user pressed Ctrl-L on a field with suggestions.
type suggestAskMsg struct {
	Form  *fieldsForm
	Field string
	Call  plugin.Call
}

// suggestPreparedMsg is the answer of the command that asked the registry for the run of the
// suggestions of a field: the run, or the reason there is none. Gen tells it from an older one.
type suggestPreparedMsg struct {
	Form  *fieldsForm
	Gen   int
	Field string
	Prep  plugin.Prepared
	Err   error
}

// suggestDoneMsg is the end of a suggestions run. Key is the exact argv that ran.
type suggestDoneMsg struct {
	Form  *fieldsForm
	Gen   int
	Field string
	Key   string
	Res   plugin.ChoicesResult
}

// suggestStopMsg asks the root to stop the listing that a form waits for.
type suggestStopMsg struct {
	Form *fieldsForm
}

// suggestPickedMsg is the value that the user picked in the list of the suggestions of a field.
type suggestPickedMsg struct {
	Form  *fieldsForm
	Field string
	Value string
}

// choicesRun is the suggestions run that the interface waits for. There is at most one at a time.
//
// Gen tells the messages of this run from those of an older one. Ctx ends when Cancel is called,
// which stops the program, and Finished is closed once the registry has returned, which is nil
// while the run is only being prepared. Wait is the loading box, nil when the form was not on top.
type choicesRun struct {
	gen      int
	form     *fieldsForm
	plugin   string
	field    string
	ctx      context.Context
	cancel   context.CancelFunc
	finished chan struct{}
	started  time.Time
	wait     *suggestWait
}

// suggestAsk starts the listing of the suggestions of a field. It asks the registry to prepare the
// run, off the event loop since it may start git to find the repository; the program is started
// only when the answer comes, see suggestPrepared.
//
// Parameters:
//   - m: the request
func (r *root) suggestAsk(m suggestAskMsg) tea.Cmd {
	e, f := r.e, m.Form
	if !e.pluginsOn() || f.sug == nil {
		return nil
	}

	if r.choices != nil {
		f.sug.phase = sugIdle

		return nil
	}

	parent := r.ctx
	if parent == nil {
		parent = context.Background()
	}

	r.choicesSeq++
	ctx, cancel := context.WithCancel(parent)
	r.choices = &choicesRun{
		gen: r.choicesSeq, form: f, plugin: m.Call.Plugin, field: m.Field, ctx: ctx,
		cancel: cancel,
	}

	host, gen := e.Plugins, r.choicesSeq

	return func() tea.Msg {
		p, err := host.Choices(m.Call, m.Field)

		return suggestPreparedMsg{Form: f, Gen: gen, Field: m.Field, Prep: p, Err: err}
	}
}

// current returns the run that a message is about, nil when the message is from a run that was
// stopped or replaced.
//
// Parameters:
//   - gen: the run the message comes from
//   - f: the form it is for
func (r *root) current(gen int, f *fieldsForm) *choicesRun {
	if c := r.choices; c != nil && c.gen == gen && c.form == f {
		return c
	}

	return nil
}

// suggestPrepared handles the answer of the registry: it opens a list that the form already holds,
// says why a run cannot start, or starts the program.
//
// Parameters:
//   - m: the run, or the reason there is none
func (r *root) suggestPrepared(m suggestPreparedMsg) tea.Cmd {
	c := r.current(m.Gen, m.Form)
	if c == nil {
		return nil
	}

	e, f := r.e, m.Form
	s := f.sug

	if m.Err != nil {
		r.clearChoices()

		if name, waits := plugin.ChoicesWaiting(m.Err); waits {
			s.phase, s.text = sugWaiting, f.waitText(e, name)

			return nil
		}

		s.phase, s.text = sugFailed, e.t("ui_choices_failed", e.errorText(m.Err))

		return nil
	}

	key := argvKey(m.Field, m.Prep)

	if res, ok := s.memo[key]; ok {
		r.clearChoices()

		return r.suggestShow(f, m.Field, res)
	}

	if s.starts[m.Field] >= suggestLimit {
		r.clearChoices()
		s.phase = sugLimit

		return nil
	}

	s.starts[m.Field]++
	s.phase, s.argv = sugRunning, shownArgv(m.Prep)
	s.spin, s.secs = 0, 0
	c.started = e.Now()

	cmds := []tea.Cmd{
		r.suggestRun(c, m.Prep, key),
		tick(spinDelay, func(time.Time) tea.Msg { return suggestTickMsg{Gen: c.gen} }),
	}

	// Another box over the form (the help) keeps the loading box away; the status line of the
	// form says what runs.
	if n := len(r.overlays); n > 0 && r.overlays[n-1] == overlay(f) {
		c.wait = newSuggestWait(e, f, m.Field)
		cmds = append(cmds, r.push(c.wait))
	}

	return tea.Batch(cmds...)
}

// suggestTick moves the spinner of the suggestions run that goes and starts the next tick. The
// tick of a run that is over asks for no other, so that nothing is redrawn while the form is idle.
//
// Parameters:
//   - m: the tick
func (r *root) suggestTick(m suggestTickMsg) tea.Cmd {
	c := r.choices
	if c == nil || c.gen != m.Gen || c.form.sug == nil || c.form.sug.phase != sugRunning {
		return nil
	}

	s := c.form.sug
	s.spin++
	s.secs = int(r.e.Now().Sub(c.started) / time.Second)

	return tick(spinDelay, func(time.Time) tea.Msg { return suggestTickMsg{Gen: m.Gen} })
}

// suggestRun starts the program of a run and returns the command that waits for its end. The
// registry runs in a goroutine of its own, started here, so that the program is ended by the
// context whether or not the interface still reads its answer.
//
// Parameters:
//   - c: the run that goes
//   - p: the prepared run
//   - key: the exact argv, for the memory of the form
func (r *root) suggestRun(c *choicesRun, p plugin.Prepared, key string) tea.Cmd {
	host, ctx, f := r.e.Plugins, c.ctx, c.form
	c.finished = make(chan struct{})
	done := make(chan plugin.ChoicesResult, 1)
	fin := c.finished

	go func() {
		defer close(fin)

		done <- host.RunChoices(ctx, p)
	}()

	gen, field := c.gen, c.field

	return func() tea.Msg {
		return suggestDoneMsg{Form: f, Gen: gen, Field: field, Key: key, Res: <-done}
	}
}

// suggestDone handles the end of a run: a list opens the picker, anything else is said on the
// status line.
//
// Parameters:
//   - m: how the run ended
func (r *root) suggestDone(m suggestDoneMsg) tea.Cmd {
	if r.current(m.Gen, m.Form) == nil {
		return nil
	}

	e, f := r.e, m.Form
	s := f.sug
	res := m.Res

	r.clearChoices()

	switch res.State {
	case plugin.ChoicesList, plugin.ChoicesEmpty:
		// An empty answer is not kept: it may come from a moment (offline, a cold cache), and
		// asking again costs one of the starts of the field, which are limited.
		if res.State == plugin.ChoicesList {
			s.memo[m.Key] = res
		}

		return r.suggestShow(f, m.Field, res)
	case plugin.ChoicesCancelled:
		s.phase = sugStopped
	default:
		s.phase, s.text = sugFailed, e.choicesFailure(res)
	}

	return nil
}

// suggestShow shows a list that was loaded or taken from the memory of the form: the picker for
// a list with values, the status line for an empty one.
//
// Parameters:
//   - f: the form
//   - field: id of the field
//   - res: the list
func (r *root) suggestShow(f *fieldsForm, field string, res plugin.ChoicesResult) tea.Cmd {
	s := f.sug
	s.field = field
	s.shown, s.hidden, s.more = len(res.Items), res.Hidden, res.More

	if len(res.Items) == 0 {
		s.phase = sugEmpty

		return nil
	}

	pick, matched := f.pickList(r.e, field, res)

	s.phase = sugReady
	if !matched {
		s.phase = sugNoMatch
	}

	// Another box over the form (the help) keeps it from the user: the list waits in the memory
	// and the next Ctrl-L shows it.
	if n := len(r.overlays); n == 0 || r.overlays[n-1] != overlay(f) {
		return nil
	}

	return r.push(pick)
}

// clearChoices forgets the run that was waited for once it ended.
func (r *root) clearChoices() {
	if c := r.choices; c != nil {
		c.cancel()
		r.choices = nil

		r.dropWait(c)
	}
}

// dropWait takes the loading box of a run off the stack, when it shows.
//
// Parameters:
//   - c: the run
func (r *root) dropWait(c *choicesRun) {
	if c.wait != nil {
		r.dropOverlay(c.wait)
		c.wait = nil
	}
}

// stopChoices stops the suggestions run that goes, if any, and drops its answer.
//
// Parameters:
//   - wait: true to wait a moment for the registry to end the program group, as the end of the
//     interface and the suspension do, so that no program of a plugin outlives them
//   - said: true when the form says that the listing was stopped
func (r *root) stopChoices(wait, said bool) {
	c := r.choices
	if c == nil {
		return
	}

	r.choices = nil

	c.cancel()
	r.dropWait(c)

	if s := c.form.sug; s != nil && s.busy() {
		s.phase = sugIdle
		if said {
			s.phase = sugStopped
		}
	}

	if c.finished != nil {
		r.stopping = append(slices.DeleteFunc(r.stopping, closed), c.finished)
	}

	if wait {
		r.waitStopped()
	}
}

// closed reports whether a channel is already closed, without blocking.
//
// Parameters:
//   - ch: the channel
func closed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// waitStopped waits a moment for the runs that were stopped to return, so that the program group
// of each is gone and nothing of them writes any more.
func (r *root) waitStopped() {
	timeout := time.After(stopWait)

	for _, ch := range r.stopping {
		select {
		case <-ch:
		case <-timeout:
			r.stopping = nil

			return
		}
	}

	r.stopping = nil
}

// endChoices stops the suggestions run when the interface ends, and waits for its program group
// and for those of the runs that were stopped before.
func (r *root) endChoices() {
	r.stopChoices(true, false)
	r.waitStopped()
}

// stopChoicesOf stops the suggestions run of a plugin, when it is going: the plugin is being
// disabled or removed.
//
// Parameters:
//   - id: the plugin
func (r *root) stopChoicesOf(id string) {
	if c := r.choices; c != nil && c.plugin == id {
		r.stopChoices(false, false)
	}
}

// watchChoices stops the run of a form that is no longer on the screen or that is being saved. It
// looks at the stack after every message, so that no way of leaving the form can forget the run.
func (r *root) watchChoices() {
	c := r.choices
	if c == nil {
		return
	}

	onScreen := slices.ContainsFunc(r.overlays, func(ov overlay) bool { return ov == c.form })
	if c.form.saving || !onScreen {
		r.stopChoices(false, false)
	}
}
