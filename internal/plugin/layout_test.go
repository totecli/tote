package plugin

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Paths of the layout rules: the module, the plugin core and its embedded plugins.
const (
	modulePath = "github.com/totecli/tote"
	pluginDir  = "internal/plugin/"
	bundledDir = "internal/plugin/bundled/"
)

// skippedDirs lists the directory names the tree scan never enters: version control, the planning
// documents (which hold a separate spike module), test fixtures and vendored code.
var skippedDirs = map[string]bool{
	".git":         true,
	"plans":        true,
	"testdata":     true,
	"vendor":       true,
	"node_modules": true,
}

// importsOf parses the import block of one source file.
//
// Parameters:
//   - name: slash separated path of the file, used in errors only
//   - src: source text of the file
//
// Returns the unquoted import paths in source order, or the parse error.
func importsOf(name, src string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	var out []string

	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}

		out = append(out, p)
	}

	return out, nil
}

// underPkg reports whether an import path is a package or one of its sub-packages. A bare prefix
// match is not enough: "net/httpx" is not under "net/http".
//
// Parameters:
//   - pkg: imported package path
//   - root: package path to compare with
func underPkg(pkg, root string) bool {
	return pkg == root || strings.HasPrefix(pkg, root+"/")
}

// isCharm reports whether an import path belongs to the Charm family, in either of its two
// module homes.
//
// Parameters:
//   - pkg: imported package path
func isCharm(pkg string) bool {
	return underPkg(pkg, "charm.land") || underPkg(pkg, "github.com/charmbracelet")
}

// isStdlib reports whether an import path is in the standard library: its first element has no
// dot, which no module path can avoid.
//
// Parameters:
//   - pkg: imported package path
func isStdlib(pkg string) bool {
	first, _, _ := strings.Cut(pkg, "/")

	return !strings.Contains(first, ".")
}

// repoRule applies the repository-wide dependency rules to one import: Charm is imported from
// internal/ui only (test files included), and net/http is imported by internal/update and
// internal/plugin/fetch.go only (test files may use httptest).
//
// Parameters:
//   - rel: slash separated path of the file from the module root
//   - pkg: imported package path
//
// Returns why the import is refused, or an empty string.
func repoRule(rel, pkg string) string {
	isTest := strings.HasSuffix(rel, "_test.go")

	switch {
	case isCharm(pkg):
		if !strings.HasPrefix(rel, "internal/ui/") {
			return "Charm is imported from internal/ui only"
		}
	case underPkg(pkg, "net/http"):
		if isTest {
			return ""
		}

		if !strings.HasPrefix(rel, "internal/update/") && rel != pluginDir+"fetch.go" {
			return "net/http is imported by internal/update and internal/plugin/fetch.go only"
		}
	}

	return ""
}

// pluginRule applies the rules of section 8.1 of the plugin specification to one import of a file
// under internal/plugin: never Charm, internal/ui or cmd, net/http in fetch.go only, os/exec in
// run.go and git.go only, and the bundled sub-package restricted to the standard library. Test
// files may use net/http (httptest) and os/exec to build fixtures; they are never exempt from the
// Charm, internal/ui and cmd rules.
//
// Parameters:
//   - rel: slash separated path of the file from the module root
//   - pkg: imported package path
//
// Returns why the import is refused, or an empty string.
func pluginRule(rel, pkg string) string {
	isTest := strings.HasSuffix(rel, "_test.go")

	switch {
	case isCharm(pkg):
		return "internal/plugin never imports Charm"
	case underPkg(pkg, modulePath+"/internal/ui"):
		return "internal/plugin never imports internal/ui"
	case underPkg(pkg, modulePath+"/cmd"):
		return "internal/plugin never imports cmd"
	case isTest:
		return ""
	case underPkg(pkg, "net/http"):
		if rel != pluginDir+"fetch.go" {
			return "net/http is imported by internal/plugin/fetch.go only"
		}
	case underPkg(pkg, "os/exec"):
		if rel != pluginDir+"run.go" && rel != pluginDir+"git.go" {
			return "os/exec is imported by internal/plugin/run.go and git.go only"
		}
	}

	if strings.HasPrefix(rel, bundledDir) && !isStdlib(pkg) {
		return "internal/plugin/bundled imports the standard library only"
	}

	return ""
}

