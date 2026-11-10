package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// firstKey returns the first key of a JSON object.
//
// Parameters:
//   - t: running test
//   - doc: JSON text
func firstKey(t *testing.T, doc string) string {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(doc))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("%q is not a JSON object: %v", doc, err)
	}

	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("no key in %q: %v", doc, err)
	}

	key, _ := tok.(string)

	return key
}

// TestJSONSchemaIsFirst checks that every JSON document of the command line starts with the
// schema key, equal to 1, so that a script can tell the format before it reads anything else.
//
// Parameters:
//   - t: test to report to
func TestJSONSchemaIsFirst(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"version", []string{"version", "--json"}, 0},
		{"doctor", []string{"doctor", "--json"}, -1},
		{"update failure", []string{"update", "--json"}, -1},
		{"list", []string{"alias", "--json"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.fixtures()

			code := h.run(tt.args...)
			if tt.code >= 0 && code != tt.code {
				t.Fatalf("code %d, stderr %q", code, h.errb.String())
			}

			if got := firstKey(t, h.out.String()); got != "schema" {
				t.Errorf("first key = %q in %q", got, h.out.String())
			}

			var doc struct {
				Schema int `json:"schema"`
			}
			if err := json.Unmarshal(h.out.Bytes(), &doc); err != nil || doc.Schema != 1 {
				t.Errorf("schema = %d (%v)", doc.Schema, err)
			}
		})
	}
}
