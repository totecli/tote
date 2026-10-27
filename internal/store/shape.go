package store

import (
	"bytes"
	"fmt"
)

// ShapeLimits bounds the structure of a TOML text that is checked before it is decoded.
//
// The TOML library recurses on nested values and builds one map per part of a dotted key, so a
// short hostile text can cost gigabytes: a table header of 10000 parts allocates 1.2 GB, a dotted
// key or an inline table nested 1000 deep tens of megabytes. A limit of zero disables its check.
// MaxBytes is the size of the whole text, MaxLineBytes the longest line, MaxNesting the depth of
// arrays and inline tables inside a value, MaxKeyParts the number of parts of a dotted key or of
// a table header, MaxHeaders the number of table headers and MaxKeys the number of keys, inline
// table entries included.
type ShapeLimits struct {
	MaxBytes     int
	MaxLineBytes int
	MaxNesting   int
	MaxKeyParts  int
	MaxHeaders   int
	MaxKeys      int
}

// ShapeUser is the preset for the files a user edits by hand: only the depth checks that keep the
// decoder away from the quadratic cases, with room for any sensible file.
var ShapeUser = ShapeLimits{MaxNesting: 32, MaxKeyParts: 32}

// The names of the limit reported by a ShapeError.
const (
	ShapeBytes    = "bytes"
	ShapeLine     = "line"
	ShapeNesting  = "nesting"
	ShapeKeyParts = "key_parts"
	ShapeHeaders  = "headers"
	ShapeKeys     = "keys"
)

// ShapeError reports the first limit that a TOML text goes beyond.
//
// Limit is one of the Shape constants, Max the value of the limit and Line the 1-based line where
// the limit was crossed, 0 for the size of the whole text.
type ShapeError struct {
	Limit string
	Max   int
	Line  int
}

// Error implements error.
func (e *ShapeError) Error() string {
	return fmt.Sprintf("too much structure: %s beyond %d", e.Limit, e.Max)
}

// CheckShape scans a TOML text in linear time and constant memory and refuses the structures the
// decoder cannot handle cheaply. It does not validate the text: a syntax error is left to the
// decoder, which meets it before any deep recursion.
//
// The scan follows the string rules of TOML (basic, literal and multi-line strings, escapes,
// comments), so a bracket or a dot inside a string or a comment counts for nothing. A text that
// is invalid because a string is unterminated is read as the decoder would read it, up to the end
// of the line for a single-line string.
//
// Parameters:
//   - data: TOML text, any bytes
//   - lim: limits to apply, zero fields being unchecked
//
// Returns nil when the text is within the limits, else the first limit crossed.
func CheckShape(data []byte, lim ShapeLimits) *ShapeError {
	if lim.MaxBytes > 0 && len(data) > lim.MaxBytes {
		return &ShapeError{Limit: ShapeBytes, Max: lim.MaxBytes}
	}

	if lim.MaxLineBytes > 0 {
		if e := longLine(data, lim.MaxLineBytes); e != nil {
			return e
		}
	}

	s := &shapeScan{src: data, lim: lim}
	for s.i < len(s.src) {
		if e := s.statement(); e != nil {
			return e
		}
	}

	return nil
}

// longLine looks for a line above a length.
//
// Parameters:
//   - data: text to read
//   - limit: longest line allowed, line feed excluded
func longLine(data []byte, limit int) *ShapeError {
	line := 1

	for len(data) > 0 {
		n := bytes.IndexByte(data, '\n')
		if n < 0 {
			n = len(data)
		}

		if n > limit {
			return &ShapeError{Limit: ShapeLine, Max: limit, Line: line}
		}

		if n == len(data) {
			break
		}

		data = data[n+1:]
		line++
	}

	return nil
}

// shapeScan is the state of one CheckShape run: the text, the position, the limits and the counts
// of headers and keys seen so far.
type shapeScan struct {
	src     []byte
	i       int
	lim     ShapeLimits
	headers int
	keys    int
}

