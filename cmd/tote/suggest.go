package main

import "unicode/utf8"

// maxSuggestDistance is the largest edit distance at which a name is offered as a suggestion.
const maxSuggestDistance = 2

// levenshtein computes the edit distance between two strings, counted in runes.
//
// Parameters:
//   - a: first string
//   - b: second string
//
// Returns the number of insertions, deletions and substitutions that turn a into b.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		cur[0] = i

		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}

			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}

		prev, cur = cur, prev
	}

	return prev[len(rb)]
}

// suggest finds the candidate closest to a word.
//
// Parameters:
//   - word: what the user typed
//   - candidates: valid names, in the order of preference for equal distances
//
// Returns the closest candidate and true when its distance is at most 2 and smaller than the
// length of the word, so that a word with nothing in common is not matched; else "" and false.
func suggest(word string, candidates []string) (string, bool) {
	best, bestDist := "", min(maxSuggestDistance, utf8.RuneCountInString(word)-1)+1

	for _, c := range candidates {
		if d := levenshtein(word, c); d < bestDist && d > 0 {
			best, bestDist = c, d
		}
	}

	return best, best != ""
}
