package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// choiceValuesOf are the values of a call of the palette command get whose earlier fields are all
// valid, so that the suggestions of tag and of asset can be asked.
var choiceValuesOf = map[string]string{"slug": "a/b", "memo": "m", "last": "l"}

// choiceRig builds the sandbox of choicesBody with a fake program and prepares the suggestions of
// a field.
//
// Parameters:
//   - t: test to report to
//   - script: body of the fake program "tool", where ROOT stands for the directory of the sandbox;
//     the program cannot change after the preparation, which Guard would see
//   - field: field whose suggestions are listed, "asset" has no pattern, "tag" has one
func choiceRig(t *testing.T, script, field string) (*lab, *Registry, Prepared) {
	t.Helper()

	l, r := choicesLab(t)
	l.script("tool", strings.ReplaceAll(script, "ROOT", l.root))

	return l, r, choices(t, r, getCall(choiceValuesOf), field)
}

// runChoicesNow runs a prepared suggestions run with a context that nothing cancels.
//
// Parameters:
//   - r: registry
//   - p: prepared run
func runChoicesNow(r *Registry, p Prepared) ChoicesResult {
	return r.RunChoices(context.Background(), p)
}

// wantFailed checks a failed result and the error it carries.
//
// Parameters:
//   - t: test to report to
//   - res: result
//   - reason: reason it must give
//   - arg: argument that goes with it
//   - detail: line of the program
func wantFailed(t *testing.T, res ChoicesResult, reason, arg, detail string) {
	t.Helper()

	if res.State != ChoicesFailed || res.Reason != reason || res.Arg != arg || res.Detail != detail {
		t.Fatalf("result %+v, want failed %s %q %q", res, reason, arg, detail)
	}

	if len(res.Items) != 0 || res.Hidden != 0 || res.More != 0 {
		t.Errorf("a failed run kept a list: %+v", res)
	}

	if res.Err == nil || res.Err.Code != CodeChoicesFailed || len(res.Err.Args) != 3 ||
		res.Err.Args[0] != "tag" && res.Err.Args[0] != "asset" || res.Err.Args[1] != reason ||
		res.Err.Args[2] != detail {
		t.Errorf("error %v", res.Err)
	}
}

// TestRunChoicesList checks a successful run: order, stderr left out, dropped entries counted.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesList(t *testing.T) {
	_, r, p := choiceRig(t, "printf 'v1\\tfirst\\nv2\\nbad value\\n'\necho noise >&2\n", "asset")

	res := runChoicesNow(r, p)

	if res.State != ChoicesList || res.Reason != "" || res.Err != nil || res.Detail != "" ||
		res.Hidden != 1 || res.More != 0 {
		t.Fatalf("result %+v", res)
	}

	if len(res.Items) != 2 || res.Items[0] != (Choice{"v1", "first"}) ||
		res.Items[1] != (Choice{"v2", ""}) {
		t.Errorf("items %+v", res.Items)
	}

	if res.Duration <= 0 {
		t.Errorf("duration %v", res.Duration)
	}
}

// TestRunChoicesArgv checks that the program gets the expanded argv of the preparation.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesArgv(t *testing.T) {
	l, r, p := choiceRig(t, "echo \"$*\" > ROOT/argv\necho ok\n", "tag")
	file := filepath.Join(l.root, "argv")

	res := runChoicesNow(r, p)
	if res.State != ChoicesList {
		t.Fatalf("result %+v", res)
	}

	data, err := os.ReadFile(file)
	must(t, err)

	if got := strings.TrimSpace(string(data)); got != "--list --pre=false -- a/b" {
		t.Errorf("argv %q", got)
	}
}

// TestRunChoicesEmpty checks the empty list: a clean exit with nothing that survives the rules.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesEmpty(t *testing.T) {
	tests := []struct {
		name, script string
		hidden       int
	}{
		{"nothing printed", "exit 0\n", 0},
		{"only blank lines", "printf '\\n\\n'\n", 0},
		{"everything dropped", "printf 'a b\\n-x\\t1\\t2\\n'\n", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, r, p := choiceRig(t, tt.script, "asset")

			res := runChoicesNow(r, p)
			if res.State != ChoicesEmpty || len(res.Items) != 0 || res.Hidden != tt.hidden ||
				res.Err != nil {
				t.Errorf("result %+v", res)
			}
		})
	}
}

