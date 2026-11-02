package ui

import (
	"slices"
	"testing"
)

// TestFilterEditing checks the editing operations of the filter text.
//
// Parameters:
//   - t: test to report to
func TestFilterEditing(t *testing.T) {
	var f filter

	f.insert("hello world")

	if f.String() != "hello world" {
		t.Fatalf("after insert = %q", f.String())
	}

	f.deleteWord()

	if f.String() != "hello " {
		t.Errorf("after deleteWord = %q, want %q", f.String(), "hello ")
	}

	f.deleteWord()

	if f.String() != "" {
		t.Errorf("after the second deleteWord = %q, want empty", f.String())
	}

	f.insert("caf\u00E9")
	f.backspace()

	if f.String() != "caf" {
		t.Errorf("backspace removed a byte instead of a character: %q", f.String())
	}

	f.typing = true
	f.clear()

	if f.String() != "" || f.typing {
		t.Errorf("clear left %q typing %v", f.String(), f.typing)
	}

	f.backspace()
	f.deleteWord()
}

// TestFilterInsertIsSafe checks that pasted text cannot hold a line break or a control
// character.
//
// Parameters:
//   - t: test to report to
func TestFilterInsertIsSafe(t *testing.T) {
	var f filter

	f.insert("a\nb\r\nc\x1b[31md\te")

	if got := f.String(); got != "abc?[31md?e" {
		t.Errorf("filter = %q, want %q", got, "abc?[31md?e")
	}
}

// TestMatchItems checks the selection and the order of the matches.
//
// Parameters:
//   - t: test to report to
func TestMatchItems(t *testing.T) {
	items := []item{
		{Key: "web-prod", Names: []string{"web-prod", "w"}, Text: "web-prod 10.0.0.1 front"},
		{Key: "prod-db", Names: []string{"prod-db"}, Text: "prod-db db.example.com"},
		{Key: "nas", Names: []string{"nas", "storage"}, Text: "nas 192.168.1.20 home production"},
		{Key: "other", Names: []string{"other"}, Text: "other o.example.com"},
	}

	tests := []struct {
		name  string
		query string
		want  []int
	}{
		{"empty", "", []int{0, 1, 2, 3}},
		{"prefix before substring", "prod", []int{1, 0, 2}},
		{"alias prefix", "stor", []int{2}},
		{"alias exact", "w", []int{0}},
		{"case", "PROD-DB", []int{1}},
		{"substring only", "example", []int{1, 3}},
		{"no match", "zzz", []int{}},
		{"sanitized query", "pr\x1bod", []int{}},
	}

	for _, tt := range tests {
		got := matchItems(items, tt.query)

		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: matchItems(%q) = %v, want %v", tt.name, tt.query, got, tt.want)
		}
	}

	if got := matchItems(nil, "x"); len(got) != 0 {
		t.Errorf("matchItems on nothing = %v", got)
	}
}

// TestHasAnyPrefix checks the prefix search over names.
//
// Parameters:
//   - t: test to report to
func TestHasAnyPrefix(t *testing.T) {
	if !hasAnyPrefix([]string{"abc", "xyz"}, "xy") || hasAnyPrefix([]string{"abc"}, "bc") ||
		hasAnyPrefix(nil, "a") {
		t.Error("hasAnyPrefix gave a wrong answer")
	}
}
