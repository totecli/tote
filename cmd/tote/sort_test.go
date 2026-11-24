package main

import (
	"slices"
	"strings"
	"testing"
)

const (
	// sortConns holds three connections whose order in the file is not the order by name: web,
	// Db and api. web and api share the alias x, as a file edited by hand may.
	sortConns = `schema = 1

[[connection]]
name = "web"
target = "w.example.org"
aliases = ["x"]

[[connection]]
name = "Db"
target = "d.example.org"
aliases = ["y"]

[[connection]]
name = "api"
target = "a.example.org"
aliases = ["x"]
`

	// sortRepos holds three repositories, Zeta first.
	sortRepos = `schema = 1

[[repo]]
name = "Zeta"
path = "/srv/zeta"

[[repo]]
name = "alpha"
path = "/srv/alpha"

[[repo]]
name = "Beta"
path = "/srv/beta"
`

	// sortAliases holds three aliases, zz first.
	sortAliases = `schema = 1

[[alias]]
name = "zz"
cmd = "true"

[[alias]]
name = "Aa"
cmd = "true"

[[alias]]
name = "mm"
cmd = "true"
`

	// sortFuncs holds three functions, tool first.
	sortFuncs = `schema = 1

[[func]]
name = "tool"
kind = "exec"
script = "/opt/tool/tool"

[[func]]
name = "Hello"
kind = "source"
script = "/opt/hello.sh"

[[func]]
name = "also"
kind = "exec"
script = "/opt/also"
`
)

// sortHarness returns a harness whose four files are not in alphabetical order.
//
// Parameters:
//   - t: running test
//   - config: text of config.toml, empty for no file
func sortHarness(t *testing.T, config string) *harness {
	t.Helper()

	h := newHarness(t)
	h.write("connections.toml", sortConns)
	h.write("repos.toml", sortRepos)
	h.write("aliases.toml", sortAliases)
	h.write("funcs.toml", sortFuncs)

	if config != "" {
		h.write("config.toml", config)
	}

	return h
}

// sortVerbs lists the four list verbs with the names in the order of the file and by name.
var sortVerbs = []struct {
	verb   []string
	file   []string
	byName []string
}{
	{[]string{"co"}, []string{"web", "Db", "api"}, []string{"api", "Db", "web"}},
	{[]string{"repos"}, []string{"Zeta", "alpha", "Beta"}, []string{"alpha", "Beta", "Zeta"}},
	{[]string{"alias"}, []string{"zz", "Aa", "mm"}, []string{"Aa", "mm", "zz"}},
	{[]string{"funcs"}, []string{"tool", "Hello", "also"}, []string{"also", "Hello", "tool"}},
}

// tableNames returns the first column of a table, without the header.
//
// Parameters:
//   - out: text of a table
func tableNames(out string) []string {
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")

	var names []string

	for _, l := range lines[1:] {
		names = append(names, strings.Fields(l)[0])
	}

	return names
}

// tsvNames returns the first field of each line of a tab separated list.
//
// Parameters:
//   - out: text of a list in the tsv format
func tsvNames(out string) []string {
	var names []string

	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		name, _, _ := strings.Cut(l, "\t")
		names = append(names, name)
	}

	return names
}

// TestListTableSortedByDefault checks that the table of every list verb is ordered by name when
// config.toml has no ui.sort, and that the table of "co -l" and of the plain verbs agree.
//
// Parameters:
//   - t: test to report to
func TestListTableSortedByDefault(t *testing.T) {
	for _, v := range sortVerbs {
		for _, args := range [][]string{
			{"list", "--format", "table"}, {"-l", "--format", "table"}, {"--format", "table"},
			{"ls", "--format=table"},
		} {
			h := sortHarness(t, "")

			if code := h.run(append(slices.Clone(v.verb), args...)...); code != 0 {
				t.Fatalf("%v %v: code %d, stderr %q", v.verb, args, code, h.errb.String())
			}

			if got := tableNames(h.out.String()); !slices.Equal(got, v.byName) {
				t.Errorf("%v %v: table lists %v, want %v", v.verb, args, got, v.byName)
			}

			if h.errb.Len() != 0 {
				t.Errorf("%v %v: stderr %q", v.verb, args, h.errb.String())
			}
		}
	}

	h := sortHarness(t, "")
	h.tty = true

	h.run("co", "-l")

	if got := tableNames(h.out.String()); !slices.Equal(got, []string{"api", "Db", "web"}) {
		t.Errorf("co -l on a terminal lists %v", got)
	}
}

