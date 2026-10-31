package ui

import (
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/colorprofile"

	"github.com/totecli/tote/internal/config"
)

// Terminal describes how the interface would draw on a terminal, for a doctor command.
//
// Color is "truecolor", "256", "16" or "none". Override is "TOTE_COLOR" or "NO_COLOR" when one of
// them decided the colors, else empty, and BadColor is true when TOTE_COLOR holds a value that
// names no profile. ASCII is true when the plain glyph set is used, UTF8 tells whether the locale
// is UTF-8 and Dumb whether TERM is unset or "dumb".
type Terminal struct {
	Color    string
	Override string
	BadColor bool
	ASCII    bool
	UTF8     bool
	Dumb     bool
}

// DescribeTerminal tells what the interface would choose in a terminal with a given environment.
//
// The answer is the one of a terminal output whatever the real output is, so that a doctor
// command whose output is a pipe still describes the terminal. Nothing is written.
//
// Parameters:
//   - environ: process environment in KEY=VALUE form
//   - forceASCII: true when --ascii was given
//   - probe: returns the client features reported by tmux, nil for the real tmux query, which
//     only runs inside tmux
//
// Returns the description.
func DescribeTerminal(environ []string, forceASCII bool, probe func() string) Terminal {
	get := envLookup(environ)
	t := Terminal{
		ASCII: useASCII(forceASCII, get),
		UTF8:  utf8Locale(get),
		Dumb:  get("TERM") == "" || get("TERM") == "dumb",
	}

	if probe == nil {
		probe = tmuxFeatures
	}

	forced := append(slices.Clone(environ), "CLICOLOR_FORCE=1")
	p := detectProfile(forced, io.Discard, probe)

	_, fixed := parseColorName(get("TOTE_COLOR"))
	word := colorWord(get("TOTE_COLOR"))

	switch {
	case fixed || word == "always":
		t.Override = "TOTE_COLOR"
	case get("TOTE_COLOR") != "" && word == "":
		t.BadColor = true
	case get("NO_COLOR") != "":
		t.Override = "NO_COLOR"
	}

	if t.BadColor {
		t.Override = ""
		p = detectProfile(slices.DeleteFunc(slices.Clone(forced), isToteColor), io.Discard, probe)
	}

	t.Color = profileName(p, t.Dumb && t.Override == "")

	return t
}

// isToteColor reports whether an environment entry sets TOTE_COLOR.
//
// Parameters:
//   - kv: entry in KEY=VALUE form
func isToteColor(kv string) bool {
	return strings.HasPrefix(kv, "TOTE_COLOR=")
}

// profileName names a color profile for a report.
//
// Parameters:
//   - p: profile chosen by detectProfile
//   - dumb: true when the terminal is unset or dumb, which has no colors whatever the library
//     was told
func profileName(p colorprofile.Profile, dumb bool) string {
	switch {
	case dumb:
		return "none"
	case p == colorprofile.TrueColor:
		return "truecolor"
	case p == colorprofile.ANSI256:
		return "256"
	case p == colorprofile.ANSI:
		return "16"
	}

	return "none"
}

// LayoutName names the layout the interface uses on a terminal of a given size.
//
// Parameters:
//   - w: width in cells
//   - h: height in rows
//   - s: settings, of which the [ui] table decides between the full and the compact layout
//
// Returns "full", "compact" or "tiny", the last one meaning that only a request for a bigger
// terminal is shown.
func LayoutName(w, h int, s config.Settings) string {
	switch chooseLayout(w, h, s.UI) {
	case layoutFull:
		return "full"
	case layoutCompact:
		return "compact"
	}

	return "tiny"
}

// MinSize gives the smallest terminal that shows the interface; a smaller one gets only the screen
// that asks for a bigger window.
//
// Returns the width in cells and the height in rows.
func MinSize() (int, int) {
	return minWidth, minHeight
}

// MouseState tells whether the interface would use the mouse, for a doctor command.
//
// Parameters:
//   - s: settings, of which ui.mouse decides
//   - get: environment lookup, of which TERM decides
//
// Returns "setting" when ui.mouse is false, else "term" when the terminal cannot report the
// mouse (TERM unset, "dumb" or "linux"), else "on".
func MouseState(s config.Settings, get func(string) string) string {
	switch {
	case !s.UI.Mouse:
		return "setting"
	case !mouseTerm(get):
		return "term"
	}

	return "on"
}

// ClipboardRoute tells how a copy would reach the clipboard in an environment, for a doctor
// command. It follows the chain of the interface without running anything: the clipboard
// programs first, then the sequence of the terminal.
//
// Parameters:
//   - get: environment lookup, of which the display variables, PATH, TERM and the terminal
//     variables decide
//   - tmuxOption: returns the value of the tmux option set-clipboard, called only inside tmux;
//     nil for the real query
//
// Returns "tool" and the name of the first program that the environment can run (wl-copy, xclip
// or xsel, found in an absolute directory of PATH with the display variable it needs), else "osc"
// and an empty name when the terminal can receive an OSC 52 sequence, else "none" and an empty
// name, when a selection can only be shown.
func ClipboardRoute(get func(string) string, tmuxOption func() string) (string, string) {
	if found := clipCandidates(get); len(found) > 0 {
		return "tool", found[0].tool.name
	}

	if tmuxOption == nil {
		tmuxOption = tmuxClipboard
	}

	if clipboardUsable(get, tmuxOption) {
		return "osc", ""
	}

	return "none", ""
}
