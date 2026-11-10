package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// rounds is the number of ssh round trips of the single core pass.
const rounds = 30

// firstCPU returns the first processor that the test process may run on.
//
// Parameters:
//   - t: running test
func firstCPU(t *testing.T) string {
	t.Helper()

	out, err := exec.Command("taskset", "-cp", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Skipf("taskset cannot read the affinity: %v", err)
	}

	list := strings.TrimSpace(string(out))
	list = list[strings.LastIndex(list, ":")+1:]

	cpu := strings.FieldsFunc(strings.TrimSpace(list), func(r rune) bool {
		return r == '-' || r == ','
	})
	if len(cpu) == 0 {
		t.Skipf("no processor in %q", list)
	}

	return cpu[0]
}

// TestSingleCore repeats the ssh round trip 30 times with the interface confined to one
// processor, where a renderer that has not settled before the terminal is released freezes the
// screen. It uses taskset, or GOMAXPROCS=1 when taskset is missing. Every round must come back
// to the list, and the first key pressed afterwards must be received.
//
// It is skipped unless TOTE_SLOW_TESTS=1 is set: run it with make slow.
//
// Parameters:
//   - t: test to report to
func TestSingleCore(t *testing.T) {
	if os.Getenv("TOTE_SLOW_TESTS") != "1" {
		t.Skip("the single core pass is slow: set TOTE_SLOW_TESTS=1 (make slow) to run it")
	}

	sb := newSandbox(t)
	opts := startOptions{Env: []string{"FAKE_SSH_PAUSE=0.15", "E2E_AFFINITY=1"}}
	mode := "GOMAXPROCS=1"

	if _, err := exec.LookPath("taskset"); err == nil {
		cpu := firstCPU(t)
		opts.Prefix = []string{"taskset", "-c", cpu}
		mode = "taskset -c " + cpu
	} else {
		opts.Env = append(opts.Env, "GOMAXPROCS=1")
	}

	t.Logf("single core pass: %s", mode)

	tm := newTerm(t, sb, opts, "tote", "co")
	tm.waitText(cmdOf("alpha"))

	cur, other := "alpha", "bravo"

	for i := 1; i <= rounds; i++ {
		tm.key("Enter")
		tm.waitText("fake-ssh-token")
		tm.waitFor("round "+strconv.Itoa(i)+": the list again", func(s snap) bool {
			return !s.has("fake-ssh-token") && s.has(cmdOf(cur)) && s.Alt
		})

		if got := lines(filepath.Join(sb.Out, "ssh.log")); got != i {
			t.Fatalf("round %d: the ssh log has %d lines", i, got)
		}

		if cur == "alpha" {
			tm.key("Down")
		} else {
			tm.key("Up")
		}

		tm.waitText(cmdOf(other))

		cur, other = other, cur
	}

	if opts.Prefix != nil {
		aff := readFile(t, filepath.Join(sb.Out, "ssh.aff"))
		if want := "affinity list: " + opts.Prefix[2] + "\n"; !strings.Contains(aff, want) {
			t.Errorf("the child was not confined to one processor: %q", aff)
		}
	}

	tm.quit()
}
