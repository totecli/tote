package store

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
)

// schemaVersion is the only value of the top-level schema key this package reads and writes.
const schemaVersion = 1

// maxFileSize is the largest data file this package reads, in bytes.
//
// A data file holds a few hundred records, so the cap only stops a corrupted or hostile file
// from filling the memory. The write side applies the same cap, so that the program never
// produces a file that it then refuses to read.
const maxFileSize = 4 << 20

// maxNesting is the deepest nesting of arrays and inline tables accepted in a data file.
//
// The TOML library recurses once per level, and a file made of a few million brackets would
// overflow the stack of the process, which no recover can catch.
const maxNesting = 32

var (
	// ErrReadOnly is returned by every write on a collection whose file could not be parsed.
	ErrReadOnly = errors.New("collection is read-only")

	// ErrExists is returned when a key is added or renamed to one that is already taken.
	ErrExists = errors.New("record already exists")

	// ErrNotFound is returned when the key of a replace or a delete is not in the file.
	ErrNotFound = errors.New("record not found")

	// ErrConflict is returned when the file was changed on disk since it was loaded and the
	// edit no longer applies cleanly to the new content.
	ErrConflict = errors.New("file changed on disk")

	// ErrBusy is returned when the lock of the file stayed taken by another process for longer
	// than the wait of the collection.
	ErrBusy = errors.New("file is locked by another process")

	// ErrTooBig is returned when a write would make the file larger than the size this package
	// accepts to read back.
	ErrTooBig = errors.New("file too large")
)

// errBadSchema is the decode failure of a schema key that is not an integer.
var errBadSchema = errors.New("schema must be an integer")

// Record is implemented by every kind of data stored in a Collection.
//
// Key returns the identity of the record inside one file, compared case-sensitively. Validate
// applies the field rules of the kind; it is called before every write and its result is never
// assumed from what the file contains.
type Record interface {
	Key() string
	Validate() error
}

// ParseError reports a file that cannot be used, with the position of the problem.
//
// File is the path given to Open. Line and Column are 1-based, and 0 when the position is
// unknown. Msg is a lowercase message without a final period. It covers TOML syntax errors,
// duplicate keys, values of the wrong type, an unknown schema number, duplicate record keys and
// headers the block writer does not support.
type ParseError struct {
	File   string
	Line   int
	Column int
	Msg    string
}

// Error implements error.
func (e *ParseError) Error() string {
	switch {
	case e.Line > 0 && e.Column > 0:
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Msg)
	case e.Line > 0:
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
	}

	return e.File + ": " + e.Msg
}

// snapshot is one parsed state of a file.
//
// Data and Sum are the bytes read and their SHA-256, Exists tells whether the file was there.
// Records, Spans and Index are parallel views of the decoded records: Spans[i] is the block of
// Records[i] and Index maps a key to i. Invalid holds the Validate error of each record that
// fails it. All of them are empty when the file did not parse.
type snapshot[T Record] struct {
	Data    []byte
	Sum     [sha256.Size]byte
	Exists  bool
	Records []T
	Spans   []span
	Index   map[string]int
	Invalid map[string]error
}

// editKind tells which write an edit performs.
type editKind int

const (
	// editAdd appends a new record at the end of the file.
	editAdd editKind = iota
	// editReplace rewrites the block of an existing record.
	editReplace
	// editDelete removes the block of an existing record.
	editDelete
)

// edit is one write request.
//
// Key is the key the edit addresses, which is the key of Rec for an add. Rec is the new record
// of an add or a replace. Prev is the record that Key designated in the collection when the edit
// was requested, set for a replace and a delete, so that a change made meanwhile by someone else
// can be detected.
type edit[T Record] struct {
	Kind editKind
	Key  string
	Rec  T
	Prev T
}

