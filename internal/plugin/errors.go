package plugin

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Stable codes of the manifest issues. A code never changes meaning: the host maps each one to a
// catalog key and tests assert on it. Args carry the offending value or key first, then the limit
// or the expected form. The codes of the failure table of the specification keep their names
// (id_invalid, perm_unknown, perm_refused, api_unsupported, tote_range, platform_none,
// reserved_not_available, declarative_extra_file, exec_self, and the lint codes argv.shell,
// argv.risky_flag, argv.ssh_no_dashdash, pattern.unanchored); the others are finer grained
// causes of the umbrella manifest_invalid.
const (
	CodeManifestTooLarge    = "manifest_too_large"
	CodeManifestSyntax      = "manifest_syntax"
	CodeManifestMissing     = "manifest_missing"
	CodeManifestNested      = "manifest_nested"
	CodeKeyMissing          = "key_missing"
	CodeTypeInvalid         = "type_invalid"
	CodeValueInvalid        = "value_invalid"
	CodeTextInvalid         = "text_invalid"
	CodeLimitExceeded       = "limit_exceeded"
	CodeKeyConflict         = "key_conflict"
	CodeUnknownKey          = "unknown_key"
	CodeDeprecatedKey       = "deprecated_key"
	CodeReserved            = "reserved_not_available"
	CodeIDInvalid           = "id_invalid"
	CodeIDDuplicate         = "id_duplicate"
	CodeAPIUnsupported      = "api_unsupported"
	CodeRangeInvalid        = "range_invalid"
	CodeToteRange           = "tote_range"
	CodeFeatureMissing      = "feature_missing"
	CodePlatformUnsupported = "platform_unsupported"
	CodePlatformNone        = "platform_none"
	CodeTierMismatch        = "tier_mismatch"
	CodePermUnknown         = "perm_unknown"
	CodePermInvalid         = "perm_invalid"
	CodePermRefused         = "perm_refused"
	CodePermMissing         = "perm_missing"
	CodeExecSelf            = "exec_self"
	CodeExecUncovered       = "exec_uncovered"
	CodeExecUnused          = "exec_unused"
	CodeEnvUncovered        = "env_uncovered"
	CodeEnvRefused          = "env_refused"
	CodePlaceholderSyntax   = "placeholder_syntax"
	CodePlaceholderUnknown  = "placeholder_unknown"
	CodePlaceholderScope    = "placeholder_scope"
	CodePlaceholderList     = "placeholder_list"
	CodeArgv0Invalid        = "argv0_invalid"
	CodeArgvShell           = "argv.shell"
	CodeArgvRiskyFlag       = "argv.risky_flag"
	CodeArgvSSHDashDash     = "argv.ssh_no_dashdash"
	CodePatternUnanchored   = "pattern.unanchored"
	CodeDeclarativeExtra    = "declarative_extra_file"
	CodeBinMissing          = "bin_missing"
	CodeBinNotExec          = "bin_not_executable"
	CodeExecUndeclared      = "exec_undeclared"
	CodeKeyClash            = "key_clash"
	CodeKeyReserved         = "key_reserved"
	CodeLocaleInvalid       = "locale_invalid"
	CodeLocaleMissingKey    = "locale_missing_key"
	CodeLocaleExtraKey      = "locale_extra_key"
)

// Severity tells an error, which makes the manifest unusable, from a warning, which only
// `plugin check` shows (an error under --strict).
type Severity int

// The two severities of an Issue.
const (
	SeverityError Severity = iota
	SeverityWarning
)

// MarshalText gives the word error or warning, so that a JSON report does not carry a number.
func (s Severity) MarshalText() ([]byte, error) {
	if s == SeverityWarning {
		return []byte("warning"), nil
	}

	return []byte("error"), nil
}

// UnmarshalText reads the word that MarshalText writes.
//
// Parameters:
//   - text: error or warning
func (s *Severity) UnmarshalText(text []byte) error {
	switch string(text) {
	case "error":
		*s = SeverityError
	case "warning":
		*s = SeverityWarning
	default:
		return fmt.Errorf("plugin: unknown severity %q", text)
	}

	return nil
}

// issueArgRunes caps the length of an argument copied into an Issue, so that a hostile value
// cannot flood a message.
const issueArgRunes = 80

// Issue is one finding about a manifest: a stable code, where it was found and its arguments.
//
// Path is the key path in the manifest, such as item_action[1].run[0], empty for the document;
// Line and Column are 1-based and 0 when the position is unknown. Incompatible marks an error
// that makes the plugin incompatible with this tote rather than invalid. Args are cleaned and cut
// at creation: they are safe to print, and they are data, never a message. The host renders text
// from Code and Args with its catalog.
type Issue struct {
	Code         string
	Severity     Severity
	Incompatible bool
	Path         string
	Line         int
	Column       int
	Args         []string
}

