package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// preambles lists the option sets under which every wrapper test runs. The wrapper must work in a
// plain shell, with unset variables fatal, and with errexit as well.
var preambles = []struct{ name, text string }{
	{"plain", ""},
	{"nounset", "set -u\n"},
	{"errexit", "set -eu\n"},
}

// wrapperEnv is a sandbox prepared for the wrapper: a fake tote in PATH, a private runtime
// directory for the hand-off files, and the shell and kind under test.
type wrapperEnv struct {
	*sandbox
	shell   string
	kind    Kind
	runtime string
}

// newWrapperEnv builds a wrapperEnv for bash.
//
// Parameters:
//   - t: test to report to
func newWrapperEnv(t *testing.T) *wrapperEnv {
	t.Helper()

	sb := newSandbox(t)
	sb.installFakeTote(t)

	runtime := filepath.Join(sb.Dir, "run")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}

	sb.setenv("XDG_RUNTIME_DIR", runtime)

	return &wrapperEnv{sandbox: sb, shell: needShell(t, "bash"), kind: Bash, runtime: runtime}
}

// call evaluates the wrapper, runs one tote command line and prints the status, the directory and
// the reload marker.
//
// Parameters:
//   - t: test to report to
//   - pre: option line such as "set -u", possibly empty
//   - line: shell command line that starts with the tote function
//
// Returns the lines printed by the script as a map from key to value, and standard error.
func (w *wrapperEnv) call(t *testing.T, pre, line string) (map[string]string, string) {
	t.Helper()

	script := pre + Wrapper(w.kind) + "RELOADED=no\n" + line + " && rc=0 || rc=$?\n" +
		"printf 'rc=%s\\npwd=%s\\nreloaded=%s\\n' \"$rc\" \"$PWD\" \"$RELOADED\"\n"

	out, errOut, code := w.run(t, w.shell, script)
	if code != 0 {
		t.Fatalf("script exited %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}

	got := map[string]string{}

	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			got[k] = v
		}
	}

	return got, errOut
}

// leftovers lists the entries of the runtime directory.
//
// Parameters:
//   - t: test to report to
func (w *wrapperEnv) leftovers(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(w.runtime)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

// checkSyntax checks the init output and the wrapper of one shell with the syntax checker of
// that shell, and with shellcheck when it exists.
//
// Parameters:
//   - t: test to report to
//   - k: shell to write for
func checkSyntax(t *testing.T, k Kind) {
	t.Helper()

	sh := needShell(t, string(k))
	sb := newSandbox(t)

	if _, errOut, code := sb.run(t, sh, "exit 0\n"); code != 0 {
		t.Fatalf("shell does not start: %s", errOut)
	}

	file := filepath.Join(sb.Dir, "init.sh")
	if err := os.WriteFile(file, []byte(Init(k, fixtureDefs())), 0o600); err != nil {
		t.Fatal(err)
	}

	if out, errOut, code := sb.exec(t, sh, "-n", file); code != 0 {
		t.Errorf("%s -n failed: %s%s", k, out, errOut)
	}

	// shellcheck reads sh, bash, dash and ksh only: it refuses zsh, which the syntax check above
	// already covered.
	if k != Bash {
		return
	}

	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck is not installed")
	}

	wrapper := filepath.Join(sb.Dir, "wrapper.sh")
	if err := os.WriteFile(wrapper, []byte(Wrapper(k)), 0o600); err != nil {
		t.Fatal(err)
	}

	if out, errOut, code := sb.exec(t, "shellcheck", "-s", string(k), wrapper); code != 0 {
		t.Errorf("shellcheck: %s%s", out, errOut)
	}
}

// TestWrapperSyntax checks both wrappers and the whole init output with the syntax checkers that
// are installed, and with shellcheck when it exists.
//
// Parameters:
//   - t: test to report to
func TestWrapperSyntax(t *testing.T) {
	for _, k := range []Kind{Bash, Zsh} {
		t.Run(string(k), func(t *testing.T) { checkSyntax(t, k) })
	}
}

