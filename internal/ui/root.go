package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
)

const (
	// toastDuration is how long a message stays on the status line.
	toastDuration = 4 * time.Second

	// toastHold is how long a message that the user has to act on stays on the status line.
	toastHold = 12 * time.Second

	// quitWindow is how long a first Ctrl-C on the welcome page waits for the second one that
	// quits.
	quitWindow = 2 * time.Second
)

// tick starts a timer that sends a message after a delay. It is a variable so that the tests,
// which play the messages by hand, can leave the timers out.
var tick = tea.Tick

// toast is the message on the status line.
//
// ID changes with every message so that the timer of an older one cannot remove a newer one.
// Reword, when set, words the message again for the current width, so that a message cut to a
// narrow window reads in full when the window grows.
type toast struct {
	text   string
	err    bool
	id     int
	reword func() string
}

// root is the model of the program. It owns the shared environment, the current screen, the
// stack of overlays, the toast and the result, and it is the only place that turns messages into
// store reloads, programs to run and the end of the interface.
//
// Mode is the color scheme asked by the settings; Waiting is true while the background of the
// terminal is unknown and the neutral palette is used. Stale becomes true once a record that
// the shell definitions are built from has been written, so that the wrapper reloads them. Ctx
// ends when the interface closes. Check runs the check for an update, nil when the build has
// none, and Checking is true while one is running. TmuxClip reads the tmux option that lets the
// interface set the clipboard. Mousable is true when the terminal can report the mouse, see
// mouseTerm, and Clicked is the last click, for the double click. NoWrapper
// is true when the shell wrapper is not active, so that nothing can be handed to the shell.
// Armed is the time of a Ctrl-C on the welcome page that waits for a second one, zero when none
// does, and ArmedToast is the toast that said so. Sel is the text selected with the mouse, Clip
// puts a text in the clipboard with the programs of the system, Hinted becomes true once the
// session was told how to select with the terminal and HintPending until the copy that
// follows has repeated it. Hover is the element under the pointer, see hoverState. Shown is the
// view of the last frame and Reuse is true after a move of the pointer that changed nothing, so
// that the view is given again instead of being drawn again. Run is the captured run of a plugin
// that the interface is showing, RunSeq numbers the runs and Opening is true while a request to
// open an address is waiting for the platform; at most one of them is going at a time.
type root struct {
	e           *env
	scr         screen
	overlays    []overlay
	toast       toast
	runner      Runner
	guard       *guard
	delay       time.Duration
	mode        themeMode
	auto        bool
	waiting     bool
	profile     colorprofile.Profile
	stale       bool
	status      statusState
	result      Result
	keys        int
	panicKey    bool
	interrupted bool
	ctx         context.Context
	check       func(ctx context.Context) (UpdateCheck, error)
	checking    bool
	tmuxClip    func() string
	mousable    bool
	clicked     clickMark
	noWrapper   bool
	armed       time.Time
	armedToast  int
	sel         selection
	clip        ClipboardFunc
	hinted      bool
	hintPending bool
	hover       hoverState
	shown       *tea.View
	reuse       bool
	job         *pluginJob
	jobSeq      int
	run         *plugRun
	runSeq      int
	opening     bool
}

