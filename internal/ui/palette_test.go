package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/shell"
)

// openPalette opens the palette with Ctrl-K and returns it.
//
// Parameters:
//   - g: rig to drive
func openPalette(g *rig) *paletteOverlay {
	g.t.Helper()
	g.press("ctrl+k")

	if len(g.r.overlays) != 1 {
		g.t.Fatalf("ctrl+k left %d overlays", len(g.r.overlays))
	}

	p, ok := g.r.overlays[0].(*paletteOverlay)
	if !ok {
		g.t.Fatalf("overlay is %T, want the palette", g.r.overlays[0])
	}

	return p
}

// top returns the name of the first result.
//
// Parameters:
//   - p: palette
func top(p *paletteOverlay) string {
	if en := p.selected(); en != nil {
		return en.Name
	}

	return ""
}

// TestPaletteOpensFromEveryScreen checks that Ctrl-K opens the palette on the welcome page and
// on each list, and that Esc and a second Ctrl-K close it and leave the screen untouched.
//
// Parameters:
//   - t: test to report to
func TestPaletteOpensFromEveryScreen(t *testing.T) {
	for _, start := range []Start{
		StartHome, StartConnections, StartRepos, StartAliases,
		StartFuncs,
	} {
		for _, closer := range []string{"esc", "ctrl+k"} {
			g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = start }))
			before := fmt.Sprintf("%T", g.r.scr)

			p := openPalette(g)

			if p.sel != 0 || len(p.hits) != len(p.entries) {
				t.Errorf("start %d: sel %d hits %d entries %d", start, p.sel, len(p.hits),
					len(p.entries))
			}

			if !strings.Contains(g.text(), "Palette") {
				t.Errorf("start %d: the palette is not drawn:\n%s", start, g.text())
			}

			g.press(closer)

			if len(g.r.overlays) != 0 || fmt.Sprintf("%T", g.r.scr) != before || g.Quit {
				t.Errorf("start %d %s: overlays %d screen %T quit %v", start, closer,
					len(g.r.overlays), g.r.scr, g.Quit)
			}
		}
	}
}

// TestPaletteEntries checks what the palette lists: the records of the four kinds and the
// actions, sanitized.
//
// Parameters:
//   - t: test to report to
func TestPaletteEntries(t *testing.T) {
	g := newRig(t, newSandbox(t))
	p := openPalette(g)

	counts := map[scope]int{}
	for _, en := range p.entries {
		counts[en.Scope]++

		if en.Name == "" || len(en.Names) == 0 {
			t.Errorf("entry without name: %+v", en)
		}

		if strings.ContainsAny(en.Name+en.Sub, "\x1b\x07\u202e") {
			t.Errorf("entry %q holds a control character", en.Name)
		}
	}

	want := map[scope]int{
		scopeConn: 6, scopeRepo: 2, scopeAlias: 2, scopeFunc: 2,
		scopeAction: len(actions),
	}

	for sc, n := range want {
		if counts[sc] != n {
			t.Errorf("scope %d has %d entries, want %d", sc, counts[sc], n)
		}
	}
}

