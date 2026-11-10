// Package e2e holds the end-to-end tests of the full-screen interface. They build the real
// binary, run it in a private tmux server inside a sandbox (home, data and state directories
// under a temporary directory, a fake ssh and a fake editor first in PATH) and drive it with key
// presses.
//
// One test, in update_test.go, needs neither tmux nor a terminal: it builds two versions of the
// binary and updates the older one from a signed file:// mirror.
//
// Another test, in doctor_test.go, breaks the terminal of a pane and repairs it with "tote doctor
// --reset".
//
// The package has no code of its own, only tests. The tests that need tmux are skipped with a
// message when it is missing, and under go test -short. The single core pass is skipped unless
// TOTE_SLOW_TESTS=1 is set: see the slow target of the Makefile.
//
// A test never waits for text that is already on the screen. It waits for a state that its own
// action has just produced: a line added to a log by the fake program, a file written by the
// wrapper, a changed status line, a new process, a terminal mode that flips.
package e2e
