package e2e

import (
	"testing"
)

// TestCtrlCTwiceQuits checks that one Ctrl-C on the welcome page only says how to quit, that a
// second one quits, and that the terminal is restored.
//
// Parameters:
//   - t: test to report to
func TestCtrlCTwiceQuits(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{}, "tote")

	tm.waitText("Connections")
	tm.key("C-c")
	tm.waitText("Press ctrl+c again to quit")

	if s := tm.look(); s.Dead || !s.Alt {
		t.Fatalf("one Ctrl-C ended the interface:\n%s", s.Text)
	}

	tm.key("C-c")

	if code := tm.waitDone(); code != 0 {
		t.Errorf("exit status %d after two Ctrl-C, want 0", code)
	}

	tm.restored()
}

// TestCtrlDQuits checks that Ctrl-D quits at once, from a list, and that the terminal is
// restored.
//
// Parameters:
//   - t: test to report to
func TestCtrlDQuits(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{}, "tote")

	tm.waitText("Connections")
	tm.typeText("1")
	tm.waitText(cmdOf("alpha"))
	tm.key("C-d")

	if code := tm.waitDone(); code != 0 {
		t.Errorf("exit status %d after Ctrl-D, want 0", code)
	}

	tm.restored()
}

// TestCtrlCGoesBack checks that Ctrl-C leaves a list for the welcome page and closes an overlay
// without quitting, and that the letter R does nothing.
//
// Parameters:
//   - t: test to report to
func TestCtrlCGoesBack(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{}, "tote")

	tm.waitText("Connections")
	tm.typeText("1")
	tm.waitText(cmdOf("alpha"))

	tm.typeText("r")
	tm.typeText("R")
	tm.waitText(cmdOf("alpha"))

	tm.typeText("n")
	tm.waitText("New connection")
	tm.key("C-c")
	tm.waitGone("New connection")
	tm.waitText(cmdOf("alpha"))

	tm.key("C-c")
	tm.waitGone(cmdOf("alpha"))
	tm.waitText("Connections")

	if s := tm.look(); s.Dead || !s.Alt {
		t.Fatalf("Ctrl-C quit instead of going back:\n%s", s.Text)
	}

	tm.quit()
}

// TestDiscardAsksBeforeLosingText checks that Esc on a form with typed text asks first, that
// Enter keeps the form, and that Ctrl-D with the form dirty asks before it quits.
//
// Parameters:
//   - t: test to report to
func TestDiscardAsksBeforeLosingText(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{}, "tote")

	tm.waitText("Connections")
	tm.typeText("1")
	tm.waitText(cmdOf("alpha"))
	tm.typeText("n")
	tm.waitText("New connection")
	tm.typeText("zulu")
	tm.key("Escape")
	tm.waitText("Discard changes?")
	tm.key("Enter")
	tm.waitGone("Discard changes?")
	tm.waitText("zulu")

	tm.key("C-d")
	tm.waitText("Quit?")
	tm.key("Escape")
	tm.waitGone("Quit?")
	tm.waitText("zulu")

	tm.key("C-c")
	tm.waitText("Discard changes?")
	tm.typeText("y")
	tm.waitGone("zulu")
	tm.waitText(cmdOf("alpha"))
	tm.quit()
}
