package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// TestHomeScrollPersists checks that the window of the home menu keeps its place: going up from
// the last entry moves the selection inside the window, which does not move until the selection
// leaves it, whether the selection moved by keys, by the wheel or by a jump key.
//
// Parameters:
//   - t: test to report to
func TestHomeScrollPersists(t *testing.T) {
	s := frameSandbox(t, dataFull)

	t.Run("keys", func(t *testing.T) {
		g := minRig(t, s, StartHome, minWidth, minHeight, "en", nil)

		for range 4 {
			g.press("down")
		}

		for step, want := range []string{"Functions", "Aliases"} {
			g.press("up")

			text := g.text()
			if !strings.Contains(text, "2 more") || !strings.Contains(text, "Settings") {
				t.Fatalf("up %d: the window moved before the selection left it:\n%s", step+1, text)
			}

			if rows := markedLines(g); len(rows) != 1 || !strings.Contains(rows[0], want) {
				t.Fatalf("up %d: marked rows %q, want %q:\n%s", step+1, rows, want, text)
			}
		}
	})

	t.Run("wheel", func(t *testing.T) {
		g, _ := mouseRig(t, minWidth, minHeight, StartHome, nil)

		for range 4 {
			g.send(tea.MouseWheelMsg{X: 3, Y: 3, Button: tea.MouseWheelDown})
		}

		for range 2 {
			g.send(tea.MouseWheelMsg{X: 3, Y: 3, Button: tea.MouseWheelUp})
		}

		text := g.text()
		if !strings.Contains(text, "2 more") || !strings.Contains(text, "Settings") {
			t.Fatalf("the wheel moved the window:\n%s", text)
		}
	})

	t.Run("jump", func(t *testing.T) {
		g := minRig(t, s, StartHome, minWidth, minHeight, "en", nil)
		g.press(",", "esc", "up", "up")

		text := g.text()
		if !strings.Contains(text, "2 more") || !strings.Contains(text, "Settings") {
			t.Fatalf("after the comma key to Settings and two ups the window moved:\n%s", text)
		}
	})
}

// TestHomeMenuRows checks that the number of rows the menu is paged by is the number it draws.
//
// Parameters:
//   - t: test to report to
func TestHomeMenuRows(t *testing.T) {
	g := minRig(t, frameSandbox(t, dataFull), StartHome, minWidth, minHeight, "en", nil)
	home := g.r.scr.(*homeScreen)
	shown := 0

	for _, l := range strings.Split(g.text(), "\n")[1:5] {
		if strings.TrimSpace(l) != "" {
			shown++
		}
	}

	if got := home.menuRows(g.r.e); got != shown {
		t.Fatalf("menuRows = %d, the menu shows %d rows:\n%s", got, shown, g.text())
	}
}

// TestHomeHeadBoundary checks the heights at which the compact header of the home screen has a
// blank line under it and the welcome box is kept, with ui.compact set to never.
//
// Parameters:
//   - t: test to report to
func TestHomeHeadBoundary(t *testing.T) {
	s := frameSandbox(t, dataFull)

	for _, c := range []struct {
		h     int
		blank bool
	}{{8, false}, {9, true}} {
		g := minRig(t, s, StartHome, minWidth, c.h, "en", nil)

		if got := strings.TrimSpace(strings.Split(g.text(), "\n")[1]) == ""; got != c.blank {
			t.Errorf("%dx%d: blank line under the header is %v, want %v:\n%s", minWidth, c.h, got,
				c.blank, g.text())
		}
	}

	for _, c := range []struct {
		h   int
		box bool
	}{{13, false}, {14, true}} {
		g := minRig(t, s, StartHome, 80, c.h, "en", func(o *Options) {
			o.Settings.UI.Compact = "never"
		})

		if got := strings.Contains(g.text(), "Welcome"); got != c.box {
			t.Errorf("80x%d with compact never: welcome box is %v, want %v", c.h, got, c.box)
		}
	}
}

