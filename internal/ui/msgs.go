package ui

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/store"
)

// writeOp is the kind of change a store write made.
type writeOp int

const (
	// opAdd is a new record.
	opAdd writeOp = iota

	// opEdit is a changed record, possibly renamed.
	opEdit

	// opDelete is a removed record.
	opDelete
)

// openKindMsg asks the root to show the list of a kind.
type openKindMsg struct {
	Kind kind
}

// goHomeMsg asks the root to show the welcome screen.
type goHomeMsg struct{}

// addMsg is sent by a list to ask for the form of a new record.
type addMsg struct {
	Kind kind
}

// editMsg is sent by a list to ask for the form of an existing record.
type editMsg struct {
	Kind kind
	Key  string
}

// deleteMsg is sent by a list to ask for the confirmation of a deletion.
type deleteMsg struct {
	Kind kind
	Key  string
}

// activateMsg is sent by a list when Enter is pressed on a record. The root decides what it
// means for the kind: run ssh, change directory or insert a command line. User is the login to
// connect as instead of the stored one, empty for the stored one; only connections use it.
type activateMsg struct {
	Kind kind
	Key  string
	User string
}

// commandMsg is sent by the list of connections to put the command of a connection at the
// prompt of the shell instead of running it. User is the login chosen for this run, if any.
type commandMsg struct {
	Key  string
	User string
}

// copyMsg is sent by a list to copy the command, path or name of a record to the clipboard.
type copyMsg struct {
	Kind kind
	Key  string
	User string
}

// clipped is what the clipboard chain found for a text. Text is what was to be copied and Chars
// its length in characters. Tool names the program that took it, empty when none did, and OSC is
// true when the terminal can receive it as an OSC 52 sequence instead; when both are empty the
// text can only be shown.
type clipped struct {
	Text  string
	Chars int
	Tool  string
	OSC   bool
}

// copyReadyMsg is the answer of the clipboard chain after the key C, which copies a record.
type copyReadyMsg clipped

// selCopiedMsg is the answer of the clipboard chain after a text selection with the mouse.
type selCopiedMsg clipped

// viewMsg is sent by a list to show every field of a record.
type viewMsg struct {
	Kind kind
	Key  string
	User string
}

// pickUserMsg is sent by the list of connections to ask which login to connect as. Cur is the
// login that is chosen at the moment, empty for the stored one.
type pickUserMsg struct {
	Key string
	Cur string
}

// openConfigMsg asks the root to show the settings screen.
type openConfigMsg struct{}

// openHelpMsg asks the root to show the help of the screen that is below the overlays.
type openHelpMsg struct{}

// noticeMsg asks for the update notice that is known: the root shows it as a message.
type noticeMsg struct{}

// checkMsg asks for a check for a new release, run in the background.
type checkMsg struct{}

// checkedMsg is the end of a check for a new release.
type checkedMsg struct {
	Res UpdateCheck
	Err error
}

// editorMsg is sent by the list of repositories to open a repository in the editor.
type editorMsg struct {
	Key string
}

// reloadMsg asks the root to read the data files again and refresh the screen.
type reloadMsg struct{}

// refreshMsg asks the root to read one collection again, without any message, after a write
// was refused because the file had changed under the interface.
type refreshMsg struct {
	Kind kind
}

// wroteMsg tells the root that a record was written, so that it reloads the collection, flags
// the shell definitions as stale and selects Key again. Overlays send it after a successful
// write through the dataset; Key is the new key of an edit.
type wroteMsg struct {
	Kind kind
	Key  string
	Op   writeOp
}

// toastMsg asks the root to show a message on the status line for a few seconds, or for
// toastHold when Hold is true (a message the user has to read and act on), or for Wait when it
// is set. Text must already be translated and sanitized.
type toastMsg struct {
	Text string
	Err  bool
	Hold bool
	Wait time.Duration
}

// toastExpireMsg removes the toast with the same ID, if it is still the current one.
type toastExpireMsg struct {
	ID int
}

// openOverlayMsg asks the root to push an overlay on top of the stack.
type openOverlayMsg struct {
	Overlay overlay
}

// openPaletteMsg asks the root to open the command palette.
type openPaletteMsg struct{}

// dismissMsg closes the overlay on top of the stack.
type dismissMsg struct{}

// backgroundTimeoutMsg ends the wait for the background color of the terminal.
type backgroundTimeoutMsg struct{}

// execDoneMsg reports the end of a program started through the exec runner.
//
// Label is the displayed command line, Result what the program did and Restore the error of
// the terminal restoration, nil when all went well. After tells what to do next. Denied is set when
// the program was not started because the gate of the command refused it, see execCommand.
type execDoneMsg struct {
	Label   string
	Result  RunResult
	Restore error
	After   after
	Denied  error
}

// interruptMsg ends the interface after a SIGINT: Run then reports ErrInterrupted.
type interruptMsg struct{}

// finishMsg ends the interface with a result for the shell wrapper.
type finishMsg struct {
	Result Result
}

// msgCmd wraps a message into a command.
//
// Parameters:
//   - m: message to deliver on the next turn of the event loop
func msgCmd(m tea.Msg) tea.Cmd {
	return func() tea.Msg { return m }
}

// toastCmd returns a command that shows a message on the status line.
//
// Parameters:
//   - text: translated and sanitized message
//   - isErr: true to draw it as an error
func toastCmd(text string, isErr bool) tea.Cmd {
	return msgCmd(toastMsg{Text: text, Err: isErr})
}

// wroteCmd returns a command that tells the root a record was written.
//
// Parameters:
//   - k: kind of the record
//   - key: key of the record after the write
//   - op: what was done
func wroteCmd(k kind, key string, op writeOp) tea.Cmd {
	return msgCmd(wroteMsg{Kind: k, Key: key, Op: op})
}

// staleWrite reports whether a write was refused because the record is gone or the file changed,
// which means that the list on the screen is out of date.
//
// Parameters:
//   - err: error of the dataset
func staleWrite(err error) bool {
	return errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict)
}

// removeCmd deletes a record and reports the outcome.
//
// Parameters:
//   - e: shared environment, which gives the dataset
//   - k: kind of the record
//   - key: raw key of the record
//
// Returns a command that yields a wroteMsg on success and an error toast otherwise.
func removeCmd(e *env, k kind, key string) tea.Cmd {
	if err := e.Data.remove(k, key); err != nil {
		if staleWrite(err) {
			return tea.Batch(msgCmd(refreshMsg{Kind: k}), toastCmd(err.Error(), true))
		}

		return toastCmd(err.Error(), true)
	}

	return wroteCmd(k, key, opDelete)
}
