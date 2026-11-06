package plugin

import "testing"

// TestIndexPositions checks the positional scanner: tables, arrays of tables, dotted and quoted
// keys, strings that hide brackets and the fallback to the parent path.
//
// Parameters:
//   - t: test to report to
func TestIndexPositions(t *testing.T) {
	src := "a = 1\n" + // 1
		"b = \"[[not]] a = header\"\n" + // 2
		"c = [\n  1,\n  2,\n]\n" + // 3 to 6
		"[t]\n" + // 7
		"k = 'x'\n" + // 8
		"d.e = 2\n" + // 9
		"\"q.r\" = 3\n" + // 10
		"[[arr]]\n" + // 11
		"id = \"one\"\n" + // 12
		"[[arr.sub]]\n" + // 13
		"z = 1\n" + // 14
		"[[arr]]\n" + // 15
		"id = \"two\"\n" + // 16
		"  [[arr.sub]]\n" + // 17
		"  z = 2\n" + // 18
		"inl = [{ a = 1 }, { b = 2 }]\n" + // 19
		"s = \"\"\"\nmulti [[x]]\nline\"\"\"\n" + // 20 to 22
		"after = 1\n" // 23

	idx := indexPositions(src)

	tests := []struct {
		path string
		line int
	}{
		{"a", 1},
		{"b", 2},
		{"c", 3},
		{"t", 7},
		{"t.k", 8},
		{"t.d.e", 9},
		{"t.q.r", 0},
		{"arr[0]", 11},
		{"arr[0].id", 12},
		{"arr[0].sub[0]", 13},
		{"arr[0].sub[0].z", 14},
		{"arr[1]", 15},
		{"arr[1].id", 16},
		{"arr[1].sub[0]", 17},
		{"arr[1].sub[0].z", 18},
		{"arr[1].sub[0].inl", 19},
		{"arr[1].sub[0].s", 20},
		{"arr[1].sub[0].after", 23},
	}

	for _, tt := range tests {
		line, _ := idx.find(tt.path)
		if tt.line != 0 && line != tt.line {
			t.Errorf("find(%q) line = %d, want %d", tt.path, line, tt.line)
		}
	}

	// A path that does not exist falls back on its parent.
	if line, _ := idx.find("arr[1].missing"); line != 15 {
		t.Errorf("fallback to the parent = %d, want 15", line)
	}

	if line, col := idx.find("arr[1].sub[0]"); line != 17 || col != 3 {
		t.Errorf("indented header = %d:%d, want 17:3", line, col)
	}

	if line, _ := idx.find("nope"); line != 0 {
		t.Errorf("unknown top-level path = %d, want 0", line)
	}
}

// TestIndexPositionsHostile checks that the scanner never panics on broken input.
//
// Parameters:
//   - t: test to report to
func TestIndexPositionsHostile(t *testing.T) {
	for _, src := range []string{
		"", "[", "[[", "[[a", "a", "a =", "a = \"", "a = '''", "a = [", "a = {", "\"", "'", "[a.", "[a.]",
		"[[a]]\n[[a.b", "# comment only", "a = \"\\", "\x00\x01", "[\"", "a.b.c.d = [[[[", "= 1",
	} {
		idx := indexPositions(src)
		_, _ = idx.find("a")
	}
}
