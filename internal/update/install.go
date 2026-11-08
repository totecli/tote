package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// stagePrefix starts the name of the private staging directory.
	stagePrefix = ".tote-update-"

	// stageArchive is the name of the downloaded archive inside the staging directory.
	stageArchive = "release.tar.gz"

	// stageBinary is the name of the unpacked binary inside the staging directory.
	stageBinary = "tote.new"

	// binaryMode is the mode of the installed binary when there is none to keep.
	binaryMode = 0o755

	// staleAge is how old a staging directory must be to count as left behind by a killed run.
	staleAge = time.Hour
)

// stage is the private directory where a release is downloaded and checked.
//
// It is created beside the target, so that the final rename never crosses a file system, with
// mode 0700. The files it holds are the only ones ever created, so cleaning is a removal of
// each of them and then of the empty directory, never a recursive delete.
type stage struct {
	dir string
}

// newStage creates a staging directory beside a target.
//
// Parameters:
//   - target: path of the binary that will be replaced
//
// Returns the stage, or an error wrapping ErrNotWritable when the directory of the target does
// not accept new files.
func newStage(target string) (*stage, error) {
	parent := filepath.Dir(target)

	dir, err := os.MkdirTemp(parent, stagePrefix+"*")
	if err != nil {
		return nil, &NotWritableError{Dir: parent, Err: err}
	}

	return &stage{dir: dir}, nil
}

// cleanStale removes the staging directories that a killed update left beside a target.
//
// Only directories named like a stage, not links, older than staleAge, are touched, and only
// the two files a stage ever holds are removed before the directory itself, so nothing else is
// ever deleted. Failures are ignored: the update does not depend on this.
//
// Parameters:
//   - target: path of the binary that will be replaced
//   - now: current time
func cleanStale(target string, now time.Time) {
	parent := filepath.Dir(target)

	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), stagePrefix) || !e.IsDir() {
			continue
		}

		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < staleAge {
			continue
		}

		old := &stage{dir: filepath.Join(parent, e.Name())}
		old.clean()
	}
}

// path returns the path of a file of the stage.
//
// Parameters:
//   - name: stageArchive or stageBinary
func (s *stage) path(name string) string {
	return filepath.Join(s.dir, name)
}

// clean removes the files of the stage and then the directory.
func (s *stage) clean() {
	_ = os.Remove(s.path(stageArchive))
	_ = os.Remove(s.path(stageBinary))
	_ = os.Remove(s.dir)
}

// install downloads, verifies and installs a release.
//
// Parameters:
//   - ctx: cancels the work
//   - res: result of the check, completed by the call
//   - rel: release to install
func (u *Updater) install(ctx context.Context, res Result, rel Release) (Result, error) {
	version := rel.Version.String()
	goos, goarch := u.system()
	archive := ArchiveName(version, goos, goarch)

	if err := u.checkTarget(); err != nil {
		return res, err
	}

	cleanStale(u.Target, time.Now())

	st, err := newStage(u.Target)
	if err != nil {
		return res, err
	}
	defer st.clean()

	u.step(StepDownload)

	sums, err := u.fetchSigned(ctx, rel)
	if err != nil {
		return res, err
	}

	res.Timestamp = sums.comment.Timestamp

	want, err := sums.checksum(archive)
	if err != nil {
		return res, err
	}

	if err := u.download(ctx, rel, archive, want, st); err != nil {
		return res, err
	}

	u.step(StepVerify)

	if err := unpack(st.path(stageArchive), st.path(stageBinary)); err != nil {
		return res, err
	}

	if err := u.sanity(ctx, st.path(stageBinary), version); err != nil {
		return res, err
	}

	u.step(StepInstall)

	if err := u.swap(ctx, st.path(stageBinary)); err != nil {
		return res, err
	}

	res.Status, res.Path = StatusUpdated, u.Target
	res.ReceiptErr = u.writeReceipt(res, rel)

	return res, nil
}

