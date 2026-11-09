package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
	"github.com/totecli/tote/internal/ui"
)

// connView is the JSON form of a connection.
type connView struct {
	Name        string   `json:"name"`
	Via         string   `json:"via"`
	Target      string   `json:"target"`
	User        string   `json:"user"`
	Aliases     []string `json:"aliases"`
	IP          string   `json:"ip"`
	Desc        string   `json:"desc"`
	NeedsReview bool     `json:"needs_review"`
}

// connSpec describes the connections for the generic verbs.
func connSpec() spec[store.Connection] {
	return spec[store.Connection]{
		command: "co",
		label:   "kind_connection",
		noun:    "connections",
		heads: []string{
			"header_name", "header_via", "header_target", "header_user", "header_aliases",
			"header_ip", "header_description",
		},
		file:  config.Paths.ConnectionsFile,
		open:  store.OpenConnections,
		cells: connCells,
		view:  newConnView,
		addOpts: []opt{
			{long: "target", value: true},
			{long: "via", value: true},
			{long: "user", value: true},
			{long: "alias", value: true},
			{long: "ip", value: true},
			descOpt,
		},
		build: buildConn,
		check: connClash,
	}
}

// connCells turns a connection into the cells of a row.
//
// Parameters:
//   - c: connection to show
func connCells(c store.Connection) []string {
	return []string{c.Name, c.Via, c.Target, c.User, strings.Join(c.Aliases, ","), c.IP, c.Desc}
}

// newConnView turns a connection into its JSON form.
//
// Parameters:
//   - c: connection to encode
//
// Returns a connView whose aliases are an empty list and not null when there are none.
func newConnView(c store.Connection) any {
	aliases := c.Aliases
	if aliases == nil {
		aliases = []string{}
	}

	return connView{c.Name, c.Via, c.Target, c.User, aliases, c.IP, c.Desc, c.NeedsReview}
}

// splitList splits a comma separated option into trimmed, non-empty items.
//
// Parameters:
//   - raw: option value, possibly empty
//
// Returns nil for an empty value.
func splitList(raw string) []string {
	var out []string

	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}

	return out
}

// buildConn makes a connection out of "add NAME --target T ..." arguments.
//
// Parameters:
//   - o: parsed options and positional arguments
//
// Returns the connection, or an *optError.
func buildConn(o options) (store.Connection, error) {
	if err := wantArgs(o.pos, 1, 1, "arg_name"); err != nil {
		return store.Connection{}, err
	}

	return store.Connection{
		Name:    o.pos[0],
		Via:     o.vals["via"],
		Target:  o.vals["target"],
		User:    o.vals["user"],
		Aliases: splitList(o.vals["alias"]),
		IP:      o.vals["ip"],
		Desc:    o.vals["desc"],
	}, nil
}

// resolveConn finds the connections a handle designates.
//
// An exact name wins, then an exact alias, and each of the two is tried with the case as typed
// before it is tried ignoring case. After that a prefix of a name or of an alias counts, when
// it is long enough and ignores case.
//
// Parameters:
//   - conns: connections in file order
//   - handle: what the user typed
//   - prefixMin: fewest characters for a prefix to count
//
// Returns one connection when the handle is unambiguous, several when an alias is shared or a
// prefix matches more than one and none when nothing matches.
func resolveConn(conns []store.Connection, handle string, prefixMin int) []store.Connection {
	same := func(a, b string) bool { return a == b }

	for _, eq := range []func(a, b string) bool{same, strings.EqualFold} {
		if out := matchName(conns, handle, eq); len(out) > 0 {
			return out
		}

		if out := matchAlias(conns, handle, eq); len(out) > 0 {
			return out
		}
	}

	if utf8.RuneCountInString(handle) < prefixMin {
		return nil
	}

	var out []store.Connection

	for _, c := range conns {
		if hasPrefixFold(c.Name, handle) || anyPrefixFold(c.Aliases, handle) {
			out = append(out, c)
		}
	}

	return out
}

// matchName lists the connections whose name equals a handle.
//
// Parameters:
//   - conns: connections in file order
//   - handle: what the user typed
//   - eq: comparison of a name with the handle
func matchName(
	conns []store.Connection, handle string, eq func(a, b string) bool,
) []store.Connection {
	var out []store.Connection

	for _, c := range conns {
		if eq(c.Name, handle) {
			out = append(out, c)
		}
	}

	return out
}

// matchAlias lists the connections that have an alias equal to a handle.
//
// Parameters:
//   - conns: connections in file order
//   - handle: what the user typed
//   - eq: comparison of an alias with the handle
func matchAlias(
	conns []store.Connection, handle string, eq func(a, b string) bool,
) []store.Connection {
	var out []store.Connection

	for _, c := range conns {
		if slices.ContainsFunc(c.Aliases, func(alias string) bool { return eq(alias, handle) }) {
			out = append(out, c)
		}
	}

	return out
}

