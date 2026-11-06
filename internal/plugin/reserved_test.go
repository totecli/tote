package plugin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestReservedID checks one name per reason, and that a normal id is free.
//
// Parameters:
//   - t: test to report to
func TestReservedID(t *testing.T) {
	tests := map[string]string{
		"help": ReservedVerb, "config": ReservedVerb, "conn": ReservedVerb, "__complete": ReservedVerb,
		"plugin": ReservedVerb, "update": ReservedVerb, "doctor": ReservedVerb, "snippets": ReservedVerb,
		"proc": ReservedVerb, "procs": ReservedVerb, "process": ReservedVerb,
		"processes": ReservedVerb, "ports": ReservedVerb, "history": ReservedVerb,
		"recent": ReservedVerb, "stats": ReservedVerb, "scheme": ReservedField,
		"start": ReservedField, "health": ReservedField, "cwd": ReservedField,
		"konsolai": ReservedOfficial,
		"tote":     ReservedWord, "mn": ReservedWord, "builtin": ReservedWord, "none": ReservedWord,
		"tote-x": ReservedPrefix, "tote-": ReservedPrefix,
		"name": ReservedField, "needs_review": ReservedField, "opener": ReservedField,
		"gft": ReservedOfficial, "ghrel": ReservedOfficial, "ghsec": ReservedOfficial,
		"ssh-tools": ReservedOfficial,
		"demo":      "", "gft2": "", "my-gft": "", "toter": "", "totex": "",
	}

	for id, want := range tests {
		if got := ReservedID(id); got != want {
			t.Errorf("ReservedID(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestReservedTables checks that no reserved table or feature is also announced.
//
// Parameters:
//   - t: test to report to
func TestReservedTables(t *testing.T) {
	for _, f := range ReservedFeatures {
		if slices.Contains(AnnouncedFeatures, f) {
			t.Errorf("feature %q is both announced and reserved", f)
		}
	}

	for _, id := range officialIDs {
		if ReservedID(id) != ReservedOfficial {
			t.Errorf("official id %q is not reserved as official", id)
		}
	}

	if len(AnnouncedFeatures) != 6 {
		t.Errorf("announced features changed: %v", AnnouncedFeatures)
	}
}

// TestShellNames checks the advisory shell list, compared without case.
//
// Parameters:
//   - t: test to report to
func TestShellNames(t *testing.T) {
	for _, n := range []string{"sh", "bash", "SH", "Bash", "env", "xargs", "python3", "osascript"} {
		if !IsShellName(n) {
			t.Errorf("IsShellName(%q) = false", n)
		}
	}

	for _, n := range []string{"git", "gh", "jq", "shell", "bashful", ""} {
		if IsShellName(n) {
			t.Errorf("IsShellName(%q) = true", n)
		}
	}
}

// TestBlockedEnvName checks the exact names and the prefixes.
//
// Parameters:
//   - t: test to report to
func TestBlockedEnvName(t *testing.T) {
	for _, n := range []string{
		"PATH", "HOME", "SHELL", "IFS", "BASH_ENV", "LD_PRELOAD", "LD_LIBRARY_PATH",
		"DYLD_INSERT_LIBRARIES",
		"TOTE_X", "TOTE_TOKEN", "PYTHONPATH", "PYTHONSTARTUP", "PERL5LIB", "RUBYOPT", "NODE_OPTIONS",
		"GIT_SSH_COMMAND", "GIT_SSH", "GIT_CONFIG_COUNT", "GIT_ASKPASS", "GIT_EXEC_PATH", "GIT_DIR",
		"GIT_WORK_TREE", "GIT_INDEX_FILE", "BASH_FUNC_foo", "SSH_ASKPASS", "JAVA_TOOL_OPTIONS",
		"GIT_EXTERNAL_DIFF", "GIT_PAGER", "GIT_EDITOR", "GIT_SEQUENCE_EDITOR", "PAGER", "EDITOR",
		"VISUAL", "LESSOPEN", "LESSCLOSE", "MANPAGER", "GIT_TRACE", "GIT_TRACE2_EVENT", "BROWSER",
		"XDG_CONFIG_HOME", "LUA_INIT", "SSL_CERT_FILE",
	} {
		if !BlockedEnvName(n) {
			t.Errorf("BlockedEnvName(%q) = false", n)
		}
	}

	for _, n := range []string{
		"MY_MODE", "LANG", "GITHUB_TOKEN", "GH_TOKEN", "GIT_AUTHOR_NAME",
		"TERM", "NO_COLOR",
	} {
		if BlockedEnvName(n) {
			t.Errorf("BlockedEnvName(%q) = true", n)
		}
	}
}

// TestCoreVerbsDrift compares the verbs of the command line with the reserved ids: every name
// that cmd/tote answers to must be a reserved plugin id, so that a new verb cannot be forgotten.
// The test reads the source with go/parser and is skipped when the source cannot be read or has
// no verb table, because the command line is edited by another step.
//
// Parameters:
//   - t: test to report to
func TestCoreVerbsDrift(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "cmd", "tote", "*.go"))
	if err != nil || len(files) == 0 {
		t.Skip("cmd/tote is not readable")
	}

	var names []string

	fset := token.NewFileSet()

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		src, err := os.ReadFile(file)
		if err != nil {
			t.Skipf("cannot read %s: %v", file, err)
		}

		f, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Skipf("cannot parse %s: %v", file, err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}

			if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "names" {
				return true
			}

			lit, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				return true
			}

			for _, el := range lit.Elts {
				if b, ok := el.(*ast.BasicLit); ok && b.Kind == token.STRING {
					if s, err := strconv.Unquote(b.Value); err == nil {
						names = append(names, s)
					}
				}
			}

			return true
		})
	}

	if len(names) == 0 {
		t.Skip("no verb table found in cmd/tote")
	}

	for _, n := range names {
		if ReservedID(n) == "" {
			t.Errorf("the command line answers to %q but it is not a reserved plugin id", n)
		}
	}
}

