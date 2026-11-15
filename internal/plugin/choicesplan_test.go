package plugin

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// choicesBody is a plugin of the sandbox with suggestions: a palette command whose fields are
// declared in this order: slug, memo (no argv names it), tag, asset and last.
const choicesBody = `
[[setting]]
id = "pre"
type = "bool"
label = "Pre"
default = false

[[command]]
id = "get"
label = "Get"
run = ["tool", "--", "{slug}", "{tag?}", "{asset?}"]
output = "pager"
[command.env]
A = "from-the-table"
[[command.field]]
id = "slug"
type = "text"
label = "Slug"
required = true
pattern = "^[a-z]+/[a-z]+$"
[[command.field]]
id = "memo"
type = "text"
label = "Memo"
required = true
[[command.field]]
id = "tag"
type = "text"
label = "Tag"
pattern = "^[a-z0-9.-]{0,16}$"
choices_from = ["tool", "--list", "--pre={setting.pre}", "--", "{slug}"]
[[command.field]]
id = "asset"
type = "text"
label = "Asset"
choices_from = ["tool", "--assets", "--", "{slug}", "{tag?}"]
[[command.field]]
id = "last"
type = "text"
label = "Last"
required = true

[[item_action]]
id = "pick"
target = "repo"
label = "Pick"
run = ["tool", "--", "{repo.slug}", "{tag}"]
output = "pager"
[[item_action.field]]
id = "tag"
type = "text"
label = "Tag"
choices_from = ["tool", "--list", "--", "{repo.slug}"]

[[command]]
id = "plain"
label = "Plain"
run = ["tool", "{x}"]
output = "pager"
[[command.field]]
id = "x"
type = "text"
label = "X"
choices_from = ["tool", "--", "{repo.slug}"]
`

// choicesPerms are the permissions of choicesBody.
var choicesPerms = []string{
	"exec:tool", "host:read:repos", "env:A", "env:GITHUB_TOKEN", "net:example.com",
}

// choicesLab builds a sandbox with the plugin of choicesBody and a registry over it.
//
// Parameters:
//   - t: test to report to
func choicesLab(t *testing.T) (*lab, *Registry) {
	t.Helper()

	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")
	l.plug("demo", choicesPerms, choicesBody)

	return l, l.reg(nil)
}

// getCall is a call of the palette command get with the given values.
//
// Parameters:
//   - values: field values by id
func getCall(values map[string]string) Call {
	return Call{Plugin: "demo", Table: TableCommand, Entry: "get", Values: values}
}

// choices runs Registry.Choices and returns the result, failing the test when it is refused.
//
// Parameters:
//   - t: test to report to
//   - r: registry
//   - call: the call
//   - field: field id
func choices(t *testing.T, r *Registry, call Call, field string) Prepared {
	t.Helper()

	p, err := r.Choices(call, field)
	if err != nil {
		t.Fatalf("Choices: %v", err)
	}

	return p
}

// choicesRefusal runs Registry.Choices and returns the error, failing the test when there is none.
//
// Parameters:
//   - t: test to report to
//   - r: registry
//   - call: the call
//   - field: field id
func choicesRefusal(t *testing.T, r *Registry, call Call, field string) *Error {
	t.Helper()

	_, err := r.Choices(call, field)
	if err == nil {
		t.Fatal("Choices accepted the call")
	}

	pe, ok := err.(*Error)
	if !ok {
		t.Fatalf("error %T is not an *Error", err)
	}

	return pe
}

// TestChoicesBuildsTheRun checks the argv, the directory and the time limit of a prepared run.
//
// Parameters:
//   - t: test to report to
func TestChoicesBuildsTheRun(t *testing.T) {
	l, r := choicesLab(t)

	p := choices(t, r, getCall(map[string]string{"slug": "cli/cli"}), "tag")

	want := []string{filepath.Join(l.bin, "tool"), "--list", "--pre=false", "--", "cli/cli"}
	if !slices.Equal(p.Argv, want) {
		t.Errorf("argv = %v, want %v", p.Argv, want)
	}

	if p.Name != "tool" || p.PluginID != "demo" || p.EntryID != "get" || p.FieldID != "tag" ||
		p.Output != OutputChoices || !p.IsChoices() || p.IsTerminal() || p.IsOpen() {
		t.Errorf("prepared = %+v", p)
	}

	if p.Display != "tool --list --pre=false -- cli/cli" {
		t.Errorf("display = %q", p.Display)
	}

	if p.Timeout != ChoicesTimeout || ChoicesTimeout.Seconds() != 10 {
		t.Errorf("timeout = %v", p.Timeout)
	}

	if p.Confirm.Required || p.Confirmed || len(p.EnvTable) != 0 || p.Pause != "" {
		t.Errorf("a suggestions run asks for nothing: %+v", p)
	}

	cache, err := l.paths.PluginCacheDir("demo")
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(cache)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("cache directory %s: %v %v", cache, info, err)
	}

	if p.Dir != cache {
		t.Errorf("dir = %q, want the cache directory %q", p.Dir, cache)
	}

	if err := r.Guard(p); err != nil {
		t.Errorf("Guard refused a fresh run: %v", err)
	}

	out := r.Run(context.Background(), p, nil)
	if out.Status != OutcomeRefused || out.Err == nil || out.Err.Code != CodeUnsupported {
		t.Errorf("Run took a suggestions run: %+v", out)
	}
}

