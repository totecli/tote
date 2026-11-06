package ui

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/config"
)

// widthCases lists the values of ui.width with the page they give on a terminal of 200 columns.
var widthCases = []struct {
	value  string
	width  int
	margin int
}{
	{"auto", 200, 0},
	{"120", 120, 40},
	{"160", 160, 20},
}

// widthFile returns a config.toml that sets ui.width.
//
// Parameters:
//   - value: value of the setting
func widthFile(value string) string {
	return "schema = 1\n\n# my own note\n[ui]\ncompact = \"never\"\nwidth = \"" + value + "\"\n"
}

// pageBounds returns the leftmost and the rightmost cell of the text of a frame, in cells, leaving
// out the blank lines.
//
// Parameters:
//   - g: rig to look at
func pageBounds(g *rig) (int, int) {
	e := g.r.e
	least, most := e.Cols, 0

	for _, l := range strings.Split(g.text(), "\n") {
		lead, right := frameBounds(e, l)
		if right < 0 {
			continue
		}

		least, most = min(least, lead), max(most, right)
	}

	return least, most
}

// TestWidthRow checks the row of the settings screen for ui.width: where it is, how it is
// stored, that no environment variable can fix it, and that every value it offers is one that the
// loader takes and the screen shows.
//
// Parameters:
//   - t: test to report to
func TestWidthRow(t *testing.T) {
	var group []string

	for _, d := range settingDefs {
		if d.table == "ui" {
			group = append(group, d.id)
		}
	}

	if want := "theme welcome compact width mouse"; strings.Join(group, " ") != want {
		t.Errorf("the ui group is %v, want %q", group, want)
	}

	d, ok := findSetting("width")
	if !ok {
		t.Fatal("no row for ui.width")
	}

	if d.kind != settingChoice || d.table != "ui" || d.key != "width" || d.path() != "ui.width" ||
		d.apply != applyNow || len(d.envs) != 0 || d.note || !d.writable() {
		t.Errorf("the row is %+v", d)
	}

	if want := []string{"auto", "120", "160"}; !slices.Equal(d.choices, want) {
		t.Errorf("choices = %v, want %v", d.choices, want)
	}

	if got := defaultValue(d); got != "auto" {
		t.Errorf("default = %q, want auto", got)
	}

	e := newRig(t, newSandbox(t)).r.e

	for _, c := range d.choices {
		s := d.with(config.DefaultSettings(), c)

		if d.value(s) != c || s.Validate() != nil || s.UI.Width != c {
			t.Errorf("%q: stored %q, validate %v", c, d.value(s), s.Validate())
		}

		if why := invalidSetting(e, d, c); why != "" {
			t.Errorf("%q is refused: %s", c, why)
		}
	}

	for _, bad := range []string{"", "80", "140", "200", "FULL", "AUTO", "full", "wide"} {
		if why := invalidSetting(e, d, bad); why == "" {
			t.Errorf("%q is accepted", bad)
		}
	}
}

