package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/store"
)

const (
	// formatTable is the aligned table with a header.
	formatTable = "table"

	// formatTSV is the tab separated output without a header, made for scripts.
	formatTSV = "tsv"

	// formatJSON is a JSON object that holds the schema version and the array of records.
	formatJSON = "json"

	// listSchema is the version of the JSON object that the list verbs print.
	listSchema = 1
)

// listDoc is the JSON form of a list: the schema version, then the records under the plural noun
// of the kind. Its encoding keeps that order and leaves the characters of HTML alone.
type listDoc struct {
	noun    string
	records []any
}

// MarshalJSON implements json.Marshaler.
//
// Returns {"schema":1,"<noun>":[...]} with the records in the order they were given.
func (d listDoc) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer

	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)

	key, err := json.Marshal(d.noun)
	if err != nil {
		return nil, err
	}

	b.WriteString(`{"schema":` + strconv.Itoa(listSchema) + `,`)
	b.Write(key)
	b.WriteByte(':')

	if err = enc.Encode(d.records); err != nil {
		return nil, err
	}

	return append(bytes.TrimRight(b.Bytes(), "\n"), '}'), nil
}

// spec describes one kind of record for the list, add and rm verbs, whatever its type T.
//
// command is the verb used in hints, label the catalog key of the kind word, noun the plural noun
// that names the array of records in the JSON form of a list and heads the catalog keys of the
// table columns. file picks the data file out of the paths and open loads
// it. cells turns a record into the strings of one row, view into the value that JSON encodes,
// addOpts lists the options of the add verb, build makes a record out of the parsed arguments
// and check refuses a record whose names clash with an existing one. prepare, when it is not nil,
// rewrites the parsed arguments of add before build reads them. cmdWords is the number of
// leading positional arguments of add after which every word, dashes included, belongs to the
// last argument, or zero for none.
type spec[T store.Record] struct {
	command  string
	label    string
	noun     string
	heads    []string
	file     func(config.Paths) string
	open     func(string) (*store.Collection[T], error)
	cells    func(T) []string
	view     func(T) any
	addOpts  []opt
	build    func(options) (T, error)
	check    func(*app, *store.Collection[T], T) *optError
	prepare  func(*app, options) options
	cmdWords int
}

// load opens the collection of a kind and checks that it can be used.
//
// Parameters:
//   - a: running app
//
// Returns the collection, or nil and the exit code to return when the paths, the file or its
// syntax are wrong. A config.toml that cannot be used is reported once on the way.
func (s spec[T]) load(a *app) (*store.Collection[T], int) {
	a.cfgWarn()

	paths, err := a.cfgPaths()
	if err != nil {
		return nil, a.fail(err)
	}

	c, err := s.open(s.file(paths))
	if err != nil {
		return nil, a.fail(err)
	}

	if err := c.Err(); err != nil {
		return nil, a.readOnly(err)
	}

	return c, exitOK
}

// dispatch routes the arguments that follow the verb of a kind to list, add or rm.
//
// Parameters:
//   - a: running app
//   - args: arguments after the verb
//
// Returns the exit code of the verb.
func (s spec[T]) dispatch(a *app, args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return s.list(a, args)
	}

	switch args[0] {
	case "list", "ls":
		return s.list(a, args[1:])
	case "add":
		return s.add(a, args[1:])
	case "rm", "remove", "delete":
		return s.remove(a, args[1:])
	}

	a.failMsg(exitUsage, "cli_unknown_subcommand", args[0])
	a.didYouMean(args[0], []string{"list", "add", "rm"})
	a.say("cli_try_help", "tote help "+s.command)

	return exitUsage
}

// format picks the output format of a list.
//
// Parameters:
//   - a: running app
//   - o: parsed options, whose format option may be set; --json is the same as --format json,
//     and an error when it comes with another format
//
// Returns the format, or an error for an unknown one. Without the option a terminal gets a table
// and anything else tab separated values.
func (s spec[T]) format(a *app, o options) (string, error) {
	f := o.vals["format"]

	if o.set["json"] {
		if f != "" && f != formatJSON {
			return "", &optError{Key: "cli_format_conflict", Args: []any{sanitize(f)}}
		}

		return formatJSON, nil
	}

	switch f {
	case formatTable, formatTSV, formatJSON:
		return f, nil
	case "":
		if a.IsTerminal(a.Stdout) {
			return formatTable, nil
		}

		return formatTSV, nil
	default:
		return "", &optError{Key: "cli_invalid_value", Args: []any{"format", f}}
	}
}

