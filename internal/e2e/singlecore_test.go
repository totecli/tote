package e2e

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// quickRounds is the number of round trips of TestReviewSingleCoreQuickChild: without the pause
// before the program starts about one run of 30 rounds in two freezes the screen, so 100 rounds
// make a missed freeze unlikely.
const quickRounds = 100

// TestReviewSingleCoreQuickChild repeats the round trip of TestSingleCore with a program that
// returns at once. TestSingleCore uses a fake ssh that sleeps 150 ms, which gives the renderer
// time to settle by itself: with the pause before the start of the program removed (execDelay
// set to zero) it still passes. A program that returns immediately is the case the pause exists
// for, and freezes the screen on one processor without it.
//
// It is skipped unless TOTE_SLOW_TESTS=1 is set: run it with make slow.
//
// Parameters:
//   - t: test to report to
func TestReviewSingleCoreQuickChild(t *testing.T) {
	if os.Getenv("TOTE_SLOW_TESTS") != "1" {
		t.Skip("the single core pass is slow: set TOTE_SLOW_TESTS=1 (make slow) to run it")
	}

	sb := newSandbox(t)
	opts := startOptions{Env: []string{"FAKE_SSH_PAUSE=0"}}

	if _, err := exec.LookPath("taskset"); err == nil {
		opts.Prefix = []string{"taskset", "-c", firstCPU(t)}
	} else {
		opts.Env = append(opts.Env, "GOMAXPROCS=1")
	}

	tm := newTerm(t, sb, opts, "tote", "co")
	tm.waitText(cmdOf("alpha"))

	cur, other := "alpha", "bravo"

	for i := 1; i <= quickRounds; i++ {
		tm.key("Enter")
		tm.waitFor("round "+strconv.Itoa(i)+": the list again", func(s snap) bool {
			return lines(sb.Out+"/ssh.log") == i && !s.has("fake-ssh-token") && s.has(cmdOf(cur)) &&
				s.Alt
		})

		if cur == "alpha" {
			tm.key("Down")
		} else {
			tm.key("Up")
		}

		tm.waitText(cmdOf(other))

		cur, other = other, cur
	}

	tm.quit()
}
