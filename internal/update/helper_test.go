package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// testStamp is the signed build time used by the fake releases.
const testStamp = 1762000000

// signer is a throwaway minisign key pair, with the signing side that tote itself lacks.
//
// pub and priv are the Ed25519 keys and id is the key identifier written in signatures.
type signer struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	id   [keyIDSize]byte
}

// newSigner builds a key pair from a seed byte, so that two seeds give two unrelated keys.
//
// Parameters:
//   - seed: byte repeated to form the Ed25519 seed
func newSigner(seed byte) signer {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	s := signer{priv: priv, pub: priv.Public().(ed25519.PublicKey)}

	copy(s.id[:], bytes.Repeat([]byte{seed}, keyIDSize))

	return s
}

// publicKey returns the key as tote parses it.
func (s signer) publicKey() PublicKey {
	return PublicKey{ID: s.id, Key: s.pub}
}

// encoded returns the base64 key line, as minisign prints it.
func (s signer) encoded() string {
	blob := append([]byte(algEd), s.id[:]...)

	return base64.StdEncoding.EncodeToString(append(blob, s.pub...))
}

// sign builds a signature file in the default prehashed format.
//
// Parameters:
//   - message: bytes to sign
//   - comment: trusted comment, signed together with the signature
func (s signer) sign(message []byte, comment string) []byte {
	hash := blake2b.Sum512(message)

	return s.file(algPrehashed, ed25519.Sign(s.priv, hash[:]), comment)
}

// signLegacy builds a signature file in the deprecated format, over the message itself.
//
// Parameters:
//   - message: bytes to sign
//   - comment: trusted comment
func (s signer) signLegacy(message []byte, comment string) []byte {
	return s.file(algEd, ed25519.Sign(s.priv, message), comment)
}

// file assembles the four lines of a signature file.
//
// Parameters:
//   - alg: algorithm tag, ED or Ed
//   - sig: signature of the message or of its hash
//   - comment: trusted comment
func (s signer) file(alg string, sig []byte, comment string) []byte {
	blob := append([]byte(alg), s.id[:]...)
	blob = append(blob, sig...)
	global := ed25519.Sign(s.priv, append(bytes.Clone(sig), comment...))

	return []byte("untrusted comment: test signature\n" +
		base64.StdEncoding.EncodeToString(blob) + "\n" +
		"trusted comment: " + comment + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n")
}

// comment builds the trusted comment goreleaser signs.
//
// Parameters:
//   - version: release version
//   - file: checksums file name
//   - channel: stable or beta
//   - stamp: build time
func comment(version, file, channel string, stamp int64) string {
	return fmt.Sprintf("project:tote\tversion:%s\ttag:%s\tchannel:%s\ttimestamp:%d\tfile:%s",
		version, version, channel, stamp, file)
}

// tarEntry is one entry of a test archive.
//
// Typeflag defaults to a regular file; Link is the target of a link entry.
type tarEntry struct {
	Name     string
	Body     string
	Typeflag byte
	Link     string
}

// tarGz builds a gzip compressed tar archive.
//
// Parameters:
//   - t: running test
//   - entries: entries in order
func tarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		flag := e.Typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}

		hdr := &tar.Header{Name: e.Name, Typeflag: flag, Mode: 0o755, Linkname: e.Link}
		if flag == tar.TypeReg {
			hdr.Size = int64(len(e.Body))
		}

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if flag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.Body)); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// fakeBinary returns the content of the stand-in for a tote binary of a version.
//
// Parameters:
//   - version: version it announces
func fakeBinary(version string) string {
	return "#!/bin/sh\nVERSION=" + version + "\n"
}

// fakeRun is the Run function of the tests: it reads the version out of a fake binary instead
// of starting a process, so that the tests do not depend on the system.
//
// Parameters:
//   - ctx: unused
//   - path: fake binary
func fakeRun(_ context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	_, v, ok := strings.Cut(string(data), "VERSION=")
	if !ok {
		return nil, fmt.Errorf("not a fake binary")
	}

	return []byte("tote " + strings.TrimSpace(v) + "\n"), nil
}

