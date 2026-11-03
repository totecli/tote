package ui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/store"
)

// play delivers a script to the model. A part that starts with "@" lists key names separated by
// commas and is pressed; any other part is typed one character at a time.
//
// Parameters:
//   - parts: text to type and keys to press, in order
func (g *rig) play(parts ...string) {
	g.t.Helper()

	for _, p := range parts {
		if keys, ok := strings.CutPrefix(p, "@"); ok {
			for _, k := range strings.Split(keys, ",") {
				if k == "shift+tab" {
					g.send(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
					continue
				}

				g.press(k)
			}

			continue
		}

		g.typeText(p)
	}
}

// openNew returns a rig on the list of a kind with the form of a new record open.
//
// Parameters:
//   - t: running test
//   - s: sandbox giving the options
//   - start: list to start on
func openNew(t *testing.T, s *sandbox, start Start) *rig {
	t.Helper()

	g := newRig(t, s.with(func(o *Options) { o.Start = start }))
	g.press("n")

	if _, ok := g.top().(*form); !ok {
		t.Fatalf("n did not open a form: %T", g.top())
	}

	return g
}

// openEdit returns a rig on the list of a kind with the form of the selected record open.
//
// Parameters:
//   - t: running test
//   - s: sandbox giving the options
//   - start: list to start on
//   - keys: keys that put the cursor on the record before e is pressed
func openEdit(t *testing.T, s *sandbox, start Start, keys ...string) *rig {
	t.Helper()

	g := newRig(t, s.with(func(o *Options) { o.Start = start }))
	g.press(keys...)
	g.press("e")

	if _, ok := g.top().(*form); !ok {
		t.Fatalf("e did not open a form: %T", g.top())
	}

	return g
}

// top returns the overlay on top of the stack, nil when there is none.
func (g *rig) top() overlay {
	if n := len(g.r.overlays); n > 0 {
		return g.r.overlays[n-1]
	}

	return nil
}

// frm returns the form on top of the stack.
func (g *rig) frm() *form {
	g.t.Helper()

	f, ok := g.top().(*form)
	if !ok {
		g.t.Fatalf("no form on top: %T", g.top())
	}

	return f
}

// selectedKey returns the key of the selected record of the list.
func (g *rig) selectedKey() string {
	l, ok := g.r.scr.(*listScreen)
	if !ok || l.selected() == nil {
		return ""
	}

	return l.selected().Key
}

// mustConns opens the connections of a sandbox.
//
// Parameters:
//   - t: running test
//   - s: sandbox giving the paths
func mustConns(t *testing.T, s *sandbox) *store.Collection[store.Connection] {
	t.Helper()

	c, err := store.OpenConnections(s.Opts.Paths.ConnectionsFile())
	if err != nil {
		t.Fatal(err)
	}

	return c
}

// readFile returns the content of a file, failing the test when it cannot be read.
//
// Parameters:
//   - t: running test
//   - path: file to read
func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// TestFormAdd adds one record of each kind through the keyboard and checks the file, the
// selection and the message.
//
// Parameters:
//   - t: test to report to
func TestFormAdd(t *testing.T) {
	tests := []struct {
		name  string
		start Start
		play  []string
		key   string
		check func(t *testing.T, s *sandbox)
	}{
		{
			name: "connection", start: StartConnections, key: "web2",
			play: []string{
				"web2", "@tab", "10.0.0.9", "@tab", "mosh", "@tab", "root", "@tab",
				"w2, www", "@tab", "10.0.0.9", "@tab", "Second web", "@ctrl+s",
			},
			check: func(t *testing.T, s *sandbox) {
				c, ok := mustConns(t, s).Get("web2")
				if !ok || c.Target != "10.0.0.9" || c.Via != "mosh" || c.User != "root" ||
					c.IP != "10.0.0.9" || c.Desc != "Second web" ||
					strings.Join(c.Aliases, "|") != "w2|www" {
					t.Errorf("connection = %+v", c)
				}
			},
		},
		{
			name: "connection with user@host and ssh", start: StartConnections, key: "web3",
			play: []string{"web3", "@tab", "ann@h3.example.com", "@tab", "ssh", "@ctrl+s"},
			check: func(t *testing.T, s *sandbox) {
				c, ok := mustConns(t, s).Get("web3")
				if !ok || c.User != "ann" || c.Target != "h3.example.com" || c.Via != "" {
					t.Errorf("connection = %+v", c)
				}
			},
		},
		{
			name: "repository", start: StartRepos, key: "newrepo",
			play: []string{"newrepo", "@tab", "~/code/newrepo", "@tab", "Fresh", "@enter"},
			check: func(t *testing.T, s *sandbox) {
				c, err := store.OpenRepos(s.Opts.Paths.ReposFile())
				if err != nil {
					t.Fatal(err)
				}

				r, ok := c.Get("newrepo")
				if !ok || r.Path != "~/code/newrepo" || r.Desc != "Fresh" {
					t.Errorf("repo = %+v", r)
				}
			},
		},
		{
			name: "alias", start: StartAliases, key: "zz",
			play: []string{"zz", "@tab", "echo hi there", "@ctrl+s"},
			check: func(t *testing.T, s *sandbox) {
				c, err := store.OpenAliases(s.Opts.Paths.AliasesFile())
				if err != nil {
					t.Fatal(err)
				}

				a, ok := c.Get("zz")
				if !ok || a.Cmd != "echo hi there" || a.Desc != "" {
					t.Errorf("alias = %+v", a)
				}
			},
		},
		{
			name: "function", start: StartFuncs, key: "fx",
			play: []string{
				"fx", "@tab,right,tab", "~/bin/fx.sh", "@tab,space,tab", "Fx tool",
				"@ctrl+s",
			},
			check: func(t *testing.T, s *sandbox) {
				c, err := store.OpenFuncs(s.Opts.Paths.FuncsFile())
				if err != nil {
					t.Fatal(err)
				}

				f, ok := c.Get("fx")
				if !ok || f.Kind != store.KindExec || f.Script != "~/bin/fx.sh" || !f.Managed ||
					f.Desc != "Fx tool" {
					t.Errorf("func = %+v", f)
				}
			},
		},
		{
			name: "function sourced by default", start: StartFuncs, key: "fy",
			play: []string{"fy", "@tab,tab", "/opt/fy.sh", "@ctrl+s"},
			check: func(t *testing.T, s *sandbox) {
				c, err := store.OpenFuncs(s.Opts.Paths.FuncsFile())
				if err != nil {
					t.Fatal(err)
				}

				f, ok := c.Get("fy")
				if !ok || f.Kind != store.KindSource || f.Managed {
					t.Errorf("func = %+v", f)
				}
			},
		},
	}

	for _, tt := range tests {
		s := newSandbox(t)
		g := openNew(t, s, tt.start)
		g.play(tt.play...)

		if len(g.r.overlays) != 0 {
			t.Errorf("%s: the form is still open:\n%s", tt.name, g.text())
			continue
		}

		tt.check(t, s)

		if got := g.selectedKey(); got != tt.key {
			t.Errorf("%s: selected %q, want %q", tt.name, got, tt.key)
		}

		if !strings.Contains(g.toastText(), "saved") || g.r.toast.err {
			t.Errorf("%s: toast %q", tt.name, g.toastText())
		}
	}
}

// TestFormAddMarksShellStale checks that a new alias asks the wrapper to reload its definitions,
// and a new connection does not.
//
// Parameters:
//   - t: test to report to
func TestFormAddMarksShellStale(t *testing.T) {
	g := openNew(t, newSandbox(t), StartAliases)
	g.play("zz", "@tab", "x", "@ctrl+s")

	if !g.r.stale {
		t.Error("an alias was added and the shell definitions are not marked stale")
	}

	g = openNew(t, newSandbox(t), StartConnections)
	g.play("zz", "@tab", "h", "@ctrl+s")

	if g.r.stale || g.selectedKey() != "zz" {
		t.Errorf("a connection was added: stale %v selected %q", g.r.stale, g.selectedKey())
	}
}

// TestFormEdit edits records, with and without a rename.
//
// Parameters:
//   - t: test to report to
func TestFormEdit(t *testing.T) {
	t.Run("alias command", func(t *testing.T) {
		s := newSandbox(t)
		g := openEdit(t, s, StartAliases)

		if !strings.Contains(g.text(), "ll") || !strings.Contains(g.text(), "Edit alias") {
			t.Errorf("the form does not show the record:\n%s", g.text())
		}

		g.play("@tab,end", " -h", "@ctrl+s")

		c, _ := store.OpenAliases(s.Opts.Paths.AliasesFile())
		if a, _ := c.Get("ll"); a.Cmd != "ls -la -h" || a.Desc != "Long list" {
			t.Errorf("alias = %+v", a)
		}

		if g.selectedKey() != "ll" || len(g.r.overlays) != 0 {
			t.Errorf("selected %q, overlays %d", g.selectedKey(), len(g.r.overlays))
		}
	})

	t.Run("rename a connection", func(t *testing.T) {
		s := newSandbox(t)
		g := openEdit(t, s, StartConnections)
		g.play("@end", "2", "@ctrl+s")

		c := mustConns(t, s)
		if _, ok := c.Get("prod"); ok {
			t.Error("the old name is still there")
		}

		if r, ok := c.Get("prod2"); !ok || r.User != "deploy" || r.Desc != "Production web" ||
			strings.Join(r.Aliases, ",") != "p" {
			t.Errorf("renamed = %+v", r)
		}

		if g.selectedKey() != "prod2" {
			t.Errorf("selected %q", g.selectedKey())
		}
	})

	t.Run("review flag is cleared", func(t *testing.T) {
		s := newSandbox(t)
		g := openEdit(t, s, StartConnections, "down", "down", "down")

		if g.frm().oldKey != "legacy" {
			t.Fatalf("editing %q", g.frm().oldKey)
		}

		g.play("@ctrl+s")

		if r, _ := mustConns(t, s).Get("legacy"); r.NeedsReview {
			t.Error("the record still needs review")
		}
	})

	t.Run("function", func(t *testing.T) {
		s := newSandbox(t)
		g := openEdit(t, s, StartFuncs, "down")

		if f := g.frm(); f.fields[1].text() != store.KindExec || !f.fields[3].On {
			t.Fatalf("kind %q managed %v", f.fields[1].text(), f.fields[3].On)
		}

		g.play("@tab,left", "@tab,tab,space", "@ctrl+s")

		c, _ := store.OpenFuncs(s.Opts.Paths.FuncsFile())
		if f, _ := c.Get("tool"); f.Kind != store.KindSource || f.Managed {
			t.Errorf("func = %+v", f)
		}
	})

	t.Run("repository path", func(t *testing.T) {
		s := newSandbox(t)
		g := openEdit(t, s, StartRepos, "down")
		g.play("@tab,ctrl+u", "~/elsewhere", "@ctrl+s")

		c, _ := store.OpenRepos(s.Opts.Paths.ReposFile())
		if r, _ := c.Get("docs"); r.Path != "~/elsewhere" {
			t.Errorf("repo = %+v", r)
		}
	})
}

// TestFormEditUnchanged checks that saving a record that was not changed writes nothing.
//
// Parameters:
//   - t: test to report to
func TestFormEditUnchanged(t *testing.T) {
	for _, start := range []Start{StartConnections, StartRepos, StartAliases, StartFuncs} {
		s := newSandbox(t)
		paths := []string{
			s.Opts.Paths.ConnectionsFile(), s.Opts.Paths.ReposFile(),
			s.Opts.Paths.AliasesFile(), s.Opts.Paths.FuncsFile(),
		}

		before := make([]string, len(paths))
		for i, p := range paths {
			before[i] = readFile(t, p)
		}

		g := openEdit(t, s, start)
		g.play("@ctrl+s")

		if len(g.r.overlays) != 0 || !strings.Contains(g.toastText(), "Nothing changed") {
			t.Errorf("start %d: overlays %d toast %q", start, len(g.r.overlays), g.toastText())
		}

		for i, p := range paths {
			if readFile(t, p) != before[i] {
				t.Errorf("start %d: %s was written", start, p)
			}
		}
	}
}

// TestFormCancel checks that Escape closes the form at any point once the question about the
// changes is answered with yes, and that nothing is ever written.
//
// Parameters:
//   - t: test to report to
func TestFormCancel(t *testing.T) {
	for _, start := range []Start{StartConnections, StartRepos, StartAliases, StartFuncs} {
		for _, edit := range []bool{false, true} {
			s := newSandbox(t)
			paths := []string{
				s.Opts.Paths.ConnectionsFile(), s.Opts.Paths.ReposFile(),
				s.Opts.Paths.AliasesFile(), s.Opts.Paths.FuncsFile(),
			}

			before := make([]string, len(paths))
			for i, p := range paths {
				before[i] = readFile(t, p)
			}

			var g *rig

			if edit {
				g = openEdit(t, s, start)
			} else {
				g = openNew(t, s, start)
			}

			g.play("changed", "@tab", "more", "@tab", "more", "@esc")

			if len(g.r.overlays) != 2 || g.Quit {
				t.Errorf("start %d edit %v: esc did not ask: overlays %d", start, edit,
					len(g.r.overlays))
			}

			g.play("y")

			if len(g.r.overlays) != 0 || g.Quit {
				t.Errorf("start %d edit %v: overlays %d quit %v", start, edit,
					len(g.r.overlays), g.Quit)
			}

			if strings.Contains(g.toastText(), "saved") {
				t.Errorf("start %d edit %v: toast %q", start, edit, g.toastText())
			}

			for i, p := range paths {
				if readFile(t, p) != before[i] {
					t.Errorf("start %d edit %v: %s was written", start, edit, p)
				}
			}
		}
	}
}

// TestFormValidation checks the message under a field and that nothing is written while it
// stands.
//
// Parameters:
//   - t: test to report to
func TestFormValidation(t *testing.T) {
	tests := []struct {
		name  string
		start Start
		play  []string
		want  string
	}{
		{"empty name", StartAliases, []string{"@tab", "x", "@shift+tab"}, "must not be empty"},
		{"name with a space", StartAliases, []string{"a b"}, "must not contain spaces"},
		{"name with a quote", StartAliases, []string{"a\"b"}, "must not contain"},
		{"connection name dash", StartConnections, []string{"-x"}, "must start with a letter"},
		{
			"connection name too long", StartConnections,
			[]string{strings.Repeat("\u00E9", 33)},
			"too long",
		},
		{
			"relative path", StartRepos,
			[]string{"r", "@tab", "rel/dir"},
			"the path must be absolute",
		},
		{
			"empty command", StartAliases,
			[]string{"r", "@tab", "@tab", "@shift+tab"},
			"must not be empty",
		},
		{
			"unknown placeholder", StartConnections,
			[]string{"c", "@tab,tab", "ssh {nope}"},
			"unknown placeholder",
		},
		{
			"bad ip", StartConnections,
			[]string{"c", "@tab,tab,tab,tab,tab", "nope"},
			"not an IP address",
		},
		{
			"bad user", StartConnections,
			[]string{"c", "@tab,tab,tab", "-root"},
			"must start with a letter",
		},
		{
			"alias repeats the name", StartConnections,
			[]string{"c", "@tab,tab,tab,tab", "c,d"},
			"repeats the name",
		},
		{
			"relative script", StartFuncs,
			[]string{"f", "@tab,tab", "bin/f.sh"},
			"the script must be an absolute",
		},
		{"empty script", StartFuncs, []string{"f", "@tab,tab,tab,tab"}, "must not be empty"},
		{"empty target", StartConnections, []string{"c", "@tab,tab"}, "must not be empty"},
	}

	for _, tt := range tests {
		s := newSandbox(t)
		before := readFile(t, s.Opts.Paths.ConnectionsFile()) +
			readFile(t, s.Opts.Paths.ReposFile()) + readFile(t, s.Opts.Paths.AliasesFile()) +
			readFile(t, s.Opts.Paths.FuncsFile())

		g := openNew(t, s, tt.start)
		g.play(tt.play...)

		if !strings.Contains(g.text(), tt.want) {
			t.Errorf("%s: %q is not shown:\n%s", tt.name, tt.want, g.text())
		}

		g.play("@ctrl+s")

		if len(g.r.overlays) != 1 {
			t.Errorf("%s: the form closed on an invalid record", tt.name)
		}

		after := readFile(t, s.Opts.Paths.ConnectionsFile()) +
			readFile(t, s.Opts.Paths.ReposFile()) + readFile(t, s.Opts.Paths.AliasesFile()) +
			readFile(t, s.Opts.Paths.FuncsFile())

		if after != before {
			t.Errorf("%s: the data changed", tt.name)
		}
	}
}

// TestFormSubmitFocusesTheFirstError checks that a refused save moves the keys to the first
// field that is wrong and says why.
//
// Parameters:
//   - t: test to report to
func TestFormSubmitFocusesTheFirstError(t *testing.T) {
	g := openNew(t, newSandbox(t), StartConnections)
	g.play("ok", "@ctrl+s")

	f := g.frm()

	if f.cur != 1 || !f.submitted || !strings.Contains(g.text(), "Some fields are not valid.") {
		t.Errorf("cur %d submitted %v\n%s", f.cur, f.submitted, g.text())
	}

	if !strings.Contains(g.text(), "must not be empty") {
		t.Errorf("the reason is missing:\n%s", g.text())
	}

	g.play("host", "@ctrl+s")

	if len(g.r.overlays) != 0 {
		t.Errorf("the corrected record was not saved:\n%s", g.text())
	}
}

// TestFormClash checks the names that other records hold, live and at save time.
//
// Parameters:
//   - t: test to report to
func TestFormClash(t *testing.T) {
	tests := []struct {
		name  string
		start Start
		play  []string
		want  string
	}{
		{"alias is a repo", StartAliases, []string{"proj", "@tab", "x"}, "is already used"},
		{"alias exists", StartAliases, []string{"ll", "@tab", "x"}, "already exists"},
		{"alias is reserved", StartAliases, []string{"if", "@tab", "x"}, "reserved name"},
		{"repo is a func", StartRepos, []string{"hello", "@tab", "/x"}, "is already used"},
		{"func is an alias", StartFuncs, []string{"gs", "@tab,tab", "/x"}, "is already used"},
		{"connection exists", StartConnections, []string{"prod", "@tab", "h"}, "already exists"},
		{
			"connection name is an alias", StartConnections,
			[]string{"p", "@tab", "h"},
			"is already used",
		},
		{
			"connection alias is a name", StartConnections,
			[]string{"c", "@tab", "h", "@tab,tab,tab", "Staging"},
			"is already used",
		},
	}

	for _, tt := range tests {
		s := newSandbox(t)
		g := openNew(t, s, tt.start)
		g.play(tt.play...)

		if !strings.Contains(g.text(), tt.want) {
			t.Errorf("%s: %q is not shown:\n%s", tt.name, tt.want, g.text())
		}

		before := readFile(t, s.Opts.Paths.ConnectionsFile())
		g.play("@ctrl+s")

		if len(g.r.overlays) != 1 || readFile(t, s.Opts.Paths.ConnectionsFile()) != before {
			t.Errorf("%s: overlays %d", tt.name, len(g.r.overlays))
		}
	}

	s := newSandbox(t)
	g := openEdit(t, s, StartConnections)
	g.play("@ctrl+u", "staging", "@ctrl+s")

	if len(g.r.overlays) != 1 || !strings.Contains(g.text(), "already exists") {
		t.Errorf("a rename onto an existing name was accepted:\n%s", g.text())
	}

	g = openEdit(t, s, StartConnections)
	g.play("@tab,tab,tab,tab,tab,tab", "x", "@ctrl+s")

	if len(g.r.overlays) != 0 {
		t.Errorf("a record may keep its own aliases:\n%s", g.text())
	}
}

// TestFormSaveFailures checks that a failed write is explained and keeps what was typed.
//
// Parameters:
//   - t: test to report to
func TestFormSaveFailures(t *testing.T) {
	t.Run("changed elsewhere", func(t *testing.T) {
		s := newSandbox(t)
		g := openNew(t, s, StartAliases)
		g.play("zz", "@tab", "echo")

		writeFile(t, s.Opts.Paths.AliasesFile(), fixtureAliases+"\n[[alias]]\nname = \"zz\"\n"+
			"cmd = \"other\"\n")

		g.play("@ctrl+s")

		if len(g.r.overlays) != 1 || !strings.Contains(g.text(), "was changed elsewhere") {
			t.Fatalf("conflict not reported:\n%s", g.text())
		}

		if f := g.frm(); f.fields[0].text() != "zz" || f.fields[1].text() != "echo" {
			t.Error("the typed text was lost")
		}

		g.play("@end", "x")

		if strings.Contains(g.text(), "was changed elsewhere") {
			t.Error("the failure stays after the text changed")
		}

		if !strings.Contains(g.text(), "already exists") {
			t.Errorf("the clash with the new record is not shown:\n%s", g.text())
		}

		if g.selectedKey() == "zz" {
			t.Error("the list moved")
		}
	})

	t.Run("unknown record", func(t *testing.T) {
		s := newSandbox(t)
		g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))
		before := readFile(t, s.Opts.Paths.AliasesFile())

		g.send(openOverlayMsg{Overlay: newForm(kindAlias, store.Alias{Name: "ghost", Cmd: "x"})})
		g.play("@tab,end", "y", "@ctrl+s")

		if len(g.r.overlays) != 1 || !strings.Contains(g.text(), "not found") {
			t.Errorf("an edit of a missing record was not refused:\n%s", g.text())
		}

		if readFile(t, s.Opts.Paths.AliasesFile()) != before {
			t.Error("the file changed")
		}

		g.play("@esc", "y")

		if len(g.r.overlays) != 0 {
			t.Error("esc did not close the form")
		}
	})

	t.Run("read-only file", func(t *testing.T) {
		s := newSandbox(t)
		writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n[[alias\n")

		g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))
		g.send(openOverlayMsg{Overlay: newForm(kindAlias, nil)})
		g.play("zz", "@tab", "x", "@ctrl+s")

		if len(g.r.overlays) != 1 || !strings.Contains(g.text(), "syntax error") {
			t.Errorf("the read-only file was not explained:\n%s", g.text())
		}

		if readFile(t, s.Opts.Paths.AliasesFile()) != "schema = 1\n[[alias\n" {
			t.Error("the broken file was written")
		}
	})

	t.Run("directory that cannot be written", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes anywhere")
		}

		s := newSandbox(t)
		g := openNew(t, s, StartAliases)
		g.play("zz", "@tab", "echo")

		if err := os.Chmod(s.Opts.Paths.Config, 0o500); err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = os.Chmod(s.Opts.Paths.Config, 0o700) })

		g.play("@ctrl+s")

		if len(g.r.overlays) != 1 || g.frm().msg == "" {
			t.Errorf("the failure was not reported:\n%s", g.text())
		}

		if f := g.frm(); f.fields[0].text() != "zz" || f.fields[1].text() != "echo" {
			t.Error("the typed text was lost")
		}
	})
}

