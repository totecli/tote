package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPreflightNeedsTwoKeys runs packaging/preflight.sh in release mode and checks that a build
// with fewer than two different well formed keys is refused for that reason, whatever else is
// missing, and that two different keys are not what it complains about.
//
// Parameters:
//   - t: test to report to
func TestPreflightNeedsTwoKeys(t *testing.T) {
	bash := requireTool(t, "bash")

	script, err := filepath.Abs(filepath.Join("..", "..", "packaging", "preflight.sh"))
	if err != nil {
		t.Fatal(err)
	}

	first := "RW" + strings.Repeat("A", 54)
	second := "RW" + strings.Repeat("B", 54)

	tests := []struct {
		name string
		keys string
		want string
		not  string
	}{
		{"none", "", "TOTE_RELEASE_KEYS is empty", "at least two"},
		{"one key", first, "at least two different keys", "is empty"},
		{"the same key twice", first + " " + first, "at least two different keys", "is empty"},
		{"a key and a placeholder", first + " RW_NEXT_RELEASE_KEY_PLACEHOLDER", "placeholder", ""},
		{"a key and garbage", first + " nonsense", "not a minisign public key", ""},
		{"two keys", first + " " + second, "MINISIGN_KEY_FILE is not set", "TOTE_RELEASE_KEYS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bash, script, "release")
			cmd.Env = []string{"PATH=/usr/bin:/bin", "TOTE_RELEASE_KEYS=" + tt.keys}

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("preflight accepted the keys %q", tt.keys)
			}

			text := string(out)
			if !strings.Contains(text, tt.want) {
				t.Errorf("output %q lacks %q", text, tt.want)
			}

			if tt.not != "" && strings.Contains(text, tt.not) {
				t.Errorf("output %q holds %q", text, tt.not)
			}
		})
	}
}

// TestPreflightNeedsMaintainer runs packaging/preflight.sh in release mode with two good keys and
// checks that the Maintainer field of the package must be written as Name <address>.
//
// Parameters:
//   - t: test to report to
func TestPreflightNeedsMaintainer(t *testing.T) {
	bash := requireTool(t, "bash")

	script, err := filepath.Abs(filepath.Join("..", "..", "packaging", "preflight.sh"))
	if err != nil {
		t.Fatal(err)
	}

	keys := "RW" + strings.Repeat("A", 54) + " RW" + strings.Repeat("B", 54)

	tests := []struct {
		name       string
		maintainer string
		complains  bool
	}{
		{"unset", "", true},
		{"a name only", "Jane Doe", true},
		{"an address only", "<jane@example.org>", true},
		{"no at sign", "Jane Doe <jane>", true},
		{"name and address", "Jane Doe <jane@example.org>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bash, script, "release")
			cmd.Env = []string{
				"PATH=/usr/bin:/bin", "TOTE_RELEASE_KEYS=" + keys,
				"TOTE_MAINTAINER=" + tt.maintainer,
			}

			out, _ := cmd.CombinedOutput()
			got := strings.Contains(string(out), "TOTE_MAINTAINER must be")
			if got != tt.complains {
				t.Errorf("maintainer %q: complaint is %v, want %v\n%s", tt.maintainer, got,
					tt.complains, out)
			}
		})
	}
}

// TestVerifyNeedsTwoKeys runs packaging/verify.sh on an empty directory and checks that a list
// with fewer than two different keys is refused before any file is read, and that two keys get
// past that check.
//
// Parameters:
//   - t: test to report to
func TestVerifyNeedsTwoKeys(t *testing.T) {
	bash := requireTool(t, "bash")

	requireTool(t, "minisign")

	script, err := filepath.Abs(filepath.Join("..", "..", "packaging", "verify.sh"))
	if err != nil {
		t.Fatal(err)
	}

	first := "RW" + strings.Repeat("A", 54)
	second := "RW" + strings.Repeat("B", 54)

	tests := []struct {
		name string
		keys string
		want string
	}{
		{"one key", first, "at least two different keys"},
		{"the same key twice", first + " " + first, "at least two different keys"},
		{"two keys", first + " " + second, "expected one checksums file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bash, script, t.TempDir())
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TOTE_RELEASE_KEYS=" + tt.keys}

			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), tt.want) {
				t.Errorf("err %v, output %q, want %q", err, out, tt.want)
			}
		})
	}
}

