// Package bundled embeds the official plugins that ship inside the tote binary, with the pin of
// each one. It imports the standard library only, so that the plugin package can stay unaware of
// it: Set satisfies plugin.BundledSet by its method set.
//
// The embedded files are never run from here. A bundled plugin is installed by extraction into
// the plugins directory and verified there like any other, against the pin.
package bundled
