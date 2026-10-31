//go:build !linux && !darwin

package ui

import (
	"errors"
	"os"
)

// termState is the mode of a terminal; there is none to keep on this system.
type termState struct{}

// makeRaw reports that the mode of the terminal cannot be changed on this system.
//
// Parameters:
//   - fd: unused
//
// Returns nil.
func makeRaw(int) *termState {
	return nil
}

// snapshot reports that the mode of the terminal cannot be read on this system.
//
// Parameters:
//   - fd: unused
//
// Returns nil.
func snapshot(int) *termState {
	return nil
}

// restore does nothing.
//
// Parameters:
//   - fd: unused
func (s *termState) restore(int) {}

// awaitKey reports that a terminal cannot be watched on this system.
//
// Parameters:
//   - in: unused
//   - wake: unused
//
// Returns an error, so that the caller reads with a timeout instead.
func awaitKey(*os.File, *os.File) (bool, error) {
	return false, errors.New("descriptor cannot be watched")
}
