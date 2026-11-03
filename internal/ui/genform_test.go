package ui

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/plugin"
)

// formProbe records what a generic form saves.
type formProbe struct {
	mu    sync.Mutex
	calls []fieldValues
	fail  string
}

// save is the save function of a form under test.
//
// Parameters:
//   - v: values to save
func (p *formProbe) save(v fieldValues) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls = append(p.calls, v)

	return p.fail
}

// count returns how many times the form saved.
func (p *formProbe) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.calls)
}

// probeForm opens a generic form with one field of each type over a rig.
//
// Parameters:
//   - t: running test
//
// Returns the rig, the form and the probe.
func probeForm(t *testing.T) (*rig, *fieldsForm, *formProbe) {
	t.Helper()

	p := &formProbe{}
	specs := []fieldSpec{
		{
			ID: "name", Label: "Name", Type: fieldText, Required: true, Max: 8,
			Check: func(e *env, raw string) string {
				if !regexp.MustCompile("^[a-z]+$").MatchString(raw) {
					return e.ruleText("pattern")
				}

				return ""
			},
		},
		{
			ID: "mode", Label: "Mode", Type: fieldChoice, Choices: []string{"fast", "safe"},
			Names: []string{"Fast", ""}, Value: "safe",
		},
		{ID: "loud", Label: "Loud", Type: fieldToggle, Value: "false"},
	}

	f := newFieldsForm("Probe", specs, "saved it", "", p.save)
	g := newRig(t, newSandbox(t))
	g.send(openOverlayMsg{Overlay: f})

	if g.top() != f {
		t.Fatalf("the form is not on top: %T", g.top())
	}

	return g, f, p
}

// TestFieldsFormSubmit checks the keys of the generic form: Enter goes to the next field and
// submits on the last one, only what is valid and changed is saved, and a bad value keeps the
// form open on the field.
//
// Parameters:
//   - t: test to report to
func TestFieldsFormSubmit(t *testing.T) {
	t.Run("enter on the last field submits", func(t *testing.T) {
		g, _, p := probeForm(t)
		g.typeText("abc")
		g.press("enter", "enter")

		if p.count() != 0 {
			t.Fatalf("the form saved before the last field")
		}

		g.press("space", "enter")
		g.await("the save", func() bool { return g.top() == nil })

		if p.count() != 1 {
			t.Fatalf("saved %d times", p.count())
		}

		v := p.calls[0]
		if v.Text("name") != "abc" || v.Text("mode") != "safe" || v.Text("loud") != "true" {
			t.Errorf("values = %+v", v)
		}

		if !v.Changed("name") || !v.Changed("loud") || v.Changed("mode") {
			t.Errorf("changed = %+v", v.changed)
		}

		if !strings.Contains(g.toastText(), "saved it") {
			t.Errorf("toast = %q", g.toastText())
		}
	})

	t.Run("an invalid value blocks the save", func(t *testing.T) {
		g, f, p := probeForm(t)
		g.typeText("ABC")
		g.press("ctrl+s")

		if p.count() != 0 || g.top() != f {
			t.Fatalf("saved %d times, top %T", p.count(), g.top())
		}

		for _, want := range []string{"does not match the pattern", "Some fields are not valid."} {
			if !strings.Contains(g.text(), want) {
				t.Errorf("the form lacks %q:\n%s", want, g.text())
			}
		}

		if f.cur != 0 {
			t.Errorf("the focus is on field %d, want the invalid one (0)", f.cur)
		}
	})

	t.Run("a required value", func(t *testing.T) {
		g, f, p := probeForm(t)
		g.press("tab", "tab", "space", "ctrl+s")

		if p.count() != 0 || f.cur != 0 {
			t.Errorf("saved %d times, focus %d", p.count(), f.cur)
		}

		if !strings.Contains(g.text(), "a value is required") {
			t.Errorf("the form lacks the required rule:\n%s", g.text())
		}
	})

	t.Run("nothing changed", func(t *testing.T) {
		specs := []fieldSpec{{ID: "name", Label: "Name", Type: fieldText, Value: "abc"}}
		p := &formProbe{}
		g := newRig(t, newSandbox(t))
		g.send(openOverlayMsg{Overlay: newFieldsForm("Probe", specs, "saved", "", p.save)})
		g.press("enter")

		if g.top() != nil || p.count() != 0 {
			t.Errorf("top %T, saved %d times", g.top(), p.count())
		}

		if !strings.Contains(g.toastText(), "Nothing changed.") {
			t.Errorf("toast = %q", g.toastText())
		}
	})

	t.Run("a failed save keeps the text", func(t *testing.T) {
		g, f, p := probeForm(t)
		p.fail = "the disk is full"
		g.typeText("abc")
		g.press("ctrl+s")
		g.await("the save", func() bool { return p.count() == 1 && !f.saving })

		if g.top() != f || !strings.Contains(g.text(), "the disk is full") ||
			f.fields[0].text() != "abc" {
			t.Errorf("top %T, name %q:\n%s", g.top(), f.fields[0].text(), g.text())
		}

		p.mu.Lock()
		p.fail = ""
		p.mu.Unlock()

		g.press("ctrl+s")
		g.await("the second save", func() bool { return g.top() == nil })

		if p.count() != 2 {
			t.Errorf("saved %d times", p.count())
		}
	})

	t.Run("the keys are held while it saves", func(t *testing.T) {
		g, f, p := probeForm(t)
		g.typeText("abc")

		f.saving = true
		g.typeText("d")
		g.press("ctrl+s")

		if p.count() != 0 || f.fields[0].text() != "abc" || g.top() != f {
			t.Errorf("saved %d times, name %q, top %T", p.count(), f.fields[0].text(), g.top())
		}
	})
}

