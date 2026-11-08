package plugin

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// reviewSources parses every non-test Go file of the package.
//
// Parameters:
//   - t: test to report to
//
// Returns the file set and the parsed files by name.
func reviewSources(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()

	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	files := map[string]*ast.File{}

	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		files[name] = f
	}

	return fset, files
}

// TestReviewCheckJSONKeys pins the JSON shape of CheckResult, which `plugin check --json` and
// vendor.sh read: every key is snake_case like the rest of the document (the
// specification shows reasons as {code,args}) and the severity is a word, not the number of a Go
// enumeration. Reason has no json tags and Severity has no marshaller, so the keys come out as
// "Code" and "Args" and the severity as 0 or 1.
//
// Parameters:
//   - t: test to report to
func TestReviewCheckJSONKeys(t *testing.T) {
	res := CheckResult{
		Reasons:  []Reason{{Code: "not_consented", Args: []string{"x"}}},
		Findings: []Finding{{Code: "argv.shell", Severity: SeverityWarning}},
	}

	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}

	reasons, _ := doc["reasons"].([]any)
	if len(reasons) != 1 {
		t.Fatalf("reasons = %v", doc["reasons"])
	}

	first, _ := reasons[0].(map[string]any)
	if _, ok := first["code"]; !ok {
		t.Errorf("a reason serialises as %s, want the keys code and args", out)
	}

	findings, _ := doc["findings"].([]any)
	if len(findings) != 1 {
		t.Fatalf("findings = %v", doc["findings"])
	}

	f, _ := findings[0].(map[string]any)
	if _, isNumber := f["severity"].(float64); isNumber {
		t.Errorf("severity serialises as the number %v, want the word error or warning", f["severity"])
	}
}

// TestReviewCodeConstants pins the stable codes: every Code constant is a lower case slug, no two
// constants share a value, and no Reason constant repeats a Code of another meaning by accident
// (a shared value must be the same constant).
//
// Parameters:
//   - t: test to report to
func TestReviewCodeConstants(t *testing.T) {
	_, files := reviewSources(t)
	slug := regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)?$`)
	seen := map[string]string{}

	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}

			for _, s := range gd.Specs {
				vs := s.(*ast.ValueSpec)

				for i, n := range vs.Names {
					if !strings.HasPrefix(n.Name, "Code") || i >= len(vs.Values) {
						continue
					}

					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}

					val, _ := strconv.Unquote(lit.Value)
					if !slug.MatchString(val) {
						t.Errorf("%s = %q is not a lower case slug", n.Name, val)
					}

					if other, dup := seen[val]; dup {
						t.Errorf("%s and %s share the value %q", n.Name, other, val)
					}

					seen[val] = n.Name
				}
			}
		}
	}

	if len(seen) < 90 {
		t.Errorf("only %d codes found, the scan is broken", len(seen))
	}
}

// builtinNames lists the predeclared functions and types that revive's redefines-builtin-id
// refuses to shadow.
var builtinNames = map[string]bool{
	"append": true, "cap": true, "clear": true, "close": true, "complex": true, "copy": true,
	"delete": true, "imag": true, "len": true, "make": true, "max": true, "min": true,
	"new": true, "panic": true, "print": true, "println": true, "real": true, "recover": true,
}

// TestReviewNoRedefinedBuiltin stands in for revive's redefines-builtin-id, which the repository
// enables in .golangci.yml but which cannot run here: a variable named real or max hides the
// predeclared function.
//
// Parameters:
//   - t: test to report to
func TestReviewNoRedefinedBuiltin(t *testing.T) {
	fset, files := reviewSources(t)

	var found []string

	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if ok && id.Obj != nil && id.Obj.Pos() == id.Pos() && builtinNames[id.Name] {
				found = append(found, name+":"+strconv.Itoa(fset.Position(id.Pos()).Line)+" "+id.Name)
			}

			return true
		})
	}

	sort.Strings(found)

	for _, line := range found {
		t.Errorf("shadows a predeclared identifier: %s", line)
	}
}

// TestReviewNoUnusedParameter stands in for revive's unused-parameter on the declared functions
// and methods of the package: a named parameter that the body never reads is renamed to _.
//
// Parameters:
//   - t: test to report to
func TestReviewNoUnusedParameter(t *testing.T) {
	fset, files := reviewSources(t)

	var found []string

	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			var (
				ft   *ast.FuncType
				body *ast.BlockStmt
			)

			switch x := n.(type) {
			case *ast.FuncDecl:
				ft, body = x.Type, x.Body
			case *ast.FuncLit:
				ft, body = x.Type, x.Body
			}

			if body == nil || ft.Params == nil {
				return true
			}

			used := map[string]bool{}

			ast.Inspect(body, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok {
					used[id.Name] = true
				}

				return true
			})

			for _, p := range ft.Params.List {
				for _, id := range p.Names {
					if id.Name != "_" && !used[id.Name] {
						pos := fset.Position(id.Pos())
						found = append(found, name+":"+strconv.Itoa(pos.Line)+" "+id.Name)
					}
				}
			}

			return true
		})
	}

	sort.Strings(found)

	for _, line := range found {
		t.Errorf("unused parameter: %s", line)
	}
}

// TestReviewNoStrayFiles pins the cleanliness of the package directory: no backup, original,
// disabled or compiled file is left next to the sources.
//
// Parameters:
//   - t: test to report to
func TestReviewNoStrayFiles(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		name := e.Name()

		for _, suffix := range []string{".off", ".orig", ".rej", ".bak", ".test", ".out", "~"} {
			if strings.HasSuffix(name, suffix) {
				t.Errorf("stray file %s", name)
			}
		}

		if strings.HasPrefix(name, "zz_") {
			t.Errorf("scratch file %s is still in the package", name)
		}
	}
}
