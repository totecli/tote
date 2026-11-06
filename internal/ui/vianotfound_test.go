package ui

import (
	"bytes"
	"errors"
	"io/fs"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/totecli/tote/internal/i18n"
)

// viaFile is a connections file with a via that is a shell alias, a template whose program is
// missing, a via that is a program, and a plain ssh.
const viaFile = `schema = 1

[[connection]]
name = "alias"
target = "h1"
via = "jmp"

[[connection]]
name = "template"
target = "h2"
via = "jmp -t {host}"

[[connection]]
name = "plain"
target = "h3"
`

// TestViaNotFoundMessage checks the pause line and the toast of a connection whose program
// cannot be started: the real cause stays, the command is shown, and a via gets the explanation
// that a shell alias cannot be one, with an example, in both languages. A plain ssh that is
// missing and a program that failed in another way keep the short message.
//
// Parameters:
//   - t: test to report to
func TestViaNotFoundMessage(t *testing.T) {
	notFound := &exec.Error{Name: "jmp", Err: exec.ErrNotFound}

	tests := []struct {
		name string
		lang string
		key  string
		err  error
		want []string
		hint bool
	}{
		{"alias", "en", "alias", notFound, []string{
			"cannot run:",
			`exec: "jmp": executable file not found in $PATH`, "not found in PATH",
			"a shell alias or function cannot be used as a via",
			"ssh -t admin@jump.example.org {user@}{host}", "press Enter to return",
		}, true},
		{"alias in French", "fr", "alias", notFound, []string{
			"lancement impossible",
			"introuvable dans le PATH",
			"un alias ou une fonction du shell ne peut pas servir de via",
			"ssh -t admin@jump.example.org {user@}{host}", "appuyer sur Entr\u00e9e",
		}, true},
		{"template", "en", "template", notFound, []string{"not found in PATH"}, true},
		{
			"not executable", "en", "alias",
			&fs.PathError{Op: "fork/exec", Path: "jmp", Err: syscall.EACCES},
			[]string{"permission denied", "not found in PATH"},
			true,
		},
		{
			"plain ssh", "en", "plain", &exec.Error{Name: "ssh", Err: exec.ErrNotFound},
			[]string{"cannot run:", "executable file not found"},
			false,
		},
		{
			"another error", "en", "alias", errors.New("exec format error"),
			[]string{"cannot run:", "exec format error"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := &fakeRunner{Res: RunResult{ExitCode: 127, Err: tt.err}}
			sb := newSandbox(t)
			writeFile(t, sb.Opts.Paths.ConnectionsFile(), viaFile)

			g := newRig(t, sb.with(func(o *Options) {
				o.Start, o.Runner, o.Printer = StartConnections, run, newPrinter(tt.lang)
			}))
			g.r.delay = 0

			var out bytes.Buffer

			cmd := settle(g, g.r.activate(activateMsg{Kind: kindConn, Key: tt.key}))
			g.send(playExecWith(t, cmd, "\n", &out))

			for _, w := range append([]string{"-> "}, tt.want...) {
				if w == "-> " {
					w = g.r.e.Glyphs.arrow
				}

				if !strings.Contains(out.String(), w) {
					t.Errorf("the pause line lacks %q:\n%s", w, out.String())
				}
			}

			if has := strings.Contains(out.String(), "ssh -t admin@jump.example.org"); has != tt.hint {
				t.Errorf("the example is shown: %v, want %v", has, tt.hint)
			}

			toast := g.toastText()
			if !g.r.toast.err || !strings.Contains(toast, tt.err.Error()) {
				t.Errorf("toast = %q, error %v", toast, g.r.toast.err)
			}

			if has := strings.Contains(toast, "ssh -t admin@jump.example.org"); has != tt.hint {
				t.Errorf("the example is in the toast: %v, want %v: %q", has, tt.hint, toast)
			}
		})
	}
}

// TestDescribeExitVia checks the toast of describeExit for a connection with a via.
//
// Parameters:
//   - t: test to report to
func TestDescribeExitVia(t *testing.T) {
	notFound := RunResult{ExitCode: 127, Err: &exec.Error{Name: "jmp", Err: exec.ErrNotFound}}

	for _, lang := range []string{"en", "fr"} {
		tr := i18n.New(lang)

		with := describeExit(tr, "jmp host", notFound, true)
		without := describeExit(tr, "jmp host", notFound, false)

		if !strings.Contains(with, "ssh -t admin@jump.example.org {user@}{host}") ||
			!strings.Contains(with, "jmp host") || !strings.Contains(with, "$PATH") {
			t.Errorf("%s: toast with a via = %q", lang, with)
		}

		if strings.Contains(without, "ssh -t admin@jump.example.org") {
			t.Errorf("%s: toast without a via = %q", lang, without)
		}

		if got := describeExit(tr, "jmp host", RunResult{ExitCode: 3}, true); strings.Contains(got,
			"ssh -4") {
			t.Errorf("%s: a plain exit status got the hint: %q", lang, got)
		}
	}
}
