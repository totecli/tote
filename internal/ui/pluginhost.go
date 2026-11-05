package ui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/plugin"
)

// PluginHost is the part of the plugin registry that the interface uses. *plugin.Registry
// satisfies it, so the screens call the same functions as the plugin verbs of the command line,
// and a test can wrap a registry to slow down or fail one call.
//
// Load reads every installed plugin and Available lists the plugins shipped with tote that are not
// installed. Recheck reads one plugin again from the files. Plan stages an install or an update
// and Allow, Grant, Enable, Disable and Remove change one installed plugin. Check examines a
// plugin and runs nothing. Settings reads the effective settings of a plugin and SaveSettings
// writes its settings file. Defaults gives the starting values of the form of a contribution.
//
// Offers lists the commands of the enabled plugins and RepoOffers the actions that apply to a saved
// repository, with whether each can run. Prepare checks a call and builds the run, Guard checks
// the state of the plugin again just before a program starts, and Run executes a prepared run that
// is not in the terminal.
type PluginHost interface {
	Load() plugin.Snapshot
	Available(snap plugin.Snapshot, lang string) []plugin.Available
	Recheck(id string) (plugin.Plugin, bool)
	Plan(ctx context.Context, req plugin.InstallRequest) (*plugin.Staged, error)
	Allow(id string) (plugin.Diff, error)
	Grant(id string, g plugin.Grant) (plugin.GrantResult, error)
	Enable(id string, g plugin.Grant) (plugin.EnableResult, error)
	Disable(id string) (plugin.DisableResult, error)
	Remove(id string, o plugin.RemoveOptions) (plugin.RemoveResult, error)
	Check(ctx context.Context, req plugin.CheckRequest) (*plugin.CheckResult, error)
	Settings(p plugin.Plugin) plugin.SettingsView
	SaveSettings(p plugin.Plugin, values map[string]any) error
	Defaults(call plugin.Call) (map[string]string, error)
	Offers(snap plugin.Snapshot, lang string) []plugin.Offer
	RepoOffers(snap plugin.Snapshot, repo, lang string) []plugin.Offer
	Prepare(call plugin.Call) (plugin.Prepared, error)
	Guard(p plugin.Prepared) *plugin.Error
	Run(ctx context.Context, p plugin.Prepared, emit func(plugin.Event)) plugin.Outcome
}

// _ checks at compile time that the registry of the plugin package is a PluginHost.
var _ PluginHost = (*plugin.Registry)(nil)

// pluginCache holds the counts of the plugins that the welcome page shows and the commands that
// the palette offers, so that drawing a frame reads no file. They are read by a command, after the
// first frame and again after a plugin changed; Gen tells a read that was started before the last
// change from a current one.
type pluginCache struct {
	host    PluginHost
	lang    string
	counts  plugin.Counts
	offers  []plugin.Offer
	gen     int
	known   bool
	loading bool
}

// pluginCountsMsg is the answer of the command that reads the counts and the commands of the
// plugins.
type pluginCountsMsg struct {
	Counts plugin.Counts
	Offers []plugin.Offer
	Gen    int
}

// summary returns the counts of the plugins and whether they are known yet.
func (c *pluginCache) summary() (plugin.Counts, bool) {
	return c.counts, c.known
}

// commands returns the commands of the enabled plugins as they were last read, none while they
// are not known.
func (c *pluginCache) commands() []plugin.Offer {
	if c == nil {
		return nil
	}

	return c.offers
}

// drop forgets the counts, so that they are read again.
func (c *pluginCache) drop() {
	c.known = false
	c.gen++
}

// fetch returns the command that reads the counts, or nil when they are known or being read.
func (c *pluginCache) fetch() tea.Cmd {
	if c == nil || c.known || c.loading {
		return nil
	}

	c.loading = true
	host, gen, lang := c.host, c.gen, c.lang

	return func() tea.Msg {
		snap := host.Load()

		return pluginCountsMsg{Counts: snap.Count(), Offers: host.Offers(snap, lang), Gen: gen}
	}
}

// arrived takes the answer of a read: it is kept when no plugin changed since it started.
//
// Parameters:
//   - m: the answer
func (c *pluginCache) arrived(m pluginCountsMsg) {
	c.loading = false

	if m.Gen == c.gen {
		c.counts, c.offers, c.known = m.Counts, m.Offers, true
	}
}

// pluginsOn reports whether the plugin screens exist in this run.
func (e *env) pluginsOn() bool {
	return e.Plugins != nil
}

