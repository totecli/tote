package shell

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// update rewrites the golden files of testdata/ instead of comparing with them. The files are
// the expected output of Init for fixtureDefs: init_bash.golden and init_zsh.golden. They were
// written on purpose by running "go test -run Golden -update" and are reviewed by hand.
var update = flag.Bool("update", false, "rewrite the golden files")

// fixtureDefs returns records that cover every branch of the generator: plain, hostile and
// invalid values, duplicates, reserved names, both function kinds, a path to quote and extras.
func fixtureDefs() Defs {
	return Defs{
		Aliases: []store.Alias{
			{Name: "ll", Cmd: "ls -la"},
			{Name: "say", Cmd: "echo 'hello world'"},
			{Name: "evil", Cmd: "echo $(touch PWNED); `id` \\ \"q\" && x || y # z"},
			{Name: "uni", Cmd: "echo caf\u00e9 \u65e5\u672c"},
			{Name: "bad name", Cmd: "x"},
			{Name: "x=y", Cmd: "x"},
			{Name: "-n", Cmd: "x"},
			{Name: "tote", Cmd: "x"},
			{Name: "mn", Cmd: "x"},
			{Name: "eval", Cmd: "x"},
			{Name: "empty", Cmd: ""},
			{Name: "multi", Cmd: "a\nb"},
			{Name: "line\nbreak; touch PWNED", Cmd: "x"},
			{Name: "ll", Cmd: "ls -l"},
		},
		Repos: []store.Repo{
			{Name: "proj", Path: "/home/u/proj"},
			{Name: "my.repo", Path: "~/code/my.repo"},
			{Name: "ll", Path: "/x"},
			{Name: "it's", Path: "/x"},
			{Name: "nopath", Path: ""},
		},
		Funcs: []store.Func{
			{Name: "gs", Kind: store.KindSource, Script: "/home/u/.local/share/tote/bin/gs.sh"},
			{Name: "spaced", Kind: store.KindSource, Script: "/home/u/my scripts/it's.sh"},
			{Name: "tilde", Kind: store.KindSource, Script: "~/bin/tool.sh"},
			{Name: "deploy", Kind: store.KindExec, Script: "/home/u/bin/deploy"},
			{Name: "1bad", Kind: store.KindSource, Script: "/x"},
			{Name: "bad kind", Kind: "other", Script: "/x"},
			{Name: "source", Kind: store.KindSource, Script: "/x"},
			{Name: "ex-bad name", Kind: store.KindExec, Script: "/x"},
			{Name: "noexec", Kind: store.KindExec, Script: ""},
			{Name: "gs", Kind: store.KindSource, Script: "/x"},
			{Name: "noscript", Kind: store.KindSource, Script: ""},
		},
		BinDir:     "/home/u/.local/share/tote/bin",
		ExtraNames: []string{"mn", "bad name", "tote", "gs", "deploy", "-x", "mn"},
	}
}