// TestCompactHeaderCounts checks that the counts of the one-line header use the singular for one
// record, and in French for none too.
//
// Parameters:
//   - t: test to report to
func TestCompactHeaderCounts(t *testing.T) {
	for _, c := range []struct {
		name    string
		repos   string
		aliases string
		funcs   string
		lang    string
		want    string
	}{
		{
			"one repo and alias", "[[repo]]\nname = \"proj\"\npath = \"/tmp\"\n",
			"[[alias]]\nname = \"ll\"\ncmd = \"ls\"\n", "", "en",
			"6 conn \u00b7 1 repo \u00b7 1 alias \u00b7 0 funcs",
		},
		{
			"one function", "", "",
			"[[func]]\nname = \"hi\"\nkind = \"exec\"\nscript = \"/bin/hi\"\n",
			"en", "0 repos \u00b7 0 aliases \u00b7 1 func",
		},
		{
			"none in French", "", "", "", "fr",
			"6 cx \u00b7 0 d\u00e9p \u00b7 0 alias \u00b7 0 fonct",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			sb := newSandbox(t)
			cfg := sb.Opts.Paths.Config

			writeFile(t, filepath.Join(cfg, "repos.toml"), "schema = 1\n"+c.repos)
			writeFile(t, filepath.Join(cfg, "aliases.toml"), "schema = 1\n"+c.aliases)
			writeFile(t, filepath.Join(cfg, "funcs.toml"), "schema = 1\n"+c.funcs)

			g := minRig(t, sb, StartHome, 100, 12, c.lang, func(o *Options) {
				o.Settings.UI.Compact = "always"
			})

			head := strings.TrimSpace(strings.Split(g.text(), "\n")[0])
			if !strings.HasSuffix(head, c.want) {
				t.Errorf("header %q does not end with %q", head, c.want)
			}
		})
	}
}

// TestBoxTitleCut checks when the title of a box is cut with an ellipsis, and when it is left
// out: fewer than minTitle cells of it would show.
//
// Parameters:
//   - t: test to report to
func TestBoxTitleCut(t *testing.T) {
	g := minRig(t, frameSandbox(t, dataFull), StartHome, 80, 24, "en", nil)
	e := g.r.e

	for _, c := range []struct {
		w    int
		want string
	}{{9, ""}, {10, "Ter\u2026"}, {11, "Term\u2026"}, {16, "Terminal"}} {
		top := stripStyles(boxWith(e, e.Theme.Accent, "Terminal", []string{"x"}, c.w)[0])

		if c.want == "" && strings.ContainsAny(top, "Te") {
			t.Errorf("w=%d: the title shows: %q", c.w, top)
		}

		if c.want != "" && !strings.Contains(top, " "+c.want+" ") {
			t.Errorf("w=%d: top %q lacks %q", c.w, top, c.want)
		}
	}
}

// TestConfirmLongName checks the question about a record with a long name: the name is cut so
// that the short question keeps its quotes and its question mark and fills the line.
//
// Parameters:
//   - t: test to report to
func TestConfirmLongName(t *testing.T) {
	e := minRig(t, frameSandbox(t, dataFull), StartHome, 80, 24, "en", nil).r.e
	name := strings.Repeat("abcdefgh", 8)

	for _, w := range []int{minWidth, 40, 56} {
		box, _ := (&confirm{kind: 0, key: name}).draw(e, w, minHeight)
		inner := min(w, confirmWidth) - 4
		row := strings.TrimSpace(strings.Trim(stripStyles(box[1]), "\u2502"))

		if !strings.HasSuffix(row, "?") || !strings.Contains(row, "\u2026") ||
			e.Metric.width(row) != inner {
			t.Errorf("w=%d: first row %q is %d cells wide, want %d with an ellipsis and a ?", w,
				row, e.Metric.width(row), inner)
		}
	}
}