// TestRunChoicesRules checks that the pattern and the length of the field filter the list in the
// run, and that nothing is rewritten.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesRules(t *testing.T) {
	script := "printf 'v1.2\\n--upgrade\\nV3\\n" + strings.Repeat("a", 17) + "\\nok\\n'\n"
	_, r, p := choiceRig(t, script, "tag")

	res := runChoicesNow(r, p)

	var got []string
	for _, c := range res.Items {
		got = append(got, c.Value)
	}

	if res.State != ChoicesList || !equalStrings(got, []string{"v1.2", "--upgrade", "ok"}) ||
		res.Hidden != 2 {
		t.Errorf("result %+v", res)
	}
}

// TestRunChoicesFailures checks that a failure discards the list and reports one cleaned line.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesFailures(t *testing.T) {
	tests := []struct {
		name, script, reason, arg, detail string
	}{
		{"status with lines", "echo v1\necho boom >&2\nexit 3\n", ChoicesReasonExit, "3", "boom"},
		{"status without a word", "exit 1\n", ChoicesReasonExit, "1", ""},
		{
			"last line of the standard error", "echo ignored >&2\necho 'Repository not found' >&2\nexit 1\n",
			ChoicesReasonExit, "1", "Repository not found",
		},
		{
			"stdout only, with colours, as gft 1.2 does",
			"printf '\\033[1;31mUnknown option: --plain\\033[0m\\n'\nexit 2\n",
			ChoicesReasonExit, "2", "Unknown option: --plain",
		},
		{
			"stderr wins over stdout", "echo from-stdout\necho from-stderr >&2\nexit 1\n",
			ChoicesReasonExit, "1", "from-stderr",
		},
		{
			"escape sequences in stderr", "printf '\\033]0;x\\007\\033[2Jbad\\033[0m\\n' >&2\nexit 1\n",
			ChoicesReasonExit, "1", "?]0;x??[2Jbad",
		},
		{
			"a token in stderr is hidden",
			"echo 'denied ghp_abcdefghijklmnopqrstuvwxyz0123456789' >&2\nexit 1\n",
			ChoicesReasonExit, "1", "denied " + Mask,
		},
		{"killed by a signal", "echo v1\nkill -9 $$\n", ChoicesReasonSignal, "KILL", "v1"},
		{"terminated by a signal", "echo oops >&2\nkill -TERM $$\n", ChoicesReasonSignal, "TERM", "oops"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, r, p := choiceRig(t, tt.script, "asset")

			res := runChoicesNow(r, p)

			wantFailed(t, res, tt.reason, tt.arg, tt.detail)

			if strings.ContainsRune(res.Detail, 0x1b) {
				t.Errorf("an escape reached the detail: %q", res.Detail)
			}

			if res.Err.Code == "" || ExitOf(res.Err) != ExitFailure {
				t.Errorf("exit %d", ExitOf(res.Err))
			}
		})
	}
}

// TestRunChoicesDetailIsBounded checks that a program that writes a very long line, or many, to
// its standard error only gives one cleaned and cut line.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesDetailIsBounded(t *testing.T) {
	script := "head -c 100000 /dev/zero | tr '\\0' 'x' >&2\nexit 1\n"
	_, r, p := choiceRig(t, script, "asset")

	res := runChoicesNow(r, p)

	wantFailed(t, res, ChoicesReasonExit, "1", strings.Repeat("x", choiceDetailRunes))
}

// TestRunChoicesCap checks the 64 KiB of the standard output: exactly that is a list, one byte
// more fails the whole list with a reason of its own, which is neither a timeout nor a cancel.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesCap(t *testing.T) {
	t.Run("exactly 64 KiB", func(t *testing.T) {
		_, r, p := choiceRig(t, "yes abcdefg | head -c 65536\n", "asset")

		res := runChoicesNow(r, p)
		if res.State != ChoicesList || len(res.Items) != 1 || res.Items[0].Value != "abcdefg" {
			t.Errorf("result %+v", res)
		}
	})

	t.Run("one byte more", func(t *testing.T) {
		_, r, p := choiceRig(t, "yes abcdefg | head -c 65537\n", "asset")

		wantFailed(t, runChoicesNow(r, p), ChoicesReasonTooLarge, "", "")
	})

	t.Run("endless output stops the program", func(t *testing.T) {
		l, r, p := choiceRig(t, "echo $$ > ROOT/yes.pid\nexec yes abcdefg\n", "asset")
		pid := filepath.Join(l.root, "yes.pid")

		began := time.Now()
		res := runChoicesNow(r, p)

		wantFailed(t, res, ChoicesReasonTooLarge, "", "")

		if took := time.Since(began); took > 10*time.Second {
			t.Errorf("took %v", took)
		}

		waitGone(t, pid)
	})

	t.Run("endless standard error", func(t *testing.T) {
		l, r, p := choiceRig(t, "echo $$ > ROOT/yes.pid\nexec yes boom >&2\n", "asset")
		pid := filepath.Join(l.root, "yes.pid")

		res := runChoicesNow(r, p)
		if res.State != ChoicesFailed || res.Reason != ChoicesReasonTooLarge || res.Detail != "boom" {
			t.Errorf("result %+v", res)
		}

		waitGone(t, pid)
	})
}

