package bundled

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Pin is the compiled-in identity of a bundled plugin: its version and the two hashes that the
// installed copy must have. Tree is the tree_sha256 of the installed files, Surface the
// surface_sha256 of the commands they run.
type Pin struct {
	ID      string
	Version string
	Tree    string
	Surface string
}

// Patterns of the values of a pin.
var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)
	hashRe    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	versionRe = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
		`(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
)

// maxPins bounds the file, which holds one block per official plugin.
const maxPins = 16

// pinReader is the state of one read of pins.toml: the pins finished so far, the block being read,
// the keys already met in it, and whether the schema line was seen.
type pinReader struct {
	pins   []Pin
	cur    *Pin
	seen   map[string]bool
	schema bool
}

// parsePins reads pins.toml with the small subset of TOML the file uses: comments, the line
// schema = 1, and [[plugin]] blocks of four double-quoted strings. The standard library has no
// TOML reader and this package may import nothing else, so the reader is strict: anything it does
// not know is an error, and a pin that fails to parse can never be taken for a pin that holds.
//
// Parameters:
//   - text: content of pins.toml
//
// Returns the pins in file order, or an error naming the line.
func parsePins(text string) ([]Pin, error) {
	rd := &pinReader{}
	lines := strings.Split(text, "\n")

	for i, raw := range lines {
		if err := rd.line(strings.TrimSpace(raw)); err != nil {
			return nil, fmt.Errorf("pins.toml:%d: %w", i+1, err)
		}
	}

	if err := rd.finish(); err != nil {
		return nil, fmt.Errorf("pins.toml:%d: %w", len(lines), err)
	}

	if !rd.schema {
		return nil, fmt.Errorf("pins.toml: no schema line")
	}

	return rd.pins, checkUnique(rd.pins)
}

// line reads one trimmed line.
//
// Parameters:
//   - line: the line without its surrounding blanks
func (rd *pinReader) line(line string) error {
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}

	if line == "[[plugin]]" {
		if err := rd.finish(); err != nil {
			return err
		}

		if !rd.schema || len(rd.pins) >= maxPins {
			return fmt.Errorf("unexpected block")
		}

		rd.cur, rd.seen = &Pin{}, map[string]bool{}

		return nil
	}

	key, val, quoted, err := keyValue(line)
	if err != nil {
		return err
	}

	if rd.cur != nil {
		if !quoted {
			return fmt.Errorf("%s: not a string", key)
		}

		return setField(rd.cur, rd.seen, key, val)
	}

	if key != "schema" || val != "1" || quoted || rd.schema {
		return fmt.Errorf("expected schema = 1")
	}

	rd.schema = true

	return nil
}

// finish checks and stores the block being read, if there is one.
func (rd *pinReader) finish() error {
	if rd.cur == nil {
		return nil
	}

	if err := checkPin(*rd.cur); err != nil {
		return err
	}

	rd.pins = append(rd.pins, *rd.cur)
	rd.cur = nil

	return nil
}

// keyValue splits a line "key = value". A string value is returned without its quotes and quoted
// is true; any other value is returned as written.
//
// Parameters:
//   - line: trimmed line
func keyValue(line string) (key, val string, quoted bool, err error) {
	key, val, ok := strings.Cut(line, "=")
	key, val = strings.TrimSpace(key), strings.TrimSpace(val)

	if !ok || key == "" || val == "" {
		return "", "", false, fmt.Errorf("not a key = value line")
	}

	if strings.HasPrefix(val, `"`) {
		s, err := strconv.Unquote(val)
		if err != nil || strings.ContainsAny(val, `\`) {
			return "", "", false, fmt.Errorf("%s: not a plain string", key)
		}

		return key, s, true, nil
	}

	return key, val, false, nil
}

// setField stores one key of a block.
//
// Parameters:
//   - p: pin being read
//   - seen: keys already met in this block
//   - key: key of the line
//   - val: value of the line
func setField(p *Pin, seen map[string]bool, key, val string) error {
	if seen[key] {
		return fmt.Errorf("%s: duplicate key", key)
	}

	seen[key] = true

	switch key {
	case "id":
		p.ID = val
	case "version":
		p.Version = val
	case "tree_sha256":
		p.Tree = val
	case "surface_sha256":
		p.Surface = val
	default:
		return fmt.Errorf("%s: unknown key", key)
	}

	return nil
}

// checkPin checks that a block is complete and that every value has the form it must have.
//
// Parameters:
//   - p: pin read from one block
func checkPin(p Pin) error {
	switch {
	case !idRe.MatchString(p.ID):
		return fmt.Errorf("id %q is not a plugin id", p.ID)
	case !versionRe.MatchString(p.Version):
		return fmt.Errorf("%s: version %q is not a semantic version", p.ID, p.Version)
	case !hashRe.MatchString(p.Tree):
		return fmt.Errorf("%s: tree_sha256 is not 64 lower case hex digits", p.ID)
	case !hashRe.MatchString(p.Surface):
		return fmt.Errorf("%s: surface_sha256 is not 64 lower case hex digits", p.ID)
	}

	return nil
}

// checkUnique refuses two blocks for one id.
//
// Parameters:
//   - pins: pins in file order
func checkUnique(pins []Pin) error {
	seen := map[string]bool{}

	for _, p := range pins {
		if seen[p.ID] {
			return fmt.Errorf("pins.toml: %s appears twice", p.ID)
		}

		seen[p.ID] = true
	}

	return nil
}
