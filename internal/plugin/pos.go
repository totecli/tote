package plugin

import (
	"strconv"
	"strings"
)

// pos is a 1-based line and column in the manifest text.
type pos struct {
	line int
	col  int
}

// posIndex maps the key paths of a manifest to their position in the text, so that an issue can
// say where it was found. The TOML library reports a position for syntax errors only.
//
// Paths are written as in the issues: table names, array indexes in brackets and keys, such as
// item_action[1].field[0].pattern. The index is built by a small scanner that follows the layout
// of a document the library has already accepted, and it is used to point at a line in a message,
// never to decide anything.
type posIndex struct {
	m map[string]pos
}

// find returns the position of a path, or of its nearest ancestor that was seen.
//
// Parameters:
//   - path: key path such as item_action[1].run[0]; a nil index and an empty path give 0, 0
//
// Returns the 1-based line and column, or 0 and 0.
func (p *posIndex) find(path string) (line, col int) {
	if p == nil {
		return 0, 0
	}

	for path != "" {
		if v, ok := p.m[path]; ok {
			return v.line, v.col
		}

		path = parentPath(path)
	}

	return 0, 0
}

// parentPath removes the last element of a key path: a trailing index, or the last key.
//
// Parameters:
//   - path: non-empty key path
func parentPath(path string) string {
	if strings.HasSuffix(path, "]") {
		if i := strings.LastIndexByte(path, '['); i >= 0 {
			return path[:i]
		}
	}

	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[:i]
	}

	return ""
}

// set records a position unless the path is already known.
//
// Parameters:
//   - path: key path
//   - at: position of the first occurrence
func (p *posIndex) set(path string, at pos) {
	if _, ok := p.m[path]; !ok && path != "" {
		p.m[path] = at
	}
}

// scanner walks the text of a manifest and counts lines.
type scanner struct {
	src       string
	i         int
	line      int
	lineStart int
}

// eof reports whether the whole text was read.
func (s *scanner) eof() bool { return s.i >= len(s.src) }

// peek returns the current byte, or 0 at the end.
func (s *scanner) peek() byte {
	if s.eof() {
		return 0
	}

	return s.src[s.i]
}

// next moves one byte forward and counts the line when it was a line feed.
func (s *scanner) next() {
	if s.eof() {
		return
	}

	if s.src[s.i] == '\n' {
		s.line++
		s.lineStart = s.i + 1
	}

	s.i++
}

// col returns the 1-based column of the current byte.
func (s *scanner) col() int { return s.i - s.lineStart + 1 }

// skipSpace skips spaces and tabs.
func (s *scanner) skipSpace() {
	for s.peek() == ' ' || s.peek() == '\t' {
		s.next()
	}
}

// skipBlank skips white space, line feeds and comments.
func (s *scanner) skipBlank() {
	for !s.eof() {
		switch s.peek() {
		case ' ', '\t', '\r', '\n':
			s.next()
		case '#':
			s.skipToEOL()
		default:
			return
		}
	}
}

// skipToEOL skips the rest of the line, a comment included, and the line feed.
func (s *scanner) skipToEOL() {
	for !s.eof() && s.peek() != '\n' {
		s.next()
	}

	s.next()
}

// indexPositions builds the position index of a manifest text.
//
// Parameters:
//   - src: text that the TOML library has already accepted
func indexPositions(src string) *posIndex {
	idx := &posIndex{m: map[string]pos{}}
	sc := &scanner{src: src, line: 1}
	last := map[string]int{}
	table := ""

	for {
		sc.skipBlank()

		if sc.eof() {
			return idx
		}

		at := pos{sc.line, sc.col()}

		if sc.peek() == '[' {
			table = sc.header(last)
			idx.set(table, at)

			sc.skipToEOL()

			continue
		}

		segs := sc.dottedKey()

		sc.skipSpace()

		if sc.peek() == '=' {
			sc.next()
			sc.skipSpace()
			sc.skipValue()
		}

		for n := 1; n <= len(segs); n++ {
			idx.set(joinPath(table, segs[:n]), at)
		}

		sc.skipToEOL()
	}
}

