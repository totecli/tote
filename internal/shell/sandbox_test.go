package shell

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fakeToteEnv is the variable that turns the test binary into the fake tote program.
const fakeToteEnv = "SHELL_TEST_FAKE_TOTE"

// TestMain runs the fake tote program when the test binary is started as one, and the tests
// otherwise.
//
// Parameters:
//   - m: test set to run
func TestMain(m *testing.M) {
	if os.Getenv(fakeToteEnv) == "1" {
		os.Exit(fakeTote(os.Args[1:]))
	}

	os.Exit(m.Run())
}

// fakeTote plays the part of the tote binary for the wrapper tests. It records its arguments and
// its hand-off variables in FAKE_DIR, prints a reload definition for "init", then leaves the
// hand-off asked for by FAKE_ACTION, FAKE_RELOAD and FAKE_PAYLOAD, or the raw bytes of FAKE_RAW,
// or a link to FAKE_LINK, and exits with FAKE_RC.
//
// Parameters:
//   - args: arguments received from the wrapper
//
// Returns the exit code.
func fakeTote(args []string) int {
	dir := os.Getenv("FAKE_DIR")
	handoff := os.Getenv("TOTE_HANDOFF")
	record := strings.Join(args, "\n") + "\n"

	_ = os.WriteFile(filepath.Join(dir, "argv"), []byte(record), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "env"), []byte(handoff+"\n"+os.Getenv("TOTE_SHELL")+"\n"),
		0o600)

	if len(args) > 0 && args[0] == "init" {
		_, _ = os.Stdout.WriteString("RELOADED=yes\n")

		return 0
	}

	leaveHandoff(handoff)

	rc, _ := strconv.Atoi(os.Getenv("FAKE_RC"))

	return rc
}

// leaveHandoff writes the hand-off that the fake tote was asked for.
//
// Parameters:
//   - path: value of TOTE_HANDOFF, possibly empty
func leaveHandoff(path string) {
	if path == "" {
		return
	}

	switch {
	case os.Getenv("FAKE_LINK") != "":
		_ = os.Remove(path)
		_ = os.Symlink(os.Getenv("FAKE_LINK"), path)
	case os.Getenv("FAKE_RAW") != "":
		_ = os.WriteFile(path, []byte(os.Getenv("FAKE_RAW")), 0o600)
	case os.Getenv("FAKE_ACTION") != "":
		h := Handoff{
			Action:  Action(os.Getenv("FAKE_ACTION")),
			Reload:  os.Getenv("FAKE_RELOAD") == "1",
			Payload: os.Getenv("FAKE_PAYLOAD"),
		}
		if err := WriteHandoff(path, h); err != nil {
			_, _ = os.Stderr.WriteString("fake tote: " + err.Error() + "\n")
		}
	}
}

// sandbox is a throw-away environment for running a real shell.
//
// Dir is the working directory and HOME, Bin is a directory placed first in PATH, and Env is
// the complete environment of the child process. Nothing outside Dir is read or written.
type sandbox struct {
	Dir string
	Bin string
	Env []string
}

// systemDirs returns the directories that hold the programs the wrapper runs, found on the PATH
// of the test process so that a host that keeps them elsewhere than /usr/bin does not fail.
//
// Returns the directories joined by ':', falling back to /usr/bin:/bin.
func systemDirs() string {
	dirs := []string{"/usr/bin", "/bin"}

	for _, name := range []string{"env", "mktemp", "rm", "sh", "cat"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}

		if dir := filepath.Dir(path); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	return strings.Join(dirs, ":")
}

// newSandbox builds a sandbox whose PATH is its own bin directory followed by the system ones.
// The directory has its symbolic links resolved, because the child environment carries no PWD and
// a shell then reports the physical path of its directory: on macOS the temporary directory is
// under /var, a link to /private/var, and the paths the tests compare would differ.
//
// Parameters:
//   - t: test to report to
func newSandbox(t *testing.T) *sandbox {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(dir, "bin")

	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}

	env := []string{
		"PATH=" + bin + ":" + systemDirs(),
		"HOME=" + dir,
		"TOTE_CONFIG_DIR=" + filepath.Join(dir, "config"),
		"TOTE_DATA_DIR=" + filepath.Join(dir, "data"),
		"TOTE_STATE_DIR=" + filepath.Join(dir, "state"),
		"TOTE_CACHE_DIR=" + filepath.Join(dir, "cache"),
		"LC_ALL=C",
		"GOCOVERDIR=" + dir,
	}

	return &sandbox{Dir: dir, Bin: bin, Env: env}
}

// setenv adds or replaces one variable of the child environment.
//
// Parameters:
//   - key: variable name
//   - value: new value
func (s *sandbox) setenv(key, value string) {
	prefix := key + "="

	for i, kv := range s.Env {
		if strings.HasPrefix(kv, prefix) {
			s.Env[i] = prefix + value

			return
		}
	}

	s.Env = append(s.Env, prefix+value)
}

// installFakeTote puts an executable named tote in the sandbox bin directory that re-runs the
// test binary as the fake tote program.
//
// Parameters:
//   - t: test to report to
func (s *sandbox) installFakeTote(t *testing.T) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	script := "#!/bin/sh\nexec env " + fakeToteEnv + "=1 " + Quote(self) + " \"$@\"\n"

	if err := os.WriteFile(filepath.Join(s.Bin, "tote"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	s.setenv("FAKE_DIR", s.Dir)
}

// run starts a shell on a script file and returns what it printed.
//
// Parameters:
//   - t: test to report to
//   - shell: path of the shell program
//   - script: text of the script, written to a file in the sandbox
//
// Returns standard output, standard error and the exit code.
func (s *sandbox) run(t *testing.T, shell, script string) (string, string, int) {
	t.Helper()

	file := filepath.Join(s.Dir, "script.sh")

	if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{file}

	switch filepath.Base(shell) {
	case "bash":
		args = []string{"--noprofile", "--norc", file}
	case "zsh":
		args = []string{"-f", file}
	}

	return s.exec(t, shell, args...)
}

// needShell returns the path of a shell program or skips the test when it is not installed.
//
// Parameters:
//   - t: test to report to
//   - name: program name such as "bash", "dash" or "zsh"
func needShell(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}

	return path
}

// installedShells lists the POSIX shells found on this machine among bash, dash and zsh.
func installedShells() []string {
	var found []string

	for _, name := range []string{"bash", "dash", "zsh"} {
		if path, err := exec.LookPath(name); err == nil {
			found = append(found, path)
		}
	}

	return found
}

// runC runs a shell with "-c" and a command string.
//
// Parameters:
//   - t: test to report to
//   - shell: path of the shell program
//   - command: command string
//
// Returns standard output, standard error and the exit code.
func (s *sandbox) runC(t *testing.T, shell, command string) (string, string, int) {
	t.Helper()

	return s.exec(t, shell, "-c", command)
}

// exec starts a program in the sandbox and waits for it.
//
// Parameters:
//   - t: test to report to
//   - program: path of the program
//   - args: its arguments
//
// Returns standard output, standard error and the exit code.
func (s *sandbox) exec(t *testing.T, program string, args ...string) (string, string, int) {
	t.Helper()

	cmd := exec.Command(program, args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env

	var stdout, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	code := 0

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %s: %v", program, err)
	}

	return stdout.String(), stderr.String(), code
}
