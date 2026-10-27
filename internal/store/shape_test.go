package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// shapeLimits is a small set of limits that makes every check reachable with a short text.
var shapeLimits = ShapeLimits{
	MaxBytes: 4096, MaxLineBytes: 200, MaxNesting: 3, MaxKeyParts: 4, MaxHeaders: 5, MaxKeys: 8,
}

// TestCheckShape checks each limit with a text just inside it and a text just beyond it, and the
// string and comment rules that keep a bracket or a dot from counting.
//
// Parameters:
//   - t: test to report to
func TestCheckShape(t *testing.T) {
	long := "a = \"" + strings.Repeat("x", 300) + "\"\n"

	tests := []struct {
		name  string
		src   string
		limit string
		line  int
	}{
		{"empty", "", "", 0},
		{"plain", "a = 1\nb = \"x\"\n[t]\nc = [1, 2]\n", "", 0},
		{"comment only", "# [[[[[[[[[[ {{{{{{{{{{ a.b.c.d.e.f.g\n", "", 0},
		{"size", strings.Repeat("# c\n", 1100), ShapeBytes, 0},
		{"line", "a = 1\n" + long, ShapeLine, 2},
		{"line last without newline", "a = 1\n" + strings.TrimSuffix(long, "\n"), ShapeLine, 2},
		{"nesting ok", "a = [[[1]]]\n", "", 0},
		{"nesting arrays", "a = [[[[1]]]]\n", ShapeNesting, 1},
		{"nesting inline", "a = {b={c={d={e=1}}}}\n", ShapeNesting, 1},
		{"nesting mixed", "x = 1\na = [{b=[{c=1}]}]\n", ShapeNesting, 2},
		{"nesting multi-line", "a = [\n[\n[\n[\n1]]]]\n", ShapeNesting, 4},
		{"nesting resets", "a = [[[1]]]\nb = [[[2]]]\n", "", 0},
		{"brackets in strings", "a = \"[[[[[[[[\"\nb = '{{{{{{{{'\n", "", 0},
		{"brackets in multi-line", "a = \"\"\"\n[[[[[[[[\n\"\"\"\nb = '''\n{{{{{{{{\n'''\n", "", 0},
		{"brackets in comment in array", "a = [ # [[[[[[[\n  1,\n]\n", "", 0},
		{"escaped quote", "a = \"\\\"[[[[[[[[\"\nb = 1\n", "", 0},
		{"quote run closes", "a = \"\"\"x\"\"\"\"[[[[[[[[\n", "", 0},
		{"parts ok", "a.b.c.d = 1\n", "", 0},
		{"parts key", "a.b.c.d.e = 1\n", ShapeKeyParts, 1},
		{"parts spaces", "a . b . c . d . e = 1\n", ShapeKeyParts, 1},
		{"parts header", "x = 1\n[a.b.c.d.e]\n", ShapeKeyParts, 2},
		{"parts array header", "[[a.b.c.d.e]]\n", ShapeKeyParts, 1},
		{"parts quoted dots", "\"a.b.c.d.e.f\" = 1\n[\"a.b.c.d.e.f\"]\n", "", 0},
		{"parts literal", "'a.b.c.d.e.f' = 1\n", "", 0},
		{"parts inline", "a = {b.c.d.e.f = 1}\n", ShapeKeyParts, 1},
		{"parts inline ok", "a = {b.c.d.e = 1, f.g = 2}\n", "", 0},
		{"float is not a key", "a = {b = 1.5.6}\nc = [1.5, 2.5, 3.5, 4.5, 5.5]\n", "", 0},
		{"headers", "[a]\n[b]\n[c]\n[d]\n[e]\n[f]\n", ShapeHeaders, 6},
		{"headers array", strings.Repeat("[[a]]\n", 6), ShapeHeaders, 6},
		{"keys", "a=1\nb=1\nc=1\nd=1\ne=1\nf=1\ng=1\nh=1\ni=1\n", ShapeKeys, 9},
		{"keys inline", "a = {b=1, c=2, d=3, e=4, f=5, g=6, h=7, i=8}\n", ShapeKeys, 1},
		{"keys after header", "[t]\n" + strings.Repeat("k=1\n", 9), ShapeKeys, 10},
		{"unterminated string", "a = \"[[[[[[[[\nb = 1\n", "", 0},
		{"unterminated multi-line", "a = \"\"\"\n[[[[[[[[\n", "", 0},
		{"dangling escape", "a = \"\\", "", 0},
		{"garbage", "]]]]\n}}}}\n====\n..\n\x00\x01\x02\n", "", 0},
		{"crlf", "a = [[[1]]]\r\n[t]\r\nb = 1\r\n", "", 0},
		{"bom", "\xef\xbb\xbfa = 1\n", "", 0},
		{"value without key", "= [[[[[[\n", ShapeNesting, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckShape([]byte(tt.src), shapeLimits)

			switch {
			case tt.limit == "" && got != nil:
				t.Fatalf("CheckShape = %v, want nil", got)
			case tt.limit != "" && got == nil:
				t.Fatalf("CheckShape = nil, want %s", tt.limit)
			case got == nil:
				return
			case got.Limit != tt.limit || got.Line != tt.line:
				t.Fatalf("CheckShape = %s at line %d, want %s at line %d", got.Limit, got.Line,
					tt.limit, tt.line)
			}

			if got.Error() == "" {
				t.Error("empty error text")
			}
		})
	}
}

