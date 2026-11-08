package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestCheckMatrix checks every combination of running version, channel and published releases
// for the status that Check reports.
//
// Parameters:
//   - t: test to report to
func TestCheckMatrix(t *testing.T) {
	stable := func(tags ...string) []fakeRelease {
		var out []fakeRelease
		for _, tag := range tags {
			out = append(out, fakeRelease{Tag: tag, Prerelease: strings.Contains(tag, "-")})
		}

		return out
	}

	tests := []struct {
		name     string
		current  string
		channel  Channel
		releases []fakeRelease
		status   Status
		latest   string
	}{
		{"same version", "1.0.0", ChannelStable, stable("1.0.0"), StatusUpToDate, "1.0.0"},
		{"newer stable", "1.0.0", ChannelStable, stable("1.0.0", "1.1.0"), StatusAvailable, "1.1.0"},
		{
			"order of the list does not matter", "1.0.0", ChannelStable, stable("1.2.0", "1.10.0", "1.9.0"),
			StatusAvailable, "1.10.0",
		},
		{
			"beta never offered to stable", "1.0.0", ChannelStable, stable("1.0.0", "1.1.0-beta.1"),
			StatusUpToDate, "1.0.0",
		},
		{"newer beta only is no release", "1.0.0", ChannelStable, stable("1.1.0-beta.1"), "", ""},
		{
			"beta offered to beta", "1.0.0", ChannelBeta, stable("1.0.0", "1.1.0-beta.1"), StatusAvailable,
			"1.1.0-beta.1",
		},
		{
			"next beta", "1.1.0-beta.1", ChannelBeta, stable("1.0.0", "1.1.0-beta.2"), StatusAvailable,
			"1.1.0-beta.2",
		},
		{
			"beta.2 to beta.10", "1.1.0-beta.2", ChannelBeta, stable("1.1.0-beta.10"), StatusAvailable,
			"1.1.0-beta.10",
		},
		{
			"beta.10 is not below beta.2", "1.1.0-beta.10", ChannelBeta, stable("1.1.0-beta.2"), StatusNewer,
			"1.1.0-beta.2",
		},
		{
			"beta to its stable", "1.1.0-beta.3", ChannelBeta, stable("1.1.0-beta.3", "1.1.0"),
			StatusAvailable, "1.1.0",
		},
		{
			"beta install sees stable on stable", "1.1.0-beta.1", ChannelStable, stable("1.1.0"),
			StatusAvailable, "1.1.0",
		},
		{
			"beta is newer than older stable", "1.1.0-beta.1", ChannelStable, stable("1.0.0"), StatusNewer,
			"1.0.0",
		},
		{
			"stable above beta of the same version", "1.1.0", ChannelBeta, stable("1.1.0-beta.9"),
			StatusNewer, "1.1.0-beta.9",
		},
		{"local is newer", "1.2.0", ChannelStable, stable("1.1.0"), StatusNewer, "1.1.0"},
		{"dev build", "dev", ChannelStable, stable("1.0.0"), StatusDev, "1.0.0"},
		{"empty version is a dev build", "", ChannelStable, stable("1.0.0"), StatusDev, "1.0.0"},
		{"not a version is a dev build", "1.0", ChannelStable, stable("1.0.0"), StatusDev, "1.0.0"},
		{
			"draft ignored", "1.0.0", ChannelStable,
			append(stable("1.0.0"), fakeRelease{Tag: "2.0.0", Draft: true}), StatusUpToDate, "1.0.0",
		},
		{
			"flagged pre-release ignored on stable", "1.0.0", ChannelStable,
			[]fakeRelease{{Tag: "1.0.0"}, {Tag: "1.0.1", Prerelease: true}},
			StatusUpToDate, "1.0.0",
		},
		{
			"doubled v ignored", "1.0.0", ChannelStable,
			[]fakeRelease{{Tag: "1.0.0"}, {Tag: "vv2.0.0"}},
			StatusUpToDate, "1.0.0",
		},
		{"no release at all", "1.0.0", ChannelStable, nil, "", ""},
	}

	for _, tt := range tests {
		m := newMirror(t, newSigner(1))
		for _, r := range tt.releases {
			if r.Tag == "v2.0.0" {
				m.releases = append(m.releases, apiRelease{TagName: r.Tag})
				m.list()

				continue
			}

			m.publish(r)
		}

		if len(tt.releases) == 0 {
			m.list()
		}

		u, _ := m.updater(tt.current)
		u.Channel = tt.channel

		res, err := u.Check(context.Background())
		if tt.status == "" {
			if !errors.Is(err, ErrNoRelease) {
				t.Errorf("%s: error = %v, want ErrNoRelease", tt.name, err)
			}

			continue
		}

		if err != nil || res.Status != tt.status || res.Latest != tt.latest {
			t.Errorf("%s: %+v, %v; want %s %s", tt.name, res, err, tt.status, tt.latest)
		}
	}
}

