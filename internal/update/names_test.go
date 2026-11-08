package update

import "testing"

// TestReviewTestsAssetNames pins the names of the release files to the ones the release tooling
// produces (goreleaser name templates and the minisign signature naming). The mirrors of the
// other tests build their files with the same functions, so a wrong name would never show there.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsAssetNames(t *testing.T) {
	tests := []struct {
		got  string
		want string
	}{
		{ArchiveName("1.1.0", "linux", "amd64"), "tote_1.1.0_linux_amd64.tar.gz"},
		{ArchiveName("1.1.0-beta.1", "darwin", "arm64"), "tote_1.1.0-beta.1_darwin_arm64.tar.gz"},
		{ChecksumsName("1.1.0"), "tote_1.1.0_checksums.txt"},
		{SignatureName("1.1.0"), "tote_1.1.0_checksums.txt.minisig"},
		{SignatureName("1.1.0-beta.1"), "tote_1.1.0-beta.1_checksums.txt.minisig"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("name = %q, want %q", tt.got, tt.want)
		}
	}
}

// TestReviewTestsSupportedSystems pins the systems that have a release build: the four
// combinations of Linux and macOS with amd64 and arm64, and nothing else.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsSupportedSystems(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         bool
	}{
		{"linux", "amd64", true},
		{"linux", "arm64", true},
		{"darwin", "amd64", true},
		{"darwin", "arm64", true},
		{"linux", "386", false},
		{"linux", "arm", false},
		{"windows", "amd64", false},
		{"freebsd", "amd64", false},
		{"", "", false},
	}

	for _, tt := range tests {
		if got := supported(tt.goos, tt.goarch); got != tt.want {
			t.Errorf("supported(%q, %q) = %v, want %v", tt.goos, tt.goarch, got, tt.want)
		}
	}
}