// TestChoicesStartsNothing checks that preparing the suggestions, like preparing a call, starts no
// program of the plugin.
//
// Parameters:
//   - t: test to report to
func TestChoicesStartsNothing(t *testing.T) {
	l, r := choicesLab(t)
	marker := filepath.Join(l.root, "started")
	l.script("tool", "touch '"+marker+"'\n")

	choices(t, r, getCall(map[string]string{"slug": "cli/cli"}), "tag")

	if _, err := os.Stat(marker); err == nil {
		t.Error("the program started")
	}
}

// TestChoicesWaitsForEarlierFields checks which earlier fields hold the suggestions back.
//
// Parameters:
//   - t: test to report to
func TestChoicesWaitsForEarlierFields(t *testing.T) {
	_, r := choicesLab(t)

	for _, tt := range []struct {
		name   string
		field  string
		values map[string]string
		wait   string
	}{
		{"named field missing", "tag", nil, "slug"},
		{"named field invalid", "tag", map[string]string{"slug": "not valid"}, "slug"},
		{"named field with a control character", "tag", map[string]string{"slug": "a/b\x1b"}, "slug"},
		{"optional field invalid", "asset", map[string]string{"slug": "a/b", "tag": "BAD!"}, "tag"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := choicesRefusal(t, r, getCall(tt.values), tt.field)

			if w, ok := ChoicesWaiting(err); !ok || w != tt.wait {
				t.Errorf("error %v: waiting for %q %v, want %q", err, w, ok, tt.wait)
			}
		})
	}

	// memo is required, empty and not named by the argv: it does not block the list of tag.
	p := choices(t, r, getCall(map[string]string{"slug": "a/b"}), "tag")
	if p.Argv[len(p.Argv)-1] != "a/b" {
		t.Errorf("argv = %v", p.Argv)
	}

	p = choices(t, r, getCall(map[string]string{"slug": "a/b"}), "asset")
	if !slices.Equal(p.Argv[1:], []string{"--assets", "--", "a/b"}) {
		t.Errorf("argv with an empty tag = %v", p.Argv)
	}

	p = choices(t, r, getCall(map[string]string{"slug": "a/b", "tag": "v1.2"}), "asset")
	if !slices.Equal(p.Argv[1:], []string{"--assets", "--", "a/b", "v1.2"}) {
		t.Errorf("argv with a tag = %v", p.Argv)
	}

	for _, tt := range []struct {
		name string
		err  error
		wait string
		ok   bool
	}{
		{"no error", nil, "", false},
		{"a plain error", os.ErrNotExist, "", false},
		{"a state refusal", newError(CodeNotRunnable, "demo", "disabled"), "", false},
		{"an invalid field", newError(CodeInputInvalid, "slug", RejectPattern), "slug", true},
		{"an empty placeholder", newError(CodeUnavailable, "repo.slug", "empty"), "repo.slug", true},
		{"an unknown value", newError(CodeInputInvalid, "zz", RejectUnknown), "", false},
		{"a missing repository", newError(CodeUnavailable, "repo", "unknown"), "", false},
		{"a produced text", newError(CodeProducedInvalid, "argv", "control"), "", false},
	} {
		if w, ok := ChoicesWaiting(tt.err); w != tt.wait || ok != tt.ok {
			t.Errorf("%s: ChoicesWaiting = %q %v, want %q %v", tt.name, w, ok, tt.wait, tt.ok)
		}
	}
}

