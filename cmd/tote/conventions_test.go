package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// convSources returns the Go files of the repository that the conventions apply to.
//
// Parameters:
//   - t: test to report to
//   - tests: true to keep the test files, false to leave them out
//
// Returns the slash separated paths, relative to the root of the repository.
func convSources(t *testing.T, tests bool) []string {
	t.Helper()

	var out []string

	for _, rel := range repoGoFiles(t, "plans", "bin", "vendor") {
		if !tests && strings.HasSuffix(rel, "_test.go") {
			continue
		}

		out = append(out, rel)
	}

	return out
}

// repoGoFiles returns the Go files of the repository: the ones git tracks or would track when
// the root is the top of a git work tree, so that an ignored or excluded folder (the build output
// of ppa-build/, the working copies of .claude/worktrees/, the notes of plans/) is never read,
// and every Go file below the root otherwise, as in a source tarball that has no .git.
//
// Parameters:
//   - t: test to report to
//   - skip: names of the directories to leave out, at any depth
//
// Returns the slash separated paths, relative to the root of the repository, sorted.
func repoGoFiles(t *testing.T, skip ...string) []string {
	t.Helper()

	root := filepath.Join("..", "..")

	files, ok := repoGitFiles(root)
	if !ok {
		files = repoWalkFiles(t, root)
	}

	var out []string

	for _, rel := range files {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}

		dirs := strings.Split(rel, "/")
		dirs = dirs[:len(dirs)-1]

		if slices.ContainsFunc(dirs, func(d string) bool { return slices.Contains(skip, d) }) {
			continue
		}

		out = append(out, rel)
	}

	slices.Sort(out)

	return out
}

// repoGitFiles lists the files git tracks or would track below the root: the index, plus the
// untracked files that no .gitignore, info/exclude or global exclude file leaves out. A tracked
// file deleted from the work tree is left out.
//
// Parameters:
//   - root: root of the repository
//
// Returns the slash separated paths relative to the root, and false when git is not installed,
// fails, or the root is not the top of a work tree (a tarball unpacked inside another clone).
func repoGitFiles(root string) ([]string, bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, false
	}

	run := func(args ...string) (string, bool) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)

		// A GIT_DIR or GIT_WORK_TREE inherited from a hook would point git elsewhere.
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "GIT_")
		})

		out, err := cmd.Output()

		return string(out), err == nil
	}

	top, ok := run("rev-parse", "--show-toplevel")
	if !ok {
		return nil, false
	}

	want, err := filepath.Abs(root)
	if err != nil {
		return nil, false
	}

	a, errA := filepath.EvalSymlinks(strings.TrimRight(top, "\r\n"))
	b, errB := filepath.EvalSymlinks(want)

	if errA != nil || errB != nil || a != b {
		return nil, false
	}

	list, ok := run("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if !ok {
		return nil, false
	}

	var out []string

	seen := map[string]bool{}

	for _, rel := range strings.Split(list, "\x00") {
		if rel == "" || seen[rel] {
			continue
		}

		seen[rel] = true

		if fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err != nil ||
			!fi.Mode().IsRegular() {
			continue
		}

		out = append(out, rel)
	}

	return out, true
}

// repoWalkFiles lists every regular file below the root, .git left out.
//
// Parameters:
//   - t: test to report to
//   - root: root of the repository
//
// Returns the slash separated paths relative to the root.
func repoWalkFiles(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}

		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, path)
			out = append(out, filepath.ToSlash(rel))
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// convCatalog reads a message catalog of the repository.
//
// Parameters:
//   - t: test to report to
//   - lang: "en" or "fr"
//
// Returns the messages by key, as they are written in the file.
func convCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "i18n", lang+".toml"))
	if err != nil {
		t.Fatal(err)
	}

	out := map[string]string{}
	line := regexp.MustCompile(`(?m)^([a-z0-9_]+) = "(.*)"$`)

	for _, m := range line.FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = m[2]
	}

	return out
}

