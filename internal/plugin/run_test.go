package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// runBody is the plugin of the run tests: the program "tool" in the pager and in silence, with a
// short timeout, with a confirmation, and in the terminal.
const runBody = `
[[command]]
id = "pg"
label = "Pg"
run = ["tool", "{arg?}"]
output = "pager"
[[command.field]]
id = "arg"
type = "text"
label = "A"

[[command]]
id = "sl"
label = "Sl"
run = ["tool"]
output = "silent"

[[command]]
id = "to"
label = "To"
run = ["tool"]
output = "pager"
timeout = "1s"

[[command]]
id = "cf"
label = "Cf"
run = ["tool"]
confirm = true
output = "pager"

[[command]]
id = "tm"
label = "Tm"
run = ["tool"]
output = "terminal"
`

// runSetup installs the run plugin with a fake program that does nothing.
//
// Parameters:
//   - t: test to report to
func runSetup(t *testing.T) (*lab, *Registry) {
	t.Helper()

	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")
	l.plug("demo", []string{"exec:tool"}, runBody)

	return l, l.reg(nil)
}

// events collects what a run emits. It takes no lock on purpose: Run promises one goroutine at a
// time, and the race detector checks the promise.
type events struct {
	list []Event
}

// add is the emit function.
//
// Parameters:
//   - e: event
func (c *events) add(e Event) { c.list = append(c.list, e) }

// text joins the output events.
func (c *events) text() string {
	var b strings.Builder

	for _, e := range c.list {
		if e.Type == EventOutput {
			b.WriteString(e.Text)
		}
	}

	return b.String()
}

// notices counts the notice events.
func (c *events) notices() int {
	n := 0

	for _, e := range c.list {
		if e.Type == EventNotice {
			n++
		}
	}

	return n
}

// runOnce prepares and runs one command of the plugin.
//
// Parameters:
//   - t: test to report to
//   - r: registry
//   - id: command id
//   - ctx: context of the run
func runOnce(t *testing.T, r *Registry, id string, ctx context.Context) (Outcome, *events) {
	t.Helper()

	p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: id})
	ev := &events{}

	return r.Run(ctx, p, ev.add), ev
}

// exists reports whether a file is there.
//
// Parameters:
//   - path: location
func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// TestRunPager checks a normal run: the output of both streams arrives in order on one pipe, and
// the outcome says how it ended.
//
// Parameters:
//   - t: test to report to
func TestRunPager(t *testing.T) {
	l, r := runSetup(t)

	t.Run("ok", func(t *testing.T) {
		l.script("tool", "echo out1\necho err1 >&2\necho out2\n")

		out, ev := runOnce(t, r, "pg", context.Background())

		if out.Status != OutcomeOK || out.Exit != 0 || out.ExitCode() != 0 || out.Err != nil {
			t.Errorf("outcome %+v", out)
		}

		if got := ev.text(); got != "out1\nerr1\nout2\n" {
			t.Errorf("output %q", got)
		}

		if out.Tail != "out1\nerr1\nout2\n" || out.Bytes != 15 {
			t.Errorf("tail %q bytes %d", out.Tail, out.Bytes)
		}
	})

	t.Run("failed", func(t *testing.T) {
		l.script("tool", "echo oops >&2\nexit 3\n")

		out, ev := runOnce(t, r, "pg", context.Background())

		if out.Status != OutcomeFailed || out.Exit != 3 || out.ExitCode() != ExitFailure ||
			ev.text() != "oops\n" {
			t.Errorf("outcome %+v output %q", out, ev.text())
		}
	})

	t.Run("killed by a signal", func(t *testing.T) {
		l.script("tool", "kill -SEGV $$\n")

		out, _ := runOnce(t, r, "pg", context.Background())

		if out.Status != OutcomeSignaled || out.Signal != "SEGV" || out.Exit != -1 {
			t.Errorf("outcome %+v", out)
		}
	})

	t.Run("silent keeps the tail of stderr and drops stdout", func(t *testing.T) {
		l.script("tool", "echo to-stdout\necho to-stderr >&2\nexit 1\n")

		out, ev := runOnce(t, r, "sl", context.Background())

		if len(ev.list) != 0 || out.Tail != "to-stderr\n" || out.Status != OutcomeFailed {
			t.Errorf("events %v outcome %+v", ev.list, out)
		}
	})

	t.Run("silent keeps only the last 4 KiB", func(t *testing.T) {
		l.script("tool", "i=0\nwhile [ $i -lt 2000 ]; do echo \"line number $i\" >&2; i=$((i+1)); done\n")

		out, _ := runOnce(t, r, "sl", context.Background())

		if len(out.Tail) > tailCap || !strings.HasSuffix(out.Tail, "line number 1999\n") ||
			strings.Contains(out.Tail, "line number 0\n") {
			t.Errorf("tail of %d bytes: %q", len(out.Tail), out.Tail[:20])
		}
	})
}

