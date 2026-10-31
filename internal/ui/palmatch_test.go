package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/config"
)

// mkEntry builds a record entry for the ranking tests.
//
// Parameters:
//   - sc: scope of the entry
//   - name: display name; it is also the first searchable name
//   - extra: more searchable text, such as a target or a description
//   - aliases: more names that a prefix matches
func mkEntry(sc scope, name, extra string, aliases ...string) entry {
	names := append([]string{strings.ToLower(name)}, aliases...)
	text := strings.ToLower(strings.Join(append([]string{name, extra}, aliases...), " "))

	return entry{Scope: sc, Name: name, Key: name, Names: names, Text: text}
}

// namesOf returns the names of the entries picked by hits.
//
// Parameters:
//   - es: entries
//   - hits: indexes into es
func namesOf(es []entry, hits []int) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = es[h].Name
	}

	return out
}

// TestParseScope checks the prefixes of the palette.
//
// Parameters:
//   - t: test to report to
func TestParseScope(t *testing.T) {
	tests := []struct {
		in   string
		sc   scope
		rest string
	}{
		{"", scopeAll, ""},
		{"prod", scopeAll, "prod"},
		{"c prod", scopeConn, "prod"},
		{"C prod", scopeConn, "prod"},
		{"r  proj", scopeRepo, "proj"},
		{"a ll", scopeAlias, "ll"},
		{"d hello", scopeFunc, "hello"},
		{"c ", scopeConn, ""},
		{"c", scopeAll, "c"},
		{"cc prod", scopeAll, "cc prod"},
		{"x prod", scopeAll, "x prod"},
		{"1 prod", scopeAll, "1 prod"},
		{"@ prod", scopeAll, "@ prod"},
		{">", scopeAction, ""},
		{">add", scopeAction, "add"},
		{">  add c", scopeAction, "add c"},
		{" c prod", scopeAll, " c prod"},
		{"\u00e9 prod", scopeAll, "\u00e9 prod"},
	}

	for _, tt := range tests {
		sc, rest := parseScope(tt.in)
		if sc != tt.sc || rest != tt.rest {
			t.Errorf("parseScope(%q) = %v, %q; want %v, %q", tt.in, sc, rest, tt.sc, tt.rest)
		}
	}
}

// TestRank checks the order of the results: exact, prefix, word start, substring, other text,
// then subsequence, and the scopes.
//
// Parameters:
//   - t: test to report to
func TestRank(t *testing.T) {
	es := []entry{
		mkEntry(scopeConn, "web-prod-2", "10.0.0.2"),
		mkEntry(scopeConn, "prod", "prod.example.com", "p"),
		mkEntry(scopeConn, "staging", "stg.example.com production like"),
		mkEntry(scopeRepo, "myprod", ""),
		mkEntry(scopeAlias, "ll", "ls -la"),
		mkEntry(scopeAlias, "prodlog", "tail -f"),
		mkEntry(scopeFunc, "pr-open", "open a pull request"),
		mkEntry(scopeAction, "Add a connection", "add a connection"),
		mkEntry(scopeAction, "Quit tote", "quit tote"),
	}

	tests := []struct {
		name  string
		sc    scope
		query string
		want  []string
	}{
		{
			"exact then prefix then word then substring then text", scopeAll, "prod",
			[]string{"prod", "prodlog", "web-prod-2", "myprod", "staging"},
		},
		{"alias counts as a name", scopeAll, "p", []string{
			"prod", "prodlog", "pr-open",
			"web-prod-2", "myprod", "staging",
		}},
		{"case is ignored", scopeAll, "PROD", []string{
			"prod", "prodlog", "web-prod-2", "myprod",
			"staging",
		}},
		{"scope connections", scopeConn, "prod", []string{"prod", "web-prod-2", "staging"}},
		{"scope aliases", scopeAlias, "", []string{"ll", "prodlog"}},
		{"scope actions", scopeAction, "add", []string{"Add a connection"}},
		{"scope actions, subsequence", scopeAction, "qt", []string{"Quit tote"}},
		{"subsequence", scopeAll, "pdl", []string{"prodlog"}},
		{"second word narrows", scopeAll, "prod 10.0", []string{"web-prod-2"}},
		{"second word must exist", scopeAll, "prod nothing", nil},
		{"no match", scopeAll, "zzz", nil},
		{"text only", scopeAll, "pull", []string{"pr-open"}},
		{"one letter is not a subsequence query", scopeAll, "z", nil},
		{"hostile query is neutralized", scopeAll, "prod\x1b[31m", nil},
	}

	for _, tt := range tests {
		got := namesOf(es, rank(es, tt.sc, tt.query))
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: rank(%q) = %q, want %q", tt.name, tt.query, got, tt.want)
		}
	}

	// An empty query keeps the original order, records first and actions last.
	all := namesOf(es, rank(es, scopeAll, ""))
	if len(all) != len(es) || all[0] != "web-prod-2" || all[len(all)-1] != "Quit tote" {
		t.Errorf("empty query = %q", all)
	}
}