// TestQuestionBoxFallbacks checks what a question box gives up, and in which order, when the
// height does not leave room for all its lines.
//
// Parameters:
//   - t: test to report to
func TestQuestionBoxFallbacks(t *testing.T) {
	e := minRig(t, frameSandbox(t, dataFull), StartHome, 80, 24, "en", nil).r.e
	two := []string{"detail one", "detail two"}

	tests := []struct {
		name    string
		w, h    int
		full    string
		short   string
		details []string
		want    []string
		absent  []string
		lines   int
	}{
		{"all lines", 24, 20, "Delete it?", "", two, []string{
			"Delete it?", "detail one",
			"detail two", "[ No ]",
		}, nil, 7},
		{"one detail and the blank", 24, 6, "Delete it?", "", two, []string{
			"Delete it?",
			"detail one", "[ No ]",
		}, []string{"detail two"}, 6},
		{
			"one detail", 24, 5, "Delete it?", "", two,
			[]string{"Delete it?", "detail one"},
			[]string{"detail two"},
			5,
		},
		{
			"no detail", 24, 4, "Delete it?", "", two,
			[]string{"Delete it?", "[ No ]"},
			[]string{"detail one"},
			4,
		},
		{
			"short question before the detail", 24, 5, "aaaa bbbb cccc dddd eeee ffff", "Short?",
			two[:1],
			[]string{"Short?", "detail one", "[ No ]"},
			[]string{"aaaa"},
			5,
		},
		{
			"one line of two", 24, 4, "aaaa bbbb cccc dddd eeee ffff", "", two[:1],
			[]string{"aaaa bbbb cccc dddd", "[ No ]"},
			[]string{"detail one", "eeee"},
			4,
		},
		{
			"the short question on one line", 17, 4, "aaaa bbbb cccc dddd", "ssss tttt uuuu", nil,
			[]string{"ssss tttt", "[No] [Yes]"},
			[]string{"aaaa", "uuuu"},
			4,
		},
		{
			"a long question gives the short one", 13, 20, "aaaa bbbb cccc dddd eeee ffff",
			"Short?", nil,
			[]string{"Short?"},
			[]string{"aaaa"},
			5,
		},
	}

	for _, tt := range tests {
		box, zs := questionBox(e, tt.w, tt.h, "Delete", tt.full, tt.short, tt.details, false)
		text := stripStyles(strings.Join(box, "\n"))

		if len(box) != tt.lines {
			t.Errorf("%s: %d lines, want %d:\n%s", tt.name, len(box), tt.lines, text)
		}

		for _, w := range tt.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: lacks %q:\n%s", tt.name, w, text)
			}
		}

		for _, a := range tt.absent {
			if strings.Contains(text, a) {
				t.Errorf("%s: shows %q:\n%s", tt.name, a, text)
			}
		}

		if len(zs) != 2 {
			t.Errorf("%s: %d buttons", tt.name, len(zs))
		}
	}
}

// TestChoiceRowTight checks how the two buttons are drawn as the width shrinks: three cells
// between them, then one, then no spaces inside the brackets.
//
// Parameters:
//   - t: test to report to
func TestChoiceRowTight(t *testing.T) {
	e := minRig(t, frameSandbox(t, dataFull), StartHome, 80, 24, "en", nil).r.e

	for _, c := range []struct {
		inner int
		want  string
	}{
		{16, "[ No ]   [ Yes ]"},
		{15, "[ No ] [ Yes ]"},
		{14, "[ No ] [ Yes ]"},
		{13, "[No] [Yes]"},
		{10, "[No] [Yes]"},
	} {
		row, zs := choiceRow(e, false, c.inner)

		if got := strings.TrimSpace(stripStyles(row)); got != c.want {
			t.Errorf("inner %d: %q, want %q", c.inner, got, c.want)
		}

		used := e.Metric.width(strings.TrimRight(stripStyles(row), " "))

		if len(zs) != 2 || zs[1].x != used-zs[1].w {
			t.Errorf("inner %d: zones %+v do not match %q", c.inner, zs, stripStyles(row))
		}
	}
}

