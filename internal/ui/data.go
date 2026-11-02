package ui

import (
	"cmp"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// lockWait is how long a write of the interface waits for the lock of a file held by another
// process. The write runs inside the event loop, so a longer wait would freeze the screen and
// ignore Ctrl-C; the user is told to try again instead.
const lockWait = 150 * time.Millisecond

// tooBigFile matches the name of the data file in the error of a write that passes the size cap.
var tooBigFile = regexp.MustCompile(`(?s)file too large: (.*?) would be [0-9]+ bytes`)

// item is one record prepared for display.
//
// Key is the raw key of the record, used for every store operation. Cells hold the sanitized
// text of the columns of the kind, Names the lowercase names that a prefix filter matches (the
// name and the aliases) and Text the lowercase text that a substring filter searches. Warn is
// the reason the record cannot be used, empty when it can. Rec is the typed record.
type item struct {
	Kind  kind
	Key   string
	Cells []string
	Names []string
	Text  string
	Warn  string
	Rec   any
}

// userError is an error whose message is already translated and safe to show.
//
// Field is the identifier of the form field the message is about, empty when it is about the
// record as a whole.
type userError struct {
	msg   string
	err   error
	field string
}

// Error implements error.
func (e *userError) Error() string { return e.msg }

// Unwrap returns the underlying store error.
func (e *userError) Unwrap() error { return e.err }

// dataset reads and writes the four collections for the interface, the way cmd/tote does.
//
// A collection whose file cannot be parsed stays loaded but read-only; a collection that cannot
// even be opened is nil. In both cases err gives the reason, the list is empty and every write
// fails with a message instead of a panic. All text that reaches the screen goes through
// sanitize first.
type dataset struct {
	tr    *i18n.Printer
	paths config.Paths
	home  string
	conns *store.Collection[store.Connection]
	repos *store.Collection[store.Repo]
	alias *store.Collection[store.Alias]
	funcs *store.Collection[store.Func]
	errs  [4]error
}

// newDataset opens the four collections.
//
// Parameters:
//   - tr: printer for the messages of the errors
//   - paths: directories of tote
//   - home: home directory, used to expand a leading "~" in the path of a repository
func newDataset(tr *i18n.Printer, paths config.Paths, home string) *dataset {
	d := &dataset{tr: tr, paths: paths, home: home}
	d.reload()

	return d
}

// reload reads the four files again.
func (d *dataset) reload() {
	for _, k := range allKinds {
		d.reloadKind(k)
	}
}

// openColl opens a collection and reports why it cannot be written.
//
// Parameters:
//   - open: opener of the kind
//   - path: file of the collection
//
// Returns the collection (nil when it could not be opened) and the reason it is not usable.
func openColl[T store.Record](
	open func(string) (*store.Collection[T], error), path string,
) (*store.Collection[T], error) {
	c, err := open(path)
	if err != nil {
		return nil, err
	}

	c.SetLockWait(lockWait)

	return c, c.Err()
}

// reloadKind reads one file again.
//
// Parameters:
//   - k: kind to reload
func (d *dataset) reloadKind(k kind) {
	var err error

	switch k {
	case kindConn:
		d.conns, err = openColl(store.OpenConnections, d.paths.ConnectionsFile())
	case kindRepo:
		d.repos, err = openColl(store.OpenRepos, d.paths.ReposFile())
	case kindAlias:
		d.alias, err = openColl(store.OpenAliases, d.paths.AliasesFile())
	case kindFunc:
		d.funcs, err = openColl(store.OpenFuncs, d.paths.FuncsFile())
	}

	d.errs[k] = err
}

// err returns why a kind cannot be written, nil when it can.
//
// Parameters:
//   - k: kind to test
func (d *dataset) err(k kind) error {
	return d.errs[k]
}

// readOnlyText describes in one sanitized line why a kind is read-only.
//
// Parameters:
//   - k: kind to describe
//
// Returns an empty string when the kind is writable.
func (d *dataset) readOnlyText(k kind) string {
	err := d.errs[k]
	if err == nil {
		return ""
	}

	var perr *store.ParseError
	if errors.As(err, &perr) {
		return d.tr.T("ui_readonly_syntax", sanitize(filepath.Base(perr.File)), perr.Line,
			perr.Column, sanitize(perr.Msg))
	}

	return d.tr.T("ui_readonly_io", sanitize(err.Error()))
}

// listOf returns the records of a possibly missing collection.
//
// Parameters:
//   - c: collection, nil when it could not be opened
func listOf[T store.Record](c *store.Collection[T]) []T {
	if c == nil {
		return nil
	}

	return c.List()
}

// count returns the number of records of a kind.
//
// Parameters:
//   - k: kind to count
func (d *dataset) count(k kind) int {
	switch k {
	case kindConn:
		return len(listOf(d.conns))
	case kindRepo:
		return len(listOf(d.repos))
	case kindAlias:
		return len(listOf(d.alias))
	}

	return len(listOf(d.funcs))
}

// items returns the records of a kind prepared for display, in file order.
//
// Parameters:
//   - k: kind to list
func (d *dataset) items(k kind) []item {
	var out []item

	switch k {
	case kindConn:
		bad := map[string]error{}
		if d.conns != nil {
			bad = d.conns.Invalid()
		}

		for _, c := range listOf(d.conns) {
			out = append(out, d.connItem(c, bad[c.Name]))
		}
	case kindRepo:
		for _, r := range listOf(d.repos) {
			out = append(out, d.simpleItem(k, r.Name, r, r.Name, r.Path, r.Desc))
		}
	case kindAlias:
		for _, a := range listOf(d.alias) {
			out = append(out, d.simpleItem(k, a.Name, a, a.Name, a.Cmd, a.Desc))
		}
	default:
		for _, f := range listOf(d.funcs) {
			out = append(out, d.simpleItem(k, f.Name, f, f.Name, f.Kind, f.Desc, f.Script))
		}
	}

	return out
}

// connItem prepares a connection.
//
// Parameters:
//   - c: connection to show
//   - invalid: why the record fails validation, nil when it passes
func (d *dataset) connItem(c store.Connection, invalid error) item {
	target := c.Target
	if target == "" {
		target = c.Name
	}

	it := item{
		Kind: kindConn, Key: c.Name, Rec: c,
		Cells: []string{sanitize(c.Name), sanitize(target), sanitize(c.User), sanitize(c.Desc)},
		Names: []string{strings.ToLower(sanitize(c.Name))},
	}

	text := []string{c.Name, target, c.User, c.IP, c.Desc}

	for _, a := range c.Aliases {
		it.Names = append(it.Names, strings.ToLower(sanitize(a)))
		text = append(text, a)
	}

	it.Text = strings.ToLower(sanitize(strings.Join(text, " ")))

	switch {
	case c.NeedsReview:
		it.Warn = d.tr.T("ui_warn_review")
	case invalid != nil:
		it.Warn = d.tr.T("ui_warn_invalid", sanitize(invalid.Error()))
	}

	return it
}

// simpleItem prepares an alias, a repository or a function.
//
// Parameters:
//   - k: kind of the record
//   - key: raw key
//   - rec: typed record
//   - name: name shown in the first column
//   - cells: the other columns in order, then any more words that the filter searches, such as
//     the script of a function
func (d *dataset) simpleItem(k kind, key string, rec any, name string, cells ...string) item {
	it := item{
		Kind: k, Key: key, Rec: rec, Cells: []string{sanitize(name)},
		Names: []string{strings.ToLower(sanitize(name))},
	}
	text := []string{name}

	for _, c := range cells {
		it.Cells = append(it.Cells, sanitize(c))
		text = append(text, c)
	}

	it.Text = strings.ToLower(sanitize(strings.Join(text, " ")))

	return it
}

// record returns the typed record of a key.
//
// Parameters:
//   - k: kind of the record
//   - key: raw key
//
// Returns the record as a store.Connection, store.Repo, store.Alias or store.Func value, and
// false when no record has this key.
func (d *dataset) record(k kind, key string) (any, bool) {
	switch k {
	case kindConn:
		return getOf(d.conns, key)
	case kindRepo:
		return getOf(d.repos, key)
	case kindAlias:
		return getOf(d.alias, key)
	}

	return getOf(d.funcs, key)
}

// getOf returns a record of a possibly missing collection as an any value.
//
// Parameters:
//   - c: collection, nil when it could not be opened
//   - key: raw key
func getOf[T store.Record](c *store.Collection[T], key string) (any, bool) {
	if c == nil {
		return nil, false
	}

	r, ok := c.Get(key)

	return r, ok
}

// kindName returns the singular name of a kind in the language of the interface.
//
// Parameters:
//   - k: kind to name
func (d *dataset) kindName(k kind) string {
	return d.tr.T(k.singularKey())
}

// save adds a record or replaces one.
//
// Parameters:
//   - k: kind of the record
//   - oldKey: key of the record to replace, empty to add
//   - rec: a store.Connection, store.Repo, store.Alias or store.Func value
//
// Returns nil on success. Otherwise it returns a *userError whose message is translated and
// sanitized, after the same checks as the command line: a name shared with another kind, a
// reserved name, a relative path. Nothing is written on error.
func (d *dataset) save(k kind, oldKey string, rec any) error {
	if err := d.err(k); err != nil {
		return d.explain(k, oldKey, store.ErrReadOnly)
	}

	if err := d.clash(k, oldKey, rec); err != nil {
		return err
	}

	var err error

	switch r := rec.(type) {
	case store.Connection:
		err = commit(d.conns, oldKey, r)
	case store.Repo:
		err = commit(d.repos, oldKey, r)
	case store.Alias:
		err = commit(d.alias, oldKey, r)
	case store.Func:
		err = commit(d.funcs, oldKey, r)
	default:
		err = errors.New("unsupported record")
	}

	if err != nil {
		return d.explain(k, cmp.Or(oldKey, nameOf(rec)), err)
	}

	return nil
}

// nameOf returns the name of a record.
//
// Parameters:
//   - rec: a store.Connection, store.Repo, store.Alias or store.Func value
//
// Returns an empty string for any other value.
func nameOf(rec any) string {
	switch r := rec.(type) {
	case store.Connection:
		return r.Name
	case store.Repo:
		return r.Name
	case store.Alias:
		return r.Name
	case store.Func:
		return r.Name
	}

	return ""
}

// commit adds a record, or replaces the one at oldKey.
//
// Parameters:
//   - c: collection to write
//   - oldKey: key to replace, empty to add
//   - r: new content
func commit[T store.Record](c *store.Collection[T], oldKey string, r T) error {
	if oldKey == "" {
		return c.Add(r)
	}

	return c.Replace(oldKey, r)
}

// remove deletes a record.
//
// Parameters:
//   - k: kind of the record
//   - key: raw key
//
// Returns nil on success, else a *userError.
func (d *dataset) remove(k kind, key string) error {
	if err := d.err(k); err != nil {
		return d.explain(k, key, store.ErrReadOnly)
	}

	var err error

	switch k {
	case kindConn:
		err = d.conns.Delete(key)
	case kindRepo:
		err = d.repos.Delete(key)
	case kindAlias:
		err = d.alias.Delete(key)
	default:
		err = d.funcs.Delete(key)
	}

	if err != nil {
		return d.explain(k, key, err)
	}

	return nil
}

// explain turns a store error into a *userError.
//
// Parameters:
//   - k: kind of the record concerned
//   - key: raw key of the record
//   - err: error returned by the store
func (d *dataset) explain(k kind, key string, err error) error {
	name := sanitize(key)

	switch {
	case errors.Is(err, store.ErrReadOnly):
		return &userError{msg: d.readOnlyText(k), err: err}
	case errors.Is(err, store.ErrExists):
		return &userError{msg: d.tr.T("cli_exists", d.kindName(k), name), err: err}
	case errors.Is(err, store.ErrNotFound):
		return &userError{msg: d.tr.T("cli_not_found", d.kindName(k), name), err: err}
	case errors.Is(err, store.ErrBusy):
		return &userError{msg: d.tr.T("ui_busy"), err: err}
	case errors.Is(err, store.ErrConflict):
		return &userError{msg: d.tr.T("cli_conflict", d.kindName(k)), err: err}
	case errors.Is(err, store.ErrTooBig):
		if m := tooBigFile.FindStringSubmatch(err.Error()); m != nil {
			return &userError{msg: d.tr.T("store_too_big", sanitize(m[1])), err: err}
		}
	}

	return &userError{msg: sanitize(err.Error()), err: err}
}

// clash applies the checks of the command line that the store does not do.
//
// Parameters:
//   - k: kind of the record
//   - oldKey: key being replaced, empty for an add
//   - rec: record about to be written
//
// Returns nil when the record may be stored, else a *userError.
func (d *dataset) clash(k kind, oldKey string, rec any) error {
	switch r := rec.(type) {
	case store.Connection:
		return d.connClash(oldKey, r)
	case store.Repo:
		if !shell.AbsOrHome(r.Path) {
			return &userError{
				msg: d.tr.T("cli_path_not_absolute"), err: store.ErrInvalidField,
				field: "path",
			}
		}
	case store.Func:
		if r.Kind == store.KindSource && !shell.ValidScript(r.Script) {
			return &userError{
				msg: d.tr.T("cli_script_not_absolute"), err: store.ErrInvalidField,
				field: "script",
			}
		}
	}

	return d.shellClash(k, oldKey, rec)
}

// connClash refuses a name or an alias that another connection already uses, with the rule of
// the command line.
//
// Parameters:
//   - oldKey: key being replaced, empty for an add
//   - rec: connection about to be written
func (d *dataset) connClash(oldKey string, rec store.Connection) error {
	var old []string

	if prev, ok := getOf(d.conns, oldKey); ok && oldKey != "" {
		c := prev.(store.Connection)
		old = append([]string{c.Name}, c.Aliases...)
	}

	for _, w := range append([]string{rec.Name}, rec.Aliases...) {
		if store.ReservedRecordName(w) && !holdsName(old, w) {
			return d.reserved(w, w == rec.Name)
		}
	}

	if word, owner, found := store.SharedName(listOf(d.conns), oldKey, rec); found {
		err := d.taken(word, kindConn, owner).(*userError)
		err.field = "aliases"

		if strings.EqualFold(word, rec.Name) {
			err.field = "name"
		}

		return err
	}

	return nil
}

// holdsName tells whether a list of names has a name, ignoring the case.
//
// Parameters:
//   - names: names of the record being replaced
//   - name: name to look for
func holdsName(names []string, name string) bool {
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return true
		}
	}

	return false
}

