package ui

import (
	"strings"
	"testing"

	"github.com/totecli/tote/internal/shell"
)

// TestNoWrapper checks what the keys that hand something to the shell do when the shell wrapper
// is not active: the interface stays open, nothing is left for the shell, and the message shows
// the command or the directory and the line that enables the wrapper. With the wrapper the same
// keys quit with their result.
//
// Parameters:
//   - t: test to report to
func TestNoWrapper(t *testing.T) {
	tests := []struct {
		name   string
		start  Start
		key    string
		shell  string
		action shell.Action
		shows  string
		init   string
	}{
		{
			"command of a connection", StartConnections, "c", "/bin/bash", shell.Insert,
			"ssh -l deploy -- prod.example.com", `eval "$(tote init bash)"`,
		},
		{
			"alias", StartAliases, "enter", "/bin/bash", shell.Insert, "ls -la",
			`eval "$(tote init bash)"`,
		},
		{
			"function", StartFuncs, "enter", "/usr/bin/zsh", shell.Insert, "hello",
			`eval "$(tote init zsh)"`,
		},
		{
			"repository", StartRepos, "enter", "", shell.ChangeDir, "cd ",
			`eval "$(tote init bash)"`,
		},
		{
			"unknown shell", StartAliases, "enter", "/usr/bin/fish", shell.Insert, "ls -la",
			`eval "$(tote init bash)"`,
		},
	}

	for _, tt := range tests {
		for _, noWrapper := range []bool{true, false} {
			g := newRig(t, newSandbox(t).with(func(o *Options) {
				o.Start = tt.start
				o.NoWrapper = noWrapper
				o.Environ = append(o.Environ, "SHELL="+tt.shell)
			}))

			g.press(tt.key)

			if !noWrapper {
				if !g.Quit || g.r.result.Action != tt.action {
					t.Errorf("%s: with the wrapper: quit %v, result %+v", tt.name, g.Quit,
						g.r.result)
				}

				continue
			}

			if g.Quit || g.r.result.Action != "" {
				t.Errorf("%s: left the interface without the wrapper: %+v", tt.name, g.r.result)
			}

			got := g.toastText()
			if !strings.Contains(got, tt.shows) || !strings.Contains(got, tt.init) {
				t.Errorf("%s: toast %q, want %q and %q", tt.name, got, tt.shows, tt.init)
			}

			if tt.action == shell.ChangeDir && !strings.Contains(got, g.r.e.Home) &&
				!strings.Contains(got, "proj") {
				t.Errorf("%s: toast %q does not name the directory", tt.name, got)
			}

			if !strings.Contains(g.text(), "tote init") {
				t.Errorf("%s: the screen does not show the message:\n%s", tt.name, g.text())
			}
		}
	}
}

// TestNoWrapperKeepsQuitting checks that a result which needs no wrapper still ends the
// interface when the wrapper is not active: leaving after a connection (after_disconnect =
// "exit") hands nothing to the shell.
//
// Parameters:
//   - t: test to report to
func TestNoWrapperKeepsQuitting(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.NoWrapper = true
		o.Settings.Connect.AfterDisconnect = "exit"
	}))

	g.r.delay = 0

	g.send(playExec(t, settle(g, g.r.activate(activateMsg{Kind: kindConn, Key: "prod"})), "\n"))

	if !g.Quit {
		t.Error("the interface stayed open after the connection with after_disconnect = exit")
	}
}
