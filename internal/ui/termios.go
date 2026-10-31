//go:build linux || darwin

package ui

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// errNoWait is returned by awaitKey for descriptors that select cannot watch.
var errNoWait = errors.New("descriptor cannot be watched")

// termState is the mode of a terminal as it was before makeRaw.
type termState struct {
	saved syscall.Termios
}

// termioctl reads or writes the mode of a terminal.
//
// Parameters:
//   - fd: descriptor of the terminal
//   - req: request, ioctlGet or ioctlSet
//   - t: mode to fill or to apply
func termioctl(fd int, req uintptr, t *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(t)))
	if errno != 0 {
		return errno
	}

	return nil
}

// makeRaw puts a terminal in raw mode, where a read gives up after a tenth of a second without a
// key (Go reports it as an end of file), so that the reader can look at a flag between keys.
//
// Parameters:
//   - fd: descriptor to change
//
// Returns the previous mode, or nil when fd is not a terminal or the mode cannot be changed.
func makeRaw(fd int) *termState {
	var old syscall.Termios

	if err := termioctl(fd, ioctlGet, &old); err != nil {
		return nil
	}

	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 0
	raw.Cc[syscall.VTIME] = 1

	if err := termioctl(fd, ioctlSet, &raw); err != nil {
		return nil
	}

	return &termState{saved: old}
}

// snapshot reads the mode of a terminal without changing it.
//
// Parameters:
//   - fd: descriptor of the terminal
//
// Returns the mode, or nil when fd is not a terminal.
func snapshot(fd int) *termState {
	var cur syscall.Termios

	if err := termioctl(fd, ioctlGet, &cur); err != nil {
		return nil
	}

	return &termState{saved: cur}
}

// restore puts a terminal back in the mode it had.
//
// Parameters:
//   - fd: descriptor to change
func (s *termState) restore(fd int) {
	_ = termioctl(fd, ioctlSet, &s.saved)
}

// fdBits is the number of descriptors a set of select can hold.
const fdBits = int(unsafe.Sizeof(syscall.FdSet{})) * 8

// fdWord returns the byte of a set that holds the bit of a descriptor, and the mask of the bit.
// The bits of a set are numbered from the least significant bit of its first word, so on the
// little-endian systems that tote supports they are the bits of its bytes in order.
//
// Parameters:
//   - set: descriptor set
//   - fd: descriptor, below fdBits
func fdWord(set *syscall.FdSet, fd int) (*byte, byte) {
	base := (*[unsafe.Sizeof(syscall.FdSet{})]byte)(unsafe.Pointer(set))

	return &base[fd/8], 1 << (uint(fd) % 8)
}

// awaitKey blocks, without polling, until a terminal has a key to read or the wake file has a
// byte. The bytes of the wake file are read and dropped.
//
// Parameters:
//   - in: input of the terminal
//   - wake: read end of a pipe that another goroutine writes to when the wait must be
//     reconsidered
//
// Returns true when in can be read, false when only wake was written to, or an error when a
// descriptor cannot be watched (the caller then reads with a timeout instead).
func awaitKey(in, wake *os.File) (bool, error) {
	inFd, err := descriptor(in)
	if err != nil {
		return false, err
	}

	wakeFd, err := descriptor(wake)
	if err != nil {
		return false, err
	}

	if inFd >= fdBits || wakeFd >= fdBits {
		return false, errNoWait
	}

	for {
		var set syscall.FdSet

		for _, fd := range []int{inFd, wakeFd} {
			w, mask := fdWord(&set, fd)
			*w |= mask
		}

		err := selectRead(max(inFd, wakeFd)+1, &set)
		if errors.Is(err, syscall.EINTR) {
			continue
		}

		if err != nil {
			return false, err
		}

		w, mask := fdWord(&set, wakeFd)
		if *w&mask != 0 {
			drain(wakeFd)
		}

		w, mask = fdWord(&set, inFd)

		return *w&mask != 0, nil
	}
}

// descriptor returns the descriptor of a file without changing its blocking mode.
//
// Parameters:
//   - f: file to look at
func descriptor(f *os.File) (int, error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return 0, err
	}

	fd := -1

	if err = rc.Control(func(n uintptr) { fd = int(n) }); err != nil {
		return 0, err
	}

	return fd, nil
}

// drain reads and drops what a pipe holds.
//
// Parameters:
//   - fd: read end of the pipe, which is not blocking
func drain(fd int) {
	var buf [64]byte

	for {
		if n, err := syscall.Read(fd, buf[:]); n <= 0 || err != nil {
			return
		}
	}
}
