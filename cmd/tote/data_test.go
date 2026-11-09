package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/shell"
)

// TestDataList checks the table, tsv and json output of the alias, repos and funcs groups.
//
// Parameters:
//   - t: test to report to
func TestDataList(t *testing.T) {
	tests := []struct {
		name string
		args []string
		tty  bool
		want string
	}{
		{"alias tsv", []string{"alias"}, false, "ll\tls -la\tLong list\ngs\tgit status\t\n"},
		{
			"alias table",
			[]string{"a", "list"},
			true,
			"NAME  COMMAND     DESCRIPTION\nll    ls -la      Long list\ngs    git status\n",
		},
		{
			"repos tsv",
			[]string{"repos", "ls"},
			false,
			"proj\t~/code/proj\tMain project\ndocs\t/srv/docs\t\n",
		},
		{
			"repo table",
			[]string{"repo", "--format", "table"},
			false,
			"NAME  PATH         DESCRIPTION\nproj  ~/code/proj  Main project\ndocs  /srv/docs\n",
		},
		{
			"funcs tsv",
			[]string{"funcs"},
			false,
			"hello\tsource\t~/bin/hello.sh\tfalse\t\ntool\texec\t/opt/tool/tool\ttrue\tA tool\n",
		},
		{
			"func table",
			[]string{"f", "--format=table"},
			false,
			"NAME   KIND    SCRIPT          MANAGED  DESCRIPTION\n" +
				"hello  source  ~/bin/hello.sh  false\n" +
				"tool   exec    /opt/tool/tool  true     A tool\n",
		},
	}

	for _, tt := range tests {
		h := newHarness(t)
		h.fixtures()
		h.tty = tt.tty

		if code := h.run(tt.args...); code != 0 || h.out.String() != tt.want {
			t.Errorf("%s: code %d, stdout %q, want %q (stderr %q)",
				tt.name, code, h.out.String(), tt.want, h.errb.String())
		}
	}

	h := newHarness(t)
	h.fixtures()

	h.run("funcs", "--format", "json")

	funcs := decodeList[funcView](t, h.out.Bytes(), "funcs")
	if len(funcs) != 2 || funcs[1].Kind != "exec" || !funcs[1].Managed {
		t.Errorf("funcs json: %+v", funcs)
	}

	h.run("alias", "-l", "--format", "json")

	aliases := decodeList[aliasView](t, h.out.Bytes(), "aliases")
	if len(aliases) != 2 || aliases[0].Cmd != "ls -la" {
		t.Errorf("alias json: %+v", aliases)
	}

	h.run("repos", "--format", "json")

	repos := decodeList[repoView](t, h.out.Bytes(), "repos")
	if len(repos) != 2 || repos[1].Path != "/srv/docs" {
		t.Errorf("repos json: %+v", repos)
	}
}