// Error implements error. The text is for logs and tests, not for the user: the host builds the
// user message from Code and Args.
func (i Issue) Error() string {
	var b strings.Builder

	if i.Line > 0 {
		fmt.Fprintf(&b, "plugin.toml:%d:%d: ", i.Line, i.Column)
	}

	if i.Path != "" {
		b.WriteString(i.Path + ": ")
	}

	b.WriteString(i.Code)

	if len(i.Args) > 0 {
		b.WriteString(" (" + strings.Join(i.Args, ", ") + ")")
	}

	return b.String()
}

// Class is the verdict on a manifest, which decides the plugin state: nothing wrong, invalid
// (broken) or incompatible with this tote.
type Class int

// The three verdicts of Report.Class.
const (
	ClassOK Class = iota
	ClassInvalid
	ClassIncompatible
)

// Header holds the identity fields of a manifest read leniently, already cleaned, so that a
// plugin that is invalid or incompatible can still be listed under its name. Any field can be
// empty.
type Header struct {
	ID      string
	Version string
	Tier    string
	Title   string
}

// Report is the result of reading a manifest: the lenient header and every issue found, in
// document order for the strict pass.
type Report struct {
	Header Header
	Issues []Issue
}

// Errors returns the issues of severity error, in report order.
func (r Report) Errors() []Issue { return r.filter(SeverityError) }

// Warnings returns the issues of severity warning, in report order.
func (r Report) Warnings() []Issue { return r.filter(SeverityWarning) }

// filter selects the issues of one severity.
//
// Parameters:
//   - sev: severity to keep
func (r Report) filter(sev Severity) []Issue {
	var out []Issue

	for _, i := range r.Issues {
		if i.Severity == sev {
			out = append(out, i)
		}
	}

	return out
}

// Has reports whether any issue carries a code.
//
// Parameters:
//   - code: stable code to look for
func (r Report) Has(code string) bool {
	for _, i := range r.Issues {
		if i.Code == code {
			return true
		}
	}

	return false
}

// Class returns the verdict: invalid when any error is not an incompatibility, incompatible when
// every error is one, else ok. Warnings never change it.
func (r Report) Class() Class {
	class := ClassOK

	for _, i := range r.Issues {
		switch {
		case i.Severity != SeverityError:
		case !i.Incompatible:
			return ClassInvalid
		default:
			class = ClassIncompatible
		}
	}

	return class
}

// collector gathers the issues of one pass and resolves their positions.
type collector struct {
	issues []Issue
	pos    *posIndex
}

// add records an error.
//
// Parameters:
//   - code: stable code
//   - path: key path of the finding, empty for the document
//   - args: arguments, cleaned and cut here
func (c *collector) add(code, path string, args ...string) {
	c.push(code, SeverityError, false, path, args)
}

// warn records a warning.
//
// Parameters:
//   - code: stable code
//   - path: key path of the finding, empty for the document
//   - args: arguments, cleaned and cut here
func (c *collector) warn(code, path string, args ...string) {
	c.push(code, SeverityWarning, false, path, args)
}

// incompatible records an error that makes the plugin incompatible rather than invalid.
//
// Parameters:
//   - code: stable code
//   - path: key path of the finding, empty for the document
//   - args: arguments, cleaned and cut here
func (c *collector) incompatible(code, path string, args ...string) {
	c.push(code, SeverityError, true, path, args)
}

// push builds and stores one issue.
//
// Parameters:
//   - code: stable code
//   - sev: severity
//   - incompat: true for an incompatibility
//   - path: key path of the finding
//   - args: raw arguments
func (c *collector) push(code string, sev Severity, incompat bool, path string, args []string) {
	clean := make([]string, len(args))
	for i, a := range args {
		clean[i] = CleanLine(a, issueArgRunes)
	}

	line, col := c.pos.find(path)

	c.issues = append(c.issues, Issue{
		Code: code, Severity: sev, Incompatible: incompat,
		Path: path, Line: line, Column: col, Args: clean,
	})
}

// sortedKeys returns the keys of a string-keyed map in byte order.
//
// Parameters:
//   - m: map to read, may be nil
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

// Codes of a refused or failed call. Prepare and Run return them in an *Error; the host maps each
// one to a catalog key and to an exit code with ExitClass. Args carry the value that matters
// first, cleaned and cut.
const (
	CodeNotRunnable     = "not_runnable"
	CodeBusy            = "busy"
	CodeBadID           = "bad_id"
	CodeNoEntry         = "no_entry"
	CodeInputInvalid    = "input_invalid"
	CodeDashRefused     = "dash_refused"
	CodeUnavailable     = "unavailable"
	CodeProducedInvalid = "produced_invalid"
	CodeDepMissing      = "dependency_missing"
	CodeCwdInvalid      = "cwd_invalid"
	CodeOpenInvalid     = "open_invalid"
	CodeStale           = "stale"
	CodeConfirmNeeded   = "confirm_needed"
	CodeStartFailed     = "start_failed"
	CodeTerminalRun     = "terminal_run"
	CodeUnsupported     = "unsupported"
	CodeProcsFile       = "procs_unavailable"
	CodeOutputLimit     = "output_limit"
)