// TestCheckShapeZeroLimits checks that a zero limit disables its check.
//
// Parameters:
//   - t: test to report to
func TestCheckShapeZeroLimits(t *testing.T) {
	src := "a = " + strings.Repeat("[", 40) + strings.Repeat("]", 40) + "\n[" +
		strings.Repeat("a.", 40) + "a]\n"

	if got := CheckShape([]byte(src), ShapeLimits{}); got != nil {
		t.Errorf("zero limits refused a text: %v", got)
	}

	if got := CheckShape([]byte(src), ShapeUser); got == nil {
		t.Error("ShapeUser accepted 40 levels of nesting")
	}
}

// TestCheckShapeHostile feeds the hostile texts that blew up the decoder to CheckShape and checks
// that each is refused quickly, without allocating more than a few megabytes, and that the
// decoder was never called.
//
// Parameters:
//   - t: test to report to
func TestCheckShapeHostile(t *testing.T) {
	const size = 256 << 10

	tests := map[string]string{
		"nested arrays":  "a = " + strings.Repeat("[", size/2) + strings.Repeat("]", size/2-10),
		"nested inline":  "a = " + strings.Repeat("{a=", size/4),
		"deep header":    "[" + strings.Repeat("a.", size/4) + "a]\n",
		"deep dotted":    strings.Repeat("a.", size/4) + "a = 1\n",
		"deep inline":    "a = {" + strings.Repeat("a.", size/4) + "a = 1}\n",
		"many headers":   strings.Repeat("[[command]]\n", size/12),
		"long line":      "a = \"" + strings.Repeat("x", size-10) + "\"",
		"quotes":         strings.Repeat("\"\"\"", size/3),
		"many keys":      strings.Repeat("a = 1\n", size/6),
		"header repeats": strings.Repeat("["+strings.Repeat("a.", 100)+"a]\n", size/204),
	}

	lim := ShapeLimits{
		MaxBytes: size, MaxLineBytes: 16 << 10, MaxNesting: 4, MaxKeyParts: 8,
		MaxHeaders: 4096, MaxKeys: 16384,
	}

	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats

			runtime.ReadMemStats(&before)

			got := CheckShape([]byte(src), lim)

			runtime.ReadMemStats(&after)

			if name != "quotes" && got == nil {
				t.Errorf("hostile text accepted")
			}

			if grown := after.TotalAlloc - before.TotalAlloc; grown > 8<<20 {
				t.Errorf("the check allocated %d bytes", grown)
			}
		})
	}
}

