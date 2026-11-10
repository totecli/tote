package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// viaConnections holds a connection whose via is a name that only a shell alias would know and
// one whose via is a template that starts a program that exists.
const viaConnections = `schema = 1

[[connection]]
name = "alias"
target = "h1"
via = "jmp"

[[connection]]
name = "template"
target = "h2"
via = "ssh -t admin@jump.example.org {user@}{host}"
`

// TestViaProgramNotFound runs the real program on a connection whose via is not a program of the
// PATH: "tote co" fails with the real cause, the command and the explanation, in both languages,
// without a shell being involved, and "tote doctor" names the connection and the fix.
//
// Parameters:
//   - t: test to report to
func TestViaProgramNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lang string
		co   []string
		doc  string
	}{
		{
			"en",
			[]string{
				"cannot run", `exec: "jmp": executable file not found in $PATH`,
				"  -> jmp h1", "a shell alias or function cannot be used as a via",
				"ssh -t admin@jump.example.org {user@}{host}",
			},
			"connections whose via program is not found in PATH",
		},
		{"fr", []string{
			"impossible de lancer", `exec: "jmp": executable file not found in $PATH`,
			"  -> jmp h1", "un alias ou une fonction du shell ne peut pas servir de via",
			"ssh -t admin@jump.example.org {user@}{host}",
		}, "introuvable dans le PATH"},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			sb.write(t, filepath.Join(sb.Dir, "config", "connections.toml"), viaConnections)

			env := sb.Env("TOTE_LANG=" + tt.lang)

			var out, errb bytes.Buffer

			cmd := exec.Command(sb.tote(), "co", "alias")
			cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = sb.Home, env, &out, &errb

			err := cmd.Run()

			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != 127 {
				t.Fatalf("exit = %v, want 127; stderr %q", err, errb.String())
			}

			for _, w := range tt.co {
				if !strings.Contains(errb.String(), w) {
					t.Errorf("stderr lacks %q:\n%s", w, errb.String())
				}
			}

			cmd = exec.Command(sb.tote(), "doctor", "--json")
			cmd.Dir, cmd.Env = sb.Home, env
			raw, _ := cmd.Output()

			var rep struct {
				Checks []struct {
					ID      string   `json:"id"`
					Status  string   `json:"status"`
					Message string   `json:"message"`
					Fix     string   `json:"fix"`
					Details []string `json:"details"`
				} `json:"checks"`
			}

			if err := json.Unmarshal(raw, &rep); err != nil {
				t.Fatalf("doctor --json: %v\n%s", err, raw)
			}

			for _, c := range rep.Checks {
				if c.ID != "via" {
					continue
				}

				if c.Status != "warn" || !strings.Contains(c.Message, tt.doc) ||
					len(c.Details) != 1 || !strings.Contains(c.Details[0], "alias") ||
					!strings.Contains(c.Details[0], "jmp") ||
					!strings.Contains(c.Fix, "ssh -t admin@jump.example.org {user@}{host}") {
					t.Errorf("via check = %+v", c)
				}

				return
			}

			t.Fatalf("doctor has no via check:\n%s", raw)
		})
	}
}

// TestViaProgramNotFoundInterface presses Enter on a connection whose via is not a program of the
// PATH, in the real interface: the output stays with the real cause, the command and the
// explanation, Enter brings the list back and the toast keeps the cause.
//
// Parameters:
//   - t: test to report to
func TestViaProgramNotFoundInterface(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, filepath.Join(sb.Dir, "config", "connections.toml"), viaConnections)

	tm := newTerm(t, sb, startOptions{W: 300, H: 30}, "tote", "co")
	tm.waitText("alias")
	tm.key("Enter")

	s := tm.waitText("press Enter to return")

	for _, w := range []string{
		"cannot run:", `exec: "jmp": executable file not found in $PATH`,
		"jmp h1", "a shell alias or function cannot be used as a via",
		"ssh -t admin@jump.example.org {user@}{host}",
	} {
		if !s.has(w) {
			t.Errorf("the pause lacks %q:\n%s", w, s.Text)
		}
	}

	tm.key("Enter")
	tm.waitGone("press Enter to return")
	tm.waitText("alias")
	tm.quit()
}
