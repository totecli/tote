package update

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// receiptSchema is the only receipt format this version reads and writes. The schema stays 1: a
// later release only adds optional fields, and a binary that does not know them keeps them when
// it rewrites the file, see WriteReceipt.
const receiptSchema = 1

// maxReceiptSize is the largest receipt read, in bytes.
const maxReceiptSize = 16 << 10

// ErrReceipt reports an install receipt that cannot be used.
var ErrReceipt = errors.New("invalid install receipt")

// Receipt is the install receipt written by install.sh and by tote update, kept in the state
// directory as install.json.
//
// InstallMethod is "script" for the installer, "apt", "brew" or "manual". ReleaseTimestamp is
// the signed build time of the installed release and guards against a rollback. The other
// fields describe the install: version, channel, absolute binary path, system, release source,
// the tool that verified the signature and the install time in RFC 3339.
type Receipt struct {
	Schema           int    `json:"schema"`
	InstallMethod    string `json:"install_method"`
	Version          string `json:"version"`
	Channel          string `json:"channel"`
	Path             string `json:"path"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	Source           string `json:"source"`
	VerifiedBy       string `json:"verified_by"`
	ReleaseTimestamp int64  `json:"release_timestamp"`
	InstalledAt      string `json:"installed_at"`
}

// ReadReceipt reads an install receipt.
//
// Parameters:
//   - path: location of install.json
//
// Returns the receipt, an error wrapping os.ErrNotExist when there is none, or an error
// wrapping ErrReceipt when the file is not a regular file of a sane size, is not JSON or has
// an unknown schema.
func ReadReceipt(path string) (Receipt, error) {
	// The type is checked before opening, as opening a pipe would block for ever.
	info, err := os.Stat(path)
	if err != nil {
		return Receipt{}, err
	}

	if !info.Mode().IsRegular() {
		return Receipt{}, fmt.Errorf("%w: %s is not a regular file", ErrReceipt, path)
	}

	f, err := os.Open(path)
	if err != nil {
		return Receipt{}, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxReceiptSize+1))
	if err != nil {
		return Receipt{}, err
	}

	if len(data) > maxReceiptSize {
		return Receipt{}, fmt.Errorf("%w: %s is too large", ErrReceipt, path)
	}

	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return Receipt{}, fmt.Errorf("%w: %s: %v", ErrReceipt, path, err)
	}

	if r.Schema != receiptSchema {
		return Receipt{}, fmt.Errorf("%w: schema %d", ErrReceipt, r.Schema)
	}

	return r, nil
}

// WriteReceipt writes an install receipt atomically, with mode 0600 in a 0700 directory.
//
// The fields of the receipt that is already there and that this version does not know are kept,
// whatever their value: the file is read, the known fields are overwritten and the others are
// written back, after them and in alphabetical order. A release that adds an optional field so
// never loses it to a self-update by an older binary. An existing file that cannot be read, is
// not a JSON object or has another schema is replaced.
//
// Parameters:
//   - path: location of install.json; its directory is created when missing
//   - r: receipt to store; the schema is set by the call
//
// Returns an error when the receipt cannot be written, in which case the old one is intact.
func WriteReceipt(path string, r Receipt) error {
	r.Schema = receiptSchema

	// A receipt kept elsewhere by a dotfiles manager is written through its link.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	data, err := mergeReceipt(path, r)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

// mergeReceipt renders a receipt as indented JSON together with the unknown fields of the file
// that is already at a path.
//
// Parameters:
//   - path: location of the current receipt, which may not exist
//   - r: receipt to render
//
// Returns the known fields in the order of the struct, then the other fields sorted by name.
func mergeReceipt(path string, r Receipt) ([]byte, error) {
	own, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(own, &fields); err != nil {
		return nil, err
	}

	// The names in the order of the struct.
	dec := json.NewDecoder(bytes.NewReader(own))
	_, _ = dec.Token()

	var names []string

	for dec.More() {
		tok, _ := dec.Token()
		name, _ := tok.(string)
		names = append(names, name)

		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}

	old := currentFields(path)

	var extra []string

	for name := range old {
		if _, known := fields[name]; !known {
			extra = append(extra, name)
		}
	}

	sort.Strings(extra)

	var out bytes.Buffer

	out.WriteByte('{')

	for i, name := range append(names, extra...) {
		if i > 0 {
			out.WriteByte(',')
		}

		key, _ := json.Marshal(name)
		out.Write(key)
		out.WriteByte(':')

		if v, ok := fields[name]; ok {
			out.Write(v)
		} else {
			out.Write(old[name])
		}
	}

	out.WriteByte('}')

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, out.Bytes(), "", "    "); err != nil {
		return nil, err
	}

	return pretty.Bytes(), nil
}

// currentFields reads the fields of the receipt that is at a path, as raw JSON.
//
// Parameters:
//   - path: location of install.json
//
// Returns nil when the file is missing, is not a regular file of a sane size, is not a JSON
// object or does not have the schema of this version.
func currentFields(path string) map[string]json.RawMessage {
	if _, err := ReadReceipt(path); err != nil {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil
	}

	return fields
}

// Describes reports whether the receipt is the receipt of a given binary.
//
// Parameters:
//   - exe: path of the binary, symbolic links resolved
//
// Returns true when the receipt has no path or its path leads to exe.
func (r Receipt) Describes(exe string) bool {
	return r.Path == "" || samePath(r.Path, exe)
}

// samePath reports whether two paths lead to the same file name once links are resolved.
//
// Parameters:
//   - a: first path
//   - b: second path
func samePath(a, b string) bool {
	return resolve(a) == resolve(b)
}

// resolve returns a cleaned path with its links resolved when the file exists.
//
// Parameters:
//   - p: path to resolve
func resolve(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}

	return filepath.Clean(p)
}

// formatTime formats a time as the receipt does.
//
// Parameters:
//   - t: time to format
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
