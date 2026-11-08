package plugin

import "io/fs"

// BundledSet is the set of plugins shipped inside the tote binary. The registry installs one of
// them by extraction and never runs it from the embedded files.
//
// Lookup returns the embedded files of a plugin and its compiled-in pin: the version and the
// tree_sha256 and surface_sha256 that the files must have; ok is false for an unknown id. IDs
// lists the ids of the embedded plugins. The methods use standard library types only, so that the
// sub-package that embeds the plugins, which may import nothing else, satisfies the interface
// without importing this package.
type BundledSet interface {
	Lookup(id string) (files fs.FS, version, tree, surface string, ok bool)
	IDs() []string
}
