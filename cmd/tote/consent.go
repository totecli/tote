package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/totecli/tote/internal/plugin"
)

const (
	// maxViewWidth is the widest line of a consent view on a terminal.
	maxViewWidth = 80

	// minViewWidth is the narrowest width a consent view wraps at.
	minViewWidth = 40

	// minViewRoom is the least cells a line keeps for its words once the start is taken; a start
	// longer than that is not worth wrapping.
	minViewRoom = 12
)

// directives lists the letters of the Printf verbs of a message, in order, without the doubled
// percent signs.
//
// Parameters:
//   - format: message of the catalog
func directives(format string) []byte {
	var out []byte

	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			continue
		}

		if format[i+1] == '%' {
			i++

			continue
		}

		out = append(out, format[i+1])
		i++
	}

	return out
}

// viewMsg renders the message of a line of a consent view. A value that is a catalog key is
// translated, any other is the plugin's or the file's own text and was cleaned by the view; a
// value that a %d directive receives is read as a number.
//
// Parameters:
//   - key: catalog key of the message
//   - args: its values
func (a *app) viewMsg(key string, args []plugin.ViewArg) string {
	format := a.t(key)
	if len(args) == 0 {
		return format
	}

	verbs := directives(format)
	vals := make([]any, len(args))

	for i, v := range args {
		text := sanitize(v.Text)
		if v.Key != "" {
			text = a.t(v.Key)
		}

		vals[i] = text

		if i < len(verbs) && verbs[i] == 'd' {
			if n, err := strconv.Atoi(text); err == nil {
				vals[i] = n
			}
		}
	}

	return fmt.Sprintf(format, vals...)
}

// viewText gives the words of a line of a consent view: the message, or the raw text, or both.
//
// Parameters:
//   - l: the line
func (a *app) viewText(l plugin.ViewLine) string {
	switch {
	case l.Key != "" && l.Text != "":
		return sanitize(l.Text) + " (" + a.viewMsg(l.Key, l.Args) + ")"
	case l.Key != "":
		return a.viewMsg(l.Key, l.Args)
	}

	return sanitize(l.Text)
}

// viewParts splits one line of a consent view into the part that starts it and the words that
// follow. The markers are plain characters and every item carries its words: ! flags an access
// that deserves attention, + and - are the lines of a difference.
//
// Parameters:
//   - l: the line
func (a *app) viewParts(l plugin.ViewLine) (string, string) {
	tag := ""
	if l.Tag != "" {
		tag = " [" + a.t("consent_tag_"+l.Tag) + "]"
	}

	switch l.Level {
	case plugin.LevelGap:
		return "", ""
	case plugin.LevelField:
		return a.fieldLabel(l.Label), a.viewText(l)
	case plugin.LevelItem:
		return "  - ", a.viewText(l) + tag
	case plugin.LevelStrong:
		return "  ! ", a.viewText(l) + tag
	case plugin.LevelAdd:
		return "  + ", a.viewText(l) + tag
	case plugin.LevelDrop:
		return "  - ", a.viewText(l)
	case plugin.LevelWarn:
		return "! ", a.viewText(l)
	case plugin.LevelNote:
		return "    ", a.viewText(l)
	case plugin.LevelQuote:
		return "  | ", a.viewText(l)
	case plugin.LevelCmd:
		return "  $ ", a.viewText(l)
	}

	return "", a.viewText(l)
}

// viewWidth gives the width the consent view wraps at: the width of the terminal the view goes
// to, at most 80 columns, and no wrapping at all when the view goes to a file or a pipe.
//
// Parameters:
//   - w: destination of the view
func (a *app) viewWidth(w io.Writer) int {
	if !a.IsTerminal(w) {
		return 0
	}

	cols, _, ok := a.terminalSize()
	if !ok || cols > maxViewWidth {
		return maxViewWidth
	}

	return max(cols, minViewWidth)
}