// checkGolden compares the init output for one shell with its golden file.
//
// Parameters:
//   - t: test to report to
//   - k: shell whose output is checked
func checkGolden(t *testing.T, k Kind) {
	t.Helper()

	file := filepath.Join("testdata", "init_"+string(k)+".golden")
	got := Init(k, fixtureDefs())

	if *update {
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	if got != string(want) {
		t.Errorf("%s: output differs from %s (run with -update after reviewing)\n%s", k, file, got)
	}
}

// TestInitGolden compares the whole init output with the golden files.
//
// Parameters:
//   - t: test to report to
func TestInitGolden(t *testing.T) {
	checkGolden(t, Bash)
	checkGolden(t, Zsh)
}

// TestInitDeterministic checks that two calls give the same bytes and that the wrapper comes
// first, before any record.
//
// Parameters:
//   - t: test to report to
func TestInitDeterministic(t *testing.T) {
	for _, k := range []Kind{Bash, Zsh} {
		a, b := Init(k, fixtureDefs()), Init(k, fixtureDefs())
		if a != b {
			t.Errorf("%s: two calls differ", k)
		}

		if !strings.HasPrefix(a, Wrapper(k)) || a != Wrapper(k)+Definitions(k, fixtureDefs()) {
			t.Errorf("%s: output is not wrapper then definitions", k)
		}

		if !strings.HasPrefix(a, "tote() {\n") || !strings.HasSuffix(a, "\n") {
			t.Errorf("%s: unexpected start or end", k)
		}
	}
}

// TestInitEmpty checks the output for no record at all.
//
// Parameters:
//   - t: test to report to
func TestInitEmpty(t *testing.T) {
	for _, k := range []Kind{Bash, Zsh} {
		out := Definitions(k, Defs{})

		if strings.Contains(out, "alias ") && !strings.Contains(out, "unalias") {
			t.Errorf("%s: unexpected alias", k)
		}

		if strings.Contains(out, "PATH=") || strings.Contains(out, "# tote:") {
			t.Errorf("%s: path or comment without input:\n%s", k, out)
		}

		if !strings.HasSuffix(out, "__TOTE_DEFS=''\n") {
			t.Errorf("%s: tracking variable: %q", k, out)
		}
	}
}

// TestInitSkips checks the comment line for each kind of refused record.
//
// Parameters:
//   - t: test to report to
func TestInitSkips(t *testing.T) {
	out := Definitions(Bash, fixtureDefs())

	want := []string{
		"# tote: alias 'bad name' skipped (invalid name)",
		"# tote: alias 'x=y' skipped (invalid name)",
		"# tote: alias '-n' skipped (invalid name)",
		"# tote: alias 'tote' skipped (invalid name)",
		"# tote: alias 'mn' skipped (invalid name)",
		"# tote: alias 'eval' skipped (reserved name)",
		"# tote: alias 'empty' skipped (invalid value)",
		"# tote: alias 'multi' skipped (invalid value)",
		"# tote: alias 'line?break; touch PWNED' skipped (invalid name)",
		"# tote: alias 'll' skipped (duplicate name)",
		"# tote: repo 'll' skipped (duplicate name)",
		"# tote: repo 'it?s' skipped (invalid name)",
		"# tote: repo 'nopath' skipped (invalid value)",
		"# tote: func '1bad' skipped (invalid name)",
		"# tote: func 'bad kind' skipped (invalid name)",
		"# tote: func 'source' skipped (reserved name)",
		"# tote: func 'ex-bad name' skipped (invalid name)",
		"# tote: func 'gs' skipped (duplicate name)",
		"# tote: func 'noscript' skipped (invalid value)",
		"# tote: func 'noexec' skipped (invalid value)",
		"# tote: extra name 'bad name' skipped (invalid name)",
		"# tote: extra name 'tote' skipped (reserved name)",
		"# tote: extra name 'gs' skipped (duplicate name)",
		"# tote: extra name 'deploy' skipped (duplicate name)",
		"# tote: extra name '-x' skipped (invalid name)",
		"# tote: extra name 'mn' skipped (duplicate name)",
	}

	for _, line := range want {
		if !strings.Contains(out+"\n", line+"\n") {
			t.Errorf("missing line %q", line)
		}
	}
}

// TestInitOnlyKnownLines checks that no line of the output starts with something other than the
// shapes the generator writes, so that data can never open a new statement.
//
// Parameters:
//   - t: test to report to
func TestInitOnlyKnownLines(t *testing.T) {
	for _, k := range []Kind{Bash, Zsh} {
		for _, line := range strings.Split(Definitions(k, fixtureDefs()), "\n") {
			if strings.Contains(line, "PWNED") && !strings.HasPrefix(line, "alias ") &&
				!strings.HasPrefix(line, "# tote: ") {
				t.Errorf("%s: unexpected line %q", k, line)
			}

			if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "# tote: ") {
				t.Errorf("%s: odd comment %q", k, line)
			}

			if strings.HasPrefix(line, "# tote: ") && strings.ContainsAny(line, "\r\x00") {
				t.Errorf("%s: control character in %q", k, line)
			}
		}
	}
}