// fail builds the error for a limit crossed at the current position.
//
// Parameters:
//   - limit: name of the limit
//   - bound: value of the limit
func (s *shapeScan) fail(limit string, bound int) *ShapeError {
	end := min(s.i, len(s.src))

	return &ShapeError{Limit: limit, Max: bound, Line: 1 + bytes.Count(s.src[:end], []byte{'\n'})}
}

// statement reads one statement at the start of a logical line: blank space, a comment, a table
// header or a key with its value.
func (s *shapeScan) statement() *ShapeError {
	for s.i < len(s.src) && isSpaceByte(s.src[s.i]) {
		s.i++
	}

	if s.i >= len(s.src) {
		return nil
	}

	switch s.src[s.i] {
	case '#':
		s.toEOL()

		return nil
	case '[':
		return s.header()
	}

	return s.keyValue()
}

// header reads a table header or an array of tables header and skips the rest of the line.
func (s *shapeScan) header() *ShapeError {
	s.headers++
	if s.lim.MaxHeaders > 0 && s.headers > s.lim.MaxHeaders {
		return s.fail(ShapeHeaders, s.lim.MaxHeaders)
	}

	s.i++
	if s.i < len(s.src) && s.src[s.i] == '[' {
		s.i++
	}

	if e := s.keyParts(); e != nil {
		return e
	}

	s.toEOL()

	return nil
}

// keyValue reads a key and its value and skips the rest of the line.
func (s *shapeScan) keyValue() *ShapeError {
	if e := s.key(); e != nil {
		return e
	}

	s.skipBlanks()

	if s.i < len(s.src) && s.src[s.i] == '=' {
		s.i++
		s.skipBlanks()

		if e := s.value(); e != nil {
			return e
		}
	}

	s.toEOL()

	return nil
}

// key counts one key of the document and reads its parts.
func (s *shapeScan) key() *ShapeError {
	s.keys++
	if s.lim.MaxKeys > 0 && s.keys > s.lim.MaxKeys {
		return s.fail(ShapeKeys, s.lim.MaxKeys)
	}

	return s.keyParts()
}

// keyParts reads a dotted key, quoted parts included, and checks its number of parts. It stops
// at the first byte that cannot continue the key.
func (s *shapeScan) keyParts() *ShapeError {
	parts := 0

	for {
		s.skipBlanks()

		parts++
		if s.lim.MaxKeyParts > 0 && parts > s.lim.MaxKeyParts {
			return s.fail(ShapeKeyParts, s.lim.MaxKeyParts)
		}

		s.keyPart()
		s.skipBlanks()

		if s.i >= len(s.src) || s.src[s.i] != '.' {
			return nil
		}

		s.i++
	}
}

// keyPart skips one bare or quoted part of a key.
func (s *shapeScan) keyPart() {
	if s.i >= len(s.src) {
		return
	}

	switch c := s.src[s.i]; c {
	case '"', '\'':
		s.i++
		s.quoted(c)
	default:
		for s.i < len(s.src) && isBareByte(s.src[s.i]) {
			s.i++
		}
	}
}

// quoted skips the body of a single-line string whose opening quote was read. An unterminated
// string stops before the line feed.
//
// Parameters:
//   - q: the quote character, a double or a single quote
func (s *shapeScan) quoted(q byte) {
	for s.i < len(s.src) {
		switch c := s.src[s.i]; {
		case c == '\n':
			return
		case c == '\\' && q == '"':
			s.i += 2
		case c == q:
			s.i++

			return
		default:
			s.i++
		}
	}
}

