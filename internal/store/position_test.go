package store

import (
	"errors"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestSyntaxPosition checks the line and the column of syntax errors, among them the ones at a
// line break, which the TOML library puts one line too low.
//
// Parameters:
//   - t: test to report to
func TestSyntaxPosition(t *testing.T) {
	tests := []struct {
		name string
		src  string
		line int
		col  int
	}{
		{"unterminated header", "schema = 1\n[ui\n", 2, 4},
		{"unterminated header before a key", "a = 1\n[ui\ntheme = 2\n", 2, 4},
		{"value expected", "schema = 1\nx = =\n", 2, 5},
		{"unterminated string", "a = \"x\n", 1, 7},
		{"unterminated array", "a = 1\nb = [1,\n", 2, 8},
		{"first line", "[ui\n", 1, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc map[string]any

			_, err := toml.Decode(tt.src, &doc)

			var te toml.ParseError
			if !errors.As(err, &te) {
				t.Fatalf("no syntax error for %q: %v", tt.src, err)
			}

			line, col := SyntaxPosition([]byte(tt.src), te.Position)
			if line != tt.line || col != tt.col {
				t.Errorf("position %d:%d, want %d:%d", line, col, tt.line, tt.col)
			}
		})
	}

	for _, start := range []int{-1, 0, 99} {
		line, col := SyntaxPosition([]byte("ab"), toml.Position{Line: 7, Col: 3, Start: start})
		if line != 7 || col != 3 {
			t.Errorf("offset %d gave %d:%d, want the library position 7:3", start, line, col)
		}
	}
}