// evalScript builds a bash script that evaluates definitions with aliases enabled and a stub
// tote function, then runs a tail.
//
// Parameters:
//   - defs: definitions text
//   - tail: commands to run after the evaluation, on later lines so that aliases apply
func evalScript(defs, tail string) string {
	return "shopt -s expand_aliases\ntote() { printf '<%s>' \"$@\"; echo; }\n" +
		"eval " + Quote(defs) + "\n" + tail
}

// checkRunsInBash evaluates the fixture definitions in a real bash under some shell options.
//
// Parameters:
//   - t: test to report to
//   - sh: path of bash
//   - pre: option line such as "set -u", possibly empty
func checkRunsInBash(t *testing.T, sh, pre string) {
	t.Helper()

	sb := newSandbox(t)
	defs := Definitions(Bash, fixtureDefs())

	tail := "echo \"ALIASES=$(printf '%s\\n' \"${!BASH_ALIASES[@]}\" | sort | tr '\\n' ' ')\"\n" +
		"echo \"FUNCS=$(declare -F | sed 's/^declare -f //' | sort | tr '\\n' ' ')\"\n" +
		"printf 'PATH=%s\\n' \"$PATH\"\n" +
		"printf 'DEFS=%s\\n' \"$__TOTE_DEFS\"\n" +
		"printf 'EVIL=%s\\n' \"${BASH_ALIASES[evil]}\"\n" +
		"printf 'UNI=%s\\n' \"${BASH_ALIASES[uni]}\"\n" +
		"printf 'LL=%s\\n' \"${BASH_ALIASES[ll]}\"\n" +
		"printf 'SAY=%s\\n' \"${BASH_ALIASES[say]}\"\n" +
		"printf 'REPO=%s\\n' \"${BASH_ALIASES[my.repo]}\"\n"

	out, errOut, code := sb.run(t, sh, pre+evalScript(defs, tail))
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}

	for _, want := range []string{
		"ALIASES=evil ll my.repo proj say uni \n",
		"FUNCS=__tote_complete gs mn spaced tilde tote \n",
		"EVIL=echo $(touch PWNED); `id` \\ \"q\" && x || y # z\n",
		"UNI=echo caf\u00e9 \u65e5\u672c\n",
		"LL=ls -la\n",
		"SAY=echo 'hello world'\n",
		"REPO=tote repo open my.repo\n",
		"PATH=/home/u/.local/share/tote/bin:",
		"DEFS=a:ll a:say a:evil a:uni a:proj a:my.repo f:gs f:spaced f:tilde f:mn\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	if _, err := os.Stat(filepath.Join(sb.Dir, "PWNED")); err == nil {
		t.Error("a record was executed")
	}
}

// TestInitRunsInBash evaluates the golden fixture in a real bash, with the options set, and
// checks the aliases, the functions, the PATH entry and the absence of side effects.
//
// Parameters:
//   - t: test to report to
func TestInitRunsInBash(t *testing.T) {
	sh := needShell(t, "bash")

	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) { checkRunsInBash(t, sh, pre.text) })
	}
}

// TestAliasValuesRoundTrip defines an alias for each hostile command and compares what bash
// stored with what was given.
//
// Parameters:
//   - t: test to report to
func TestAliasValuesRoundTrip(t *testing.T) {
	sh := needShell(t, "bash")
	sb := newSandbox(t)

	var cmds []string

	for _, s := range append(append([]string{}, hostileStrings...), randomStrings(300)...) {
		if strings.TrimSpace(s) != "" && !strings.ContainsAny(s, "\x00\n\r") {
			cmds = append(cmds, s)
		}
	}

	var d Defs

	for i, c := range cmds {
		d.Aliases = append(d.Aliases, store.Alias{Name: "a" + strconv.Itoa(i), Cmd: c})
	}

	tail := "for n in \"${!BASH_ALIASES[@]}\"; do\n" +
		"printf '%s\\0%s\\0' \"$n\" \"${BASH_ALIASES[$n]}\"\ndone\n"

	out, errOut, code := sb.run(t, sh, evalScript(Definitions(Bash, d), tail))
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}

	parts := strings.Split(out, "\x00")
	got := map[string]string{}

	for i := 0; i+1 < len(parts); i += 2 {
		got[parts[i]] = parts[i+1]
	}

	for i, c := range cmds {
		a := d.Aliases[i]
		if a.Validate() != nil {
			if _, ok := got[a.Name]; ok {
				t.Errorf("invalid record %q was defined", a.Name)
			}

			continue
		}

		if got[a.Name] != c {
			t.Errorf("alias %s: got %q, want %q", a.Name, got[a.Name], c)
		}
	}

	if _, err := os.Stat(filepath.Join(sb.Dir, "PWNED")); err == nil {
		t.Error("an alias value was executed")
	}
}

