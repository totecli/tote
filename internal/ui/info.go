package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// infoWidth is the widest the box of the details of a record is drawn.
const infoWidth = 84

// infoMaxLines is the most lines the value of one field takes in the details.
const infoMaxLines = 4

// infoField is one line of the details of a record: the catalog key of its label and its value.
type infoField struct {
	label string
	value string
}

// infoOverlay is the overlay that shows every field of a record, with the exact command for a
// connection, which the columns of the list cut.
//
// Fields are the lines to show, Warn the reason the record cannot be used and Off the first
// visible line, scrolled with the arrow keys when the box is shorter than the details. Rows and
// Total are recorded by the last View for the scroll keys.
type infoOverlay struct {
	title  string
	fields []infoField
	warn   string
	off    int
	rows   int
	total  int
}

// newInfo returns the overlay that shows the fields of a record.
//
// Parameters:
//   - e: shared environment
//   - it: the record
//   - user: login chosen for the next connection, empty for the stored one
func newInfo(e *env, it item, user string) overlay {
	return &infoOverlay{title: sanitize(it.Key), fields: infoFields(e, it, user), warn: it.Warn}
}

// infoFields lists the fields to show for a record.
//
// Parameters:
//   - e: shared environment
//   - it: the record
//   - user: login chosen for the next connection, empty for the stored one
//
// Returns the fields in the order of the form, the empty ones left out, every value sanitized.
func infoFields(e *env, it item, user string) []infoField {
	var out []infoField

	add := func(label, value string) {
		if value != "" {
			out = append(out, infoField{label: e.t(label), value: sanitize(value)})
		}
	}

	switch r := it.Rec.(type) {
	case store.Connection:
		cmd, _ := commandOf(e, it, user)
		add("ui_field_target", r.Target)
		add("ui_field_via", r.Via)
		add("ui_field_user", r.User)
		add("ui_field_aliases", strings.Join(r.Aliases, ", "))
		add("ui_field_ip", r.IP)
		add("ui_field_desc", r.Desc)
		add("ui_field_command", strings.TrimPrefix(strings.TrimPrefix(cmd, e.Glyphs.arrow+" "),
			e.Glyphs.warn+" "))

		if user != "" {
			add("ui_field_once", user)
		}
	case store.Repo:
		add("ui_field_path", r.Path)
		add("ui_field_resolved", shell.ExpandHome(r.Path, e.Home))
		add("ui_field_desc", r.Desc)
	case store.Alias:
		add("ui_field_cmd", r.Cmd)
		add("ui_field_desc", r.Desc)
	case store.Func:
		add("ui_field_script", r.Script)
		add("ui_field_kind", r.Kind)

		if r.Managed {
			add("ui_field_managed", e.t("ui_choice_yes"))
		}

		add("ui_field_desc", r.Desc)
	}

	return out
}

// viewRecord opens the details of a record.
//
// Parameters:
//   - m: kind, key and login of the record
func (r *root) viewRecord(m viewMsg) tea.Cmd {
	e := r.e

	for _, it := range e.Data.items(m.Kind) {
		if it.Key == m.Key {
			return r.push(newInfo(e, it, m.User))
		}
	}

	return r.say(e.t("cli_not_found", e.Data.kindName(m.Kind), sanitize(m.Key)), true)
}

// Init implements overlay.
//
// Parameters:
//   - e: shared environment, unused
func (o *infoOverlay) Init(_ *env) tea.Cmd {
	return nil
}

// Update implements overlay.
//
// Parameters:
//   - e: shared environment, for the key map
//   - msg: key press or dismissal; other messages are ignored
func (o *infoOverlay) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return o, nil, true
	case wheelMsg:
		o.off = clampInt(o.off+m.Lines, 0, maxOffset(o.rows, o.total))
	case itemClickMsg:
		if by, ok := pageClick(m.ID, pageOf(o.off, o.rows, o.total)); ok {
			o.off = clampInt(o.off+by, 0, maxOffset(o.rows, o.total))
		}
	case tea.KeyPressMsg:
		last := maxOffset(o.rows, o.total)
		page := pageOf(o.off, o.rows, o.total)

		switch {
		case key.Matches(m, e.Keys.Close), key.Matches(m, e.Keys.View):
			return o, nil, true
		case key.Matches(m, e.Keys.Up):
			o.off--
		case key.Matches(m, e.Keys.Down):
			o.off++
		case key.Matches(m, e.Keys.PageUp):
			o.off -= page
		case key.Matches(m, e.Keys.PageDown):
			o.off += page
		case key.Matches(m, e.Keys.Home):
			o.off = 0
		case key.Matches(m, e.Keys.End):
			o.off = last
		}

		o.off = max(0, min(o.off, last))
	}

	return o, nil, false
}

// lines lays out the details for a width.
//
// Parameters:
//   - e: shared environment
//   - inner: cells available inside the box
func (o *infoOverlay) lines(e *env, inner int) []string {
	labelW := 0

	for _, f := range o.fields {
		labelW = max(labelW, e.Metric.width(f.label))
	}

	labelW = min(labelW, inner/3)
	room := max(1, inner-labelW-2)

	var out []string

	for _, f := range o.fields {
		parts := wrapWords(e, f.value, room, infoMaxLines)
		if len(parts) == 0 {
			parts = []string{""}
		}

		for i, p := range parts {
			label := ""
			if i == 0 {
				label = e.Metric.fit(f.label, labelW, e.Glyphs.ellipsis)
			}

			out = append(out, e.Theme.Muted.Render(label+spaces(labelW-e.Metric.width(label))+
				"  ")+e.Theme.Text.Render(p))
		}
	}

	if o.warn != "" {
		out = append(out, "", e.Theme.Err.Render(e.Metric.fit(e.Glyphs.warn+" "+o.warn, inner,
			e.Glyphs.ellipsis)))
	}

	return out
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (o *infoOverlay) View(e *env, maxW, maxH int) []string {
	box, _ := o.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: the "N more" lines can be clicked to scroll by a page.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (o *infoOverlay) Zones(e *env, maxW, maxH int) []zone {
	_, zs := o.draw(e, maxW, maxH)

	return zs
}

// draw lays out the box of the details. When the text is longer than the box, a muted "N more"
// line takes the first or the last row.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
//
// Returns the lines of the box and the zones of the "N more" lines, counted from its top left
// corner.
func (o *infoOverlay) draw(e *env, maxW, maxH int) ([]string, []zone) {
	w := max(8, min(maxW, infoWidth))
	inner := w - 4
	all := o.lines(e, inner)

	o.rows = max(1, min(maxH-2, max(1, len(all))))
	o.total = len(all)

	win := windowOf(o.off, o.rows, o.total)
	o.off = win.off

	items := make([]string, win.count)
	for i := range items {
		items[i] = " " + e.clipPad(all[win.off+i], inner)
	}

	lines, zs := win.stack(e, items, " ", 1, w-2)
	lines = fitLines(lines, o.rows)
	title := e.Theme.Bold.Render(e.Metric.fit(o.title, max(1, w-8), e.Glyphs.ellipsis))

	return floating(e, title, lines, w), shiftZones(onlyMore(zs), 1)
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (o *infoOverlay) Keys(e *env) []key.Binding {
	return infoBindings(e)
}

// infoBindings lists the keys of the details of a record.
//
// Parameters:
//   - e: shared environment
func infoBindings(e *env) []key.Binding {
	return []key.Binding{e.Keys.Close, e.Keys.Scroll}
}
