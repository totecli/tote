package ui

import (
	"errors"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/store"
)

// formWidth is the widest the box of a form gets, in cells.
const formWidth = 72

// formLabelMax is the widest the column of labels gets, in cells.
const formLabelMax = 14

// formMinRows is the fewest rows the fields keep inside the box: the failure message of the form
// is cut, or dropped, before the fields have fewer.
const formMinRows = 3

// form is the overlay that creates or edits a record of one kind.
//
// OldKey is the key of the record being edited, empty when a new one is created, and Orig is that
// record. The fields and the keys that move between them are those of the fieldSet. The form never
// writes anything but through Update with a valid record, and leaves the typed text in place on
// every refusal.
type form struct {
	fieldSet
	kind   kind
	oldKey string
	orig   any
}

// newForm returns the overlay that creates or edits a record.
//
// Parameters:
//   - k: kind of the record
//   - rec: the record to edit as a store.Connection, store.Repo, store.Alias or store.Func
//     value, nil to create one. On a successful write the form calls e.Data.save and returns
//     wroteCmd with the new key
func newForm(k kind, rec any) overlay {
	fields, oldKey := formFields(k, rec)
	f := &form{kind: k, oldKey: oldKey, fieldSet: fieldSet{fields: fields}}

	if oldKey != "" {
		f.orig = rec
	}

	f.recheck, f.entered = f.refresh, f.suggest
	fields[0].focus()

	f.base = f.texts()

	return f
}

// formTitleKey returns the catalog key of the title of a form.
//
// Parameters:
//   - k: kind of the record
//   - edit: true for the form of an existing record
func formTitleKey(k kind, edit bool) string {
	if edit {
		return [...]string{
			"ui_form_edit_connection", "ui_form_edit_repo", "ui_form_edit_alias",
			"ui_form_edit_func",
		}[k]
	}

	return [...]string{
		"ui_form_new_connection", "ui_form_new_repo", "ui_form_new_alias", "ui_form_new_func",
	}[k]
}

// Init implements overlay: it checks the fields as they are, which shows the problem of a
// record that is already wrong.
//
// Parameters:
//   - e: shared environment
func (f *form) Init(e *env) tea.Cmd {
	f.refresh(e)

	return nil
}

// values returns the text of every field by its ID, normalized the way the command line does.
func (f *form) values() map[string]string {
	vals := make(map[string]string, len(f.fields))

	for _, fd := range f.fields {
		vals[fd.ID] = fd.text()
	}

	if f.kind == kindConn {
		normalizeConn(vals)
	}

	return vals
}

// refresh checks every field again, then the record as a whole when all the fields are valid.
//
// Parameters:
//   - e: shared environment, which gives the data to compare names with
func (f *form) refresh(e *env) {
	vals := f.values()
	valid := true

	for _, fd := range f.fields {
		fd.Err = ""

		if fd.Type == fieldText {
			fd.Err = f.fieldProblem(e, fd.ID, vals)
		}

		valid = valid && fd.Err == ""
	}

	f.problem = ""
	if valid {
		f.problem = f.taken(e, vals)
	}
}

// fieldProblem checks one text field.
//
// Parameters:
//   - e: shared environment
//   - id: field to check
//   - vals: text of each field by its ID
//
// Returns the translated reason the value is refused, empty when it is accepted.
func (f *form) fieldProblem(e *env, id string, vals map[string]string) string {
	var err error

	if f.kind == kindConn {
		err = probeConn(id, vals)
	} else {
		err = probeShell(f.kind, id, vals)
	}

	if err == nil {
		err = checkPath(f.kind, id, vals)
	}

	return fieldError(e, err)
}

// taken tells whether the record as typed clashes with another one.
//
// Parameters:
//   - e: shared environment
//   - vals: text of each field by its ID, all valid
//
// Returns the same message as the command line, empty when the name is free.
func (f *form) taken(e *env, vals map[string]string) string {
	rec := buildRecord(f.kind, vals)
	name := nameOf(rec)

	if name != f.oldKey {
		if _, found := e.Data.record(f.kind, name); found {
			return e.t("cli_exists", e.Data.kindName(f.kind), sanitize(name))
		}
	}

	if err := e.Data.clash(f.kind, f.oldKey, rec); err != nil {
		return err.Error()
	}

	return ""
}