// TestWidthRowText checks what the row says in both languages: the name, the three values, the
// description that tells that the page is centered and that the change is applied at once, and
// the frame of the value of the selected row.
//
// Parameters:
//   - t: test to report to
func TestWidthRowText(t *testing.T) {
	tests := []struct {
		lang   string
		name   string
		values []string
		desc   []string
		hint   string
	}{
		{
			"en", "Page width",
			[]string{"auto (terminal width)", "120 columns", "160 columns"},
			[]string{"centers it", "Applies at once"},
			"applied now",
		},
		{
			"fr", "Largeur de la page",
			[]string{"auto (largeur du terminal)", "120 colonnes", "160 colonnes"},
			[]string{"la centre", "S\u2019applique imm\u00e9diatement"},
			"appliqu\u00e9 tout de suite",
		},
	}

	for _, tt := range tests {
		for i, value := range []string{"auto", "120", "160"} {
			sb := newSandbox(t)
			writeFile(t, sb.Opts.Paths.ConfigFile(), widthFile(value))

			loaded, err := config.LoadSettings(sb.Opts.Paths.ConfigFile())
			if err != nil {
				t.Fatal(err)
			}

			g := newRig(t, sb.with(func(o *Options) {
				o.Start, o.Printer, o.Settings = StartConfig, newPrinter(tt.lang), loaded
				o.Width, o.Height = 160, 40
				o.Environ = append(o.Environ, "TOTE_LANG=")
			}))
			selectRow(g, "width")

			text := g.text()
			framed := "\u2039 " + tt.values[i] + " \u203a"

			for _, want := range []string{tt.name, framed, "ui.width", tt.hint} {
				if !strings.Contains(text, want) {
					t.Errorf("%s %s: the screen lacks %q:\n%s", tt.lang, value, want, text)
				}
			}

			if value == "auto" {
				for _, want := range append(tt.desc, strings.Join(tt.values, ", ")) {
					if !strings.Contains(text, want) {
						t.Errorf("%s: the panel lacks %q:\n%s", tt.lang, want, text)
					}
				}
			}

			if foot := g.footerText(); !strings.Contains(foot, "\u2190\u2192") {
				t.Errorf("%s: the footer does not offer the arrows: %q", tt.lang, foot)
			}
		}
	}
}

// TestWidthAbsentKeyIsAuto checks that a config.toml without ui.width shows the page as wide as
// the terminal, "auto (terminal width)" on the row, and that the screen does not count the key as
// set.
//
// Parameters:
//   - t: test to report to
func TestWidthAbsentKeyIsAuto(t *testing.T) {
	g, _ := configRig(t, "schema = 1\n[ui]\ntheme = \"dark\"\n")
	g.resize(200, 40)

	d, _ := findSetting("width")
	selectRow(g, "width")

	if e := g.r.e; e.Width != 200 || e.Margin != 0 {
		t.Errorf("an absent key gives width %d margin %d, want 200 and 0", e.Width, e.Margin)
	}

	if !strings.Contains(g.text(), "auto (terminal width)") ||
		!strings.Contains(g.text(), "default value") {
		t.Errorf("the row does not show the default:\n%s", g.text())
	}

	if g.r.scr.(*configScreen).file.present[d.path()] {
		t.Error("the screen counts an absent key as set")
	}
}

// TestAdjustWidth steps ui.width with the left and right keys, wrapping around at both ends.
//
// Parameters:
//   - t: test to report to
func TestAdjustWidth(t *testing.T) {
	tests := []struct {
		name string
		file string
		key  string
		want string
	}{
		{"right from the default", "schema = 1\n", "right", "120"},
		{"right from 120", widthFile("120"), "right", "160"},
		{"right wraps", widthFile("160"), "right", "auto"},
		{"left wraps", "schema = 1\n", "left", "160"},
		{"left from auto", widthFile("auto"), "left", "160"},
		{"left from 160", widthFile("160"), "left", "120"},
		{"left from 120", widthFile("120"), "left", "auto"},
		{"l", widthFile("120"), "l", "160"},
		{"h", widthFile("120"), "h", "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, s := configRig(t, tt.file)
			g.resize(200, 40)
			selectRow(g, "width")

			g.press(tt.key)
			g.await("the write", func() bool { return g.r.e.Settings.UI.Width == tt.want })
			g.settle()

			if got := readConfig(t, s); !strings.Contains(got, `width = "`+tt.want+`"`) {
				t.Errorf("config.toml lacks width = %q:\n%s", tt.want, got)
			}

			if got := readConfig(t, s); strings.Contains(tt.file, "# my own note") &&
				!strings.Contains(got, "# my own note") {
				t.Errorf("config.toml lost its comment:\n%s", got)
			}

			if got := g.toastText(); !strings.Contains(got, "Page width") ||
				!strings.Contains(got, "applied now") {
				t.Errorf("toast = %q, want the name and the scope", got)
			}

			if g.top() != nil || !idle(g) {
				t.Errorf("an overlay is open (%T) or the screen still waits", g.top())
			}
		})
	}
}

