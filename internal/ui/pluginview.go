package ui

import (
	"context"
	"slices"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

const (
	// spinDelay is the time between two frames of the spinner of a run.
	spinDelay = 120 * time.Millisecond

	// eventBatch is the most pieces of output one message carries to the interface.
	eventBatch = 256

	// eventQueue is how many pieces of output wait for the interface before the run is slowed.
	eventQueue = 256

	// stopWait is how long the end of the interface waits for a program it stopped.
	stopWait = 3 * time.Second

	// silentWidth is the widest the box of a run that shows no output gets.
	silentWidth = 48
)

// runItem is what a run hands to the interface: a piece of output, or, last of all, its outcome.
type runItem struct {
	event plugin.Event
	done  *plugin.Outcome
}

// plugRun is the captured run that the interface is showing.
//
// Seq tells it from an older one. Ch carries its output to the interface, Cancel stops the
// program, Quit is closed when the interface ends so that a run nobody reads is not blocked, and
// Finished is closed when the registry has returned.
type plugRun struct {
	seq      int
	start    pluginStart
	prep     plugin.Prepared
	ch       chan runItem
	cancel   context.CancelFunc
	quit     chan struct{}
	finished chan struct{}
	view     *runView
}

// runEventsMsg carries output of a run to the interface, and its outcome when the run ended.
type runEventsMsg struct {
	Seq    int
	Events []plugin.Event
	Done   *plugin.Outcome
}

// runTickMsg moves the spinner of a run on.
type runTickMsg struct {
	Seq int
}

// runCancelMsg asks the root to stop the run that is showing.
type runCancelMsg struct {
	Seq int
}

// runView is the overlay that shows a captured run: the output of the program in a pager while
// it runs and after, with the plugin id in the title. A run whose output is dropped (silent) shows
// a small box instead, which closes by itself when the run worked and turns into the pager with
// the last lines the program wrote when it failed.
//
// Running is true until the outcome arrives and Stopping once Esc asked the program to stop. Spin
// is the frame of the spinner and Status the sentence of the end of the run, shown on the status
// line.
type runView struct {
	pagerView
	seq      int
	id       string
	label    string
	silent   bool
	running  bool
	stopping bool
	spin     int
	status   string
}

// newRunView returns the overlay of a run that starts now.
//
// Parameters:
//   - e: shared environment
//   - run: the run
func newRunView(e *env, run *plugRun) *runView {
	v := &runView{
		seq: run.seq, id: sanitize(run.start.Plugin), label: sanitize(run.start.Label),
		silent: run.prep.Output == plugin.OutputSilent, running: true,
	}
	v.text, v.follow = newPagerText(!e.isMono()), true

	return v
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (v *runView) Init(_ *env) tea.Cmd {
	return nil
}

// Update implements overlay: Esc stops a program that runs and closes the view of one that ended,
// the scroll keys and the wheel move the output.
//
// Parameters:
//   - e: shared environment, for the key map
//   - msg: key press, wheel or click; other messages are ignored
func (v *runView) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return v, nil, !v.running
	case wheelMsg:
		v.move(m.Lines)
	case itemClickMsg:
		if by, ok := pageClick(m.ID, pageOf(v.off, v.rows, v.total)); ok {
			v.move(by)
		}
	case tea.KeyPressMsg:
		switch {
		case key.Matches(m, e.Keys.Close):
			if v.running {
				v.stopping = true

				return v, msgCmd(runCancelMsg{Seq: v.seq}), false
			}

			return v, nil, true
		default:
			v.scroll(e, m)
		}
	}

	return v, nil, false
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (v *runView) View(e *env, maxW, maxH int) []string {
	box, _ := v.drawBox(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: the "N more" lines can be clicked to scroll by a page.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (v *runView) Zones(e *env, maxW, maxH int) []zone {
	_, zs := v.drawBox(e, maxW, maxH)

	return zs
}

// title draws the title of the box: the plugin id first, then what it runs.
//
// Parameters:
//   - e: shared environment
//   - w: width of the box
func (v *runView) title(e *env, w int) string {
	text := v.id + e.colon() + " " + v.label

	return e.Theme.Bold.Render(e.Metric.fit(text, max(1, w-8), e.Glyphs.ellipsis))
}

// drawBox lays out the box.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (v *runView) drawBox(e *env, maxW, maxH int) ([]string, []zone) {
	if v.silent {
		w := max(8, min(maxW, silentWidth))
		lines := wrapWords(e, v.progress(e), w-4, max(1, maxH-2))

		rows := make([]string, len(lines))
		for i, l := range lines {
			rows[i] = " " + e.Theme.Text.Render(l)
		}

		return floating(e, v.title(e, w), rows, w), nil
	}

	w := max(8, min(maxW, pagerWidth))
	lines, zs := v.draw(e, w, maxH)

	return floating(e, v.title(e, w), lines, w), shiftZones(zs, 1)
}

// progress words what the run is doing now, with its spinner, or how it ended.
//
// Parameters:
//   - e: shared environment
func (v *runView) progress(e *env) string {
	if !v.running {
		return v.status
	}

	key := "ui_plug_run_running"
	if v.stopping {
		key = "ui_plug_run_stopping"
	}

	return e.t(key, v.id, e.spinner(v.spin))
}

// spinner gives a frame of the spinner.
//
// Parameters:
//   - n: number of ticks so far
func (e *env) spinner(n int) string {
	return e.Glyphs.spin[n%len(e.Glyphs.spin)]
}

// Status implements statuser: the status slot shows what the plugin is doing, with a spinner
// while it runs, and how the run ended after.
//
// Parameters:
//   - e: shared environment
func (v *runView) Status(e *env) statusInfo {
	return statusInfo{Command: v.progress(e)}
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (v *runView) Keys(e *env) []key.Binding {
	if v.running {
		stop := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", e.t("ui_help_cancel")))

		if v.stopping || v.silent {
			return []key.Binding{stop}
		}

		return []key.Binding{stop, e.Keys.Scroll}
	}

	return []key.Binding{e.Keys.Close, e.Keys.Scroll}
}

// runCaptured starts the program of a contribution whose output is shown by the interface: the
// pager, or nothing for a silent one. The program runs in the registry, off the event loop; its
// output reaches the interface as batches of pieces on a bounded queue, so a program that writes
// faster than the screen is read waits for it instead of growing the memory.
//
// Parameters:
//   - st: the contribution
//   - p: the prepared run
func (r *root) runCaptured(st pluginStart, p plugin.Prepared) tea.Cmd {
	e := r.e
	r.runSeq++

	parent := r.ctx
	if parent == nil {
		parent = context.Background()
	}

	r.toast.text = ""

	ctx, cancel := context.WithCancel(parent)
	run := &plugRun{
		seq: r.runSeq, start: st, prep: p, ch: make(chan runItem, eventQueue),
		cancel: cancel, quit: make(chan struct{}), finished: make(chan struct{}),
	}
	run.view = newRunView(e, run)
	r.run = run
	host := e.Plugins

	go func() {
		defer close(run.finished)

		out := host.Run(ctx, p, func(ev plugin.Event) {
			select {
			case run.ch <- runItem{event: ev}:
			case <-run.quit:
			}
		})

		select {
		case run.ch <- runItem{done: &out}:
		case <-run.quit:
		}
	}()

	spin := tick(spinDelay, func(time.Time) tea.Msg { return runTickMsg{Seq: run.seq} })

	return tea.Batch(r.push(run.view), waitRun(run.seq, run.ch), spin)
}

// waitRun waits for the next output of a run and takes with it whatever else is already queued.
//
// Parameters:
//   - seq: the run
//   - ch: its queue
func waitRun(seq int, ch <-chan runItem) tea.Cmd {
	return func() tea.Msg {
		m := runEventsMsg{Seq: seq}

		for len(m.Events) < eventBatch && m.Done == nil {
			var it runItem

			if len(m.Events) == 0 {
				it = <-ch
			} else {
				select {
				case it = <-ch:
				default:
					return m
				}
			}

			if it.done != nil {
				m.Done = it.done

				continue
			}

			m.Events = append(m.Events, it.event)
		}

		return m
	}
}

// runEvents takes output of the run that is showing, and its end.
//
// Parameters:
//   - m: the output and, when the run ended, its outcome
func (r *root) runEvents(m runEventsMsg) tea.Cmd {
	run := r.run
	if run == nil || run.seq != m.Seq {
		return nil
	}

	for _, ev := range m.Events {
		switch ev.Type {
		case plugin.EventOutput:
			run.view.text.add(ev.Text)
		case plugin.EventNotice:
			run.view.text.cut = true
		}
	}

	if m.Done != nil {
		return r.runFinished(run, *m.Done)
	}

	return waitRun(run.seq, run.ch)
}

// runFinished shows how a run ended: a toast, and the status slot of its view. A silent run that
// worked is closed; one that failed shows what the program wrote last.
//
// Parameters:
//   - run: the run
//   - out: how it ended
func (r *root) runFinished(run *plugRun, out plugin.Outcome) tea.Cmd {
	e := r.e
	v := run.view
	r.run = nil

	run.cancel()

	text, bad := e.outcomeText(run.start.Plugin, out, run.prep.Timeout)
	v.running, v.stopping, v.status = false, false, text

	if v.silent {
		if !bad {
			r.dropOverlay(v)

			return r.say(text, false)
		}

		v.silent, v.follow = false, false
		v.text.add(out.Tail)
	}

	if !bad && v.follow {
		v.off, v.follow = 0, false
	}

	return r.showToast(toastMsg{Text: text, Err: bad, Hold: bad})
}

// dropOverlay removes an overlay from the stack, wherever it is.
//
// Parameters:
//   - ov: the overlay
func (r *root) dropOverlay(ov overlay) {
	r.overlays = slices.DeleteFunc(r.overlays, func(o overlay) bool { return o == ov })
	r.moved()
}

// runTick moves the spinner of the run that is showing and starts the next tick.
//
// Parameters:
//   - m: the tick
func (r *root) runTick(m runTickMsg) tea.Cmd {
	if r.run == nil || r.run.seq != m.Seq {
		return nil
	}

	r.run.view.spin++

	return tick(spinDelay, func(time.Time) tea.Msg { return runTickMsg{Seq: m.Seq} })
}

// runCancel stops the program of the run that is showing. The run ends with the outcome
// cancelled, which the view then shows.
//
// Parameters:
//   - seq: the run Esc was pressed for
func (r *root) runCancel(seq int) tea.Cmd {
	if r.run == nil || r.run.seq != seq {
		return nil
	}

	r.run.cancel()

	return nil
}

// endRun stops the program of a run that is still showing when the interface ends, and waits a
// moment for the registry to stop its group, so that no program of a plugin outlives tote.
func (r *root) endRun() {
	run := r.run
	if run == nil {
		return
	}

	r.run = nil

	run.cancel()
	close(run.quit)

	select {
	case <-run.finished:
	case <-time.After(stopWait):
	}
}
