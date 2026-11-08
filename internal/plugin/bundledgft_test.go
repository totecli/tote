package plugin

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/plugin/bundled"
)

// m1Lab installs the real gft plugin of the binary by extraction, with the grant token the consent
// view shows, and puts a fake gft in PATH that records its argv, one element per line, in the file
// that it returns. It also returns a repository with a GitHub remote.
//
// Parameters:
//   - t: test to report to
func m1Lab(t *testing.T) (l *lab, rec, repo string) {
	t.Helper()

	l = newLab(t)
	l.useSystemTools()

	rec = filepath.Join(l.root, "argv.txt")
	l.script("gft", "printf '%s\\n' \"$@\" > '"+rec+"'\nprintf 'done\\n'\n")
	l.script(OpenerName("linux"), "exit 0\n")

	repo = l.gitRepo("myrepo", "git@github.com:20syldev/tote.git")

	st := plan(t, m1Registry(l), InstallRequest{Source: "gft"})
	d := st.Diff()

	if d.Kind != KindBundled || d.Class != OriginBundled || !d.NeedsConsent || d.Grant == "" ||
		d.ID != "gft" {
		t.Fatalf("diff %+v", d)
	}

	// The consent view lists what the plugin may do; the token binds those and the commands.
	for _, c := range []string{"exec:gft", "fs:write:any", "net:api.github.com"} {
		if !hasString(d.Caps, c) {
			t.Errorf("capability %q missing in %v", c, d.Caps)
		}
	}

	if _, err := st.Commit(Grant{Token: "wrong"}); err == nil {
		t.Fatal("a wrong grant token installed the plugin")
	}

	st = plan(t, m1Registry(l), InstallRequest{Source: "gft"})

	res, err := st.Commit(Grant{Token: st.Diff().Grant})
	if err != nil || res.State != StateEnabled {
		t.Fatalf("commit %+v %v", res, err)
	}

	return l, rec, repo
}

// m1Registry opens a registry over the sandbox that knows the bundled plugins of the binary.
//
// Parameters:
//   - l: sandbox
func m1Registry(l *lab) *Registry {
	return l.reg(func(o *Options) { o.Bundled = bundled.Set{} })
}

// hasString reports whether a list holds a string.
//
// Parameters:
//   - list: strings
//   - s: string to look for
func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}

	return false
}