// TestPaletteRunsLikeTheList checks that Enter on each kind does exactly what Enter in the
// list does.
//
// Parameters:
//   - t: test to report to
func TestPaletteRunsLikeTheList(t *testing.T) {
	t.Run("connection", func(t *testing.T) {
		s := newSandbox(t)
		g := newRig(t, s)
		openPalette(g)
		g.typeText("c prod")
		g.press("enter")

		var got activateMsg

		for _, m := range g.Seen {
			if a, ok := m.(activateMsg); ok {
				got = a
			}
		}

		if got.Kind != kindConn || got.Key != "prod" || g.r.guard.busy.Load() != 1 {
			t.Errorf("activateMsg %+v, guard busy %d", got, g.r.guard.busy.Load())
		}

		if len(g.r.overlays) != 0 {
			t.Error("the palette is still open")
		}
	})

	t.Run("connection by alias", func(t *testing.T) {
		s := newSandbox(t)
		g := newRig(t, s)
		openPalette(g)
		g.typeText("p")

		if p := g.r.overlays[0].(*paletteOverlay); top(p) != "prod" {
			t.Errorf("top = %q, want prod", top(p))
		}
	})

	t.Run("needs review", func(t *testing.T) {
		s := newSandbox(t)
		g := newRig(t, s)
		openPalette(g)
		g.typeText("c legacy")
		g.press("enter")

		if len(s.Opts.Runner.(*fakeRunner).calls()) != 0 {
			t.Error("a connection that needs review was run")
		}

		if !strings.Contains(g.toastText(), "needs review") {
			t.Errorf("toast = %q", g.toastText())
		}
	})

	t.Run("repository", func(t *testing.T) {
		s := newSandbox(t)
		g := newRig(t, s)
		openPalette(g)
		g.typeText("r proj")
		g.press("enter")

		if !g.Quit || g.r.result.Action != shell.ChangeDir || g.r.result.Payload != s.Repo {
			t.Errorf("quit %v result %+v, want a cd to %s", g.Quit, g.r.result, s.Repo)
		}
	})

	t.Run("alias", func(t *testing.T) {
		g := newRig(t, newSandbox(t))
		openPalette(g)
		g.typeText("a ll")
		g.press("enter")

		if !g.Quit || g.r.result.Action != shell.Insert || g.r.result.Payload != "ls -la" {
			t.Errorf("quit %v result %+v", g.Quit, g.r.result)
		}
	})

	t.Run("function", func(t *testing.T) {
		g := newRig(t, newSandbox(t))
		openPalette(g)
		g.typeText("d hello")
		g.press("enter")

		if !g.Quit || g.r.result.Action != shell.Insert || g.r.result.Payload != "hello" {
			t.Errorf("quit %v result %+v", g.Quit, g.r.result)
		}
	})

	t.Run("repository with a missing directory", func(t *testing.T) {
		g := newRig(t, newSandbox(t))
		openPalette(g)
		g.typeText("r docs")
		g.press("enter")

		if g.Quit || !strings.Contains(g.toastText(), "not found") {
			t.Errorf("quit %v toast %q", g.Quit, g.toastText())
		}
	})
}

// TestPaletteActions checks the entries behind the ">" prefix.
//
// Parameters:
//   - t: test to report to
func TestPaletteActions(t *testing.T) {
	tests := []struct {
		query string
		check func(t *testing.T, g *rig)
	}{
		{">quit", func(t *testing.T, g *rig) {
			if !g.Quit || !g.r.result.Empty() {
				t.Errorf("quit %v result %+v", g.Quit, g.r.result)
			}
		}},
		{">aliases", func(t *testing.T, g *rig) {
			if l, ok := g.r.scr.(*listScreen); !ok || l.kind != kindAlias {
				t.Errorf("screen = %T", g.r.scr)
			}
		}},
		{">funcs", func(t *testing.T, g *rig) {
			if l, ok := g.r.scr.(*listScreen); !ok || l.kind != kindFunc {
				t.Errorf("screen = %T", g.r.scr)
			}
		}},
		{">home", func(t *testing.T, g *rig) {
			if _, ok := g.r.scr.(*homeScreen); !ok {
				t.Errorf("screen = %T", g.r.scr)
			}
		}},
		{">reload", func(t *testing.T, g *rig) {
			if g.toastText() != "data reloaded" {
				t.Errorf("toast = %q", g.toastText())
			}
		}},
		{">new conn", func(t *testing.T, g *rig) {
			if !seen[addMsg](g) {
				t.Error("addMsg was not sent")
			}
		}},
		{">new alias", func(t *testing.T, g *rig) {
			for _, m := range g.Seen {
				if a, ok := m.(addMsg); ok && a.Kind != kindAlias {
					t.Errorf("addMsg for kind %d", a.Kind)
				}
			}
		}},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
		openPalette(g)
		g.typeText(tt.query)
		g.press("enter")
		tt.check(t, g)
	}
}

// seen reports whether a message of a type went through the rig.
//
// Parameters:
//   - g: rig that recorded the messages
func seen[T any](g *rig) bool {
	for _, m := range g.Seen {
		if _, ok := m.(T); ok {
			return true
		}
	}

	return false
}

