package plugin

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/totecli/tote/internal/store"
)

// maxRemoteBytes is the longest remote url that is kept: one argv element.
const maxRemoteBytes = MaxArgvElemBytes

// slugPartRe matches the owner and the name of a GitHub slug.
var slugPartRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// scpUserRe matches the user of an scp-like remote, which is kept.
var scpUserRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// remoteHostRe matches the host of a remote, with an optional port or an IPv6 literal.
var remoteHostRe = regexp.MustCompile(`^[A-Za-z0-9._:\[\]-]+$`)

// RepoInfo is the {repo.*} data of one repository, derived by tote and never taken from the
// plugin. Any member can be empty, which makes the placeholders that use it unresolved.
type RepoInfo struct {
	Name   string
	Path   string
	Remote string
	Slug   string
	Branch string
}

// member returns one member of the repository by the name that follows "repo." in a placeholder.
//
// Parameters:
//   - name: path, name, remote, slug or branch
func (r RepoInfo) member(name string) string {
	switch name {
	case "path":
		return r.Path
	case "name":
		return r.Name
	case "remote":
		return r.Remote
	case "slug":
		return r.Slug
	case "branch":
		return r.Branch
	}

	return ""
}

// CleanRemote validates the url of a git remote and removes what must not travel to a program.
//
// Parameters:
//   - raw: url as git prints it
//
// Returns the url without user information and true, or an empty string and false when the url is
// refused: another scheme than https, ssh or git (and a scp-like form), a host or a first path
// segment that starts with '-', a control, bidi or space character or any byte outside printable
// ASCII, "ext::" and "file:", the remote helper form <transport>::<address> ("::" before the
// scheme separator or in a scp-like remote, which git resolves to git-remote-<transport>), or more
// than 1024 bytes. The user of a scp-like remote
// (git@host:path) is not a credential and stays; the user and password of a url with a scheme are
// dropped, and a url with a scheme that still holds an "@" after that is refused.
func CleanRemote(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)

	if raw == "" || len(raw) > maxRemoteBytes || !printableASCII(raw) {
		return "", false
	}

	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "ext::") || strings.HasPrefix(lower, "file:") {
		return "", false
	}

	head, _, _ := strings.Cut(raw, "://")
	if strings.Contains(head, "::") {
		return "", false
	}

	if strings.Contains(raw, "://") {
		return cleanSchemeRemote(raw)
	}

	return cleanScpRemote(raw)
}

// printableASCII reports whether every byte is a visible ASCII character: no space, no control
// character and nothing above 0x7e, so that no bidi or zero-width rune can pass.
//
// Parameters:
//   - s: text to test
func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] <= 0x20 || s[i] >= 0x7f {
			return false
		}
	}

	return true
}

// cleanSchemeRemote handles scheme://[user[:password]@]host[:port]/path.
//
// Parameters:
//   - raw: url that contains "://"
func cleanSchemeRemote(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}

	switch strings.ToLower(u.Scheme) {
	case "https", "ssh", "git":
	default:
		return "", false
	}

	host := u.Host
	if !remoteHostRe.MatchString(host) || strings.HasPrefix(host, "-") ||
		strings.HasPrefix(u.Hostname(), "-") {
		return "", false
	}

	path := strings.TrimPrefix(u.EscapedPath(), "/")
	if strings.HasPrefix(path, "-") || strings.HasPrefix(firstSegment(path), "-") {
		return "", false
	}

	out := strings.ToLower(u.Scheme) + "://" + host + u.EscapedPath()
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}

	// An "@" left in the path or the query could be a credential that was written there; a url with a
	// scheme never keeps one.
	if strings.Contains(out, "@") {
		return "", false
	}

	return out, printableASCII(out)
}

