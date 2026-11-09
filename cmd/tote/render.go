package main

import (
	"errors"
	"io/fs"
	"strings"

	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/plugin"
)

// maxCause is the most runes of the system error that a message appends to its arguments.
const maxCause = 160

// word translates one word of a family (a reason of an unsafe archive entry, a limit, a kind of
// failure) from its stable code.
//
// Parameters:
//   - prefix: catalog prefix of the family, such as plugin_unsafe_
//   - code: stable code of the word
//
// Returns the word in the language of the run, or the code itself, sanitized, when the family
// has no word for it.
func (a *app) word(prefix, code string) string {
	msg, _ := a.tr().Code(prefix, code, nil)

	return sanitize(msg)
}

// stateWord names a plugin state in the language of the run.
//
// Parameters:
//   - state: state of a plugin, or "available" for a plugin shipped with tote and not installed
func (a *app) stateWord(state string) string {
	return a.word(i18n.PrefixState, state)
}

// causeText writes the system error under an *Error as one short line.
//
// Parameters:
//   - err: the Err of an *Error; nil gives an empty text
func causeText(err error) string {
	if err == nil {
		return ""
	}

	return plugin.CleanLine(scrubURLs(err.Error()), maxCause)
}

// arg gives one argument of a code, or an empty text.
//
// Parameters:
//   - args: arguments of the code
//   - i: index of the wanted one
func arg(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}

	return ""
}

// entryName names the entry of an archive that an error is about, the archive itself when the
// error has no entry.
//
// Parameters:
//   - name: path of the entry, possibly empty
func (a *app) entryName(name string) string {
	if name == "" {
		return a.t("plugin_word_archive")
	}

	return name
}

// prepArgs prepares the arguments of an error for its message: the words that are codes are
// translated, the lists are joined and the system error is added where the message shows it.
//
// Parameters:
//   - code: stable code of the error
//   - args: arguments of the error
//   - cause: the error under it, may be nil
func (a *app) prepArgs(code string, args []string, cause error) []string {
	switch code {
	case plugin.CodeArchiveFormat:
		return []string{a.word("plugin_archive_", arg(args, 1))}
	case plugin.CodeArchiveUnsafe:
		return []string{a.entryName(arg(args, 0)), a.word("plugin_unsafe_", arg(args, 1))}
	case plugin.CodeArchiveLimits:
		return []string{a.entryName(arg(args, 0)), a.word("plugin_limit_", arg(args, 1))}
	case plugin.CodeSourceUnsafe:
		return []string{arg(args, 0), a.word("plugin_unsafe_", arg(args, 1))}
	case plugin.CodeSourceLimits:
		return []string{arg(args, 0), a.word("plugin_limit_", arg(args, 1))}
	case plugin.CodeIDInvalid:
		return []string{arg(args, 0), a.word("plugin_idwhy_", arg(args, 1))}
	case plugin.CodeInputInvalid:
		return []string{arg(args, 0), a.word("plugin_reject_", arg(args, 1))}
	case plugin.CodeEnableRefused:
		return a.refusalArgs(args, cause)
	case plugin.CodeLimitExceeded:
		return []string{strings.Join(args, " ")}
	case plugin.CodeValueInvalid:
		return []string{strings.Join(args, ", ")}
	case plugin.CodeDisk, plugin.CodeNetOffline, plugin.CodeNetTLS:
		return []string{arg(args, 0), causeText(cause)}
	}

	return args
}

// refusalArgs prepares the arguments of an enable that was refused: the plugin and its reasons as
// one text. The reasons come with their own arguments when the error carries them, else only as
// codes.
//
// Parameters:
//   - codes: reason codes, the arguments of the error
//   - cause: the error under it, a *plugin.ReasonsError when the plugin layer built it
func (a *app) refusalArgs(codes []string, cause error) []string {
	var re *plugin.ReasonsError
	if !errors.As(cause, &re) {
		re = &plugin.ReasonsError{}

		for _, c := range codes {
			re.Reasons = append(re.Reasons, plugin.Reason{Code: c})
		}
	}

	texts := make([]string, len(re.Reasons))
	for i, r := range re.Reasons {
		texts[i] = a.reasonText(r)
	}

	return []string{re.ID, strings.Join(texts, "; ")}
}

