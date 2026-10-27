package store

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidField is wrapped by every error that a validator returns, so that callers can tell a
// rejected value from an I/O or parse failure with errors.Is.
var ErrInvalidField = errors.New("invalid value")

const (
	// maxNameLen is the longest accepted name, in bytes, for every record kind and for aliases.
	maxNameLen = 64

	// maxUserLen is the longest accepted connection user, in bytes.
	maxUserLen = 32

	// maxDescRunes is the longest accepted description, in runes.
	maxDescRunes = 200

	// maxLineLen is the longest accepted command, path, script, target or via, in bytes. It is
	// the size of PATH_MAX, so every path fits, and it keeps a data file far below maxFileSize.
	maxLineLen = 4096

	// shellForbidden lists the characters that bash does not allow in an alias name, plus the
	// quotes and the shell metacharacters that would be dangerous in a generated file.
	shellForbidden = "/$`=\"'|&;()<>!{}*?[]#~^\\"
)

// reservedAliases lists the alias names that belong to the program itself.
var reservedAliases = map[string]bool{"tote": true, "mn": true}

// placeholders lists the substitution markers that a via template may contain.
var placeholders = map[string]bool{
	"user@": true,
	"user":  true,
	"host":  true,
	"ip":    true,
	"name":  true,
}

var (
	// connNameRe matches a connection name or a connection alias.
	connNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

	// viaVerbRe matches the first word of a via command.
	viaVerbRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]*$`)

	// userRe matches a connection user.
	userRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)

	// funcNameRe matches a function name.
	funcNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
)

// invalid builds an error that wraps ErrInvalidField and carries a reason.
//
// Parameters:
//   - format: fmt format of the reason, lowercase and without a final period
//   - args: values for the format
//
// Returns the wrapped error.
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidField, fmt.Sprintf(format, args...))
}

// field prefixes a validator error with the name of the field it concerns.
//
// Parameters:
//   - name: field label such as "connection name"
//   - err: error returned by a validator, nil is passed through
//
// Returns nil when err is nil, otherwise err wrapped with the label.
func field(name string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", name, err)
}

// ValidConnectionName checks a connection name or a connection alias.
//
// Parameters:
//   - name: candidate, at most 64 bytes, ASCII letters, digits, '_', '.' and '-', not starting
//     with '.' or '-'
//
// Returns nil when the name is accepted, otherwise an error wrapping ErrInvalidField.
func ValidConnectionName(name string) error {
	if name == "" {
		return invalid("must not be empty")
	}

	if len(name) > maxNameLen {
		return invalid("too long (%d bytes, max %d)", len(name), maxNameLen)
	}

	if !connNameRe.MatchString(name) {
		return invalid("must start with a letter, digit or '_' and contain only ASCII " +
			"letters, digits, '_', '.' and '-'")
	}

	return nil
}

// ValidAliasName checks a shell alias name against bash's alias rule and the reserved words.
//
// Parameters:
//   - name: candidate, at most 64 bytes; no whitespace, quote, shell metacharacter, '/', '$',
//     backtick, '=' or backslash, no leading '-', and not "tote" or "mn"
//
// Returns nil when the name is accepted, otherwise an error wrapping ErrInvalidField.
func ValidAliasName(name string) error {
	if err := validShellName(name); err != nil {
		return err
	}

	if reservedAliases[name] {
		return invalid("%q is reserved", name)
	}

	return nil
}

// ValidRepoName checks a repository name, which follows the alias rule except that only "tote"
// is reserved.
//
// Parameters:
//   - name: candidate, same rule as ValidAliasName
//
// Returns nil when the name is accepted, otherwise an error wrapping ErrInvalidField.
func ValidRepoName(name string) error {
	if err := validShellName(name); err != nil {
		return err
	}

	if name == "tote" {
		return invalid("%q is reserved", name)
	}

	return nil
}

// ValidFuncName checks the name of a shell function or executable managed by the program.
//
// A name with a '-' is valid for bash and zsh in their normal mode, and for an executable. Bash
// in POSIX mode (bash --posix, sh linked to bash, POSIXLY_CORRECT) refuses such a function name,
// and no other spelling of the definition avoids it, so this function does not reject it.
//
// Parameters:
//   - name: candidate, at most 64 bytes, ASCII letters, digits, '_' and '-', not starting with
//     a digit or '-'
//
// Returns nil when the name is accepted, otherwise an error wrapping ErrInvalidField.
func ValidFuncName(name string) error {
	if name == "" {
		return invalid("must not be empty")
	}

	if len(name) > maxNameLen {
		return invalid("too long (%d bytes, max %d)", len(name), maxNameLen)
	}

	if !funcNameRe.MatchString(name) {
		return invalid("must start with a letter, a digit or '_' and contain only ASCII letters, " +
			"digits, '_' and '-'")
	}

	return nil
}

// ValidDesc checks a free text description.
//
// Parameters:
//   - desc: candidate, may be empty; valid UTF-8, at most 200 runes, no control character (tab
//     and newline included), no Unicode line or paragraph separator and no format character
//     such as a bidirectional override or a zero-width space, except the joiners used by emoji
//     sequences and by some scripts
//
// Returns nil when the description is accepted, otherwise an error wrapping ErrInvalidField.
func ValidDesc(desc string) error {
	if !utf8.ValidString(desc) {
		return invalid("is not valid UTF-8")
	}

	if n := utf8.RuneCountInString(desc); n > maxDescRunes {
		return invalid("too long (%d runes, max %d)", n, maxDescRunes)
	}

	for _, r := range desc {
		if unicode.IsControl(r) || hiddenFormat(r) || r == '\u2028' || r == '\u2029' {
			return invalid("must not contain control characters")
		}
	}

	return nil
}

// hiddenFormat reports whether a rune is an invisible format character that can spoof a display.
//
// Parameters:
//   - r: rune to test; the zero-width joiner and non-joiner are not counted
func hiddenFormat(r rune) bool {
	return unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d'
}

// validShellName applies bash's alias rule to a name.
//
// Parameters:
//   - name: candidate alias or repository name
//
// Returns nil when the name is accepted, otherwise an error wrapping ErrInvalidField.
func validShellName(name string) error {
	if name == "" {
		return invalid("must not be empty")
	}

	if len(name) > maxNameLen {
		return invalid("too long (%d bytes, max %d)", len(name), maxNameLen)
	}

	if !utf8.ValidString(name) {
		return invalid("is not valid UTF-8")
	}

	if name[0] == '-' {
		return invalid("must not start with '-'")
	}

	for _, r := range name {
		if r == ' ' || !unicode.IsPrint(r) {
			return invalid("must not contain whitespace or control characters")
		}

		if strings.ContainsRune(shellForbidden, r) {
			return invalid("must not contain %q", r)
		}
	}

	return nil
}

// validLine checks a single line value such as a command, a path or a script path.
//
// Parameters:
//   - value: candidate, stored verbatim; must contain something other than white space and be
//     at most 4096 bytes
//
// Returns nil when the value is accepted, otherwise an error wrapping ErrInvalidField.
func validLine(value string) error {
	if strings.TrimSpace(value) == "" {
		return invalid("must not be empty")
	}

	if len(value) > maxLineLen {
		return invalid("too long (%d bytes, max %d)", len(value), maxLineLen)
	}

	if !utf8.ValidString(value) {
		return invalid("is not valid UTF-8")
	}

	if strings.ContainsAny(value, "\x00\n\r") {
		return invalid("must not contain a NUL, a newline or a carriage return")
	}

	return nil
}

// viaPlaceholders validates a via command and reports the placeholders it uses.
//
// A via is split on spaces and run without a shell, so a quote would reach the program as a
// literal character instead of grouping words, and it is refused.
//
// Parameters:
//   - via: stored via value, empty meaning "ssh", at most 4096 bytes
//
// Returns the set of placeholder names found, or an error wrapping ErrInvalidField when the
// first word is not a command name, when a character is not printable or is a quote, or when a
// brace does not close a known placeholder.
func viaPlaceholders(via string) (map[string]bool, error) {
	found := map[string]bool{}
	if via == "" {
		return found, nil
	}

	if len(via) > maxLineLen {
		return nil, invalid("too long (%d bytes, max %d)", len(via), maxLineLen)
	}

	if !utf8.ValidString(via) {
		return nil, invalid("is not valid UTF-8")
	}

	for _, r := range via {
		if r != ' ' && !unicode.IsPrint(r) {
			return nil, invalid("must not contain control characters or tabs")
		}

		if r == '\'' || r == '"' {
			return nil, invalid("quotes are not interpreted: the via is split on spaces and runs " +
				"without a shell, put options such as ProxyCommand in ssh_config")
		}
	}

	if via[0] == ' ' || via[len(via)-1] == ' ' {
		return nil, invalid("must not start or end with a space")
	}

	verb, _, _ := strings.Cut(via, " ")
	if !viaVerbRe.MatchString(verb) {
		return nil, invalid("first word must be a command name: it starts with an ASCII letter, " +
			"a digit or '_', then letters, digits, '_', '.', '/' and '-'")
	}

	return found, scanPlaceholders(via, found)
}

// scanPlaceholders walks a via string and records every placeholder in it.
//
// Parameters:
//   - via: stored via value
//   - found: set that receives the placeholder names
//
// Returns an error wrapping ErrInvalidField on an unknown placeholder or a stray brace.
func scanPlaceholders(via string, found map[string]bool) error {
	rest := via

	for {
		i := strings.IndexAny(rest, "{}")
		if i < 0 {
			return nil
		}

		if rest[i] == '}' {
			return invalid("unmatched '}'")
		}

		name, after, ok := strings.Cut(rest[i+1:], "}")
		if !ok {
			return invalid("unmatched '{'")
		}

		if !placeholders[name] {
			return invalid("unknown placeholder %q", name)
		}

		found[name] = true
		rest = after
	}
}

// validTarget checks the host part of a connection.
//
// Parameters:
//   - target: stored target, may be empty only for a template that never uses {host}
//   - found: placeholders of the via command, as returned by viaPlaceholders
//
// Returns nil when the target is accepted, otherwise an error wrapping ErrInvalidField.
func validTarget(target string, found map[string]bool) error {
	if target == "" {
		if len(found) == 0 || found["host"] {
			return invalid("must not be empty")
		}

		return nil
	}

	if len(target) > maxLineLen {
		return invalid("too long (%d bytes, max %d)", len(target), maxLineLen)
	}

	if !utf8.ValidString(target) {
		return invalid("is not valid UTF-8")
	}

	for _, r := range target {
		if r == ' ' || !unicode.IsPrint(r) {
			return invalid("must not contain whitespace or control characters")
		}
	}

	if target[0] == '-' {
		return invalid("must not start with '-'")
	}

	if target[0] == ':' || target[len(target)-1] == ':' {
		return invalid("must not start or end with ':' (put an IPv6 address in brackets)")
	}

	return nil
}

// validUser checks the login of a connection.
//
// Parameters:
//   - user: stored user, empty meaning that ssh_config or the command decides
//
// Returns nil when the user is accepted, otherwise an error wrapping ErrInvalidField.
func validUser(user string) error {
	if user == "" {
		return nil
	}

	if len(user) > maxUserLen {
		return invalid("too long (%d bytes, max %d)", len(user), maxUserLen)
	}

	if !userRe.MatchString(user) {
		return invalid("must start with a letter, a digit or '_' and contain only ASCII letters, " +
			"digits, '_', '.' and '-'")
	}

	return nil
}

// validAliases checks the extra names of a connection.
//
// Parameters:
//   - aliases: stored aliases, each one a valid connection name
//   - name: name of the record, which no alias may repeat
//
// Returns nil when the list is accepted, otherwise an error wrapping ErrInvalidField.
func validAliases(aliases []string, name string) error {
	seen := make(map[string]bool, len(aliases))

	for _, a := range aliases {
		if err := ValidConnectionName(a); err != nil {
			return err
		}

		if a == name {
			return invalid("alias repeats the connection name")
		}

		if seen[a] {
			return invalid("duplicate alias")
		}

		seen[a] = true
	}

	return nil
}

// validIP checks the informational address of a connection.
//
// Parameters:
//   - ip: stored address, empty or an IPv4 or IPv6 literal without brackets or zone
//
// Returns nil when the address is accepted, otherwise an error wrapping ErrInvalidField.
func validIP(ip string) error {
	if ip == "" {
		return nil
	}

	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return invalid("is not an IP address")
	}

	if addr.Zone() != "" {
		return invalid("must not carry a zone")
	}

	return nil
}
