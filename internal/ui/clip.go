package ui

import (
	"context"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/shell"
)

const (
	// maxOSC is the longest text sent to the terminal as an OSC 52 sequence. Terminals cap the
	// sequence, and 100 KiB is far below the cap of the ones that have the most generous one.
	maxOSC = 100 * 1024

	// clipToolTimeout is the longest a clipboard program may take to read the text and take the
	// selection. The ones that keep the selection fork into the background and return at once.
	clipToolTimeout = 2 * time.Second
)

// clipTool is a program that puts its standard input in the clipboard.
//
// Name is looked up in PATH, Args are its fixed arguments and Needs the variable of the
// environment that says its display server is there: a Wayland or an X11 program is not tried
// without it, so that a session over ssh falls through to the sequence of the terminal.
type clipTool struct {
	name  string
	args  []string
	needs string
}

// clipTools lists the programs tried, in order, before the sequence of the terminal.
var clipTools = []clipTool{
	{"wl-copy", nil, "WAYLAND_DISPLAY"},
	{"xclip", []string{"-selection", "clipboard", "-in"}, "DISPLAY"},
	{"xsel", []string{"--clipboard", "--input"}, "DISPLAY"},
}

// ClipboardFunc puts a text in the clipboard with the programs of the system.
//
// Parameters:
//   - ctx: ends the attempt
//   - environ: environment of the interface, which decides the display server and the PATH
//   - text: what to copy
//
// Returns the name of the program that took the text, or an empty string when none did.
type ClipboardFunc func(ctx context.Context, environ []string, text string) string

// clipFound is a clipboard program that an environment can run: the entry of clipTools and the
// full name of the program.
type clipFound struct {
	tool clipTool
	path string
}

// clipCandidates lists the programs of clipTools that an environment can run, in the order they
// are tried: the display variable they need is set and the program is in an absolute directory
// of the PATH.
//
// Parameters:
//   - get: environment lookup, of which the display variables and PATH decide
func clipCandidates(get func(string) string) []clipFound {
	var found []clipFound

	for _, t := range clipTools {
		if get(t.needs) == "" {
			continue
		}

		if path, ok := shell.LookProgram(get("PATH"), t.name); ok {
			found = append(found, clipFound{tool: t, path: path})
		}
	}

	return found
}

// copyWithTools is the ClipboardFunc of the interface: it tries wl-copy, xclip and xsel in order
// and stops at the first that takes the text.
//
// Parameters:
//   - ctx: ends the attempt
//   - environ: environment of the interface
//   - text: what to copy
//
// Returns the name of the program that took the text, or an empty string when none did. A program
// is run from an absolute directory of the PATH of environ only, with fixed arguments, the text on
// its standard input and no output.
func copyWithTools(ctx context.Context, environ []string, text string) string {
	for _, c := range clipCandidates(envLookup(environ)) {
		if runTool(ctx, environ, c.path, c.tool.args, text) == nil {
			return c.tool.name
		}
	}

	return ""
}

// runTool runs a clipboard program on a text.
//
// Parameters:
//   - ctx: ends the program when done, clipToolTimeout being the other limit
//   - environ: environment of the program
//   - path: full name of the program
//   - args: its arguments
//   - text: what it reads on its standard input
func runTool(ctx context.Context, environ []string, path string, args []string, text string) error {
	ctx, cancel := context.WithTimeout(ctx, clipToolTimeout)
	defer cancel()

	// Standard output and error stay unset: a program that forks to keep the selection would
	// otherwise hold a pipe open and make Run wait for it.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = environ
	cmd.Stdin = strings.NewReader(text)

	return cmd.Run()
}

// copySelection starts the copy of the text selected with the mouse: the programs of the system
// first, then the sequence of the terminal, off the event loop.
//
// Parameters:
//   - text: what was selected, made of printable characters only
func (r *root) copySelection(text string) tea.Cmd {
	put := r.putInClipboard(text)

	return func() tea.Msg {
		return selCopiedMsg(put())
	}
}

// selCopied tells where a selection went, and puts it in the clipboard of the terminal when no
// program took it. The first time, the toast also tells how to select with the terminal itself,
// unless nothing could take the text: that message tells it already.
//
// Parameters:
//   - m: the text and the way it was copied
func (r *root) selCopied(m selCopiedMsg) tea.Cmd {
	e := r.e

	var (
		text string
		cmds []tea.Cmd
	)

	hint := r.hintPending
	r.hintPending = false

	switch {
	case m.Tool != "":
		text = e.copiedText(m.Chars, m.Tool)
	case m.OSC:
		cmds = append(cmds, tea.SetClipboard(m.Text))
		text = e.sentText(m.Chars)
	default:
		// The message already says how to select with the terminal, so the hint is not added.
		text = e.t("ui_sel_shown", strings.ReplaceAll(m.Text, "\n", " "))
		hint = false
	}

	if hint {
		text += " " + e.Glyphs.sep + " " + e.t("ui_sel_hint")
	}

	return tea.Batch(append(cmds, r.say(text, false))...)
}

// sentText words the message of a selection that was sent to the terminal as an OSC 52 sequence.
// The terminal gives no answer, so the message does not say that the text was copied.
//
// Parameters:
//   - n: number of characters
func (e *env) sentText(n int) string {
	if n == 1 {
		return e.t("ui_sel_sent_one")
	}

	return e.t("ui_sel_sent", n)
}

// copiedText words the message of a selection that was copied.
//
// Parameters:
//   - n: number of characters
//   - tool: name of the program or of the sequence that took them
func (e *env) copiedText(n int, tool string) string {
	if n == 1 {
		return e.t("ui_sel_copied_one", tool)
	}

	return e.t("ui_sel_copied", n, tool)
}
