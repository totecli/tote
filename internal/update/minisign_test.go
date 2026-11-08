package update

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"
)

// realFixture reads a file produced by the real minisign 0.11 with a throwaway key.
//
// Parameters:
//   - t: running test
//   - name: file name in testdata/minisign
func realFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile("testdata/minisign/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

// TestVerifyRealMinisign checks that signatures made by the real minisign verify, and that the
// legacy format it can also produce is refused.
//
// Parameters:
//   - t: test to report to
func TestVerifyRealMinisign(t *testing.T) {
	key, err := ParsePublicKey(string(realFixture(t, "key.pub")))
	if err != nil {
		t.Fatal(err)
	}

	sums := realFixture(t, "sums.txt")

	got, err := Verify([]PublicKey{key}, sums, realFixture(t, "sums.minisig"))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	c, err := ParseComment(got)
	if err != nil {
		t.Fatal(err)
	}

	want := Comment{
		Project: "tote", Version: "1.0.0", Tag: "1.0.0", Channel: "stable",
		Timestamp: 1762000000, File: "tote_1.0.0_checksums.txt",
	}
	if c != want {
		t.Errorf("comment = %+v, want %+v", c, want)
	}

	keys := []PublicKey{key}
	legacy := realFixture(t, "legacy.minisig")

	if _, err := Verify(keys, sums, legacy); !errors.Is(err, ErrLegacySignature) {
		t.Errorf("legacy signature: error %v, want ErrLegacySignature", err)
	}

	altered := append(bytes.Clone(sums), 'x')
	good := realFixture(t, "sums.minisig")

	if _, err := Verify(keys, altered, good); !errors.Is(err, ErrBadSignature) {
		t.Errorf("altered message: error %v, want ErrBadSignature", err)
	}
}

// TestParsePublicKey checks the accepted and refused key texts.
//
// Parameters:
//   - t: test to report to
func TestParsePublicKey(t *testing.T) {
	s := newSigner(1)
	good := s.encoded()

	blob, _ := base64.StdEncoding.DecodeString(good)
	legacy := append([]byte(nil), blob...)
	legacy[0], legacy[1] = 'E', 'D'

	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"bare key", good, true},
		{"key file", "untrusted comment: minisign public key ABCD\n" + good + "\n", true},
		{"padding around", "  " + good + "\n\n", true},
		{"empty", "", false},
		{"placeholder", "RW_CURRENT_RELEASE_KEY_PLACEHOLDER", false},
		{"truncated", good[:len(good)-4], false},
		{"extra bytes", good + "AAAA", false},
		{"not base64", strings.Repeat("!", 56), false},
		{"wrong algorithm", base64.StdEncoding.EncodeToString(legacy), false},
		{"two keys", good + "\n" + good, false},
	}

	for _, tt := range tests {
		k, err := ParsePublicKey(tt.in)
		if (err == nil) != tt.ok {
			t.Errorf("%s: error = %v, want ok = %v", tt.name, err, tt.ok)
			continue
		}

		if tt.ok && (k.ID != s.id || !bytes.Equal(k.Key, s.pub)) {
			t.Errorf("%s: wrong key decoded", tt.name)
		}

		if !tt.ok && !errors.Is(err, ErrKeyFormat) {
			t.Errorf("%s: error %v does not wrap ErrKeyFormat", tt.name, err)
		}
	}
}

// TestParsePublicKeys checks the list form used for the embedded keys and the override.
//
// Parameters:
//   - t: test to report to
func TestParsePublicKeys(t *testing.T) {
	a, b := newSigner(1), newSigner(2)

	keys, err := ParsePublicKeys(a.encoded() + "  \t" + b.encoded() + "\n")
	if err != nil || len(keys) != 2 || keys[1].ID != b.id {
		t.Errorf("ParsePublicKeys = %v, %v", keys, err)
	}

	if keys, err := ParsePublicKeys(" "); err != nil || len(keys) != 0 {
		t.Errorf("empty list = %v, %v", keys, err)
	}

	if _, err := ParsePublicKeys(a.encoded() + " nonsense"); !errors.Is(err, ErrKeyFormat) {
		t.Errorf("bad entry: error %v", err)
	}
}

