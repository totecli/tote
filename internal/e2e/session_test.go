package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// reviewConnCount is the number of connections of the long list.
	reviewConnCount = 150

	// slowProbeSSH is a fake ssh whose "-G" lookup marks its start (S) and its end (E) in
	// ssh.live and lasts a while, so that the test can count the lookups that overlap.
	slowProbeSSH = `#!/bin/sh
case " $* " in
*" -G "*)
    echo S >> "$E2E_DIR/ssh.live"
    sleep 0.6
    echo E >> "$E2E_DIR/ssh.live"
    printf 'serveraliveinterval 0\nserveralivecountmax 3\n'
    exit 0
    ;;
esac
echo "$$ $*" >> "$E2E_DIR/ssh.log"
exit 0
`
)

// toteChild returns the process id of the program that runs in the pane.
//
// Parameters:
//   - m: terminal whose pane is looked at
func toteChild(m *term) int {
	m.t.Helper()

	requireTool(m.t, "pgrep")

	out, err := m.run("list-panes", "-t", m.Sess, "-F", "#{pane_pid}")
	if err != nil {
		m.t.Fatalf("list-panes: %v", err)
	}

	pane := strings.TrimSpace(out)

	kids, err := exec.Command("pgrep", "-P", pane, "-x", "tote").Output()
	if err != nil {
		m.t.Fatalf("no tote below the pane process %s: %v", pane, err)
	}

	pid, err := strconv.Atoi(strings.Fields(string(kids))[0])
	if err != nil {
		m.t.Fatal(err)
	}

	return pid
}

// TestReviewSigtermAtPause sends SIGTERM to tote while it waits for Enter after a failed
// connection. A termination request must end the program as it does on the list; today the
// signal is kept for the end of the program, which has already ended, so tote sits there until
// a key is pressed and a plain "kill" does nothing.
//
// Parameters:
//   - t: test to report to
func TestReviewSigtermAtPause(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.key("End", "Up")
	tm.waitText(cmdOf("fail"))
	tm.key("Enter")
	tm.waitText("press Enter")

	if err := syscall.Kill(toteChild(tm), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	tm.waitDone()
	tm.restored()
}

// TestReviewHeldArrowRunsNoLookup runs the cursor down a long list of connections in one go. The
// "ssh -G" lookup can run the Match exec commands of ssh_config, so no row that the cursor
// crosses may start one: the fake ssh must not see a single -G until Enter connects, and then it
// sees exactly one.
//
// Parameters:
//   - t: test to report to
func TestReviewHeldArrowRunsNoLookup(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, filepath.Join(sb.Bin, "ssh"), slowProbeSSH)

	if err := os.Chmod(filepath.Join(sb.Bin, "ssh"), 0o700); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder

	b.WriteString("schema = 1\n")

	for i := range reviewConnCount {
		n := strconv.Itoa(1000 + i)
		b.WriteString("\n[[connection]]\nname = \"h" + n + "\"\ntarget = \"h" + n +
			".example.com\"\nuser = \"deploy\"\n")
	}

	sb.write(t, filepath.Join(sb.Dir, "config", "connections.toml"), b.String())

	tm := newTerm(t, sb, startOptions{W: 120, H: 30}, "tote", "co")
	tm.waitText("h1000")

	downs := make([]string, 0, reviewConnCount-1)
	for range reviewConnCount - 1 {
		downs = append(downs, "Down")
	}

	tm.key(downs...)
	tm.waitText("h" + strconv.Itoa(1000+reviewConnCount-1))
	time.Sleep(1500 * time.Millisecond)

	if _, err := os.Stat(filepath.Join(sb.Out, "ssh.live")); err == nil {
		t.Errorf("ssh -G was run while the cursor crossed the list: %q",
			readFile(t, filepath.Join(sb.Out, "ssh.live")))
	}

	tm.key("Enter")
	tm.waitFile("ssh.log", func(c string) bool { return strings.Contains(c, "-l deploy") })

	live := readFile(t, filepath.Join(sb.Out, "ssh.live"))
	if strings.Count(live, "S") != 1 || strings.Count(live, "E") != 1 {
		t.Errorf("the connection made lookups %q, want exactly one", live)
	}

	tm.quit()
}