// convVerbs counts the arguments a Printf message takes: the highest explicit index, or the
// number of directives when none is indexed. A literal percent sign takes none.
//
// Parameters:
//   - msg: message as written in the catalog
func convVerbs(msg string) int {
	verb := regexp.MustCompile(`%(?:\[(\d+)\])?[-+# 0-9.]*[a-zA-Z%]`)
	next, most := 1, 0

	for _, m := range verb.FindAllStringSubmatch(msg, -1) {
		if strings.HasSuffix(m[0], "%") {
			continue
		}

		at := next

		if m[1] != "" {
			at, _ = strconv.Atoi(m[1])
		}

		next = at + 1
		most = max(most, at)
	}

	return most
}

// TestReviewGoSourcesAreASCII checks CONVENTIONS section 5 and 9: the Go sources, the test files
// included, are ASCII; a glyph is written with a \u escape. internal/e2e/palette_test.go finds
// the input line of the palette with literal box drawing, prompt marker and cursor glyphs, which
// breaks the rule that git grep -nP "[^\x00-\x7F]" -- '*.go' comes back empty.
//
// Parameters:
//   - t: test to report to
func TestReviewGoSourcesAreASCII(t *testing.T) {
	for _, path := range convSources(t, true) {
		raw, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}

		for i, line := range strings.Split(string(raw), "\n") {
			for _, r := range line {
				if r > 0x7f {
					t.Errorf("%s:%d holds a character that is not ASCII: %q", path, i+1, line)

					break
				}
			}
		}
	}
}

// TestReviewCatalogArity checks that every call that translates a literal catalog key gives as
// many arguments as the message has directives, in both languages. A call that gives too many
// prints "%!(EXTRA ...)" at the end of the message: the key u of a connection whose stored
// login is set shows "next connection with the stored user%!(EXTRA string=deploy)", because
// ui_user_stored has no directive and internal/ui/user.go passes the login.
//
// Parameters:
//   - t: test to report to
func TestReviewCatalogArity(t *testing.T) {
	cats := map[string]map[string]string{"en": convCatalog(t, "en"), "fr": convCatalog(t, "fr")}
	callers := map[string]bool{"t": true, "T": true, "failMsg": true, "say": true, "note": true}
	fset := token.NewFileSet()

	for _, path := range convSources(t, false) {
		f, err := parser.ParseFile(fset, filepath.Join("..", "..", filepath.FromSlash(path)), nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || call.Ellipsis.IsValid() {
				return true
			}

			name := ""

			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			case *ast.Ident:
				name = fn.Name
			}

			if !callers[name] {
				return true
			}

			for i, a := range call.Args {
				lit, ok := a.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}

				key, _ := strconv.Unquote(lit.Value)
				if _, known := cats["en"][key]; !known {
					continue
				}

				got := len(call.Args) - i - 1

				for lang, cat := range cats {
					if want := convVerbs(cat[key]); want != got {
						t.Errorf("%s: %s gives %d arguments to %s, whose %s message takes %d",
							fset.Position(call.Pos()), name, got, key, lang, want)
					}
				}
			}

			return true
		})
	}
}

// TestReviewCatalogInitialCase checks that a message starts with a capital letter in French
// exactly when it does in English: the keys that are new or changed in this wave must not have
// one language that reads as a sentence and the other as a fragment. ui_sel_shown is "No
// clipboard available, selected text: %s" in English and "aucun presse-papiers disponible, ..."
// in French, while its neighbour ui_copy_manual starts with a capital in both.
//
// Parameters:
//   - t: test to report to
func TestReviewCatalogInitialCase(t *testing.T) {
	en, fr := convCatalog(t, "en"), convCatalog(t, "fr")

	// "PATH" is a name, which French writes after an article.
	except := map[string]bool{"cli_doctor_path_other": true}

	for key, a := range en {
		b := fr[key]
		first := func(s string) (rune, bool) {
			for _, r := range s {
				return r, r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
			}

			return 0, false
		}

		ra, okA := first(a)
		rb, okB := first(b)

		if !okA || !okB || except[key] {
			continue
		}

		if (ra >= 'A' && ra <= 'Z') != (rb >= 'A' && rb <= 'Z') {
			t.Errorf("%s: English %q and French %q do not start with the same case", key, a, b)
		}
	}
}