// Update implements overlay.
//
// Parameters:
//   - e: shared environment
//   - msg: key press, paste or dismissal; other messages are ignored
func (f *form) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return f, nil, true
	case tea.PasteMsg:
		f.typed(e, m)
	case itemClickMsg:
		if m.ID >= 0 && m.ID < len(f.fields) && m.ID != f.cur {
			f.jump(e, m.ID)
		}
	case tea.KeyPressMsg:
		return f.key(e, m)
	}

	return f, nil, false
}

// key handles a key press.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (f *form) key(e *env, k tea.KeyPressMsg) (overlay, tea.Cmd, bool) {
	switch f.navigate(e, k) {
	case fkCancel:
		return f, nil, true
	case fkSubmit:
		return f.submit(e)
	}

	return f, nil, false
}

// suggest fills the path of a new repository with the repositories root and the name, when the
// path was not touched and a root is set.
//
// Parameters:
//   - e: shared environment, which gives the root
func (f *form) suggest(e *env) {
	fd := f.fields[f.cur]

	if f.kind != kindRepo || f.oldKey != "" || fd.ID != "path" || fd.Touched ||
		fd.Input.Value() != "" || e.Settings.ReposRoot == "" {
		return
	}

	name := f.fields[0].text()
	if name == "" {
		return
	}

	fd.setText(strings.TrimRight(e.Settings.ReposRoot, "/") + "/" + name)
}

// submit writes the record when everything is valid.
//
// Parameters:
//   - e: shared environment, which gives the data
//
// Returns the form with its reason when the record is refused, and a done form with the command
// that reports the write when it was made. A record that is the same as the one being edited is
// not written.
func (f *form) submit(e *env) (overlay, tea.Cmd, bool) {
	f.submitted = true
	f.refresh(e)

	for i, fd := range f.fields {
		if fd.Err != "" {
			f.focusOn(e, i)
			f.msg = e.t("ui_form_invalid")

			return f, nil, false
		}
	}

	if f.problem != "" {
		f.focusOn(e, 0)

		return f, nil, false
	}

	rec := buildRecord(f.kind, f.values())

	if f.oldKey != "" && sameRecord(rec, f.orig) {
		return f, toastCmd(e.t("ui_form_unchanged"), false), true
	}

	if err := e.Data.save(f.kind, f.oldKey, rec); err != nil {
		f.msg = err.Error()

		var ue *userError
		if errors.As(err, &ue) && ue.field != "" {
			f.focusID(e, ue.field)
		}

		if staleWrite(err) {
			return f, msgCmd(refreshMsg{Kind: f.kind}), false
		}

		return f, nil, false
	}

	op := opAdd
	if f.oldKey != "" {
		op = opEdit
	}

	return f, wroteCmd(f.kind, nameOf(rec), op), true
}

// sameRecord tells whether two records hold the same values.
//
// Parameters:
//   - a: a store.Connection, store.Repo, store.Alias or store.Func value
//   - b: another one
func sameRecord(a, b any) bool {
	ca, okA := a.(store.Connection)
	cb, okB := b.(store.Connection)

	if okA && okB {
		return slices.Equal(ca.Aliases, cb.Aliases) && ca.Name == cb.Name &&
			ca.Via == cb.Via && ca.Target == cb.Target && ca.User == cb.User &&
			ca.IP == cb.IP && ca.Desc == cb.Desc && ca.NeedsReview == cb.NeedsReview
	}

	return a == b
}

// draw lays out the box of the form.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
//
// Returns the lines of the box and the zones of the fields that are on them, counted from its top
// left corner.
func (f *form) draw(e *env, maxW, maxH int) ([]string, []zone) {
	return f.layout(e, e.t(formTitleKey(f.kind, f.oldKey != "")), maxW, maxH)
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed; when the form is taller, the fields scroll to keep the one that
//     has the keys in view, and the failure message stays at the bottom
func (f *form) View(e *env, maxW, maxH int) []string {
	box, _ := f.draw(e, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: a click on the line of a field gives it the keys.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (f *form) Zones(e *env, maxW, maxH int) []zone {
	_, zs := f.draw(e, maxW, maxH)

	return zs
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (f *form) Keys(e *env) []key.Binding {
	return formBindings(e)
}

// formBindings lists the keys of the form.
//
// Parameters:
//   - e: shared environment
func formBindings(e *env) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", e.t("ui_help_next"))),
		key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", e.t("ui_help_save"))),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", e.t("ui_help_cancel"))),
	}
}
