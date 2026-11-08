package plugin

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/totecli/tote/internal/config"
)

// TestParseProcs checks the reading of plugin-procs: a line that does not parse is dropped and
// the rest is kept.
//
// Parameters:
//   - t: test to report to
func TestParseProcs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []ProcEntry
	}{
		{"empty", "", nil},
		{"one", "demo 4242 99\n", []ProcEntry{{"demo", 4242, "99"}}},
		{"unknown token", "demo 4242 -\n", []ProcEntry{{"demo", 4242, "-"}}},
		{"no final newline", "demo 4242 99", []ProcEntry{{"demo", 4242, "99"}}},
		{"two", "ab 5 1\ncd 6 2\n", []ProcEntry{{"ab", 5, "1"}, {"cd", 6, "2"}}},
		{"too few fields", "demo 4242\n", nil},
		{"one more field", "demo 4242 99 x\n", []ProcEntry{{"demo", 4242, "99"}}},
		{
			"two more fields", "demo 4242 99 x y\nab 5 1\n",
			[]ProcEntry{{"demo", 4242, "99"}, {"ab", 5, "1"}},
		},
		{"extra field and bad token", "demo 4242 9x y\n", nil},
		{"bad id", "../x 4242 99\nDemo 4242 99\n", nil},
		{"sid zero", "demo 0 99\n", nil},
		{"sid one", "demo 1 99\n", nil},
		{"sid negative", "demo -5 99\n", nil},
		{"sid not a number", "demo 12a 99\n", nil},
		{"sid overflow", "demo 99999999999999999999 99\n", nil},
		{"token not digits", "demo 4242 9x\n", nil},
		{"token too long", "demo 4242 " + strings.Repeat("9", 21) + "\n", nil},
		{"junk between", "x\ndemo 4242 99\n\x00\x01\n", []ProcEntry{{"demo", 4242, "99"}}},
		{"blank lines", "\n\n  \ndemo 4242 99\n", []ProcEntry{{"demo", 4242, "99"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseProcs(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}

	t.Run("the count is bounded", func(t *testing.T) {
		got := parseProcs(strings.Repeat("demo 4242 99\n", 10*maxProcsEntries))
		if len(got) != maxProcsEntries {
			t.Errorf("%d entries", len(got))
		}
	})
}

// TestSafeSID checks the ids that must never be signalled.
//
// Parameters:
//   - t: test to report to
func TestSafeSID(t *testing.T) {
	own, _ := getsid(0)

	for _, id := range []int{-1, 0, 1, os.Getpid(), syscall.Getpgrp(), own} {
		if safeSID(id) {
			t.Errorf("%d is safe", id)
		}

		if err := killGroup(id, syscall.Signal(0)); err == nil {
			t.Errorf("killGroup accepted %d", id)
		}
	}
}

// sleeper starts a program in a session of its own, as a plugin process is, and reaps it in the
// background. The caller gets its pid.
//
// Parameters:
//   - t: test to report to
func sleeper(t *testing.T) int {
	t.Helper()

	path, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep program: %v", err)
	}

	cmd := exec.Command(path, "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = cmd.Process.Kill() })

	go func() { _ = cmd.Wait() }()

	return cmd.Process.Pid
}

// dead reports whether a process is gone, giving it a moment.
//
// Parameters:
//   - pid: process id
func dead(pid int) bool {
	for i := 0; i < 100; i++ {
		if syscall.Kill(pid, 0) != nil {
			return true
		}

		time.Sleep(20 * time.Millisecond)
	}

	return false
}

// writeProcs writes plugin-procs of the sandbox, the way the host does: private to the user.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
//   - text: content
func writeProcs(t *testing.T, l *lab, text string) {
	t.Helper()

	path := l.paths.ProcsFile()

	must(t, os.MkdirAll(filepath.Dir(path), 0o700))
	must(t, os.WriteFile(path, []byte(text), 0o600))
}

// TestProcsFileMode checks that the file the host creates is private.
//
// Parameters:
//   - t: test to report to
func TestProcsFileMode(t *testing.T) {
	l, r := runSetup(t)

	l.script("tool", "echo hi\n")

	if out, _ := runOnce(t, r, "pg", context.Background()); out.Status != OutcomeOK {
		t.Fatalf("outcome %+v", out)
	}

	info, err := os.Lstat(l.paths.ProcsFile())
	must(t, err)

	if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
		t.Errorf("mode %v", info.Mode())
	}

	if procs, err := r.Procs(); err != nil || len(procs) != 0 {
		t.Errorf("a finished run left %v, %v", procs, err)
	}
}

// TestProcsFileUnsafe checks that the host does not start anything when plugin-procs cannot be
// trusted: the program would run out of reach of the kill switch.
//
// Parameters:
//   - t: test to report to
func TestProcsFileUnsafe(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, l *lab)
	}{
		{"symlink", func(t *testing.T, l *lab) {
			target := filepath.Join(l.root, "elsewhere")
			must(t, os.WriteFile(target, nil, 0o600))
			must(t, os.MkdirAll(l.paths.State, 0o700))
			must(t, os.Symlink(target, l.paths.ProcsFile()))
		}},
		{"dangling symlink", func(t *testing.T, l *lab) {
			must(t, os.MkdirAll(l.paths.State, 0o700))
			must(t, os.Symlink(filepath.Join(l.root, "missing"), l.paths.ProcsFile()))
		}},
		{"group readable", func(t *testing.T, l *lab) {
			writeProcs(t, l, "")
			must(t, os.Chmod(l.paths.ProcsFile(), 0o640))
		}},
		{"world writable", func(t *testing.T, l *lab) {
			writeProcs(t, l, "")
			must(t, os.Chmod(l.paths.ProcsFile(), 0o666))
		}},
		{"a directory", func(t *testing.T, l *lab) {
			must(t, os.MkdirAll(l.paths.ProcsFile(), 0o700))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			l.useSystemTools()

			canary := filepath.Join(l.root, "canary")
			l.script("tool", "touch "+canary+"\n")
			l.plug("demo", []string{"exec:tool"}, runBody)

			tt.setup(t, l)

			r := l.reg(nil)
			out, _ := runOnce(t, r, "pg", context.Background())

			if out.Status != OutcomeNotStarted || out.Err == nil || out.Err.Code != CodeProcsFile {
				t.Fatalf("outcome %+v", out)
			}

			time.Sleep(100 * time.Millisecond)

			if exists(canary) {
				t.Error("the program started")
			}

			if _, err := r.Procs(); err == nil {
				t.Error("Procs accepted the file")
			}

			if _, err := r.StopPlugin("demo", time.Millisecond); err == nil {
				t.Error("StopPlugin accepted the file")
			}
		})
	}
}

