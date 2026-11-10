package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/update"
)

// englishDetail matches the English words that the update detail lines are built from.
var englishDetail = regexp.MustCompile(`signed at|installed release|expected|announces`)

// TestReviewFrenchUpdateErrorsAreFrench pins that the detail added to a French update failure is
// not English text. A rollback refusal currently reads "(signed at 1760000200, installed release
// signed at 1770000000)" inside a French sentence, with raw Unix times.
//
// Parameters:
//   - t: test to report to
func TestReviewFrenchUpdateErrorsAreFrench(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.1.0")

	target := installFake(h, m, "1.0.0")
	writeReceipt(h, update.Receipt{
		InstallMethod: "script", ReleaseTimestamp: testStamp + 1,
		Path: target,
	})

	h.vars["TOTE_LANG"] = "fr"

	if code := h.run("update"); code != 1 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if !strings.Contains(h.errb.String(), "plus ancienne") {
		t.Fatalf("not the French rollback message: %q", h.errb.String())
	}

	if m := englishDetail.FindString(h.errb.String()); m != "" {
		t.Errorf("English text %q inside the French message: %q", m, h.errb.String())
	}
}

// TestReviewNotWritableHintHasNoControlBytes pins that the sudo command suggested when the
// install directory is read-only carries no terminal control byte, as the other paths of the
// message do. A binary path with an escape sequence currently reaches the terminal unchanged
// through shell.Quote.
//
// Parameters:
//   - t: test to report to
func TestReviewNotWritableHintHasNoControlBytes(t *testing.T) {
	h := newHarness(t)
	exe := "/opt/x\x1b]0;pwn\x07/tote"

	key, args := h.app.updateErrorMessage(
		&update.NotWritableError{Dir: "/opt/x\x1b]0;pwn\x07"}, installCtx{exe: exe},
	)

	if msg := h.app.t(key, args...); strings.ContainsAny(msg, "\x1b\x07") {
		t.Errorf("control bytes in %q", msg)
	}
}
