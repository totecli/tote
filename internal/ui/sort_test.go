package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// The fixtures of the sandbox are not in alphabetical order: the connections are prod, staging,
// bastion, legacy, emoji, hostile in the file, and the repositories proj then docs, the aliases ll
// then gs. The tests below rely on that to tell a position in the file from a position on screen.
var (
	connsFile   = []string{"prod", "staging", "bastion", "legacy", "emoji", "hostile"}
	connsByName = []string{"bastion", "emoji", "hostile", "legacy", "prod", "staging"}
)

// sortedBox returns a sandbox that shows the lists by name, the default of ui.sort.
//
// Parameters:
//   - t: running test
func sortedBox(t *testing.T) *sandbox {
	t.Helper()

	return newSandbox(t).with(func(o *Options) { o.Settings.UI.Sort = "name" })
}

// keysOf lists the keys of items.
//
// Parameters:
//   - items: items to read
func keysOf(items []item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Key
	}

	return out
}

// shownKeys lists the keys of the rows of a list, in the order they are drawn.
//
// Parameters:
//   - g: rig on a list
func shownKeys(g *rig) []string {
	l := g.r.scr.(*listScreen)
	out := make([]string, len(l.shown))

	for i, idx := range l.shown {
		out[i] = l.items[idx].Key
	}

	return out
}

// TestSortedItems checks the order of the items of each kind in both modes, and that the count
// and the lookup by key do not depend on it.
//
// Parameters:
//   - t: test to report to
func TestSortedItems(t *testing.T) {
	tests := []struct {
		kind   kind
		file   []string
		byName []string
	}{
		{kindConn, connsFile, connsByName},
		{kindRepo, []string{"proj", "docs"}, []string{"docs", "proj"}},
		{kindAlias, []string{"ll", "gs"}, []string{"gs", "ll"}},
		{kindFunc, []string{"hello", "tool"}, []string{"hello", "tool"}},
	}

	for _, tt := range tests {
		s := newSandbox(t)
		d := newData(t, s)

		if got := keysOf(d.items(tt.kind)); !slices.Equal(got, tt.file) {
			t.Errorf("kind %d, new dataset: %v, want the file order %v", tt.kind, got, tt.file)
		}

		d.setSort("name")

		if got := keysOf(d.items(tt.kind)); !slices.Equal(got, tt.byName) {
			t.Errorf("kind %d, by name: %v, want %v", tt.kind, got, tt.byName)
		}

		if got := keysOf(d.itemsIn(tt.kind, false)); !slices.Equal(got, tt.file) {
			t.Errorf("kind %d, itemsIn(false): %v, want %v", tt.kind, got, tt.file)
		}

		d.setSort("file")

		if got := keysOf(d.items(tt.kind)); !slices.Equal(got, tt.file) {
			t.Errorf("kind %d, file: %v, want %v", tt.kind, got, tt.file)
		}

		if d.count(tt.kind) != len(tt.file) {
			t.Errorf("kind %d: count %d, want %d", tt.kind, d.count(tt.kind), len(tt.file))
		}

		for _, key := range tt.file {
			if _, ok := d.record(tt.kind, key); !ok {
				t.Errorf("kind %d: record %q not found", tt.kind, key)
			}
		}
	}

	d := newData(t, newSandbox(t))
	d.setSort("")

	if got := keysOf(d.items(kindConn)); !slices.Equal(got, connsByName) {
		t.Errorf("an empty setting gives %v, want the default order by name", got)
	}
}

// TestSortedItemsKeepRecords checks that each item still carries its own record and cells once
// the list is sorted, so that nothing is read from the wrong row.
//
// Parameters:
//   - t: test to report to
func TestSortedItemsKeepRecords(t *testing.T) {
	d := newData(t, newSandbox(t))
	d.setSort("name")

	for _, it := range d.items(kindConn) {
		c, ok := it.Rec.(store.Connection)
		if !ok || c.Name != it.Key || it.Cells[0] != sanitize(c.Name) {
			t.Errorf("item %q holds %+v and cells %q", it.Key, it.Rec, it.Cells)
		}
	}
}

