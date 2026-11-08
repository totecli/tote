package plugin

import (
	"cmp"
	"slices"
	"strings"

	"github.com/totecli/tote/internal/shell"
)

// Modes of a consent view: a new install, an update and the allow screen of a plugin that waits
// for consent.
const (
	ViewInstall = "install"
	ViewUpdate  = "update"
	ViewAllow   = "allow"
)

// Levels of a ViewLine, which tell the edge how to lay a line out and which marker to draw.
// Strong is an item that deserves a warning marker, Add and Drop are lines of a diff, Quote is a
// line of the description box, Cmd is a command and Gap is an empty line.
const (
	LevelTitle  = "title"
	LevelField  = "field"
	LevelHead   = "head"
	LevelText   = "text"
	LevelItem   = "item"
	LevelStrong = "strong"
	LevelAdd    = "add"
	LevelDrop   = "drop"
	LevelWarn   = "warn"
	LevelNote   = "note"
	LevelQuote  = "quote"
	LevelCmd    = "cmd"
	LevelCode   = "code"
	LevelGap    = "gap"
)

// Tags of an item of the list of capabilities: whether tote enforces it or only declares it.
const (
	TagEnforced = "enforced"
	TagDeclared = "declared"
)

// Maximum sizes of the texts a view shows.
const (
	viewSourceRunes = 240
	viewTextRunes   = 200
	viewDescLines   = 3
	viewCommandRune = 400
)

// ViewArg is one value of a ViewLine message: a raw text, which the edge inserts as it is, or the
// catalog key of a word to translate first.
type ViewArg struct {
	Text string
	Key  string
}

// ViewLine is one line of a consent view, as data: the edge renders it with its catalog.
//
// Label is the catalog key of the name of a field line, Key the catalog key of the message and
// Args its values; Text is a raw value (a source, a command, a line of the description) written
// before the message, which is then shown in brackets. Tag is one of the Tag constants for an
// item of the list of capabilities. Every text that came from the plugin is cleaned and cut.
type ViewLine struct {
	Level string
	Label string
	Key   string
	Args  []ViewArg
	Text  string
	Tag   string
}

// ConsentView is what the user reads before consenting, built once for the command line and the
// screens. It is pure data: ordered lines made of catalog keys and values.
type ConsentView struct {
	Lines []ViewLine
}

// NewConsentView builds the view of a Diff.
//
// The order is the one of section 7.6: the title, the source and origin, the checksum and where it
// came from, the code line, the description in a box labelled as written by the plugin, then what
// the commands it runs in full for an install or an allow screen (they are the code of a
// declarative plugin), then what the plugin will be able to do with one line per effective
// capability grouped by kind (marked enforced or declared only), the warnings and the closing
// sentence. An update shows the added capabilities, the counts of the unchanged
// and removed ones and the diff of the commands instead of the full lists.
//
// Parameters:
//   - d: what the install, the update or the allow screen will do
//   - mode: one of ViewInstall, ViewUpdate and ViewAllow
//   - pinned: true when the user typed the sha256 of the archive (--sha256)
func NewConsentView(d Diff, mode string, pinned bool) ConsentView {
	var v ConsentView

	v.title(d, mode)
	v.identity(d, pinned)
	v.description(d)

	if mode == ViewUpdate && d.Update {
		v.access(d)
	} else {
		v.commands(d)
		v.can(d)
	}

	v.warnings(d)

	return v
}

// add appends a line.
//
// Parameters:
//   - l: line to append
func (v *ConsentView) add(l ViewLine) { v.Lines = append(v.Lines, l) }

// gap appends an empty line, unless the view already ends with one.
func (v *ConsentView) gap() {
	if n := len(v.Lines); n > 0 && v.Lines[n-1].Level == LevelGap {
		return
	}

	v.add(ViewLine{Level: LevelGap})
}

// title appends the title of the view.
//
// Parameters:
//   - d: the diff
//   - mode: mode of the view
func (v *ConsentView) title(d Diff, mode string) {
	id, ver := ViewArg{Text: d.ID}, ViewArg{Text: d.Version}

	switch {
	case mode == ViewAllow:
		v.add(ViewLine{Level: LevelTitle, Key: "consent_title_allow", Args: []ViewArg{id, ver}})
	case mode == ViewUpdate && d.Update && len(d.Added) == 0 && len(d.SurfaceChanges) == 0:
		v.add(ViewLine{
			Level: LevelTitle, Key: "consent_title_update",
			Args: []ViewArg{id, {Text: d.OldVersion}, ver},
		})
	case mode == ViewUpdate && d.Update:
		v.add(ViewLine{
			Level: LevelTitle, Key: "consent_title_escalation",
			Args: []ViewArg{id, ver},
		})
	default:
		v.add(ViewLine{Level: LevelTitle, Key: "consent_title_install", Args: []ViewArg{id, ver}})
	}

	v.gap()
}

