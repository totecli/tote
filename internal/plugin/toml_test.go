package plugin

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// TestTomlAssumptions pins the behaviour of the TOML library that the manifest reader relies on:
// integers decode to int64, arrays of tables to a slice of maps, nested arrays of tables keep their
// parent, and a document with a NUL byte or an invalid construct is refused. If a library update
// changes one of them, this test fails before the reader misreads a manifest.
//
// Parameters:
//   - t: test to report to
func TestTomlAssumptions(t *testing.T) {
	src := "a = 1\nb = [ 1, 2 ]\n[[x]]\nk = \"v\"\n[[x.y]]\nz = 1\n[i18n.fr]\n\"a.b\" = \"c\"\n" +
		"d.e = \"f\"\ninl = [{a=1}]\n"

	var m map[string]any

	if _, err := toml.Decode(src, &m); err != nil {
		t.Fatal(err)
	}

	if _, ok := m["a"].(int64); !ok {
		t.Errorf("integer decoded to %T, want int64", m["a"])
	}

	if _, ok := m["b"].([]any); !ok {
		t.Errorf("array decoded to %T, want []any", m["b"])
	}

	x, ok := m["x"].([]map[string]any)
	if !ok || len(x) != 1 {
		t.Fatalf("array of tables decoded to %T, want []map[string]any", m["x"])
	}

	if _, ok := x[0]["y"].([]map[string]any); !ok {
		t.Errorf("nested array of tables decoded to %T", x[0]["y"])
	}

	fr, _ := m["i18n"].(map[string]any)["fr"].(map[string]any)
	if fr["a.b"] != "c" {
		t.Errorf("quoted dotted key = %v, want c", fr["a.b"])
	}

	if _, ok := fr["d"].(map[string]any); !ok {
		t.Errorf("dotted key decoded to %T, want a nested table", fr["d"])
	}

	if _, ok := fr["inl"].([]any); !ok {
		t.Errorf("inline array of tables decoded to %T, want []any", fr["inl"])
	}

	for _, bad := range []string{"a = \"\x00\"", "a = ", "a = 1\na = 2", "[a]\n[a]"} {
		var out map[string]any
		if _, err := toml.Decode(bad, &out); err == nil {
			t.Errorf("Decode(%q) accepted a document the reader assumes is refused", bad)
		}
	}
}