// pluginWords sets what the plugins add to the interface in a language: the range of the jump keys
// on the footer, and the language of the commands that the palette offers, which are read again.
// It does nothing when plugins are off.
//
// Parameters:
//   - lang: language of the interface
func (e *env) pluginWords(lang string) {
	if e.Plugins == nil {
		return
	}

	e.Keys.Jump.SetHelp("1-5", e.Tr.T("ui_help_jump"))

	if e.Plug != nil {
		e.Plug.lang = lang
		e.Plug.drop()
	}
}

// pluginsChanged tells that a plugin changed, so that the counts of the welcome page are read
// again.
func (e *env) pluginsChanged() {
	if e.Plug != nil {
		e.Plug.drop()
	}
}

// pword translates one word of a family of the plugin catalog from its stable code.
//
// Parameters:
//   - prefix: catalog prefix of the family, such as plugin_unsafe_
//   - code: stable code of the word
//
// Returns the word, or the code itself when the family has none for it, sanitized.
func (e *env) pword(prefix, code string) string {
	msg, _ := e.Tr.Code(prefix, code, nil)

	return sanitize(msg)
}

// pstate names a plugin state in the language of the interface.
//
// Parameters:
//   - state: state of a plugin, or "available" for a plugin shipped with tote and not installed
func (e *env) pstate(state string) string {
	return e.pword(i18n.PrefixState, state)
}

// pfix gives the way out of a state that is not enabled, as the command line words it.
//
// Parameters:
//   - state: state of a plugin
//   - id: plugin id, put in the sentence when it has a place for it
//
// Returns the text and true, or false for a state with no fix.
func (e *env) pfix(state, id string) (string, bool) {
	key := "plugin_fix_" + i18n.CodeKey(state)
	if !e.Tr.Has(key) {
		return "", false
	}

	if len(verbs(e.t(key))) == 0 {
		return sanitize(e.t(key)), true
	}

	return sanitize(e.t(key, sanitize(id))), true
}

// verbs lists the letters of the Printf verbs of a message, in order, without the doubled percent
// signs.
//
// Parameters:
//   - format: message of the catalog
func verbs(format string) []byte {
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

// argAt gives one argument of a code, or an empty text.
//
// Parameters:
//   - args: arguments of the code
//   - i: index of the wanted one
func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}

	return ""
}

// prepArgs prepares the arguments of an error for its message: the words that are codes are
// translated, the lists are joined and the system error is added where the message shows it.
//
// Parameters:
//   - code: stable code of the error
//   - args: arguments of the error
//   - cause: the error under it, may be nil
func (e *env) prepArgs(code string, args []string, cause error) []string {
	switch code {
	case plugin.CodeArchiveFormat:
		return []string{e.pword("plugin_archive_", argAt(args, 1))}
	case plugin.CodeArchiveUnsafe:
		return []string{e.entryName(argAt(args, 0)), e.pword("plugin_unsafe_", argAt(args, 1))}
	case plugin.CodeArchiveLimits:
		return []string{e.entryName(argAt(args, 0)), e.pword("plugin_limit_", argAt(args, 1))}
	case plugin.CodeSourceUnsafe:
		return []string{argAt(args, 0), e.pword("plugin_unsafe_", argAt(args, 1))}
	case plugin.CodeSourceLimits:
		return []string{argAt(args, 0), e.pword("plugin_limit_", argAt(args, 1))}
	case plugin.CodeIDInvalid:
		return []string{argAt(args, 0), e.pword("plugin_idwhy_", argAt(args, 1))}
	case plugin.CodeInputInvalid:
		return []string{argAt(args, 0), e.pword("plugin_reject_", argAt(args, 1))}
	case plugin.CodeSettingsFile:
		return []string{e.pword("plugin_reject_", argAt(args, 0))}
	case plugin.CodeEnableRefused:
		return e.refusalArgs(args, cause)
	case plugin.CodeLimitExceeded:
		return []string{strings.Join(args, " ")}
	case plugin.CodeValueInvalid:
		return []string{strings.Join(args, ", ")}
	case plugin.CodeDisk, plugin.CodeNetOffline, plugin.CodeNetTLS:
		return []string{argAt(args, 0), causeText(cause)}
	case plugin.CodeNotRunnable:
		return []string{argAt(args, 0), e.pstate(argAt(args, 1))}
	case plugin.CodeDepMissing:
		hint := ""
		if h := argAt(args, 1); h != "" {
			hint = e.t("plugin_hint_label", sanitize(h))
		}

		return []string{argAt(args, 0), hint}
	}

	return args
}

// maxCause is the most runes of the system error that a message appends to its arguments.
const maxCause = 160

// causeText writes the system error under an *Error as one short line.
//
// Parameters:
//   - err: the Err of an *Error; nil gives an empty text
func causeText(err error) string {
	if err == nil {
		return ""
	}

	return plugin.CleanLine(err.Error(), maxCause)
}