// reserved builds the error of a name that tote keeps for its command line.
//
// Parameters:
//   - name: the reserved word
//   - isName: true when the word is the name of the record, false when it is an alias
func (d *dataset) reserved(name string, isName bool) error {
	field := "aliases"
	if isName {
		field = "name"
	}

	return &userError{
		msg: d.tr.T("cli_reserved_name", sanitize(name)), err: store.ErrInvalidField,
		field: field,
	}
}

// shellClash refuses a name that the shell cannot give to an alias, a repository or a function:
// a reserved word, a word of the command line of tote (for an alias or a repository, unless the
// record already had it), or a name that one of the two other kinds already uses.
//
// Parameters:
//   - k: kind of the record
//   - oldKey: key being replaced, empty for an add
//   - rec: record about to be written
func (d *dataset) shellClash(k kind, oldKey string, rec any) error {
	name := nameOf(rec)
	if _, isConn := rec.(store.Connection); isConn || name == "" {
		return nil
	}

	if shell.Reserved(name) {
		return d.reserved(name, true)
	}

	if (k == kindAlias || k == kindRepo) && store.ReservedRecordName(name) &&
		!strings.EqualFold(name, oldKey) {
		return d.reserved(name, true)
	}

	for _, other := range []kind{kindAlias, kindRepo, kindFunc} {
		if other == k {
			continue
		}

		if _, ok := d.record(other, name); ok {
			err := d.taken(name, other, name).(*userError)
			err.field = "name"

			return err
		}
	}

	return nil
}

// taken builds the error of a name that another record already holds.
//
// Parameters:
//   - word: the shared name
//   - owner: kind of the record that holds it
//   - ownerName: name of that record
func (d *dataset) taken(word string, owner kind, ownerName string) error {
	return &userError{
		msg: d.tr.T("cli_name_taken", sanitize(word), d.kindName(owner), sanitize(ownerName)),
		err: store.ErrExists,
	}
}
