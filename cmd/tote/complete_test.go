package main

import (
	"slices"
	"strings"
	"testing"
)

// completeLines runs __complete and returns its output split by line.
//
// Parameters:
//   - t: running test
//   - h: harness to run on
//   - words: words after tote, the last one being the current word
func completeLines(t *testing.T, h *harness, words ...string) []string {
	t.Helper()

	if code := h.run(append([]string{"__complete"}, words...)...); code != 0 {
		t.Fatalf("__complete %q: code %d", words, code)
	}

	if h.errb.Len() != 0 {
		t.Errorf("__complete %q: stderr %q", words, h.errb.String())
	}

	if h.out.Len() == 0 {
		return nil
	}

	return strings.Split(strings.TrimSuffix(h.out.String(), "\n"), "\n")
}

// names strips the descriptions from completion lines.
//
// Parameters:
//   - lines: completion lines
func names(lines []string) []string {
	out := make([]string, len(lines))

	for i, l := range lines {
		out[i], _, _ = strings.Cut(l, "\t")
	}

	return out
}

// TestCompleteVerbs checks the verbs offered for the first word.
//
// Parameters:
//   - t: test to report to
func TestCompleteVerbs(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	all := names(completeLines(t, h, ""))
	wantVerbs := []string{
		"help", "version", "init", "co", "connections", "alias", "repos", "funcs", "config",
	}

	for _, want := range wantVerbs {
		if !slices.Contains(all, want) {
			t.Errorf("verbs lack %q: %v", want, all)
		}
	}

	if slices.Contains(all, "__complete") || slices.Contains(all, "a") {
		t.Errorf("hidden or one letter verb offered: %v", all)
	}

	wantCon := []string{"conn", "connections", "connexions", "config"}
	if got := names(completeLines(t, h, "con")); !slices.Equal(got, wantCon) {
		t.Errorf("con = %v", got)
	}

	if got := completeLines(t, h); len(got) == 0 {
		t.Error("no words must complete like an empty word")
	}

	for _, line := range completeLines(t, h, "ver") {
		if line != "version\tPrint the version" {
			t.Errorf("line = %q", line)
		}
	}

	if got := completeLines(t, h, "zzz"); got != nil {
		t.Errorf("zzz = %v", got)
	}
}

// TestCompleteConnections checks the names and aliases offered after the connections verb, and
// that nothing is offered anywhere else.
//
// Parameters:
//   - t: test to report to
func TestCompleteConnections(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	got := completeLines(t, h, "co", "")
	want := []string{
		"web1\tWeb front", "w\tWeb front", "web\tWeb front",
		"db1\tdb1.example.org", "db\tdb1.example.org", "v6\t[2001:db8::1]",
	}

	if !slices.Equal(got, want) {
		t.Errorf("co '' = %q, want %q", got, want)
	}

	gotW := names(completeLines(t, h, "connections", "W"))
	if !slices.Equal(gotW, []string{"web1", "w", "web"}) {
		t.Errorf("connections W = %v", gotW)
	}

	for _, words := range [][]string{
		{"alias", ""},
		{"repos", ""},
		{"co", "web1", ""},
		{"co", "add", ""},
		{"init", ""},
		{"nope", ""},
		{"help", ""},
		{"co", "web1", "x", ""},
	} {
		if got := completeLines(t, h, words...); got != nil {
			t.Errorf("%q = %q, want nothing", words, got)
		}
	}
}

// TestCompleteListOptions checks the options offered after a list verb: only once a dash is
// typed, never after a name, and --format and --json never together.
//
// Parameters:
//   - t: test to report to
func TestCompleteListOptions(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	tests := []struct {
		words []string
		want  []string
	}{
		{[]string{"alias", "-"}, []string{"--format", "--json"}},
		{[]string{"repos", "list", "--j"}, []string{"--json"}},
		{[]string{"funcs", "--json", "-"}, nil},
		{[]string{"funcs", "--format", "-"}, nil},
		{[]string{"co", "--"}, []string{"--format", "--json"}},
		{[]string{"connections", "ls", "--f"}, []string{"--format"}},
		{[]string{"co", "web1", "--"}, nil},
		{[]string{"alias", ""}, nil},
		{[]string{"co", "w", "-"}, nil},
	}

	for _, tt := range tests {
		if got := names(completeLines(t, h, tt.words...)); !slices.Equal(got, tt.want) &&
			len(got)+len(tt.want) > 0 {
			t.Errorf("%q = %q, want %q", tt.words, got, tt.want)
		}
	}
}

// TestCompleteSkipsBadRecords checks that invalid and unreviewed records are never offered, that
// control characters are removed and that a broken file gives nothing.
//
// Parameters:
//   - t: test to report to
func TestCompleteSkipsBadRecords(t *testing.T) {
	h := newHarness(t)
	h.write("connections.toml", `schema = 1

[[connection]]
name = "good"
target = "h"
desc = "fine"

[[connection]]
name = "bad name"
target = "h"

[[connection]]
name = "review"
target = "h"
needs_review = true

[[connection]]
name = "badtarget"
target = "-x"
`)

	got := completeLines(t, h, "co", "")
	if len(got) != 1 || got[0] != "good\tfine" {
		t.Errorf("lines = %q", got)
	}

	h.write("connections.toml", "schema = 1\n[[connection]\n")

	if got := completeLines(t, h, "co", ""); got != nil {
		t.Errorf("broken file gave %q", got)
	}

	h.vars["TOTE_CONFIG_DIR"] = "relative"

	if got := completeLines(t, h, "co", ""); got != nil {
		t.Errorf("bad paths gave %q", got)
	}
}

// TestCompleteFrench checks that descriptions follow the language.
//
// Parameters:
//   - t: test to report to
func TestCompleteFrench(t *testing.T) {
	h := newHarness(t)
	h.vars["TOTE_LANG"] = "fr"

	for _, line := range completeLines(t, h, "vers") {
		if line != "version\tAfficher la version" {
			t.Errorf("line = %q", line)
		}
	}
}

// TestCandidate checks the sanitizing of one completion line.
//
// Parameters:
//   - t: test to report to
func TestCandidate(t *testing.T) {
	if got := candidate("a\nb", ""); got != "a?b" {
		t.Errorf("got %q", got)
	}

	if got := candidate("n", "d\te\x1b"); got != "n\td?e?" {
		t.Errorf("got %q", got)
	}
}
