package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// loadInBash writes the init text to the sandbox and evaluates it twice the way a startup file
// and a later reload would, with alias expansion on as in an interactive shell.
//
// Parameters:
//   - t: test to report to
//   - s: sandbox to run in
//   - text: output of Init
//   - tail: shell commands run after the two evaluations
//
// Returns standard output and standard error.
func loadInBash(t *testing.T, s *sandbox, text, tail string) (string, string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(s.Dir, "init.sh"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	script := "shopt -s expand_aliases\n" +
		"eval \"$(cat init.sh)\"\n" +
		"eval \"$(cat init.sh)\"\n" + tail
	stdout, stderr, _ := s.run(t, needShell(t, "bash"), script)

	return stdout, stderr
}

// firstLine returns the first line of a text.
//
// Parameters:
//   - text: text to cut
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")

	return line
}

// TestReviewKeywordFunctionName checks that a function record named like a shell keyword, which
// the store accepts, cannot break the evaluation of the rest of the init output.
//
// Parameters:
//   - t: test to report to
func TestReviewKeywordFunctionName(t *testing.T) {
	words := []string{
		"if", "then", "else", "elif", "fi", "case", "esac", "for", "select", "while", "until",
		"do", "done", "in", "function", "time", "coproc",
	}

	for _, w := range words {
		f := store.Func{Name: w, Kind: store.KindSource, Script: "/nonexistent/x.sh"}
		if err := f.Validate(); err != nil {
			continue
		}

		s := newSandbox(t)
		text := Init(Bash, Defs{Funcs: []store.Func{f}, BinDir: s.Dir + "/bin"})
		stdout, stderr := loadInBash(t, s, text, "echo \"defs=${__TOTE_DEFS-unset}\"\n")

		if stderr != "" || strings.Contains(stdout, "defs=unset") {
			t.Errorf("func %q breaks the init output: stdout %q first stderr line %q", w,
				stdout, firstLine(stderr))
		}
	}
}

// TestReviewHelperNamesCannotHijackInit checks that an alias record cannot take the name of a
// command that the generated code itself runs, which would make the alias run while the init
// output is evaluated, or break the redefinition of the wrapper and of the helpers.
//
// Parameters:
//   - t: test to report to
func TestReviewHelperNamesCannotHijackInit(t *testing.T) {
	names := []string{"complete", "set", "printf", ":", "__tote_unload", "__tote_complete"}

	for _, name := range names {
		s := newSandbox(t)
		marker := filepath.Join(s.Dir, "marker")
		alias := store.Alias{Name: name, Cmd: "touch " + Quote(marker) + "; :"}
		fn := store.Func{Name: "hello", Kind: store.KindSource, Script: "/nonexistent/h.sh"}
		text := Init(Bash, Defs{Aliases: []store.Alias{alias}, Funcs: []store.Func{fn}})
		stdout, stderr := loadInBash(t, s, text, "declare -f tote\n")

		_, statErr := os.Stat(marker)
		hijacked := statErr == nil || strings.Contains(stdout, "touch ")

		if hijacked || stderr != "" {
			t.Errorf("alias %q hijacks init: ran=%v wrapper=%v first stderr line=%q", name,
				statErr == nil, strings.Contains(stdout, "touch "), firstLine(stderr))
		}
	}
}

// TestReviewRelativeScriptIsNotSearchedInWorkingDirectory checks that a source function whose
// script path has no directory part never runs a file of the current directory, which "source"
// would find after looking in PATH.
//
// Parameters:
//   - t: test to report to
func TestReviewRelativeScriptIsNotSearchedInWorkingDirectory(t *testing.T) {
	s := newSandbox(t)
	work := filepath.Join(s.Dir, "work")

	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(s.Dir, "pwned")
	evil := "touch " + Quote(marker) + "\n"

	if err := os.WriteFile(filepath.Join(work, "helpers.sh"), []byte(evil), 0o600); err != nil {
		t.Fatal(err)
	}

	f := store.Func{Name: "hello", Kind: store.KindSource, Script: "helpers.sh"}
	text := Init(Bash, Defs{Funcs: []store.Func{f}})
	loadInBash(t, s, text, "cd work\nhello\n")

	if _, err := os.Stat(marker); err == nil {
		t.Error("a function sourced ./helpers.sh from the current directory")
	}
}

// TestReviewHandoffRefusesToOverwriteAnOwnedFile checks that a hand-off path pointing to a
// file of the user that the wrapper did not create is left untouched, whatever its mode.
//
// Parameters:
//   - t: test to report to
func TestReviewHandoffRefusesToOverwriteAnOwnedFile(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o644, 0o400} {
		path := filepath.Join(t.TempDir(), "precious")

		if err := os.WriteFile(path, []byte("keep me"), mode); err != nil {
			t.Fatal(err)
		}

		_ = WriteHandoff(path, Handoff{Action: ChangeDir, Payload: "/tmp"})

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		if string(data) != "keep me" {
			t.Errorf("mode %o: file was overwritten with %q", mode, data)
		}
	}
}

// TestReviewExecFunctionHonoursReservedNames checks that a function record of kind exec cannot
// take a reserved name such as the wrapper name, which the other record kinds refuse.
//
// Parameters:
//   - t: test to report to
func TestReviewExecFunctionHonoursReservedNames(t *testing.T) {
	for _, name := range []string{"tote", "eval", "source", "command"} {
		f := store.Func{Name: name, Kind: store.KindExec, Script: "/opt/x/" + name}
		text := Definitions(Bash, Defs{Funcs: []store.Func{f}})

		if !strings.Contains(text, "# tote: func '"+name+"' skipped") {
			t.Errorf("exec function %q is accepted silently", name)
		}
	}
}
