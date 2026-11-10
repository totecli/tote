package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/shell"
)

// rowPattern matches one line of the table that scripts/import.sh prints.
var rowPattern = regexp.MustCompile(
	`(?m)^  (co|alias|repo|func) +(.*?) +(imported|import|same|skip|conflict) +(.*)$`)

// mnRig is a throw-away account for scripts/import.sh: a home directory with the mn tree in
// .config/mn, the places where the scripts of the functions live, and a directory of fake
// programs that comes first in PATH. The tote files go where the binary under test looks for
// them by default, in .config/tote of the home directory.
//
// Dir is the root, Home the home directory, MN the mn directory, Sys the system directory of
// the scripts, Bin the directory of the fake programs, Real the directory of the binary under
// test, Tote the configuration directory of tote and Script the path of the script under test.
type mnRig struct {
	Dir    string
	Home   string
	MN     string
	Sys    string
	Bin    string
	Real   string
	Tote   string
	Script string
}

// dat joins the fields of one record of an mn data file.
//
// Parameters:
//   - fields: values of the record, in file order
//
// Returns the line, without a line ending.
func dat(fields ...string) string {
	return strings.Join(fields, ":::")
}

// newMNRig makes the account and skips the test where the script cannot run: as root, or with a
// bash older than version 4.
//
// Parameters:
//   - t: running test
//
// Returns the account, with an empty mn tree.
func newMNRig(t *testing.T) *mnRig {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("the script refuses to run as root")
	}

	out, err := exec.Command("bash", "-c", "echo ${BASH_VERSINFO[0]}").Output()
	if err != nil || strings.TrimSpace(string(out)) < "4" {
		t.Skip("bash 4 or later is needed")
	}

	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "import.sh"))
	if err != nil {
		t.Fatal(err)
	}

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	r := &mnRig{
		Dir:    dir,
		Home:   filepath.Join(dir, "home"),
		MN:     filepath.Join(dir, "home", ".config", "mn"),
		Sys:    filepath.Join(dir, "sysbin"),
		Bin:    filepath.Join(dir, "fakebin"),
		Real:   toteBin(t),
		Tote:   filepath.Join(dir, "home", ".config", "tote"),
		Script: script,
	}

	for _, d := range []string{
		filepath.Join(r.MN, "data"), r.Sys, r.Bin, filepath.Join(dir, "run"),
		filepath.Join(r.Home, ".local", "bin"),
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	for _, name := range []string{"xe", "ssh", "git"} {
		r.put(t, filepath.Join(r.Bin, name), "#!/bin/sh\nexit 0\n", 0o700)
	}

	return r
}

// put writes a file and its directory.
//
// Parameters:
//   - t: running test
//   - path: file to write
//   - content: text to store
//   - mode: permission bits
func (r *mnRig) put(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// data writes a data file of mn.
//
// Parameters:
//   - t: running test
//   - name: file name below data, such as aliases.dat
//   - lines: the lines of the file
func (r *mnRig) data(t *testing.T, name string, lines ...string) {
	t.Helper()

	r.put(t, filepath.Join(r.MN, "data", name), strings.Join(lines, "\n")+"\n", 0o600)
}

// env returns the environment of the programs under test.
//
// Parameters:
//   - extra: variables added last
//
// Returns the variables, one per entry, in the form NAME=value.
func (r *mnRig) env(extra ...string) []string {
	env := []string{
		"PATH=" + r.Bin + ":" + r.Real + ":/usr/bin:/bin",
		"HOME=" + r.Home,
		"USER=tester",
		"LANG=en_US.UTF-8",
		"TOTE_LANG=en",
		"TOTE_COLOR=none",
		"XDG_RUNTIME_DIR=" + filepath.Join(r.Dir, "run"),
	}

	return append(env, extra...)
}

// run runs a program and returns what it printed and its exit code.
//
// Parameters:
//   - t: running test
//   - env: environment of the program
//   - name: program
//   - args: arguments
//
// Returns standard output, standard error and the exit code.
func (r *mnRig) run(t *testing.T, env []string, name string, args ...string) (string, string, int) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Dir = r.Dir

	var out, errb bytes.Buffer

	cmd.Stdout, cmd.Stderr = &out, &errb

	err := cmd.Run()

	var ee *exec.ExitError

	switch {
	case err == nil:
		return out.String(), errb.String(), 0
	case errors.As(err, &ee):
		return out.String(), errb.String(), ee.ExitCode()
	}

	t.Fatal(err)

	return "", "", -1
}

// runImport runs the script with this account as the mn user.
//
// Parameters:
//   - t: running test
//   - args: options of the script
//
// Returns standard output, standard error and the exit code.
func (r *mnRig) runImport(t *testing.T, args ...string) (string, string, int) {
	t.Helper()

	all := append([]string{r.Script, "--home", r.Home, "--system-bin", r.Sys}, args...)

	return r.run(t, r.env(), "bash", all...)
}

// tote runs the binary under test with this account.
//
// Parameters:
//   - t: running test
//   - args: command line without the program name
//
// Returns standard output; the test fails when the exit code is not 0.
func (r *mnRig) tote(t *testing.T, args ...string) string {
	t.Helper()

	out, errs, code := r.run(t, r.env(), filepath.Join(r.Real, "tote"), args...)
	if code != 0 {
		t.Fatalf("tote %v: exit %d, stdout %q, stderr %q", args, code, out, errs)
	}

	return out
}

// snapshot describes a tree: the mode of every entry, and the size, time and digest of every
// file.
//
// Parameters:
//   - t: running test
//   - root: directory to describe, which may not exist
//
// Returns one description per path.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()

	out := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		desc := info.Mode().String()
		if info.Mode().IsRegular() {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}

			sum := sha256.Sum256(data)
			desc += " " + info.ModTime().String() + " " + string(sum[:8])
		}

		out[path] = desc

		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}

	return out
}