// TestRunChoicesTimeout checks the time limit: the group is stopped and the state is a timeout.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesTimeout(t *testing.T) {
	l, r, p := choiceRig(t, "echo v1\necho $$ > ROOT/sleep.pid\nsleep 60 &\n"+
		"echo $! > ROOT/child.pid\nwait\n", "asset")
	pid := filepath.Join(l.root, "sleep.pid")
	child := filepath.Join(l.root, "child.pid")

	p.Timeout = 400 * time.Millisecond

	began := time.Now()
	res := runChoicesNow(r, p)

	if res.State != ChoicesTimedOut || res.Reason != "timeout" || len(res.Items) != 0 ||
		res.Err != nil {
		t.Fatalf("result %+v", res)
	}

	if took := time.Since(began); took < 400*time.Millisecond || took > 10*time.Second {
		t.Errorf("took %v", took)
	}

	waitGone(t, pid)
	waitGone(t, child)
}

// TestChoicesTimeoutIsFixed checks that a prepared run cannot ask for more than 10 seconds.
//
// Parameters:
//   - t: test to report to
func TestChoicesTimeoutIsFixed(t *testing.T) {
	_, _, p := choiceRig(t, "exit 0\n", "asset")

	if p.Timeout != ChoicesTimeout || ChoicesTimeout != 10*time.Second {
		t.Errorf("timeout %v", p.Timeout)
	}
}

// TestRunChoicesCancel checks a cancel: the group dies, the state is cancelled and not a timeout,
// and a context that was cancelled before the start starts nothing.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesCancel(t *testing.T) {
	l, r, p := choiceRig(t, "echo x >> ROOT/runs\necho v1\necho $$ > ROOT/sleep.pid\nsleep 60 &\n"+
		"echo $! > ROOT/child.pid\nwait\n", "asset")
	pid := filepath.Join(l.root, "sleep.pid")
	child := filepath.Join(l.root, "child.pid")
	runs := filepath.Join(l.root, "runs")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan ChoicesResult, 1)

	go func() { done <- r.RunChoices(ctx, p) }()

	waitFile(t, pid)
	waitFile(t, child)
	waitProc(t, r, "demo")
	cancel()

	select {
	case res := <-done:
		if res.State != ChoicesCancelled || len(res.Items) != 0 || res.Err != nil {
			t.Errorf("result %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not end")
	}

	waitGone(t, pid)
	waitGone(t, child)

	if res := r.RunChoices(ctx, p); res.State != ChoicesCancelled {
		t.Errorf("a cancelled context: %+v", res)
	}

	if data, err := os.ReadFile(runs); err != nil || strings.Count(string(data), "x") != 1 {
		t.Errorf("the program started %q times, err %v", data, err)
	}
}

// TestRunChoicesKillsWhatIgnoresTerm checks that a group that ignores SIGTERM is killed after the
// grace time, and that the run does not wait for it any longer.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesKillsWhatIgnoresTerm(t *testing.T) {
	l, r, p := choiceRig(t, "trap '' TERM\necho $$ > ROOT/stubborn.pid\n"+
		"while true; do sleep 1; done\n", "asset")
	pid := filepath.Join(l.root, "stubborn.pid")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan ChoicesResult, 1)

	go func() { done <- r.RunChoices(ctx, p) }()

	waitFile(t, pid)

	began := time.Now()

	cancel()

	select {
	case res := <-done:
		if res.State != ChoicesCancelled {
			t.Errorf("result %+v", res)
		}

		if took := time.Since(began); took < time.Second || took > 10*time.Second {
			t.Errorf("stopped after %v, want about %v", took, killGrace)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end")
	}

	waitGone(t, pid)
}

