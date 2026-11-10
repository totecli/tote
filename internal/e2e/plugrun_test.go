package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeGft plays the part of gft: it records its directory and arguments, one line per run, prints
// them, and acts on a word of its arguments: "many" writes three hundred lines, "hang" waits to be
// stopped, "fail" exits with status 3 and "color" writes colors and a few sequences that a terminal
// must never be sent. The word OUT stands for the output directory of the sandbox.
const fakeGft = `#!/bin/sh
echo "$PWD|$*" >> "OUT/gft.log"
echo "gft-out: $*"
case "$*" in
*many*) i=0; while [ $i -lt 300 ]; do echo "row $i"; i=$((i+1)); done;;
*hang*) echo "hang" > "OUT/gft.hang"; exec sleep 30;;
*fail*) echo "boom" >&2; exit 3;;
*color*) printf 'plain \033[31mred\033[0m\n\033]0;owned\007\033[2J\033[10;10Hstill text\n';;
*pause*) sleep 0.2;;
esac
exit 0
`

// fakeOpener plays the part of the program that opens an address: it records its argument.
const fakeOpener = `#!/bin/sh
echo "$*" >> "OUT/open.log"
exit 0
`

// gftSandbox returns a sandbox where the plugin gft is installed with the command line, the fake
// gft and opener are first on the PATH, and the repository proj has a GitHub remote.
//
// Parameters:
//   - t: running test
func gftSandbox(t *testing.T) *sandbox {
	t.Helper()

	sb := newSandbox(t)
	t.Cleanup(func() { openUp(sb.Dir) })

	for name, body := range map[string]string{
		"gft": fakeGft, "xdg-open": fakeOpener,
		"open": fakeOpener,
	} {
		path := filepath.Join(sb.Bin, name)
		sb.write(t, path, strings.ReplaceAll(body, "OUT", sb.Out))

		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	proj := filepath.Join(sb.Home, "code", "proj")

	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "git@github.com:demo/proj.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = proj
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + sb.Home, "GIT_CONFIG_NOSYSTEM=1"}

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git is not usable here: %v\n%s", err, out)
		}
	}

	out, errs, rc := sb.run(t, "plugin", "check", "gft")
	if rc != 0 {
		t.Fatalf("check gft: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	token := ""

	for _, l := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(l, "grant token: "); ok {
			token = strings.TrimSpace(rest)
		}
	}

	if token == "" {
		t.Fatalf("no grant token in %q", out)
	}

	if out, errs, rc = sb.run(t, "plugin", "install", "gft", "--grant", token); rc != 0 {
		t.Fatalf("install gft: exit %d, stdout %q, stderr %q", rc, out, errs)
	}

	return sb
}

// toRepos goes from the welcome page to the list of the repositories.
func (m *term) toRepos() {
	m.t.Helper()
	m.waitText("Welcome")
	m.typeText("2")
	m.waitText("The project")
}

// openDrop opens the menu of the actions of proj and the form of "Drop a stash".
func (m *term) openDrop() {
	m.t.Helper()
	m.waitText("proj")
	m.key("Tab")
	m.waitText("Actions: proj")
	m.waitText("Drop a stash")
	m.key("Down", "Down", "Down", "Down", "Down", "Enter")
	m.waitText("Stash number")
}

// TestRecordActionBinary drives a record action of the real binary with the fake gft: Tab on the
// repository, "Drop a stash", the number, the question with the exact command, the cancel paths,
// the run, the output and the end on the status line.
//
// Parameters:
//   - t: test to report to
func TestRecordActionBinary(t *testing.T) {
	t.Parallel()

	sb := gftSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.waitText("proj")
	tm.key("Tab")
	tm.waitText("Actions: proj")
	tm.waitText("Show the latest release")
	tm.waitText("Drop a stash")
	tm.key("Escape")
	tm.waitGone("Actions: proj")

	// A greyed action does not start anything and says why.
	tm.openDrop()
	tm.key("Escape")
	tm.waitGone("Stash number")
	tm.waitText("proj")

	// The question comes with the exact command, and each way out starts nothing.
	tm.openDrop()
	tm.typeText("3")
	tm.key("Enter")
	tm.waitText("Drop stash 3 of proj?")
	tm.waitText("gft stash drop 3 --yes")
	tm.waitText("Cancel")
	tm.key("Enter")
	tm.waitGone("Drop stash 3 of proj?")

	tm.openDrop()
	tm.typeText("3")
	tm.key("Enter")
	tm.waitText("Drop stash 3 of proj?")
	tm.key("y")
	tm.waitText("Drop stash 3 of proj?")
	tm.key("Escape")
	tm.waitGone("Drop stash 3 of proj?")

	if _, err := os.Stat(filepath.Join(sb.Out, "gft.log")); err == nil {
		t.Fatalf("a program ran although every question was answered no: %s",
			readFile(t, filepath.Join(sb.Out, "gft.log")))
	}

	// The same way, answered yes.
	tm.openDrop()
	tm.typeText("3")
	tm.key("Enter")
	tm.waitText("Drop stash 3 of proj?")
	tm.key("Tab", "Enter")
	tm.waitText("gft-out: stash drop 3 --yes")
	tm.waitText("plugin gft: done")

	log := tm.waitFile("gft.log", func(c string) bool { return strings.Contains(c, "stash drop") })
	want := filepath.Join(sb.Home, "code", "proj") + "|stash drop 3 --yes"
	if strings.TrimSpace(log) != want {
		t.Errorf("gft ran as %q, want %q", strings.TrimSpace(log), want)
	}

	tm.key("Escape")
	tm.waitGone("gft-out")
	tm.waitText("proj")
	tm.quit()
}

