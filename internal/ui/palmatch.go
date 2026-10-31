package ui

import (
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// scope tells which entries the palette searches.
type scope int

const (
	// scopeAll searches records of every kind and the actions.
	scopeAll scope = iota

	// scopeConn searches connections only, chosen with the prefix "c ".
	scopeConn

	// scopeRepo searches repositories only, chosen with "r ".
	scopeRepo

	// scopeAlias searches aliases only, chosen with "a ".
	scopeAlias

	// scopeFunc searches functions only, chosen with "d ".
	scopeFunc

	// scopeAction searches the actions only, chosen with ">".
	scopeAction
)

// scopeOfKind maps a kind to the scope that holds its records.
var scopeOfKind = [...]scope{scopeConn, scopeRepo, scopeAlias, scopeFunc}

// scopeKeys maps the letter of a prefix to its scope.
var scopeKeys = map[byte]scope{'c': scopeConn, 'r': scopeRepo, 'a': scopeAlias, 'd': scopeFunc}

// entry is one line the palette can show: a record of a store, or an action.
//
// Scope is the category it belongs to. For a record, Kind and Key identify it, Item is the
// prepared record (used for the status line) and Warn tells why it cannot be used. Cat is the
// sanitized name of the category that a record is drawn under ("Connections"), empty for an
// action, Name the sanitized name or label, and Sub the sanitized target, path or command, which
// is drawn muted at the right of the row when the width allows. Nav is true for an action that
// only goes somewhere, which is drawn muted. Names holds the lowercase names that a query is
// matched against, the first being Name itself, and Text the lowercase text of the whole entry,
// category included. Low is the lowercase category. For an action, Msg is the message that runs
// it.
type entry struct {
	Scope scope
	Kind  kind
	Key   string
	Cat   string
	Low   string
	Name  string
	Sub   string
	Names []string
	Text  string
	Warn  string
	Nav   bool
	Item  *item
	Msg   tea.Msg
}

// act describes an action of the palette: the catalog key of its label, what it does, the
// catalog key of other words that find it, empty for none, and whether it only goes somewhere.
type act struct {
	label string
	msg   tea.Msg
	words string
	nav   bool
}

// actions lists the actions in the order they appear when nothing is typed.
var actions = []act{
	{"ui_pal_act_add_connection", addMsg{Kind: kindConn}, "", false},
	{"ui_pal_act_add_repo", addMsg{Kind: kindRepo}, "", false},
	{"ui_pal_act_add_alias", addMsg{Kind: kindAlias}, "", false},
	{"ui_pal_act_add_func", addMsg{Kind: kindFunc}, "", false},
	{"ui_pal_act_open_connections", openKindMsg{Kind: kindConn}, "", true},
	{"ui_pal_act_open_repos", openKindMsg{Kind: kindRepo}, "", true},
	{"ui_pal_act_open_aliases", openKindMsg{Kind: kindAlias}, "", true},
	{"ui_pal_act_open_funcs", openKindMsg{Kind: kindFunc}, "", true},
	{"ui_pal_act_home", goHomeMsg{}, "", true},
	{"ui_pal_act_settings", openConfigMsg{}, "ui_pal_kw_settings", false},
	{"ui_pal_act_help", openHelpMsg{}, "", false},
	{"ui_pal_act_check_update", checkMsg{}, "", false},
	{"ui_pal_act_reload", reloadMsg{}, "", false},
	{"ui_pal_act_quit", finishMsg{}, "", false},
}

// pluginAction is the action that opens the plugins screen. It is in the palette only when the
// plugins are on, right after the actions that open the four kinds.
var pluginAction = act{"ui_pal_act_open_plugins", openPluginsMsg{}, "", true}

// buildEntries lists everything the palette can search.
//
// Parameters:
//   - e: shared environment, which gives the records and the labels of the actions
//
// Returns the records of the four kinds in file order, then the actions. Every text is
// sanitized, and the text a query is matched against holds the category of a record, so that
// "conn" finds every connection.
func buildEntries(e *env) []entry {
	out := make([]entry, 0, 64+len(actions)+1)

	for _, k := range allKinds {
		items := e.Data.items(k)
		cat := sanitize(e.t(k.titleKey()))
		low := strings.ToLower(cat)

		for i := range items {
			it := &items[i]
			out = append(out, entry{
				Scope: scopeOfKind[k], Kind: k, Key: it.Key, Cat: cat, Low: low,
				Name: it.Cells[0], Sub: subOf(it), Names: it.Names, Text: it.Text + " " + low,
				Warn: it.Warn, Item: it,
			})
		}
	}

	list := actions
	if e.pluginsOn() {
		at := slices.IndexFunc(actions, func(a act) bool {
			return a.label == "ui_pal_act_open_funcs"
		}) + 1

		list = slices.Insert(slices.Clone(actions), at, pluginAction)
	}

	for _, a := range list {
		label := sanitize(e.t(a.label))
		low := strings.ToLower(label)
		names, text := []string{low}, low

		if a.words != "" {
			words := strings.ToLower(sanitize(e.t(a.words)))
			names = append(names, strings.Fields(words)...)
			text += " " + words
		}

		out = append(out, entry{
			Scope: scopeAction, Name: label, Names: names, Text: text, Nav: a.nav, Msg: a.msg,
		})
	}

	for _, o := range e.Plug.commands() {
		out = append(out, pluginEntry(e, o))
	}

	return out
}

// pluginEntry makes the palette entry of a command that an enabled plugin contributes. It is an
// action: its Msg starts the command, and the plugin id is the muted detail at the right. The
// label, the description and the keywords of the plugin, in the language of the interface, are
// what a query finds it by.
//
// Parameters:
//   - e: shared environment, which folds the words of a plugin in the ASCII set
//   - o: the command
func pluginEntry(e *env, o plugin.Offer) entry {
	label := sanitize(e.fold(o.Label))
	low := strings.ToLower(label)
	names, text := []string{low}, low+" "+strings.ToLower(sanitize(o.Plugin))

	for _, k := range o.Keywords {
		k = strings.ToLower(sanitize(e.fold(k)))
		names = append(names, strings.Fields(k)...)
		text += " " + k
	}

	if o.Description != "" {
		text += " " + strings.ToLower(sanitize(e.fold(o.Description)))
	}

	return entry{
		Scope: scopeAction, Name: label, Sub: sanitize(o.Plugin), Names: names, Text: text,
		Msg: pluginStartMsg{Start: pluginStart{
			Plugin: o.Plugin, Table: o.Table, Entry: o.ID,
			Label: label,
		}},
	}
}

// subOf returns the text of the second column of a record: its target, path or command line,
// or for a function its description, else its kind.
//
// Parameters:
//   - it: prepared record
func subOf(it *item) string {
	if it.Kind == kindFunc {
		if len(it.Cells) > 2 && it.Cells[2] != "" {
			return it.Cells[2]
		}
	}

	if len(it.Cells) > 1 {
		return it.Cells[1]
	}

	return ""
}

// parseScope splits a palette query into its scope and the text to search.
//
// Parameters:
//   - q: text as typed
//
// Returns the scope chosen by a prefix and the rest of the query. The prefix is ">" for
// actions, or one of "c", "r", "a" and "d", in either case, followed by a space; a bare letter
// is text to search. Spaces after the prefix are dropped.
func parseScope(q string) (scope, string) {
	sc, _, rest := splitScope(q)

	return sc, rest
}

// scopePrefix returns the prefix of a query that chose a scope, in its normal form.
//
// Parameters:
//   - q: text as typed
//
// Returns ">" for the actions, a letter and one space for a kind, and an empty string for a query
// that has no prefix.
func scopePrefix(q string) string {
	_, prefix, _ := splitScope(q)

	return prefix
}

// splitScope cuts a query into the scope its prefix chose, the prefix and the rest.
//
// Parameters:
//   - q: text as typed
//
// Returns the scope, the prefix in its normal form (">" or the letter as typed and one space) and
// the text after the prefix and the spaces that follow it. A query without a prefix gives
// scopeAll, an empty prefix and the query itself.
func splitScope(q string) (scope, string, string) {
	if strings.HasPrefix(q, ">") {
		return scopeAction, ">", strings.TrimLeft(q[1:], " ")
	}

	if len(q) >= 2 && q[1] == ' ' {
		if sc, ok := scopeKeys[q[0]|0x20]; ok {
			return sc, q[:1] + " ", strings.TrimLeft(q[2:], " ")
		}
	}

	return scopeAll, "", q
}

const (
	// tierExact is a name equal to the query.
	tierExact = iota

	// tierPrefix is a name that starts with the query.
	tierPrefix

	// tierCategory is a record whose category starts with the query, such as "conn" for every
	// connection. These keep the order of the file.
	tierCategory

	// tierWord is a name that has the query at the start of one of its words.
	tierWord

	// tierSubstring is a name that has the query inside a word.
	tierSubstring

	// tierText is a match in the other text of the record, such as the target or the
	// description.
	tierText

	// tierSubsequence is a name that holds the letters of the query in order.
	tierSubsequence
)

// rank selects and orders the entries that match a query.
//
// Parameters:
//   - entries: everything the palette can show
//   - sc: scope to search
//   - query: text to search, without the scope prefix; it is lowercased and sanitized here
//
// Returns the indexes of the matches, best first. The first word of the query decides the rank:
// an exact name, then a name that starts with it (aliases count as names), then a record whose
// category starts with it, in the order of the file, then a name that has it at the start of a
// word, then a name that contains it, then any other text of the entry, then a name that holds
// its letters in order. Every further word must be found somewhere in the text of the entry.
// Within a rank the shorter name comes first, then the original order.
// An empty query returns the entries of the scope in their original order.
func rank(entries []entry, sc scope, query string) []int {
	words := strings.Fields(strings.ToLower(sanitize(query)))
	keys := make([]uint64, 0, len(entries))

	for i := range entries {
		en := &entries[i]
		if sc != scopeAll && en.Scope != sc {
			continue
		}

		tier := 0

		if len(words) > 0 {
			if tier = tierOf(en, words); tier < 0 {
				continue
			}
		}

		size := uint64(min(len(en.Names[0]), 0xFFFFFF))
		if tier == tierCategory {
			size = 0
		}

		keys = append(keys, uint64(tier)<<56|size<<32|uint64(i))
	}

	if len(words) > 0 {
		slices.Sort(keys)
	}

	out := make([]int, len(keys))
	for i, k := range keys {
		out[i] = int(uint32(k))
	}

	return out
}

// tierOf ranks one entry against the words of a query.
//
// Parameters:
//   - en: entry to test
//   - words: lowercase words of the query, at least one
//
// Returns the tier of the first word, or -1 when the entry does not match: the first word is
// nowhere in it, or a later word is not in its text.
func tierOf(en *entry, words []string) int {
	for _, w := range words[1:] {
		if !strings.Contains(en.Text, w) {
			return -1
		}
	}

	q := words[0]
	best := -1

	for _, n := range en.Names {
		t := nameTier(n, q)
		if t >= 0 && (best < 0 || t < best) {
			best = t
		}
	}

	switch {
	case best >= 0 && best <= tierPrefix:
		return best
	case en.Low != "" && strings.HasPrefix(en.Low, q):
		return tierCategory
	case best >= 0:
		return best
	case strings.Contains(en.Text, q):
		return tierText
	case utf8.RuneCountInString(q) >= 2 && subsequence(en.Names[0], q):
		return tierSubsequence
	}

	return -1
}

// nameTier ranks a query against one lowercase name.
//
// Parameters:
//   - name: lowercase name
//   - q: lowercase query
//
// Returns tierExact, tierPrefix, tierWord or tierSubstring, or -1 when name does not contain q.
func nameTier(name, q string) int {
	if name == q {
		return tierExact
	}

	if strings.HasPrefix(name, q) {
		return tierPrefix
	}

	tier := -1

	for from := 0; from < len(name); {
		i := strings.Index(name[from:], q)
		if i < 0 {
			break
		}

		i += from

		if i > 0 && isWordStart(name, i) {
			return tierWord
		}

		tier = tierSubstring
		from = i + 1
	}

	return tier
}

// isWordStart reports whether a position in a name follows a separator.
//
// Parameters:
//   - name: text to look at
//   - i: byte index, at least 1
func isWordStart(name string, i int) bool {
	return strings.IndexByte("-_./ :@", name[i-1]) >= 0
}

// subsequence reports whether the runes of q appear in s in the same order, not necessarily
// next to each other.
//
// Parameters:
//   - s: text to search
//   - q: runes to find
func subsequence(s, q string) bool {
	for _, r := range s {
		if q == "" {
			break
		}

		if next, size := utf8.DecodeRuneInString(q); next == r {
			q = q[size:]
		}
	}

	return q == ""
}