// TestAliasNamesAreSafe feeds hostile alias names and checks that the shell state and the
// filesystem are the same as with no record.
//
// Parameters:
//   - t: test to report to
func TestAliasNamesAreSafe(t *testing.T) {
	sh := needShell(t, "bash")
	sb := newSandbox(t)

	names := []string{
		"a;touch PWNED", "a b", "a\ntouch PWNED", "a`touch PWNED`", "a$(touch PWNED)", "a'b",
		"a\"b", "a|b", "a&b", "a>b", "a<b", "(a)", "{a}", "a#b", "a\\b", "a\tb", "\x1b[31m",
		"a\x00b", "", "-", "-a", "a=b", "a/b", "~a", "!a", "a*", "a?", "a[1]", "\u202e", "a\u2028b",
		strings.Repeat("n", 65),
	}

	var d Defs

	for _, n := range names {
		d.Aliases = append(d.Aliases, store.Alias{Name: n, Cmd: "true"})
		d.Repos = append(d.Repos, store.Repo{Name: n, Path: "/x"})
		d.Funcs = append(d.Funcs, store.Func{Name: n, Kind: store.KindSource, Script: "/x"})
	}

	d.ExtraNames = names
	d.Aliases = append(d.Aliases, store.Alias{Name: "fine", Cmd: "echo ok"})
	defs := Definitions(Bash, d)

	tail := "printf 'N=%s\\n' \"${#BASH_ALIASES[@]}\"\nfine\n"

	out, errOut, code := sb.run(t, sh, evalScript(defs, tail))
	if code != 0 || errOut != "" || out != "N=1\nok\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	if _, err := os.Stat(filepath.Join(sb.Dir, "PWNED")); err == nil {
		t.Error("a name was executed")
	}

	for _, line := range strings.Split(defs, "\n") {
		if line != "" && !strings.HasPrefix(line, "# tote: ") && strings.Contains(line, "touch") {
			t.Errorf("a hostile name reached the output outside a comment: %q", line)
		}
	}
}

