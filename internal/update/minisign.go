package update

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

var (
	// ErrKeyFormat reports a public key that is not a minisign Ed25519 key.
	ErrKeyFormat = errors.New("invalid public key")

	// ErrSignatureFormat reports a signature file that is malformed.
	ErrSignatureFormat = errors.New("invalid signature file")

	// ErrLegacySignature reports a signature made without the BLAKE2b prehash, a format that
	// minisign has deprecated and that tote never accepts.
	ErrLegacySignature = errors.New("legacy signature format")

	// ErrUnknownKey reports a signature made by a key tote does not trust.
	ErrUnknownKey = errors.New("signature made by an unknown key")

	// ErrBadSignature reports a signature that does not verify.
	ErrBadSignature = errors.New("signature does not verify")
)

const (
	// keyBlobSize is the decoded size of a public key: algorithm, key id and Ed25519 key.
	keyBlobSize = 2 + keyIDSize + ed25519.PublicKeySize

	// keyIDSize is the size of the key identifier shared by a key and its signatures.
	keyIDSize = 8

	// sigBlobSize is the decoded size of a signature line: algorithm, key id and signature.
	sigBlobSize = 2 + keyIDSize + ed25519.SignatureSize

	// maxCommentSize is the longest trusted comment accepted, in bytes.
	maxCommentSize = 1024

	// algEd is the algorithm tag of a public key and of a legacy signature.
	algEd = "Ed"

	// algPrehashed is the algorithm tag of a signature over the BLAKE2b-512 hash of the file.
	algPrehashed = "ED"

	// untrustedPrefix starts the first line of a signature file.
	untrustedPrefix = "untrusted comment: "

	// trustedPrefix starts the third line of a signature file.
	trustedPrefix = "trusted comment: "
)

// PublicKey is a minisign public key.
//
// ID is the key identifier written in every signature made with the key, and Key is the raw
// Ed25519 public key.
type PublicKey struct {
	ID  [keyIDSize]byte
	Key ed25519.PublicKey
}

// ParsePublicKey decodes a minisign public key.
//
// Parameters:
//   - s: the base64 key line that starts with RW, or the content of a minisign .pub file, from
//     which the comment line is ignored
//
// Returns the key, or an error wrapping ErrKeyFormat.
func ParsePublicKey(s string) (PublicKey, error) {
	line := ""

	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "untrusted comment:") {
			continue
		}

		if line != "" {
			return PublicKey{}, fmt.Errorf("%w: more than one key line", ErrKeyFormat)
		}

		line = l
	}

	raw, err := base64.StdEncoding.Strict().DecodeString(line)
	if err != nil || len(raw) != keyBlobSize || string(raw[:2]) != algEd {
		return PublicKey{}, ErrKeyFormat
	}

	var k PublicKey

	copy(k.ID[:], raw[2:2+keyIDSize])
	k.Key = ed25519.PublicKey(bytes.Clone(raw[2+keyIDSize:]))

	return k, nil
}

// ParsePublicKeys decodes a list of minisign public keys.
//
// Parameters:
//   - list: base64 keys separated by white space; an entry that does not decode is an error
//
// Returns the keys in order, or an error wrapping ErrKeyFormat. An empty list gives no key and
// no error.
func ParsePublicKeys(list string) ([]PublicKey, error) {
	var keys []PublicKey

	for _, f := range strings.Fields(list) {
		k, err := ParsePublicKey(f)
		if err != nil {
			return nil, err
		}

		keys = append(keys, k)
	}

	return keys, nil
}

// Signature is a parsed minisign signature file.
//
// KeyID names the signing key, Sig is the Ed25519 signature of the BLAKE2b-512 hash of the
// file, Comment is the trusted comment and Global is the signature of Sig followed by Comment.
type Signature struct {
	KeyID   [keyIDSize]byte
	Sig     [ed25519.SignatureSize]byte
	Comment string
	Global  [ed25519.SignatureSize]byte
}

