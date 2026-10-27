package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// tblKind names the kind of error a table test expects.
type tblKind int

const (
	// tblOK expects a successful edit.
	tblOK tblKind = iota
	// tblLayout expects an error wrapping ErrLayout.
	tblLayout
	// tblParse expects a *ParseError.
	tblParse
)

// tblCase is one row of the table-driven tests of patchBytes.
type tblCase struct {
	name  string
	in    string
	table string
	set   map[string]any
	unset []string
	want  string
	kind  tblKind
}

// tblApply runs the pure editor of PatchTable on a string.
//
// Parameters:
//   - t: test to report to
//   - in: file content
//   - table: dotted table path
//   - set: keys to write
//   - unset: keys to remove
//
// Returns the new content and the error of the edit.
func tblApply(t *testing.T, in, table string, set map[string]any, unset []string) (string, error) {
	t.Helper()

	tpath, err := parseTablePath(table)
	if err != nil {
		t.Fatal(err)
	}
	ed, err := newTableEdit(tpath, set, unset)
	if err != nil {
		t.Fatal(err)
	}
	out, err := patchBytes([]byte(in), tpath, ed)

	return string(out), err
}

// tblCheckKind checks an error against the kind a case expects.
//
// Parameters:
//   - t: test to report to
//   - err: error returned by the edit
//   - kind: expected kind
func tblCheckKind(t *testing.T, err error, kind tblKind) {
	t.Helper()

	var pe *ParseError
	switch kind {
	case tblOK:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case tblLayout:
		if !errors.Is(err, ErrLayout) {
			t.Fatalf("error = %v, want ErrLayout", err)
		}
	case tblParse:
		if !errors.As(err, &pe) || errors.Is(err, ErrLayout) {
			t.Fatalf("error = %v, want a ParseError", err)
		}
	}
}

// tblRun runs a list of cases and checks the output, the kind of error and idempotence.
//
// Parameters:
//   - t: test to report to
//   - cases: rows to run
func tblRun(t *testing.T, cases []tblCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := tc.table
			if table == "" && !strings.HasPrefix(tc.name, "root") {
				table = "plugins.gft"
			}

			got, err := tblApply(t, tc.in, table, tc.set, tc.unset)
			tblCheckKind(t, err, tc.kind)
			if tc.kind != tblOK {
				return
			}
			if got != tc.want {
				t.Fatalf("output:\n%q\nwant:\n%q", got, tc.want)
			}

			again, err := tblApply(t, got, table, tc.set, tc.unset)
			if err != nil || again != got {
				t.Fatalf("second run = %q, %v; want the same output", again, err)
			}
		})
	}
}

