package ui

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

const (
	// helpWidth is the widest the box of the help is drawn.
	helpWidth = 76

	// helpKeyMax is the widest the column of key names gets in the help.
	helpKeyMax = 14

	// helpWrapLines is the most lines a sentence of the help takes. The help scrolls, so it is
	// only a guard against a runaway text: a sentence is never cut short in a narrow window.
	helpWrapLines = 40
)

// helpSection is a group of key bindings under a heading.
//
// ID names the screen or overlay the keys belong to, Title is the catalog key of the heading and
// Bindings are the bindings that the footer of that screen is made from, so that the help cannot
// say anything the keys do not do.
type helpSection struct {
	id       string
	title    string
	bindings []key.Binding
}

// helpSections lists the groups of keys, one per screen or overlay, in the order the help shows
// them when it is opened from nowhere in particular.
//
// Parameters:
//   - e: shared environment, which gives the key map
//
// Returns the sections. Each one is built by the same function as the footer of its screen, and a
// binding that the global section already lists is left out of the others.
func helpSections(e *env) []helpSection {
	k := e.Keys
	suspend := key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("ctrl+z",
		e.t("ui_help_suspend")))

	all := []helpSection{
		{"global", "ui_help_sec_global", []key.Binding{
			k.Palette, k.Help, k.Settings, k.Quit,
			k.Interrupt, k.Exit, suspend,
		}},
		{"home", "ui_help_sec_home", homeBindings(e)},
		{"list", "ui_help_sec_list", commonListBindings(e)},
	}

	for _, kd := range allKinds {
		all = append(all, helpSection{kindSlug[kd], kd.titleKey(), kindBindings(e, kd)})
	}

	all = append(all,
		helpSection{"filter", "ui_help_sec_filter", filterBindings(e)},
		helpSection{
			"config", "ui_help_sec_config",
			configBindings(e, settingDefs[0], false, false),
		},
		helpSection{"form", "ui_help_sec_form", formBindings(e)},
		helpSection{"palette", "ui_help_sec_palette", append(paletteBindings(e, true, true, false),
			restoreBinding(e))},
		helpSection{"confirm", "ui_help_sec_confirm", confirmBindings(e)},
		helpSection{"info", "ui_help_sec_info", infoBindings(e)},
		helpSection{"pick", "ui_help_sec_pick", pickBindings(e)},
		helpSection{"prompt", "ui_help_sec_prompt", promptBindings(e)},
		helpSection{"help", "ui_help_sec_help", helpBindings(e)},
	)

	if e.pluginsOn() {
		all = append(all,
			helpSection{"plugins", "ui_help_sec_plugins", pluginsBindings(e, nil, true)},
			helpSection{"consent", "ui_help_sec_consent", consentBindings(e)},
			helpSection{"listedit", "ui_help_sec_listedit", editorBindings(e)},
		)
	}

	global := all[0].bindings

	for i := 1; i < len(all); i++ {
		isGlobal := func(b key.Binding) bool {
			return slices.ContainsFunc(global, func(g key.Binding) bool {
				return slices.Equal(g.Keys(), b.Keys())
			})
		}

		all[i].bindings = slices.DeleteFunc(slices.Clone(all[i].bindings), isGlobal)
	}

	return all
}

// helpOrder returns the identifiers of the sections to show first for a topic.
//
// Parameters:
//   - topic: identifier of the screen or overlay the help was opened from
func helpOrder(topic string) []string {
	if slices.Contains(kindSlug[:], topic) {
		return []string{topic, "list"}
	}

	return []string{topic}
}

// helpOverlay is the overlay that lists the keys of every screen and a three line quick start.
//
// Topic is the screen it was opened from, whose section comes first. Off is the first visible
// line; Rows and Total are the number of visible lines and of lines, recorded by the last View
// so that the scroll keys know where the end is.
type helpOverlay struct {
	topic string
	off   int
	rows  int
	total int
}

// newHelp returns the help overlay.
//
// Parameters:
//   - topic: identifier of the screen or overlay it was opened from, such as "connections" or
//     "config"; an unknown one shows the sections in their usual order
func newHelp(topic string) overlay {
	return &helpOverlay{topic: topic}
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (h *helpOverlay) Init(_ *env) tea.Cmd {
	return nil
}

// Update implements overlay.
//
// Parameters:
//   - e: shared environment, for the key map
//   - msg: key press or dismissal; other messages are ignored
func (h *helpOverlay) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return h, nil, true
	case wheelMsg:
		h.off = clampInt(h.off+m.Lines, 0, maxOffset(h.rows, h.total))
	case itemClickMsg:
		if by, ok := pageClick(m.ID, pageOf(h.off, h.rows, h.total)); ok {
			h.off = clampInt(h.off+by, 0, maxOffset(h.rows, h.total))
		}
	case tea.KeyPressMsg:
		page := pageOf(h.off, h.rows, h.total)
		last := maxOffset(h.rows, h.total)

		switch {
		case key.Matches(m, e.Keys.Dismiss):
			return h, nil, true
		case key.Matches(m, e.Keys.Up):
			h.off--
		case key.Matches(m, e.Keys.Down):
			h.off++
		case key.Matches(m, e.Keys.PageUp):
			h.off -= page
		case key.Matches(m, e.Keys.PageDown):
			h.off += page
		case key.Matches(m, e.Keys.Home):
			h.off = 0
		case key.Matches(m, e.Keys.End):
			h.off = last
		}

		h.off = max(0, min(h.off, last))
	}

	return h, nil, false
}

