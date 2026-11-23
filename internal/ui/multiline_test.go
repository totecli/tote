package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// multiRig opens the canned form with a multiline field (see cannedMulti) over a rig, with a probe
// that records what it submits.
//
// Parameters:
//   - t: running test
//   - sb: sandbox of the rig, nil for a new one
func multiRig(t *testing.T, sb *sandbox) (*rig, *fieldsForm, *formProbe) {
	t.Helper()

	if sb == nil {
		sb = newSandbox(t)
	}

	p := &formProbe{}
	f := cannedMulti()
	f.save = p.save

	g := newRig(t, sb)
	g.send(openOverlayMsg{Overlay: f})

	if g.top() != f {
		t.Fatalf("the form is not on top: %T", g.top())
	}

	return g, f, p
}

// TestMultilineEnter checks that Enter in a multiline field starts a new line and stays in the
// field, where it goes to the next field or runs in any other field.
func TestMultilineEnter(t *testing.T) {
	g, f, p := multiRig(t, nil)

	g.press("tab")
	g.typeText("milk")
	g.press("enter")
	g.typeText("eggs")

	if f.cur != 1 {
		t.Fatalf("Enter left the multiline field for field %d", f.cur)
	}

	if got := f.fields[1].Area.Value(); got != "milk\neggs" {
		t.Errorf("value = %q", got)
	}

	if p.count() != 0 {
		t.Error("Enter in the multiline field ran the form")
	}

	text := g.text()
	for _, want := range []string{"milk", "eggs"} {
		if !strings.Contains(text, want) {
			t.Errorf("the form lacks %q:\n%s", want, text)
		}
	}

	g.press("tab", "enter")
	g.await("the run", func() bool { return p.count() == 1 })

	if got := p.calls[0].Text("msg"); got != "milk\neggs" {
		t.Errorf("submitted %q", got)
	}
}

// TestMultilineSubmit checks that Ctrl+S runs from the multiline field with its line feeds, and
// that the blank lines around the text and the spaces at the end of the lines are dropped.
func TestMultilineSubmit(t *testing.T) {
	g, f, p := multiRig(t, nil)

	g.press("tab", "enter")
	g.typeText("  - milk  ")
	g.press("enter")
	g.typeText("  - eggs")
	g.press("enter", "enter")
	g.press("ctrl+s")
	g.await("the run", func() bool { return g.top() == nil })

	if p.count() != 1 {
		t.Fatalf("ran %d times", p.count())
	}

	if got := p.calls[0].Text("msg"); got != "  - milk\n  - eggs" {
		t.Errorf("submitted %q", got)
	}

	if f.fields[0].text() != "30m" {
		t.Errorf("when = %q", f.fields[0].text())
	}
}

// TestMultilineArrows checks that up and down move along the lines of a multiline field and leave
// it only from its first or its last row, and that Tab and Shift+Tab leave it at once.
func TestMultilineArrows(t *testing.T) {
	g, f, _ := multiRig(t, nil)
	fd := f.fields[1]

	g.press("tab")
	g.typeText("abc")
	g.press("enter")
	g.typeText("d")
	g.press("enter")
	g.typeText("efgh")

	steps := []struct {
		key       string
		cur       int
		line, col int
	}{
		{"up", 1, 1, 1},
		{"up", 1, 0, 1},
		{"right", 1, 0, 2},
		{"down", 1, 1, 1},
		{"down", 1, 2, 1},
		{"down", 2, 2, 1},
		{"up", 1, 2, 1},
		{"up", 1, 1, 1},
		{"up", 1, 0, 1},
		{"up", 0, 0, 1},
		{"down", 1, 0, 1},
		{"tab", 2, 0, 1},
		{"shift+tab", 1, 0, 1},
		{"shift+tab", 0, 0, 1},
	}

	for i, s := range steps {
		g.press(s.key)

		if f.cur != s.cur || fd.Area.Line() != s.line || fd.Area.Column() != s.col {
			t.Fatalf("step %d (%s): field %d at %d:%d, want field %d at %d:%d", i, s.key,
				f.cur, fd.Area.Line(), fd.Area.Column(), s.cur, s.line, s.col)
		}
	}

	if got := fd.Area.Value(); got != "abc\nd\nefgh" {
		t.Errorf("the moves changed the value: %q", got)
	}
}