// TestPaletteEdit checks Ctrl-E: it asks for the form of a record and does nothing on an
// action or an empty result.
//
// Parameters:
//   - t: test to report to
func TestPaletteEdit(t *testing.T) {
	g := newRig(t, newSandbox(t))
	openPalette(g)
	g.typeText("a ll")
	g.press("ctrl+e")

	var got editMsg

	for _, m := range g.Seen {
		if e, ok := m.(editMsg); ok {
			got = e
		}
	}

	if got.Kind != kindAlias || got.Key != "ll" {
		t.Errorf("editMsg = %+v", got)
	}

	for _, q := range []string{">quit", "zzzz"} {
		h := newRig(t, newSandbox(t))
		openPalette(h)
		h.typeText(q)
		h.press("ctrl+e")

		if len(h.r.overlays) != 1 || h.Quit || seen[editMsg](h) {
			t.Errorf("%q: ctrl+e closed the palette or acted", q)
		}
	}
}

// TestPaletteNoResult checks the empty state and that Enter does nothing on it.
//
// Parameters:
//   - t: test to report to
func TestPaletteNoResult(t *testing.T) {
	g := newRig(t, newSandbox(t))
	p := openPalette(g)
	g.typeText("c zzzz")

	if len(p.hits) != 0 || p.selected() != nil {
		t.Fatalf("hits %d", len(p.hits))
	}

	if !strings.Contains(g.text(), `No result for "zzzz"`) {
		t.Errorf("no empty message:\n%s", g.text())
	}

	g.press("enter", "down", "pgdown", "end", "up")

	if len(g.r.overlays) != 1 || p.sel != 0 {
		t.Errorf("overlays %d sel %d", len(g.r.overlays), p.sel)
	}

	g.press("ctrl+u")

	if len(p.hits) != len(p.entries) || strings.Contains(g.text(), "No result") {
		t.Errorf("ctrl+u did not clear the query: %d hits", len(p.hits))
	}

	// An empty scope without a query has its own wording.
	empty := newSandbox(t)
	writeFile(t, empty.Opts.Paths.AliasesFile(), "schema = 1\n")
	h := newRig(t, empty)
	openPalette(h)
	h.typeText("a ")

	if !strings.Contains(h.text(), "Nothing to search yet.") {
		t.Errorf("no empty scope message:\n%s", h.text())
	}
}

// TestPaletteEditing checks the keys that edit the query.
//
// Parameters:
//   - t: test to report to
func TestPaletteEditing(t *testing.T) {
	g := newRig(t, newSandbox(t))
	p := openPalette(g)

	g.typeText("c staging")

	if p.sc != scopeConn || top(p) != "staging" {
		t.Errorf("scope %d top %q", p.sc, top(p))
	}

	if n := len(p.View(g.r.e, 80, 40)); n != paletteHeight {
		t.Errorf("the box is %d lines with one result, want %d", n, paletteHeight)
	}

	g.press("ctrl+w")

	if p.flt.String() != "c " || p.sc != scopeConn || len(p.hits) != 6 {
		t.Errorf("after ctrl+w: %q scope %d hits %d", p.flt.String(), p.sc, len(p.hits))
	}

	g.press("backspace")

	if p.sc != scopeAll || p.flt.String() != "c" {
		t.Errorf("after backspace: %q scope %d", p.flt.String(), p.sc)
	}

	g.press("ctrl+h", "ctrl+h")

	if p.flt.String() != "" {
		t.Errorf("ctrl+h: %q", p.flt.String())
	}

	g.send(tea.PasteMsg{Content: "r pr\noj\x1b[31m"})

	if p.flt.String() != "r proj?[31m" || len(p.hits) != 0 {
		t.Errorf("paste: %q, %d hits", p.flt.String(), len(p.hits))
	}

	g.press("ctrl+u")
	g.send(tea.PasteMsg{Content: "r proj"})

	if top(p) != "proj" {
		t.Errorf("paste: top = %q", top(p))
	}

	g.send(tea.WindowSizeMsg{Width: 90, Height: 30})
	g.send(tea.MouseClickMsg{})

	if len(g.r.overlays) != 1 {
		t.Error("an unrelated message closed the palette")
	}

	if _, _, done := p.Update(g.r.e, dismissMsg{}); !done {
		t.Error("dismissMsg did not close the palette")
	}
}

