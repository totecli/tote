package ui

import (
	"os"
	"strings"
	"testing"
)

// edgeForm opens a generic form over a rig.
//
// Parameters:
//   - t: running test
//   - specs: the fields
//   - save: save function, nil for one that works
func edgeForm(t *testing.T, specs []fieldSpec, save func(fieldValues) string) (*rig, *fieldsForm) {
	t.Helper()

	if save == nil {
		save = func(fieldValues) string { return "" }
	}

	f := newFieldsForm("Edge", specs, "saved it", "", save)
	g := newRig(t, newSandbox(t))
	g.send(openOverlayMsg{Overlay: f})

	if g.top() != f {
		t.Fatalf("the form is not on top: %T", g.top())
	}

	return g, f
}

// TestFieldsFormLocked checks that a field that is locked is shown as it is: it takes no key, it
// is not checked, and a list in it opens no editor.
//
// Parameters:
//   - t: test to report to
func TestFieldsFormLocked(t *testing.T) {
	refuse := func(e *env, raw string) string { return e.ruleText("pattern") }
	specs := []fieldSpec{
		{
			ID: "env", Label: "Env", Type: fieldText, Value: "BAD", Locked: "set elsewhere",
			Check: refuse,
		},
		{ID: "tags", Label: "Tags", Type: fieldList, Items: []string{"a"}, Locked: "set elsewhere"},
		{ID: "free", Label: "Free", Type: fieldText, Value: "x"},
	}

	var saved []fieldValues

	g, f := edgeForm(t, specs, func(v fieldValues) string {
		saved = append(saved, v)

		return ""
	})

	g.typeText("zz")

	if got := f.fields[0].text(); got != "BAD" || f.fields[0].Touched {
		t.Errorf("a locked field took keys: %q, touched %v", got, f.fields[0].Touched)
	}

	g.press("tab", "enter")

	if g.top() != f {
		t.Fatalf("enter on a locked list opened %T", g.top())
	}

	if bs := f.Keys(g.r.e); len(bs) != 3 {
		t.Errorf("the footer of a locked list has %d keys, want the three of a form", len(bs))
	}

	g.press("tab")
	g.typeText("y")
	g.press("ctrl+s")
	g.await("the save", func() bool { return g.top() == nil })

	if len(saved) != 1 || saved[0].Text("free") != "xy" || saved[0].Changed("env") {
		t.Errorf("saved = %+v", saved)
	}
}

// TestFieldsFormStart checks how the fields start and how the keys move between them: a toggle
// starts as its value says, typing marks a field as touched, and Tab and Shift-Tab wrap around.
//
// Parameters:
//   - t: test to report to
func TestFieldsFormStart(t *testing.T) {
	g, f := edgeForm(t, []fieldSpec{
		{ID: "a", Label: "A", Type: fieldText},
		{ID: "b", Label: "B", Type: fieldToggle, Value: "true"},
		{ID: "c", Label: "C", Type: fieldText},
	}, nil)

	if !f.fields[1].On || !strings.Contains(g.text(), "[x]") {
		t.Errorf("the toggle does not start on:\n%s", g.text())
	}

	if f.fields[0].Touched {
		t.Error("a field is touched before a key")
	}

	g.typeText("q")

	if !f.fields[0].Touched {
		t.Error("typing did not touch the field")
	}

	g.press("shift+tab")

	if f.cur != 2 {
		t.Errorf("shift+tab from the first field went to %d, want the last", f.cur)
	}

	g.press("tab")

	if f.cur != 0 {
		t.Errorf("tab from the last field went to %d, want the first", f.cur)
	}
}

// TestFieldsFormSaving checks the form while a save runs: the flag is set at once, a dismissal and
// the end of another form's save are not taken, and the end of its own closes it.
//
// Parameters:
//   - t: test to report to
func TestFieldsFormSaving(t *testing.T) {
	gate := make(chan struct{})

	g, f := edgeForm(t, []fieldSpec{{ID: "a", Label: "A", Type: fieldText}},
		func(fieldValues) string {
			<-gate

			return ""
		})

	g.typeText("v")
	g.press("ctrl+s")

	if !f.saving || !f.submitted {
		t.Fatalf("after ctrl+s: saving %v, submitted %v", f.saving, f.submitted)
	}

	if _, _, done := f.Update(g.r.e, dismissMsg{}); done {
		t.Error("a dismissal closed the form while it saved")
	}

	other := &fieldsForm{}

	if _, _, done := f.Update(g.r.e, fieldsSavedMsg{Form: other}); done || !f.saving {
		t.Errorf("the end of a foreign save was taken: done %v, saving %v", done, f.saving)
	}

	close(gate)
	g.await("the save", func() bool { return g.top() == nil })

	g2, f2 := edgeForm(t, []fieldSpec{{ID: "a", Label: "A", Type: fieldText}}, nil)

	if _, _, done := f2.Update(g2.r.e, dismissMsg{}); !done {
		t.Error("a dismissal did not close a form that does not save")
	}
}