// TestMultilineWrappedRows checks that up and down follow the rows as they are drawn when a line
// is wrapped.
func TestMultilineWrappedRows(t *testing.T) {
	g, _, _ := multiRig(t, nil)
	e := g.r.e

	fd := newMultiField("m", "M", "", 4, 1000)
	fd.Raw = true
	fd.focus()
	fd.setText("one two three four five\nsix")

	rows := fd.areaRows(e, 10)

	var got []string

	for _, r := range rows {
		var b strings.Builder

		for _, c := range r.cells {
			b.WriteString(c.text)
		}

		got = append(got, b.String())
	}

	if want := []string{"one two ", "three four ", "five", "six"}; strings.Join(got, "|") !=
		strings.Join(want, "|") {
		t.Fatalf("rows = %q, want %q", got, want)
	}

	fd.Wrap = 10
	fd.areaGo(0, 0)

	for i, want := range [][2]int{{0, 8}, {0, 19}, {1, 0}} {
		if !fd.areaStep(e, true) {
			t.Fatalf("step %d: down left the field", i)
		}

		if fd.Area.Line() != want[0] || fd.Area.Column() != want[1] {
			t.Fatalf("step %d: at %d:%d, want %d:%d", i, fd.Area.Line(), fd.Area.Column(),
				want[0], want[1])
		}
	}

	if fd.areaStep(e, true) {
		t.Error("down on the last row did not leave the field")
	}

	fd.areaGo(0, 22)

	if !fd.areaStep(e, false) || fd.Area.Column() != 11 {
		t.Errorf("up from the end of a wrapped line: at %d:%d", fd.Area.Line(), fd.Area.Column())
	}
}

// TestMultilineEsc checks that Esc closes the form of a run at once, even with lines typed.
func TestMultilineEsc(t *testing.T) {
	g, _, p := multiRig(t, nil)

	g.press("tab")
	g.typeText("a")
	g.press("enter")
	g.typeText("b")
	g.press("esc")

	if g.top() != nil || p.count() != 0 {
		t.Errorf("Esc left %T open, ran %d times", g.top(), p.count())
	}
}