// TestRunEscapes checks that nothing a program prints can reach the pager as an escape: the
// colours of the whitelist stay, everything else is neutralised, in every string.
//
// Parameters:
//   - t: test to report to
func TestRunEscapes(t *testing.T) {
	l, r := runSetup(t)

	l.script("tool", `printf '\033]52;c;aGk=\007'
printf '\033[31mred\033[0m \033[5mblink\033[0m \033[41mbg\033[0m\n'
printf '\033P1$r\033\\\033[2J\033[?1049h\033c\n'
printf 'bell\007 nul-free \302\233 c1 \342\200\256 bidi\n'
printf 'cr\rhere\r\n'
`)

	out, ev := runOnce(t, r, "pg", context.Background())
	text := ev.text()

	if out.Status != OutcomeOK {
		t.Fatalf("outcome %+v", out)
	}

	assertSafeOutput(t, text)

	for _, bad := range []string{"\x1b]", "\x1bP", "\x1b[2J", "\x1b[5m", "\x1b[41m", "\x1bc", "\x07"} {
		if strings.Contains(text, bad) {
			t.Errorf("%q survived in %q", bad, text)
		}
	}

	if !strings.Contains(text, "\x1b[31mred") {
		t.Errorf("the whitelisted colour was lost: %q", text)
	}

	t.Run("mono keeps no colour", func(t *testing.T) {
		p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg", Mono: true})
		ev := &events{}

		r.Run(context.Background(), p, ev.add)

		if strings.Contains(ev.text(), "\x1b") {
			t.Errorf("escape in the mono theme: %q", ev.text())
		}
	})
}

// TestRunRedacts checks that a secret the program echoes is hidden: the exact value of a declared
// variable, and a token shape.
//
// Parameters:
//   - t: test to report to
func TestRunRedacts(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()
	l.env["MY_TOKEN"] = "correct-horse-battery"
	l.plug("demo", []string{"exec:tool", "env:MY_TOKEN"}, runBody)

	l.script("tool", `echo "token is $MY_TOKEN here"
echo "also ghp_abcdefghijklmnopqrstuvwxyz0123456789"
echo "stderr $MY_TOKEN" >&2
`)

	out, ev := runOnce(t, l.reg(nil), "pg", context.Background())

	got := ev.text()
	if strings.Contains(got, "correct-horse") || strings.Contains(got, "ghp_") ||
		strings.Count(got, Mask) != 3 {
		t.Errorf("output %q", got)
	}

	if strings.Contains(out.Tail, "correct-horse") {
		t.Errorf("tail %q", out.Tail)
	}
}

