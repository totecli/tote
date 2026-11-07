package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"syscall"
)

// Codes of the sources that are fetched or read as an archive. hash_required, hash_malformed and
// hash_unexpected are usage errors that come before any network access; hash_mismatch and
// platform_hash are integrity failures; tag_mismatch is a release whose manifest version is not
// the tag it was published under (the first argument is the tag, the second the version).
const (
	CodeHashRequired   = "hash_required"
	CodeHashMalformed  = "hash_malformed"
	CodeHashUnexpected = "hash_unexpected"
	CodeHashMismatch   = "hash_mismatch"
	CodePlatformHash   = "platform_hash"
	CodeTagMismatch    = "tag_mismatch"
)

// releaseHost is the only host a release URL may name.
const releaseHost = "github.com"

// releaseSegmentRe matches the owner, the repository, the tag and the asset of a release URL.
var releaseSegmentRe = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

// ReleaseRef is a release asset on GitHub: https://github.com/<Owner>/<Repo>/releases/download/
// <Tag>/<Asset>. The tag is the version of the plugin.
type ReleaseRef struct {
	Owner string
	Repo  string
	Tag   string
	Asset string
}

// Origin returns the lower case owner/repo, which identifies where a plugin comes from.
//
// Returns the origin, such as totecli/gft.
func (u ReleaseRef) Origin() string { return strings.ToLower(u.Owner + "/" + u.Repo) }

// URL returns the canonical release URL.
//
// Returns the https address of the asset in its release.
func (u ReleaseRef) URL() string {
	return "https://" + releaseHost + "/" + u.Owner + "/" + u.Repo + "/releases/download/" +
		u.Tag + "/" + u.Asset
}

// WithAsset returns the reference of another asset of the same release.
//
// Parameters:
//   - asset: name of the asset
func (u ReleaseRef) WithAsset(asset string) ReleaseRef {
	u.Asset = asset

	return u
}

// validSegment reports whether a text is usable as an owner, repository, tag or asset.
//
// Parameters:
//   - s: candidate
func validSegment(s string) bool {
	return releaseSegmentRe.MatchString(s) && s != "." && s != ".."
}

// ParseRelease reads a release URL with net/url, never by slicing the text.
//
// The scheme must be https and the host exactly github.com, in any case and without a trailing
// dot; port, user information, query and fragment must be empty. The text holds no percent sign
// and its path equals its own path.Clean form, with exactly the segments
// /<owner>/<repo>/releases/download/<tag>/<asset>, each of owner, repo, tag and asset inside
// [A-Za-z0-9._+-]+ and none of them "." or "..".
//
// Parameters:
//   - text: the URL as typed
//
// Returns the reference, or an *Error: bad_source.
func ParseRelease(text string) (ReleaseRef, error) {
	bad := newError(CodeBadSource, text)

	if strings.ContainsAny(text, "%?#\\") {
		return ReleaseRef{}, bad
	}

	u, err := url.Parse(text)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" ||
		strings.Contains(u.Host, ":") || strings.ToLower(u.Host) != releaseHost ||
		path.Clean(u.Path) != u.Path {
		return ReleaseRef{}, bad
	}

	parts := strings.Split(u.Path, "/")
	if len(parts) != 7 || parts[0] != "" || parts[3] != "releases" || parts[4] != "download" {
		return ReleaseRef{}, bad
	}

	ref := ReleaseRef{Owner: parts[1], Repo: parts[2], Tag: parts[5], Asset: parts[6]}

	for _, s := range []string{ref.Owner, ref.Repo, ref.Tag, ref.Asset} {
		if !validSegment(s) {
			return ReleaseRef{}, bad
		}
	}

	if !assetRe.MatchString(ref.Asset) {
		return ReleaseRef{}, bad
	}

	return ref, nil
}

// ParseShorthand reads owner/repo@tag, which stands for the asset <repo>.tar.gz of that release.
// The tag is mandatory.
//
// Parameters:
//   - text: the shorthand as typed
//
// Returns the reference, or an *Error: bad_source.
func ParseShorthand(text string) (ReleaseRef, error) {
	if !shorthandRe.MatchString(text) {
		return ReleaseRef{}, newError(CodeBadSource, text)
	}

	slug, tag, _ := strings.Cut(text, "@")
	owner, repo, _ := strings.Cut(slug, "/")
	ref := ReleaseRef{Owner: owner, Repo: repo, Tag: tag, Asset: repo + ".tar.gz"}

	for _, s := range []string{ref.Owner, ref.Repo, ref.Tag} {
		if !validSegment(s) {
			return ReleaseRef{}, newError(CodeBadSource, text)
		}
	}

	if !assetRe.MatchString(ref.Asset) {
		return ReleaseRef{}, newError(CodeBadSource, text)
	}

	return ref, nil
}

// ParseReleaseSource reads a release written as a URL or as the shorthand.
//
// Parameters:
//   - text: the source as typed
//
// Returns the reference, or an *Error: bad_source.
func ParseReleaseSource(text string) (ReleaseRef, error) {
	if strings.Contains(text, "://") {
		return ParseRelease(text)
	}

	return ParseShorthand(text)
}

