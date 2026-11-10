package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// noKeepalive is what the fake ssh reports with -G for a host that has no keepalive of its
	// own, which is what ssh itself reports by default.
	noKeepalive = "serveraliveinterval 0\nserveralivecountmax 3\n"

	// keepOptions are the options that the interface adds to such a host.
	keepOptions = "-o ServerAliveInterval=30 -o ServerAliveCountMax=3 "
)

// TestKeepaliveAdded checks that a host without keepalive gets the options on the command that
// runs and in the banner, that the status line shows the base command until then, that ssh -G is
// never asked while the cursor moves, and that the lookup was made with the options of the
// connection and nothing else.
//
// Parameters:
//   - t: test to report to
func TestKeepaliveAdded(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, filepath.Join(sb.Out, "ssh.conf"), noKeepalive)

	tm := newTerm(t, sb, startOptions{W: 140, H: 30}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.key("End", "Up", "Home", "Down", "Up")
	tm.waitText(cmdOf("alpha"))

	if tm.look().has("ServerAlive") {
		t.Error("the status line shows keepalive options before the connection starts")
	}

	if _, err := os.Stat(filepath.Join(sb.Out, "ssh.probe")); err == nil {
		t.Error("ssh -G was run while the cursor moved")
	}

	tm.key("Enter")
	tm.waitText("fake-ssh-token")
	tm.waitFor("the list again", func(s snap) bool {
		return !s.has("fake-ssh-token") && s.has("alpha.example.com") && s.Alt
	})

	log := strings.TrimSpace(readFile(t, filepath.Join(sb.Out, "ssh.log")))
	if !strings.HasSuffix(log, " "+keepOptions+"-l deploy -- alpha.example.com") ||
		strings.Count(log, "\n") != 0 {
		t.Errorf("ssh was started as %q", log)
	}

	if probe := readFile(t, filepath.Join(sb.Out, "ssh.probe")); !strings.Contains(probe,
		"-G -l deploy -- alpha.example.com") || strings.Contains(probe, "ServerAlive") {
		t.Errorf("the lookup was %q", probe)
	}

	tm.quit()
}

// TestKeepaliveKept checks that a host that has its own keepalive is left as it is.
//
// Parameters:
//   - t: test to report to
func TestKeepaliveKept(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, filepath.Join(sb.Out, "ssh.conf"),
		"serveraliveinterval 60\nserveralivecountmax 3\n")

	tm := newTerm(t, sb, startOptions{W: 140, H: 30}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.key("Enter")
	tm.waitText("fake-ssh-token")
	tm.waitFor("the list again", func(s snap) bool {
		return !s.has("fake-ssh-token") && s.has("alpha.example.com") && s.Alt
	})

	log := strings.TrimSpace(readFile(t, filepath.Join(sb.Out, "ssh.log")))
	if strings.Contains(log, "ServerAlive") ||
		!strings.HasSuffix(log, " -l deploy -- alpha.example.com") {
		t.Errorf("ssh was started as %q", log)
	}

	tm.quit()
}

// TestAfterDisconnectExit checks that connect.after_disconnect = "exit" ends the interface once
// the connection ends well, prints nothing more and restores the terminal, while a connection
// that fails keeps the interface and its pause.
//
// Parameters:
//   - t: test to report to
func TestAfterDisconnectExit(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, filepath.Join(sb.Dir, "config", "config.toml"),
		"[connect]\nafter_disconnect = \"exit\"\n")

	tm := newTerm(t, sb, startOptions{}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.key("End", "Up")
	tm.waitText(cmdOf("fail"))
	tm.key("Enter")
	tm.waitText("press Enter")
	tm.key("Enter")
	tm.waitGone("press Enter")
	tm.waitText(cmdOf("fail"))

	tm.key("Home")
	tm.waitText(cmdOf("alpha"))
	tm.key("Enter")

	if code := tm.waitDone(); code != 0 {
		t.Errorf("exit status %d, want 0", code)
	}

	tm.restored()

	if got := lines(filepath.Join(sb.Out, "ssh.log")); got != 2 {
		t.Errorf("the ssh log has %d lines, want 2", got)
	}
}

// TestCtrlCAtPause presses Ctrl-C at the "press Enter" pause after a failed connection: it
// returns to the list as Enter does, tote is not killed, and the terminal is not left raw.
//
// Parameters:
//   - t: test to report to
func TestCtrlCAtPause(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.key("End", "Up")
	tm.waitText(cmdOf("fail"))

	// The text of a pause stays on the main screen, so the pauses are counted.
	for round, key := range []string{"C-c", "C-d"} {
		tm.key("Enter")
		tm.waitFor("a new pause", func(s snap) bool {
			return strings.Count(s.Text, "press Enter") > round
		})
		tm.key(key)
		tm.waitFor("the list again", func(s snap) bool {
			return s.Alt && s.has(cmdOf("fail"))
		})
	}

	tm.key("Up")
	tm.waitText(cmdOf("bravo"))
	tm.quit()
}
