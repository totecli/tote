package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/totecli/tote/internal/store"
)

// reasonKeys maps the start of the reasons that the store validators give to the catalog key of
// their translation. A reason with a value in it, such as a length, is handled by reasonText.
var reasonKeys = []struct {
	prefix string
	key    string
}{
	{"must not be empty", "ui_invalid_empty"},
	{"is not valid UTF-8", "ui_invalid_utf8"},
	{"must not start with '-'", "ui_invalid_dash"},
	{"must not contain whitespace or control characters", "ui_invalid_space"},
	{"must not contain control characters", "ui_invalid_control"},
	{"must not contain a NUL", "ui_invalid_line"},
	{"must not start or end with a space", "ui_invalid_edge_space"},
	{"quotes are not interpreted", "ui_invalid_via_quote"},
	{"first word must be a command name", "ui_invalid_via_verb"},
	{"unmatched '}'", "ui_invalid_unmatched_close"},
	{"unmatched '{'", "ui_invalid_unmatched_open"},
	{"must not start or end with ':'", "ui_invalid_colon"},
	{"alias repeats the connection name", "ui_invalid_alias_repeat"},
	{"duplicate alias", "ui_invalid_alias_dup"},
	{"is not an IP address", "ui_invalid_ip"},
	{"must not carry a zone", "ui_invalid_zone"},
}

// reasonText translates the reason of a validation error of the store.
//
// Parameters:
//   - e: shared environment, for the printer
//   - err: error returned by a Validate method or a Valid function of the store
//
// Returns the reason without the name of the field, in the language of the interface and safe to
// draw. A reason that the catalog does not know is returned as the store wrote it.
func reasonText(e *env, err error) string {
	msg := err.Error()

	if _, rest, ok := strings.Cut(msg, store.ErrInvalidField.Error()+": "); ok {
		msg = rest
	}

	for _, r := range reasonKeys {
		if strings.HasPrefix(msg, r.prefix) {
			return e.t(r.key)
		}
	}

	var n, limit int

	switch {
	case scanned(msg, "too long (%d bytes, max %d)", &n, &limit):
		return e.t("ui_invalid_long_bytes", n, limit)
	case scanned(msg, "too long (%d runes, max %d)", &n, &limit):
		return e.t("ui_invalid_long_runes", n, limit)
	case strings.HasPrefix(msg, "must start with a letter, a digit or '_'"),
		strings.HasPrefix(msg, "must start with a letter, digit or '_'"):
		if strings.Contains(msg, "'.'") {
			return e.t("ui_invalid_conn_name")
		}

		return e.t("ui_invalid_func_name")
	case strings.HasPrefix(msg, "must not contain "):
		return e.t("ui_invalid_char", sanitize(strings.TrimPrefix(msg, "must not contain ")))
	case strings.HasPrefix(msg, "unknown placeholder "):
		return e.t("ui_invalid_placeholder",
			sanitize(strings.TrimPrefix(msg, "unknown placeholder ")))
	}

	if quoted, ok := strings.CutSuffix(msg, " is reserved"); ok {
		return e.t("cli_reserved_name", sanitize(strings.Trim(quoted, `"`)))
	}

	return sanitize(msg)
}

// scanned reports whether a message has exactly the shape of a format with numbers in it.
//
// Parameters:
//   - msg: text to read
//   - format: fmt format with %d directives
//   - args: pointers that receive the numbers
func scanned(msg, format string, args ...any) bool {
	n, err := fmt.Sscanf(msg, format, args...)

	return err == nil && n == len(args) && strings.HasSuffix(msg, ")")
}

// fieldError tells why a field is refused.
//
// Parameters:
//   - e: shared environment
//   - err: error of the field, nil when it is accepted
//
// Returns an empty string for nil, else the translated reason.
func fieldError(e *env, err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errRelative):
		return e.t("cli_path_not_absolute")
	case errors.Is(err, errScriptRelative):
		return e.t("cli_script_not_absolute")
	}

	return reasonText(e, err)
}
