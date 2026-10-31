package ui

import "syscall"

const (
	// ioctlFlush is the request that discards the data of a terminal queue: TCFLSH on the
	// architectures tote is built for.
	ioctlFlush = 0x540b

	// flushInQueue is the argument of ioctlFlush that picks the input queue: TCIFLUSH.
	flushInQueue = 0
)

// flushFd discards the keys that were typed on a terminal and not read yet.
//
// Parameters:
//   - fd: descriptor of the terminal
//
// Returns the error of the system, such as ENOTTY when fd is not a terminal.
func flushFd(fd int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), ioctlFlush, flushInQueue)
	if errno != 0 {
		return errno
	}

	return nil
}
