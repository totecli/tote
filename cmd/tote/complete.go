package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/totecli/tote/internal/store"
)

// verbFlags lists the options offered after the verbs that have some, under the canonical name
// of the verb: every spelling the command table accepts is resolved to it first.
var verbFlags = map[string][]string{
	"doctor":    {"--json", "--reset"},
	"update":    {"--check", "--json", "--beta", "--force"},
	"uninstall": {"--yes", "--shell", "--purge", "--dry-run"},
	"alias":     {"--format", "--json"},
	"funcs":     {"--format", "--json"},
	"repos":     {"--format", "--json"},
	"co":        {"--format", "--json"},

	// The options of the plugin subverbs, under the key "plugin <subverb>".
	"plugin list":    {"--format", "--json"},
	"plugin info":    {"--json"},
	"plugin install": {"--sha256", "--grant", "--dry-run", "--force", "--json"},
	"plugin update":  {"--sha256", "--grant", "--dry-run", "--force", "--json"},
	"plugin enable":  {"--grant", "--json"},
	"plugin disable": {"--json"},
	"plugin remove":  {"--yes", "--purge", "--json"},
	"plugin check":   {"--strict", "--official", "--argv", "--set", "--repo", "--json"},
}

// globalFlags lists the options that come before the verb.
var globalFlags = []string{"--ascii", "--no-plugins"}

// pluginValueFlags lists the options of the plugin subverbs that take a value, so that the word
// after one is not counted as a plugin.
var pluginValueFlags = []string{"--sha256", "--grant", "--set", "--repo", "--format"}

// verbExclusive lists, for the verbs that have some, the options that cannot be used together,
// under the canonical name of the verb.
var verbExclusive = map[string][]string{
	"doctor": {"--json", "--reset"},
	"alias":  {"--format", "--json"},
	"funcs":  {"--format", "--json"},
	"repos":  {"--format", "--json"},
	"co":     {"--format", "--json"},

	"plugin list": {"--format", "--json"},
}

// listVerbs are the verbs whose options are offered only once a dash is typed, since a bare word
// after them is a name or nothing at all.
var listVerbs = []string{"alias", "funcs", "repos", "co"}

// canonical gives the canonical name of a verb, whichever of its spellings was typed.
//
// Parameters:
//   - word: what the user typed, matched exactly
//
// Returns the first name of the command that accepts the word, or an empty text when none does.
func canonical(word string) string {
	if c, ok := findCommand(word); ok {
		return c.names[0]
	}

	return ""
}

// listTyped reports whether the words typed after a list verb are only options and the list verb
// itself, so that no name has been given.
//
// Parameters:
//   - typed: the words between the verb and the word being completed
func listTyped(typed []string) bool {
	for _, w := range typed {
		if w != "list" && w != "ls" && !strings.HasPrefix(w, "-") {
			return false
		}
	}

	return true
}

// candidate formats one completion line.
//
// Parameters:
//   - name: word to offer
//   - desc: short description, may be empty
//
// Returns the name, then a tab and the description when there is one, both sanitized so that
// neither can hold a control character.
func candidate(name, desc string) string {
	if desc == "" {
		return sanitize(name)
	}

	return sanitize(name) + "\t" + sanitize(desc)
}

// verbCandidates lists the verbs that start with a word.
//
// Parameters:
//   - cur: the word being completed, possibly empty
func (a *app) verbCandidates(cur string) []string {
	var out []string

	for _, c := range commands() {
		if c.hidden {
			continue
		}

		for _, n := range c.names {
			if len(n) > 1 && strings.HasPrefix(n, cur) {
				out = append(out, candidate(n, a.t(c.desc)))
			}
		}
	}

	return out
}

// flagCandidates lists the options of a verb that start with a word.
//
// Parameters:
//   - verb: canonical name of the verb, a key of verbFlags
//   - cur: the word being completed, empty or starting with a dash
//   - typed: the words already typed after the verb, whose options are not offered again
//
// Options that cannot be combined with one already typed are left out too.
func flagCandidates(verb, cur string, typed []string) []string {
	var out []string

	for _, f := range verbFlags[verb] {
		if slices.Contains(typed, f) {
			continue
		}

		excl := verbExclusive[verb]

		if slices.Contains(excl, f) &&
			slices.ContainsFunc(typed, func(w string) bool {
				return w != f && slices.Contains(excl, w)
			}) {
			continue
		}

		if strings.HasPrefix(f, cur) {
			out = append(out, candidate(f, ""))
		}
	}

	return out
}

// connCandidates lists the names and the aliases of the usable connections that start with a
// word.
//
// Parameters:
//   - cur: the word being completed, possibly empty
//
// Returns nothing when the data file is missing or cannot be parsed. A record that fails
// validation or waits for a review is left out.
func (a *app) connCandidates(cur string) []string {
	paths, err := a.cfgPaths()
	if err != nil {
		return nil
	}

	c, err := store.OpenConnections(paths.ConnectionsFile())
	if err != nil || c.Err() != nil {
		return nil
	}

	bad := c.Invalid()
	seen := map[string]bool{}

	var out []string

	for _, rec := range c.List() {
		if bad[rec.Name] != nil || rec.NeedsReview {
			continue
		}

		desc := rec.Desc
		if desc == "" {
			desc = rec.Target
		}

		for _, n := range append([]string{rec.Name}, rec.Aliases...) {
			if !seen[n] && hasPrefixFold(n, cur) {
				seen[n] = true
				out = append(out, candidate(n, desc))
			}
		}
	}

	return out
}