// TestFormUnexpectedRecord checks that a record of the wrong type gives a form for a new record.
//
// Parameters:
//   - t: test to report to
func TestFormUnexpectedRecord(t *testing.T) {
	for _, k := range allKinds {
		f := newForm(k, 42).(*form)

		if f.oldKey != "" || f.orig != nil {
			t.Errorf("kind %d: form edits %q", k, f.oldKey)
		}
	}

	f := newForm(kindAlias, store.Repo{Name: "x", Path: "/x"}).(*form)
	if f.oldKey != "" {
		t.Error("an alias form took a repository")
	}
}

// TestFormSuggestsRepoPath checks the path suggested for a new repository.
//
// Parameters:
//   - t: test to report to
func TestFormSuggestsRepoPath(t *testing.T) {
	s := newSandbox(t)
	g := newRig(t, s.with(func(o *Options) {
		o.Start = StartRepos
		o.Settings.ReposRoot = "~/code/"
	}))

	g.press("n")
	g.play("tote", "@tab")

	if got := g.frm().fields[1].text(); got != "~/code/tote" {
		t.Errorf("suggested path = %q", got)
	}

	g.play("@shift+tab,ctrl+a,delete", "@tab")

	if got := g.frm().fields[1].text(); got != "~/code/tote" {
		t.Errorf("the suggestion was changed after the first visit: %q", got)
	}

	g = openNew(t, newSandbox(t), StartRepos)
	g.play("tote", "@tab")

	if got := g.frm().fields[1].text(); got != "" {
		t.Errorf("a path was suggested without a root: %q", got)
	}

	g = openEdit(t, newSandbox(t), StartRepos, "down")
	g.play("@tab")

	if got := g.frm().fields[1].text(); got != "~/missing/docs" {
		t.Errorf("an edit changed the path: %q", got)
	}
}