// TestPaletteNavigation checks the cursor keys and scrolling on a long list.
//
// Parameters:
//   - t: test to report to
func TestPaletteNavigation(t *testing.T) {
	s := newSandbox(t)

	var sb strings.Builder

	sb.WriteString("schema = 1\n")

	for i := range 120 {
		fmt.Fprintf(&sb, "\n[[alias]]\nname = \"al%03d\"\ncmd = \"echo %d\"\n", i, i)
	}

	writeFile(t, s.Opts.Paths.AliasesFile(), sb.String())

	g := newRig(t, s.with(func(o *Options) { o.Width, o.Height = 100, 24 }))
	g.resize(100, 24)
	p := openPalette(g)
	g.typeText("a al")

	rows := rowsFor(22)
	if rows != 12 || len(p.hits) != 120 {
		t.Fatalf("rows %d hits %d", rows, len(p.hits))
	}

	g.press("down")

	if p.sel != 1 || top(p) != "al001" {
		t.Errorf("down: sel %d top %q", p.sel, top(p))
	}

	g.press("up", "up")

	if p.sel != 0 {
		t.Errorf("up stops at the first: sel %d", p.sel)
	}

	g.press("pgdown")

	// Twelve rows hold ten records between the two "N more" lines, so the record under the
	// cursor is the last one of the window when the window starts at 3.
	if p.sel != rows || p.off != 3 {
		t.Errorf("pgdown: sel %d off %d, want %d and 3", p.sel, p.off, rows)
	}

	g.press("end")

	if p.sel != 119 || p.off != maxOffset(rows, 120) {
		t.Errorf("end: sel %d off %d, want %d", p.sel, p.off, maxOffset(rows, 120))
	}

	if !strings.Contains(g.text(), "al119") || strings.Contains(g.text(), "al000") {
		t.Errorf("the last page is not drawn:\n%s", g.text())
	}

	g.press("pgup", "ctrl+p", "ctrl+n")

	if p.sel != 119-rows {
		t.Errorf("pgup/ctrl+p/ctrl+n: sel %d, want %d", p.sel, 119-rows)
	}

	g.press("home")

	if p.sel != 0 || p.off != 0 {
		t.Errorf("home: sel %d off %d", p.sel, p.off)
	}

	// The selected row is always drawn.
	for i := 0; i < 130; i += 7 {
		g.press("down", "down", "down", "down", "down", "down", "down")

		want := fmt.Sprintf("al%03d", min(p.sel, 119))
		if !strings.Contains(g.text(), want) {
			t.Fatalf("row %d (%s) is not on screen:\n%s", p.sel, want, g.text())
		}
	}

	g.press("pgdown", "pgdown", "pgdown", "pgdown", "pgdown", "pgdown", "pgdown", "pgdown",
		"pgdown", "pgdown", "pgdown")

	if p.sel != 119 {
		t.Errorf("pgdown past the end: sel %d", p.sel)
	}
}

// TestPaletteStatus checks that the status line follows the selection: the command of a
// connection, the command line of an alias and the path of a repository.
//
// Parameters:
//   - t: test to report to
func TestPaletteStatus(t *testing.T) {
	s := newSandbox(t)
	g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))
	g.status(func(string) branchMsg { return branchMsg{} })
	openPalette(g)
	g.typeText("c prod")

	if got := g.statusText(); !strings.Contains(got, "ssh") ||
		!strings.Contains(got, "prod.example.com") {
		t.Errorf("connection status = %q", got)
	}

	g.press("ctrl+u")
	g.typeText("a gs")

	if got := g.statusText(); !strings.Contains(got, "git status") {
		t.Errorf("alias status = %q", got)
	}

	g.press("ctrl+u")
	g.typeText("r proj")

	if got := g.statusText(); !showsPath(got, s.Repo) {
		t.Errorf("repository status = %q", got)
	}

	g.press("ctrl+u")
	g.typeText(">quit")

	if got := g.statusText(); strings.TrimSpace(got) != "" {
		t.Errorf("action status = %q", got)
	}
}