// TestReservationKeepsInstalled proves the promise of API 1, section 3.3: a name that a later
// release reserves never breaks a plugin that is installed. The plugin demo is installed while
// its id is free; each list of reservations then grows to hold it. The plugin must still run
// after the growth, and an update of it must still go through, while a new install under that id
// is refused. The one exception is the list of official ids: the plugin still runs, but an update
// is refused, because only the shipped copy and the releases of totecli may carry such an id.
//
// Parameters:
//   - t: test to report to
func TestReservationKeepsInstalled(t *testing.T) {
	lists := map[string]*[]string{
		"verb": &plannedVerbs, "word": &reservedWords, "field": &recordFields,
		"official": &officialIDs,
	}

	plain := surfaceBase("", "", "")
	call := Call{
		Plugin: "demo", Table: TableCommand, Entry: "go",
		Values: map[string]string{"x": "hello"},
	}

	for _, name := range []string{"verb", "word", "field", "official"} {
		t.Run(name, func(t *testing.T) {
			l := newLab(t)
			r, dir := seeded(t, l, plain)
			prepare(t, r, call)

			list := lists[name]
			saved := *list
			*list = append(slices.Clone(saved), "demo")

			t.Cleanup(func() { *list = saved })

			if ReservedID("demo") == "" {
				t.Fatal("the id is not reserved")
			}

			prepare(t, l.reg(nil), call)

			rewrite(t, dir, withVersion(plain, "1.0.1"))

			upd := InstallRequest{Source: dir, ID: "demo", Update: true}

			if name == "official" {
				if pe := planErr(t, l.reg(nil), upd); pe.Code != CodeIDInvalid {
					t.Errorf("update under an official id = %v", pe)
				}
			} else {
				must(t, plan(t, l.reg(nil), upd).Abort())
			}

			fresh := newLab(t)
			fresh.fakeBin("tool")
			src := fresh.srcDir("demo", map[string]string{"plugin.toml": plain})

			pe := planErr(t, fresh.reg(nil), InstallRequest{Source: src})
			if pe.Code != CodeIDInvalid {
				t.Errorf("a new install under the reserved id = %v", pe)
			}
		})
	}
}
