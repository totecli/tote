package ui

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/config"
)

const (
	// settingLabelMax is the widest the column of names gets on the settings screen.
	settingLabelMax = 28

	// settingValueMax is the widest the column of values gets.
	settingValueMax = 30

	// settingBriefMin is the narrowest the column of descriptions can be; below it the row shows
	// the name and the value only.
	settingBriefMin = 24

	// settingArrowRoom is the number of cells the column of values keeps for the angle quotes and
	// the two spaces that frame the value of the selected row.
	settingArrowRoom = 4
)

// configScreen is the screen of settings: one row per setting of config.toml that makes sense
// in the interface, the colors in use and the check for an update.
//
// Sel is the selected row and Off the first visible one. File is what config.toml holds; when
// it cannot be used ReadOnly is the sanitized reason, and the screen shows the settings without
// changing them. The left and right keys step a value before the write of the last step has
// ended, which env.Steps remembers, see stepState.
type configScreen struct {
	sel      int
	off      int
	file     fileState
	readOnly string
}

// newConfig builds the settings screen from the current file.
//
// Parameters:
//   - e: shared environment
//   - id: setting to select, empty for the first one
func newConfig(e *env, id string) *configScreen {
	s := &configScreen{}
	s.load(e)

	for i, d := range settingDefs {
		if d.id == id {
			s.sel = i
		}
	}

	return s
}

// load reads config.toml again.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) load(e *env) {
	s.file = readFileState(e.Paths.ConfigFile())
	s.readOnly = ""

	if s.file.err != nil {
		s.readOnly = settingsFileText(e, s.file.err)
	}

	s.off = scrollTo(s.sel, s.off, s.rows(e), len(settingDefs))
}

// headLines returns the number of lines above the rows.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) headLines(e *env) int {
	n := 1
	if e.Layout == layoutFull {
		n = 2
	}

	if s.readOnly != "" {
		n++
	}

	return n
}

// panelShown reports whether the panel that explains the selected row is on screen.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) panelShown(e *env) bool {
	return e.Layout == layoutFull && e.Height >= panelHeight
}

// rows returns the number of rows that fit between the heading and the panel.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) rows(e *env) int {
	n := e.Height - 2 - s.headLines(e)
	if s.panelShown(e) {
		n -= panelRows + s.panelExtra(e)
	}

	return max(1, n)
}

// def returns the setting of the selected row.
func (s *configScreen) def() settingDef {
	return settingDefs[s.sel]
}

// Typing implements screen: the settings screen reads commands only.
func (s *configScreen) Typing() bool {
	return false
}

// Reload implements screen.
//
// Parameters:
//   - e: shared environment
//   - selectKey: identifier of the setting to select, empty to keep the selection
func (s *configScreen) Reload(e *env, selectKey string) screen {
	s.load(e)

	for i, d := range settingDefs {
		if selectKey != "" && d.id == selectKey {
			s.sel = i
		}
	}

	s.off = scrollTo(s.sel, s.off, s.rows(e), len(settingDefs))

	return s
}

// Status implements screen: below the full layout the status line explains the selected row.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) Status(e *env) statusInfo {
	if s.panelShown(e) {
		return statusInfo{}
	}

	d := s.def()
	text := e.t(d.descKey())

	if warn := s.warning(e); warn != "" {
		text = e.Glyphs.warn + " " + warn
	}

	if d.writable() {
		text = d.path() + ": " + text
	}

	return statusInfo{Command: joinFields(e, text, d.applyText(e))}
}

// move changes the selection and scrolls to keep it visible.
//
// Parameters:
//   - e: shared environment
//   - to: new selected row, brought back into range
func (s *configScreen) move(e *env, to int) {
	s.sel = max(0, min(to, len(settingDefs)-1))
	s.off = scrollTo(s.sel, s.off, s.rows(e), len(settingDefs))
}

