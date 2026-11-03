package ui

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// fieldLimit is the most characters a text field of a generic form accepts when its spec gives no
// limit.
const fieldLimit = 1000

// fieldSpec describes one field of a generic form. The texts are literal, already translated and
// clean: a plugin brings its own words.
//
// Type is fieldText, fieldChoice, fieldToggle or fieldList. Value is the starting text, the
// chosen value, or "true" or "false" for a toggle; Items the starting items of a list. Choices
// are the stored values of a choice and Names what is shown for each, the value itself when Names
// is shorter. Placeholder is the grey text of an empty text field and Max the most characters it
// takes. A field that is Required refuses the empty text. Check gives the translated reason a text
// is refused and CheckList the one for a list, both "" to accept. Locked is the translated reason
// the field cannot be changed and Note a remark shown under it while it has the keys.
type fieldSpec struct {
	ID          string
	Label       string
	Hint        string
	Type        fieldType
	Value       string
	Items       []string
	Choices     []string
	Names       []string
	Placeholder string
	Max         int
	Required    bool
	Check       func(e *env, raw string) string
	CheckList   func(e *env, items []string) string
	Locked      string
	Note        string
}

// build makes the field a spec describes.
func (s fieldSpec) build() *formField {
	var fd *formField

	switch {
	case s.Type == fieldChoice && len(s.Choices) > 0:
		fd = newChoiceField(s.ID, s.Label, s.Choices, s.names(), nil)
		fd.Hint = s.Hint
		fd.Pick = max(0, slices.Index(s.Choices, s.Value))
	case s.Type == fieldToggle:
		fd = newToggleField(s.ID, s.Label, s.Hint)
		fd.On = s.Value == "true"
	case s.Type == fieldList:
		fd = &formField{
			ID: s.ID, Label: s.Label, Hint: s.Hint, Type: fieldList,
			Items: slices.Clone(s.Items),
		}
	default:
		fd = newTextField(s.ID, s.Label, s.Hint, limitOf(s.Max))
		fd.setText(s.Value)
		fd.Default = s.Placeholder
	}

	fd.Raw, fd.Locked, fd.Note = true, s.Locked, s.Note

	return fd
}

// names gives what a choice shows for each of its values.
func (s fieldSpec) names() []string {
	out := make([]string, len(s.Choices))

	for i, c := range s.Choices {
		out[i] = c
		if i < len(s.Names) && s.Names[i] != "" {
			out[i] = s.Names[i]
		}
	}

	return out
}

// limitOf gives the limit of a text field: the one asked for, else fieldLimit.
//
// Parameters:
//   - n: limit of the spec, 0 for none
func limitOf(n int) int {
	if n > 0 {
		return n
	}

	return fieldLimit
}

// fieldValues are the values of a generic form at the moment it is saved. They are copies, so the
// save can run while the form stays on screen.
type fieldValues struct {
	text    map[string]string
	items   map[string][]string
	changed map[string]bool
}

// Text gives the text of a field, the chosen value of a choice or "true" or "false".
//
// Parameters:
//   - id: field id
func (v fieldValues) Text(id string) string {
	return v.text[id]
}

// Items gives the items of a list field.
//
// Parameters:
//   - id: field id
func (v fieldValues) Items(id string) []string {
	return v.items[id]
}

// Changed tells whether the user changed a field since the form opened.
//
// Parameters:
//   - id: field id
func (v fieldValues) Changed(id string) bool {
	return v.changed[id]
}

// fieldsForm is the overlay that edits a set of fields described by specs, with no record of tote
// behind it: the arguments of a command of a plugin, the settings of a plugin. It is the form of
// the records with other validators: the same fields, the same keys and the same box, see
// fieldSet.
//
// Specs are the descriptions in the order of the fields. Save writes the values and returns the
// translated reason it failed, "" when it worked; it runs as a command, off the event loop, and
// Saving is true until it ends, which ignores the keys. Done is the message shown when it worked
// and Banner a standing remark about the form as a whole, shown at the bottom, such as a settings
// file that is ignored. The form never writes anything unless every field is valid and at least
// one changed, and leaves the typed text in place on every failure.
//
// Runs is true for the form that asks the arguments of a command of a plugin. Its Save does not
// write anything: it checks the values and builds the run, so it is not a write that quitting
// must wait for, the form submits even when no field changed, a form that was changed is closed
// by Esc without a question, and After gives the command that continues with the run once the
// form is closed.
type fieldsForm struct {
	fieldSet
	title  string
	specs  []fieldSpec
	save   func(v fieldValues) string
	done   string
	banner string
	saving bool
	runs   bool
	after  func() tea.Cmd
}