// identity appends the source, the origin, the checksum and the code line.
//
// Parameters:
//   - d: the diff
//   - pinned: true when the user typed the sha256 of the archive
func (v *ConsentView) identity(d Diff, pinned bool) {
	if d.Source != "" {
		v.add(ViewLine{
			Level: LevelField, Label: "consent_source",
			Text: CleanLine(d.Source, viewSourceRunes),
		})
	}

	v.add(ViewLine{
		Level: LevelField, Label: "consent_origin", Text: CleanLine(d.Origin, 80),
		Key: originKey(d.Class),
	})

	sum, key := short(d.TreeSHA256), "consent_checksum_local"

	switch {
	case d.Kind == KindBundled:
		key = "consent_checksum_embedded"
	case d.ArchiveSHA256 != "" && (d.Kind == KindRelease || pinned):
		sum, key = short(d.ArchiveSHA256), "consent_checksum_pinned"
	case d.ArchiveSHA256 != "":
		sum = short(d.ArchiveSHA256)
	}

	v.add(ViewLine{
		Level: LevelField, Label: "consent_checksum", Key: key,
		Args: []ViewArg{{Text: sum}},
	})

	if d.Tier == TierExec {
		v.add(ViewLine{
			Level: LevelText, Key: "consent_code_exec",
			Args: []ViewArg{{Text: CleanLine(d.Program, 80)}},
		})
	} else {
		v.add(ViewLine{Level: LevelText, Key: "consent_code_declarative"})
	}

	v.gap()
}

// originKey gives the catalog key that names an origin class.
//
// Parameters:
//   - class: one of the Origin constants
func originKey(class string) string {
	switch class {
	case OriginOfficial:
		return "consent_origin_official"
	case OriginLocal:
		return "consent_origin_local"
	case OriginBundled:
		return "consent_origin_embedded"
	}

	return "consent_origin_community"
}

// description appends the description of the plugin in its box.
//
// Parameters:
//   - d: the diff
func (v *ConsentView) description(d Diff) {
	text := strings.TrimSpace(d.Description)
	if text == "" {
		return
	}

	v.add(ViewLine{Level: LevelHead, Key: "consent_description_head"})

	for i, line := range strings.Split(text, "\n") {
		if i == viewDescLines {
			break
		}

		v.add(ViewLine{Level: LevelQuote, Text: CleanLine(line, viewTextRunes)})
	}

	v.gap()
}

// can appends the list of what the plugin will be able to do, one line per capability.
//
// Parameters:
//   - d: the diff
func (v *ConsentView) can(d Diff) {
	v.add(ViewLine{Level: LevelHead, Key: "consent_can_head"})

	if len(d.Caps) == 0 {
		v.add(ViewLine{Level: LevelItem, Key: "consent_none"})
		v.gap()

		return
	}

	tags := map[string]bool{}

	caps := slices.Clone(d.Caps)
	slices.SortStableFunc(caps, func(x, y string) int {
		return cmp.Or(capGroup(x)-capGroup(y), strings.Compare(x, y))
	})

	for _, c := range caps {
		line := capLine(c, d.Tier)
		tags[line.Tag] = true

		v.add(line)
	}

	for _, tag := range []string{TagEnforced, TagDeclared} {
		if tags[tag] {
			v.add(ViewLine{Level: LevelNote, Key: "consent_legend_" + tag})
		}
	}

	v.gap()
}

// capGroup gives the place of a capability in the list: the terminal, the programs, the network,
// the files, the environment, then the host.
//
// Parameters:
//   - c: capability string
func capGroup(c string) int {
	switch {
	case c == CapDerivedTTY:
		return 0
	case c == CapDerivedCode, strings.HasPrefix(c, PermExec+":"):
		return 1
	case strings.HasPrefix(c, PermNet+":"):
		return 2
	case strings.HasPrefix(c, "fs:"):
		return 3
	case strings.HasPrefix(c, PermEnv+":"):
		return 4
	}

	return 5
}