// TestDataAdd checks the golden file and the message of the add verb of each group.
//
// Parameters:
//   - t: test to report to
func TestDataAdd(t *testing.T) {
	tests := []struct {
		name string
		args []string
		file string
		base string
		add  string
		out  string
	}{
		{
			"alias",
			[]string{"alias", "add", "up", "--desc", "Refresh", "apt", "update"},
			"aliases.toml", fixtureAliases,
			"\n[[alias]]\nname = \"up\"\ncmd = \"apt update\"\ndesc = \"Refresh\"\n",
			"added alias \"up\"\n",
		},
		{
			"alias with dashes in the command",
			[]string{"alias", "add", "dfh", "df", "-h", "--desc"},
			"aliases.toml", fixtureAliases,
			"\n[[alias]]\nname = \"dfh\"\ncmd = \"df -h --desc\"\n", "added alias \"dfh\"\n",
		},
		{
			"alias after double dash",
			[]string{"a", "add", "la", "--", "ls", "-A"},
			"aliases.toml", fixtureAliases,
			"\n[[alias]]\nname = \"la\"\ncmd = \"ls -A\"\n", "added alias \"la\"\n",
		},
		{
			"repo",
			[]string{"repos", "add", "tmp", "/tmp/x", "--desc", "Scratch"},
			"repos.toml", fixtureRepos,
			"\n[[repo]]\nname = \"tmp\"\npath = \"/tmp/x\"\ndesc = \"Scratch\"\n",
			"added repository \"tmp\"\n",
		},
		{
			"func default kind",
			[]string{"funcs", "add", "go2", "--script", "~/s/go2.sh"},
			"funcs.toml", fixtureFuncs,
			"\n[[func]]\nname = \"go2\"\nkind = \"source\"\nscript = \"~/s/go2.sh\"\nmanaged = false\n",
			"added function \"go2\"\n",
		},
		{
			"func exec managed",
			[]string{
				"func", "add", "run2", "--script", "/b/run2",
				"--kind", "exec", "--managed", "--desc", "Runner",
			},
			"funcs.toml", fixtureFuncs,
			"\n[[func]]\nname = \"run2\"\ndesc = \"Runner\"\nkind = \"exec\"\n" +
				"script = \"/b/run2\"\nmanaged = true\n",
			"added function \"run2\"\n",
		},
	}

	for _, tt := range tests {
		h := newHarness(t)
		h.fixtures()

		if code := h.run(tt.args...); code != 0 || h.out.String() != tt.out {
			t.Errorf("%s: code %d, stdout %q, stderr %q", tt.name, code, h.out.String(), h.errb.String())
		}

		if got := h.read(tt.file); got != tt.base+tt.add {
			t.Errorf("%s: file =\n%s\nwant\n%s", tt.name, got, tt.base+tt.add)
		}
	}
}

// TestReposAddUnderRoot checks that a relative path of "tote repos add NAME rel/path" is looked
// up under repos_root, only when it starts with neither "/", "~" nor "." and the folder exists
// there, and that every other case keeps the contract of the command: the path as typed, and a
// relative path refused.
//
// Parameters:
//   - t: test to report to
func TestReposAddUnderRoot(t *testing.T) {
	tests := []struct {
		name   string
		root   string
		folder string
		path   string
		want   string
		fail   bool
	}{
		{"exists under root", "ROOT", "work/app", "work/app", "ROOT/work/app", false},
		{"one level", "ROOT", "app", "app", "ROOT/app", false},
		{"root with a tilde", "~/src", "app", "app", "~/src/app", false},
		{"missing folder", "ROOT", "other", "app", "", true},
		{"no root", "", "app", "app", "", true},
		{"relative root", "src", "app", "app", "", true},
		{"dot path", "ROOT", "app", "./app", "", true},
		{"dot dot path", "ROOT", "app", "../app", "", true},
		{"home path", "ROOT", "app", "~/app", "~/app", false},
		{"absolute path", "ROOT", "app", "/tmp/app", "/tmp/app", false},
		{"a file is not a folder", "ROOT", "", "app", "", true},
	}

	for _, tt := range tests {
		h := newHarness(t)
		h.fixtures()

		root := strings.Replace(tt.root, "ROOT", filepath.Join(h.dir, "root"), 1)
		real := root

		if strings.HasPrefix(root, "~") {
			real = filepath.Join(h.app.Home, root[1:])
		}

		if tt.folder != "" {
			if err := os.MkdirAll(filepath.Join(real, tt.folder), 0o700); err != nil {
				t.Fatal(err)
			}
		} else if err := os.MkdirAll(real, 0o700); err == nil {
			_ = os.WriteFile(filepath.Join(real, "app"), nil, 0o600)
		}

		if root != "" {
			h.write("config.toml", "schema = 1\nrepos_root = \""+root+"\"\n")
		}

		code := h.run("repos", "add", "new", tt.path)

		if tt.fail {
			if code == 0 || strings.Contains(h.read("repos.toml"), "name = \"new\"") {
				t.Errorf("%s: code %d, the path was accepted: %s", tt.name, code, h.read("repos.toml"))
			}

			continue
		}

		want := strings.Replace(tt.want, "ROOT", filepath.Join(h.dir, "root"), 1)
		if code != 0 || !strings.Contains(h.read("repos.toml"), "path = \""+want+"\"") {
			t.Errorf("%s: code %d, stderr %q, file\n%s\nwant path %q", tt.name, code,
				h.errb.String(), h.read("repos.toml"), want)
		}
	}
}