// TestPaletteCommandBinary drives a command of a plugin from the palette: found by a keyword, a
// form with a default and a choice, the output in the pager with scrolling, a resize while the
// pager is open, and Esc.
//
// Parameters:
//   - t: test to report to
func TestPaletteCommandBinary(t *testing.T) {
	t.Parallel()

	sb := gftSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.waitText("proj")
	tm.key("C-k")
	tm.waitText("Palette")
	tm.typeText("assets")
	tm.waitText("Show a GitHub release")
	tm.waitText("gft")
	tm.key("Enter")
	tm.waitText("Repository")
	tm.typeText("demo/proj")
	tm.key("Tab")
	tm.typeText("many1")
	tm.key("Tab", "Enter")

	tm.waitText("plugin gft: done")
	tm.waitText("gft-out: demo/proj many1")
	tm.waitText("more")

	tm.key("PageDown")
	tm.waitGone("gft-out: demo/proj many1")
	tm.key("End")
	tm.waitText("row 299")

	tm.resize(24, 7)
	tm.waitText("row 299")
	tm.resize(40, 12)
	tm.waitText("row 299")
	tm.resize(100, 30)
	tm.waitText("row 299")

	tm.key("Escape")
	tm.waitGone("row 299")
	tm.waitText("The project")
	tm.quit()
}

// TestRunFailureAndCancelBinary checks a program that fails, with its output and status, and a
// program that is stopped with Esc.
//
// Parameters:
//   - t: test to report to
func TestRunFailureAndCancelBinary(t *testing.T) {
	t.Parallel()

	sb := gftSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	run := func(tag string) {
		tm.waitText("The project")
		tm.key("C-k")
		tm.waitText("Palette")
		tm.typeText("show a github")
		tm.key("Enter")
		tm.waitText("Repository")
		tm.typeText("demo/proj")
		tm.key("Tab")
		tm.typeText(tag)
		tm.key("Tab", "Enter")
	}

	run("fail1")
	tm.waitText("boom")
	tm.waitText("plugin gft: failed, exit status 3")
	tm.key("Escape")
	tm.waitGone("boom")
	tm.waitText("The project")

	run("hang1")
	tm.waitText("gft-out: demo/proj hang1")
	tm.waitFile("gft.hang", func(c string) bool { return c != "" })
	tm.waitText("esc cancel")
	tm.key("Escape")
	tm.waitText("plugin gft: stopped")
	tm.waitText("esc close")
	tm.key("Escape")
	tm.waitGone("gft-out: demo/proj hang1")
	tm.quit()

	// The program ran in the directory of the sandbox: none may be left there.
	if out, err := exec.Command("pgrep", "-x", "sleep").Output(); err == nil {
		for _, pid := range strings.Fields(string(out)) {
			args, _ := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
			cwd, err := os.Readlink(filepath.Join("/proc", pid, "cwd"))

			if err == nil && string(args) == "sleep\x0030\x00" && strings.HasPrefix(cwd, sb.Dir) {
				t.Errorf("the program of the plugin outlived tote: pid %s", pid)
			}
		}
	}
}

// TestRunHostileOutputBinary checks that a program that writes control sequences cannot move the
// cursor, change the title or clear the screen of the real terminal: the escape is shown as a
// question mark and the colors survive.
//
// Parameters:
//   - t: test to report to
func TestRunHostileOutputBinary(t *testing.T) {
	t.Parallel()

	sb := gftSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.waitText("proj")
	tm.key("C-k")
	tm.waitText("Palette")
	tm.typeText("show a github")
	tm.key("Enter")
	tm.waitText("Repository")
	tm.typeText("demo/proj")
	tm.key("Tab")
	tm.typeText("color1")
	tm.key("Tab", "Enter")

	tm.waitText("plugin gft: done")

	s := tm.look()
	if !s.has("plain red") || !s.has("still text") || !s.has("Show a GitHub release") {
		t.Errorf("the output was damaged:\n%s", s.Text)
	}

	tm.key("Escape")
	tm.waitGone("gft-out: demo/proj color1")
	tm.quit()
	tm.restored()
}