// Collection is one TOML file made of [[table]] blocks, decoded to T.
//
// The file starts with "schema = 1" and every record is a block introduced by [[table]]. Reads
// serve the state of the last Load or write. Writes edit the file block by block: the bytes
// outside the block concerned are kept as they are, comments and blank lines included, and
// nothing is ever sorted. A comment run that touches a header from above belongs to its block:
// it is kept by Replace and removed by Delete, unless it is a section title such as
// "# --- clients ---", which is never owned. Replace changes only the keys whose value changed:
// the other lines of the block, their comments and any key the type does not know are kept.
// Header lines must be written exactly [[table]], with no space inside the brackets, and the
// schema key must come before the first table.
//
// A file that fails to parse puts the collection in read-only mode: Err reports why and every
// write returns ErrReadOnly. A record that decodes but fails Validate is kept and listed by
// Invalid so that the user can fix it. A missing schema key is read as schema 1.
//
// A Collection is safe for use by one goroutine at a time. Several collections, in one process
// or in several, may share a file: writes are serialised by a lock file and a change made by
// somebody else between Load and a write is detected.
type Collection[T Record] struct {
	path     string
	table    string
	snap     snapshot[T]
	err      error
	lockWait time.Duration
}

// Open creates a collection for a file and loads it.
//
// The file may not exist yet: the collection is then empty, and the file is created with mode
// 0600 inside a directory created with mode 0700 by the first write.
//
// Parameters:
//   - path: TOML file path, not empty
//   - table: array of tables name without brackets, such as connection
//
// Returns a non-nil collection when the path and the table are usable. A file that does not
// parse is not an error here: the collection is read-only and Err reports it. The returned
// error is non-nil only for an invalid argument or when the file exists but cannot be read.
func Open[T Record](path, table string) (*Collection[T], error) {
	if path == "" {
		return nil, errors.New("open collection: empty path")
	}
	if !validTable(table) {
		return nil, fmt.Errorf("open collection: invalid table name %q", table)
	}

	c := &Collection[T]{path: path, table: table, lockWait: DefaultLockWait}
	if err := c.Load(); err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			return c, nil
		}
		return c, err
	}

	return c, nil
}

// SetLockWait sets how long a write waits for the lock of the file before it gives up with
// ErrBusy.
//
// Parameters:
//   - d: longest wait, zero or negative to fail at once when the lock is taken
func (c *Collection[T]) SetLockWait(d time.Duration) {
	c.lockWait = d
}

// validTable reports whether a name is usable as an array of tables name.
//
// Parameters:
//   - name: candidate name
func validTable(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		ok := r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z'
		if !ok {
			return false
		}
	}

	return true
}

// Load reads and parses the file again, replacing the state of the collection.
//
// Returns nil on success, which also lifts a previous read-only mode. A file that does not parse
// gives a *ParseError and leaves the collection empty and read-only. A file that cannot be read
// gives that error and leaves the collection empty and read-only too.
func (c *Collection[T]) Load() error {
	data, exists, err := readFile(c.path)
	if err != nil {
		c.snap = snapshot[T]{Invalid: map[string]error{}, Index: map[string]int{}}
		c.err = err
		return err
	}

	return c.adopt(data, exists)
}

// readFile reads a file, treating a missing file as empty.
//
// The file must be a regular file of at most maxFileSize bytes. It is opened without blocking so
// that a FIFO in its place gives an error instead of a hang.
//
// Parameters:
//   - path: file path, symlinks are followed
//
// Returns the content, whether the file exists, and an error for any failure but a missing file.
func readFile(path string) ([]byte, bool, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("read %s: not a regular file", path)
	}

	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxFileSize {
		return nil, false, fmt.Errorf("read %s: file larger than %d bytes", path, maxFileSize)
	}

	return data, true, nil
}

// adopt parses bytes and makes them the state of the collection.
//
// Parameters:
//   - data: file content
//   - exists: whether the file exists
//
// Returns the parse error, which is also kept as the read-only reason.
func (c *Collection[T]) adopt(data []byte, exists bool) error {
	snap, err := parseSnapshot[T](c.path, c.table, data, exists)
	c.snap = snap
	c.err = err

	return err
}