// TestPickNoteCut checks the note beside a choice of a picker: it is cut with an ellipsis down to
// minNote cells, and left out below.
//
// Parameters:
//   - t: test to report to
func TestPickNoteCut(t *testing.T) {
	e := minRig(t, frameSandbox(t, dataFull), StartHome, 80, 24, "en", nil).r.e

	for _, c := range []struct {
		extra int
		want  string
	}{{1, ""}, {2, ""}, {3, "cu\u2026"}, {4, "cur\u2026"}, {7, "current"}} {
		p := newPick("T", []pickItem{{label: "abcdefgh", note: "current", value: "a"}}, 0,
			nil).(*pickOverlay)
		box, _ := p.draw(e, 16+c.extra, 10)
		row := stripStyles(box[1])

		if c.want == "" && strings.Contains(row, "\u2026") {
			t.Errorf("extra=%d: the note shows: %q", c.extra, row)
		}

		if c.want != "" && !strings.Contains(row, c.want) {
			t.Errorf("extra=%d: row %q lacks %q", c.extra, row, c.want)
		}
	}
}

// TestHintsPinSave checks that the key that saves a form is among the hints that go last.
//
// Parameters:
//   - t: test to report to
func TestHintsPinSave(t *testing.T) {
	e := minRig(t, frameSandbox(t, dataFull), StartHome, 80, 24, "en", nil).r.e

	hs := hintsOf(e, []key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
		key.NewBinding(key.WithKeys("down"), key.WithHelp("down", "x")),
		key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
	})

	if len(hs) != 3 || hs[1].pinned || !hs[2].pinned {
		t.Errorf("pinned hints = %+v, want the first and ctrl+s only", hs)
	}
}

// TestFormScrolledKeepsFocus checks that a block too short for a "N more" line still shows the
// first row of the focused field, even when the field has more rows than the block, and that the
// messages of a form take at most three lines.
//
// Parameters:
//   - t: test to report to
func TestFormScrolledKeepsFocus(t *testing.T) {
	e := minRig(t, frameSandbox(t, dataFull), StartHome, 80, 24, "en", nil).r.e
	rows := make([]string, 10)

	for i := range rows {
		rows[i] = fmt.Sprintf("row%d", i)
	}

	f := &form{}
	got, start, lift := f.scrolled(e, rows, []int{0, 3, 7}, 1, 7, 3)

	if start != 3 || lift != 0 || strings.Join(got, ",") != "row3,row4,row5" {
		t.Errorf("scrolled = %v from %d (lift %d), want row3 to row5", got, start, lift)
	}

	f.msg = strings.Repeat("word ", 20)

	if lines := f.messages(e, 24, 5); len(lines) != 3 {
		t.Errorf("a long message takes %d lines, want 3", len(lines))
	}
}

// TestSettingToastWording checks the message that follows a change of a setting: the name is kept
// whole when cutting it would leave nothing readable, and the message is worded again for the new
// width when the window is resized while it shows.
//
// Parameters:
//   - t: test to report to
func TestSettingToastWording(t *testing.T) {
	cfg := frameSandbox(t, dataConfig)
	g := minRig(t, cfg, StartConfig, minWidth, minHeight, "en", nil)
	g.send(settingWroteMsg{ID: "keepalive", Value: "true"})

	name := g.r.e.t("ui_set_keepalive")

	if got := g.toastText(); !strings.Contains(got, name) {
		t.Errorf("at %d columns the toast is %q, want the whole name %q", minWidth, got, name)
	}

	g.resize(80, 24)

	if got, want := g.toastText(), "saved: "+name+" (applied now)"; got != want {
		t.Errorf("after the window grew the toast is %q, want %q", got, want)
	}

	g.resize(30, 10)

	got := g.toastText()
	if !strings.Contains(got, "\u2026") || !strings.HasSuffix(got, "(applied now)") {
		t.Errorf("at 30 columns the toast is %q, want the name cut and the state kept", got)
	}
}

