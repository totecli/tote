package update

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestReviewTestsChecksumExactName checks that the signed checksums are matched on the whole
// file name: a line whose name merely ends with the archive name, carrying the right digest, is
// not the archive's line, and neither is a line with extra fields.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsChecksumExactName(t *testing.T) {
	const tag = "1.1.0"

	archive := ArchiveName(tag, "linux", "amd64")
	good := comment(tag, ChecksumsName(tag), "stable", testStamp)

	tests := []struct {
		name string
		edit func(line string) string
	}{
		{"name with a prefix", func(l string) string { return strings.Replace(l, "  ", "  evil_", 1) }},
		{"name in a directory", func(l string) string { return strings.Replace(l, "  ", "  dist/", 1) }},
		{"extra field", func(l string) string { return strings.TrimSuffix(l, "\n") + " extra\n" }},
	}

	for _, tt := range tests {
		m := newMirror(t, newSigner(1))
		m.publish(fakeRelease{Tag: tag})

		u, target := m.updater("1.0.0")
		resign(m, tag, tt.edit(sumLine(t, m, tag, archive)), good)

		_, err := u.Apply(context.Background())
		if !errors.Is(err, ErrBadChecksum) {
			t.Errorf("%s: error = %v, want ErrBadChecksum", tt.name, err)
		}

		if content(t, target) != fakeBinary("1.0.0") {
			t.Errorf("%s: the binary changed", tt.name)
		}
	}
}
