package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/config"
)

// minSizes are the sizes around the minimum: the minimum itself, one more column, one more row,
// both, and the three sizes just below it, where the too small screen shows.
var minSizes = []struct {
	w, h  int
	tiny  bool
	label string
}{
	{minWidth, minHeight, false, "minimum"},
	{minWidth + 1, minHeight, false, "one column more"},
	{minWidth, minHeight + 1, false, "one row more"},
	{minWidth + 1, minHeight + 1, false, "one column and one row more"},
	{minWidth - 1, minHeight, true, "one column less"},
	{minWidth, minHeight - 1, true, "one row less"},
	{minWidth - 1, minHeight - 1, true, "one column and one row less"},
}

// tooSmallTitles are the titles of the too small screen in the languages of the catalogs.
var tooSmallTitles = map[string]string{"en": "Terminal too small", "fr": "Terminal trop petit"}

// TestMinimumSizeScreens draws every screen and overlay of the interface at the sizes around the
// minimum, in English and in French, in every mode: no line wider than the terminal, exactly
// the height in lines, the interface at the minimum and one column or one row more, and the too
// small screen one column or one row less.
//
// Parameters:
//   - t: test to report to
func TestMinimumSizeScreens(t *testing.T) {
	sets := map[dataSet]*sandbox{}

	for _, d := range []dataSet{
		dataFull, dataPlain, dataEmpty, dataBroken, dataConfig,
		dataBadConfig, dataVia, dataPlugins,
	} {
		sets[d] = frameSandbox(t, d)
	}

	for _, c := range frameCases() {
		if strings.HasPrefix(c.name, "toosmall") || c.sizes != nil {
			continue
		}

		for lang, title := range tooSmallTitles {
			t.Run(c.name+" "+lang, func(t *testing.T) {
				lc := c
				setup := c.setup
				lc.setup = func(o *Options) {
					if setup != nil {
						setup(o)
					}

					o.Printer = newPrinter(lang)
				}

				for _, m := range frameModes() {
					d := c.data
					if m.ascii && d == dataFull {
						d = dataPlain
					}

					for _, sz := range minSizes {
						frame := renderFrame(t, sets[d], lc, m, [2]int{sz.w, sz.h})
						name := fmt.Sprintf("%s %s %s %dx%d", c.name, lang, m.name, sz.w, sz.h)

						checkScreen(t, name, frame, sz.w, sz.h, m.ascii)

						if has := strings.Contains(stripStyles(frame), title); has != sz.tiny {
							t.Errorf("%s (%s): the too small screen shows is %v, want %v:\n%s",
								name, sz.label, has, sz.tiny, stripStyles(frame))
						}
					}
				}
			})
		}
	}
}

// minRig builds a rig on a screen at a size, in a language, without colors.
//
// Parameters:
//   - t: running test
//   - s: sandbox holding the data
//   - start: screen to start on
//   - w: width in cells
//   - h: height in rows
//   - lang: "en" or "fr"
//   - setup: edit of the options, nil for none
func minRig(t *testing.T, s *sandbox, start Start, w, h int, lang string,
	setup func(*Options),
) *rig {
	t.Helper()

	return newRig(t, s.with(func(o *Options) {
		o.Start = start
		o.Width, o.Height = w, h
		o.Environ = append(append([]string(nil), o.Environ...), "TOTE_COLOR=none")
		o.Printer = newPrinter(lang)

		if setup != nil {
			setup(o)
		}
	}))
}

// markedLines returns the lines of a frame that carry the marker of a selected or focused row,
// without the header, whose prompt glyph is not followed by a space.
//
// Parameters:
//   - g: rig whose screen is read
func markedLines(g *rig) []string {
	var out []string

	for _, l := range strings.Split(g.text(), "\n") {
		if strings.Contains(l, g.r.e.Glyphs.marker+" ") {
			out = append(out, l)
		}
	}

	return out
}