// Update implements screen.
//
// Parameters:
//   - e: shared environment
//   - msg: message to handle
func (s *configScreen) Update(e *env, msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case itemClickMsg:
		rows, n := s.rows(e), len(settingDefs)

		if by, ok := pageClick(m.ID, pageOf(scrollTo(s.sel, s.off, rows, n), rows, n)); ok {
			s.sel, s.off = scrollRows(s.sel, s.off, rows, n, by)

			return s, nil
		}

		if m.ID < 0 || m.ID >= n {
			return s, nil
		}

		s.move(e, m.ID)

		if m.Double {
			return s, s.change(e)
		}

		return s, nil
	case wheelMsg:
		s.sel, s.off = scrollRows(s.sel, s.off, s.rows(e), len(settingDefs), m.Lines)

		return s, nil
	}

	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}

	switch {
	case key.Matches(k, e.Keys.Up):
		s.move(e, s.sel-1)
	case key.Matches(k, e.Keys.Down):
		s.move(e, s.sel+1)
	case key.Matches(k, e.Keys.PageUp):
		s.move(e, s.sel-s.rows(e))
	case key.Matches(k, e.Keys.PageDown):
		s.move(e, s.sel+s.rows(e))
	case key.Matches(k, e.Keys.Home):
		s.move(e, 0)
	case key.Matches(k, e.Keys.End):
		s.move(e, len(settingDefs)-1)
	case key.Matches(k, e.Keys.Back):
		return s, msgCmd(goHomeMsg{})
	case key.Matches(k, e.Keys.Change):
		return s, s.change(e)
	case key.Matches(k, e.Keys.Left):
		return s, s.adjust(e, -1)
	case key.Matches(k, e.Keys.Right):
		return s, s.adjust(e, 1)
	case key.Matches(k, e.Keys.Default):
		return s, s.reset(e)
	case key.Matches(k, e.Keys.View):
		return s, s.details(e)
	case key.Matches(k, e.Keys.Notice):
		return s, msgCmd(noticeMsg{})
	case key.Matches(k, e.Keys.Check):
		return s, msgCmd(checkMsg{})
	case key.Matches(k, e.Keys.Reload):
		return s, msgCmd(reloadMsg{})
	}

	return s, nil
}

// refusal returns the message that explains why the selected row cannot be written, empty when
// it can.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) refusal(e *env) string {
	d := s.def()

	switch {
	case !d.writable():
		return ""
	case s.readOnly != "":
		return s.readOnly
	}

	if src := sourceOf(e, s.file, d); src.locked {
		return e.t("ui_set_locked", sanitize(d.path()), src.name)
	}

	return ""
}

// change starts the change of the selected row: a switch is flipped, a choice and a text open
// their overlay and the check for an update runs.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) change(e *env) tea.Cmd {
	d := s.def()

	switch d.kind {
	case settingAction:
		return msgCmd(checkMsg{})
	case settingInfo:
		return toastCmd(e.t("ui_set_info"), false)
	}

	if why := s.refusal(e); why != "" {
		return toastCmd(why, true)
	}

	switch d.kind {
	case settingBool:
		return e.stepTo(d, strconv.FormatBool(e.stepFrom(d) != "true"))
	case settingChoice:
		return msgCmd(openOverlayMsg{Overlay: newSettingPick(e, d)})
	}

	return msgCmd(openOverlayMsg{Overlay: newSettingPrompt(e, d)})
}

// adjust steps the value of the selected row with the left and right keys: a switch is turned
// off by left and on by right, and a choice moves to the previous or the next value, wrapping
// around at both ends, whatever the number of values. The other rows do nothing.
//
// Parameters:
//   - e: shared environment
//   - dir: -1 for the left key, 1 for the right key
//
// Returns the command that writes the new value, a toast that says why the row cannot be changed
// (the same one as Enter), or nil when there is nothing to write.
func (s *configScreen) adjust(e *env, dir int) tea.Cmd {
	d := s.def()

	if d.kind != settingBool && d.kind != settingChoice {
		return nil
	}

	if why := s.refusal(e); why != "" {
		return toastCmd(why, true)
	}

	if d.kind == settingBool {
		return e.stepTo(d, strconv.FormatBool(dir > 0))
	}

	return e.stepTo(d, stepChoice(d, e.stepFrom(d), dir))
}

// stepChoice returns the value that follows or precedes a value in the choices of a setting.
//
// Parameters:
//   - d: setting whose choices are walked
//   - cur: value to start from; one that is not a choice counts as the default of the setting
//   - dir: -1 for the previous value, 1 for the next one
//
// Returns the value reached, wrapping around at the ends of the list.
func stepChoice(d settingDef, cur string, dir int) string {
	n := len(d.choices)
	at := slices.Index(d.choices, cur)

	if at < 0 {
		at = max(0, slices.Index(d.choices, defaultValue(d)))
	}

	return d.choices[((at+dir)%n+n)%n]
}