// TestCheckErrors checks the failures of Check that do not depend on the releases.
//
// Parameters:
//   - t: test to report to
func TestCheckErrors(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.0.0"})

	u, _ := m.updater("0.9.0")
	u.OS = "windows"

	if _, err := u.Check(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("windows: error %v, want ErrUnsupported", err)
	}

	u.OS, u.Arch = "linux", "arm64"

	if _, err := u.Check(context.Background()); !errors.Is(err, ErrNoAsset) {
		t.Errorf("no arm64 file: error %v, want ErrNoAsset", err)
	}

	u.Arch = "amd64"
	u.Source = Source{Base: "http://example.org"}

	if _, err := u.Check(context.Background()); !errors.Is(err, ErrInsecure) {
		t.Errorf("http source: error %v, want ErrInsecure", err)
	}

	u.Source = Source{Base: "file://" + filepath.Join(m.dir, "missing")}

	if _, err := u.Check(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Errorf("missing list: error %v, want ErrNoRelease", err)
	}

	bad := newMirror(t, newSigner(1))
	list := filepath.Join(bad.dir, "releases.json")
	if err := os.WriteFile(list, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	u2, _ := bad.updater("1.0.0")
	if _, err := u2.Check(context.Background()); !errors.Is(err, ErrServer) {
		t.Errorf("bad list: error %v, want ErrServer", err)
	}

	empty := newMirror(t, newSigner(1))
	empty.publish(fakeRelease{Tag: "1.0.0", NoAssets: true})

	u3, _ := empty.updater("0.9.0")
	if _, err := u3.Check(context.Background()); !errors.Is(err, ErrNoAsset) {
		t.Errorf("release without files: error %v, want ErrNoAsset", err)
	}
}

// TestApplySuccess checks a full update from a mirror: the file is replaced, the mode is
// right, nothing else is left in the directory and the receipt is written.
//
// Parameters:
//   - t: test to report to
func TestApplySuccess(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.0.0"})
	m.publish(fakeRelease{Tag: "1.1.0", Stamp: testStamp + 100})

	u, target := m.updater("1.0.0")
	u.ReceiptPath = filepath.Join(t.TempDir(), "state", "install.json")
	u.Now = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }

	var steps []Step

	u.Progress = func(s Step) { steps = append(steps, s) }

	res, err := u.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if res.Status != StatusUpdated || res.Latest != "1.1.0" || res.Path != target ||
		res.Timestamp != testStamp+100 || res.ReceiptErr != nil {
		t.Errorf("result = %+v", res)
	}

	if got := content(t, target); got != fakeBinary("1.1.0") {
		t.Errorf("target = %q", got)
	}

	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("target mode = %v, %v", info, err)
	}

	if got := names(t, filepath.Dir(target)); !slices.Equal(got, []string{"tote"}) {
		t.Errorf("directory holds %v after the update", got)
	}

	if !slices.Equal(steps, []Step{StepDownload, StepVerify, StepInstall}) {
		t.Errorf("steps = %v", steps)
	}

	r, err := ReadReceipt(u.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}

	want := Receipt{
		Schema: 1, InstallMethod: "manual", Version: "1.1.0", Channel: "stable", Path: target,
		OS: "linux", Arch: "amd64", Source: m.base(), VerifiedBy: "minisign",
		ReleaseTimestamp: testStamp + 100, InstalledAt: "2026-11-03T12:00:00Z",
	}
	if r != want {
		t.Errorf("receipt = %+v, want %+v", r, want)
	}

	info, err = os.Stat(u.ReceiptPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("receipt mode = %v, %v", info, err)
	}
}

