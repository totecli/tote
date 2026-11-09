//go:build !linux && !darwin

package main

// termSize reports that the size of the terminal is unknown on this system.
//
// Returns false.
func termSize() (int, int, bool) {
	return 0, 0, false
}
