package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestReviewTestsDoctorResetWriteErrorSkipsStty checks that a reset whose sequence could not be
// written stops there, with the input being a terminal: no "stty sane" and no confirmation. The
// existing write error test leaves the input a pipe, so it would pass without that rule.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsDoctorResetWriteErrorSkipsStty(t *testing.T) {
	r := newRig(t, newFixture(t))
	r.ttyIn = true
	r.app.Stdout = failingWriter{}
	r.app.IsTerminal = func(io.Writer) bool { return true }

	a := *r.app

	if code := a.run([]string{"doctor", "--reset"}); code != exitFail {
		t.Errorf("code = %d, want 1", code)
	}

	if r.stty != 0 {
		t.Errorf("stty ran %d time after a failed write", r.stty)
	}

	if strings.Contains(r.errb.String(), "terminal reset") {
		t.Errorf("stderr = %q, the reset was announced although it failed", r.errb.String())
	}
}

// TestReviewTestsDoctorFilesUnderClosedDirectory checks that data files which cannot even be
// looked at, because their directory refuses access, are failures and not "missing" or a
// warning: the person must be told that tote cannot read its own files.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsDoctorFilesUnderClosedDirectory(t *testing.T) {
	notRoot(t)

	f := newFixture(t)
	f.conf("connections.toml", "schema = 1\n")
	f.mkdir(".config/tote", 0o000)

	t.Cleanup(func() { _ = os.Chmod(f.path(".config/tote"), 0o700) })

	found := f.run()

	for _, id := range []string{"file.config", "file.connections", "file.aliases", "file.repos"} {
		if got := get(t, found, id); got.Level != levelFail {
			t.Errorf("%s: level %v, want a failure: %s", id, got.Level, text(got))
		}
	}
}
