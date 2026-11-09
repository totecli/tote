package main

import "testing"

// TestLevenshtein checks the edit distance on typical typos.
//
// Parameters:
//   - t: test to report to
func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"kitten", "sitting", 3},
		{"help", "hlep", 2},
		{"conections", "connections", 1},
		{"caf\u00e9", "cafe", 1},
	}

	for _, tt := range tests {
		if got := levenshtein(tt.a, tt.b); got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}

		if got := levenshtein(tt.b, tt.a); got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.b, tt.a, got, tt.want)
		}
	}
}

// TestSuggest checks the threshold, the tie break and the words that match nothing.
//
// Parameters:
//   - t: test to report to
func TestSuggest(t *testing.T) {
	names := []string{"web1", "web2", "database"}

	tests := []struct {
		word string
		want string
		ok   bool
	}{
		{"web", "web1", true},
		{"wbe1", "web1", true},
		{"databse", "database", true},
		{"web1", "web2", true},
		{"zzzzzz", "", false},
		{"ab", "", false},
		{"x", "", false},
		{"", "", false},
		{"databasexxx", "", false},
	}

	for _, tt := range tests {
		got, ok := suggest(tt.word, names)
		if got != tt.want || ok != tt.ok {
			t.Errorf("suggest(%q) = %q, %v; want %q, %v", tt.word, got, ok, tt.want, tt.ok)
		}
	}

	if _, ok := suggest("web", nil); ok {
		t.Error("suggest with no candidates")
	}
}