// TestFormKeys checks the movement between fields and the fields that are not text.
//
// Parameters:
//   - t: test to report to
func TestFormKeys(t *testing.T) {
	g := openNew(t, newSandbox(t), StartFuncs)
	f := g.frm()

	steps := []struct {
		keys string
		cur  int
	}{
		{"tab", 1},
		{"down", 2},
		{"enter", 3},
		{"tab", 4},
		{"tab", 0},
		{"shift+tab", 4},
		{"up", 3},
		{"shift+tab", 2},
		{"up", 1},
		{"up", 0},
		{"up", 4},
	}

	for _, st := range steps {
		g.play("@" + st.keys)

		if f.cur != st.cur {
			t.Fatalf("after %s: field %d, want %d", st.keys, f.cur, st.cur)
		}
	}

	g.play("@tab,tab")

	if f.cur != 1 || f.fields[1].text() != store.KindSource {
		t.Fatalf("cur %d kind %q", f.cur, f.fields[1].text())
	}

	for _, st := range []struct {
		key  string
		want string
	}{
		{"right", store.KindExec},
		{"right", store.KindSource},
		{"left", store.KindExec},
		{"left", store.KindSource},
		{"space", store.KindExec},
		{"x", store.KindExec},
		{"l", store.KindSource},
		{"h", store.KindExec},
	} {
		g.play("@" + st.key)

		if got := f.fields[1].text(); got != st.want {
			t.Errorf("%s: kind %q, want %q", st.key, got, st.want)
		}
	}

	g.play("@tab,tab")

	if f.cur != 3 || f.fields[3].On {
		t.Fatal("not on the switch")
	}

	for _, k := range []string{"space", "space", "x", "left", "right"} {
		before := f.fields[3].On
		g.play("@" + k)

		if f.fields[3].On == before {
			t.Errorf("%s did not flip the switch", k)
		}
	}

	g.play("@a")

	if !strings.Contains(g.text(), "managed") {
		t.Errorf("the switch is not shown:\n%s", g.text())
	}
}

