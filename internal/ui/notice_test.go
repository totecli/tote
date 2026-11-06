package ui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestReviewRunUpdateNotice checks that the version returned by Options.UpdateNotice reaches
// the status line, that the callback gets a live context, and that the context ends with Run.
//
// Parameters:
//   - t: test to report to
func TestReviewRunUpdateNotice(t *testing.T) {
	s := newSandbox(t)

	got := make(chan context.Context, 1)

	s.Opts.UpdateNotice = func(ctx context.Context) string {
		got <- ctx

		return "9.8.7"
	}

	_, err, out := runPaced(t, s, 400*time.Millisecond, "q")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out, "9.8.7") {
		t.Errorf("the status line does not announce 9.8.7: %q", out)
	}

	ctx := <-got
	if ctx.Err() == nil {
		t.Error("the context of the notice is still live after Run returned")
	}
}
