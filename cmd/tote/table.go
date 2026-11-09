package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// wideRanges lists the code point ranges that take two terminal cells.
var wideRanges = [][2]rune{
	{0x1100, 0x115F},
	{0x2E80, 0x303E},
	{0x3041, 0xA4CF},
	{0xAC00, 0xD7A3},
	{0xF900, 0xFAFF},
	{0xFE30, 0xFE6F},
	{0xFF00, 0xFF60},
	{0xFFE0, 0xFFE6},
	{0x1F300, 0x1F64F},
	{0x1F900, 0x1F9FF},
	{0x20000, 0x3FFFD},
}

// unsafeRune reports whether a rune could act on the terminal or hide text.
//
// Parameters:
//   - r: rune to test
//
// Returns true for control characters, line and paragraph separators and format characters
// such as bidirectional overrides, except the two joiners used by emoji and some scripts.
func unsafeRune(r rune) bool {
	if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
		return true
	}

	return unicode.Is(unicode.Cf, r) && r != 0x200C && r != 0x200D
}

// sanitize neutralises everything in a stored value that a terminal could act on.
//
// Parameters:
//   - s: value read from a data file, any bytes
//
// Returns s with every control character, escape, unsafe format character and invalid byte
// replaced by a question mark. Nothing is truncated.
func sanitize(s string) string {
	var b strings.Builder

	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if (r == utf8.RuneError && size == 1) || unsafeRune(r) {
			r = '?'
		}

		b.WriteRune(r)
		s = s[size:]
	}

	return b.String()
}

// runeWidth returns the number of terminal cells of a rune.
//
// Parameters:
//   - r: rune to measure
//
// Returns 0 for a combining mark or a joiner, 2 for a wide East Asian or emoji rune, else 1.
func runeWidth(r rune) int {
	if unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf) {
		return 0
	}

	for _, w := range wideRanges {
		if r >= w[0] && r <= w[1] {
			return 2
		}
	}

	return 1
}

// textWidth returns the number of terminal cells of a string.
//
// Parameters:
//   - s: text to measure
func textWidth(s string) int {
	n := 0

	for _, r := range s {
		n += runeWidth(r)
	}

	return n
}

// writeTable prints rows as columns aligned by display width, two spaces apart.
//
// Parameters:
//   - w: destination
//   - heads: header cells, printed first
//   - rows: data rows, each as long as heads; every cell is sanitized here
func writeTable(w io.Writer, heads []string, rows [][]string) {
	all := make([][]string, 0, len(rows)+1)
	all = append(all, cleanRow(heads))

	for _, r := range rows {
		all = append(all, cleanRow(r))
	}

	widths := make([]int, len(heads))

	for _, r := range all {
		for i, c := range r {
			widths[i] = max(widths[i], textWidth(c))
		}
	}

	for _, r := range all {
		var b strings.Builder

		for i, c := range r {
			b.WriteString(c)
			b.WriteString(strings.Repeat(" ", widths[i]-textWidth(c)+2))
		}

		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

// cleanRow sanitizes every cell of a row.
//
// Parameters:
//   - row: cells to clean
//
// Returns a new slice.
func cleanRow(row []string) []string {
	out := make([]string, len(row))

	for i, c := range row {
		out[i] = sanitize(c)
	}

	return out
}

// tsvCell escapes one cell of a tab separated line.
//
// Parameters:
//   - s: cell text
//
// Returns s with tab, newline and carriage return written as \t, \n and \r and any other
// dangerous character replaced by a question mark.
func tsvCell(s string) string {
	s = strings.NewReplacer("\t", `\t`, "\n", `\n`, "\r", `\r`).Replace(s)

	return sanitize(s)
}

// writeTSV prints rows as tab separated lines without a header.
//
// Parameters:
//   - w: destination
//   - rows: data rows
func writeTSV(w io.Writer, rows [][]string) {
	for _, r := range rows {
		cells := make([]string, len(r))

		for i, c := range r {
			cells[i] = tsvCell(c)
		}

		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
}

// escapeJSON rewrites the runes of encoded JSON that a terminal could act on as \u escapes.
//
// Parameters:
//   - data: output of encoding/json, which already escapes the ASCII controls
//
// Returns data where every rune for which unsafeRune is true, except the newline between two
// tokens, is written as \uXXXX, with a surrogate pair above U+FFFF.
func escapeJSON(data []byte) []byte {
	var b bytes.Buffer

	for _, r := range string(data) {
		switch {
		case r == '\n' || !unsafeRune(r):
			b.WriteRune(r)
		case r > 0xFFFF:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, hi, lo)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}

	return b.Bytes()
}

// writeJSON prints a value as indented JSON followed by a newline.
//
// Parameters:
//   - w: destination
//   - v: value to encode
//
// Returns the encoding or write error.
func writeJSON(w io.Writer, v any) error {
	var b bytes.Buffer

	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(v); err != nil {
		return err
	}

	_, err := w.Write(escapeJSON(b.Bytes()))

	return err
}