// Init implements tea.Model: it asks the terminal for its background when the theme is
// automatic, and bounds the wait; when the theme is a fixed dark or light one it asks too, but
// waits for nothing, since only the settings screen uses the answer, see themeClash. When
// config.toml is refused, or has keys that are ignored, and the settings screen that says so is
// not the one opening, or when TOTE_COLOR holds a value that is not understood, it tells the user,
// since the warnings of the command line are gone with the terminal screen they were printed on.
func (r *root) Init() tea.Cmd {
	var (
		cmds  []tea.Cmd
		notes []string
	)

	if _, here := r.scr.(*configScreen); !here {
		if st := readFileState(r.e.Paths.ConfigFile()); st.err != nil {
			notes = append(notes, r.e.t("ui_config_ignored", settingsReason(r.e, st.err)))
		} else if _, warns, err := config.LoadSettingsWarn(r.e.Paths.ConfigFile()); err == nil &&
			len(warns) > 0 {
			notes = append(notes, r.e.t("ui_config_warned"))
		}
	}

	bad, wrong := UnknownColor(r.e.Environ)
	if wrong {
		notes = append(notes, r.e.t("cli_ui_bad_color", bad))
	}

	if len(notes) > 0 {
		cmds = append(cmds, r.showToast(toastMsg{
			Text: strings.Join(notes, " "+r.e.Glyphs.sep+" "), Err: true, Hold: wrong,
		}))
	}

	if r.waiting {
		cmds = append(cmds, tea.RequestBackgroundColor, tick(backgroundWait,
			func(time.Time) tea.Msg { return backgroundTimeoutMsg{} }))
	} else if r.forced() {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}

	cmds = append(cmds, r.e.Plug.fetch())

	return tea.Batch(cmds...)
}

// rebuildTheme recomputes the theme after the background or the profile changed.
func (r *root) rebuildTheme() {
	r.e.Theme = newTheme(r.mode, r.profile)
}

// resize records the size of the terminal, works out the page and picks the layout. A change of
// the settings goes through it too, see applySettings, so that a resize and a change of ui.width
// or ui.compact share one path: the selection of text is dropped because its cells moved, and
// the screens draw again at the new width on the next frame.
//
// Parameters:
//   - w: width of the terminal in cells
//   - h: height in rows
func (r *root) resize(w, h int) {
	r.sel = selection{}
	r.e.Cols, r.e.Height = w, h
	r.e.Width, r.e.Margin = pageGeometry(w, r.e.Settings.UI.Width)
	r.e.Layout = chooseLayout(w, h, r.e.Settings.UI)

	if r.toast.text != "" && r.toast.reword != nil {
		r.toast.text = r.toast.reword()
	}
}

// Update implements tea.Model: it handles the message, then starts the lookup of the git branch
// of the repository on the status line when it is not known yet, and works out again what the
// pointer rests on, since any message may have moved what is under it.
//
// A plain move of the pointer is dealt with first and on its own, see glide: it changes nothing
// but the hovered element, and when that is the same one as before the view of the last frame is
// kept, so a stream of moves over one element draws nothing and starts nothing.
//
// Parameters:
//   - msg: message to handle
func (r *root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.MouseMotionMsg); ok {
		if cmd, done := r.glide(m); done {
			r.reuse = r.shown != nil

			return r, cmd
		}
	}

	r.shown = nil

	m, cmd := r.update(msg)

	if load := r.e.Plug.fetch(); load != nil {
		cmd = tea.Batch(cmd, load)
	}

	if look := r.watchStatus(); look != nil {
		cmd = tea.Batch(cmd, look)
	}

	r.track(msg)
	r.rehover()

	return m, cmd
}

