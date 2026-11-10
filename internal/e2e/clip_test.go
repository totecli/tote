package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// vteEnv is what a VTE terminal such as GNOME Terminal 3.52 sets: it ignores the sequence OSC 52,
// so a copy that no program takes can only be shown.
var vteEnv = []string{"TERM=xterm-256color", "VTE_VERSION=7600"}

// bareEnv gives the PATH of a sandbox that holds no clipboard program, whatever the machine
// has: the fake programs, the program under test, and a directory with links to the few tools
// that the wrapper and the fake programs call. Nothing of the real PATH is in it.
//
// Parameters:
//   - t: running test
//   - sb: sandbox that the PATH is for
func bareEnv(t *testing.T, sb *sandbox) string {
	t.Helper()

	tools := filepath.Join(sb.Dir, "tools")
	if err := os.MkdirAll(tools, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"sh", "sleep", "stty", "sed", "mv", "cat"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}

		if err := os.Symlink(path, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}

	return "PATH=" + sb.Bin + ":" + sb.Real + ":" + tools
}

// TestSelectionClipboardAdvice drags text in a VTE terminal with a PATH that holds nothing real.
// With the fake xclip the text reaches it and the toast says so, without it the toast tells what
// to install and how to select with the terminal, shows the text and does not repeat the hint of
// the first selection.
//
// Parameters:
//   - t: test to report to
func TestSelectionClipboardAdvice(t *testing.T) {
	const advice = "No copy tool: install xclip, xsel or wl-clipboard, or Shift+drag then " +
		"Ctrl+Shift+C. Text: bravo.example.com"

	for _, withTool := range []bool{true, false} {
		sb := newSandbox(t)

		if withTool {
			installXclip(t, sb)
		}

		env := append([]string{bareEnv(t, sb), "DISPLAY=:0"}, vteEnv...)
		tm := newTerm(t, sb, startOptions{W: 130, H: 30, Env: env}, "tote", "co")

		tm.waitText(cmdOf("alpha"))
		tm.waitMouse(wantMouse)

		col, row := tm.where("bravo.example.com", false)
		tm.dragFrom(col, row, col+16, row)

		if withTool {
			got := tm.waitFile("clip.txt", func(c string) bool { return c != "" })
			if got != "bravo.example.com" {
				t.Errorf("xclip got %q, want the dragged word", got)
			}

			tm.waitText("Copied 17 characters (xclip)")
			tm.quit()

			continue
		}

		s := tm.waitText(advice)
		if s.has("Shift+drag selects with the terminal instead") {
			t.Errorf("the toast repeats the hint:\n%s", s.Text)
		}

		if _, err := os.Stat(filepath.Join(sb.Out, "clip.txt")); err == nil {
			t.Error("a program took the text on a PATH without any")
		}

		tm.quit()
	}
}

// doctorClipboard runs "tote doctor --json" in a sandbox and returns its clipboard check. With a
// terminal the program runs in a tmux pane with its output in a file, so that it sees a terminal
// on the error output only; without one it runs with pipes.
//
// Parameters:
//   - t: running test
//   - sb: sandbox to run in
//   - tty: true to give the program a terminal
//   - env: variables added to the environment, last so that they win
//
// Returns the status and the message of the check.
func doctorClipboard(t *testing.T, sb *sandbox, tty bool, env ...string) (string, string) {
	t.Helper()

	var out []byte

	if tty {
		file := filepath.Join(sb.Dir, "doctor.json")
		tm := newTerm(t, sb, startOptions{Env: env}, "sh", "-c", `exec "$0" doctor --json >"$1"`,
			sb.tote(), file)

		// The exit code is 1 when another check fails, which says nothing about the clipboard.
		tm.waitDone()

		var err error
		if out, err = os.ReadFile(file); err != nil {
			t.Fatal(err)
		}
	} else {
		cmd := exec.Command(sb.tote(), "doctor", "--json")
		cmd.Dir = sb.Home
		cmd.Env = sb.Env(env...)
		out, _ = cmd.Output()
	}

	var rep struct {
		Checks []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Message string `json:"message"`
			Fix     string `json:"fix"`
		} `json:"checks"`
	}

	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, out)
	}

	for _, c := range rep.Checks {
		if c.ID == "clipboard" {
			return c.Status, c.Message + "|" + c.Fix
		}
	}

	t.Fatalf("doctor has no clipboard check:\n%s", out)

	return "", ""
}

// TestDoctorClipboard runs the real program on a machine with a fake xclip, with none in a VTE
// terminal, and with none in a terminal that takes OSC 52.
//
// Parameters:
//   - t: test to report to
func TestDoctorClipboard(t *testing.T) {
	tests := []struct {
		name  string
		tool  bool
		tty   bool
		env   []string
		state string
		has   []string
	}{
		{
			"fake xclip", true, true,
			[]string{"DISPLAY=:0", "VTE_VERSION=7600"},
			"ok",
			[]string{"copy works with xclip"},
		},
		{
			"xclip without a display", true, true,
			[]string{"VTE_VERSION=7600"},
			"warn",
			[]string{"no clipboard program", "install xclip or xsel", "Ctrl+Shift+C"},
		},
		{
			"nothing in a VTE terminal", false, true,
			[]string{"DISPLAY=:0", "VTE_VERSION=7600"},
			"warn",
			[]string{"no clipboard program", "install xclip or xsel", "Ctrl+Shift+C"},
		},
		{"nothing in a terminal that takes OSC 52", false, true, []string{
			"DISPLAY=:0",
			"TERM=xterm-256color",
		}, "ok", []string{"copy goes through the terminal (OSC 52)"}},
		{"nothing in a VTE terminal and no terminal", false, false, []string{
			"DISPLAY=:0",
			"VTE_VERSION=7600",
		}, "ok", []string{"no mouse selection can happen here"}},
	}

	for _, tt := range tests {
		sb := newSandbox(t)

		if tt.tool {
			installXclip(t, sb)
		}

		env := append([]string{bareEnv(t, sb), "TERM=xterm-256color"}, tt.env...)

		state, text := doctorClipboard(t, sb, tt.tty, env...)
		if state != tt.state {
			t.Errorf("%s: status %q, want %q (%s)", tt.name, state, tt.state, text)
		}

		for _, w := range tt.has {
			if !strings.Contains(text, w) {
				t.Errorf("%s: %q lacks %q", tt.name, text, w)
			}
		}
	}
}
