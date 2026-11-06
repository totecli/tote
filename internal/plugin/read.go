package plugin

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// deprecatedKeys maps the index-free path of a manifest key (table.key, or key at the root) to the
// text of its replacement. A deprecated key stays accepted for two minor releases of tote with a
// warning, then becomes an error. API 1 of tote 1.0 deprecates nothing, so the table is empty.
var deprecatedKeys = map[string]string{}

// node is a table of the decoded manifest together with the bookkeeping of strict reading: the
// keys that were read, so that the others can be reported as unknown.
//
// kind is the index-free path of the table, used to find deprecated keys; path is the indexed
// path used in issues.
type node struct {
	c    *collector
	kind string
	path string
	m    map[string]any
	seen map[string]bool
}

// newNode wraps a decoded table.
//
// Parameters:
//   - c: collector that receives the issues
//   - kind: index-free path of the table, empty for the root
//   - path: indexed path of the table, empty for the root
//   - m: decoded table
func newNode(c *collector, kind, path string, m map[string]any) *node {
	return &node{c: c, kind: kind, path: path, m: m, seen: map[string]bool{}}
}

// at returns the issue path of a key of this table.
//
// Parameters:
//   - key: key name
func (n *node) at(key string) string { return joinKey(n.path, key) }

// joinKey joins a table path and a key.
//
// Parameters:
//   - path: path of the table, empty for the root
//   - key: key name
func joinKey(path, key string) string {
	if path == "" {
		return key
	}

	return path + "." + key
}

// raw returns the value of a key and marks it as read.
//
// Parameters:
//   - key: key name
func (n *node) raw(key string) (any, bool) {
	v, ok := n.m[key]
	if ok {
		n.seen[key] = true
	}

	return v, ok
}

// has reports whether a key is present, without marking it as read.
//
// Parameters:
//   - key: key name
func (n *node) has(key string) bool {
	_, ok := n.m[key]

	return ok
}

// typeName names the TOML type of a decoded value for an issue.
//
// Parameters:
//   - v: decoded value
func typeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case int64:
		return "integer"
	case float64:
		return "float"
	case bool:
		return "boolean"
	case time.Time:
		return "datetime"
	case []any, []map[string]any:
		return "array"
	case map[string]any:
		return "table"
	}

	return "unknown"
}

// strOpt sets the checks that str applies to a string value: whether the key is required, whether
// an empty string is refused, whether a string with nothing but spaces is refused too, and the
// limits in runes and in bytes, 0 for none.
type strOpt struct {
	required bool
	nonEmpty bool
	visible  bool
	runes    int
	bytes    int
}

// str reads a string value.
//
// Parameters:
//   - key: key name
//   - o: checks to apply
//
// Returns the value and true when the key holds a valid string. A missing required key gives
// key_missing, another type gives type_invalid, a control or format character gives
// text_invalid and a value over a limit gives limit_exceeded; the value is then still returned
// with true only when it is usable, so the callers never see an unchecked string.
func (n *node) str(key string, o strOpt) (string, bool) {
	v, ok := n.raw(key)
	if !ok {
		if o.required {
			n.c.add(CodeKeyMissing, n.at(key))
		}

		return "", false
	}

	return n.checkStr(n.at(key), v, o)
}

// checkStr applies the checks of str to a value.
//
// Parameters:
//   - path: issue path of the value
//   - v: decoded value
//   - o: checks to apply
func (n *node) checkStr(path string, v any, o strOpt) (string, bool) {
	s, isStr := v.(string)
	if !isStr {
		n.c.add(CodeTypeInvalid, path, "string", typeName(v))

		return "", false
	}

	if !shownText(s) {
		n.c.add(CodeTextInvalid, path)

		return "", false
	}

	if (o.nonEmpty && s == "") || (o.visible && strings.TrimSpace(s) == "") {
		n.c.add(CodeValueInvalid, path, "empty")

		return "", false
	}

	if o.runes > 0 && utf8.RuneCountInString(s) > o.runes {
		n.c.add(CodeLimitExceeded, path, "runes", itoa(o.runes))

		return "", false
	}

	if o.bytes > 0 && len(s) > o.bytes {
		n.c.add(CodeLimitExceeded, path, "bytes", itoa(o.bytes))

		return "", false
	}

	return s, true
}

// listOpt sets the checks that strs applies to an array of strings: the longest array, 0 for no
// limit, and the checks of every element.
type listOpt struct {
	limit int
	item  strOpt
}