// TestPatchTableEdit checks the text produced for ordinary files.
//
// Parameters:
//   - t: test to report to
func TestPatchTableEdit(t *testing.T) {
	off := map[string]any{"enabled": false}
	tblRun(t, []tblCase{
		{
			name: "value replaced and trailing comment kept",
			in:   "[plugins.gft]\nenabled = true   # why\n",
			set:  off,
			want: "[plugins.gft]\nenabled = false   # why\n",
		},
		{
			name: "unchanged value keeps its spacing",
			in:   "[plugins.gft]\nenabled     =   false # c\n",
			set:  off,
			want: "[plugins.gft]\nenabled     =   false # c\n",
		},
		{
			name: "new key goes after the last key and before the next table",
			in:   "[plugins.gft]\nenabled = true\n\n# other\n[plugins.other]\nenabled = false\n",
			set:  map[string]any{"env": []string{"A", "B"}},
			want: "[plugins.gft]\nenabled = true\nenv = [\"A\", \"B\"]\n\n# other\n" +
				"[plugins.other]\nenabled = false\n",
		},
		{
			name: "new keys are sorted",
			in:   "[plugins.gft]\n",
			set:  map[string]any{"b": 2, "a": "x", "c": true},
			want: "[plugins.gft]\na = \"x\"\nb = 2\nc = true\n",
		},
		{
			name: "key added to an empty table goes after the header",
			in:   "[plugins.gft]\n# note\n\n[ui]\nascii = true\n",
			set:  off,
			want: "[plugins.gft]\nenabled = false\n# note\n\n[ui]\nascii = true\n",
		},
		{
			name: "missing table is appended after a blank line",
			in:   "[ui]\nascii = true\n",
			set:  off,
			want: "[ui]\nascii = true\n\n[plugins.gft]\nenabled = false\n",
		},
		{
			name: "missing table after a blank line gets no second one",
			in:   "[ui]\nascii = true\n\n",
			set:  off,
			want: "[ui]\nascii = true\n\n[plugins.gft]\nenabled = false\n",
		},
		{
			name: "missing final newline stays missing",
			in:   "[ui]\nascii = true",
			set:  off,
			want: "[ui]\nascii = true\n\n[plugins.gft]\nenabled = false",
		},
		{
			name: "missing final newline stays missing on an in-place edit",
			in:   "[plugins.gft]\nenabled = true",
			set:  off,
			want: "[plugins.gft]\nenabled = false",
		},
		{
			name: "final newline stays",
			in:   "[plugins.gft]\nenabled = true\n",
			set:  map[string]any{"x": 1},
			want: "[plugins.gft]\nenabled = true\nx = 1\n",
		},
		{
			name: "crlf file",
			in:   "[ui]\r\nascii = true\r\n\r\n[plugins.gft]\r\nenabled = true # c\r\n",
			set:  map[string]any{"enabled": false, "n": 3},
			want: "[ui]\r\nascii = true\r\n\r\n[plugins.gft]\r\nenabled = false # c\r\nn = 3\r\n",
		},
		{
			name: "crlf file gets a crlf table",
			in:   "a = 1\r\n",
			set:  off,
			want: "a = 1\r\n\r\n[plugins.gft]\r\nenabled = false\r\n",
		},
		{
			name: "byte order mark kept",
			in:   "\xef\xbb\xbf[plugins.gft]\nenabled = true\n",
			set:  off,
			want: "\xef\xbb\xbf[plugins.gft]\nenabled = false\n",
		},
		{
			name: "byte order mark before a key at the top",
			in:   "\xef\xbb\xbfa = 1\n",
			set:  off,
			want: "\xef\xbb\xbfa = 1\n\n[plugins.gft]\nenabled = false\n",
		},
		{
			name: "comments blank lines and unknown keys kept",
			in: "# top\n\n[plugins.gft]\n# about enabled\nenabled = true\n\nmine = \"x\" # mine\n" +
				"# tail\n",
			set: off,
			want: "# top\n\n[plugins.gft]\n# about enabled\nenabled = false\n\n" +
				"mine = \"x\" # mine\n# tail\n",
		},
		{
			name: "keys of other tables untouched",
			in: "[plugins.other]\nenabled = true\n\n[plugins.gft]\nenabled = true\n\n" +
				"[plugins.third]\nenabled = true\n",
			set: off,
			want: "[plugins.other]\nenabled = true\n\n[plugins.gft]\nenabled = false\n\n" +
				"[plugins.third]\nenabled = true\n",
		},
		{
			name: "same key in another table untouched",
			in:   "[plugins.other]\nenabled = true\n",
			set:  off,
			want: "[plugins.other]\nenabled = true\n\n[plugins.gft]\nenabled = false\n",
		},
		{
			name: "multi-line array replaced",
			in:   "[plugins.gft]\nenv = [\n  \"A\", # first\n  \"B\",\n]\nx = 1\n",
			set:  map[string]any{"env": []string{"C"}},
			want: "[plugins.gft]\nenv = [\"C\"]\nx = 1\n",
		},
		{
			name: "multi-line string key kept as it is",
			in:   "[plugins.gft]\ndoc = \"\"\"\n[plugins.gft]\n\"\"\"\nenabled = true\n",
			set:  off,
			want: "[plugins.gft]\ndoc = \"\"\"\n[plugins.gft]\n\"\"\"\nenabled = false\n",
		},
		{
			name: "empty list",
			in:   "[plugins.gft]\nenv = [\"A\"]\n",
			set:  map[string]any{"env": []string{}},
			want: "[plugins.gft]\nenv = []\n",
		},
		{
			name: "quoted existing key replaced by a bare one",
			in:   "[plugins.gft]\n\"enabled\" = true # c\n",
			set:  off,
			want: "[plugins.gft]\nenabled = false # c\n",
		},
		{
			name: "new key that needs quotes",
			in:   "[plugins.gft]\nenabled = true\n",
			set:  map[string]any{"my key": 1, "a.b": 2},
			want: "[plugins.gft]\nenabled = true\n\"a.b\" = 2\n\"my key\" = 1\n",
		},
		{
			name: "existing quoted key with a dot is found",
			in:   "[plugins.gft]\n\"a.b\" = 1\n",
			set:  map[string]any{"a.b": 5},
			want: "[plugins.gft]\n\"a.b\" = 5\n",
		},
		{
			name: "header written with quotes, spaces and a comment",
			in:   "[ plugins . \"gft\" ]  # mine\nenabled = true\n",
			set:  off,
			want: "[ plugins . \"gft\" ]  # mine\nenabled = false\n",
		},
		{
			name: "sub-table after the target does not take the new key",
			in:   "[plugins.gft]\nenabled = true\n[plugins.gft.sub]\nx = 1\n",
			set:  map[string]any{"y": 2},
			want: "[plugins.gft]\nenabled = true\ny = 2\n[plugins.gft.sub]\nx = 1\n",
		},
		{
			name: "sub-table before the target",
			in:   "[plugins.gft.sub]\nx = 1\n\n[plugins.gft]\nenabled = true\n",
			set:  off,
			want: "[plugins.gft.sub]\nx = 1\n\n[plugins.gft]\nenabled = false\n",
		},
		{
			name: "sub-table only, target created",
			in:   "[plugins.gft.sub]\nx = 1\n",
			set:  off,
			want: "[plugins.gft.sub]\nx = 1\n\n[plugins.gft]\nenabled = false\n",
		},
		{
			name: "super table with other children",
			in:   "[plugins]\nnote = 1\n\n[plugins.other]\nx = 1\n",
			set:  off,
			want: "[plugins]\nnote = 1\n\n[plugins.other]\nx = 1\n\n[plugins.gft]\nenabled = false\n",
		},
		{
			name: "dotted key of a sibling is not a conflict",
			in:   "[plugins.other]\nx.y = 1\n",
			set:  off,
			want: "[plugins.other]\nx.y = 1\n\n[plugins.gft]\nenabled = false\n",
		},
	})
}

