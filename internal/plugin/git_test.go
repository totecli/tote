package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/store"
)

// fakeGit installs a git that records how it was called and answers like git for the two
// questions the helper asks.
//
// Parameters:
//   - l: sandbox
//
// Returns the path of the log.
func fakeGit(l *lab) string {
	log := filepath.Join(l.root, "git.log")

	l.script("git", `{
printf 'ARGS'; printf ' <%s>' "$@"; printf '\n'
env | grep -v '^PWD=\|^OLDPWD=\|^SHLVL=\|^_=' | sort | sed 's/^/ENV /'
} >> `+log+`
case "$*" in
*get-regexp*) echo "remote.origin.url git@github.com:o/r.git";;
*symbolic-ref*) echo main;;
esac
`)

	return log
}

// calls counts the runs of the fake git.
//
// Parameters:
//   - t: test to report to
//   - log: path of the log
func calls(t *testing.T, log string) int {
	t.Helper()

	data, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return 0
	}

	must(t, err)

	return strings.Count(string(data), "ARGS")
}

// derive runs the repository derivation for a saved repository of the sandbox.
//
// Parameters:
//   - l: sandbox
//   - r: registry
//   - name: saved name
//   - needs: members to derive
func derive(t *testing.T, l *lab, r *Registry, name string, needs RepoNeeds) RepoInfo {
	t.Helper()

	col, err := store.OpenRepos(l.paths.ReposFile())
	must(t, err)

	saved, ok := col.Get(name)
	if !ok {
		t.Fatalf("repository %q is not saved", name)
	}

	info, ok := r.repoInfo(r.context(), saved, needs)
	if !ok {
		t.Fatalf("repository %q has no usable path", name)
	}

	return info
}

// TestGitHelperCall checks how git is started: the exact arguments, and an environment built from
// nothing with the six variables of the specification, whatever tote itself runs in.
//
// Parameters:
//   - t: test to report to
func TestGitHelperCall(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()

	for k, v := range hostileParent {
		l.env[k] = v
	}

	log := fakeGit(l)
	dir := filepath.Join(l.root, "repos", "r")

	must(t, os.MkdirAll(dir, 0o700))
	l.saveRepo("r", dir)

	info := derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true})

	if info.Remote != "git@github.com:o/r.git" || info.Slug != "o/r" || info.Branch != "main" ||
		info.Name != "r" || info.Path != dir {
		t.Errorf("info %+v", info)
	}

	data, _ := os.ReadFile(log)
	text := string(data)

	for _, want := range []string{
		"ARGS <-c> <core.fsmonitor=false> <-C> <" + dir + "> <config> <--local> <--get-regexp> " +
			`<^remote\..*\.url$>`,
		"ARGS <-c> <core.fsmonitor=false> <-C> <" + dir + "> <symbolic-ref> <--short> <-q> <HEAD>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing call %q in\n%s", want, text)
		}
	}

	var names []string

	for _, line := range strings.Split(text, "\n") {
		if kv, ok := strings.CutPrefix(line, "ENV "); ok {
			names = append(names, strings.SplitN(kv, "=", 2)[0])
		}
	}

	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}

	want := []string{
		"HOME", "PATH", "LC_ALL", "LANG", "GIT_TERMINAL_PROMPT", "GIT_OPTIONAL_LOCKS",
		"GIT_CEILING_DIRECTORIES",
	}

	if len(set) != len(want) {
		t.Errorf("environment names %v want %v", set, want)
	}

	for _, n := range want {
		if !set[n] {
			t.Errorf("%s missing from the environment", n)
		}
	}

	for _, line := range strings.Split(text, "\n") {
		for _, bad := range []string{
			"GIT_DIR", "GIT_CONFIG", "GIT_SSH", "GIT_ASKPASS", "GIT_EXEC_PATH",
			"GITHUB_TOKEN", "SSH_AUTH_SOCK", "LD_PRELOAD",
		} {
			if strings.HasPrefix(line, "ENV "+bad) {
				t.Errorf("forbidden variable: %s", line)
			}
		}
	}
}

