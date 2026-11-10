package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// modalSSH is a fake ssh whose session turns on every mouse mode a program may ask for, and
// leaves them on: a long session of a full-screen program that is killed would do the same.
const modalSSH = `#!/bin/sh
case " $* " in
*" -G "*) printf 'serveraliveinterval 15\nserveralivecountmax 3\n'; exit 0 ;;
esac
echo "$$ $*" >> "$E2E_DIR/ssh.log"
printf '\033[?1000h\033[?1003h\033[?1006h\033[?1015h\033[?1005h\033[?1004h\033[?2004h'
echo "modal-ssh-token"
sleep 1
case "$*" in *fail*) exit 255 ;; esac
exit 0
`

// TestReviewMouseChildModesDoNotLeak checks that the mouse modes a program turned on and left on
// are gone when the interface comes back: while the program runs the terminal reports what the
// program asked for, during the "press Enter" pause nothing is reported, and when the list is
// back only the modes of the interface are on, all-motion reporting with SGR coordinates and no
// button and drag mode. It passes, it pins what was checked on the real binary.
//
// Parameters:
//   - t: test to report to
func TestReviewMouseChildModesDoNotLeak(t *testing.T) {
	sb := newSandbox(t)
	sb.write(t, filepath.Join(sb.Bin, "ssh"), modalSSH)

	if err := os.Chmod(filepath.Join(sb.Bin, "ssh"), 0o700); err != nil {
		t.Fatal(err)
	}

	tm := newTerm(t, sb, startOptions{}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.waitMouse(wantMouse)

	for _, name := range []string{"bravo", "fail"} {
		col, row := tm.where(name+".example.com", false)
		tm.click(col, row)
		time.Sleep(60 * time.Millisecond)
		tm.click(col, row)

		tm.waitText("modal-ssh-token")

		if name == "fail" {
			tm.waitFor("the pause", func(s snap) bool { return s.has("255") })
			tm.waitMouse(off)
			tm.key("Enter")
		}

		tm.waitFor("the list again", func(s snap) bool {
			return !s.has("modal-ssh-token") && s.has("alpha.example.com") && s.Alt
		})
		tm.waitMouse(wantMouse)
	}

	tm.quit()
	tm.waitMouse(off)
}

// TestReviewMouseDoubleClickOnTheMenuOnlyOpensTheList sends a double click to an entry of the
// welcome menu of a short window and checks that no connection is started. It fails: the first
// click opens the list at once, the second one lands on its first row, at the same line and with
// the same number as the entry, and counts as a double click on it, so ssh runs for the first
// connection.
//
// Parameters:
//   - t: test to report to
func TestReviewMouseDoubleClickOnTheMenuOnlyOpensTheList(t *testing.T) {
	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 18}, "tote")

	tm.waitText("Connections")
	tm.waitMouse(wantMouse)

	col, row := tm.where("Connections", false)
	tm.click(col, row)
	time.Sleep(60 * time.Millisecond)
	tm.click(col, row)

	tm.waitText("alpha.example.com")
	time.Sleep(1500 * time.Millisecond)

	if got := lines(filepath.Join(sb.Out, "ssh.log")); got != 0 {
		t.Errorf("a double click on the welcome menu started ssh %d time(s)", got)
	}

	tm.quit()
}