// TestChoicesFiltersTheValues checks that the values the form still sends for the field itself and
// for the later fields are ignored, and that a name that is no field is refused.
//
// Parameters:
//   - t: test to report to
func TestChoicesFiltersTheValues(t *testing.T) {
	_, r := choicesLab(t)

	p := choices(t, r, getCall(map[string]string{
		"slug": "a/b", "tag": "--upgrade\x00", "asset": "x y", "last": "",
	}), "tag")

	if !slices.Equal(p.Argv[1:], []string{"--list", "--pre=false", "--", "a/b"}) {
		t.Errorf("argv = %v", p.Argv)
	}

	err := choicesRefusal(t, r, getCall(map[string]string{"slug": "a/b", "zz": "1"}), "tag")
	if err.Code != CodeInputInvalid || err.Args[0] != "zz" {
		t.Errorf("unknown value: %v", err)
	}

	for _, field := range []string{"memo", "last", "nope", ""} {
		if err := choicesRefusal(t, r, getCall(nil), field); err.Code != CodeNoEntry {
			t.Errorf("field %q: %v", field, err)
		}
	}

	if err := choicesRefusal(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "nope"},
		"tag"); err.Code != CodeNoEntry {
		t.Errorf("unknown entry: %v", err)
	}

	if err := choicesRefusal(t, r, Call{Plugin: "No Way", Table: TableCommand, Entry: "get"},
		"tag"); err.Code != CodeBadID {
		t.Errorf("bad id: %v", err)
	}
}

// TestChoicesDashRule checks the dash rule on the values an argv reads: a text field is never
// exempt. A field with no pattern is stopped by the rule itself; a pattern that starts with an
// alphanumeric stops the value before the rule is reached.
//
// Parameters:
//   - t: test to report to
func TestChoicesDashRule(t *testing.T) {
	body := `
[[command]]
id = "go"
label = "Go"
run = ["tool"]
output = "pager"
[[command.field]]
id = "free"
type = "text"
label = "Free"
[[command.field]]
id = "strict"
type = "text"
label = "Strict"
pattern = "^[A-Za-z0-9]+([._+-][A-Za-z0-9]+)*$"
[[command.field]]
id = "a"
type = "text"
label = "A"
choices_from = ["tool", "{free}"]
[[command.field]]
id = "b"
type = "text"
label = "B"
choices_from = ["tool", "{strict}"]
`
	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")
	l.plug("demo", []string{"exec:tool"}, body)

	r := l.reg(nil)
	call := func(k, v string) Call {
		return Call{Plugin: "demo", Table: TableCommand, Entry: "go", Values: map[string]string{k: v}}
	}

	for _, v := range []string{"--upgrade", "-q", "-"} {
		if err := choicesRefusal(t, r, call("free", v), "a"); err.Code != CodeDashRefused {
			t.Errorf("free %q: %v", v, err)
		}

		if _, err := r.Prepare(Call{
			Plugin: "demo", Table: TableCommand, Entry: "go",
			Values: map[string]string{"free": v},
		}); err != nil {
			t.Errorf("Prepare never reads the argv of the suggestions: %v", err)
		}

		err := choicesRefusal(t, r, call("strict", v), "b")
		if w, ok := ChoicesWaiting(err); err.Code != CodeInputInvalid || !ok || w != "strict" {
			t.Errorf("strict %q: %v", v, err)
		}
	}

	for _, v := range []string{"1..2", "a--b", "a/b"} {
		if err := choicesRefusal(t, r, call("strict", v), "b"); err.Code != CodeInputInvalid {
			t.Errorf("strict %q: %v", v, err)
		}
	}

	choices(t, r, call("strict", "v1.0.0-rc"), "b")

	if p := choices(t, r, call("free", "v1.0"), "a"); p.Argv[len(p.Argv)-1] != "v1.0" {
		t.Errorf("argv = %v", p.Argv)
	}
}