// TestWrapperUnknownKind checks that an unknown shell yields no text at all.
//
// Parameters:
//   - t: test to report to
func TestWrapperUnknownKind(t *testing.T) {
	for _, k := range []Kind{"", "fish", "BASH"} {
		if Wrapper(k) != "" || Definitions(k, Defs{}) != "" || Init(k, Defs{}) != "" {
			t.Errorf("kind %q produced text", k)
		}
	}
}

// checkActions makes the fake tote ask for a change of directory to several awkward directory
// names, under one set of shell options.
//
// Parameters:
//   - t: test to report to
//   - pre: option line such as "set -u", possibly empty
func checkActions(t *testing.T, pre string) {
	t.Helper()

	w := newWrapperEnv(t)

	dirs := map[string]string{
		"plain":   filepath.Join(w.Dir, "plain"),
		"space":   filepath.Join(w.Dir, "with space"),
		"newline": filepath.Join(w.Dir, "new\nline"),
		"quote":   filepath.Join(w.Dir, "it's \"q\" $x `y`"),
		"-dash":   filepath.Join(w.Dir, "-dash"),
	}

	for _, d := range dirs {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	for name, d := range dirs {
		w.setenv("FAKE_ACTION", "cd")
		w.setenv("FAKE_PAYLOAD", d)

		// A newline in the value cannot be told apart in the key=value report.
		got, _ := w.call(t, pre, "tote go")
		if got["pwd"] != d && name != "newline" {
			t.Errorf("%s: pwd = %q, want %q", name, got["pwd"], d)
		}

		if got["rc"] != "0" {
			t.Errorf("%s: rc = %q", name, got["rc"])
		}
	}

	w.setenv("FAKE_PAYLOAD", "-dash")

	if got, _ := w.call(t, pre, "tote go"); got["pwd"] != dirs["-dash"] {
		t.Errorf("relative dash directory: pwd = %q", got["pwd"])
	}

	if left := w.leftovers(t); len(left) != 0 {
		t.Errorf("hand-off files left behind: %q", left)
	}
}

// TestWrapperActions runs the wrapper against the fake tote for the cd action.
//
// Parameters:
//   - t: test to report to
func TestWrapperActions(t *testing.T) {
	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) { checkActions(t, pre.text) })
	}
}

// TestWrapperCdIgnored checks the payloads that must leave the directory alone.
//
// Parameters:
//   - t: test to report to
func TestWrapperCdIgnored(t *testing.T) {
	w := newWrapperEnv(t)

	file := filepath.Join(w.Dir, "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, payload := range []string{"", "/nonexistent/dir", file, "$(touch PWNED)", "~"} {
		w.setenv("FAKE_ACTION", "cd")
		w.setenv("FAKE_PAYLOAD", payload)

		got, _ := w.call(t, "set -u\n", "tote go")
		if got["pwd"] != w.Dir || got["rc"] != "0" {
			t.Errorf("payload %q: pwd %q rc %q", payload, got["pwd"], got["rc"])
		}
	}

	if _, err := os.Stat(filepath.Join(w.Dir, "PWNED")); err == nil {
		t.Error("a payload was evaluated")
	}
}

// checkExitCode makes the fake tote exit with 7 after leaving a hand-off and checks that the
// function returns 7.
//
// Parameters:
//   - t: test to report to
//   - pre: option line such as "set -u", possibly empty
//   - action: action the fake tote leaves, empty for no hand-off
func checkExitCode(t *testing.T, pre, action string) {
	t.Helper()

	w := newWrapperEnv(t)
	w.setenv("FAKE_ACTION", action)
	w.setenv("FAKE_PAYLOAD", w.Dir)
	w.setenv("FAKE_RC", "7")

	got, _ := w.call(t, pre, "tote go")
	if got["rc"] != "7" {
		t.Errorf("rc = %q, want 7", got["rc"])
	}

	if left := w.leftovers(t); len(left) != 0 {
		t.Errorf("hand-off files left behind: %q", left)
	}
}