// TestRunChoicesGrandchildKeepsThePipe checks the drain of the normal run: a program that leaves a
// child holding its pipes does not hang the caller.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesGrandchildKeepsThePipe(t *testing.T) {
	l, r, p := choiceRig(t, "sleep 30 &\necho $! > ROOT/child.pid\necho v1\n", "asset")
	child := filepath.Join(l.root, "child.pid")

	began := time.Now()
	res := runChoicesNow(r, p)

	t.Cleanup(func() {
		if data, err := os.ReadFile(child); err == nil {
			if pid, _ := strconv.Atoi(strings.TrimSpace(string(data))); pid > 1 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})

	if res.State != ChoicesList || len(res.Items) != 1 || res.Items[0].Value != "v1" {
		t.Errorf("result %+v", res)
	}

	if took := time.Since(began); took > 10*time.Second {
		t.Errorf("the run waited for the grandchild: %v", took)
	}
}

// TestRunChoicesKillsWhatItLeftBehind checks that a process left running after exit 0 is killed
// (a list is a bounded lookup) and that its plugin-procs entry is gone.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesKillsWhatItLeftBehind(t *testing.T) {
	script := "sleep 300 >/dev/null 2>&1 &\necho $! > ROOT/child.pid\necho v1\n"
	l, r, p := choiceRig(t, script, "asset")
	child := filepath.Join(l.root, "child.pid")

	t.Cleanup(func() {
		if data, err := os.ReadFile(child); err == nil {
			if pid, _ := strconv.Atoi(strings.TrimSpace(string(data))); pid > 1 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})

	res := runChoicesNow(r, p)

	if res.State != ChoicesList || len(res.Items) != 1 || res.Items[0].Value != "v1" {
		t.Fatalf("result %+v", res)
	}

	waitGone(t, child)

	data, err := os.ReadFile(r.opts.Paths.ProcsFile())
	if err == nil && strings.TrimSpace(string(data)) != "" {
		t.Errorf("plugin-procs still holds %q", data)
	}
}

// TestRunChoicesGrandchildKeepsWriting checks the other end of the drain: a child that keeps
// writing is cut off after 5 seconds, and the run does not wait for it any longer.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesGrandchildKeepsWriting(t *testing.T) {
	if testing.Short() {
		t.Skip("takes 5 seconds")
	}

	l, r, p := choiceRig(t, "(while true; do echo v1; sleep 0.05; done) &\necho $! > ROOT/child.pid\n"+
		"echo v0\n", "asset")
	child := filepath.Join(l.root, "child.pid")

	t.Cleanup(func() {
		if data, err := os.ReadFile(child); err == nil {
			if pid, _ := strconv.Atoi(strings.TrimSpace(string(data))); pid > 1 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})

	began := time.Now()
	res := runChoicesNow(r, p)

	if res.State != ChoicesList || len(res.Items) < 1 {
		t.Errorf("result %+v", res)
	}

	if took := time.Since(began); took < 4*time.Second || took > 8*time.Second {
		t.Errorf("drain took %v, want about 5s", took)
	}
}

// TestRunChoicesEnvironment checks the environment of the program: proxy variables reach it,
// secrets, env: permission variables and the entry env table do not, and NO_COLOR is set.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesEnvironment(t *testing.T) {
	l, r := choicesLab(t)
	file := filepath.Join(l.root, "env.txt")

	for k, v := range hostileParent {
		l.env[k] = v
	}

	l.env["A"] = "declared-value"
	l.env["PATH"] = l.bin + ":" + shellPath

	l.script("tool", "env > "+file+"\necho v1\n")

	p := choices(t, r, getCall(choiceValuesOf), "asset")

	if res := runChoicesNow(r, p); res.State != ChoicesList {
		t.Fatalf("result %+v", res)
	}

	data, err := os.ReadFile(file)
	must(t, err)

	got := map[string]string{}

	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			got[k] = v
		}
	}

	if got["HTTP_PROXY"] != "http://p:1" || got["https_proxy"] != "http://p:2" ||
		got["NO_PROXY"] != "x" {
		t.Errorf("the proxy variables did not reach the program: %v", got)
	}

	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN", "A", "AWS_SECRET_ACCESS_KEY"} {
		if v, ok := got[k]; ok {
			t.Errorf("%s=%s reached the program", k, v)
		}
	}

	if strings.Contains(string(data), "ghp_secret") ||
		strings.Contains(string(data), "from-the-table") {
		t.Errorf("a secret or a table value leaked:\n%s", data)
	}

	if got["NO_COLOR"] != "1" || got["TOTE_PLUGIN_ID"] != "demo" {
		t.Errorf("NO_COLOR %q, TOTE_PLUGIN_ID %q", got["NO_COLOR"], got["TOTE_PLUGIN_ID"])
	}
}