// TestFormSwitchReusesScriptRule checks that the rule for a script follows the kind.
//
// Parameters:
//   - t: test to report to
func TestFormSwitchReusesScriptRule(t *testing.T) {
	g := openNew(t, newSandbox(t), StartFuncs)
	g.play("f", "@tab,tab", "bin/f", "@tab")

	if !strings.Contains(g.text(), "the script must be an absolute") {
		t.Fatalf("a relative script for source is accepted:\n%s", g.text())
	}

	g.play("@shift+tab,shift+tab,right")

	if strings.Contains(g.text(), "the script must be an absolute") {
		t.Errorf("a relative script for exec is refused:\n%s", g.text())
	}
}

// TestFormTextSafety checks that nothing dangerous gets into a field.
//
// Parameters:
//   - t: test to report to
func TestFormTextSafety(t *testing.T) {
	g := openNew(t, newSandbox(t), StartAliases)
	f := g.frm()

	g.play("@tab", "ab")
	g.send(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	g.send(tea.KeyPressMsg{Code: 'x', Text: "x", Mod: tea.ModAlt})
	g.send(tea.KeyPressMsg{Code: 'x', Text: "\x1b[31mred"})
	g.send(tea.PasteMsg{Content: "p\nq\t\x1b]0;title\x07r\u202Es\x00t"})
	g.send(tea.PasteMsg{Content: "\x1b\x07"})
	g.send(tea.PasteMsg{Content: "u\xffv"})

	if got := f.fields[1].Input.Value(); got != "ab[31mredp q ]0;titlerstuv" {
		t.Errorf("value = %q", got)
	}

	if f.cur != 1 {
		t.Errorf("the keys moved to field %d", f.cur)
	}

	for _, l := range g.view() {
		if strings.ContainsAny(stripStyles(l), "\x1b\x07\x00") {
			t.Errorf("a control character is drawn: %q", l)
		}
	}
}

// TestFormWideText checks the cursor on wide characters and on a sequence of joined characters.
//
// Parameters:
//   - t: test to report to
func TestFormWideText(t *testing.T) {
	g := openNew(t, newSandbox(t), StartAliases)
	f := g.frm()
	desc := func() *formField { return f.fields[2] }

	g.play("@tab,tab", "\u65E5\u672C", "e\u0301", "\U0001F468\u200D\U0001F469\u200D\U0001F467", "z")

	if got := desc().Input.Position(); got != 2+2+5+1 {
		t.Fatalf("position = %d", got)
	}

	steps := []struct {
		key string
		pos int
	}{
		{"left", 9},
		{"left", 4},
		{"left", 2},
		{"left", 1},
		{"left", 0},
		{"left", 0},
		{"right", 1},
		{"end", 10},
		{"home", 0},
		{"right", 1},
		{"right", 2},
		{"right", 4},
		{"right", 9},
	}

	for _, st := range steps {
		g.play("@" + st.key)

		if got := desc().Input.Position(); got != st.pos {
			t.Fatalf("after %s: position %d, want %d", st.key, got, st.pos)
		}
	}

	g.play("@backspace")

	if got := desc().Input.Value(); !strings.HasPrefix(got, "\u65E5\u672Ce\u0301\U0001F468") ||
		!strings.HasSuffix(got, "z") || len([]rune(got)) != 9 {
		t.Errorf("value after backspace = %q", got)
	}

	for _, w := range []int{40, 60, 100} {
		g.resize(w, 24)

		for i, l := range g.view() {
			if gw := g.r.e.Metric.width(l); gw > w {
				t.Errorf("width %d: line %d is %d cells", w, i, gw)
			}
		}
	}
}

// TestFormLongValue checks the horizontal scroll of a value longer than the field.
//
// Parameters:
//   - t: test to report to
func TestFormLongValue(t *testing.T) {
	long := strings.Repeat("0123456789", 12) + "END"

	for _, width := range []int{40, 60, 100} {
		g := openNew(t, newSandbox(t), StartAliases)
		g.resize(width, 24)
		g.play("n", "@tab", long)

		text := g.text()
		if !strings.Contains(text, "789END") {
			t.Errorf("width %d: the end is not shown:\n%s", width, text)
		}

		if strings.Contains(text, "0123456789012345678901234567890123456789012345678901234567890") {
			t.Errorf("width %d: more than the room is drawn:\n%s", width, text)
		}

		g.play("@home")

		text = g.text()
		if !strings.Contains(text, "0123456789") || strings.Contains(text, "END") {
			t.Errorf("width %d: the start is not shown:\n%s", width, text)
		}

		g.play("@ctrl+e,left,left,left,left,left,left,left,left,left,left,left,left,left,left")

		for i, l := range g.view() {
			if gw := g.r.e.Metric.width(l); gw > width {
				t.Errorf("width %d: line %d is %d cells", width, i, gw)
			}
		}
	}
}

// TestFormCursorIsDrawn checks that the focused field shows where the text goes, in every color
// mode, without changing the text.
//
// Parameters:
//   - t: test to report to
func TestFormCursorIsDrawn(t *testing.T) {
	for _, env := range [][]string{
		{"TOTE_COLOR=truecolor"},
		{"TOTE_COLOR=16"},
		{"TOTE_COLOR=none"},
		{"NO_COLOR=1", "TOTE_COLOR="},
	} {
		s := newSandbox(t).with(func(o *Options) {
			o.Environ = append(append([]string(nil), o.Environ...), env...)
		})

		g := openNew(t, s, StartAliases)
		g.play("ab", "@left")

		fd, e := g.frm().fields[0], g.r.e
		on, off := fd.view(e, 20, true), fd.view(e, 20, false)

		if on == off || stripStyles(on) != stripStyles(off) || e.Metric.width(on) != 20 {
			t.Errorf("%v: focused %q, other %q", env, on, off)
		}
	}
}

// TestFormViewMatrix draws a form of each kind, valid and with errors, at many sizes and in the
// color modes, and applies the rules of checkScreen to each frame.
//
// Parameters:
//   - t: test to report to
func TestFormViewMatrix(t *testing.T) {
	cases := []struct {
		name  string
		start Start
		play  []string
	}{
		{"connection", StartConnections, nil},
		{"connection errors", StartConnections, []string{"-x", "@tab", "-y", "@ctrl+s"}},
		{"connection long", StartConnections, []string{
			strings.Repeat("a", 60), "@tab",
			strings.Repeat("h", 200), "@tab,tab,tab,tab,tab,tab",
			"\u65E5\u672C\U0001F468\u200D\U0001F469",
		}},
		{"repo", StartRepos, []string{"x", "@tab", "rel"}},
		{"alias", StartAliases, []string{"@tab"}},
		{"alias clash", StartAliases, []string{"proj", "@tab", "x", "@ctrl+s"}},
		{"func", StartFuncs, []string{"@tab,tab,tab"}},
		{"func exec", StartFuncs, []string{"@tab,right", "@tab,tab"}},
	}

	modes := []struct {
		name  string
		env   []string
		ascii bool
	}{
		{"truecolor", []string{"TOTE_COLOR=truecolor"}, false},
		{"16", []string{"TOTE_COLOR=16"}, false},
		{"no_color", []string{"NO_COLOR=1", "TOTE_COLOR="}, false},
		{"ascii", []string{"TOTE_COLOR=none", "LANG=C"}, true},
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

			for _, w := range []int{40, 60, 80, 120} {
				for _, h := range []int{10, 12, 24, 40} {
					for _, c := range cases {
						s := src.with(func(o *Options) {
							o.Start = c.start
							o.Width, o.Height = w, h
							o.Printer = newPrinter(lang)
							o.Environ = append(append([]string(nil), o.Environ...), m.env...)
						})

						g := newRig(t, s)
						g.press("n")
						g.play(c.play...)

						checkScreen(t, m.name+"/"+lang+"/"+c.name, g.r.render(), w, h,
							m.ascii && c.name != "connection long")
					}
				}
			}
		}
	}
}

