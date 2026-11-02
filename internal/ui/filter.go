package ui

import (
	"strings"
)

// filter is the text typed after "/" in a list.
//
// Text is kept as runes so that a backspace removes a whole character. Typing is true while the
// filter reads keys; once the user presses Enter the text stays applied but the list gets its
// keys back.
type filter struct {
	text   []rune
	typing bool
}

// String returns the text of the filter.
func (f filter) String() string {
	return string(f.text)
}

// insert appends typed or pasted text to the filter.
//
// Parameters:
//   - s: text to add; it is sanitized and any line break is dropped, so that the filter stays
//     on one line and never holds a control character
func (f *filter) insert(s string) {
	s = sanitize(strings.NewReplacer("\r", "", "\n", "").Replace(s))
	f.text = append(f.text, []rune(s)...)
}

// backspace removes the last character.
func (f *filter) backspace() {
	if len(f.text) > 0 {
		f.text = f.text[:len(f.text)-1]
	}
}

// deleteWord removes the last word and the spaces that follow it.
func (f *filter) deleteWord() {
	n := len(f.text)

	for n > 0 && f.text[n-1] == ' ' {
		n--
	}

	for n > 0 && f.text[n-1] != ' ' {
		n--
	}

	f.text = f.text[:n]
}

// clear empties the filter and stops typing.
func (f *filter) clear() {
	f.text = nil
	f.typing = false
}

// matchItems selects the items that match a query.
//
// Parameters:
//   - items: all items of a kind, in file order
//   - query: text typed by the user, compared without case
//
// Returns the indexes of the matching items: first those whose name or one of whose aliases
// starts with the query, then those that contain it anywhere in their searchable text, each
// group in file order. An empty query returns every index.
func matchItems(items []item, query string) []int {
	q := strings.ToLower(sanitize(query))
	out := make([]int, 0, len(items))

	if q == "" {
		for i := range items {
			out = append(out, i)
		}

		return out
	}

	var rest []int

	for i, it := range items {
		switch {
		case hasAnyPrefix(it.Names, q):
			out = append(out, i)
		case strings.Contains(it.Text, q):
			rest = append(rest, i)
		}
	}

	return append(out, rest...)
}

// hasAnyPrefix reports whether one of the names starts with a prefix.
//
// Parameters:
//   - names: lowercase names
//   - prefix: lowercase prefix
func hasAnyPrefix(names []string, prefix string) bool {
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}

	return false
}