// TestRunChoicesDirectoryAndInput checks that the working directory is the cache directory of the
// plugin, not the one of the registry, and that the input is /dev/null.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesDirectoryAndInput(t *testing.T) {
	l, r, p := choiceRig(t, "pwd -P > ROOT/where.txt\nif read x; then echo read >> ROOT/where.txt\n"+
		"else echo eof >> ROOT/where.txt; fi\nif [ -t 0 ]; then echo tty >> ROOT/where.txt; fi\n"+
		"echo v1\n", "asset")
	file := filepath.Join(l.root, "where.txt")

	if res := runChoicesNow(r, p); res.State != ChoicesList {
		t.Fatalf("result %+v", res)
	}

	data, err := os.ReadFile(file)
	must(t, err)

	cache, _ := l.paths.PluginCacheDir("demo")
	want, err := filepath.EvalSymlinks(cache)
	must(t, err)

	if got := strings.Split(strings.TrimSpace(string(data)), "\n"); len(got) != 2 || got[0] != want ||
		got[1] != "eof" {
		t.Errorf("working directory, input: %q, want %q and eof", got, want)
	}
}

// TestRunChoicesSession checks that the program runs in a session of its own, with no controlling
// terminal, and that plugin-procs records it while it runs and forgets it after.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesSession(t *testing.T) {
	l, r, p := choiceRig(t, "echo 1 > ROOT/ready\nsleep 0.5\necho v1\n", "asset")
	ready := filepath.Join(l.root, "ready")

	done := make(chan ChoicesResult, 1)

	go func() { done <- r.RunChoices(context.Background(), p) }()

	waitFile(t, ready)
	waitProc(t, r, "demo")

	if res := <-done; res.State != ChoicesList {
		t.Errorf("result %+v", res)
	}

	procs, err := r.Procs()
	must(t, err)

	if len(procs) != 0 {
		t.Errorf("plugin-procs still lists %v", procs)
	}
}

// TestRunChoicesStoppedByDisable checks the kill switch: a disable that stops the processes of the
// plugin ends a suggestions run, which reports a signal and no list.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesStoppedByDisable(t *testing.T) {
	l, r, p := choiceRig(t, "echo v1\necho $$ > ROOT/long.pid\nsleep 60 &\nwait\n", "asset")
	pid := filepath.Join(l.root, "long.pid")

	done := make(chan ChoicesResult, 1)

	go func() { done <- r.RunChoices(context.Background(), p) }()

	waitFile(t, pid)
	waitProc(t, r, "demo")

	must(t, l.lock().SetDisabled("demo", true))

	if _, err := r.StopPlugin("demo", 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-done:
		if res.State != ChoicesFailed || res.Reason != ChoicesReasonSignal || len(res.Items) != 0 {
			t.Errorf("result %+v", res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end")
	}

	waitGone(t, pid)
}

// TestRunChoicesGuard checks the state check before the start: a plugin that was disabled after
// the preparation, a tree that changed and a program that was replaced start nothing.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesGuard(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		l, r, p := choiceRig(t, "touch ROOT/canary\necho v1\n", "asset")
		canary := filepath.Join(l.root, "canary")

		must(t, l.lock().SetDisabled("demo", true))

		res := runChoicesNow(r, p)
		if res.State != ChoicesFailed || res.Reason != ChoicesReasonState || res.Arg != CodeNotRunnable ||
			res.Err == nil || res.Err.Code != CodeChoicesFailed {
			t.Errorf("result %+v", res)
		}

		if exists(canary) {
			t.Error("the program started")
		}
	})

	t.Run("program replaced", func(t *testing.T) {
		l, r, p := choiceRig(t, "echo v1\n", "asset")
		canary := filepath.Join(l.root, "canary")

		path := filepath.Join(l.bin, "tool")
		must(t, os.Remove(path))
		l.script("tool", "touch "+canary+"\necho v1\n")

		res := runChoicesNow(r, p)
		if res.State != ChoicesFailed || res.Reason != ChoicesReasonState || res.Arg != CodeStale {
			t.Errorf("result %+v", res)
		}

		if exists(canary) {
			t.Error("the program started")
		}
	})

	t.Run("disabled right after the start", func(t *testing.T) {
		l, r, p := choiceRig(t, "echo $$ > ROOT/pid\nsleep 20\ntouch ROOT/canary\necho v1\n", "asset")
		canary := filepath.Join(l.root, "canary")
		pid := filepath.Join(l.root, "pid")

		afterStart = func() { must(t, l.lock().SetDisabled("demo", true)) }

		defer func() { afterStart = nil }()

		began := time.Now()
		res := runChoicesNow(r, p)

		if res.State != ChoicesFailed || res.Reason != ChoicesReasonState || res.Arg != CodeNotRunnable {
			t.Errorf("result %+v", res)
		}

		// The program sleeps 20 seconds: ending long before is the proof that it was stopped.
		if took := time.Since(began); took > 15*time.Second {
			t.Errorf("the process was not stopped: %v", took)
		}

		// A disable that comes right after the start can stop the program before it wrote its pid.
		if exists(pid) {
			waitGone(t, pid)
		}

		if exists(canary) {
			t.Error("the program finished its work after the disable")
		}
	})
}