// signed is the content of a checksums file whose signature has been verified.
type signed struct {
	body    []byte
	comment Comment
}

// fetchSigned downloads the checksums file and its signature and verifies them.
//
// The signature is checked against the trusted keys, then the signed comment must describe this
// project, this version, this file and the channel of the version, and the signed time must not
// be older than the one in the install receipt unless Force is set.
//
// Parameters:
//   - ctx: cancels the downloads
//   - rel: release being installed
func (u *Updater) fetchSigned(ctx context.Context, rel Release) (signed, error) {
	version := rel.Version.String()

	body, err := u.asset(ctx, rel, ChecksumsName(version), maxSmallSize)
	if err != nil {
		return signed{}, err
	}

	sig, err := u.asset(ctx, rel, SignatureName(version), maxSmallSize)
	if err != nil {
		return signed{}, err
	}

	text, err := Verify(u.Keys, body, sig)
	if err != nil {
		return signed{}, err
	}

	c, err := ParseComment(text)
	if err != nil {
		return signed{}, err
	}

	if err := checkComment(c, rel.Version); err != nil {
		return signed{}, err
	}

	if r := u.Receipt; r != nil && r.ReleaseTimestamp > 0 && c.Timestamp < r.ReleaseTimestamp &&
		!u.Force {
		return signed{}, &RollbackError{Signed: c.Timestamp, Installed: r.ReleaseTimestamp}
	}

	return signed{body: body, comment: c}, nil
}

// checkComment compares a signed comment with the release that was asked for.
//
// Parameters:
//   - c: fields of the trusted comment
//   - v: version of the release
//
// Returns nil, or an error wrapping ErrBadComment that names the first field that differs.
func checkComment(c Comment, v Version) error {
	version := v.String()

	switch {
	case c.Project != "tote":
		return &CommentError{Field: "project"}
	case c.Version != version:
		return &CommentError{Field: "version"}
	case c.File != ChecksumsName(version):
		return &CommentError{Field: "file"}
	case c.Channel != string(ChannelOf(v)):
		return &CommentError{Field: "channel"}
	case c.Timestamp <= 0:
		return &CommentError{Field: "timestamp"}
	}

	return nil
}

// asset downloads one small file of a release into memory.
//
// Parameters:
//   - ctx: cancels the download
//   - rel: release holding the file
//   - name: file name
//   - limit: most bytes accepted
func (u *Updater) asset(
	ctx context.Context, rel Release, name string, limit int64,
) ([]byte, error) {
	data, err := u.Source.fetch(ctx, u.Source.assetLocation(rel.Tag, name), limit)
	if err != nil {
		if errors.Is(err, errMissing) {
			return nil, fmt.Errorf("%w: %s", ErrNoAsset, name)
		}

		return nil, err
	}

	return data, nil
}

// checksum finds the SHA-256 listed for a file.
//
// Parameters:
//   - name: file name; it must be listed exactly once
//
// Returns the 32 bytes of the digest, or an error wrapping ErrBadChecksum.
func (s signed) checksum(name string) ([]byte, error) {
	var found []byte

	for _, line := range strings.Split(string(s.body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}

		sum, err := hex.DecodeString(fields[0])
		if err != nil || len(sum) != sha256.Size {
			return nil, fmt.Errorf("%w: malformed line for %s", ErrBadChecksum, name)
		}

		if found != nil {
			return nil, fmt.Errorf("%w: %s is listed twice", ErrBadChecksum, name)
		}

		found = sum
	}

	if found == nil {
		return nil, fmt.Errorf("%w: %s is not listed", ErrBadChecksum, name)
	}

	return found, nil
}