// TestVerifyHostile runs a corpus of damaged and forged signature files against a good one.
//
// Parameters:
//   - t: test to report to
func TestVerifyHostile(t *testing.T) {
	s, other := newSigner(1), newSigner(2)
	keys := []PublicKey{s.publicKey()}
	msg := []byte("abc  tote_1.0.0_linux_amd64.tar.gz\n")
	trusted := comment("1.0.0", "tote_1.0.0_checksums.txt", "stable", testStamp)
	good := s.sign(msg, trusted)

	if got, err := Verify(keys, msg, good); err != nil || got != trusted {
		t.Fatalf("good signature: %q, %v", got, err)
	}

	lines := strings.Split(strings.TrimSuffix(string(good), "\n"), "\n")
	join := func(l ...string) []byte { return []byte(strings.Join(l, "\n") + "\n") }
	flip := func(line string, at int) string {
		raw, _ := base64.StdEncoding.DecodeString(line)
		raw[at] ^= 0x01

		return base64.StdEncoding.EncodeToString(raw)
	}

	tests := []struct {
		name string
		file []byte
		msg  []byte
		want error
	}{
		{"empty", nil, msg, ErrSignatureFormat},
		{"only a newline", []byte("\n"), msg, ErrSignatureFormat},
		{"truncated after line 2", join(lines[0], lines[1]), msg, ErrSignatureFormat},
		{"truncated after line 3", join(lines[0], lines[1], lines[2]), msg, ErrSignatureFormat},
		{"truncated in the middle of a line", good[:len(good)-30], msg, ErrSignatureFormat},
		{"trailing line", append(bytes.Clone(good), "extra\n"...), msg, ErrSignatureFormat},
		{"trailing blank line", append(bytes.Clone(good), '\n'), msg, ErrSignatureFormat},
		{"trailing NUL", append(bytes.Clone(good), 0), msg, ErrSignatureFormat},
		{
			"garbage after the signature", join(lines[0], lines[1]+"AAAA", lines[2], lines[3]), msg,
			ErrSignatureFormat,
		},
		{
			"garbage after the global signature", join(lines[0], lines[1], lines[2], lines[3]+"!"), msg,
			ErrSignatureFormat,
		},
		{"wrong first line", join("comment", lines[1], lines[2], lines[3]), msg, ErrSignatureFormat},
		{"wrong third line", join(lines[0], lines[1], "comment: x", lines[3]), msg, ErrSignatureFormat},
		{"signature not base64", join(lines[0], "!!!!", lines[2], lines[3]), msg, ErrSignatureFormat},
		{
			"signature too short", join(lines[0], lines[1][:40], lines[2], lines[3]), msg,
			ErrSignatureFormat,
		},
		{
			"carriage return inside", join(lines[0], lines[1][:20]+"\r"+lines[1][20:], lines[2], lines[3]),
			msg, ErrSignatureFormat,
		},
		{
			"unknown algorithm", join(lines[0], flipAlg(lines[1], "XX"), lines[2], lines[3]), msg,
			ErrSignatureFormat,
		},
		{"legacy algorithm", s.signLegacy(msg, trusted), msg, ErrLegacySignature},
		{"wrong key", other.sign(msg, trusted), msg, ErrUnknownKey},
		{
			"key id of another key, signature of ours",
			join(lines[0], flip(lines[1], 3), lines[2], lines[3]), msg, ErrUnknownKey,
		},
		{
			"flipped bit in the signature", join(lines[0], flip(lines[1], 20), lines[2], lines[3]), msg,
			ErrBadSignature,
		},
		{
			"flipped bit in the global signature", join(lines[0], lines[1], lines[2], flip(lines[3], 5)),
			msg, ErrBadSignature,
		},
		{"wrong trusted comment", join(lines[0], lines[1], lines[2]+"x", lines[3]), msg, ErrBadSignature},
		{
			"trusted comment of another release",
			join(lines[0], lines[1], "trusted comment: "+comment("2.0.0", "f", "stable", 1), lines[3]), msg,
			ErrBadSignature,
		},
		{"altered message", good, append(bytes.Clone(msg), '\n'), ErrBadSignature},
		{"empty message", good, nil, ErrBadSignature},
		{
			"signature of the legacy kind on the hash",
			s.file(algPrehashed, s.signLegacy(msg, "")[2+keyIDSize:2+keyIDSize+64], trusted), msg,
			ErrBadSignature,
		},
		{
			"sound signature, global signed by another key", mixGlobal(s, other, msg, trusted), msg,
			ErrBadSignature,
		},
	}

	for _, tt := range tests {
		_, err := Verify(keys, tt.msg, tt.file)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}
}

