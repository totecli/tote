package store

import (
	"bytes"
	"strings"

	"github.com/BurntSushi/toml"
)

// span locates one [[table]] block inside the bytes of a file.
//
// Start is the offset of the first comment line owned by the block, which is the first line of
// the run of comment lines that touches the header from above, or Header when there is none. A
// section title such as "# --- clients ---" ends the run and is never owned.
// Header is the offset of the header line. End is the offset just after the last line of the
// block, line ending included. Line is the 1-based number of the header line. The blank lines
// and the comments that follow the last key of a block are not part of it, so a block never
// overlaps its neighbours and a span always satisfies Start <= Header < End.
type span struct {
	Start  int
	Header int
	End    int
	Line   int
}

// badHeader describes a header line the writer cannot edit safely.
//
// Line is the 1-based line number and Msg a lowercase message without a final period.
type badHeader struct {
	Line int
	Msg  string
}

// scanResult is what scanBlocks learned about a file.
//
// Blocks lists the [[table]] blocks in file order. Bad lists the headers that the scanner would
// not recognise as blocks although the TOML parser may read them as records, such as a header
// with spaces inside the brackets or a sub-table of the table. SchemaLine is the 1-based line
// of the top-level schema key, or 0 when there is none. HasHeader tells whether the file holds
// any table header at all, and MaxDepth is the deepest nesting of arrays and inline tables met
// in a value, which lets the caller refuse a file before handing it to a recursive parser.
type scanResult struct {
	Blocks     []span
	Bad        []badHeader
	SchemaLine int
	HasHeader  bool
	MaxDepth   int
}

// scanMode tells in which kind of string the scanner currently is.
type scanMode int

const (
	// modeNormal means that no multi-line string is open.
	modeNormal scanMode = iota
	// modeBasic means that a multi-line basic string (triple double quotes) is open.
	modeBasic
	// modeLiteral means that a multi-line literal string (triple single quotes) is open.
	modeLiteral
)

// valueState follows a key value across lines.
//
// The mode field is set while a multi-line string is open and depth counts the arrays and
// inline tables that are still open, peak being the highest value depth reached. A line that
// starts while either is set belongs to the value being read, whatever it looks like, so a
// "[[table]]" line inside a multi-line string is not a header.
type valueState struct {
	mode  scanMode
	depth int
	peak  int
}

// headerNormalizer removes the spaces and quotes that TOML allows inside a header.
var headerNormalizer = strings.NewReplacer(" ", "", "\t", "", "\"", "", "'", "")

// scanner carries the state of one scanBlocks run.
//
// The field cur is the block being built, valid while open is true. The field runStart is the
// offset of the run of comment lines that touches the current line from above, or -1. The field
// seenHeader becomes true at the first table header, after which a schema key is no longer a
// top-level key.
type scanner struct {
	table      string
	res        scanResult
	state      valueState
	cur        span
	open       bool
	runStart   int
	seenHeader bool
}

// scanBlocks finds the [[table]] blocks of a TOML file without parsing it.
//
// A header is a line whose trimmed content is exactly [[table]], optionally followed by
// whitespace and a comment. Whitespace inside the brackets is not supported: such a header is
// reported in the Bad list instead of being treated as a block. Lines inside multi-line strings
// and multi-line arrays are never headers. Any other table header, [other] or [[other]], ends
// the current block. A block runs from its header to its last key line; the comment lines that
// touch its header from above are owned by it, except a section title such as "# --- name ---",
// and every other blank line or comment is outside all blocks. A schema key inside a block is
// reported in the Bad list. The function never fails and never panics, whatever the input.
//
// Parameters:
//   - data: whole file, any line ending style, possibly invalid TOML
//   - table: array of tables name without brackets, such as connection
//
// Returns the blocks in file order together with the headers that could not be handled.
func scanBlocks(data []byte, table string) scanResult {
	s := &scanner{table: table, runStart: -1}

	pos, no := 0, 0
	for pos < len(data) {
		no++
		end, next := len(data), len(data)
		if i := bytes.IndexByte(data[pos:], '\n'); i >= 0 {
			end = pos + i
			next = end + 1
		}
		s.feed(pos, next, no, data[pos:end])
		pos = next
	}
	s.closeBlock()
	s.res.MaxDepth = s.state.peak

	return s.res
}