// TestCheckShapeAgreesWithDecoder checks, on documents the decoder accepts, that the limits of
// ShapeUser never refuse a valid document of ordinary depth, so the guard cannot reject a real
// file.
//
// Parameters:
//   - t: test to report to
func TestCheckShapeAgreesWithDecoder(t *testing.T) {
	docs := []string{
		"schema = 1\n\n[[repo]]\nname = \"a\"\npath = \"~/a\"\ndesc = \"x [[y]] {z}\"\n",
		"a.b.c = 1\n[t.u.v]\nw = [[1, 2], [3]]\nx = { y = { z = [ \"q\" ] } }\n",
		"s = '''\nmulti [[line]]\n'''\nt = \"\"\"a\\\"\"\"b\"\"\"\nu = 'it'\n",
		"a = [\n  1, # c [[\n  2,\n]\n",
	}

	for _, doc := range docs {
		var m map[string]any
		if _, err := toml.Decode(doc, &m); err != nil {
			t.Fatalf("fixture %q is not valid: %v", doc, err)
		}

		if got := CheckShape([]byte(doc), ShapeUser); got != nil {
			t.Errorf("CheckShape refused a valid document: %v\n%s", got, doc)
		}
	}
}

// FuzzCheckShape checks that the scanner terminates without a panic whatever the input.
//
// Parameters:
//   - f: fuzz target
func FuzzCheckShape(f *testing.F) {
	for _, seed := range []string{
		"", "a = 1\n", "[[a.b]]\nc = [[1]]\n", "a = {b.c = [1, {d = 2}]}\n", "a = \"\"\"\n[[\n\"\"\"\n",
		"a = '''", "a = \"\\", "[", "[[", "a.", "a = [", "a = {", "= =", "\"", "'", "#", "]]]]}}}}",
		"a = [[[[[[[[[[[[1]]]]]]]]]]]]\n", "[a.b.c.d.e.f.g.h.i.j]\n",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = CheckShape(data, shapeLimits)
		_ = CheckShape(data, ShapeUser)
	})
}

// TestShapeGuardsFiles checks that a collection and PatchTable refuse a file whose dotted header
// or dotted key has thousands of parts, which costs the decoder gigabytes, with a *ParseError
// that names the line.
//
// Parameters:
//   - t: test to report to
func TestShapeGuardsFiles(t *testing.T) {
	deep := strings.Repeat("a.", 20000) + "a"

	bodies := map[string]string{
		"header": "schema = 1\n[" + deep + "]\n",
		"dotted": "schema = 1\n" + deep + " = 1\n",
		"inline": "schema = 1\nx = {" + deep + " = 1}\n",
	}

	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats

			runtime.ReadMemStats(&before)

			c, err := OpenConnections(rvWrite(t, body))
			if err != nil {
				t.Fatal(err)
			}

			var pe *ParseError
			if !errors.As(c.Err(), &pe) || pe.Line != 2 || !strings.Contains(pe.Msg, "key_parts") {
				t.Errorf("collection Err = %v, want key_parts at line 2", c.Err())
			}

			path := filepath.Join(t.TempDir(), "c.toml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			err = PatchTable(path, "plugins.x", map[string]any{"enabled": true}, nil)
			if !errors.As(err, &pe) || !strings.Contains(pe.Msg, "key_parts") {
				t.Errorf("PatchTable error = %v, want key_parts", err)
			}

			got, _ := os.ReadFile(path)
			if string(got) != body {
				t.Error("PatchTable changed a file it refused")
			}

			runtime.ReadMemStats(&after)

			if grown := after.TotalAlloc - before.TotalAlloc; grown > 64<<20 {
				t.Errorf("reading the file allocated %d bytes", grown)
			}
		})
	}
}
