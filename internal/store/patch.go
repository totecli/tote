package store

import (
	"bytes"
	"reflect"

	"github.com/BurntSushi/toml"
)

// piece is one part of the body of a block: a key with its value, or a filler line.
//
// Text holds the lines of the piece with their line endings. A key piece can span several lines
// when its value is a multi-line string or array. Key is the root key the piece sets and Value
// its decoded value; both are empty for a comment line, a blank line, and a piece that could not
// be decoded alone, which patchBlock always keeps as it is.
type piece struct {
	Text  []byte
	Key   string
	Value any
}

// splitPieces cuts the body of a block into pieces.
//
// Parameters:
//   - body: lines of a block after its header line, any line ending style
//
// Returns the pieces in file order; concatenating their Text gives body back.
func splitPieces(body []byte) []piece {
	var (
		pieces []piece
		state  valueState
	)

	for pos := 0; pos < len(body); {
		next := len(body)
		if i := bytes.IndexByte(body[pos:], '\n'); i >= 0 {
			next = pos + i + 1
		}
		line := body[pos:next]
		pos = next

		if state.active() {
			last := &pieces[len(pieces)-1]
			last.Text = append(last.Text, line...)
			state.advance(line)
			continue
		}

		text := bytes.Trim(line, " \t\r\n")
		if len(text) == 0 || text[0] == '#' {
			pieces = append(pieces, piece{Text: append([]byte(nil), line...)})
			continue
		}

		pieces = append(pieces, piece{Text: append([]byte(nil), line...)})
		state.advance(line)
	}

	for i := range pieces {
		pieces[i].Key, pieces[i].Value = decodePiece(pieces[i].Text)
	}

	return pieces
}

// decodePiece decodes the key and the value set by one piece.
//
// Parameters:
//   - text: lines of the piece
//
// Returns the root key and its value, or an empty key for a filler line or a piece that does
// not decode to exactly one root key.
func decodePiece(text []byte) (string, any) {
	trimmed := bytes.TrimLeft(text, " \t")
	if len(trimmed) == 0 || trimmed[0] == '#' || trimmed[0] == '\r' || trimmed[0] == '\n' {
		return "", nil
	}

	var m map[string]any
	if _, err := toml.Decode(string(text), &m); err != nil || len(m) != 1 {
		return "", nil
	}
	for k, v := range m {
		return k, v
	}

	return "", nil
}

// unknownKeys lists the root keys of a block that the record type does not know.
//
// Parameters:
//   - body: lines of the block after its header line
//
// Returns the set of unknown root keys, or the decoding error.
func unknownKeys[T Record](body []byte) (map[string]bool, error) {
	var probe T
	md, err := toml.Decode(string(body), &probe)
	if err != nil {
		return nil, err
	}

	unknown := map[string]bool{}
	for _, k := range md.Undecoded() {
		unknown[k[0]] = true
	}

	return unknown, nil
}

// splitHeader separates the header line of a block from the lines that follow it.
//
// Parameters:
//   - block: bytes of a block, starting with its header line
//
// Returns the header line with its line ending, and the rest.
func splitHeader(block []byte) ([]byte, []byte) {
	if i := bytes.IndexByte(block, '\n'); i >= 0 {
		return block[:i+1], block[i+1:]
	}

	return block, nil
}

// patchBlock rewrites a block so that it holds the new field values and nothing else changes.
//
// The header line is kept. A key whose value is unchanged keeps its lines, so its trailing
// comment and its spacing survive. A key whose value changed gets the new line, and keeps the
// trailing comment of a single-line key. A key that the new encoding leaves out, because the
// field is now empty, is removed. A key that the record type does not know is kept. A key that
// is new goes after the last line of the block. Comments and blank lines between keys are kept.
// The result ends with a line ending when a key was added, even if old did not; replaceBlock
// removes it again at the end of a file.
//
// Parameters:
//   - old: bytes of the current block, from its header line to its last line
//   - fresh: encoded replacement from encodeBlock, starting with its header line
//   - unknown: root keys of old that the record type does not know
//   - eol: line ending of the fresh block
func patchBlock(old, fresh []byte, unknown map[string]bool, eol string) []byte {
	head, oldBody := splitHeader(old)
	_, freshBody := splitHeader(fresh)

	pending := splitPieces(freshBody)
	byKey := make(map[string]int, len(pending))
	for i, p := range pending {
		byKey[p.Key] = i
	}
	used := make([]bool, len(pending))

	out := append([]byte(nil), head...)
	for _, p := range splitPieces(oldBody) {
		i, ok := byKey[p.Key]
		switch {
		case p.Key == "" || unknown[p.Key]:
			out = append(out, p.Text...)
		case !ok:
			continue
		case reflect.DeepEqual(p.Value, pending[i].Value):
			used[i] = true
			out = append(out, p.Text...)
		default:
			used[i] = true
			out = append(out, withComment(p.Text, pending[i].Text)...)
		}
	}

	for i, p := range pending {
		if used[i] {
			continue
		}
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, eol...)
		}
		out = append(out, p.Text...)
	}

	return out
}

// withComment gives a new key line the trailing comment of the line it replaces.
//
// Parameters:
//   - old: lines of the key being replaced
//   - fresh: new lines of the key
//
// Returns fresh, followed by the spacing and the comment of old when both are single lines.
func withComment(old, fresh []byte) []byte {
	if !singleLine(old) || !singleLine(fresh) {
		return fresh
	}

	content := bytes.TrimRight(old, "\r\n")
	var state valueState
	at := state.advance(content)
	if at < 0 || state.active() {
		return fresh
	}

	value := bytes.TrimRight(content[:at], " \t")
	out := append([]byte(nil), bytes.TrimRight(fresh, "\r\n")...)
	out = append(out, content[len(value):]...)

	return append(out, old[len(content):]...)
}

// singleLine reports whether a piece is made of one line.
//
// Parameters:
//   - text: lines of the piece
func singleLine(text []byte) bool {
	i := bytes.IndexByte(text, '\n')

	return i < 0 || i == len(text)-1
}
