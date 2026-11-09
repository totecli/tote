package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// TestResolveConn checks the order of the lookup: name, alias, then prefix.
//
// Parameters:
//   - t: test to report to
func TestResolveConn(t *testing.T) {
	conns := []store.Connection{
		{Name: "web1", Aliases: []string{"w"}},
		{Name: "web2", Aliases: []string{"wb"}},
		{Name: "db", Aliases: []string{"web"}},
		{Name: "Lab"},
	}

	tests := []struct {
		handle string
		min    int
		want   []string
	}{
		{"web1", 2, []string{"web1"}},
		{"web", 2, []string{"db"}},
		{"w", 2, []string{"web1"}},
		{"we", 2, []string{"web1", "web2", "db"}},
		{"wb", 2, []string{"web2"}},
		{"lab", 2, []string{"Lab"}},
		{"la", 2, []string{"Lab"}},
		{"l", 2, nil},
		{"l", 1, []string{"Lab"}},
		{"", 1, nil},
		{"zz", 2, nil},
	}

	for _, tt := range tests {
		var got []string
		for _, c := range resolveConn(conns, tt.handle, tt.min) {
			got = append(got, c.Name)
		}

		if !slices.Equal(got, tt.want) {
			t.Errorf("resolveConn(%q, %d) = %v, want %v", tt.handle, tt.min, got, tt.want)
		}
	}
}

// TestConnList checks the three formats of "co -l" and the choice of the default one.
//
// Parameters:
//   - t: test to report to
func TestConnList(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	h.run("co", "-l")

	wantTSV := "web1\t\t10.0.0.1\tdeploy\tw,web\t\tWeb front\n" +
		"db1\tmosh\tdb1.example.org\t\tdb\t\t\n" +
		"v6\t\t[2001:db8::1]\t\t\t\t\n"
	if h.out.String() != wantTSV {
		t.Errorf("piped list = %q, want %q", h.out.String(), wantTSV)
	}

	h.tty = true
	h.run("co", "--list")

	wantTable := "NAME  VIA   TARGET           USER    ALIASES  IP  DESCRIPTION\n" +
		"web1        10.0.0.1         deploy  w,web        Web front\n" +
		"db1   mosh  db1.example.org          db\n" +
		"v6          [2001:db8::1]\n"
	if h.out.String() != wantTable {
		t.Errorf("terminal list = %q, want %q", h.out.String(), wantTable)
	}

	h.tty = false
	h.run("co", "-l", "--format", "table")

	if h.out.String() != wantTable {
		t.Errorf("forced table = %q, want %q", h.out.String(), wantTable)
	}

	h.run("co", "list", "--format=json")

	got := decodeList[connView](t, h.out.Bytes(), "connections")

	if len(got) != 3 || got[0].Name != "web1" || !slices.Equal(got[0].Aliases, []string{"w", "web"}) ||
		got[2].Target != "[2001:db8::1]" || got[2].Aliases == nil {
		t.Errorf("json records = %+v", got)
	}

	if code := h.run("co", "-l", "--format", "xml"); code != 2 || h.out.Len() != 0 {
		t.Errorf("bad format: code %d, stdout %q", code, h.out.String())
	}
}

// TestConnListEmpty checks the three formats on a missing file.
//
// Parameters:
//   - t: test to report to
func TestConnListEmpty(t *testing.T) {
	h := newHarness(t)

	if code := h.run("co", "-l", "--format", "json"); code != 0 ||
		h.out.String() != "{\n  \"schema\": 1,\n  \"connections\": []\n}\n" {
		t.Errorf("json: code %d, stdout %q", code, h.out.String())
	}

	if code := h.run("co", "-l"); code != 0 || h.out.Len() != 0 {
		t.Errorf("tsv: code %d, stdout %q", code, h.out.String())
	}

	if code := h.run("co"); code != 0 || h.out.Len() != 0 {
		t.Errorf("co alone: code %d, stdout %q", code, h.out.String())
	}
}

