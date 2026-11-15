package plugin

import (
	"bytes"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits of the suggestions of a field.
const (
	// ChoicesStdoutCap is the most a suggestions program may write to its standard output. A
	// program that writes more fails the whole list: a list is never cut short.
	ChoicesStdoutCap = 64 << 10
	// choicesStderrCap is the most a suggestions program may write to its standard error; only a
	// tail of it is ever shown, so a program that writes more than this is stopped and fails.
	choicesStderrCap = 1 << 20
	// ChoiceMaxValue is the longest value of a suggestion, in bytes.
	ChoiceMaxValue = 128
	// ChoiceMaxEntries is the number of suggestions kept; the others are only counted.
	ChoiceMaxEntries = 200
	// ChoiceMaxLabel is the longest label of a suggestion, in runes.
	ChoiceMaxLabel = 60
	// choiceDetailRunes is the longest reason line a failure shows, in runes.
	choiceDetailRunes = 200
)

// States of a ChoicesResult.
const (
	// ChoicesList: the program listed at least one suggestion.
	ChoicesList = "list"
	// ChoicesEmpty: the program succeeded and no suggestion is left, Hidden says how many it printed
	// that the rules refused.
	ChoicesEmpty = "empty"
	// ChoicesFailed: the program did not run or did not succeed; Reason says why.
	ChoicesFailed = "failed"
	// ChoicesTimedOut: the program was stopped because it ran longer than the time limit.
	ChoicesTimedOut = "timeout"
	// ChoicesCancelled: the caller cancelled the run.
	ChoicesCancelled = "cancelled"
)

// Reasons of a failed ChoicesResult. The catalogs have a message plugin_choices_<reason> for each.
const (
	// ChoicesReasonExit: the program exited with a status other than 0; Arg is the status.
	ChoicesReasonExit = "exit"
	// ChoicesReasonSignal: a signal ended the program; Arg is its name, such as KILL.
	ChoicesReasonSignal = "signal"
	// ChoicesReasonTooLarge: the program wrote more than ChoicesStdoutCap to its standard output,
	// or more than 1 MiB to its standard error.
	ChoicesReasonTooLarge = "too_large"
	// ChoicesReasonStart: the program could not be started.
	ChoicesReasonStart = "start"
	// ChoicesReasonState: the plugin cannot run any more (disabled, removed, changed on disk);
	// Arg is the code of the refusal.
	ChoicesReasonState = "state"
	// ChoicesReasonBusy: four plugin programs are already running.
	ChoicesReasonBusy = "busy"
	// ChoicesReasonRefused: the run was not one that Registry.Choices prepared.
	ChoicesReasonRefused = "refused"
)

// Choice is one suggestion: the value that fills the field, and a label to show beside it. The
// value is exactly what the program printed. The label is cleaned and may be empty.
type Choice struct {
	Value string
	Label string
}

// ChoiceRules are the rules of the field that a suggested value must satisfy to be listed: the
// ones the field applies to a typed value, so that no suggestion is refused when it is picked.
// MaxLen is in runes, 0 for no limit; Pattern is the pattern of the manifest, empty for none.
type ChoiceRules struct {
	MaxLen  int
	Pattern string
}

// ChoiceList is what ParseChoices keeps of the output of a program. Hidden counts the entries
// that were dropped, More the valid entries after the first ChoiceMaxEntries, which are not kept.
// A repeated value and an empty line are neither: they are not information that was lost.
type ChoiceList struct {
	Items  []Choice
	Hidden int
	More   int
}

// ChoicesResult is how a suggestions run ended. State is one of ChoicesList, ChoicesEmpty,
// ChoicesFailed, ChoicesTimedOut and ChoicesCancelled.
//
// Items, Hidden and More are the list of ChoiceList, set for a list and for an empty result. A
// failed result has Reason, one of the ChoicesReason constants, Arg, the status or signal or
// refusal code it goes with, Detail, the line of the program that says what went wrong (cleaned,
// redacted and cut, never raw output, empty when the program said nothing), and Err, an *Error
// with the code choices_failed whose Args are the field, the reason and the detail. A result
// never carries the output of a program that failed or was stopped, even when it had printed
// lines: the list is discarded whole.
type ChoicesResult struct {
	State    string
	Items    []Choice
	Hidden   int
	More     int
	Reason   string
	Arg      string
	Detail   string
	Err      *Error
	Duration time.Duration
}

// ParseChoices reads the standard output of a suggestions program. It works on the raw bytes: a
// value is never cleaned, converted or redacted, only accepted or dropped.
//
// The output is UTF-8 in lines that end with a line feed; a last line without one counts, and an
// empty line is ignored. A line is a value, or a value, a TAB and a label; a second TAB drops the
// entry. A value is dropped when it is empty, longer than ChoiceMaxValue bytes or than the
// MaxLen of the rules, not valid UTF-8, contains a space of any kind, contains a control, escape,
// bidirectional, zero-width or other format character (so a carriage return too: a program that
// ends its lines with CR LF lists nothing), or does not match the pattern. The first of a repeated
// value is kept. The first ChoiceMaxEntries entries are kept; the valid ones after them are
// counted in More.
//
// Parameters:
//   - raw: bytes the program wrote to its standard output
//   - rules: rules of the field
//   - rd: redactor for the labels, which are shown; may be nil
func ParseChoices(raw []byte, rules ChoiceRules, rd *Redactor) ChoiceList {
	var out ChoiceList

	seen := map[string]bool{}

	for len(raw) > 0 {
		var line []byte

		line, raw, _ = bytes.Cut(raw, []byte{'\n'})
		if len(line) == 0 {
			continue
		}

		value, label, ok := splitChoice(line)

		switch {
		case !ok || !validChoice(value, rules):
			out.Hidden++
		case seen[value]:
		case len(out.Items) >= ChoiceMaxEntries:
			seen[value] = true
			out.More++
		default:
			seen[value] = true
			out.Items = append(out.Items, Choice{Value: value, Label: choiceLabel(label, rd)})
		}
	}

	return out
}

// splitChoice cuts a line at its first TAB.
//
// Parameters:
//   - line: line without its line feed
//
// Returns the value, the raw label, and false when the label holds another TAB.
func splitChoice(line []byte) (string, []byte, bool) {
	value, label, found := bytes.Cut(line, []byte{'\t'})
	if found && bytes.IndexByte(label, '\t') >= 0 {
		return "", nil, false
	}

	return string(value), label, true
}

// validChoice applies the rules of a value.
//
// Parameters:
//   - v: value, any bytes
//   - rules: rules of the field
func validChoice(v string, rules ChoiceRules) bool {
	if v == "" || ProducedIssue(v, ChoiceMaxValue) != "" {
		return false
	}

	if strings.IndexFunc(v, func(r rune) bool { return unicode.IsSpace(r) || blankRune(r) }) >= 0 {
		return false
	}

	if rules.MaxLen > 0 && utf8.RuneCountInString(v) > rules.MaxLen {
		return false
	}

	return rules.Pattern == "" || matchPattern(rules.Pattern, v)
}

// blankRune reports whether a character draws nothing, or a blank, although it is not a space or a
// control character: a value that holds one looks like another value in the picker and in the
// field, as "ma<ZWJ>in" would draw as "main". The code points are the joiners, the fillers, the
// braille blank and the variation selectors. Homoglyphs are not stopped here, a pattern anchored
// to an ASCII class does that.
//
// Parameters:
//   - r: character
func blankRune(r rune) bool {
	switch {
	case r == 0x034F, r == 0x115F, r == 0x1160, r == 0x17B4, r == 0x17B5, r == 0x200C,
		r == 0x200D, r == 0x2800, r == 0x3164, r == 0xFFA0:
		return true
	case r >= 0x180B && r <= 0x180F, r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF:
		return true
	}

	return false
}

// choiceLabel makes the text that is shown beside a value from what the program wrote after the
// TAB: redacted, stripped of escape sequences, redacted again, cleaned and cut. The second pass
// catches a secret that an escape sequence split in two. It is shown and never matched, and it
// never reaches an argv.
//
// Parameters:
//   - raw: bytes after the TAB
//   - rd: redactor, may be nil
func choiceLabel(raw []byte, rd *Redactor) string {
	s := rd.Scrub(FilterOutput(rd.Scrub(string(raw)), false))

	return strings.TrimSpace(CleanLine(s, ChoiceMaxLabel))
}

// choiceDetail picks the line that tells why a program failed from what it wrote: the last
// non-empty line of its standard error, else the first non-empty line of its standard output,
// where some programs report an unknown option. Both are cleaned text already.
//
// Parameters:
//   - stderr: cleaned tail of the standard error
//   - stdout: cleaned tail of the standard output
func choiceDetail(stderr, stdout string) string {
	lines := func(s string) []string {
		var out []string

		for _, l := range strings.Split(s, "\n") {
			l = strings.TrimSpace(strings.ReplaceAll(l, "\t", " "))
			if l != "" {
				out = append(out, CleanLine(l, choiceDetailRunes))
			}
		}

		return out
	}

	if l := lines(stderr); len(l) > 0 {
		return l[len(l)-1]
	}

	if l := lines(stdout); len(l) > 0 {
		return l[0]
	}

	return ""
}