// TestPatchTableUnset checks the removal of keys.
//
// Parameters:
//   - t: test to report to
func TestPatchTableUnset(t *testing.T) {
	tblRun(t, []tblCase{
		{
			name:  "key removed, comment above and other keys kept",
			in:    "[plugins.gft]\n# about\nenabled = true # c\nx = 1\n",
			unset: []string{"enabled"},
			want:  "[plugins.gft]\n# about\nx = 1\n",
		},
		{
			name:  "last key removed leaves the header",
			in:    "[plugins.gft]\nenabled = true\n\n[ui]\na = 1\n",
			unset: []string{"enabled"},
			want:  "[plugins.gft]\n\n[ui]\na = 1\n",
		},
		{
			name:  "multi-line key removed",
			in:    "[plugins.gft]\nenv = [\n \"A\",\n]\nx = 1\n",
			unset: []string{"env"},
			want:  "[plugins.gft]\nx = 1\n",
		},
		{
			name:  "absent key ignored",
			in:    "[plugins.gft]\nx = 1\n",
			unset: []string{"enabled"},
			want:  "[plugins.gft]\nx = 1\n",
		},
		{
			name:  "absent table ignored and not created",
			in:    "[ui]\na = 1\n",
			unset: []string{"enabled"},
			want:  "[ui]\na = 1\n",
		},
		{
			name:  "key of another table kept",
			in:    "[plugins.other]\nenabled = true\n[plugins.gft]\nenabled = true\n",
			unset: []string{"enabled"},
			want:  "[plugins.other]\nenabled = true\n[plugins.gft]\n",
		},
		{
			name:  "set and unset together",
			in:    "[plugins.gft]\nold = 1\nenabled = true\n",
			set:   map[string]any{"enabled": false, "new": "x"},
			unset: []string{"old"},
			want:  "[plugins.gft]\nenabled = false\nnew = \"x\"\n",
		},
		{
			name:  "missing final newline after a removal",
			in:    "[plugins.gft]\na = 1\nb = 2",
			unset: []string{"b"},
			want:  "[plugins.gft]\na = 1",
		},
	})
}

// TestPatchTableRoot checks the edit of the root keys of a flat file.
//
// Parameters:
//   - t: test to report to
func TestPatchTableRoot(t *testing.T) {
	tblRun(t, []tblCase{
		{
			name: "root replace",
			in:   "schema = 1\n# language\nlang = \"en\" # c\n\n[ui]\nlang = \"x\"\n",
			set:  map[string]any{"lang": "fr"},
			want: "schema = 1\n# language\nlang = \"fr\" # c\n\n[ui]\nlang = \"x\"\n",
		},
		{
			name: "root new key goes before the first table",
			in:   "schema = 1\n\n# --- ui ---\n[ui]\nlang = \"x\"\n",
			set:  map[string]any{"lang": "fr"},
			want: "schema = 1\nlang = \"fr\"\n\n# --- ui ---\n[ui]\nlang = \"x\"\n",
		},
		{
			name: "root key in a file made of comments",
			in:   "# only a comment\n",
			set:  map[string]any{"lang": "fr"},
			want: "lang = \"fr\"\n# only a comment\n",
		},
		{
			name: "root key before a table",
			in:   "[ui]\nascii = true\n",
			set:  map[string]any{"lang": "fr"},
			want: "lang = \"fr\"\n[ui]\nascii = true\n",
		},
		{
			name: "root unknown keys and crlf",
			in:   "schema = 1\r\nmine = 3\r\nlang = \"en\"\r\n",
			set:  map[string]any{"lang": "fr", "n": 2},
			want: "schema = 1\r\nmine = 3\r\nlang = \"fr\"\r\nn = 2\r\n",
		},
		{
			name:  "root unset",
			in:    "schema = 1\nlang = \"en\"\nmine = 3\n",
			unset: []string{"lang"},
			want:  "schema = 1\nmine = 3\n",
		},
		{
			name: "root new file",
			in:   "",
			set:  map[string]any{"lang": "fr"},
			want: "schema = 1\nlang = \"fr\"\n",
		},
		{
			name: "root only blank lines",
			in:   "\n\n",
			set:  map[string]any{"lang": "fr"},
			want: "schema = 1\nlang = \"fr\"\n",
		},
		{
			name:  "root unset on an empty file creates nothing",
			in:    "",
			unset: []string{"lang"},
			want:  "",
		},
	})
}

