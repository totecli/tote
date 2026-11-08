package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestReviewTestsDefaultRunStarts checks that the real runner starts a program with the argument
// "version" and returns what it printed. TestDefaultRun skips on any error, so a runner that can
// no longer start anything (a zero timeout, a bad environment) would not be noticed there.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsDefaultRunStarts(t *testing.T) {
	echo, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("no echo program in PATH")
	}

	out, err := defaultRun(context.Background(), echo)
	if err != nil {
		t.Fatalf("defaultRun: %v", err)
	}

	if string(out) != "version\n" {
		t.Errorf("output = %q, want %q", out, "version\n")
	}
}

// TestReviewTestsDefaultRunCancelled checks that a cancelled context stops the real runner with
// an error instead of an answer.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsDefaultRunCancelled(t *testing.T) {
	echo, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("no echo program in PATH")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := defaultRun(ctx, echo); err == nil {
		t.Error("a cancelled context still ran the program")
	}
}

// TestReviewTestsApplyInterruptedDuringVersionCheck checks that a Ctrl-C while the downloaded
// binary is being run is a cancellation, which the command turns into exit 130, and not a bad
// binary, which is exit 1; the old binary and the directory stay as they were.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsApplyInterruptedDuringVersionCheck(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, target := m.updater("1.0.0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u.Run = func(context.Context, string) ([]byte, error) {
		cancel()

		return nil, errors.New("signal: killed")
	}

	if _, err := u.Apply(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}

	if content(t, target) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}

	if got := names(t, filepath.Dir(target)); !slices.Equal(got, []string{"tote"}) {
		t.Errorf("directory holds %v", got)
	}
}

// TestReviewTestsStagingIsPrivate checks, at the moment the new binary is moved into place, that
// the staging directory is private to the user, the downloaded archive is not readable by
// others and the binary is executable: what is verified is not exposed to another local user.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsStagingIsPrivate(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, target := m.updater("1.0.0")
	seen := false

	u.Rename = func(oldpath, newpath string) error {
		seen = true
		dir := filepath.Dir(oldpath)

		for path, want := range map[string]os.FileMode{
			dir:                              0o700,
			filepath.Join(dir, stageArchive): 0o600,
			oldpath:                          0o755,
		} {
			info, err := os.Stat(path)
			if err != nil {
				t.Errorf("stat %s: %v", path, err)
				continue
			}

			if got := info.Mode().Perm(); got != want {
				t.Errorf("%s has mode %o, want %o", filepath.Base(path), got, want)
			}
		}

		return os.Rename(oldpath, newpath)
	}

	if _, err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !seen {
		t.Fatal("the rename hook never ran")
	}

	if content(t, target) != fakeBinary("1.1.0") {
		t.Error("the binary was not replaced")
	}
}

// TestReviewTestsReceiptSourceDefault checks the source recorded in a receipt when the release
// came from GitHub, which no mirror based test can reach.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsReceiptSourceDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "install.json")
	u := &Updater{Target: "/x/tote", ReceiptPath: path, OS: "linux", Arch: "amd64"}

	rel := Release{Tag: "1.1.0", Version: Version{Major: 1, Minor: 1}}
	if err := u.writeReceipt(Result{Timestamp: 5}, rel); err != nil {
		t.Fatal(err)
	}

	r, err := ReadReceipt(path)
	if err != nil || r.Source != downloadBase || r.Version != "1.1.0" {
		t.Errorf("receipt = %+v, %v, want source %q", r, err, downloadBase)
	}
}

// TestReviewTestsReceiptTooLarge checks that a receipt above the size cap is refused even when
// the part inside the cap is valid JSON, so that padding cannot make the reader take more.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsReceiptTooLarge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.json")
	text := `{"schema":1,"install_method":"script"}` + strings.Repeat(" ", maxReceiptSize)

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadReceipt(path); !errors.Is(err, ErrReceipt) {
		t.Errorf("error = %v, want ErrReceipt", err)
	}
}
