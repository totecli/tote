package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

// surface is one place where keys are read: a screen or an overlay in a given state.
//
// Name describes it, Keys are the bindings it hands to the footer, E the environment they were
// built for and Build makes a fresh rig in that state at a size.
type surface struct {
	name  string
	keys  []key.Binding
	e     *env
	build func(w, h int) *rig
}

// surfaces builds every screen and overlay of the interface in the states that change their key
// bindings, in a language and with a glyph set.
//
// Parameters:
//   - t: running test
//   - lang: language of the interface
//   - ascii: true for the ASCII glyph set
func surfaces(t *testing.T, lang string, ascii bool) []surface {
	t.Helper()

	cases := []struct {
		name  string
		start Start
		keys  []string
		data  func(*sandbox)
	}{
		{"home", StartHome, nil, nil},
		{"connections", StartConnections, nil, nil},
		{"repositories", StartRepos, nil, nil},
		{"aliases", StartAliases, nil, nil},
		{"functions", StartFuncs, nil, nil},
		{"connections read-only", StartConnections, nil, func(s *sandbox) {
			writeFile(t, s.Opts.Paths.ConnectionsFile(), "schema = 1\n\n[[connection\nname = \"x\n")
		}},
		{"filter", StartConnections, []string{"/"}, nil},
		{"config first", StartConfig, nil, nil},
		{"config choice", StartConfig, []string{"down", "down", "down"}, nil},
		{"config action", StartConfig, []string{"end"}, nil},
		{"config info", StartConfig, []string{"end", "up"}, nil},
		{"config read-only", StartConfig, nil, func(s *sandbox) {
			writeFile(t, s.Opts.Paths.ConfigFile(), "schema = 1\nlang = \"fr\n")
		}},
		{"form", StartConnections, []string{"n"}, nil},
		{"confirm", StartConnections, []string{"d"}, nil},
		{"palette", StartConnections, []string{"ctrl+k"}, nil},
		{"info", StartConnections, []string{"v"}, nil},
		{"pick", StartConnections, []string{"U"}, nil},
		{"setting pick", StartConfig, []string{"enter"}, nil},
		{"prompt", StartConfig, []string{"down", "enter"}, nil},
		{"help", StartHome, []string{"?"}, nil},
	}

	plug := func(s *sandbox) { newPlugLab(t, s).fixtures() }
	downs := func(n int, rest ...string) []string {
		return append(slices.Repeat([]string{"down"}, n), rest...)
	}

	cases = append(cases, []struct {
		name  string
		start Start
		keys  []string
		data  func(*sandbox)
	}{
		{"plugins enabled", StartPlugins, nil, plug},
		{"plugins available", StartPlugins, downs(1), plug},
		{"plugins needs consent", StartPlugins, downs(5), plug},
		{"plugins broken", StartPlugins, downs(6), plug},
		{"plugins filter", StartPlugins, []string{"/"}, plug},
		{"plugin details", StartPlugins, []string{"enter"}, plug},
		{"plugin menu", StartPlugins, []string{"tab"}, plug},
		{"plugin remove", StartPlugins, []string{"d"}, plug},
		{"plugin check", StartPlugins, downs(4, "c"), plug},
		{"plugin consent", StartPlugins, downs(5, "a"), plug},
		{"plugin settings", StartPlugins, []string{"s"}, plug},
		{"plugin list editor", StartPlugins, append([]string{"s"}, downs(4, "enter")...), plug},
	}...)

	var out []surface

	for _, c := range cases {
		build := func(w, h int) *rig {
			s := newSandbox(t)

			if c.data != nil {
				c.data(s)
			}

			s = s.with(func(o *Options) {
				o.Start, o.Printer, o.ASCII = c.start, newPrinter(lang), ascii
				o.Width, o.Height = w, h
			})

			g := newRig(t, s)
			g.press(c.keys...)

			return g
		}

		g := build(200, 50)

		var bs []key.Binding

		if top := g.top(); top != nil {
			bs = top.Keys(g.r.e)
		} else {
			bs = g.r.scr.Keys(g.r.e)
		}

		out = append(out, surface{name: c.name, keys: bs, e: g.r.e, build: build})
	}

	return out
}

// shown returns the bindings of a surface that the footer lists: enabled and with a key label.
//
// Parameters:
//   - bs: bindings of a surface
func shown(bs []key.Binding) []key.Binding {
	var out []key.Binding

	for _, b := range bs {
		if b.Enabled() && b.Help().Key != "" {
			out = append(out, b)
		}
	}

	return out
}

// TestKeymapNoDuplicates checks that no two bindings of one screen or overlay answer to the same
// key, in either language and glyph set, and that none of them takes a key that is global.
//
// Parameters:
//   - t: test to report to
func TestKeymapNoDuplicates(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		for _, s := range surfaces(t, lang, false) {
			owner := map[string]string{}
			global := s.e.Keys

			for _, b := range shown(s.keys) {
				for _, k := range b.Keys() {
					if prev, dup := owner[k]; dup {
						t.Errorf("%s %s: key %q is bound to %q and to %q", lang, s.name, k, prev,
							b.Help().Desc)
					}

					owner[k] = b.Help().Desc
				}
			}

			if strings.HasPrefix(s.name, "form") || s.name == "prompt" || s.name == "filter" ||
				s.name == "palette" || s.name == "help" {
				continue
			}

			for _, g := range []key.Binding{global.Palette, global.Help} {
				for _, k := range g.Keys() {
					for _, b := range shown(s.keys) {
						if slices.Contains(b.Keys(), k) && !slices.Equal(b.Keys(), g.Keys()) {
							t.Errorf("%s %s: %q (%s) is also the global key %q", lang, s.name, k,
								b.Help().Desc, g.Help().Desc)
						}
					}
				}
			}
		}
	}
}