// capLine builds the line of one capability.
//
// Parameters:
//   - c: capability string
//   - tier: tier of the plugin, which decides whether tote enforces exec: and env:
func capLine(c, tier string) ViewLine {
	l := ViewLine{Level: LevelItem, Tag: TagDeclared}
	if Enforced(c, tier) {
		l.Tag = TagEnforced
	}

	if Broad(c) || c == CapDerivedTTY {
		l.Level = LevelStrong
	}

	p, perr := ParsePermission(c)

	switch {
	case c == CapDerivedTTY:
		l.Key = "consent_cap_tty"
	case c == CapDerivedCode:
		l.Key = "consent_cap_code"
	case perr != nil:
		l.Key, l.Args = "consent_cap_other", []ViewArg{{Text: CleanLine(c, 80)}}
	default:
		l.Key, l.Args = permKey(p)
		l.Level = strongIf(l.Level, c, p)
	}

	return l
}

// strongIf raises the level of a capability that is sensitive: a launcher that runs any program
// or a variable that may hold a secret.
//
// Parameters:
//   - level: level so far
//   - c: capability string
//   - p: its parsed form
func strongIf(level, c string, p Permission) string {
	switch {
	case p.Kind == PermExec && IsShellName(p.Arg):
		return LevelStrong
	case p.Kind == PermEnv && SensitiveEnv(p.Arg):
		return LevelStrong
	case Broad(c):
		return LevelStrong
	}

	return level
}

// permKey gives the catalog key and the values of the line of a parsed permission.
//
// Parameters:
//   - p: permission of the closed vocabulary
func permKey(p Permission) (string, []ViewArg) {
	arg := []ViewArg{{Text: CleanLine(p.Arg, 80)}}

	switch p.Kind {
	case PermExec:
		if IsShellName(p.Arg) {
			return "consent_cap_exec_broad", arg
		}

		return "consent_cap_exec", arg
	case PermNet:
		return "consent_cap_net", arg
	case PermFSRead:
		return "consent_cap_fs_read", []ViewArg{{Key: "consent_zone_" + p.Arg}}
	case PermFSWrite:
		return "consent_cap_fs_write", []ViewArg{{Key: "consent_zone_" + p.Arg}}
	case PermEnv:
		if SensitiveEnv(p.Arg) {
			return "consent_cap_env_sensitive", arg
		}

		return "consent_cap_env", arg
	case PermReadRepos:
		return "consent_cap_read_repos", nil
	case PermOpenURL:
		return "consent_cap_openurl", nil
	}

	return "consent_cap_other", []ViewArg{{Text: CleanLine(p.Raw, 80)}}
}

// commands appends every command the plugin runs, in full: they are the code of a declarative
// plugin.
//
// Parameters:
//   - d: the diff
func (v *ConsentView) commands(d Diff) {
	if len(d.Commands) == 0 {
		return
	}

	v.add(ViewLine{Level: LevelHead, Key: "consent_cmds_head"})

	for _, c := range d.Commands {
		v.add(commandLine(LevelCmd, c))

		for _, extra := range commandExtras(c) {
			v.add(extra)
		}
	}

	v.gap()
}

// commandLine writes a command for the view: its table, its id, then the template as it would be
// typed. The table is a catalog key, so that the word is in the language of the run.
//
// Parameters:
//   - level: level of the line
//   - c: command
func commandLine(level string, c CommandRef) ViewLine {
	body := ""

	switch {
	case c.Open != "":
		body = "open " + c.Open
	case len(c.Run) > 0:
		body = shell.PrettyCommand(c.Run)
	}

	return ViewLine{Level: level, Key: "consent_cmd_line", Args: []ViewArg{
		{Key: "plugin_table_" + strings.ReplaceAll(c.Table, ".", "_")},
		{Text: CleanLine(c.ID, viewTextRunes)},
		{Text: CleanLine(body, viewCommandRune)},
	}}
}

// commandExtras lists the parts of a command that change what it runs: its directory, the names
// it lets through the dash rule and its environment table.
//
// Parameters:
//   - c: command
func commandExtras(c CommandRef) []ViewLine {
	var out []ViewLine

	if c.Cwd != "" {
		out = append(out, ViewLine{
			Level: LevelNote, Key: "consent_cmd_cwd",
			Args: []ViewArg{{Text: CleanLine(c.Cwd, viewTextRunes)}},
		})
	}

	if len(c.AllowDash) > 0 {
		out = append(out, ViewLine{
			Level: LevelNote, Key: "consent_cmd_dash",
			Args: []ViewArg{{Text: CleanLine(strings.Join(c.AllowDash, ", "), viewTextRunes)}},
		})
	}

	for _, e := range c.Env {
		out = append(out, ViewLine{
			Level: LevelNote, Key: "consent_cmd_env",
			Args: []ViewArg{{Text: CleanLine(e, viewTextRunes)}},
		})
	}

	return out
}