// update handles a message.
//
// Parameters:
//   - msg: message to handle
func (r *root) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		r.resize(m.Width, m.Height)
	case tea.BackgroundColorMsg:
		r.waiting = false
		r.e.Backdrop = backdropOf(m.IsDark())
		r.setBackground(m.IsDark())
	case backgroundTimeoutMsg:
		if r.waiting {
			r.waiting = false
			r.setBackground(fallbackDark(envLookup(r.e.Environ)))
		}
	case tea.ModeReportMsg:
		if m.Mode == ansi.ModeUnicodeCore &&
			(m.Value.IsSet() || m.Value.IsReset() || m.Value.IsPermanentlySet()) {
			r.e.Metric = metric{method: ansi.GraphemeWidth}
		}
	case tea.KeyPressMsg:
		return r.key(m)
	case tea.MouseClickMsg:
		return r, r.click(m)
	case tea.MouseWheelMsg:
		return r, r.wheel(m)
	case tea.MouseMotionMsg:
		r.hover.reset()

		return r, r.dragTo(m)
	case tea.FocusMsg:
		// Nothing to do: the next move of the pointer hovers again, and the message is not for a
		// screen.
	case tea.BlurMsg:
		r.hover.reset()
	case tea.MouseReleaseMsg:
		return r, r.dropAt(m)
	case selCopiedMsg:
		return r, r.selCopied(m)
	case branchMsg, updateMsg:
		r.status.apply(r.e, m)
	case toastMsg:
		return r, r.showToast(m)
	case toastExpireMsg:
		if m.ID == r.toast.id {
			r.toast.text = ""
		}
	case discardMsg:
		r.discard()
	case resetSettingMsg:
		return r, r.resetSetting(m.ID)
	case quitMsg:
		return r, tea.Quit
	case interruptMsg:
		r.interrupted = true

		return r, tea.Quit
	case finishMsg:
		if m.Result.Empty() {
			return r, r.quit()
		}

		r.result = m.Result
		r.result.Reload = r.stale

		return r, tea.Quit
	case openOverlayMsg:
		return r, r.push(m.Overlay)
	case settingWroteMsg:
		return r, r.settingWrote(m)
	case noticeMsg:
		return r, r.showNotice()
	case checkMsg:
		return r, r.startCheck()
	case checkedMsg:
		return r, r.endCheck(m)
	case copyReadyMsg:
		return r, r.copyReady(m)
	case probedMsg:
		return r, r.useCommand(m)
	case execDoneMsg:
		return r, r.execDone(m)
	case openKindMsg, goHomeMsg, addMsg, editMsg, deleteMsg, reloadMsg, wroteMsg,
		openPaletteMsg, activateMsg, editorMsg, openConfigMsg, openHelpMsg, commandMsg, copyMsg,
		viewMsg, pickUserMsg, refreshMsg, openPluginsMsg:
		return r, r.navigate(msg)
	case pluginCountsMsg:
		r.e.Plug.arrived(m)
	case pluginActMsg:
		return r, r.doPlugin(m)
	case pluginRemoveMsg:
		return r, r.removePlugin(m)
	case pluginGrantMsg:
		return r, r.granted(m)
	case pluginDoneMsg:
		return r, r.jobDone(m)
	case jobCancelMsg:
		return r, r.cancelJob(m.Seq)
	case fieldsSavedMsg:
		return r, r.fieldsSaved(m)
	case pluginStartMsg:
		return r, r.startPlugin(m)
	case pluginAskedMsg:
		return r, r.asked(m)
	case pluginPreparedMsg:
		return r, r.prepared(m)
	case pluginGoMsg:
		return r, r.launch(m.Start, m.Prep)
	case openedMsg:
		return r, r.opened(m)
	case repoMenuMsg:
		return r, r.repoMenu(m)
	case repoMenuReadyMsg:
		return r, r.menuReady(m)
	case runEventsMsg:
		return r, r.runEvents(m)
	case runTickMsg:
		return r, r.runTick(m)
	case runCancelMsg:
		return r, r.runCancel(m.Seq)
	default:
		return r, r.forward(msg)
	}

	return r, nil
}

// pickMode chooses the color scheme asked by the ui.theme setting: a fixed scheme, the
// automatic choice from the background of the terminal (neutral until it answers), or none.
func (r *root) pickMode() {
	r.auto = false
	r.mode = modeNeutral

	switch r.e.Settings.UI.Theme {
	case "dark":
		r.mode = modeDark
	case "light":
		r.mode = modeLight
	case "auto":
		r.auto = colors(r.profile)
	}
}

// forced reports whether the ui.theme setting fixes a dark or a light scheme that is drawn in
// color, the case where the terminal is asked for its background only to warn about a scheme
// that does not suit it.
func (r *root) forced() bool {
	return !r.auto && r.mode != modeNeutral && colors(r.profile)
}

// setBackground picks the palette once the background of the terminal is known.
//
// Parameters:
//   - dark: true for a dark background
func (r *root) setBackground(dark bool) {
	if !r.auto {
		return
	}

	r.mode = modeLight
	if dark {
		r.mode = modeDark
	}

	r.rebuildTheme()
}