// TestConnect checks the command line that each way of naming a connection execs.
//
// Parameters:
//   - t: test to report to
func TestConnect(t *testing.T) {
	sshWeb := []string{"ssh", "-l", "deploy", "--", "10.0.0.1"}
	tests := []struct {
		name string
		args []string
		want []string
		echo string
	}{
		{
			"exact name",
			[]string{"co", "web1"},
			[]string{"ssh", "-l", "deploy", "--", "10.0.0.1"},
			"  -> ssh -l deploy -- 10.0.0.1",
		},
		{"alias", []string{"co", "w"}, []string{"ssh", "-l", "deploy", "--", "10.0.0.1"}, ""},
		{"long spelling", []string{"connections", "web"}, sshWeb, ""},
		{"unique prefix", []string{"conn", "db"}, []string{"mosh", "db1.example.org"}, ""},
		{"prefix of name", []string{"co", "v"}, nil, ""},
		{"ipv6", []string{"co", "v6"}, []string{"ssh", "--", "2001:db8::1"}, "  -> ssh -- 2001:db8::1"},
		{
			"user override",
			[]string{"co", "web1", "root"},
			[]string{"ssh", "-l", "root", "--", "10.0.0.1"},
			"",
		},
		{"user on custom via", []string{"co", "db1", "bob"}, []string{"mosh", "bob@db1.example.org"}, ""},
		{"after double dash", []string{"co", "--", "web1"}, sshWeb, ""},
	}

	for _, tt := range tests {
		h := newHarness(t)
		h.fixtures()

		code := h.run(tt.args...)

		if tt.want == nil {
			if code == 0 {
				t.Errorf("%s: code 0, want failure", tt.name)
			}

			continue
		}

		if code != 0 || len(h.execs) != 1 || !slices.Equal(h.execs[0], tt.want) {
			t.Errorf("%s: code %d, execs %v, want %v (stderr %q)",
				tt.name, code, h.execs, tt.want, h.errb.String())
		}

		if tt.echo != "" && !strings.Contains(h.errb.String(), tt.echo+"\n") {
			t.Errorf("%s: stderr = %q, want %q", tt.name, h.errb.String(), tt.echo)
		}

		if h.out.Len() != 0 {
			t.Errorf("%s: stdout = %q, want empty", tt.name, h.out.String())
		}
	}
}

// TestConnectFailures checks the exit code and the message of every way a connection fails.
//
// Parameters:
//   - t: test to report to
func TestConnectFailures(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	if code := h.run("co", "d"); code != 1 || len(h.execs) != 0 {
		t.Errorf("prefix below prefix_min: code %d, stderr %q", code, h.errb.String())
	}

	h.write("connections.toml", fixtureConns+"\n[[connection]]\nname = \"web9\"\ntarget = \"h9\"\n")

	if code := h.run("co", "we"); code != 2 ||
		!strings.Contains(h.errb.String(), `"we" is ambiguous: web1, web9`) {
		t.Errorf("ambiguous: code %d, stderr %q", code, h.errb.String())
	}

	if code := h.run("co", "web11"); code != 1 || len(h.execs) != 0 ||
		!strings.Contains(h.errb.String(), `no connection matches "web11"`) ||
		!strings.Contains(h.errb.String(), `did you mean "web1"?`) {
		t.Errorf("not found: code %d, stderr %q", code, h.errb.String())
	}

	if code := h.run("co", "zzzzzz"); code != 1 || strings.Contains(h.errb.String(), "did you mean") {
		t.Errorf("not found, far: code %d, stderr %q", code, h.errb.String())
	}

	if code := h.run("co", "db1", "-evil"); code == 0 || len(h.execs) != 0 {
		t.Errorf("hostile user: code %d, execs %v", code, h.execs)
	}

	if code := h.run("co", "web1", "bad user"); code != 1 || len(h.execs) != 0 {
		t.Errorf("invalid user: code %d, execs %v", code, h.execs)
	}

	if code := h.run("co", "a", "b", "c"); code != 2 {
		t.Errorf("too many arguments: code %d", code)
	}

	h.execErr = errNoProgram
	if code := h.run("co", "web1"); code != 127 ||
		!strings.Contains(h.errb.String(), `cannot run "ssh"`) {
		t.Errorf("exec failure: code %d, stderr %q", code, h.errb.String())
	}
}