// TestReviewDocCommentRules pins the comment rules of CONVENTIONS section 6 on every Go file of
// the repository: each top-level declaration has a doc comment that starts with its name, no
// field of a struct or an interface is commented, nothing sits before the package clause of a
// file that is not doc.go, and no check is switched off with a directive.
//
// Parameters:
//   - t: test to report to
func TestReviewDocCommentRules(t *testing.T) {
	fset := token.NewFileSet()
	switchOff := regexp.MustCompile(`//\s*(nolint|lint:ignore|#?nosec)\b`)

	for _, path := range convSources(t, true) {
		full := filepath.Join("..", "..", filepath.FromSlash(path))

		f, err := parser.ParseFile(fset, full, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}

		if f.Doc != nil && filepath.Base(path) != "doc.go" {
			t.Errorf("%s: comment before the package clause", path)
		}

		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if switchOff.MatchString(c.Text) {
					t.Errorf("%s: %s", fset.Position(c.Pos()), c.Text)
				}
			}
		}

		for _, d := range f.Decls {
			convCheckDecl(t, fset, d)
		}
	}
}

// convCheckDecl applies the rules of TestReviewDocCommentRules to one top-level declaration.
//
// Parameters:
//   - t: test to report to
//   - fset: positions of the file
//   - d: declaration to check
func convCheckDecl(t *testing.T, fset *token.FileSet, d ast.Decl) {
	t.Helper()

	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Doc == nil || !strings.HasPrefix(d.Doc.Text(), d.Name.Name+" ") {
			t.Errorf("%s: %s has no doc comment that starts with its name", fset.Position(d.Pos()),
				d.Name.Name)
		}
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				doc := s.Doc
				if doc == nil {
					doc = d.Doc
				}

				if doc == nil || !strings.HasPrefix(doc.Text(), s.Name.Name+" ") {
					t.Errorf("%s: type %s has no doc comment that starts with its name",
						fset.Position(s.Pos()), s.Name.Name)
				}

				convCheckFields(t, fset, s)
			case *ast.ValueSpec:
				if d.Doc == nil && s.Doc == nil {
					t.Errorf("%s: %s has no doc comment", fset.Position(s.Pos()), s.Names[0].Name)
				}
			}
		}
	}
}

// convCheckFields reports the commented fields of a struct or the commented methods of an
// interface.
//
// Parameters:
//   - t: test to report to
//   - fset: positions of the file
//   - s: type declaration
func convCheckFields(t *testing.T, fset *token.FileSet, s *ast.TypeSpec) {
	t.Helper()

	var fields *ast.FieldList

	switch ty := s.Type.(type) {
	case *ast.StructType:
		fields = ty.Fields
	case *ast.InterfaceType:
		fields = ty.Methods
	}

	if fields == nil {
		return
	}

	for _, fl := range fields.List {
		if fl.Doc != nil || fl.Comment != nil {
			t.Errorf("%s: a member of %s is commented", fset.Position(fl.Pos()), s.Name.Name)
		}
	}
}

// TestReviewImportGroups pins the import rule of CONVENTIONS section 2: three groups separated
// by a blank line, in the order standard library, other modules, modules of the project, each
// group sorted, and no group that mixes two kinds.
//
// Parameters:
//   - t: test to report to
func TestReviewImportGroups(t *testing.T) {
	fset := token.NewFileSet()

	kind := func(p string) int {
		switch {
		case strings.HasPrefix(p, "github.com/totecli/tote"):
			return 2
		case strings.Contains(strings.SplitN(p, "/", 2)[0], "."):
			return 1
		}

		return 0
	}

	for _, path := range convSources(t, true) {
		f, err := parser.ParseFile(fset, filepath.Join("..", "..", filepath.FromSlash(path)), nil,
			parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}

		last, prevKind, prevPath, prevLine := -1, -1, "", -10

		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			line := fset.Position(im.Pos()).Line

			if line > prevLine+1 {
				last, prevPath = -1, ""
			}

			k := kind(p)

			switch {
			case last >= 0 && k != last:
				t.Errorf("%s: the import %s is in a group of another kind", path, p)
			case last < 0 && k < prevKind:
				t.Errorf("%s: the import %s opens a group that is out of order", path, p)
			case prevPath != "" && p < prevPath:
				t.Errorf("%s: the import %s is not sorted", path, p)
			}

			if last < 0 || k > prevKind {
				prevKind = k
			}

			last, prevPath, prevLine = k, p, line
		}
	}
}