// key handles a key press: the global keys first, then the top overlay, then the screen.
//
// Ctrl-C cancels or goes up one level and never quits in one press, see interrupt. Ctrl-D quits
// from anywhere, see quit. The letter q quits only from the welcome page and from the top-level
// lists, when no overlay is open and no text field has the focus. While a program runs every key
// is dropped but Ctrl-C, which ends the interface as it always did. A terminal too small to show
// a question keeps the three ways to quit direct.
//
// Parameters:
//   - k: key pressed
func (r *root) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r.keys++
	if r.panicKey && r.keys == 1 {
		panic("TOTE_DEBUG_PANIC")
	}

	if r.forgetSelection() && k.String() == "esc" {
		return r, nil
	}

	r.clicked.valid = false

	interrupt := key.Matches(k, r.e.Keys.Interrupt)
	if !interrupt {
		r.disarm()
	}

	switch {
	case interrupt && (r.guard.running() || r.e.Layout == layoutTiny):
		return r, tea.Quit
	case key.Matches(k, r.e.Keys.Suspend):
		return r, tea.Suspend
	case r.guard.running():
		return r, nil
	case r.e.Handing && !interrupt && !key.Matches(k, r.e.Keys.Exit):
		return r, nil
	case r.e.Layout == layoutTiny:
		if key.Matches(k, r.e.Keys.Quit) || key.Matches(k, r.e.Keys.Exit) {
			return r, tea.Quit
		}

		return r, nil
	case interrupt:
		return r, r.interrupt()
	case key.Matches(k, r.e.Keys.Exit):
		return r, r.quit()
	case key.Matches(k, r.e.Keys.Help) && r.helpAllowed(k):
		return r, r.push(newHelp(r.topic()))
	case len(r.overlays) > 0:
		return r, r.overlayKey(k)
	case key.Matches(k, r.e.Keys.Palette):
		return r, r.navigate(openPaletteMsg{})
	case !r.scr.Typing() && key.Matches(k, r.e.Keys.Settings):
		return r, r.openConfig()
	case !r.scr.Typing() && key.Matches(k, r.e.Keys.Quit):
		return r, r.quit()
	}

	return r, r.forward(k)
}

// overlayKey gives a key to the overlay on top. Esc on an overlay that holds changes asks before
// it closes it, so that nothing typed is lost to a stray key.
//
// Parameters:
//   - k: key pressed
func (r *root) overlayKey(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "esc" && r.topDirty() {
		return r.push(newAskDiscard(r.e))
	}

	return r.forward(k)
}

// topDirty reports that the overlay on top holds changes that closing it would lose.
func (r *root) topDirty() bool {
	n := len(r.overlays)
	if n == 0 {
		return false
	}

	d, ok := r.overlays[n-1].(dirtier)

	return ok && d.Dirty()
}

// interrupt handles Ctrl-C: it does what Esc does, which closes the overlay on top, with the
// question about the changes when it has some, or goes up one level on a screen. On the welcome
// page, where there is no level above, the first press says that a second one quits and the
// second press, within quitWindow, quits.
func (r *root) interrupt() tea.Cmd {
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}

	if len(r.overlays) > 0 {
		return r.overlayKey(esc)
	}

	if _, home := r.scr.(*homeScreen); !home {
		return r.forward(esc)
	}

	now := r.e.Now()

	if !r.armed.IsZero() && now.Sub(r.armed) <= quitWindow {
		r.disarm()

		return r.quit()
	}

	r.armed = now
	again := sanitize(r.e.Keys.Interrupt.Help().Key)
	cmd := r.showToast(toastMsg{Text: r.e.t("ui_quit_again", again), Wait: quitWindow})
	r.armedToast = r.toast.id

	return cmd
}

// disarm forgets a first Ctrl-C, and removes the message that announced it.
func (r *root) disarm() {
	if r.armed.IsZero() {
		return
	}

	r.armed = time.Time{}

	if r.toast.id == r.armedToast {
		r.toast.text = ""
	}
}