// showsPath reports whether a status line shows a path, in full or cut with an ellipsis: the box
// of the palette is narrower than a long temporary directory such as the one of macOS.
//
// Parameters:
//   - status: text of the status line
//   - path: path expected on it
func showsPath(status, path string) bool {
	if strings.Contains(status, path) {
		return true
	}

	head, _, cut := strings.Cut(strings.TrimSpace(status), "\u2026")

	return cut && len(head) >= 20 && strings.HasPrefix(path, head)
}

// TestPaletteView draws the palette at many sizes and in the color modes, with a long query, an
// empty result and wide characters, and applies the rules of checkScreen.
//
// Parameters:
//   - t: test to report to
func TestPaletteView(t *testing.T) {
	modes := []struct {
		name  string
		env   []string
		ascii bool
	}{
		{"truecolor", []string{"TOTE_COLOR=truecolor"}, false},
		{"16", []string{"TOTE_COLOR=16"}, false},
		{"none", []string{"TOTE_COLOR=none"}, false},
		{"ascii", []string{"TOTE_COLOR=none", "LANG=C"}, true},
	}

	queries := []string{
		"", "e", "c e", "> ", "zzz", "c " + strings.Repeat("long", 40), "emoji",
		"\u65e5\u672c",
	}

	base := newSandbox(t)

	for _, m := range modes {
		for _, lang := range []string{"en", "fr"} {
			for _, w := range []int{40, 41, 60, 100} {
				for _, h := range []int{10, 16, 40} {
					for _, q := range queries {
						s := base.with(func(o *Options) {
							o.Width, o.Height = w, h
							o.Printer = newPrinter(lang)
							o.Environ = append(append([]string(nil), o.Environ...), m.env...)
							o.Start = StartConnections

							if m.ascii {
								o.Start = StartAliases
							}
						})
						g := newRig(t, s)
						g.resize(w, h)
						openPalette(g)
						g.send(tea.PasteMsg{Content: q})

						name := fmt.Sprintf("%s/%s %dx%d %q", m.name, lang, w, h, q)
						ascii := m.ascii && lang == "en" && q != "\u65E5\u672C"
						checkScreen(t, name, g.r.render(), w, h, ascii)
					}
				}
			}
		}
	}
}

// TestPaletteTinyBox checks the box when the terminal is smaller than the palette.
//
// Parameters:
//   - t: test to report to
func TestPaletteTinyBox(t *testing.T) {
	g := newRig(t, newSandbox(t))
	p := openPalette(g)

	for _, size := range [][2]int{{1, 1}, {4, 4}, {10, 4}, {20, 6}, {36, 8}, {80, 40}} {
		lines := p.View(g.r.e, size[0], size[1])

		if want := min(size[1], paletteHeight); len(lines) != want && size[1] > 4 {
			t.Errorf("%v: %d lines, want %d", size, len(lines), want)
		}

		if len(lines) > max(1, size[1]) {
			t.Errorf("%v: %d lines", size, len(lines))
		}

		for _, l := range lines {
			if w := g.r.e.Metric.width(l); w > max(size[0], 4) {
				t.Errorf("%v: line of %d cells", size, w)
			}
		}
	}
}

// TestPaletteBig opens the palette on five thousand records and checks that it works.
//
// Parameters:
//   - t: test to report to
func TestPaletteBig(t *testing.T) {
	s := newSandbox(t)

	var sb strings.Builder

	sb.WriteString("schema = 1\n")

	for i := range 5000 {
		fmt.Fprintf(&sb, "\n[[alias]]\nname = \"alias%04d\"\ncmd = \"echo %d\"\n", i, i)
	}

	writeFile(t, s.Opts.Paths.AliasesFile(), sb.String())

	g := newRig(t, s)
	p := openPalette(g)

	if len(p.entries) < 5000 {
		t.Fatalf("%d entries", len(p.entries))
	}

	g.typeText("a alias4999")

	if top(p) != "alias4999" || len(p.hits) != 1 {
		t.Errorf("top %q hits %d", top(p), len(p.hits))
	}

	g.press("enter")

	if g.r.result.Payload != "echo 4999" {
		t.Errorf("result %+v", g.r.result)
	}
}