// TestFormKeysHelp checks the footer of a form.
//
// Parameters:
//   - t: test to report to
func TestFormKeysHelp(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		s := newSandbox(t).with(func(o *Options) { o.Printer = newPrinter(lang) })
		g := openNew(t, s, StartAliases)

		var got []string

		for _, b := range g.top().Keys(g.r.e) {
			if b.Help().Key == "" || b.Help().Desc == "" || strings.HasPrefix(b.Help().Desc, "!") {
				t.Errorf("%s: binding %+v", lang, b.Help())
			}

			got = append(got, b.Help().Key)
		}

		if strings.Join(got, " ") != "tab ctrl+s esc" {
			t.Errorf("%s: keys %v", lang, got)
		}

		if foot := g.footerText(); !strings.Contains(foot, "ctrl+s") {
			t.Errorf("%s: footer %q", lang, foot)
		}
	}
}

// TestFormTitles checks the title of the form of each kind in both languages.
//
// Parameters:
//   - t: test to report to
func TestFormTitles(t *testing.T) {
	want := map[string][]string{
		"en": {"New connection", "New repository", "New alias", "New function"},
		"fr": {
			"Nouvelle connexion", "Nouveau d\u00E9p\u00F4t", "Nouvel alias",
			"Nouvelle fonction",
		},
	}

	starts := []Start{StartConnections, StartRepos, StartAliases, StartFuncs}

	for lang, titles := range want {
		for i, start := range starts {
			s := newSandbox(t).with(func(o *Options) { o.Printer = newPrinter(lang) })
			g := openNew(t, s, start)

			if !strings.Contains(g.text(), titles[i]) {
				t.Errorf("%s: %q is not the title:\n%s", lang, titles[i], g.text())
			}
		}
	}
}