// TestRankTies checks that equal ranks are ordered by name length, then by original order.
//
// Parameters:
//   - t: test to report to
func TestRankTies(t *testing.T) {
	es := []entry{
		mkEntry(scopeConn, "srv-long-name", ""),
		mkEntry(scopeConn, "srv-b", ""),
		mkEntry(scopeConn, "srv-a", ""),
		mkEntry(scopeConn, "srv", ""),
	}

	got := namesOf(es, rank(es, scopeAll, "sr"))
	want := []string{"srv", "srv-b", "srv-a", "srv-long-name"}

	if !slices.Equal(got, want) {
		t.Errorf("ties = %q, want %q", got, want)
	}
}

// TestNameTier checks the tiers of a single name.
//
// Parameters:
//   - t: test to report to
func TestNameTier(t *testing.T) {
	tests := []struct {
		name, q string
		want    int
	}{
		{"prod", "prod", tierExact},
		{"prod-web", "prod", tierPrefix},
		{"web-prod", "prod", tierWord},
		{"web_prod", "prod", tierWord},
		{"web.prod", "prod", tierWord},
		{"webprod", "prod", tierSubstring},
		{"webprodprod", "prod", tierSubstring},
		{"webprod-prod", "prod", tierWord},
		{"web", "prod", -1},
		{"", "a", -1},
	}

	for _, tt := range tests {
		if got := nameTier(tt.name, tt.q); got != tt.want {
			t.Errorf("nameTier(%q, %q) = %d, want %d", tt.name, tt.q, got, tt.want)
		}
	}
}

// TestSubsequence checks the in-order letter match, including multi-byte letters.
//
// Parameters:
//   - t: test to report to
func TestSubsequence(t *testing.T) {
	tests := []struct {
		s, q string
		want bool
	}{
		{"prodlog", "pdl", true},
		{"prodlog", "pld", false},
		{"prodlog", "", true},
		{"", "a", false},
		{"caf\u00e9-pro", "cfp", true},
		{"caf\u00e9-pro", "\u00e9p", true},
		{"\u65e5\u672c\u8a9e", "\u65e5\u8a9e", true},
	}

	for _, tt := range tests {
		if got := subsequence(tt.s, tt.q); got != tt.want {
			t.Errorf("subsequence(%q, %q) = %v, want %v", tt.s, tt.q, got, tt.want)
		}
	}
}

// bigEntries builds many synthetic entries.
//
// Parameters:
//   - n: number of entries
func bigEntries(n int) []entry {
	es := make([]entry, 0, n)

	for i := range n {
		sc := []scope{scopeConn, scopeRepo, scopeAlias, scopeFunc}[i%4]
		es = append(es, mkEntry(sc, fmt.Sprintf("host-%05d-node", i),
			fmt.Sprintf("10.%d.%d.%d some description of %d", i/65025, i/255%255, i%255, i)))
	}

	return es
}

// rankUnits is the most a query over five thousand entries may cost, in units of referenceWork
// measured at the same time; see TestRankBig.
const rankUnits = 60

// rankAttempts is the number of times TestRankBig measures before it fails.
const rankAttempts = 4