// NormalizeSum checks a pinned sha256 and returns it in lower case.
//
// Parameters:
//   - s: the value of --sha256: 64 hexadecimal digits in either case
//
// Returns the digest, or an *Error: hash_malformed.
func NormalizeSum(s string) (string, error) {
	low := strings.ToLower(s)
	if !hexRe.MatchString(low) {
		return "", newError(CodeHashMalformed)
	}

	return low, nil
}

// Sidecar returns the text of the <asset>.sha256 file an author publishes next to an asset, in
// the format of sha256sum.
//
// Parameters:
//   - sum: sha256 of the asset
//   - asset: name of the asset
func Sidecar(sum, asset string) string { return sum + "  " + asset + "\n" }

// ParseSidecar reads a sidecar: the digest alone, or the digest and the name of the asset as
// sha256sum prints them. A name that is not the asset's is refused, so that the checksum of
// another file cannot be taken for this one.
//
// Parameters:
//   - data: contents of the file
//   - asset: name of the asset it belongs to
//
// Returns the digest in lower case, or an *Error: hash_malformed.
func ParseSidecar(data []byte, asset string) (string, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields) > 2 {
		return "", newError(CodeHashMalformed)
	}

	sum, err := NormalizeSum(fields[0])
	if err != nil {
		return "", err
	}

	if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") != asset {
		return "", newError(CodeHashMalformed)
	}

	return sum, nil
}

// Checksum downloads the <asset>.sha256 file that the author of a release published, and returns
// the digest it holds. Install and update never use it as a pin: it comes from the same place as
// the asset, so it proves integrity only (decision d). It is for the user to compare with a
// digest obtained elsewhere.
//
// Parameters:
//   - ctx: cancels the download
//   - source: release URL or owner/repo@tag
//
// Returns the digest, or an *Error: bad_source, hash_malformed, too_large and the net_* codes of
// a download.
func (r *Registry) Checksum(ctx context.Context, source string) (string, error) {
	ref, err := ParseReleaseSource(source)
	if err != nil {
		return "", err
	}

	side := ref.WithAsset(ref.Asset + ".sha256")
	if !assetRe.MatchString(side.Asset) {
		return "", newError(CodeBadSource, source)
	}

	var buf memSink

	if _, _, err := r.fetcher().download(ctx, side, maxSidecar, &buf); err != nil {
		return "", err
	}

	return ParseSidecar(buf.b, ref.Asset)
}

// maxSidecar is the largest sidecar file that is read.
const maxSidecar = 4096

// memSink collects a small download in memory.
type memSink struct {
	b []byte
}

// Write implements io.Writer.
//
// Parameters:
//   - p: bytes to keep
func (l *memSink) Write(p []byte) (int, error) {
	l.b = append(l.b, p...)

	return len(p), nil
}

// setPin checks the sha256 of the request against what the source needs. A release needs it, an
// archive on this machine takes it when given, and a directory or a bundled plugin refuse it: a
// checksum that is not checked must not look like one that was.
//
// Parameters:
//   - sum: value of the request, possibly empty
func (sp *sourceSpec) setPin(sum string) error {
	if sp.Kind != KindRelease && !sp.Archive {
		if sum != "" {
			return newError(CodeHashUnexpected)
		}

		return nil
	}

	if sum == "" {
		if sp.Kind == KindRelease {
			return newError(CodeHashRequired, sp.Text)
		}

		return nil
	}

	pin, err := NormalizeSum(sum)
	if err != nil {
		return err
	}

	sp.Pin = pin

	return nil
}

// downloadFile is a file in the staging directory that holds an archive while it is hashed and
// read. It is deleted when the install ends, whatever happens.
type downloadFile struct {
	path string
	f    *os.File
}

// newDownload creates the file next to a staging directory, with a name that the cleanup of
// staging recognises.
//
// Parameters:
//   - dir: the staging directory it belongs to
//   - suffix: end of the name
func newDownload(dir, suffix string) (*downloadFile, error) {
	p := dir + suffix

	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, wrapError(CodeDisk, err, p)
	}

	return &downloadFile{path: p, f: f}, nil
}

// drop closes and deletes the file.
func (d *downloadFile) drop() {
	_ = d.f.Close()
	_ = removeTree(d.path)
}

// archiveLimit is the largest archive that is fetched or read, 16 MiB.
const archiveLimit = MaxTreeBytes

