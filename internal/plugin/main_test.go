package plugin

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

// sessionProbeArg makes the test binary print its session id, process group id and process id
// and exit, so that a test can ask a child what session it was given on any system.
const sessionProbeArg = "tote-session-probe"

// TestMain opens the exec tier for the tests of this package: the implementation stays in place
// and most of its tests exercise it. The tests of the 1.0 refusal close it again with
// withExecTier.
//
// Parameters:
//   - m: tests to run
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == sessionProbeArg {
		sid, _ := getsid(0)
		fmt.Println(sid, syscall.Getpgrp(), os.Getpid())
		os.Exit(0)
	}

	execTierAvailable = true

	os.Exit(m.Run())
}

// withExecTier sets the switch of the exec tier for one test and restores it afterwards.
//
// Parameters:
//   - t: test to report to
//   - on: value of the switch during the test
func withExecTier(t *testing.T, on bool) {
	t.Helper()

	old := execTierAvailable
	execTierAvailable = on

	t.Cleanup(func() { execTierAvailable = old })
}