// TestWrapperExitCode checks that the code of the binary is the code of the function for every
// action, with and without a hand-off.
//
// Parameters:
//   - t: test to report to
func TestWrapperExitCode(t *testing.T) {
	for _, pre := range preambles {
		for _, action := range []string{"", "none", "cd", "insert"} {
			t.Run(pre.name+"/"+action, func(t *testing.T) { checkExitCode(t, pre.text, action) })
		}
	}
}

// checkExit makes the fake tote ask the shell to exit, inside a subshell, and checks the status.
//
// Parameters:
//   - t: test to report to
//   - pre: option line such as "set -u", possibly empty
func checkExit(t *testing.T, pre string) {
	t.Helper()

	w := newWrapperEnv(t)
	w.setenv("FAKE_ACTION", "exit")
	w.setenv("FAKE_RC", "5")

	script := pre + Wrapper(w.kind) + "set +e\n( tote go; echo NOT-REACHED )\necho status=$?\n"

	out, errOut, code := w.run(t, w.shell, script)
	reached := strings.Contains(out, "NOT-REACHED")

	if code != 0 || reached || !strings.Contains(out, "status=5") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	if left := w.leftovers(t); len(left) != 0 {
		t.Errorf("hand-off files left behind: %q", left)
	}
}

// TestWrapperExit checks the exit action inside a subshell, with its status.
//
// Parameters:
//   - t: test to report to
func TestWrapperExit(t *testing.T) {
	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) { checkExit(t, pre.text) })
	}
}

// checkWrapperReload makes the fake tote set the reload flag with some action.
//
// Parameters:
//   - t: test to report to
//   - pre: option line such as "set -u", possibly empty
//   - action: action the fake tote leaves together with the flag
func checkWrapperReload(t *testing.T, pre, action string) {
	t.Helper()

	w := newWrapperEnv(t)
	w.setenv("FAKE_ACTION", action)
	w.setenv("FAKE_RELOAD", "1")
	w.setenv("FAKE_PAYLOAD", w.Dir)

	got, _ := w.call(t, pre, "tote go")
	if got["reloaded"] != "yes" || got["rc"] != "0" {
		t.Errorf("reloaded %q rc %q", got["reloaded"], got["rc"])
	}

	if left := w.leftovers(t); len(left) != 0 {
		t.Errorf("hand-off files left behind: %q", left)
	}
}

// TestWrapperReload checks that the reload flag evaluates the output of "init --defs" for the
// right shell, with every action.
//
// Parameters:
//   - t: test to report to
func TestWrapperReload(t *testing.T) {
	for _, pre := range preambles {
		for _, action := range []string{"none", "cd", "insert"} {
			name := pre.name + "/" + action
			t.Run(name, func(t *testing.T) { checkWrapperReload(t, pre.text, action) })
		}
	}

	w := newWrapperEnv(t)

	got, _ := w.call(t, "", "tote go")
	if got["reloaded"] != "no" {
		t.Errorf("reload without the flag: %q", got["reloaded"])
	}
}

