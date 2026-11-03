package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/shell"
)

// TestClipboardUsable checks which terminals are trusted with an OSC 52 sequence.
//
// Parameters:
//   - t: test to report to
func TestClipboardUsable(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		tmux string
		want bool
	}{
		{"xterm", map[string]string{"TERM": "xterm-256color"}, "", true},
		{"kitty", map[string]string{"TERM": "xterm-kitty"}, "", true},
		{"unset", map[string]string{}, "", false},
		{"dumb", map[string]string{"TERM": "dumb"}, "", false},
		{"linux console", map[string]string{"TERM": "linux"}, "", false},
		{"apple terminal", map[string]string{
			"TERM":         "xterm-256color",
			"TERM_PROGRAM": "Apple_Terminal",
		}, "", false},
		{"screen", map[string]string{"TERM": "screen-256color"}, "", false},
		{"tmux on", map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/x,1,0"}, "on", true},
		{
			"tmux external",
			map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/x,1,0"},
			"external", false,
		},
		{
			"tmux off",
			map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/x,1,0"},
			"off",
			false,
		},
		{
			"tmux silent",
			map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/x,1,0"},
			"",
			false,
		},
	}

	for _, tt := range tests {
		asked := false
		got := clipboardUsable(func(k string) string { return tt.env[k] }, func() string {
			asked = true

			return tt.tmux
		})

		if got != tt.want {
			t.Errorf("%s: usable = %v, want %v", tt.name, got, tt.want)
		}

		term := tt.env["TERM"]
		inTmux := tt.env["TMUX"] != "" && term != "dumb" && term != "linux" && term != "" &&
			tt.env["TERM_PROGRAM"] == ""

		if asked != inTmux {
			t.Errorf("%s: tmux asked = %v", tt.name, asked)
		}
	}
}

// clipboardOf returns the text of the clipboard command among the messages a rig has seen.
//
// Parameters:
//   - g: rig that played the keys
func clipboardOf(g *rig) (string, bool) {
	for _, m := range g.Seen {
		if strings.Contains(fmt.Sprintf("%T", m), "setClipboard") {
			return fmt.Sprint(m), true
		}
	}

	return "", false
}

// TestCopyRecords checks what the key C puts in the clipboard for each kind, that the terminal
// is told with an OSC 52 command, and that the toast shows the text.
//
// Parameters:
//   - t: test to report to
func TestCopyRecords(t *testing.T) {
	tests := []struct {
		name  string
		start Start
		keys  []string
		want  string
	}{
		{"connection", StartConnections, []string{"C"}, "ssh -l deploy -- prod.example.com"},
		{
			"connection as another user", StartConnections,
			[]string{"u", "C"},
			"ssh -l root -- prod.example.com",
		},
		{
			"connection with a via", StartConnections,
			[]string{"down", "down", "C"},
			"mosh ops@bastion.example.com",
		},
		{"alias", StartAliases, []string{"C"}, "ls -la"},
		{"function", StartFuncs, []string{"C"}, ""},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = tt.start }))
		g.press(tt.keys...)

		got, ok := clipboardOf(g)
		if !ok {
			t.Errorf("%s: nothing was put in the clipboard (toast %q)", tt.name, g.toastText())
			continue
		}

		if tt.want != "" && got != tt.want {
			t.Errorf("%s: clipboard = %q, want %q", tt.name, got, tt.want)
		}

		if !strings.Contains(g.toastText(), "Sent to the terminal clipboard (OSC 52): ") ||
			!strings.Contains(g.toastText(), got) {
			t.Errorf("%s: toast = %q", tt.name, g.toastText())
		}
	}

	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartRepos }))
	g.press("down", "C")

	if got, _ := clipboardOf(g); got != g.r.e.Home+"/missing/docs" {
		t.Errorf("repository: clipboard = %q, want the path with ~ expanded", got)
	}
}

// TestCopyFallback checks that a terminal that cannot receive OSC 52 gets no sequence, and that
// the toast shows the text so that it can be selected.
//
// Parameters:
//   - t: test to report to
func TestCopyFallback(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		tmux string
	}{
		{"dumb", []string{"TERM=dumb"}, ""},
		{"linux", []string{"TERM=linux"}, ""},
		{"tmux without passthrough", []string{"TERM=tmux-256color", "TMUX=/tmp/x,1,0"}, "off"},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Environ = append(append([]string(nil), o.Environ...), tt.env...)
			o.TmuxClipboard = func() string { return tt.tmux }
		}))
		g.press("C")

		if got, ok := clipboardOf(g); ok {
			t.Errorf("%s: a clipboard command was sent: %q", tt.name, got)
		}

		if tt := g.toastText(); !strings.Contains(tt, "press v to select") {
			t.Errorf("toast = %q, want the way to the text to select", tt)
		}

		g.press("v")

		if !strings.Contains(g.text(), "ssh -l deploy -- prod.example.com") {
			t.Errorf("%s: the details do not hold the text to select:\n%s", tt.name, g.text())
		}
	}

	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.Environ = append(append([]string(nil), o.Environ...), "TERM=tmux-256color",
			"TMUX=/tmp/x,1,0")
		o.TmuxClipboard = func() string { return "on" }
	}))
	g.press("C")

	if _, ok := clipboardOf(g); !ok {
		t.Error("tmux with set-clipboard on: nothing sent")
	}
}