// TestKeymapBoundKeysAreUnique checks the key map itself: two bindings that have a help text
// never share a key unless they are the same action meant for different screens, which is the
// case of the pairs listed here.
//
// Parameters:
//   - t: test to report to
func TestKeymapBoundKeysAreUnique(t *testing.T) {
	e := newRig(t, newSandbox(t)).r.e

	byKey := map[string][]string{}

	for name, b := range map[string]key.Binding{
		"Nav": e.Keys.Nav, "Open": e.Keys.Open, "Back": e.Keys.Back, "Quit": e.Keys.Quit,
		"Filter": e.Keys.Filter, "Palette": e.Keys.Palette, "New": e.Keys.New, "Edit": e.Keys.Edit,
		"Delete": e.Keys.Delete, "Editor": e.Keys.Editor, "Tabs": e.Keys.Tabs,
		"Reload": e.Keys.Reload, "Help": e.Keys.Help, "Settings": e.Keys.Settings,
		"Jump": e.Keys.Jump, "Notice": e.Keys.Notice, "Check": e.Keys.Check,
		"User": e.Keys.User, "Pick": e.Keys.Pick, "Command": e.Keys.Command, "Copy": e.Keys.Copy,
		"View": e.Keys.View, "Change": e.Keys.Change, "Default": e.Keys.Default,
	} {
		for _, k := range b.Keys() {
			byKey[k] = append(byKey[k], name)
		}
	}

	allowed := map[string]bool{
		"Check+Pick": true, "Notice+User": true, "Default+Delete": true, "Change+Open": true,
		"Nav+Scroll": true, "Back+Close": true,
	}

	for k, names := range byKey {
		if len(names) < 2 {
			continue
		}

		slices.Sort(names)

		if !allowed[strings.Join(names, "+")] {
			t.Errorf("key %q is bound to %v", k, names)
		}
	}
}

// TestKeymapInHelp checks that every binding a screen or overlay offers is described by the
// help, with the same key label and keys, so that the help cannot fall behind the footer.
//
// Parameters:
//   - t: test to report to
func TestKeymapInHelp(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		for _, ascii := range []bool{false, true} {
			for _, s := range surfaces(t, lang, ascii) {
				secs := helpSections(s.e)

				for _, b := range shown(s.keys) {
					found := false

					for _, sec := range secs {
						for _, h := range sec.bindings {
							if h.Help().Key == b.Help().Key && slices.Equal(h.Keys(), b.Keys()) {
								found = true
							}
						}
					}

					if !found {
						t.Errorf("%s ascii=%v %s: %q (%s) is not in the help", lang, ascii, s.name,
							b.Help().Key, b.Help().Desc)
					}
				}
			}
		}
	}
}

// TestKeymapInFooter checks that on a wide terminal the footer of every screen and overlay lists
// each of its bindings with its key and its description.
//
// Parameters:
//   - t: test to report to
func TestKeymapInFooter(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		for _, ascii := range []bool{false, true} {
			for _, s := range surfaces(t, lang, ascii) {
				line := stripStyles(footerLine(s.e, s.keys, false, 400))

				for _, b := range shown(s.keys) {
					h := b.Help()

					if !strings.Contains(line, h.Key+" "+h.Desc) {
						t.Errorf("%s ascii=%v %s: the footer lacks %q:\n%s", lang, ascii, s.name,
							h.Key+" "+h.Desc, line)
					}
				}
			}
		}
	}
}

// TestKeymapFooterLeastKept checks that when the footer is cut the keys that leave a way out
// (back or close, and quit) are the ones that stay, on every surface at 80 columns.
//
// Parameters:
//   - t: test to report to
func TestKeymapFooterLeastKept(t *testing.T) {
	for _, s := range surfaces(t, "en", false) {
		if s.name == "help" || s.name == "prompt" || s.name == "palette" || s.name == "form" ||
			s.name == "confirm" || s.name == "plugin remove" {
			continue
		}

		line := stripStyles(footerLine(s.e, s.keys, false, 80))

		if !strings.Contains(line, "esc") && !strings.Contains(line, "q") {
			t.Errorf("%s: no way out in the footer at 80 columns: %q", s.name, line)
		}
	}
}

// TestKeymapSmoke presses every key of every surface at several sizes: nothing may panic or
// leave a frame of the wrong size.
//
// Parameters:
//   - t: test to report to
func TestKeymapSmoke(t *testing.T) {
	for _, size := range [][2]int{{60, 12}, {80, 24}, {120, 40}} {
		for _, s := range surfaces(t, "en", false) {
			for _, b := range s.keys {
				for _, k := range b.Keys() {
					name := fmt.Sprintf("%s %q %dx%d", s.name, k, size[0], size[1])

					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Errorf("%s: panic %v", name, r)
							}
						}()

						g := s.build(size[0], size[1])
						g.press(k)
						checkScreen(t, name, g.r.render(), size[0], size[1], false)
					}()
				}
			}
		}
	}
}