// TestWrapperReloadFlagIsAWholeWord checks that only the flag reload triggers a reload, wherever
// it stands in the list of flags, and not a flag that merely starts or ends with it. A wrapper
// that a running shell has loaded cannot be replaced, so a flag added in a later release must
// not wake it up.
//
// Parameters:
//   - t: test to report to
func TestWrapperReloadFlagIsAWholeWord(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"v1 none,reload\n", "yes"},
		{"v1 none,x,reload\n", "yes"},
		{"v1 none,reload,x\n", "yes"},
		{"v1 insert,x,reload,y\nls\n", "yes"},
		{"v1 none,reload2\n", "no"},
		{"v1 none,reload-defs\n", "no"},
		{"v1 none,xreload\n", "no"},
		{"v1 none,x,reloaded\n", "no"},
		{"v1 none\n", "no"},
		{"v1 none,\n", "no"},
		{"v2 none,reload\n", "no"},
	}

	for _, pre := range preambles {
		for _, tt := range tests {
			t.Run(pre.name+"/"+strings.TrimSpace(tt.header), func(t *testing.T) {
				w := newWrapperEnv(t)
				w.setenv("FAKE_RAW", tt.header)

				got, _ := w.call(t, pre.text, "tote go")
				if got["reloaded"] != tt.want {
					t.Errorf("header %q: reloaded %q, want %q", tt.header, got["reloaded"], tt.want)
				}
			})
		}
	}
}

// TestWrapperEnvironment checks what the binary receives: its arguments untouched, a private
// hand-off file in the runtime directory and the shell name.
//
// Parameters:
//   - t: test to report to
func TestWrapperEnvironment(t *testing.T) {
	w := newWrapperEnv(t)

	w.call(t, "set -u\n", `tote 'a b' '' "it's" -- '$x'`)

	argv, err := os.ReadFile(filepath.Join(w.Dir, "argv"))
	if err != nil {
		t.Fatal(err)
	}

	if want := "a b\n\nit's\n--\n$x\n"; string(argv) != want {
		t.Errorf("argv = %q, want %q", argv, want)
	}

	env, err := os.ReadFile(filepath.Join(w.Dir, "env"))
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(string(env), "\n")
	if filepath.Dir(lines[0]) != w.runtime || !strings.HasPrefix(filepath.Base(lines[0]), "tote.") {
		t.Errorf("hand-off path = %q", lines[0])
	}

	if lines[1] != "bash" {
		t.Errorf("TOTE_SHELL = %q", lines[1])
	}
}

