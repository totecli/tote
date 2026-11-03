package ui

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/plugin"
)

// settingsRig opens the settings form of the plugin "demo" over the fixtures.
//
// Parameters:
//   - t: running test
//   - prepare: change to make to the lab before the form opens, may be nil
//
// Returns the rig, the lab and the form.
func settingsRig(t *testing.T, prepare func(*plugLab)) (*rig, *plugLab, *fieldsForm) {
	t.Helper()

	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	l.fixtures()

	if prepare != nil {
		prepare(l)
	}

	sb.Opts.Start = StartPlugins
	g := newRig(t, sb)
	g.pick("demo")
	g.press("s")

	f, ok := g.top().(*fieldsForm)
	if !ok {
		t.Fatalf("s opened %T:\n%s", g.top(), g.text())
	}

	return g, l, f
}

// settingsFile returns the path of the settings file of the plugin "demo".
//
// Parameters:
//   - l: the lab
func (l *plugLab) settingsFile() string {
	file, _ := l.sb.Opts.Paths.PluginSettingsFile("demo")

	return file
}

// stored returns what the plugin "demo" has as settings, by id.
//
// Parameters:
//   - t: running test
func (l *plugLab) stored(t *testing.T) map[string]plugin.SettingValue {
	t.Helper()

	p, ok := l.reg.Recheck("demo")
	if !ok {
		t.Fatal("demo is not installed")
	}

	out := map[string]plugin.SettingValue{}
	for _, v := range l.reg.Settings(p).Values {
		out[v.ID] = v
	}

	return out
}

// clearField empties the text field that has the keys.
//
// Parameters:
//   - g: the rig
//   - n: number of characters it holds
func clearField(g *rig, n int) {
	g.t.Helper()

	for range n {
		g.press("backspace")
	}
}

// TestSettingsOpen checks the form that the key opens: one field per setting in the order of the
// manifest, the values the plugin runs with, and a plugin without settings.
//
// Parameters:
//   - t: test to report to
func TestSettingsOpen(t *testing.T) {
	g, _, f := settingsRig(t, nil)

	if want := []string{"owner", "verbose", "limit", "mode", "tags"}; len(f.fields) != len(want) {
		t.Fatalf("%d fields, want %v", len(f.fields), want)
	}

	got := map[string]string{}
	for _, fd := range f.fields {
		got[fd.ID] = fd.text()
	}

	if got["owner"] != "someone" || got["verbose"] != "false" || got["limit"] != "10" ||
		got["tags"] == "" {
		t.Errorf("values = %v", got)
	}

	for _, w := range []string{"Settings of demo", "Owner", "Verbose", "Limit", "Mode", "Tags"} {
		if !strings.Contains(g.text(), w) {
			t.Errorf("the form lacks %q:\n%s", w, g.text())
		}
	}

	g.press("esc")
	g.pick("notes")
	g.press("s")

	if g.top() != nil || !strings.Contains(g.toastText(), "plugin notes has no settings") {
		t.Errorf("top %T, toast %q", g.top(), g.toastText())
	}
}

// TestSettingsLabelsWide checks that the labels of a plugin are not cut to the narrow column of
// the other forms when the box has room: a label of seventeen cells shows whole at 80 columns, and
// a label wider than half of the box is still cut.
//
// Parameters:
//   - t: test to report to
func TestSettingsLabelsWide(t *testing.T) {
	g, _, f := settingsRig(t, nil)
	f.fields[0].Label = "Folders to search"
	f.fields[1].Label = strings.Repeat("w", 60)

	g.resize(80, 24)

	text := g.text()
	if !strings.Contains(text, "Folders to search") {
		t.Errorf("a label of 17 cells is cut:\n%s", text)
	}

	if strings.Contains(text, strings.Repeat("w", 60)) ||
		!strings.Contains(text, string(rune(0x2026))) {
		t.Errorf("a label wider than half of the box is not cut:\n%s", text)
	}
}

