package update

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNoKey reports a build that carries no usable release key, so no signature can be
	// checked and no update is trusted.
	ErrNoKey = errors.New("this build has no release key")

	// ErrOffline reports a release server that cannot be reached.
	ErrOffline = errors.New("cannot reach the release server")

	// ErrCertificate reports a server whose TLS certificate cannot be trusted: unknown
	// authority, expired, or issued for another name. It is not a connectivity problem.
	ErrCertificate = errors.New("untrusted certificate")

	// ErrRateLimited reports that the release server refuses more requests for now. The error
	// returned carries a *RateLimitError.
	ErrRateLimited = errors.New("rate limited by the release server")

	// ErrServer reports an unexpected answer of the release server.
	ErrServer = errors.New("unexpected answer from the release server")

	// ErrNoRelease reports that no release can be found for the channel.
	ErrNoRelease = errors.New("no release found")

	// ErrNoAsset reports a release that lacks a file for this system.
	ErrNoAsset = errors.New("release file not found")

	// ErrInsecure reports a source or a redirect that does not use https (or file for tests).
	ErrInsecure = errors.New("insecure source")

	// ErrUnsupported reports a system that has no release build.
	ErrUnsupported = errors.New("unsupported system")

	// ErrTooLarge reports a download that is larger than any release file should be.
	ErrTooLarge = errors.New("file too large")

	// ErrBadChecksum reports a file whose SHA-256 differs from the signed checksums file, or a
	// checksums file that does not list it exactly once.
	ErrBadChecksum = errors.New("checksum mismatch")

	// ErrBadComment reports a signed comment that does not describe the expected release.
	ErrBadComment = errors.New("signed comment does not match the release")

	// ErrRollback reports a release signed before the one that is installed.
	ErrRollback = errors.New("release older than the installed one")

	// ErrBadArchive reports a release archive that does not hold a regular file named tote.
	ErrBadArchive = errors.New("invalid release archive")

	// ErrBadBinary reports a downloaded binary that does not announce the expected version.
	ErrBadBinary = errors.New("downloaded binary failed its check")

	// ErrNotWritable reports a target directory the user cannot write in. The error returned
	// carries a *NotWritableError.
	ErrNotWritable = errors.New("directory not writable")

	// errMissing is the internal answer of a source for a file that does not exist.
	errMissing = errors.New("not found")
)

// RateLimitError is returned, wrapping ErrRateLimited, when the server asks to slow down.
//
// Reset is when requests are accepted again, or the zero time when the server did not say.
type RateLimitError struct {
	Reset time.Time
}

// Error implements error.
func (e *RateLimitError) Error() string {
	return ErrRateLimited.Error()
}

// Unwrap returns ErrRateLimited.
func (e *RateLimitError) Unwrap() error {
	return ErrRateLimited
}

// InsecureError is returned, wrapping ErrInsecure, for a source or a redirect that is not https.
//
// URL is the address concerned, without credentials, and Redirect is true when a server sent
// the client there.
type InsecureError struct {
	URL      string
	Redirect bool
}

// Error implements error.
func (e *InsecureError) Error() string {
	return ErrInsecure.Error() + ": " + e.URL
}

// Unwrap returns ErrInsecure.
func (e *InsecureError) Unwrap() error {
	return ErrInsecure
}

// RollbackError is returned, wrapping ErrRollback, for a release signed before the installed one.
//
// Signed and Installed are the Unix times at which the two releases were signed.
type RollbackError struct {
	Signed    int64
	Installed int64
}

// Error implements error.
func (e *RollbackError) Error() string {
	return ErrRollback.Error()
}

// Unwrap returns ErrRollback.
func (e *RollbackError) Unwrap() error {
	return ErrRollback
}

// CommentError is returned, wrapping ErrBadComment, for a signed comment that describes another
// release.
//
// Field is the first field that differs: project, version, file, channel or timestamp.
type CommentError struct {
	Field string
}

// Error implements error.
func (e *CommentError) Error() string {
	return ErrBadComment.Error() + ": " + e.Field
}

// Unwrap returns ErrBadComment.
func (e *CommentError) Unwrap() error {
	return ErrBadComment
}

// NotWritableError is returned, wrapping ErrNotWritable, when no file can be created in the
// directory of the binary.
//
// Dir is that directory and Err the system error.
type NotWritableError struct {
	Dir string
	Err error
}

// Error implements error.
func (e *NotWritableError) Error() string {
	return ErrNotWritable.Error() + ": " + e.Dir
}

// Unwrap returns ErrNotWritable and the system error.
func (e *NotWritableError) Unwrap() []error {
	return []error{ErrNotWritable, e.Err}
}

// Code returns the stable machine name of an error, for scripts that read the JSON output.
//
// Parameters:
//   - err: error returned by this package
//
// Returns names such as "offline", "rate_limited", "bad_signature", "unknown_key" (a signature
// made with a key this build does not embed) or "bad_checksum", and "failed" for any other error.
func Code(err error) string {
	table := []struct {
		target error
		code   string
	}{
		{context.Canceled, "interrupted"},
		{ErrNoKey, "no_key"},
		{ErrKeyFormat, "bad_key"},
		{ErrOffline, "offline"},
		{ErrCertificate, "certificate"},
		{ErrRateLimited, "rate_limited"},
		{ErrServer, "server"},
		{ErrNoRelease, "no_release"},
		{ErrNoAsset, "no_asset"},
		{ErrInsecure, "insecure_source"},
		{ErrUnsupported, "unsupported_system"},
		{ErrTooLarge, "too_large"},
		{ErrBadChecksum, "bad_checksum"},
		{ErrBadSignature, "bad_signature"},
		{ErrUnknownKey, "unknown_key"},
		{ErrSignatureFormat, "bad_signature"},
		{ErrLegacySignature, "bad_signature"},
		{ErrBadComment, "bad_comment"},
		{ErrRollback, "rollback"},
		{ErrBadArchive, "bad_archive"},
		{ErrBadBinary, "bad_binary"},
		{ErrNotWritable, "not_writable"},
	}

	for _, e := range table {
		if errors.Is(err, e.target) {
			return e.code
		}
	}

	return "failed"
}
