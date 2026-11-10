package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// shellRig is a shell with the tote wrapper loaded, running in a pane.
//
// Name is the shell, Argv the command that starts it as an interactive shell without startup
// files and Init the line that loads the wrapper.
type shellRig struct {
	name string
	argv []string
	init string
}

// shells lists the shells the wrapper is tested with.
func shells() []shellRig {
	return []shellRig{
		{"bash", []string{"bash", "--norc", "--noprofile", "-i"}, `eval "$(tote init bash)"`},
		{"zsh", []string{"zsh", "-f", "-i"}, `eval "$(tote init zsh)"`},
	}
}

// startShell starts an interactive shell with the wrapper loaded and waits until it is ready.
//
// Parameters:
//   - t: running test; skipped when the shell is not installed
//   - sh: shell to start
//   - extra: extra environment
func startShell(t *testing.T, sh shellRig, extra ...string) (*term, *sandbox) {
	t.Helper()
	requireTmux(t)

	requireTool(t, sh.argv[0])

	sb := newSandbox(t)
	env := append([]string{"PS1=$ ", "PROMPT=$ ", "HISTFILE=/dev/null"}, extra...)
	tm := newTerm(t, sb, startOptions{Env: env}, sh.argv...)

	tm.typeText(sh.init + ` && echo ready > "$E2E_DIR/ready"`)
	tm.key("Enter")
	tm.waitFile("ready", func(c string) bool { return strings.TrimSpace(c) == "ready" })

	return tm, sb
}

// TestHandoffChangeDir picks a repository in the interface and checks that the shell changes
// directory, in every shell with a wrapper.
//
// Parameters:
//   - t: test to report to
func TestHandoffChangeDir(t *testing.T) {
	t.Parallel()

	for _, sh := range shells() {
		t.Run(sh.name, func(t *testing.T) {
			t.Parallel()

			tm, sb := startShell(t, sh)

			tm.typeText("tote")
			tm.key("Enter")
			tm.waitText("Repos")
			tm.typeText("2")
			tm.waitText("The project")
			tm.key("Enter")

			// The interface leaves the alternate screen, then the wrapper changes directory.
			tm.waitFor("the end of the interface", func(s snap) bool { return !s.Alt })
			tm.typeText(`pwd > "$E2E_DIR/pwd.out"`)
			tm.key("Enter")

			got := tm.waitFile("pwd.out", func(c string) bool { return strings.TrimSpace(c) != "" })
			if want := filepath.Join(sb.Home, "code", "proj"); strings.TrimSpace(got) != want {
				t.Errorf("the shell is in %q, want %q", strings.TrimSpace(got), want)
			}
		})
	}
}

// TestHandoffInsertAlias picks an alias and checks that its command line is put at the prompt of
// bash, where Enter runs it. The command writes a value computed by the shell, so a command line
// that was only printed would not pass.
//
// Parameters:
//   - t: test to report to
func TestHandoffInsertAlias(t *testing.T) {
	t.Parallel()

	tm, sb := startShell(t, shells()[0])

	tm.typeText("tote")
	tm.key("Enter")
	tm.waitText("Repos")
	tm.typeText("3")
	tm.waitText("Say hi")
	tm.key("Enter")

	tm.waitFor("the command line at the prompt", func(s snap) bool {
		return !s.Alt && s.has(`> echo inserted-$((6*7))`)
	})

	tm.key("Enter")

	got := tm.waitFile("alias.out", func(c string) bool { return strings.TrimSpace(c) != "" })
	if strings.TrimSpace(got) != "inserted-42" {
		t.Errorf("alias.out = %q, want inserted-42", got)
	}

	_ = sb
}

// TestHandoffReload creates an alias in the interface and checks that, once the interface is
// closed, the wrapper loads it: the new alias runs in the same shell.
//
// Parameters:
//   - t: test to report to
func TestHandoffReload(t *testing.T) {
	t.Parallel()

	tm, _ := startShell(t, shells()[0])

	tm.typeText("tote")
	tm.key("Enter")
	tm.waitText("Repos")
	tm.typeText("3")
	tm.waitText("Say hi")

	tm.typeText("n")
	tm.waitText("New alias")
	tm.typeText("made")
	tm.key("Tab")
	tm.typeText(`echo made-$((5*5)) > "$E2E_DIR/made.out"`)
	tm.key("C-s")
	tm.waitText("saved")
	tm.waitGone("New alias")
	tm.typeText("q")

	tm.waitFor("the end of the interface", func(s snap) bool { return !s.Alt })
	tm.typeText("made")
	tm.key("Enter")

	got := tm.waitFile("made.out", func(c string) bool { return strings.TrimSpace(c) != "" })
	if strings.TrimSpace(got) != "made-25" {
		t.Errorf("made.out = %q, want made-25", got)
	}
}