// parseSnapshot parses file bytes into a snapshot.
//
// Parameters:
//   - path: file path, used in errors
//   - table: array of tables name
//   - data: file content
//   - exists: whether the file exists
//
// Returns the snapshot, which keeps the bytes and their hash even on failure, and a *ParseError
// when the content cannot be used.
func parseSnapshot[T Record](path, table string, data []byte, exists bool) (snapshot[T], error) {
	snap := snapshot[T]{
		Data:    data,
		Sum:     sha256.Sum256(data),
		Exists:  exists,
		Index:   map[string]int{},
		Invalid: map[string]error{},
	}

	sc := scanBlocks(data, table)
	if sc.MaxDepth > maxNesting {
		msg := fmt.Sprintf("values nested more than %d levels deep", maxNesting)
		return snap, atLine(path, 0, msg)
	}
	if se := CheckShape(data, ShapeUser); se != nil {
		return snap, atLine(path, se.Line, se.Error())
	}

	recs, schema, err := decodeAll[T](data, table)
	if err != nil {
		return snap, parseFailure[T](path, data, sc, err)
	}
	if err := checkLayout(path, table, sc, recs, schema); err != nil {
		return snap, err
	}

	snap.Records = recs
	snap.Spans = sc.Blocks
	for i, r := range recs {
		snap.Index[r.Key()] = i
		if verr := r.Validate(); verr != nil {
			snap.Invalid[r.Key()] = verr
		}
	}

	return snap, nil
}

// decodeAll decodes a whole file with the TOML library.
//
// Parameters:
//   - data: file content
//   - table: array of tables name
//
// Returns the records of the table, the schema number (1 when the key is absent) and the
// library error, which is a toml.ParseError for a syntax problem.
func decodeAll[T Record](data []byte, table string) ([]T, int, error) {
	var top map[string]toml.Primitive
	md, err := toml.Decode(string(data), &top)
	if err != nil {
		return nil, 0, err
	}

	schema := schemaVersion
	if p, ok := top["schema"]; ok {
		if err := md.PrimitiveDecode(p, &schema); err != nil {
			return nil, 0, fmt.Errorf("%w: %w", errBadSchema, err)
		}
	}

	var recs []T
	if p, ok := top[table]; ok {
		if err := md.PrimitiveDecode(p, &recs); err != nil {
			return nil, 0, err
		}
	}

	return recs, schema, nil
}

// parseFailure turns a decoding error into a ParseError with the best position available.
//
// A syntax error carries its own position. A header the scanner does not support explains many
// type errors and is reported first. A value of the wrong type has no position, so each block is
// decoded alone to find the one at fault.
//
// Parameters:
//   - path: file path
//   - data: file content
//   - sc: scan of the content
//   - err: error returned by decodeAll
func parseFailure[T Record](path string, data []byte, sc scanResult, err error) *ParseError {
	var te toml.ParseError
	if errors.As(err, &te) {
		line, col := SyntaxPosition(data, te.Position)

		return &ParseError{File: path, Line: line, Column: col, Msg: cleanMessage(te.Message)}
	}
	if errors.Is(err, errBadSchema) {
		return atLine(path, sc.SchemaLine, errBadSchema.Error())
	}
	if len(sc.Bad) > 0 {
		return atLine(path, sc.Bad[0].Line, sc.Bad[0].Msg)
	}

	for _, sp := range sc.Blocks {
		var rec T
		body := blockBody(data, sp)
		_, derr := toml.Decode(string(body), &rec)
		if derr == nil {
			continue
		}
		if errors.As(derr, &te) {
			line, col := SyntaxPosition(body, te.Position)

			return &ParseError{
				File:   path,
				Line:   sp.Line + line,
				Column: col,
				Msg:    cleanMessage(te.Message),
			}
		}
		msg := derr.Error()
		return atLine(path, sp.Line+keyLine(blockBody(data, sp), lastKey(msg)), cleanMessage(msg))
	}

	return atLine(path, 0, cleanMessage(err.Error()))
}

// blockBody returns the lines of a block that follow its header line.
//
// Parameters:
//   - data: file content
//   - sp: span of the block
func blockBody(data []byte, sp span) []byte {
	block := data[sp.Header:sp.End]
	if i := bytes.IndexByte(block, '\n'); i >= 0 {
		return block[i+1:]
	}

	return nil
}