// TestFieldsFormCancel checks Esc: it closes a form that was not touched, and asks first for one
// that was, without saving in either case.
//
// Parameters:
//   - t: test to report to
func TestFieldsFormCancel(t *testing.T) {
	g, _, _ := probeForm(t)
	g.press("esc")

	if g.top() != nil {
		t.Fatalf("Esc left %T open on a clean form", g.top())
	}

	g, f, p := probeForm(t)
	g.typeText("ab")
	g.press("tab", "tab", "space", "esc")

	if _, ok := g.top().(*ask); !ok {
		t.Fatalf("Esc on a dirty form opened %T, want the question", g.top())
	}

	g.press("n")

	if g.top() != f {
		t.Fatalf("No left %T on top, want the form", g.top())
	}

	g.press("esc", "y")

	if g.top() != nil || p.count() != 0 {
		t.Errorf("top %T, saved %d times", g.top(), p.count())
	}
}

// TestFieldsFormSmall checks that the form fits the smallest terminal and still works there.
//
// Parameters:
//   - t: test to report to
func TestFieldsFormSmall(t *testing.T) {
	g, _, p := probeForm(t)
	g.resize(24, 7)
	checkScreen(t, "fieldsform", g.r.render(), 24, 7, false)

	g.typeText("abc")
	checkScreen(t, "fieldsform", g.r.render(), 24, 7, false)

	g.press("enter", "enter", "space")
	checkScreen(t, "fieldsform", g.r.render(), 24, 7, false)

	g.press("enter")
	g.await("the save", func() bool { return g.top() == nil })

	if p.count() != 1 {
		t.Errorf("saved %d times", p.count())
	}
}

// TestRuleText checks the sentence of every rule that FieldRule names, in English and French.
//
// Parameters:
//   - t: test to report to
func TestRuleText(t *testing.T) {
	tests := []struct {
		rule string
		en   string
		fr   string
	}{
		{"", "", ""},
		{"required", "a value is required", "une valeur est requise"},
		{"max_len", "too long", "trop long"},
		{"length", "too long", "trop long"},
		{"pattern", "does not match the pattern", "ne correspond pas au motif"},
		{"choice", "not one of the choices", "ce n\u2019est pas l\u2019un des choix"},
		{"bool", "wrong type", "mauvais type"},
		{"utf8", "contains characters that are not allowed", "contient des caract"},
		{"control", "contains characters that are not allowed", "contient des caract"},
	}

	for _, tt := range tests {
		t.Run(tt.rule, func(t *testing.T) {
			sb := newSandbox(t)
			en := newRig(t, sb).r.e

			sb.Opts.Printer = i18n.New("fr")
			fr := newRig(t, sb).r.e

			if got := en.ruleText(tt.rule); got != tt.en {
				t.Errorf("en %q = %q, want %q", tt.rule, got, tt.en)
			}

			if got := fr.ruleText(tt.rule); !strings.HasPrefix(got, tt.fr) {
				t.Errorf("fr %q = %q, want a text that starts with %q", tt.rule, got, tt.fr)
			}
		})
	}
}

