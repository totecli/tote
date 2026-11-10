package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/totecli/tote/internal/update"
)

// TestReviewTestsUpdateBlankKeyOverride checks that a key override with nothing but white space
// is a bad key, reported as such, and not an empty key list that is mistaken for a build without
// a key.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsUpdateBlankKeyOverride(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.1.0")

	target := installFake(h, m, "1.0.0")
	h.vars["TOTE_UPDATE_PUBKEY"] = "  \t "

	if code := h.run("update", "--json"); code != 1 {
		t.Fatalf("code %d, stderr %q", code, h.errb.String())
	}

	if doc := failOut(t, h); doc.Error.Code != "bad_key" {
		t.Errorf("error = %q, want bad_key", doc.Error.Code)
	}

	if data, _ := os.ReadFile(target); string(data) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}
}

// TestReviewTestsUpdateNoticeUpToDate checks that the status line has nothing to announce when
// the cached release is the running one, or older: the notice is read, but not reported.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsUpdateNoticeUpToDate(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	m.publish("1.0.0")

	installFake(h, m, "1.0.0")

	a := *h.app

	n, err := a.refreshUpdateNotice(context.Background())
	if err != nil || n.Available || n.Latest != "1.0.0" {
		t.Fatalf("refresh = %+v, %v", n, err)
	}

	if n, ok := a.updateNotice(); ok {
		t.Errorf("updateNotice announced %+v for the running version", n)
	}
}

// TestReviewTestsExecutableResolvesLinks checks that the program to replace is the file behind
// a symbolic link, never the link: an update through a link would otherwise swap the link for a
// regular file and leave the real program in place.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsExecutableResolvesLinks(t *testing.T) {
	h := newHarness(t)
	m := newTestMirror(t, newTestSigner(1))
	real := installFake(h, m, "1.0.0")

	link := filepath.Join(t.TempDir(), "tote")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	h.app.Executable = func() (string, error) { return link, nil }

	got, err := h.app.executable()
	if err != nil || got != real {
		t.Errorf("executable() = %q, %v, want %q", got, err, real)
	}
}

// TestReviewTestsUpdateReceiptOwners checks the two cases around the sudo rule that the existing
// table lacks: a real root login (no SUDO_USER) owns its home and gets a receipt, and a normal
// user whose environment still carries SUDO_USER does too.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsUpdateReceiptOwners(t *testing.T) {
	tests := []struct {
		name string
		uid  int
		sudo string
	}{
		{"root without sudo", 0, ""},
		{"user with a stale SUDO_USER", 1000, "alice"},
	}

	for _, tt := range tests {
		h := newHarness(t)
		m := newTestMirror(t, newTestSigner(1))
		m.publish("1.1.0")

		installFake(h, m, "1.0.0")
		h.app.Geteuid = func() int { return tt.uid }

		if tt.uid == 0 {
			// Root does not accept the override of the release key.
			delete(h.vars, "TOTE_UPDATE_PUBKEY")
			h.app.Updater.Keys = []update.PublicKey{mustKey(t, m.signer)}
		}

		if tt.sudo != "" {
			h.vars["SUDO_USER"] = tt.sudo
		}

		if code := h.run("update"); code != 0 {
			t.Fatalf("%s: code %d, stderr %q", tt.name, code, h.errb.String())
		}

		r, err := update.ReadReceipt(filepath.Join(h.vars["TOTE_STATE_DIR"], "install.json"))
		if err != nil || r.Version != "1.1.0" {
			t.Errorf("%s: receipt %+v, %v, want one for 1.1.0", tt.name, r, err)
		}
	}
}
