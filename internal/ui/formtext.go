package ui

import "strings"

// wrapWords breaks a text into lines of at most a width, at the spaces.
//
// Parameters:
//   - e: shared environment, for the width rule and the ellipsis
//   - s: plain text
//   - w: cells per line; zero or less gives no line
//   - maxLines: most lines returned; when the text needs more, the last line ends with an
//     ellipsis
//
// Returns at least one line for a text that is not empty. Only the ASCII space separates words,
// so that a non-breaking space keeps a "?" or a "!" with the word before it. A word wider than w
// is cut.
func wrapWords(e *env, s string, w, maxLines int) []string {
	if w <= 0 || maxLines <= 0 || strings.TrimSpace(s) == "" {
		return nil
	}

	var lines []string

	cur := ""

	for _, word := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' }) {
		for e.Metric.width(word) > w {
			head := e.Metric.cut(word, w)
			if head == "" {
				break
			}

			if cur != "" {
				lines, cur = append(lines, cur), ""
			}

			lines = append(lines, head)
			word = word[len(head):]
		}

		switch {
		case cur == "":
			cur = word
		case e.Metric.width(cur)+1+e.Metric.width(word) <= w:
			cur += " " + word
		default:
			lines, cur = append(lines, cur), word
		}
	}

	if cur != "" {
		lines = append(lines, cur)
	}

	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = e.Metric.fit(lines[maxLines-1]+" "+e.Glyphs.ellipsis, w,
			e.Glyphs.ellipsis)
	}

	return lines
}