// TestWidthReset resets the page width with d then y: the key leaves config.toml with the other
// keys and the comment, and the page goes back to the whole terminal at once, with the zones of
// the mouse where a fresh start puts them.
//
// Parameters:
//   - t: test to report to
func TestWidthReset(t *testing.T) {
	for _, from := range []string{"160", "120"} {
		t.Run(from, func(t *testing.T) {
			g, s := configRig(t, widthFile(from))
			g.resize(200, 40)
			selectRow(g, "width")

			if from == "120" {
				checkPage(t, g, "start", 120, 40)
			} else {
				checkPage(t, g, "start", 160, 20)
			}

			g.press("d", "y")
			g.await("the reset", func() bool { return g.r.e.Settings.UI.Width == "auto" })
			g.settle()

			checkPage(t, g, "after the reset", 200, 0)

			got := readConfig(t, s)
			if strings.Contains(got, "width") {
				t.Errorf("the reset left the key:\n%s", got)
			}

			if !strings.Contains(got, "# my own note") || !strings.Contains(got, "compact") {
				t.Errorf("the reset lost another line:\n%s", got)
			}

			if g.top() != nil || !idle(g) {
				t.Errorf("an overlay is open (%T) or the screen still waits", g.top())
			}
		})
	}
}

// TestWidthPick opens the list of values with Enter, reads the labels and the current mark, picks
// the last value and checks that it is written and followed at once.
//
// Parameters:
//   - t: test to report to
func TestWidthPick(t *testing.T) {
	g, s := configRig(t, "schema = 1\n")
	g.resize(200, 40)
	selectRow(g, "width")

	g.press("enter")

	p, ok := g.top().(*pickOverlay)
	if !ok {
		t.Fatalf("enter opened %T", g.top())
	}

	var labels []string

	for _, it := range p.items {
		labels = append(labels, it.label)
	}

	want := []string{"auto (terminal width)", "120 columns", "160 columns"}
	if !slices.Equal(labels, want) {
		t.Errorf("labels = %v, want %v", labels, want)
	}

	if p.cur != 0 || p.items[0].note != "current" {
		t.Errorf("cursor %d, notes %q: the current value is not marked", p.cur, p.items[0].note)
	}

	if !strings.Contains(g.text(), "Page width") {
		t.Errorf("the title of the list is missing:\n%s", g.text())
	}

	g.press("down", "down", "enter")
	g.await("the write", func() bool { return g.r.e.Settings.UI.Width == "160" })
	g.settle()

	if got := readConfig(t, s); !strings.Contains(got, "[ui]") || !strings.Contains(got,
		`width = "160"`) {
		t.Errorf("config.toml:\n%s", got)
	}

	if e := g.r.e; e.Width != 160 || e.Margin != 20 {
		t.Errorf("width %d margin %d after 160, want 160 and 20", e.Width, e.Margin)
	}

	g.press("enter", "enter")
	g.settle()

	if g.r.e.Settings.UI.Width != "160" {
		t.Errorf("picking the current value changed the width to %q", g.r.e.Settings.UI.Width)
	}
}

