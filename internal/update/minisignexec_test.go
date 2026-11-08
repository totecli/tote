package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReviewTestsRealMinisign signs a checksums file with the minisign program, when it is
// installed, and checks that Verify accepts the result and rejects every damaged copy. The
// tests above sign with code written beside the verifier, which cannot catch a shared
// misreading of the format; this one can.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsRealMinisign(t *testing.T) {
	bin := requireTool(t, "minisign")

	dir := t.TempDir()
	home := filepath.Join(dir, "home")

	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) {
		t.Helper()

		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}

		if out, err := cmd.CombinedOutput(); err != nil {
			toolFailed(t, "minisign %v: %v: %s", args[0], err, out)
		}
	}

	pub, sec, sums, sig := filepath.Join(dir, "k.pub"), filepath.Join(dir, "k.key"),
		filepath.Join(dir, "sums.txt"), filepath.Join(dir, "sums.txt.minisig")

	body := []byte(strings.Repeat("0", 64) + "  tote_1.1.0_linux_amd64.tar.gz\n")
	trusted := comment("1.1.0", "sums.txt", "stable", 1700000000)

	if err := os.WriteFile(sums, body, 0o600); err != nil {
		t.Fatal(err)
	}

	run("-G", "-W", "-f", "-p", pub, "-s", sec)
	run("-S", "-s", sec, "-m", sums, "-x", sig, "-t", trusted)

	pubText, err := os.ReadFile(pub)
	if err != nil {
		t.Fatal(err)
	}

	key, err := ParsePublicKey(string(pubText))
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}

	sigText, err := os.ReadFile(sig)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Verify([]PublicKey{key}, body, sigText)
	if err != nil || got != trusted {
		t.Fatalf("Verify = %q, %v, want %q", got, err, trusted)
	}

	t.Logf("verified trusted comment %q", got)

	damaged := append([]byte{}, body...)
	damaged[3] ^= 1

	if _, err := Verify([]PublicKey{key}, damaged, sigText); err == nil {
		t.Error("a damaged message was accepted")
	}

	changed := strings.Replace(string(sigText), "version:1.1.0", "version:1.1.1", 1)
	if _, err := Verify([]PublicKey{key}, body, []byte(changed)); err == nil {
		t.Error("an altered trusted comment was accepted")
	}
}