// TestProcsTampered checks that a plugin-procs which names the host itself, the init process or
// a process of another plugin never makes the host signal them.
//
// Parameters:
//   - t: test to report to
func TestProcsTampered(t *testing.T) {
	l, r := runSetup(t)

	own, _ := getsid(0)
	other := sleeper(t)
	victim := sleeper(t)

	text := strings.Join([]string{
		"demo 1 -",
		"demo " + strconv.Itoa(os.Getpid()) + " " + startToken(os.Getpid()),
		"demo " + strconv.Itoa(syscall.Getpgrp()) + " -",
		"demo " + strconv.Itoa(own) + " -",
		"demo -1 -",
		"demo 0 -",
		"other " + strconv.Itoa(other) + " " + startToken(other),
	}, "\n") + "\n"

	writeProcs(t, l, text)

	n, err := r.StopPlugin("demo", 50*time.Millisecond)
	must(t, err)

	if n != 0 {
		t.Errorf("StopPlugin signalled %d sessions of a tampered file", n)
	}

	time.Sleep(100 * time.Millisecond)

	if syscall.Kill(other, 0) != nil {
		t.Error("the process of another plugin was killed")
	}

	if syscall.Kill(victim, 0) != nil {
		t.Error("an unrelated process was killed")
	}

	procs, err := r.Procs()
	must(t, err)

	for _, e := range procs {
		if e.ID == "demo" {
			t.Errorf("entry left for the plugin: %v", e)
		}
	}
}