// TestDataAddRefusals checks the validation messages, which must name the field.
//
// Parameters:
//   - t: test to report to
func TestDataAddRefusals(t *testing.T) {
	tests := []struct {
		args []string
		file string
		code int
		msg  string
	}{
		{[]string{"alias", "add", "ll", "x"}, "aliases.toml", 1, `alias "ll" already exists`},
		{[]string{"alias", "add", "a=b", "x"}, "aliases.toml", 1, "name: must not contain '='"},
		{[]string{"alias", "add", "tote", "x"}, "aliases.toml", 1, `"tote" is a reserved name`},
		{[]string{"alias", "add", "ok"}, "aliases.toml", 2, "missing argument: command"},
		{
			[]string{"alias", "add", "ok", "--desc", "a\nb", "x"},
			"aliases.toml", 1,
			"description: must not contain",
		},
		{[]string{"repos", "add", "proj", "/x"}, "repos.toml", 1, `repository "proj" already exists`},
		{[]string{"repos", "add", "r", "a\nb"}, "repos.toml", 1, "path: "},
		{[]string{"repos", "add", "r"}, "repos.toml", 2, "missing argument: path"},
		{[]string{"repos", "add", "r", "p", "q"}, "repos.toml", 2, "too many arguments"},
		{[]string{"funcs", "add", "f1"}, "funcs.toml", 2, "missing argument: --script"},
		{
			[]string{"funcs", "add", "f1", "--script", "/s", "--kind", "bad"},
			"funcs.toml", 1,
			"kind: must be",
		},
		{[]string{"funcs", "add", "1f", "--script", "/s"}, "funcs.toml", 1, "name: must start with"},
		{
			[]string{"funcs", "add", "hello", "--script", "/s"},
			"funcs.toml", 1,
			`function "hello" already exists`,
		},
	}

	for _, tt := range tests {
		h := newHarness(t)
		h.fixtures()
		before := h.read(tt.file)

		code := h.run(tt.args...)
		if code != tt.code || !strings.Contains(h.errb.String(), tt.msg) || h.out.Len() != 0 {
			t.Errorf("%v: code %d, stdout %q, stderr %q, want %q",
				tt.args, code, h.out.String(), h.errb.String(), tt.msg)
		}

		if h.read(tt.file) != before {
			t.Errorf("%v: the file changed", tt.args)
		}
	}
}

// TestDataRm checks the golden file after a removal in each group.
//
// Parameters:
//   - t: test to report to
func TestDataRm(t *testing.T) {
	tests := []struct {
		args []string
		file string
		want string
		out  string
	}{
		{
			[]string{"alias", "rm", "ll", "--yes"},
			"aliases.toml",
			"schema = 1\n\n\n[[alias]]\nname = \"gs\"\ncmd = \"git status\"\n",
			"removed alias \"ll\"\n",
		},
		{
			[]string{"repos", "rm", "docs", "-y"},
			"repos.toml",
			"schema = 1\n\n[[repo]]\nname = \"proj\"\npath = \"~/code/proj\"\ndesc = \"Main project\"\n\n",
			"removed repository \"docs\"\n",
		},
		{
			[]string{"funcs", "rm", "hello", "--yes"},
			"funcs.toml",
			"schema = 1\n\n\n[[func]]\nname = \"tool\"\ndesc = \"A tool\"\nkind = \"exec\"\n" +
				"script = \"/opt/tool/tool\"\nmanaged = true\n",
			"removed function \"hello\"\n",
		},
	}

	for _, tt := range tests {
		h := newHarness(t)
		h.fixtures()

		if code := h.run(tt.args...); code != 0 || h.out.String() != tt.out {
			t.Errorf("%v: code %d, stdout %q, stderr %q", tt.args, code, h.out.String(), h.errb.String())
		}

		if got := h.read(tt.file); got != tt.want {
			t.Errorf("%v: file =\n%s\nwant\n%s", tt.args, got, tt.want)
		}
	}

	h := newHarness(t)
	h.fixtures()

	if code := h.run("funcs", "rm", "helo", "--yes"); code != 1 ||
		!strings.Contains(h.errb.String(), `did you mean "hello"?`) {
		t.Errorf("missing func: code %d, stderr %q", code, h.errb.String())
	}
}