// TestListTableFileOrder checks that ui.sort = "file" gives the table of every verb in the order
// of the file, the same names as the tab separated form.
//
// Parameters:
//   - t: test to report to
func TestListTableFileOrder(t *testing.T) {
	for _, v := range sortVerbs {
		h := sortHarness(t, "[ui]\nsort = \"file\"\n")

		h.run(append(slices.Clone(v.verb), "--format", "table")...)

		if got := tableNames(h.out.String()); !slices.Equal(got, v.file) {
			t.Errorf("%v: table lists %v, want the file order %v", v.verb, got, v.file)
		}

		table := h.out.String()

		h.run(append(slices.Clone(v.verb), "--format", "tsv")...)

		if got := tsvNames(h.out.String()); !slices.Equal(got, v.file) {
			t.Errorf("%v: tsv lists %v, want %v", v.verb, got, v.file)
		}

		// The same table as a harness without the key, once that harness is told "file" the
		// old way: the order of the file is the only thing the setting changes.
		other := sortHarness(t, "[ui]\nsort = \"file\"\ntheme = \"dark\"\n")
		other.run(append(slices.Clone(v.verb), "--format", "table")...)

		if other.out.String() != table {
			t.Errorf("%v: another key of [ui] changed the table:\n%s\n%s", v.verb, table,
				other.out.String())
		}
	}
}

// TestListJSONAndTSVKeepFileOrder checks that the two forms that scripts read stay in the order
// of the file, whatever ui.sort says and wherever it comes from.
//
// Parameters:
//   - t: test to report to
func TestListJSONAndTSVKeepFileOrder(t *testing.T) {
	configs := []string{
		"", "[ui]\nsort = \"name\"\n", "[ui]\nsort = \"file\"\n", "[ui]\nsort = \"nope\"\n",
		"[ui]\nsort = \"name\"\nmouse = false\n",
	}

	for _, cfg := range configs {
		for _, v := range sortVerbs {
			h := sortHarness(t, cfg)

			for _, f := range []string{"tsv", "json"} {
				h.run(append(slices.Clone(v.verb), "list", "--format", f)...)

				var got []string

				if f == "tsv" {
					got = tsvNames(h.out.String())
				} else {
					got = jsonNames(t, v.verb[0], h.out.Bytes())
				}

				if !slices.Equal(got, v.file) {
					t.Errorf("config %q, %v %s: %v, want the file order %v", cfg, v.verb, f, got,
						v.file)
				}
			}

			// --json is the same as --format json, and a pipe gets tsv without any option.
			h.run(append(slices.Clone(v.verb), "--json")...)

			if got := jsonNames(t, v.verb[0], h.out.Bytes()); !slices.Equal(got, v.file) {
				t.Errorf("config %q, %v --json: %v, want %v", cfg, v.verb, got, v.file)
			}

			h.run(append(slices.Clone(v.verb), "list")...)

			if got := tsvNames(h.out.String()); !slices.Equal(got, v.file) {
				t.Errorf("config %q, %v piped: %v, want %v", cfg, v.verb, got, v.file)
			}
		}
	}
}

// jsonNames returns the names of the records of a JSON list.
//
// Parameters:
//   - t: running test
//   - verb: first word of the command, which says the kind
//   - data: standard output
func jsonNames(t *testing.T, verb string, data []byte) []string {
	t.Helper()

	var names []string

	switch verb {
	case "co":
		for _, c := range decodeList[connView](t, data, "connections") {
			names = append(names, c.Name)
		}
	case "repos":
		for _, r := range decodeList[repoView](t, data, "repos") {
			names = append(names, r.Name)
		}
	case "alias":
		for _, a := range decodeList[aliasView](t, data, "aliases") {
			names = append(names, a.Name)
		}
	default:
		for _, f := range decodeList[funcView](t, data, "funcs") {
			names = append(names, f.Name)
		}
	}

	return names
}

