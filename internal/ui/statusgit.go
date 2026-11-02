package ui

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// gitTimeout is the longest a git lookup may take before it is given up.
const gitTimeout = 1500 * time.Millisecond

// gitNameMax is the most bytes of a branch name that are kept.
const gitNameMax = 200

// gitBranch reads the branch of a repository with git.
//
// Parameters:
//   - dir: absolute directory of the repository
//
// Returns OK false, silently, when git is not installed, the directory is not a repository or
// git does not answer within gitTimeout. A detached head gives its short hash with Detached
// true. The name is not sanitized here. The call blocks, so it only runs inside a command.
func gitBranch(dir string) branchMsg {
	path, err := exec.LookPath("git")
	if err != nil {
		return branchMsg{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	if name, ok := runGit(ctx, path, dir, "symbolic-ref", "--short", "-q", "HEAD"); ok {
		return branchMsg{Name: name, OK: true}
	}

	if name, ok := runGit(ctx, path, dir, "rev-parse", "--short", "HEAD"); ok {
		return branchMsg{Name: name, Detached: true, OK: true}
	}

	return branchMsg{}
}

// runGit runs git in a directory and returns the first line it prints.
//
// Parameters:
//   - ctx: context that bounds the run
//   - git: path of the git program
//   - dir: directory given to git with -C
//   - args: git command and arguments
//
// Returns false when git fails or prints nothing. Git never prompts, takes no optional lock and
// ignores the variables that would point it at another repository.
func runGit(ctx context.Context, git, dir string, args ...string) (string, bool) {
	cmd := exec.CommandContext(ctx, git, append([]string{"-C", dir}, args...)...)
	cmd.Env = gitEnv(os.Environ())
	cmd.WaitDelay = 200 * time.Millisecond

	out, err := cmd.Output()
	if err != nil {
		return "", false
	}

	line, _, _ := strings.Cut(string(out), "\n")
	line = strings.TrimSpace(line)

	if line == "" {
		return "", false
	}

	return line[:min(len(line), gitNameMax)], true
}

// gitEnv prepares the environment of a git lookup.
//
// Parameters:
//   - environ: environment of the process, in KEY=value form
//
// Returns it without GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE, with the prompts and the
// optional locks turned off.
func gitEnv(environ []string) []string {
	out := make([]string, 0, len(environ)+2)

	for _, kv := range environ {
		switch {
		case strings.HasPrefix(kv, "GIT_DIR="), strings.HasPrefix(kv, "GIT_WORK_TREE="),
			strings.HasPrefix(kv, "GIT_INDEX_FILE="):
		default:
			out = append(out, kv)
		}
	}

	return append(out, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
}