// TestPaletteKeys checks the bindings of the footer.
//
// Parameters:
//   - t: test to report to
func TestPaletteKeys(t *testing.T) {
	g := newRig(t, newSandbox(t))
	p := openPalette(g)
	e := g.r.e

	enabled := func() int {
		n := 0

		for _, b := range p.Keys(e) {
			if b.Enabled() {
				n++
			}
		}

		return n
	}

	if enabled() != 5 {
		t.Errorf("with a result: %d bindings", enabled())
	}

	g.typeText(">quit")

	if enabled() != 4 {
		t.Errorf("on an action: %d bindings", enabled())
	}

	g.typeText("zz")

	if enabled() != 2 {
		t.Errorf("without a result: %d bindings", enabled())
	}

	if cmd := p.Init(e); cmd != nil {
		t.Error("Init returned a command")
	}
}

// paletteRig opens the palette on a terminal of a given size and types a query.
//
// Parameters:
//   - t: test to report to
//   - w: width in cells
//   - h: height in rows
//   - query: text to type, may be empty
//   - setup: edits the options, may be nil
func paletteRig(t *testing.T, w, h int, query string, setup func(*Options)) *rig {
	t.Helper()

	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Width, o.Height = w, h
		o.Settings.UI.Theme = "dark"

		if setup != nil {
			setup(o)
		}
	}))

	g.press("ctrl+k")
	g.typeText(query)

	return g
}

// TestPaletteLooksLikeTheMockup checks the palette against the mockup: a bold title in the top
// border, the query, a rule that joins the sides, rows that read "Category > name" and the
// actions as plain labels, the footer under the box, and no column of tags.
//
// Parameters:
//   - t: test to report to
func TestPaletteLooksLikeTheMockup(t *testing.T) {
	g := paletteRig(t, 110, 30, "conn", nil)
	lines := strings.Split(g.text(), "\n")

	find := func(sub string) int {
		for i, l := range lines {
			if strings.Contains(l, sub) {
				return i
			}
		}

		t.Fatalf("no line holds %q:\n%s", sub, g.text())

		return -1
	}

	top, query, rule := find("\u256d\u2500 Palette "), find("\u276f conn\u258f"), find("\u251c")
	first, second := find("\u276f Connections \u203a prod"), find("Connections \u203a staging")
	newConn, nav := find("New connection"), find("Go to\u2026 Connections")
	bottom, foot := find("\u2570"), find("\u2191\u2193 choose \u00b7 \u23ce open \u00b7 esc close")

	order := []int{top, query, rule, first, second, newConn, nav, bottom, foot}
	if !slices.IsSorted(order) || len(slices.Compact(slices.Clone(order))) != len(order) {
		t.Errorf("lines are out of order: %v", order)
	}

	if foot != bottom+1 {
		t.Errorf("the footer is on line %d, want %d, right under the box", foot, bottom+1)
	}

	for _, tag := range []string{"action", "conn  ", " alias ", " repo "} {
		if strings.Contains(g.text(), tag) {
			t.Errorf("the palette still shows the tag %q:\n%s", tag, g.text())
		}
	}

	if x := strings.Index(lines[foot], "\u2191"); x != strings.Index(lines[top], "\u256d")+2 {
		t.Errorf("the footer starts at %d, not two cells inside the box", x)
	}
}