// atLine builds a ParseError at the start of a line.
//
// Parameters:
//   - path: file path
//   - line: 1-based line number, 0 when unknown
//   - msg: message of the error
func atLine(path string, line int, msg string) *ParseError {
	pe := &ParseError{File: path, Line: line, Msg: msg}
	if line > 0 {
		pe.Column = 1
	}

	return pe
}

// lastKeyPattern extracts the key named by a type error of the TOML library.
var lastKeyPattern = regexp.MustCompile(`\(last key "([^"]+)"\)`)

// lastKey returns the key a decoding error is about.
//
// Parameters:
//   - msg: message returned by the TOML library
//
// Returns the key, or an empty string when the message names none.
func lastKey(msg string) string {
	if m := lastKeyPattern.FindStringSubmatch(msg); m != nil {
		return m[1]
	}

	return ""
}

// keyLine finds the line that sets a key inside the body of a block.
//
// Parameters:
//   - body: lines of the block after its header
//   - key: bare key to look for, empty for none
//
// Returns the 1-based line number inside body, or 0 so that the caller stays on the header.
func keyLine(body []byte, key string) int {
	if key == "" {
		return 0
	}
	for i, line := range bytes.Split(body, []byte("\n")) {
		rest, ok := bytes.CutPrefix(bytes.TrimLeft(line, " \t"), []byte(key))
		if ok && bytes.HasPrefix(bytes.TrimLeft(rest, " \t"), []byte("=")) {
			return i + 1
		}
	}

	return 0
}

// cleanMessage turns a library message into a lowercase message without a final period.
//
// The "toml: " prefix and the "line N (last key ...): " prefix, whose line is relative to a
// block, are dropped.
//
// Parameters:
//   - msg: message returned by the TOML library
func cleanMessage(msg string) string {
	msg = strings.TrimPrefix(msg, "toml: ")
	if strings.HasPrefix(msg, "line ") {
		if _, rest, ok := strings.Cut(msg, "): "); ok {
			msg = rest
		}
	}
	msg = strings.TrimSuffix(msg, ".")
	if msg != "" {
		msg = strings.ToLower(msg[:1]) + msg[1:]
	}

	return msg
}

// checkLayout verifies that the decoded file matches what the block writer can edit.
//
// Parameters:
//   - path: file path
//   - table: array of tables name
//   - sc: scan of the content
//   - recs: records decoded from the content
//   - schema: schema number of the content
//
// Returns a *ParseError for an unknown schema, a header the scanner does not support, a record
// that is not written as a [[table]] block, or a key used twice; nil otherwise.
func checkLayout[T Record](
	path, table string, sc scanResult, recs []T, schema int,
) error {
	if schema != schemaVersion {
		msg := fmt.Sprintf("unsupported schema %d, this version reads schema %d", schema,
			schemaVersion)
		return atLine(path, sc.SchemaLine, msg)
	}
	if len(sc.Bad) > 0 {
		return atLine(path, sc.Bad[0].Line, sc.Bad[0].Msg)
	}
	if len(sc.Blocks) != len(recs) {
		return atLine(path, 0, "records of "+table+" must be written as [["+table+"]] blocks")
	}

	seen := make(map[string]int, len(recs))
	for i, r := range recs {
		if first, dup := seen[r.Key()]; dup {
			msg := fmt.Sprintf("duplicate key %q, first used on line %d", r.Key(),
				sc.Blocks[first].Line)
			return atLine(path, sc.Blocks[i].Line, msg)
		}
		seen[r.Key()] = i
	}

	return nil
}

// Path returns the file path given to Open.
func (c *Collection[T]) Path() string {
	return c.path
}

// Err returns why the collection is read-only.
//
// Returns nil when the file is usable, a *ParseError when it failed to parse, or the read error
// when it could not be read.
func (c *Collection[T]) Err() error {
	return c.err
}

// List returns a copy of the records in file order, never sorted.
//
// A record type that has a Clone method is copied with it, so that changing a slice of a
// returned record does not change the collection.
func (c *Collection[T]) List() []T {
	out := slices.Clone(c.snap.Records)
	for i := range out {
		out[i] = cloneRecord(out[i])
	}

	return out
}

// cloneRecord copies a record with its Clone method when it has one.
//
// Parameters:
//   - r: record to copy
func cloneRecord[T Record](r T) T {
	if c, ok := any(r).(interface{ Clone() T }); ok {
		return c.Clone()
	}

	return r
}