// check applies a rule to every Go file of a file set.
//
// Parameters:
//   - files: source text of each file, keyed by slash separated path from the module root
//   - scope: path prefix a file must have to be checked, empty for every file
//   - rule: rule giving why an import is refused, or an empty string
//
// Returns one sorted line per violation, and an error when a file does not parse.
func check(
	files map[string]string,
	scope string,
	rule func(rel, pkg string) string,
) ([]string, error) {
	var out []string

	for rel, src := range files {
		if !strings.HasPrefix(rel, scope) || !strings.HasSuffix(rel, ".go") {
			continue
		}

		imports, err := importsOf(rel, src)
		if err != nil {
			return nil, err
		}

		for _, pkg := range imports {
			if reason := rule(rel, pkg); reason != "" {
				out = append(out, rel+" imports "+pkg+": "+reason)
			}
		}
	}

	slices.Sort(out)

	return out, nil
}

// loadTree reads every Go file of the repository, skipping the directories of skippedDirs and any
// nested module. Symbolic links are not followed.
//
// Parameters:
//   - t: test to report to
//
// Returns the source text of each file keyed by slash separated path from the module root.
func loadTree(t *testing.T) map[string]string {
	t.Helper()

	root := os.DirFS(filepath.Join("..", ".."))

	mod, err := fs.ReadFile(root, "go.mod")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(mod), "module "+modulePath+"\n") {
		t.Fatalf("go.mod at the repository root is not the module %s", modulePath)
	}

	files := map[string]string{}

	err = fs.WalkDir(root, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if rel == "." {
				return nil
			}

			if skippedDirs[d.Name()] {
				return fs.SkipDir
			}

			if _, err := fs.Stat(root, rel+"/go.mod"); err == nil {
				return fs.SkipDir
			}

			return nil
		}

		if !d.Type().IsRegular() || !strings.HasSuffix(rel, ".go") {
			return nil
		}

		src, err := fs.ReadFile(root, rel)
		if err != nil {
			return err
		}

		files[rel] = string(src)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return files
}

// TestLayoutRepoImports checks the repository-wide rules on the real tree: Charm only in
// internal/ui, net/http only in internal/update and internal/plugin/fetch.go. It also checks that
// the scan saw the packages it is meant to police, so that a wrong root cannot pass it.
//
// Parameters:
//   - t: test to report to
func TestLayoutRepoImports(t *testing.T) {
	files := loadTree(t)

	wanted := []string{"internal/ui/doc.go", "internal/update/doc.go", pluginDir + "doc.go"}

	for _, want := range wanted {
		if _, ok := files[want]; !ok {
			t.Fatalf("scan did not find %s", want)
		}
	}

	got, err := check(files, "", repoRule)
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range got {
		t.Error(line)
	}
}

// TestLayoutPluginImports checks section 8.1 of the plugin specification on the real tree: every
// Go file under internal/plugin, sub-packages and test files included.
//
// Parameters:
//   - t: test to report to
func TestLayoutPluginImports(t *testing.T) {
	files := loadTree(t)

	if _, ok := files[pluginDir+"doc.go"]; !ok {
		t.Fatal("scan did not find internal/plugin/doc.go")
	}

	got, err := check(files, pluginDir, pluginRule)
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range got {
		t.Error(line)
	}
}

// layoutCase is one synthetic file of the rule tests: the file, the single package it imports,
// the rule set applied (repo or plugin) and whether a violation is expected.
type layoutCase struct {
	name   string
	path   string
	pkg    string
	plugin bool
	bad    bool
}