// reset asks whether to remove the key of the selected row from config.toml, which gives the
// default back and loses the value that was set. The letter alone never does it.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) reset(e *env) tea.Cmd {
	d := s.def()

	if !d.writable() {
		return nil
	}

	if why := s.refusal(e); why != "" {
		return toastCmd(why, true)
	}

	if !s.file.present[d.path()] {
		return toastCmd(e.t("ui_set_default_already", e.t(d.nameKey())), false)
	}

	return msgCmd(openOverlayMsg{Overlay: newAskReset(e, d)})
}

// newSettingPick returns the overlay that picks the value of a choice.
//
// Parameters:
//   - e: shared environment
//   - d: setting to change
func newSettingPick(e *env, d settingDef) overlay {
	cur := d.value(e.Settings)
	items := make([]pickItem, len(d.choices))
	at := 0

	for i, c := range d.choices {
		items[i] = pickItem{label: optionText(e, d, c), value: c}

		if c == cur {
			items[i].note = e.t("ui_pick_current")
			at = i
		}
	}

	return newPick(e.t(d.nameKey()), items, at, func(e *env, v string) tea.Cmd {
		return writeSettingCmd(e, d, v, false)
	})
}

// newSettingPrompt returns the overlay that reads the value of a text setting.
//
// Parameters:
//   - e: shared environment
//   - d: setting to change
func newSettingPrompt(e *env, d settingDef) overlay {
	check := func(e *env, text string) string {
		return invalidSetting(e, d, strings.TrimSpace(text))
	}

	return newPrompt(e.t(d.nameKey()), e.t("ui_setp_"+d.id), d.value(e.Settings), check,
		func(e *env, text string) tea.Cmd {
			return writeSettingCmd(e, d, strings.TrimSpace(text), false)
		})
}

// optionKey returns the catalog key of the name of a stored value.
//
// Parameters:
//   - d: setting the value belongs to; the "auto" of the page width has its own name
//   - v: stored value; the empty string stands for the automatic choice
func optionKey(d settingDef, v string) string {
	if v == "" {
		v = "auto"
	}

	if d.id == "width" && v == "auto" {
		return "ui_opt_width_auto"
	}

	return "ui_opt_" + v
}

// optionText returns the translated name of a stored value.
//
// Parameters:
//   - e: shared environment
//   - d: setting the value belongs to
//   - v: stored value; the empty string stands for the automatic choice
func optionText(e *env, d settingDef, v string) string {
	return e.t(optionKey(d, v))
}

// valueText returns the text shown in the column of values for a setting.
//
// Parameters:
//   - e: shared environment
//   - d: setting to show
//   - src: where the value comes from
func valueText(e *env, d settingDef, src source) string {
	get := envLookup(e.Environ)

	switch d.id {
	case "lang":
		eff := config.EffectiveLang(e.Settings, get)

		if e.Settings.Lang == "" || src.locked {
			return e.t("ui_opt_auto") + " (" + eff + ")"
		}

		return optionText(e, d, eff)
	case "editor":
		return sanitize(strings.Join(config.EffectiveEditor(e.Settings, get), " "))
	case "repos_root":
		if e.Settings.ReposRoot == "" {
			return e.t("ui_val_none")
		}

		return sanitize(e.Settings.ReposRoot)
	case "colors":
		return colorsText(e)
	case "update":
		return sanitize(e.Version)
	}

	if d.kind == settingBool {
		if d.value(e.Settings) == "true" {
			return e.t("ui_choice_yes")
		}

		return e.t("ui_choice_no")
	}

	return optionText(e, d, d.value(e.Settings))
}

// colorsText names the colors the interface draws with.
//
// Parameters:
//   - e: shared environment
func colorsText(e *env) string {
	get := envLookup(e.Environ)
	_, fixed := parseColorName(get("TOTE_COLOR"))
	override := fixed || colorWord(get("TOTE_COLOR")) == "always" || get("NO_COLOR") != ""
	dumb := (get("TERM") == "" || get("TERM") == "dumb") && !override

	return profileName(e.Theme.Profile, dumb)
}

