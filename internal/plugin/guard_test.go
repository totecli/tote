package plugin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// TestReviewMutatingVerbsRefuseFromAPluginSession checks the second test of the "called from a
// plugin" rule, the one that survives `env -u TOTE_PLUGIN_ID`: when the calling process belongs to
// a session recorded in plugin-procs, every verb that changes plugins is refused with from_plugin.
// InPluginSession was tested alone; its use by the verbs was not.
//
// Parameters:
//   - t: test to report to
func TestReviewMutatingVerbsRefuseFromAPluginSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the session check needs /proc")
	}

	l, r := runSetup(t)
	src := demoSource(l, "other", "1.0.0")

	own, err := getsid(0)
	must(t, err)

	writeProcs(t, l, "demo "+strconv.Itoa(own)+" -\n")

	calls := map[string]func() error{
		"Plan": func() error {
			_, err := r.Plan(context.Background(), InstallRequest{Source: src})

			return err
		},
		"Enable":  func() error { _, err := r.Enable("demo", Grant{Prompt: true}); return err },
		"Disable": func() error { _, err := r.Disable("demo"); return err },
		"Remove":  func() error { _, err := r.Remove("demo", RemoveOptions{}); return err },
		"Grant":   func() error { _, err := r.Grant("demo", Grant{Prompt: true}); return err },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if e := asError(t, call()); e.Code != CodeFromPlugin {
				t.Errorf("got %s, want %s", e.Code, CodeFromPlugin)
			}
		})
	}

	if rec, ok := l.lock().Get("demo"); !ok || rec.Disabled {
		t.Errorf("the record changed: %+v %v", rec, ok)
	}
}

// TestReviewStopPluginTermThenKill checks the two signals of the kill switch separately: the
// session first gets SIGTERM, so that a program can finish its work, and gets SIGKILL when it is
// still there after the grace period. Both are in the specification, and either signal could be
// dropped without a test failing.
//
// Parameters:
//   - t: test to report to
func TestReviewStopPluginTermThenKill(t *testing.T) {
	t.Run("term first", func(t *testing.T) {
		l, r := runSetup(t)
		pidFile := filepath.Join(l.root, "term.pid")
		termFile := filepath.Join(l.root, "term.seen")

		l.script("tool", "trap 'echo term > "+termFile+"; exit 0' TERM\necho $$ > "+pidFile+
			"\nsleep 60 &\nwait\n")

		done := make(chan Outcome, 1)
		p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

		go func() { done <- r.Run(context.Background(), p, nil) }()

		waitFile(t, pidFile)
		waitProc(t, r, "demo")

		if n, err := r.StopPlugin("demo", 3*time.Second); err != nil || n != 1 {
			t.Fatalf("StopPlugin = %d %v", n, err)
		}

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the run did not end")
		}

		if !exists(termFile) {
			t.Error("the program never saw SIGTERM")
		}
	})

	t.Run("kill after the grace period", func(t *testing.T) {
		l, r := runSetup(t)
		pidFile := filepath.Join(l.root, "stubborn.pid")

		l.script("tool", "trap '' TERM\necho $$ > "+pidFile+"\nwhile :; do sleep 1; done\n")

		done := make(chan Outcome, 1)
		p := prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "pg"})

		go func() { done <- r.Run(context.Background(), p, nil) }()

		waitFile(t, pidFile)
		waitProc(t, r, "demo")

		if n, err := r.StopPlugin("demo", 300*time.Millisecond); err != nil || n != 1 {
			t.Fatalf("StopPlugin = %d %v", n, err)
		}

		waitGone(t, pidFile)

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the run did not end")
		}
	})
}

// TestReviewPluginsDirectoryWritableByOthers checks that a plugins directory that the group or
// the world can write is refused before anything is staged or placed: another user could swap a
// tree between the check and the run. Only the staging link and a foreign owner were covered.
//
// Parameters:
//   - t: test to report to
func TestReviewPluginsDirectoryWritableByOthers(t *testing.T) {
	for _, mode := range []os.FileMode{0o770, 0o707, 0o777} {
		t.Run(mode.String(), func(t *testing.T) {
			l := newLab(t)
			src := demoSource(l, "demo", "1.0.0")
			r := l.reg(nil)

			must(t, os.MkdirAll(l.paths.PluginsDir(), 0o700))
			must(t, os.Chmod(l.paths.PluginsDir(), mode))

			_, err := r.Plan(context.Background(), InstallRequest{Source: src})
			if err == nil {
				t.Fatal("Plan accepted a plugins directory that others can write")
			}

			if e := asError(t, err); e.Code != CodeStoreWritable {
				t.Errorf("got %s %v, want %s", e.Code, e.Args, CodeStoreWritable)
			}
		})
	}
}

// TestReviewCoversLeadingDot pins the wildcard rule at its edge: net:*.example.org covers a
// subdomain, never the bare domain, a host that only ends with the domain, or the empty label.
//
// Parameters:
//   - t: test to report to
func TestReviewCoversLeadingDot(t *testing.T) {
	have := []string{"net:*.example.org"}

	tests := []struct {
		want string
		ok   bool
	}{
		{"net:api.example.org", true},
		{"net:a.b.example.org", true},
		{"net:example.org", false},
		{"net:badexample.org", false},
		{"net:.example.org", false},
		{"net:*.example.org", true},
		{"net:*.sub.example.org", false},
		{"net:*", false},
	}

	for _, tt := range tests {
		if got := Covers(have, tt.want); got != tt.ok {
			t.Errorf("Covers(%v, %q) = %v, want %v", have, tt.want, got, tt.ok)
		}
	}
}