// access appends the escalation view of an update: the added capabilities, the counts of the
// unchanged and removed ones, what asks again about the commands, and the diff of the commands.
//
// Parameters:
//   - d: the diff
func (v *ConsentView) access(d Diff) {
	if len(d.Added) > 0 {
		v.add(ViewLine{Level: LevelHead, Key: "consent_diff_added"})

		for _, c := range d.Added {
			l := capLine(c, d.Tier)
			l.Level = LevelAdd
			v.add(l)
		}
	}

	v.add(ViewLine{
		Level: LevelText, Key: "consent_diff_kept",
		Args: []ViewArg{{Text: itoa(d.Unchanged)}},
	})
	v.add(ViewLine{
		Level: LevelText, Key: "consent_diff_removed",
		Args: []ViewArg{{Text: itoa(len(d.Removed))}},
	})

	for _, s := range d.SurfaceChanges {
		v.add(ViewLine{
			Level: LevelStrong, Key: "plugin_esc_" + s.Kind,
			Args: []ViewArg{{Text: CleanLine(s.Table, 40)}, {Text: CleanLine(s.ID, 80)}},
		})
	}

	v.commandDiff(d.CommandChanges)
	v.gap()
}

// commandDiff appends the counts of the changed commands and each change, old and new.
//
// Parameters:
//   - changes: differences between the commands of the two versions
func (v *ConsentView) commandDiff(changes []CommandChange) {
	var added, removed, changed int

	for _, c := range changes {
		switch c.Kind {
		case "added":
			added++
		case "removed":
			removed++
		default:
			changed++
		}
	}

	v.add(ViewLine{
		Level: LevelText, Key: "consent_diff_cmds",
		Args: []ViewArg{{Text: itoa(added)}, {Text: itoa(removed)}, {Text: itoa(changed)}},
	})

	for _, c := range changes {
		if c.Kind != "added" {
			v.add(commandLine(LevelDrop, c.Old))
		}

		if c.Kind != "removed" {
			v.add(commandLine(LevelAdd, c.New))
		}
	}
}

// warnings appends the warnings and the closing sentence.
//
// Parameters:
//   - d: the diff
func (v *ConsentView) warnings(d Diff) {
	if len(d.Missing) > 0 {
		bins := make([]string, len(d.Missing))
		for i, m := range d.Missing {
			bins[i] = m.Bin
		}

		v.add(ViewLine{
			Level: LevelWarn, Key: "consent_missing_dep",
			Args: []ViewArg{{Text: CleanLine(strings.Join(bins, ", "), viewTextRunes)}},
		})

		for _, m := range d.Missing {
			if m.Hint != "" {
				v.add(ViewLine{
					Level: LevelNote, Key: "consent_hint",
					Args: []ViewArg{{Text: CleanLine(m.Hint, viewTextRunes)}},
				})
			}
		}
	}

	for _, w := range d.Warnings {
		if l, ok := warningLine(w); ok {
			v.add(l)
		}
	}

	closing := "consent_warn_declarative"
	if d.Tier == TierExec {
		closing = "consent_warn_exec"
	}

	v.gap()
	v.add(ViewLine{Level: LevelWarn, Key: closing})
}

// warningLine builds the line of a warning of a diff. Only the four warnings of the update rules
// are shown here: the missing programs have their own lines.
//
// Parameters:
//   - w: warning of the diff
func warningLine(w Reason) (ViewLine, bool) {
	args := make([]ViewArg, len(w.Args))
	for i, a := range w.Args {
		args[i] = ViewArg{Text: CleanLine(a, viewTextRunes)}
	}

	switch w.Code {
	case WarnSourceChanged:
		return ViewLine{Level: LevelWarn, Key: "consent_warn_source_changed", Args: args}, true
	case WarnDowngrade:
		return ViewLine{Level: LevelWarn, Key: "consent_warn_downgrade", Args: args}, true
	case WarnSameVersion:
		return ViewLine{
			Level: LevelWarn, Key: "consent_warn_same_version",
			Args: args[:min(len(args), 1)],
		}, true
	case WarnPrerelease:
		return ViewLine{Level: LevelWarn, Key: "consent_warn_prerelease", Args: args}, true
	}

	return ViewLine{}, false
}