// download fetches the archive into the stage and compares its SHA-256 with the signed one.
//
// Parameters:
//   - ctx: cancels the download
//   - rel: release holding the archive
//   - name: archive name
//   - want: digest listed in the signed checksums file
//   - st: stage that receives the file
func (u *Updater) download(
	ctx context.Context, rel Release, name string, want []byte, st *stage,
) error {
	f, err := os.OpenFile(st.path(stageArchive), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return &NotWritableError{Dir: st.dir, Err: err}
	}

	h := sha256.New()

	err = u.Source.fetchTo(ctx, u.Source.assetLocation(rel.Tag, name),
		io.MultiWriter(f, h), maxArchiveSize)
	if cerr := f.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		if errors.Is(err, errMissing) {
			return fmt.Errorf("%w: %s", ErrNoAsset, name)
		}

		return err
	}

	if !bytes.Equal(h.Sum(nil), want) {
		return fmt.Errorf("%w: %s", ErrBadChecksum, name)
	}

	return nil
}

// sanity runs the unpacked binary and checks that it announces the expected version.
//
// Parameters:
//   - ctx: cancels the process
//   - path: staged binary
//   - version: version text of the release
func (u *Updater) sanity(ctx context.Context, path, version string) error {
	run := u.Run
	if run == nil {
		run = defaultRun
	}

	out, err := run(ctx, path)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("%w: %v", ErrBadBinary, err)
	}

	if !announces(out, version) {
		return fmt.Errorf("%w: it announces %q, expected %s", ErrBadBinary,
			strings.TrimSpace(string(out)), version)
	}

	return nil
}

// swap moves the checked binary over the target.
//
// Parameters:
//   - ctx: a cancellation just before the rename stops the install
//   - staged: path of the checked binary, beside the target
//
// Returns nil after the rename, or an error with the target unchanged. A running tote keeps its
// old file, which the rename only unlinks.
func (u *Updater) swap(ctx context.Context, staged string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := keepModeAndOwner(staged, u.Target); err != nil {
		return fmt.Errorf("prepare %s: %w", u.Target, err)
	}

	rename := u.Rename
	if rename == nil {
		rename = os.Rename
	}

	if err := rename(staged, u.Target); err != nil {
		return fmt.Errorf("replace %s: %w", u.Target, err)
	}

	syncDir(filepath.Dir(u.Target))

	return nil
}

// keepModeAndOwner gives the new binary the permissions and, for root, the owner of the one it
// replaces, so that an update neither widens a private mode nor leaves a file owned by root in
// the directory of a user.
//
// Parameters:
//   - staged: checked new binary
//   - target: binary that will be replaced; nothing changes when it cannot be examined
//
// Returns the error of a change that failed.
func keepModeAndOwner(staged, target string) error {
	old, err := os.Lstat(target)
	if err != nil || !old.Mode().IsRegular() {
		return nil
	}

	if err := keepOwner(staged, old); err != nil {
		return err
	}

	// The owner must still be able to run the program.
	return os.Chmod(staged, old.Mode().Perm()|0o100)
}

// writeReceipt records the installed release in the install receipt.
//
// Parameters:
//   - res: result of the install, holding the signed time
//   - rel: installed release
//
// Returns nil, also when ReceiptPath is empty, else the error of the write.
func (u *Updater) writeReceipt(res Result, rel Release) error {
	if u.ReceiptPath == "" {
		return nil
	}

	goos, goarch := u.system()
	now := time.Now
	method := "manual"

	if u.Now != nil {
		now = u.Now
	}

	if u.Receipt != nil && u.Receipt.InstallMethod != "" {
		method = u.Receipt.InstallMethod
	}

	source := redact(u.Source.Base)
	if source == "" {
		source = downloadBase
	}

	return WriteReceipt(u.ReceiptPath, Receipt{
		InstallMethod:    method,
		Version:          rel.Version.String(),
		Channel:          string(ChannelOf(rel.Version)),
		Path:             u.Target,
		OS:               goos,
		Arch:             goarch,
		Source:           source,
		VerifiedBy:       "minisign",
		ReleaseTimestamp: res.Timestamp,
		InstalledAt:      formatTime(now()),
	})
}