// TestConnectNeedsReview checks that a connection flagged by the importer is refused.
//
// Parameters:
//   - t: test to report to
func TestConnectNeedsReview(t *testing.T) {
	h := newHarness(t)
	h.write("connections.toml",
		"schema = 1\n\n[[connection]]\nname = \"odd\"\ntarget = \"h\"\nneeds_review = true\n")

	if code := h.run("co", "odd"); code != 1 || len(h.execs) != 0 ||
		!strings.Contains(h.errb.String(), `connection "odd" needs review`) {
		t.Errorf("code %d, execs %v, stderr %q", code, h.execs, h.errb.String())
	}
}

// TestConnectKeepalive checks that "tote co NAME" asks ssh for the keepalive options of a plain
// ssh connection, adds them when the user's configuration has none, shows the real command and
// runs it, and that the setting, a failed lookup and another program leave the command alone.
//
// Parameters:
//   - t: test to report to
func TestConnectKeepalive(t *testing.T) {
	plain := []string{"ssh", "-l", "deploy", "--", "10.0.0.1"}
	both := []string{
		"ssh", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3",
		"-l", "deploy", "--", "10.0.0.1",
	}
	none := "serveraliveinterval 0\nserveralivecountmax 3\n"

	tests := []struct {
		name   string
		config string
		args   []string
		out    string
		err    error
		want   []string
		asked  bool
	}{
		{"none configured", "", []string{"co", "web1"}, none, nil, both, true},
		{
			"configured by the user", "",
			[]string{"co", "web1"},
			"serveraliveinterval 60\n", nil, plain, true,
		},
		{"lookup failed", "", []string{"co", "web1"}, none, errors.New("exit 255"), plain, true},
		{
			"user override", "",
			[]string{"co", "web1", "root"},
			none, nil,
			[]string{
				"ssh", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3", "-l",
				"root", "--", "10.0.0.1",
			},
			true,
		},
		{
			"setting off", "[connect]\nkeepalive = false\n",
			[]string{"co", "web1"},
			none, nil,
			plain, false,
		},
		{
			"not ssh", "",
			[]string{"co", "db1"},
			none, nil,
			[]string{"mosh", "db1.example.org"},
			false,
		},
	}

	for _, tt := range tests {
		h := newHarness(t)
		h.fixtures()
		h.write("config.toml", "schema = 1\n\n"+tt.config)
		h.probeOut, h.probeErr = tt.out, tt.err

		if code := h.run(tt.args...); code != 0 {
			t.Fatalf("%s: code %d, stderr %q", tt.name, code, h.errb.String())
		}

		if len(h.execs) != 1 || !slices.Equal(h.execs[0], tt.want) {
			t.Errorf("%s: execs %v, want %v", tt.name, h.execs, tt.want)
		}

		if (len(h.probes) == 1) != tt.asked || len(h.probes) > 1 {
			t.Errorf("%s: lookups %v, want one: %v", tt.name, h.probes, tt.asked)
		}

		want := "  -> " + strings.Join(tt.want, " ") + "\n"
		if !strings.Contains(h.errb.String(), want) {
			t.Errorf("%s: stderr %q does not show %q", tt.name, h.errb.String(), want)
		}
	}
}

// TestConnectPrefixMin checks that connect.prefix_min from config.toml is honoured.
//
// Parameters:
//   - t: test to report to
func TestConnectPrefixMin(t *testing.T) {
	h := newHarness(t)
	h.fixtures()
	h.write("config.toml", "schema = 1\n\n[connect]\nprefix_min = 4\n")

	if code := h.run("co", "db"); code != 0 {
		t.Fatalf("db is an alias: code %d", code)
	}

	if code := h.run("co", "web"); code != 0 {
		t.Fatalf("web is an alias: code %d", code)
	}

	if code := h.run("co", "dummy"); code != 1 {
		t.Fatalf("no match: code %d", code)
	}

	h.write("connections.toml", fixtureConns+"\n[[connection]]\nname = \"database\"\ntarget = \"d\"\n")

	if code := h.run("co", "dat"); code != 1 {
		t.Errorf("prefix shorter than 4 must not connect: code %d", code)
	}

	if code := h.run("co", "data"); code != 0 || len(h.execs) != 1 {
		t.Errorf("prefix of 4 must connect: code %d, execs %v", code, h.execs)
	}
}

