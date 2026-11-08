package plugin

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// hostileManifests builds the structures that made the TOML library allocate gigabytes, each
// sized to the 256 KiB limit of a manifest, plus smaller ones that sit just beyond a limit.
func hostileManifests() map[string]string {
	const size = MaxManifestSize

	many := func(format string, limit int) string {
		var b strings.Builder

		for i := 0; b.Len() < limit; i++ {
			fmt.Fprintf(&b, format, i)
		}

		return b.String()
	}

	return map[string]string{
		"nested arrays":     "a = " + times("[", size/2) + times("]", size/2-10),
		"nested matched":    "a = " + times("[", 60000) + times("]", 60000),
		"nested inline":     "a = " + times("{a=", size/4),
		"nested matched in": "a = " + times("{a=", 20000) + "1" + times("}", 20000),
		"deep header":       "[" + times("a.", size/4) + "a]\n",
		"deep array header": "[[" + times("a.", size/4) + "a]]\n",
		"deep dotted key":   times("a.", size/4) + "a = 1\n",
		"deep inline key":   "a = {" + times("a.", size/4) + "a = 1}\n",
		"header repeats":    times("["+times("a.", 100)+"a]\n", size/204),
		"many headers":      times("[[command]]\n", size/12),
		"many keys":         many("k%d = 1\n", size-1000),
		"many dotted keys":  many("a.b.c%d.d = 1\n", size-1000),
		"many headers 2":    many("[a.b%d.c]\n", size-1000),
		"long line":         "a = \"" + times("x", size-10) + "\"",
		"long array line":   "a = [" + times("1,", size/2-10) + "]",
		"oversized":         times("# c\n", size/4+10),
		"nested at 5":       "a = [[[[[1]]]]]\n",
		"header at 9":       "[a.b.c.d.e.f.g.h.i]\n",
		"key at 9":          "a.b.c.d.e.f.g.h.i = 1\n",
		"quotes":            times("\"\"\"", size/3),
	}
}

// TestHostileManifests checks that every hostile structure is refused before the TOML library
// sees it: no manifest, an error, a bounded allocation (the structures that pass the limits cost
// tens of megabytes), whatever the memory
// that the library would have used. The test sets a soft memory limit as a second net; the
// whole package is also run under a cgroup memory cap.
//
// Parameters:
//   - t: test to report to
func TestHostileManifests(t *testing.T) {
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(768 << 20))

	for name, src := range hostileManifests() {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats

			runtime.ReadMemStats(&before)

			m, rep := Decode([]byte(src), DecodeOptions{Host: testHost()})

			runtime.ReadMemStats(&after)

			if m != nil || len(rep.Errors()) == 0 {
				t.Fatalf("hostile manifest accepted: %v", rep.Issues)
			}

			if grown := after.TotalAlloc - before.TotalAlloc; grown > 96<<20 {
				t.Errorf("reading the manifest allocated %d bytes", grown)
			}

			if rep.Class() != ClassInvalid {
				t.Errorf("class = %v, want invalid", rep.Class())
			}
		})
	}
}

// TestShapeIssues checks the code, the limit name and the line that a structure limit reports.
//
// Parameters:
//   - t: test to report to
func TestShapeIssues(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		code  string
		limit string
		line  int
	}{
		{"size", times("#\n", MaxManifestSize), CodeManifestTooLarge, "", 0},
		{"nesting", header + "x = [[[[[1]]]]]\n", CodeLimitExceeded, "nesting", 7},
		{"header parts", header + "[a.b.c.d.e.f.g.h.i]\n", CodeLimitExceeded, "key_parts", 7},
		{"key parts", header + "a.b.c.d.e.f.g.h.i = 1\n", CodeLimitExceeded, "key_parts", 7},
		{"headers", times("[[a]]\n", MaxHeaders+1), CodeLimitExceeded, "headers", MaxHeaders + 1},
		{"keys", times("a = 1\n", MaxKeys+1), CodeLimitExceeded, "keys", MaxKeys + 1},
		{"line", header + "x = \"" + times("y", MaxLineBytes) + "\"\n", CodeLimitExceeded, "line", 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, rep := decode(tt.src)
			if m != nil {
				t.Fatal("manifest accepted")
			}

			is := find(rep, tt.code, "")
			if is == nil {
				t.Fatalf("no %s in %v", tt.code, rep.Issues)
			}

			if tt.limit != "" && (len(is.Args) < 1 || is.Args[0] != tt.limit) {
				t.Errorf("args = %v, want the limit %s", is.Args, tt.limit)
			}

			if is.Line != tt.line {
				t.Errorf("line = %d, want %d", is.Line, tt.line)
			}
		})
	}
}

// TestShapeLimitsAccept checks that the limits leave room for what the specification allows: the
// deepest legitimate header, a dotted locale key, nested values at the limit.
//
// Parameters:
//   - t: test to report to
func TestShapeLimitsAccept(t *testing.T) {
	src := decl("", tool, cmdBody+`
[[command]]
id = "deep"
label = "Deep"
run = ["tool"]
output = "pager"
keywords = [["x"]]

[i18n.fr]
command.go.field.x.choice.value = "Valeur"
"command.go.label" = "Aller"
`)

	_, rep := decode(src)
	if find(rep, CodeLimitExceeded, "") != nil {
		t.Errorf("a limit refused a legitimate structure: %v", rep.Issues)
	}
}

// TestHostileLocales checks the same refusal for a locale file.
//
// Parameters:
//   - t: test to report to
func TestHostileLocales(t *testing.T) {
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(768 << 20))

	tests := map[string]string{
		"deep header": "[" + times("a.", 60000) + "a]\n",
		"deep key":    times("a.", 60000) + "a = \"x\"\n",
		"nested":      "a = " + times("{a=", 30000),
		"oversized":   times("# c\n", MaxLocaleSize/4+10),
		"long line":   "a = \"" + times("x", 20000) + "\"",
	}

	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats

			runtime.ReadMemStats(&before)

			out, issues := ParseLocale([]byte(src))

			runtime.ReadMemStats(&after)

			if out != nil || len(issues) == 0 {
				t.Fatalf("hostile locale accepted: %v", issues)
			}

			if grown := after.TotalAlloc - before.TotalAlloc; grown > 16<<20 {
				t.Errorf("reading the locale allocated %d bytes", grown)
			}
		})
	}
}

// FuzzParseLocale feeds arbitrary bytes to the locale reader. The invariants: no panic, a table
// unless the file was refused as locale_invalid, and every key and value of the table is clean
// text.
//
// Parameters:
//   - f: fuzz target
func FuzzParseLocale(f *testing.F) {
	for _, name := range []string{"gft/locales/en.toml", "gft/locales/fr.toml"} {
		f.Add(readFixture(f, name))
	}

	for _, s := range []string{
		"", "a = \"x\"", "a.b.c = \"x\"", "[a.b]\nc = \"d\"", "a = 1", "a = [\"x\"]",
		"[" + times("a.", 50) + "a]\n", "a = " + times("{a=", 50), "\xff", "a = \"\\u001b\"",
	} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		out, issues := ParseLocale(data)
		for _, is := range issues {
			if is.Code == CodeLocaleInvalid && out != nil {
				t.Fatalf("a refused file returned a table: %v", is)
			}
		}

		if len(issues) == 0 && out == nil {
			t.Fatal("no table and no issue")
		}

		for k, v := range out {
			if !plainText(k) || !plainText(v) {
				t.Fatalf("unclean entry %q = %q", k, v)
			}
		}
	})
}