// TestSourceFunctions checks the generated functions: they source their script with the
// arguments, through a path that needs quoting.
//
// Parameters:
//   - t: test to report to
func TestSourceFunctions(t *testing.T) {
	sh := needShell(t, "bash")
	sb := newSandbox(t)

	script := filepath.Join(sb.Dir, "my scripts", "it's $x.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}

	body := "echo \"sourced:$#:$1:$2\"\nSOURCED=yes\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	d := Defs{Funcs: []store.Func{{Name: "gs", Kind: store.KindSource, Script: script}}}
	tail := "gs 'a b' c\necho \"SOURCED=$SOURCED\"\n"

	out, errOut, code := sb.run(t, sh, evalScript(Definitions(Bash, d), tail))
	if code != 0 || errOut != "" || out != "sourced:2:a b:c\nSOURCED=yes\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// TestFunctionReplacesAlias checks that a source function wins over an alias of the same name that
// the user had, which would otherwise be a syntax error.
//
// Parameters:
//   - t: test to report to
func TestFunctionReplacesAlias(t *testing.T) {
	sh := needShell(t, "bash")
	sb := newSandbox(t)

	d := Defs{Funcs: []store.Func{{Name: "gs", Kind: store.KindSource, Script: "/dev/null"}}}
	script := "shopt -s expand_aliases\nalias gs='echo OLD'\n" +
		"eval " + Quote(Definitions(Bash, d)) + "\ntype -t gs\n"

	out, errOut, code := sb.run(t, sh, script)
	if code != 0 || errOut != "" || out != "function\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// checkDefsReload loads two sets of definitions one after the other in a real bash.
//
// Parameters:
//   - t: test to report to
//   - sh: path of bash
//   - pre: option line such as "set -u", possibly empty
func checkDefsReload(t *testing.T, sh, pre string) {
	t.Helper()

	sb := newSandbox(t)

	first := Definitions(Bash, Defs{
		Aliases:    []store.Alias{{Name: "one", Cmd: "true"}, {Name: "two", Cmd: "true"}},
		Repos:      []store.Repo{{Name: "rp", Path: "/x"}},
		Funcs:      []store.Func{{Name: "fn", Kind: store.KindSource, Script: "/dev/null"}},
		ExtraNames: []string{"ex"},
	})
	second := Definitions(Bash, Defs{Aliases: []store.Alias{{Name: "two", Cmd: "false"}}})

	report := "echo \"$1:$(printf '%s\\n' \"${!BASH_ALIASES[@]}\" | sort | tr '\\n' ' ')|" +
		"$(declare -F | grep -c ' \\(fn\\|ex\\)$' || true)\"\n"

	script := pre + "shopt -s expand_aliases\nalias mine='echo mine'\nset -- 1\n" +
		"eval " + Quote(first) + "\n" + report +
		"set -- 2\neval " + Quote(second) + "\n" + report +
		"set -- 3\neval " + Quote(second) + "\n" + report +
		"echo \"two=${BASH_ALIASES[two]} mine=${BASH_ALIASES[mine]}\"\n" +
		"echo \"leaked=$(compgen -v | grep -c '^__tote_' || true)\"\n"

	out, errOut, code := sb.run(t, sh, script)
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}

	want := "1:mine one rp two |2\n2:mine two |0\n3:mine two |0\n" +
		"two=false mine=echo mine\nleaked=0\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestReloadRemovesDeleted checks that evaluating a second set of definitions removes the
// aliases and functions of the first one that are no longer defined, and keeps the user's own.
//
// Parameters:
//   - t: test to report to
func TestReloadRemovesDeleted(t *testing.T) {
	sh := needShell(t, "bash")

	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) { checkDefsReload(t, sh, pre.text) })
	}
}

// TestPathEntry checks that the script directory is added once, in front, whatever the number
// of reloads, and that a hostile directory is refused.
//
// Parameters:
//   - t: test to report to
func TestPathEntry(t *testing.T) {
	sh := needShell(t, "bash")
	sb := newSandbox(t)

	dir := filepath.Join(sb.Dir, "it's a $dir", "bin")
	defs := Definitions(Bash, Defs{BinDir: dir})

	script := "before=$PATH\n" + "eval " + Quote(defs) + "\neval " + Quote(defs) + "\n" +
		"printf '%s\\n' \"$PATH\"\nprintf '%s\\n' \"${PATH#\"$before\"}\" >/dev/null\n"

	out, errOut, code := sb.run(t, sh, script)
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}

	want := dir + ":" + sb.Bin + ":/usr/bin:/bin\n"
	if out != want {
		t.Errorf("PATH = %q, want %q", out, want)
	}

	for _, bad := range []string{"relative/bin", "/a:/b", "/a\n/b", "/a\x00b", "~/bin"} {
		out := Definitions(Bash, Defs{BinDir: bad})
		if strings.Contains(out, "PATH=") || !strings.Contains(out, "# tote: bin dir ") {
			t.Errorf("BinDir %q was not refused:\n%s", bad, out)
		}
	}
}