// TestPatchTableNewFile checks the content of a file that did not exist.
//
// Parameters:
//   - t: test to report to
func TestPatchTableNewFile(t *testing.T) {
	tblRun(t, []tblCase{
		{
			name: "new file with a nested table",
			in:   "",
			set:  map[string]any{"enabled": false, "env": []string{"A"}},
			want: "schema = 1\n\n[plugins.gft]\nenabled = false\nenv = [\"A\"]\n",
		},
		{
			name: "new file with one segment",
			in:   "",
			set:  map[string]any{"a": 1},
			want: "schema = 1\n\n[plugins.gft]\na = 1\n",
		},
	})
}

// TestPatchTableValues checks that awkward values round-trip through the encoder and the decoder.
//
// Parameters:
//   - t: test to report to
func TestPatchTableValues(t *testing.T) {
	values := map[string]any{
		"quote":     `say "hi"`,
		"backslash": `C:\dir\file`,
		"newline":   "a\nb\r\nc",
		"tab":       "a\tb",
		"control":   "a\x00b\x01c\x1b[0m\x7f",
		"unicode":   "caf\u00e9 \u65e5\u672c\u8a9e \U0001F600",
		"separator": "a\u2028b",
		"hash":      "a # b",
		"bracket":   "[plugins.gft]",
		"triple":    `"""`,
		"empty":     "",
		"negative":  -42,
		"big":       int64(1) << 62,
		"uint":      uint8(200),
		"flag":      true,
		"list":      []string{"a", "b\"c", "d\ne", ""},
		"nothing":   []string(nil),
	}

	for _, in := range []string{"", "[plugins.gft]\n", "a = 1\r\n"} {
		out, err := tblApply(t, in, "plugins.gft", values, nil)
		if err != nil {
			t.Fatalf("input %q: %v", in, err)
		}

		var tree map[string]map[string]map[string]any
		if _, err := toml.Decode(out, &tree); err != nil {
			t.Fatalf("output does not parse: %v\n%s", err, out)
		}
		got := tree["plugins"]["gft"]
		for k, v := range values {
			_, want, _ := encodeValue(v)
			if !reflect.DeepEqual(got[k], want) {
				t.Errorf("key %q = %#v, want %#v", k, got[k], want)
			}
		}
	}
}

// TestPatchTableHostile checks the layouts that must be refused untouched.
//
// Parameters:
//   - t: test to report to
func TestPatchTableHostile(t *testing.T) {
	off := map[string]any{"enabled": false}
	cases := []tblCase{
		{name: "dotted key at the root", in: "plugins.gft.a = 1\n", kind: tblLayout},
		{
			name: "dotted key in the super table", in: "[plugins]\ngft.enabled = true\n",
			kind: tblLayout,
		},
		{name: "dotted key under a dotted key", in: "[plugins]\ngft.sub.x = 1\n", kind: tblLayout},
		{
			name: "inline table for the target", in: "[plugins]\ngft = { enabled = true }\n",
			kind: tblLayout,
		},
		{
			name: "inline table for the super table",
			in:   "plugins = { gft = { enabled = true } }\n",
			kind: tblLayout,
		},
		{name: "empty inline table for the target", in: "[plugins]\ngft = {}\n", kind: tblLayout},
		{name: "scalar where the table should be", in: "[plugins]\ngft = 1\n", kind: tblLayout},
		{
			name: "array of tables with the target name", in: "[[plugins.gft]]\na = 1\n",
			kind: tblLayout,
		},
		{
			name: "array of tables above the target",
			in:   "[[plugins]]\n[plugins.gft]\nenabled = true\n",
			kind: tblLayout,
		},
		{
			name: "duplicate target", in: "[plugins.gft]\na = 1\n[plugins.gft]\nb = 2\n",
			kind: tblLayout,
		},
		{
			name: "duplicate target written differently",
			in:   "[plugins.gft]\na = 1\n[plugins.\"gft\"]\nb = 2\n", kind: tblLayout,
		},
		{name: "dotted key inside the target", in: "[plugins.gft]\na.b = 1\n", kind: tblLayout},
		{
			name: "inline table inside the target", in: "[plugins.gft]\na = { b = 1 }\n",
			kind: tblLayout,
		},
		{
			name: "array of inline tables inside the target",
			in:   "[plugins.gft]\na = [{ b = 1 }]\n",
			kind: tblLayout,
		},
		{
			name: "dotted key at the root with the target also declared",
			in:   "plugins.gft.a = 1\n[plugins.gft]\nb = 2\n", kind: tblLayout,
		},
		{name: "root dotted key", in: "a.b = 1\n", table: "root", kind: tblLayout},
		{name: "root inline table", in: "a = { b = 1 }\n", table: "root", kind: tblLayout},
		{name: "syntax error", in: "[plugins.gft]\nenabled =\n", kind: tblParse},
		{name: "unclosed header", in: "[plugins.gft\nenabled = 1\n", kind: tblParse},
		{name: "duplicate key", in: "[plugins.gft]\na = 1\na = 2\n", kind: tblParse},
		{name: "bad bom position", in: "a = 1\n\xef\xbb\xbfb = 2\n", kind: tblParse},
		{name: "nesting bomb", in: "a = " + strings.Repeat("[", 5000) + "\n", kind: tblParse},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := "plugins.gft"
			if tc.table == "root" {
				table = ""
			}

			_, err := tblApply(t, tc.in, table, off, nil)
			tblCheckKind(t, err, tc.kind)

			// The detector must catch the layout itself, not only the final verification.
			if tc.kind == tblLayout {
				tpath, _ := parseTablePath(table)
				ps, ok := classifyPieces([]byte(tc.in))
				if !ok || !errors.Is(checkTableLayout(ps, tpath), ErrLayout) {
					t.Fatalf("checkTableLayout missed %q", tc.in)
				}
			}
		})
	}
}