// feed handles one line of the file.
//
// Parameters:
//   - off: offset of the first byte of the line
//   - next: offset of the first byte of the following line
//   - no: 1-based line number
//   - raw: line content without its newline, a trailing carriage return still included
func (s *scanner) feed(off, next, no int, raw []byte) {
	if s.state.active() {
		s.content(next)
		s.state.advance(raw)
		return
	}

	text := bytes.Trim(raw, " \t\r")
	if off == 0 {
		text = bytes.TrimPrefix(text, []byte("\xef\xbb\xbf"))
	}

	switch {
	case len(text) == 0:
		s.runStart = -1
	case text[0] == '#':
		switch {
		case isSectionComment(text):
			s.runStart = -1
		case s.runStart < 0:
			s.runStart = off
		}
	case text[0] == '[':
		s.header(off, next, no, text)
	default:
		s.runStart = -1
		if isSchemaKey(text) {
			s.schemaKey(no)
		}
		s.content(next)
		s.state.advance(raw)
	}
}

// schemaKey handles a line that sets the schema key.
//
// The key is only valid before the first table header. Inside a block it would be read as a
// field of the record, so it is reported instead of being carried silently.
//
// Parameters:
//   - no: 1-based line number
func (s *scanner) schemaKey(no int) {
	switch {
	case s.open:
		s.res.Bad = append(s.res.Bad, badHeader{
			Line: no,
			Msg:  "schema must be written before the first table",
		})
	case !s.seenHeader && s.res.SchemaLine == 0:
		s.res.SchemaLine = no
	}
}

// content extends the open block, if any, to the end of a content line.
//
// Parameters:
//   - next: offset just after the content line
func (s *scanner) content(next int) {
	if s.open {
		s.cur.End = next
	}
}

// header handles a line that starts with a bracket.
//
// Parameters:
//   - off: offset of the header line
//   - next: offset just after the header line
//   - no: 1-based line number
//   - text: trimmed line content
func (s *scanner) header(off, next, no int, text []byte) {
	start := off
	if s.runStart >= 0 {
		start = s.runStart
	}
	s.runStart = -1
	s.seenHeader = true
	s.res.HasHeader = true
	s.closeBlock()

	name := cleanHeader(text)
	if name == "[["+s.table+"]]" {
		s.cur = span{Start: start, Header: off, End: next, Line: no}
		s.open = true
		return
	}
	if msg := s.badHeaderMessage(name); msg != "" {
		s.res.Bad = append(s.res.Bad, badHeader{Line: no, Msg: msg})
	}
}

// badHeaderMessage explains why a header that is not [[table]] is still a problem.
//
// Parameters:
//   - name: header text without its trailing comment
//
// Returns an empty string when the header belongs to an unrelated table.
func (s *scanner) badHeaderMessage(name string) string {
	norm := headerNormalizer.Replace(name)
	exact := "[[" + s.table + "]]"
	low, lowTable := strings.ToLower(norm), strings.ToLower(s.table)
	switch {
	case norm == exact:
		return "write the header exactly as " + exact + " without spaces or quotes"
	case low == "[["+lowTable+"]]":
		return "table names are case sensitive, write the header as " + exact
	case low == "["+lowTable+"]":
		return s.table + " must be an array of tables written as " + exact
	case strings.HasPrefix(low, "[["+lowTable+".") || strings.HasPrefix(low, "["+lowTable+"."):
		return "sub-tables of " + s.table + " are not supported"
	}

	return ""
}

// closeBlock stores the open block, if any.
func (s *scanner) closeBlock() {
	if s.open {
		s.res.Blocks = append(s.res.Blocks, s.cur)
		s.open = false
	}
}

// isSchemaKey reports whether a trimmed line is the top-level schema key.
//
// Parameters:
//   - text: trimmed line content
func isSchemaKey(text []byte) bool {
	rest, ok := bytes.CutPrefix(text, []byte("schema"))
	if !ok {
		return false
	}

	return bytes.HasPrefix(bytes.TrimLeft(rest, " \t"), []byte("="))
}