// TestScrollWindow checks the part of a long text that is shown.
//
// Parameters:
//   - t: test to report to
func TestScrollWindow(t *testing.T) {
	ones := func(n int) []int {
		w := make([]int, n)
		for i := range w {
			w[i] = 1
		}

		return w
	}

	tests := []struct {
		name               string
		widths             []int
		cur, off, avail    int
		mark               int
		wantStart, wantEnd int
	}{
		{"fits", ones(5), 2, 3, 10, 1, 0, 5},
		{"cursor at the start", ones(30), 0, 0, 10, 1, 0, 9},
		{"cursor moves right", ones(30), 9, 0, 10, 1, 2, 10},
		{"stays while visible", ones(30), 6, 2, 10, 1, 2, 10},
		{"end of text", ones(30), 29, 0, 10, 1, 21, 30},
		{"pulled back", ones(30), 29, 5, 10, 1, 21, 30},
		{"left of the window", ones(30), 3, 10, 10, 1, 3, 11},
		{"wide cells", []int{2, 2, 2, 2, 2, 2, 1}, 6, 0, 7, 1, 4, 7},
		{"tiny room", ones(30), 5, 0, 1, 1, 5, 6},
		{"three cell mark", ones(30), 29, 0, 10, 3, 23, 30},
	}

	for _, tt := range tests {
		start, end := scrollWindow(tt.widths, tt.cur, tt.off, tt.avail, tt.mark)

		if start != tt.wantStart || end != tt.wantEnd {
			t.Errorf("%s: window [%d, %d), want [%d, %d)", tt.name, start, end, tt.wantStart,
				tt.wantEnd)
		}

		if tt.cur < start || tt.cur >= end {
			t.Errorf("%s: the cursor %d is outside [%d, %d)", tt.name, tt.cur, start, end)
		}
	}
}