// lines lays out the whole text of the help for a width.
//
// Parameters:
//   - e: shared environment
//   - inner: cells available inside the box
func (h *helpOverlay) lines(e *env, inner int) []string {
	k := e.Keys
	keyOf := func(b key.Binding) string { return "[" + sanitize(b.Help().Key) + "]" }

	var out []string

	quick := []string{
		e.t("ui_help_quick_1", keyOf(k.Palette), keyOf(k.Settings), keyOf(k.Help)),
		e.t("ui_help_quick_2", keyOf(k.Jump), keyOf(k.Nav), keyOf(k.Open), keyOf(k.New)),
		e.t("ui_help_quick_3", keyOf(k.Back), keyOf(k.Quit)),
	}

	out = append(out, e.Theme.Bold.Render(e.t("ui_help_sec_quick")))
	out = append(out, bulletLines(e, quick, inner, helpWrapLines)...)

	for _, s := range h.ordered(e) {
		out = append(out, "", e.Theme.Bold.Render(e.t(s.title)))
		out = append(out, bindingLines(e, s.bindings, inner)...)
	}

	mouse := []string{e.t("ui_help_mouse_1"), e.t("ui_help_mouse_2"), e.t("ui_help_mouse_3")}

	out = append(out, "", e.Theme.Bold.Render(e.t("ui_help_sec_mouse")))
	out = append(out, bulletLines(e, mouse, inner, helpWrapLines)...)
	out = append(out, "")

	notes := []string{
		e.t("ui_help_note_safety", keyOf(k.Interrupt), keyOf(k.Exit)),
		e.t("ui_help_note_uninstall"), e.t("ui_help_note_files"), e.t("ui_help_note_keepalive"),
	}

	for _, note := range notes {
		for _, l := range wrapWords(e, note, inner, helpWrapLines) {
			out = append(out, e.Theme.Muted.Render(l))
		}
	}

	return out
}

// bulletLines wraps sentences into bullets.
//
// Parameters:
//   - e: shared environment
//   - texts: one sentence per bullet
//   - inner: cells available
//   - maxLines: most lines per bullet, the last ending with an ellipsis when the text needs more
func bulletLines(e *env, texts []string, inner, maxLines int) []string {
	var out []string

	for _, t := range texts {
		for i, l := range wrapWords(e, t, inner-2, maxLines) {
			prefix := "  "
			if i == 0 {
				prefix = e.Glyphs.sep + " "
			}

			out = append(out, e.Theme.Text.Render(prefix+l))
		}
	}

	return out
}

// ordered returns the sections with the one of the topic first.
//
// Parameters:
//   - e: shared environment
func (h *helpOverlay) ordered(e *env) []helpSection {
	all := helpSections(e)
	first := helpOrder(h.topic)

	var out []helpSection

	for _, id := range first {
		for _, s := range all {
			if s.id == id && len(s.bindings) > 0 {
				out = append(out, s)
			}
		}
	}

	for _, s := range all {
		if !slices.Contains(first, s.id) && len(s.bindings) > 0 {
			out = append(out, s)
		}
	}

	return out
}

// bindingLines draws bindings as a column of keys followed by what they do.
//
// Parameters:
//   - e: shared environment
//   - bs: bindings; those without a key label are skipped
//   - inner: cells available
func bindingLines(e *env, bs []key.Binding, inner int) []string {
	keyW := 0

	for _, b := range bs {
		keyW = max(keyW, e.Metric.width(sanitize(b.Help().Key)))
	}

	keyW = min(keyW, helpKeyMax)

	var out []string

	for _, b := range bs {
		hp := b.Help()
		if hp.Key == "" {
			continue
		}

		k := e.Metric.fit(sanitize(hp.Key), keyW, e.Glyphs.ellipsis)
		pad := spaces(keyW - e.Metric.width(k))

		out = append(out, "  "+e.Theme.Key.Render(k)+pad+"  "+e.Theme.Text.Render(
			e.Metric.fit(sanitize(hp.Desc), max(1, inner-keyW-4), e.Glyphs.ellipsis)))
	}

	return out
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (h *helpOverlay) View(e *env, maxW, maxH int) []string {
	box, _ := h.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: the "N more" lines can be clicked to scroll by a page.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (h *helpOverlay) Zones(e *env, maxW, maxH int) []zone {
	_, zs := h.draw(e, maxW, maxH)

	return zs
}

// draw lays out the box of the help. When the text is longer than the box, a muted "N more" line
// takes the first or the last row.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
//
// Returns the lines of the box and the zones of the "N more" lines, counted from its top left
// corner.
func (h *helpOverlay) draw(e *env, maxW, maxH int) ([]string, []zone) {
	w := max(8, min(maxW, helpWidth))
	inner := w - 4
	all := h.lines(e, inner)

	h.rows = max(1, min(maxH-2, len(all)))
	h.total = len(all)

	win := windowOf(h.off, h.rows, h.total)
	h.off = win.off

	items := make([]string, win.count)
	for i := range items {
		items[i] = " " + e.clipPad(all[win.off+i], inner)
	}

	lines, zs := win.stack(e, items, " ", 1, w-2)

	title := e.Theme.Bold.Render(e.t("ui_help_title"))

	return floating(e, title, lines, w), shiftZones(onlyMore(zs), 1)
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (h *helpOverlay) Keys(e *env) []key.Binding {
	return helpBindings(e)
}

// helpBindings lists the keys of the help.
//
// Parameters:
//   - e: shared environment
func helpBindings(e *env) []key.Binding {
	return []key.Binding{e.Keys.Dismiss, e.Keys.Scroll}
}