// TestSettingsSave checks the write: only the settings the user changed reach the file, a value
// that is the default is not stored, and the form closes with a message.
//
// Parameters:
//   - t: test to report to
func TestSettingsSave(t *testing.T) {
	g, l, _ := settingsRig(t, nil)

	clearField(g, len("someone"))
	g.typeText("mine")
	g.press("tab", "space", "tab")
	clearField(g, 2)
	g.typeText("25")
	g.press("ctrl+s")
	g.await("the save", func() bool { return g.top() == nil })

	if !strings.Contains(g.toastText(), "settings of demo saved") {
		t.Errorf("toast = %q", g.toastText())
	}

	got := l.stored(t)
	if got["owner"].Value != "mine" || got["verbose"].Value != true ||
		got["limit"].Value != int64(25) {
		t.Errorf("stored = %+v", got)
	}

	data, err := os.ReadFile(l.settingsFile())
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(data), "mode") || strings.Contains(string(data), "tags") {
		t.Errorf("the file holds settings that were not changed:\n%s", data)
	}

	// Putting the default back removes the key instead of writing the default.
	g.pick("demo")
	g.press("s")
	g.press("tab", "tab")
	clearField(g, 2)
	g.typeText("10")
	g.press("ctrl+s")
	g.await("the second save", func() bool { return g.top() == nil })

	data, _ = os.ReadFile(l.settingsFile())
	if strings.Contains(string(data), "limit") {
		t.Errorf("the default was written:\n%s", data)
	}
}

// TestSettingsInvalid checks that a value the manifest refuses blocks the save and names the rule,
// and that nothing is written.
//
// Parameters:
//   - t: test to report to
func TestSettingsInvalid(t *testing.T) {
	tests := []struct {
		name string
		skip int
		del  int
		text string
		want string
	}{
		{"pattern", 0, len("someone"), "Bad Name", "does not match the pattern"},
		{"not a number", 2, 2, "ten", "wrong type"},
		{"range", 2, 2, "99", "must be between 1 and 50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, l, f := settingsRig(t, nil)

			for range tt.skip {
				g.press("tab")
			}

			clearField(g, tt.del)
			g.typeText(tt.text)
			g.press("ctrl+s")

			if g.top() != f || !strings.Contains(g.text(), tt.want) {
				t.Fatalf("top %T, want %q:\n%s", g.top(), tt.want, g.text())
			}

			if _, err := os.Stat(l.settingsFile()); err == nil {
				t.Errorf("a file was written for an invalid value")
			}
		})
	}
}

// TestSettingsMaxLen checks that a text field stops at the max_len of its setting.
//
// Parameters:
//   - t: test to report to
func TestSettingsMaxLen(t *testing.T) {
	g, _, f := settingsRig(t, nil)

	clearField(g, len("someone"))
	g.typeText("abcdefghijklm")

	if got := f.fields[0].text(); got != "abcdefghijkl" {
		t.Errorf("the field holds %q, want 12 characters", got)
	}
}

// TestSettingsUnchanged checks that saving a form nobody touched writes nothing.
//
// Parameters:
//   - t: test to report to
func TestSettingsUnchanged(t *testing.T) {
	g, l, _ := settingsRig(t, nil)
	g.press("ctrl+s")

	if g.top() != nil || !strings.Contains(g.toastText(), "Nothing changed.") {
		t.Errorf("top %T, toast %q", g.top(), g.toastText())
	}

	if _, err := os.Stat(l.settingsFile()); err == nil {
		t.Errorf("a file was written")
	}
}

// TestSettingsEnvironment checks a setting that the environment sets: its row is locked with the
// name of the variable, the typed keys do nothing there, and the other rows still save.
//
// Parameters:
//   - t: test to report to
func TestSettingsEnvironment(t *testing.T) {
	g, l, f := settingsRig(t, func(l *plugLab) {
		l.env[plugin.SettingEnvName("demo", "limit")] = "7"
	})

	g.press("tab", "tab")
	g.typeText("9")

	if f.fields[2].text() != "7" {
		t.Errorf("the locked field holds %q, want 7", f.fields[2].text())
	}

	if !strings.Contains(g.text(), "TOTE_PLUGIN_DEMO_LIMIT") {
		t.Errorf("the form does not name the variable:\n%s", g.text())
	}

	g.press("shift+tab", "shift+tab")
	clearField(g, len("someone"))
	g.typeText("other")
	g.press("ctrl+s")
	g.await("the save", func() bool { return g.top() == nil })

	got := l.stored(t)
	if got["owner"].Value != "other" || got["limit"].Value != int64(7) {
		t.Errorf("stored = %+v", got)
	}

	if data, _ := os.ReadFile(l.settingsFile()); strings.Contains(string(data), "limit") {
		t.Errorf("the locked setting was written:\n%s", data)
	}
}