// TestChoicesRepository checks {repo.*} in the argv: the saved repository of an item action, the
// current repository of a palette command, and what each does without one.
//
// Parameters:
//   - t: test to report to
func TestChoicesRepository(t *testing.T) {
	l, r := choicesLab(t)
	repo := l.gitRepo("proj", "git@github.com:owner/proj.git")

	item := Call{Plugin: "demo", Table: TableItemAction, Entry: "pick", Repo: "proj"}

	p := choices(t, r, item, "tag")
	if !slices.Equal(p.Argv[1:], []string{"--list", "--", "owner/proj"}) {
		t.Errorf("item action argv = %v", p.Argv)
	}

	if p.Dir == repo {
		t.Error("the working directory is the repository")
	}

	// An item action with no repository fails like Prepare does, and nothing is waited for.
	item.Repo = ""

	err := choicesRefusal(t, r, item, "tag")
	if err.Code != CodeUnavailable || err.Args[0] != "repo" {
		t.Errorf("no repository: %v", err)
	}

	if _, waiting := ChoicesWaiting(err); waiting {
		t.Error("a missing repository is not a field to wait for")
	}

	// A palette command outside a repository has an empty {repo.slug}: the form waits.
	plain := Call{Plugin: "demo", Table: TableCommand, Entry: "plain"}

	err = choicesRefusal(t, r, plain, "x")
	if w, ok := ChoicesWaiting(err); !ok || w != "repo.slug" {
		t.Errorf("palette command outside a repository: %v", err)
	}

	// With a saved name it uses that repository, from any directory.
	plain.Repo = "proj"

	if p := choices(t, r, plain, "x"); p.Argv[len(p.Argv)-1] != "owner/proj" {
		t.Errorf("palette command with a repository: %v", p.Argv)
	}

	inside := l.reg(func(o *Options) { o.Cwd = repo })

	plain.Repo = ""

	if p := choices(t, inside, plain, "x"); p.Argv[len(p.Argv)-1] != "owner/proj" {
		t.Errorf("palette command in the repository: %v", p.Argv)
	}
}

// TestChoicesRepositoryOnlyForTheArgv checks that an unreadable repository file breaks only
// the suggestions that read {repo.*}, not Prepare.
//
// Parameters:
//   - t: test to report to
func TestChoicesRepositoryOnlyForTheArgv(t *testing.T) {
	l, r := choicesLab(t)
	l.writeFile(l.paths.ReposFile(), "schema = [[[\n")

	if _, err := r.Prepare(Call{
		Plugin: "demo", Table: TableCommand, Entry: "plain",
		Values: map[string]string{"x": "v"},
	}); err != nil {
		t.Errorf("Prepare looked the repository up: %v", err)
	}

	err := choicesRefusal(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "plain"}, "x")
	if err.Code != CodeUnavailable || err.Args[0] != "repo" || err.Args[1] != "unreadable" {
		t.Errorf("unreadable repositories: %v", err)
	}

	if p := choices(t, r, getCall(map[string]string{"slug": "a/b"}), "tag"); p.Argv == nil {
		t.Error("no argv")
	}
}

// TestChoicesEnvironment checks that a suggestions run gets the proxy variables and NO_COLOR=1,
// but never the env: permissions or the env table.
//
// Parameters:
//   - t: test to report to
func TestChoicesEnvironment(t *testing.T) {
	l, r := choicesLab(t)

	for k, v := range hostileParent {
		l.env[k] = v
	}

	l.env["A"] = "declared-value"
	l.env["PATH"] = l.bin + ":" + shellPath

	values := map[string]string{"slug": "a/b", "memo": "m", "last": "l"}

	p := choices(t, r, getCall(values), "tag")
	env := envMap(p.Env)

	if env["HTTP_PROXY"] == "" || env["https_proxy"] == "" || env["NO_PROXY"] == "" {
		t.Errorf("the proxy variables did not reach the program: %v", keysOf(p.Env))
	}

	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN", "A"} {
		if _, ok := env[k]; ok {
			t.Errorf("%s was passed to a suggestions run", k)
		}
	}

	if strings.Contains(strings.Join(p.Env, "\n"), "ghp_secret") ||
		strings.Contains(strings.Join(p.Env, "\n"), "from-the-table") {
		t.Errorf("a secret or an env table value leaked: %v", p.Env)
	}

	if env["NO_COLOR"] != "1" || env["GIT_TERMINAL_PROMPT"] != "0" || env["TOTE_PLUGIN_ID"] != "demo" {
		t.Errorf("NO_COLOR, GIT_TERMINAL_PROMPT or TOTE_PLUGIN_ID wrong: %v", p.Env)
	}

	cache, _ := l.paths.PluginCacheDir("demo")
	if env["TOTE_PLUGIN_CACHE"] != cache {
		t.Errorf("TOTE_PLUGIN_CACHE = %q, want %q", env["TOTE_PLUGIN_CACHE"], cache)
	}

	delete(l.env, "NO_COLOR")

	if got := envMap(choices(t, r, getCall(values), "tag").Env)["NO_COLOR"]; got != "1" {
		t.Errorf("NO_COLOR = %q without it in the parent", got)
	}

	// The same plugin, run as a command, does get the variable and the table.
	run := envMap(prepare(t, r, getCall(values)).Env)
	if run["GITHUB_TOKEN"] != "ghp_secretsecretsecret" || run["A"] != "from-the-table" {
		t.Errorf("the control run lost its variables: %v", run)
	}

	if _, ok := run["NO_COLOR"]; ok {
		t.Errorf("NO_COLOR leaked into a normal run: %v", run["NO_COLOR"])
	}
}