// joinPath joins a table path and the segments of a key.
//
// Parameters:
//   - table: resolved path of the current table, empty at the root
//   - segs: key segments
func joinPath(table string, segs []string) string {
	path := strings.Join(segs, ".")
	if table == "" {
		return path
	}

	return table + "." + path
}

// header reads a table header and resolves it to an indexed path.
//
// Parameters:
//   - last: latest index of every array of tables seen so far, by resolved path; updated here
//
// Returns the resolved path, such as item_action[1].field[0].
func (s *scanner) header(last map[string]int) string {
	array := strings.HasPrefix(s.src[s.i:], "[[")

	s.next()

	if array {
		s.next()
	}

	segs := s.dottedKey()

	s.skipSpace()

	for s.peek() == ']' {
		s.next()
	}

	resolved := ""

	for i, seg := range segs {
		base := seg
		if resolved != "" {
			base = resolved + "." + seg
		}

		n, known := last[base]

		switch {
		case array && i == len(segs)-1:
			if known {
				n++
			}

			last[base] = n
			resolved = base + "[" + strconv.Itoa(n) + "]"
		case known:
			resolved = base + "[" + strconv.Itoa(n) + "]"
		default:
			resolved = base
		}
	}

	return resolved
}

// dottedKey reads a key that may be dotted and quoted.
//
// Returns the unquoted segments. It stops before "=", "]" or the end of the line.
func (s *scanner) dottedKey() []string {
	var segs []string

	for {
		s.skipSpace()

		before := s.i

		seg := s.keySegment()
		if s.i == before {
			return segs
		}

		segs = append(segs, seg)

		s.skipSpace()

		if s.peek() != '.' {
			return segs
		}

		s.next()
	}
}

// keySegment reads one bare or quoted key segment.
func (s *scanner) keySegment() string {
	switch c := s.peek(); c {
	case '"', '\'':
		s.next()

		start := s.i

		for !s.eof() && s.peek() != c && s.peek() != '\n' {
			if c == '"' && s.peek() == '\\' {
				s.next()
			}

			s.next()
		}

		seg := s.src[start:s.i]

		s.next()

		return seg
	}

	start := s.i

	for !s.eof() && isBareKeyByte(s.peek()) {
		s.next()
	}

	return s.src[start:s.i]
}

// isBareKeyByte reports whether a byte may appear in a bare key.
//
// Parameters:
//   - c: byte to test
func isBareKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' ||
		c == '-'
}

// skipValue skips one TOML value: a string, a multi-line string, an array, an inline table or a
// scalar.
func (s *scanner) skipValue() {
	switch s.peek() {
	case '"', '\'':
		s.skipString()
	case '[', '{':
		s.skipNested()
	default:
		for !s.eof() && s.peek() != '\n' && s.peek() != '#' {
			s.next()
		}
	}
}

// skipString skips a basic, literal or multi-line string.
func (s *scanner) skipString() {
	q := s.peek()
	triple := strings.HasPrefix(s.src[s.i:], strings.Repeat(string(q), 3))

	if triple {
		s.next()
		s.next()
	}

	s.next()

	for !s.eof() {
		c := s.peek()

		switch {
		case c == '\\' && q == '"':
			s.next()
			s.next()
		case c == q && !triple:
			s.next()

			return
		case c == q && strings.HasPrefix(s.src[s.i:], strings.Repeat(string(q), 3)):
			s.next()
			s.next()
			s.next()

			return
		case c == '\n' && !triple:
			return
		default:
			s.next()
		}
	}
}

// skipNested skips an array or an inline table, which may span lines and hold strings and
// comments.
func (s *scanner) skipNested() {
	depth := 0

	for !s.eof() {
		switch s.peek() {
		case '[', '{':
			depth++

			s.next()
		case ']', '}':
			depth--

			s.next()

			if depth <= 0 {
				return
			}
		case '"', '\'':
			s.skipString()
		case '#':
			for !s.eof() && s.peek() != '\n' {
				s.next()
			}
		default:
			s.next()
		}
	}
}