// TestProcsStartToken checks that an entry whose start token does not match is not signalled:
// the id was reused by an unrelated process.
//
// Parameters:
//   - t: test to report to
func TestProcsStartToken(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("start tokens need /proc")
	}

	l, r := runSetup(t)

	pid := sleeper(t)
	real := startToken(pid)

	if real == "-" {
		t.Fatal("no start token")
	}

	wrong := "1" + real

	writeProcs(t, l, "demo "+strconv.Itoa(pid)+" "+wrong+"\n")

	n, err := r.StopPlugin("demo", 50*time.Millisecond)
	must(t, err)

	time.Sleep(100 * time.Millisecond)

	if n != 0 || syscall.Kill(pid, 0) != nil {
		t.Fatalf("signalled %d, alive %v: a process with another start time was killed", n,
			syscall.Kill(pid, 0) == nil)
	}

	writeProcs(t, l, "demo "+strconv.Itoa(pid)+" "+real+"\n")

	n, err = r.StopPlugin("demo", 200*time.Millisecond)
	must(t, err)

	if n != 1 || !dead(pid) {
		t.Errorf("signalled %d, the matching process is still there", n)
	}
}

// TestStopPluginOnlyThatPlugin checks that stopping one plugin leaves the processes of the others
// and their entries alone.
//
// Parameters:
//   - t: test to report to
func TestStopPluginOnlyThatPlugin(t *testing.T) {
	l, r := runSetup(t)

	mine := sleeper(t)
	theirs := sleeper(t)

	writeProcs(t, l, "demo "+strconv.Itoa(mine)+" "+startToken(mine)+"\n"+
		"other "+strconv.Itoa(theirs)+" "+startToken(theirs)+"\n")

	n, err := r.StopPlugin("demo", 200*time.Millisecond)
	must(t, err)

	if n != 1 || !dead(mine) {
		t.Errorf("signalled %d, own process alive %v", n, syscall.Kill(mine, 0) == nil)
	}

	if syscall.Kill(theirs, 0) != nil {
		t.Error("the other plugin was stopped")
	}

	procs, err := r.Procs()
	must(t, err)

	if len(procs) != 1 || procs[0].ID != "other" {
		t.Errorf("entries %v", procs)
	}

	if n, err := r.StopPlugin("demo", time.Millisecond); err != nil || n != 0 {
		t.Errorf("second stop: %d, %v", n, err)
	}
}

