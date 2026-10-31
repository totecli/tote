//go:build !linux && !darwin

package ui

import "errors"

// flushFd reports that the input of a terminal cannot be discarded on this system.
//
// Parameters:
//   - fd: unused
//
// Returns an error.
func flushFd(int) error {
	return errors.New("input cannot be flushed")
}
