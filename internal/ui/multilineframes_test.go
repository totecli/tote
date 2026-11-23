package ui

import (
	"strconv"

	"github.com/totecli/tote/internal/plugin"
)

// multiField is the field of a manifest that the canned form with a multiline field checks its
// message with.
var multiField = plugin.Field{
	ID: "msg", Type: "text", Required: true, Multiline: true, Lines: 4,
	MaxLen: plugin.MaxMultilineBytes,
}

// multiSpecs describes the fields of the canned form with a multiline field: a text, the
// multiline message of four rows and a toggle.
func multiSpecs() []fieldSpec {
	return []fieldSpec{
		{ID: "when", Label: "When", Type: fieldText, Value: "30m", Required: true},
		{
			ID: "msg", Label: "Message", Type: fieldMulti, Rows: multiField.Lines, Required: true,
			Hint: "One item per line.",
			Check: func(e *env, raw string) string {
				return e.ruleText(plugin.FieldRule(multiField, raw))
			},
		},
		{ID: "urgent", Label: "Urgent", Type: fieldToggle, Value: "false"},
	}
}

// cannedMulti returns the form that asks the arguments of a run with a multiline field, as a
// plugin form is, with the keys on its first field and nothing to run behind it.
func cannedMulti() *fieldsForm {
	f := newFieldsForm("remind: Add a reminder", multiSpecs(), "", "",
		func(fieldValues) string { return "" })
	f.runs, f.wide = true, true

	return f
}

// multilineFrames lists the frames of a form with a multiline field: empty with the keys, two
// lines typed, more lines than its rows, the error of a required message and the field without
// the keys after its text went past its rows, in English, in French and in ASCII.
func multilineFrames() []frameCase {
	lines := []string{"multi:form", "tab"}

	for i := 1; i <= 7; i++ {
		if i > 1 {
			lines = append(lines, "enter")
		}

		lines = append(lines, "type:Line "+strconv.Itoa(i))
	}

	lines = append(lines, "enter", "type:and a last line long enough to be wrapped at the words")

	kinds := []struct {
		name string
		keys []string
	}{
		{"multilineempty", []string{"multi:form", "tab"}},
		{"multilinetyped", []string{
			"multi:form", "tab", "type:Buy milk", "enter", "type:Call the bank about the card",
		}},
		{"multilineoverflow", lines},
		{"multilineerror", []string{"multi:form", "tab", "ctrl+s"}},
		{"multilineleft", append(append([]string(nil), lines...), "up", "up", "tab")},
	}

	var out []frameCase

	for _, k := range kinds {
		for _, look := range pluginLooks {
			out = append(out, frameCase{
				name: k.name + look.suffix, data: dataPlugRun, start: StartHome, keys: k.keys,
				sizes: goldenSizes, check: checkedBesides(goldenSizes), setup: look.setup,
			})
		}
	}

	return out
}
