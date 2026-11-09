package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
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
	{"quotes are not interpreted", "cli_reason_quotes"},
	{"must be \"exec\" or \"source\"", "cli_reason_kind"},
	{"must not start or end with a space", "ui_invalid_edge_space"},
	{"first word must be a command name", "ui_invalid_via_verb"},
	{"unmatched '}'", "ui_invalid_unmatched_close"},
	{"unmatched '{'", "ui_invalid_unmatched_open"},
	{"must not start or end with ':'", "ui_invalid_colon"},
	{"alias repeats the connection name", "ui_invalid_alias_repeat"},
	{"duplicate alias", "ui_invalid_alias_dup"},
	{"is not an IP address", "ui_invalid_ip"},
	{"must not carry a zone", "ui_invalid_zone"},
}

// fieldRecord matches the message of a rejected record: the table, the quoted key, the field
// label that the store gives (the kind of record, then the field) and the reason.
var fieldRecord = regexp.MustCompile(
	`(?s)^invalid ([a-z]+) ("(?:[^"\\]|\\.)*"): [a-z]+ ([a-z]+): invalid value: (.*)$`)

// tooBig matches the error of a data file that a write would make larger than tote reads back.
var tooBig = regexp.MustCompile(`(?s)file too large: (.*?) would be [0-9]+ bytes`)

// writeFailure matches the error of a data file that the permissions forbid to replace.
var writeFailure = regexp.MustCompile(`(?s)^write (.*): file is read-only$`)

// tableKinds gives the catalog key of the name of the kind of record stored in a table.
var tableKinds = map[string]string{
	"connection": "kind_connection", "alias": "kind_alias", "repo": "kind_repo",
	"func": "kind_func",
}

// fieldLabels gives the catalog key of the label of each field of the records.
var fieldLabels = map[string]string{
	"aliases": "cli_field_aliases", "cmd": "cli_field_cmd", "desc": "cli_field_desc",
	"ip": "cli_field_ip", "kind": "cli_field_kind", "name": "cli_field_name",
	"path": "cli_field_path", "script": "cli_field_script", "target": "cli_field_target",
	"user": "cli_field_user", "via": "cli_field_via",
}

// fieldReason translates the reason of a validation error of the store.
//
// Parameters:
//   - msg: reason as the store wrote it, without the name of the field
//
// Returns the reason in the language of the run and safe to print. A reason that the catalog does
// not know is returned as the store wrote it, sanitized.
func (a *app) fieldReason(msg string) string {
	for _, r := range reasonKeys {
		if strings.HasPrefix(msg, r.prefix) {
			return a.t(r.key)
		}
	}

	var n, limit int

	switch {
	case scanned(msg, "too long (%d bytes, max %d)", &n, &limit):
		return a.t("ui_invalid_long_bytes", n, limit)
	case scanned(msg, "too long (%d runes, max %d)", &n, &limit):
		return a.t("ui_invalid_long_runes", n, limit)
	case strings.HasPrefix(msg, "must start with a letter, a digit or '_'"),
		strings.HasPrefix(msg, "must start with a letter, digit or '_'"):
		if strings.Contains(msg, "'.'") {
			return a.t("ui_invalid_conn_name")
		}

		return a.t("ui_invalid_func_name")
	case strings.HasPrefix(msg, "must not contain "):
		return a.t("ui_invalid_char", sanitize(strings.TrimPrefix(msg, "must not contain ")))
	case strings.HasPrefix(msg, "unknown placeholder "):
		return a.t("ui_invalid_placeholder",
			sanitize(strings.TrimPrefix(msg, "unknown placeholder ")))
	}

	if quoted, ok := strings.CutSuffix(msg, " is reserved"); ok {
		return a.t("cli_reserved_name", sanitize(strings.Trim(quoted, `"`)))
	}

	return sanitize(msg)
}

// scanned reports whether a reason has exactly the shape of a format with values in it.
//
// Parameters:
//   - msg: reason to read
//   - format: fmt scan format that ends with a closing bracket
//   - args: pointers that receive the values
func scanned(msg, format string, args ...any) bool {
	n, err := fmt.Sscanf(msg, format, args...)

	return err == nil && n == len(args) && strings.HasSuffix(msg, ")")
}

// errorText writes an error for the user. The errors of the store that come from a mistake of the
// user, a value that is refused, a data file that cannot be replaced, a file written for a later
// release, are translated; any other error is shown as it is, sanitized.
//
// Parameters:
//   - err: error to show
func (a *app) errorText(err error) string {
	msg := err.Error()

	if errors.Is(err, store.ErrInvalidField) {
		if m := fieldRecord.FindStringSubmatch(msg); m != nil {
			return a.recordText(m[1], m[2], m[3], m[4])
		}
	}

	if errors.Is(err, store.ErrTooBig) {
		if m := tooBig.FindStringSubmatch(msg); m != nil {
			return a.t("store_too_big", sanitize(m[1]))
		}
	}

	if m := writeFailure.FindStringSubmatch(msg); m != nil {
		return a.t("cli_file_readonly", sanitize(m[1]))
	}

	return sanitize(msg)
}

// recordText writes the refusal of a field of a record.
//
// Parameters:
//   - table: table of the store, such as connection
//   - quoted: key of the record, quoted as the store wrote it
//   - field: last word of the label of the field
//   - reason: why the value is refused
func (a *app) recordText(table, quoted, field, reason string) string {
	kind, ok := tableKinds[table]
	if !ok {
		return sanitize(fmt.Sprintf("invalid %s %s: %s", table, quoted, reason))
	}

	key, err := strconv.Unquote(quoted)
	if err != nil {
		key = quoted
	}

	label := field
	if k, ok := fieldLabels[field]; ok {
		label = a.t(k)
	}

	return a.t("cli_invalid_record", a.t(kind), sanitize(key), sanitize(label),
		a.fieldReason(reason))
}

// parseText translates the reason of a syntax error of a data file when the catalog knows it.
//
// Parameters:
//   - msg: reason as the store wrote it
func (a *app) parseText(msg string) string {
	var found, want int

	if n, err := fmt.Sscanf(msg, "unsupported schema %d, this version reads schema %d", &found,
		&want); err == nil && n == 2 {
		return a.t("cli_parse_schema", found, want)
	}

	return sanitize(msg)
}