// TestConnAdd checks "co add": the golden file, the message and the refusals.
//
// Parameters:
//   - t: test to report to
func TestConnAdd(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	code := h.run("co", "add", "lab", "--target", "lab.example.org", "--via", "mosh",
		"--user", "root", "--alias", "l, lb", "--ip", "192.0.2.7", "--desc", "Test lab")
	if code != 0 || h.out.String() != "added connection \"lab\"\n" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, h.out.String(), h.errb.String())
	}

	want := fixtureConns + `
[[connection]]
name = "lab"
via = "mosh"
target = "lab.example.org"
user = "root"
aliases = ["l", "lb"]
ip = "192.0.2.7"
desc = "Test lab"
`
	if got := h.read("connections.toml"); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}

	before := h.read("connections.toml")
	tests := []struct {
		name string
		args []string
		code int
		msg  string
	}{
		{"duplicate", []string{"web1", "--target", "x"}, 1, `connection "web1" already exists`},
		{"bad target", []string{"zed", "--target", "-oProxy"}, 1, "target: must not start with -"},
		{"missing target", []string{"zed"}, 1, "target: must not be empty"},
		{"bad user", []string{"zed", "--target", "h", "--user", "a b"}, 1, "user: must start with"},
		{"bad name", []string{"a/b", "--target", "h"}, 1, "name: must start with"},
		{"bad alias", []string{"zed", "--target", "h", "--alias", "x y"}, 1, "aliases: must start with"},
		{
			"bad desc",
			[]string{"zed", "--target", "h", "--desc", "a\tb"},
			1,
			"description: must not contain control",
		},
		{"no name", []string{"--target", "h"}, 2, "missing argument: name"},
		{"two names", []string{"a", "b", "--target", "h"}, 2, "too many arguments"},
		{"unknown option", []string{"zed", "--tarbet", "h"}, 2, `unknown option "--tarbet"`},
		{"option without value", []string{"zed", "--target"}, 2, `option "--target" needs a value`},
	}

	for _, tt := range tests {
		code := h.run(append([]string{"co", "add"}, tt.args...)...)
		if code != tt.code || !strings.Contains(h.errb.String(), tt.msg) || h.out.Len() != 0 {
			t.Errorf("%s: code %d, stdout %q, stderr %q", tt.name, code, h.out.String(), h.errb.String())
		}

		if h.read("connections.toml") != before {
			t.Errorf("%s: the file changed", tt.name)
		}
	}
}