// TestCopyRefusals checks that nothing is put in the clipboard when there is nothing to copy,
// when the text holds a control character, when it is too long, and when the record cannot be
// used.
//
// Parameters:
//   - t: test to report to
func TestCopyRefusals(t *testing.T) {
	g := newRig(t, newSandbox(t))

	for _, tt := range []struct {
		name string
		text string
		want string
	}{
		{"empty", "", "nothing to copy"},
		{"escape", "echo \x1b]52;c;aGk=\x07", "control character"},
		{"newline", "echo a\nrm b", "control character"},
		{"c1", "echo \u009b31m", "control character"},
		{"bidi", "echo \u202ehi", "control character"},
		{"invalid utf-8", "echo \xff", "control character"},
	} {
		g.Seen = nil
		g.r.toast = toast{}

		g.r.copyText(tt.text)

		// A refusal is answered at once by a toast, whose timer is the only command.
		_, ok := clipboardOf(g)
		if ok || !strings.Contains(g.toastText(), tt.want) || !g.r.toast.err {
			t.Errorf("%s: clipboard sent %v, toast %q", tt.name, ok, g.toastText())
		}
	}

	g.Seen = nil
	long := strings.Repeat("a", maxOSC+1)

	g.send(g.r.copyText(long)())

	if _, ok := clipboardOf(g); ok || !strings.Contains(g.toastText(), "press v to select") {
		t.Errorf("a text of %d bytes: clipboard %v, toast %.40q", len(long), ok, g.toastText())
	}

	g = newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	g.press("down", "down", "down", "C")

	if _, ok := clipboardOf(g); ok || !g.r.toast.err {
		t.Errorf("a connection that needs review: clipboard %v, toast %q", ok, g.toastText())
	}

	g.send(copyMsg{Kind: kindConn, Key: "no-such-record"})

	if !g.r.toast.err {
		t.Errorf("an unknown record: toast %q", g.toastText())
	}
}

// TestCommandMode checks the key c: the interface closes with the command at the prompt, as the
// command line does, and a login chosen with u is part of it.
//
// Parameters:
//   - t: test to report to
func TestCommandMode(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	g.press("c")

	if !g.Quit || g.r.result.Action != shell.Insert ||
		g.r.result.Payload != "ssh -l deploy -- prod.example.com" {
		t.Errorf("quit %v, result %+v", g.Quit, g.r.result)
	}

	g = newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	g.press("u", "c")

	if g.r.result.Payload != "ssh -l root -- prod.example.com" {
		t.Errorf("result with another user: %+v", g.r.result)
	}

	g = newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	g.press("down", "down", "down", "c")

	if g.Quit || !g.r.toast.err {
		t.Errorf("a connection that needs review: quit %v, toast %q", g.Quit, g.toastText())
	}

	g = newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartAliases }))
	g.press("c")

	if g.Quit {
		t.Error("c on an alias list closed the interface")
	}
}

// TestCopyFallbackDetails checks, for each kind, that the details opened by the key the fallback
// toast points to hold the text that was to be copied, so that it can be selected there.
//
// Parameters:
//   - t: test to report to
func TestCopyFallbackDetails(t *testing.T) {
	tests := []struct {
		name  string
		start Start
		keys  []string
		want  string
	}{
		{"connection", StartConnections, []string{"C"}, "ssh -l deploy -- prod.example.com"},
		{"alias", StartAliases, []string{"C"}, "ls -la"},
		{"repository", StartRepos, []string{"down", "C"}, "/missing/docs"},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = tt.start
			o.Environ = append(append([]string(nil), o.Environ...), "TERM=linux")
		}))
		g.press(tt.keys...)

		if !strings.Contains(g.toastText(), "press v to select") {
			t.Errorf("%s: toast = %q", tt.name, g.toastText())
		}

		g.press("v")

		if !strings.Contains(g.text(), tt.want) {
			t.Errorf("%s: the details lack %q:\n%s", tt.name, tt.want, g.text())
		}
	}
}
