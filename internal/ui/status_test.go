package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/store"
)

// lookups is a fake branch lookup that records the directories it was asked about.
type lookups struct {
	mu   sync.Mutex
	dirs []string
	by   map[string]branchMsg
}

// get implements the lookup function of statusState.
//
// Parameters:
//   - dir: directory to look up
func (l *lookups) get(dir string) branchMsg {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.dirs = append(l.dirs, dir)

	return l.by[dir]
}

// count returns how many lookups ran.
func (l *lookups) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.dirs)
}

// status replaces the branch lookup of the model.
//
// Parameters:
//   - f: lookup to use
func (g *rig) status(f func(string) branchMsg) {
	g.r.status.lookup = f
}

// statusText returns the status line as plain text.
func (g *rig) statusText() string {
	return stripStyles(g.r.statusLine(g.r.e.Width))
}

// TestStatusBranch checks the branch of the selected repository: looked up once, off the
// interface thread, cached, refreshed after the delay, and silent when unknown.
//
// Parameters:
//   - t: test to report to
func TestStatusBranch(t *testing.T) {
	s := newSandbox(t)
	look := &lookups{by: map[string]branchMsg{s.Repo: {Name: "feature/x", OK: true}}}
	g := newRig(t, s.with(func(o *Options) { o.Start = StartRepos; o.Height = 20 }))
	g.status(look.get)

	if got := g.statusText(); strings.Contains(got, "branch") {
		t.Errorf("a branch is shown before the first message: %q", got)
	}

	g.resize(120, 20)

	if look.count() != 1 || look.dirs[0] != s.Repo {
		t.Fatalf("lookups = %q", look.dirs)
	}

	// The path follows the branch and is cut with an ellipsis when the temporary directory is long.
	got := g.statusText()
	_, after, found := strings.Cut(got, "branch feature/x")

	if !found || !showsPath(strings.TrimLeft(after, " \u00b7"), s.Repo) {
		t.Errorf("status = %q", got)
	}

	g.press("k", "k", "up")

	if look.count() != 1 {
		t.Errorf("%d lookups for the same repository", look.count())
	}

	// The second repository lives under the home directory and is not a repository.
	g.press("j")

	home := filepath.Join(g.r.e.Home, "missing", "docs")
	if look.count() != 2 || look.dirs[1] != home {
		t.Errorf("lookups = %q, want the home path %q", look.dirs, home)
	}

	if got := g.statusText(); strings.Contains(got, "branch") {
		t.Errorf("status of a non-repository = %q", got)
	}

	g.press("k")

	if look.count() != 2 || !strings.Contains(g.statusText(), "branch feature/x") {
		t.Errorf("cache not used: %d lookups, status %q", look.count(), g.statusText())
	}

	// After the delay the branch is read again, and the old name stays until the answer.
	look.by[s.Repo] = branchMsg{Name: "main", OK: true}
	clk := &clock{t: time.Date(2026, 11, 3, 10, 0, 6, 0, time.UTC)}
	g.r.e.Now = clk.now

	g.press("j", "k")

	if got := g.statusText(); !strings.Contains(got, "branch main") {
		t.Errorf("after the delay: %q", got)
	}

	if look.count() != 4 {
		t.Errorf("lookups = %d (%q), want 4", look.count(), look.dirs)
	}
}