// Exit codes of the command line, from section 8.3.
const (
	ExitFailure     = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitPolicy      = 4
	ExitIntegrity   = 5
	ExitCannotStart = 127
)

// Error is a refusal or a failure of the plugin layer: a stable code, its arguments and the
// underlying error, if any. It never carries user-visible text; the host renders the message from
// Code and Args with its catalog.
//
// Args are cleaned and cut at creation, so they are safe to print.
type Error struct {
	Code string
	Args []string
	Err  error
}

// Error implements error. The text is for logs and tests, not for the user.
func (e *Error) Error() string {
	var b strings.Builder

	b.WriteString("plugin: " + e.Code)

	if len(e.Args) > 0 {
		b.WriteString(" (" + strings.Join(e.Args, ", ") + ")")
	}

	if e.Err != nil {
		b.WriteString(": " + errText(e.Err))
	}

	return b.String()
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error { return e.Err }

// newError builds an *Error with cleaned arguments.
//
// Parameters:
//   - code: stable code
//   - args: raw arguments, cleaned and cut here
func newError(code string, args ...string) *Error {
	clean := make([]string, len(args))
	for i, a := range args {
		clean[i] = CleanLine(a, maxReasonRunes)
	}

	return &Error{Code: code, Args: clean}
}

// wrapError builds an *Error that wraps a cause.
//
// Parameters:
//   - code: stable code
//   - err: underlying error
//   - args: raw arguments, cleaned and cut here
func wrapError(code string, err error, args ...string) *Error {
	e := newError(code, args...)
	e.Err = err

	return e
}

// exitClasses maps the codes that do not exit 1 to their exit code. The install codes are listed
// here as plain words so that the command line has one table; the matching constants live where
// the code that returns them does.
var exitClasses = map[string]int{
	"bad_source": ExitUsage, "hash_required": ExitUsage, "hash_malformed": ExitUsage,
	"hash_unexpected": ExitUsage,
	"consent_needed":  ExitUsage, "bad_id": ExitUsage, "source_required": ExitUsage,
	"no_entry": ExitUsage, "input_invalid": ExitUsage, "dash_refused": ExitUsage,
	"confirm_needed": ExitUsage, "source_unsupported": ExitUsage, "no_embedded": ExitUsage,
	"grant_mismatch": ExitPolicy, "not_runnable": ExitPolicy, "enable_refused": ExitPolicy,
	"from_plugin": ExitPolicy, "busy": ExitPolicy, "stale": ExitPolicy, "exec_self": ExitPolicy,
	"exec_uncovered": ExitPolicy, "perm_missing": ExitPolicy,
	"hash_mismatch": ExitIntegrity, "too_large": ExitIntegrity, "tree_hash": ExitIntegrity,
	"platform_hash": ExitIntegrity, "tree_collision": ExitIntegrity,
	"origin_redirect": ExitIntegrity,
	"tree_perms":      ExitIntegrity, "tree_owner": ExitIntegrity, "tree_symlink": ExitIntegrity,
	"tree_special": ExitIntegrity, "tree_name": ExitIntegrity,
	"dependency_missing": ExitCannotStart, "start_failed": ExitCannotStart,
}

// ExitClass gives the exit code of the command line for a stable code: 2 for a usage error, 4 for
// a refusal by state or policy, 5 for an integrity failure, 127 when a program cannot start and 1
// for every other failure. Exit 3, the authentication request, has no code of its own: only a
// download answered with status 401 gives it, and ExitFor decides that from the arguments.
//
// Parameters:
//   - code: stable code of an *Error, or an install code such as hash_mismatch
func ExitClass(code string) int {
	if c, ok := exitClasses[code]; ok {
		return c
	}

	if strings.HasPrefix(code, "archive_") {
		return ExitIntegrity
	}

	return ExitFailure
}

// ExitOf gives the exit code for an error returned by this package.
//
// Parameters:
//   - err: error to classify; nil gives 0 and an error that is not an *Error gives 1
func ExitOf(err error) int {
	if err == nil {
		return 0
	}

	var pe *Error
	if errors.As(err, &pe) {
		return ExitFor(pe.Code, pe.Args)
	}

	return ExitFailure
}

// ExitFor gives the exit code for a code and its arguments. It is ExitClass, except that a
// download answered with status 401 is an authentication request (3): tote never sends
// credentials, so the release needs a sign-in it cannot give. An enable that is refused because
// the files of the plugin were modified is an integrity failure (5): its arguments are the reason
// codes.
//
// Parameters:
//   - code: stable code of an *Error
//   - args: arguments of the error; for net_status the second one is the HTTP status
func ExitFor(code string, args []string) int {
	if code == CodeNetStatus && len(args) > 1 && args[1] == "401" {
		return ExitAuth
	}

	if code == CodeEnableRefused {
		for _, reason := range args {
			if ExitClass(reason) == ExitIntegrity {
				return ExitIntegrity
			}
		}
	}

	return ExitClass(code)
}