// TestTooSmallFrenchHint checks that the French hint of the too small screen is not cut at the
// smallest height: the blank row above it goes first.
//
// Parameters:
//   - t: test to report to
func TestTooSmallFrenchHint(t *testing.T) {
	for _, c := range []struct {
		lang, want string
	}{{"fr", "quitter"}, {"en", "to quit"}} {
		g := minRig(t, frameSandbox(t, dataFull), StartHome, minWidth-1, minHeight, c.lang, nil)
		text := g.text()

		if !strings.Contains(text, c.want) || strings.Contains(text, "\u2026") {
			t.Errorf("%s at %dx%d: the hint is cut:\n%s", c.lang, minWidth-1, minHeight, text)
		}

		if got := len(g.view()); got != minHeight {
			t.Errorf("%s: %d lines, want %d", c.lang, got, minHeight)
		}
	}
}

// TestTooSmallNoMouse checks that the interface asks for the mouse everywhere but on the too
// small screen, which takes none, so that the terminal keeps its own selection there.
//
// Parameters:
//   - t: test to report to
func TestTooSmallNoMouse(t *testing.T) {
	for _, c := range []struct {
		w, h int
		on   bool
	}{{minWidth, minHeight, true}, {minWidth - 1, minHeight, false}, {
		minWidth, minHeight - 1,
		false,
	}} {
		g, _ := mouseRig(t, c.w, c.h, StartHome, nil)

		if v := g.r.View(); (v.MouseMode != tea.MouseModeNone) != c.on || v.ReportFocus != c.on {
			t.Errorf("%dx%d: mouse mode %v, focus %v, want on = %v", c.w, c.h, v.MouseMode,
				v.ReportFocus, c.on)
		}
	}
}

// TestPaletteNoResultPadding checks that the message of a search without result, cut to the
// width, keeps the cell of padding before the border like every other row of the palette.
//
// Parameters:
//   - t: test to report to
func TestPaletteNoResultPadding(t *testing.T) {
	g := minRig(t, frameSandbox(t, dataFull), StartConnections, minWidth, minHeight, "en", nil)
	g.press("ctrl+k")
	g.typeText("zzzzzz")

	found := false

	for _, l := range strings.Split(g.text(), "\n") {
		if strings.Contains(l, "No result") {
			found = true

			if !strings.HasSuffix(strings.TrimSpace(l), " \u2502") && !strings.HasSuffix(
				strings.TrimSpace(l), "\u2026 \u2502") {
				t.Errorf("no padding before the border: %q", l)
			}
		}
	}

	if !found {
		t.Fatalf("no message in the palette:\n%s", g.text())
	}
}

// TestHelpTextNotCut checks that a sentence of the help is wrapped on as many lines as it needs,
// not cut with an ellipsis, in a narrow box: the end of every sentence is in the text.
//
// Parameters:
//   - t: test to report to
func TestHelpTextNotCut(t *testing.T) {
	e := minRig(t, frameSandbox(t, dataFull), StartHome, 80, 24, "en", nil).r.e
	ends := []string{
		"for this help.", "to add one.", "saved at once.",
		"click a key of the footer to press it.", "then Ctrl+Shift+C copies.", "works as usual.",
		"tote co does the same,", "asked about first.",
	}

	for _, inner := range []int{16, 20, 22, 30, 36} {
		var got []string

		for _, l := range (&helpOverlay{}).lines(e, inner) {
			got = append(got, strings.TrimSpace(stripStyles(l)))
		}

		joined := strings.Join(strings.Fields(strings.Join(got, " ")), " ")

		for _, end := range ends {
			if !strings.Contains(joined, end) {
				t.Errorf("inner %d: the help lacks the end %q:\n%s", inner, end,
					strings.Join(got, "\n"))
			}
		}
	}
}