// quit ends the interface, unless something would be lost: a write that has not ended, or an
// overlay with changes. Then it asks first, and "No" is the default answer.
func (r *root) quit() tea.Cmd {
	if n := len(r.overlays); n > 0 {
		if _, asking := r.overlays[n-1].(*ask); asking {
			return nil
		}
	}

	switch {
	case r.e.pendingWrites():
		return r.push(newAskQuit(r.e, "ui_quit_writing"))
	case r.run != nil:
		return r.push(newAskQuit(r.e, "ui_quit_running"))
	case slices.ContainsFunc(r.overlays, func(ov overlay) bool {
		d, ok := ov.(dirtier)

		return ok && d.Dirty()
	}):
		return r.push(newAskQuit(r.e, "ui_quit_dirty"))
	}

	return tea.Quit
}

// resetSetting removes a setting from config.toml, after the user said yes.
//
// Parameters:
//   - id: identifier of the setting
func (r *root) resetSetting(id string) tea.Cmd {
	for _, d := range settingDefs {
		if d.id == id && d.writable() {
			return writeSettingCmd(r.e, d, defaultValue(d), true)
		}
	}

	return nil
}

// discard closes the overlay on top after the question about its changes was answered with yes.
func (r *root) discard() {
	if r.topDirty() {
		r.overlays = r.overlays[:len(r.overlays)-1]
		r.moved()
	}
}

// forward gives a message to the top overlay, or to the screen when there is none.
//
// Parameters:
//   - msg: message to deliver
func (r *root) forward(msg tea.Msg) tea.Cmd {
	if n := len(r.overlays); n > 0 {
		ov, cmd, done := r.overlays[n-1].Update(r.e, msg)

		if done {
			r.overlays = r.overlays[:n-1]
			r.moved()
		} else {
			r.overlays[n-1] = ov
		}

		return cmd
	}

	var cmd tea.Cmd

	r.scr, cmd = r.scr.Update(r.e, msg)

	if n, ok := r.scr.(navigator); ok {
		if m := n.takeNav(); m != nil {
			cmd = tea.Batch(cmd, r.navigate(m))
		}
	}

	return cmd
}

// push puts an overlay on the stack and starts it.
//
// Parameters:
//   - ov: overlay to show
func (r *root) push(ov overlay) tea.Cmd {
	r.overlays = append(r.overlays, ov)
	r.moved()

	return ov.Init(r.e)
}

// showToast shows a message on the status line.
//
// Parameters:
//   - m: message and its severity
//
// Returns the command that removes it after toastDuration, after toastHold for a held message, or
// after the delay the message asks for.
func (r *root) showToast(m toastMsg) tea.Cmd {
	r.toast = toast{text: m.Text, err: m.Err, id: r.toast.id + 1}
	id := r.toast.id

	wait := toastDuration

	switch {
	case m.Wait > 0:
		wait = m.Wait
	case m.Hold:
		wait = toastHold
	}

	return tick(wait, func(time.Time) tea.Msg { return toastExpireMsg{ID: id} })
}

// say shows a message and returns the command that removes it.
//
// Parameters:
//   - text: translated and sanitized message
//   - isErr: true for an error
func (r *root) say(text string, isErr bool) tea.Cmd {
	return r.showToast(toastMsg{Text: text, Err: isErr})
}

// leaveWith ends the interface with something for the shell to do, the insertion of a command
// line or a change of directory. Without the shell wrapper the shell cannot be told anything, so
// the interface stays open and a message shows what was meant, with the line to enable the
// wrapper, instead of quitting to print it on the terminal.
//
// Parameters:
//   - action: what the shell should do; only shell.Insert and shell.ChangeDir need the wrapper
//   - payload: command line or directory
func (r *root) leaveWith(action shell.Action, payload string) tea.Cmd {
	if !r.noWrapper || (action != shell.Insert && action != shell.ChangeDir) {
		return r.finish(action, payload)
	}

	e := r.e
	what := sanitize(payload)

	if action == shell.ChangeDir {
		what = "cd " + what
	}

	return r.showToast(toastMsg{
		Text: e.t("ui_no_wrapper", wrapperCommand(envLookup(e.Environ)), what), Hold: true,
	})
}

