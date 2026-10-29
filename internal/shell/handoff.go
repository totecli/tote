package shell

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

// handoffVersion is the first word of the header line of a hand-off file.
const handoffVersion = "v1"

// reloadFlag is the header flag that asks the wrapper to re-evaluate the definitions.
//
// Flag names are whole words: the wrappers compare a flag with the list of the header as a
// whole, never as a beginning, and a wrapper that a running shell has loaded cannot be replaced.
// A flag added later therefore must not be named "reload" followed by anything, and an unknown
// flag or action is ignored by the old wrappers.
const reloadFlag = "reload"

// getuid returns the user id that must own a hand-off file; tests replace it to play another user.
var getuid = os.Getuid

// ErrHandoff is wrapped by every error that WriteHandoff and ReadHandoff return for a refused
// path, file or content, so that callers can tell it from an I/O failure with errors.Is.
var ErrHandoff = errors.New("refused hand-off")

// Action is what the wrapper must do in the user shell after tote has exited.
type Action string

const (
	// Insert puts Payload on the command line of the user shell for editing and running.
	Insert Action = "insert"

	// ChangeDir changes the directory of the user shell to Payload.
	ChangeDir Action = "cd"

	// Exit closes the user shell.
	Exit Action = "exit"

	// None asks for nothing.
	None Action = "none"
)

// Handoff is the message that tote leaves for the wrapper in the file named by TOTE_HANDOFF.
//
// The first line of the file is "v1 " followed by the action and, when Reload is true, the flag
// ",reload". Everything after the first newline is Payload, which never contains a NUL byte.
type Handoff struct {
	Action  Action
	Reload  bool
	Payload string
}

// validAction reports whether an action is one of the four known values.
//
// Parameters:
//   - a: action to test
func validAction(a Action) bool {
	switch a {
	case Insert, ChangeDir, Exit, None:
		return true
	}

	return false
}

// encode renders the file content of a hand-off.
//
// Returns the content, or an error wrapping ErrHandoff for an unknown action or a payload that
// holds a NUL byte.
func (h Handoff) encode() ([]byte, error) {
	if !validAction(h.Action) {
		return nil, fmt.Errorf("%w: unknown action %q", ErrHandoff, string(h.Action))
	}

	if strings.IndexByte(h.Payload, 0) >= 0 {
		return nil, fmt.Errorf("%w: payload contains a NUL byte", ErrHandoff)
	}

	head := handoffVersion + " " + string(h.Action)
	if h.Reload {
		head += "," + reloadFlag
	}

	return []byte(head + "\n" + h.Payload), nil
}

// decode parses the content of a hand-off file.
//
// Parameters:
//   - data: whole file content
//
// Returns the hand-off, or an error wrapping ErrHandoff for an unknown version, action or flag,
// or for a NUL byte.
func decode(data []byte) (Handoff, error) {
	text := string(data)
	if strings.IndexByte(text, 0) >= 0 {
		return Handoff{}, fmt.Errorf("%w: content contains a NUL byte", ErrHandoff)
	}

	head, payload, _ := strings.Cut(text, "\n")

	rest, ok := strings.CutPrefix(head, handoffVersion+" ")
	if !ok {
		return Handoff{}, fmt.Errorf("%w: unknown header %q", ErrHandoff, head)
	}

	action, flags, _ := strings.Cut(rest, ",")
	h := Handoff{Action: Action(action), Payload: payload}

	if !validAction(h.Action) {
		return Handoff{}, fmt.Errorf("%w: unknown action %q", ErrHandoff, action)
	}

	switch flags {
	case "":
	case reloadFlag:
		h.Reload = true
	default:
		return Handoff{}, fmt.Errorf("%w: unknown flag %q", ErrHandoff, flags)
	}

	return h, nil
}

