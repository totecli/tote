package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestReviewImportRules checks the dependency rules of the repository on the imports of every
// non-test source file: net/http only in internal/update and internal/plugin, golang.org/x/crypto
// only for its blake2b package and only in internal/update, golang.org/x/sys only in cmd/tote,
// and Charm only in internal/ui.
//
// Parameters:
//   - t: test to report to
func TestReviewImportRules(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && (d.Name() == "plans" || d.Name() == ".git" || d.Name() == "testdata") {
			return filepath.SkipDir
		}

		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		slash := filepath.ToSlash(path)

		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)

			if reason := forbiddenImport(slash, p); reason != "" {
				t.Errorf("%s imports %s: %s", slash, p, reason)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// forbiddenImport applies the dependency rules to one import.
//
// Parameters:
//   - file: slash separated path of the source file
//   - pkg: imported package
//
// Returns why the import is not allowed, or an empty string.
func forbiddenImport(file, pkg string) string {
	in := func(dir string) bool { return strings.Contains(file, "/"+dir+"/") }

	switch {
	case pkg == "net/http" || strings.HasPrefix(pkg, "net/http/"):
		if !in("internal/update") && !in("internal/plugin") {
			return "the network is reached from internal/update and internal/plugin only"
		}
	case strings.HasPrefix(pkg, "golang.org/x/crypto"):
		if pkg != "golang.org/x/crypto/blake2b" || !in("internal/update") {
			return "x/crypto is limited to blake2b, for the signatures in internal/update"
		}
	case strings.HasPrefix(pkg, "golang.org/x/sys"):
		if !in("cmd/tote") {
			return "x/sys is limited to cmd/tote"
		}
	case strings.HasPrefix(pkg, "charm.land") || strings.HasPrefix(pkg, "github.com/charmbracelet"):
		if !in("internal/ui") {
			return "Charm is imported from internal/ui only"
		}
	}

	return ""
}