// TestStatusPending checks that no second lookup starts while one is running, and that a
// message for another directory or an unknown message changes nothing.
//
// Parameters:
//   - t: test to report to
func TestStatusPending(t *testing.T) {
	s := newSandbox(t)
	look := &lookups{by: map[string]branchMsg{s.Repo: {Name: "main", OK: true}}}
	g := newRig(t, s.with(func(o *Options) { o.Start = StartRepos }))
	g.status(look.get)

	_, first := g.r.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_, second := g.r.Update(keyMsg("x"))

	if first == nil || second != nil {
		t.Fatalf("first %v second %v: want one pending lookup only", first != nil, second != nil)
	}

	g.send(branchMsg{Dir: "/elsewhere", Name: "other", OK: true})

	if name, _ := g.r.status.branch(s.Repo); name != "" {
		t.Errorf("branch known before the answer: %q", name)
	}

	for _, m := range collect(first) {
		g.r.Update(m)
	}

	if name, _ := g.r.status.branch(s.Repo); name != "main" {
		t.Errorf("branch = %q", name)
	}

	if name, _ := g.r.status.branch(""); name != "" {
		t.Errorf("branch of no directory = %q", name)
	}

	g.r.status.apply(g.r.e, 42)

	var zero statusState

	zero.apply(g.r.e, branchMsg{Dir: "/x", Name: "n", OK: true})

	if name, _ := zero.branch("/x"); name != "n" {
		t.Errorf("zero state branch = %q", name)
	}

	if g.r.status.update != "" {
		t.Error("an unknown message changed the update notice")
	}
}

// TestStatusDetachedAndHostile checks a detached head, a hostile branch name and a failed lookup.
//
// Parameters:
//   - t: test to report to
func TestStatusDetachedAndHostile(t *testing.T) {
	s := newSandbox(t)
	look := &lookups{by: map[string]branchMsg{}}
	g := newRig(t, s.with(func(o *Options) { o.Start = StartRepos }))
	g.status(look.get)

	look.by[s.Repo] = branchMsg{Name: "abc1234", Detached: true, OK: true}
	g.resize(100, 30)

	if got := g.statusText(); !strings.Contains(got, "detached abc1234") {
		t.Errorf("detached = %q", got)
	}

	g.r.status = statusState{lookup: look.get}
	look.by[s.Repo] = branchMsg{Name: "ma\x1b[31min\u202e", OK: true}
	g.resize(100, 30)

	if got := g.r.statusLine(g.r.e.Width); strings.Contains(got, "\x1b[31m") ||
		strings.Contains(stripStyles(got), "\u202e") {
		t.Errorf("hostile branch drawn raw: %q", got)
	}

	if got := g.statusText(); !strings.Contains(got, "ma?[31min?") {
		t.Errorf("hostile branch = %q", got)
	}

	g.r.status = statusState{lookup: look.get}
	look.by[s.Repo] = branchMsg{}
	g.resize(100, 30)

	if got := g.statusText(); strings.Contains(got, "branch") || strings.Contains(got, "detached") {
		t.Errorf("failed lookup = %q", got)
	}
}

// TestStatusNoRepoOutsideRepos checks that no lookup runs for the other kinds, the home page and
// a relative path.
//
// Parameters:
//   - t: test to report to
func TestStatusNoRepoOutsideRepos(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.ReposFile(), "schema = 1\n\n[[repo]]\nname = \"rel\"\npath = "+
		"\"relative/dir\"\n")

	look := &lookups{by: map[string]branchMsg{}}

	for _, start := range []Start{
		StartHome, StartConnections, StartAliases, StartFuncs,
		StartRepos,
	} {
		g := newRig(t, s.with(func(o *Options) { o.Start = start }))
		g.status(look.get)
		g.resize(100, 30)
		g.press("ctrl+k")
		g.press("esc")
	}

	if look.count() != 0 {
		t.Errorf("lookups = %q", look.dirs)
	}

	if dir := repoDir(&env{Home: "/h"}, store.Repo{Path: "~/x/../y"}); dir != "/h/y" {
		t.Errorf("repoDir = %q", dir)
	}
}

// TestStatusFollowsPalette checks that a repository selected in the palette gets its branch on
// the status line.
//
// Parameters:
//   - t: test to report to
func TestStatusFollowsPalette(t *testing.T) {
	s := newSandbox(t)
	look := &lookups{by: map[string]branchMsg{s.Repo: {Name: "dev", OK: true}}}
	g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))
	g.status(look.get)
	g.resize(100, 30)
	g.press("ctrl+k")
	g.typeText("r proj")

	if got := g.statusText(); !strings.Contains(got, "branch dev") {
		t.Errorf("status = %q", got)
	}

	g.press("esc")

	if got := g.statusText(); strings.Contains(got, "branch") {
		t.Errorf("status after closing = %q", got)
	}
}