// recorded reads and removes the argv that the fake gft recorded.
//
// Parameters:
//   - t: test to report to
//   - rec: record file
func recorded(t *testing.T, rec string) []string {
	t.Helper()

	b, err := os.ReadFile(rec)
	if err != nil {
		t.Fatalf("gft did not run: %v", err)
	}

	must(t, os.Remove(rec))

	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// TestM1BundledGft is the first milestone: the gft plugin of the binary is installed by
// extraction, enabled by its grant token, and prepares and runs the exact argv of its entries
// against a fake gft that records what it receives.
//
// Parameters:
//   - t: test to report to
func TestM1BundledGft(t *testing.T) {
	l, rec, repo := m1Lab(t)
	r := m1Registry(l)
	gft := filepath.Join(l.bin, "gft")

	found, ok := r.Load().Get("gft")
	if !ok || found.State != StateEnabled || found.Manifest == nil {
		t.Fatalf("gft after the grant: %+v", found)
	}

	run := func(t *testing.T, p Prepared) Outcome {
		t.Helper()

		out := r.Run(context.Background(), p, nil)
		if out.Status != OutcomeOK {
			t.Fatalf("outcome %+v", out)
		}

		return out
	}

	item := func(id string, values map[string]string) Call {
		return Call{Plugin: "gft", Table: TableItemAction, Entry: id, Repo: "myrepo", Values: values}
	}

	t.Run("drop a stash", func(t *testing.T) {
		p := prepare(t, r, item("stash-drop", map[string]string{"n": "2"}))

		want := []string{gft, "stash", "drop", "2", "--yes"}
		if strings.Join(p.Argv, "|") != strings.Join(want, "|") {
			t.Fatalf("argv %q want %q", p.Argv, want)
		}

		if p.Dir != repo || p.Display != "gft stash drop 2 --yes" || p.Output != OutputPager ||
			!p.Confirm.Required || p.Confirm.Text != "Drop stash 2 of myrepo?" {
			t.Errorf("dir %q display %q output %q confirm %+v", p.Dir, p.Display, p.Output,
				p.Confirm)
		}

		if g := r.Guard(p); g != nil {
			t.Fatalf("guard: %v", g)
		}

		if out := r.Run(context.Background(), p, nil); out.Status != OutcomeRefused ||
			out.Err == nil || out.Err.Code != CodeConfirmNeeded {
			t.Fatalf("a run that was not confirmed: %+v", out)
		}

		if _, err := os.Stat(rec); err == nil {
			t.Fatal("gft ran without the confirmation")
		}

		call := item("stash-drop", map[string]string{"n": "2"})
		call.Yes = true
		p = prepare(t, r, call)

		run(t, p)

		if got := recorded(t, rec); strings.Join(got, "|") != "stash|drop|2|--yes" {
			t.Errorf("gft received %q", got)
		}
	})

	t.Run("a release command", func(t *testing.T) {
		p := prepare(t, r, item("release", nil))

		if strings.Join(p.Argv, "|") != gft+"|20syldev/tote" {
			t.Fatalf("argv %q", p.Argv)
		}

		run(t, p)

		if got := recorded(t, rec); strings.Join(got, "|") != "20syldev/tote" {
			t.Errorf("gft received %q", got)
		}
	})

	t.Run("the terminal commands run as the host runs them", func(t *testing.T) {
		cmd := Call{Plugin: "gft", Table: TableCommand, Entry: "get", Values: map[string]string{
			"slug": "cli/cli", "tag": "v2.1.0", "pattern": "linux-amd64", "verify": "true",
		}}
		p := prepare(t, r, cmd)

		want := []string{gft, "cli/cli", "v2.1.0", "--get", "linux-amd64", "--checksum"}
		if strings.Join(p.Argv, "|") != strings.Join(want, "|") {
			t.Fatalf("argv %q want %q", p.Argv, want)
		}

		if !p.IsTerminal() || p.Pause != PauseAlways {
			t.Errorf("output %q pause %q", p.Output, p.Pause)
		}

		if g := r.Guard(p); g != nil {
			t.Fatalf("guard: %v", g)
		}

		// The host owns the terminal: it executes the argv with the environment of the plan.
		c := exec.Command(p.Argv[0], p.Argv[1:]...)
		c.Dir, c.Env = p.Dir, p.Env

		if out, err := c.CombinedOutput(); err != nil || string(out) != "done\n" {
			t.Fatalf("exec: %q %v", out, err)
		}

		if got := recorded(t, rec); strings.Join(got, "|") != strings.Join(want[1:], "|") {
			t.Errorf("gft received %q", got)
		}

		for _, kv := range p.Env {
			if strings.HasPrefix(kv, "GITHUB_TOKEN=") {
				t.Errorf("a variable that is not set appeared: %s", kv)
			}
		}
	})

	t.Run("install asks first", func(t *testing.T) {
		p := prepare(t, r, Call{
			Plugin: "gft", Table: TableCommand, Entry: "install",
			Values: map[string]string{"slug": "cli/cli"},
		})

		want := []string{gft, "cli/cli", "--install"}
		if strings.Join(p.Argv, "|") != strings.Join(want, "|") || !p.IsTerminal() ||
			!p.Confirm.Required || p.Confirm.Text != "Download and install cli/cli?" {
			t.Errorf("argv %q confirm %+v", p.Argv, p.Confirm)
		}
	})
}

// TestM1MissingBinary checks that without gft in PATH the plugin is in the missing-dependency
// state, says which program, and prepares nothing.
//
// Parameters:
//   - t: test to report to
func TestM1MissingBinary(t *testing.T) {
	l, _, _ := m1Lab(t)
	l.env["PATH"] = filepath.Join(l.root, "empty")

	r := m1Registry(l)

	p, ok := r.Recheck("gft")
	if !ok || p.State != StateMissingDependency {
		t.Fatalf("state %q (found %v)", p.State, ok)
	}

	e := refusal(t, r, Call{Plugin: "gft", Table: TableItemAction, Entry: "release", Repo: "myrepo"})
	if e.Code != CodeNotRunnable || e.Args[1] != string(StateMissingDependency) {
		t.Errorf("got %s %v", e.Code, e.Args)
	}

	// Putting the program back is enough: nothing was recorded against the plugin.
	l.env["PATH"] = l.bin + ":" + shellPath

	if p, _ := m1Registry(l).Recheck("gft"); p.State != StateEnabled {
		t.Errorf("state %q after the program came back", p.State)
	}
}

// TestM1Hostile tries to turn the tag, the slug and the state of gft against the host.
//
// Parameters:
//   - t: test to report to
func TestM1Hostile(t *testing.T) {
	l, rec, _ := m1Lab(t)
	r := m1Registry(l)

	show := func(values map[string]string) Call {
		return Call{Plugin: "gft", Table: TableCommand, Entry: "show", Values: values}
	}

	t.Run("values that are not a tag", func(t *testing.T) {
		for _, tag := range []string{
			"-rf", "--install", "v1 --install", "v1\n--install", "a;b",
			"$(id)", "`id`", "v1|x", strings.Repeat("v", 65),
		} {
			e := refusal(t, r, show(map[string]string{"slug": "cli/cli", "tag": tag}))

			if e.Code != CodeInputInvalid && e.Code != CodeDashRefused {
				t.Errorf("tag %q: %s %v", tag, e.Code, e.Args)
			}
		}
	})

	t.Run("values that are not a slug", func(t *testing.T) {
		for _, slug := range []string{
			"--install", "-x/y", "a/b/c", "a b/c", "a/b;id", "../x",
			"a/b\n", "", "a", "./x", "a/..", "a/.",
		} {
			if p, err := r.Prepare(show(map[string]string{"slug": slug})); err == nil {
				t.Errorf("slug %q accepted: %q", slug, p.Argv)
			}
		}
	})

	t.Run("a view that is not a choice", func(t *testing.T) {
		e := refusal(t, r, show(map[string]string{"slug": "cli/cli", "view": "--install"}))

		if e.Code != CodeInputInvalid {
			t.Errorf("got %s %v", e.Code, e.Args)
		}
	})

	t.Run("a tag with two dots is one element", func(t *testing.T) {
		// gft reads v1..v2 as a comparison. The host only guarantees that it stays one element.
		p := prepare(t, r, show(map[string]string{"slug": "cli/cli", "tag": "v1..v2"}))

		if len(p.Argv) != 3 || p.Argv[2] != "v1..v2" {
			t.Errorf("argv %q", p.Argv)
		}
	})

	t.Run("a call to an entry that does not exist", func(t *testing.T) {
		for _, c := range []Call{
			{Plugin: "gft", Table: TableCommand, Entry: "nope"},
			{Plugin: "gft", Table: TableItemAction, Entry: "show", Repo: "myrepo"},
			{Plugin: "gft", Table: "hook", Entry: "x"},
			{Plugin: "../gft", Table: TableCommand, Entry: "show"},
		} {
			if _, err := r.Prepare(c); err == nil {
				t.Errorf("call %+v accepted", c)
			}
		}
	})

	t.Run("no plugins", func(t *testing.T) {
		off := l.reg(func(o *Options) {
			o.Bundled = bundled.Set{}
			o.NoPlugins = true
		})

		e := refusal(t, off, Call{
			Plugin: "gft", Table: TableItemAction, Entry: "release",
			Repo: "myrepo",
		})
		if e.Code != CodeNotRunnable {
			t.Errorf("got %s %v", e.Code, e.Args)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		if _, err := r.Disable("gft"); err != nil {
			t.Fatal(err)
		}

		e := refusal(t, r, Call{
			Plugin: "gft", Table: TableItemAction, Entry: "release",
			Repo: "myrepo",
		})
		if e.Code != CodeNotRunnable || e.Args[1] != string(StateDisabled) {
			t.Errorf("got %s %v", e.Code, e.Args)
		}
	})

	if _, err := os.Stat(rec); err == nil {
		t.Error("gft ran during the refusals")
	}
}

// TestM1TamperedTree checks that a change of one byte in the installed copy stops the plugin,
// before any program is looked for.
//
// Parameters:
//   - t: test to report to
func TestM1TamperedTree(t *testing.T) {
	l, rec, _ := m1Lab(t)
	r := m1Registry(l)

	rc, ok := l.lock().Get("gft")
	if !ok {
		t.Fatal("no record")
	}

	tree := l.treeDir(rc)
	file := filepath.Join(tree, "plugin.toml")

	must(t, os.Chmod(tree, 0o700))
	must(t, os.Chmod(file, 0o600))

	b, err := os.ReadFile(file)
	must(t, err)
	must(t, os.WriteFile(file, append(b, []byte("# changed\n")...), 0o600))

	e := refusal(t, r, Call{Plugin: "gft", Table: TableItemAction, Entry: "release", Repo: "myrepo"})
	if e.Code != CodeNotRunnable {
		t.Errorf("got %s %v", e.Code, e.Args)
	}

	if p, _ := r.Recheck("gft"); p.State != StateBroken {
		t.Errorf("state %q", p.State)
	}

	if _, err := os.Stat(rec); err == nil {
		t.Error("gft ran from a tampered tree")
	}
}

// TestM1PinGuard checks that the install of a bundled plugin whose files drifted from the pin is
// refused and leaves nothing behind, with the real embedded files.
//
// Parameters:
//   - t: test to report to
func TestM1PinGuard(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()

	good := bundled.Set{}
	fsys, version, tree, surface, ok := good.Lookup("gft")

	if !ok {
		t.Fatal("no gft")
	}

	for _, drift := range []driftedSet{
		{fsys: fsys, version: version, tree: strings.Repeat("0", 64), surface: surface},
		{fsys: fsys, version: version, tree: tree, surface: strings.Repeat("f", 64)},
	} {
		r := l.reg(func(o *Options) { o.Bundled = drift })

		if pe := planErr(t, r, InstallRequest{Source: "gft"}); pe.Code != CodeTreeHash {
			t.Errorf("drifted pin: %v", pe)
		}
	}

	if names := stagingNames(t, l); len(names) != 0 {
		t.Errorf("staging after a refusal = %v", names)
	}

	if _, ok := l.lock().Get("gft"); ok {
		t.Error("a record was written")
	}
}

// driftedSet is a bundled set of gft whose pin was changed, as if the files and the pin had gone
// apart.
type driftedSet struct {
	fsys    fs.FS
	version string
	tree    string
	surface string
}

// Lookup implements BundledSet.
//
// Parameters:
//   - id: plugin id
func (d driftedSet) Lookup(id string) (fsys fs.FS, version, tree, surface string, ok bool) {
	if id != "gft" {
		return nil, "", "", "", false
	}

	return d.fsys, d.version, d.tree, d.surface, true
}

// IDs implements BundledSet.
func (d driftedSet) IDs() []string { return []string{"gft"} }