// TestWidthReadOnly checks that a config.toml with a syntax error keeps the row read-only: the
// arrows, Enter and d say why and write nothing, and the page keeps its width.
//
// Parameters:
//   - t: test to report to
func TestWidthReadOnly(t *testing.T) {
	broken := "schema = 1\nlang = \"fr\n"
	g, s := configRig(t, "")
	writeFile(t, s.Opts.Paths.ConfigFile(), broken)
	g.send(reloadMsg{})
	g.resize(200, 40)
	selectRow(g, "width")

	for _, k := range []string{"right", "left", "l", "enter", "d"} {
		g.press(k)

		if g.top() != nil {
			t.Errorf("%s opened %T on a broken file", k, g.top())
		}

		if !g.r.toast.err {
			t.Errorf("%s: no error toast: %q", k, g.toastText())
		}
	}

	g.settle()

	if readConfig(t, s) != broken {
		t.Errorf("the broken file was changed:\n%s", readConfig(t, s))
	}

	if e := g.r.e; e.Width != 200 || e.Margin != 0 || e.Settings.UI.Width != "auto" {
		t.Errorf("width %d margin %d setting %q changed", e.Width, e.Margin, e.Settings.UI.Width)
	}
}

// TestWidthIsNeverLocked checks that no environment variable fixes ui.width: whatever the
// environment holds, the row has its default origin and can be changed.
//
// Parameters:
//   - t: test to report to
func TestWidthIsNeverLocked(t *testing.T) {
	g, _ := configRig(t, "schema = 1\n", "TOTE_WIDTH=full", "TOTE_UI_WIDTH=160", "COLUMNS=80",
		"TOTE_LANG=en", "NO_COLOR=1")
	g.resize(200, 40)
	selectRow(g, "width")

	d, _ := findSetting("width")
	src := sourceOf(g.r.e, g.r.scr.(*configScreen).file, d)

	if src.locked || src.origin != originDefault {
		t.Errorf("source = %+v, want the default and unlocked", src)
	}

	if why := g.r.scr.(*configScreen).refusal(g.r.e); why != "" {
		t.Errorf("the row is refused: %q", why)
	}

	g.press("right")
	g.await("the write", func() bool { return g.r.e.Settings.UI.Width == "120" })
	g.settle()

	if e := g.r.e; e.Width != 120 || e.Margin != 40 {
		t.Errorf("width %d margin %d after the write, want 120 and 40", e.Width, e.Margin)
	}
}

// checkPage checks that the geometry of the root and the frame it draws agree.
//
// Parameters:
//   - t: running test
//   - g: rig to look at
//   - label: what the check is about, for the failures
//   - width: expected width of the page
//   - margin: expected margin
func checkPage(t *testing.T, g *rig, label string, width, margin int) {
	t.Helper()

	e := g.r.e

	if e.Width != width || e.Margin != margin || e.Cols != 200 {
		t.Fatalf("%s: width %d margin %d cols %d, want %d %d 200", label, e.Width, e.Margin,
			e.Cols, width, margin)
	}

	lines := strings.Split(g.text(), "\n")
	if len(lines) != e.Height {
		t.Fatalf("%s: %d lines, want %d", label, len(lines), e.Height)
	}

	for i, l := range lines {
		if got := e.Metric.width(l); got > e.Cols {
			t.Errorf("%s: line %d is %d cells wide", label, i, got)
		}

		if lead, right := frameBounds(e, l); right >= 0 && (lead < margin || right > margin+width) {
			t.Errorf("%s: line %d spans %d to %d, outside the page %d to %d: %q", label, i, lead,
				right, margin, margin+width, l)
		}
	}

	if least, most := pageBounds(g); least != margin || most < margin+width-spareColumns {
		t.Errorf("%s: the text spans %d to %d, want the page %d to %d", label, least, most,
			margin, margin+width)
	}

	drawn, hits := g.r.compose(true)

	if len(drawn) != e.Height {
		t.Errorf("%s: %d lines in the frame of the hits", label, len(drawn))
	}

	for _, h := range hits {
		if h.press == "" && (h.x != margin || h.w != width) {
			t.Errorf("%s: the zone %+v does not cover the page %d to %d", label, h, margin,
				margin+width)
		}

		if h.x < margin || h.x+h.w > margin+width {
			t.Errorf("%s: the hit %+v is outside the page %d to %d", label, h, margin,
				margin+width)
		}
	}
}

