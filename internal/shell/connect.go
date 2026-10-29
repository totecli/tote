package shell

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/totecli/tote/internal/store"
)

var (
	// ErrNeedsReview is wrapped by the error of ConnectArgv for a connection that the importer
	// flagged as ambiguous and that the user has not edited yet.
	ErrNeedsReview = errors.New("connection needs review")

	// ErrUserOverride is wrapped by the error of ConnectArgv when a user override is given for a
	// via command that has no place for a user.
	ErrUserOverride = errors.New("user override not allowed for this connection")

	// ErrMissing is wrapped by the error of ConnectArgv when a template uses a placeholder whose
	// value the connection does not have.
	ErrMissing = errors.New("connection lacks a value required by its command")
)

// plainSSH is the verb used when a connection has no via command.
const plainSSH = "ssh"

// ConnectArgv builds the command line that opens a connection.
//
// A via that is empty or exactly "ssh" gives ssh, then "-l" and the user when there is one, then
// "--" and the target, so that a hostile target can never be read as an option. A via without
// braces keeps its words as they are and receives the target as last argument, prefixed with
// "user@" when there is a user. A via with braces is a template: {user@}, {user}, {host}, {ip}
// and {name} are replaced in every word and nothing is appended. An empty target stands for the
// connection name. Name, via, target, user and, for a template that uses it, ip are checked again
// here with the validators of the store, so a target or a user that starts with '-' never reaches
// a command line. For plain ssh a target written as a bracketed IPv6 address loses its brackets,
// which ssh does not accept on its command line.
//
// Parameters:
//   - c: connection to open
//   - userOverride: user chosen for this run, empty to use the stored one; accepted only for
//     plain ssh, for a via made of a single word, or for a template that contains {user
//
// Returns the argument vector, whose first element is the command, or an error wrapping
// ErrNeedsReview, ErrUserOverride, ErrMissing or store.ErrInvalidField.
func ConnectArgv(c store.Connection, userOverride string) ([]string, error) {
	if c.NeedsReview {
		return nil, fmt.Errorf("%w: %q", ErrNeedsReview, c.Name)
	}

	user := c.User

	if userOverride != "" {
		if !acceptsUser(c.Via) {
			return nil, fmt.Errorf("%w: %q", ErrUserOverride, c.Name)
		}

		user = userOverride
	}

	target := c.Target
	if target == "" {
		target = c.Name
	}

	probe := store.Connection{Name: c.Name, Via: c.Via, Target: target, User: user}
	if strings.Contains(c.Via, "{ip}") {
		probe.IP = c.IP
	}

	if err := probe.Validate(); err != nil {
		return nil, err
	}

	switch {
	case c.Via == "" || c.Via == plainSSH:
		return sshArgv(user, target), nil
	case strings.ContainsAny(c.Via, "{}"):
		return templateArgv(c, user, target)
	}

	return verbArgv(c.Via, user, target), nil
}

// acceptsUser reports whether a via command has a place for a user chosen at connect time.
//
// Parameters:
//   - via: stored via command, empty meaning ssh
func acceptsUser(via string) bool {
	switch {
	case via == "" || via == plainSSH:
		return true
	case strings.ContainsAny(via, "{}"):
		return strings.Contains(via, "{user")
	}

	return len(strings.Fields(via)) == 1
}

// sshArgv builds the plain ssh command line.
//
// Parameters:
//   - user: login, empty to leave it to ssh
//   - target: validated host
func sshArgv(user, target string) []string {
	argv := []string{plainSSH}
	if user != "" {
		argv = append(argv, "-l", user)
	}

	return append(argv, "--", unbracket(target))
}

// unbracket removes the brackets around an IPv6 address.
//
// Parameters:
//   - target: host as stored
//
// Returns the address alone when target is "[" + IPv6 address + "]", otherwise target.
func unbracket(target string) string {
	inner, ok := strings.CutPrefix(target, "[")
	if !ok {
		return target
	}

	inner, ok = strings.CutSuffix(inner, "]")
	if !ok {
		return target
	}

	if addr, err := netip.ParseAddr(inner); err != nil || !addr.Is6() {
		return target
	}

	return inner
}

// verbArgv builds the command line of a via without placeholders.
//
// Parameters:
//   - via: stored via command
//   - user: login, empty for none
//   - target: validated host
func verbArgv(via, user, target string) []string {
	if user != "" {
		target = user + "@" + target
	}

	return append(strings.Fields(via), target)
}

// templateArgv substitutes the placeholders of a via template word by word.
//
// Parameters:
//   - c: connection that holds the via, the name and the ip
//   - user: login, empty for none
//   - target: validated host
//
// Returns the argument vector, or an error wrapping ErrMissing when {user} or {ip} has no value.
func templateArgv(c store.Connection, user, target string) ([]string, error) {
	if strings.Contains(c.Via, "{user}") && user == "" {
		return nil, fmt.Errorf("%w: %q has no user", ErrMissing, c.Name)
	}

	if strings.Contains(c.Via, "{ip}") && c.IP == "" {
		return nil, fmt.Errorf("%w: %q has no ip", ErrMissing, c.Name)
	}

	prefix := ""
	if user != "" {
		prefix = user + "@"
	}

	// A single pass keeps a substituted value from being read as a placeholder.
	sub := strings.NewReplacer("{user@}", prefix, "{user}", user, "{host}", target,
		"{ip}", c.IP, "{name}", c.Name)

	words := strings.Fields(c.Via)
	argv := make([]string, 0, len(words))

	for _, w := range words {
		out := sub.Replace(w)
		if out == "" && w == "{user@}" {
			continue
		}

		argv = append(argv, out)
	}

	return argv, nil
}

// PrettyCommand renders an argument vector for display.
//
// Parameters:
//   - argv: command and arguments; a word is quoted only when the shell would otherwise read it
//     differently
//
// Returns the words joined by single spaces.
func PrettyCommand(argv []string) string {
	words := make([]string, len(argv))
	for i, a := range argv {
		words[i] = quoteIfNeeded(a)
	}

	return strings.Join(words, " ")
}
