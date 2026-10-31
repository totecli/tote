package ui

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/colorprofile"
)

// backgroundWait is how long the first frames stay neutral while the terminal is asked for its
// background color. A local terminal answers in 12 to 40 ms and a remote one in about a round
// trip, so this is the longest the interface waits before it guesses, see fallbackDark. An answer
// that comes later still replaces the guess.
const backgroundWait = 300 * time.Millisecond

// fallbackDark guesses whether the background of the terminal is dark when the terminal did not
// answer the query for it within backgroundWait.
//
// Parameters:
//   - get: environment lookup
//
// Returns the answer of COLORFGBG when it is set to something readable, the last field of the
// variable being the number of the background color (0 to 6 and 8 are dark, the others light),
// else true, a dark background being what most terminals have and what the libraries of the
// terminal assume when they cannot tell.
func fallbackDark(get func(string) string) bool {
	parts := strings.Split(get("COLORFGBG"), ";")

	if n, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1])); err == nil {
		return n < 7 || n == 8
	}

	return true
}

// backdrop is the kind of background that the terminal reported when it was asked for its
// background color.
type backdrop int

const (
	// backdropUnknown is the state before the terminal answered, and for good when it never
	// does: the guess of fallbackDark is not an answer.
	backdropUnknown backdrop = iota

	// backdropDark is a terminal that answered with a dark background.
	backdropDark

	// backdropLight is a terminal that answered with a light background.
	backdropLight
)

// backdropOf turns the answer of the terminal into a kind of background.
//
// Parameters:
//   - dark: true when the background color that the terminal reported is dark
func backdropOf(dark bool) backdrop {
	if dark {
		return backdropDark
	}

	return backdropLight
}

// tmuxProbeTimeout is the longest the tmux client features query may take.
const tmuxProbeTimeout = 300 * time.Millisecond

// envLookup builds a lookup function over a list of KEY=VALUE strings.
//
// Parameters:
//   - environ: environment as returned by os.Environ; the last entry of a key wins
//
// Returns a function that gives the value of a key, or an empty string when it is not set.
func envLookup(environ []string) func(string) string {
	vars := make(map[string]string, len(environ))

	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}

	return func(key string) string { return vars[key] }
}

// parseColorName reads the value of TOTE_COLOR.
//
// Parameters:
//   - name: value of the variable, any case
//
// Returns the profile and true for truecolor (24bit), 256, 16 (ansi) and none (mono), else the
// zero profile and false. The words never, off, no and false name the same profile as none, which
// is also what 0 means.
func parseColorName(name string) (colorprofile.Profile, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "truecolor", "24bit", "true":
		return colorprofile.TrueColor, true
	case "256", "ansi256":
		return colorprofile.ANSI256, true
	case "16", "ansi":
		return colorprofile.ANSI, true
	case "none", "mono", "0", "never", "off", "no", "false":
		return colorprofile.ASCII, true
	}

	return colorprofile.Unknown, false
}

// colorWord reads the words of TOTE_COLOR that choose how the colors are detected instead of
// naming a profile.
//
// Parameters:
//   - name: value of the variable, any case
//
// Returns "auto" for the detection that an unset variable gets, "always" for colors even when the
// terminal says it has none or NO_COLOR is set, and an empty string for any other value.
func colorWord(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "auto":
		return "auto"
	case "always", "on", "yes", "1":
		return "always"
	}

	return ""
}

// colorKnown reports whether TOTE_COLOR holds a value that is understood: a profile or one of
// the words of colorWord.
//
// Parameters:
//   - name: value of the variable
func colorKnown(name string) bool {
	_, ok := parseColorName(name)

	return ok || colorWord(name) != ""
}

// UnknownColor reports a value of TOTE_COLOR that is not understood, so that the command line and
// the interface can say that the interface falls back to the automatic detection.
//
// Parameters:
//   - environ: process environment in KEY=VALUE form
//
// Returns the value, cut to 40 cells and with its control characters removed, and true when
// TOTE_COLOR is set to something that is not understood. An unset or empty variable is not an
// error. Nothing is probed, so the call is instant.
func UnknownColor(environ []string) (string, bool) {
	v := envLookup(environ)("TOTE_COLOR")
	if v == "" {
		return "", false
	}

	if colorKnown(v) {
		return "", false
	}

	v = sanitize(v)

	return newMetric().fit(v, 40, ""), true
}

