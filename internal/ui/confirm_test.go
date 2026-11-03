package ui

import (
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// openDelete returns a rig on the list of a kind with the confirmation for the selected record
// open.
//
// Parameters:
//   - t: running test
//   - s: sandbox giving the options
//   - start: list to start on
//   - keys: keys that put the cursor on the record before d is pressed
func openDelete(t *testing.T, s *sandbox, start Start, keys ...string) *rig {
	t.Helper()

	g := newRig(t, s.with(func(o *Options) { o.Start = start }))
	g.press(keys...)
	g.press("d")

	if _, ok := g.top().(*confirm); !ok {
		t.Fatalf("d did not open a confirmation: %T", g.top())
	}

	return g
}

// TestConfirmDelete deletes one record of each kind with each way of saying yes.
//
// Parameters:
//   - t: test to report to
func TestConfirmDelete(t *testing.T) {
	tests := []struct {
		name  string
		start Start
		keys  []string
		yes   []string
		key   string
		gone  func(s *sandbox) bool
	}{
		{"connection y", StartConnections, nil, []string{"y"}, "prod", func(s *sandbox) bool {
			_, ok := mustConns(t, s).Get("prod")

			return !ok
		}},
		{
			"connection capital Y", StartConnections,
			[]string{"down"},
			[]string{"Y"},
			"staging",
			func(s *sandbox) bool {
				_, ok := mustConns(t, s).Get("staging")

				return !ok
			},
		},
		{
			"repository right enter", StartRepos, nil,
			[]string{"right", "enter"},
			"proj",
			func(s *sandbox) bool {
				c, _ := store.OpenRepos(s.Opts.Paths.ReposFile())
				_, ok := c.Get("proj")

				return !ok
			},
		},
		{
			"alias tab enter", StartAliases,
			[]string{"down"},
			[]string{"tab", "enter"},
			"gs",
			func(s *sandbox) bool {
				c, _ := store.OpenAliases(s.Opts.Paths.AliasesFile())
				_, ok := c.Get("gs")

				return !ok
			},
		},
		{
			"function l enter", StartFuncs, nil,
			[]string{"l", "enter"},
			"hello",
			func(s *sandbox) bool {
				c, _ := store.OpenFuncs(s.Opts.Paths.FuncsFile())
				_, ok := c.Get("hello")

				return !ok
			},
		},
	}

	for _, tt := range tests {
		s := newSandbox(t)
		g := openDelete(t, s, tt.start, tt.keys...)

		if !strings.Contains(g.text(), tt.key) {
			t.Errorf("%s: the name is not asked about:\n%s", tt.name, g.text())
		}

		g.press(tt.yes...)

		if len(g.r.overlays) != 0 || !tt.gone(s) {
			t.Errorf("%s: overlays %d, gone %v", tt.name, len(g.r.overlays), tt.gone(s))
		}

		if !strings.Contains(g.toastText(), "removed") || g.r.toast.err {
			t.Errorf("%s: toast %q", tt.name, g.toastText())
		}

		if g.selectedKey() == tt.key {
			t.Errorf("%s: the deleted record is still selected", tt.name)
		}
	}
}

// TestConfirmKeepsTheRecord checks every way of answering no, and that the default answer is no.
//
// Parameters:
//   - t: test to report to
func TestConfirmKeepsTheRecord(t *testing.T) {
	answers := [][]string{
		{"enter"},
		{"n"},
		{"N"},
		{"esc"},
		{"right", "left", "enter"},
		{"right", "right", "enter"},
		{"right", "esc"},
		{"right", "n"},
		{"x", "q", "space", "up", "down", "enter"},
	}

	for _, keys := range answers {
		s := newSandbox(t)
		before := readFile(t, s.Opts.Paths.AliasesFile())

		g := openDelete(t, s, StartAliases)

		if c := g.top().(*confirm); c.yes {
			t.Fatal("the default answer is yes")
		}

		g.press(keys...)

		if len(g.r.overlays) != 0 || g.Quit {
			t.Errorf("%v: overlays %d quit %v", keys, len(g.r.overlays), g.Quit)
		}

		if readFile(t, s.Opts.Paths.AliasesFile()) != before {
			t.Errorf("%v: the file changed", keys)
		}

		if strings.Contains(g.toastText(), "removed") {
			t.Errorf("%v: toast %q", keys, g.toastText())
		}
	}

	s := newSandbox(t)
	g := openDelete(t, s, StartAliases)
	g.press("x", "up", "space")

	if len(g.r.overlays) != 1 {
		t.Error("a stray key closed the confirmation")
	}
}

// TestConfirmStaleRecord checks the answer yes for a record that vanished meanwhile.
//
// Parameters:
//   - t: test to report to
func TestConfirmStaleRecord(t *testing.T) {
	s := newSandbox(t)
	g := openDelete(t, s, StartAliases)

	writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n")
	g.r.e.Data.reloadKind(kindAlias)
	g.press("y")

	if len(g.r.overlays) != 0 || !g.r.toast.err || !strings.Contains(g.toastText(), "not found") {
		t.Errorf("overlays %d toast %q err %v", len(g.r.overlays), g.toastText(), g.r.toast.err)
	}

	g = newRig(t, s)
	g.send(openOverlayMsg{Overlay: newConfirm(kindAlias, "ghost")})

	if !strings.Contains(g.text(), "ghost") {
		t.Errorf("the name of an unknown record is not shown:\n%s", g.text())
	}

	g.press("y")

	if !g.r.toast.err {
		t.Error("deleting an unknown record did not fail")
	}
}

// TestConfirmDismiss checks that the overlay closes on a dismissal message.
//
// Parameters:
//   - t: test to report to
func TestConfirmDismiss(t *testing.T) {
	g := openDelete(t, newSandbox(t), StartAliases)
	g.send(dismissMsg{})

	if len(g.r.overlays) != 0 {
		t.Error("dismissMsg did not close the confirmation")
	}
}

// TestConfirmView draws the confirmation at many sizes, in both languages and every color mode.
//
// Parameters:
//   - t: test to report to
func TestConfirmView(t *testing.T) {
	modes := []struct {
		env   []string
		ascii bool
	}{
		{[]string{"TOTE_COLOR=truecolor"}, false},
		{[]string{"TOTE_COLOR=16"}, false},
		{[]string{"NO_COLOR=1", "TOTE_COLOR="}, false},
		{[]string{"TOTE_COLOR=none", "LANG=C"}, true},
	}

	base := newSandbox(t)
	plain := newSandbox(t)

	cut := strings.Index(fixtureConns, "[[connection]]\nname = \"emoji\"")
	writeFile(t, plain.Opts.Paths.ConnectionsFile(), fixtureConns[:cut])

	for _, m := range modes {
		src := base
		if m.ascii {
			src = plain
		}

		for _, lang := range []string{"en", "fr"} {
			if m.ascii && lang == "fr" {
				continue
			}

			for _, start := range []Start{StartConnections, StartRepos, StartAliases, StartFuncs} {
				for _, w := range []int{40, 60, 100} {
					for _, h := range []int{10, 24} {
						s := src.with(func(o *Options) {
							o.Start = start
							o.Width, o.Height = w, h
							o.Printer = newPrinter(lang)
							o.Environ = append(append([]string(nil), o.Environ...), m.env...)
						})

						g := newRig(t, s)
						g.press("d")
						checkScreen(t, lang+"/confirm", g.r.render(), w, h, m.ascii)

						g.press("right")
						checkScreen(t, lang+"/confirm right", g.r.render(), w, h, m.ascii)
					}
				}
			}
		}
	}
}

// TestConfirmContent checks the text of the question and the details, in both languages.
//
// Parameters:
//   - t: test to report to
func TestConfirmContent(t *testing.T) {
	tests := []struct {
		lang  string
		start Start
		want  []string
	}{
		{"en", StartConnections, []string{"Delete the connection \"prod\"?", "prod.example.com"}},
		{"en", StartRepos, []string{"Delete the repository \"proj\"?", "[ No ]", "[ Yes ]"}},
		{"en", StartAliases, []string{"Delete the alias \"ll\"?", "ls -la"}},
		{"en", StartFuncs, []string{
			"Delete the function \"hello\"?", "~/bin/hello.sh",
			"The script file is kept.",
		}},
		{"fr", StartConnections, []string{
			"Supprimer la connexion \"prod\"\u00A0?", "[ Non ]",
			"[ Oui ]",
		}},
		{"fr", StartRepos, []string{"Supprimer le d\u00E9p\u00F4t \"proj\"\u00A0?"}},
		{"fr", StartAliases, []string{"Supprimer l\u2019alias \"ll\"\u00A0?"}},
		{"fr", StartFuncs, []string{
			"Supprimer la fonction \"hello\"\u00A0?",
			"Le fichier du script est conserv\u00E9.",
		}},
	}

	for _, tt := range tests {
		s := newSandbox(t).with(func(o *Options) { o.Printer = newPrinter(tt.lang) })
		g := openDelete(t, s, tt.start)

		for _, want := range tt.want {
			if !strings.Contains(g.text(), want) {
				t.Errorf("%s %d: %q is missing:\n%s", tt.lang, tt.start, want, g.text())
			}
		}

		var keys []string

		for _, b := range g.top().Keys(g.r.e) {
			if b.Help().Key == "" || b.Help().Desc == "" || strings.HasPrefix(b.Help().Desc, "!") {
				t.Errorf("%s: binding %+v", tt.lang, b.Help())
			}

			keys = append(keys, b.Help().Key)
		}

		if len(keys) != 4 || keys[0] != "y" || keys[1] != "n" {
			t.Errorf("%s: keys %v", tt.lang, keys)
		}
	}
}

// TestConfirmReadOnly checks that a read-only file cannot be deleted from, even when the
// confirmation is open.
//
// Parameters:
//   - t: test to report to
func TestConfirmReadOnly(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n[[alias\n")

	g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))
	g.send(openOverlayMsg{Overlay: newConfirm(kindAlias, "ll")})
	g.press("y")

	if len(g.r.overlays) != 0 || !g.r.toast.err ||
		!strings.Contains(g.toastText(), "syntax error") {
		t.Errorf("overlays %d toast %q", len(g.r.overlays), g.toastText())
	}

	if readFile(t, s.Opts.Paths.AliasesFile()) != "schema = 1\n[[alias\n" {
		t.Error("the broken file was written")
	}
}

// TestConfirmGoneRecordRefreshesList checks that a deletion refused because the record was
// removed elsewhere also updates the list, which no longer shows it.
//
// Parameters:
//   - t: test to report to
func TestConfirmGoneRecordRefreshesList(t *testing.T) {
	s := newSandbox(t)
	g := openDelete(t, s, StartAliases)

	name := g.r.e.Data.items(kindAlias)[0].Cells[0]
	if !strings.Contains(g.text(), name) {
		t.Fatalf("the alias %q is not listed:\n%s", name, g.text())
	}

	writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n")
	g.press("y")

	if !g.r.toast.err || strings.Contains(g.text(), name+" ") {
		t.Errorf("toast %q err %v, list:\n%s", g.toastText(), g.r.toast.err, g.text())
	}
}
