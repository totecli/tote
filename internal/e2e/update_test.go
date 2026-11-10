package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// updateStamp is the signed build time of the fake release.
const updateStamp = 1762000000

// goBuild builds the real binary with a version and release keys injected, as a release build
// does.
//
// Parameters:
//   - t: running test
//   - out: file to create
//   - version: value of main.version
//   - keys: value of the embedded release keys, empty to keep the placeholder
func goBuild(t *testing.T, out, version, keys string) {
	t.Helper()

	flags := "-X main.version=" + version
	if keys != "" {
		flags += " -X github.com/totecli/tote/internal/update.releaseKeys=" + keys
	}

	cmd := exec.Command("go", "build", "-ldflags", flags, "-o", out,
		"github.com/totecli/tote/cmd/tote")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local")

	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, msg)
	}
}

// releaseMirror writes a file:// mirror holding one signed release of linux and darwin
// archives for amd64 and arm64, all carrying the same binary.
//
// Parameters:
//   - t: running test
//   - dir: mirror directory
//   - binary: file to ship as tote
//   - version: release version
//   - priv: signing key
//   - id: key identifier
func releaseMirror(t *testing.T, dir, binary, version string, priv ed25519.PrivateKey, id []byte) {
	t.Helper()

	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range []struct{ name, body string }{{"LICENSE", "MIT\n"}, {"tote", string(data)}} {
		hdr := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(buf.Bytes())
	sums := fmt.Sprintf("tote_%s_checksums.txt", version)
	rel := filepath.Join(dir, "download", version)

	var list string

	var assets []string

	for _, platform := range []string{"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"} {
		name := fmt.Sprintf("tote_%s_%s.tar.gz", version, platform)
		list += hex.EncodeToString(sum[:]) + "  " + name + "\n"
		assets = append(assets, name)

		writeFile(t, filepath.Join(rel, name), buf.Bytes())
	}

	trusted := fmt.Sprintf("project:tote\tversion:%s\ttag:%s\tchannel:stable\ttimestamp:%d\tfile:%s",
		version, version, updateStamp, sums)
	hash := blake2b.Sum512([]byte(list))
	sig := ed25519.Sign(priv, hash[:])
	global := ed25519.Sign(priv, append(bytes.Clone(sig), trusted...))
	blob := append(append([]byte("ED"), id...), sig...)

	writeFile(t, filepath.Join(rel, sums), []byte(list))
	writeFile(t, filepath.Join(rel, sums+".minisig"), []byte("untrusted comment: test\n"+
		base64.StdEncoding.EncodeToString(blob)+"\ntrusted comment: "+trusted+"\n"+
		base64.StdEncoding.EncodeToString(global)+"\n"))

	assets = append(assets, sums, sums+".minisig")

	var names []string
	for _, a := range assets {
		names = append(names, fmt.Sprintf(`{"name":%q}`, a))
	}

	index := fmt.Sprintf(`[{"tag_name":%q,"draft":false,"prerelease":false,"assets":[%s]}]`,
		version, strings.Join(names, ","))
	writeFile(t, filepath.Join(dir, "releases.json"), []byte(index))
}

// writeFile creates a file and its directories.
//
// Parameters:
//   - t: running test
//   - path: file to create
//   - data: content
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// installCopy puts a copy of a binary in a fresh install directory.
//
// Parameters:
//   - t: running test
//   - src: binary to copy
func installCopy(t *testing.T, src string) string {
	t.Helper()

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "bin", "tote")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}

	return dst
}

// runTote runs a binary in a sandbox and returns what it printed.
//
// Parameters:
//   - t: running test
//   - bin: binary to run
//   - root: sandbox root holding home, state and cache
//   - mirror: value of TOTE_UPDATE_BASE
//   - args: arguments
//
// Returns the standard output, the standard error and the exit code.
func runTote(t *testing.T, bin, root, mirror string, args ...string) (string, string, int) {
	t.Helper()

	cmd := exec.Command(bin, args...)
	cmd.Env = []string{
		"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(root, "home"), "TOTE_LANG=en",
		"TOTE_CONFIG_DIR=" + filepath.Join(root, "config"),
		"TOTE_DATA_DIR=" + filepath.Join(root, "data"),
		"TOTE_STATE_DIR=" + filepath.Join(root, "state"),
		"TOTE_CACHE_DIR=" + filepath.Join(root, "cache"),
		"XDG_RUNTIME_DIR=" + filepath.Join(root, "run"), "TOTE_UPDATE_BASE=" + mirror,
	}

	var stdout, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	code := 0

	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}

	return stdout.String(), stderr.String(), code
}