// TestWidthAppliesAtOnce changes ui.width on the settings screen of a 200 column terminal and
// checks that the page follows after every write, through the path of a resize: the width, the
// margin, the lines of the frame, the zones of the mouse and the footer hints.
//
// Parameters:
//   - t: test to report to
func TestWidthAppliesAtOnce(t *testing.T) {
	g, s := configRig(t, "schema = 1\n[ui]\ncompact = \"never\"\n")
	g.resize(200, 40)
	selectRow(g, "width")
	checkPage(t, g, "start", 200, 0)

	steps := []struct {
		key  string
		want string
	}{
		{"right", "120"}, {"right", "160"}, {"right", "auto"}, {"left", "160"}, {"left", "120"},
	}

	for _, st := range steps {
		g.press(st.key)
		g.await("the write", func() bool { return g.r.e.Settings.UI.Width == st.want })
		g.settle()

		var width, margin int

		for _, c := range widthCases {
			if c.value == st.want {
				width, margin = c.width, c.margin
			}
		}

		checkPage(t, g, st.key+" to "+st.want, width, margin)

		if got := readConfig(t, s); !strings.Contains(got, `width = "`+st.want+`"`) {
			t.Errorf("%s: config.toml lacks the value:\n%s", st.want, got)
		}

		if g.r.e.Layout != layoutFull {
			t.Errorf("%s: the layout is %d, want the full one", st.want, g.r.e.Layout)
		}
	}
}

// TestWidthKeepsTheCursor checks that a change of ui.width leaves what does not depend on the
// width where it was: the selected row and the first visible row of a list and of the settings,
// on a terminal short enough to scroll.
//
// Parameters:
//   - t: test to report to
func TestWidthKeepsTheCursor(t *testing.T) {
	g, _ := configRig(t, widthFile("120"))
	g.resize(200, 14)
	selectRow(g, "update")

	c := g.r.scr.(*configScreen)
	if c.off == 0 {
		t.Fatalf("the test needs a scrolled screen, offset %d", c.off)
	}

	selectRow(g, "width")

	sel, off := c.sel, c.off

	g.press("right")
	g.await("the write", func() bool { return g.r.e.Settings.UI.Width == "160" })
	g.settle()

	if c.sel != sel || c.off != off {
		t.Errorf("the cursor moved from %d/%d to %d/%d", sel, off, c.sel, c.off)
	}

	if !strings.Contains(g.text(), "\u2039 160 columns \u203a") {
		t.Errorf("the selected row is not visible:\n%s", g.text())
	}

	// A list keeps its selected record too.
	l, _ := mouseRig(t, 200, 14, StartConnections, nil)
	l.press("end")

	list := l.r.scr.(*listScreen)
	sel, off = list.sel, list.off
	l.r.applySettings(settingsWidth(l.r.e.Settings, "120"))

	if list.sel != sel || list.off != off || l.r.e.Width != 120 {
		t.Errorf("the list moved from %d/%d to %d/%d (width %d)", sel, off, list.sel, list.off,
			l.r.e.Width)
	}

	if lines := l.view(); !strings.Contains(stripStyles(strings.Join(lines, "\n")), "hostile") {
		t.Errorf("the last record is not visible:\n%s", l.text())
	}
}

// settingsWidth returns a copy of the settings with another page width.
//
// Parameters:
//   - s: settings to start from
//   - value: value of ui.width
func settingsWidth(s config.Settings, value string) config.Settings {
	s.UI.Width = value

	return s
}