// TestWrapperHandoffMode checks that the file the binary receives is private, even under a
// permissive umask. A stand-in for the binary records the mode and the owner of the file.
//
// Parameters:
//   - t: test to report to
func TestWrapperHandoffMode(t *testing.T) {
	w := newWrapperEnv(t)

	// GNU stat takes -c, the BSD one of macOS takes -f, with an octal permission conversion.
	format := "-c '%a %u'"
	if runtime.GOOS == "darwin" {
		format = "-f '%Lp %u'"
	}

	stat := "#!/bin/sh\nstat " + format + " \"$TOTE_HANDOFF\" > \"$FAKE_DIR/mode\"\n"
	if err := os.WriteFile(filepath.Join(w.Bin, "tote"), []byte(stat), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, errOut, code := w.run(t, w.shell, "umask 000\n"+Wrapper(w.kind)+"tote go\n"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}

	want := "600 " + strconv.Itoa(os.Getuid()) + "\n"
	if got := readFile(t, filepath.Join(w.Dir, "mode")); got != want {
		t.Errorf("mode and owner = %q, want %q", got, want)
	}
}

// untrustedCase is one row of the table of hand-off files that the wrapper must ignore: the
// environment variables that make the fake tote produce it.
type untrustedCase struct {
	name string
	env  map[string]string
}

// checkUntrusted makes the fake tote leave an untrusted hand-off and checks that the shell is
// still there afterwards, with the temporary file removed.
//
// Parameters:
//   - t: test to report to
//   - w: wrapper environment shared by the cases
//   - tc: variables that make the fake tote produce the file
func checkUntrusted(t *testing.T, w *wrapperEnv, tc untrustedCase) {
	t.Helper()

	for k, v := range tc.env {
		w.setenv(k, v)
	}

	defer func() {
		for k := range tc.env {
			w.setenv(k, "")
		}
	}()

	out, errOut, code := w.run(t, w.shell, Wrapper(w.kind)+"( tote go; echo AFTER )\n")
	if code != 0 || !strings.Contains(out, "AFTER") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	if left := w.leftovers(t); len(left) != 0 {
		t.Errorf("hand-off files left behind: %q", left)
	}
}

// TestWrapperUntrustedFile checks the hand-off files that the wrapper must ignore: a symbolic
// link to a file that asks to exit, and headers it does not know.
//
// Parameters:
//   - t: test to report to
func TestWrapperUntrustedFile(t *testing.T) {
	w := newWrapperEnv(t)

	victim := filepath.Join(w.Dir, "victim")
	if err := os.WriteFile(victim, []byte("v1 exit\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []untrustedCase{
		{"symlink", map[string]string{"FAKE_LINK": victim}},
		{"other version", map[string]string{"FAKE_RAW": "v2 exit\n"}},
		{"no version", map[string]string{"FAKE_RAW": "exit\n"}},
		{"unknown action", map[string]string{"FAKE_RAW": "v1 run\nid"}},
		{"garbage", map[string]string{"FAKE_RAW": "$(touch PWNED)\n"}},
		{"prefix only", map[string]string{"FAKE_RAW": "v1exit\n"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkUntrusted(t, w, tc) })
	}

	if got := readFile(t, victim); got != "v1 exit\n" {
		t.Errorf("the link target was changed: %q", got)
	}

	if _, err := os.Stat(filepath.Join(w.Dir, "PWNED")); err == nil {
		t.Error("a hand-off was evaluated")
	}
}

// TestWrapperMissingHandoff checks a binary that leaves nothing, and a runtime directory and a
// TMPDIR that do not exist, where the hand-off falls back to /tmp.
//
// Parameters:
//   - t: test to report to
func TestWrapperMissingHandoff(t *testing.T) {
	w := newWrapperEnv(t)
	w.setenv("FAKE_RC", "3")

	got, _ := w.call(t, "set -eu\n", "tote go")
	if got["rc"] != "3" || got["pwd"] != w.Dir {
		t.Errorf("rc %q pwd %q", got["rc"], got["pwd"])
	}

	w.setenv("XDG_RUNTIME_DIR", filepath.Join(w.Dir, "absent"))
	w.setenv("TMPDIR", filepath.Join(w.Dir, "absent"))
	w.setenv("FAKE_ACTION", "cd")
	w.setenv("FAKE_PAYLOAD", w.Bin)

	got, _ = w.call(t, "set -eu\n", "tote go")
	if got["rc"] != "3" || got["pwd"] != w.Bin {
		t.Errorf("with the last resort directory: rc %q pwd %q", got["rc"], got["pwd"])
	}

	env, err := os.ReadFile(filepath.Join(w.Dir, "env"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(env), "/tmp/tote.") {
		t.Errorf("hand-off path = %q, want it under /tmp", env)
	}
}

// TestWrapperTempDirFallback checks the order XDG_RUNTIME_DIR, TMPDIR, /tmp.
//
// Parameters:
//   - t: test to report to
func TestWrapperTempDirFallback(t *testing.T) {
	w := newWrapperEnv(t)

	tmp := filepath.Join(w.Dir, "tmp")
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}

	w.setenv("XDG_RUNTIME_DIR", "")
	w.setenv("TMPDIR", tmp)
	w.call(t, "", "tote go")

	env, err := os.ReadFile(filepath.Join(w.Dir, "env"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(env), tmp+"/tote.") {
		t.Errorf("hand-off path = %q, want it under %s", env, tmp)
	}

	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("hand-off files left behind in TMPDIR: %d", len(left))
	}
}

// checkInsertSkipped makes the fake tote ask to insert a command that would leave a file behind.
//
// Parameters:
//   - t: test to report to
//   - pre: option line such as "set -u", possibly empty
func checkInsertSkipped(t *testing.T, pre string) {
	t.Helper()

	w := newWrapperEnv(t)
	w.setenv("FAKE_ACTION", "insert")
	w.setenv("FAKE_PAYLOAD", "touch "+filepath.Join(w.Dir, "RAN"))

	got, _ := w.call(t, pre, "tote go")
	if got["rc"] != "0" {
		t.Errorf("rc = %q", got["rc"])
	}

	if _, err := os.Stat(filepath.Join(w.Dir, "RAN")); err == nil {
		t.Error("the inserted command ran in a non interactive shell")
	}
}

// TestWrapperInsertNeedsTerminal checks that the insert action does nothing when the shell is
// not interactive, instead of running the payload.
//
// Parameters:
//   - t: test to report to
func TestWrapperInsertNeedsTerminal(t *testing.T) {
	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) { checkInsertSkipped(t, pre.text) })
	}
}