// TestRunBigOutput checks the output limits: a 70 KiB line comes out whole, and 10 MiB stops
// at 8 MiB with one notice while the program is let finish.
//
// Parameters:
//   - t: test to report to
func TestRunBigOutput(t *testing.T) {
	l, r := runSetup(t)

	t.Run("a 70 KiB line", func(t *testing.T) {
		l.script("tool", "head -c 71680 /dev/zero | tr '\\0' 'x'\necho\n")

		out, ev := runOnce(t, r, "pg", context.Background())

		if out.Status != OutcomeOK || ev.text() != strings.Repeat("x", 71680)+"\n" {
			t.Errorf("outcome %+v, %d bytes", out, len(ev.text()))
		}

		for _, e := range ev.list {
			if len(e.Text) > chunkMax+readChunk {
				t.Errorf("event of %d bytes", len(e.Text))
			}
		}
	})

	t.Run("10 MiB", func(t *testing.T) {
		l.script("tool", "yes | head -c 10485760\n")

		out, ev := runOnce(t, r, "pg", context.Background())

		if out.Status != OutcomeOK || !out.Truncated || ev.notices() != 1 || out.Bytes != 10<<20 {
			t.Errorf("outcome %+v notices %d", out, ev.notices())
		}

		if n := len(ev.text()); n < pagerCap || n > pagerCap+chunkMax+readChunk {
			t.Errorf("emitted %d bytes", n)
		}
	})

	t.Run("silent output is not capped in events", func(t *testing.T) {
		l.script("tool", "yes | head -c 1048576 >&2\n")

		out, ev := runOnce(t, r, "sl", context.Background())

		if len(ev.list) != 0 || out.Truncated || len(out.Tail) > tailCap {
			t.Errorf("outcome %+v events %d", out, len(ev.list))
		}
	})
}

// TestRunSlowReceiver checks that the end of the output is not lost when the receiver is slow
// after the program has exited: the pipe is closed after 250 ms of silence, not 250 ms after the
// exit.
//
// Parameters:
//   - t: test to report to
func TestRunSlowReceiver(t *testing.T) {
	l, r := runSetup(t)
	l.script("tool", "head -c 40000 /dev/zero | tr '\\0' 'x'\necho\n")

	p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})
	ev := &events{}
	slow := true

	out := r.Run(context.Background(), p, func(e Event) {
		ev.add(e)

		if slow {
			slow = false

			time.Sleep(700 * time.Millisecond)
		}
	})

	if out.Status != OutcomeOK || out.Bytes != 40001 || len(ev.text()) != 40001 {
		t.Errorf("outcome %+v, %d bytes of events", out, len(ev.text()))
	}
}

// TestRunPipeRule is the background-grandchild vector: a program that leaves a child holding the
// pipe ends at its own exit, the pipe is drained for 250 ms and closed, and the child is left
// alone. The kill switch then still reaches it through plugin-procs.
//
// Parameters:
//   - t: test to report to
func TestRunPipeRule(t *testing.T) {
	l, r := runSetup(t)
	pidFile := filepath.Join(l.root, "bg.pid")

	l.script("tool", "sleep 60 &\necho $! > "+pidFile+"\necho started\n")

	began := time.Now()
	out, ev := runOnce(t, r, "pg", context.Background())

	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("the run waited for the grandchild: %v", took)
	}

	if out.Status != OutcomeOK || ev.text() != "started\n" {
		t.Errorf("outcome %+v output %q", out, ev.text())
	}

	data, err := os.ReadFile(pidFile)
	must(t, err)

	pid := atoi(strings.TrimSpace(string(data)))
	if syscall.Kill(pid, 0) != nil {
		t.Fatal("the grandchild was killed at the end of the run")
	}

	procs, err := r.Procs()
	must(t, err)

	if len(procs) != 1 || procs[0].ID != "demo" {
		t.Fatalf("plugin-procs lost the session that is still alive: %v", procs)
	}

	n, err := r.StopPlugin("demo", 200*time.Millisecond)
	must(t, err)

	if n != 1 {
		t.Errorf("StopPlugin signalled %d sessions", n)
	}

	waitGone(t, pidFile)

	if procs, _ := r.Procs(); len(procs) != 0 {
		t.Errorf("entries left: %v", procs)
	}
}

// atoi reads a decimal number, 0 when it is not one.
//
// Parameters:
//   - s: text
func atoi(s string) int {
	n := 0

	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}

		n = n*10 + int(c-'0')
	}

	return n
}