// openHandoff opens a hand-off file without following a symbolic link and checks it through the
// open descriptor, so that nothing can change between the check and the use.
//
// Parameters:
//   - path: file created by the wrapper
//   - flag: access mode, O_WRONLY or O_RDONLY
//   - private: when true, the file must also be empty with mode 0600, as the wrapper made it
//
// Returns the open file, or an error when the path is not a regular file owned by the current
// user.
func openHandoff(path string, flag int, private bool) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: empty path", ErrHandoff)
	}

	// O_NONBLOCK keeps the open of a named pipe from blocking; the check below refuses it.
	f, err := os.OpenFile(path, flag|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}

	if err := checkHandoff(f, private); err != nil {
		_ = f.Close()

		return nil, err
	}

	return f, nil
}

// checkHandoff verifies that an open file is a regular file owned by the current user.
//
// Parameters:
//   - f: open hand-off file
//   - private: when true, the file must be empty and have mode 0600, which is what mktemp makes,
//     so that a file the wrapper did not create is never overwritten
//
// Returns nil when the file is acceptable, otherwise an error wrapping ErrHandoff.
func checkHandoff(f *os.File, private bool) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file", ErrHandoff)
	}

	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != getuid() {
		return fmt.Errorf("%w: not owned by the current user", ErrHandoff)
	}

	if private && info.Mode().Perm() != 0o600 {
		return fmt.Errorf("%w: mode is not 0600", ErrHandoff)
	}

	if private && info.Size() != 0 {
		return fmt.Errorf("%w: not an empty file", ErrHandoff)
	}

	return nil
}

// WriteHandoff leaves a message for the wrapper in its hand-off file.
//
// The file is never created here: the wrapper made it, with mode 0600, and an absent file means
// that tote was not started by the wrapper. The file is opened for writing without following a
// symbolic link and is checked through the open descriptor: it must be an empty regular file
// owned by the current user with mode 0600, exactly what the wrapper made. Any other file of the
// user, such as a key or a startup file named by a hostile environment, is left untouched.
//
// Parameters:
//   - path: value of TOTE_HANDOFF, must not be empty
//   - h: message, with a known action and a payload free of NUL bytes
//
// Returns nil on success, an error wrapping ErrHandoff when the path, the file or the message is
// refused, or the underlying I/O error.
func WriteHandoff(path string, h Handoff) error {
	data, err := h.encode()
	if err != nil {
		return err
	}

	f, err := openHandoff(path, os.O_WRONLY, true)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()

		return err
	}

	return f.Close()
}

// ReadHandoff reads a hand-off file the way the wrapper would, for tests and diagnostics.
//
// The file is opened without following a symbolic link and must be a regular file owned by the
// current user.
//
// Parameters:
//   - path: hand-off file to read, must not be empty
//
// Returns the message, an error wrapping ErrHandoff when the file or its content is refused, or
// the underlying I/O error.
func ReadHandoff(path string) (Handoff, error) {
	f, err := openHandoff(path, os.O_RDONLY, false)
	if err != nil {
		return Handoff{}, err
	}

	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	if err != nil {
		return Handoff{}, err
	}

	return decode(data)
}

// RemoveHandoff deletes the hand-off file when the program is about to be replaced by another
// one, so that an interrupted run does not leave the file of the wrapper behind: the wrapper
// removes it only when the function is allowed to finish.
//
// The same checks as WriteHandoff apply, so a path that is not an empty private file made by the
// wrapper is never removed.
//
// Parameters:
//   - path: value of TOTE_HANDOFF, nothing is done when it is empty
//
// Returns nil when the file is gone or was not asked for, otherwise the refusal or I/O error.
func RemoveHandoff(path string) error {
	if path == "" {
		return nil
	}

	f, err := openHandoff(path, os.O_WRONLY, true)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	opened, err := f.Stat()
	if err != nil {
		return err
	}

	linked, err := os.Lstat(path)
	if err != nil {
		return err
	}

	if !os.SameFile(opened, linked) {
		return fmt.Errorf("%w: file replaced", ErrHandoff)
	}

	return os.Remove(path)
}
