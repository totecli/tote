//go:build linux || darwin

package main

import "golang.org/x/sys/unix"

// termSize asks the terminal for its size, trying the output, the error output and the input in
// turn, so that a redirected output does not hide a terminal.
//
// Returns the width and the height in cells, and false when none of them is a terminal.
func termSize() (int, int, bool) {
	for _, fd := range []int{1, 2, 0} {
		ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
		if err == nil && ws.Col > 0 && ws.Row > 0 {
			return int(ws.Col), int(ws.Row), true
		}
	}

	return 0, 0, false
}