// TestUpdateEndToEnd builds two real binaries, publishes the newer one on a signed file://
// mirror and updates the older one with itself: the running program is replaced, the receipt is
// written, and every tampering leaves the old program in place.
//
// Parameters:
//   - t: test to report to
func TestUpdateEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries")
	}

	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	id := bytes.Repeat([]byte{7}, 8)
	blob := append(append([]byte("Ed"), id...), priv.Public().(ed25519.PublicKey)...)
	key := base64.StdEncoding.EncodeToString(blob)

	work := t.TempDir()
	oldBin := filepath.Join(work, "tote-old")
	newBin := filepath.Join(work, "tote-new")

	goBuild(t, oldBin, "1.0.0", key)
	goBuild(t, newBin, "1.1.0", "")

	setup := func(t *testing.T) (target, mirror, root string) {
		t.Helper()

		mirror = t.TempDir()
		releaseMirror(t, mirror, newBin, "1.1.0", priv, id)

		return installCopy(t, oldBin), "file://" + mirror, t.TempDir()
	}

	version := func(t *testing.T, bin string) string {
		t.Helper()

		out, err := exec.Command(bin, "version").Output()
		if err != nil {
			t.Fatal(err)
		}

		return strings.TrimSpace(string(out))
	}

	t.Run("check", func(t *testing.T) {
		target, mirror, root := setup(t)

		out, _, code := runTote(t, target, root, mirror, "update", "--check", "--json")

		var info struct {
			Status, Latest string
			Available      bool
		}

		if err := json.Unmarshal([]byte(out), &info); err != nil {
			t.Fatalf("stdout %q: %v", out, err)
		}

		if code != 10 || info.Status != "available" || info.Latest != "1.1.0" || !info.Available {
			t.Errorf("code %d, info %+v", code, info)
		}

		if got := version(t, target); got != "tote 1.0.0" {
			t.Errorf("binary announces %q after a check", got)
		}
	})

	t.Run("update replaces the running program", func(t *testing.T) {
		target, mirror, root := setup(t)

		out, errOut, code := runTote(t, target, root, mirror, "update")
		if code != 0 || !strings.Contains(out, "tote updated from 1.0.0 to 1.1.0") {
			t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
		}

		if got := version(t, target); got != "tote 1.1.0" {
			t.Errorf("binary announces %q", got)
		}

		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("mode %v, %v", info, err)
		}

		entries, _ := os.ReadDir(filepath.Dir(target))
		if len(entries) != 1 {
			t.Errorf("install directory holds %d entries", len(entries))
		}

		data, err := os.ReadFile(filepath.Join(root, "state", "install.json"))
		if err != nil {
			t.Fatal(err)
		}

		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}

		if raw["version"] != "1.1.0" || raw["path"] != target || raw["verified_by"] != "minisign" ||
			raw["release_timestamp"] != float64(updateStamp) || raw["schema"] != float64(1) {
			t.Errorf("receipt %v", raw)
		}

		// The new program has no embedded key: it now refuses to update, never trusting nothing.
		_, errOut, code = runTote(t, target, root, mirror, "update", "--force")
		if code != 1 || !strings.Contains(errOut, "no release key") {
			t.Errorf("second update: code %d, stderr %q", code, errOut)
		}
	})

	t.Run("tampered archive", func(t *testing.T) {
		target, mirror, root := setup(t)
		name := fmt.Sprintf("tote_1.1.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
		archive := filepath.Join(strings.TrimPrefix(mirror, "file://"), "download", "1.1.0", name)

		data, err := os.ReadFile(archive)
		if err != nil {
			t.Fatal(err)
		}

		data[len(data)/2] ^= 1
		writeFile(t, archive, data)

		_, errOut, code := runTote(t, target, root, mirror, "update")
		if code != 1 || !strings.Contains(errOut, "does not match its signed checksum") {
			t.Errorf("code %d, stderr %q", code, errOut)
		}

		if got := version(t, target); got != "tote 1.0.0" {
			t.Errorf("binary announces %q", got)
		}

		if _, err := os.Stat(filepath.Join(root, "state", "install.json")); err == nil {
			t.Error("a receipt was written for a failed update")
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		target, mirror, root := setup(t)

		other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, ed25519.SeedSize))
		dir := strings.TrimPrefix(mirror, "file://")
		releaseMirror(t, dir, newBin, "1.1.0", other, bytes.Repeat([]byte{8}, 8))

		_, errOut, code := runTote(t, target, root, mirror, "update")
		if code != 1 || !strings.Contains(errOut, "signed with a key this version of tote") {
			t.Errorf("code %d, stderr %q", code, errOut)
		}

		if got := version(t, target); got != "tote 1.0.0" {
			t.Errorf("binary announces %q", got)
		}
	})

	t.Run("apt receipt", func(t *testing.T) {
		target, _, root := setup(t)

		writeFile(t, filepath.Join(root, "state", "install.json"), []byte(fmt.Sprintf(
			`{"schema":1,"install_method":"apt","path":%q}`, target)))

		out, _, code := runTote(t, target, root, "file:///nonexistent", "update")
		if code != 0 || !strings.Contains(out, "sudo apt update && sudo apt upgrade tote") {
			t.Errorf("code %d, stdout %q", code, out)
		}

		out, errOut, code := runTote(t, target, root, "file:///nonexistent", "uninstall", "--yes")
		if code != 1 || !strings.Contains(errOut, "sudo apt remove tote") || out != "" {
			t.Errorf("uninstall: code %d, stdout %q, stderr %q", code, out, errOut)
		}

		if got := version(t, target); got != "tote 1.0.0" {
			t.Errorf("binary announces %q", got)
		}
	})

	t.Run("uninstall", func(t *testing.T) {
		target, mirror, root := setup(t)

		_, _, code := runTote(t, target, root, mirror, "uninstall", "--yes")
		if code != 0 {
			t.Fatalf("code %d", code)
		}

		if _, err := os.Stat(target); err == nil {
			t.Error("the program is still there")
		}
	})
}