// TestRunChoicesRefusesOtherRuns checks that only a run of Registry.Choices is accepted, and that
// Run keeps refusing it.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesRefusesOtherRuns(t *testing.T) {
	_, r, p := choiceRig(t, "echo v1\n", "asset")

	normal := prepare(t, r, getCall(choiceValuesOf))

	res := r.RunChoices(context.Background(), normal)
	if res.State != ChoicesFailed || res.Reason != ChoicesReasonRefused {
		t.Errorf("a normal run: %+v", res)
	}

	res = r.RunChoices(context.Background(), Prepared{})
	if res.State != ChoicesFailed || res.Reason != ChoicesReasonRefused {
		t.Errorf("an empty run: %+v", res)
	}

	if out := r.Run(context.Background(), p, nil); out.Status != OutcomeRefused ||
		out.Err.Code != CodeUnsupported {
		t.Errorf("Run took a suggestions run: %+v", out)
	}
}

// TestRunChoicesBusy checks that a suggestions run takes one of the four process slots and is
// refused at once when they are all taken.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesBusy(t *testing.T) {
	l, r, p := choiceRig(t, "touch ROOT/canary\necho v1\n", "asset")
	canary := filepath.Join(l.root, "canary")

	for i := 0; i < maxProcesses; i++ {
		if !r.rt.acquire() {
			t.Fatal("no slot")
		}
	}

	began := time.Now()
	res := runChoicesNow(r, p)

	if res.State != ChoicesFailed || res.Reason != ChoicesReasonBusy ||
		time.Since(began) > 5*time.Second {
		t.Errorf("result %+v", res)
	}

	if exists(canary) {
		t.Error("the program started")
	}

	r.rt.release()

	if res := runChoicesNow(r, p); res.State != ChoicesList {
		t.Errorf("after a slot was freed: %+v", res)
	}

	for i := 1; i < maxProcesses; i++ {
		r.rt.release()
	}
}

// TestRunChoicesConcurrent runs several suggestions at once, under the race detector: each gets
// its own list.
//
// Parameters:
//   - t: test to report to
func TestRunChoicesConcurrent(t *testing.T) {
	_, r, _ := choiceRig(t, "echo \"t$$\"\n", "asset")

	var wg sync.WaitGroup

	results := make([]ChoicesResult, maxProcesses)

	for i := range results {
		p := choices(t, r, getCall(choiceValuesOf), "asset")

		wg.Add(1)

		go func() {
			defer wg.Done()

			results[i] = runChoicesNow(r, p)
		}()
	}

	wg.Wait()

	seen := map[string]bool{}

	for _, res := range results {
		if res.State == ChoicesFailed && res.Reason == ChoicesReasonBusy {
			continue
		}

		if res.State != ChoicesList || len(res.Items) != 1 || seen[res.Items[0].Value] {
			t.Errorf("result %+v", res)
		}

		seen[res.Items[0].Value] = true
	}

	if len(seen) == 0 {
		t.Error("no run completed")
	}
}