// TestStatusUpdateNotice checks the update slot: set by a message, cleared by an empty one, shown
// on every screen, sanitized.
//
// Parameters:
//   - t: test to report to
func TestStatusUpdateNotice(t *testing.T) {
	for _, start := range []Start{StartHome, StartConnections, StartRepos} {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = start }))
		g.status(func(string) branchMsg { return branchMsg{} })
		g.resize(100, 30)

		if strings.Contains(g.statusText(), "update") {
			t.Fatalf("start %d: notice before any message", start)
		}

		g.send(updateMsg{Version: "2.0.0"})

		if got := g.statusText(); !strings.Contains(got, "update 2.0.0 available") {
			t.Errorf("start %d: status = %q", start, got)
		}

		g.send(updateMsg{Version: "2.1\x1b[0m"})

		if got := g.statusText(); !strings.Contains(got, "update 2.1?[0m available") {
			t.Errorf("start %d: hostile version = %q", start, got)
		}

		g.send(updateMsg{})

		if strings.Contains(g.statusText(), "update") {
			t.Errorf("start %d: the notice stayed", start)
		}
	}

	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Printer = newPrinter("fr") }))
	g.send(updateMsg{Version: "2.0.0"})

	if got := g.statusText(); !strings.Contains(got, "mise \u00e0 jour 2.0.0 disponible") {
		t.Errorf("french notice = %q", got)
	}
}

// TestStatusRenderLayout checks the right-aligned update notice and how a narrow line gives up
// the notice before the command.
//
// Parameters:
//   - t: test to report to
func TestStatusRenderLayout(t *testing.T) {
	g := newRig(t, newSandbox(t))
	e := g.r.e
	s := statusInfo{
		Command: "-> ssh deploy@prod.example.com", Branch: "main", Update: "update 2 available",
	}

	got := stripStyles(s.render(e, 80))

	if !strings.HasSuffix(strings.TrimRight(got, " "), "update 2 available") ||
		e.Metric.width(got) > 80-spareColumns {
		t.Errorf("wide status = %q (%d)", got, e.Metric.width(got))
	}

	narrow := stripStyles(s.render(e, 40))

	if strings.Contains(narrow, "update") || !strings.Contains(narrow, "ssh") {
		t.Errorf("narrow status = %q", narrow)
	}

	only := stripStyles(statusInfo{Update: "update 2 available"}.render(e, 80))
	if strings.TrimSpace(only) != "update 2 available" {
		t.Errorf("notice alone = %q", only)
	}

	for w := 1; w <= 120; w++ {
		for _, in := range []statusInfo{
			s,
			{Update: s.Update},
			{Command: s.Command},
			{Branch: "\u65e5\u672c\u8a9e", Update: "\u8a9e"},
		} {
			if got := e.Metric.width(in.render(e, w)); got > w {
				t.Errorf("status at %d is %d cells", w, got)
			}
		}
	}
}

// TestStatusForRepo checks that the status of a repository keeps its directory even when the
// detail panel is on screen, and shows the path otherwise.
//
// Parameters:
//   - t: test to report to
func TestStatusForRepo(t *testing.T) {
	s := newSandbox(t)
	g := newRig(t, s)
	e := g.r.e
	items := e.Data.items(kindRepo)

	withPanel := statusFor(e, &items[0], true, "")
	if withPanel.Repo != s.Repo || withPanel.Command != "" {
		t.Errorf("with panel = %+v", withPanel)
	}

	without := statusFor(e, &items[0], false, "")
	if without.Repo != s.Repo || without.Command != s.Repo {
		t.Errorf("without panel = %+v", without)
	}

	if got := statusFor(e, nil, false, ""); got != (statusInfo{}) {
		t.Errorf("no item = %+v", got)
	}

	conn := e.Data.items(kindConn)

	got := statusFor(e, &conn[0], false, "")
	if got.Repo != "" || !strings.Contains(got.Command, "ssh") {
		t.Errorf("connection = %+v", got)
	}
}

