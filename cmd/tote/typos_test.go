package main

import (
	"strings"
	"testing"
)

// TestReviewSuggestShortWords checks that a very short word is never matched to a name it shares
// nothing with, and that a longer typo still is.
//
// Parameters:
//   - t: test to report to
func TestReviewSuggestShortWords(t *testing.T) {
	names := []string{"co", "ls", "web1", "alias", "repos"}

	for _, tt := range []struct {
		word string
		want string
		ok   bool
	}{
		{"a", "", false},
		{"x", "", false},
		{"zz", "", false},
		{"ab", "", false},
		{"", "", false},
		{"web2", "web1", true},
		{"aliaz", "alias", true},
		{"alias", "", false},
		{"rep", "repos", true},
		{"reps", "repos", true},
		{"zzzzz", "", false},
		{"repoz", "repos", true},
	} {
		got, ok := suggest(tt.word, names)
		if got != tt.want || ok != tt.ok {
			t.Errorf("suggest(%q) = %q, %v; want %q, %v", tt.word, got, ok, tt.want, tt.ok)
		}
	}
}

// TestReviewSuggestDistanceLimit checks the largest distance that is still offered.
//
// Parameters:
//   - t: test to report to
func TestReviewSuggestDistanceLimit(t *testing.T) {
	if got, ok := suggest("abcdef", []string{"abcdxx"}); !ok || got != "abcdxx" {
		t.Errorf("distance 2: %q, %v", got, ok)
	}

	if got, ok := suggest("abcdef", []string{"abcxxx"}); ok {
		t.Errorf("distance 3 was suggested: %q", got)
	}
}

// TestReviewSuggestNoCandidates checks that an empty candidate list is not an error.
//
// Parameters:
//   - t: test to report to
func TestReviewSuggestNoCandidates(t *testing.T) {
	if got, ok := suggest("anything", nil); ok || got != "" {
		t.Errorf("suggest on nil = %q, %v", got, ok)
	}
}

// TestReviewUnknownShortCommand checks that a one or two letter unknown command gets no
// suggestion built from an unrelated verb.
//
// Parameters:
//   - t: test to report to
func TestReviewUnknownShortCommand(t *testing.T) {
	h := newHarness(t)

	for _, word := range []string{"x", "zz", "q"} {
		if code := h.run(word); code != 2 {
			t.Errorf("%q: code %d, want 2", word, code)
		}

		if got := h.errb.String(); strings.Contains(got, "did you mean") {
			t.Errorf("%q: unexpected suggestion in %q", word, got)
		}
	}
}