// mouseTerm reports whether the terminal can be asked for mouse events.
//
// Parameters:
//   - get: environment lookup
//
// Returns false when TERM is unset, "dumb" or "linux": a dumb terminal has no mouse, and the
// Linux console only reports it through a daemon that does not speak the xterm protocol. Every
// other terminal gets the xterm mouse protocol with SGR coordinates, which all the terminals and
// multiplexers in use understand. The glyph set does not matter: a terminal in the C locale
// still has a mouse.
func mouseTerm(get func(string) string) bool {
	switch get("TERM") {
	case "", "dumb", "linux":
		return false
	}

	return true
}

// detectProfile decides how many colors the interface may use.
//
// Parameters:
//   - environ: process environment
//   - out: terminal output, used by the library to tell a terminal from a file
//   - probe: returns the client features reported by tmux, nil to skip the tmux hint
//
// Returns TOTE_COLOR when it names a profile, else the ASCII profile (no color, text styles
// kept) when NO_COLOR is not empty, else what the library detects from TERM, COLORTERM and the
// output. A detection of 256 colors inside tmux is raised to TrueColor when tmux says that
// its client has RGB, because the library cannot see that through TERM=tmux. TOTE_COLOR=always
// detects the same way without NO_COLOR, and gives 256 colors when nothing is detected.
func detectProfile(environ []string, out io.Writer, probe func() string) colorprofile.Profile {
	get := envLookup(environ)

	if p, ok := parseColorName(get("TOTE_COLOR")); ok {
		return p
	}

	always := colorWord(get("TOTE_COLOR")) == "always"

	switch {
	case always:
		environ = slices.DeleteFunc(slices.Clone(environ), func(kv string) bool {
			return strings.HasPrefix(kv, "NO_COLOR=")
		})
	case get("NO_COLOR") != "":
		return colorprofile.ASCII
	}

	p := colorprofile.Detect(out, environ)

	if p == colorprofile.ANSI256 && get("TMUX") != "" && probe != nil {
		if hasFeature(probe(), "RGB") {
			return colorprofile.TrueColor
		}
	}

	if always && !colors(p) {
		return colorprofile.ANSI256
	}

	return p
}

// hasFeature reports whether a comma separated feature list holds a name.
//
// Parameters:
//   - list: features as printed by tmux
//   - name: feature to look for, compared exactly
func hasFeature(list, name string) bool {
	for _, f := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' }) {
		if f == name {
			return true
		}
	}

	return false
}

// tmuxFeatures asks the tmux client for its terminal features.
//
// Returns the comma separated list, or an empty string when tmux does not answer in time.
func tmuxFeatures() string {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxProbeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p",
		"#{client_termfeatures}").Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// utf8Locale reports whether the locale of the environment is UTF-8.
//
// Parameters:
//   - get: environment lookup
//
// Returns true when the first of LC_ALL, LC_CTYPE and LANG that is set names a UTF-8 charset.
// A missing locale, "C" and "POSIX" are not UTF-8.
func utf8Locale(get func(string) string) bool {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.ToLower(get(key)); v != "" {
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}

	return false
}

// useASCII decides whether the plain ASCII glyph set must be used.
//
// Parameters:
//   - forced: true when the user asked for it with --ascii
//   - get: environment lookup
//
// Returns true when forced, when TERM is "linux" or "dumb", or when the locale is not UTF-8.
func useASCII(forced bool, get func(string) string) bool {
	if forced {
		return true
	}

	switch get("TERM") {
	case "linux", "dumb":
		return true
	}

	return !utf8Locale(get)
}

// colors reports whether a profile can show colors.
//
// Parameters:
//   - p: color profile
func colors(p colorprofile.Profile) bool {
	return p >= colorprofile.ANSI
}

// wrapperCommand is the line that enables the shell wrapper, which the user is told to run.
//
// Parameters:
//   - get: environment lookup
//
// Returns the eval line of "tote init" for zsh when SHELL names zsh, and for bash otherwise,
// since bash and zsh are the shells that "tote init" supports.
func wrapperCommand(get func(string) string) string {
	name := "bash"
	if filepath.Base(get("SHELL")) == "zsh" {
		name = "zsh"
	}

	return "eval \"$(tote init " + name + ")\""
}