// TestApplyKeepsReceiptMethod checks that the install method of the previous receipt is kept.
//
// Parameters:
//   - t: test to report to
func TestApplyKeepsReceiptMethod(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, _ := m.updater("1.0.0")
	u.Receipt = &Receipt{Schema: 1, InstallMethod: "script", ReleaseTimestamp: 1}
	u.ReceiptPath = filepath.Join(t.TempDir(), "install.json")

	if _, err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	if r, err := ReadReceipt(u.ReceiptPath); err != nil || r.InstallMethod != "script" {
		t.Errorf("receipt = %+v, %v", r, err)
	}
}

// TestApplyBetaReceipt checks that installing a beta records the beta channel and keeps the
// install method of the previous receipt.
//
// Parameters:
//   - t: test to report to
func TestApplyBetaReceipt(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0-beta.1", Prerelease: true})

	u, _ := m.updater("1.0.0")
	u.Channel = ChannelBeta
	u.Receipt = &Receipt{Schema: 1, InstallMethod: "manual"}
	u.ReceiptPath = filepath.Join(t.TempDir(), "install.json")

	if _, err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	r, err := ReadReceipt(u.ReceiptPath)
	if err != nil || r.Channel != "beta" || r.Version != "1.1.0-beta.1" ||
		r.InstallMethod != "manual" {
		t.Errorf("receipt = %+v, %v", r, err)
	}
}

// TestApplyNoChange checks the cases where Apply leaves the binary alone, and how Force
// changes them.
//
// Parameters:
//   - t: test to report to
func TestApplyNoChange(t *testing.T) {
	tests := []struct {
		name    string
		current string
		force   bool
		status  Status
		after   string
	}{
		{"up to date", "1.1.0", false, StatusUpToDate, "1.1.0"},
		{"up to date, forced reinstall", "1.1.0", true, StatusUpdated, "1.1.0"},
		{"local newer", "1.2.0", false, StatusNewer, "1.2.0"},
		{"local newer, forced downgrade", "1.2.0", true, StatusUpdated, "1.1.0"},
		{"dev build", "dev", false, StatusDev, "dev"},
		{"dev build, forced", "dev", true, StatusUpdated, "1.1.0"},
		{"beta of a later version", "1.2.0-beta.1", false, StatusNewer, "1.2.0-beta.1"},
	}

	for _, tt := range tests {
		m := newMirror(t, newSigner(1))
		m.publish(fakeRelease{Tag: "1.1.0"})

		u, target := m.updater(tt.current)
		u.Force = tt.force

		res, err := u.Apply(context.Background())
		if err != nil || res.Status != tt.status {
			t.Errorf("%s: %+v, %v; want %s", tt.name, res, err, tt.status)
		}

		if got := content(t, target); got != fakeBinary(tt.after) {
			t.Errorf("%s: target = %q, want version %s", tt.name, got, tt.after)
		}

		if got := names(t, filepath.Dir(target)); !slices.Equal(got, []string{"tote"}) {
			t.Errorf("%s: directory holds %v", tt.name, got)
		}
	}
}