// TestPatchTableVerifyBackstop checks that the final decode refuses a result the layout scan
// could not predict.
//
// Parameters:
//   - t: test to report to
func TestPatchTableVerifyBackstop(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		table string
		set   map[string]any
	}{
		{
			"root key clashes with a later table", "[lang]\nx = 1\n", "",
			map[string]any{"lang": "fr"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tblApply(t, tc.in, tc.table, tc.set, nil)
			if !errors.Is(err, ErrLayout) {
				t.Fatalf("error = %v, want ErrLayout", err)
			}
		})
	}
}

// TestPatchTableDottedAbove checks that a dotted key above the table is only refused when it
// reaches the table.
//
// Parameters:
//   - t: test to report to
func TestPatchTableDottedAbove(t *testing.T) {
	off := map[string]any{"enabled": false}

	out, err := tblApply(t, "plugins.other.x = 1\n", "plugins.gft", off, nil)
	if err != nil {
		t.Logf("sibling dotted key refused: %v", err)
		if !errors.Is(err, ErrLayout) {
			t.Fatalf("error = %v, want ErrLayout or success", err)
		}
		return
	}

	var tree map[string]any
	if _, err := toml.Decode(out, &tree); err != nil {
		t.Fatalf("output does not parse: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "plugins.other.x = 1\n") {
		t.Fatalf("sibling dotted key changed: %q", out)
	}
}

// TestPatchTableArguments checks the validation of the arguments.
//
// Parameters:
//   - t: test to report to
func TestPatchTableArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	cases := []struct {
		name  string
		table string
		set   map[string]any
		unset []string
	}{
		{"space in table", "plugins gft", map[string]any{"a": 1}, nil},
		{"empty segment", "plugins..gft", map[string]any{"a": 1}, nil},
		{"quoted segment", `plugins."gft"`, map[string]any{"a": 1}, nil},
		{"empty key", "plugins.gft", map[string]any{"": 1}, nil},
		{"empty unset key", "plugins.gft", nil, []string{""}},
		{"schema at the root", "", map[string]any{"schema": 2}, nil},
		{"schema unset at the root", "", nil, []string{"schema"}},
		{"float", "plugins.gft", map[string]any{"a": 1.5}, nil},
		{"nil", "plugins.gft", map[string]any{"a": nil}, nil},
		{"map", "plugins.gft", map[string]any{"a": map[string]any{}}, nil},
		{"slice of ints", "plugins.gft", map[string]any{"a": []int{1}}, nil},
		{"huge uint", "plugins.gft", map[string]any{"a": uint64(1) << 63}, nil},
		{"invalid utf-8 value", "plugins.gft", map[string]any{"a": "a\xffb"}, nil},
		{"invalid utf-8 key", "plugins.gft", map[string]any{"a\xff": 1}, nil},
		{"set and unset", "plugins.gft", map[string]any{"a": 1}, []string{"a"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := PatchTable(path, tc.table, tc.set, tc.unset); err == nil {
				t.Fatal("expected an error")
			}
			if _, err := os.Lstat(path); err == nil {
				t.Fatal("the file was created")
			}
		})
	}
}