// copyPackaging copies one script of packaging/ into a throw-away tree, next to a README.md with
// the given text, so that a script that looks for ../README.md finds that one.
//
// Parameters:
//   - t: running test
//   - script: file name in packaging/, for example preflight.sh
//   - readme: content of the README.md of the throw-away tree
//
// Returns the path of the copy.
func copyPackaging(t *testing.T, script, readme string) string {
	t.Helper()

	src, err := os.ReadFile(filepath.Join("..", "..", "packaging", script))
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "packaging"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(readme), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(root, "packaging", script)
	if err := os.WriteFile(dst, src, 0o700); err != nil {
		t.Fatal(err)
	}

	return dst
}

// minisignKey makes a throw-away minisign key pair.
//
// Parameters:
//   - t: running test
//   - minisign: path of the minisign program
//   - password: password of the secret key, empty for a key without one
//
// Returns the public key (the second line of the .pub file) and the path of the secret key.
func minisignKey(t *testing.T, minisign, password string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	pub := filepath.Join(dir, "k.pub")
	sec := filepath.Join(dir, "k.key")

	args := []string{"-G", "-p", pub, "-s", sec}
	cmd := exec.Command(minisign, args...)
	if password == "" {
		cmd = exec.Command(minisign, append(args, "-W")...)
	} else {
		cmd.Stdin = strings.NewReader(password + "\n" + password + "\n")
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("minisign -G: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(pub)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected public key file %q", raw)
	}

	return lines[1], sec
}

// TestPreflightReadme runs packaging/preflight.sh in release mode and checks that a README.md
// that still holds a placeholder is refused, that a clean one is not, and that the opt-out of a
// throw-away test run works.
//
// Parameters:
//   - t: running test
func TestPreflightReadme(t *testing.T) {
	bash := requireTool(t, "bash")

	optOut := "TOTE_ALLOW_PLACEHOLDER_README=1"

	tests := []struct {
		name      string
		readme    string
		env       []string
		complains bool
	}{
		{"placeholder", "key RW_CURRENT_RELEASE_KEY_PLACEHOLDER", nil, true},
		{"written keys", "key " + "RW" + strings.Repeat("C", 54), nil, false},
		{"opt-out", "RW_NEXT_RELEASE_KEY_PLACEHOLDER", []string{optOut}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := copyPackaging(t, "preflight.sh", tt.readme)

			cmd := exec.Command(bash, script, "release")
			cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, tt.env...)

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("preflight accepted an empty environment\n%s", out)
			}

			got := strings.Contains(string(out), "README.md still holds a PLACEHOLDER")
			if got != tt.complains {
				t.Errorf("complaint is %v, want %v\n%s", got, tt.complains, out)
			}
		})
	}
}

// TestPreflightDrySign runs packaging/preflight.sh in release mode with real throw-away minisign
// keys: the password must open the secret key and the key must be one of the release keys.
//
// Parameters:
//   - t: running test
func TestPreflightDrySign(t *testing.T) {
	bash := requireTool(t, "bash")
	minisign := requireTool(t, "minisign")

	pub, sec := minisignKey(t, minisign, "right")
	other := "RW" + strings.Repeat("B", 54)
	unlisted := "RW" + strings.Repeat("A", 54)

	tests := []struct {
		name     string
		keys     string
		password string
		want     string
	}{
		{"listed key", pub + " " + other, "right", "release environment is complete"},
		{"listed second", other + " " + pub, "right", "release environment is complete"},
		{"unlisted key", unlisted + " " + other, "right", "is not the secret key of any key"},
		{"wrong password", pub + " " + other, "wrong", "cannot sign with MINISIGN_PASSWORD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := copyPackaging(t, "preflight.sh", "clean")

			cmd := exec.Command(bash, script, "release")
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"), "TMPDIR=" + t.TempDir(),
				"TOTE_RELEASE_KEYS=" + tt.keys, "MINISIGN_KEY_FILE=" + sec,
				"MINISIGN_PASSWORD=" + tt.password, "GITHUB_TOKEN=x",
				"TOTE_MAINTAINER=Jane Doe <jane@example.org>",
			}

			out, err := cmd.CombinedOutput()
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("output %q lacks %q", out, tt.want)
			}

			if (err == nil) != (tt.want == "release environment is complete") {
				t.Errorf("err %v for %q", err, tt.name)
			}
		})
	}
}