// TestApplyNoKey checks that an update without a trusted key is refused before any request.
//
// Parameters:
//   - t: test to report to
func TestApplyNoKey(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, target := m.updater("1.0.0")
	u.Keys = nil

	if _, err := u.Apply(context.Background()); !errors.Is(err, ErrNoKey) {
		t.Errorf("error = %v, want ErrNoKey", err)
	}

	if content(t, target) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}
}

// resign replaces the checksums file of a release and signs it again, so that a test can build
// a release whose signature is sound but whose content is not.
//
// Parameters:
//   - m: mirror holding the release
//   - tag: release tag
//   - sums: new checksums content
//   - trusted: trusted comment
func resign(m *mirror, tag, sums, trusted string) {
	m.write(tag, ChecksumsName(tag), []byte(sums))
	m.write(tag, SignatureName(tag), m.signer.sign([]byte(sums), trusted))
}

// sumLine returns the checksum line of a file of a release.
//
// Parameters:
//   - t: running test
//   - m: mirror holding the release
//   - tag: release tag
//   - name: file name
func sumLine(t *testing.T, m *mirror, tag, name string) string {
	t.Helper()

	sum := sha256.Sum256([]byte(content(t, m.path(tag, name))))

	return hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

// TestApplyHostile runs a corpus of tampered releases and checks that each one is refused with
// the right error, that the old binary stays and that the staging directory is gone.
//
// Parameters:
//   - t: test to report to
func TestApplyHostile(t *testing.T) {
	const tag = "1.1.0"

	archive := ArchiveName(tag, "linux", "amd64")
	sums := ChecksumsName(tag)
	good := comment(tag, sums, "stable", testStamp)

	tests := []struct {
		name   string
		damage func(t *testing.T, m *mirror, u *Updater)
		want   error
	}{
		{"flipped bit in the checksums", func(t *testing.T, m *mirror, u *Updater) {
			b := []byte(content(t, m.path(tag, sums)))
			b[0] ^= 1
			m.write(tag, sums, b)
		}, ErrBadSignature},
		{"flipped bit in the signature file", func(t *testing.T, m *mirror, u *Updater) {
			b := []byte(content(t, m.path(tag, SignatureName(tag))))
			b[len(b)-10] ^= 1
			m.write(tag, SignatureName(tag), b)
		}, ErrBadSignature},
		{"truncated signature", func(t *testing.T, m *mirror, u *Updater) {
			b := []byte(content(t, m.path(tag, SignatureName(tag))))
			m.write(tag, SignatureName(tag), b[:len(b)/2])
		}, ErrSignatureFormat},
		{"empty signature", func(t *testing.T, m *mirror, u *Updater) {
			m.write(tag, SignatureName(tag), nil)
		}, ErrSignatureFormat},
		{"trailing garbage in the signature", func(t *testing.T, m *mirror, u *Updater) {
			b := []byte(content(t, m.path(tag, SignatureName(tag))))
			m.write(tag, SignatureName(tag), append(b, "junk\n"...))
		}, ErrSignatureFormat},
		{"signed by another key", func(t *testing.T, m *mirror, u *Updater) {
			m.signer = newSigner(9)
			resign(m, tag, content(t, m.path(tag, sums)), good)
		}, ErrUnknownKey},
		{"key id of ours but not our key", func(t *testing.T, m *mirror, u *Updater) {
			forger := newSigner(9)
			forger.id = u.Keys[0].ID
			m.signer = forger
			resign(m, tag, content(t, m.path(tag, sums)), good)
		}, ErrBadSignature},
		{"legacy signature", func(t *testing.T, m *mirror, u *Updater) {
			body := []byte(content(t, m.path(tag, sums)))
			m.write(tag, SignatureName(tag), m.signer.signLegacy(body, good))
		}, ErrLegacySignature},
		{"archive altered after signing", func(t *testing.T, m *mirror, u *Updater) {
			b := []byte(content(t, m.path(tag, archive)))
			b[len(b)/2] ^= 1
			m.write(tag, archive, b)
		}, ErrBadChecksum},
		{"archive replaced by a longer one", func(t *testing.T, m *mirror, u *Updater) {
			m.write(tag, archive, append([]byte(content(t, m.path(tag, archive))), 'x'))
		}, ErrBadChecksum},
		{"archive not listed in the signed sums", func(t *testing.T, m *mirror, u *Updater) {
			resign(m, tag, strings.Repeat("0", 64)+"  other.tar.gz\n", good)
		}, ErrBadChecksum},
		{"archive listed twice", func(t *testing.T, m *mirror, u *Updater) {
			line := sumLine(t, m, tag, archive)
			resign(m, tag, line+line, good)
		}, ErrBadChecksum},
		{"digest of the wrong length", func(t *testing.T, m *mirror, u *Updater) {
			resign(m, tag, "abcd  "+archive+"\n", good)
		}, ErrBadChecksum},
		{"comment of another version", func(t *testing.T, m *mirror, u *Updater) {
			resign(m, tag, content(t, m.path(tag, sums)), comment("1.0.9", sums, "stable", testStamp))
		}, ErrBadComment},
		{"comment of another file", func(t *testing.T, m *mirror, u *Updater) {
			other := comment(tag, "tote_1.0.0_checksums.txt", "stable", testStamp)
			resign(m, tag, content(t, m.path(tag, sums)), other)
		}, ErrBadComment},
		{"comment of another channel", func(t *testing.T, m *mirror, u *Updater) {
			resign(m, tag, content(t, m.path(tag, sums)), comment(tag, sums, "beta", testStamp))
		}, ErrBadComment},
		{"comment of another project", func(t *testing.T, m *mirror, u *Updater) {
			c := strings.Replace(good, "project:tote", "project:other", 1)
			resign(m, tag, content(t, m.path(tag, sums)), c)
		}, ErrBadComment},
		{"comment without a timestamp", func(t *testing.T, m *mirror, u *Updater) {
			resign(m, tag, content(t, m.path(tag, sums)), comment(tag, sums, "stable", 0))
		}, ErrBadComment},
		{"comment with a repeated field", func(t *testing.T, m *mirror, u *Updater) {
			resign(m, tag, content(t, m.path(tag, sums)), good+"\tversion:9.9.9")
		}, ErrSignatureFormat},
		{"archive is not gzip", func(t *testing.T, m *mirror, u *Updater) {
			m.write(tag, archive, []byte("plain"))
			resign(m, tag, sumLine(t, m, tag, archive), good)
		}, ErrBadArchive},
		{"archive without a binary", func(t *testing.T, m *mirror, u *Updater) {
			m.write(tag, archive, tarGz(t, tarEntry{Name: "README.md", Body: "x"}))
			resign(m, tag, sumLine(t, m, tag, archive), good)
		}, ErrBadArchive},
		{"binary announces another version", func(t *testing.T, m *mirror, u *Updater) {
			m.write(tag, archive, tarGz(t, tarEntry{Name: "tote", Body: fakeBinary("1.0.5")}))
			resign(m, tag, sumLine(t, m, tag, archive), good)
		}, ErrBadBinary},
		{"binary does not start", func(t *testing.T, m *mirror, u *Updater) {
			u.Run = func(context.Context, string) ([]byte, error) {
				return nil, errors.New("exec format error")
			}
		}, ErrBadBinary},
		{"binary prints nothing", func(t *testing.T, m *mirror, u *Updater) {
			u.Run = func(context.Context, string) ([]byte, error) { return nil, nil }
		}, ErrBadBinary},
		{"archive missing", func(t *testing.T, m *mirror, u *Updater) {
			if err := os.Remove(m.path(tag, archive)); err != nil {
				t.Fatal(err)
			}
		}, ErrNoAsset},
		{"signature missing", func(t *testing.T, m *mirror, u *Updater) {
			if err := os.Remove(m.path(tag, SignatureName(tag))); err != nil {
				t.Fatal(err)
			}
		}, ErrNoAsset},
		{"checksums missing", func(t *testing.T, m *mirror, u *Updater) {
			if err := os.Remove(m.path(tag, sums)); err != nil {
				t.Fatal(err)
			}
		}, ErrNoAsset},
	}

	for _, tt := range tests {
		m := newMirror(t, newSigner(1))
		m.publish(fakeRelease{Tag: tag})

		u, target := m.updater("1.0.0")
		tt.damage(t, m, u)

		_, err := u.Apply(context.Background())
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}

		if got := content(t, target); got != fakeBinary("1.0.0") {
			t.Errorf("%s: the binary changed to %q", tt.name, got)
		}

		if got := names(t, filepath.Dir(target)); !slices.Equal(got, []string{"tote"}) {
			t.Errorf("%s: directory holds %v", tt.name, got)
		}
	}
}

