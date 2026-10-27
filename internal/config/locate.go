package config

import (
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// keyLine finds the first line of a TOML document that defines a key or a table below a path.
//
// The TOML library reports a position for syntax errors only, so this helper rebuilds it for
// the errors found after decoding. Each line is decoded alone: a table header gives the current
// table, a key line gives a path below it. A line that does not decode alone is a continuation of
// a multi-line value and is skipped, so the answer can be wrong for a document that hides a
// key-like line inside a multi-line string. It is used to point at a line in a message, never to
// decide anything.
//
// Parameters:
//   - data: document text
//   - key: path to look for; a line matches when its own path starts with it
//
// Returns the 1-based line and column, or 0 and 0 when no line matches.
func keyLine(data []byte, key []string) (line, col int) {
	var table []string

	for i, raw := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		var doc map[string]any
		if _, err := toml.Decode(text, &doc); err != nil {
			continue
		}

		path := keyPath(doc)
		if strings.HasPrefix(text, "[") {
			table = path
		} else {
			path = append(slices.Clone(table), path...)
		}

		if len(path) >= len(key) && slices.Equal(path[:len(key)], key) {
			return i + 1, strings.Index(raw, text) + 1
		}
	}

	return 0, 0
}

// keyPath follows a decoded line down its chain of single-key tables.
//
// Parameters:
//   - doc: document decoded from one line
//
// Returns the keys from the root to the first value that is not a one-key table.
func keyPath(doc map[string]any) []string {
	var path []string

	for len(doc) == 1 {
		var next any
		for k, v := range doc {
			path = append(path, k)
			next = v
		}

		sub, ok := next.(map[string]any)
		if !ok {
			break
		}
		doc = sub
	}

	return path
}