// buildTote builds a linux amd64 binary of tote with the version and the release keys of a
// release build.
//
// Parameters:
//   - t: running test
//   - version: value of main.version
//   - keys: value of internal/update.releaseKeys
//
// Returns the path of the binary.
func buildTote(t *testing.T, version, keys string) string {
	t.Helper()

	goBin := requireTool(t, "go")
	out := filepath.Join(t.TempDir(), "tote")
	flags := "-s -w -X main.version=" + version +
		" -X 'github.com/totecli/tote/internal/update.releaseKeys=" + keys + "'"

	cmd := exec.Command(goBin, "build", "-trimpath", "-ldflags", flags, "-o", out,
		"github.com/totecli/tote/cmd/tote")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")

	if res, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, res)
	}

	return out
}

// writeRelease lays out a dist directory like the one GoReleaser makes: the linux amd64 archive
// holds the given binary, the other files are dummies, and the checksums file is signed.
//
// Parameters:
//   - t: running test
//   - minisign: path of the minisign program
//   - secret: secret key (no password) that signs the checksums file
//   - version: version of the release
//   - binary: binary packed into the linux amd64 archive
//
// Returns the dist directory.
func writeRelease(t *testing.T, minisign, secret, version, binary string) string {
	t.Helper()

	dist := t.TempDir()
	var sums strings.Builder

	add := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dist, name), data, 0o600); err != nil {
			t.Fatal(err)
		}

		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), name)
	}

	for _, target := range []string{"darwin_amd64", "darwin_arm64", "linux_arm64"} {
		add("tote_"+version+"_"+target+".tar.gz", []byte(target))
	}

	add("tote_"+version+"_linux_amd64.deb", []byte("deb"))
	add("tote_"+version+"_linux_arm64.deb", []byte("deb"))

	raw, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}

	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	tw := tar.NewWriter(zw)

	head := &tar.Header{Name: "tote", Mode: 0o755, Size: int64(len(raw))}
	if err := tw.WriteHeader(head); err != nil {
		t.Fatal(err)
	}

	if _, err := tw.Write(raw); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	add("tote_"+version+"_linux_amd64.tar.gz", packed.Bytes())

	name := "tote_" + version + "_checksums.txt"
	sumsPath := filepath.Join(dist, name)
	if err := os.WriteFile(sumsPath, []byte(sums.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	comment := strings.Join([]string{
		"project:tote", "version:" + version, "tag:v" + version, "channel:stable",
		"timestamp:1700000000", "file:" + name,
	}, "\t")

	cmd := exec.Command(minisign, "-S", "-s", secret, "-m", sumsPath, "-x", sumsPath+".minisig",
		"-t", comment)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("minisign -S: %v\n%s", err, out)
	}

	return dist
}

// TestVerifyBinary runs packaging/verify.sh on a signed release whose linux amd64 archive holds a
// real build of tote, and checks that a binary that lacks a release key, or that reports another
// version, makes the verification fail.
//
// Parameters:
//   - t: running test
func TestVerifyBinary(t *testing.T) {
	bash := requireTool(t, "bash")
	minisign := requireTool(t, "minisign")

	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("the binary of the archive is run only on linux amd64")
	}

	script, err := filepath.Abs(filepath.Join("..", "..", "packaging", "verify.sh"))
	if err != nil {
		t.Fatal(err)
	}

	first, secret := minisignKey(t, minisign, "")
	second, _ := minisignKey(t, minisign, "")
	keys := first + " " + second
	version := "1.2.3"
	missing := "does not embed the release key"

	tests := []struct {
		name    string
		version string
		keys    string
		want    string
	}{
		{"good build", version, keys, "binary ok"},
		{"a key is missing", version, first + " RW" + strings.Repeat("D", 54), missing},
		{"no key at all", version, "", missing},
		{"wrong version", "9.9.9", keys, "tote version says 'tote 9.9.9', expected 'tote 1.2.3'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary := buildTote(t, tt.version, tt.keys)
			dist := writeRelease(t, minisign, secret, version, binary)

			cmd := exec.Command(bash, script, dist)
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"), "TMPDIR=" + t.TempDir(), "TOTE_RELEASE_KEYS=" + keys,
			}

			out, err := cmd.CombinedOutput()
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("output %q lacks %q", out, tt.want)
			}

			if (err == nil) != (tt.name == "good build") {
				t.Errorf("err %v for %q\n%s", err, tt.name, out)
			}
		})
	}
}