// tblWrite writes a test file.
//
// Parameters:
//   - t: test to report to
//   - path: file path
//   - text: content
func tblWrite(t *testing.T, path, text string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// tblRead reads a test file.
//
// Parameters:
//   - t: test to report to
//   - path: file path
func tblRead(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// tblLeftovers lists the files of a directory that are neither the data file nor its lock.
//
// Parameters:
//   - t: test to report to
//   - dir: directory to list
//   - keep: names that are expected
func tblLeftovers(t *testing.T, dir string, keep ...string) []string {
	t.Helper()

	var extra []string
	for _, name := range engListDir(t, dir) {
		known := false
		for _, k := range keep {
			known = known || name == k || name == k+".lock"
		}
		if !known {
			extra = append(extra, name)
		}
	}

	return extra
}

// TestPatchTableFile checks the creation and the edit of a file on disk.
//
// Parameters:
//   - t: test to report to
func TestPatchTableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "c.toml")

	if err := PatchTable(path, "plugins.gft", map[string]any{"enabled": false}, nil); err != nil {
		t.Fatal(err)
	}
	if got := tblRead(t, path); got != "schema = 1\n\n[plugins.gft]\nenabled = false\n" {
		t.Fatalf("new file = %q", got)
	}

	if err := PatchTable(path, "plugins.gft", map[string]any{"enabled": true}, nil); err != nil {
		t.Fatal(err)
	}
	if err := PatchTable(path, "plugins.other", map[string]any{"enabled": false}, nil); err != nil {
		t.Fatal(err)
	}
	want := "schema = 1\n\n[plugins.gft]\nenabled = true\n\n[plugins.other]\nenabled = false\n"
	if got := tblRead(t, path); got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	if extra := tblLeftovers(t, filepath.Dir(path), "c.toml"); len(extra) > 0 {
		t.Fatalf("leftovers: %v", extra)
	}
}

// TestPatchTableNoWrite checks that an edit that changes nothing leaves the file alone.
//
// Parameters:
//   - t: test to report to
func TestPatchTableNoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	tblWrite(t, path, "[plugins.gft]\nenabled = false\n")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := PatchTable(path, "plugins.gft", map[string]any{"enabled": false}, nil); err != nil {
		t.Fatal(err)
	}
	if err := PatchTable(path, "plugins.gft", nil, []string{"absent"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("the file was rewritten")
	}

	missing := filepath.Join(dir, "none", "x.toml")
	if err := PatchTable(missing, "plugins.gft", nil, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := PatchTable(missing, "plugins.gft", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); err == nil {
		t.Fatal("a no-op created a directory")
	}
}

// TestPatchTableMode checks the permission bits under permissive and strict umasks.
//
// Parameters:
//   - t: test to report to
func TestPatchTableMode(t *testing.T) {
	for _, mask := range []int{0, 0o077, 0o022} {
		t.Run(fmt.Sprintf("umask %03o", mask), func(t *testing.T) {
			old := syscall.Umask(mask)
			defer syscall.Umask(old)

			dir := t.TempDir()
			path := filepath.Join(dir, "a", "c.toml")
			set := map[string]any{"x": 1}
			if err := PatchTable(path, "plugins.gft", set, nil); err != nil {
				t.Fatal(err)
			}
			if err := PatchTable(path, "plugins.gft", map[string]any{"x": 2}, nil); err != nil {
				t.Fatal(err)
			}

			for _, p := range []string{path, path + ".lock"} {
				if m := engMode(t, p); m != 0o600 {
					t.Errorf("%s mode = %o, want 600", p, m)
				}
			}
			if m := engMode(t, filepath.Dir(path)); m != 0o700 {
				t.Errorf("directory mode = %o, want 700", m)
			}
		})
	}
}

// TestPatchTableKeepsMode checks that an existing file keeps its mode and a read-only one is
// refused.
//
// Parameters:
//   - t: test to report to
func TestPatchTableKeepsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	tblWrite(t, path, "[plugins.gft]\nx = 1\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	if err := PatchTable(path, "plugins.gft", map[string]any{"x": 2}, nil); err != nil {
		t.Fatal(err)
	}
	if m := engMode(t, path); m != 0o640 {
		t.Errorf("mode = %o, want 640", m)
	}

	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := PatchTable(path, "plugins.gft", map[string]any{"x": 3}, nil); err == nil {
		t.Fatal("a read-only file was edited")
	}
	if got := tblRead(t, path); got != "[plugins.gft]\nx = 2\n" {
		t.Errorf("read-only file changed: %q", got)
	}
}

