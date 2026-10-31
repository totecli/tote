package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/shell"
)

// probeUse says what is done with a command once its keepalive options are known.
type probeUse int

const (
	// useRun starts the connection.
	useRun probeUse = iota

	// useCopy puts the command in the clipboard.
	useCopy

	// useInsert leaves the interface with the command for the prompt of the shell.
	useInsert
)

// probedMsg is sent when the keepalive options of a command are known: Argv is the command with
// them. Use says what is done with it, and for useRun Next tells what follows.
type probedMsg struct {
	Argv []string
	Use  probeUse
	Next after
}

// withKeepalive completes a connection command with the keepalive options of ssh, when the
// setting is on and the command is plain ssh. The lookup asks "ssh -G", which may run the Match
// exec commands of the user's ssh_config, so it is made only for a key that runs, copies or
// inserts the command, never for a cursor move, and it runs off the event loop, for two seconds
// at most. The command that the status line and the detail panel show is the base command; the
// banner of the pause line, the clipboard and the prompt get the real one.
//
// Parameters:
//   - argv: command of the connection
//   - use: what to do with the command once it is complete
//   - next: what follows a connection, for useRun
//
// Returns nil when there is nothing to ask and argv can be used as it is.
func (r *root) withKeepalive(argv []string, use probeUse, next after) tea.Cmd {
	e := r.e

	if !e.Settings.Connect.Keepalive || !shell.KeepEligible(argv) {
		return nil
	}

	probe, environ := e.Probe, e.Environ

	return func() tea.Msg {
		return probedMsg{Argv: shell.Keepalive(true, probe, environ, argv), Use: use, Next: next}
	}
}
