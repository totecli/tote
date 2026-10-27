package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// ErrLayout is returned by PatchTable when the file is valid TOML but written in a way the
// editor does not dare to touch: the table is defined through dotted keys or inline tables,
// is declared twice or as an array of tables, holds keys that are not plain values, or a value
// does not survive a round trip through TOML.
var ErrLayout = errors.New("unsupported layout")

// tableLockWait is how long PatchTable waits for the lock of a file, a variable so that tests
// can shorten it.
var tableLockWait = DefaultLockWait

// pieceKind tells what a piece of a flat TOML file is.
type pieceKind int

const (
	// pieceFiller is a comment line or a blank line.
	pieceFiller pieceKind = iota
	// pieceHeader is a table header, [a.b] or [[a.b]].
	pieceHeader
	// pieceKey is a key with its value.
	pieceKey
)

// tpiece is one statement of a flat TOML file with the table it belongs to.
//
// For a header, Path is the table it opens and Array tells whether it is a [[table]] header. For
// a key, Path is the table that is open where the key is written, Key the root key decoded from
// the statement and Value the decoded value, which is a map when the statement uses a dotted key
// or an inline table.
type tpiece struct {
	Text  []byte
	Kind  pieceKind
	Key   string
	Value any
	Path  []string
	Array bool
}

// tableEdit is a validated request to set and unset root keys of one table.
//
// Order lists the keys to set in ascending order, Lines the TOML text of each assignment without
// its line ending, Want the value each key must decode to, and Unset the keys to remove.
type tableEdit struct {
	Order []string
	Lines map[string]string
	Want  map[string]any
	Unset map[string]bool
}

// PatchTable edits root keys of one table of a TOML file and keeps everything else as it is.
//
// The table is a dotted path such as "plugins.gft" and is created, with a blank line before its
// header, when the file has none. An empty table means the root of the file, which is how flat
// settings files and the root keys of config.toml are edited. The write takes the same lock and
// the same atomic replacement as a collection, follows a symlinked file, creates a missing file
// with the schema = 1 line and the mode 0600, and keeps the mode of an existing file. A key
// that exists keeps its place and its trailing comment, a new key goes after the last key of the
// table, an unset key is removed, and comments, blank lines, unknown keys, other tables, the
// line endings and a byte order mark stay as they are. Nothing is written when the result would
// be identical to the file.
//
// The edit is refused with ErrLayout when the table is defined through dotted keys or inline
// tables, is declared twice or as an array of tables, holds anything but plain values, or when
// the edited text does not decode to exactly the old content plus the intended change. A file
// that does not parse gives a *ParseError. The file is never written on error.
//
// Parameters:
//   - path: file path, a symlink is followed and a missing file is created
//   - table: dotted bare-key path of the table, or "" for the root of the file
//   - set: root keys to write, values being string, bool, any integer or []string
//   - unset: root keys to remove, a key that is absent is ignored
//
// Returns nil on success, ErrLayout or a *ParseError wrapped with the file name, ErrBusy when
// the lock stayed taken, ErrTooBig when the result would pass the size cap of a data file, or an
// I/O error.
func PatchTable(path, table string, set map[string]any, unset []string) error {
	tpath, err := parseTablePath(table)
	if err != nil {
		return err
	}
	ed, err := newTableEdit(tpath, set, unset)
	if err != nil {
		return err
	}
	if len(ed.Order) == 0 && len(ed.Unset) == 0 {
		return nil
	}
	if len(ed.Order) == 0 {
		if _, serr := os.Lstat(path); errors.Is(serr, fs.ErrNotExist) {
			return nil
		}
	}

	target, err := resolveTarget(path)
	if err != nil {
		return err
	}
	lock, err := acquireLock(target, tableLockWait)
	if err != nil {
		return err
	}
	defer releaseLock(lock)

	data, exists, err := readFile(target)
	if err != nil {
		return err
	}
	out, err := patchBytes(data, tpath, ed)
	if err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			pe.File = path
			return pe
		}

		return fmt.Errorf("patch %s: %w", path, err)
	}
	if exists && bytes.Equal(out, data) || !exists && len(out) == 0 {
		return nil
	}

	return writeAtomic(target, out)
}

// parseTablePath splits a dotted table path.
//
// Parameters:
//   - table: dotted path made of bare keys, or "" for the root
//
// Returns the segments, nil for the root, or an error for an empty or non-bare segment.
func parseTablePath(table string) ([]string, error) {
	if table == "" {
		return nil, nil
	}

	segs := strings.Split(table, ".")
	for _, s := range segs {
		if !validTable(s) {
			return nil, fmt.Errorf("invalid table path %q", table)
		}
	}

	return segs, nil
}