// runComplete implements the hidden "tote __complete WORDS..." used by the shell completion.
//
// The words are those typed after "tote", the last one being the word under the cursor. Only
// the verbs, for the connections verb the names and aliases of the connections, and for the verbs
// that have some, their options, are ever offered, one per line with an optional description
// after a tab. It never fails: a shell completion has no place to show an error.
//
// Parameters:
//   - args: the words typed after tote
//
// Returns the exit code of the verb.
func (a *app) runComplete(args []string) int {
	words := args
	if len(words) == 0 {
		words = []string{""}
	}

	// The options before the verb change nothing to what follows it.
	for len(words) > 1 && slices.Contains(globalFlags, words[0]) {
		words = words[1:]
	}

	cur := words[len(words)-1]
	verb := canonical(words[0])

	var lines []string

	switch {
	case len(words) == 1 && strings.HasPrefix(cur, "-"):
		for _, f := range globalFlags {
			if strings.HasPrefix(f, cur) {
				lines = append(lines, candidate(f, ""))
			}
		}
	case len(words) == 1:
		lines = a.verbCandidates(cur)
	case verb == "plugin":
		lines = a.pluginComplete(words[1:])
	case len(words) == 2 && verb == "co" && (cur == "" || cur[0] != '-'):
		lines = a.connCandidates(cur)
	case len(words) >= 2 && verbFlags[verb] != nil && slices.Contains(listVerbs, verb):
		if cur != "" && cur[0] == '-' && listTyped(words[1:len(words)-1]) {
			lines = flagCandidates(verb, cur, words[1:len(words)-1])
		}
	case len(words) >= 2 && verbFlags[verb] != nil && (cur == "" || cur[0] == '-'):
		lines = flagCandidates(verb, cur, words[1:len(words)-1])
	}

	for _, l := range lines {
		fmt.Fprintln(a.Stdout, l)
	}

	return exitOK
}

// pluginComplete lists what can follow "tote plugin": the subverbs, then for each its options and
// the ids of the plugins it works on.
//
// Parameters:
//   - words: the words typed after plugin, the last one being the word under the cursor
func (a *app) pluginComplete(words []string) []string {
	cur := words[len(words)-1]

	if len(words) == 1 {
		if strings.HasPrefix(cur, "-") {
			return flagCandidates("plugin list", cur, nil)
		}

		var out []string

		for _, v := range pluginVerbs {
			if strings.HasPrefix(v, cur) {
				out = append(out, candidate(v, ""))
			}

			if v == "list" && strings.HasPrefix("ls", cur) {
				out = append(out, candidate("ls", ""))
			}
		}

		return out
	}

	verb := words[0]
	if verb == "ls" {
		verb = "list"
	}

	sub := "plugin " + verb
	typed := words[1 : len(words)-1]

	if verbFlags[sub] == nil {
		return nil
	}

	if strings.HasPrefix(cur, "-") {
		return flagCandidates(sub, cur, typed)
	}

	if len(typed) > 0 && typed[len(typed)-1] == "--format" {
		return formatCandidates(cur)
	}

	if len(typed) > 0 && slices.Contains(pluginValueFlags, typed[len(typed)-1]) {
		return nil
	}

	if pluginPositionals(typed) > 0 {
		return nil
	}

	return a.pluginIDs(verb, cur)
}

// formatCandidates lists the output formats of the list verbs that start with a word.
//
// Parameters:
//   - cur: the word being completed, possibly empty
func formatCandidates(cur string) []string {
	var out []string

	for _, f := range []string{formatTable, formatTSV, formatJSON} {
		if strings.HasPrefix(f, cur) {
			out = append(out, candidate(f, ""))
		}
	}

	return out
}

// pluginPositionals counts the words that are neither options nor the values of options.
//
// Parameters:
//   - typed: the words typed after the subverb, without the one under the cursor
func pluginPositionals(typed []string) int {
	n := 0

	for i, w := range typed {
		switch {
		case strings.HasPrefix(w, "-"):
		case i > 0 && slices.Contains(pluginValueFlags, typed[i-1]):
		default:
			n++
		}
	}

	return n
}

// pluginIDs lists the plugin ids that a subverb works on and that start with a word: the
// installed plugins, plus the ones shipped with tote for install and check.
//
// Parameters:
//   - sub: the subverb
//   - cur: the word being completed, possibly empty
func (a *app) pluginIDs(sub, cur string) []string {
	reg, err := a.pluginRegistry()
	if err != nil {
		return nil
	}

	snap := reg.Load()

	var ids []string

	if sub != "install" {
		for _, p := range snap.Plugins() {
			ids = append(ids, p.ID)
		}
	}

	if sub == "install" || sub == "check" || sub == "info" {
		for _, av := range reg.Available(snap, a.tr().Lang()) {
			ids = append(ids, av.ID)
		}
	}

	switch sub {
	case "install", "info", "update", "enable", "disable", "remove", "check":
	default:
		return nil
	}

	var out []string

	for _, id := range ids {
		if strings.HasPrefix(id, cur) {
			out = append(out, candidate(id, ""))
		}
	}

	return out
}