// fakeRelease describes a release published on the test mirror.
//
// Zero fields give a normal stable release signed at testStamp.
type fakeRelease struct {
	Tag        string
	Prerelease bool
	Draft      bool
	Stamp      int64
	NoAssets   bool
}

// mirror is a file:// release mirror in a temporary directory.
//
// releases accumulates the entries of releases.json.
type mirror struct {
	t        *testing.T
	dir      string
	signer   signer
	releases []apiRelease
}

// newMirror creates an empty mirror signed by a key.
//
// Parameters:
//   - t: running test
//   - s: key that signs the releases
func newMirror(t *testing.T, s signer) *mirror {
	t.Helper()

	return &mirror{t: t, dir: t.TempDir(), signer: s}
}

// base returns the file:// URL of the mirror.
func (m *mirror) base() string {
	return "file://" + m.dir
}

// path returns where a release file lives.
//
// Parameters:
//   - tag: release tag
//   - name: file name
func (m *mirror) path(tag, name string) string {
	return filepath.Join(m.dir, "download", tag, name)
}

// publish writes the files of a release for linux/amd64 and lists it.
//
// Parameters:
//   - r: release to publish
func (m *mirror) publish(r fakeRelease) {
	m.t.Helper()

	stamp := r.Stamp
	if stamp == 0 {
		stamp = testStamp
	}

	api := apiRelease{TagName: r.Tag, Prerelease: r.Prerelease, Draft: r.Draft}
	channel := "stable"

	if strings.Contains(r.Tag, "-") {
		channel = "beta"
	}

	archive := ArchiveName(r.Tag, "linux", "amd64")
	sums := ChecksumsName(r.Tag)
	body := tarGz(m.t,
		tarEntry{Name: "LICENSE", Body: "MIT\n"},
		tarEntry{Name: "tote", Body: fakeBinary(r.Tag)},
		tarEntry{Name: "README.md", Body: "tote\n"},
	)

	sum := sha256.Sum256(body)
	list := hex.EncodeToString(sum[:]) + "  " + archive + "\n"
	sig := m.signer.sign([]byte(list), comment(r.Tag, sums, channel, stamp))

	if !r.NoAssets {
		m.write(r.Tag, archive, body)
		m.write(r.Tag, sums, []byte(list))
		m.write(r.Tag, sums+".minisig", sig)

		for _, n := range []string{archive, sums, sums + ".minisig"} {
			api.Assets = append(api.Assets, apiAsset{Name: n})
		}
	}

	m.releases = append(m.releases, api)
	m.list()
}

// write stores one release file.
//
// Parameters:
//   - tag: release tag
//   - name: file name
//   - data: content
func (m *mirror) write(tag, name string, data []byte) {
	m.t.Helper()

	p := m.path(tag, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		m.t.Fatal(err)
	}

	if err := os.WriteFile(p, data, 0o644); err != nil {
		m.t.Fatal(err)
	}
}

// list writes releases.json.
func (m *mirror) list() {
	m.t.Helper()

	data, err := json.Marshal(m.releases)
	if err != nil {
		m.t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(m.dir, "releases.json"), data, 0o644); err != nil {
		m.t.Fatal(err)
	}
}

// updater builds an Updater that installs from the mirror over a copy of an old binary.
//
// Parameters:
//   - version: running version
//
// Returns the updater and the path of the target binary, which holds the fake binary of the
// running version.
func (m *mirror) updater(version string) (*Updater, string) {
	m.t.Helper()

	dir := m.t.TempDir()
	target := filepath.Join(dir, "tote")

	if err := os.WriteFile(target, []byte(fakeBinary(version)), 0o755); err != nil {
		m.t.Fatal(err)
	}

	return &Updater{
		Version: version, Source: Source{Base: m.base()},
		Keys: []PublicKey{m.signer.publicKey()}, Target: target,
		OS: "linux", Arch: "amd64", Run: fakeRun,
	}, target
}

// content returns the text of a file, or fails the test.
//
// Parameters:
//   - t: running test
//   - path: file to read
func content(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// names lists the entry names of a directory.
//
// Parameters:
//   - t: running test
//   - dir: directory to list
func names(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}

	return out
}