// newTableEdit validates and encodes a request.
//
// Parameters:
//   - tpath: table path, nil for the root
//   - set: keys to write
//   - unset: keys to remove
//
// Returns the edit, or an error for an empty key, a key in both lists, a value of an
// unsupported type, or the key "schema" at the root of a file.
func newTableEdit(tpath []string, set map[string]any, unset []string) (tableEdit, error) {
	ed := tableEdit{
		Lines: map[string]string{},
		Want:  map[string]any{},
		Unset: map[string]bool{},
	}

	for _, k := range unset {
		if err := checkKey(tpath, k); err != nil {
			return ed, err
		}
		ed.Unset[k] = true
	}
	for k, v := range set {
		if err := checkKey(tpath, k); err != nil {
			return ed, err
		}
		if ed.Unset[k] {
			return ed, fmt.Errorf("key %q is both set and unset", k)
		}
		text, want, err := encodeValue(v)
		if err != nil {
			return ed, fmt.Errorf("key %q: %w", k, err)
		}
		name, err := encodeKey(k)
		if err != nil {
			return ed, err
		}
		ed.Order = append(ed.Order, k)
		ed.Lines[k] = name + " = " + text
		ed.Want[k] = want
	}
	slices.Sort(ed.Order)

	return ed, nil
}

// checkKey validates a key name given to PatchTable.
//
// Parameters:
//   - tpath: table path, nil for the root
//   - key: key name
func checkKey(tpath []string, key string) error {
	switch {
	case key == "":
		return errors.New("empty key")
	case len(tpath) == 0 && key == "schema":
		return errors.New("the schema key is not editable")
	case !utf8.ValidString(key):
		return fmt.Errorf("key %q: %w", key, ErrLayout)
	}

	return nil
}

// encodeKey renders a key, quoted when it is not a bare key.
//
// Parameters:
//   - key: key name
func encodeKey(key string) (string, error) {
	if validTable(key) {
		return key, nil
	}

	return quoteString(key)
}

// quoteString renders a string as a TOML basic string.
//
// Quotes, backslashes and control characters are escaped, other characters are written as they
// are.
//
// Parameters:
//   - s: valid UTF-8 text
//
// Returns the quoted text, or ErrLayout for invalid UTF-8, which cannot round-trip.
func quoteString(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", fmt.Errorf("invalid UTF-8: %w", ErrLayout)
	}

	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')

	return b.String(), nil
}

// encodeValue renders a value and gives the form the TOML decoder returns for it.
//
// Parameters:
//   - v: string, bool, any integer type that fits an int64, or a slice of strings
//
// Returns the TOML text, the decoded form (string, bool, int64 or []any of strings), or an
// error for any other type.
func encodeValue(v any) (string, any, error) {
	rv := reflect.ValueOf(v)

	switch rv.Kind() {
	case reflect.String:
		text, err := quoteString(rv.String())
		return text, rv.String(), err
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool()), rv.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return "", nil, fmt.Errorf("integer out of range: %w", ErrLayout)
		}
		return strconv.FormatUint(u, 10), int64(u), nil
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.String {
			return encodeList(rv)
		}
	}

	return "", nil, fmt.Errorf("unsupported value type %T", v)
}

// encodeList renders a slice of strings as a TOML array.
//
// Parameters:
//   - rv: reflected slice whose elements are strings
func encodeList(rv reflect.Value) (string, any, error) {
	parts := make([]string, rv.Len())
	want := make([]any, rv.Len())
	for i := range parts {
		s := rv.Index(i).String()
		text, err := quoteString(s)
		if err != nil {
			return "", nil, err
		}
		parts[i], want[i] = text, s
	}

	return "[" + strings.Join(parts, ", ") + "]", want, nil
}

// headerPath reads the table named by a header statement.
//
// Parameters:
//   - text: lines of the header statement
//
// Returns the path, whether it is an array of tables header, and false when the statement does
// not parse alone.
func headerPath(text []byte) ([]string, bool, bool) {
	var m map[string]any
	if _, err := toml.Decode(string(text), &m); err != nil {
		return nil, false, false
	}

	var path []string
	var cur any = m
	for {
		node, ok := cur.(map[string]any)
		if !ok || len(node) == 0 {
			break
		}
		if len(node) != 1 {
			return nil, false, false
		}
		for k, v := range node {
			path = append(path, k)
			cur = v
		}
	}

	switch cur.(type) {
	case []map[string]any, []any:
		return path, true, len(path) > 0
	}

	return path, false, len(path) > 0
}

