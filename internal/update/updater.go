package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Status is the outcome of a check or of an update.
type Status string

const (
	// StatusUpToDate means the installed version is the latest one.
	StatusUpToDate Status = "up-to-date"

	// StatusAvailable means a newer release exists.
	StatusAvailable Status = "available"

	// StatusUpdated means a release was installed.
	StatusUpdated Status = "updated"

	// StatusDev means the running binary is a development build, which has no version to
	// compare.
	StatusDev Status = "dev"

	// StatusNewer means the installed version is newer than the latest release.
	StatusNewer Status = "newer"

	// StatusManaged means a package manager owns the binary, so nothing was checked or done.
	StatusManaged Status = "managed"
)

// Step is a stage of an installation, reported to the Progress function.
type Step string

const (
	// StepDownload is the download of the signed files and of the archive.
	StepDownload Step = "download"

	// StepVerify is the check of the signature, the checksum and the new binary.
	StepVerify Step = "verify"

	// StepInstall is the replacement of the running binary.
	StepInstall Step = "install"
)

const (
	// versionTimeout is how long the downloaded binary has to answer "version".
	versionTimeout = 20 * time.Second

	// maxVersionOutput is the most bytes read from the downloaded binary when it runs.
	maxVersionOutput = 4096
)

// Result is what Check and Apply report.
//
// Current is the running version as given, Latest and Tag the newest release for the channel.
// Timestamp is the signed build time of the installed release and Path the replaced binary,
// both set when Status is StatusUpdated. ReceiptErr is the failure to write the install
// receipt after a successful update, which does not undo the update.
type Result struct {
	Status     Status
	Current    string
	Latest     string
	Tag        string
	Channel    Channel
	Timestamp  int64
	Path       string
	ReceiptErr error
}

// Updater checks for and installs a release.
//
// Version is the running version, "dev" for a development build. Channel defaults to stable.
// Force allows a downgrade, a reinstall of the same version and the update of a development
// build. Source says where the releases are, Keys are the trusted minisign keys, and Target is
// the binary to replace, with symbolic links resolved. Receipt is the install receipt of that
// binary, nil when there is none, and ReceiptPath is where the new receipt is written; an
// empty ReceiptPath leaves the receipts alone. OS and Arch default to the running system.
// Progress, when set, is told about each step. Run starts the downloaded binary with the
// argument "version" and returns its output, and Rename moves the new binary over Target; both
// default to the real operations and exist for tests, as does Now.
type Updater struct {
	Version     string
	Channel     Channel
	Force       bool
	Source      Source
	Keys        []PublicKey
	Target      string
	Receipt     *Receipt
	ReceiptPath string
	OS          string
	Arch        string
	Progress    func(Step)
	Run         func(ctx context.Context, path string) ([]byte, error)
	Rename      func(oldpath, newpath string) error
	Now         func() time.Time
}

// channel returns the channel to use.
func (u *Updater) channel() Channel {
	if u.Channel == ChannelBeta {
		return ChannelBeta
	}

	return ChannelStable
}

// system returns the operating system and the architecture to fetch.
func (u *Updater) system() (string, string) {
	goos, goarch := u.OS, u.Arch
	if goos == "" {
		goos = runtime.GOOS
	}

	if goarch == "" {
		goarch = runtime.GOARCH
	}

	return goos, goarch
}

// step reports a step to the Progress function.
//
// Parameters:
//   - s: step that starts
func (u *Updater) step(s Step) {
	if u.Progress != nil {
		u.Progress(s)
	}
}

// Check looks up the latest release for the channel and compares it with the running version.
//
// Parameters:
//   - ctx: cancels the request
//
// Returns StatusAvailable, StatusUpToDate, StatusNewer or StatusDev with the latest version, or
// an error: ErrOffline, ErrRateLimited, ErrNoRelease, ErrNoAsset, ErrUnsupported and the like.
func (u *Updater) Check(ctx context.Context) (Result, error) {
	res, _, err := u.check(ctx)

	return res, err
}