// TestGroupSubcommands checks the spellings of the sub-verbs and the unknown sub-verb.
//
// Parameters:
//   - t: test to report to
func TestGroupSubcommands(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	for _, args := range [][]string{
		{"aliases", "ls"}, {"alias", "list"}, {"alias", "-l"}, {"alias"},
	} {
		if code := h.run(args...); code != 0 || !strings.HasPrefix(h.out.String(), "ll\t") {
			t.Errorf("%v: code %d, stdout %q", args, code, h.out.String())
		}
	}

	for _, verb := range []string{"alias", "repos", "funcs", "co"} {
		if code := h.run(verb, "lst"); verb != "co" && (code != 2 || h.out.Len() != 0 ||
			!strings.Contains(h.errb.String(), `unknown subcommand "lst"`) ||
			!strings.Contains(h.errb.String(), `did you mean "list"?`)) {
			t.Errorf("%s lst: code %d, stderr %q", verb, code, h.errb.String())
		}
	}

	if code := h.run("alias", "remove", "ll", "--yes"); code != 0 {
		t.Errorf("remove spelling: code %d", code)
	}

	if code := h.run("alias", "list", "extra"); code != 2 {
		t.Errorf("list extra: code %d, want 2", code)
	}

	if code := h.run("alias", "rm", "-h"); code != 0 ||
		!strings.Contains(h.out.String(), "tote alias rm") {
		t.Errorf("rm -h: code %d, stdout %q", code, h.out.String())
	}
}

// TestRepoOpenPrints checks that repos open prints the path when no hand-off applies.
//
// Parameters:
//   - t: test to report to
func TestRepoOpenPrints(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	if code := h.run("repos", "open", "proj"); code != 0 ||
		h.out.String() != filepath.Join(h.dir, "home", "code", "proj")+"\n" {
		t.Errorf("code %d, stdout %q", code, h.out.String())
	}

	if code := h.run("r", "open", "docs"); code != 0 || h.out.String() != "/srv/docs\n" {
		t.Errorf("absolute: code %d, stdout %q", code, h.out.String())
	}

	h.write("repos.toml", "schema = 1\n\n[[repo]]\nname = \"here\"\npath = \""+h.dir+"\"\n")

	file := filepath.Join(h.dir, "handoff")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_HANDOFF"] = file

	if code := h.run("repos", "open", "here"); code != 0 || h.out.Len() != 0 {
		t.Errorf("with the wrapper: code %d, stdout %q", code, h.out.String())
	}

	if got, err := shell.ReadHandoff(file); err != nil || got.Action != shell.ChangeDir {
		t.Errorf("default settings must hand off a cd: %+v, %v", got, err)
	}

	h.fixtures()

	if code := h.run("repos", "open", "prj"); code != 1 ||
		!strings.Contains(h.errb.String(), `did you mean "proj"?`) {
		t.Errorf("missing: code %d, stderr %q", code, h.errb.String())
	}

	if code := h.run("repos", "open"); code != 2 {
		t.Errorf("no name: code %d", code)
	}
}