// classifyPieces cuts a file into statements and tells which table each one belongs to.
//
// Parameters:
//   - body: file content without its byte order mark
//
// Returns the pieces in file order, and false when a header does not parse, in which case the
// file is not valid TOML.
func classifyPieces(body []byte) ([]tpiece, bool) {
	var (
		out []tpiece
		ctx []string
	)

	for _, p := range splitPieces(body) {
		tp := tpiece{Text: p.Text}
		trimmed := bytes.TrimLeft(p.Text, " \t")

		switch {
		case len(trimmed) == 0 || trimmed[0] == '#' || trimmed[0] == '\r' || trimmed[0] == '\n':
			tp.Kind = pieceFiller
		case trimmed[0] == '[':
			path, array, ok := headerPath(p.Text)
			if !ok {
				return nil, false
			}
			tp.Kind, tp.Path, tp.Array = pieceHeader, path, array
			ctx = path
		default:
			tp.Kind, tp.Key, tp.Value, tp.Path = pieceKey, p.Key, p.Value, ctx
		}
		out = append(out, tp)
	}

	return out, true
}

// hasPrefix reports whether a path starts with another one.
//
// Parameters:
//   - path: path to test
//   - prefix: candidate prefix, nil matches every path
func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

// dottedPath follows the single-key maps that a dotted key or an inline table decodes to.
//
// Parameters:
//   - ctx: table that is open where the statement is written
//   - key: root key of the statement
//   - value: decoded value of the statement
//
// Returns the full path of the deepest table or key the statement defines.
func dottedPath(ctx []string, key string, value any) []string {
	full := append(slices.Clone(ctx), key)
	for {
		node, ok := value.(map[string]any)
		if !ok || len(node) != 1 {
			return full
		}
		for k, v := range node {
			full = append(full, k)
			value = v
		}
	}
}

// hasMap reports whether a decoded value is or holds a table.
//
// Parameters:
//   - v: decoded value
func hasMap(v any) bool {
	switch x := v.(type) {
	case map[string]any, []map[string]any:
		return true
	case []any:
		return slices.ContainsFunc(x, hasMap)
	}

	return false
}

// checkTableLayout refuses the layouts of a file that PatchTable cannot edit safely.
//
// Parameters:
//   - ps: statements of the file
//   - tpath: table to edit, nil for the root
//
// Returns an error wrapping ErrLayout, or nil.
func checkTableLayout(ps []tpiece, tpath []string) error {
	seen := false

	for _, p := range ps {
		switch p.Kind {
		case pieceHeader:
			if len(tpath) == 0 {
				continue
			}
			if p.Array && hasPrefix(tpath, p.Path) {
				return fmt.Errorf("%w: %s is an array of tables", ErrLayout,
					strings.Join(p.Path, "."))
			}
			if slices.Equal(p.Path, tpath) {
				if seen {
					return fmt.Errorf("%w: table %s is declared twice", ErrLayout,
						strings.Join(tpath, "."))
				}
				seen = true
			}
		case pieceKey:
			if err := checkKeyPiece(p, tpath); err != nil {
				return err
			}
		}
	}

	return nil
}

// checkKeyPiece refuses a key statement that defines the target table by other means than a
// header, or that is not a plain key inside it.
//
// Parameters:
//   - p: key statement
//   - tpath: table to edit, nil for the root
func checkKeyPiece(p tpiece, tpath []string) error {
	switch {
	case slices.Equal(p.Path, tpath):
		if hasMap(p.Value) {
			return fmt.Errorf("%w: key %q is a dotted key or an inline table", ErrLayout, p.Key)
		}
	case len(p.Path) < len(tpath) && hasPrefix(tpath, p.Path):
		full := dottedPath(p.Path, p.Key, p.Value)
		if hasPrefix(full, tpath) || hasPrefix(tpath, full) {
			return fmt.Errorf("%w: %s is defined by a dotted key or an inline table", ErrLayout,
				strings.Join(tpath, "."))
		}
	}

	return nil
}

// replaceKeyLine gives a new assignment the trailing comment and the line ending of the lines it
// replaces.
//
// Parameters:
//   - old: lines of the statement being replaced
//   - line: new assignment without line ending
//   - eol: line ending for the new line when the old one has none to give
func replaceKeyLine(old []byte, line, eol string) []byte {
	out := withComment(old, []byte(line+eol))
	if !bytes.HasSuffix(old, []byte("\n")) {
		out = bytes.TrimSuffix(bytes.TrimSuffix(out, []byte("\n")), []byte("\r"))
	}

	return out
}