// TestWrapWords checks the breaking of a message into lines.
//
// Parameters:
//   - t: test to report to
func TestWrapWords(t *testing.T) {
	e := newRig(t, newSandbox(t)).r.e

	tests := []struct {
		name  string
		in    string
		w     int
		lines int
		want  []string
	}{
		{"fits", "one two", 20, 3, []string{"one two"}},
		{"two lines", "one two three four", 9, 3, []string{"one two", "three", "four"}},
		{"cut at the limit", "one two three four", 9, 2, []string{"one two", "three \u2026"}},
		{"long word", "abcdefghij", 4, 5, []string{"abcd", "efgh", "ij"}},
		{"empty", "  ", 10, 3, nil},
		{"no room", "abc", 0, 3, nil},
		{"wide", "\u65E5\u672C\u8A9E", 4, 5, []string{"\u65E5\u672C", "\u8A9E"}},
	}

	for _, tt := range tests {
		got := wrapWords(e, tt.in, tt.w, tt.lines)

		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestReasonText checks that the reasons of the store validators are translated, and that a
// reason it does not know is kept.
//
// Parameters:
//   - t: test to report to
func TestReasonText(t *testing.T) {
	long := strings.Repeat("a", 65)

	tests := []struct {
		name string
		err  error
		en   string
	}{
		{"empty", store.ValidConnectionName(""), "must not be empty"},
		{"too long", store.ValidConnectionName(long), "too long (65 bytes, 64 at most)"},
		{
			"desc too long", store.ValidDesc(strings.Repeat("a", 201)),
			"too long (201 characters, 200 at most)",
		},
		{"connection name", store.ValidConnectionName("-a"), "must start with a letter"},
		{"func name", store.ValidFuncName("1a"), "must start with a letter or _"},
		{"utf8", store.ValidDesc("\xff"), "is not valid UTF-8"},
		{"dash", store.ValidAliasName("-a"), "must not start with -"},
		{"space", store.ValidAliasName("a b"), "must not contain spaces"},
		{"char", store.ValidAliasName("a/b"), "must not contain '/'"},
		{"control", store.ValidDesc("a\x01"), "must not contain control characters"},
		{"line", store.Alias{Name: "a", Cmd: "x\ny"}.Validate(), "must not contain a NUL"},
		{
			"edge space", store.Connection{Name: "a", Target: "a", Via: " ssh"}.Validate(),
			"must not start or end with a space",
		},
		{
			"via verb", store.Connection{Name: "a", Target: "a", Via: "-x"}.Validate(),
			"the first word must be a command name",
		},
		{
			"close brace", store.Connection{Name: "a", Target: "a", Via: "x }"}.Validate(),
			"} without a matching {",
		},
		{
			"open brace", store.Connection{Name: "a", Target: "a", Via: "x {host"}.Validate(),
			"{ without a matching }",
		},
		{
			"placeholder", store.Connection{Name: "a", Target: "a", Via: "x {no}"}.Validate(),
			`unknown placeholder "no"`,
		},
		{"colon", store.Connection{Name: "a", Target: ":a"}.Validate(), "must not start or end"},
		{"alias repeat", store.Connection{Name: "a", Target: "a", Aliases: []string{"a"}}.
			Validate(), "an alias repeats the name"},
		{"alias duplicate", store.Connection{Name: "a", Target: "a", Aliases: []string{"b", "b"}}.
			Validate(), "duplicate alias"},
		{"ip", store.Connection{Name: "a", Target: "a", IP: "x"}.Validate(), "is not an IP"},
		{
			"zone", store.Connection{Name: "a", Target: "a", IP: "fe80::1%eth0"}.Validate(),
			"must not carry a zone",
		},
		{"reserved", store.ValidAliasName("tote"), `"tote" is a reserved name`},
		{"unknown", store.Func{Name: "a", Kind: "x", Script: "/a"}.Validate(), "must be"},
	}

	en := newRig(t, newSandbox(t)).r.e
	fr := newRig(t, newSandbox(t).with(func(o *Options) { o.Printer = newPrinter("fr") })).r.e

	for _, tt := range tests {
		if tt.err == nil {
			t.Errorf("%s: the validator accepted the value", tt.name)
			continue
		}

		if got := reasonText(en, tt.err); !strings.Contains(got, tt.en) {
			t.Errorf("%s: en %q does not contain %q (from %q)", tt.name, got, tt.en, tt.err)
		}

		got := reasonText(fr, tt.err)
		if tt.name != "unknown" && strings.Contains(got, tt.en) && tt.en != "is not an IP" {
			t.Errorf("%s: fr %q is not translated", tt.name, got)
		}

		if _, rest, _ := strings.Cut(tt.err.Error(), "invalid value: "); tt.name != "unknown" &&
			got == rest {
			t.Errorf("%s: fr %q is the raw reason", tt.name, got)
		}
	}
}