// checkNoLeak compares the variable names of the shell before and after a call.
//
// Parameters:
//   - t: test to report to
//   - pre: option line such as "set -u", possibly empty
func checkNoLeak(t *testing.T, pre string) {
	t.Helper()

	w := newWrapperEnv(t)
	w.setenv("FAKE_ACTION", "cd")
	w.setenv("FAKE_PAYLOAD", w.Bin)

	script := pre + Wrapper(w.kind) +
		"set -- keep these\nrc=0 after= before= OLDPWD=\n" +
		"before=$(compgen -v | sort | tr '\\n' ' ')\n" +
		"tote go && rc=0 || rc=$?\n" +
		"after=$(compgen -v | sort | tr '\\n' ' ')\n" +
		"[ \"$before\" = \"$after\" ] && echo SAME || echo \"DIFF: $after\"\n" +
		"echo \"params=$*\"\n"

	out, errOut, code := w.run(t, w.shell, script)
	if code != 0 || out != "SAME\nparams=keep these\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// TestWrapperNoLeak checks that the function leaves no variable behind except what a reload
// defines, and does not change the positional parameters of the caller.
//
// Parameters:
//   - t: test to report to
func TestWrapperNoLeak(t *testing.T) {
	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) { checkNoLeak(t, pre.text) })
	}
}

// TestWrapperDoesNotUseAliases checks that an alias for rm, cd or mktemp cannot change the
// behavior of the function, even in a shell that expands aliases.
//
// Parameters:
//   - t: test to report to
func TestWrapperDoesNotUseAliases(t *testing.T) {
	w := newWrapperEnv(t)
	w.setenv("FAKE_ACTION", "cd")
	w.setenv("FAKE_PAYLOAD", w.Bin)

	script := "shopt -s expand_aliases\n" +
		"alias rm='echo ALIASED-RM'\nalias cd='echo ALIASED-CD'\n" +
		"alias mktemp='echo ALIASED-MKTEMP'\n" +
		"alias read='echo ALIASED-READ'\nalias eval='echo ALIASED-EVAL'\n" +
		Wrapper(w.kind) + "tote go\nprintf 'pwd=%s\\n' \"$PWD\"\n"

	out, errOut, code := w.run(t, w.shell, script)
	if code != 0 || strings.Contains(out, "ALIASED") || !strings.Contains(out, "pwd="+w.Bin) {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	if left := w.leftovers(t); len(left) != 0 {
		t.Errorf("hand-off files left behind: %q", left)
	}
}

// TestWrapperTwoInvocationsDoNotShare checks that two runs use two different files.
//
// Parameters:
//   - t: test to report to
func TestWrapperTwoInvocationsDoNotShare(t *testing.T) {
	w := newWrapperEnv(t)

	paths := map[string]bool{}

	for range 2 {
		w.call(t, "", "tote go")

		env, err := os.ReadFile(filepath.Join(w.Dir, "env"))
		if err != nil {
			t.Fatal(err)
		}

		paths[strings.SplitN(string(env), "\n", 2)[0]] = true
	}

	if len(paths) != 2 {
		t.Errorf("the same hand-off path was used twice: %v", paths)
	}
}

// TestWrapperCdIgnoresCDPATH checks that a relative directory is entered from the current
// directory and never searched in CDPATH, which would land in another directory than the one
// that was checked.
//
// Parameters:
//   - t: test to report to
func TestWrapperCdIgnoresCDPATH(t *testing.T) {
	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) {
			w := newWrapperEnv(t)

			work := filepath.Join(w.Dir, "work")
			want := filepath.Join(work, "rel", "dir")
			other := filepath.Join(w.Dir, "other")

			for _, d := range []string{want, filepath.Join(other, "rel", "dir")} {
				if err := os.MkdirAll(d, 0o700); err != nil {
					t.Fatal(err)
				}
			}

			w.setenv("CDPATH", other)
			w.setenv("FAKE_ACTION", "cd")
			w.setenv("FAKE_PAYLOAD", "rel/dir")

			got, stderr := w.call(t, pre.text+"cd "+Quote(work)+"\n", "tote go")
			if got["pwd"] != want || got["rc"] != "0" || stderr != "" {
				t.Errorf("pwd %q rc %q stderr %q, want %q", got["pwd"], got["rc"], stderr, want)
			}
		})
	}
}