// TestPaletteColors checks the accent border, the bold title, the selected row and the muted
// navigation rows at true color.
//
// Parameters:
//   - t: test to report to
func TestPaletteColors(t *testing.T) {
	g := paletteRig(t, 110, 30, "conn", nil)
	lines := g.view()

	var border, selected, navRow string

	for _, l := range lines {
		switch plain := stripStyles(l); {
		case strings.Contains(plain, "\u256d\u2500 Palette"):
			border = l
		case strings.Contains(plain, "\u276f Connections"):
			selected = l
		case strings.Contains(plain, "Go to\u2026 Connections"):
			navRow = l
		}
	}

	accent := "\x1b[38;2;219;39;119m"
	if !strings.Contains(border, accent) {
		t.Errorf("the border is not the accent pink: %q", border)
	}

	if !strings.Contains(border, "\x1b[1;38;2;236;232;240mPalette") {
		t.Errorf("the title is not bold text: %q", border)
	}

	if !strings.Contains(selected, "\x1b[1;38;2;244;217;226;48;2;58;34;48m \u276f Connections") {
		t.Errorf("the selected row has no selection background and bold: %q", selected)
	}

	if strings.Contains(selected, "249;115;22") {
		t.Errorf("the arrow of the selected row has its own color: %q", selected)
	}

	if !strings.Contains(navRow, "\x1b[38;2;139;134;151mGo to") {
		t.Errorf("the navigation row is not muted: %q", navRow)
	}

	if strings.Contains(g.r.render(), "\x1b[38;2;139;134;151m\u256d") {
		t.Error("a muted border corner is drawn, the box should be all accent")
	}
}

// TestPaletteASCII checks the ASCII replacements: the chevron, the tees and the rounded corners.
//
// Parameters:
//   - t: test to report to
func TestPaletteASCII(t *testing.T) {
	g := paletteRig(t, 100, 30, "conn", func(o *Options) {
		o.ASCII = true
		o.Environ = append(o.Environ, "TOTE_COLOR=none")
	})

	text := g.text()

	for _, want := range []string{
		"Connections > prod", "Go to... Connections", "+- Palette ",
		"up/down choose - enter open - esc close",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in the ASCII palette:\n%s", want, text)
		}
	}

	for _, r := range text {
		if r > 0x7e {
			t.Fatalf("non ASCII character %q in the ASCII palette:\n%s", r, text)
		}
	}
}

// TestPaletteDetail checks that the target of a record is shown muted at the right of its row
// when the width allows, flush with the right edge of the rows, and left out when it does not.
//
// Parameters:
//   - t: test to report to
func TestPaletteDetail(t *testing.T) {
	wide := paletteRig(t, 110, 30, "c prod", nil)

	var row string

	for _, l := range strings.Split(wide.text(), "\n") {
		if strings.Contains(l, "Connections \u203a prod") {
			row = l
		}
	}

	trimmed := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(row, " "), "\u2502"), " ")
	if !strings.HasSuffix(trimmed, "prod.example.com") {
		t.Errorf("the row does not end with the target: %q", row)
	}

	if !strings.Contains(wide.r.render(), "\x1b[38;2;139;134;151m") {
		t.Error("the detail is not muted")
	}

	narrow := paletteRig(t, 40, 20, "c bast", nil)

	for _, l := range strings.Split(narrow.text(), "\n") {
		if strings.Contains(l, "\u203a bastion") && strings.Contains(l, "bastion.") {
			t.Errorf("a 40 column palette still shows the target: %q", l)
		}
	}

	if !strings.Contains(narrow.text(), "Connections \u203a bastion") {
		t.Errorf("the narrow palette lost the label:\n%s", narrow.text())
	}
}

// TestPaletteFloatsAlone checks that nothing of the screen below is drawn while the palette is
// open, whatever the screen: the welcome page, a list, and a terminal wider than the page.
//
// Parameters:
//   - t: test to report to
func TestPaletteFloatsAlone(t *testing.T) {
	for _, start := range []Start{
		StartHome, StartConnections, StartRepos, StartAliases,
		StartFuncs, StartConfig,
	} {
		for _, size := range [][2]int{{80, 24}, {110, 30}, {160, 40}} {
			for _, width := range []string{"auto", "120"} {
				g := newRig(t, newSandbox(t).with(func(o *Options) {
					o.Start = start
					o.Width, o.Height = size[0], size[1]
					o.Settings.UI.Width = width
				}))

				page := stripStyles(g.r.render())
				g.press("ctrl+k")

				assertFloating(t, g, page)
			}
		}
	}
}