// flipAlg replaces the two algorithm bytes of a signature line.
//
// Parameters:
//   - line: base64 signature line
//   - alg: two characters to put in place
func flipAlg(line, alg string) string {
	raw, _ := base64.StdEncoding.DecodeString(line)
	copy(raw, alg)

	return base64.StdEncoding.EncodeToString(raw)
}

// mixGlobal builds a file whose signature is by one key and whose global signature is by
// another one that has the same identifier.
//
// Parameters:
//   - s: key that signs the message
//   - o: key that signs the comment
//   - msg: message
//   - trusted: trusted comment
func mixGlobal(s, o signer, msg []byte, trusted string) []byte {
	o.id = s.id
	good := s.sign(msg, trusted)
	forged := o.sign(msg, trusted)

	gl := strings.Split(string(good), "\n")
	fl := strings.Split(string(forged), "\n")
	gl[3] = fl[3]

	return []byte(strings.Join(gl, "\n"))
}

// TestVerifyCRLF checks that a signature file with Windows line endings still verifies.
//
// Parameters:
//   - t: test to report to
func TestVerifyCRLF(t *testing.T) {
	s := newSigner(1)
	msg := []byte("x\n")
	file := bytes.ReplaceAll(s.sign(msg, "a:b"), []byte("\n"), []byte("\r\n"))

	if _, err := Verify([]PublicKey{s.publicKey()}, msg, file); err != nil {
		t.Errorf("CRLF signature: %v", err)
	}
}

// TestVerifyKeySelection checks that the key is chosen by identifier among several.
//
// Parameters:
//   - t: test to report to
func TestVerifyKeySelection(t *testing.T) {
	a, b := newSigner(1), newSigner(2)
	msg := []byte("x\n")

	both := []PublicKey{a.publicKey(), b.publicKey()}
	if _, err := Verify(both, msg, b.sign(msg, "k:v")); err != nil {
		t.Errorf("second key: %v", err)
	}

	if _, err := Verify(nil, msg, a.sign(msg, "k:v")); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("no key: error %v", err)
	}
}

// TestParseComment checks the trusted comment parser.
//
// Parameters:
//   - t: test to report to
func TestParseComment(t *testing.T) {
	tests := []struct {
		in   string
		want Comment
		ok   bool
	}{
		{comment("1.0.0", "f", "stable", 5), Comment{"tote", "1.0.0", "1.0.0", "stable", 5, "f"}, true},
		{"project:tote\tfuture:1", Comment{Project: "tote"}, true},
		{"project:tote\tproject:evil", Comment{}, false},
		{"project", Comment{}, false},
		{"", Comment{}, false},
		{":x", Comment{}, false},
		{"timestamp:abc", Comment{}, false},
		{"timestamp:-1", Comment{}, false},
		{"timestamp:", Comment{}, false},
		{"timestamp:1234567890123456789", Comment{}, false},
		{"timestamp:12\t", Comment{}, false},
	}

	for _, tt := range tests {
		c, err := ParseComment(tt.in)
		if (err == nil) != tt.ok || (tt.ok && c != tt.want) {
			t.Errorf("ParseComment(%q) = %+v, %v", tt.in, c, err)
		}
	}
}