// TestWidthLiveKeepsTheMouse delivers the end of a write of ui.width while the pointer rests on a
// row and a text is selected: the selection is dropped as a resize drops it, and the hover and the
// zones are worked out again for the new page.
//
// Parameters:
//   - t: test to report to
func TestWidthLiveKeepsTheMouse(t *testing.T) {
	for _, to := range []string{"160", "auto"} {
		t.Run(to, func(t *testing.T) {
			g, s := configRig(t, widthFile("120"))
			g.resize(200, 40)
			writeFile(t, s.Opts.Paths.ConfigFile(), widthFile(to))

			y, x := lineOf(g, "Theme")
			g.move(x, y)

			if h, ok := g.over(); !ok || h.x != 40 || h.w != 120 {
				t.Fatalf("hover before = %+v (%v), want the row of the 120 page", h, ok)
			}

			drag(g, cellPos{x, y}, cellPos{x + 4, y})

			if !g.r.sel.drag || !strings.Contains(strings.Join(markedScreen(g), ""), "[[") {
				t.Fatal("no text is selected before the change")
			}

			g.move(x, y)
			g.send(settingWroteMsg{ID: "width", Value: to})

			e := g.r.e
			if want := map[string]int{"160": 160, "auto": 200}[to]; e.Width != want {
				t.Fatalf("width = %d, want %d", e.Width, want)
			}

			if !reflect.DeepEqual(g.r.sel, selection{}) {
				t.Errorf("the selection survived the change: %+v", g.r.sel)
			}

			if strings.Contains(strings.Join(markedScreen(g), ""), "[[") {
				t.Error("the highlight is still drawn")
			}

			h, ok := g.over()
			if !ok || h.x != e.Margin || h.w != e.Width {
				t.Errorf("hover after = %+v (%v), want the row of the new page %d to %d", h, ok,
					e.Margin, e.Margin+e.Width)
			}

			_, fresh := g.r.compose(true)
			if !slices.Equal(g.r.hover.hits, fresh) {
				t.Errorf("the hits of the hover are stale: %v, want %v", g.r.hover.hits, fresh)
			}

			if !g.r.hover.fresh {
				t.Error("the hover was not worked out again")
			}

			// The pointer is where it was: on the margin of the new page when it has one.
			g.move(0, y)

			if _, ok := g.over(); ok != (e.Margin == 0) {
				t.Errorf("the cell 0 is hovered: %v with a margin of %d", ok, e.Margin)
			}
		})
	}
}