// TestApplyRollback checks the protection against an older signed release.
//
// Parameters:
//   - t: test to report to
func TestApplyRollback(t *testing.T) {
	tests := []struct {
		name    string
		release int64
		known   int64
		force   bool
		want    error
	}{
		{"older than the receipt", testStamp - 1, testStamp, false, ErrRollback},
		{"same time", testStamp, testStamp, false, nil},
		{"newer", testStamp + 1, testStamp, false, nil},
		{"no timestamp in the receipt", testStamp, 0, false, nil},
		{"older but forced", testStamp - 1, testStamp, true, nil},
	}

	for _, tt := range tests {
		m := newMirror(t, newSigner(1))
		m.publish(fakeRelease{Tag: "1.1.0", Stamp: tt.release})

		u, target := m.updater("1.0.0")
		u.Receipt = &Receipt{Schema: 1, InstallMethod: "script", ReleaseTimestamp: tt.known}
		u.Force = tt.force

		_, err := u.Apply(context.Background())
		if !errors.Is(err, tt.want) && (tt.want != nil || err != nil) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}

		want := fakeBinary("1.1.0")
		if tt.want != nil {
			want = fakeBinary("1.0.0")
		}

		if got := content(t, target); got != want {
			t.Errorf("%s: target = %q", tt.name, got)
		}
	}
}