// TestLayoutRulesDetect plants a violation in an in-memory file set, one per rule, and checks
// that the rule reports it and only it. It also checks the allowed neighbours, so that a rule
// that refuses everything does not pass.
//
// Parameters:
//   - t: test to report to
func TestLayoutRulesDetect(t *testing.T) {
	const (
		lip    = "charm.land/lipgloss/v2"
		ansi   = "github.com/charmbracelet/x/ansi"
		tea    = "charm.land/bubbletea/v2"
		ui     = modulePath + "/internal/ui"
		store  = modulePath + "/internal/store"
		toml   = "github.com/BurntSushi/toml"
		app    = "cmd/tote/app.go"
		plug   = "internal/plugin/"
		bundle = "internal/plugin/bundled/a.go"
	)

	tests := []layoutCase{
		{"charm in app", app, lip, false, true},
		{"charm x in lib", "internal/shell/a.go", ansi, false, true},
		{"charm in other test", "internal/store/a_test.go", tea, false, true},
		{"charm in ui", "internal/ui/a.go", lip, false, false},
		{"charm in ui test", "internal/ui/a_test.go", tea, false, false},
		{"lookalike of charm", "internal/shell/a.go", "charm.land.example/x", false, false},
		{"http in shell", "internal/shell/a.go", "net/http", false, true},
		{"httptest in config", "internal/config/a.go", "net/http/httptest", false, true},
		{"http in app", app, "net/http", false, true},
		{"http in plugin run", plug + "run.go", "net/http", false, true},
		{"http in plugin sub", bundle, "net/http", false, true},
		{"http in fetch", plug + "fetch.go", "net/http", false, false},
		{"http in update", "internal/update/source.go", "net/http", false, false},
		{"http in update sub", "internal/update/x/a.go", "net/http", false, false},
		{"http in a test", "cmd/tote/update_test.go", "net/http/httptest", false, false},
		{"lookalike of http", "internal/shell/a.go", "net/httpx", false, false},
		{"plugin charm", plug + "a.go", lip, true, true},
		{"plugin charm test", plug + "a_test.go", ansi, true, true},
		{"plugin charm sub", bundle, "charm.land/bubbles/v2/key", true, true},
		{"plugin ui", plug + "a.go", ui, true, true},
		{"plugin ui sub", plug + "a.go", ui + "/theme", true, true},
		{"plugin ui test", plug + "a_test.go", ui, true, true},
		{"plugin cmd", plug + "a.go", modulePath + "/cmd/tote", true, true},
		{"plugin http", plug + "run.go", "net/http", true, true},
		{"plugin http sub", bundle, "net/http", true, true},
		{"plugin http fetch", plug + "fetch.go", "net/http", true, false},
		{"plugin http test", plug + "fetch_test.go", "net/http/httptest", true, false},
		{"plugin exec", plug + "fetch.go", "os/exec", true, true},
		{"plugin exec elsewhere", plug + "install.go", "os/exec", true, true},
		{"plugin exec sub", plug + "bundled/run.go", "os/exec", true, true},
		{"plugin exec run", plug + "run.go", "os/exec", true, false},
		{"plugin exec git", plug + "git.go", "os/exec", true, false},
		{"plugin exec test", plug + "run_test.go", "os/exec", true, false},
		{"bundled toml", bundle, toml, true, true},
		{"bundled module", bundle, store, true, true},
		{"bundled embed", bundle, "embed", true, false},
		{"bundled stdlib sub", bundle, "crypto/sha256", true, false},
		{"plugin toml", plug + "manifest.go", toml, true, false},
		{"plugin store", plug + "lock.go", store, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package x\n\nimport (\n\t\"fmt\"\n\n\t" + strconv.Quote(tt.pkg) + "\n)\n\n" +
				"var _ = fmt.Sprint\n"

			scope, rule := "", repoRule
			if tt.plugin {
				scope, rule = pluginDir, pluginRule
			}

			got, err := check(map[string]string{tt.path: src}, scope, rule)
			if err != nil {
				t.Fatal(err)
			}

			if tt.bad && len(got) != 1 {
				t.Errorf("violations = %q, want exactly one for %s", got, tt.pkg)
			}

			if !tt.bad && len(got) != 0 {
				t.Errorf("violations = %q, want none", got)
			}
		})
	}
}

// TestLayoutCheckForms checks the parsing side of the scan: aliased, blank and dot imports are
// still seen, files outside the scope or without the .go suffix are ignored, a file that does
// not parse is an error, and several violations come back sorted.
//
// Parameters:
//   - t: test to report to
func TestLayoutCheckForms(t *testing.T) {
	files := map[string]string{
		"internal/plugin/b.go":      "package p\n\nimport h \"net/http\"\n",
		"internal/plugin/a.go":      "package p\n\nimport _ \"os/exec\"\n",
		"internal/plugin/c.go":      "package p\n\nimport . \"charm.land/lipgloss/v2\"\n",
		"internal/plugin/notes.txt": "import \"net/http\"",
		"internal/shell/a.go":       "package s\n\nimport \"os/exec\"\n",
	}

	got, err := check(files, pluginDir, pluginRule)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"internal/plugin/a.go imports os/exec: " +
			"os/exec is imported by internal/plugin/run.go and git.go only",
		"internal/plugin/b.go imports net/http: " +
			"net/http is imported by internal/plugin/fetch.go only",
		"internal/plugin/c.go imports charm.land/lipgloss/v2: " +
			"internal/plugin never imports Charm",
	}
	if !slices.Equal(got, want) {
		t.Errorf("violations = %q, want %q", got, want)
	}

	broken := map[string]string{"internal/plugin/x.go": "package"}
	if _, err := check(broken, "", pluginRule); err == nil {
		t.Error("a file that does not parse gave no error")
	}
}