// TestWidthMouse plays the mouse at 200 columns for each value of ui.width: a click on a row and
// on a hint of the footer reaches the right element wherever the page starts, a click on the blank
// margin or on the blank cells at the right does nothing, the hover follows the page, and a
// selection by dragging takes the text under the pointer.
//
// Parameters:
//   - t: test to report to
func TestWidthMouse(t *testing.T) {
	dragged := map[string]string{}

	for _, c := range widthCases {
		t.Run(c.value, func(t *testing.T) {
			var copied []string

			g, _ := mouseRig(t, 200, 40, StartConnections, func(o *Options) {
				o.Settings.UI.Width = c.value
				o.Clipboard = func(_ context.Context, _ []string, text string) string {
					copied = append(copied, text)

					return "fake"
				}
			})

			e := g.r.e
			if e.Width != c.width || e.Margin != c.margin {
				t.Fatalf("width %d margin %d, want %d %d", e.Width, e.Margin, c.width, c.margin)
			}

			l := g.r.scr.(*listScreen)
			y, x := lineOf(g, "staging")

			if x < c.margin+2 {
				t.Fatalf("the name starts at %d, inside the margin %d", x, c.margin)
			}

			// A click on the name, and on the first and the last cell of the row.
			for _, at := range []int{x, c.margin, c.margin + c.width - 1} {
				g.click(x, y-1)
				g.click(at, y)

				if it := l.selected(); it == nil || it.Key != "staging" {
					t.Errorf("click at column %d: selected %+v, want staging", at, it)
				}
			}

			// Blank cells on both sides of the page do nothing.
			blanks := []int{0, c.margin - 1, c.margin + c.width, e.Cols - 1}
			for _, at := range blanks {
				if at < 0 || at >= e.Cols || (at >= c.margin && at < c.margin+c.width) {
					continue
				}

				g.click(x, y-1)
				g.click(at, y)

				if it := l.selected(); it == nil || it.Key != "prod" {
					t.Errorf("click on the blank column %d: selected %+v, want prod", at, it)
				}
			}

			// A hint of the footer presses its key wherever the footer starts.
			g.click(x, y-1)
			clickFooter(g, "n new")

			if _, ok := g.r.overlayTop().(*form); !ok {
				t.Errorf("the hint n opened %T", g.r.overlayTop())
			}

			g.press("esc")

			if g.r.overlayTop() != nil {
				t.Errorf("esc left %T open", g.r.overlayTop())
			}

			// The hover is on the row under the pointer and never on the margin.
			g.move(x, y)

			if h, ok := g.over(); !ok || h.y != y || h.x != c.margin || h.w != c.width {
				t.Errorf("hover = %+v (%v), want the row at %d, %d wide", h, ok, c.margin,
					c.width)
			}

			if c.margin > 0 {
				g.move(c.margin-1, y)

				if _, ok := g.over(); ok {
					t.Error("the blank margin is hovered")
				}
			}

			foot := g.footerAt("n new")
			g.move(foot.x, foot.y)

			if h, ok := g.over(); !ok || h.press != "n" || h.x != foot.x {
				t.Errorf("hover on the footer = %+v (%v)", h, ok)
			}

			// Dragging over the name selects it and copies it, the margin being left out.
			g.move(0, 0)
			drag(g, cellPos{x, y}, cellPos{x + 6, y})

			if len(copied) != 1 || copied[0] != "staging" {
				t.Errorf("copied = %q, want [staging]", copied)
			}

			if got := markedScreen(g)[y]; !strings.Contains(got, "[[staging]]") {
				t.Errorf("line %d = %q, want the name highlighted", y, got)
			}

			// A drag that starts in the blank margin takes the text of the row and nothing else,
			// the same text whatever the margin is.
			copied = nil
			drag(g, cellPos{1, y}, cellPos{x + 6, y})

			if len(copied) != 1 || strings.TrimSpace(copied[0]) != "staging" {
				t.Fatalf("drag from the margin copied %q", copied)
			}

			dragged[c.value] = copied[0]
		})
	}

	if dragged["auto"] != dragged["120"] || dragged["120"] != dragged["160"] {
		t.Errorf("the selected text depends on the margin: %q", dragged)
	}
}

// TestWidthFrames draws the lists, the welcome page, the settings and the palette at 200 columns
// for each value of ui.width and checks the rules every frame obeys: the lines stay between the
// margin and the right edge of the page, and the table fills the page.
//
// Parameters:
//   - t: test to report to
func TestWidthFrames(t *testing.T) {
	starts := []struct {
		name  string
		start Start
		keys  []string
	}{
		{"home", StartHome, nil},
		{"connections", StartConnections, nil},
		{"repos", StartRepos, nil},
		{"aliases", StartAliases, nil},
		{"funcs", StartFuncs, nil},
		{"settings", StartConfig, nil},
		{"details", StartConnections, []string{"v"}},
		{"help", StartConnections, []string{"?"}},
		{"palette", StartConnections, []string{"ctrl+k"}},
		{"form", StartConnections, []string{"n"}},
		{"confirm", StartConnections, []string{"d"}},
	}

	for _, c := range widthCases {
		for _, st := range starts {
			name := fmt.Sprintf("%s %s", c.value, st.name)

			g, _ := mouseRig(t, 200, 40, st.start, func(o *Options) {
				o.Settings.UI.Width = c.value
			})
			g.press(st.keys...)

			if len(g.r.overlays) > 0 {
				// An overlay floats alone, centered on the terminal.
				assertFloating(t, g, "")

				continue
			}

			checkPage(t, g, name, c.width, c.margin)
		}
	}
}

