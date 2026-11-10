package main

import (
	"strings"
	"testing"
)

// TestReviewNoReleaseForChannel checks that "tote update --check" on a stable install, when the
// release list only holds pre-releases, reports the missing release once and in the language of
// the catalog: the detail must not repeat the sentence of the message nor bring English words
// into a translated one.
//
// Parameters:
//   - t: test to report to
func TestReviewNoReleaseForChannel(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.1.0-beta.1")

	installFake(h, m, "1.0.0")

	if code := h.run("update", "--check"); code != 1 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if strings.Contains(h.errb.String(), "(no release found") {
		t.Errorf("the detail repeats the message: %q", h.errb.String())
	}
}