// TestGitHelperCache checks the five second cache and that git is not started when only the
// path and the name are read.
//
// Parameters:
//   - t: test to report to
func TestGitHelperCache(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()

	log := fakeGit(l)
	dir := filepath.Join(l.root, "repos", "r")

	must(t, os.MkdirAll(dir, 0o700))
	l.saveRepo("r", dir)

	r := l.reg(nil)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	r.rt.git.now = func() time.Time { return now }

	if info := derive(t, l, r, "r", RepoNeeds{}); info.Path != dir || info.Slug != "" {
		t.Errorf("info %+v", info)
	}

	if n := calls(t, log); n != 0 {
		t.Fatalf("%d git runs for the path alone", n)
	}

	derive(t, l, r, "r", RepoNeeds{Git: true})
	first := calls(t, log)

	if first != 2 {
		t.Fatalf("%d git runs, want 2 (remote and branch)", first)
	}

	now = now.Add(4 * time.Second)
	derive(t, l, r, "r", RepoNeeds{Git: true})

	if n := calls(t, log); n != first {
		t.Errorf("a cached derivation ran git again: %d", n)
	}

	now = now.Add(2 * time.Second)
	derive(t, l, r, "r", RepoNeeds{Git: true})

	if n := calls(t, log); n != 2*first {
		t.Errorf("an expired derivation did not run git: %d", n)
	}
}

// TestGitHelperHostileRepo checks a repository whose own config tries to run a program or to
// hand a credential to the plugin: nothing runs, nothing leaks.
//
// Parameters:
//   - t: test to report to
func TestGitHelperHostileRepo(t *testing.T) {
	canary := func(l *lab) string { return filepath.Join(l.root, "canary") }

	t.Run("config that runs programs", func(t *testing.T) {
		l := newLab(t)
		l.useSystemTools()

		dir := l.gitRepo("r", "")
		hook := filepath.Join(l.root, "hook.sh")

		l.writeFile(hook, "#!/bin/sh\ntouch "+canary(l)+"\n")
		must(t, os.Chmod(hook, 0o700))

		cfg := filepath.Join(dir, ".git", "config")
		data, err := os.ReadFile(cfg)
		must(t, err)

		hostile := string(data) + "\n[core]\n\tfsmonitor = " + hook + "\n\tsshCommand = " + hook +
			"\n\thooksPath = " + l.root + "\n[remote \"origin\"]\n\turl = ext::sh -c \"touch " +
			canary(l) + "\"\n[include]\n\tpath = " + hook + "\n[alias]\n\tconfig = !" + hook + "\n"
		must(t, os.WriteFile(cfg, []byte(hostile), 0o600))

		info := derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true})

		if info.Remote != "" || info.Slug != "" {
			t.Errorf("remote %q slug %q", info.Remote, info.Slug)
		}

		if _, err := os.Stat(canary(l)); err == nil {
			t.Fatal("a program named by the repository ran")
		}
	})

	t.Run("credentials in the remote are dropped", func(t *testing.T) {
		l := newLab(t)
		l.useSystemTools()
		l.gitRepo("r", "https://user:s3cretpassword@github.com/o/r.git")

		info := derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true})

		if strings.Contains(info.Remote, "s3cret") || strings.Contains(info.Remote, "user") ||
			info.Slug != "o/r" || info.Branch != "main" {
			t.Errorf("info %+v", info)
		}
	})

	t.Run("detached head", func(t *testing.T) {
		l := newLab(t)
		l.useSystemTools()

		dir := l.gitRepo("r", "https://github.com/o/r")
		l.git(dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty",
			"-m", "x")
		l.git(dir, "checkout", "-q", "--detach")

		info := derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true})
		if info.Branch != "" || info.Slug != "o/r" {
			t.Errorf("info %+v", info)
		}
	})

	t.Run("a directory below another repository", func(t *testing.T) {
		l := newLab(t)
		l.useSystemTools()

		outer := l.gitRepo("outer", "https://github.com/o/outer")
		inner := filepath.Join(outer, "inner")

		must(t, os.MkdirAll(inner, 0o700))
		l.saveRepo("inner", inner)

		info := derive(t, l, l.reg(nil), "inner", RepoNeeds{Git: true})
		if info.Remote != "" || info.Branch != "" {
			t.Errorf("the helper climbed to the outer repository: %+v", info)
		}
	})

	t.Run("one remote that is not origin", func(t *testing.T) {
		l := newLab(t)
		l.useSystemTools()

		dir := l.gitRepo("r", "")
		l.git(dir, "remote", "add", "upstream", "https://github.com/o/fork")

		if info := derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true}); info.Slug != "o/fork" {
			t.Errorf("info %+v", info)
		}
	})

	t.Run("two remotes, none is origin", func(t *testing.T) {
		l := newLab(t)
		l.useSystemTools()

		dir := l.gitRepo("r", "")
		l.git(dir, "remote", "add", "a", "https://github.com/o/a")
		l.git(dir, "remote", "add", "b", "https://github.com/o/b")

		if info := derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true}); info.Slug != "" {
			t.Errorf("info %+v", info)
		}
	})
}