// writeView prints a consent view. On a terminal a line that is too long is wrapped at a word,
// the next line starting under the first word of the first.
//
// Parameters:
//   - w: destination
//   - d: what the install, the update or the allow screen will do
//   - mode: one of the View constants of the plugin package
//   - pinned: true when the user typed the sha256 of the archive
func (a *app) writeView(w io.Writer, d plugin.Diff, mode string, pinned bool) {
	width := a.viewWidth(w)

	for _, l := range plugin.NewConsentView(d, mode, pinned).Lines {
		start, body := a.viewParts(l)

		for _, line := range wrapText(start, body, width) {
			fmt.Fprintln(w, strings.TrimRight(line, " "))
		}
	}
}

// wrapText breaks a line into lines that fit a width. The first line starts with the start text
// and the others with as many spaces. A word longer than the room is cut.
//
// Parameters:
//   - start: what the first line begins with
//   - body: the words
//   - width: most cells of a line, 0 for no limit
func wrapText(start, body string, width int) []string {
	room := width - textWidth(start)
	if width <= 0 || room < minViewRoom || textWidth(start+body) <= width {
		return []string{start + body}
	}

	var (
		lines []string
		cur   strings.Builder
		used  int
	)

	hang := strings.Repeat(" ", textWidth(start))
	prefix := start

	flush := func() {
		lines = append(lines, prefix+cur.String())
		prefix = hang

		cur.Reset()

		used = 0
	}

	for _, word := range strings.Fields(body) {
		for textWidth(word) > room {
			if used > 0 {
				flush()
			}

			cut, rest := splitWidth(word, room)
			cur.WriteString(cut)

			used = room
			word = rest
		}

		if word == "" {
			continue
		}

		if used > 0 && used+1+textWidth(word) > room {
			flush()
		}

		if used > 0 {
			cur.WriteString(" ")

			used++
		}

		cur.WriteString(word)

		used += textWidth(word)
	}

	if used > 0 {
		flush()
	}

	return lines
}

// splitWidth cuts the start of a word that fits a width.
//
// Parameters:
//   - word: text without spaces
//   - room: most cells of the first part
func splitWidth(word string, room int) (string, string) {
	used := 0

	for i, r := range word {
		w := runeWidth(r)
		if used+w > room {
			return word[:i], word[i:]
		}

		used += w
	}

	return word, ""
}

// whyLines prints why a consent is asked.
//
// Parameters:
//   - w: destination
//   - why: the Why codes of a diff
func (a *app) whyLines(w io.Writer, why []string) {
	if len(why) == 0 {
		return
	}

	fmt.Fprintln(w, a.t("cli_plugin_why_head"))

	for _, c := range why {
		fmt.Fprintln(w, "  - "+a.word("plugin_why_", c))
	}
}

// consent obtains the consent that a diff asks for.
//
// A diff that asks nothing, or a --grant token, gives the grant at once (the plugin layer checks
// the token). Otherwise a terminal gets the view on standard error and a question whose default
// is no. Without a terminal and without a token, there is nothing to ask: the grant is empty and
// the plugin layer answers consent_needed, which exits 2 and names the flag.
//
// Parameters:
//   - ctx: cancelled by Ctrl-C while the question is open
//   - d: what will be done
//   - mode: one of the View constants of the plugin package
//   - token: the --grant flag, may be empty
//   - pinned: true when the user typed --sha256
//   - ask: catalog key of the question
//
// Returns the grant and true, or false and the exit code to return when the user declined or
// interrupted the question.
func (a *app) consent(ctx context.Context, d plugin.Diff, mode, token string, pinned bool,
	ask string,
) (plugin.Grant, bool, int) {
	if !d.NeedsConsent || token != "" {
		return plugin.Grant{Token: token}, true, exitOK
	}

	if !a.IsInputTerminal(a.Stdin) {
		return plugin.Grant{}, true, exitOK
	}

	a.writeView(a.Stderr, d, mode, pinned)
	fmt.Fprintln(a.Stderr)

	if yes, rc := a.askYesIn(ctx, a.t(ask, sanitize(d.ID)), "cli_plugin_declined"); !yes {
		return plugin.Grant{}, false, rc
	}

	return plugin.Grant{Prompt: true}, true, exitOK
}
