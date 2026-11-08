package plugin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// catalogKeys reads the keys of a catalog file of the interface.
//
// Parameters:
//   - t: test to report to
//   - lang: en or fr
func catalogKeys(t *testing.T, lang string) map[string]bool {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "i18n", lang+".toml"))
	must(t, err)

	keys := map[string]bool{}
	re := regexp.MustCompile(`(?m)^([a-z0-9_]+) = `)

	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		keys[m[1]] = true
	}

	return keys
}

// constValues lists the string values of the constants of a file whose name starts with a prefix.
//
// Parameters:
//   - t: test to report to
//   - file: Go source file
//   - prefix: prefix of the names of the constants
func constValues(t *testing.T, file, prefix string) []string {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	must(t, err)

	var out []string

	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}

		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)

			for i, name := range vs.Names {
				if i >= len(vs.Values) || !strings.HasPrefix(name.Name, prefix) {
					continue
				}

				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}

				v, err := strconv.Unquote(lit.Value)
				must(t, err)

				out = append(out, v)
			}
		}
	}

	return out
}

// TestRejectCodesHaveMessages checks that every rule code a refusal can carry has its message in
// both catalogs: the codes of the Reject constants, and those that the field checks and the
// produced-text check return. A code without a message is printed raw, in French too.
//
// Parameters:
//   - t: test to report to
func TestRejectCodesHaveMessages(t *testing.T) {
	codes := constValues(t, "settings.go", "Reject")
	if len(codes) < 19 {
		t.Fatalf("only %d Reject constants were found", len(codes))
	}

	// What the field checks return must be among the constants.
	for _, raw := range []string{"\xff", "a\tb", strings.Repeat("x", maxValueBytes+1)} {
		rule := ProducedIssue(raw, maxValueBytes)
		if rule == "" {
			t.Fatalf("ProducedIssue(%q) accepted the text", raw)
		}

		codes = append(codes, rule)
	}

	for _, lang := range []string{"en", "fr"} {
		keys := catalogKeys(t, lang)

		for _, code := range codes {
			if !keys["plugin_reject_"+code] {
				t.Errorf("%s: no message plugin_reject_%s", lang, code)
			}
		}
	}
}

// TestErrorCodesHaveMessages checks that every stable code of an error or an issue that the
// plugin layer defines has a message plugin_err_<code> in both catalogs.
//
// Parameters:
//   - t: test to report to
func TestErrorCodesHaveMessages(t *testing.T) {
	var codes []string

	for _, file := range []string{"errors.go", "install.go"} {
		codes = append(codes, constValues(t, file, "Code")...)
	}

	if len(codes) < 50 {
		t.Fatalf("only %d codes were found", len(codes))
	}

	for _, lang := range []string{"en", "fr"} {
		keys := catalogKeys(t, lang)

		for _, code := range codes {
			if !keys["plugin_err_"+strings.ReplaceAll(code, ".", "_")] {
				t.Errorf("%s: no message plugin_err_%s", lang, code)
			}
		}
	}
}