// isSectionComment reports whether a trimmed comment line is a section title.
//
// A section title looks like "# --- clients ---": it starts and ends with three dashes. It
// names a group of records, so it is never owned by the block that follows it.
//
// Parameters:
//   - text: trimmed line content that starts with a hash sign
func isSectionComment(text []byte) bool {
	body := bytes.TrimLeft(text[1:], " \t")

	return bytes.HasPrefix(body, []byte("---")) && bytes.HasSuffix(body, []byte("---"))
}

// cleanHeader removes the trailing comment and whitespace of a trimmed header line.
//
// Parameters:
//   - text: trimmed line content that starts with a bracket
func cleanHeader(text []byte) string {
	var quote byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return strings.TrimRight(string(text[:i]), " \t")
		}
	}

	return strings.TrimRight(string(text), " \t")
}

// active reports whether the next line continues a value started on an earlier line.
func (v *valueState) active() bool {
	return v.mode != modeNormal || v.depth > 0
}

// advance reads one line of a key value and updates the open strings and brackets.
//
// Parameters:
//   - line: line content, with or without a trailing carriage return
//
// Returns the index of the hash sign that starts a trailing comment, or -1 when there is none.
func (v *valueState) advance(line []byte) int {
	i := 0
	for i < len(line) {
		switch v.mode {
		case modeBasic:
			i = v.closeMulti(line, i, '"')
		case modeLiteral:
			i = v.closeMulti(line, i, '\'')
		default:
			at := i
			var stop bool
			i, stop = v.step(line, i)
			if stop {
				return at
			}
		}
	}

	return -1
}

// step reads one token of a line outside any multi-line string.
//
// Parameters:
//   - line: line content
//   - i: index of the token start, always inside the line
//
// Returns the index of the next token and true when the rest of the line is a comment.
func (v *valueState) step(line []byte, i int) (int, bool) {
	switch c := line[i]; c {
	case '#':
		return len(line), true
	case '"', '\'':
		if hasRun(line, i, c, 3) {
			v.mode = modeBasic
			if c == '\'' {
				v.mode = modeLiteral
			}
			return i + 3, false
		}
		return skipString(line, i+1, c), false
	case '[', '{':
		v.depth++
		v.peak = max(v.peak, v.depth)
	case ']', '}':
		if v.depth > 0 {
			v.depth--
		}
	}

	return i + 1, false
}

// closeMulti looks for the end of a multi-line string in the rest of a line.
//
// Parameters:
//   - line: line content
//   - i: index to start from
//   - quote: quote character of the string, a double or a single quote
//
// Returns the index after the closing delimiter, or the line length when the string goes on.
func (v *valueState) closeMulti(line []byte, i int, quote byte) int {
	for i < len(line) {
		c := line[i]
		if c == '\\' && quote == '"' {
			i += 2
			continue
		}
		if c == quote && hasRun(line, i, quote, 3) {
			i += 3
			for extra := 0; extra < 2 && i < len(line) && line[i] == quote; extra++ {
				i++
			}
			v.mode = modeNormal
			return i
		}
		i++
	}

	return len(line)
}

// hasRun reports whether n copies of a byte start at an index.
//
// Parameters:
//   - line: line content
//   - i: start index
//   - c: byte to look for
//   - n: number of consecutive copies
func hasRun(line []byte, i int, c byte, n int) bool {
	if i+n > len(line) {
		return false
	}
	for k := range n {
		if line[i+k] != c {
			return false
		}
	}

	return true
}

// skipString skips a single-line string.
//
// Parameters:
//   - line: line content
//   - i: index just after the opening quote
//   - quote: quote character, where only a double quote honours backslash escapes
//
// Returns the index after the closing quote, or the line length when the string is unterminated.
func skipString(line []byte, i int, quote byte) int {
	for i < len(line) {
		switch {
		case line[i] == '\\' && quote == '"':
			i += 2
		case line[i] == quote:
			return i + 1
		default:
			i++
		}
	}

	return len(line)
}