// value reads one value, which ends with the value itself for a scalar and a string, and with the
// matching close for an array or an inline table, whatever the number of lines it spans.
func (s *shapeScan) value() *ShapeError {
	var stack []byte

	keyState, dots := false, 0

	for s.i < len(s.src) {
		c := s.src[s.i]
		top := byte(0)

		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}

		switch {
		case c == '"' || c == '\'':
			s.str(c)

			if len(stack) == 0 {
				return nil
			}
		case c == '#':
			if len(stack) == 0 {
				return nil
			}

			s.toEOL()
		case c == '\n':
			if len(stack) == 0 {
				return nil
			}

			s.i++
		case c == '[' || c == '{':
			if s.lim.MaxNesting > 0 && len(stack) >= s.lim.MaxNesting {
				return s.fail(ShapeNesting, s.lim.MaxNesting)
			}

			stack = append(stack, c)
			keyState, dots = c == '{', 0
			s.i++
		case c == ']' || c == '}':
			s.i++

			if len(stack) == 0 {
				return nil
			}

			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return nil
			}
		case top == '{':
			var e *ShapeError

			keyState, dots, e = s.inlineByte(c, keyState, dots)
			if e != nil {
				return e
			}
		default:
			s.i++
		}
	}

	return nil
}

// inlineByte reads one byte inside an inline table: it tracks whether a key or a value is being
// read, counts the parts of a dotted key and the keys of the document.
//
// Parameters:
//   - c: the byte at the current position
//   - keyState: true while a key is being read
//   - dots: number of dots seen in the current key
//
// Returns the new key state, the new dot count and the error of a limit crossed.
func (s *shapeScan) inlineByte(c byte, keyState bool, dots int) (bool, int, *ShapeError) {
	s.i++

	switch {
	case c == '=' && keyState:
		s.keys++
		if s.lim.MaxKeys > 0 && s.keys > s.lim.MaxKeys {
			return false, 0, s.fail(ShapeKeys, s.lim.MaxKeys)
		}

		return false, 0, nil
	case c == ',':
		return true, 0, nil
	case c == '.' && keyState:
		dots++
		if s.lim.MaxKeyParts > 0 && dots+1 > s.lim.MaxKeyParts {
			return keyState, dots, s.fail(ShapeKeyParts, s.lim.MaxKeyParts)
		}
	}

	return keyState, dots, nil
}

// str skips a string value of any kind, the opening quote being at the current position.
//
// Parameters:
//   - q: the quote character, a double or a single quote
func (s *shapeScan) str(q byte) {
	if s.i+2 < len(s.src) && s.src[s.i+1] == q && s.src[s.i+2] == q {
		s.i += 3
		s.multiline(q)

		return
	}

	s.i++
	s.quoted(q)
}

// multiline skips the body of a multi-line string whose opening delimiter was read. A run of
// three or more quotes closes it, the quotes beyond the third belonging to the string.
//
// Parameters:
//   - q: the quote character, a double or a single quote
func (s *shapeScan) multiline(q byte) {
	for s.i < len(s.src) {
		switch c := s.src[s.i]; {
		case c == '\\' && q == '"':
			s.i += 2
		case c == q:
			run := 0
			for s.i < len(s.src) && s.src[s.i] == q {
				run++
				s.i++
			}

			if run >= 3 {
				return
			}
		default:
			s.i++
		}
	}
}

// skipBlanks skips spaces and tabs.
func (s *shapeScan) skipBlanks() {
	for s.i < len(s.src) && (s.src[s.i] == ' ' || s.src[s.i] == '\t') {
		s.i++
	}
}

// toEOL moves after the next line feed, or to the end of the text.
func (s *shapeScan) toEOL() {
	n := bytes.IndexByte(s.src[min(s.i, len(s.src)):], '\n')
	if n < 0 {
		s.i = len(s.src)

		return
	}

	s.i += n + 1
}

// isSpaceByte reports whether a byte is white space or a line feed.
//
// Parameters:
//   - c: byte to test
func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// isBareByte reports whether a byte can be part of a bare key, loosely: anything but the bytes
// that end a key or open a string. Being loose is safe, the decoder rejects what is not a key.
//
// Parameters:
//   - c: byte to test
func isBareByte(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '.', '=', ']', '[', '#', ',', '{', '}', '"', '\'':
		return false
	}

	return true
}