// TestFieldsFormShowsErrorsAfterSubmit checks that an invalid value that was never typed is told
// once the user asks to save.
//
// Parameters:
//   - t: test to report to
func TestFieldsFormShowsErrorsAfterSubmit(t *testing.T) {
	g, f := edgeForm(t, []fieldSpec{
		{ID: "a", Label: "A", Type: fieldText},
		{ID: "b", Label: "B", Type: fieldText},
		{ID: "c", Label: "C", Type: fieldText, Required: true},
	}, nil)

	g.press("tab")

	if strings.Contains(g.text(), "a value is required") {
		t.Fatalf("an untouched field is refused before the save:\n%s", g.text())
	}

	g.press("ctrl+s")

	if !f.submitted || !strings.Contains(g.text(), "a value is required") {
		t.Errorf("submitted %v:\n%s", f.submitted, g.text())
	}
}

// TestSettingsEmptyGivesTheDefault checks that emptying a text setting that has a default gives
// the default back: the key leaves the file and the plugin runs with the default again.
//
// Parameters:
//   - t: test to report to
func TestSettingsEmptyGivesTheDefault(t *testing.T) {
	g, l, _ := settingsRig(t, func(l *plugLab) {
		writeFile(t, l.settingsFile(), "schema = 1\nowner = \"mine\"\nlimit = 20\n")
	})

	clearField(g, len("mine"))
	g.press("tab", "tab")
	clearField(g, 2)
	g.press("ctrl+s")
	g.await("the save", func() bool { return g.top() == nil })

	data, err := os.ReadFile(l.settingsFile())
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(data), "owner") || strings.Contains(string(data), "limit") {
		t.Errorf("the file keeps emptied settings:\n%s", data)
	}

	got := l.stored(t)
	if got["owner"].Value != "someone" || got["limit"].Value != int64(10) {
		t.Errorf("stored = %+v", got)
	}
}

// TestListEditorEdges checks the corners of the list editor: delete on an empty list does
// nothing, Esc closes, and the list it gives back is a copy.
//
// Parameters:
//   - t: test to report to
func TestListEditorEdges(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	empty := newListEditor("Tags", "tags", nil, nil, nil)
	if _, _, done := empty.Update(e, keyMsg("d")); done {
		t.Error("d on an empty list closed the editor")
	}

	if _, _, done := empty.Update(e, keyMsg("esc")); !done {
		t.Error("esc did not close the editor")
	}

	ed := newListEditor("Tags", "tags", []string{"a", "b"}, nil, nil).(*listEditor)

	_, cmd, done := ed.Update(e, keyMsg("ctrl+s"))
	if !done || cmd == nil {
		t.Fatalf("ctrl+s: done %v, command %v", done, cmd != nil)
	}

	m, ok := cmd().(listEditedMsg)
	if !ok {
		t.Fatalf("ctrl+s gave %T", cmd())
	}

	ed.items[0] = "changed"

	if m.Items[0] != "a" {
		t.Errorf("the list handed back shares the draft: %v", m.Items)
	}
}

// TestListEditorDismiss checks that the list editor closes when the interface dismisses it.
//
// Parameters:
//   - t: test to report to
func TestListEditorDismiss(t *testing.T) {
	g, _, _ := settingsRig(t, nil)
	g.press("down", "down", "down", "down", "enter")

	ed, ok := g.top().(*listEditor)
	if !ok {
		t.Fatalf("enter opened %T", g.top())
	}

	if _, _, done := ed.Update(g.r.e, dismissMsg{}); !done {
		t.Error("dismissMsg did not close the list editor")
	}
}

// TestHostileText checks that the words of a list item and of a missing key never reach the
// screen unclean.
//
// Parameters:
//   - t: test to report to
func TestHostileText(t *testing.T) {
	g, _, _ := settingsRig(t, nil)
	g.press("down", "down", "down", "down", "enter")

	ed, ok := g.top().(*listEditor)
	if !ok {
		t.Fatalf("enter opened %T", g.top())
	}

	ed.items = append(ed.items, "evil\x1b]0;x\x07text")

	if got := stripStyles(strings.Join(ed.View(g.r.e, 80, 24), "\n")); strings.ContainsAny(got,
		"\x1b\x07") {
		t.Errorf("a list item reached the screen with a control character: %q", got)
	}

	if line := g.r.e.viewMsg("no_such_key_\x1b[31m", nil); strings.ContainsRune(line, '\x1b') {
		t.Errorf("a missing key shows an escape: %q", line)
	}
}
