package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRefreshNotice checks the cache of the status line: one request, then none for a day.
//
// Parameters:
//   - t: test to report to
func TestRefreshNotice(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, _ := m.updater("1.0.0")
	cache := filepath.Join(t.TempDir(), "cache", CacheFile)
	t0 := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)

	if _, ok := ReadNotice(cache, "1.0.0", ChannelStable); ok {
		t.Error("a notice exists before any check")
	}

	if !NoticeDue(cache, ChannelStable, t0) {
		t.Error("no cache: a check should be due")
	}

	n, err := u.RefreshNotice(context.Background(), cache, t0)
	if err != nil || !n.Available || n.Latest != "1.1.0" || n.Current != "1.0.0" ||
		!n.CheckedAt.Equal(t0) {
		t.Fatalf("first refresh = %+v, %v", n, err)
	}

	info, err := os.Stat(cache)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("cache file: %v, %v", info, err)
	}

	// The mirror disappears: within a day the cache answers and nothing is requested.
	u.Source = Source{Base: "file://" + filepath.Join(t.TempDir(), "gone")}

	n, err = u.RefreshNotice(context.Background(), cache, t0.Add(23*time.Hour))
	if err != nil || !n.Available || n.Latest != "1.1.0" {
		t.Errorf("cached refresh = %+v, %v", n, err)
	}

	within, after := t0.Add(23*time.Hour), t0.Add(24*time.Hour)
	if NoticeDue(cache, ChannelStable, within) || !NoticeDue(cache, ChannelStable, after) {
		t.Error("the interval is not one day")
	}

	if !NoticeDue(cache, ChannelStable, t0.Add(-time.Hour)) {
		t.Error("a cache from the future should be due")
	}

	if !NoticeDue(cache, ChannelBeta, t0.Add(time.Minute)) {
		t.Error("a cache of another channel should be due")
	}

	if _, ok := ReadNotice(cache, "1.0.0", ChannelBeta); ok {
		t.Error("a cache of another channel was used")
	}

	// A day later the failed check is recorded, so that an offline machine waits again.
	t1 := t0.Add(25 * time.Hour)

	n, err = u.RefreshNotice(context.Background(), cache, t1)
	if err == nil || n.Available || n.Latest != "" {
		t.Errorf("failed refresh = %+v, %v", n, err)
	}

	if NoticeDue(cache, ChannelStable, t1.Add(time.Hour)) {
		t.Error("a failed check was not recorded")
	}

	n, ok := ReadNotice(cache, "1.0.0", ChannelStable)
	if !ok || n.Available || !n.CheckedAt.Equal(t1) {
		t.Errorf("notice after failure = %+v, %v", n, ok)
	}
}

// TestNoticeStates checks Available for the usual running versions.
//
// Parameters:
//   - t: test to report to
func TestNoticeStates(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"1.0.0", "1.1.0", true},
		{"1.1.0", "1.1.0", false},
		{"1.2.0", "1.1.0", false},
		{"1.1.0-beta.1", "1.1.0", true},
		{"dev", "1.1.0", false},
		{"1.0.0", "", false},
	}

	for _, tt := range tests {
		n := notice(cacheEntry{Latest: tt.latest, Channel: "stable"}, tt.current, time.Time{})
		if n.Available != tt.want {
			t.Errorf("notice(%q, %q).Available = %v", tt.current, tt.latest, n.Available)
		}
	}
}

// TestReadCacheHostile checks that a damaged cache file is ignored and never shown.
//
// Parameters:
//   - t: test to report to
func TestReadCacheHostile(t *testing.T) {
	dir := t.TempDir()
	entry := func(schema, latest string) string {
		return `{"schema":` + schema + `,"checked_at":"2026-11-03T08:00:00Z",` +
			`"channel":"stable","latest":"` + latest + `"}`
	}
	good := entry("1", "1.1.0")
	tests := map[string]string{
		"not json":      "nope",
		"wrong schema":  entry("2", "1.1.0"),
		"bad time":      `{"schema":1,"checked_at":"yesterday","channel":"stable","latest":"1.1.0"}`,
		"escape latest": entry("1", `\u001b[31m1.1.0`),
		"text latest":   entry("1", "update now!"),
		"too large":     good + strings.Repeat(" ", 5000),
	}

	for name, text := range tests {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}

		if n, ok := ReadNotice(path, "1.0.0", ChannelStable); ok {
			t.Errorf("%s: notice %+v accepted", name, n)
		}

		if !NoticeDue(path, ChannelStable, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)) {
			t.Errorf("%s: a damaged cache should make the check due", name)
		}
	}

	path := filepath.Join(dir, "good")
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}

	if n, ok := ReadNotice(path, "1.0.0", ChannelStable); !ok || !n.Available {
		t.Errorf("good cache: %+v, %v", n, ok)
	}

	if _, ok := ReadNotice(dir, "1.0.0", ChannelStable); ok {
		t.Error("a directory was read as a cache")
	}
}

// TestRefreshNoticeCacheUnwritable checks that a cache that cannot be written is reported
// but still gives the notice.
//
// Parameters:
//   - t: test to report to
func TestRefreshNoticeCacheUnwritable(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, _ := m.updater("1.0.0")

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := u.RefreshNotice(context.Background(), filepath.Join(blocker, CacheFile), time.Now())
	if err == nil || !n.Available {
		t.Errorf("notice = %+v, %v", n, err)
	}

	if errors.Is(err, ErrOffline) {
		t.Errorf("unexpected error kind: %v", err)
	}
}