// cleanScpRemote handles [user@]host:path.
//
// Parameters:
//   - raw: remote that has no scheme
func cleanScpRemote(raw string) (string, bool) {
	colon := strings.IndexByte(raw, ':')
	slash := strings.IndexByte(raw, '/')

	if colon <= 0 || (slash >= 0 && slash < colon) {
		return "", false
	}

	hostPart, path := raw[:colon], raw[colon+1:]

	user, host := "", hostPart
	if at := strings.LastIndexByte(hostPart, '@'); at >= 0 {
		user, host = hostPart[:at], hostPart[at+1:]
		if user == "" || !scpUserRe.MatchString(user) {
			return "", false
		}
	}

	if !remoteHostRe.MatchString(host) || strings.HasPrefix(host, "-") ||
		strings.ContainsAny(host, "[]") {
		return "", false
	}

	if path == "" || strings.HasPrefix(firstSegment(path), "-") || strings.Contains(path, "@") {
		return "", false
	}

	if user != "" {
		return user + "@" + host + ":" + path, true
	}

	return host + ":" + path, true
}

// firstSegment returns the text of a path up to the first slash.
//
// Parameters:
//   - path: path without a leading slash
func firstSegment(path string) string {
	path = strings.TrimLeft(path, "/")
	if i := strings.IndexByte(path, '/'); i >= 0 {
		return path[:i]
	}

	return path
}

// SlugOf derives the owner/name of a GitHub remote.
//
// Parameters:
//   - remote: url cleaned by CleanRemote
//
// Returns owner/name for github.com over https, ssh or git (ssh and scp-like forms with any user),
// and an empty string for every other host (an ssh host alias included), for a path that does not
// have exactly two segments, and for a segment outside [A-Za-z0-9._-]+, "." or "..", or that
// starts with '-'. A trailing ".git" and a trailing slash are removed.
func SlugOf(remote string) string {
	host, path := splitRemote(remote)
	if !strings.EqualFold(host, "github.com") {
		return ""
	}

	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")

	if len(parts) != 2 {
		return ""
	}

	for _, p := range parts {
		if !slugPartRe.MatchString(p) || p == "." || p == ".." || strings.HasPrefix(p, "-") {
			return ""
		}
	}

	return parts[0] + "/" + parts[1]
}

// splitRemote returns the host and the path of a cleaned remote. A host with a port is returned
// with it, so that it never equals github.com.
//
// Parameters:
//   - remote: url cleaned by CleanRemote
func splitRemote(remote string) (host, path string) {
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return "", ""
		}

		return u.Host, u.Path
	}

	colon := strings.IndexByte(remote, ':')
	if colon < 0 {
		return "", ""
	}

	host = remote[:colon]
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}

	return host, remote[colon+1:]
}

// ExpandRepoPath resolves the path of a saved repository: a leading "~" becomes the home
// directory and the result must be an absolute, existing directory. No owner check is made:
// shared and WSL mounts must work.
//
// Parameters:
//   - path: path as stored
//   - home: user's home directory
//
// Returns the cleaned absolute path and true, or false when it is not an existing directory.
func ExpandRepoPath(path, home string) (string, bool) {
	switch {
	case path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[2:])
	}

	if !filepath.IsAbs(path) || !utf8.ValidString(path) {
		return "", false
	}

	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", false
	}

	return path, true
}

// CurrentRepo picks the current repository: the saved repository whose resolved path contains the
// working directory, the longest path winning, on a segment boundary.
//
// Parameters:
//   - repos: saved repositories
//   - cwd: tote's working directory
//   - home: user's home directory, for the "~" of stored paths
//
// Returns the repository and true, or false when none contains the directory.
func CurrentRepo(repos []store.Repo, cwd, home string) (store.Repo, bool) {
	want := resolvePath(cwd)

	var (
		best    store.Repo
		bestLen = -1
	)

	for _, r := range repos {
		p, ok := ExpandRepoPath(r.Path, home)
		if !ok {
			continue
		}

		p = resolvePath(p)

		if len(p) > bestLen && containsPath(p, want) {
			best, bestLen = r, len(p)
		}
	}

	return best, bestLen >= 0
}

// resolvePath follows the symbolic links of a path when it can, else cleans it.
//
// Parameters:
//   - p: absolute path
func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}

	return filepath.Clean(p)
}

// containsPath reports whether dir is base or lies below it, comparing whole segments.
//
// Parameters:
//   - base: directory, cleaned
//   - dir: path, cleaned
func containsPath(base, dir string) bool {
	if base == dir {
		return true
	}

	if base == string(filepath.Separator) {
		return strings.HasPrefix(dir, base)
	}

	return strings.HasPrefix(dir, base+string(filepath.Separator))
}
