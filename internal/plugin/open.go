package plugin

import (
	"net/url"
	"runtime"
	"strings"
)

// Limits of an open contribution.
const maxOpenURL = 2048

// desktopEnv lists the variables the platform opener needs to find the user's session. The opener
// is run by the host with a URL it checked, not by the plugin.
var desktopEnv = []string{
	"PATH", "HOME", "USER", "LOGNAME", "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_MESSAGES",
	"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR",
	"XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE", "XDG_DATA_DIRS", "XDG_CONFIG_HOME", "DESKTOP_SESSION",
	"BROWSER", "TERM",
}

// OpenerName gives the program that opens a URL on an operating system: xdg-open on Linux, open
// on macOS.
//
// Parameters:
//   - goos: runtime.GOOS value
func OpenerName(goos string) string {
	if goos == "darwin" {
		return "open"
	}

	return "xdg-open"
}

// CheckOpenURL checks the URL an open contribution produced: https, a host, no user information,
// no space or control character, at most 2048 bytes.
//
// Parameters:
//   - raw: URL after substitution
//   - host: host written literally in the template, empty when the template leaves it to a
//     placeholder; the URL must have exactly this host, so that no value can move the link
//
// Returns the URL as it will be shown and opened, or an *Error with the rule it breaks.
func CheckOpenURL(raw, host string) (string, *Error) {
	bad := func(rule string) (string, *Error) {
		return "", newError(CodeOpenInvalid, rule)
	}

	if len(raw) > maxOpenURL || !printableASCII(raw) || strings.Contains(raw, `\`) {
		return bad("text")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return bad("syntax")
	}

	switch {
	case u.Scheme != "https" || !strings.HasPrefix(raw, "https://"):
		return bad("scheme")
	case u.User != nil || strings.Contains(u.Host, "@"):
		return bad("userinfo")
	case u.Hostname() == "" || strings.HasPrefix(u.Hostname(), "-") ||
		!remoteHostRe.MatchString(u.Host):
		return bad("host")
	case host != "" && !strings.EqualFold(u.Host, host):
		return bad("host")
	}

	return raw, nil
}

// literalHost reads the host from the literal start of an open template, "https://host/..." or
// "https://host" followed by a placeholder-free end. It gives the empty text when a placeholder is
// inside the host.
//
// Parameters:
//   - t: parsed template
func literalHost(t Template) string {
	if len(t.Parts) == 0 || t.Parts[0].Holder != nil {
		return ""
	}

	rest, ok := strings.CutPrefix(t.Parts[0].Text, "https://")
	if !ok {
		return ""
	}

	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		return rest[:i]
	}

	if len(t.Parts) > 1 {
		return ""
	}

	return rest
}

// openURL substitutes the open template of the contribution and checks the result.
func (pc *prep) openURL() (string, *Error) {
	t := pc.tmpl[pc.e.Open]

	raw, keep, err := ExpandScalar(t, pc.vals, maxOpenURL)
	if err != nil {
		return "", err
	}

	if !keep {
		return "", newError(CodeUnavailable, "open", "empty")
	}

	return CheckOpenURL(raw, literalHost(t))
}

// openerPath finds the platform opener on the scrubbed PATH.
func (pc *prep) openerPath() (string, *Error) {
	name := OpenerName(runtime.GOOS)

	path, ok := LookBin(pc.ctx.pathEnv, name, pc.ctx.avoid)
	if !ok {
		return "", newError(CodeDepMissing, name, "")
	}

	return path, nil
}
