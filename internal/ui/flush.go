package ui

import "os"

// flushInput discards the keys that were typed on a terminal and have not been read yet, so that
// what a program received no input for is not replayed into the interface once it is back.
//
// Parameters:
//   - f: input of the interface; the call does nothing when it is not a terminal
//
// Returns the error of the system, nil when the queue was emptied.
func flushInput(f *os.File) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}

	var ferr error

	if err = rc.Control(func(fd uintptr) { ferr = flushFd(int(fd)) }); err != nil {
		return err
	}

	return ferr
}
