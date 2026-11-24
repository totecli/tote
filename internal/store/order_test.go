package store

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
)

// names lists the names of test records, in order.
//
// Parameters:
//   - recs: records to read
func names(recs []testRec) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Name
	}

	return out
}

// recsOf builds test records from names.
//
// Parameters:
//   - ns: names, in file order
func recsOf(ns ...string) []testRec {
	out := make([]testRec, len(ns))
	for i, n := range ns {
		out[i] = testRec{Name: n}
	}

	return out
}

// TestSortByName checks the order of the display sort on the cases that the setting promises.
func TestSortByName(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{}},
		{"one", []string{"x"}, []string{"x"}},
		{"already sorted", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"reversed", []string{"c", "b", "a"}, []string{"a", "b", "c"}},
		{"case is ignored", []string{"beta", "Alpha", "gamma", "ALPHA2"},
			[]string{"Alpha", "ALPHA2", "beta", "gamma"}},
		{"upper before lower on a tie", []string{"api", "Api", "API"},
			[]string{"API", "Api", "api"}},
		{"digits before letters", []string{"web", "10", "2", "app"},
			[]string{"10", "2", "app", "web"}},
		{"accents are not folded", []string{"zed", "\u00e9cole", "ecole", "Ecole", "eagle"},
			[]string{"eagle", "Ecole", "ecole", "zed", "\u00e9cole"}},
		{"a prefix comes first", []string{"web2", "web", "web-1"},
			[]string{"web", "web-1", "web2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := names(SortByName(recsOf(tt.in...)))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("SortByName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSortByNameKeepsInput checks that the input slice is not reordered and that the result is
// never nil.
func TestSortByNameKeepsInput(t *testing.T) {
	in := recsOf("b", "A", "c")

	out := SortByName(in)

	if got := names(in); !slices.Equal(got, []string{"b", "A", "c"}) {
		t.Fatalf("input was reordered: %q", got)
	}

	if got := names(out); !slices.Equal(got, []string{"A", "b", "c"}) {
		t.Fatalf("output = %q", got)
	}

	if SortByName[testRec](nil) == nil {
		t.Fatal("SortByName(nil) is nil, want an empty slice")
	}
}

// TestSortByNameStable checks that records which compare equal keep the order they came in.
func TestSortByNameStable(t *testing.T) {
	in := []testRec{
		{Name: "dup", Desc: "first"}, {Name: "a"}, {Name: "dup", Desc: "second"},
		{Name: "dup", Desc: "third"},
	}

	out := SortByName(in)

	var descs []string

	for _, r := range out {
		if r.Name == "dup" {
			descs = append(descs, r.Desc)
		}
	}

	if want := []string{"first", "second", "third"}; !slices.Equal(descs, want) {
		t.Fatalf("duplicates came out as %q, want %q", descs, want)
	}

	if out[0].Name != "a" {
		t.Fatalf("first = %q, want a", out[0].Name)
	}
}

// TestNameOrder checks that the permutation is the one SortByName applies and that it is a
// permutation.
func TestNameOrder(t *testing.T) {
	in := recsOf("c", "B", "a", "b")

	order := NameOrder(in)

	if want := []int{2, 1, 3, 0}; !slices.Equal(order, want) {
		t.Fatalf("NameOrder = %v, want %v", order, want)
	}

	if got := NameOrder[testRec](nil); len(got) != 0 {
		t.Fatalf("NameOrder(nil) = %v, want none", got)
	}

	sorted := SortByName(in)
	for i, from := range order {
		if sorted[i].Name != in[from].Name {
			t.Fatalf("position %d: SortByName gave %q, NameOrder points at %q", i,
				sorted[i].Name, in[from].Name)
		}
	}
}

// TestSortByNameLeavesCollectionInFileOrder checks that sorting a copy does not touch the
// collection, whose List stays in file order, and that the file is not rewritten.
func TestSortByNameLeavesCollectionInFileOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rec.toml")
	text := "schema = 1\n\n[[rec]]\nname = \"zeta\"\n\n[[rec]]\nname = \"Alpha\"\n\n" +
		"[[rec]]\nname = \"beta\"\n"

	writeText(t, path, text)

	c, err := Open[testRec](path, testTable)
	if err != nil {
		t.Fatal(err)
	}

	sorted := names(SortByName(c.List()))
	if want := []string{"Alpha", "beta", "zeta"}; !slices.Equal(sorted, want) {
		t.Fatalf("sorted = %q, want %q", sorted, want)
	}

	if got := names(c.List()); !slices.Equal(got, []string{"zeta", "Alpha", "beta"}) {
		t.Fatalf("List = %q, want the file order", got)
	}

	if got := readText(t, path); got != text {
		t.Fatalf("file changed:\n%s", got)
	}
}

// TestSortByNameStableOnManyRecords checks the stability on an input large enough that an
// unstable sort would move equal names: 200 records over three names, each carrying its position.
func TestSortByNameStableOnManyRecords(t *testing.T) {
	in := make([]testRec, 200)
	cycle := []string{"c", "a", "b"}

	for i := range in {
		in[i] = testRec{Name: cycle[i%3], Desc: fmt.Sprintf("%03d", i)}
	}

	last := map[string]string{}

	for _, r := range SortByName(in) {
		if r.Desc <= last[r.Name] {
			t.Fatalf("%q: record %s came after %s, the file order was lost", r.Name, r.Desc,
				last[r.Name])
		}

		last[r.Name] = r.Desc
	}
}
