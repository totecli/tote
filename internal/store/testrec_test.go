package store

import (
	"errors"
	"strings"
)

// testTable is the array of tables name used by the engine tests.
const testTable = "rec"

// testRec is the small record type of the engine tests.
//
// Name is the key and must be non-empty without spaces. Desc and Tags are written only when not
// empty. Lost is never written, so a record that sets it cannot round-trip through a file, which
// lets the tests exercise the verification step.
type testRec struct {
	Name string   `toml:"name"`
	Desc string   `toml:"desc,omitempty"`
	Tags []string `toml:"tags,omitempty"`
	Lost string   `toml:"-"`
}

// Key implements Record.
func (r testRec) Key() string {
	return r.Name
}

// Validate implements Record.
func (r testRec) Validate() error {
	if r.Name == "" {
		return errors.New("name is empty")
	}
	if strings.ContainsAny(r.Name, " \t") {
		return errors.New("name contains a space")
	}

	return nil
}