// openSettingsAndChange opens the settings of a sandbox whose config.toml was edited behind the
// back of the running interface, then changes the choice "After a connection".
//
// Parameters:
//   - t: test to report to
//   - sb: sandbox, whose config.toml holds userConfig
//   - edited: text that config.toml holds when the interface changes the setting
//   - width: width of the terminal
func openSettingsAndChange(t *testing.T, sb *sandbox, edited string, width int) *term {
	t.Helper()

	sb.write(t, configPath(sb), userConfig)

	tm := newTerm(t, sb, startOptions{W: width, H: 30}, "tote")

	tm.waitText("Connections")
	tm.typeText(",")
	tm.waitText("After a connection")

	sb.write(t, configPath(sb), edited)

	tm.key("Down", "Down", "Down", "Enter")
	tm.waitText("exit (quit on a normal end)")
	tm.key("Down", "Enter")

	return tm
}

// TestReviewConfigExternalEditKept edits config.toml from outside while the settings are open
// and then changes another setting in the interface: the outside edit and its comment must
// survive the write.
//
// Parameters:
//   - t: test to report to
func TestReviewConfigExternalEditKept(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	edited := strings.Replace(userConfig, `compact = "never"`,
		"compact = \"never\"\n# added by hand\ntheme = \"dark\"  # night", 1)

	tm := openSettingsAndChange(t, sb, edited, 100)
	tm.waitText("saved: After a connection")

	got := tm.waitFile("../config/config.toml", func(c string) bool {
		return strings.Contains(c, `after_disconnect = "exit"`)
	})

	for _, want := range []string{
		"# added by hand", `theme = "dark"  # night`, "# my own note",
		`editor = "nano -w"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config.toml lost %q:\n%s", want, got)
		}
	}

	tm.key("Escape")
	tm.waitText("Your terminal tools")
	tm.quit()
}

// TestReviewConfigRowsFollowFile edits config.toml from outside while the settings are open and
// then changes another setting: the row of the edited key must show what the file says, as its
// origin column does. Today the origin says config.toml and the value is the one read at start.
//
// Parameters:
//   - t: test to report to
func TestReviewConfigRowsFollowFile(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	edited := strings.Replace(userConfig, `compact = "never"`,
		"compact = \"never\"\ntheme = \"dark\"", 1)

	tm := openSettingsAndChange(t, sb, edited, 100)
	tm.waitText("saved: After a connection")

	s := tm.look()

	for line := range strings.SplitSeq(s.Text, "\n") {
		if strings.Contains(line, "Theme ") && strings.Contains(line, "Color scheme") {
			if !strings.Contains(line, "dark") {
				t.Errorf("the theme row does not follow the file: %q", strings.TrimSpace(line))
			}

			tm.quit()

			return
		}
	}

	t.Errorf("no theme row on the screen:\n%s", s.Text)
}

// TestReviewConfigBrokenMidSession breaks config.toml behind the back of the interface and then
// changes a setting: nothing may be written and the file must stay byte for byte as it was.
//
// Parameters:
//   - t: test to report to
func TestReviewConfigBrokenMidSession(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	broken := userConfig + "oops = = 1\n"

	tm := openSettingsAndChange(t, sb, broken, 240)
	tm.waitText("syntax error")

	if got := readFile(t, configPath(sb)); got != broken {
		t.Errorf("config.toml was changed:\n%s", got)
	}

	tm.key("Escape")
	tm.waitText("Your terminal tools")
	tm.quit()
}

// TestReviewInvalidValueReasonVisible starts with a config.toml of a schema that tote does not
// know, the one fault that still refuses the whole file: the settings screen says that the file
// is read-only, and the reason must be readable on an ordinary 100 column terminal, not cut off
// behind the path.
//
// Parameters:
//   - t: test to report to
func TestReviewInvalidValueReasonVisible(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, configPath(sb), "schema = 2\n[ui]\ntheme = \"dark\"\n")

	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")

	tm.waitText("Connections")
	tm.typeText(",")
	tm.waitText("read-only")

	if s := tm.look(); !strings.Contains(s.Text, "schema is 2") &&
		!strings.Contains(s.Text, "only schema 1") {
		t.Errorf("the reason is not on the screen:\n%s", s.Text)
	}

	tm.quit()
}