// TestRankBig checks the results on five thousand entries and that a pass stays fast, so that
// the filter stays interactive on a list that size.
//
// A clock on the wall counts the time of the other processes, and the race detector multiplies
// the cost of the code, so the limit is not a duration: it is a number of times a fixed piece of
// work that does not use the interface (referenceWork), measured at the same moment, which
// follows the speed and the load of the machine. The test is given rankAttempts tries and fails
// only when every one is over the limit.
//
// Parameters:
//   - t: test to report to
func TestRankBig(t *testing.T) {
	es := bigEntries(5000)

	if got := namesOf(es, rank(es, scopeAll, "host-04999")); len(got) != 1 ||
		got[0] != "host-04999-node" {
		t.Errorf("one host = %q", got)
	}

	if n := len(rank(es, scopeAll, "")); n != 5000 {
		t.Errorf("empty query gives %d", n)
	}

	if n := len(rank(es, scopeRepo, "node")); n != 1250 {
		t.Errorf("repos named node = %d, want 1250", n)
	}

	if n := len(rank(es, scopeAll, "hn")); n != 5000 {
		t.Errorf("subsequence hn = %d, want 5000", n)
	}

	queries := []string{"h", "ho", "host", "node", "7", "10.0", "zz", "hst0", "description of 4"}

	var per, unit time.Duration

	for try := 1; try <= rankAttempts; try++ {
		// The best of several samples, so that a busy machine does not count: see bestMean.
		per = bestMean(12, 1, func() {
			for _, q := range queries {
				rank(es, scopeAll, q)
			}
		}) / time.Duration(len(queries))

		unit = bestMean(12, 12, referenceWork)

		t.Logf("try %d: a pass over 5000 entries: %v, the reference work: %v", try, per, unit)

		if per <= rankUnits*unit {
			return
		}
	}

	t.Errorf("a pass over 5000 entries took %v, want at most %d times the reference work (%v)",
		per, rankUnits, unit)
}

// BenchmarkRank measures one filter pass over five thousand entries.
//
// Parameters:
//   - b: benchmark to run
func BenchmarkRank(b *testing.B) {
	es := bigEntries(5000)

	for _, q := range []string{"h", "host", "node-0", "hst0", "10.0", "zz"} {
		b.Run(q, func(b *testing.B) {
			for range b.N {
				rank(es, scopeAll, q)
			}
		})
	}
}

// BenchmarkBuildEntries measures the opening of the palette on five thousand records.
//
// Parameters:
//   - b: benchmark to run
func BenchmarkBuildEntries(b *testing.B) {
	dir := b.TempDir()
	paths := config.Paths{Config: dir, Data: dir, State: dir, Cache: dir, Runtime: dir}

	var sb strings.Builder

	sb.WriteString("schema = 1\n")

	for i := range 5000 {
		fmt.Fprintf(&sb, "\n[[alias]]\nname = \"alias%d\"\ncmd = \"echo %d\"\n", i, i)
	}

	if err := os.WriteFile(paths.AliasesFile(), []byte(sb.String()), 0o600); err != nil {
		b.Fatal(err)
	}

	tr := newPrinter("en")
	e := &env{Tr: tr, Data: newDataset(tr, paths, dir), Metric: newMetric()}

	b.ResetTimer()

	for range b.N {
		buildEntries(e)
	}
}

// TestSubOf checks the detail column of each kind.
//
// Parameters:
//   - t: test to report to
func TestSubOf(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e

	tests := []struct {
		k    kind
		key  string
		want string
	}{
		{kindConn, "prod", "prod.example.com"},
		{kindRepo, "proj", g.r.e.Data.items(kindRepo)[0].Cells[1]},
		{kindAlias, "ll", "ls -la"},
		{kindFunc, "tool", "A tool"},
		{kindFunc, "hello", "source"},
	}

	for _, tt := range tests {
		for _, it := range e.Data.items(tt.k) {
			if it.Key == tt.key {
				if got := subOf(&it); got != tt.want {
					t.Errorf("subOf(%s) = %q, want %q", tt.key, got, tt.want)
				}
			}
		}
	}

	if got := subOf(&item{}); got != "" {
		t.Errorf("subOf of an empty item = %q", got)
	}
}

// mkCatEntry builds a record entry drawn under a category, as buildEntries does.
//
// Parameters:
//   - sc: scope of the entry
//   - cat: category label, such as "Connections"
//   - name: display name
//   - extra: more searchable text
func mkCatEntry(sc scope, cat, name, extra string) entry {
	en := mkEntry(sc, name, extra)
	en.Cat, en.Low = cat, strings.ToLower(cat)
	en.Text += " " + en.Low

	return en
}