// TestSortedListDrawn checks the order of the rows on screen and the cursor in both modes.
//
// Parameters:
//   - t: test to report to
func TestSortedListDrawn(t *testing.T) {
	for _, tt := range []struct {
		name string
		box  *sandbox
		want []string
	}{
		{"by name", sortedBox(t), connsByName},
		{"file", newSandbox(t), connsFile},
	} {
		g := newRig(t, tt.box.with(func(o *Options) { o.Start = StartConnections }))

		if got := shownKeys(g); !slices.Equal(got, tt.want) {
			t.Errorf("%s: rows %v, want %v", tt.name, got, tt.want)
		}

		last := -1

		for _, name := range tt.want {
			y, _ := lineOf(g, name)
			if y <= last {
				t.Errorf("%s: %q is drawn at line %d, not below line %d:\n%s", tt.name, name, y,
					last, g.text())
			}

			last = y
		}

		if g.selectedKey() != tt.want[0] {
			t.Errorf("%s: the cursor is on %q, want %q", tt.name, g.selectedKey(), tt.want[0])
		}
	}
}

// TestSortedActionsHitTheRightRecord checks that the keys that act on the second row act on the
// record that is drawn there, when the file holds another one in second place.
//
// Parameters:
//   - t: test to report to
func TestSortedActionsHitTheRightRecord(t *testing.T) {
	t.Run("cursor", func(t *testing.T) {
		g := newRig(t, sortedBox(t).with(func(o *Options) { o.Start = StartConnections }))
		g.press("down")

		if g.selectedKey() != "emoji" {
			t.Errorf("the second row is %q, want emoji (staging is second in the file)",
				g.selectedKey())
		}
	})

	t.Run("edit", func(t *testing.T) {
		s := sortedBox(t)
		g := openEdit(t, s, StartConnections, "down")

		if f := g.frm(); f.oldKey != "emoji" || f.kind != kindConn {
			t.Errorf("the form edits %q, want emoji (staging is second in the file)", f.oldKey)
		}

		g.play("@end", "2", "@ctrl+s")

		c := mustConns(t, s)
		if _, ok := c.Get("emoji2"); !ok {
			t.Error("emoji was not renamed")
		}

		for _, name := range []string{"staging", "prod", "bastion"} {
			if _, ok := c.Get(name); !ok {
				t.Errorf("%s was touched", name)
			}
		}

		if g.selectedKey() != "emoji2" {
			t.Errorf("selected %q, want emoji2", g.selectedKey())
		}
	})

	t.Run("delete", func(t *testing.T) {
		s := sortedBox(t)
		g := openDelete(t, s, StartConnections, "down")

		if c := g.top().(*confirm); c.key != "emoji" {
			t.Errorf("the question is about %q, want emoji", c.key)
		}

		g.press("y")

		c := mustConns(t, s)
		if _, ok := c.Get("emoji"); ok {
			t.Error("emoji is still there")
		}

		if got := len(c.List()); got != 5 {
			t.Errorf("%d connections left, want 5", got)
		}

		if _, ok := c.Get("staging"); !ok {
			t.Error("staging, second in the file, was deleted")
		}
	})

	t.Run("open", func(t *testing.T) {
		g := newRig(t, sortedBox(t).with(func(o *Options) { o.Start = StartConnections }))
		g.press("down", "enter")

		var got []string

		for _, m := range g.Seen {
			if a, ok := m.(activateMsg); ok {
				got = append(got, a.Key)
			}
		}

		if !slices.Equal(got, []string{"emoji"}) {
			t.Errorf("enter on the second row opened %v, want emoji", got)
		}
	})

	t.Run("copy", func(t *testing.T) {
		g := newRig(t, sortedBox(t).with(func(o *Options) { o.Start = StartConnections }))
		g.press("down", "C")

		var got []string

		for _, m := range g.Seen {
			if c, ok := m.(copyMsg); ok {
				got = append(got, c.Key)
			}
		}

		if !slices.Equal(got, []string{"emoji"}) {
			t.Errorf("copy on the second row asked for %v, want emoji", got)
		}
	})

	t.Run("view", func(t *testing.T) {
		g := newRig(t, sortedBox(t).with(func(o *Options) { o.Start = StartConnections }))
		g.press("down", "v")

		if _, ok := g.top().(*infoOverlay); !ok || !strings.Contains(g.text(), "emoji") ||
			strings.Contains(g.text(), "staging.example.com") {
			t.Errorf("the details are not the ones of emoji (%T):\n%s", g.top(), g.text())
		}
	})

	t.Run("repositories", func(t *testing.T) {
		g := openEdit(t, sortedBox(t), StartRepos, "down")

		if f := g.frm(); f.oldKey != "proj" {
			t.Errorf("the form edits %q, want proj (docs is second in the file)", f.oldKey)
		}
	})

	t.Run("aliases", func(t *testing.T) {
		g := openDelete(t, sortedBox(t), StartAliases, "down")

		if c := g.top().(*confirm); c.key != "ll" {
			t.Errorf("the question is about %q, want ll (gs is second in the file)", c.key)
		}
	})
}

