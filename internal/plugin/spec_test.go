package plugin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestReviewEnvTableRunsUndeclaredPrograms is a proof of concept for a gap in the closed set of
// refused environment names (specification 3.6: "loader and interpreter variables refused"). A
// declarative plugin that declares only exec:git and env:GIT_EXTERNAL_DIFF makes git run a program
// it never declared, and the consent view lists the command as plain git. The test installs such a
// plugin in a sandbox, runs "git diff" in a sandbox repository and fails when the program the
// manifest named in its env table ran. It passes once the manifest is refused (or the variable is
// blocked), which is the fix.
//
// Parameters:
//   - t: test to report to
func TestReviewEnvTableRunsUndeclaredPrograms(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	l := newLab(t)
	l.useSystemTools()

	canary := filepath.Join(l.root, "canary")
	script := filepath.Join(l.root, "ext.sh")

	body := "#!/bin/sh\ntouch '" + canary + "'\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(l.root, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}

	l.git(repo, "init", "-q", "-b", "main")

	file := filepath.Join(repo, "f.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	l.git(repo, "add", "f.txt")
	l.git(repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "m")

	if err := os.WriteFile(file, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	src := strings.Replace(decl("", []string{"exec:git", "env:GIT_EXTERNAL_DIFF"},
		"[[command]]\nid = \"d\"\nlabel = \"D\"\nrun = [\"git\", \"diff\"]\noutput = \"pager\"\n"+
			"cwd = \""+repo+"\"\nenv = { GIT_EXTERNAL_DIFF = \""+script+"\" }\n"),
		`id = "demo"`, `id = "poc"`, 1)

	if m, _ := decode(src); m == nil {
		return
	}

	l.install("poc", map[string]string{"plugin.toml": src})

	r := l.reg(nil)
	p := prepare(t, r, Call{Plugin: "poc", Table: TableCommand, Entry: "d"})

	if out := r.Run(t.Context(), p, nil); out.Status == OutcomeNotStarted {
		t.Fatalf("outcome %+v", out)
	}

	if _, err := os.Stat(canary); err == nil {
		t.Errorf("git ran %s, a program that the manifest never declared with exec:", script)
	}
}

// TestReviewBlockedEnvCoversProgramVariables checks that the environment names through which a
// declared program runs another program are refused. The specification lists the loader and
// interpreter variables with an open "..."; these are the ones of git, pagers, editors and viewers
// that execute their value.
//
// Parameters:
//   - t: test to report to
func TestReviewBlockedEnvCoversProgramVariables(t *testing.T) {
	for _, name := range []string{
		"GIT_PAGER", "GIT_EXTERNAL_DIFF", "GIT_EDITOR",
		"GIT_SEQUENCE_EDITOR", "PAGER", "EDITOR", "VISUAL", "LESSOPEN", "LESSCLOSE", "MANPAGER",
	} {
		if !BlockedEnvName(name) {
			t.Errorf("%s is accepted in a manifest: it makes git, less or man run its value", name)
		}
	}
}

// TestReviewGftTagIsNotASubcommand checks the bundled gft manifest against the real gft 1.0.6
// parser, which treats the words update, delete, uninstall and stash as subcommands wherever they
// appear in its arguments: "gft owner/repo update" upgrades gft itself from the network without
// asking. The tag field of the palette commands must not let such a word through.
//
// Parameters:
//   - t: test to report to
func TestReviewGftTagIsNotASubcommand(t *testing.T) {
	l, _, _ := m1Lab(t)
	r := m1Registry(l)

	for _, tag := range []string{"update", "delete", "uninstall", "stash"} {
		_, err := r.Prepare(Call{
			Plugin: "gft", Table: TableCommand, Entry: "show",
			Values: map[string]string{"slug": "a/b", "tag": tag},
		})
		if err == nil {
			t.Errorf("the tag %q reaches gft, which runs its %s subcommand", tag, tag)
		}
	}
}

// TestReviewReservedVerbsFollowHelpGo is the drift test that the specification (3.1) and the
// comment of coreVerbs promise: every name that cmd/tote/help.go registers for a verb must be a
// reserved plugin id.
//
// Parameters:
//   - t: test to report to
func TestReviewReservedVerbsFollowHelpGo(t *testing.T) {
	path := filepath.Join("..", "..", "cmd", "tote", "help.go")

	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Skipf("cannot read the verbs: %v", err)
	}

	var names []string

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

		for _, e := range lit.Elts {
			if b, ok := e.(*ast.BasicLit); ok {
				s, _ := strconv.Unquote(b.Value)
				names = append(names, s)
			}
		}

		return true
	})

	if len(names) == 0 {
		t.Fatal("no verb found in help.go: the parser of this test needs updating")
	}

	for _, n := range names {
		if ReservedID(n) == "" {
			t.Errorf("the verb %q of cmd/tote/help.go is not a reserved plugin id", n)
		}
	}
}