// TestRankCategory checks that a query that starts the category of a record finds every record
// of the category, in the order of the file, behind the names that start with the query and
// before the other words.
//
// Parameters:
//   - t: test to report to
func TestRankCategory(t *testing.T) {
	es := []entry{
		mkCatEntry(scopeConn, "Connections", "prod", "prod.example.com"),
		mkCatEntry(scopeConn, "Connections", "staging", ""),
		mkCatEntry(scopeConn, "Connections", "nas", ""),
		mkCatEntry(scopeRepo, "Repos", "tote", ""),
		mkCatEntry(scopeAlias, "Aliases", "connect-vpn", "vpn up"),
		mkEntry(scopeAction, "New connection", ""),
		mkEntry(scopeAction, "Go to\u2026 Repos", ""),
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"category keeps the order of the file", "conn", []string{
			"connect-vpn", "prod",
			"staging", "nas", "New connection",
		}},
		{"a longer prefix still finds the category", "connections", []string{
			"prod", "staging",
			"nas",
		}},
		{"category then the word of an action", "repos", []string{"tote", "Go to\u2026 Repos"}},
		{"second word narrows the category", "conn prod", []string{"prod"}},
		{
			"the middle of a category is only a text match, ordered by name", "nections",
			[]string{"nas", "prod", "staging"},
		},
	}

	for _, tt := range tests {
		got := namesOf(es, rank(es, scopeAll, tt.query))
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: rank(%q) = %q, want %q", tt.name, tt.query, got, tt.want)
		}
	}
}

// TestActionLabels checks the wording of the actions of the palette in both languages: creation
// reads like the forms and navigation reads "Go to... Repos", as in the mockups.
//
// Parameters:
//   - t: test to report to
func TestActionLabels(t *testing.T) {
	want := map[string][]string{
		"en": {
			"New connection", "New repository", "New alias", "New function",
			"Go to\u2026 Connections", "Go to\u2026 Repos", "Go to\u2026 Aliases",
			"Go to\u2026 Functions", "Go to\u2026 Home",
		},
		"fr": {
			"Nouvelle connexion", "Nouveau d\u00e9p\u00f4t", "Nouvel alias",
			"Nouvelle fonction", "Aller \u00e0\u2026 Connexions",
			"Aller \u00e0\u2026 D\u00e9p\u00f4ts", "Aller \u00e0\u2026 Alias",
			"Aller \u00e0\u2026 Fonctions", "Aller \u00e0\u2026 Accueil",
		},
	}

	for lang, labels := range want {
		s := newSandbox(t)
		s.Opts.Printer = newPrinter(lang)
		g := newRig(t, s)
		var have []string
		nav := 0

		for _, en := range buildEntries(g.r.e) {
			if en.Scope != scopeAction {
				continue
			}

			have = append(have, en.Name)

			if en.Nav {
				nav++
			}
		}

		for _, l := range labels {
			if !slices.Contains(have, l) {
				t.Errorf("%s: no action %q in %q", lang, l, have)
			}
		}

		if nav != 5 {
			t.Errorf("%s: %d navigation actions, want 5", lang, nav)
		}
	}
}

// TestEntriesCarryTheirCategory checks that the records of the palette know the category they
// are drawn under, in the language of the interface, and that "conn" finds them through it.
//
// Parameters:
//   - t: test to report to
func TestEntriesCarryTheirCategory(t *testing.T) {
	g := newRig(t, newSandbox(t))
	es := buildEntries(g.r.e)

	cats := map[scope]string{
		scopeConn: "Connections", scopeRepo: "Repos",
		scopeAlias: "Aliases", scopeFunc: "Functions", scopeAction: "",
	}

	for _, en := range es {
		if en.Cat != cats[en.Scope] || en.Low != strings.ToLower(en.Cat) {
			t.Errorf("entry %q has category %q (%q), want %q", en.Name, en.Cat, en.Low,
				cats[en.Scope])
		}
	}

	got := 0

	for _, i := range rank(es, scopeAll, "conn") {
		if es[i].Scope == scopeConn {
			got++
		}
	}

	if got != 6 {
		t.Errorf("conn finds %d connections, want 6", got)
	}
}