// entryName names the entry of an archive that an error is about, the archive itself when the
// error has no entry.
//
// Parameters:
//   - name: path of the entry, possibly empty
func (e *env) entryName(name string) string {
	if name == "" {
		return e.t("plugin_word_archive")
	}

	return name
}

// refusalArgs prepares the arguments of an enable that was refused: the plugin and its reasons as
// one text.
//
// Parameters:
//   - codes: reason codes, the arguments of the error
//   - cause: the error under it, a *plugin.ReasonsError when the plugin layer built it
func (e *env) refusalArgs(codes []string, cause error) []string {
	var re *plugin.ReasonsError
	if !errors.As(cause, &re) {
		re = &plugin.ReasonsError{}

		for _, c := range codes {
			re.Reasons = append(re.Reasons, plugin.Reason{Code: c})
		}
	}

	texts := make([]string, len(re.Reasons))
	for i, r := range re.Reasons {
		texts[i] = e.reasonText(r)
	}

	return []string{re.ID, strings.Join(texts, "; ")}
}

// codeText renders the message of an error code with its arguments.
//
// Parameters:
//   - code: stable code
//   - args: arguments of the code
//   - cause: the error under it, may be nil
//
// Returns the message in the language of the interface, or the code with its arguments when the
// catalog has no message for it, so that nothing is ever lost.
func (e *env) codeText(code string, args []string, cause error) string {
	if code == plugin.CodeBadSource && errors.Is(cause, fs.ErrNotExist) {
		return sanitize(e.t("cli_plugin_source_missing", argAt(args, 0)))
	}

	if code == plugin.CodeBadSource && errors.Is(cause, fs.ErrPermission) {
		return sanitize(e.t("cli_plugin_source_unreadable", argAt(args, 0)))
	}

	if code == plugin.CodeUnavailable && argAt(args, 1) == "empty" {
		if argAt(args, 0) == "repo.slug" {
			return sanitize(e.t("ui_plug_err_noslug"))
		}

		return sanitize(e.t("ui_plug_err_empty", sanitize(argAt(args, 0))))
	}

	clean := e.prepArgs(code, args, cause)
	for i, a := range clean {
		clean[i] = sanitize(a)
	}

	if key, ok := noForceKeys[code]; ok {
		vals := make([]any, len(clean))
		for i, a := range clean {
			vals[i] = a
		}

		return tidyMsg(sanitize(e.t(key, vals...)))
	}

	msg, ok := e.Tr.Code(i18n.PrefixError, code, clean)
	if !ok {
		return sanitize(strings.TrimSpace(code + " " + strings.Join(args, " ")))
	}

	if msg = tidyMsg(sanitize(msg)); msg == "" {
		return sanitize(code)
	}

	return msg
}

// noForceKeys maps the codes whose message tells to use --force to the message of the interface,
// which has no such option and says to use the command line.
var noForceKeys = map[string]string{
	plugin.CodeDowngrade:     "ui_plug_err_downgrade",
	plugin.CodePrerelease:    "ui_plug_err_prerelease",
	plugin.CodeSourceChanged: "ui_plug_err_source_changed",
}

// tidyMsg removes what an empty argument leaves in a message: an empty pair of brackets and a colon
// at the end, and the spaces around them.
//
// Parameters:
//   - msg: message with its values filled in
func tidyMsg(msg string) string {
	msg = strings.ReplaceAll(msg, " ()", "")
	msg = strings.ReplaceAll(msg, "\u00a0()", "")

	return strings.TrimRight(strings.TrimSpace(msg), ":\u00a0 ")
}

// errorText renders the message of an error of the plugin layer. An error that is not an
// *plugin.Error gives its own text, cleaned.
//
// Parameters:
//   - err: the error
func (e *env) errorText(err error) string {
	var pe *plugin.Error
	if errors.As(err, &pe) {
		return e.fold(e.codeText(pe.Code, pe.Args, pe.Err))
	}

	return e.fold(sanitize(plugin.CleanLine(err.Error(), maxCause)))
}

// reasonText renders a reason of a state or a warning, falling back to the message of the error
// code of the same name (the reasons of an incompatible or broken plugin are error codes).
//
// Parameters:
//   - r: the reason
func (e *env) reasonText(r plugin.Reason) string {
	var args []string

	switch r.Code {
	case plugin.ReasonCapsEscalated:
		args = []string{strings.Join(r.Args, ", ")}
	case plugin.ReasonQuarantined:
		args = []string{e.pword("plugin_fail_", argAt(r.Args, 0)), argAt(r.Args, 1)}
	default:
		args = r.Args
	}

	clean := make([]string, len(args))
	for i, a := range args {
		clean[i] = sanitize(a)
	}

	if msg, ok := e.Tr.Code(i18n.PrefixReason, r.Code, clean); ok {
		return tidyMsg(sanitize(msg))
	}

	return e.codeText(r.Code, r.Args, nil)
}