// list implements the list verb of a kind.
//
// Parameters:
//   - a: running app
//   - args: arguments after list
//
// Returns the exit code of the verb.
func (s spec[T]) list(a *app, args []string) int {
	o, err := parseOptions(args, []opt{
		{long: "format", value: true},
		{long: "json"},
		{long: "list", short: 'l'},
	})
	if err == nil {
		err = wantArgs(o.pos, 0, 0)
	}

	if err != nil {
		return a.usageErr(err, s.command)
	}

	if o.help {
		return a.verbHelp(s.command)
	}

	format, err := s.format(a, o)
	if err != nil {
		return a.usageErr(err, s.command)
	}

	c, rc := s.load(a)
	if c == nil {
		return rc
	}

	return s.render(a, c.List(), format)
}

// render prints records in a format.
//
// Parameters:
//   - a: running app
//   - recs: records to print
//   - format: one of the format constants
//
// Returns the exit code of the verb.
func (s spec[T]) render(a *app, recs []T, format string) int {
	if format == formatJSON {
		views := make([]any, len(recs))
		for i, r := range recs {
			views[i] = s.view(r)
		}

		if err := writeJSON(a.Stdout, listDoc{noun: s.noun, records: views}); err != nil {
			return a.fail(err)
		}

		return exitOK
	}

	rows := make([][]string, len(recs))
	for i, r := range recs {
		rows[i] = s.cells(r)
	}

	if format == formatTSV {
		writeTSV(a.Stdout, rows)
		return exitOK
	}

	heads := make([]string, len(s.heads))
	for i, h := range s.heads {
		heads[i] = a.t(h)
	}

	writeTable(a.Stdout, heads, rows)

	return exitOK
}

// add implements the add verb of a kind.
//
// Parameters:
//   - a: running app
//   - args: arguments after add
//
// Returns the exit code of the verb.
func (s spec[T]) add(a *app, args []string) int {
	o, err := parseOptionsWords(args, s.addOpts, s.cmdWords)
	if err != nil {
		return a.usageErr(err, s.command)
	}

	if o.help {
		return a.verbHelp(s.command)
	}

	if s.prepare != nil {
		o = s.prepare(a, o)
	}

	rec, err := s.build(o)
	if err != nil {
		return a.usageErr(err, s.command)
	}

	c, rc := s.load(a)
	if c == nil {
		return rc
	}

	if s.check != nil && rec.Validate() == nil {
		if oerr := s.check(a, c, rec); oerr != nil {
			return a.failMsg(exitFail, oerr.Key, a.optArgs(oerr)...)
		}
	}

	if err := c.Add(rec); err != nil {
		return s.writeErr(a, c, rec.Key(), err)
	}

	fmt.Fprintln(a.Stdout, a.t("cli_added", a.t(s.label), rec.Key()))

	return exitOK
}

// remove implements the rm verb of a kind.
//
// Parameters:
//   - a: running app
//   - args: arguments after rm
//
// Returns the exit code of the verb.
func (s spec[T]) remove(a *app, args []string) int {
	o, err := parseOptions(args, []opt{{long: "yes", short: 'y'}})
	if err != nil {
		return a.usageErr(err, s.command)
	}

	if o.help {
		return a.verbHelp(s.command)
	}

	if err := wantArgs(o.pos, 1, 1, "arg_name"); err != nil {
		return a.usageErr(err, s.command)
	}

	c, rc := s.load(a)
	if c == nil {
		return rc
	}

	name := o.pos[0]
	if _, ok := c.Get(name); !ok {
		return a.notFound(s.label, name, s.names(c))
	}

	if !o.set["yes"] {
		if ok, rc := a.confirm(s.label, name); !ok {
			return rc
		}
	}

	if err := c.Delete(name); err != nil {
		return s.writeErr(a, c, name, err)
	}

	fmt.Fprintln(a.Stdout, a.t("cli_removed", a.t(s.label), name))

	return exitOK
}

// names lists the keys of a collection in file order.
//
// Parameters:
//   - c: loaded collection
func (s spec[T]) names(c *store.Collection[T]) []string {
	recs := c.List()
	out := make([]string, len(recs))

	for i, r := range recs {
		out[i] = r.Key()
	}

	return out
}

// writeErr reports the failure of a write on a collection.
//
// Parameters:
//   - a: running app
//   - c: collection that refused the write
//   - key: key of the record concerned
//   - err: error returned by the store
//
// Returns exitFail.
func (s spec[T]) writeErr(a *app, c *store.Collection[T], key string, err error) int {
	switch {
	case errors.Is(err, store.ErrExists):
		return a.failMsg(exitFail, "cli_exists", a.t(s.label), sanitize(key))
	case errors.Is(err, store.ErrNotFound):
		return a.failMsg(exitFail, "cli_not_found", a.t(s.label), sanitize(key))
	case errors.Is(err, store.ErrConflict):
		return a.failMsg(exitFail, "cli_conflict", filepath.Base(c.Path()))
	case errors.Is(err, store.ErrReadOnly):
		return a.readOnly(c.Err())
	}

	return a.fail(err)
}