// sameTree fails the test when two snapshots differ.
//
// Parameters:
//   - t: running test
//   - what: name of the tree, for the message
//   - before: snapshot taken first
//   - after: snapshot taken last
func sameTree(t *testing.T, what string, before, after map[string]string) {
	t.Helper()

	var diff []string

	for k, v := range before {
		if after[k] != v {
			diff = append(diff, "changed or removed: "+k)
		}
	}

	for k := range after {
		if _, ok := before[k]; !ok {
			diff = append(diff, "added: "+k)
		}
	}

	sort.Strings(diff)

	if len(diff) > 0 {
		t.Errorf("%s was touched:\n%s", what, strings.Join(diff, "\n"))
	}
}

// tableRow is one line of the table of the script.
type tableRow struct {
	Kind   string
	Name   string
	Status string
	Detail string
}

// rowsOf reads the table out of the output of the script.
//
// Parameters:
//   - out: standard output of the script
//
// Returns the rows in the order of the table.
func rowsOf(out string) []tableRow {
	var rows []tableRow

	for _, m := range rowPattern.FindAllStringSubmatch(out, -1) {
		rows = append(rows, tableRow{m[1], m[2], m[3], m[4]})
	}

	return rows
}

// wantRows checks that each expected row is in the table: same kind, name and status, and a
// detail that contains the expected text.
//
// Parameters:
//   - t: running test
//   - out: standard output of the script
//   - want: expected rows, whose Detail is a part of the real one
func wantRows(t *testing.T, out string, want []tableRow) {
	t.Helper()

	rows := rowsOf(out)

	for _, w := range want {
		found := false

		for _, r := range rows {
			if r.Kind == w.Kind && r.Name == w.Name && r.Status == w.Status &&
				strings.Contains(r.Detail, w.Detail) {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("no row %+v in the table:\n%s", w, out)
		}
	}
}

// fullTree writes an mn tree that covers every kind of file, with valid records, records tote
// refuses and malformed lines.
//
// Parameters:
//   - t: running test
//   - r: account to write into
func fullTree(t *testing.T, r *mnRig) {
	t.Helper()

	r.data(t, "connexions.dat",
		"# Format: name:::via:::target:::user:::aliases:::ip:::desc",
		dat("prod", "ssh", "host1.example.org", "admin", "web,p1", "10.0.0.5", "Production web"),
		dat("lab", "xe", "lab.example.org", "", "lb", "10.0.0.42", "Lab host through xe")+"\r",
		dat("jump", "ssh", "admin@jump.example.org", "", "", "", "Jump host"),
		dat("v6", "", "host2.example.org", "deploy", "", "2001:db8::1", ""),
		dat("tpl", "ssh -J admin@jump.example.org {user@}{host}", "10.0.0.7", "ops", "", "", ""),
		dat("notarget", "ssh", "", "", "", "", "Target is the name"),
		dat("serveur", "ssh", "host3.example.org", "", "srv", "", "Caf\u00e9 serveur"),
		dat("multi", "ssh", "host4.example.org", "", "p1,lb,ok1,bad name!,MULTI", "300.1.1.1", "x"),
		dat("longdesc", "ssh", "host5.example.org", "", "", "", strings.Repeat("d", 250)),
		dat("bad name", "ssh", "h", "", "", "", ""),
		dat(".hidden", "ssh", "h", "", "", "", ""),
		dat("prod", "ssh", "other.example.org", "", "", "", "Same name, other content"),
		dat("Prod", "ssh", "case.example.org", "", "", "", ""),
		dat("legacy", "192.0.2.1", "d", "ssh", "x", ""),
		dat("baduser", "ssh", "h", "ba d", "", "", ""),
		dat("badtarget", "ssh", "ho st", "", "", "", ""),
		dat("badvia", "ssh {nope}", "h", "", "", "", ""),
		dat("colon", "ssh", ":8080", "", "", "", ""),
		dat("badutf\xff", "ssh", "h", "", "", "", ""),
		"",
		"# a comment",
	)
	r.data(t, "aliases.dat",
		dat("ll", "ls -lah", "Detailed listing")+"\r",
		dat("..", "cd ..", "Up one"),
		dat("gq", `git commit -m "quoted \"msg\"" && echo 'x'`, `Quotes and a \ backslash`),
		dat("caf\u00e9", "echo cafe", ""),
		dat("tabbed", "echo a\tb", "Has a tab"),
		dat("esc", "echo hi", "bold\x1b[1mtext"),
		dat("gsign", "echo sign", "Alias and function"),
		dat("mn", "echo reserved", ""),
		dat("set", "echo reserved", ""),
		dat("bad;name", "echo x", ""),
		dat("-dash", "echo x", ""),
		dat("empty", "   ", ""),
		dat("ll", "ls", "Other content"),
		dat("many", "a", "b", "c"),
		dat("justname"),
	)
	r.data(t, "repos.dat",
		dat("app", "~/src/app", "My app"),
		dat("app", "~/src/app", "My app"),
		dat("home", "~", "Home"),
		dat("srv", "/srv/www", ""),
		dat("spaced", "~/dir with spaces/x", "Spaces in the path"),
		dat("rel", "src/rel", "Relative path"),
		dat("nopath", "", ""),
		dat("tote", "~/x", "Reserved"),
		dat("ll", "~/ll", "Clashes with the alias"),
	)
	r.data(t, "functions.dat",
		dat("mkcd", "Make a directory and enter it", "shell"),
		dat("hello", "Say hello", ""),
		dat("hello2", "Two fields only"),
		dat("both", "Script in both places"),
		dat("lost", "No script anywhere"),
		dat("gsign", "Clashes with the alias"),
		dat("select", "Shell keyword"),
		dat("bad.name", "Dot in the name"),
		dat("2fast", "Starts with a digit"),
	)

	r.put(t, filepath.Join(r.Home, ".local", "bin", "mkcd"), "#!/bin/sh\n", 0o700)
	r.put(t, filepath.Join(r.Home, ".local", "bin", "both"), "#!/bin/sh\n", 0o700)
	r.put(t, filepath.Join(r.Sys, "both"), "#!/bin/sh\n", 0o700)

	for _, n := range []string{"hello", "hello2", "gsign", "select", "bad.name", "2fast"} {
		r.put(t, filepath.Join(r.Sys, n), "#!/bin/sh\n", 0o700)
	}

	// Files that are left behind on purpose.
	r.put(t, filepath.Join(r.MN, ".lang"), "fr\n", 0o600)
	r.put(t, filepath.Join(r.MN, ".editor"), "vim\n", 0o600)
	r.put(t, filepath.Join(r.MN, ".repos"), "~/src\n", 0o600)
	r.put(t, filepath.Join(r.MN, ".cdn"), "https://cdn.example.org\n", 0o600)
	r.put(t, filepath.Join(r.MN, "data", "ghrel.list"), "tote\n", 0o600)
	r.put(t, filepath.Join(r.MN, "plugins", "ghrel", "plugin.sh"), "#!/bin/bash\n", 0o700)

	// The files that mn generates, which are not a source of data.
	block := "# >>> mn >>>\nalias ll='ls -lah'\n# <<< mn <<<\n"
	r.put(t, filepath.Join(r.Home, ".bash_aliases"), block, 0o600)
	r.put(t, filepath.Join(r.Home, ".bash_functions"), block, 0o600)
}

// fullRows is what the dry run of fullTree must say.
var fullRows = []tableRow{
	{"co", "prod", "import", "admin@host1.example.org"},
	{"co", "lab", "import", "via xe"},
	{"co", "jump", "import", "admin@jump.example.org"},
	{"co", "v6", "import", "deploy@host2.example.org"},
	{"co", "tpl", "import", "ops@10.0.0.7 via ssh -J"},
	{"co", "notarget", "import", "notarget"},
	{"co", "serveur", "import", "host3.example.org"},
	{"co", "multi", "import", "host4.example.org"},
	{"co", "longdesc", "import", "host5.example.org"},
	{"co", "bad name", "skip", "name must use ASCII"},
	{"co", ".hidden", "skip", "name must use ASCII"},
	{"co", "prod", "skip", "duplicate name in mn with other content"},
	{"co", "Prod", "skip", "name already used by connection prod"},
	{"co", "legacy", "skip", "expected 7 fields, found 6"},
	{"co", "baduser", "skip", "user must use"},
	{"co", "badtarget", "skip", "target contains whitespace"},
	{"co", "badvia", "skip", "unknown placeholder"},
	{"co", "colon", "skip", "target starts with -"},
	{"alias", "ll", "import", "ls -lah"},
	{"alias", "..", "import", "cd .."},
	{"alias", "gq", "import", "git commit"},
	{"alias", "caf\u00e9", "import", "echo cafe"},
	{"alias", "tabbed", "import", "echo a"},
	{"alias", "esc", "import", "echo hi"},
	{"alias", "gsign", "import", "echo sign"},
	{"alias", "mn", "skip", "reserved name"},
	{"alias", "set", "skip", "reserved name"},
	{"alias", "bad;name", "skip", "name holds a character"},
	{"alias", "-dash", "skip", "name must not start with -"},
	{"alias", "empty", "skip", "command is empty"},
	{"alias", "ll", "skip", "duplicate name in mn with other content"},
	{"alias", "many", "skip", "expected 2 to 3 fields, found 4"},
	{"alias", "justname", "skip", "expected 2 to 3 fields, found 1"},
	{"repo", "app", "import", "~/src/app"},
	{"repo", "home", "import", "~"},
	{"repo", "srv", "import", "/srv/www"},
	{"repo", "spaced", "import", "~/dir with spaces/x"},
	{"repo", "rel", "skip", "path is relative"},
	{"repo", "nopath", "skip", "path is empty"},
	{"repo", "tote", "skip", "reserved name"},
	{"repo", "ll", "skip", "name already used by alias ll"},
	{"func", "mkcd", "import", "source ~/.local/bin/mkcd"},
	{"func", "hello", "import", "exec "},
	{"func", "hello2", "import", "exec "},
	{"func", "both", "import", "exec ~/.local/bin/both"},
	{"func", "lost", "skip", "no script in"},
	{"func", "gsign", "skip", "name already used by alias gsign"},
	{"func", "select", "skip", "reserved name"},
	{"func", "bad.name", "skip", "name must use ASCII"},
	{"func", "2fast", "skip", "name must use ASCII"},
}

// TestImportDryRun checks that the default run reads everything, says what it would do and
// writes nothing, neither in the tote directory nor in the mn tree.
//
// Parameters:
//   - t: test to report to
func TestImportDryRun(t *testing.T) {
	r := newMNRig(t)
	fullTree(t, r)

	before := snapshot(t, r.Dir)

	out, errs, code := r.runImport(t)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (skipped entries)\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}

	wantRows(t, out, fullRows)

	for _, want := range []string{
		"tote import from mn: dry run", "Dry run: nothing written",
		"repos        read 9    import 4    same 1    conflict 0    skipped 4",
		"--apply to import 24 entries",
		"Not imported, on purpose: .lang .editor .repos .cdn data/ghrel.list plugins",
		`co multi: line 9: alias "p1" is already used by connection prod, left out`,
		`co multi: line 9: alias "lb" is already used by connection lab, left out`,
		`co multi: line 9: alias "badname!": name must use ASCII`,
		`co multi: line 9: ip "300.1.1.1" is not an IP address, left out`,
		"co longdesc: line 10: description cleaned or cut",
		"alias esc: line 6: description cleaned or cut",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	sameTree(t, "the account", before, snapshot(t, r.Dir))

	if _, err := os.Stat(r.Tote); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the dry run created %s (%v)", r.Tote, err)
	}
}

