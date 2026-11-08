package bundled

import (
	"embed"
	"io/fs"
	"sync"
)

// files holds the tree of each bundled plugin below plugins/<id>. A new plugin is a directory
// there and a block in pins.toml, with no edit of Go code; TestBundledIDs fails when only one of
// the two exists.
//
//go:embed all:plugins
var files embed.FS

// treeDir is the directory of files that holds one directory per bundled plugin.
const treeDir = "plugins/"

// pinsText is the content of pins.toml.
//
//go:embed pins.toml
var pinsText string

// loaded caches the parsed pins, which cannot change while the program runs.
var loaded struct {
	once sync.Once
	pins []Pin
	err  error
}

// Set is the set of plugins shipped inside the tote binary. The zero value is ready to use.
type Set struct{}

// Pins returns the pins of the bundled plugins in the order of pins.toml.
//
// Returns the pins, or the error of the first malformed line of pins.toml, which only a broken
// build can have.
func Pins() ([]Pin, error) {
	loaded.once.Do(func() {
		loaded.pins, loaded.err = parsePins(pinsText)
	})

	return append([]Pin(nil), loaded.pins...), loaded.err
}

// Lookup returns the embedded files of a plugin and its compiled-in pin.
//
// Parameters:
//   - id: plugin id
//
// Returns the files of the plugin with plugin.toml at the root of the returned file system, the
// version, the tree_sha256 and the surface_sha256 that the files must have once installed, and
// true; or false for an id that is not bundled or when pins.toml does not parse.
func (Set) Lookup(id string) (fsys fs.FS, version, tree, surface string, ok bool) {
	pins, err := Pins()
	if err != nil {
		return nil, "", "", "", false
	}

	return lookup(files, pins, id)
}

// lookup finds a plugin in a file system that holds one directory per plugin below treeDir.
//
// Parameters:
//   - all: file system with the trees of the plugins
//   - pins: pins of the plugins
//   - id: plugin id
//
// Returns the same values as Set.Lookup.
func lookup(all fs.FS, pins []Pin, id string) (fsys fs.FS, version, tree, surface string, ok bool) {
	for _, p := range pins {
		if p.ID != id {
			continue
		}

		sub, err := fs.Sub(all, treeDir+id)
		if err != nil {
			return nil, "", "", "", false
		}

		return sub, p.Version, p.Tree, p.Surface, true
	}

	return nil, "", "", "", false
}

// IDs lists the ids of the bundled plugins in the order of pins.toml.
func (Set) IDs() []string {
	pins, _ := Pins()

	ids := make([]string, len(pins))
	for i, p := range pins {
		ids[i] = p.ID
	}

	return ids
}