// Keys implements screen.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) Keys(e *env) []key.Binding {
	d := s.def()

	return configBindings(e, d, s.readOnly != "", sourceOf(e, s.file, d).locked)
}

// configBindings lists the keys of the settings screen.
//
// Parameters:
//   - e: shared environment
//   - d: setting of the selected row
//   - frozen: true when config.toml cannot be written
//   - locked: true when an environment variable fixes the setting of the row
//
// Returns the bindings, the ones that the row cannot use being disabled. The keys that step a value
// are listed only where they do, so that the hint and the angle quotes of the row agree.
func configBindings(e *env, d settingDef, frozen, locked bool) []key.Binding {
	change := e.Keys.Change

	switch d.kind {
	case settingAction:
		change.SetHelp(e.Glyphs.enter, e.t("ui_help_check"))
	case settingInfo:
		change.SetEnabled(false)
	}

	reset := e.Keys.Default
	reset.SetEnabled(d.writable() && !frozen)

	adjust := e.Keys.Adjust
	adjust.SetEnabled((d.kind == settingBool || d.kind == settingChoice) && !frozen && !locked)

	if frozen && d.writable() {
		change.SetEnabled(false)
	}

	return []key.Binding{
		change, adjust, reset, e.Keys.Back, e.Keys.Palette, e.Keys.Quit, e.Keys.Nav, e.Keys.View,
		e.Keys.Notice, e.Keys.Check, e.Keys.Reload, e.Keys.Help,
	}
}

// View implements screen.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines to return
func (s *configScreen) View(e *env, h int) []string {
	lines, _ := s.draw(e, h)

	return lines
}

// Zones implements zonedScreen: the visible rows can be clicked.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines of the page
func (s *configScreen) Zones(e *env, h int) []zone {
	_, zs := s.draw(e, h)

	return zs
}

// draw lays out the page of the settings.
//
// Parameters:
//   - e: shared environment
//   - h: number of lines to return
//
// Returns exactly h lines and the zones of the rows that are on them.
func (s *configScreen) draw(e *env, h int) ([]string, []zone) {
	lines := []string{s.titleLine(e)}

	if e.Layout == layoutFull {
		lines = append(lines, "")
	}

	if s.readOnly != "" {
		lines = append(lines, e.lead()+e.Theme.Err.Render(e.Metric.fit(
			e.Glyphs.warn+" "+s.readOnly, e.Width-e.indent(), e.Glyphs.ellipsis)))
	}

	body, zs := s.rowLines(e, s.rows(e))
	zs = shiftZones(zs, len(lines))
	lines = append(lines, body...)

	if s.panelShown(e) {
		lines = fitLines(lines, h-panelRows-s.panelExtra(e))
		zs = clipZones(zs, len(lines))
		lines = append(lines, s.panel(e)...)
	}

	lines = fitLines(lines, h)

	return lines, clipZones(zs, len(lines))
}

// titleLine draws the first line: the logo, the name of the program and of the screen, and the
// number of rows at the right.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) titleLine(e *env) string {
	g := e.Glyphs
	count := strconv.Itoa(len(settingDefs))
	left := g.prompt + " tote " + g.crumb + " "
	room := e.Width - spareColumns - 1 - e.Metric.width(count) - 1

	name := e.Metric.fit(e.t("ui_config_title"), max(1, room-e.Metric.width(left)), g.ellipsis)
	gap := e.Width - spareColumns - e.Metric.width(left) - e.Metric.width(name) -
		e.Metric.width(count)

	return e.Theme.Title.Render(left) + e.Theme.Bold.Render(name) + spaces(gap) +
		e.Theme.Muted.Render(count)
}

// settingRow is what a row of the settings screen says: the name, the short description and the
// effective value.
type settingRow struct {
	name  string
	brief string
	value string
}

// rowTexts returns the three texts of every row.
//
// Parameters:
//   - e: shared environment
//
// Returns the name, the short description and the effective value. A row that an environment
// variable overrides says which one instead of its description, since it cannot be changed here.
func (s *configScreen) rowTexts(e *env) []settingRow {
	out := make([]settingRow, len(settingDefs))

	for i, d := range settingDefs {
		src := sourceOf(e, s.file, d)
		brief := e.t(d.briefKey())

		if src.origin == originEnv && src.locked {
			brief = e.t("ui_src_locked", src.name)
		}

		out[i] = settingRow{name: e.t(d.nameKey()), brief: brief, value: valueText(e, d, src)}
	}

	return out
}

