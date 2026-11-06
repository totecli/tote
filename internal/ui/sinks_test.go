package ui

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// allCanaries joins the raw bytes of every canary of the injection tests.
func allCanaries() string {
	var b strings.Builder

	for _, k := range canaries {
		b.WriteString(k.raw)
	}

	return b.String()
}

// auditFrame checks one frame of the rig and reports what reached it.
//
// Parameters:
//   - t: running test
//   - what: description of the step, for the report
//   - frame: the rendered screen
func auditFrame(t *testing.T, what, frame string) {
	t.Helper()

	for _, p := range auditStream(frame) {
		t.Errorf("%s: %s", what, p)
	}
}

// TestReviewInjectionSinks drives every record of the hostile files through the keys that print
// its name in a message (activate, edit, delete, delete confirmed, editor), through the messages
// about records that do not exist, and through the user name, the version and the update notice,
// and audits the frame after each step. TestInjectionFrames only draws lists, details, forms and
// the palette; the messages built from a key or from an error text were not reached by it.
//
// Parameters:
//   - t: test to report to
func TestReviewInjectionSinks(t *testing.T) {
	bad := allCanaries()

	for _, k := range allKinds {
		probe := hostileSandbox(t)
		count := newRig(t, probe).r.e.Data.count(k)

		if count < 20 {
			t.Fatalf("kind %d: only %d hostile records", k, count)
		}

		start := func(o *Options) {
			o.Start = Start(int(k) + 1)
			o.User, o.Version = "u"+bad, "v"+bad
			o.Width, o.Height = 110, 30
		}

		g := newRig(t, hostileSandbox(t).with(start))

		// Even rows are the records whose name holds the canary, odd rows have a clean name; every
		// sixth row is enough to reach each sink with a different canary.
		for i := 0; i < count; i += 6 {
			g.press("home")

			for j := 0; j < i; j++ {
				g.press("down")
			}

			for _, steps := range [][]string{{"enter"}, {"o"}, {"e", "esc"}, {"d", "esc"}} {
				if steps[0] == "o" && k != kindRepo {
					continue
				}

				for _, step := range steps {
					g.press(step)
					auditFrame(t, fmt.Sprintf("kind %d row %d after %v", k, i, steps),
						g.r.render())
				}

				// A hostile record that opened nothing may have left a message on screen.
				g.r.overlays = nil
			}

			d := newRig(t, hostileSandbox(t).with(start))

			for j := 0; j < i; j++ {
				d.press("down")
			}

			for _, step := range []string{"d", "y"} {
				d.press(step)
				auditFrame(t, fmt.Sprintf("kind %d row %d delete %s", k, i, step), d.r.render())
			}
		}

		m := newRig(t, hostileSandbox(t).with(func(o *Options) { o.Start = Start(int(k) + 1) }))

		for _, msg := range []tea.Msg{
			activateMsg{Kind: k, Key: bad},
			editMsg{Kind: k, Key: bad},
			deleteMsg{Kind: k, Key: bad},
			editorMsg{Key: bad},
			wroteMsg{Kind: k, Key: bad, Op: opAdd},
			wroteMsg{Kind: k, Key: bad, Op: opDelete},
		} {
			m.send(msg)
			auditFrame(t, fmt.Sprintf("kind %d message %T", k, msg), m.r.render())
		}
	}

	home := newRig(t, hostileSandbox(t).with(func(o *Options) {
		o.User, o.Version = "u"+bad, "v"+bad
	}))

	auditFrame(t, "home with a hostile user", home.r.render())
	home.send(updateMsg{Version: "9" + bad})
	auditFrame(t, "update notice", home.r.render())
	home.press("1")
	home.send(updateMsg{Version: "9" + bad})
	auditFrame(t, "update notice on a list", home.r.render())
}

// TestReviewInjectionExecOutcome checks the line that asks for Enter after a failed program and
// the message that follows, with an error text and a signal name that hold escape sequences.
//
// Parameters:
//   - t: test to report to
func TestReviewInjectionExecOutcome(t *testing.T) {
	bad := allCanaries()

	for _, res := range []RunResult{
		{ExitCode: 127, Err: errors.New("cannot start " + bad)},
		{ExitCode: 130, Signal: "interrupt " + bad},
	} {
		run := &fakeRunner{Res: res}
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Runner = run
		}))

		g.r.delay = 0

		var out strings.Builder

		msg := playExecTo(t, settle(g, g.r.activate(activateMsg{Kind: kindConn, Key: "prod"})),
			"\n", &out)

		auditFrame(t, "pause line", out.String())

		g.send(msg)
		auditFrame(t, "message after the program", g.r.render())

		if g.toastText() == "" {
			t.Errorf("no message after %+v", res)
		}
	}

	g := newRig(t, newSandbox(t))
	g.send(execDoneMsg{Label: "x", Restore: errors.New("restore " + bad)})
	auditFrame(t, "restore failure", g.r.render())
}

// TestReviewInjectionBrokenFile checks the reason shown for a file that cannot be read when the
// file holds escape sequences where the parser fails.
//
// Parameters:
//   - t: test to report to
func TestReviewInjectionBrokenFile(t *testing.T) {
	bad := allCanaries()

	for name, content := range map[string]string{
		"value":  "schema = 1\n[[alias]]\nname = " + bad + "\n",
		"key":    "schema = 1\n" + bad + " = 1\n",
		"table":  "schema = 1\n[[" + bad + "]]\n",
		"string": "schema = 1\n[[alias]]\nname = \"" + bad + "\n",
	} {
		s := newSandbox(t)
		writeFile(t, s.Opts.Paths.AliasesFile(), content)

		g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))

		auditFrame(t, name+": list", g.r.render())
		g.press("n")
		auditFrame(t, name+": new", g.r.render())
		g.press("e")
		g.press("d")
		auditFrame(t, name+": edit and delete", g.r.render())
	}
}

// playExecTo is playExec with the output of the program kept.
//
// Parameters:
//   - t: running test
//   - cmd: command returned by the model for a program to run
//   - in: text the program may read
//   - out: receives what the command writes to the terminal
func playExecTo(t *testing.T, cmd tea.Cmd, in string, out io.Writer) tea.Msg {
	t.Helper()

	return playExecWith(t, cmd, in, out)
}