// TestSortedFilter checks that a filter ranks in the displayed order, and that the cursor and the
// keys act on the match that is on screen.
//
// Parameters:
//   - t: test to report to
func TestSortedFilter(t *testing.T) {
	tests := []struct {
		name string
		box  *sandbox
		want []string
	}{
		{"by name", sortedBox(t), []string{"emoji", "bastion", "hostile", "legacy", "prod", "staging"}},
		{"file", newSandbox(t), []string{"emoji", "prod", "staging", "bastion", "legacy", "hostile"}},
	}

	for _, tt := range tests {
		g := newRig(t, tt.box.with(func(o *Options) { o.Start = StartConnections }))
		g.press("/")
		g.typeText("e")

		if got := shownKeys(g); !slices.Equal(got, tt.want) {
			t.Errorf("%s: filter e shows %v, want %v", tt.name, got, tt.want)
		}
	}

	g := newRig(t, sortedBox(t).with(func(o *Options) { o.Start = StartConnections }))
	g.press("/")
	g.typeText("st")
	g.press("enter")

	if got := shownKeys(g); got[0] != "staging" {
		t.Fatalf("filter st shows %v, want staging first", got)
	}

	g.press("e")

	if f := g.frm(); f.oldKey != "staging" {
		t.Errorf("the form edits %q, want staging", f.oldKey)
	}
}

// TestSortedPalette checks that the palette lists the records of each kind by name, with and
// without a query, and that Enter runs the entry that is drawn.
//
// Parameters:
//   - t: test to report to
func TestSortedPalette(t *testing.T) {
	names := func(p *paletteOverlay, k kind) []string {
		var out []string

		for _, h := range p.hits {
			if en := &p.entries[h]; en.Scope == scopeOfKind[k] {
				out = append(out, en.Key)
			}
		}

		return out
	}

	t.Run("empty query", func(t *testing.T) {
		g := newRig(t, sortedBox(t))
		p := openPalette(g)

		if got := names(p, kindConn); !slices.Equal(got, connsByName) {
			t.Errorf("connections: %v, want %v", got, connsByName)
		}

		if got := names(p, kindRepo); !slices.Equal(got, []string{"docs", "proj"}) {
			t.Errorf("repositories: %v", got)
		}

		if got := names(p, kindAlias); !slices.Equal(got, []string{"gs", "ll"}) {
			t.Errorf("aliases: %v", got)
		}
	})

	t.Run("empty query in file order", func(t *testing.T) {
		g := newRig(t, newSandbox(t))
		p := openPalette(g)

		if got := names(p, kindConn); !slices.Equal(got, connsFile) {
			t.Errorf("connections: %v, want %v", got, connsFile)
		}

		if got := names(p, kindAlias); !slices.Equal(got, []string{"ll", "gs"}) {
			t.Errorf("aliases: %v", got)
		}
	})

	t.Run("category", func(t *testing.T) {
		g := newRig(t, sortedBox(t))
		p := openPalette(g)
		g.typeText("conn")

		if got := names(p, kindConn); !slices.Equal(got, connsByName) {
			t.Errorf("the category query gives %v, want %v", got, connsByName)
		}
	})

	t.Run("enter on the second entry", func(t *testing.T) {
		g := newRig(t, sortedBox(t))
		openPalette(g)
		g.typeText("c ")
		g.press("down", "enter")

		var got []string

		for _, m := range g.Seen {
			if a, ok := m.(activateMsg); ok {
				got = append(got, a.Key)
			}
		}

		if !slices.Equal(got, []string{"emoji"}) {
			t.Errorf("enter on the second connection opened %v, want emoji", got)
		}
	})
}