// TestSettingsIgnored checks the conditions the form reports: a value of the file that is ignored
// is told on its row, and a file that is ignored as a whole is told at the bottom.
//
// Parameters:
//   - t: test to report to
func TestSettingsIgnored(t *testing.T) {
	t.Run("a rejected value", func(t *testing.T) {
		g, _, _ := settingsRig(t, func(l *plugLab) {
			writeFile(t, l.settingsFile(), "schema = 1\nlimit = 99\n")
		})

		g.press("tab", "tab")

		if got := g.text(); !strings.Contains(got,
			"setting limit from the file was ignored: out of range") ||
			!strings.Contains(got, "(value: 99)") {
			t.Errorf("the row does not say that the value is ignored:\n%s", g.text())
		}
	})

	t.Run("an unusable file", func(t *testing.T) {
		g, _, _ := settingsRig(t, func(l *plugLab) {
			writeFile(t, l.settingsFile(), "schema = 1\nlimit = [[[\n")
		})

		if !strings.Contains(g.text(), "the settings file was ignored") {
			t.Errorf("the form does not say that the file is ignored:\n%s", g.text())
		}
	})

	t.Run("an unknown key", func(t *testing.T) {
		g, _, _ := settingsRig(t, func(l *plugLab) {
			writeFile(t, l.settingsFile(), "schema = 1\nnope = 1\n")
		})

		if !strings.Contains(g.text(), "nope") {
			t.Errorf("the form does not name the unknown key:\n%s", g.text())
		}
	})
}

// TestSettingsStale checks a plugin that changed while the form was open: the save is refused
// with the reason and the typed text stays, so that nothing is lost.
//
// Parameters:
//   - t: test to report to
func TestSettingsStale(t *testing.T) {
	g, l, f := settingsRig(t, nil)

	clearField(g, len("someone"))
	g.typeText("mine")

	l.edit("demo", func(r *plugin.LockRecord) { r.TreeSHA256 = strings.Repeat("1", 64) })
	g.press("ctrl+s")
	g.await("the refusal", func() bool { return !f.saving })

	if g.top() != f || f.fields[0].text() != "mine" {
		t.Fatalf("top %T, owner %q", g.top(), f.fields[0].text())
	}

	if !strings.Contains(g.text(), "changed while this was running") &&
		!strings.Contains(g.text(), "demo") {
		t.Errorf("the form does not say why:\n%s", g.text())
	}

	if _, err := os.Stat(l.settingsFile()); err == nil {
		t.Errorf("a file was written for a plugin that changed")
	}
}

// openTags opens the list editor of the setting "tags".
//
// Parameters:
//   - g: the rig
func openTags(g *rig) *listEditor {
	g.t.Helper()
	g.press("tab", "tab", "tab", "tab", "enter")

	ed, ok := g.top().(*listEditor)
	if !ok {
		g.t.Fatalf("Enter on the list opened %T:\n%s", g.top(), g.text())
	}

	return ed
}

