package ui

import (
	"os"
)

// pauseKeys are the bytes that end the pause after a failed program, as the raw terminal
// delivers them: Enter as a carriage return or a line feed, Ctrl-C and Ctrl-D.
const pauseKeys = "\r\n\x03\x04"

// rawInput puts the terminal behind a file in raw mode: no echo, no line editing and no signal
// for Ctrl-C, so that a key reaches the reader as a byte.
//
// Parameters:
//   - f: input of the interface
//
// Returns the function that puts the mode back, or nil when f is not a terminal or its mode
// cannot be changed, in which case nothing was touched. The file descriptor is used through the
// syscall connection of the file, which leaves the blocking mode of the file alone.
func rawInput(f *os.File) func() {
	rc, err := f.SyscallConn()
	if err != nil {
		return nil
	}

	var saved *termState

	err = rc.Control(func(fd uintptr) {
		saved = makeRaw(int(fd))
	})
	if err != nil || saved == nil {
		return nil
	}

	return func() {
		_ = rc.Control(func(fd uintptr) { saved.restore(int(fd)) })
	}
}

// holdMode remembers the mode of the terminal behind a file, so that it can be put back after a
// program that changed it and did not restore it: Bubble Tea keeps the mode it finds when it takes
// the terminal again as the one to restore at exit.
//
// Parameters:
//   - f: input of the interface
//
// Returns the function that puts the mode back, or nil when f is not a terminal or its mode
// cannot be read.
func holdMode(f *os.File) func() {
	rc, err := f.SyscallConn()
	if err != nil {
		return nil
	}

	var saved *termState

	err = rc.Control(func(fd uintptr) {
		saved = snapshot(int(fd))
	})
	if err != nil || saved == nil {
		return nil
	}

	return func() {
		_ = rc.Control(func(fd uintptr) { saved.restore(int(fd)) })
	}
}
