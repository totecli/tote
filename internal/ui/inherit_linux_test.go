package ui

import (
	"fmt"
	"strings"
	"syscall"
	"testing"
)

// TestReviewChildDoesNotInheritDescriptors checks that a program started through the exec
// command does not get the descriptors above 2 that the process holds, such as the ones of the
// input reader of Bubble Tea: the pipe made here is not close-on-exec, like those.
//
// Parameters:
//   - t: test to report to
func TestReviewChildDoesNotInheritDescriptors(t *testing.T) {
	var p [2]int

	if err := syscall.Pipe(p[:]); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = syscall.Close(p[0])
		_ = syscall.Close(p[1])
	}()

	script := fmt.Sprintf("if [ -e /proc/self/fd/%d ]; then exit 3; fi", p[0])
	spec := RunSpec{Argv: []string{"sh", "-c", script}, Env: []string{"PATH=/usr/bin:/bin"}}
	cmd := newExecCommand(osRunner{}, spec, "", "sh", newPrinter("en"), (&clock{}).now, 0)

	cmd.SetStdin(strings.NewReader(""))
	cmd.SetStdout(&strings.Builder{})
	cmd.SetStderr(&strings.Builder{})

	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	if cmd.res.ExitCode != 0 {
		t.Errorf("the child inherited descriptor %d (exit %d)", p[0], cmd.res.ExitCode)
	}
}