// unpackSource gets an archive, compares its digest with the pin and extracts it into the
// staging directory. Nothing is parsed before the digest matches.
//
// Parameters:
//   - ctx: cancels the download and the extraction
//   - sp: the classified source, a release or an archive file
//   - dir: the staging directory
//   - dst: the staging directory, opened as a root
//   - want: id the manifest must carry, empty for any
//   - official: true when the plugin may use an official id
//
// Returns the digest of the archive, or an *Error.
func (r *Registry) unpackSource(ctx context.Context, sp sourceSpec, dir string, dst *os.Root,
	want string, official bool,
) (string, error) {
	dl, err := newDownload(dir, ".dl")
	if err != nil {
		return "", err
	}

	defer dl.drop()

	sum, err := r.getArchive(ctx, sp, dl.f)
	if err != nil {
		return "", err
	}

	if sp.Pin != "" && sum != sp.Pin {
		return "", newError(CodeHashMismatch, sp.Pin, sum)
	}

	u := newUnpacker(ctx, dst)

	// The file is read again by path, so the digest of the bytes that are extracted is compared
	// once more: a file swapped between the two reads never passes for the pinned archive.
	again, err := u.unpackFile(dl.path)
	if err != nil {
		return "", err
	}

	if again != sum {
		return "", newError(CodeHashMismatch, sum, again)
	}

	if sp.Kind == KindRelease {
		if err := r.platformAssets(ctx, sp, dir, dst, want, official, u); err != nil {
			return "", err
		}
	}

	return sum, nil
}

// getArchive writes the bytes of the source into a file while hashing them.
//
// Parameters:
//   - ctx: cancels the download
//   - sp: the classified source
//   - out: the file to write
func (r *Registry) getArchive(ctx context.Context, sp sourceSpec, out io.Writer) (string, error) {
	if sp.Kind == KindRelease {
		sum, _, err := r.fetcher().download(ctx, sp.Ref, archiveLimit, out)

		return sum, err
	}

	return copyLocal(sp.Path, out)
}

// copyLocal copies an archive of this machine, hashing it, so that the bytes that are extracted
// are the bytes that were hashed even if the file changes afterwards. The file is opened without
// blocking, so that a FIFO put in its place cannot stall the install.
//
// Parameters:
//   - src: path of the archive
//   - out: where the bytes go
func copyLocal(src string, out io.Writer) (string, error) {
	f, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", wrapError(CodeBadSource, err, src)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", newError(CodeBadSource, src)
	}

	h := sha256.New()

	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(f, archiveLimit+1))
	if err != nil {
		return "", wrapError(CodeDisk, err, src)
	}

	if n > archiveLimit {
		return "", newError(CodeTooLarge, sizeText(archiveLimit))
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// platformAssets downloads the program of an exec plugin for this machine, checks it against the
// sha256 of its [[platform]] table and extracts it into the same staging tree. A path that is in
// both archives is an error. A package that is not an exec package, or whose manifest is refused,
// or that has no build for this machine, has nothing to download: the validation that follows
// reports why.
//
// Parameters:
//   - ctx: cancels the download
//   - sp: the classified source, a release
//   - dir: the staging directory
//   - dst: the staging directory, opened as a root
//   - want: id the manifest must carry, empty for any
//   - official: true when the plugin may use an official id
//   - u: the extractor of the main archive, which keeps its names and counters
func (r *Registry) platformAssets(ctx context.Context, sp sourceSpec, dir string, dst *os.Root,
	want string, official bool, u *unpacker,
) error {
	data, err := readStaged(dst, manifestName)
	if err != nil {
		return nil
	}

	m, _ := Decode(data, DecodeOptions{
		Host: r.opts.Host, WantID: want, Official: official, Origin: sp.Origin,
	})
	if m == nil {
		return nil
	}

	host := r.opts.Host

	for _, b := range m.Builds {
		if b.OS != host.OS || b.Arch != host.Arch {
			continue
		}

		return r.platformAsset(ctx, sp, dir, b, u)
	}

	return nil
}

// platformAsset fetches and extracts one platform build.
//
// Parameters:
//   - ctx: cancels the download
//   - sp: the classified source, a release
//   - dir: the staging directory
//   - b: the [[platform]] table of this machine
//   - u: the extractor of the main archive
func (r *Registry) platformAsset(ctx context.Context, sp sourceSpec, dir string, b Build,
	u *unpacker,
) error {
	dl, err := newDownload(dir, ".pf")
	if err != nil {
		return err
	}

	defer dl.drop()

	sum, _, err := r.fetcher().download(ctx, sp.Ref.WithAsset(b.Asset), archiveLimit,
		dl.f)
	if err != nil {
		return err
	}

	if sum != b.SHA256 {
		return newError(CodePlatformHash, b.OS+"/"+b.Arch, b.Asset)
	}

	again, err := u.unpackFile(dl.path)
	if err != nil {
		return err
	}

	if again != sum {
		return newError(CodePlatformHash, b.OS+"/"+b.Arch, b.Asset)
	}

	return nil
}

// releaseSpec completes a specification for a release.
//
// Parameters:
//   - sp: the specification, whose Text is the source as typed
//   - ref: the parsed release
func releaseSpec(sp sourceSpec, ref ReleaseRef) sourceSpec {
	sp.Kind = KindRelease
	sp.Ref = ref
	sp.Origin = ref.Origin()

	return sp
}