// errText renders the message of an error code with its arguments.
//
// Parameters:
//   - code: stable code
//   - args: arguments of the code
//   - cause: the error under it, may be nil
//
// Returns the message in the language of the run, or the code with its arguments when the catalog
// has no message for it, so that nothing is ever lost.
func (a *app) errText(code string, args []string, cause error) string {
	if code == plugin.CodeBadSource && errors.Is(cause, fs.ErrNotExist) {
		return sanitize(a.t("cli_plugin_source_missing", arg(args, 0)))
	}

	if code == plugin.CodeBadSource && errors.Is(cause, fs.ErrPermission) {
		return sanitize(a.t("cli_plugin_source_unreadable", arg(args, 0)))
	}

	msg, ok := a.tr().Code(i18n.PrefixError, code, a.cleanArgs(a.prepArgs(code, args, cause)))
	if !ok {
		return sanitize(scrubURLs(strings.TrimSpace(code + " " + strings.Join(args, " "))))
	}

	if msg = tidy(sanitize(msg)); msg == "" {
		return sanitize(code)
	}

	return msg
}

// tidy removes what an empty argument leaves in a message: a colon or an empty pair of brackets
// at the end, and the spaces around them.
//
// Parameters:
//   - msg: message with its values filled in
func tidy(msg string) string {
	msg = strings.ReplaceAll(msg, " ()", "")
	msg = strings.ReplaceAll(msg, "\u00a0()", "")

	return strings.TrimRight(strings.TrimSpace(msg), ":\u00a0 ")
}

// cleanArgs makes the arguments of a message safe to print: control characters are replaced and
// an address loses its credentials, query and fragment.
//
// Parameters:
//   - args: arguments of a message
func (a *app) cleanArgs(args []string) []string {
	out := make([]string, len(args))

	for i, s := range args {
		out[i] = sanitize(scrubURLs(s))
	}

	return out
}

// reasonText renders a reason of a state or a warning, falling back to the message of the error
// code of the same name (the reasons of an incompatible or broken plugin are error codes).
//
// Parameters:
//   - r: the reason
func (a *app) reasonText(r plugin.Reason) string {
	args := a.reasonArgs(r)

	if msg, ok := a.tr().Code(i18n.PrefixReason, r.Code, args); ok {
		return tidy(sanitize(msg))
	}

	return a.errText(r.Code, r.Args, nil)
}

// reasonArgs prepares the arguments of a reason for its message.
//
// Parameters:
//   - r: the reason
func (a *app) reasonArgs(r plugin.Reason) []string {
	switch r.Code {
	case plugin.ReasonCapsEscalated:
		return a.cleanArgs([]string{strings.Join(r.Args, ", ")})
	case plugin.ReasonQuarantined:
		return a.cleanArgs([]string{a.word("plugin_fail_", arg(r.Args, 0)), arg(r.Args, 1)})
	}

	return a.cleanArgs(r.Args)
}

// issueText renders a finding about a package: the position when there is one, then the message.
//
// Parameters:
//   - path: key path in the manifest or the locale file, may be empty
//   - line: 1-based line, 0 when unknown
//   - code: stable code of the finding
//   - args: its arguments
func (a *app) issueText(path string, line int, code string, args []string) string {
	var b strings.Builder

	if path != "" {
		b.WriteString(sanitize(path))
	}

	if line > 0 {
		if b.Len() > 0 {
			b.WriteString(", ")
		}

		b.WriteString(a.t("cli_plugin_line", line))
	}

	if b.Len() > 0 {
		b.WriteString(a.colon() + " ")
	}

	b.WriteString(a.errText(code, args, nil))

	return b.String()
}

// fixText gives the way out of a state that is not enabled.
//
// Parameters:
//   - state: state of a plugin
//   - id: plugin id, put in the command the message suggests
//
// Returns the text and true, or false for a state with no fix.
func (a *app) fixText(state, id string) (string, bool) {
	key := "plugin_fix_" + i18n.CodeKey(state)
	if !a.tr().Has(key) {
		return "", false
	}

	if len(directives(a.t(key))) == 0 {
		return a.t(key), true
	}

	return a.t(key, sanitize(id)), true
}