// strs reads an array of strings.
//
// Parameters:
//   - key: key name
//   - o: checks to apply
//
// Returns the values and true when the key holds an array whose elements all pass. An array over
// the limit gives limit_exceeded and no value.
func (n *node) strs(key string, o listOpt) ([]string, bool) {
	v, ok := n.raw(key)
	if !ok {
		return nil, false
	}

	arr, isArr := v.([]any)
	if !isArr {
		n.c.add(CodeTypeInvalid, n.at(key), "array", typeName(v))

		return nil, false
	}

	if o.limit > 0 && len(arr) > o.limit {
		n.c.add(CodeLimitExceeded, n.at(key), "count", itoa(o.limit))

		return nil, false
	}

	out := make([]string, 0, len(arr))
	good := true

	for i, e := range arr {
		s, ok := n.checkStr(n.at(key)+"["+itoa(i)+"]", e, o.item)
		if !ok {
			good = false

			continue
		}

		out = append(out, s)
	}

	return out, good
}

// boolean reads a boolean value.
//
// Parameters:
//   - key: key name
//
// Returns the value and true when the key holds a boolean; another type gives type_invalid.
func (n *node) boolean(key string) (val, ok bool) {
	v, present := n.raw(key)
	if !present {
		return false, false
	}

	b, isBool := v.(bool)
	if !isBool {
		n.c.add(CodeTypeInvalid, n.at(key), "boolean", typeName(v))

		return false, false
	}

	return b, true
}

// integer reads an integer value within bounds.
//
// Parameters:
//   - key: key name
//   - lo: smallest accepted value
//   - hi: largest accepted value
//
// Returns the value and true when the key holds an integer in [lo, hi]; another type gives
// type_invalid and a value outside the bounds gives limit_exceeded.
func (n *node) integer(key string, lo, hi int64) (int64, bool) {
	v, present := n.raw(key)
	if !present {
		return 0, false
	}

	i, isInt := v.(int64)
	if !isInt {
		n.c.add(CodeTypeInvalid, n.at(key), "integer", typeName(v))

		return 0, false
	}

	if i < lo || i > hi {
		n.c.add(CodeLimitExceeded, n.at(key), "range", itoa64(lo), itoa64(hi))

		return 0, false
	}

	return i, true
}

// table reads a sub-table.
//
// Parameters:
//   - key: key name
//
// Returns the table and true when the key holds a table; another type gives type_invalid.
func (n *node) table(key string) (*node, bool) {
	v, present := n.raw(key)
	if !present {
		return nil, false
	}

	m, isMap := v.(map[string]any)
	if !isMap {
		n.c.add(CodeTypeInvalid, n.at(key), "table", typeName(v))

		return nil, false
	}

	return newNode(n.c, joinKey(n.kind, key), n.at(key), m), true
}

// tables reads an array of tables.
//
// Parameters:
//   - key: key name
//
// Returns one node per table, in order; a value that is not an array of tables gives
// type_invalid and no node.
func (n *node) tables(key string) []*node {
	v, present := n.raw(key)
	if !present {
		return nil
	}

	var maps []map[string]any

	switch arr := v.(type) {
	case []map[string]any:
		maps = arr
	case []any:
		for _, e := range arr {
			m, ok := e.(map[string]any)
			if !ok {
				n.c.add(CodeTypeInvalid, n.at(key), "array of tables", typeName(e))

				return nil
			}

			maps = append(maps, m)
		}
	default:
		n.c.add(CodeTypeInvalid, n.at(key), "array of tables", typeName(v))

		return nil
	}

	out := make([]*node, len(maps))
	for i, m := range maps {
		out[i] = newNode(n.c, joinKey(n.kind, key), n.at(key)+"["+itoa(i)+"]", m)
	}

	return out
}

// reserve reports every listed key that is present as reserved_not_available and marks it as
// read, so that it is not also reported as unknown.
//
// Parameters:
//   - keys: key names reserved for a later release
func (n *node) reserve(keys ...string) {
	for _, k := range keys {
		if _, ok := n.raw(k); ok {
			n.c.add(CodeReserved, n.at(k), k)
		}
	}
}

// finish reports the keys that nothing read: a deprecated key as a warning that names its
// replacement, any other as unknown_key. All of them are listed, in byte order.
func (n *node) finish() {
	for _, k := range sortedKeys(n.m) {
		if n.seen[k] {
			continue
		}

		if repl, ok := deprecatedKeys[joinKey(n.kind, k)]; ok {
			n.c.warn(CodeDeprecatedKey, n.at(k), k, repl)

			continue
		}

		n.c.add(CodeUnknownKey, n.at(k), k)
	}
}

// itoa formats an int for an issue argument.
//
// Parameters:
//   - i: number to format
func itoa(i int) string { return strconv.Itoa(i) }

// itoa64 formats an int64 for an issue argument.
//
// Parameters:
//   - i: number to format
func itoa64(i int64) string { return strconv.FormatInt(i, 10) }