// TestApplyStableRefusesBeta checks that a stable install never takes a pre-release, even when
// it is the only newer release and the signature is sound.
//
// Parameters:
//   - t: test to report to
func TestApplyStableRefusesBeta(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.0.0"})
	m.publish(fakeRelease{Tag: "1.1.0-beta.1", Prerelease: true})

	u, target := m.updater("1.0.0")

	res, err := u.Apply(context.Background())
	if err != nil || res.Status != StatusUpToDate {
		t.Errorf("result = %+v, %v", res, err)
	}

	if content(t, target) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}
}

// TestApplyReplaceFailure checks that a failed rename leaves the old binary and cleans up.
//
// Parameters:
//   - t: test to report to
func TestApplyReplaceFailure(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, target := m.updater("1.0.0")
	u.Rename = func(string, string) error { return errors.New("disk on fire") }

	_, err := u.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("error = %v", err)
	}

	if content(t, target) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}

	if got := names(t, filepath.Dir(target)); !slices.Equal(got, []string{"tote"}) {
		t.Errorf("directory holds %v", got)
	}
}

// TestApplyReplaceIsAtomic checks that a reader of the target sees the old or the new content
// at every moment of the replacement, never a partial file.
//
// Parameters:
//   - t: test to report to
func TestApplyReplaceIsAtomic(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, target := m.updater("1.0.0")

	var seen []string

	u.Rename = func(oldpath, newpath string) error {
		// Just before the rename the target is still whole and the new file is complete.
		seen = append(seen, content(t, newpath), content(t, oldpath))

		return os.Rename(oldpath, newpath)
	}

	if _, err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 2 || seen[0] != fakeBinary("1.0.0") || seen[1] != fakeBinary("1.1.0") {
		t.Errorf("contents before the rename = %q", seen)
	}

	if content(t, target) != fakeBinary("1.1.0") {
		t.Error("the binary was not replaced")
	}
}