// Get returns the record with a key.
//
// Parameters:
//   - key: record key, compared case-sensitively
//
// Returns the record and true, or the zero value and false when no record has this key. The
// record is copied like the ones of List.
func (c *Collection[T]) Get(key string) (T, bool) {
	if i, ok := c.snap.Index[key]; ok {
		return cloneRecord(c.snap.Records[i]), true
	}

	var zero T
	return zero, false
}

// Invalid returns the records that decoded but fail Validate, as a copy.
//
// The map is keyed by Key and holds the Validate error. It is empty when every record is valid.
func (c *Collection[T]) Invalid() map[string]error {
	return maps.Clone(c.snap.Invalid)
}

// Line returns the line of the header of a record, so that an editor can be opened there.
//
// Parameters:
//   - key: record key
//
// Returns the 1-based line number, or 0 when no record has this key.
func (c *Collection[T]) Line(key string) int {
	if i, ok := c.snap.Index[key]; ok {
		return c.snap.Spans[i].Line
	}

	return 0
}

// Add appends a record at the end of the file.
//
// Parameters:
//   - r: record to add, validated before the write
//
// Returns ErrExists when the key is taken, ErrReadOnly on a read-only collection, ErrConflict
// when the file changed meanwhile in a way that clashes with the edit, the Validate error of the
// record, ErrTooBig when the file would pass the size cap, or an I/O error. Nothing is written
// on error.
func (c *Collection[T]) Add(r T) error {
	return c.commit(edit[T]{Kind: editAdd, Key: r.Key(), Rec: r})
}

// Replace rewrites the block of a record, which may be renamed.
//
// Parameters:
//   - key: current key of the record
//   - r: new content, validated before the write; its key may differ from key
//
// Returns ErrNotFound when no record has the key, ErrExists when a rename collides with another
// record, ErrReadOnly, ErrConflict, the Validate error of r, ErrTooBig, or an I/O error. Nothing
// is written on error.
func (c *Collection[T]) Replace(key string, r T) error {
	return c.commit(edit[T]{Kind: editReplace, Key: key, Rec: r})
}

// Delete removes the block of a record, with the comment lines that touch its header, except a
// section title such as "# --- clients ---".
//
// Parameters:
//   - key: key of the record to remove
//
// Returns ErrNotFound when no record has the key, ErrReadOnly, ErrConflict, or an I/O error.
// Nothing is written on error.
func (c *Collection[T]) Delete(key string) error {
	return c.commit(edit[T]{Kind: editDelete, Key: key})
}

// commit runs an edit: checks it, takes the lock, refreshes the state and writes.
//
// Parameters:
//   - e: edit to perform
func (c *Collection[T]) commit(e edit[T]) error {
	if c.err != nil {
		return c.readOnly()
	}
	if e.Kind != editDelete {
		if err := e.Rec.Validate(); err != nil {
			return fmt.Errorf("invalid %s %q: %w", c.table, e.Rec.Key(), err)
		}
	}

	if i, ok := c.snap.Index[e.Key]; ok {
		e.Prev = c.snap.Records[i]
	}
	if err := c.recheck(e, false); err != nil {
		return err
	}

	target, err := resolveTarget(c.path)
	if err != nil {
		return err
	}
	lock, err := acquireLock(target, c.lockWait)
	if err != nil {
		return err
	}
	defer releaseLock(lock)

	return c.writeLocked(target, e)
}

// readOnly builds the error returned by writes on a read-only collection.
func (c *Collection[T]) readOnly() error {
	return fmt.Errorf("%w: %w", ErrReadOnly, c.err)
}