// TestTerminalCommandBinary checks a command that owns the terminal: its output goes to the real
// terminal, tote waits for Enter after it and shows the end on the status line.
//
// Parameters:
//   - t: test to report to
func TestTerminalCommandBinary(t *testing.T) {
	t.Parallel()

	sb := gftSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.waitText("proj")
	tm.key("C-k")
	tm.waitText("Palette")
	tm.typeText("download a release")
	tm.key("Enter")
	tm.waitText("Repository")
	tm.typeText("demo/proj")
	tm.key("Tab", "Tab")
	tm.typeText("pause")
	tm.key("Tab", "Enter")

	tm.waitText("gft-out: demo/proj --get pause")
	tm.waitText("press Enter")
	tm.key("Enter")
	tm.waitGone("gft-out")
	tm.waitText("plugin gft: done")
	tm.waitText("proj")
	tm.quit()
}

// TestPauseTerminalGoneBinary checks that tote does not spin for ever when the terminal goes away
// while it waits for Enter after a program of a plugin: it must end by itself.
//
// Parameters:
//   - t: test to report to
func TestPauseTerminalGoneBinary(t *testing.T) {
	t.Parallel()

	sb := gftSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.waitText("proj")
	tm.key("C-k")
	tm.waitText("Palette")
	tm.typeText("download a release")
	tm.key("Enter")
	tm.waitText("Repository")
	tm.typeText("demo/proj")
	tm.key("Tab", "Tab")
	tm.typeText("pause")
	tm.key("Tab", "Enter")
	tm.waitText("press Enter")

	pid := toteChild(tm)

	if err := tm.tmux("kill-server"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)

	for {
		if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err != nil {
			return
		}

		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("tote (pid %d) still runs 15 s after its terminal went away", pid)
		}

		time.Sleep(pollEvery)
	}
}

// TestOpenAddressBinary checks a command that opens an address: the question shows it, Esc opens
// nothing, Run calls the opener with the address.
//
// Parameters:
//   - t: test to report to
func TestOpenAddressBinary(t *testing.T) {
	t.Parallel()

	sb := gftSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	open := func() {
		tm.waitText("proj")
		tm.key("C-k")
		tm.waitText("Palette")
		tm.typeText("releases page")
		tm.key("Enter")
		tm.waitText("Repository")
		tm.typeText("demo/proj")
		tm.key("Enter")
		tm.waitText("https://github.com/demo/proj/releases")
	}

	open()
	tm.key("Escape")
	tm.waitGone("https://github.com/demo/proj/releases")

	if _, err := os.Stat(filepath.Join(sb.Out, "open.log")); err == nil {
		t.Fatal("the address was opened although the question was cancelled")
	}

	open()
	tm.key("Tab", "Enter")
	tm.waitFile("open.log", func(c string) bool {
		return strings.TrimSpace(c) == "https://github.com/demo/proj/releases"
	})
	tm.waitText("address opened")
	tm.quit()
}

// TestRunResizeBinary resizes the terminal while the menu, the form and the question are open, down
// to the smallest terminal and back.
//
// Parameters:
//   - t: test to report to
func TestRunResizeBinary(t *testing.T) {
	t.Parallel()

	sb := gftSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 100, H: 30}, "tote")
	tm.toRepos()

	tm.waitText("proj")
	tm.key("Tab")
	tm.waitText("Actions: proj")
	tm.resize(24, 7)
	tm.waitText("Actions")
	tm.resize(100, 30)
	tm.waitText("Drop a stash")
	tm.key("Down", "Down", "Down", "Down", "Down", "Enter")
	tm.waitText("Stash number")
	tm.resize(24, 7)
	tm.waitText("Stash")
	tm.resize(100, 30)
	tm.waitText("Stash number")
	tm.typeText("7")
	tm.key("Enter")
	tm.waitText("Drop stash 7 of proj?")
	tm.resize(24, 7)
	tm.waitText("Cancel")
	tm.waitText("Run")
	tm.resize(100, 30)
	tm.waitText("gft stash drop 7 --yes")
	tm.key("Escape")
	tm.waitGone("Drop stash 7 of proj?")
	tm.quit()
	tm.restored()
}
