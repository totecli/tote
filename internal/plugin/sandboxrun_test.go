package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/totecli/tote/internal/store"
)

// shellPath is the PATH of the sandbox: its own program directory first, then the system
// directories that the fixture scripts need for env, cat, sleep and the like.
const shellPath = "/usr/bin:/bin"

// useSystemTools gives the programs of the sandbox the system tools besides the fake ones. The
// lab PATH normally holds only the fake program directory.
func (l *lab) useSystemTools() {
	l.env["PATH"] = l.bin + ":" + shellPath
}

// script writes an executable POSIX shell script into the fake PATH directory.
//
// Parameters:
//   - name: program name
//   - body: script text after the shebang line
func (l *lab) script(name, body string) {
	l.t.Helper()

	path := filepath.Join(l.bin, name)

	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		l.t.Fatal(err)
	}
}

// plug installs a declarative plugin whose manifest holds the given tables and permissions.
//
// Parameters:
//   - id: plugin id
//   - perms: permission entries
//   - body: tables written after the header
func (l *lab) plug(id string, perms []string, body string) LockRecord {
	l.t.Helper()

	src := strings.Replace(decl("", perms, body), `id = "demo"`, `id = "`+id+`"`, 1)

	return l.install(id, map[string]string{"plugin.toml": src})
}

// reg opens a registry over the sandbox with an existing working directory.
//
// Parameters:
//   - mutate: changes the options before the registry is opened; may be nil
func (l *lab) reg(mutate func(*Options)) *Registry {
	l.t.Helper()

	cwd := filepath.Join(l.root, "cwd")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		l.t.Fatal(err)
	}

	return l.registry(func(o *Options) {
		o.Bin = "/usr/local/bin/tote"
		o.Cwd = cwd

		if mutate != nil {
			mutate(o)
		}
	})
}

// cwd returns the working directory of the sandbox.
func (l *lab) cwd() string { return filepath.Join(l.root, "cwd") }

// gitRepo creates a git repository below the sandbox with an origin remote, saves it under a
// name in repos.toml and returns its path.
//
// Parameters:
//   - name: saved name
//   - remote: url of origin, empty for a repository without remote
func (l *lab) gitRepo(name, remote string) string {
	l.t.Helper()

	dir := filepath.Join(l.root, "repos", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		l.t.Fatal(err)
	}

	l.git(dir, "init", "-q", "-b", "main")

	if remote != "" {
		l.git(dir, "remote", "add", "origin", remote)
	}

	l.saveRepo(name, dir)

	return dir
}

// saveRepo adds a repository to repos.toml.
//
// Parameters:
//   - name: saved name
//   - path: stored path
func (l *lab) saveRepo(name, path string) {
	l.t.Helper()

	col, err := store.OpenRepos(l.paths.ReposFile())
	if err != nil {
		l.t.Fatal(err)
	}

	if err := col.Add(store.Repo{Name: name, Path: path}); err != nil {
		l.t.Fatal(err)
	}
}

// git runs git for the test with an environment of its own, so that nothing of the machine's
// configuration is read or written.
//
// Parameters:
//   - dir: repository
//   - args: git arguments
func (l *lab) git(dir string, args ...string) {
	l.t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = []string{
		"PATH=" + shellPath, "HOME=" + l.home, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		l.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gftFiles reads the bundled gft fixture and its locales.
//
// Parameters:
//   - t: test to report to
func gftFiles(t *testing.T) map[string]string {
	t.Helper()

	files := map[string]string{"plugin.toml": string(readFixture(t, "gft/plugin.toml"))}

	for _, name := range []string{"en", "fr"} {
		files["locales/"+name+".toml"] = string(readFixture(t, "gft/locales/"+name+".toml"))
	}

	return files
}

// prepare runs Prepare and returns the prepared run, failing the test on an error.
//
// Parameters:
//   - t: test to report to
//   - r: registry
//   - call: the call
func prepare(t *testing.T, r *Registry, call Call) Prepared {
	t.Helper()

	p, err := r.Prepare(call)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	return p
}

// refusal runs Prepare and returns the error, failing the test when there is none.
//
// Parameters:
//   - t: test to report to
//   - r: registry
//   - call: the call
func refusal(t *testing.T, r *Registry, call Call) *Error {
	t.Helper()

	_, err := r.Prepare(call)
	if err == nil {
		t.Fatal("Prepare accepted the call")
	}

	pe, ok := err.(*Error)
	if !ok {
		t.Fatalf("error %T is not an *Error", err)
	}

	return pe
}

// envMap splits KEY=value entries into a map.
//
// Parameters:
//   - env: environment
func envMap(env []string) map[string]string {
	m := map[string]string{}

	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}

	return m
}

// waitProc waits until plugin-procs records a process group of the plugin. A program that wrote a
// file is not yet recorded: launch records the group after the start, so a test that stops or
// counts the processes of a plugin must wait for the record, not for the program.
//
// Parameters:
//   - t: test to report to
//   - r: registry that started the run
//   - id: plugin identifier
func waitProc(t *testing.T, r *Registry, id string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		procs, err := r.Procs()
		must(t, err)

		for _, e := range procs {
			if e.ID == id {
				return
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("plugin-procs never recorded a process of %s", id)
}

// waitGone reads a process id from a file and waits until that process no longer exists, failing
// the test after a few seconds. A process that is still there is killed so that the test leaves
// nothing behind.
//
// Parameters:
//   - t: test to report to
//   - file: file that holds the process id
func waitGone(t *testing.T, file string) {
	t.Helper()

	var pid int

	for i := 0; i < 100 && pid == 0; i++ {
		if data, err := os.ReadFile(file); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}

		if pid == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}

	if pid == 0 {
		t.Fatalf("%s holds no process id", file)
	}

	for i := 0; i < 100; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	_ = syscall.Kill(pid, syscall.SIGKILL)

	t.Errorf("process %d is still alive", pid)
}
