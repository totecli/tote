package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// recorder is a lookup that plays "ssh -G": it answers with a fixed text or error and keeps
// the commands it was asked about.
type recorder struct {
	mu   sync.Mutex
	out  string
	err  error
	seen [][]string
}

// probe implements the lookup of the options.
//
// Parameters:
//   - ctx: unused
//   - argv: command that was asked about
//   - environ: unused
func (r *recorder) probe(_ context.Context, argv, _ []string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.seen = append(r.seen, append([]string(nil), argv...))

	return r.out, r.err
}

// asked returns a copy of the commands that were asked about.
func (r *recorder) asked() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([][]string(nil), r.seen...)
}

// TestConnectKeepalive plays the connection of a plain ssh host against several answers of the
// lookup and checks the command that runs.
//
// Parameters:
//   - t: test to report to
func TestConnectKeepalive(t *testing.T) {
	plain := "ssh -l deploy -- prod.example.com"
	both := "ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -l deploy -- prod.example.com"

	tests := []struct {
		name string
		out  string
		err  error
		want string
	}{
		{"none configured", "serveraliveinterval 0\nserveralivecountmax 3\n", nil, both},
		{"configured", "serveraliveinterval 60\nserveralivecountmax 3\n", nil, plain},
		{
			"count chosen", "serveraliveinterval 0\nserveralivecountmax 9\n",
			nil, "ssh -o ServerAliveInterval=30 -l deploy -- prod.example.com",
		},
		{"lookup failed", "serveraliveinterval 0\n", errors.New("exit status 255"), plain},
		{"no answer", "", nil, plain},
	}

	for _, tt := range tests {
		rec := &recorder{out: tt.out, err: tt.err}
		run := &fakeRunner{}
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Runner = run
			o.Probe = rec.probe
		}))

		g.r.delay = 0

		cmd := g.r.activate(activateMsg{Kind: kindConn, Key: "prod"})
		if g.r.guard.busy.Load() == 0 {
			t.Errorf("%s: the signal guard is not armed during the lookup", tt.name)
		}

		g.send(playExec(t, settle(g, cmd), "\n"))

		calls := run.calls()
		if len(calls) != 1 || strings.Join(calls[0].Argv, " ") != tt.want {
			t.Errorf("%s: calls = %+v, want %q", tt.name, calls, tt.want)
		}

		asked := rec.asked()
		wantAsk := "ssh -G -l deploy -- prod.example.com"
		if len(asked) != 1 || strings.Join(asked[0], " ") != wantAsk {
			t.Errorf("%s: asked %v", tt.name, asked)
		}
	}
}

// TestKeepaliveNeverOnTheCursor checks the rule that "ssh -G" is asked only when a key runs,
// copies or inserts a command: it can run the Match exec commands of the user's ssh_config, so
// nothing that merely moves around may start it. Every way of reaching a connection is played,
// the status line and the detail panel keep showing the base command, and the one lookup is
// made by the key that connects.
//
// Parameters:
//   - t: test to report to
func TestKeepaliveNeverOnTheCursor(t *testing.T) {
	rec := &recorder{out: "serveraliveinterval 0\nserveralivecountmax 3\n"}
	run := &fakeRunner{}
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.Runner = run
		o.Probe = rec.probe
	}))

	g.r.delay = 0

	g.press("j", "k", "j", "k", "down", "up", "end", "home")
	g.press("/", "p", "r", "esc")
	g.press("ctrl+k")
	g.typeText("prod")
	g.press("down", "up", "esc")
	g.press("ctrl+r")
	g.resize(120, 40)
	g.press("v", "esc")

	if got := rec.asked(); len(got) != 0 {
		t.Fatalf("the cursor started %d lookups: %v", len(got), got)
	}

	text := g.text()
	if strings.Contains(text, "ServerAlive") || !strings.Contains(text, "ssh -l") {
		t.Errorf("the screen does not show the base command:\n%s", text)
	}

	g.send(playExec(t, settle(g, g.r.activate(activateMsg{Kind: kindConn, Key: "prod"})), "\n"))

	if got := rec.asked(); len(got) != 1 {
		t.Errorf("the connection made %d lookups, want 1: %v", len(got), got)
	}

	want := "ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -l deploy -- prod.example.com"
	if calls := run.calls(); len(calls) != 1 || strings.Join(calls[0].Argv, " ") != want {
		t.Errorf("calls = %+v, want %q", calls, want)
	}

	before := len(rec.asked())

	g.send(playExec(t, settle(g, g.r.activate(activateMsg{Kind: kindConn, Key: "bastion"})), "\n"))

	if n := len(rec.asked()); n != before {
		t.Errorf("a lookup was made for mosh: %v", rec.asked())
	}

	if len(run.calls()) != 2 || run.calls()[1].Argv[0] != "mosh" {
		t.Errorf("calls = %+v", run.calls())
	}
}

// TestKeepaliveSettingOff checks that connect.keepalive = false stops the lookup for the three
// keys: nothing is asked, the signal guard is not armed for a lookup, and the commands are the
// base ones.
//
// Parameters:
//   - t: test to report to
func TestKeepaliveSettingOff(t *testing.T) {
	const plain = "ssh -l deploy -- prod.example.com"

	rec := &recorder{out: "serveraliveinterval 0\n"}
	run := &fakeRunner{}
	open := func() *rig {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Runner = run
			o.Probe = rec.probe
			o.Settings.Connect.Keepalive = false
		}))

		g.r.delay = 0

		return g
	}

	g := open()
	cmd := g.r.activate(activateMsg{Kind: kindConn, Key: "prod"})

	if g.r.guard.busy.Load() != 1 {
		t.Errorf("the guard is armed %d times, want once for the connection", g.r.guard.busy.Load())
	}

	g.send(playExec(t, cmd, "\n"))

	if calls := run.calls(); len(calls) != 1 || strings.Join(calls[0].Argv, " ") != plain {
		t.Errorf("calls = %+v, want %q", calls, plain)
	}

	g = open()
	g.press("C")

	if got, ok := clipboardOf(g); !ok || got != plain {
		t.Errorf("clipboard = %q (%v), want %q", got, ok, plain)
	}

	g = open()
	g.press("c")

	if g.r.result.Payload != plain {
		t.Errorf("prompt = %q, want %q", g.r.result.Payload, plain)
	}

	if got := rec.asked(); len(got) != 0 {
		t.Errorf("lookups with the setting off: %v", got)
	}
}

// TestKeepaliveBanner checks that the line printed before a connection starts, which stays on
// the screen when the connection fails, shows the command that really runs, options included.
//
// Parameters:
//   - t: test to report to
func TestKeepaliveBanner(t *testing.T) {
	rec := &recorder{out: "serveraliveinterval 0\nserveralivecountmax 3\n"}
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.Probe = rec.probe
	}))

	g.r.delay = 0

	var out bytes.Buffer

	cmd := settle(g, g.r.activate(activateMsg{Kind: kindConn, Key: "prod"}))
	g.send(playExecTo(t, cmd, "\n", &out))

	want := g.r.e.Glyphs.arrow + " ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -l " +
		"deploy -- prod.example.com"
	if !strings.Contains(out.String(), want) {
		t.Errorf("the banner does not show the real command:\n%s", out.String())
	}
}
