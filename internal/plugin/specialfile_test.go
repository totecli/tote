package plugin

import (
	"path/filepath"
	"syscall"
	"testing"
)

// TestReviewSourceSpecialFileReason pins the reason given for a special file in a source
// directory. The existing table checks the code and the path only, and a FIFO is also refused,
// with the reason "changed", by the later regular-file check of the copy; the early check that
// names the real cause had no test of its own.
//
// Parameters:
//   - t: test to report to
func TestReviewSourceSpecialFileReason(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")

	must(t, syscall.Mkfifo(filepath.Join(src, "pipe"), 0o600))

	e := planErr(t, l.reg(nil), InstallRequest{Source: src})
	if e.Code != CodeSourceUnsafe || len(e.Args) < 2 || e.Args[0] != "pipe" ||
		e.Args[1] != UnsafeSpecial {
		t.Errorf("got %s %v, want %s [pipe %s]", e.Code, e.Args, CodeSourceUnsafe, UnsafeSpecial)
	}
}