// TestRunStops checks cancel and timeout: SIGTERM to the group, SIGKILL after two seconds, the
// grandchildren die with the group.
//
// Parameters:
//   - t: test to report to
func TestRunStops(t *testing.T) {
	l, r := runSetup(t)
	pidFile := filepath.Join(l.root, "gc.pid")

	t.Run("cancel kills the group", func(t *testing.T) {
		l.script("tool", "sleep 60 &\necho $! > "+pidFile+"\nwait\n")

		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(300*time.Millisecond, cancel)

		began := time.Now()
		out, _ := runOnce(t, r, "pg", ctx)

		if out.Status != OutcomeCancelled || out.ExitCode() != 130 || time.Since(began) > 3*time.Second {
			t.Errorf("outcome %+v after %v", out, time.Since(began))
		}

		waitGone(t, pidFile)
	})

	t.Run("a context that is already cancelled starts nothing", func(t *testing.T) {
		canary := filepath.Join(l.root, "canary")
		l.script("tool", "touch "+canary+"\n")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if out, _ := runOnce(t, r, "pg", ctx); out.Status != OutcomeCancelled {
			t.Errorf("outcome %+v", out)
		}

		time.Sleep(100 * time.Millisecond)

		if exists(canary) {
			t.Error("the program ran")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		l.script("tool", "sleep 60 &\necho $! > "+pidFile+"\nwait\n")

		began := time.Now()
		out, _ := runOnce(t, r, "to", context.Background())

		if out.Status != OutcomeTimeout || out.ExitCode() != ExitFailure {
			t.Errorf("outcome %+v", out)
		}

		if took := time.Since(began); took < time.Second || took > 4*time.Second {
			t.Errorf("took %v", took)
		}

		waitGone(t, pidFile)
	})

	t.Run("a program that ignores SIGTERM gets SIGKILL after two seconds", func(t *testing.T) {
		l.script("tool", "trap '' TERM\nsleep 60 &\necho $! > "+pidFile+"\nwait\nwait\nwait\n")

		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(300*time.Millisecond, cancel)

		began := time.Now()
		out, _ := runOnce(t, r, "pg", ctx)
		took := time.Since(began)

		if out.Status != OutcomeCancelled || took < 1800*time.Millisecond || took > 6*time.Second {
			t.Errorf("outcome %+v after %v", out, took)
		}

		waitGone(t, pidFile)
	})
}

// TestRunIsolation checks what the program is given: no stdin, no controlling terminal, a session
// of its own, and exactly the environment that was prepared.
//
// Parameters:
//   - t: test to report to
func TestRunIsolation(t *testing.T) {
	l, r := runSetup(t)

	for k, v := range hostileParent {
		l.env[k] = v
	}

	t.Run("no tty and an empty stdin", func(t *testing.T) {
		l.script("tool", `if (echo x > /dev/tty) 2>/dev/null; then echo TTY_OPEN; else echo NO_TTY; fi
[ -t 0 ] && echo STDIN_TTY
[ -t 1 ] && echo STDOUT_TTY
cat
echo stdin-closed
`)

		out, ev := runOnce(t, r, "pg", context.Background())

		if out.Status != OutcomeOK || ev.text() != "NO_TTY\nstdin-closed\n" {
			t.Errorf("outcome %+v output %q", out, ev.text())
		}
	})

	t.Run("own session", func(t *testing.T) {
		// The test binary reports the ids itself: ps has no session column on every system.
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}

		l.script("tool", "exec '"+exe+"' "+sessionProbeArg+"\n")

		_, ev := runOnce(t, r, "pg", context.Background())
		f := strings.Fields(ev.text())

		own, _ := getsid(0)
		if len(f) != 3 || f[0] != f[1] || f[0] != f[2] || atoi(f[0]) == own {
			t.Errorf("sid pgid pid = %v (ours %d)", f, own)
		}
	})

	t.Run("environment dump", func(t *testing.T) {
		l.script("tool", "env\n")

		p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})
		ev := &events{}

		r.Run(context.Background(), p, ev.add)

		got := envMap(strings.Split(strings.TrimSpace(ev.text()), "\n"))
		want := envMap(p.Env)

		// The shell adds its own working directory variables.
		for _, sh := range []string{"PWD", "OLDPWD", "SHLVL", "_"} {
			delete(got, sh)
		}

		if len(got) != len(want) {
			t.Errorf("program sees %v, prepared %v", keysOf(strings.Split(ev.text(), "\n")),
				keysOf(p.Env))
		}

		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s = %q want %q", k, got[k], v)
			}
		}

		for _, name := range []string{
			"SSH_AUTH_SOCK", "GITHUB_TOKEN", "XDG_RUNTIME_DIR", "LD_PRELOAD",
			"TOTE_HANDOFF", "GIT_DIR", "BASH_ENV",
		} {
			if _, ok := got[name]; ok {
				t.Errorf("%s reached the program", name)
			}
		}
	})

	t.Run("working directory", func(t *testing.T) {
		l.script("tool", "pwd\n")

		_, ev := runOnce(t, r, "pg", context.Background())
		// The shell prints the physical path: the temporary directory may sit behind a symbolic
		// link (/var is one on macOS).
		want, err := filepath.EvalSymlinks(l.cwd())
		if err != nil {
			t.Fatal(err)
		}

		if got := strings.TrimSpace(ev.text()); got != want {
			t.Errorf("pwd %q want %q", got, want)
		}
	})
}