// ParseSignature decodes a minisign signature file in the default prehashed format.
//
// The file must hold exactly four lines: the untrusted comment, the signature, the trusted
// comment and the global signature, with nothing after them. The algorithm must be ED.
//
// Parameters:
//   - data: content of the .minisig file
//
// Returns the signature, an error wrapping ErrLegacySignature for the old Ed algorithm, or an
// error wrapping ErrSignatureFormat for anything else that is malformed.
func ParseSignature(data []byte) (Signature, error) {
	text := string(data)
	text = strings.TrimSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\r")

	lines := strings.Split(text, "\n")
	if len(lines) != 4 {
		return Signature{}, fmt.Errorf("%w: want 4 lines, got %d", ErrSignatureFormat, len(lines))
	}

	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if strings.ContainsAny(l, "\r\x00") {
			return Signature{}, fmt.Errorf("%w: control character in line %d",
				ErrSignatureFormat, i+1)
		}

		lines[i] = l
	}

	if !strings.HasPrefix(lines[0], untrustedPrefix) {
		return Signature{}, fmt.Errorf("%w: first line", ErrSignatureFormat)
	}

	blob, err := base64.StdEncoding.Strict().DecodeString(lines[1])
	if err != nil || len(blob) != sigBlobSize {
		return Signature{}, fmt.Errorf("%w: signature line", ErrSignatureFormat)
	}

	switch string(blob[:2]) {
	case algPrehashed:
	case algEd:
		return Signature{}, ErrLegacySignature
	default:
		return Signature{}, fmt.Errorf("%w: unknown algorithm", ErrSignatureFormat)
	}

	if !strings.HasPrefix(lines[2], trustedPrefix) {
		return Signature{}, fmt.Errorf("%w: third line", ErrSignatureFormat)
	}

	comment := strings.TrimPrefix(lines[2], trustedPrefix)
	if len(comment) > maxCommentSize {
		return Signature{}, fmt.Errorf("%w: trusted comment too long", ErrSignatureFormat)
	}

	global, err := base64.StdEncoding.Strict().DecodeString(lines[3])
	if err != nil || len(global) != ed25519.SignatureSize {
		return Signature{}, fmt.Errorf("%w: global signature line", ErrSignatureFormat)
	}

	var s Signature

	copy(s.KeyID[:], blob[2:2+keyIDSize])
	copy(s.Sig[:], blob[2+keyIDSize:])
	copy(s.Global[:], global)
	s.Comment = comment

	return s, nil
}

// Verify checks a minisign signature over a message against a set of trusted keys.
//
// The key is chosen by the identifier in the signature. The Ed25519 signature must verify over
// the BLAKE2b-512 hash of the message, and the global signature must verify over the signature
// followed by the trusted comment, so that the comment cannot be altered either.
//
// Parameters:
//   - keys: trusted public keys; at least one
//   - message: the signed bytes
//   - sigFile: content of the .minisig file
//
// Returns the trusted comment. The error wraps ErrSignatureFormat, ErrLegacySignature,
// ErrUnknownKey or ErrBadSignature.
func Verify(keys []PublicKey, message, sigFile []byte) (string, error) {
	sig, err := ParseSignature(sigFile)
	if err != nil {
		return "", err
	}

	var key *PublicKey

	for i := range keys {
		if keys[i].ID == sig.KeyID {
			key = &keys[i]
			break
		}
	}

	if key == nil {
		return "", ErrUnknownKey
	}

	hash := blake2b.Sum512(message)
	if !ed25519.Verify(key.Key, hash[:], sig.Sig[:]) {
		return "", fmt.Errorf("%w: file", ErrBadSignature)
	}

	signed := append(bytes.Clone(sig.Sig[:]), sig.Comment...)
	if !ed25519.Verify(key.Key, signed, sig.Global[:]) {
		return "", fmt.Errorf("%w: trusted comment", ErrBadSignature)
	}

	return sig.Comment, nil
}

// Comment holds the fields of the signed comment of a release.
//
// The comment is a list of tab separated name:value fields, written by the release tooling:
// project, version, tag, channel, timestamp and file. Timestamp is the Unix time of the build.
type Comment struct {
	Project   string
	Version   string
	Tag       string
	Channel   string
	Timestamp int64
	File      string
}

// ParseComment splits a trusted comment into its fields.
//
// Parameters:
//   - s: the comment returned by Verify; a field that appears twice, has no colon, or a
//     timestamp that is not a plain decimal number is an error; unknown fields are ignored
//
// Returns the fields, or an error wrapping ErrSignatureFormat.
func ParseComment(s string) (Comment, error) {
	seen := map[string]bool{}

	var c Comment

	for _, f := range strings.Split(s, "\t") {
		name, value, ok := strings.Cut(f, ":")
		if !ok || name == "" || seen[name] {
			return Comment{}, fmt.Errorf("%w: trusted comment field %q", ErrSignatureFormat, name)
		}

		seen[name] = true

		switch name {
		case "project":
			c.Project = value
		case "version":
			c.Version = value
		case "tag":
			c.Tag = value
		case "channel":
			c.Channel = value
		case "file":
			c.File = value
		case "timestamp":
			if !allDigits(value) || len(value) > 18 {
				return Comment{}, fmt.Errorf("%w: timestamp %q", ErrSignatureFormat, value)
			}

			for _, d := range value {
				c.Timestamp = c.Timestamp*10 + int64(d-'0')
			}
		}
	}

	return c, nil
}