// TestApplyCancel checks that a cancellation at every step leaves the old binary and no file.
//
// Parameters:
//   - t: test to report to
func TestApplyCancel(t *testing.T) {
	for _, at := range []Step{StepDownload, StepVerify, StepInstall} {
		m := newMirror(t, newSigner(1))
		m.publish(fakeRelease{Tag: "1.1.0"})

		u, target := m.updater("1.0.0")
		ctx, cancel := context.WithCancel(context.Background())

		u.Progress = func(s Step) {
			if s == at {
				cancel()
			}
		}

		if _, err := u.Apply(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("cancel at %s: error = %v, want context.Canceled", at, err)
		}

		if content(t, target) != fakeBinary("1.0.0") {
			t.Errorf("cancel at %s: the binary changed", at)
		}

		if got := names(t, filepath.Dir(target)); !slices.Equal(got, []string{"tote"}) {
			t.Errorf("cancel at %s: directory holds %v", at, got)
		}

		cancel()
	}
}

// TestApplyNotWritable checks the report for a directory the user cannot write in.
//
// Parameters:
//   - t: test to report to
func TestApplyNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}

	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, target := m.updater("1.0.0")
	dir := filepath.Dir(target)

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := u.Apply(context.Background())

	var nw *NotWritableError
	if !errors.Is(err, ErrNotWritable) || !errors.As(err, &nw) || nw.Dir != dir {
		t.Fatalf("error = %v, want a NotWritableError for %s", err, dir)
	}

	if content(t, target) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}
}

// TestApplyBadTarget checks that only a regular file is replaced.
//
// Parameters:
//   - t: test to report to
func TestApplyBadTarget(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, target := m.updater("1.0.0")
	link := filepath.Join(filepath.Dir(target), "link")

	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"symbolic link": link,
		"directory":     filepath.Dir(target),
		"missing":       filepath.Join(filepath.Dir(target), "absent"),
	} {
		u.Target = path

		if _, err := u.Apply(context.Background()); err == nil {
			t.Errorf("%s: Apply succeeded", name)
		}
	}

	if content(t, target) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}
}

// TestReceiptFailureDoesNotUndo checks that a receipt that cannot be written is reported
// without undoing the update.
//
// Parameters:
//   - t: test to report to
func TestReceiptFailureDoesNotUndo(t *testing.T) {
	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: "1.1.0"})

	u, target := m.updater("1.0.0")

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	u.ReceiptPath = filepath.Join(blocker, "install.json")

	res, err := u.Apply(context.Background())
	if err != nil || res.Status != StatusUpdated || res.ReceiptErr == nil {
		t.Errorf("result = %+v, %v", res, err)
	}

	if content(t, target) != fakeBinary("1.1.0") {
		t.Error("the binary was not replaced")
	}
}

// TestDefaultRun checks the real execution of the sanity check on a script and the output cap.
//
// Parameters:
//   - t: test to report to
func TestDefaultRun(t *testing.T) {
	sh := filepath.Join(t.TempDir(), "tote")

	script := "#!/bin/sh\n" +
		"[ \"$1\" = version ] && echo \"tote 9.9.9 $TOTE_LANG\" && head -c 100000 /dev/zero\n"
	if err := os.WriteFile(sh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := defaultRun(context.Background(), sh)
	if err != nil {
		t.Skipf("cannot run a script here: %v", err)
	}

	if !strings.HasPrefix(string(out), "tote 9.9.9 en\n") || len(out) != maxVersionOutput {
		t.Errorf("output = %d bytes starting %q", len(out), out[:min(len(out), 20)])
	}

	if !announces(out, "9.9.9") || announces(out, "9.9") || announces([]byte("tote"), "9.9.9") ||
		announces([]byte("other 9.9.9"), "9.9.9") {
		t.Error("announces is wrong")
	}
}
