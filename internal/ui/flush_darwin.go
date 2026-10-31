package ui

import (
	"syscall"
	"unsafe"
)

// flushRead is the argument of TIOCFLUSH that picks the input queue (FREAD).
const flushRead = 1

// flushFd discards the keys that were typed on a terminal and not read yet.
//
// Parameters:
//   - fd: descriptor of the terminal
//
// Returns the error of the system, such as ENOTTY when fd is not a terminal.
func flushFd(fd int) error {
	which := int32(flushRead)

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCFLUSH,
		uintptr(unsafe.Pointer(&which)))
	if errno != 0 {
		return errno
	}

	return nil
}