// TestPatchTableSymlink checks that the target of a symlink is edited and the link kept.
//
// Parameters:
//   - t: test to report to
func TestPatchTableSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(real), 0o700); err != nil {
		t.Fatal(err)
	}
	tblWrite(t, real, "# mine\n[plugins.gft]\nenabled = true\n")
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := PatchTable(link, "plugins.gft", map[string]any{"enabled": false}, nil); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v", err)
	}
	if got := tblRead(t, real); got != "# mine\n[plugins.gft]\nenabled = false\n" {
		t.Fatalf("target = %q", got)
	}
	if extra := tblLeftovers(t, filepath.Dir(real), "settings.toml"); len(extra) > 0 {
		t.Fatalf("leftovers beside the target: %v", extra)
	}

	dangling := filepath.Join(dir, "dangling.toml")
	if err := os.Symlink(filepath.Join(dir, "nowhere.toml"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := PatchTable(dangling, "plugins.gft", map[string]any{"x": 1}, nil); err == nil {
		t.Fatal("a dangling symlink was accepted")
	}
	if err := PatchTable(dangling, "plugins.gft", nil, []string{"x"}); err == nil {
		t.Fatal("a dangling symlink was accepted for an unset")
	}
}

// TestPatchTableRefusalsLeaveFile checks that a refused edit does not touch the file.
//
// Parameters:
//   - t: test to report to
func TestPatchTableRefusalsLeaveFile(t *testing.T) {
	cases := map[string]error{
		"plugins.gft.a = 1\n":                   ErrLayout,
		"[plugins.gft]\na = 1\n[plugins.gft]\n": ErrLayout,
		"[plugins]\ngft = { a = 1 }\n":          ErrLayout,
	}

	for text, want := range cases {
		path := filepath.Join(t.TempDir(), "c.toml")
		tblWrite(t, path, text)

		err := PatchTable(path, "plugins.gft", map[string]any{"enabled": false}, nil)
		if !errors.Is(err, want) {
			t.Errorf("%q: error = %v, want %v", text, err, want)
		}
		if got := tblRead(t, path); got != text {
			t.Errorf("%q: file changed to %q", text, got)
		}
		if extra := tblLeftovers(t, filepath.Dir(path), "c.toml"); len(extra) > 0 {
			t.Errorf("%q: leftovers %v", text, extra)
		}
	}

	path := filepath.Join(t.TempDir(), "c.toml")
	tblWrite(t, path, "[plugins.gft\n")
	err := PatchTable(path, "plugins.gft", map[string]any{"enabled": false}, nil)
	var pe *ParseError
	if !errors.As(err, &pe) || pe.File != path || pe.Line < 1 {
		t.Fatalf("error = %#v, want a ParseError for %s", err, path)
	}
}

// TestPatchTableNotRegular checks that a directory or a FIFO is refused without blocking.
//
// Parameters:
//   - t: test to report to
func TestPatchTableNotRegular(t *testing.T) {
	dir := t.TempDir()
	if err := PatchTable(dir, "plugins.gft", map[string]any{"x": 1}, nil); err == nil {
		t.Fatal("a directory was accepted")
	}

	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("no fifo here")
	}
	if err := PatchTable(fifo, "plugins.gft", map[string]any{"x": 1}, nil); err == nil {
		t.Fatal("a fifo was accepted")
	}
}

// TestPatchTableBusy checks that a lock held elsewhere gives ErrBusy and leaves the file alone.
//
// Parameters:
//   - t: test to report to
func TestPatchTableBusy(t *testing.T) {
	old := tableLockWait
	tableLockWait = 30 * time.Millisecond
	defer func() { tableLockWait = old }()

	path := filepath.Join(t.TempDir(), "c.toml")
	tblWrite(t, path, "[plugins.gft]\nx = 1\n")
	lock, err := acquireLock(path, 0)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err = PatchTable(path, "plugins.gft", map[string]any{"x": 2}, nil)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("error = %v, want ErrBusy", err)
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Error("the call did not wait for the lock")
	}
	if got := tblRead(t, path); got != "[plugins.gft]\nx = 1\n" {
		t.Fatalf("file changed under a held lock: %q", got)
	}

	releaseLock(lock)
	if err := PatchTable(path, "plugins.gft", map[string]any{"x": 2}, nil); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

// TestPatchTableSharesLockWithCollection checks that a collection write holds PatchTable off.
//
// Parameters:
//   - t: test to report to
func TestPatchTableSharesLockWithCollection(t *testing.T) {
	old := tableLockWait
	tableLockWait = 20 * time.Millisecond
	defer func() { tableLockWait = old }()

	path := filepath.Join(t.TempDir(), "c.toml")
	tblWrite(t, path, "schema = 1\n")
	target, err := resolveTarget(path)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := acquireLock(target, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLock(lock)

	err = PatchTable(path, "plugins.gft", map[string]any{"x": 1}, nil)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("error = %v, want ErrBusy", err)
	}
}

// TestPatchTableAtomic checks that a failed rename leaves the old file and no temporary file.
//
// Parameters:
//   - t: test to report to
func TestPatchTableAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	tblWrite(t, path, "[plugins.gft]\nx = 1\n")

	old := renameFile
	renameFile = func(string, string) error { return errors.New("crash") }
	err := PatchTable(path, "plugins.gft", map[string]any{"x": 2}, nil)
	missing := PatchTable(filepath.Join(dir, "new.toml"), "plugins.gft", map[string]any{"x": 2}, nil)
	renameFile = old

	if err == nil || missing == nil {
		t.Fatalf("errors = %v, %v; want failures", err, missing)
	}
	if got := tblRead(t, path); got != "[plugins.gft]\nx = 1\n" {
		t.Fatalf("file after a crash = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.toml")); err == nil {
		t.Fatal("a new file appeared after a crash")
	}
	if extra := tblLeftovers(t, dir, "c.toml", "new.toml"); len(extra) > 0 {
		t.Fatalf("temporary files left: %v", extra)
	}
}