// TestRunCrashes checks that three crashes in a row change nothing: a declarative run is never
// counted, the plugin stays enabled and nothing in the lock moves.
//
// Parameters:
//   - t: test to report to
func TestRunCrashes(t *testing.T) {
	l, r := runSetup(t)
	before, _ := l.lock().Get("demo")

	scripts := []string{"exit 139\n", "kill -SEGV $$\n", "exit 255\n"}
	statuses := []string{OutcomeFailed, OutcomeSignaled, OutcomeFailed}

	for i, s := range scripts {
		l.script("tool", s)

		out, _ := runOnce(t, r, "pg", context.Background())
		if out.Status != statuses[i] {
			t.Errorf("crash %d: %+v", i, out)
		}
	}

	after, _ := l.lock().Get("demo")
	if after.Failures != 0 || after.Quarantined || after.LastReason != "" ||
		strings.Join(after.Caps, ",") != strings.Join(before.Caps, ",") {
		t.Errorf("record changed: %+v", after)
	}

	if p, _ := r.Recheck("demo"); p.State != StateEnabled {
		t.Errorf("state %s", p.State)
	}
}

// TestRunNotStarted checks a program that cannot be executed.
//
// Parameters:
//   - t: test to report to
func TestRunNotStarted(t *testing.T) {
	l, r := runSetup(t)

	// No shebang line: the kernel refuses to run it.
	must(t, os.WriteFile(filepath.Join(l.bin, "tool"), []byte("echo hi\n"), 0o700))

	out, _ := runOnce(t, r, "pg", context.Background())

	if out.Status != OutcomeNotStarted || out.Err == nil || out.Err.Code != CodeStartFailed ||
		out.ExitCode() != ExitCannotStart {
		t.Errorf("outcome %+v", out)
	}
}