// TestChoicesNeedsAnEnabledPlugin checks that the suggestions go through the same state check as a
// run, and that a disable after the preparation is seen by Guard.
//
// Parameters:
//   - t: test to report to
func TestChoicesNeedsAnEnabledPlugin(t *testing.T) {
	l, r := choicesLab(t)
	call := getCall(map[string]string{"slug": "a/b"})

	p := choices(t, r, call, "tag")

	must(t, l.lock().SetDisabled("demo", true))

	if err := r.Guard(p); err == nil || err.Code != CodeNotRunnable {
		t.Errorf("Guard after a disable: %v", err)
	}

	err := choicesRefusal(t, r, call, "tag")
	if err.Code != CodeNotRunnable || err.Args[1] != string(StateDisabled) {
		t.Errorf("a disabled plugin: %v", err)
	}

	off := l.reg(func(o *Options) { o.NoPlugins = true })
	if err := choicesRefusal(t, off, call, "tag"); err.Code != CodeNotRunnable {
		t.Errorf("--no-plugins: %v", err)
	}

	if err := choicesRefusal(t, r, Call{Plugin: "ghost", Table: TableCommand, Entry: "get"},
		"tag"); err.Code != CodeNotRunnable {
		t.Errorf("an unknown plugin: %v", err)
	}
}

// TestChoicesCacheDirectory checks the working directory of the run: it is created when missing,
// and a link in its place is refused.
//
// Parameters:
//   - t: test to report to
func TestChoicesCacheDirectory(t *testing.T) {
	l, r := choicesLab(t)
	call := getCall(map[string]string{"slug": "a/b"})

	cache, err := l.paths.PluginCacheDir("demo")
	if err != nil {
		t.Fatal(err)
	}

	if _, serr := os.Stat(cache); serr == nil {
		t.Fatal("the cache directory exists before the first run")
	}

	choices(t, r, call, "tag")
	choices(t, r, call, "tag")

	other := filepath.Join(l.root, "elsewhere")
	must(t, os.MkdirAll(other, 0o700))
	must(t, os.Remove(cache))
	must(t, os.Symlink(other, cache))

	if err := choicesRefusal(t, r, call, "tag"); err.Code != CodeCwdInvalid {
		t.Errorf("a link as cache directory: %v", err)
	}
}

// TestChoicesProgramRules checks that a program that is not installed is reported like a run does.
//
// Parameters:
//   - t: test to report to
func TestChoicesProgramRules(t *testing.T) {
	l, r := choicesLab(t)

	must(t, os.Remove(filepath.Join(l.bin, "tool")))

	err := choicesRefusal(t, r, getCall(map[string]string{"slug": "a/b"}), "tag")
	if err.Code != CodeDepMissing || err.Args[0] != "tool" {
		t.Errorf("a missing program: %v", err)
	}
}

// TestCheckShowsSuggestionsAndStartsNothing checks that check lists the choices_from argv of the
// fields as written, in the JSON of its contributions too, and starts neither it nor the command.
//
// Parameters:
//   - t: test to report to
func TestCheckShowsSuggestionsAndStartsNothing(t *testing.T) {
	l := chkLab(t)
	l.useSystemTools()

	canary := filepath.Join(l.root, "canary")
	l.script("tool", "echo ran > "+canary+"\n")

	body := cmd(run(", \"{a}\"")) + plainField("a") +
		cfField("b", "\"tool\", \"--list\", \"--\", \"{a?}\"", "")
	res := chkDir(t, l, chkPlugin(tool, body, nil), func(q *CheckRequest) {
		q.Set = map[string]string{"a": "1"}
	})

	if res.Failed() {
		t.Fatalf("check failed: %v", res.Findings)
	}

	c := res.Contributions[0]
	if len(c.Suggestions) != 1 || c.Suggestions[0].Field != "b" ||
		!slices.Equal(c.Suggestions[0].Run, []string{"tool", "--list", "--", "{a?}"}) {
		t.Errorf("suggestions = %+v", c.Suggestions)
	}

	if _, err := os.Stat(canary); err == nil {
		t.Error("check started a program")
	}
}
