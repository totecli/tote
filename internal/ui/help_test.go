package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

// TestHelpOpensEverywhere checks that "?" and F1 open the help from every screen, that "?" and
// esc close it, and that the screen below is untouched.
//
// Parameters:
//   - t: test to report to
func TestHelpOpensEverywhere(t *testing.T) {
	for _, start := range []Start{
		StartHome, StartConnections, StartRepos, StartAliases, StartFuncs,
		StartConfig,
	} {
		for _, open := range []string{"?", "f1"} {
			for _, shut := range []string{"?", "esc", "f1"} {
				g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = start }))
				scr := g.r.scr

				g.press(open)

				if _, ok := g.top().(*helpOverlay); !ok {
					t.Errorf("start %d: %s opened %T", start, open, g.top())
					continue
				}

				g.press(shut)

				if g.top() != nil {
					t.Errorf("start %d: %s then %s left %T", start, open, shut, g.top())
				}

				if g.r.scr != scr || g.Quit {
					t.Errorf("start %d: the help changed the screen below", start)
				}
			}
		}
	}
}

// TestHelpOverOverlays checks that F1 opens the help over a form, a prompt, the filter and the
// palette, that "?" is a character there, and that closing the help gives the field back.
//
// Parameters:
//   - t: test to report to
func TestHelpOverOverlays(t *testing.T) {
	tests := []struct {
		name  string
		start Start
		keys  []string
		topic string
		typed func(g *rig) string
	}{
		{
			"form", StartConnections,
			[]string{"n"},
			"form",
			func(g *rig) string { return g.frm().fields[0].text() },
		},
		{
			"prompt", StartConfig,
			[]string{"down", "enter"},
			"prompt",
			func(g *rig) string { return g.top().(*promptOverlay).field.text() },
		},
		{
			"filter", StartConnections,
			[]string{"/"},
			"filter",
			func(g *rig) string { return g.r.scr.(*listScreen).flt.String() },
		},
		{
			"palette", StartConnections,
			[]string{"ctrl+k"},
			"palette",
			func(g *rig) string { return g.top().(*paletteOverlay).flt.String() },
		},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = tt.start }))
		g.press(tt.keys...)
		g.typeText("a?")

		if _, ok := g.top().(*helpOverlay); ok {
			t.Errorf("%s: \"?\" opened the help instead of being typed", tt.name)
		}

		if got := tt.typed(g); !strings.HasSuffix(got, "a?") {
			t.Errorf("%s: the field holds %q after typing a?", tt.name, got)
		}

		g.press("f1")

		h, ok := g.top().(*helpOverlay)
		if !ok {
			t.Errorf("%s: F1 opened %T", tt.name, g.top())
			continue
		}

		if h.topic != tt.topic {
			t.Errorf("%s: help topic = %q, want %q", tt.name, h.topic, tt.topic)
		}

		g.press("?")

		if _, still := g.top().(*helpOverlay); still {
			t.Errorf("%s: \"?\" did not close the help", tt.name)
		}

		if got := tt.typed(g); !strings.HasSuffix(got, "a?") {
			t.Errorf("%s: closing the help lost the text: %q", tt.name, got)
		}
	}
}

// TestHelpKeysDoNotReachTheScreen checks that while the help is open the keys of the screen below
// do nothing: n does not open a form, q does not quit, a digit does not open a list.
//
// Parameters:
//   - t: test to report to
func TestHelpKeysDoNotReachTheScreen(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	g.press("?", "n", "q", "d", "ctrl+r", "1", "enter", ",")

	if _, ok := g.top().(*helpOverlay); !ok || len(g.r.overlays) != 1 || g.Quit {
		t.Errorf("overlays %d, top %T, quit %v", len(g.r.overlays), g.top(), g.Quit)
	}

	if _, ok := g.r.scr.(*listScreen); !ok {
		t.Errorf("the screen below changed to %T", g.r.scr)
	}
}

// TestHelpTopicFirst checks that the section of the screen the help was opened from comes first.
//
// Parameters:
//   - t: test to report to
func TestHelpTopicFirst(t *testing.T) {
	tests := []struct {
		start Start
		keys  []string
		first string
		later string
	}{
		{StartConnections, nil, "Connections", "Welcome page"},
		{StartRepos, nil, "Repos", "Welcome page"},
		{StartAliases, nil, "Aliases", "Settings"},
		{StartHome, nil, "Welcome page", "Connections"},
		{StartConfig, nil, "Settings", "Welcome page"},
		{StartConnections, []string{"n"}, "Forms", "Welcome page"},
		{StartConnections, []string{"d"}, "Confirmation", "Welcome page"},
		{StartConnections, []string{"v"}, "Details of a record", "Welcome page"},
		{StartConnections, []string{"U"}, "Choosing a value", "Welcome page"},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = tt.start
			o.Height = 200
		}))
		g.press(tt.keys...)
		g.press("f1")

		text := g.text()
		text = text[strings.Index(text, "\u2500 Help \u2500"):]
		first, later := strings.Index(text, tt.first), strings.Index(text, tt.later)

		if first < 0 || later < 0 || first > later {
			t.Errorf("start %d keys %v: %q at %d, %q at %d:\n%s", tt.start, tt.keys, tt.first,
				first, tt.later, later, text)
		}
	}
}

