package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// canaryScript is the body of every program a management verb might be tricked into starting:
// it writes a file and nothing else.
//
// Parameters:
//   - canary: path of the file the program writes
func canaryScript(canary string) string {
	return "echo ran >> '" + canary + "'\n"
}

// TestManagementVerbsExecuteNothing proves that installing, updating, enabling, disabling,
// granting, removing, listing leftovers and cleaning up never start a program: not the ones of the
// plugin, not the ones on PATH that a plugin names, not a hook, an installer or a helper in the
// package. Every one of them is a script that writes a canary file; the canary must not exist at
// the end.
//
// Parameters:
//   - t: test to report to
func TestManagementVerbsExecuteNothing(t *testing.T) {
	l := newLab(t)
	canary := filepath.Join(l.root, "canary")

	for _, name := range []string{
		"tool", "git", "gft", "tar", "unzip", "curl", "sh", "make",
		"npm", "pip", "go", "bash", "env",
	} {
		l.script(name, canaryScript(canary))
	}

	declarative := l.srcDir("decl", map[string]string{
		"plugin.toml": manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")),
	})
	program := strings.Replace(execM("", []string{"exec:tool"}, ""), `id = "demo"`,
		`id = "prog"`, 1)
	prog := l.srcDir("exec", map[string]string{
		"plugin.toml":      program,
		"bin/demo*":        canaryScript(canary),
		"hooks/pre*":       canaryScript(canary),
		"hooks/post*":      canaryScript(canary),
		"postinstall.sh*":  canaryScript(canary),
		"scripts/update*":  canaryScript(canary),
		"scripts/remove*":  canaryScript(canary),
		"scripts/disable*": canaryScript(canary),
	})

	b := bundle(t, l, map[string]string{"plugin.toml": manifest("bund", "1.0.0",
		[]string{"exec:tool"}, cmdWith("go", ""))})
	r := l.reg(func(o *Options) { o.Bundled = b })

	check := func(step string) {
		t.Helper()

		if _, err := os.Lstat(canary); err == nil {
			data, _ := os.ReadFile(canary)
			t.Fatalf("a program ran during %s: %q", step, data)
		}
	}

	for _, src := range []string{declarative, prog, "bund"} {
		st := plan(t, r, InstallRequest{Source: src})
		_ = st.Diff()
		check("Plan " + src)

		if _, err := st.Commit(Grant{Prompt: true}); err != nil {
			t.Fatal(err)
		}

		check("Commit " + src)
	}

	r.Load()
	r.Recheck("demo")
	check("Load")

	for _, id := range []string{"demo", "prog", "bund"} {
		if _, err := r.Allow(id); err != nil {
			t.Fatal(err)
		}

		if _, err := r.Disable(id); err != nil {
			t.Fatal(err)
		}

		check("Disable " + id)

		unconsent(t, l, id)

		if _, err := r.Enable(id, Grant{Prompt: true}); err != nil {
			t.Fatal(err)
		}

		check("Enable " + id)

		if _, err := r.Grant(id, Grant{Prompt: true}); err != nil {
			t.Fatal(err)
		}

		check("Grant " + id)
	}

	// An update over the installed ones, with a new version of each.
	rewrite(t, declarative, manifest("demo", "1.1.0", []string{"exec:tool"}, cmdWith("go", "")))

	st := plan(t, r, InstallRequest{Source: declarative})
	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}

	check("update")

	if _, err := r.Leftovers(); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Cleanup(); err != nil {
		t.Fatal(err)
	}

	check("Cleanup")

	for _, id := range []string{"demo", "prog", "bund"} {
		if _, err := r.Remove(id, RemoveOptions{Purge: true}); err != nil {
			t.Fatal(err)
		}

		check("Remove " + id)
	}

	// The sources that are not supported yet are refused before anything starts.
	for _, src := range []string{
		"https://example.org/p.tar.gz", "owner/repo@1.0.0",
		filepath.Join(l.root, "demo.zip"),
	} {
		if _, err := r.Plan(context.Background(), InstallRequest{Source: src}); err == nil {
			t.Errorf("Plan(%s) succeeded", src)
		}
	}

	check("unsupported sources")

	// The control: the canary is real, so the silence above means something.
	if err := exec.Command(filepath.Join(l.bin, "tool")).Run(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(canary); err != nil {
		t.Errorf("the canary does not work: %v", err)
	}
}