// TestReviewSpecTomlBlocksParse checks that every toml block of the specification parses with the
// TOML library, so that the examples cannot rot (section 9). The block of a placeholder such as
// "<64 hex>" is a quoted string and parses too.
//
// Parameters:
//   - t: test to report to
func TestReviewSpecTomlBlocksParse(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "plans", "PLUGINS-API1.md"))
	if err != nil {
		t.Skipf("no specification: %v", err)
	}

	blocks := regexp.MustCompile("(?s)```toml\n(.*?)```").FindAllStringSubmatch(string(data), -1)
	if len(blocks) < 5 {
		t.Fatalf("found %d toml blocks, want at least 5", len(blocks))
	}

	for i, b := range blocks {
		var v map[string]any
		if _, err := toml.Decode(b[1], &v); err != nil {
			t.Errorf("toml block %d does not parse: %v", i+1, err)
		}
	}
}

// TestReviewSpecRemoteAndSlugTable pins the repository derivation table of section 3.5 and the
// refusals that follow it.
//
// Parameters:
//   - t: test to report to
func TestReviewSpecRemoteAndSlugTable(t *testing.T) {
	tests := []struct{ in, slug string }{
		{"https://github.com/o/r", "o/r"},
		{"https://github.com/o/r.git", "o/r"},
		{"https://github.com/o/r/", "o/r"},
		{"git@github.com:o/r.git", "o/r"},
		{"ssh://git@github.com/o/r.git", "o/r"},
		{"git@gh-alias:o/r.git", ""},
		{"https://github.com/o/r/extra", ""},
		{"https://github.com/../r", ""},
		{"ext::sh -c x", ""},
		{"file:///tmp/x", ""},
		{"ssh://-oProxyCommand=x/o/r", ""},
	}

	for _, tt := range tests {
		out, _ := CleanRemote(tt.in)
		if got := SlugOf(out); got != tt.slug {
			t.Errorf("slug of %q = %q, want %q", tt.in, got, tt.slug)
		}
	}
}

// TestReviewSpecWildcardCover pins the coverage rule of 7.6: a wildcard covers only a strict
// subdomain, and nothing else is implied.
//
// Parameters:
//   - t: test to report to
func TestReviewSpecWildcardCover(t *testing.T) {
	tests := []struct {
		old, next string
		asks      bool
	}{
		{"net:*.github.com", "net:api.github.com", false},
		{"net:*.github.com", "net:github.com", true},
		{"net:*.github.com", "net:evilgithub.com", true},
		{"fs:write:any", "fs:write:cwd", true},
		{"fs:write:cwd", "fs:read:cwd", true},
	}

	for _, tt := range tests {
		if got := len(Escalation([]string{tt.old}, []string{tt.next})) > 0; got != tt.asks {
			t.Errorf("%s -> %s asks=%v, want %v", tt.old, tt.next, got, tt.asks)
		}
	}
}