// newFieldsForm returns the overlay that edits a set of fields.
//
// Parameters:
//   - title: translated title of the box
//   - specs: the fields, at least one
//   - done: translated message shown when the values were saved
//   - banner: translated remark about the whole form, "" for none
//   - save: writes the values, see fieldsForm
func newFieldsForm(title string, specs []fieldSpec, done, banner string,
	save func(fieldValues) string,
) *fieldsForm {
	f := &fieldsForm{title: title, specs: specs, save: save, done: done, banner: banner}

	for _, s := range specs {
		f.fields = append(f.fields, s.build())
	}

	f.recheck = f.refresh
	f.fields[0].focus()
	f.base = f.texts()

	return f
}

// Init implements overlay: it checks the fields as they are, which shows the problem of a value
// that is already wrong.
//
// Parameters:
//   - e: shared environment
func (f *fieldsForm) Init(e *env) tea.Cmd {
	f.refresh(e)

	return nil
}

// refresh checks every field again.
//
// Parameters:
//   - e: shared environment
func (f *fieldsForm) refresh(e *env) {
	f.problem = f.banner

	for i, fd := range f.fields {
		fd.Err = ""
		s := f.specs[i]

		switch {
		case fd.Locked != "":
		case fd.Type == fieldList:
			if s.CheckList != nil {
				fd.Err = s.CheckList(e, fd.Items)
			}
		case fd.Type == fieldText:
			raw := fd.text()

			switch {
			case raw == "" && s.Required:
				fd.Err = e.t("ui_rule_required")
			case s.Check != nil:
				fd.Err = s.Check(e, raw)
			}
		}
	}
}

// ruleText words the rule that a value of a field of a plugin breaks.
//
// Parameters:
//   - rule: one of the rules of plugin.FieldRule, "" for none
func (e *env) ruleText(rule string) string {
	switch rule {
	case "":
		return ""
	case "required":
		return e.t("ui_rule_required")
	case "max_len", "length":
		return e.pword("plugin_reject_", plugin.RejectLength)
	case "pattern":
		return e.pword("plugin_reject_", plugin.RejectPattern)
	case "choice":
		return e.pword("plugin_reject_", plugin.RejectChoice)
	case "bool":
		return e.pword("plugin_reject_", plugin.RejectType)
	}

	return e.pword("plugin_reject_", plugin.RejectText)
}

// values copies the values of the fields.
func (f *fieldsForm) values() fieldValues {
	v := fieldValues{
		text: map[string]string{}, items: map[string][]string{},
		changed: map[string]bool{},
	}

	for i, fd := range f.fields {
		v.changed[fd.ID] = fd.text() != f.base[i]

		if fd.Type == fieldList {
			v.items[fd.ID] = slices.Clone(fd.Items)

			continue
		}

		v.text[fd.ID] = fd.text()
	}

	return v
}