// TestSortedClickHitsTheRow checks that the mouse zones follow the drawn order.
//
// Parameters:
//   - t: test to report to
func TestSortedClickHitsTheRow(t *testing.T) {
	g, _ := mouseRig(t, 100, 30, StartConnections, func(o *Options) {
		o.Settings.UI.Sort = "name"
	})

	for _, name := range []string{"staging", "emoji", "bastion", "prod"} {
		clickOn(g, name)

		if g.selectedKey() != name {
			t.Errorf("a click on %q selected %q", name, g.selectedKey())
		}
	}
}

// TestSortedAddSelectsTheNewRecord checks that after an add or a rename the cursor is on the
// record where it now appears, which is not where the file puts it.
//
// Parameters:
//   - t: test to report to
func TestSortedAddSelectsTheNewRecord(t *testing.T) {
	t.Run("add in the middle", func(t *testing.T) {
		s := sortedBox(t)
		g := openNew(t, s, StartConnections)
		g.play("cc", "@tab", "h", "@ctrl+s")

		l := g.r.scr.(*listScreen)
		if g.selectedKey() != "cc" || l.sel != 1 {
			t.Errorf("selected %q at row %d, want cc at row 1", g.selectedKey(), l.sel)
		}

		if got := shownKeys(g); !slices.Equal(got, []string{"bastion", "cc", "emoji", "hostile",
			"legacy", "prod", "staging"}) {
			t.Errorf("rows = %v", got)
		}

		file := mustConns(t, s).List()
		if last := file[len(file)-1].Name; last != "cc" {
			t.Errorf("the file ends with %q, want the new record at the end", last)
		}
	})

	t.Run("add at the end", func(t *testing.T) {
		g := openNew(t, sortedBox(t), StartConnections)
		g.play("zz", "@tab", "h", "@ctrl+s")

		if l := g.r.scr.(*listScreen); g.selectedKey() != "zz" || l.sel != 6 {
			t.Errorf("selected %q at row %d, want zz at row 6", g.selectedKey(), l.sel)
		}
	})

	t.Run("add in file mode", func(t *testing.T) {
		g := openNew(t, newSandbox(t), StartConnections)
		g.play("cc", "@tab", "h", "@ctrl+s")

		if l := g.r.scr.(*listScreen); g.selectedKey() != "cc" || l.sel != 6 {
			t.Errorf("selected %q at row %d, want cc at row 6", g.selectedKey(), l.sel)
		}
	})

	t.Run("rename moves the row", func(t *testing.T) {
		s := sortedBox(t)
		g := openEdit(t, s, StartConnections, "down", "down", "down", "down", "down")

		if g.selectedKey() != "staging" {
			t.Fatalf("the sixth row is %q, want staging", g.selectedKey())
		}

		g.play("@home", "a", "@ctrl+s")

		if l := g.r.scr.(*listScreen); g.selectedKey() != "astaging" || l.sel != 0 {
			t.Errorf("selected %q at row %d, want astaging at row 0", g.selectedKey(), l.sel)
		}
	})
}

