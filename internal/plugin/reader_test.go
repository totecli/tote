package plugin

import (
	"slices"
	"strings"
	"testing"
)

// TestReaderEmptyValues checks that the reader refuses an empty id, an empty version and a list
// with no column or with an empty column, and names the key: the code is the one of the family
// (id_invalid, version_invalid, value_invalid, key_missing) and the path is the key. A missing key
// or a key of another type is reported once, by its own code.
//
// Parameters:
//   - t: test to report to
func TestReaderEmptyValues(t *testing.T) {
	list := func(columns string) string {
		return execM("", []string{"exec:tool"}, "\n[[list]]\nid = \"l\"\nlabel = \"L\"\n"+columns)
	}

	col := "[[list.column]]\nid = \"a\"\nlabel = \"A\"\n"

	tests := []struct {
		name string
		src  string
		code string
		path string
		args []string
		only bool
	}{
		{
			"empty id", strings.Replace(decl("", nil, ""), `id = "demo"`, `id = ""`, 1),
			CodeIDInvalid, "id",
			[]string{"", "grammar"},
			true,
		},
		{
			"id of another type", strings.Replace(decl("", nil, ""), `id = "demo"`, `id = 5`, 1),
			CodeTypeInvalid, "id",
			[]string{"string", "integer"},
			true,
		},
		{
			"missing id", strings.Replace(decl("", nil, ""), "id = \"demo\"\n", "", 1),
			CodeKeyMissing, "id", nil, true,
		},
		{"empty version", strings.Replace(decl("", nil, ""), `version = "1.0.0"`,
			`version = ""`, 1), CodeVersionInvalid, "version", []string{""}, true},
		{"version of another type", strings.Replace(decl("", nil, ""), `version = "1.0.0"`,
			`version = 1`, 1), CodeTypeInvalid, "version", []string{"string", "integer"}, true},
		{
			"missing version", strings.Replace(decl("", nil, ""), "version = \"1.0.0\"\n", "", 1),
			CodeKeyMissing, "version", nil, true,
		},
		{"version with spaces", strings.Replace(decl("", nil, ""), `version = "1.0.0"`,
			`version = " "`, 1), CodeVersionInvalid, "version", []string{" "}, true},
		{
			"empty columns array", list("column = []\n"), CodeValueInvalid, "list[0].column",
			[]string{"empty"},
			true,
		},
		{"missing columns", list(""), CodeKeyMissing, "list[0].column", nil, true},
		{
			"columns of another type", list("column = 5\n"), CodeTypeInvalid, "list[0].column",
			[]string{"array of tables", "integer"},
			true,
		},
		{
			"empty column entry", list("column = [{}]\n"), CodeKeyMissing, "list[0].column[0].id",
			nil, false,
		},
		{
			"empty column table", list("[[list.column]]\n"), CodeKeyMissing,
			"list[0].column[0].label", nil, false,
		},
		{
			"empty column id", list("[[list.column]]\nid = \"\"\nlabel = \"A\"\n"),
			CodeValueInvalid, "list[0].column[0].id",
			[]string{"empty"},
			true,
		},
		{
			"empty column label", list("[[list.column]]\nid = \"a\"\nlabel = \"\"\n"),
			CodeValueInvalid, "list[0].column[0].label",
			[]string{"empty"},
			true,
		},
		{
			"second column empty", list(col + "[[list.column]]\nid = \"\"\nlabel = \"B\"\n"),
			CodeValueInvalid, "list[0].column[1].id",
			[]string{"empty"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, rep := decode(tt.src)
			if m != nil {
				t.Fatal("the manifest was accepted")
			}

			is := find(rep, tt.code, tt.path)
			if is == nil {
				t.Fatalf("no %s at %s in %v", tt.code, tt.path, rep.Issues)
			}

			if tt.args != nil && !slices.Equal(is.Args, tt.args) {
				t.Errorf("args = %q, want %q", is.Args, tt.args)
			}

			if is.Line == 0 && tt.code != CodeKeyMissing {
				t.Errorf("no position: %+v", is)
			}

			if rep.Class() != ClassInvalid {
				t.Errorf("class = %v", rep.Class())
			}

			if tt.only && len(rep.Errors()) != 1 {
				t.Errorf("errors = %v, want only %s", rep.Errors(), tt.code)
			}
		})
	}

	t.Run("valid", func(t *testing.T) {
		m := mustDecode(t, list(col))
		if m.ID != "demo" || m.Version != "1.0.0" || len(m.Lists) != 1 ||
			len(m.Lists[0].Columns) != 1 {
			t.Errorf("manifest = %+v", m)
		}
	})
}

// TestReaderBlankValues checks that the reader refuses a text that shows nothing, the empty string
// and a string of spaces alike, for the title, the description, the labels and the choices of an
// enum, and an enum with no choice at all. A blank title or label gives an entry that cannot be
// told from its neighbours in the screen, the palette and the form.
//
// Parameters:
//   - t: test to report to
func TestReaderBlankValues(t *testing.T) {
	enum := func(label, choices string) string {
		return decl("", nil, "\n[[setting]]\nid = \"s1\"\ntype = \"enum\"\nlabel = \""+label+
			"\"\nchoices = "+choices+"\n")
	}

	cmd := func(label string) string {
		return decl("", []string{"exec:tool"}, "\n[[command]]\nid = \"go\"\nlabel = \""+label+
			"\"\nrun = [\"tool\"]\noutput = \"pager\"\n")
	}

	tests := []struct {
		name string
		src  string
		path string
	}{
		{"title of spaces", decl(`title = "   "`+"\n", nil, ""), "title"},
		{"empty title", decl(`title = ""`+"\n", nil, ""), "title"},
		{
			"description of spaces",
			strings.Replace(decl("", nil, ""), `"Demo plugin."`, `"  "`, 1), "description",
		},
		{"command label of spaces", cmd("   "), "command[0].label"},
		{"empty command label", cmd(""), "command[0].label"},
		{"setting label of spaces", enum("   ", `["a"]`), "setting[0].label"},
		{"setting with no choice", enum("Mode", `[]`), "setting[0].choices"},
		{"choice of spaces", enum("Mode", `["a", "  "]`), "setting[0].choices[1]"},
		{"empty choice", enum("Mode", `["a", ""]`), "setting[0].choices[1]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, rep := decode(tt.src)
			if m != nil {
				t.Fatal("the manifest was accepted")
			}

			is := find(rep, CodeValueInvalid, tt.path)
			if is == nil {
				t.Fatalf("no %s at %s in %v", CodeValueInvalid, tt.path, rep.Issues)
			}

			if len(is.Args) != 1 || is.Args[0] != "empty" {
				t.Errorf("args = %q", is.Args)
			}
		})
	}

	t.Run("visible text is accepted", func(t *testing.T) {
		m := mustDecode(t, enum("Mode", `["a", " b "]`))
		if len(m.Settings) != 1 || len(m.Settings[0].Choices) != 2 {
			t.Errorf("manifest = %+v", m.Settings)
		}
	})
}
