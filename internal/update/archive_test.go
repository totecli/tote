package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestUnpack checks which archives give a binary and which are refused.
//
// Parameters:
//   - t: test to report to
func TestUnpack(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
		ok      bool
		body    string
	}{
		{
			"normal", tarGz(t, tarEntry{Name: "LICENSE", Body: "x"}, tarEntry{Name: "tote", Body: "BIN"}),
			true, "BIN",
		},
		{"dot slash", tarGz(t, tarEntry{Name: "./tote", Body: "BIN"}), true, "BIN"},
		{
			"first of several files",
			tarGz(t, tarEntry{Name: "README.md", Body: "x"}, tarEntry{Name: "tote", Body: "BIN"}), true,
			"BIN",
		},
		{"no binary", tarGz(t, tarEntry{Name: "LICENSE", Body: "x"}), false, ""},
		{"empty archive", tarGz(t), false, ""},
		{"binary in a directory", tarGz(t, tarEntry{Name: "dist/tote", Body: "BIN"}), false, ""},
		{"traversal name", tarGz(t, tarEntry{Name: "../tote", Body: "BIN"}), false, ""},
		{"absolute name", tarGz(t, tarEntry{Name: "/tmp/tote", Body: "BIN"}), false, ""},
		{
			"symbolic link named tote",
			tarGz(t, tarEntry{Name: "tote", Typeflag: tar.TypeSymlink, Link: "/etc/passwd"}), false, "",
		},
		{
			"hard link named tote",
			tarGz(t, tarEntry{Name: "tote", Typeflag: tar.TypeLink, Link: "/etc/passwd"}), false, "",
		},
		{"directory named tote", tarGz(t, tarEntry{Name: "tote/", Typeflag: tar.TypeDir}), false, ""},
		{
			"two binaries", tarGz(t, tarEntry{Name: "tote", Body: "A"}, tarEntry{Name: "./tote", Body: "B"}),
			false, "",
		},
		{"empty binary", tarGz(t, tarEntry{Name: "tote", Body: ""}), false, ""},
		{"not gzip", []byte("plain text"), false, ""},
		{"empty file", nil, false, ""},
		{
			"truncated",
			tarGz(t, tarEntry{Name: "tote", Body: string(bytes.Repeat([]byte("z"), 5000))})[:60], false, "",
		},
		{"gzip of garbage", gzipOf(t, []byte("not a tar archive at all, just text")), false, ""},
	}

	for _, tt := range tests {
		dir := t.TempDir()
		src := filepath.Join(dir, "a.tar.gz")
		dst := filepath.Join(dir, "tote.new")

		if err := os.WriteFile(src, tt.archive, 0o600); err != nil {
			t.Fatal(err)
		}

		err := unpack(src, dst)
		if (err == nil) != tt.ok {
			t.Errorf("%s: error = %v, want ok = %v", tt.name, err, tt.ok)
			continue
		}

		if tt.ok {
			info, serr := os.Stat(dst)
			if serr != nil || info.Mode().Perm() != 0o755 || content(t, dst) != tt.body {
				t.Errorf("%s: output %v %v", tt.name, info, serr)
			}

			continue
		}

		if !errors.Is(err, ErrBadArchive) {
			t.Errorf("%s: error %v does not wrap ErrBadArchive", tt.name, err)
		}

		if _, serr := os.Lstat(dst); serr == nil {
			t.Errorf("%s: a file was left behind", tt.name)
		}
	}
}

// gzipOf compresses bytes.
//
// Parameters:
//   - t: running test
//   - data: bytes to compress
func gzipOf(t *testing.T, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// TestUnpackRefusesExisting checks that an existing destination is never overwritten.
//
// Parameters:
//   - t: test to report to
func TestUnpackRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.tar.gz")
	dst := filepath.Join(dir, "tote.new")

	if err := os.WriteFile(src, tarGz(t, tarEntry{Name: "tote", Body: "NEW"}), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(dst, []byte("OLD"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := unpack(src, dst); err == nil {
		t.Error("unpack overwrote an existing file")
	}

	if content(t, dst) != "OLD" {
		t.Error("the existing file changed")
	}
}