// TestRunRefusals checks the runs that Run refuses before they start: a confirmation that was not
// given, a terminal run, and a value that was not prepared.
//
// Parameters:
//   - t: test to report to
func TestRunRefusals(t *testing.T) {
	l, r := runSetup(t)
	canary := filepath.Join(l.root, "canary")

	l.script("tool", "touch "+canary+"\n")

	t.Run("confirm not given", func(t *testing.T) {
		out, _ := runOnce(t, r, "cf", context.Background())

		if out.Status != OutcomeRefused || out.Err.Code != CodeConfirmNeeded ||
			out.ExitCode() != ExitUsage {
			t.Errorf("outcome %+v", out)
		}

		if exists(canary) {
			t.Error("the program ran without the confirmation")
		}
	})

	t.Run("confirm given", func(t *testing.T) {
		p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "cf", Yes: true})

		if out := r.Run(context.Background(), p, nil); out.Status != OutcomeOK {
			t.Errorf("outcome %+v", out)
		}

		if !exists(canary) {
			t.Error("the program did not run")
		}
	})

	t.Run("confirm given afterwards", func(t *testing.T) {
		p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "cf"})
		p.Confirmed = true

		if out := r.Run(context.Background(), p, nil); out.Status != OutcomeOK {
			t.Errorf("outcome %+v", out)
		}
	})

	t.Run("terminal run belongs to the host", func(t *testing.T) {
		p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "tm"})

		if !p.IsTerminal() || p.Pause != PauseAlways || p.Timeout != 0 {
			t.Errorf("prepared %+v", p)
		}

		if out := r.Run(context.Background(), p, nil); out.Status != OutcomeRefused ||
			out.Err.Code != CodeTerminalRun {
			t.Errorf("outcome %+v", out)
		}

		if err := r.Guard(p); err != nil {
			t.Errorf("Guard: %v", err)
		}
	})

	t.Run("a run that Prepare did not make", func(t *testing.T) {
		for _, p := range []Prepared{{}, {Argv: []string{"/bin/true"}}, {
			Argv: []string{"/bin/true"},
			Env:  []string{"A=1"},
		}} {
			p.PluginID, p.Output = "demo", OutputPager

			if out := r.Run(context.Background(), p, nil); out.Status != OutcomeRefused {
				t.Errorf("outcome %+v for %+v", out, p)
			}
		}
	})
}

// TestRunStateIsRechecked is the other half of P1: the state is read again immediately before the
// start, so a disable, a changed tree, a changed consent or a replaced program between Prepare and
// Run stops the run and no program starts.
//
// Parameters:
//   - t: test to report to
func TestRunStateIsRechecked(t *testing.T) {
	canary := func(l *lab) string { return filepath.Join(l.root, "canary") }

	tests := []struct {
		name   string
		change func(t *testing.T, l *lab, rec LockRecord)
		code   string
		arg    string
	}{
		{"lock flag", func(t *testing.T, l *lab, _ LockRecord) {
			must(t, l.lock().SetDisabled("demo", true))
		}, CodeNotRunnable, string(StateDisabled)},
		{"config switch", func(t *testing.T, l *lab, _ LockRecord) {
			l.writeConfig("[plugins.demo]\nenabled = false\n")
		}, CodeNotRunnable, string(StateDisabled)},
		{"dotted config switch", func(t *testing.T, l *lab, _ LockRecord) {
			l.writeConfig("plugins.demo.enabled = false\n")
		}, CodeNotRunnable, string(StateDisabled)},
		{"quarantine", func(t *testing.T, l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.Quarantined = true }))
		}, CodeNotRunnable, string(StateQuarantined)},
		{"consent withdrawn", func(t *testing.T, l *lab, _ LockRecord) {
			must(t, l.lock().Update("demo", func(r *LockRecord) { r.ConsentedAt, r.ConsentedBy = "", "" }))
		}, CodeNotRunnable, string(StateNeedsConsent)},
		{"record removed", func(t *testing.T, l *lab, _ LockRecord) {
			must(t, l.lock().Delete("demo"))
		}, CodeNotRunnable, string(StateBroken)},
		{"manifest rewritten", func(t *testing.T, l *lab, rec LockRecord) {
			dir := l.treeDir(rec)
			l.open(dir)
			l.open(filepath.Join(dir, "plugin.toml"))
			must(t, os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte("api = 1\n"), 0o600))
		}, CodeNotRunnable, string(StateBroken)},
		{"program replaced", func(t *testing.T, l *lab, _ LockRecord) {
			l.script("tool.new", "touch "+canary(l)+"\n")
			must(t, os.Rename(filepath.Join(l.bin, "tool.new"), filepath.Join(l.bin, "tool")))
		}, CodeStale, "program"},
		{"program rewritten in place", func(t *testing.T, l *lab, _ LockRecord) {
			time.Sleep(20 * time.Millisecond)
			l.script("tool", "touch "+canary(l)+"\n# changed\n")
		}, CodeStale, "program"},
		{"program removed", func(t *testing.T, l *lab, _ LockRecord) {
			must(t, os.Rename(filepath.Join(l.bin, "tool"), filepath.Join(l.root, "tool.gone")))
		}, CodeNotRunnable, string(StateMissingDependency)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			l.useSystemTools()
			l.script("tool", "touch "+canary(l)+"\n")

			body := `
[[requires]]
bin = "tool"

` + runBody
			rec := l.plug("demo", []string{"exec:tool"}, body)
			r := l.reg(nil)

			p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

			tt.change(t, l, rec)

			out := r.Run(context.Background(), p, nil)

			if out.Status != OutcomeRefused || out.Err == nil || out.Err.Code != tt.code ||
				out.Err.Args[len(out.Err.Args)-1] != tt.arg && out.Err.Args[1] != tt.arg {
				t.Fatalf("outcome %+v err %v", out, out.Err)
			}

			time.Sleep(100 * time.Millisecond)

			if exists(canary(l)) {
				t.Fatal("the program ran")
			}

			if out.ExitCode() != ExitPolicy {
				t.Errorf("exit %d", out.ExitCode())
			}
		})
	}
}