// TestConnAddCreatesFile checks that add creates the file and its directory when missing.
//
// Parameters:
//   - t: test to report to
func TestConnAddCreatesFile(t *testing.T) {
	h := newHarness(t)

	if code := h.run("connexions", "add", "n1", "--target", "h1"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	want := "schema = 1\n\n[[connection]]\nname = \"n1\"\ntarget = \"h1\"\n"
	if got := h.read("connections.toml"); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// TestConnRm checks "co rm": --yes, the confirmation and the refusal without a terminal.
//
// Parameters:
//   - t: test to report to
func TestConnRm(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	if code := h.run("co", "rm", "db1"); code != 2 ||
		!strings.Contains(h.errb.String(), "refusing to remove without --yes (no terminal)") {
		t.Errorf("no terminal: code %d, stderr %q", code, h.errb.String())
	}

	if h.read("connections.toml") != fixtureConns {
		t.Fatal("the file changed without confirmation")
	}

	h.ttyIn, h.stdin = true, "n\n"

	if code := h.run("co", "rm", "db1"); code != 1 ||
		!strings.Contains(h.errb.String(), "confirm removal of connection \"db1\"? [y/N]") ||
		h.read("connections.toml") != fixtureConns {
		t.Errorf("answer n: code %d, stderr %q", code, h.errb.String())
	}

	h.stdin = "y\n"

	if code := h.run("co", "rm", "db1"); code != 0 ||
		h.out.String() != "removed connection \"db1\"\n" {
		t.Errorf("answer y: code %d, stdout %q, stderr %q", code, h.out.String(), h.errb.String())
	}

	want := strings.Replace(fixtureConns, `[[connection]]
name = "db1"
via = "mosh"
target = "db1.example.org"
aliases = ["db"]
`, "", 1)
	if got := h.read("connections.toml"); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}

	h.ttyIn = false

	if code := h.run("co", "rm", "v7", "-y"); code != 1 ||
		!strings.Contains(h.errb.String(), `connection "v7" not found`) ||
		!strings.Contains(h.errb.String(), `did you mean "v6"?`) {
		t.Errorf("missing: code %d, stderr %q", code, h.errb.String())
	}

	if code := h.run("co", "rm", "v6", "--yes"); code != 0 {
		t.Errorf("--yes: code %d, stderr %q", code, h.errb.String())
	}

	if code := h.run("co", "rm"); code != 2 {
		t.Errorf("no name: code %d", code)
	}
}

// TestBrokenFile checks that a syntax error blocks every verb on that file and leaves it alone.
//
// Parameters:
//   - t: test to report to
func TestBrokenFile(t *testing.T) {
	broken := "schema = 1\n\n[[connection]]\nname = \"a\"\ntarget = \n"
	files := map[string]string{
		"co": "connections.toml", "alias": "aliases.toml",
		"repos": "repos.toml", "funcs": "funcs.toml",
	}
	adds := map[string][]string{
		"co":    {"add", "n", "--target", "h"},
		"alias": {"add", "n", "ls"},
		"repos": {"add", "n", "/p"},
		"funcs": {"add", "n", "--script", "/s"},
	}

	for verb, file := range files {
		h := newHarness(t)
		h.write(file, broken)

		for _, args := range [][]string{adds[verb], {"rm", "a", "--yes"}, {"list"}} {
			code := h.run(append([]string{verb}, args...)...)
			want := file + ":5:10"

			if code != 1 || !strings.Contains(h.errb.String(), "read-only") ||
				!strings.Contains(h.errb.String(), want) || h.out.Len() != 0 {
				t.Errorf("%s %v: code %d, stderr %q, want %s", verb, args, code, h.errb.String(), want)
			}

			if h.read(file) != broken {
				t.Errorf("%s %v: the file changed", verb, args)
			}
		}
	}
}

// TestResolveSharedAliasIsAmbiguous checks that two connections owning the same exact alias are
// reported together instead of the first one winning.
//
// Parameters:
//   - t: test to report to
func TestResolveSharedAliasIsAmbiguous(t *testing.T) {
	conns := []store.Connection{
		{Name: "web1", Aliases: []string{"web"}},
		{Name: "dupa", Aliases: []string{"web"}},
	}

	if got := resolveConn(conns, "web", 2); len(got) != 2 {
		t.Errorf("resolveConn = %v, want both connections", got)
	}
}

// TestConnectRemovesHandoff checks that the hand-off file of the wrapper is deleted before the
// process is replaced by the connection program, and that other files are not.
//
// Parameters:
//   - t: test to report to
func TestConnectRemovesHandoff(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	file := filepath.Join(h.dir, "handoff")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_HANDOFF"] = file

	if code := h.run("co", "web1"); code != 0 || len(h.execs) != 1 {
		t.Fatalf("code %d, execs %v, stderr %q", code, h.execs, h.errb.String())
	}

	if _, err := os.Stat(file); err == nil {
		t.Error("the hand-off file is still there")
	}

	keep := filepath.Join(h.dir, "keep")
	if err := os.WriteFile(keep, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	h.vars["TOTE_HANDOFF"] = keep

	h.run("co", "web1")

	if data, err := os.ReadFile(keep); err != nil || string(data) != "data" {
		t.Errorf("a file that is not a hand-off was touched: %q, %v", data, err)
	}
}