// gitIn runs git for a test, isolated from the configuration of the machine.
//
// Parameters:
//   - t: test to report to
//   - dir: working directory
//   - args: git arguments
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(gitEnv(os.Environ()), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@example.com")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestGitBranch runs the real lookup on temporary repositories.
//
// Parameters:
//   - t: test to report to
func TestGitBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_DIR", "/nonexistent/elsewhere")
	t.Setenv("GIT_WORK_TREE", "/nonexistent/elsewhere")

	root := t.TempDir()
	repo := filepath.Join(root, "repo")

	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}

	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "symbolic-ref", "HEAD", "refs/heads/feature/\u00e9t\u00e9")

	// An unborn branch has a name but no commit.
	if got := gitBranch(repo); !got.OK || got.Detached || got.Name != "feature/\u00e9t\u00e9" {
		t.Errorf("unborn branch = %+v", got)
	}

	sub := filepath.Join(repo, "sub", "dir")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	if got := gitBranch(sub); !got.OK || got.Name != "feature/\u00e9t\u00e9" {
		t.Errorf("from a subdirectory = %+v", got)
	}

	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "first")
	gitIn(t, repo, "checkout", "-q", "--detach")

	if got := gitBranch(repo); !got.OK || !got.Detached || len(got.Name) < 4 ||
		strings.ContainsAny(got.Name, "/ ") {
		t.Errorf("detached head = %+v", got)
	}

	plain := filepath.Join(root, "plain")
	if err := os.Mkdir(plain, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_CEILING_DIRECTORIES", root)

	if got := gitBranch(plain); got.OK {
		t.Errorf("a directory that is not a repository gave %+v", got)
	}

	if got := gitBranch(filepath.Join(root, "missing")); got.OK {
		t.Errorf("a missing directory gave %+v", got)
	}
}

// TestGitMissing checks that a machine without git gets no branch and no error.
//
// Parameters:
//   - t: test to report to
func TestGitMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if got := gitBranch(t.TempDir()); got.OK || got.Name != "" {
		t.Errorf("without git: %+v", got)
	}
}

// TestGitEnv checks the environment given to git.
//
// Parameters:
//   - t: test to report to
func TestGitEnv(t *testing.T) {
	got := gitEnv([]string{
		"PATH=/bin", "GIT_DIR=/x", "GIT_WORK_TREE=/y", "GIT_INDEX_FILE=/z",
		"GIT_AUTHOR_NAME=n", "HOME=/h",
	})

	want := []string{
		"PATH=/bin", "GIT_AUTHOR_NAME=n", "HOME=/h", "GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	}

	if !slices.Equal(got, want) {
		t.Errorf("gitEnv = %q, want %q", got, want)
	}
}

// TestRunGitTimeout checks that a slow git is cut short and ignored.
//
// Parameters:
//   - t: test to report to
func TestRunGitTimeout(t *testing.T) {
	sleeper := filepath.Join(t.TempDir(), "git")
	writeFile(t, sleeper, "#!/bin/sh\nsleep 5\necho late\n")

	if err := os.Chmod(sleeper, 0o700); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := timeoutCtx(100 * time.Millisecond)
	defer cancel()

	start := time.Now()

	if name, ok := runGit(ctx, sleeper, t.TempDir(), "symbolic-ref", "HEAD"); ok || name != "" {
		t.Errorf("slow git gave %q", name)
	}

	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("the lookup took %v", d)
	}
}

// timeoutCtx returns a context that ends after a delay.
//
// Parameters:
//   - d: delay
func timeoutCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// TestRunGitEmpty checks that a git that prints nothing gives no branch.
//
// Parameters:
//   - t: test to report to
func TestRunGitEmpty(t *testing.T) {
	quiet := filepath.Join(t.TempDir(), "git")
	writeFile(t, quiet, "#!/bin/sh\necho\n")

	if err := os.Chmod(quiet, 0o700); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := timeoutCtx(time.Second)
	defer cancel()

	if name, ok := runGit(ctx, quiet, t.TempDir(), "x"); ok || name != "" {
		t.Errorf("empty output gave %q", name)
	}
}
