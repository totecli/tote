package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gftCheck checks the gft fixture as a directory, in the sandbox of gftLab with a fake gft, from
// inside the repository myrepo.
//
// Parameters:
//   - t: test to report to
//   - mutate: changes the request; may be nil
func gftCheck(t *testing.T, mutate func(*CheckRequest)) (*CheckResult, string) {
	t.Helper()

	l := chkLab(t)
	l.useSystemTools()
	l.script("gft", "exit 0\n")

	repo := l.gitRepo("myrepo", "git@github.com:20syldev/tote.git")
	l.gitRepo("plain", "")

	req := CheckRequest{Target: l.srcDir("gft", gftFiles(t)), Official: true}
	if mutate != nil {
		mutate(&req)
	}

	res, err := l.reg(func(o *Options) { o.Cwd = repo }).Check(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	return res, repo
}

// contribution finds a contribution of a result.
//
// Parameters:
//   - t: test to report to
//   - res: result to search
//   - table: table of the contribution
//   - id: its id
func contribution(t *testing.T, res *CheckResult, table, id string) Contribution {
	t.Helper()

	for _, c := range res.Contributions {
		if c.Table == table && c.ID == id {
			return c
		}
	}

	t.Fatalf("no %s %s in the result", table, id)

	return Contribution{}
}

// TestCheckArgvOfGft checks the exact argv and directory that check prints for the repository
// myrepo, the table of section 10 for a repository whose remote is
// git@github.com:20syldev/tote.git.
//
// Parameters:
//   - t: test to report to
func TestCheckArgvOfGft(t *testing.T) {
	res, repo := gftCheck(t, func(r *CheckRequest) {
		r.Repo = "myrepo"
		r.Set = map[string]string{
			"n": "2", "pattern": "linux-amd64", "verify": "true",
			"view": "--assets",
		}
	})

	if res.Failed() {
		t.Fatalf("findings %+v", res.Findings)
	}

	tests := []struct {
		table, id string
		display   string
		dir       string
		output    string
	}{
		{"item_action", "release", "gft 20syldev/tote", repo, "pager"},
		{"item_action", "assets", "gft 20syldev/tote --assets", repo, "pager"},
		{"item_action", "notes", "gft 20syldev/tote --notes", repo, "pager"},
		{"item_action", "stash-list", "gft stash list", repo, "pager"},
		{"item_action", "stash-show", "gft stash show 2", repo, "pager"},
		{"item_action", "stash-drop", "gft stash drop 2 --yes", repo, "pager"},
		{"item_action", "stash-clean", "gft stash clean --yes", repo, "pager"},
		{"command", "show", "gft 20syldev/tote --assets", repo, "pager"},
		{"command", "get", "gft 20syldev/tote --get linux-amd64 --checksum", repo, "terminal"},
		{"command", "install", "gft 20syldev/tote --install", repo, "terminal"},
	}

	for _, tt := range tests {
		c := contribution(t, res, tt.table, tt.id)

		if !c.Available || c.Display != tt.display || c.Dir != tt.dir || c.Output != tt.output ||
			c.Why != nil || c.Program == "" || c.Argv[0] != "gft" {
			t.Errorf("%s %s: %+v", tt.table, tt.id, c)
		}
	}

	drop := contribution(t, res, "item_action", "stash-drop")
	if !drop.ConfirmRequired || drop.Confirm != "Drop stash 2 of myrepo?" {
		t.Errorf("confirm %q", drop.Confirm)
	}

	open := contribution(t, res, "command", "open")
	if open.URL != "https://github.com/20syldev/tote/releases" || len(open.Argv) != 0 ||
		!open.Available {
		t.Errorf("open %+v", open)
	}

	if len(res.Requires) != 1 || !res.Requires[0].Found {
		t.Errorf("requires %+v", res.Requires)
	}
}

// TestCheckArgvWithoutRepository checks what check shows when no repository is given and the
// working directory is outside any saved one: the template, and why it cannot be built.
//
// Parameters:
//   - t: test to report to
func TestCheckArgvWithoutRepository(t *testing.T) {
	l := chkLab(t)
	l.useSystemTools()
	l.script("gft", "exit 0\n")

	res, err := l.reg(nil).Check(context.Background(), CheckRequest{
		Target: l.srcDir("gft", gftFiles(t)), Official: true, Set: map[string]string{"n": "2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	rel := contribution(t, res, "item_action", "release")
	if rel.Available || rel.Why == nil || rel.Why.Code != CodeUnavailable ||
		rel.Why.Args[0] != "repo" || rel.Display != "gft '{repo.slug}'" || len(rel.Argv) != 0 {
		t.Errorf("release %+v", rel)
	}

	show := contribution(t, res, "command", "show")
	if show.Available || show.Why.Code != CodeInputInvalid || show.Why.Args[0] != "slug" ||
		show.Display != "gft '{slug}' '{tag?}' '{view?}'" {
		t.Errorf("show %+v", show)
	}

	if find := res.find(CodeSetUnknown); find != nil {
		t.Errorf("n is a field of two contributions: %+v", find)
	}
}

// TestCheckArgvRefusals checks that check shows the refusals Prepare would give: the dash rule, a
// value outside its pattern, a required field left empty, and the unknown values of the request.
//
// Parameters:
//   - t: test to report to
func TestCheckArgvRefusals(t *testing.T) {
	res, _ := gftCheck(t, func(r *CheckRequest) {
		r.Repo = "myrepo"
		r.Set = map[string]string{"tag": "-rf", "n": "1 --force", "pattern": "x", "nothing": "1"}
	})

	tests := []struct {
		table, id string
		code      string
		arg       string
	}{
		{"command", "show", CodeDashRefused, "tag"},
		{"command", "install", CodeDashRefused, "tag"},
		{"item_action", "stash-drop", CodeInputInvalid, "n"},
		{"item_action", "stash-show", CodeInputInvalid, "n"},
	}

	for _, tt := range tests {
		c := contribution(t, res, tt.table, tt.id)

		if c.Available || c.Why == nil || c.Why.Code != tt.code || c.Why.Args[0] != tt.arg {
			t.Errorf("%s %s: %+v", tt.table, tt.id, c)
		}
	}

	if !contribution(t, res, "item_action", "release").Available {
		t.Error("the values of another contribution spoiled release")
	}

	f := res.find(CodeSetUnknown)
	if f == nil || f.Args[0] != "nothing" || f.Scope != ScopeRequest {
		t.Errorf("unknown value: %+v", f)
	}

	if f.intrinsic() {
		t.Error("an unknown value must not fail --strict")
	}
}

// TestCheckArgvMissingProgram checks a contribution whose program is not installed: the argv is
// exact, the program is empty and the reason is dependency_missing, with the hint of the manifest.
//
// Parameters:
//   - t: test to report to
func TestCheckArgvMissingProgram(t *testing.T) {
	l := chkLab(t)
	l.useSystemTools()

	repo := l.gitRepo("myrepo", "git@github.com:20syldev/tote.git")

	res, err := l.reg(func(o *Options) { o.Cwd = repo }).Check(context.Background(),
		CheckRequest{
			Target: l.srcDir("gft", gftFiles(t)), Official: true, Repo: "myrepo",
			Lang: "fr",
		})
	if err != nil {
		t.Fatal(err)
	}

	c := contribution(t, res, "item_action", "release")
	if !c.Available || c.Display != "gft 20syldev/tote" || c.Program != "" || c.Why == nil ||
		c.Why.Code != CodeDepMissing || c.Why.Args[0] != "gft" {
		t.Errorf("release %+v", c)
	}

	if len(res.Requires) != 1 || res.Requires[0].Found ||
		res.Requires[0].Hint != "FR requires.gft.hint" {
		t.Errorf("requires %+v", res.Requires)
	}

	f := res.find(ReasonMissingDep)
	if f == nil || f.Scope != ScopeEnv || f.Severity != SeverityWarning {
		t.Errorf("finding %+v", f)
	}

	label := contribution(t, res, "item_action", "release").Label
	if label != "FR item_action.release.label" {
		t.Errorf("label %q", label)
	}
}

// TestCheckListAndExec checks the contributions of an exec plugin: the list and its actions. A
// list action needs a row, and the lists and actions that the program answers show the program.
//
// Parameters:
//   - t: test to report to
func TestCheckListAndExec(t *testing.T) {
	l := chkLab(t)
	l.useSystemTools()
	l.script("gh", "exit 0\n")

	files := map[string]string{
		"plugin.toml": string(readFixture(t, "ghrel/plugin.toml")),
		"bin/ghrel":   "#!/bin/sh\nexit 0\n",
	}

	res := chkDir(t, l, files, func(r *CheckRequest) { r.Official = true })

	if res.Failed() {
		t.Fatalf("findings %+v", res.Findings)
	}

	list := contribution(t, res, "list", "latest")
	if !list.Available || strings.Join(list.Argv, " ") != "bin/ghrel" || list.Display != "bin/ghrel" {
		t.Errorf("list %+v", list)
	}

	notes := contribution(t, res, "list.action", "notes")
	if notes.Available || notes.List != "latest" || notes.Why == nil ||
		notes.Why.Code != CodeUnavailable || notes.Display != "gh release view --repo '{item.id}'" {
		t.Errorf("notes %+v", notes)
	}

	if !strings.Contains(strings.Join(res.Caps, ","), CapDerivedCode) {
		t.Errorf("caps %v", res.Caps)
	}

	plain := chkDir(t, l, files, nil)
	if f := plain.find(CodeIDInvalid); f == nil {
		t.Errorf("the official id was accepted for a plain directory: %+v", plain.Findings)
	}
}

// TestCheckInstalledGft checks an installed plugin: its state, the hashes of its record and the
// contributions, and that a tree that changed gives a broken state with an error finding.
//
// Parameters:
//   - t: test to report to
func TestCheckInstalledGft(t *testing.T) {
	l, repo := gftLab(t)
	r := l.reg(func(o *Options) { o.Cwd = repo })

	res, err := r.Check(context.Background(), CheckRequest{Target: "gft", Repo: "myrepo"})
	if err != nil {
		t.Fatal(err)
	}

	rec, _ := l.lock().Get("gft")
	if res.Kind != TargetInstalled || res.State != StateEnabled || res.TreeSHA256 != rec.TreeSHA256 ||
		res.Failed() || res.Version != "1.0.0" {
		t.Errorf("result %+v", res)
	}

	if c := contribution(t, res, "item_action", "stash-clean"); c.Display != "gft stash clean --yes" {
		t.Errorf("stash-clean %+v", c)
	}

	tree := l.treeDir(rec)
	l.open(tree)
	l.open(filepath.Join(tree, "plugin.toml"))
	must(t, os.WriteFile(filepath.Join(tree, "plugin.toml"), []byte("x"), 0o600))

	res, err = r.Check(context.Background(), CheckRequest{Target: "gft"})
	if err != nil {
		t.Fatal(err)
	}

	if res.State != StateBroken || !res.Failed() || res.Errors() == 0 || len(res.Contributions) != 0 {
		t.Errorf("tampered: %v %+v", res.State, res.Findings)
	}
}

// TestCheckMentionsRejectedSettings checks that a setting value the user wrote and the plugin
// refuses is listed, as a warning that --strict does not count.
//
// Parameters:
//   - t: test to report to
func TestCheckMentionsRejectedSettings(t *testing.T) {
	l := chkLab(t)

	body := cmdBody + "[[setting]]\nid = \"mode\"\ntype = \"enum\"\nlabel = \"Mode\"\n" +
		"choices = [\"a\", \"b\"]\ndefault = \"a\"\n"
	l.install("demo", chkPlugin(tool, body, map[string]string{"locales/en.toml": "\"setting.mode." +
		"choice.a\" = \"A\"\n\"setting.mode.choice.b\" = \"B\"\n"}))
	l.writeFile(filepath.Join(l.paths.Config, "plugins", "demo.toml"),
		"schema = 1\nmode = \"zzz\"\nstale = 1\n")

	res, err := l.reg(nil).Check(context.Background(), CheckRequest{Target: "demo", Strict: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Rejected) != 1 || res.Rejected[0].Setting != "mode" ||
		res.Rejected[0].Source != "file" {
		t.Errorf("rejected %+v", res.Rejected)
	}

	if !res.Has(CodeSettingsIgnore) || !res.Has(CodeSettingsKey) || res.Failed() {
		t.Errorf("findings %+v failed %v", res.Findings, res.Failed())
	}
}