// TestProcsPruned checks that the entries of sessions that ended are dropped when the file is
// next written.
//
// Parameters:
//   - t: test to report to
func TestProcsPruned(t *testing.T) {
	l, r := runSetup(t)

	path, lerr := exec.LookPath("true")
	if lerr != nil {
		t.Skipf("no true program: %v", lerr)
	}

	cmd := exec.Command(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	must(t, cmd.Start())

	pid := cmd.Process.Pid

	must(t, cmd.Wait())

	writeProcs(t, l, "demo "+strconv.Itoa(pid)+" -\n")

	procs, err := r.Procs()
	must(t, err)

	if len(procs) != 0 {
		t.Errorf("entries %v", procs)
	}

	data, err := os.ReadFile(l.paths.ProcsFile())
	must(t, err)

	if len(data) != 0 {
		t.Errorf("file %q", data)
	}
}

// TestInPluginSession checks the guard that tells tote it was started by a plugin: true while the
// session is recorded, false for a session that is not, and false without a file.
//
// Parameters:
//   - t: test to report to
func TestInPluginSession(t *testing.T) {
	l := newLab(t)
	own, _ := getsid(0)

	if InPluginSession(l.paths) {
		t.Error("true without a file")
	}

	writeProcs(t, l, "demo "+strconv.Itoa(own+12345)+" -\n")

	if InPluginSession(l.paths) {
		t.Error("true for another session")
	}

	writeProcs(t, l, "garbage\n\x00\n")

	if InPluginSession(l.paths) {
		t.Error("true for a damaged file")
	}

	if runtime.GOOS == "linux" {
		writeProcs(t, l, "demo "+strconv.Itoa(own)+" -\n")

		if !InPluginSession(l.paths) {
			t.Error("false for the session of the caller")
		}

		writeProcs(t, l, "demo "+strconv.Itoa(own)+" 1\n")

		// With the leader gone (a detached runner) the members of the session still count, as
		// they must, so the token can only be compared while the leader is alive.
		_, leaderErr := os.Stat("/proc/" + strconv.Itoa(own) + "/stat")

		if own > 1 && leaderErr == nil && InPluginSession(l.paths) {
			t.Error("true although the start token does not match")
		}
	}

	if err := os.Truncate(l.paths.ProcsFile(), 0); err != nil {
		t.Fatal(err)
	}

	if InPluginSession(l.paths) {
		t.Error("true for an empty file")
	}
}

// TestInPluginSessionFromPlugin runs the test binary itself as a plugin and checks, from inside,
// that the guard sees the session and does not need the environment.
//
// Parameters:
//   - t: test to report to
func TestInPluginSessionFromPlugin(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the session check needs /proc")
	}

	l, r := runSetup(t)
	out := filepath.Join(l.root, "insession")

	l.script("tool", "exec "+os.Args[0]+" -test.run='^TestInSessionChild$' -- "+l.paths.State+" "+
		out+"\n")

	if o, _ := runOnce(t, r, "pg", context.Background()); o.Status != OutcomeOK {
		t.Fatalf("outcome %+v", o)
	}

	data, err := os.ReadFile(out)
	must(t, err)

	if string(data) != "true" {
		t.Errorf("InPluginSession from the plugin: %q", data)
	}

	if InPluginSession(config.Paths{State: l.paths.State}) {
		t.Error("the test process itself is not in a plugin session")
	}
}

// TestInSessionChild is the plugin side of TestInPluginSessionFromPlugin. It does nothing unless
// it is started with the state directory and the output file after "--".
//
// Parameters:
//   - t: test to report to
func TestInSessionChild(t *testing.T) {
	args := flag.Args()
	if len(args) != 2 {
		t.Skip("only runs as a plugin")
	}

	got := "false"
	if InPluginSession(config.Paths{State: args[0]}) {
		got = "true"
	}

	must(t, os.WriteFile(args[1], []byte(got), 0o600))
}

// TestRunTracksAndForgets checks the life of an entry: recorded while the program runs, gone once
// it ended with nothing left in its session.
//
// Parameters:
//   - t: test to report to
func TestRunTracksAndForgets(t *testing.T) {
	l, r := runSetup(t)
	seen := filepath.Join(l.root, "seen")
	release := filepath.Join(l.root, "release")

	// The program waits for a file, not for a delay: a slow observer must still find the entry.
	l.script("tool", "echo started > "+seen+"\nwhile [ ! -e "+release+" ]; do sleep 0.05; done\n")

	// A failed assertion must not leave the program waiting for ever.
	t.Cleanup(func() { _ = os.WriteFile(release, []byte("x"), 0o600) })

	done := make(chan Outcome, 1)
	p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

	go func() { done <- r.Run(context.Background(), p, nil) }()

	waitFile(t, seen)
	waitProc(t, r, "demo")

	procs, err := r.Procs()
	must(t, err)

	if len(procs) != 1 || procs[0].ID != "demo" || !safeSID(procs[0].SID) {
		t.Errorf("while running: %v", procs)
	}

	must(t, os.WriteFile(release, []byte("x"), 0o600))

	if out := <-done; out.Status != OutcomeOK {
		t.Errorf("outcome %+v", out)
	}

	if procs, _ := r.Procs(); len(procs) != 0 {
		t.Errorf("after the run: %v", procs)
	}
}