// hasPrefixFold reports whether a string starts with a prefix, ignoring case.
//
// Parameters:
//   - s: string to test
//   - prefix: expected start
func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// anyPrefixFold reports whether one of the strings starts with a prefix, ignoring case.
//
// Parameters:
//   - list: strings to test
//   - prefix: expected start
func anyPrefixFold(list []string, prefix string) bool {
	for _, s := range list {
		if hasPrefixFold(s, prefix) {
			return true
		}
	}

	return false
}

// runConn implements "tote co".
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the exit code of the verb.
func (a *app) runConn(args []string) int {
	s := connSpec()

	if len(args) > 0 {
		switch args[0] {
		case "list", "ls", "add", "rm", "remove", "delete":
			return s.dispatch(a, args)
		case "--":
			return a.connectArgs(args[1:])
		}
	}

	o, err := parseOptions(args, []opt{
		{long: "list", short: 'l'},
		{long: "format", value: true},
		{long: "json"},
	})
	if err != nil {
		return a.usageErr(err, s.command)
	}

	if len(o.pos) == 0 && !o.help && !o.set["list"] && !o.set["format"] &&
		!o.set["json"] && a.interactive() {
		return a.runUI(ui.StartConnections)
	}

	if o.set["list"] || len(o.pos) == 0 || o.help {
		return s.list(a, args)
	}

	return a.connectArgs(o.pos)
}

// connectArgs opens the connection named by "HANDLE [USER]".
//
// Parameters:
//   - pos: handle then optional user
//
// Returns the exit code of the verb.
func (a *app) connectArgs(pos []string) int {
	if err := wantArgs(pos, 1, 2, "arg_name"); err != nil {
		return a.usageErr(err, "co")
	}

	user := ""
	if len(pos) == 2 {
		user = pos[1]
	}

	return a.connect(pos[0], user)
}

// connect resolves a handle, prints the command and replaces the process by it.
//
// Parameters:
//   - handle: name, alias or prefix typed by the user
//   - user: login for this run, empty to use the stored one
//
// Returns a code only when the connection is not opened: exitFail for an unknown or refused
// connection, exitUsage for an ambiguous handle and exitExec when the program does not start.
func (a *app) connect(handle, user string) int {
	s := connSpec()

	c, rc := s.load(a)
	if c == nil {
		return rc
	}

	prefixMin := a.cfgWarn().Connect.PrefixMin
	matches := resolveConn(c.List(), handle, prefixMin)

	switch len(matches) {
	case 0:
		return a.noConnection(handle, c.List())
	case 1:
		return a.launch(matches[0], user)
	}

	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = sanitize(m.Name)
	}

	a.say("cli_ambiguous", handle, strings.Join(names, ", "))

	return exitUsage
}

// noConnection reports a handle that matches nothing.
//
// Parameters:
//   - handle: what the user typed
//   - conns: connections that exist
//
// Returns exitFail.
func (a *app) noConnection(handle string, conns []store.Connection) int {
	var names []string

	for _, c := range conns {
		names = append(names, c.Name)
		names = append(names, c.Aliases...)
	}

	a.failMsg(exitFail, "cli_no_connection", handle)
	a.didYouMean(handle, names)

	return exitFail
}

// launch starts the program that connects to a host.
//
// Parameters:
//   - c: connection to open
//   - user: login for this run, empty to use the stored one
//
// Returns the exit code of the verb.
func (a *app) launch(c store.Connection, user string) int {
	argv, err := shell.ConnectArgv(c, user)
	if errors.Is(err, shell.ErrNeedsReview) {
		return a.failMsg(exitFail, "cli_needs_review", sanitize(c.Name))
	}

	if err != nil {
		return a.fail(err)
	}

	argv = shell.Keepalive(a.cfgWarn().Connect.Keepalive, a.Probe, a.Environ(), argv)

	fmt.Fprintln(a.Stderr, "  -> "+sanitize(shell.PrettyCommand(argv)))

	// The wrapper only cleans up when the function finishes, which a Ctrl-C on the connection
	// prevents, so the file nobody will write to is removed before the process is replaced.
	_ = shell.RemoveHandoff(a.Getenv("TOTE_HANDOFF"))

	if err := a.Exec(argv, a.Environ()); err != nil {
		if shell.ViaProgram(c) != "" && shell.IsMissingProgram(err) {
			return a.failMsg(exitExec, "cli_exec_novia", argv[0], err, a.t("cli_via_hint"))
		}

		return a.failMsg(exitExec, "cli_exec_failed", argv[0], err)
	}

	return exitOK
}