// widths returns the widths of the name, the description and the value of a row.
//
// Parameters:
//   - e: shared environment
//   - rows: the texts of the rows, which give the natural widths
//
// Returns the three widths. The value sits at the right of the row and the name at the left; the
// description takes what is left between them and is cut with an ellipsis. When less than
// settingBriefMin cells would be left the description is dropped, and its width is 0, and the name
// takes the room. The column of values also keeps up to settingArrowRoom cells for the angle quotes
// of the selected row, taken from the description while it keeps settingBriefMin cells, else from
// the name, so that the columns never move with the selection.
func (s *configScreen) widths(e *env, rows []settingRow) (int, int, int) {
	nameW, valueW := 0, 0

	for _, r := range rows {
		nameW = max(nameW, e.Metric.width(r.name))
		valueW = max(valueW, e.Metric.width(r.value))
	}

	avail := max(1, e.Width-e.indent()-spareColumns-2)
	nameW, valueW = min(nameW, settingLabelMax), min(valueW, settingValueMax)

	if nameW+valueW+columnGap > avail {
		valueW = min(valueW, avail/2)

		return max(1, avail-columnGap-valueW), 0, valueW
	}

	if brief := avail - nameW - valueW - 2*columnGap; brief >= settingBriefMin {
		room := min(settingArrowRoom, brief-settingBriefMin)

		return nameW, brief - room, valueW + room
	}

	valueW += min(settingArrowRoom, avail-columnGap-nameW-valueW)

	return avail - columnGap - valueW, 0, valueW
}

// rowLines draws the visible rows: the name in bold, the description muted and the value aligned
// at the right.
//
// Parameters:
//   - e: shared environment
//   - rows: number of rows that fit, "N more" lines included
//
// Returns the lines and the zones of the rows, counted from the first row. When the list goes on
// above or below the rows, a muted "N more" line takes the first or the last row.
func (s *configScreen) rowLines(e *env, rows int) ([]string, []zone) {
	texts := s.rowTexts(e)
	nameW, briefW, valueW := s.widths(e, texts)
	win := windowOf(scrollTo(s.sel, s.off, rows, len(settingDefs)), rows, len(settingDefs))
	items := make([]string, 0, win.count)

	for i := win.off; i < win.off+win.count; i++ {
		cells := []cell{{text: texts[i].name, w: nameW, bold: true}}

		if briefW > 0 {
			cells = append(cells, cell{text: texts[i].brief, w: briefW, muted: true})
		}

		value := texts[i].value
		if i == s.sel {
			value = s.framed(e, value, valueW)
		}

		cells = append(cells, cell{text: value, w: valueW, right: true})
		items = append(items, e.renderRow(i == s.sel, false, cells, e.Width))
	}

	return win.stack(e, items, e.lead()+"  ", 0, e.Width)
}

// framed puts the value of the selected row between angle quotes when the left and right keys
// change it, to tell that they do.
//
// Parameters:
//   - e: shared environment
//   - value: text of the value
//   - w: width of the column of values
//
// Returns the value alone when the row cannot be changed with the arrows, or when the framed
// value would not fit in the column.
func (s *configScreen) framed(e *env, value string, w int) string {
	d := s.def()

	if (d.kind != settingBool && d.kind != settingChoice) || s.refusal(e) != "" {
		return value
	}

	text := e.Glyphs.prev + " " + value + " " + e.Glyphs.next
	if e.Metric.width(text) > w {
		return value
	}

	return text
}

// settingFacts is what the interface says about one setting: the panel under the rows shows it
// on three lines, cut to the width, and the details show it whole. Warn is a warning about the
// value in force, empty for most settings, which the panel adds as a fourth line.
type settingFacts struct {
	head   string
	where  string
	desc   string
	values []string
	note   string
	apply  string
	warn   string
}