// TestMinimumSizeSelectionVisible walks the selection through the home menu, the lists, the
// settings and the palette, and the focus through the fields of a form, at the minimum and one row
// and one column more, and checks that the selected row is on the screen after each key and shows
// its start; that what is typed in a field shows; and that both answers of a question show.
//
// Parameters:
//   - t: test to report to
func TestMinimumSizeSelectionVisible(t *testing.T) {
	s := frameSandbox(t, dataFull)
	cfg := frameSandbox(t, dataConfig)

	for _, lang := range []string{"en", "fr"} {
		for _, sz := range minSizes[:4] {
			name := fmt.Sprintf("%s %dx%d", lang, sz.w, sz.h)

			t.Run("home "+name, func(t *testing.T) {
				for _, setup := range []func(*Options){
					nil,
					func(o *Options) { o.Settings.UI.Welcome = "off" },
					func(o *Options) { o.Settings.UI.Compact = "never" },
				} {
					g := minRig(t, s, StartHome, sz.w, sz.h, lang, setup)

					for step := range 2 * homeEntries {
						e := g.r.e
						want := e.t("ui_config_title")

						if i := step % homeEntries; i < len(allKinds) {
							want = e.t(allKinds[i].titleKey())
						}

						want = string([]rune(want)[:min(len([]rune(want)), 4)])
						rows := markedLines(g)

						if len(rows) != 1 || !strings.Contains(rows[0], want) {
							t.Fatalf("%s step %d: marked rows %q, want one with %q:\n%s", name,
								step, rows, want, g.text())
						}

						g.press("down")
					}
				}
			})

			t.Run("lists "+name, func(t *testing.T) {
				for _, start := range []Start{
					StartConnections, StartRepos, StartAliases,
					StartFuncs,
				} {
					g := minRig(t, s, start, sz.w, sz.h, lang, nil)

					for step := 0; step < 12; step++ {
						l := g.r.scr.(*listScreen)
						rows := markedLines(g)

						if len(rows) != 1 {
							t.Fatalf("%s %d step %d: marked rows %q:\n%s", name, start, step, rows,
								g.text())
						}

						if it := l.selected(); it != nil && isASCII(it.Cells[0]) {
							want := it.Cells[0][:min(len(it.Cells[0]), 3)]
							if !strings.Contains(rows[0], want) {
								t.Fatalf("%s %d step %d: the selected row lacks %q:\n%s", name,
									start, step, want, g.text())
							}
						}

						g.press("down")
					}

					g.press("end")

					if rows := markedLines(g); len(rows) != 1 {
						t.Fatalf("%s %d at the end: marked rows %q:\n%s", name, start, rows,
							g.text())
					}
				}
			})

			t.Run("settings "+name, func(t *testing.T) {
				g := minRig(t, cfg, StartConfig, sz.w, sz.h, lang, nil)

				for step := 0; step < 20; step++ {
					if rows := markedLines(g); len(rows) != 1 {
						t.Fatalf("%s step %d: marked rows %q:\n%s", name, step, rows, g.text())
					}

					g.press("down")
				}
			})

			t.Run("forms "+name, func(t *testing.T) {
				for _, start := range []Start{
					StartConnections, StartRepos, StartAliases,
					StartFuncs,
				} {
					g := minRig(t, s, start, sz.w, sz.h, lang, nil)
					g.press("n")
					g.typeText("zq")

					if rows := markedLines(g); len(rows) != 1 || !strings.Contains(rows[0], "zq") {
						t.Fatalf("%s %d: the field being typed in: %q:\n%s", name, start, rows,
							g.text())
					}

					n := len(g.r.overlays[0].(*form).fields)

					for step := 0; step < n+1; step++ {
						g.press("tab")

						if rows := markedLines(g); len(rows) != 1 {
							t.Fatalf("%s %d field %d: marked rows %q:\n%s", name, start, step, rows,
								g.text())
						}
					}

					g.press("ctrl+s")

					if rows := markedLines(g); len(rows) != 1 {
						t.Fatalf("%s %d after ctrl+s: marked rows %q:\n%s", name, start, rows,
							g.text())
					}
				}
			})

			t.Run("questions "+name, func(t *testing.T) {
				for _, start := range []Start{
					StartConnections, StartRepos, StartAliases,
					StartFuncs,
				} {
					g := minRig(t, s, start, sz.w, sz.h, lang, nil)
					g.press("d")

					text := g.text()
					for _, key := range []string{"ui_choice_no", "ui_choice_yes"} {
						if want := g.r.e.t(key); !strings.Contains(text, want) {
							t.Fatalf("%s %d: the delete question lacks %q:\n%s", name, start, want,
								text)
						}
					}
				}

				g := minRig(t, s, StartConnections, sz.w, sz.h, lang, nil)
				g.press("n")
				g.typeText("x")
				g.press("esc")

				text := g.text()
				for _, key := range []string{"ui_choice_no", "ui_choice_yes"} {
					if want := g.r.e.t(key); !strings.Contains(text, want) {
						t.Fatalf("%s: the discard question lacks %q:\n%s", name, want, text)
					}
				}
			})

			t.Run("palette "+name, func(t *testing.T) {
				g := minRig(t, s, StartConnections, sz.w, sz.h, lang, nil)
				g.press("ctrl+k")

				for step := 0; step < 12; step++ {
					if rows := markedLines(g); len(rows) != 2 {
						t.Fatalf("%s step %d: marked rows %q, want the query and a result:\n%s",
							name, step, rows, g.text())
					}

					if n := strings.Count(g.text(), g.r.e.Glyphs.crumb); n < 2 {
						t.Fatalf("%s step %d: %d results shown, want at least 2:\n%s", name, step,
							n, g.text())
					}

					g.press("down")
				}
			})
		}
	}
}

