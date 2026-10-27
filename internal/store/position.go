package store

import (
	"bytes"

	"github.com/BurntSushi/toml"
)

// SyntaxPosition gives the line and the column of a TOML syntax error.
//
// The Line of toml.Position is one too high when the offending character is a line break (an
// unterminated table header, for instance), so in that case the position is computed from the
// byte offset. Any other error keeps the position of the library, whose offset is not always the
// one of the column.
//
// Parameters:
//   - data: the text that was decoded
//   - pos: position of the error, as the TOML library reports it
//
// Returns the 1-based line and column.
func SyntaxPosition(data []byte, pos toml.Position) (int, int) {
	if pos.Start < 0 || pos.Start >= len(data) || data[pos.Start] != '\n' {
		return pos.Line, pos.Col
	}

	before := data[:pos.Start]
	line := bytes.Count(before, []byte{'\n'}) + 1

	return line, pos.Start - bytes.LastIndexByte(before, '\n')
}
