package ui

import (
	"errors"
	"testing"
)

// TestAfterDisconnect checks what the interface does when a connection returns, for each value
// of connect.after_disconnect and each way the connection can end.
//
// Parameters:
//   - t: test to report to
func TestAfterDisconnect(t *testing.T) {
	tests := []struct {
		name    string
		setting string
		res     RunResult
		quit    bool
		toast   bool
	}{
		{"menu, clean end", "menu", RunResult{}, false, false},
		{"menu, remote status", "menu", RunResult{ExitCode: 2}, false, true},
		{"menu, ssh error", "menu", RunResult{ExitCode: 255}, false, true},
		{"unset means menu", "", RunResult{}, false, false},
		{"exit, clean end", "exit", RunResult{}, true, false},
		{"exit, remote status", "exit", RunResult{ExitCode: 2}, true, false},
		{"exit, interrupted", "exit", RunResult{ExitCode: 130}, false, true},
		{"exit, ssh error", "exit", RunResult{ExitCode: 255}, false, true},
		{"exit, not found", "exit", RunResult{ExitCode: 127}, false, true},
		{"exit, killed", "exit", RunResult{ExitCode: 143, Signal: "SIGTERM"}, false, true},
		{"exit, not started", "exit", RunResult{ExitCode: 127, Err: errors.New("x")}, false, true},
	}

	for _, tt := range tests {
		run := &fakeRunner{Res: tt.res}
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Runner = run
			o.Settings.Connect.AfterDisconnect = tt.setting
		}))

		g.r.delay = 0

		g.send(playExec(t, settle(g, g.r.activate(activateMsg{Kind: kindConn, Key: "prod"})), "\n"))

		if len(run.calls()) != 1 {
			t.Errorf("%s: the connection ran %d times", tt.name, len(run.calls()))
		}

		if g.Quit != tt.quit {
			t.Errorf("%s: quit = %v, want %v", tt.name, g.Quit, tt.quit)
		}

		if g.r.result.Action != "" || g.r.result.Payload != "" {
			t.Errorf("%s: the interface left a hand-off: %+v", tt.name, g.r.result)
		}

		if !tt.quit && (g.toastText() != "") != tt.toast {
			t.Errorf("%s: toast %q, want one: %v", tt.name, g.toastText(), tt.toast)
		}
	}
}

// TestAfterDisconnectOnlyConnections checks that the setting concerns connections: an editor
// that ends with an error leaves the list in place whatever the setting says.
//
// Parameters:
//   - t: test to report to
func TestAfterDisconnectOnlyConnections(t *testing.T) {
	run := &fakeRunner{Res: RunResult{ExitCode: 1}}
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartRepos
		o.Runner = run
		o.Settings.Connect.AfterDisconnect = "exit"
		o.Settings.Repos.AfterOpen = "quit"
		o.Environ = append(o.Environ, "EDITOR=myeditor")
	}))

	g.r.delay = 0

	g.send(playExec(t, g.r.openInEditor("proj"), "\n"))

	if g.Quit {
		t.Error("a failed editor ended the interface")
	}
}