// finish ends the interface with a result for the wrapper.
//
// Parameters:
//   - action: what the shell should do
//   - payload: argument of the action
func (r *root) finish(action shell.Action, payload string) tea.Cmd {
	r.result = Result{Action: action, Payload: payload, Reload: r.stale}

	return tea.Quit
}

// View implements tea.Model. After a move of the pointer that changed nothing it gives the view
// of the last frame again, once, instead of drawing it.
func (r *root) View() tea.View {
	if r.reuse && r.shown != nil {
		r.reuse = false

		return *r.shown
	}

	r.reuse = false

	v := tea.NewView(r.render())
	v.AltScreen = true

	if r.mouseOn() && r.e.Layout != layoutTiny {
		// All motion reports the pointer with no button down, which is what the hover follows;
		// Shift+drag still selects text in the terminals. The focus is reported too, so that the
		// hover goes away when the terminal loses it, where the terminal can say so. Not asked
		// for on the too small screen, which takes no mouse event: the terminal keeps its own
		// selection there.
		v.MouseMode = tea.MouseModeAllMotion
		v.ReportFocus = true
	}

	r.shown = &v

	return v
}

// render draws the whole screen as one string of exactly Height lines.
//
// The page, which is the screen, the status line and the footer, is drawn at the page width and
// shifted by the margin. When an overlay is open the page is not drawn at all: the overlay floats
// alone on a cleared screen, see renderOverlay. The element under the pointer is drawn in the
// hover look first and the selected text, which is the stronger, over it.
func (r *root) render() string {
	return strings.Join(r.paintSelection(r.paintHover(r.composeFrame(false).lines)), "\n")
}

// frame is a drawn screen: its lines, what reacts to the mouse on it, and the line where the
// toasts and the status are, or -1 when it has none.
type frame struct {
	lines  []string
	hits   []hit
	status int
}

// compose draws the whole screen, and says what reacts to the mouse on it.
//
// Parameters:
//   - withHits: true to compute the hits too, see composeFrame
//
// Returns the lines and the hits of composeFrame.
func (r *root) compose(withHits bool) ([]string, []hit) {
	f := r.composeFrame(withHits)

	return f.lines, f.hits
}

// composeFrame draws the whole screen, and says what reacts to the mouse on it.
//
// It is the one function that places things on the terminal, for the frame and for the mouse
// alike.
//
// Parameters:
//   - withHits: true to compute the hits too, which costs a second drawing of the screen or
//     of the overlay; render does not need them
//
// Returns Height lines, each at most Cols cells, the hits in drawing order and the status line.
// The hits are the zones of the screen, or of the overlay when one is open, then the hints of the
// footer. Nothing under an overlay is a hit.
func (r *root) composeFrame(withHits bool) frame {
	e := r.e
	if e.Width == 0 || e.Height == 0 {
		return frame{status: -1}
	}

	if e.Layout == layoutTiny {
		return frame{lines: e.tooSmall(), status: -1}
	}

	var (
		lines  []string
		hits   []hit
		status = e.Height - 2
	)

	if n := len(r.overlays); n > 0 {
		lines, hits, status = r.renderOverlay(r.overlays[n-1], withHits)
	} else {
		lines = fitLines(r.scr.View(e, e.Height-2), e.Height-2)
		foot, spans := r.footerDraw(e.Width)
		lines = append(lines, r.statusLine(e.Width), foot)

		pad := spaces(e.Margin)
		for i, l := range lines {
			lines[i] = pad + l
		}

		if withHits {
			var zs []zone
			if z, ok := r.scr.(zonedScreen); ok {
				zs = z.Zones(e, e.Height-2)
			}

			hits = placeHits(zs, spans, e.Margin, 0, e.Height-1, e.Margin)
		}
	}

	for i, l := range lines {
		lines[i] = e.clip(l, e.Cols)
	}

	return frame{lines: lines, hits: hits, status: status}
}

