package store

import (
	"cmp"
	"slices"
	"strings"
)

// SortByName returns the records ordered by name, for display only.
//
// The order is the one of the setting ui.sort = "name": names are compared without regard to
// case with strings.ToLower, which lowers each letter on its own and does not fold accents (a
// name that starts with an accented letter sorts after every name that starts with a plain ASCII
// letter, never next to its base letter). Names that are equal once lowered
// are ordered by the original name, byte by byte, so that "Api" comes before "api". Names that
// are equal in every way, which a collection never holds but a caller may pass, keep their
// position in recs, so that the result is always the same for the same input.
//
// The collections themselves are never sorted: List returns the file order and the files are
// never reordered. Callers that resolve a name, such as "the first one wins when a name clashes",
// keep using that order and sort a copy only to show it.
//
// Parameters:
//   - recs: records in file order; the slice is not changed
//
// Returns a new slice holding the same records, sorted by Key. An empty or nil input gives an
// empty, non-nil slice.
func SortByName[T Record](recs []T) []T {
	order := NameOrder(recs)
	out := make([]T, len(recs))

	for i, from := range order {
		out[i] = recs[from]
	}

	return out
}

// NameOrder returns the positions of the records in the order SortByName shows them.
//
// Parameters:
//   - recs: records in file order
//
// Returns a permutation of 0..len(recs)-1: out[i] is the position in recs of the record that
// comes i-th. Use it when something else, such as a list of prepared rows, must follow the same
// order as the records.
func NameOrder[T Record](recs []T) []int {
	keys := make([]string, len(recs))
	lower := make([]string, len(recs))
	order := make([]int, len(recs))

	for i, r := range recs {
		keys[i] = r.Key()
		lower[i] = strings.ToLower(keys[i])
		order[i] = i
	}

	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Or(
			cmp.Compare(lower[a], lower[b]),
			cmp.Compare(keys[a], keys[b]),
		)
	})

	return order
}
