package ui

import (
	"strings"

	"github.com/totecli/tote/internal/store"
)

const (
	// panelLines is the number of content lines of the detail panel, whatever the kind.
	panelLines = 3

	// panelRows is the total height of the detail panel: its content plus the two borders.
	panelRows = panelLines + 2
)

// joinFields joins the non-empty fields of a line with the separator glyph.
//
// Parameters:
//   - e: shared environment
//   - fields: sanitized texts, empty ones are skipped
func joinFields(e *env, fields ...string) string {
	var kept []string

	for _, f := range fields {
		if f != "" {
			kept = append(kept, f)
		}
	}

	return strings.Join(kept, " "+e.Glyphs.sep+" ")
}

// labeled formats a field with its label, or nothing when the value is empty.
//
// Parameters:
//   - e: shared environment
//   - key: catalog key of the label
//   - value: raw value
func labeled(e *env, key, value string) string {
	if value == "" {
		return ""
	}

	return e.t(key) + " " + sanitize(value)
}

// detailLines returns the text of the detail panel for an item, three lines at most.
//
// Parameters:
//   - e: shared environment
//   - it: item to describe
//   - user: login chosen for the next connection, empty for the stored one
//
// Returns sanitized plain lines. For a connection the first line is the exact command that
// Enter will run, built by shell.ConnectArgv and shell.PrettyCommand, or the reason it cannot
// run. The other kinds show their fields.
func detailLines(e *env, it item, user string) []string {
	switch r := it.Rec.(type) {
	case store.Connection:
		cmd, _ := commandOf(e, it, user)

		return []string{
			cmd,
			joinFields(e, labeled(e, "ui_field_via", r.Via),
				labeled(e, "ui_field_aliases", strings.Join(r.Aliases, ", ")),
				labeled(e, "ui_field_ip", r.IP)),
			sanitize(r.Desc),
		}
	case store.Repo:
		return []string{sanitize(r.Path), sanitize(r.Desc)}
	case store.Alias:
		return []string{sanitize(r.Cmd), sanitize(r.Desc)}
	case store.Func:
		managed := ""
		if r.Managed {
			managed = e.t("ui_field_managed")
		}

		return []string{
			sanitize(r.Script), joinFields(e, sanitize(r.Kind), managed), sanitize(r.Desc),
		}
	}

	return nil
}

// detailPanel draws the panel under a list for the selected item.
//
// Parameters:
//   - e: shared environment
//   - it: selected item
//   - w: width available
//   - user: login chosen for the next connection, empty for the stored one
//
// Returns panelRows lines. The name of the record is in the top border; a warning on the record
// takes the last line.
func detailPanel(e *env, it item, w int, user string) []string {
	inner := w - 2

	name := e.Metric.fit(sanitize(it.Key), max(1, inner-6), e.Glyphs.ellipsis)
	title := e.Theme.Muted.Render(name)
	lines := make([]string, panelLines)

	shown := detailLines(e, it, user)

	for i := 0; i < len(shown) && i < panelLines; i++ {
		l := shown[i]
		style := e.Theme.Text
		if i > 0 {
			style = e.Theme.Muted
		}

		if i == 0 && it.Kind == kindConn && strings.HasPrefix(l, e.Glyphs.warn) {
			style = e.Theme.Err
		}

		lines[i] = " " + style.Render(e.Metric.fit(l, max(1, inner-2), e.Glyphs.ellipsis))
	}

	if it.Warn != "" {
		lines[panelLines-1] = " " + e.Theme.Err.Render(e.Metric.fit(e.Glyphs.warn+" "+it.Warn,
			max(1, inner-2), e.Glyphs.ellipsis))
	}

	return boxed(e, title, lines, w)
}
