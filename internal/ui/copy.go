package ui

import (
	"context"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// clipboardProbeTimeout is the longest the query of the tmux option may take.
const clipboardProbeTimeout = 300 * time.Millisecond

// tmuxClipboard asks tmux whether it lets programs set the clipboard.
//
// Returns the value of the server option set-clipboard ("on", "external" or "off"), or an empty
// string when tmux does not answer in time. Only "on" passes the sequence of a program on.
func tmuxClipboard() string {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardProbeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tmux", "show-options", "-sv", "set-clipboard").Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// clipboardUsable tells whether the terminal can receive an OSC 52 sequence.
//
// Parameters:
//   - get: environment lookup
//   - tmuxOption: returns the value of the tmux option set-clipboard, called only inside tmux
//
// Returns false for a terminal that is unset, dumb or the Linux console, for the Apple terminal
// and the terminals built on VTE (GNOME Terminal, Tilix, Xfce Terminal), which ignore the
// sequence, for GNU screen, and inside tmux unless it is set to pass the sequences of programs
// on. Every other terminal is trusted: the sequence is a silent no-op where it is not understood,
// which is why the message that follows it says that the text was sent and not that it was copied.
func clipboardUsable(get func(string) string, tmuxOption func() string) bool {
	term := get("TERM")

	switch {
	case term == "" || term == "dumb" || term == "linux":
		return false
	case get("TERM_PROGRAM") == "Apple_Terminal" || get("VTE_VERSION") != "":
		return false
	case get("TMUX") != "":
		return tmuxOption() == "on"
	case strings.HasPrefix(term, "screen"):
		return false
	}

	return true
}

// copyText starts the copy of a text to the clipboard. The text is checked first: it must be
// exactly what it would be after sanitize, so a command that holds a control character is never
// put in a clipboard to be pasted in a shell. The clipboard programs of the system are then tried,
// and the sequence of the terminal after them, see putInClipboard.
//
// Parameters:
//   - text: what to copy
func (r *root) copyText(text string) tea.Cmd {
	e := r.e

	switch {
	case text == "":
		return r.say(e.t("ui_copy_nothing"), true)
	case sanitize(text) != text:
		return r.say(e.t("ui_copy_unsafe"), true)
	}

	put := r.putInClipboard(text)

	return func() tea.Msg {
		return copyReadyMsg(put())
	}
}

// putInClipboard prepares the copy of a text and decides where it can go. It is the one chain
// that the keys C and the selection by mouse share.
//
// Parameters:
//   - text: what to copy, made of printable characters only
//
// Returns a function to run off the event loop. It tries wl-copy (when WAYLAND_DISPLAY is set),
// then xclip and xsel (when DISPLAY is set), with the text on their standard input, found in the
// absolute directories of the PATH of the interface, two seconds each at most, no shell. When no
// program takes the text it tells whether the terminal can receive an OSC 52 sequence, which
// holds up to maxOSC bytes; when it cannot either, the text can only be shown.
func (r *root) putInClipboard(text string) func() clipped {
	e := r.e
	get, option, tools := envLookup(e.Environ), r.tmuxClip, r.clip

	return func() clipped {
		c := clipped{Text: text, Chars: utf8.RuneCountInString(text)}

		if c.Tool = tools(context.Background(), e.Environ, text); c.Tool == "" {
			c.OSC = len(text) <= maxOSC && clipboardUsable(get, option)
		}

		return c
	}
}

// copyReady tells where a text went, and puts it in the clipboard of the terminal when no program
// took it and the terminal can receive it. When nothing can take it the toast shows the text, with
// the key that opens the details of the record where it can be selected.
//
// Parameters:
//   - m: the text and the way it was copied
func (r *root) copyReady(m copyReadyMsg) tea.Cmd {
	e := r.e
	text := strings.ReplaceAll(m.Text, "\n", " ")

	switch {
	case m.Tool != "":
		return r.say(e.t("ui_copied_with", m.Tool, text), false)
	case m.OSC:
		return tea.Batch(tea.SetClipboard(m.Text), r.say(e.t("ui_copy_sent", text), false))
	}

	return r.say(e.t("ui_copy_manual", sanitize(e.Keys.View.Help().Key), text), false)
}

// copyRecord copies what a record stands for: the command of a connection, the path of a
// repository, the command line of an alias or the name of a function.
//
// Parameters:
//   - m: kind, key and login of the record
func (r *root) copyRecord(m copyMsg) tea.Cmd {
	e := r.e

	rec, ok := e.Data.record(m.Kind, m.Key)
	if !ok {
		return r.say(e.t("cli_not_found", e.Data.kindName(m.Kind), sanitize(m.Key)), true)
	}

	switch v := rec.(type) {
	case store.Connection:
		argv, why := r.argvOf(v, m.User)
		if why != "" {
			return r.say(why, true)
		}

		if wait := r.withKeepalive(argv, useCopy, after{}); wait != nil {
			return wait
		}

		return r.copyText(shell.PrettyCommand(argv))
	case store.Repo:
		return r.copyText(shell.ExpandHome(v.Path, e.Home))
	case store.Alias:
		return r.copyText(v.Cmd)
	case store.Func:
		return r.copyText(v.Name)
	}

	return nil
}

// commandMode leaves the interface with the command of a connection to put at the prompt of the
// shell, where the user can read it, change it and run it.
//
// Parameters:
//   - m: key and login of the connection
func (r *root) commandMode(m commandMsg) tea.Cmd {
	e := r.e

	rec, ok := e.Data.record(kindConn, m.Key)
	if !ok {
		return r.say(e.t("cli_not_found", e.Data.kindName(kindConn), sanitize(m.Key)), true)
	}

	argv, why := r.argvOf(rec.(store.Connection), m.User)
	if why != "" {
		return r.say(why, true)
	}

	if wait := r.withKeepalive(argv, useInsert, after{}); wait != nil {
		return wait
	}

	return r.leaveWith(shell.Insert, shell.PrettyCommand(argv))
}