// TestListEditor checks the keys of the list editor: n adds after the cursor, e changes, d
// removes, K and J move, a bad item is refused in its question, and ctrl+s hands the items back to
// the form.
//
// Parameters:
//   - t: test to report to
func TestListEditor(t *testing.T) {
	g, l, f := settingsRig(t, nil)
	ed := openTags(g)

	if !slices.Equal(ed.items, []string{"x"}) {
		t.Fatalf("items = %v, want the default", ed.items)
	}

	g.press("n")
	g.typeText("Bad")
	g.press("enter")

	if _, ok := g.top().(*promptOverlay); !ok ||
		!strings.Contains(g.text(), "does not match the pattern") {
		t.Fatalf("a bad item was taken: %T\n%s", g.top(), g.text())
	}

	g.press("ctrl+u")
	g.typeText("bb")
	g.press("enter")

	if g.top() != ed || !slices.Equal(ed.items, []string{"x", "bb"}) {
		t.Fatalf("items = %v, top %T", ed.items, g.top())
	}

	g.press("n")
	g.typeText("cc")
	g.press("enter")

	g.press("K")

	if !slices.Equal(ed.items, []string{"x", "cc", "bb"}) {
		t.Errorf("after K: %v", ed.items)
	}

	g.press("J")
	g.press("e")
	g.press("ctrl+u")
	g.typeText("zz")
	g.press("enter")

	if !slices.Equal(ed.items, []string{"x", "bb", "zz"}) {
		t.Errorf("after the change: %v", ed.items)
	}

	g.press("n")
	g.typeText("dd")
	g.press("enter")
	g.press("ctrl+s")

	if g.top() != ed || !strings.Contains(g.text(), "at most 3 items") {
		t.Fatalf("four items were accepted, max_items is 3:\n%s", g.text())
	}

	g.press("d")
	g.press("ctrl+s")

	if g.top() != f {
		t.Fatalf("ctrl+s left %T on top", g.top())
	}

	if f.fields[4].Items == nil || len(f.fields[4].Items) != 3 {
		t.Fatalf("the form holds %v", f.fields[4].Items)
	}

	g.press("ctrl+s")
	g.await("the save", func() bool { return g.top() == nil })

	tags, _ := l.stored(t)["tags"].Value.([]string)
	if len(tags) != 3 {
		t.Errorf("stored tags = %v", tags)
	}
}

// TestListEditorCancel checks that Esc in the list editor leaves the form as it was.
//
// Parameters:
//   - t: test to report to
func TestListEditorCancel(t *testing.T) {
	g, _, f := settingsRig(t, nil)
	ed := openTags(g)

	g.press("n")
	g.typeText("bb")
	g.press("enter")
	g.press("esc")

	if _, ok := g.top().(*ask); !ok {
		t.Fatalf("Esc on a changed list opened %T, want the question", g.top())
	}

	g.press("y")

	if g.top() != f || !slices.Equal(f.fields[4].Items, []string{"x"}) {
		t.Errorf("top %T, form items %v, editor %v", g.top(), f.fields[4].Items, ed.items)
	}
}

// TestListEditorMouse checks the mouse in the editor: a click selects an item and a double click
// changes it.
//
// Parameters:
//   - t: test to report to
func TestListEditorMouse(t *testing.T) {
	g, _, _ := settingsRig(t, nil)
	ed := openTags(g)

	g.press("n")
	g.typeText("bb")
	g.press("enter")
	g.press("n")
	g.typeText("cc")
	g.press("enter")

	y, x := lineOf(g, "bb")
	g.click(x, y)

	if ed.cur != 1 {
		t.Fatalf("the click selected item %d, want 1", ed.cur)
	}

	g.click(x, y)

	if _, ok := g.top().(*promptOverlay); !ok {
		t.Errorf("the double click opened %T, want the question", g.top())
	}
}

// TestSettingsFormMouse checks that a click on a row of the form moves the focus there.
//
// Parameters:
//   - t: test to report to
func TestSettingsFormMouse(t *testing.T) {
	g, _, f := settingsRig(t, nil)

	y, x := lineOf(g, "Limit")
	g.click(x, y)

	if f.cur != 2 {
		t.Errorf("the focus is on field %d, want 2", f.cur)
	}
}

// TestSettingsSmall checks that the settings form and its editor fit the smallest terminal and
// stay usable there.
//
// Parameters:
//   - t: test to report to
func TestSettingsSmall(t *testing.T) {
	g, _, f := settingsRig(t, nil)
	g.resize(24, 7)
	checkScreen(t, "settings", g.r.render(), 24, 7, false)

	g.press("tab", "tab", "tab", "tab")
	checkScreen(t, "settings tags", g.r.render(), 24, 7, false)

	g.press("enter")
	checkScreen(t, "editor", g.r.render(), 24, 7, false)

	g.press("n")
	checkScreen(t, "item question", g.r.render(), 24, 7, false)

	g.typeText("bb")
	g.press("enter", "ctrl+s")

	if g.top() != f || len(f.fields[4].Items) != 2 {
		t.Errorf("top %T, items %v", g.top(), f.fields[4].Items)
	}

	g.press("ctrl+s")
	g.await("the save", func() bool { return g.top() == nil })
}
