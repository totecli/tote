package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"

	"github.com/totecli/tote/internal/update"
)

// testStamp is the signed build time of the fake releases.
const testStamp = 1762000000

// testSigner is a throwaway minisign key pair with the signing side tote does not have.
type testSigner struct {
	priv ed25519.PrivateKey
	id   []byte
}

// newTestSigner builds a key pair from a seed byte.
//
// Parameters:
//   - seed: byte repeated to form the Ed25519 seed
func newTestSigner(seed byte) testSigner {
	return testSigner{
		priv: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize)),
		id:   bytes.Repeat([]byte{seed}, 8),
	}
}

// encoded returns the base64 public key line.
func (s testSigner) encoded() string {
	blob := append([]byte("Ed"), s.id...)

	return base64.StdEncoding.EncodeToString(append(blob, s.priv.Public().(ed25519.PublicKey)...))
}

// sign builds a prehashed minisign signature file.
//
// Parameters:
//   - message: bytes to sign
//   - trusted: trusted comment
func (s testSigner) sign(message []byte, trusted string) []byte {
	hash := blake2b.Sum512(message)
	sig := ed25519.Sign(s.priv, hash[:])
	blob := append(append([]byte("ED"), s.id...), sig...)
	global := ed25519.Sign(s.priv, append(bytes.Clone(sig), trusted...))

	return []byte("untrusted comment: test\n" + base64.StdEncoding.EncodeToString(blob) +
		"\ntrusted comment: " + trusted + "\n" + base64.StdEncoding.EncodeToString(global) + "\n")
}

// testMirror is a file:// release mirror for linux/amd64 in a temporary directory.
type testMirror struct {
	t      *testing.T
	dir    string
	signer testSigner
	tags   []string
}

// newTestMirror creates an empty mirror.
//
// Parameters:
//   - t: running test
//   - s: key that signs the releases
func newTestMirror(t *testing.T, s testSigner) *testMirror {
	t.Helper()

	return &testMirror{t: t, dir: t.TempDir(), signer: s}
}

// base returns the file:// URL of the mirror.
func (m *testMirror) base() string {
	return "file://" + m.dir
}

// put writes a file of the mirror.
//
// Parameters:
//   - parts: path below the mirror directory
//   - data: content
func (m *testMirror) put(data []byte, parts ...string) {
	m.t.Helper()

	p := filepath.Join(append([]string{m.dir}, parts...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		m.t.Fatal(err)
	}

	if err := os.WriteFile(p, data, 0o644); err != nil {
		m.t.Fatal(err)
	}
}

// publish adds a release with its three files.
//
// Parameters:
//   - tag: version, also the tag
func (m *testMirror) publish(tag string) {
	m.t.Helper()

	archive := update.ArchiveName(tag, "linux", "amd64")
	sums := update.ChecksumsName(tag)

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := fakeBinary(tag)

	hdr := &tar.Header{Name: "tote", Mode: 0o755, Size: int64(len(body))}
	if err := tw.WriteHeader(hdr); err != nil {
		m.t.Fatal(err)
	}

	if _, err := tw.Write([]byte(body)); err != nil {
		m.t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		m.t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		m.t.Fatal(err)
	}

	sum := sha256.Sum256(buf.Bytes())
	list := hex.EncodeToString(sum[:]) + "  " + archive + "\n"
	channel := "stable"

	if strings.Contains(tag, "-") {
		channel = "beta"
	}

	trusted := fmt.Sprintf("project:tote\tversion:%s\ttag:%s\tchannel:%s\ttimestamp:%d\tfile:%s",
		tag, tag, channel, testStamp, sums)

	m.put(buf.Bytes(), "download", tag, archive)
	m.put([]byte(list), "download", tag, sums)
	m.put(m.signer.sign([]byte(list), trusted), "download", tag, sums+".minisig")

	m.tags = append(m.tags, tag)
	m.writeList()
}

// writeList writes releases.json in the format of the GitHub API.
func (m *testMirror) writeList() {
	var items []string

	for _, tag := range m.tags {
		files := []string{
			update.ArchiveName(tag, "linux", "amd64"), update.ChecksumsName(tag),
			update.SignatureName(tag),
		}

		var assets []string
		for _, f := range files {
			assets = append(assets, fmt.Sprintf(`{"name":%q}`, f))
		}

		items = append(items, fmt.Sprintf(`{"tag_name":%q,"draft":false,"prerelease":%v,"assets":[%s]}`,
			tag, strings.Contains(tag, "-"), strings.Join(assets, ",")))
	}

	m.put([]byte("["+strings.Join(items, ",")+"]"), "releases.json")
}

// fakeBinary returns the content of the stand-in for a tote binary.
//
// Parameters:
//   - version: version it announces
func fakeBinary(version string) string {
	return "#!/bin/sh\nVERSION=" + version + "\n"
}

// fakeRun is the check of the downloaded binary used by the tests: it reads the announced
// version out of the fake binary instead of starting a process.
//
// Parameters:
//   - ctx: unused
//   - path: fake binary
func fakeRun(_ context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	_, v, _ := strings.Cut(string(data), "VERSION=")

	return []byte("tote " + strings.TrimSpace(v) + "\n"), nil
}

// installFake puts a fake binary of a version in a fresh directory and wires the harness to
// run as that binary, to the mirror and to the key.
//
// Parameters:
//   - h: harness to wire
//   - m: mirror to update from
//   - version: version the harness reports and the fake binary announces
//
// Returns the path of the fake binary.
func installFake(h *harness, m *testMirror, version string) string {
	h.t.Helper()

	target := filepath.Join(h.dir, "bin", "tote")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		h.t.Fatal(err)
	}

	if err := os.WriteFile(target, []byte(fakeBinary(version)), 0o755); err != nil {
		h.t.Fatal(err)
	}

	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		h.t.Fatal(err)
	}

	h.app.Version = version
	h.app.Executable = func() (string, error) { return real, nil }
	h.app.Geteuid = func() int { return 1000 }
	h.app.Context = func() (context.Context, context.CancelFunc) {
		return context.WithCancel(context.Background())
	}
	h.app.Updater = update.Updater{OS: "linux", Arch: "amd64", Run: fakeRun}
	h.vars["TOTE_UPDATE_BASE"] = m.base()
	h.vars["TOTE_UPDATE_PUBKEY"] = m.signer.encoded()

	return real
}