// Update implements overlay.
//
// Parameters:
//   - e: shared environment
//   - msg: key press, paste, click, the end of a save or the result of the list editor; other
//     messages are ignored
func (f *fieldsForm) Update(e *env, msg tea.Msg) (overlay, tea.Cmd, bool) {
	switch m := msg.(type) {
	case dismissMsg:
		return f, nil, !f.saving
	case fieldsSavedMsg:
		return f.saved(m)
	case listEditedMsg:
		f.edited(e, m)
	}

	if f.saving {
		return f, nil, false
	}

	switch m := msg.(type) {
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

// edited takes the items a list editor gives back.
//
// Parameters:
//   - e: shared environment
//   - m: the result
func (f *fieldsForm) edited(e *env, m listEditedMsg) {
	for _, fd := range f.fields {
		if fd.ID == m.ID && fd.Type == fieldList && fd.Locked == "" {
			fd.Items, fd.Touched = slices.Clone(m.Items), true
			f.changed(e)
		}
	}
}

// key handles a key press.
//
// Parameters:
//   - e: shared environment
//   - k: key pressed
func (f *fieldsForm) key(e *env, k tea.KeyPressMsg) (overlay, tea.Cmd, bool) {
	switch f.navigate(e, k) {
	case fkCancel:
		return f, nil, true
	case fkSubmit:
		return f.submit(e)
	case fkOpen:
		return f, f.open(), false
	}

	return f, nil, false
}

// open starts the editor of the list that has the keys. A list that is locked is shown as it is.
func (f *fieldsForm) open() tea.Cmd {
	fd, s := f.fields[f.cur], f.specs[f.cur]
	if fd.Locked != "" {
		return nil
	}

	ed := newListEditor(sanitize(fd.Label), fd.ID, fd.Items, func(e *env, item string) string {
		if s.Check != nil {
			return s.Check(e, item)
		}

		return ""
	}, s.CheckList)

	return msgCmd(openOverlayMsg{Overlay: ed})
}

// submit saves the values when every field is valid and one changed.
//
// Parameters:
//   - e: shared environment
func (f *fieldsForm) submit(e *env) (overlay, tea.Cmd, bool) {
	f.submitted = true
	f.refresh(e)

	for i, fd := range f.fields {
		if fd.Err != "" {
			f.focusOn(e, i)
			f.msg = e.t("ui_form_invalid")

			return f, nil, false
		}
	}

	if !f.runs && !f.Dirty() {
		return f, toastCmd(e.t("ui_form_unchanged"), false), true
	}

	f.saving, f.msg = true, ""
	v, save, runs := f.values(), f.save, f.runs

	if !runs {
		e.writing(1)
	}

	return f, func() tea.Msg {
		if !runs {
			defer e.writing(-1)
		}

		return fieldsSavedMsg{Form: f, Fail: save(v)}
	}, false
}

// saved handles the end of a save.
//
// Parameters:
//   - m: its result
func (f *fieldsForm) saved(m fieldsSavedMsg) (overlay, tea.Cmd, bool) {
	if m.Form != f {
		return f, nil, false
	}

	f.saving = false

	if m.Fail != "" {
		f.msg = m.Fail

		return f, nil, false
	}

	f.base = f.texts()

	if f.after != nil {
		return f, f.after(), true
	}

	return f, toastCmd(f.done, false), true
}

// Dirty implements dirtier: it reports that a field differs from what the form opened with. A
// form that asks the arguments of a run holds nothing worth a question.
func (f *fieldsForm) Dirty() bool {
	return !f.runs && f.fieldSet.Dirty()
}

// View implements overlay.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (f *fieldsForm) View(e *env, maxW, maxH int) []string {
	box, _ := f.layout(e, f.title, maxW, maxH)

	return box
}

// Zones implements zonedOverlay: a click on the line of a field gives it the keys.
//
// Parameters:
//   - e: shared environment
//   - maxW: widest line allowed
//   - maxH: most lines allowed
func (f *fieldsForm) Zones(e *env, maxW, maxH int) []zone {
	_, zs := f.layout(e, f.title, maxW, maxH)

	return zs
}

// Keys implements overlay.
//
// Parameters:
//   - e: shared environment
func (f *fieldsForm) Keys(e *env) []key.Binding {
	if f.runs {
		return f.runBindings(e)
	}

	bs := formBindings(e)

	if fd := f.fields[f.cur]; fd.Type == fieldList && fd.Locked == "" {
		bs = slices.Insert(bs, 1, key.NewBinding(key.WithKeys("enter"),
			key.WithHelp(e.Glyphs.enter, e.t("ui_help_edit"))))
	}

	return bs
}

// runBindings lists the keys of a form that asks the arguments of a run: Enter goes to the next
// field and runs on the last one, Ctrl-S runs from anywhere, and nothing is saved.
//
// Parameters:
//   - e: shared environment
func (f *fieldsForm) runBindings(e *env) []key.Binding {
	last := f.cur == len(f.fields)-1
	word := e.t("ui_help_next")

	if last {
		word = e.t("ui_help_run")
	}

	var bs []key.Binding

	switch fd := f.fields[f.cur]; {
	case fd.Type == fieldList && fd.Locked == "":
		bs = append(bs, key.NewBinding(key.WithKeys("enter"),
			key.WithHelp(e.Glyphs.enter, e.t("ui_help_edit"))))
	default:
		bs = append(bs, key.NewBinding(key.WithKeys("enter"), key.WithHelp(e.Glyphs.enter, word)))
	}

	if len(f.fields) > 1 {
		bs = append(bs, key.NewBinding(key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", e.t("ui_help_run"))))
	}

	return append(bs, key.NewBinding(key.WithKeys("esc"),
		key.WithHelp("esc", e.t("ui_help_cancel"))))
}

// commandSpecs describes the fields of a contribution of a plugin: a command, an item action or a
// list action. The texts come from the catalog of the plugin, the starting values from the
// defaults the registry expanded, and the checks are the ones a run applies.
//
// Parameters:
//   - e: shared environment, for the language
//   - fields: the fields of the contribution
//   - cat: catalog of the plugin
//   - base: key prefix of the contribution in the catalog, such as command.show
//   - defaults: starting value of each field by id, from Registry.Defaults
func commandSpecs(e *env, fields []plugin.Field, cat plugin.Catalog, base string,
	defaults map[string]string,
) []fieldSpec {
	out := make([]fieldSpec, 0, len(fields))

	for _, pf := range fields {
		key := base + ".field." + pf.ID
		s := fieldSpec{
			ID:       pf.ID,
			Label:    e.ptext(cat, key+".label", pf.Label),
			Hint:     e.ptext(cat, key+".hint", pf.Hint),
			Value:    defaults[pf.ID],
			Max:      pf.MaxLen,
			Required: pf.Required,
			Check: func(e *env, raw string) string {
				return e.ruleText(plugin.FieldRule(pf, raw))
			},
		}

		switch pf.Type {
		case "bool":
			s.Type = fieldToggle
		case "choice":
			s.Type, s.Choices = fieldChoice, pf.Choices

			for _, c := range pf.Choices {
				seg := c
				if seg == "" {
					seg = "_"
				}

				s.Names = append(s.Names, e.ptext(cat, key+".choice."+seg, c))
			}
		}

		out = append(out, s)
	}

	return out
}
