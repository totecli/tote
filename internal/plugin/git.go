package plugin

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/totecli/tote/internal/store"
)

// Limits of the git helper of section 3.5.
const (
	gitTimeout    = 1500 * time.Millisecond
	gitCacheTTL   = 5 * time.Second
	gitOutputMax  = 64 << 10
	gitBranchMax  = 256
	gitCacheLimit = 256
)

// gitCache keeps the repository data derived by git for a few seconds, so that a menu that opens
// and closes does not spawn git each time.
type gitCache struct {
	mu      sync.Mutex
	entries map[string]gitEntry
	now     func() time.Time
}

// gitEntry is one cached derivation.
type gitEntry struct {
	at   time.Time
	info RepoInfo
}

// newGitCache creates an empty cache that reads the real clock.
func newGitCache() *gitCache {
	return &gitCache{entries: map[string]gitEntry{}, now: time.Now}
}

// get returns a cached derivation that is younger than the time to live.
//
// Parameters:
//   - key: absolute path of the repository
func (c *gitCache) get(key string) (RepoInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok || c.now().Sub(e.at) >= gitCacheTTL {
		return RepoInfo{}, false
	}

	return e.info, true
}

// put stores a derivation.
//
// Parameters:
//   - key: absolute path of the repository
//   - info: derived data
func (c *gitCache) put(key string, info RepoInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= gitCacheLimit {
		c.entries = map[string]gitEntry{}
	}

	c.entries[key] = gitEntry{at: c.now(), info: info}
}

// RepoNeeds tells which members of a repository a call reads, so that git is spawned only for
// the members that need it.
type RepoNeeds struct {
	Git bool
}

// repoInfo derives the {repo.*} data of one saved repository.
//
// The path and the name never need git. The remote, the slug and the branch do: they come from a
// git run on the scrubbed PATH, in an environment built from nothing, with a timeout of 1500 ms,
// and are cached for 5 seconds. The remote is read from the repository's own config file
// (--local), so it runs no hook and reads no global or included file.
//
// Parameters:
//   - ctx: inputs of the pass (PATH and the places a program may not be found in)
//   - saved: saved repository
//   - needs: members the call reads
//
// Returns the data and true, or false when the saved path is not an existing absolute directory.
func (r *Registry) repoInfo(ctx *loadContext, saved store.Repo, needs RepoNeeds) (RepoInfo, bool) {
	dir, ok := ExpandRepoPath(saved.Path, r.opts.Home)
	if !ok {
		return RepoInfo{}, false
	}

	info := RepoInfo{Name: saved.Name, Path: dir}
	if !needs.Git {
		return info, true
	}

	if cached, hit := r.rt.git.get(dir); hit {
		cached.Name = saved.Name

		return cached, true
	}

	remote, branch := r.gitFacts(ctx, dir)
	info.Remote, info.Slug, info.Branch = remote, SlugOf(remote), branch

	r.rt.git.put(dir, info)

	return info, true
}

// gitFacts runs git for the remote and the branch of a repository. Any failure gives empty
// values: the placeholders that need them are then unresolved, never an error.
//
// Parameters:
//   - ctx: inputs of the pass
//   - dir: absolute path of the repository
func (r *Registry) gitFacts(ctx *loadContext, dir string) (remote, branch string) {
	avoid := ctx.avoid
	avoid.Exactly = append(append([]string(nil), avoid.Exactly...), dir)

	bin, found := LookBin(ctx.pathEnv, "git", avoid)
	if !found {
		return "", ""
	}

	env := r.gitEnv(ctx, avoid, dir)

	run, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	out, ok := runGit(run, bin, env, dir, "config", "--local", "--get-regexp",
		`^remote\..*\.url$`)
	if ok {
		remote = pickRemote(out)
	}

	out, ok = runGit(run, bin, env, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if ok {
		branch = cleanBranch(string(out))
	}

	return remote, branch
}

// gitEnv builds the environment of the git helper from nothing. GIT_DIR, GIT_WORK_TREE,
// GIT_INDEX_FILE, GIT_CONFIG*, GIT_EXEC_PATH, GIT_SSH* and GIT_ASKPASS are never present. The
// ceiling stops git from looking above the repository for another one.
//
// Parameters:
//   - ctx: inputs of the pass
//   - avoid: places no program may be found in
//   - dir: repository
func (r *Registry) gitEnv(ctx *loadContext, avoid Avoid, dir string) []string {
	env := map[string]string{
		"HOME":                    r.opts.Home,
		"LC_ALL":                  "C",
		"LANG":                    "C",
		"GIT_TERMINAL_PROMPT":     "0",
		"GIT_OPTIONAL_LOCKS":      "0",
		"GIT_CEILING_DIRECTORIES": filepath.Dir(dir),
	}

	if path := scrubPath(ctx.pathEnv, avoid); path != "" {
		env["PATH"] = path
	}

	return flattenEnv(env)
}

// runGit runs git with the safety options and returns its standard output.
//
// Parameters:
//   - ctx: deadline of the helper
//   - bin: absolute path of git
//   - env: environment, used as given
//   - dir: repository, given to -C
//   - args: git arguments after the options
//
// Returns the output, at most 64 KiB, and true when git exited with status 0.
func runGit(ctx context.Context, bin string, env []string, dir string,
	args ...string,
) ([]byte, bool) {
	argv := append([]string{"-c", "core.fsmonitor=false", "-C", dir}, args...)

	var out limitedBuffer

	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdout = &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 200 * time.Millisecond

	if err := cmd.Run(); err != nil {
		return nil, false
	}

	return out.Bytes(), true
}

// limitedBuffer keeps the first gitOutputMax bytes written to it and drops the rest.
type limitedBuffer struct {
	buf bytes.Buffer
}

// Write implements io.Writer. It reports the whole write as done, so that the writer is never
// blocked.
//
// Parameters:
//   - p: bytes to keep, up to the limit
func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := gitOutputMax - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(room, len(p))])
	}

	return len(p), nil
}

// Bytes returns what was kept.
func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

// pickRemote chooses the remote url from the output of "git config --get-regexp": the url of
// origin, else the only remote when there is exactly one. The url is cleaned by CleanRemote.
//
// Parameters:
//   - out: lines "remote.<name>.url <url>"
func pickRemote(out []byte) string {
	urls := map[string]string{}

	var names []string

	for _, line := range strings.Split(string(out), "\n") {
		key, value, found := strings.Cut(line, " ")
		if !found || !strings.HasPrefix(key, "remote.") || !strings.HasSuffix(key, ".url") {
			continue
		}

		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		if _, dup := urls[name]; dup {
			continue
		}

		urls[name] = value

		names = append(names, name)
	}

	switch {
	case urls["origin"] != "":
		return cleaned(urls["origin"])
	case len(names) == 1:
		return cleaned(urls[names[0]])
	}

	return ""
}

// cleaned returns the remote after CleanRemote, or an empty string when it is refused.
//
// Parameters:
//   - raw: url as git prints it
func cleaned(raw string) string {
	out, _ := CleanRemote(raw)

	return out
}

// cleanBranch checks a branch name read from git: valid UTF-8, no control character, no leading
// '-', at most 256 bytes. Anything else gives an empty name.
//
// Parameters:
//   - raw: output of git symbolic-ref
func cleanBranch(raw string) string {
	b := strings.TrimRight(raw, "\r\n")

	if b == "" || len(b) > gitBranchMax || !utf8.ValidString(b) || strings.HasPrefix(b, "-") {
		return ""
	}

	for _, c := range b {
		if unsafeRune(c) || c == '\t' || c == ' ' {
			return ""
		}
	}

	return b
}