// doctor runs tote doctor and returns the status of each check by identifier.
//
// Parameters:
//   - t: running test
//   - r: account to check
//
// Returns the status and the message of each check, joined with a space.
func doctor(t *testing.T, r *mnRig) map[string]string {
	t.Helper()

	var rep struct {
		Summary struct {
			Fail int `json:"fail"`
		} `json:"summary"`
		Checks []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"checks"`
	}

	out, _, _ := r.run(t, r.env(), filepath.Join(r.Real, "tote"), "doctor", "--json")
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("doctor output: %v\n%s", err, out)
	}

	if rep.Summary.Fail != 0 {
		t.Errorf("doctor reports %d failures:\n%s", rep.Summary.Fail, out)
	}

	got := map[string]string{}
	for _, c := range rep.Checks {
		got[c.ID] = c.Status + " " + c.Message
	}

	return got
}

// TestImportApply imports the full tree, reads the result back with the real binary and with
// tote doctor, and runs the script a second time to see that it changes nothing.
//
// Parameters:
//   - t: test to report to
func TestImportApply(t *testing.T) {
	r := newMNRig(t)
	fullTree(t, r)

	mnBefore := snapshot(t, r.MN)

	out, errs, code := r.runImport(t, "--apply")
	if code != 1 || !strings.Contains(out, "Imported 24 entries.") {
		t.Fatalf("apply: exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}

	wantRows(t, out, []tableRow{{"co", "prod", "imported", ""}, {"func", "mkcd", "imported", ""}})
	sameTree(t, "the mn tree", mnBefore, snapshot(t, r.MN))

	if info, err := os.Stat(r.Tote); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("tote directory: %v, %v", info, err)
	}

	for _, f := range []string{"connections", "aliases", "repos", "funcs"} {
		info, err := os.Stat(filepath.Join(r.Tote, f+".toml"))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s.toml: %v, %v", f, info, err)
		}
	}

	checks := map[string][]string{
		"co list": {
			"prod\t\thost1.example.org\tadmin\tweb,p1\t10.0.0.5\tProduction web", "lab\txe\t",
		},
		"alias list": {
			"ll\tls -lah\tDetailed listing", `gq	git commit -m "quoted \"msg\"" && echo 'x'`,
		},
		"repos list": {"app\t~/src/app\tMy app", "home\t~\tHome", "spaced\t~/dir with spaces/x"},
		"funcs list": {
			"mkcd\tsource\t~/.local/bin/mkcd\tfalse", "both\texec\t~/.local/bin/both\tfalse",
		},
	}

	for cmd, wants := range checks {
		got := r.tote(t, strings.Fields(cmd)...)

		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("tote %s lacks %q:\n%s", cmd, want, got)
			}
		}
	}

	status := doctor(t, r)

	for id, want := range map[string]string{
		"file.connections": "ok connections.toml: valid, records: 9",
		"file.aliases":     "ok aliases.toml: valid, records: 7",
		// The sandbox has no ~/src or ~/dir with spaces: doctor names the three dead paths.
		"file.repos": "warn repositories whose path is not a directory that exists: 3",
		"file.funcs": "ok funcs.toml: valid, records: 4",
		"names":      "ok no name clash, records checked: 24",
		"via":        "ok",
	} {
		if got := status[id]; !strings.HasPrefix(got, want) {
			t.Errorf("doctor %s = %q, want %q", id, got, want)
		}
	}

	tree := snapshot(t, r.Tote)

	out, errs, code = r.runImport(t, "--apply")
	if code != 1 || !strings.Contains(out, "Nothing to import.") ||
		strings.Contains(out, "wrote ") || errs != "" {
		t.Errorf("second apply: exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}

	sameTree(t, "the tote directory (second run)", tree, snapshot(t, r.Tote))
	sameTree(t, "the mn tree (second run)", mnBefore, snapshot(t, r.MN))
}

// TestImportCleanTreeExitsZero checks the exit code 0 of a tree without anything to skip, and
// that two runs of --apply leave the same files.
//
// Parameters:
//   - t: test to report to
func TestImportCleanTreeExitsZero(t *testing.T) {
	r := newMNRig(t)
	r.data(t, "connexions.dat", dat("prod", "ssh", "host1.example.org", "admin", "p", "", ""))
	r.data(t, "aliases.dat", dat("ll", "ls -lah", ""))
	r.data(t, "repos.dat")
	r.put(t, filepath.Join(r.MN, "data", "functions.dat"), "", 0o600)

	if out, errs, code := r.runImport(t); code != 0 || !strings.Contains(out, "import 1 ") {
		t.Fatalf("dry run: exit %d\n%s\n%s", code, out, errs)
	}

	for i := 0; i < 2; i++ {
		out, errs, code := r.runImport(t, "--apply")
		if code != 0 || errs != "" {
			t.Fatalf("apply %d: exit %d\n%s\n%s", i+1, code, out, errs)
		}
	}

	if got := readFile(t, filepath.Join(r.Tote, "aliases.toml")); got !=
		"schema = 1\n\n[[alias]]\nname = \"ll\"\ncmd = \"ls -lah\"\n" {
		t.Errorf("aliases.toml = %q", got)
	}

	for _, f := range []string{"repos.toml", "funcs.toml"} {
		if _, err := os.Stat(filepath.Join(r.Tote, f)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s was created for nothing (%v)", f, err)
		}
	}
}

// TestImportMatchesTote checks that the records of the script are the ones tote itself writes,
// byte for byte, and that a record added with tote is seen as the same one.
//
// Parameters:
//   - t: test to report to
func TestImportMatchesTote(t *testing.T) {
	desc := "Caf\u00e9 \"quoted\" \\ back\x01"
	clean := "Caf\u00e9 \"quoted\" \\ back"

	add := func(r *mnRig) {
		r.tote(t, "co", "add", "prod", "--target", "host1.example.org", "--user", "admin",
			"--alias", "web,p1", "--ip", "2001:db8::1", "--desc", clean)
		r.tote(t, "co", "add", "lab", "--via", "xe", "--target", "lab.example.org")
		r.tote(t, "alias", "add", "gq", "--desc", clean, "echo \"a\\b\"\t$HOME 'x'")
		r.tote(t, "alias", "add", "ll", "ls -lah")
		r.tote(t, "repos", "add", "app", "~/src/app", "--desc", clean)
		r.tote(t, "funcs", "add", "mkcd", "--script", "~/.local/bin/mkcd", "--kind", "source",
			"--desc", clean)
		r.tote(t, "funcs", "add", "hello", "--script", "~/.local/bin/hello", "--kind", "exec")
	}

	setup := func(r *mnRig) {
		r.data(t, "connexions.dat",
			dat("prod", "ssh", "host1.example.org", "admin", "web,p1", "2001:db8::1", clean),
			dat("lab", "xe", "lab.example.org", "", "", "", ""))
		r.data(t, "aliases.dat",
			dat("gq", "echo \"a\\b\"\t$HOME 'x'", desc), dat("ll", "ls -lah", ""))
		r.data(t, "repos.dat", dat("app", "~/src/app", clean))
		r.data(t, "functions.dat", dat("mkcd", clean, "shell"), dat("hello", "", ""))
		r.put(t, filepath.Join(r.Home, ".local", "bin", "mkcd"), "#!/bin/sh\n", 0o700)
		r.put(t, filepath.Join(r.Home, ".local", "bin", "hello"), "#!/bin/sh\n", 0o700)
	}

	byTote := newMNRig(t)
	setup(byTote)
	add(byTote)

	byScript := newMNRig(t)
	setup(byScript)

	if out, errs, code := byScript.runImport(t, "--apply"); code != 0 {
		t.Fatalf("apply: exit %d\n%s\n%s", code, out, errs)
	}

	for _, f := range []string{"connections", "aliases", "repos", "funcs"} {
		want := readFile(t, filepath.Join(byTote.Tote, f+".toml"))
		if got := readFile(t, filepath.Join(byScript.Tote, f+".toml")); got != want {
			t.Errorf("%s.toml differs from tote:\n--- script\n%s--- tote\n%s", f, got, want)
		}
	}

	tree := snapshot(t, byTote.Tote)

	out, errs, code := byTote.runImport(t, "--apply", "--verbose")
	if code != 0 || strings.Contains(out, "wrote ") || errs != "" {
		t.Fatalf("apply over the records of tote: exit %d\n%s\n%s", code, out, errs)
	}

	wantRows(t, out, []tableRow{
		{"co", "prod", "same", "already in tote"},
		{"alias", "gq", "same", "already in tote"},
		{"repo", "app", "same", "already in tote"},
		{"func", "mkcd", "same", "already in tote"},
	})
	sameTree(t, "the tote directory", tree, snapshot(t, byTote.Tote))
}

// TestImportExistingFiles checks the files of tote that already exist: their comments, order,
// line endings and mode are kept, a record with other content is a conflict that stays as it
// is, names of other kinds are respected, and a backup is made of each file that changes.
//
// Parameters:
//   - t: test to report to
func TestImportExistingFiles(t *testing.T) {
	r := newMNRig(t)
	r.data(t, "connexions.dat",
		dat("prod", "ssh", "other.example.org", "", "", "", "Other content"),
		dat("new", "ssh", "new.example.org", "", "web", "", ""),
		dat("web2", "ssh", "web2.example.org", "", "mine", "", ""))
	r.data(t, "aliases.dat", dat("ll", "ls", ""), dat("fresh", "echo fresh", ""),
		dat("keep", "echo mn", ""), dat("cf", "echo mn", ""))
	r.data(t, "repos.dat", dat("keep", "~/keep", ""), dat("proj", "~/proj", ""))

	conns := "# my hosts\r\nschema = 1\r\n\r\n# the first\r\n[[connection]]\r\nname = 'prod'\r\n" +
		"target = 'host1.example.org'\r\naliases = ['web']\r\n"
	aliases := "schema = 1\n\n# listing\n[[alias]]\nname = \"ll\"\ncmd = \"ls\"\n\n" +
		"[[alias]]\nname = \"cf\"\ncmd = \"echo tote\"\n"
	repos := "# only comments\n"

	target := filepath.Join(r.Dir, "elsewhere", "aliases.toml")
	r.put(t, target, aliases, 0o640)
	r.put(t, filepath.Join(r.Tote, "connections.toml"), conns, 0o640)
	r.put(t, filepath.Join(r.Tote, "repos.toml"), repos, 0o600)

	if err := os.Symlink(target, filepath.Join(r.Tote, "aliases.toml")); err != nil {
		t.Fatal(err)
	}

	out, errs, code := r.runImport(t, "--apply", "--verbose")
	if code != 1 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}

	wantRows(t, out, []tableRow{
		{"co", "prod", "conflict", "other content (aliases, desc, target), kept"},
		{"co", "new", "imported", "new.example.org"},
		{"co", "web2", "imported", "web2.example.org"},
		{"alias", "ll", "same", "already in tote"},
		{"alias", "cf", "conflict", "other content (cmd), kept"},
		{"alias", "fresh", "imported", ""},
		{"alias", "keep", "imported", ""},
		{"repo", "keep", "skip", "name already used by alias keep"},
		{"repo", "proj", "imported", ""},
	})

	if !strings.Contains(out, `co new: line 2: alias "web" is already used by connection prod`) {
		t.Errorf("no note for the alias held by the record of tote:\n%s", out)
	}

	gotConns := readFile(t, filepath.Join(r.Tote, "connections.toml"))
	if !strings.HasPrefix(gotConns, conns) || strings.Contains(gotConns, "other.example.org") ||
		strings.Contains(strings.ReplaceAll(gotConns, "\r\n", ""), "\n") {
		t.Errorf("connections.toml:\n%q", gotConns)
	}

	wantConns := conns + "\r\n[[connection]]\r\nname = \"new\"\r\n" +
		"target = \"new.example.org\"\r\n\r\n[[connection]]\r\nname = \"web2\"\r\n" +
		"target = \"web2.example.org\"\r\naliases = [\"mine\"]\r\n"
	if gotConns != wantConns {
		t.Errorf("connections.toml = %q\nwant %q", gotConns, wantConns)
	}

	if got := readFile(t, filepath.Join(r.Tote, "repos.toml")); got !=
		repos+"\nschema = 1\n\n[[repo]]\nname = \"proj\"\npath = \"~/proj\"\n" {
		t.Errorf("repos.toml = %q", got)
	}

	if info, err := os.Lstat(filepath.Join(r.Tote, "aliases.toml")); err != nil ||
		info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", info, err)
	}

	if got := readFile(t, target); !strings.HasPrefix(got, aliases) ||
		!strings.Contains(got, "name = \"fresh\"") || strings.Count(got, "name = \"cf\"") != 1 {
		t.Errorf("the target of the link = %q", got)
	}

	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("mode of the target: %v, %v", info, err)
	}

	for name, original := range map[string]string{
		"connections.toml": conns, "repos.toml": repos, "aliases.toml": aliases,
	} {
		matches, _ := filepath.Glob(filepath.Join(r.Tote, name+".bak-*"))
		if len(matches) != 1 || readFile(t, matches[0]) != original {
			t.Errorf("backups of %s: %v", name, matches)
		}
	}

	if matches, _ := filepath.Glob(filepath.Join(r.Tote, "funcs.toml.*")); len(matches) != 1 ||
		!strings.HasSuffix(matches[0], ".lock") {
		t.Errorf("funcs files: %v, want the lock file only", matches)
	}

	if _, err := os.Stat(filepath.Join(r.Tote, "funcs.toml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("funcs.toml was created for nothing (%v)", err)
	}

	if status := doctor(t, r)["file.connections"]; !strings.HasPrefix(status, "ok") {
		t.Errorf("doctor: %s", status)
	}
}

// TestImportRefusals checks the fatal errors: each one exits with 2, says why and touches
// nothing.
//
// Parameters:
//   - t: test to report to
func TestImportRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*mnRig)
		args  func(*mnRig) []string
		env   []string
		want  string
	}{
		{
			"unknown option", nil, func(*mnRig) []string { return []string{"--nope"} }, nil,
			"unknown option",
		},
		{
			"missing value", nil, func(*mnRig) []string { return []string{"--from"} }, nil,
			"needs a value",
		},
		{"no mn directory", nil, func(r *mnRig) []string {
			return []string{"--from", filepath.Join(r.Dir, "absent")}
		}, nil, "not an mn directory"},
		{"to inside from", nil, func(r *mnRig) []string {
			return []string{"--to", filepath.Join(r.MN, "tote")}
		}, nil, "must not contain each other"},
		{"to is a file", nil, func(r *mnRig) []string {
			return []string{"--to", filepath.Join(r.MN, "data", "aliases.dat")}
		}, nil, "is not a directory"},
		{
			"relative override", nil, func(*mnRig) []string { return nil },
			[]string{"TOTE_CONFIG_DIR=relative/dir"},
			"TOTE_CONFIG_DIR is not an absolute path",
		},
		{"broken tote file", func(r *mnRig) {
			r.put(t, filepath.Join(r.Tote, "aliases.toml"), "schema = 1\n[[alias]\nname = 1\n",
				0o600)
		}, func(*mnRig) []string { return []string{"--apply"} }, nil, "aliases.toml:2"},
		{"unknown schema", func(r *mnRig) {
			r.put(t, filepath.Join(r.Tote, "repos.toml"), "schema = 2\n", 0o600)
		}, func(*mnRig) []string { return []string{"--apply"} }, nil, "repos.toml:1"},
		{"multi-line value", func(r *mnRig) {
			r.put(t, filepath.Join(r.Tote, "funcs.toml"),
				"schema = 1\n[[func]]\nname = \"x\"\ndesc = \"\"\"a\nb\"\"\"\n", 0o600)
		}, func(*mnRig) []string { return []string{"--apply"} }, nil, "funcs.toml:4"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newMNRig(t)
			r.data(t, "aliases.dat", dat("ll", "ls", ""))
			r.data(t, "repos.dat", dat("app", "~/app", ""))

			if c.setup != nil {
				c.setup(r)
			}

			before := snapshot(t, r.Dir)
			args := []string{r.Script, "--home", r.Home, "--system-bin", r.Sys}

			if c.args != nil {
				args = append(args, c.args(r)...)
			}

			out, errs, code := r.run(t, r.env(c.env...), "bash", args...)
			if code != 2 || !strings.Contains(errs, c.want) {
				t.Errorf("exit %d, stderr %q, want exit 2 and %q\n%s", code, errs, c.want, out)
			}

			sameTree(t, "the account", before, snapshot(t, r.Dir))
		})
	}
}

// TestImportDefaultPaths checks where the script writes by default: the way tote itself finds
// its configuration directory, and the mn directory of the account.
//
// Parameters:
//   - t: test to report to
func TestImportDefaultPaths(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		want func(*mnRig) string
	}{
		{"home", nil, func(r *mnRig) string { return filepath.Join(r.Home, ".config", "tote") }},
		{
			"xdg",
			[]string{"XDG_CONFIG_HOME=/xdg/conf"},
			func(*mnRig) string { return "/xdg/conf/tote" },
		},
		{"relative xdg is ignored", []string{"XDG_CONFIG_HOME=conf"}, func(r *mnRig) string {
			return filepath.Join(r.Home, ".config", "tote")
		}},
		{
			"override wins",
			[]string{"XDG_CONFIG_HOME=/xdg/conf", "TOTE_CONFIG_DIR=/own/dir/"},
			func(*mnRig) string { return "/own/dir" },
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newMNRig(t)
			r.data(t, "aliases.dat", dat("ll", "ls", ""))

			out, errs, code := r.run(t, r.env(c.env...), "bash", r.Script, "--home", r.Home)
			if code != 0 {
				t.Fatalf("exit %d\n%s\n%s", code, out, errs)
			}

			for _, want := range []string{"  to    " + c.want(r) + "\n", "  from  " + r.MN + "\n"} {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestImportReadOnlyMN checks that the script works on an mn tree it cannot write to, which
// proves that it never tries.
//
// Parameters:
//   - t: test to report to
func TestImportReadOnlyMN(t *testing.T) {
	r := newMNRig(t)
	fullTree(t, r)

	data := filepath.Join(r.MN, "data")
	files, _ := filepath.Glob(filepath.Join(data, "*"))

	for _, f := range files {
		if err := os.Chmod(f, 0o400); err != nil {
			t.Fatal(err)
		}
	}

	for _, d := range []string{data, r.MN} {
		if err := os.Chmod(d, 0o500); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		_ = os.Chmod(r.MN, 0o700)
		_ = os.Chmod(data, 0o700)
	})

	out, errs, code := r.runImport(t, "--apply")
	if code != 1 || !strings.Contains(out, "Imported 24") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}
}

// TestImportReadOnlyTote checks that a tote file that cannot be written stops the import before
// anything is written, instead of leaving half of it done.
//
// Parameters:
//   - t: test to report to
func TestImportReadOnlyTote(t *testing.T) {
	r := newMNRig(t)
	r.data(t, "connexions.dat", dat("prod", "ssh", "host1.example.org", "", "", "", ""))
	r.data(t, "aliases.dat", dat("ll", "ls", ""))

	aliases := filepath.Join(r.Tote, "aliases.toml")
	r.put(t, aliases, "schema = 1\n", 0o400)

	before := snapshot(t, r.Tote)

	out, errs, code := r.runImport(t, "--apply")
	if code != 2 || !strings.Contains(errs, "aliases.toml is not writable") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}

	_, err := os.Stat(filepath.Join(r.Tote, "connections.toml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("connections.toml was written before the failure (%v)", err)
	}

	if matches, _ := filepath.Glob(filepath.Join(r.Tote, "*.bak-*")); len(matches) != 0 {
		t.Errorf("backups were made: %v", matches)
	}

	for path, was := range before {
		if got := snapshot(t, path)[path]; got != was {
			t.Errorf("%s changed: %q, was %q", path, got, was)
		}
	}
}

// TestImportReservedNames checks that every name that tote reserves is refused for an alias, a
// repository and a function.
//
// Parameters:
//   - t: test to report to
func TestImportReservedNames(t *testing.T) {
	names := []string{
		"tote", "source", "eval", "command", "builtin", "alias", "unalias", "unset",
		"local", "read", "history", "umask", "exit", "return", "emulate", ":", ".", "set", "printf",
		"complete", "compdef", "_describe", "_tote", "__tote_unload", "__tote_complete", "if",
		"then", "else", "elif", "fi", "case", "esac", "for", "select", "while", "until", "do",
		"done", "in", "function", "time", "coproc", "repeat", "foreach", "end", "always",
		"nocorrect",
	}

	r := newMNRig(t)

	var aliases, repos, funcs []string

	for _, n := range names {
		if !shell.Reserved(n) {
			t.Errorf("%q is not reserved by tote", n)
		}

		if n == ":" {
			// A name that ends with a colon cannot be written in a data file of mn.
			continue
		}

		if n != "." {
			r.put(t, filepath.Join(r.Sys, n), "#!/bin/sh\n", 0o700)
		}

		aliases = append(aliases, dat(n, "echo x", ""))
		repos = append(repos, dat(n, "~/x", ""))
		funcs = append(funcs, dat(n, "d", ""))
	}

	r.data(t, "aliases.dat", aliases...)
	r.data(t, "repos.dat", repos...)
	r.data(t, "functions.dat", funcs...)

	out, errs, code := r.runImport(t)
	if code != 1 || errs != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}

	if n := len(rowsOf(out)); n != 3*(len(names)-1) {
		t.Errorf("%d rows, want %d", n, 3*(len(names)-1))
	}

	for _, row := range rowsOf(out) {
		if row.Status != "skip" {
			t.Errorf("row %+v is not a refusal", row)
		}
	}
}

// TestImportHelp checks that the help documents the options and exits with 0.
//
// Parameters:
//   - t: test to report to
func TestImportHelp(t *testing.T) {
	r := newMNRig(t)

	out, errs, code := r.run(t, r.env(), "bash", r.Script, "--help")
	if code != 0 || errs != "" {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}

	for _, want := range []string{
		"--apply", "--from", "--to", "--home", "--system-bin", "--verbose",
		"connexions.dat", "aliases.dat", "repos.dat", "functions.dat", "conflict", "Exit status",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

// TestImportNamesAgreeWithTote checks that a name with an invisible, private use or non-ASCII
// space character is judged by the script as tote judges it, and that a plain accented name is
// still imported.
//
// Parameters:
//   - t: test to report to
func TestImportNamesAgreeWithTote(t *testing.T) {
	cases := []struct {
		name     string
		alias    string
		importOK bool
	}{
		{"zero width joiner", "a\u200db", false},
		{"zero width non joiner", "a\u200cb", false},
		{"arabic number sign", "a\u0600b", false},
		{"mongolian vowel separator", "a\u180eb", false},
		{"private use", "a\ue000b", false},
		{"private use plane 15", "a\U000f0000b", false},
		{"ogham space mark", "a\u1680b", false},
		{"medium mathematical space", "a\u205fb", false},
		{"tag character", "a\U000e0001b", false},
		{"no break space", "a\u00a0b", false},
		{"accented letter", "caf\u00e9", true},
		{"han characters", "\u65e5\u672c", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newMNRig(t)
			r.data(t, "aliases.dat", dat(c.alias, "echo x", ""))

			out, errs, code := r.runImport(t)
			want := "skip"

			if c.importOK {
				want = "import"
			}

			rows := rowsOf(out)
			if len(rows) != 1 || rows[0].Status != want {
				t.Fatalf("exit %d, rows %+v, want one %s\n%s\n%s", code, rows, want, out, errs)
			}

			_, _, toteCode := r.run(t, r.env(), filepath.Join(r.Real, "tote"), "alias", "add",
				c.alias, "echo x")
			if accepted := toteCode == 0; accepted != c.importOK {
				t.Errorf("tote accepts the name: %v, the script imports it: %v", accepted,
					c.importOK)
			}
		})
	}
}

// TestImportIPv4Tail checks that an IPv6 address that ends with an IPv4 address is kept.
//
// Parameters:
//   - t: test to report to
func TestImportIPv4Tail(t *testing.T) {
	r := newMNRig(t)

	var lines []string

	for i, ip := range []string{
		"::ffff:1.2.3.4", "64:ff9b::1.2.3.4", "1:2:3:4:5:6:1.2.3.4",
		"::1.2.3.4", "::ffff:1.2.3.400", "1:2:3:4:5:6:7:1.2.3.4",
	} {
		lines = append(lines, dat(fmt.Sprintf("h%d", i), "ssh", "h.example.org", "", "", ip, ""))
	}

	r.data(t, "connexions.dat", lines...)

	out, errs, code := r.runImport(t, "--apply")
	if code != 1 || errs != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}

	for _, ip := range []string{
		"::ffff:1.2.3.4", "64:ff9b::1.2.3.4", "1:2:3:4:5:6:1.2.3.4",
		"::1.2.3.4",
	} {
		if strings.Contains(out, `ip "`+ip+`"`) {
			t.Errorf("the ip %s was left out:\n%s", ip, out)
		}

		if !strings.Contains(readFile(t, filepath.Join(r.Tote, "connections.toml")),
			"ip = \""+ip+"\"") {
			t.Errorf("the ip %s was not written", ip)
		}
	}

	for _, ip := range []string{"::ffff:1.2.3.400", "1:2:3:4:5:6:7:1.2.3.4"} {
		if !strings.Contains(out, `ip "`+ip+`" is not an IP address`) {
			t.Errorf("the ip %s was not refused:\n%s", ip, out)
		}
	}
}

// TestImportRelativeSystemBin checks that a relative system directory becomes an absolute path
// in funcs.toml, since tote refuses a relative script.
//
// Parameters:
//   - t: test to report to
func TestImportRelativeSystemBin(t *testing.T) {
	r := newMNRig(t)
	r.data(t, "functions.dat", dat("fs", "from the system directory", "shell"))
	r.put(t, filepath.Join(r.Dir, "relbin", "fs"), "#!/bin/sh\n", 0o700)

	out, errs, code := r.runImport(t, "--system-bin", "relbin", "--apply")
	if code != 0 || errs != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}

	want := "script = \"" + filepath.Join(r.Dir, "relbin", "fs") + "\""
	if got := readFile(t, filepath.Join(r.Tote, "funcs.toml")); !strings.Contains(got, want) {
		t.Errorf("funcs.toml lacks %s:\n%s", want, got)
	}
}

// TestImportLongLine checks that a line longer than the limit is skipped with its reason and
// that the lines after it are still read.
//
// Parameters:
//   - t: test to report to
func TestImportLongLine(t *testing.T) {
	r := newMNRig(t)
	r.data(t, "aliases.dat", dat("big", "echo "+strings.Repeat("x", 70000), ""),
		dat("ll", "ls", ""))

	out, errs, code := r.runImport(t)
	if code != 1 || errs != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}

	wantRows(t, out, []tableRow{
		{"alias", "big", "skip", "line 1: line longer than 65536 bytes"},
		{"alias", "ll", "import", ""},
	})
}

// TestImportControlCharacters checks that a command with an escape or a delete character
// written by tote itself is seen as the same record, which needs the same escapes in both.
//
// Parameters:
//   - t: test to report to
func TestImportControlCharacters(t *testing.T) {
	r := newMNRig(t)
	cmd := "echo a\x1bb\x7fc\x01d"

	r.tote(t, "alias", "add", "dl", cmd)
	r.data(t, "aliases.dat", dat("dl", cmd, ""))

	out, errs, code := r.runImport(t, "--verbose")
	if code != 0 || errs != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}

	wantRows(t, out, []tableRow{{"alias", "dl", "same", "already in tote"}})
}

// TestImportOddTargetFiles checks two files of tote that the script reads without writing a
// duplicate or a confusing message: one that starts with a byte order mark, and one whose schema
// is newer than the script.
//
// Parameters:
//   - t: test to report to
func TestImportOddTargetFiles(t *testing.T) {
	t.Run("byte order mark", func(t *testing.T) {
		r := newMNRig(t)
		r.data(t, "aliases.dat", dat("fresh", "echo fresh", ""))
		r.put(t, filepath.Join(r.Tote, "aliases.toml"),
			"\ufeffschema = 1\n\n[[alias]]\nname = \"ll\"\ncmd = \"ls\"\n", 0o600)

		out, errs, code := r.runImport(t, "--apply")
		if code != 0 || errs != "" {
			t.Fatalf("exit %d\n%s\n%s", code, out, errs)
		}

		got := readFile(t, filepath.Join(r.Tote, "aliases.toml"))
		if strings.Count(got, "schema") != 1 || !strings.Contains(got, "name = \"fresh\"") {
			t.Errorf("aliases.toml = %q", got)
		}

		if status := doctor(t, r)["file.aliases"]; !strings.HasPrefix(status, "ok") {
			t.Errorf("doctor: %s", status)
		}
	})

	t.Run("newer schema", func(t *testing.T) {
		r := newMNRig(t)
		r.data(t, "aliases.dat", dat("fresh", "echo fresh", ""))
		r.put(t, filepath.Join(r.Tote, "aliases.toml"), "schema = 2\n", 0o600)

		_, errs, code := r.runImport(t, "--apply")
		if code != 2 || !strings.Contains(errs, "aliases.toml:1: schema 2 is not supported") {
			t.Errorf("exit %d, stderr %q", code, errs)
		}
	})
}

// TestImportGlobInStore checks that a pattern character in the aliases of a tote file is not
// expanded against the files of the working directory.
//
// Parameters:
//   - t: test to report to
func TestImportGlobInStore(t *testing.T) {
	r := newMNRig(t)
	r.data(t, "connexions.dat", dat("web", "ssh", "web.example.org", "", "", "", ""))
	r.put(t, filepath.Join(r.Tote, "connections.toml"),
		"schema = 1\n\n[[connection]]\nname = \"x\"\naliases = [\"*\"]\n", 0o600)
	r.put(t, filepath.Join(r.Dir, "web"), "", 0o600)

	out, errs, code := r.runImport(t)
	if code != 0 || errs != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}

	wantRows(t, out, []tableRow{{"co", "web", "import", ""}})
}