// TestRunUpdateBetweenPrepareAndRun checks that a run prepared from one version of a plugin is
// not started on another: the record points to a new tree.
//
// Parameters:
//   - t: test to report to
func TestRunUpdateBetweenPrepareAndRun(t *testing.T) {
	l, r := runSetup(t)

	p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

	files := map[string]string{"plugin.toml": strings.Replace(decl("", []string{"exec:tool"}, runBody),
		`version = "1.0.0"`, `version = "1.0.1"`, 1)}
	files["plugin.toml"] = strings.Replace(files["plugin.toml"], `id = "demo"`, `id = "demo"`, 1)

	l.install("demo", files)

	out := r.Run(context.Background(), p, nil)

	if out.Status != OutcomeRefused || out.Err.Code != CodeStale {
		t.Errorf("outcome %+v err %v", out, out.Err)
	}
}

// TestRunDisableDuringStart is the window between the start of a program and its record in
// plugin-procs: a disable made there is seen by the second check, and the process is killed
// before it can do anything.
//
// Parameters:
//   - t: test to report to
func TestRunDisableDuringStart(t *testing.T) {
	l, r := runSetup(t)
	canary := filepath.Join(l.root, "canary")

	l.script("tool", "sleep 1\ntouch "+canary+"\n")

	afterStart = func() { must(t, l.lock().SetDisabled("demo", true)) }

	defer func() { afterStart = nil }()

	p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

	began := time.Now()
	out := r.Run(context.Background(), p, nil)

	if out.Status != OutcomeRefused || out.Err.Code != CodeNotRunnable {
		t.Fatalf("outcome %+v err %v", out, out.Err)
	}

	if time.Since(began) > 1500*time.Millisecond {
		t.Errorf("the process was not stopped: %v", time.Since(began))
	}

	time.Sleep(1500 * time.Millisecond)

	if exists(canary) {
		t.Error("the program finished its work after the disable")
	}
}

