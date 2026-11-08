package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecReviewEnvTableCodeExecution pins a consent bypass. A declarative plugin that covers git
// with exec:git and a code-running variable with env:GIT_EXTERNAL_DIFF makes git run a program
// that appears nowhere in the consent view: the command list shows only `git diff --no-index ...`
// and the values of the env table are in no view (Diff, Check, the confirmation). The test
// installs such a plugin, runs it and fails when the program named by the env table ran. The
// expected fix is to refuse the variable names that make git, pagers and editors run a program
// (GIT_EXTERNAL_DIFF GIT_PAGER GIT_EDITOR GIT_SEQUENCE_EDITOR PAGER EDITOR VISUAL LESSOPEN
// MANPAGER ...) and to show the env table values at consent.
//
// Parameters:
//   - t: test to report to
func TestSecReviewEnvTableCodeExecution(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()

	canary := filepath.Join(l.root, "canary")
	cwd := filepath.Join(l.root, "cwd")

	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}

	for name, content := range map[string]string{"a": "one\n", "b": "two\n"} {
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	body := `
[[command]]
id = "diff"
label = "Diff"
run = ["git", "diff", "--no-index", "a", "b"]
output = "pager"
env = { GIT_EXTERNAL_DIFF = "touch ` + canary + `" }
`

	// The wanted outcome is that Decode refuses the variable name, which closes the hole.
	if m, _ := decode(decl("", []string{"exec:git", "env:GIT_EXTERNAL_DIFF"}, body)); m == nil {
		return
	}

	l.plug("demo", []string{"exec:git", "env:GIT_EXTERNAL_DIFF"}, body)

	r := l.reg(nil)

	p, err := r.Prepare(Call{Plugin: "demo", Table: TableCommand, Entry: "diff"})
	if err != nil {
		return
	}

	r.Run(context.Background(), p, nil)

	if _, err := os.Stat(canary); err == nil {
		t.Fatalf("GIT_EXTERNAL_DIFF from the env table ran a program the consent view never showed")
	}
}

// TestSecReviewCodeEnvNamesRefused lists the variables that make a listed program run another one
// and that a manifest may still name in env: today. Each must be refused like LD_PRELOAD.
//
// Parameters:
//   - t: test to report to
func TestSecReviewCodeEnvNamesRefused(t *testing.T) {
	for _, n := range strings.Fields("GIT_EXTERNAL_DIFF GIT_PAGER GIT_EDITOR GIT_SEQUENCE_EDITOR " +
		"PAGER EDITOR VISUAL LESSOPEN LESSCLOSE MANPAGER GIT_TRACE GIT_TEMPLATE_DIR") {
		if !BlockedEnvName(n) {
			t.Errorf("BlockedEnvName(%q) = false: it makes a declared program run another", n)
		}
	}
}

// TestSecReviewHostFileHardlink pins a nit in WriteHostFile. The file is checked with Lstat and
// opened with O_NOFOLLOW, which stops a symbolic link, but a hard link to a file of the user is a
// regular file: the host truncates and rewrites it. The directory of host files is mode 0700 of
// the user, so a same-user plugin can plant the link; it could write the victim directly, which is
// why this is a nit, yet the point of plugins-host is that a plugin cannot make the host write for
// it. The fix is to refuse a file whose link count is not 1.
//
// Parameters:
//   - t: test to report to
func TestSecReviewHostFileHardlink(t *testing.T) {
	l := newLab(t)
	uid := os.Getuid()

	if err := WriteHostFile(l.paths, "demo", "last.log", []byte("one"), uid); err != nil {
		t.Fatal(err)
	}

	dir, err := l.paths.PluginHostDir("demo")
	if err != nil {
		t.Fatal(err)
	}

	victim := filepath.Join(l.root, "victim")

	if err := os.WriteFile(victim, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "last.log")

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(victim, link); err != nil {
		t.Fatal(err)
	}

	_ = WriteHostFile(l.paths, "demo", "last.log", []byte("clobbered"), uid)

	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "precious" {
		t.Fatalf("a hard link in plugins-host made the host rewrite %q", victim)
	}
}

// TestSecReviewRemoteHelperSyntax pins a nit in CleanRemote. A remote of the form
// <transport>::<address> makes git start the helper git-remote-<transport>; only ext:: and file:
// are refused, so fd::17 or any helper installed on the machine passes the filter and reaches an
// argv as {repo.remote}. No legitimate remote url holds "::" (an scp-like remote has one colon).
//
// Parameters:
//   - t: test to report to
func TestSecReviewRemoteHelperSyntax(t *testing.T) {
	for _, raw := range []string{"fd::17", "foo::bar", "hg::https://example.org/x"} {
		if out, ok := CleanRemote(raw); ok {
			t.Errorf("CleanRemote(%q) = %q, true: git would start a remote helper", raw, out)
		}
	}
}