// TestRepoOpenHandoff checks the hand-off of a cd, with the home directory expanded.
//
// Parameters:
//   - t: test to report to
func TestRepoOpenHandoff(t *testing.T) {
	h := newHarness(t)
	h.fixtures()
	h.write("config.toml", "schema = 1\n\n[repos]\nafter_open = \"cd\"\n")

	file := filepath.Join(h.dir, "handoff")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_HANDOFF"] = file

	if err := os.MkdirAll(filepath.Join(h.dir, "home", "code", "proj"), 0o700); err != nil {
		t.Fatal(err)
	}

	if code := h.run("repos", "open", "proj"); code != 0 || h.out.Len() != 0 {
		t.Fatalf("code %d, stdout %q, stderr %q", code, h.out.String(), h.errb.String())
	}

	got, err := shell.ReadHandoff(file)
	if err != nil {
		t.Fatal(err)
	}

	proj := filepath.Join(h.dir, "home", "code", "proj")
	want := shell.Handoff{Action: shell.ChangeDir, Payload: proj}
	if got != want {
		t.Errorf("hand-off = %+v, want %+v", got, want)
	}

	h.vars["TOTE_HANDOFF"] = filepath.Join(h.dir, "missing")

	if code := h.run("repos", "open", "proj"); code != 0 || h.out.String() != proj+"\n" {
		t.Errorf("missing hand-off file: code %d, stdout %q", code, h.out.String())
	}

	h.vars["TOTE_HANDOFF"] = ""

	if code := h.run("repos", "open", "docs"); code != 0 || h.out.String() != "/srv/docs\n" {
		t.Errorf("no wrapper: code %d, stdout %q", code, h.out.String())
	}
}

// TestConfigPath checks "config path" and the usage errors of config.
//
// Parameters:
//   - t: test to report to
func TestConfigPath(t *testing.T) {
	h := newHarness(t)

	if code := h.run("config", "path"); code != 0 || h.out.String() != h.vars["TOTE_CONFIG_DIR"]+"\n" {
		t.Errorf("code %d, stdout %q", code, h.out.String())
	}

	if code := h.run("config"); code != 2 {
		t.Errorf("config alone: code %d, want 2", code)
	}

	if code := h.run("config", "get", "lang"); code != 2 {
		t.Errorf("config get: code %d, want 2", code)
	}

	if code := h.run("config", "-h"); code != 0 ||
		!strings.Contains(h.out.String(), "tote config path") {
		t.Errorf("config -h: code %d, stdout %q", code, h.out.String())
	}

	h.vars["TOTE_CONFIG_DIR"] = "relative"

	if code := h.run("config", "path"); code != 1 {
		t.Errorf("relative override: code %d, want 1", code)
	}
}

// TestAddNameClashes checks that an add verb refuses a name that the shell would drop silently:
// a reserved word, a name held by another kind, a relative repository path and a relative
// script, and that the file is left alone.
//
// Parameters:
//   - t: test to report to
func TestAddNameClashes(t *testing.T) {
	tests := []struct {
		args []string
		file string
		msg  string
	}{
		{[]string{"alias", "add", "tote", "x"}, "aliases.toml", `"tote" is a reserved name`},
		{[]string{"alias", "add", "complete", "x"}, "aliases.toml", `"complete" is a reserved name`},
		{[]string{"alias", "add", "if", "x"}, "aliases.toml", `"if" is a reserved name`},
		{[]string{"alias", "add", "proj", "x"}, "aliases.toml", `already used by repository "proj"`},
		{[]string{"alias", "add", "hello", "x"}, "aliases.toml", `already used by function "hello"`},
		{[]string{"repos", "add", "ll", "/tmp"}, "repos.toml", `already used by alias "ll"`},
		{[]string{"repos", "add", "set", "/tmp"}, "repos.toml", `"set" is a reserved name`},
		{[]string{"alias", "add", "edit", "x"}, "aliases.toml", `"edit" is a reserved name`},
		{[]string{"alias", "add", "Status", "x"}, "aliases.toml", `"Status" is a reserved name`},
		{[]string{"alias", "add", "__tote_pick", "x"}, "aliases.toml", `is a reserved name`},
		{[]string{"alias", "add", "_toteX", "x"}, "aliases.toml", `is a reserved name`},
		{[]string{"repos", "add", "sync", "/tmp"}, "repos.toml", `"sync" is a reserved name`},
		{[]string{"repos", "add", "_tote_x", "/tmp"}, "repos.toml", `is a reserved name`},
		{
			[]string{"funcs", "add", "__tote_pick", "--script", "/s/x"},
			"funcs.toml",
			`is a reserved name`,
		},
		{[]string{"repos", "add", "rel", "relative/path"}, "repos.toml", "must be absolute"},
		{[]string{"funcs", "add", "gs", "--script", "/s"}, "funcs.toml", `used by alias "gs"`},
		{[]string{"funcs", "add", "bad", "--script", "rel.sh"}, "funcs.toml", "absolute path"},
		{[]string{"funcs", "add", "done", "--script", "/s"}, "funcs.toml", "reserved name"},
	}

	for _, tt := range tests {
		h := newHarness(t)
		h.fixtures()
		before := h.read(tt.file)

		code := h.run(tt.args...)
		if code == 0 || !strings.Contains(h.errb.String(), tt.msg) {
			t.Errorf("%v: code %d, stderr %q, want %q", tt.args, code, h.errb.String(), tt.msg)
		}

		if h.read(tt.file) != before {
			t.Errorf("%v: the file changed", tt.args)
		}
	}
}