// TestWrapperTempDirUnusable checks that an unusable XDG_RUNTIME_DIR falls through to TMPDIR,
// quietly, and that the hand-off still works.
//
// Parameters:
//   - t: test to report to
func TestWrapperTempDirUnusable(t *testing.T) {
	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) {
			w := newWrapperEnv(t)

			tmp := filepath.Join(w.Dir, "tmp")
			if err := os.Mkdir(tmp, 0o700); err != nil {
				t.Fatal(err)
			}

			target := filepath.Join(w.Dir, "target")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}

			w.setenv("XDG_RUNTIME_DIR", filepath.Join(w.Dir, "missing"))
			w.setenv("TMPDIR", tmp)
			w.setenv("FAKE_ACTION", "cd")
			w.setenv("FAKE_PAYLOAD", target)

			got, stderr := w.call(t, pre.text, "tote go")
			if got["pwd"] != target || got["rc"] != "0" || stderr != "" {
				t.Errorf("pwd %q rc %q stderr %q, want %q", got["pwd"], got["rc"], stderr, target)
			}

			env, err := os.ReadFile(filepath.Join(w.Dir, "env"))
			if err != nil {
				t.Fatal(err)
			}

			if !strings.HasPrefix(string(env), tmp+"/tote.") {
				t.Errorf("hand-off path = %q, want it under %s", env, tmp)
			}
		})
	}
}

// TestWrapperNoTempDir checks that when no temporary file can be made the function says so once
// on standard error, still runs the binary without a hand-off, and returns its exit code.
//
// Parameters:
//   - t: test to report to
func TestWrapperNoTempDir(t *testing.T) {
	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) {
			w := newWrapperEnv(t)

			failing := "#!/bin/sh\nexit 1\n"
			fake := filepath.Join(w.Bin, "mktemp")

			if err := os.WriteFile(fake, []byte(failing), 0o700); err != nil {
				t.Fatal(err)
			}

			w.setenv("FAKE_RC", "3")

			got, stderr := w.call(t, pre.text, "tote go")
			if got["rc"] != "3" {
				t.Errorf("rc = %q, want 3", got["rc"])
			}

			if n := strings.Count(stderr, "no usable temporary directory"); n != 1 {
				t.Errorf("the message appeared %d times in %q", n, stderr)
			}

			env, err := os.ReadFile(filepath.Join(w.Dir, "env"))
			if err != nil {
				t.Fatalf("the binary did not run: %v", err)
			}

			if lines := strings.Split(string(env), "\n"); lines[0] != "" {
				t.Errorf("TOTE_HANDOFF = %q, want it unset", lines[0])
			}
		})
	}
}
