package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Release is a published release of tote.
//
// Tag is the git tag, the version text with a leading "v", and Assets lists the names of the
// files attached to it.
type Release struct {
	Tag     string
	Version Version
	Assets  []string
}

// apiRelease is one entry of the release list of the GitHub API.
type apiRelease struct {
	TagName    string     `json:"tag_name"`
	Draft      bool       `json:"draft"`
	Prerelease bool       `json:"prerelease"`
	Assets     []apiAsset `json:"assets"`
}

// apiAsset is one file of a release in the GitHub API.
type apiAsset struct {
	Name string `json:"name"`
}

// supported tells whether a system has a release build.
//
// Parameters:
//   - goos: operating system name as in GOOS
//   - goarch: architecture name as in GOARCH
func supported(goos, goarch string) bool {
	return (goos == "linux" || goos == "darwin") && (goarch == "amd64" || goarch == "arm64")
}

// ArchiveName returns the name of the release archive for a system.
//
// Parameters:
//   - version: version text such as 1.1.0
//   - goos: linux or darwin
//   - goarch: amd64 or arm64
func ArchiveName(version, goos, goarch string) string {
	return "tote_" + version + "_" + goos + "_" + goarch + ".tar.gz"
}

// ChecksumsName returns the name of the signed checksums file of a release.
//
// Parameters:
//   - version: version text such as 1.1.0
func ChecksumsName(version string) string {
	return "tote_" + version + "_checksums.txt"
}

// SignatureName returns the name of the minisign signature of the checksums file.
//
// Parameters:
//   - version: version text such as 1.1.0
func SignatureName(version string) string {
	return ChecksumsName(version) + ".minisig"
}

// latest finds the newest release the channel may be offered.
//
// Parameters:
//   - ctx: cancels the request
//
// Returns the release, ErrNoRelease when the list holds none that fits, or a transport error.
func (u *Updater) latest(ctx context.Context) (Release, error) {
	data, err := u.Source.fetch(ctx, u.Source.listLocation(), maxListSize)
	if err != nil {
		if errors.Is(err, errMissing) {
			return Release{}, fmt.Errorf("%w: %s", ErrNoRelease, u.channel())
		}

		return Release{}, err
	}

	var list []apiRelease
	if err := json.Unmarshal(data, &list); err != nil {
		return Release{}, fmt.Errorf("%w: release list: %v", ErrServer, err)
	}

	rel, ok := pickLatest(list, u.channel())
	if !ok {
		return Release{}, fmt.Errorf("%w: %s", ErrNoRelease, u.channel())
	}

	return rel, nil
}

// pickLatest chooses the highest version of a release list that a channel may be offered.
//
// Drafts, tags that are not strict versions and, on the stable channel, pre-releases are
// ignored. A release GitHub flags as a pre-release counts as one even if its tag does not.
//
// Parameters:
//   - list: releases in the order of the API
//   - channel: ChannelStable or ChannelBeta
//
// Returns the release and true, or false when none fits.
func pickLatest(list []apiRelease, channel Channel) (Release, bool) {
	var best Release

	found := false

	for _, r := range list {
		v, err := ParseVersion(strings.TrimPrefix(r.TagName, "v"))
		if err != nil || r.Draft {
			continue
		}

		if (r.Prerelease || v.Prerelease()) && channel != ChannelBeta {
			continue
		}

		if found && v.Compare(best.Version) <= 0 {
			continue
		}

		best = Release{Tag: r.TagName, Version: v}
		best.Assets = best.Assets[:0]

		for _, a := range r.Assets {
			best.Assets = append(best.Assets, a.Name)
		}

		found = true
	}

	return best, found
}

// has reports whether the release carries a file.
//
// Parameters:
//   - name: file name
func (r Release) has(name string) bool {
	return slices.Contains(r.Assets, name)
}