// TestRunTwoRegistries is the two-registry test of the kill switch: a disable made through one
// registry stops the next spawn of the other, and a StopPlugin through one kills the process the
// other started.
//
// Parameters:
//   - t: test to report to
func TestRunTwoRegistries(t *testing.T) {
	l, a := runSetup(t)
	b := l.reg(nil)
	pidFile := filepath.Join(l.root, "long.pid")

	l.script("tool", "sleep 60 &\necho $! > "+pidFile+"\nwait\n")

	done := make(chan Outcome, 1)
	p := prepare(t, a, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

	go func() { done <- a.Run(context.Background(), p, nil) }()

	waitFile(t, pidFile)
	waitProc(t, a, "demo")

	must(t, l.lock().SetDisabled("demo", true))

	n, err := b.StopPlugin("demo", 200*time.Millisecond)
	must(t, err)

	if n != 1 {
		t.Errorf("StopPlugin signalled %d sessions", n)
	}

	select {
	case out := <-done:
		if out.Status != OutcomeSignaled && out.Status != OutcomeFailed {
			t.Errorf("outcome %+v", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end")
	}

	waitGone(t, pidFile)

	e := refusal(t, b, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})
	if e.Code != CodeNotRunnable {
		t.Errorf("the second registry still prepares: %v", e)
	}

	out := a.Run(context.Background(), p, nil)
	if out.Status != OutcomeRefused {
		t.Errorf("the first registry still runs: %+v", out)
	}
}

// waitFile waits until a file exists and is not empty.
//
// Parameters:
//   - t: test to report to
//   - path: location
func waitFile(t *testing.T, path string) {
	t.Helper()

	for i := 0; i < 200; i++ {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("%s did not appear", path)
}

// TestRunBusy checks the cap of four plugin processes: a fifth start is refused at once, and a
// slot comes back when a run ends.
//
// Parameters:
//   - t: test to report to
func TestRunBusy(t *testing.T) {
	l, r := runSetup(t)

	l.script("tool", "sleep 30\n")

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	for i := 0; i < maxProcesses; i++ {
		p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

		wg.Add(1)

		go func() {
			defer wg.Done()

			r.Run(ctx, p, nil)
		}()
	}

	for i := 0; i < 250; i++ {
		r.rt.mu.Lock()
		active := r.rt.active
		r.rt.mu.Unlock()

		if active == maxProcesses {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	began := time.Now()
	out := r.Run(context.Background(), prepare(t, r, Call{
		Plugin: "demo", Table: TableCommand,
		Entry: "pg",
	}), nil)

	if out.Status != OutcomeRefused || out.Err.Code != CodeBusy || out.ExitCode() != ExitPolicy ||
		time.Since(began) > time.Second {
		t.Errorf("fifth run: %+v", out)
	}

	cancel()
	wg.Wait()

	l.script("tool", "echo again\n")

	out, ev := runOnce(t, r, "pg", context.Background())
	if out.Status != OutcomeOK || ev.text() != "again\n" {
		t.Errorf("after the others ended: %+v", out)
	}
}

// TestRunConcurrent runs many calls at once under the race detector: every run either completes
// with its own output or is refused as busy, and nothing is shared between them.
//
// Parameters:
//   - t: test to report to
func TestRunConcurrent(t *testing.T) {
	l, r := runSetup(t)

	l.script("tool", "echo \"$1\"\nsleep 0.05\n")

	const total = 24

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
		busy int
	)

	for i := 0; i < total; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			arg := strings.Repeat(string(rune('a'+i%26)), 5)

			p, err := r.Prepare(Call{
				Plugin: "demo", Table: TableCommand, Entry: "pg",
				Values: map[string]string{"arg": arg},
			})
			if err != nil {
				t.Error(err)

				return
			}

			ev := &events{}
			out := r.Run(context.Background(), p, ev.add)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case out.Status == OutcomeOK && ev.text() == arg+"\n":
				done++
			case out.Status == OutcomeRefused && out.Err.Code == CodeBusy:
				busy++
			default:
				t.Errorf("outcome %+v output %q", out, ev.text())
			}
		}()
	}

	wg.Wait()

	if done+busy != total || done == 0 {
		t.Errorf("done %d busy %d", done, busy)
	}

	r.rt.mu.Lock()
	defer r.rt.mu.Unlock()

	if r.rt.active != 0 {
		t.Errorf("%d slots still taken", r.rt.active)
	}
}