// TestExtraNames checks that an extra name becomes a function that calls the wrapper.
//
// Parameters:
//   - t: test to report to
func TestExtraNames(t *testing.T) {
	sh := needShell(t, "bash")
	sb := newSandbox(t)

	d := Defs{ExtraNames: []string{"mn", "t2"}}
	tail := "mn 'a b' c\nt2\ntype -t mn\n"

	out, errOut, code := sb.run(t, sh, evalScript(Definitions(Bash, d), tail))
	if code != 0 || errOut != "" || out != "<a b><c>\n<>\nfunction\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// TestCompletionBash checks the bash completion function against a fake binary that answers
// "__complete" with candidates and descriptions.
//
// Parameters:
//   - t: test to report to
func TestCompletionBash(t *testing.T) {
	sh := needShell(t, "bash")
	sb := newSandbox(t)

	fake := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$FAKE_DIR/complete-args\"\n" +
		"printf 'web-1\\tProduction web\\nweb-2\\nit'\"'\"'s a name\\tdesc: with colon\\n'\n"
	if err := os.WriteFile(filepath.Join(sb.Bin, "tote"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}

	sb.setenv("FAKE_DIR", sb.Dir)

	d := Defs{ExtraNames: []string{"mn"}}
	tail := "complete -p tote mn\n" +
		"COMP_WORDS=(tote connect we) COMP_CWORD=2\n__tote_complete\n" +
		"printf '[%s]' \"${COMPREPLY[@]}\"; echo\n"

	out, errOut, code := sb.run(t, sh, "set -u\n"+evalScript(Definitions(Bash, d), tail))
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}

	want := "complete -o default -F __tote_complete tote\n" +
		"complete -o default -F __tote_complete mn\n[web-1][web-2][it's a name]\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}

	got := readFile(t, filepath.Join(sb.Dir, "complete-args"))
	if got != "__complete connect we\n" {
		t.Errorf("binary arguments = %q", got)
	}
}

// TestCompletionZshText checks the zsh completion text, which cannot run here: it escapes colons
// and registers only when compsys is loaded.
//
// Parameters:
//   - t: test to report to
func TestCompletionZshText(t *testing.T) {
	out := Definitions(Zsh, Defs{ExtraNames: []string{"mn"}})

	for _, want := range []string{
		"_tote() {", `${name//:/\\:}`, "_describe -t tote tote items",
		"if (( $+functions[compdef] )); then compdef _tote tote mn; fi",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}

	if strings.Contains(out, "complete -o") || strings.Contains(out, "COMPREPLY") {
		t.Error("bash completion in the zsh output")
	}
}

// TestParseKind checks the accepted and refused shell names.
//
// Parameters:
//   - t: test to report to
func TestParseKind(t *testing.T) {
	for name, want := range map[string]Kind{"bash": Bash, "zsh": Zsh} {
		if got, err := ParseKind(name); err != nil || got != want {
			t.Errorf("ParseKind(%q) = %q, %v", name, got, err)
		}
	}

	bad := []string{"", "fish", "sh", "dash", "BASH", " bash", "bash\n", "/bin/bash"}

	for _, name := range bad {
		got, err := ParseKind(name)
		if !errors.Is(err, ErrUnsupported) || got != "" {
			t.Errorf("ParseKind(%q) = %q, %v", name, got, err)
		}
	}
}

// TestDetectKind checks the shell kind found from $SHELL.
//
// Parameters:
//   - t: test to report to
func TestDetectKind(t *testing.T) {
	cases := []struct {
		path string
		want Kind
		ok   bool
	}{
		{"/bin/bash", Bash, true},
		{"/usr/bin/zsh", Zsh, true},
		{"/opt/homebrew/bin/zsh", Zsh, true},
		{"bash", Bash, true},
		{"-bash", Bash, true},
		{"/usr/local/bin/bash/", Bash, true},
		{"", "", false},
		{"/", "", false},
		{"/usr/bin/fish", "", false},
		{"/bin/sh", "", false},
		{"/bin/bash5", "", false},
		{"/bin/zsh.exe", "", false},
	}

	for _, tc := range cases {
		got, err := DetectKind(tc.path)
		if got != tc.want || (err == nil) != tc.ok {
			t.Errorf("DetectKind(%q) = %q, %v", tc.path, got, err)
		}
	}
}

// TestScriptWord checks the quoting of a script path, with and without a home prefix.
//
// Parameters:
//   - t: test to report to
func TestScriptWord(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/a/b.sh", `'/a/b.sh'`},
		{"~", `"$HOME"`},
		{"~/s/it's.sh", `"$HOME"/'s/it'\''s.sh'`},
		{"~root/x", `'~root/x'`},
		{"a~/b", `'a~/b'`},
		{"$HOME/x", `'$HOME/x'`},
	}

	for _, tc := range cases {
		if got := scriptWord(tc.in); got != tc.want {
			t.Errorf("scriptWord(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	sh := needShell(t, "bash")
	sb := newSandbox(t)

	script := filepath.Join(sb.Dir, "my dir", "s.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(script, []byte("echo home-script\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := Defs{Funcs: []store.Func{{Name: "hs", Kind: store.KindSource, Script: "~/my dir/s.sh"}}}

	out, errOut, code := sb.run(t, sh, evalScript(Definitions(Bash, d), "hs\n"))
	if code != 0 || errOut != "" || out != "home-script\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// TestReservedNames checks the names that no record may take: the program, the commands that the
// generated code runs, its helpers and the reserved words of both shells.
//
// Parameters:
//   - t: test to report to
func TestReservedNames(t *testing.T) {
	for _, name := range []string{
		"tote", "eval", ":", "set", "printf", "complete", "compdef", "_describe", "_tote",
		"__tote_unload", "__tote_complete", "if", "done", "function", "coproc", "foreach",
		"__tote_pick", "__tote_", "_tote_x", "_toteX", "__TOTE_Bind",
	} {
		if !Reserved(name) {
			t.Errorf("Reserved(%q) = false", name)
		}
	}

	for _, name := range []string{"ll", "hello", "gs", "iff", "Done", "edit", "_tot", "x_tote"} {
		if Reserved(name) {
			t.Errorf("Reserved(%q) = true", name)
		}
	}
}

// TestValidScript checks the script paths that a source function may use.
//
// Parameters:
//   - t: test to report to
func TestValidScript(t *testing.T) {
	for script, want := range map[string]bool{
		"/opt/x.sh": true, "~": true, "~/x.sh": true, "helpers.sh": false, "./x.sh": false,
		"-p": false, "~root/x": false, "": false,
	} {
		if got := ValidScript(script); got != want {
			t.Errorf("ValidScript(%q) = %v, want %v", script, got, want)
		}
	}
}

// TestDefinitionsSkipRelativeScript checks that a source function with a script that depends on
// the current directory leaves a comment and no function.
//
// Parameters:
//   - t: test to report to
func TestDefinitionsSkipRelativeScript(t *testing.T) {
	f := store.Func{Name: "hello", Kind: store.KindSource, Script: "helpers.sh"}
	text := Definitions(Bash, Defs{Funcs: []store.Func{f}})

	if !strings.Contains(text, "# tote: func 'hello' skipped (invalid value)") ||
		strings.Contains(text, "hello()") {
		t.Errorf("text = %q", text)
	}
}

// TestPathHelpers checks the expansion of "~" and "~/", and nothing else, and the test of a path
// that does not depend on the current directory.
//
// Parameters:
//   - t: test to report to
func TestPathHelpers(t *testing.T) {
	tests := []struct{ path, home, want string }{
		{"~", "/h", "/h"},
		{"~/a b", "/h", "/h/a b"},
		{"~x/a", "/h", "~x/a"},
		{"/a/~", "/h", "/a/~"},
		{"~/a", "", "~/a"},
		{"$HOME/a", "/h", "$HOME/a"},
		{"rel", "/h", "rel"},
	}

	for _, tt := range tests {
		if got := ExpandHome(tt.path, tt.home); got != tt.want {
			t.Errorf("ExpandHome(%q, %q) = %q, want %q", tt.path, tt.home, got, tt.want)
		}
	}

	for path, want := range map[string]bool{
		"~": true, "~/a": true, "/a": true, "a": false,
		"~a": false, "": false,
	} {
		if got := AbsOrHome(path); got != want {
			t.Errorf("AbsOrHome(%q) = %v, want %v", path, got, want)
		}
	}
}