// TestSortInvalidValue checks that an invalid ui.sort is reported like the other settings, with
// the allowed values, and that the default, the order by name, applies to the table.
//
// Parameters:
//   - t: test to report to
func TestSortInvalidValue(t *testing.T) {
	h := sortHarness(t, "[ui]\nsort = \"size\"\n")

	if code := h.run("co", "list", "--format", "table"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if got := tableNames(h.out.String()); !slices.Equal(got, []string{"api", "Db", "web"}) {
		t.Errorf("table lists %v, want the default order by name", got)
	}

	for _, want := range []string{"config.toml:2", "ui.sort", `"size"`, "name, file"} {
		if !strings.Contains(h.errb.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, h.errb.String())
		}
	}

	h = sortHarness(t, "[ui]\nsort = true\n")
	h.run("co", "list", "--format", "table")

	if got := tableNames(h.out.String()); !slices.Equal(got, []string{"api", "Db", "web"}) ||
		!strings.Contains(h.errb.String(), "ui.sort") {
		t.Errorf("a boolean: table %v, stderr %q", got, h.errb.String())
	}
}

// TestSortNeverRewritesFiles checks that listing, in every format, leaves the data files as they
// were.
//
// Parameters:
//   - t: test to report to
func TestSortNeverRewritesFiles(t *testing.T) {
	h := sortHarness(t, "")

	files := []string{"connections.toml", "repos.toml", "aliases.toml", "funcs.toml"}
	before := make([]string, len(files))

	for i, f := range files {
		before[i] = h.read(f)
	}

	for _, v := range sortVerbs {
		for _, f := range []string{"table", "tsv", "json"} {
			h.run(append(slices.Clone(v.verb), "list", "--format", f)...)
		}
	}

	for i, f := range files {
		if got := h.read(f); got != before[i] {
			t.Errorf("%s changed:\n%s", f, got)
		}
	}
}

// TestSortKeepsNameResolution checks that a name that two connections claim still resolves in
// the order of the file: the message of an ambiguous alias lists them in that order, a new alias
// that clashes names the first owner in the file, and a prefix finds the same connection.
//
// Parameters:
//   - t: test to report to
func TestSortKeepsNameResolution(t *testing.T) {
	for _, cfg := range []string{"", "[ui]\nsort = \"name\"\n", "[ui]\nsort = \"file\"\n"} {
		h := sortHarness(t, cfg)

		if code := h.run("co", "x"); code != exitUsage || len(h.execs) != 0 {
			t.Errorf("config %q: co x: code %d, execs %v", cfg, code, h.execs)
		}

		if want := `"x" is ambiguous: web, api`; !strings.Contains(h.errb.String(), want) {
			t.Errorf("config %q: stderr %q, want it to list %q in the order of the file", cfg,
				h.errb.String(), want)
		}

		h = sortHarness(t, cfg)

		if code := h.run("co", "add", "new", "--target", "n.example.org", "--alias", "x"); code ==
			0 || !strings.Contains(h.errb.String(), `"web"`) ||
			strings.Contains(h.errb.String(), `"api"`) {
			t.Errorf("config %q: co add with a taken alias: code %d, stderr %q", cfg, code,
				h.errb.String())
		}

		h = sortHarness(t, cfg)

		if code := h.run("co", "ap"); code != exitOK || len(h.execs) != 1 ||
			!slices.Contains(h.execs[0], "a.example.org") {
			t.Errorf("config %q: co ap: code %d, execs %v, stderr %q", cfg, code, h.execs,
				h.errb.String())
		}
	}
}

// TestSortKeepsCompletionOrder checks that the candidates of the shell completion stay in the
// order of the file, with and without ui.sort.
//
// Parameters:
//   - t: test to report to
func TestSortKeepsCompletionOrder(t *testing.T) {
	want := []string{"web", "x", "Db", "y", "api"}

	for _, cfg := range []string{"", "[ui]\nsort = \"name\"\n", "[ui]\nsort = \"file\"\n"} {
		h := sortHarness(t, cfg)

		if got := names(completeLines(t, h, "co", "")); !slices.Equal(got, want) {
			t.Errorf("config %q: candidates %v, want %v", cfg, got, want)
		}
	}
}