// tableHeader renders the header line of a table.
//
// Parameters:
//   - tpath: non-empty table path of bare keys
func tableHeader(tpath []string) string {
	return "[" + strings.Join(tpath, ".") + "]"
}

// appendLines writes the assignments of the keys that are still to set.
//
// Parameters:
//   - out: output so far
//   - ed: edit to apply
//   - done: keys already written in place
//   - eol: line ending
func appendLines(out []byte, ed tableEdit, done map[string]bool, eol string) []byte {
	for _, k := range ed.Order {
		if !done[k] {
			out = append(out, ed.Lines[k]...)
			out = append(out, eol...)
		}
	}

	return out
}

// freshFile renders a file that does not exist yet.
//
// Parameters:
//   - tpath: table to create, nil for the root
//   - ed: edit to apply
//   - eol: line ending
func freshFile(tpath []string, ed tableEdit, eol string) []byte {
	out := []byte("schema = 1" + eol)
	if len(tpath) > 0 {
		out = append(out, eol+tableHeader(tpath)+eol...)
	}

	return appendLines(out, ed, nil, eol)
}

// appendTable adds a missing table at the end of a file.
//
// Parameters:
//   - body: file content
//   - tpath: table to create
//   - ed: edit to apply
//   - eol: line ending
func appendTable(body []byte, tpath []string, ed tableEdit, eol string) []byte {
	out := slices.Clone(body)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, eol...)
	}
	if len(out) > 0 && !endsBlank(out) {
		out = append(out, eol...)
	}
	out = append(out, tableHeader(tpath)+eol...)

	return appendLines(out, ed, nil, eol)
}

// locateTable finds the header of the table and the last key written in it.
//
// Parameters:
//   - ps: statements of the file
//   - tpath: table to edit, nil for the root
//
// Returns whether the table exists and the index of the statement after which new keys go, which
// is -1 for the root of a file without keys.
func locateTable(ps []tpiece, tpath []string) (bool, int) {
	if len(tpath) == 0 {
		at := -1
		for i, p := range ps {
			if p.Kind == pieceKey && len(p.Path) == 0 {
				at = i
			}
		}

		return true, at
	}

	header, last := -1, -1
	for i, p := range ps {
		switch {
		case p.Kind == pieceHeader && slices.Equal(p.Path, tpath):
			header = i
		case p.Kind == pieceKey && slices.Equal(p.Path, tpath):
			last = i
		}
	}
	if header < 0 {
		return false, -1
	}

	return true, max(header, last)
}

// applyEdit rewrites the statements of a file for an edit.
//
// Parameters:
//   - ps: statements of the file
//   - tpath: table to edit, nil for the root
//   - ed: edit to apply
//   - eol: line ending for the new lines
//
// Returns the new content, or false when the table does not exist.
func applyEdit(ps []tpiece, tpath []string, ed tableEdit, eol string) ([]byte, bool) {
	found, after := locateTable(ps, tpath)
	if !found {
		return nil, false
	}

	var out []byte
	done := map[string]bool{}
	insert := func() {
		if len(ed.Order) == len(done) {
			return
		}
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, eol...)
		}
		out = appendLines(out, ed, done, eol)
	}

	if after < 0 {
		insert()
	}
	for i, p := range ps {
		text := p.Text
		inTable := p.Kind == pieceKey && slices.Equal(p.Path, tpath)

		switch {
		case inTable && ed.Unset[p.Key]:
			text = nil
		case inTable && ed.Lines[p.Key] != "":
			done[p.Key] = true
			if !reflect.DeepEqual(p.Value, ed.Want[p.Key]) {
				text = replaceKeyLine(p.Text, ed.Lines[p.Key], eol)
			}
		}
		out = append(out, text...)

		if i == after {
			insert()
		}
	}

	return out, true
}

// expectedTree computes what a file must decode to after an edit.
//
// Parameters:
//   - body: original file content
//   - tpath: table to edit, nil for the root
//   - ed: edit to apply
//   - schema: whether the schema key is added because the file is new
//
// Returns the expected tree, or ErrLayout when a segment of the path is not a table.
func expectedTree(body []byte, tpath []string, ed tableEdit, schema bool) (map[string]any, error) {
	tree := map[string]any{}
	if _, err := toml.Decode(string(body), &tree); err != nil {
		return nil, err
	}
	if schema {
		tree["schema"] = int64(1)
	}

	node := tree
	for _, seg := range tpath {
		child, ok := node[seg]
		if !ok {
			if len(ed.Order) == 0 {
				return tree, nil
			}
			child = map[string]any{}
			node[seg] = child
		}
		next, ok := child.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: %s is not a table", ErrLayout, seg)
		}
		node = next
	}

	for k := range ed.Unset {
		delete(node, k)
	}
	for k, v := range ed.Want {
		node[k] = v
	}

	return tree, nil
}