// TestMultilineCaps checks that a multiline field never holds more than the registry takes:
// plugin.MaxMultilineLines lines, plugin.MaxMultilineBytes bytes and its own limit.
func TestMultilineCaps(t *testing.T) {
	t.Run("lines", func(t *testing.T) {
		g, f, _ := multiRig(t, nil)
		fd := f.fields[1]

		g.press("tab")
		g.send(tea.PasteMsg{Content: strings.Repeat("x\n", 20)})

		if n := plugin.LineCount(fd.Area.Value()); n != plugin.MaxMultilineLines {
			t.Fatalf("%d lines after a paste of 21", n)
		}

		before := fd.Area.Value()
		g.press("enter")
		g.typeText("y")

		if got := fd.Area.Value(); got != before+"y" {
			t.Errorf("Enter added a line past the limit: %q", got)
		}

		if fd.Err != "" {
			t.Errorf("the value is refused: %s", fd.Err)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		g, f, _ := multiRig(t, nil)
		fd := f.fields[1]

		g.press("tab")
		g.send(tea.PasteMsg{Content: strings.Repeat("\u00e9", 600)})

		if n := len(fd.Area.Value()); n != plugin.MaxMultilineBytes {
			t.Fatalf("%d bytes after a paste of 1200", n)
		}

		g.typeText("z")

		if n := len(fd.Area.Value()); n != plugin.MaxMultilineBytes {
			t.Errorf("%d bytes after typing past the limit", n)
		}

		if fd.Err != "" {
			t.Errorf("the value is refused: %s", fd.Err)
		}
	})

	t.Run("max_len", func(t *testing.T) {
		fd := newMultiField("m", "M", "", 0, 5)
		fd.focus()

		if fd.Rows != plugin.DefaultMultilineRows {
			t.Errorf("rows = %d, want the default %d", fd.Rows, plugin.DefaultMultilineRows)
		}

		for _, k := range []string{"a", "b", "enter", "c", "d", "e", "f"} {
			fd.edit(keyMsg(k))
		}

		if got := fd.Area.Value(); got != "ab\ncd" {
			t.Errorf("value = %q", got)
		}
	})

	t.Run("lines rule", func(t *testing.T) {
		g, f, _ := multiRig(t, nil)
		fd := f.fields[1]

		g.press("tab")
		fd.setText(strings.Repeat("x\n", 17) + "x")
		f.refresh(g.r.e)

		if want := g.r.e.t("plugin_reject_lines"); fd.Err != want || want == "" {
			t.Errorf("error = %q, want %q", fd.Err, want)
		}
	})
}

// TestMultilineSafety checks that typed and pasted text keeps only its line feeds: the other
// control characters, escapes, bidirectional controls and invalid bytes are dropped, a tab
// becomes a space and every line break becomes a line feed.
func TestMultilineSafety(t *testing.T) {
	g, f, _ := multiRig(t, nil)
	fd := f.fields[1]

	g.press("tab")
	g.typeText("ab")
	g.send(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	g.send(tea.KeyPressMsg{Code: 'x', Text: "x", Mod: tea.ModAlt})
	g.send(tea.KeyPressMsg{Code: 'x', Text: "\x1b[31mred"})
	g.send(tea.PasteMsg{Content: "p\r\nq\rr\t\x1b]0;title\x07s\u202Et\x00u\u2028v\u200Bw"})
	g.send(tea.PasteMsg{Content: "\x1b\x07"})
	g.send(tea.PasteMsg{Content: "\xffy\u2066z\u2069"})

	want := "ab[31mredp\nq\nr ]0;titlestuvwyz"
	if got := fd.Area.Value(); got != want {
		t.Errorf("value = %q, want %q", got, want)
	}

	if f.cur != 1 {
		t.Errorf("the keys moved to field %d", f.cur)
	}

	if rule := plugin.FieldRule(multiField, fd.text()); rule != "" {
		t.Errorf("the registry refuses the value: %s", rule)
	}

	for _, l := range g.view() {
		if strings.ContainsAny(stripStyles(l), "\x1b\x07\x00\u202E\u2066") {
			t.Errorf("a control character is drawn: %q", l)
		}
	}

	if got := cleanLines("a\r\n\r\nb\n\tc"); got != "a\n\nb\n c" {
		t.Errorf("cleanLines = %q", got)
	}

	if got := cleanTyped("a\nb"); got != "a b" {
		t.Errorf("cleanTyped keeps line feeds: %q", got)
	}
}

// TestMultilineHelp checks the footer of a multiline field: Enter starts a new line and Ctrl+S
// runs, even in a form whose only field is the multiline one, in English and in French.
func TestMultilineHelp(t *testing.T) {
	for _, tc := range []struct {
		lang, newline, run string
	}{
		{"en", "new line", "ctrl+s run"},
		{"fr", "nouvelle ligne", "ctrl+s lancer"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			sb := newSandbox(t).with(func(o *Options) { o.Printer = newPrinter(tc.lang) })
			f := newFieldsForm("remind: Note", multiSpecs()[1:2], "", "",
				func(fieldValues) string { return "" })
			f.runs = true

			g := newRig(t, sb)
			g.send(openOverlayMsg{Overlay: f})

			foot := g.footerText()
			for _, want := range []string{tc.newline, tc.run, "esc"} {
				if !strings.Contains(foot, want) {
					t.Errorf("the footer lacks %q: %q", want, foot)
				}
			}

			plain := newFieldsForm("remind: Note", multiSpecs()[:1], "", "",
				func(fieldValues) string { return "" })
			plain.runs = true

			g.send(openOverlayMsg{Overlay: plain})

			if foot = g.footerText(); strings.Contains(foot, "ctrl+s") ||
				strings.Contains(foot, tc.newline) {
				t.Errorf("a form of one text field: %q", foot)
			}
		})
	}

	g, f, _ := multiRig(t, nil)

	f.runs = false
	g.press("tab")

	if foot := g.footerText(); !strings.Contains(foot, "new line") ||
		!strings.Contains(foot, "ctrl+s save") {
		t.Errorf("a form that saves: %q", foot)
	}
}

// TestMultilineZones checks that every row of a multiline field takes the keys on a click, and
// that the zones of the fields below it are where they are drawn.
func TestMultilineZones(t *testing.T) {
	g, f, _ := multiRig(t, nil)
	e := g.r.e

	g.press("tab")
	g.typeText("first")
	g.press("enter")
	g.typeText("second")

	_, zs := f.layout(e, f.title, 80, 24)

	var ids []int

	for i, z := range zs {
		ids = append(ids, z.id)

		// The hint of the message takes the row between its last row and the toggle.
		want := zs[0].y + i
		if i == 5 {
			want++
		}

		if z.y != want {
			t.Errorf("zone %d is on row %d, want %d", i, z.y, want)
		}
	}

	if want := []int{0, 1, 1, 1, 1, 2}; !slices.Equal(ids, want) {
		t.Fatalf("zones of the fields %v, want %v", ids, want)
	}

	g.press("shift+tab")
	clickOn(g, "second")

	if f.cur != 1 {
		t.Fatalf("a click on the second row of the message gave the keys to field %d", f.cur)
	}

	clickOn(g, "Urgent")

	if f.cur != 2 {
		t.Fatalf("a click on the toggle below the message gave the keys to field %d", f.cur)
	}

	clickOn(g, "When")

	if f.cur != 0 {
		t.Fatalf("a click on the first field gave the keys to field %d", f.cur)
	}
}

// TestMultilineSmall checks that the row of the cursor of a multiline field stays in view however
// small the box, and that the box never takes more lines than it is given.
func TestMultilineSmall(t *testing.T) {
	g, f, _ := multiRig(t, nil)
	e := g.r.e

	g.press("tab")

	for i := 1; i <= 6; i++ {
		if i > 1 {
			g.press("enter")
		}

		g.typeText("row" + string(rune('0'+i)))
	}

	for _, size := range [][2]int{{80, 24}, {60, 12}, {40, 10}, {40, 8}, {30, 6}, {24, 5}, {24, 4}} {
		for _, key := range []string{"", "up", "up", "up", "up", "up"} {
			if key != "" {
				f.navigate(e, keyMsg(key))
			}

			if f.cur != 1 {
				break
			}

			box, _ := f.layout(e, f.title, size[0], size[1])

			if len(box) > size[1] {
				t.Fatalf("%v: the box takes %d lines", size, len(box))
			}

			want := "row" + string(rune('1'+f.fields[1].Area.Line()))
			if text := stripStyles(strings.Join(box, "\n")); !strings.Contains(text, want) {
				t.Fatalf("%v: the row of the cursor (%s) is out of view:\n%s", size, want, text)
			}
		}

		f.fields[1].areaGo(5, 4)
	}
}

// TestMultilineSpecs checks that a multiline field of a manifest becomes a multiline field of the
// form, with its rows and its limit.
func TestMultilineSpecs(t *testing.T) {
	g, _, _ := multiRig(t, nil)
	e := g.r.e

	fields := []plugin.Field{
		{ID: "body", Type: "text", Multiline: true, Lines: 6, MaxLen: 300},
		{ID: "note", Type: "text", MaxLen: 20},
	}

	specs := commandSpecs(e, fields, plugin.NewCatalog(nil), "command.add", nil)

	if s := specs[0]; s.Type != fieldMulti || s.Rows != 6 || s.Max != 300 {
		t.Errorf("multiline spec = %+v", s)
	}

	if s := specs[1]; s.Type != fieldText || s.Rows != 0 {
		t.Errorf("text spec = %+v", s)
	}

	fd := specs[0].build()
	if fd.Type != fieldMulti || fd.Rows != 6 || fd.Limit != 300 {
		t.Errorf("field = type %d rows %d limit %d", fd.Type, fd.Rows, fd.Limit)
	}

	if got := specs[0].Check(e, strings.Repeat("a\n", 16)+"a"); got != e.t("plugin_reject_lines") {
		t.Errorf("check of 17 lines = %q", got)
	}

	if got := e.ruleText("lines"); got != "too many lines" {
		t.Errorf("ruleText(lines) = %q", got)
	}
}

// memoManifest is a plugin with a command whose message is a multiline field, which runs the fake
// program memo after a question.
const memoManifest = `api = 1
id = "memo"
version = "1.0.0"
tote = ">=1.0.0, <2"
requires_features = ["field.multiline"]
title = "Memo"
description = "Keeps memos."
license = "MIT"
tier = "declarative"
permissions = ["exec:memo"]

[[command]]
id = "add"
label = "Add a memo"
run = ["memo", "add", "--title={title}", "--body={body}"]
output = "pager"
confirm = "Add the memo {title}?"

[[command.field]]
id = "title"
type = "text"
label = "Title"
required = true

[[command.field]]
id = "body"
type = "text"
label = "Body"
multiline = true
lines = 3
required = true
`

// TestMultilineRun plays the whole way of a command with a multiline field: the palette, the form,
// two lines typed, Ctrl+S, the question that shows the line feed as \n, and the program that gets
// the real line feed in its argument.
func TestMultilineRun(t *testing.T) {
	sb := newSandbox(t)
	l := newPlugLab(t, sb)

	bin := filepath.Join(sb.Dir, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}

	log := filepath.Join(sb.Dir, "memo.log")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '[%s]' \"$a\" >> \"" + log + "\"; done\nexit 0\n"

	if err := os.WriteFile(filepath.Join(bin, "memo"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	l.env["PATH"] = bin + ":/usr/bin:/bin"
	l.install("memo", memoManifest)

	sb.Opts.Start = StartHome
	g := newRig(t, sb)

	g.press("ctrl+k")
	g.typeText("add a memo")
	g.press("enter")

	f, ok := g.top().(*fieldsForm)
	if !ok {
		t.Fatalf("the palette led to %T:\n%s", g.top(), g.text())
	}

	if fd := f.fields[1]; fd.Type != fieldMulti || fd.Rows != 3 {
		t.Fatalf("the body is type %d with %d rows", fd.Type, fd.Rows)
	}

	g.typeText("shop")
	g.press("enter")
	g.typeText("- milk")
	g.press("enter")
	g.typeText("- eggs")
	g.press("ctrl+s")

	if _, ok := g.top().(*runAsk); !ok {
		t.Fatalf("Ctrl+S led to %T:\n%s", g.top(), g.text())
	}

	text := g.text()
	if !strings.Contains(text, `'--body=- milk\n- eggs'`) {
		t.Errorf("the question does not show the line feed as \\n:\n%s", text)
	}

	if strings.Contains(text, "milk?") {
		t.Errorf("the question shows a line feed as a question mark:\n%s", text)
	}

	g.press("tab", "enter")
	g.settled()

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(b), "[add][--title=shop][--body=- milk\n- eggs]"; got != want {
		t.Errorf("argv %q, want %q", got, want)
	}
}

// TestMultilineDelete checks that Delete removes exactly one character: the one under the cursor,
// also when it is the last of its line (the engine would join the next line there), and that it
// joins the next line only from the end of a line.
func TestMultilineDelete(t *testing.T) {
	tests := []struct {
		name    string
		line    int
		col     int
		want    string
		wantCol int
	}{
		{"in the middle", 0, 1, "ac\nd", 1},
		{"on the last character of the line", 0, 2, "ab\nd", 2},
		{"at the end of a line, joins the next one", 0, 3, "abcd", 3},
		{"on the only character of the last line", 1, 0, "abc\n", 0},
		{"at the end of the text, nothing", 1, 1, "abc\nd", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, f, _ := multiRig(t, nil)
			fd := f.fields[1]

			g.press("tab")
			g.typeText("abc")
			g.press("enter")
			g.typeText("d")
			fd.areaGo(tt.line, tt.col)
			g.send(tea.KeyPressMsg{Code: tea.KeyDelete})

			if got := fd.Area.Value(); got != tt.want {
				t.Errorf("value = %q, want %q", got, tt.want)
			}

			if got := fd.Area.Column(); got != tt.wantCol {
				t.Errorf("column = %d, want %d", got, tt.wantCol)
			}
		})
	}
}