// TestAddAcceptsHomeAndAbsolutePaths checks the two forms of path that stay valid.
//
// Parameters:
//   - t: test to report to
func TestAddAcceptsHomeAndAbsolutePaths(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	for _, args := range [][]string{
		{"repos", "add", "home", "~/x"},
		{"repos", "add", "abs", "/x"},
		{"funcs", "add", "tilde", "--script", "~/s.sh"},
		{"funcs", "add", "exe", "--script", "run", "--kind", "exec"},
	} {
		if code := h.run(args...); code != 0 {
			t.Errorf("%v: code %d, stderr %q", args, code, h.errb.String())
		}
	}
}

// TestUsageErrorValueIsSanitized checks that an escape sequence typed as an option value never
// reaches the terminal through the error message.
//
// Parameters:
//   - t: test to report to
func TestUsageErrorValueIsSanitized(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	if code := h.run("co", "list", "--format", "\x1b[31mX\u009b"); code != 2 {
		t.Errorf("code %d, want 2", code)
	}

	if strings.ContainsAny(h.errb.String(), "\x1b\u009b") {
		t.Errorf("raw control characters in stderr %q", h.errb.String())
	}
}

// TestJSONEscapesTerminalRunes checks that the JSON output writes the runes a terminal acts on
// as escapes, including a character outside the basic plane as a surrogate pair, and that the
// result still decodes to the original text.
//
// Parameters:
//   - t: test to report to
func TestJSONEscapesTerminalRunes(t *testing.T) {
	h := newHarness(t)
	h.write("aliases.toml", "schema = 1\n\n[[alias]]\nname = \"ok\"\ncmd = \"ls\"\n"+
		"desc = \"a\\u009bb\\U000E0041c\\u202ed\"\n")

	if code := h.run("alias", "--format", "json"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	out := h.out.String()
	if !strings.Contains(out, `a\u009bb\udb40\udc41c\u202ed`) {
		t.Errorf("stdout = %q", out)
	}

	got := decodeList[aliasView](t, h.out.Bytes(), "aliases")
	if len(got) != 1 || got[0].Desc != "a\u009bb\U000E0041c\u202ed" {
		t.Errorf("decoded %+v", got)
	}
}

// TestRepoOpenMissingDirectory checks that a directory that does not exist is reported and the
// hand-off file stays empty, while the print mode does not look at the disk.
//
// Parameters:
//   - t: test to report to
func TestRepoOpenMissingDirectory(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	file := filepath.Join(h.dir, "handoff")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_HANDOFF"] = file

	if code := h.run("repos", "open", "docs"); code != 1 ||
		!strings.Contains(h.errb.String(), "directory not found: /srv/docs") {
		t.Errorf("code %d, stderr %q", code, h.errb.String())
	}

	if data, _ := os.ReadFile(file); len(data) != 0 {
		t.Errorf("the hand-off file was written: %q", data)
	}
}

// TestBrokenConfigWarnsOnce checks that an unusable config.toml is mentioned a single time by a
// verb that reads a data file.
//
// Parameters:
//   - t: test to report to
func TestBrokenConfigWarnsOnce(t *testing.T) {
	h := newHarness(t)
	h.fixtures()
	h.write("config.toml", "lang = \n")

	if code := h.run("alias"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if n := strings.Count(h.errb.String(), "config.toml ignored"); n != 1 {
		t.Errorf("%d warnings in stderr %q", n, h.errb.String())
	}
}
