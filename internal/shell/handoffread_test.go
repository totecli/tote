package shell

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestReviewHandoffSpecialFilesAreHandoffErrors checks that a FIFO with a reader attached and a
// directory are refused by the file type check itself and not by a later I/O failure.
//
// Parameters:
//   - t: test to report to
func TestReviewHandoffSpecialFilesAreHandoffErrors(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")

	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}

	reader, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = reader.Close() }()

	if err := WriteHandoff(fifo, Handoff{Action: Insert, Payload: "x"}); !errors.Is(err, ErrHandoff) {
		t.Errorf("write to a FIFO with a reader: %v, want ErrHandoff", err)
	}

	buf := make([]byte, 16)
	if n, _ := reader.Read(buf); n != 0 {
		t.Errorf("%d bytes were written to the FIFO: %q", n, buf[:n])
	}

	if _, err := ReadHandoff(fifo); !errors.Is(err, ErrHandoff) {
		t.Errorf("read of a FIFO: %v, want ErrHandoff", err)
	}

	if _, err := ReadHandoff(dir); !errors.Is(err, ErrHandoff) {
		t.Errorf("read of a directory: %v, want ErrHandoff", err)
	}

	if err := WriteHandoff(dir, Handoff{Action: Exit}); err == nil {
		t.Error("write to a directory accepted")
	}
}

// TestReviewHandoffReadLeavesFileAlone checks that reading never modifies the file it reads.
//
// Parameters:
//   - t: test to report to
func TestReviewHandoffReadLeavesFileAlone(t *testing.T) {
	path := newHandoffFile(t, 0o600)
	content := "v1 insert,reload\nls -la\n"

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if _, err := ReadHandoff(path); err != nil {
			t.Fatal(err)
		}
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, path); got != content || !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("file changed by reading: %q (%v -> %v)", got, before.ModTime(), after.ModTime())
	}
}

// TestReviewHandoffReadAcceptsLooseMode checks the asymmetry of the two sides: the reader does
// not insist on a private mode, the writer does.
//
// Parameters:
//   - t: test to report to
func TestReviewHandoffReadAcceptsLooseMode(t *testing.T) {
	path := newHandoffFile(t, 0o600)

	if err := os.WriteFile(path, []byte("v1 none\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadHandoff(path); err != nil {
		t.Errorf("read of a mode 0666 file: %v", err)
	}

	if err := WriteHandoff(path, Handoff{Action: Exit}); !errors.Is(err, ErrHandoff) {
		t.Errorf("write to a mode 0666 file: %v, want ErrHandoff", err)
	}
}
