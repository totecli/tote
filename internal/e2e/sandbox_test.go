package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	// fakeSSH plays the part of ssh. Asked for its configuration with -G, it logs the arguments
	// in ssh.probe and prints the content of ssh.conf, or a ServerAliveInterval of 15 seconds
	// when the file is missing, so that the interface adds nothing to the command. Otherwise it
	// logs its process id and arguments, prints a token that the tests can wait for, and then
	// behaves according to the host: "fail" exits with 255, "hang" waits for an interrupt and
	// "slow" is the default, a short pause and a clean exit.
	fakeSSH = `#!/bin/sh
case " $* " in
*" -G "*)
    echo "$*" >> "$E2E_DIR/ssh.probe"
    if [ -f "$E2E_DIR/ssh.conf" ]; then cat "$E2E_DIR/ssh.conf"
    else printf 'serveraliveinterval 15\nserveralivecountmax 3\n'; fi
    exit 0
    ;;
esac
echo "$$ $*" >> "$E2E_DIR/ssh.log"
echo "fake-ssh-token $$"
[ -n "$E2E_AFFINITY" ] && taskset -cp $$ >> "$E2E_DIR/ssh.aff"
case "$*" in
*fail*)
    sleep "${FAKE_SSH_PAUSE:-0.2}"
    exit 255
    ;;
*hang*)
    sleep 30 &
    sp=$!
    trap 'kill $sp 2>/dev/null; echo "$$" >> "$E2E_DIR/ssh.int"; exit 130' INT
    echo "$$" > "$E2E_DIR/ssh.pid"
    wait $sp
    exit 0
    ;;
esac
sleep "${FAKE_SSH_PAUSE:-0.2}"
exit 0
`

	// fakeEditor plays the part of the editor: it prints the file it was given, logs it and
	// exits at once.
	fakeEditor = `#!/bin/sh
echo "$*" >> "$E2E_DIR/editor.log"
echo "fake-editor-token $$ $*"
sleep "${FAKE_SSH_PAUSE:-0.2}"
exit 0
`

	// connections is the data of the connections file of the sandbox.
	connections = `schema = 1

[[connection]]
name = "alpha"
target = "alpha.example.com"
user = "deploy"
desc = "First host"

[[connection]]
name = "bravo"
target = "bravo.example.com"
user = "deploy"

[[connection]]
name = "fail"
target = "fail.example.com"
user = "deploy"

[[connection]]
name = "hang"
target = "hang.example.com"
user = "deploy"
`

	// aliases is the data of the aliases file: the command writes a value computed by the
	// shell, so that it proves that the command line was run and not only echoed.
	aliases = `schema = 1

[[alias]]
name = "hi"
cmd = 'echo inserted-$((6*7)) > "$E2E_DIR/alias.out"'
desc = "Say hi"
`
)

// sandbox is a throw-away home, configuration and data tree with a fake ssh and a fake editor.
//
// Dir is the root of everything, Home the home directory, Out the directory where the programs
// and the wrapper leave their traces, Bin the directory of the fake programs and Real the
// directory of the binary under test.
type sandbox struct {
	Dir  string
	Home string
	Out  string
	Bin  string
	Real string
}

// newSandbox writes the data files and the fake programs below a temporary directory. Nothing
// outside the directory of the test is read or written.
//
// Parameters:
//   - t: running test
func newSandbox(t *testing.T) *sandbox {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	sb := &sandbox{
		Dir:  dir,
		Home: filepath.Join(dir, "home"),
		Out:  filepath.Join(dir, "out"),
		Bin:  filepath.Join(dir, "fakebin"),
		Real: toteBin(t),
	}

	for _, d := range []string{
		sb.Home, sb.Out, sb.Bin, filepath.Join(sb.Home, "code", "proj"),
		filepath.Join(sb.Home, "code", "docs"),
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.MkdirAll(filepath.Join(dir, "run"), 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := filepath.Join(dir, "config")

	sb.write(t, filepath.Join(cfg, "connections.toml"), connections)
	sb.write(t, filepath.Join(cfg, "aliases.toml"), aliases)
	sb.write(t, filepath.Join(cfg, "repos.toml"), "schema = 1\n\n[[repo]]\nname = \"proj\"\n"+
		"path = \""+filepath.Join(sb.Home, "code", "proj")+"\"\ndesc = \"The project\"\n\n"+
		"[[repo]]\nname = \"docs\"\npath = \""+filepath.Join(sb.Home, "code", "docs")+"\"\n")
	sb.write(t, filepath.Join(cfg, "funcs.toml"), "schema = 1\n")

	for name, body := range map[string]string{"ssh": fakeSSH, "editor": fakeEditor} {
		sb.write(t, filepath.Join(sb.Bin, name), body)

		if err := os.Chmod(filepath.Join(sb.Bin, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	return sb
}

// write creates a file and its directory.
//
// Parameters:
//   - t: running test
//   - path: file to create
//   - content: text to store
func (sb *sandbox) write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Env returns the environment of the program under test: a clean one, whose home, directories
// and PATH point into the sandbox.
//
// Parameters:
//   - extra: variables added last, so that they win
func (sb *sandbox) Env(extra ...string) []string {
	env := []string{
		"PATH=" + sb.Bin + ":" + sb.Real + ":/usr/bin:/bin",
		"HOME=" + sb.Home,
		"USER=tester",
		"TERM=screen-256color",
		"LANG=en_US.UTF-8",
		"TOTE_LANG=en",
		"TOTE_COLOR=none",
		"TOTE_CONFIG_DIR=" + filepath.Join(sb.Dir, "config"),
		"TOTE_DATA_DIR=" + filepath.Join(sb.Dir, "data"),
		"TOTE_STATE_DIR=" + filepath.Join(sb.Dir, "state"),
		"TOTE_CACHE_DIR=" + filepath.Join(sb.Dir, "cache"),
		"XDG_RUNTIME_DIR=" + filepath.Join(sb.Dir, "run"),
		"E2E_DIR=" + sb.Out,
		"EDITOR=" + filepath.Join(sb.Bin, "editor"),
	}

	return append(env, extra...)
}

// tote returns the path of the binary under test.
func (sb *sandbox) tote() string {
	return filepath.Join(sb.Real, "tote")
}