// TestCommandSpecs checks the form of the arguments of a command: the labels from the catalog,
// the rules of the manifest, the types, and the starting values that the registry computes from
// the templated defaults.
//
// Parameters:
//   - t: test to report to
func TestCommandSpecs(t *testing.T) {
	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.install("args", plugManifest("args", "Takes arguments.", `
[[command.field]]
id = "dir"
type = "text"
label = "Folder"
hint = "Where to look."
default = "alpha"
required = true
max_len = 6
pattern = "^[a-z]+$"

[[command.field]]
id = "next"
type = "text"
label = "Next"
default = "{dir}-beta"

[[command.field]]
id = "view"
type = "choice"
label = "View"
choices = ["", "wide"]

[[command.field]]
id = "dry"
type = "bool"
label = "Dry run"
flag = "--dry"
`))

	p, ok := l.reg.Recheck("args")
	if !ok {
		t.Fatal("args is not installed")
	}

	cmd := p.Manifest.Commands[0]
	fields := cmd.Fields[1:]

	call := plugin.Call{Plugin: "args", Table: plugin.TableCommand, Entry: "go"}

	defaults, err := l.reg.Defaults(call)
	if err != nil {
		t.Fatal(err)
	}

	if defaults["dir"] != "alpha" || defaults["next"] != "alpha-beta" {
		t.Errorf("defaults = %v, want the template expanded over the field before it", defaults)
	}

	cat := plugin.NewCatalog(map[string]map[string]string{
		"fr": {"command.go.field.dir.label": "Dossier", "command.go.field.view.choice._": "Normal"},
		"en": {"command.go.field.view.choice.wide": "Wide view"},
	})

	g := newRig(t, sb)
	specs := commandSpecs(g.r.e, fields, cat, "command.go", defaults)

	if len(specs) != 4 {
		t.Fatalf("%d specs, want 4", len(specs))
	}

	if s := specs[0]; s.Label != "Folder" || s.Hint != "Where to look." || s.Value != "alpha" ||
		!s.Required || s.Max != 6 || s.Type != fieldText {
		t.Errorf("text spec = %+v", s)
	}

	if specs[1].Value != "alpha-beta" {
		t.Errorf("templated value = %q", specs[1].Value)
	}

	if s := specs[2]; s.Type != fieldChoice || !slices.Equal(s.Choices, []string{"", "wide"}) ||
		!slices.Equal(s.Names, []string{"", "Wide view"}) {
		t.Errorf("choice spec = %+v", s)
	}

	if specs[3].Type != fieldToggle {
		t.Errorf("bool spec = %+v", specs[3])
	}

	if got := specs[0].Check(g.r.e, "ABC"); got != "does not match the pattern" {
		t.Errorf("check ABC = %q", got)
	}

	if got := specs[0].Check(g.r.e, "abc"); got != "" {
		t.Errorf("check abc = %q", got)
	}

	if got := specs[0].Check(g.r.e, "abcdefgh"); got != "too long" {
		t.Errorf("check abcdefgh = %q", got)
	}

	sb.Opts.Printer = i18n.New("fr")
	fr := newRig(t, sb)

	if s := commandSpecs(fr.r.e, fields, cat, "command.go", defaults); s[0].Label != "Dossier" ||
		s[2].Names[0] != "Normal" || s[2].Names[1] != "Wide view" {
		t.Errorf("french specs = %+v", s)
	}

	f := newFieldsForm("Args", specs, "done", "", (&formProbe{}).save)
	if f.fields[0].text() != "alpha" || f.fields[1].text() != "alpha-beta" {
		t.Errorf("the form starts with %q", f.texts())
	}
}