// TestHelpScrolls checks that a help taller than the terminal scrolls with every scroll key,
// that the offset stays in range and that each line can be reached.
//
// Parameters:
//   - t: test to report to
func TestHelpScrolls(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.Width, o.Height = 60, 12
	}))
	g.press("?")

	h := g.top().(*helpOverlay)
	g.r.render()

	if h.total <= h.rows {
		t.Fatalf("the help fits in %d rows (%d lines): nothing to scroll", h.rows, h.total)
	}

	first := g.text()

	g.press("down", "down")

	if h.off != 2 {
		t.Errorf("two downs: off = %d", h.off)
	}

	g.press("up", "up", "up")

	if h.off != 0 {
		t.Errorf("up past the top: off = %d", h.off)
	}

	g.press("pgdown")

	if want := pageOf(0, h.rows, h.total); h.off != want {
		t.Errorf("pgdown: off = %d, want %d", h.off, want)
	}

	g.press("end")
	g.r.render()

	last := maxOffset(h.rows, h.total)
	if h.off != last {
		t.Errorf("end: off = %d, want %d", h.off, last)
	}

	g.press("down", "pgdown")

	if h.off != last {
		t.Errorf("past the end: off = %d", h.off)
	}

	if g.text() == first {
		t.Error("the end of the help looks like its start")
	}

	g.press("home")

	if h.off != 0 || g.text() != first {
		t.Errorf("home: off = %d", h.off)
	}

	// Page by page, every description of every binding of every section shows up.
	seen := map[string]bool{}

	for range h.total {
		for _, l := range strings.Split(g.text(), "\n") {
			seen[strings.TrimSpace(l)] = true
		}

		g.press("pgdown")
	}

	var all strings.Builder
	for l := range seen {
		all.WriteString(l + "\n")
	}

	for _, sec := range helpSections(g.r.e) {
		for _, b := range sec.bindings {
			if b.Help().Key != "" && !strings.Contains(all.String(), b.Help().Desc) {
				t.Errorf("section %s: %q never shows while scrolling", sec.id, b.Help().Desc)
			}
		}
	}
}

// TestHelpContent checks the quick start, the note on the uninstall and the sections.
//
// Parameters:
//   - t: test to report to
func TestHelpContent(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Height = 200
			o.Printer = newPrinter(lang)
		}))
		g.press("?")

		text := g.text()
		e := g.r.e

		for _, want := range []string{
			e.t("ui_help_title"), e.t("ui_help_sec_quick"),
			e.t("ui_help_sec_mouse"), e.t("ui_sel_hint"), "ui.mouse", "ctrl+k", "tote uninstall",
			"tote config path",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the help lacks %q:\n%s", lang, want, text)
			}
		}

		bullets := 0

		for _, l := range strings.Split(text, "\n") {
			if strings.Contains(l, "\u2502 "+e.Glyphs.sep+" ") {
				bullets++
			}
		}

		if bullets != 6 {
			t.Errorf("%s: quick start and mouse have %d bullets, want 3 and 3:\n%s", lang, bullets,
				text)
		}
	}
}

// TestHelpSectionsAreGenerated checks that a change of the key map shows in the help: relabel a
// binding and the section follows, since nothing is written by hand.
//
// Parameters:
//   - t: test to report to
func TestHelpSectionsAreGenerated(t *testing.T) {
	e := newRig(t, newSandbox(t)).r.e
	e.Keys.Copy = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yank it"))

	found := false

	for _, sec := range helpSections(e) {
		for _, b := range sec.bindings {
			if b.Help().Desc == "yank it" {
				found = sec.id == "list"
			}
		}
	}

	if !found {
		t.Error("a relabelled binding does not reach the help")
	}

	ids := map[string]bool{}

	for _, sec := range helpSections(e) {
		if ids[sec.id] {
			t.Errorf("section %q appears twice", sec.id)
		}

		ids[sec.id] = true

		if e.t(sec.title) == sec.title {
			t.Errorf("section %q has no title in the catalog", sec.id)
		}
	}
}

// TestHelpGlobalKeysOnce checks that a key of the global section is not repeated in the others.
//
// Parameters:
//   - t: test to report to
func TestHelpGlobalKeysOnce(t *testing.T) {
	e := newRig(t, newSandbox(t)).r.e
	secs := helpSections(e)

	global := map[string]bool{}
	for _, b := range secs[0].bindings {
		global[fmt.Sprint(b.Keys())] = true
	}

	for _, sec := range secs[1:] {
		for _, b := range sec.bindings {
			if global[fmt.Sprint(b.Keys())] {
				t.Errorf("section %s repeats the global key %q", sec.id, b.Help().Key)
			}
		}
	}
}

// TestPaletteOpensScreens checks the palette entries that open the settings, the help and the check
// for an update.
//
// Parameters:
//   - t: test to report to
func TestPaletteOpensScreens(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		checkWith(UpdateCheck{Status: "up-to-date", Current: "1.2.3"}, nil)(o)
	}))

	g.press("ctrl+k")
	g.typeText("settings")
	g.press("enter")

	if _, ok := g.r.scr.(*configScreen); !ok || g.top() != nil {
		t.Errorf("the settings entry led to %T with %T open", g.r.scr, g.top())
	}

	g.press("ctrl+k")
	g.typeText("help")
	g.press("enter")

	h, ok := g.top().(*helpOverlay)
	if !ok || h.topic != "config" {
		t.Errorf("the help entry opened %T", g.top())
	}

	g.press("esc", "ctrl+k")
	g.typeText("update")
	g.press("enter")
	g.await("the check", func() bool { return strings.Contains(g.toastText(), "up to date") })
}