// findingText gives the message of a finding of a check. A finding of the state or the
// environment scope carries a reason code of the state machine, the others an error or issue code.
//
// Parameters:
//   - f: the finding
func (e *env) findingText(f plugin.Finding) string {
	if f.Scope == plugin.ScopeState || f.Scope == plugin.ScopeEnv {
		return e.reasonText(plugin.Reason{Code: f.Code, Args: f.Args})
	}

	args := f.Args

	switch f.Code {
	case plugin.CodeSettingsIgnore:
		args = []string{
			argAt(f.Args, 0), e.pword("plugin_source_", argAt(f.Args, 1)),
			e.pword("plugin_reject_", argAt(f.Args, 2)),
		}
	case plugin.CodeSettingsFile:
		args = []string{e.pword("plugin_reject_", argAt(f.Args, 0))}
	}

	text := e.codeText(f.Code, args, nil)

	where := ""
	if f.Path != "" {
		where = sanitize(f.Path)
	}

	if f.Line > 0 {
		if where != "" {
			where += ", "
		}

		where += e.t("cli_plugin_line", f.Line)
	}

	if where != "" {
		return where + e.colon() + " " + text
	}

	return text
}

// colon gives the punctuation that follows a label: a colon, preceded by a no-break space in
// French.
func (e *env) colon() string {
	if e.Tr.Lang() == "fr" {
		return e.fold("\u00a0:")
	}

	return ":"
}

// viewMsg renders the message of a line of a consent view. A value that is a catalog key is
// translated, any other is the plugin's or the file's own text and was cleaned by the view; a
// value that a %d directive receives is read as a number.
//
// Parameters:
//   - key: catalog key of the message
//   - args: its values
func (e *env) viewMsg(key string, args []plugin.ViewArg) string {
	format := e.t(key)
	if len(args) == 0 {
		return sanitize(format)
	}

	vs := verbs(format)
	vals := make([]any, len(args))

	for i, v := range args {
		text := sanitize(v.Text)
		if v.Key != "" {
			text = e.t(v.Key)
		}

		vals[i] = text

		if i < len(vs) && vs[i] == 'd' {
			if n, err := strconv.Atoi(text); err == nil {
				vals[i] = n
			}
		}
	}

	return sanitize(fmt.Sprintf(format, vals...))
}

// viewText gives the words of a line of a consent view: the message, or the raw text, or both.
//
// Parameters:
//   - l: the line
func (e *env) viewText(l plugin.ViewLine) string {
	switch {
	case l.Key != "" && l.Text != "":
		return sanitize(l.Text) + " (" + e.viewMsg(l.Key, l.Args) + ")"
	case l.Key != "":
		return e.viewMsg(l.Key, l.Args)
	}

	return sanitize(l.Text)
}

// viewParts splits one line of a consent view into the part that starts it and the words that
// follow. The markers are plain characters and every item carries its words: ! flags an access
// that deserves attention, + and - are the lines of a difference.
//
// Parameters:
//   - l: the line
func (e *env) viewParts(l plugin.ViewLine) (string, string) {
	tag := ""
	if l.Tag != "" {
		tag = " [" + e.t("consent_tag_"+l.Tag) + "]"
	}

	switch l.Level {
	case plugin.LevelGap:
		return "", ""
	case plugin.LevelField:
		return e.t(l.Label) + e.colon() + " ", e.viewText(l)
	case plugin.LevelItem:
		return "  - ", e.viewText(l) + tag
	case plugin.LevelStrong:
		return "  ! ", e.viewText(l) + tag
	case plugin.LevelAdd:
		return "  + ", e.viewText(l) + tag
	case plugin.LevelDrop:
		return "  - ", e.viewText(l)
	case plugin.LevelWarn:
		return "! ", e.viewText(l)
	case plugin.LevelNote:
		return "    ", e.viewText(l)
	case plugin.LevelQuote:
		return "  | ", e.viewText(l)
	case plugin.LevelCmd:
		return "  $ ", e.viewText(l)
	}

	return "", e.viewText(l)
}

// healthOf maps the state of a plugin to the role that colors it.
//
// Parameters:
//   - state: state of a plugin, or "available"
func healthOf(state string) health {
	switch plugin.State(state) {
	case plugin.StateEnabled:
		return healthOK
	case plugin.StateDisabled:
		return healthMuted
	case plugin.StateNeedsConsent, plugin.StateMissingDependency:
		return healthWarn
	case plugin.StateBroken, plugin.StateIncompatible, plugin.StateQuarantined:
		return healthErr
	}

	return healthMuted
}