// TestGitHelperLookup checks where git may come from: not from the repository, not from the
// working directory, not from a relative PATH entry; and that a missing git gives empty values.
//
// Parameters:
//   - t: test to report to
func TestGitHelperLookup(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()

	dir := l.gitRepo("r", "https://github.com/o/r")
	mark := filepath.Join(l.root, "planted")
	planted := "#!/bin/sh\ntouch " + mark + "\n"

	for _, d := range []string{dir, l.cwd()} {
		must(t, os.MkdirAll(d, 0o700))
		must(t, os.WriteFile(filepath.Join(d, "git"), []byte(planted), 0o700))
	}

	l.env["PATH"] = ".:" + dir + ":" + l.cwd() + ":" + shellPath

	info := derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true})
	if info.Slug != "o/r" {
		t.Errorf("the real git was not used: %+v", info)
	}

	if _, err := os.Stat(mark); err == nil {
		t.Fatal("a git planted in the repository or the working directory ran")
	}

	l.env["PATH"] = filepath.Join(l.root, "nothing")

	info = derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true})
	if info.Remote != "" || info.Branch != "" || info.Path != dir {
		t.Errorf("no git: %+v", info)
	}
}

// TestGitHelperTimeout checks that a git that hangs is stopped after 1500 ms with its children,
// and that the answer is empty values, not an error.
//
// Parameters:
//   - t: test to report to
func TestGitHelperTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("takes 1.5 s")
	}

	l := newLab(t)
	l.useSystemTools()

	pidFile := filepath.Join(l.root, "child.pid")

	l.script("git", "sleep 60 &\necho $! > "+pidFile+"\nwait\n")

	dir := filepath.Join(l.root, "repos", "r")

	must(t, os.MkdirAll(dir, 0o700))
	l.saveRepo("r", dir)

	began := time.Now()
	info := derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true})

	if took := time.Since(began); took > 4*time.Second {
		t.Errorf("took %v", took)
	}

	if info.Remote != "" || info.Branch != "" {
		t.Errorf("info %+v", info)
	}

	waitGone(t, pidFile)
}

// TestGitHelperBigOutput checks that a git that prints far more than the limit is read up to the
// limit and does not block.
//
// Parameters:
//   - t: test to report to
func TestGitHelperBigOutput(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()
	l.script("git", `echo "remote.origin.url https://github.com/o/r"
i=0
while [ $i -lt 40000 ]; do echo "remote.zz$i.url https://github.com/o/$i"; i=$((i+1)); done
`)

	dir := filepath.Join(l.root, "repos", "r")

	must(t, os.MkdirAll(dir, 0o700))
	l.saveRepo("r", dir)

	info := derive(t, l, l.reg(nil), "r", RepoNeeds{Git: true})
	if info.Slug != "o/r" {
		t.Errorf("info %+v", info)
	}
}