// check is Check that also returns the release found.
//
// Parameters:
//   - ctx: cancels the request
func (u *Updater) check(ctx context.Context) (Result, Release, error) {
	res := Result{Current: u.Version, Channel: u.channel()}

	goos, goarch := u.system()
	if !supported(goos, goarch) {
		return res, Release{}, fmt.Errorf("%w: %s/%s", ErrUnsupported, goos, goarch)
	}

	rel, err := u.latest(ctx)
	if err != nil {
		return res, Release{}, err
	}

	res.Latest, res.Tag = rel.Version.String(), rel.Tag

	for _, name := range []string{
		ArchiveName(res.Latest, goos, goarch), ChecksumsName(res.Latest), SignatureName(res.Latest),
	} {
		if !rel.has(name) {
			return res, rel, fmt.Errorf("%w: %s", ErrNoAsset, name)
		}
	}

	cur, err := ParseVersion(u.Version)

	switch c := cur.Compare(rel.Version); {
	case err != nil:
		res.Status = StatusDev
	case c < 0:
		res.Status = StatusAvailable
	case c == 0:
		res.Status = StatusUpToDate
	default:
		res.Status = StatusNewer
	}

	return res, rel, nil
}

// Apply installs the latest release when it is newer than the running version.
//
// With Force it installs the latest release whatever the running version is. Nothing is
// written to Target until the signature, the checksum and the new binary have all been checked.
//
// Parameters:
//   - ctx: cancels the work; a cancellation leaves the old binary and removes the staged files
//
// Returns StatusUpdated after an install, otherwise the status of Check with no change made, or
// an error. The old binary is intact whenever an error is returned.
func (u *Updater) Apply(ctx context.Context) (Result, error) {
	if len(u.Keys) == 0 {
		return Result{Current: u.Version, Channel: u.channel()}, ErrNoKey
	}

	if err := u.Source.Validate(); err != nil {
		return Result{Current: u.Version, Channel: u.channel()}, err
	}

	res, rel, err := u.check(ctx)
	if err != nil {
		return res, err
	}

	if res.Status != StatusAvailable && !u.Force {
		return res, nil
	}

	return u.install(ctx, res, rel)
}

// defaultRun starts a downloaded binary with the argument "version".
//
// Parameters:
//   - ctx: cancels the process
//   - path: binary to run
//
// Returns the first bytes of its standard output. The environment is reduced to the language,
// so that nothing of the user's session reaches a binary that has not run before.
func defaultRun(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	var out cappedBuffer

	cmd := exec.CommandContext(ctx, path, "version")
	cmd.Env = []string{"TOTE_LANG=en", "LC_ALL=C"}
	cmd.Stdout = &out

	err := cmd.Run()

	return out.data, err
}

// cappedBuffer keeps the first maxVersionOutput bytes written to it and drops the rest.
type cappedBuffer struct {
	data []byte
}

// Write implements io.Writer.
//
// Parameters:
//   - p: bytes to keep, up to the cap
func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := maxVersionOutput - len(b.data); room > 0 {
		b.data = append(b.data, p[:min(room, len(p))]...)
	}

	return len(p), nil
}

// announces reports whether the output of "version" names the expected version.
//
// Parameters:
//   - out: output of the downloaded binary
//   - version: version text of the release
func announces(out []byte, version string) bool {
	fields := strings.Fields(string(out))

	return len(fields) >= 2 && fields[0] == "tote" && fields[1] == version
}

// checkTarget checks that the target is an ordinary file.
//
// Returns an error when Target is missing, a link or not a regular file.
func (u *Updater) checkTarget() error {
	info, err := os.Lstat(u.Target)
	if err != nil {
		return fmt.Errorf("binary %s: %w", u.Target, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("binary %s is not a regular file", u.Target)
	}

	return nil
}
