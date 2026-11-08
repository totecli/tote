package update

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestReviewTestsRefreshNoticeFailedCheck checks that a check that fails after the release was
// found, here because the release has no file for this system, is recorded with an empty latest
// version and announces nothing, as the documentation of RefreshNotice says.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsRefreshNoticeFailedCheck(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0", NoAssets: true})

	u, _ := m.updater("1.0.0")
	path := filepath.Join(t.TempDir(), CacheFile)
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)

	n, err := u.RefreshNotice(context.Background(), path, now)
	if !errors.Is(err, ErrNoAsset) {
		t.Fatalf("error = %v, want ErrNoAsset", err)
	}

	if n.Available || n.Latest != "" {
		t.Errorf("notice = %+v, want nothing announced", n)
	}

	e, _, ok := readCache(path)
	if !ok || e.Latest != "" {
		t.Errorf("cache = %+v, %v, want a recorded failure with no latest version", e, ok)
	}

	if NoticeDue(path, ChannelStable, now.Add(time.Hour)) {
		t.Error("a recorded failure should keep the check quiet for the interval")
	}
}