// dominantEOL returns the line ending used by most lines of a file.
//
// Parameters:
//   - data: file content
//
// Returns "\r\n" when strictly more lines end with it than with a bare "\n", else "\n".
func dominantEOL(data []byte) string {
	crlf := bytes.Count(data, []byte("\r\n"))
	bare := bytes.Count(data, []byte("\n")) - crlf
	if crlf > bare {
		return "\r\n"
	}

	return "\n"
}

// headerEOL returns the line ending of the header line of a block.
//
// Parameters:
//   - data: file content
//   - sp: span of the block
//   - fallback: line ending to return when the header line has none
func headerEOL(data []byte, sp span, fallback string) string {
	i := bytes.IndexByte(data[sp.Header:sp.End], '\n')
	switch {
	case i < 0:
		return fallback
	case i > 0 && data[sp.Header+i-1] == '\r':
		return "\r\n"
	}

	return "\n"
}

// encodeBlock renders one record as a [[table]] block.
//
// Zero values are left out only where the record type says so with omitempty in its tags.
//
// Parameters:
//   - table: array of tables name without brackets
//   - rec: record value to encode
//   - eol: line ending used for every line of the block
//
// Returns the block bytes, every line ending included, or the encoder error.
func encodeBlock(table string, rec any, eol string) ([]byte, error) {
	var body bytes.Buffer
	enc := toml.NewEncoder(&body)
	enc.Indent = ""
	if err := enc.Encode(rec); err != nil {
		return nil, err
	}

	text := "[[" + table + "]]\n" + body.String()
	if eol != "\n" {
		text = strings.ReplaceAll(text, "\n", eol)
	}

	return []byte(text), nil
}

// addBlock appends a block at the end of a file.
//
// An empty file gets the schema line first. Otherwise a missing final line ending is added, and
// one blank line separates the new block from the previous content unless the file already ends
// with a blank line. A file without any schema key or table header, such as one made only of
// comments or blank lines, gets the schema line after its content.
//
// Parameters:
//   - data: current file content
//   - block: encoded block, line endings included
//   - eol: line ending of the file
//   - schema: whether the schema line must be written because the file has none and no table
//     header, which would make a line appended at the end part of a table
//
// Returns a new slice; data is not modified.
func addBlock(data, block []byte, eol string, schema bool) []byte {
	out := make([]byte, 0, len(data)+len(block)+32)
	if len(data) > 0 {
		out = append(out, data...)
		if out[len(out)-1] != '\n' {
			out = append(out, eol...)
		}
		if !endsBlank(out) {
			out = append(out, eol...)
		}
	}

	if len(data) == 0 || schema {
		out = append(out, "schema = 1"...)
		out = append(out, eol...)
		out = append(out, eol...)
	}

	return append(out, block...)
}

// endsBlank reports whether the last line of text is empty or only whitespace.
//
// Parameters:
//   - text: non-empty content that ends with a newline
func endsBlank(text []byte) bool {
	body := text[:len(text)-1]
	last := body[bytes.LastIndexByte(body, '\n')+1:]

	return len(bytes.Trim(last, " \t\r")) == 0
}

// replaceBlock swaps the lines of one block for a new encoding.
//
// The comment lines above the header, which belong to the block, are kept as they are, and so is
// everything outside the span. When the replaced block was the last line of a file without a
// final line ending, the new block does not get one either.
//
// Parameters:
//   - data: current file content
//   - sp: span of the block to replace
//   - block: encoded replacement, starting with its header line
//
// Returns a new slice; data is not modified.
func replaceBlock(data []byte, sp span, block []byte) []byte {
	if data[sp.End-1] != '\n' {
		block = bytes.TrimSuffix(bytes.TrimSuffix(block, []byte("\n")), []byte("\r"))
	}

	out := make([]byte, 0, len(data)+len(block))
	out = append(out, data[:sp.Header]...)
	out = append(out, block...)

	return append(out, data[sp.End:]...)
}

// removeBlock cuts one block, comments above its header included, out of a file.
//
// Parameters:
//   - data: current file content
//   - sp: span of the block to remove
//
// Returns a new slice; data is not modified.
func removeBlock(data []byte, sp span) []byte {
	out := make([]byte, 0, len(data))
	out = append(out, data[:sp.Start]...)

	return append(out, data[sp.End:]...)
}