// TestSortedDeleteKeepsPosition checks that after a delete the cursor stays on the same row.
//
// Parameters:
//   - t: test to report to
func TestSortedDeleteKeepsPosition(t *testing.T) {
	g := openDelete(t, sortedBox(t), StartConnections, "down")
	g.press("y")

	if got := g.selectedKey(); got != "hostile" {
		t.Errorf("after deleting emoji the cursor is on %q, want hostile", got)
	}
}

// TestSortedLoginCycleKeepsFileOrder checks that the logins offered by the user key do not depend
// on how the list is shown: they come in the order of the file, even when the names say another.
//
// Parameters:
//   - t: test to report to
func TestSortedLoginCycleKeepsFileOrder(t *testing.T) {
	const conns = `schema = 1

[[connection]]
name = "zed"
target = "zed.example.com"
user = "alice"

[[connection]]
name = "mid"
target = "mid.example.com"
user = "bob"

[[connection]]
name = "amy"
target = "amy.example.com"
user = "carl"
`

	want := []string{"", "root", "tester", "bob", "carl"}

	for _, box := range []*sandbox{newSandbox(t), sortedBox(t)} {
		writeFile(t, box.Opts.Paths.ConnectionsFile(), conns)

		g := newRig(t, box.with(func(o *Options) { o.Start = StartConnections }))
		c, _ := mustConns(t, box).Get("zed")

		if got := userChoices(g.r.e, c); !slices.Equal(got, want) {
			t.Errorf("ui.sort = %q: logins %v, want %v", box.Opts.Settings.UI.Sort, got, want)
		}
	}
}

// TestSortedFunctions checks the order of the functions in both modes, with a file that is not in
// alphabetical order.
//
// Parameters:
//   - t: test to report to
func TestSortedFunctions(t *testing.T) {
	const funcs = `schema = 1

[[func]]
name = "tool"
kind = "exec"
script = "/opt/tool/tool"

[[func]]
name = "hello"
kind = "source"
script = "~/bin/hello.sh"
`

	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.FuncsFile(), funcs)

	d := newData(t, s)

	if got := keysOf(d.items(kindFunc)); !slices.Equal(got, []string{"tool", "hello"}) {
		t.Errorf("file mode: %v", got)
	}

	d.setSort("name")

	if got := keysOf(d.items(kindFunc)); !slices.Equal(got, []string{"hello", "tool"}) {
		t.Errorf("by name: %v", got)
	}

	box := sortedBox(t)
	writeFile(t, box.Opts.Paths.FuncsFile(), funcs)

	g := openEdit(t, box, StartFuncs)

	if f := g.frm(); f.oldKey != "hello" {
		t.Errorf("the form edits %q, want hello, the first row by name", f.oldKey)
	}
}

// TestSortedNameClashFollowsTheFile checks that two names that differ only by case resolve like
// the command line does, whatever ui.sort says: the same case first, then the first record of
// the file. Enter in the filter and Enter in the palette both go by that rule.
//
// Parameters:
//   - t: test to report to
func TestSortedNameClashFollowsTheFile(t *testing.T) {
	const conns = `schema = 1

[[connection]]
name = "api"
target = "lower.example.com"

[[connection]]
name = "Api"
target = "upper.example.com"

[[connection]]
name = "Zed"
target = "zed.example.com"

[[connection]]
name = "alpha"
target = "alpha.example.com"
`

	activated := func(g *rig) []string {
		var out []string

		for _, m := range g.Seen {
			if a, ok := m.(activateMsg); ok {
				out = append(out, a.Key)
			}
		}

		return out
	}

	for _, box := range []*sandbox{newSandbox(t), sortedBox(t)} {
		mode := box.Opts.Settings.UI.Sort
		writeFile(t, box.Opts.Paths.ConnectionsFile(), conns)

		for _, tt := range []struct{ typed, want string }{{"api", "api"}, {"Api", "Api"}} {
			g := newRig(t, box.with(func(o *Options) { o.Start = StartConnections }))
			g.press("/")
			g.typeText(tt.typed)
			g.press("enter")

			if got := g.selectedKey(); got != tt.want {
				t.Errorf("ui.sort = %q: filter %q and Enter select %q, want %q", mode, tt.typed,
					got, tt.want)
			}
		}

		// The file lists "api" first and the palette has no case rule, so the file decides.
		g := newRig(t, box.with(func(o *Options) { o.Start = StartConnections }))
		openPalette(g)
		g.typeText("api")
		g.press("enter")

		if got := activated(g); !slices.Equal(got, []string{"api"}) {
			t.Errorf("ui.sort = %q: the palette opened %v, want api", mode, got)
		}
	}
}