// placeHits puts the zones of a block and the hints of the footer on the terminal.
//
// Parameters:
//   - zs: zones of the block, relative to its top left corner
//   - spans: hints of the footer line
//   - x: column of the left edge of the block
//   - y: row of the top edge of the block
//   - footRow: row of the footer
//   - footX: column of the first cell of the footer line
//
// Returns the zones, then the hints that press a key.
func placeHits(zs []zone, spans []span, x, y, footRow, footX int) []hit {
	out := make([]hit, 0, len(zs)+len(spans))

	for _, z := range zs {
		out = append(out, hit{y: y + z.y, x: x + z.x, w: z.w, id: z.id})
	}

	for _, sp := range spans {
		if sp.press != "" {
			out = append(out, hit{y: footRow, x: footX + sp.x, w: sp.w, press: sp.press})
		}
	}

	return out
}

// renderOverlay draws an overlay alone on a cleared screen.
//
// The box is centered on the terminal, with the footer of the overlay under its left edge, and
// the toast, or the status line of an overlay that has its own, under the footer. Nothing of the
// screen below is drawn. The box and its footer are centered together, so that the status line
// appearing or disappearing never moves the box.
//
// Parameters:
//   - ov: overlay on top of the stack
//   - withHits: true to compute the hits, see compose
//
// Returns Height lines, each at most Cols cells, the hits of the box and of the footer, and the
// line under the footer.
func (r *root) renderOverlay(ov overlay, withHits bool) ([]string, []hit, int) {
	e := r.e
	lines := make([]string, e.Height)

	maxW, maxH := max(1, e.Cols-4), max(1, e.Height-2)
	block := ov.View(e, maxW, maxH)

	if len(block) == 0 {
		return lines, nil, -1
	}

	bw := e.Metric.width(block[0])
	x := max(0, (e.Cols-bw)/2)
	y := max(0, (e.Height-len(block)-1)/2)
	pad := spaces(x)

	for i, l := range block {
		if y+i < len(lines) {
			lines[y+i] = pad + l
		}
	}

	foot, spans := r.footerDraw(bw)

	if f := y + len(block); f < len(lines) {
		lines[f] = pad + foot
	}

	_, own := ov.(statuser)
	status := y + len(block) + 1

	if s := status; s < len(lines) && (own || r.toast.text != "") {
		if text := r.statusLine(bw); text != "" {
			lines[s] = pad + text
		}
	}

	if !withHits {
		return lines, nil, status
	}

	var zs []zone
	if z, ok := ov.(zonedOverlay); ok {
		zs = clipZones(z.Zones(e, maxW, maxH), len(block))
	}

	return lines, placeHits(zs, spans, x, y, y+len(block), x), status
}

// statusLine draws the line above the footer: the toast while there is one, else the status of
// the screen, or of the overlay on top.
//
// Parameters:
//   - width: cells available
func (r *root) statusLine(width int) string {
	e := r.e

	if r.toast.text != "" {
		style := e.Theme.Text
		if r.toast.err {
			style = e.Theme.Err
		}

		return e.lead() + style.Render(e.Metric.fit(r.toast.text, width-e.indent(),
			e.Glyphs.ellipsis))
	}

	return r.statusOf().render(e, width)
}

// footer draws the key help line of the overlay on top, or of the screen.
//
// Parameters:
//   - width: cells available
func (r *root) footer(width int) string {
	line, _ := r.footerDraw(width)

	return line
}

// footerDraw draws the key help line of the overlay on top, or of the screen, and says where its
// hints are.
//
// Parameters:
//   - width: cells available
func (r *root) footerDraw(width int) (string, []span) {
	e := r.e
	bs := r.scr.Keys(e)

	if n := len(r.overlays); n > 0 {
		bs = r.overlays[n-1].Keys(e)
	}

	return footerDraw(e, bs, e.compact(), width)
}

// debugPanic reports whether the environment asks for a crash on the first key, which tests the
// restoration of the terminal.
//
// Parameters:
//   - get: environment lookup
func debugPanic(get func(string) string) bool {
	v := get("TOTE_DEBUG_PANIC")

	return v != "" && v != "0"
}