// writeLocked performs an edit while the lock is held.
//
// The file is read again; when it differs from what was loaded, the new content becomes the
// state of the collection and the edit is checked against it before being applied.
//
// Parameters:
//   - target: resolved path of the file
//   - e: edit to perform
func (c *Collection[T]) writeLocked(target string, e edit[T]) error {
	data, exists, err := readFile(target)
	if err != nil {
		return err
	}

	changed := exists != c.snap.Exists || sha256.Sum256(data) != c.snap.Sum
	if changed {
		if err := c.adopt(data, exists); err != nil {
			return c.readOnly()
		}
		if err := c.recheck(e, true); err != nil {
			return err
		}
	}

	out, want, err := c.build(e)
	if err != nil {
		return err
	}
	if err := c.verify(out, want); err != nil {
		return err
	}
	if err := writeAtomic(target, out); err != nil {
		return err
	}

	return c.adopt(out, true)
}

// recheck tests an edit against the current state of the collection.
//
// When changed is false the state is what the caller saw, so a missing or taken key is reported
// as ErrNotFound or ErrExists. When changed is true the file moved under the caller, so the same
// situations, and a record of the edit that was modified by somebody else, are ErrConflict.
//
// Parameters:
//   - e: edit to test
//   - changed: whether the state was reloaded because the file changed on disk
func (c *Collection[T]) recheck(e edit[T], changed bool) error {
	miss, clash := ErrNotFound, ErrExists
	if changed {
		miss, clash = ErrConflict, ErrConflict
	}

	i, found := c.snap.Index[e.Key]
	if e.Kind == editAdd {
		if found {
			return c.wrap(clash, e.Key)
		}
		return nil
	}

	if !found {
		return c.wrap(miss, e.Key)
	}
	if changed && !sameValueOf(c.snap.Records[i], e.Prev) {
		return c.wrap(ErrConflict, e.Key)
	}
	if e.Kind == editReplace && e.Rec.Key() != e.Key {
		if _, taken := c.snap.Index[e.Rec.Key()]; taken {
			return c.wrap(clash, e.Rec.Key())
		}
	}

	return nil
}

// wrap adds the table and the key to a sentinel error.
//
// Parameters:
//   - err: sentinel error to wrap
//   - key: record key concerned
func (c *Collection[T]) wrap(err error, key string) error {
	return fmt.Errorf("%s %q: %w", c.table, key, err)
}

// build computes the bytes of the file after an edit and the records it must then contain.
//
// Parameters:
//   - e: edit to apply, already checked against the current state
//
// Returns the new file content and the expected records, or an encoding error.
func (c *Collection[T]) build(e edit[T]) ([]byte, []T, error) {
	s := &c.snap
	want := slices.Clone(s.Records)

	if e.Kind == editDelete {
		i := s.Index[e.Key]
		return removeBlock(s.Data, s.Spans[i]), slices.Delete(want, i, i+1), nil
	}

	eol := dominantEOL(s.Data)
	if e.Kind == editReplace {
		eol = headerEOL(s.Data, s.Spans[s.Index[e.Key]], eol)
	}
	block, err := encodeBlock(c.table, e.Rec, eol)
	if err != nil {
		return nil, nil, fmt.Errorf("encode %s %q: %w", c.table, e.Rec.Key(), err)
	}
	if e.Kind == editAdd {
		sc := scanBlocks(s.Data, c.table)
		withSchema := sc.SchemaLine == 0 && !sc.HasHeader
		return addBlock(s.Data, block, eol, withSchema), append(want, e.Rec), nil
	}

	i := s.Index[e.Key]
	want[i] = e.Rec
	sp := s.Spans[i]
	unknown, err := unknownKeys[T](blockBody(s.Data, sp))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s %q: %w", c.table, e.Key, err)
	}

	return replaceBlock(s.Data, sp, patchBlock(s.Data[sp.Header:sp.End], block, unknown, eol)),
		want, nil
}

// verify parses candidate bytes and checks that they decode to the intended records.
//
// Parameters:
//   - out: candidate file content
//   - want: records the file must decode to, in order
//
// Returns an error, and the caller writes nothing, when the bytes do not parse or decode to
// something else, for instance because a field cannot round-trip through TOML.
func (c *Collection[T]) verify(out []byte, want []T) error {
	got, _, err := decodeAll[T](out, c.table)
	if err != nil {
		return fmt.Errorf("verify %s: edited file does not parse: %w", c.path, err)
	}
	if !sameRecords(got, want) {
		return fmt.Errorf("verify %s: edited file does not decode to the intended records",
			c.path)
	}

	return nil
}
