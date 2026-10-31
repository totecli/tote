package ui

import "syscall"

const (
	// ioctlGet reads the mode of a terminal.
	ioctlGet = syscall.TIOCGETA

	// ioctlSet writes the mode of a terminal.
	ioctlSet = syscall.TIOCSETA
)

// selectRead waits until one of the descriptors of a set can be read.
//
// Parameters:
//   - nfd: highest descriptor of the set plus one
//   - set: descriptors to watch, which holds the readable ones on return
func selectRead(nfd int, set *syscall.FdSet) error {
	return syscall.Select(nfd, set, nil, nil, nil)
}
