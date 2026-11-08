package update

import (
	"context"
	"os"
	"testing"
)

// TestReviewSecurityKeepsBinaryMode checks that an update does not widen the mode of the binary.
//
// A binary installed with mode 0700 (private to its owner) is replaced by a file with mode 0755,
// whatever the previous mode was. The test currently fails.
//
// Parameters:
//   - t: test to report to
func TestReviewSecurityKeepsBinaryMode(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, target := m.updater("1.0.0")
	if err := os.Chmod(target, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("mode after update = %o, want 700", got)
	}
}

// TestReviewSecurityTamperedListCannotInstall pins that an unauthenticated release list cannot
// make tote install anything the key did not sign: a list that advertises a higher version than
// the one the signed files describe is refused and the binary is untouched.
//
// Parameters:
//   - t: test to report to
func TestReviewSecurityTamperedListCannotInstall(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	// Serve the signed files of 1.1.0 under the directory of a fake 9.9.9 release.
	for _, n := range []string{
		ArchiveName("1.1.0", "linux", "amd64"), ChecksumsName("1.1.0"),
		SignatureName("1.1.0"),
	} {
		data, err := os.ReadFile(m.path("1.1.0", n))
		if err != nil {
			t.Fatal(err)
		}

		fake := ArchiveName("9.9.9", "linux", "amd64")
		switch n {
		case ChecksumsName("1.1.0"):
			fake = ChecksumsName("9.9.9")
		case SignatureName("1.1.0"):
			fake = SignatureName("9.9.9")
		}

		m.write("9.9.9", fake, data)
	}

	m.releases = append(m.releases, apiRelease{TagName: "9.9.9", Assets: []apiAsset{
		{Name: ArchiveName("9.9.9", "linux", "amd64")},
		{Name: ChecksumsName("9.9.9")},
		{Name: SignatureName("9.9.9")},
	}})
	m.list()

	u, target := m.updater("1.0.0")
	before := content(t, target)

	if _, err := u.Apply(context.Background()); err == nil {
		t.Fatal("a renamed signed release was installed")
	}

	if content(t, target) != before {
		t.Error("the binary changed")
	}
}
