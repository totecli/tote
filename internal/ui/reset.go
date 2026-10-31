package ui

import (
	"io"
	"os"
	"os/exec"
)

// panicReset is written after a crash: the mode reset, then the exit of the alternate screen
// (a plain "reset" does not leave it on tmux 3.4) and a cleared main screen.
const panicReset = modeReset + "\x1b[?1049l\x1b[2J\x1b[H"

// ResetSequence is what a doctor command writes to a broken terminal: the mode reset, then the
// exit of the alternate screen and a cleared main screen.
const ResetSequence = panicReset

// ttyPath is the terminal that ResetTerminal runs "stty sane" on. Tests empty it so that they
// never touch the terminal of the person who runs them.
var ttyPath = "/dev/tty"

// ResetTerminal puts the terminal back into a usable state.
//
// It writes the sequences that switch off mouse reporting, focus reporting, bracketed paste and
// the keyboard protocols, shows the cursor, leaves the alternate screen and clears the main
// screen, then runs "stty sane" on the terminal. It is meant for the recovery after a crash and
// for a doctor command.
//
// Parameters:
//   - out: terminal output, usually os.Stdout
//
// Returns the error of the write, or nil. A missing terminal or stty is not an error.
func ResetTerminal(out io.Writer) error {
	_, err := io.WriteString(out, panicReset)

	if ttyPath == "" {
		return err
	}

	if tty, terr := os.OpenFile(ttyPath, os.O_RDWR, 0); terr == nil {
		defer func() { _ = tty.Close() }()

		_ = SaneTerminal(tty)
	}

	return err
}

// SaneTerminal runs "stty sane" on a terminal, which restores the line discipline (echo, line
// editing, signals) that a crashed program may have left off.
//
// Parameters:
//   - tty: open terminal, given to stty as its standard input
//
// Returns the error of the command, for instance when stty is not installed.
func SaneTerminal(tty *os.File) error {
	cmd := exec.Command("stty", "sane")
	cmd.Stdin = tty

	return cmd.Run()
}