// tomlFailure turns an error of the TOML library into a ParseError.
//
// Parameters:
//   - data: the text that was decoded
//   - err: error returned by toml.Decode
func tomlFailure(data []byte, err error) error {
	var te toml.ParseError
	if errors.As(err, &te) {
		line, col := SyntaxPosition(data, te.Position)

		return &ParseError{Line: line, Column: col, Msg: cleanMessage(te.Message)}
	}

	return atLine("", 0, cleanMessage(err.Error()))
}

// patchBytes computes the content of a file after an edit, without touching the disk.
//
// Parameters:
//   - data: current content of the file, empty when it does not exist
//   - tpath: table to edit, nil for the root
//   - ed: validated edit
//
// Returns the new content, which is data itself when nothing changes, ErrLayout, or a
// *ParseError without a file name.
func patchBytes(data []byte, tpath []string, ed tableEdit) ([]byte, error) {
	bom := []byte("\xef\xbb\xbf")
	body := bytes.TrimPrefix(data, bom)
	if len(ed.Order) == 0 && len(ed.Unset) == 0 {
		return data, nil
	}
	if scanBlocks(body, "").MaxDepth > maxNesting {
		return nil, atLine("", 0, fmt.Sprintf("values nested more than %d levels deep", maxNesting))
	}
	if se := CheckShape(body, ShapeUser); se != nil {
		return nil, atLine("", se.Line, se.Error())
	}

	ps, ok := classifyPieces(body)
	if ok {
		if err := checkTableLayout(ps, tpath); err != nil {
			return nil, err
		}
	}
	var probe map[string]any
	if _, err := toml.Decode(string(body), &probe); err != nil {
		return nil, tomlFailure(body, err)
	}
	if !ok {
		return nil, fmt.Errorf("%w: unreadable table header", ErrLayout)
	}

	out, fresh, err := editBody(body, ps, tpath, ed)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(out, body) {
		return data, nil
	}

	if len(body) > 0 && body[len(body)-1] != '\n' {
		out = bytes.TrimSuffix(bytes.TrimSuffix(out, []byte("\n")), []byte("\r"))
	}
	if err := verifyEdit(body, out, tpath, ed, fresh); err != nil {
		return nil, err
	}
	if bytes.HasPrefix(data, bom) {
		out = append(slices.Clone(bom), out...)
	}

	return out, nil
}

// editBody builds the new content of a file whose structure was checked.
//
// Parameters:
//   - body: file content without its byte order mark
//   - ps: statements of the file
//   - tpath: table to edit, nil for the root
//   - ed: edit to apply
//
// Returns the content and whether the file was treated as new because it holds nothing.
func editBody(body []byte, ps []tpiece, tpath []string, ed tableEdit) ([]byte, bool, error) {
	eol := dominantEOL(body)
	if len(bytes.TrimSpace(body)) == 0 {
		if len(ed.Order) == 0 {
			return body, false, nil
		}

		return freshFile(tpath, ed, eol), true, nil
	}

	out, found := applyEdit(ps, tpath, ed, eol)
	if found {
		return out, false, nil
	}
	if len(ed.Order) == 0 {
		return body, false, nil
	}

	return appendTable(body, tpath, ed, eol), false, nil
}

// verifyEdit decodes the new content and checks that only the intended keys changed.
//
// Parameters:
//   - body: original content without its byte order mark
//   - out: new content without its byte order mark
//   - tpath: table edited, nil for the root
//   - ed: edit applied
//   - fresh: whether the schema key was added with a new file
//
// Returns an error wrapping ErrLayout when the new content does not parse or decodes to
// anything but the old content with the edit applied.
func verifyEdit(body, out []byte, tpath []string, ed tableEdit, fresh bool) error {
	want, err := expectedTree(body, tpath, ed, fresh)
	if err != nil {
		if errors.Is(err, ErrLayout) {
			return err
		}

		return fmt.Errorf("%w: %w", ErrLayout, err)
	}

	got := map[string]any{}
	if _, err := toml.Decode(string(out), &got); err != nil {
		return fmt.Errorf("%w: edited text does not parse: %w", ErrLayout, err)
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("%w: edited text does not decode to the intended content", ErrLayout)
	}

	return nil
}