// isASCII reports whether a text holds nothing but printable ASCII.
//
// Parameters:
//   - s: text to look at
func isASCII(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}

	return s != ""
}

// TestMinimumSizeAlways pins the single source of the minimum: the too small screen asks for the
// sizes of minWidth and minHeight, in both languages, and the layout name and the exported size
// agree with them.
//
// Parameters:
//   - t: test to report to
func TestMinimumSizeAlways(t *testing.T) {
	s := frameSandbox(t, dataFull)

	for lang, want := range map[string]string{
		"en": fmt.Sprintf("needs %d \u00D7 %d", minWidth, minHeight),
		"fr": fmt.Sprintf("il faut %d \u00D7 %d", minWidth, minHeight),
	} {
		g := minRig(t, s, StartHome, minWidth-1, minHeight+5, lang, nil)

		if text := g.text(); !strings.Contains(text, want) {
			t.Errorf("%s: the too small screen lacks %q:\n%s", lang, want, text)
		}
	}

	if w, h := MinSize(); w != minWidth || h != minHeight {
		t.Errorf("MinSize() = %dx%d, want %dx%d", w, h, minWidth, minHeight)
	}

	if got := LayoutName(minWidth, minHeight, config.DefaultSettings()); got != "compact" {
		t.Errorf("the layout at the minimum is %q, want compact", got)
	}
}

// TestMinimumSizeHomeMouse checks the menu that scrolls at the minimum: a click on the "N more"
// line moves the window, a click on an entry opens it, and the wheel keeps the selection in view.
//
// Parameters:
//   - t: test to report to
func TestMinimumSizeHomeMouse(t *testing.T) {
	g, _ := mouseRig(t, minWidth, minHeight, StartHome, nil)

	for y := range minHeight {
		for x := range minWidth {
			g.move(x, y)
		}
	}

	if strings.Contains(g.text(), "Settings") {
		t.Fatalf("the menu fits at the minimum, the test has nothing to scroll:\n%s", g.text())
	}

	clickOn(g, g.r.e.Glyphs.below+" ")

	if !strings.Contains(g.text(), "Settings") {
		t.Fatalf("a click on the line of the hidden entries did not scroll:\n%s", g.text())
	}

	if rows := markedLines(g); len(rows) != 1 {
		t.Errorf("marked rows %q after the click:\n%s", rows, g.text())
	}

	clickOn(g, "Settings")

	if _, ok := g.r.scr.(*configScreen); !ok {
		t.Errorf("a click on Settings opened %T:\n%s", g.r.scr, g.text())
	}

	g, _ = mouseRig(t, minWidth, minHeight, StartHome, nil)

	for range 6 {
		g.send(tea.MouseWheelMsg{X: 3, Y: 3, Button: tea.MouseWheelDown})
	}

	if rows := markedLines(g); len(rows) != 1 || !strings.Contains(rows[0], "Settings") {
		t.Errorf("the wheel left the selection out of view: %q\n%s", rows, g.text())
	}
}