// TestWidthColumnsFillThePage checks the table of a list on a page of every width: the columns of
// the heading and of the rows are the ones the rule gives, the short columns are never wider than
// twice their natural width, and the description takes the rest of the page.
//
// Parameters:
//   - t: test to report to
func TestWidthColumnsFillThePage(t *testing.T) {
	for _, c := range widthCases {
		g, _ := mouseRig(t, 200, 40, StartConnections, func(o *Options) {
			o.Settings.UI.Width = c.value
		})

		e := g.r.e
		l := g.r.scr.(*listScreen)
		ws := l.widths(e)

		if len(ws) != 4 {
			t.Fatalf("%s: %d columns, want 4", c.value, len(ws))
		}

		avail := e.Width - e.indent() - spareColumns - 2
		if totalWidth(ws)+columnGap*(len(ws)-1) != avail {
			t.Errorf("%s: the columns %v take %d of the %d cells of the page", c.value, ws,
				totalWidth(ws)+columnGap*(len(ws)-1), avail)
		}

		for i, w := range ws[:3] {
			if w < l.natural[i] || w > 2*l.natural[i] {
				t.Errorf("%s: column %d is %d wide, natural %d", c.value, i, w, l.natural[i])
			}
		}

		if ws[3] < l.natural[3] {
			t.Errorf("%s: the description is %d wide, natural %d", c.value, ws[3], l.natural[3])
		}

		lines := strings.Split(g.text(), "\n")
		head := slices.IndexFunc(lines, func(l string) bool {
			return strings.Contains(l, "NAME") && strings.Contains(l, "DESCRIPTION")
		})

		if head < 0 {
			t.Fatalf("%s: no heading:\n%s", c.value, g.text())
		}

		at := c.margin + 4

		for i, h := range []string{"NAME", "TARGET", "USER", "DESCRIPTION"} {
			if got := strings.Index(lines[head], h); got != at {
				t.Errorf("%s: %s starts at %d, want %d", c.value, h, got, at)
			}

			at += ws[i] + columnGap
		}
	}
}

// TestWidthTinyAndCompactIgnoreIt checks that the choice between the layouts is about the
// terminal and not about ui.width: a narrow terminal gets the same frame for every value.
//
// Parameters:
//   - t: test to report to
func TestWidthTinyAndCompactIgnoreIt(t *testing.T) {
	for _, size := range [][2]int{
		{39, 12},
		{40, 10},
		{60, 24},
		{69, 40},
		{70, 20},
		{100, 30},
		{119, 30},
		{120, 30},
	} {
		var frames []string

		for _, c := range widthCases {
			g, _ := mouseRig(t, size[0], size[1], StartConnections, func(o *Options) {
				o.Settings.UI.Width = c.value
			})

			frames = append(frames, g.text())

			if e := g.r.e; e.Layout != chooseLayout(size[0], size[1], e.Settings.UI) ||
				e.Margin != 0 || e.Width != size[0] {
				t.Errorf("%dx%d %s: layout %d width %d margin %d", size[0], size[1], c.value,
					e.Layout, e.Width, e.Margin)
			}
		}

		if frames[0] != frames[1] || frames[1] != frames[2] {
			t.Errorf("%dx%d: the frame depends on ui.width", size[0], size[1])
		}
	}
}

// TestWidthAutoFollowsEveryResize checks that with "auto" the page is the terminal after each
// resize, whatever the width, and that the margin stays nil.
//
// Parameters:
//   - t: test to report to
func TestWidthAutoFollowsEveryResize(t *testing.T) {
	g, _ := mouseRig(t, 200, 40, StartHome, func(o *Options) { o.Settings.UI.Width = "auto" })

	for _, cols := range []int{199, 301, 121, 120, 100, 50, 200} {
		g.send(tea.WindowSizeMsg{Width: cols, Height: 40})

		if e := g.r.e; e.Width != cols || e.Margin != 0 || e.Cols != cols {
			t.Errorf("%d columns: width %d margin %d cols %d", cols, e.Width, e.Margin, e.Cols)
		}
	}
}
