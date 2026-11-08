package update

import (
	"context"
	"testing"
)

// TestReviewTestsPickTagPrerelease checks that a tag with a pre-release part is a pre-release
// for the channel rules even when the release list does not flag it, so that a stable install is
// never offered 1.1.0-beta.1 because of a missing flag on the release page.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsPickTagPrerelease(t *testing.T) {
	list := []apiRelease{
		{TagName: "1.0.0"},
		{TagName: "1.1.0-beta.1", Prerelease: false},
	}

	tests := []struct {
		channel Channel
		want    string
	}{
		{ChannelStable, "1.0.0"},
		{ChannelBeta, "1.1.0-beta.1"},
	}

	for _, tt := range tests {
		got, ok := pickLatest(list, tt.channel)
		if !ok || got.Tag != tt.want {
			t.Errorf("pickLatest(%s) = %q, %v, want %q", tt.channel, got.Tag, ok, tt.want)
		}
	}
}

// TestReviewTestsApplyStableUnflaggedBeta checks the same rule through Apply: an unflagged beta
// tag is neither reported nor installed on the stable channel.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsApplyStableUnflaggedBeta(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.0.0"})
	m.publish(fakeRelease{Tag: "1.1.0-beta.1", Prerelease: false})

	u, target := m.updater("1.0.0")

	res, err := u.Apply(context.Background())
	if err != nil || res.Status != StatusUpToDate {
		t.Fatalf("result = %+v, %v", res, err)
	}

	if content(t, target) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}
}

// TestPickLatestVPrefix checks that tags with a leading "v" are read as versions while the raw
// tag is kept for the download URLs, and that a doubled prefix is not a version.
//
// Parameters:
//   - t: test to report to
func TestPickLatestVPrefix(t *testing.T) {
	list := []apiRelease{
		{TagName: "v1.0.0"},
		{TagName: "vv9.0.0"},
		{TagName: "v1.1.0-beta.1"},
	}

	tests := []struct {
		channel Channel
		tag     string
		version string
	}{
		{ChannelStable, "v1.0.0", "1.0.0"},
		{ChannelBeta, "v1.1.0-beta.1", "1.1.0-beta.1"},
	}

	for _, tt := range tests {
		got, ok := pickLatest(list, tt.channel)
		if !ok || got.Tag != tt.tag || got.Version.String() != tt.version {
			t.Errorf("pickLatest(%s) = %q %s, %v, want %q %s",
				tt.channel, got.Tag, got.Version, ok, tt.tag, tt.version)
		}
	}
}