// TestPatchTableConcurrent checks that parallel writers lose no update.
//
// Parameters:
//   - t: test to report to
func TestPatchTableConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	tblWrite(t, path, "# keep me\n[ui]\nascii = true\n")

	const workers, rounds = 8, 10
	var wg sync.WaitGroup
	errs := make(chan error, workers*rounds)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range rounds {
				table := fmt.Sprintf("plugins.p%d", w%3)
				key := fmt.Sprintf("k%d_%d", w, r)
				errs <- PatchTable(path, table, map[string]any{key: r}, nil)
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	text := tblRead(t, path)
	var tree map[string]map[string]map[string]any
	if _, err := toml.Decode(text, &tree); err != nil {
		t.Fatalf("file does not parse: %v\n%s", err, text)
	}
	total := 0
	for _, tbl := range tree["plugins"] {
		total += len(tbl)
	}
	if total != workers*rounds {
		t.Errorf("%d keys survived, want %d\n%s", total, workers*rounds, text)
	}
	if !strings.HasPrefix(text, "# keep me\n[ui]\nascii = true\n") {
		t.Errorf("existing content changed:\n%s", text)
	}
	if extra := tblLeftovers(t, dir, "c.toml"); len(extra) > 0 {
		t.Errorf("leftovers: %v", extra)
	}
}

// TestPatchTableLargeFile checks that a file past the size cap is refused.
//
// Parameters:
//   - t: test to report to
func TestPatchTableLargeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	tblWrite(t, path, "# "+strings.Repeat("x", maxFileSize)+"\n")

	if err := PatchTable(path, "plugins.gft", map[string]any{"x": 1}, nil); err == nil {
		t.Fatal("a file over the cap was accepted")
	}
}

// tblStrip returns a copy of a decoded tree without some keys of one table.
//
// A table on the path that ends up empty is removed too, so that a table the edit created and a
// table that was absent compare equal.
//
// Parameters:
//   - tree: decoded file
//   - tpath: table path, nil for the root
//   - drop: keys to remove from that table
func tblStrip(tree map[string]any, tpath []string, drop map[string]bool) map[string]any {
	out := map[string]any{}
	for k, v := range tree {
		out[k] = v
	}

	if len(tpath) == 0 {
		for k := range drop {
			delete(out, k)
		}
		return out
	}

	if sub, ok := out[tpath[0]].(map[string]any); ok {
		out[tpath[0]] = tblStrip(sub, tpath[1:], drop)
		if len(out[tpath[0]].(map[string]any)) == 0 {
			delete(out, tpath[0])
		}
	}

	return out
}

// tblTree decodes a file, failing the test when it does not parse.
//
// Parameters:
//   - t: test to report to
//   - data: file content, possibly with a byte order mark
func tblTree(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var tree map[string]any
	text := string(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	if _, err := toml.Decode(text, &tree); err != nil {
		t.Fatalf("does not parse: %v\n%q", err, data)
	}

	return tree
}

// FuzzPatchTableLayout checks the layout detector and the editor on arbitrary files.
//
// Whatever the input, the editor must not panic and must return the old content, an ErrLayout
// or a ParseError. When it succeeds, the new content must parse, hold the requested values, lack
// the removed keys, leave every other key as it was, and be a fixed point of the same edit.
//
// Parameters:
//   - f: fuzzing driver
func FuzzPatchTableLayout(f *testing.F) {
	seeds := []string{
		"",
		"[plugins.gft]\nenabled = true\n",
		"[plugins.gft]\r\nenabled = true # c\r\n",
		"plugins.gft.enabled = true\n",
		"[plugins]\ngft = { enabled = true }\n",
		"[[plugins.gft]]\nx = 1\n",
		"[plugins.gft]\n[plugins.gft]\n",
		"\xef\xbb\xbf# c\nold = 1\n[plugins.gft]\nenv = [\n \"a\",\n]\nold = 2\n",
		"a = '''\n[plugins.gft]\n'''\n",
		"[ plugins . \"gft\" ] # c\nenabled = 1",
		"a = [[[[[[1]]]]]]\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s), true)
		f.Add([]byte(s), false)
	}

	set := map[string]any{"enabled": false, "env": []string{"A", "b\"c"}}
	unset := []string{"old"}

	f.Fuzz(func(t *testing.T, data []byte, nested bool) {
		table := ""
		if nested {
			table = "plugins.gft"
		}
		tpath, _ := parseTablePath(table)
		ed, err := newTableEdit(tpath, set, unset)
		if err != nil {
			t.Fatal(err)
		}

		out, err := patchBytes(data, tpath, ed)
		if err != nil {
			var pe *ParseError
			if !errors.Is(err, ErrLayout) && !errors.As(err, &pe) {
				t.Fatalf("unexpected error type: %v", err)
			}
			return
		}

		before, after := tblTree(t, data), tblTree(t, out)
		drop := map[string]bool{"enabled": true, "env": true, "old": true}
		blank := len(bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))) == 0
		if !blank && !reflect.DeepEqual(tblStrip(before, tpath, drop), tblStrip(after, tpath, drop)) {
			t.Fatalf("other keys changed:\n%q\n%q", data, out)
		}

		again, err := patchBytes(out, tpath, ed)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("not a fixed point: %q -> %q (%v)", out, again, err)
		}
	})
}