// TestSortSettingAppliesAtOnce steps ui.sort on the settings screen and checks that the lists
// follow it, that the file is written with the key and keeps the rest.
//
// Parameters:
//   - t: test to report to
func TestSortSettingAppliesAtOnce(t *testing.T) {
	g, s := configRig(t, "schema = 1\n\n# my own note\n[ui]\ntheme = \"dark\"\n")

	if g.r.e.Settings.UI.Sort != "name" {
		t.Fatalf("a file without the key gives ui.sort = %q, want name", g.r.e.Settings.UI.Sort)
	}

	g.send(openKindMsg{Kind: kindConn})

	if got := shownKeys(g); !slices.Equal(got, connsByName) {
		t.Fatalf("by name: %v", got)
	}

	g.send(openConfigMsg{})
	selectRow(g, "sort")

	if !strings.Contains(g.text(), "name (A to Z)") || !strings.Contains(g.text(), "ui.sort") {
		t.Errorf("the row does not show the default:\n%s", g.text())
	}

	g.press("right")
	g.await("the write", func() bool { return g.r.e.Settings.UI.Sort == "file" })
	g.settle()

	if got := readConfig(t, s); !strings.Contains(got, `sort = "file"`) ||
		!strings.Contains(got, "# my own note") || !strings.Contains(got, `theme = "dark"`) {
		t.Errorf("config.toml after the change:\n%s", got)
	}

	g.send(openKindMsg{Kind: kindConn})

	if got := shownKeys(g); !slices.Equal(got, connsFile) {
		t.Errorf("after the change: %v, want the file order %v", got, connsFile)
	}

	g.send(openConfigMsg{})
	selectRow(g, "sort")
	g.press("d", "y")
	g.await("the reset", func() bool { return g.r.e.Settings.UI.Sort == "name" })
	g.settle()

	if strings.Contains(readConfig(t, s), "sort") {
		t.Errorf("the reset left the key:\n%s", readConfig(t, s))
	}

	g.send(openKindMsg{Kind: kindConn})

	if got := shownKeys(g); !slices.Equal(got, connsByName) {
		t.Errorf("after the reset: %v", got)
	}
}

// TestSortNeverRewritesFiles checks that showing the lists by name, with every action of a list
// that does not edit a record, leaves every data file byte for byte as it was.
//
// Parameters:
//   - t: test to report to
func TestSortNeverRewritesFiles(t *testing.T) {
	s := sortedBox(t)
	paths := []string{
		s.Opts.Paths.ConnectionsFile(), s.Opts.Paths.ReposFile(), s.Opts.Paths.AliasesFile(),
		s.Opts.Paths.FuncsFile(),
	}

	before := make([]string, len(paths))
	for i, p := range paths {
		before[i] = readFile(t, p)
	}

	g := newRig(t, s.with(func(o *Options) { o.Start = StartConnections }))
	g.press("down", "v", "esc", "/")
	g.typeText("e")
	g.press("esc", "esc")
	g.send(openKindMsg{Kind: kindRepo})
	g.send(openKindMsg{Kind: kindAlias})
	g.send(openKindMsg{Kind: kindFunc})
	g.send(reloadMsg{})

	for i, p := range paths {
		if got := readFile(t, p); got != before[i] {
			t.Errorf("%s changed:\n%s", p, got)
		}
	}
}