// themeClash tells whether the color scheme in force is the opposite of the background that the
// terminal reported.
//
// Parameters:
//   - e: shared environment
//
// Returns the catalog key of the warning, or an empty string. The dark scheme is for a dark
// terminal and the light one for a light terminal, and tote paints no background of its own, so
// the other pairing leaves the text unreadable. A terminal that never answered, the automatic
// scheme, the neutral one and a profile without colors all give an empty string.
func themeClash(e *env) string {
	switch {
	case e.Settings.UI.Theme == "dark" && e.Theme.Mode == modeDark && e.Backdrop == backdropLight:
		return "ui_setw_dark"
	case e.Settings.UI.Theme == "light" && e.Theme.Mode == modeLight &&
		e.Backdrop == backdropDark:
		return "ui_setw_light"
	}

	return ""
}

// warning returns the translated warning about the value of the selected setting, empty when the
// value raises none. Only the theme can.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) warning(e *env) string {
	if s.def().id != "theme" {
		return ""
	}

	key := themeClash(e)
	if key == "" {
		return ""
	}

	return e.t(key, sanitize(e.Keys.Default.Help().Key))
}

// panelExtra returns the number of lines that the panel has beyond panelLines: one when the
// selected setting has a warning to show, else none.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) panelExtra(e *env) int {
	if s.warning(e) != "" {
		return 1
	}

	return 0
}

// facts gathers what is said about the selected setting.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) facts(e *env) settingFacts {
	d := s.def()
	src := sourceOf(e, s.file, d)

	f := settingFacts{head: d.path(), where: e.t("ui_srcd_default"), desc: e.t(d.descKey())}
	if !d.writable() {
		f.head = e.t(d.nameKey())
	}

	switch {
	case src.origin == originEnv && src.locked:
		f.where = e.t("ui_srcd_locked", src.name)
	case src.origin == originEnv:
		f.where = e.t("ui_srcd_env", src.name)
	case src.origin == originFile:
		f.where = e.t("ui_srcd_file")
	case d.kind == settingInfo:
		f.where = e.t("ui_srcd_detected")
	case d.kind == settingAction:
		f.where = ""
	}

	for _, c := range d.choices {
		f.values = append(f.values, optionText(e, d, c))
	}

	if k := d.noteKey(); k != "" {
		f.note = e.t(k)
	}

	f.apply = d.applyText(e)
	f.warn = s.warning(e)

	return f
}

// panel draws the box under the rows that explains the selected setting: where it lives, what it
// does and which values it takes, then a warning line when the value in force raises one. A line
// that does not fit is cut, and the details show it whole.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) panel(e *env) []string {
	d := s.def()
	w := e.Width
	inner := w - 2
	f := s.facts(e)

	vals := ""
	if len(f.values) > 0 {
		vals = e.t("ui_set_values", strings.Join(f.values, ", "))
	}

	texts := []string{
		joinFields(e, sanitize(f.head), f.where),
		f.desc,
		joinFields(e, vals, f.note, f.apply),
	}

	lines := make([]string, panelLines, panelLines+1)

	for i, t := range texts {
		style := e.Theme.Text
		if i > 0 {
			style = e.Theme.Muted
		}

		lines[i] = " " + style.Render(e.Metric.fit(t, max(1, inner-2), e.Glyphs.ellipsis))
	}

	if f.warn != "" {
		lines = append(lines, " "+e.Theme.Warn.Render(e.Metric.fit(e.Glyphs.warn+" "+f.warn,
			max(1, inner-2), e.Glyphs.ellipsis)))
	}

	title := e.Theme.Bold.Render(e.Metric.fit(e.t(d.nameKey()), max(1, inner-6),
		e.Glyphs.ellipsis))

	return boxed(e, title, lines, w)
}

// details opens the overlay that shows the selected setting whole, with the lines that the panel
// cuts.
//
// Parameters:
//   - e: shared environment
func (s *configScreen) details(e *env) tea.Cmd {
	d := s.def()
	f := s.facts(e)

	var fields []infoField

	add := func(label, value string) {
		if value != "" {
			fields = append(fields, infoField{label: e.t(label), value: sanitize(value)})
		}
	}

	add("ui_field_setting", f.head)
	add("ui_field_source", f.where)
	add("ui_field_desc", f.desc)
	add("ui_field_values", strings.Join(f.values, ", "))
	add("ui_field_note", f.note)
	add("ui_field_applies", f.apply)

	return msgCmd(openOverlayMsg{Overlay: &infoOverlay{
		title: sanitize(e.t(d.nameKey())), fields: fields, warn: f.warn,
	}})
}
