package config

import "path/filepath"

// OwnedDir is one of the directories where tote keeps files, with the names it creates there.
//
// Dir is the absolute path of the directory and Entries the names, files or directories, that
// belong to tote. Every other file found in Dir is the user's.
type OwnedDir struct {
	Dir     string
	Entries []string
}

// updateCheckName is the name of the file that remembers the last update check, in the cache
// directory. internal/update owns the value, update.CacheFile, and a test of cmd/tote compares
// the two.
const updateCheckName = "update-check.json"

// reservedFiles lists the data files of the planned releases: snippets, tunnels and port labels.
// They are purged from the first release on, so that a later one never leaves a file behind that
// an older uninstall did not know. PurgeDir only touches entries that exist.
var reservedFiles = []string{"snippets.toml", "tunnels.toml", "ports.toml"}

// Owned lists everything tote creates in its four directories, for "tote uninstall --purge".
//
// The list is written by hand, so a test walks the accessors of Paths and fails when one leads
// to a name that is not covered. Each data file comes with the ".lock" file that the store
// creates next to it and never removes. Directories are listed by name and removed with what
// they hold, which is why the plugins, their state, their cache and the host files of the
// plugins are single entries.
//
// Returns the config, data, state and cache directories with their entries.
func (p Paths) Owned() []OwnedDir {
	var conf []string

	files := []string{
		p.ConfigFile(), p.ConnectionsFile(), p.ReposFile(), p.AliasesFile(), p.FuncsFile(),
	}

	for _, f := range files {
		conf = append(conf, filepath.Base(f), filepath.Base(f)+".lock")
	}

	for _, name := range reservedFiles {
		conf = append(conf, name, name+".lock")
	}

	conf = append(conf, filepath.Base(filepath.Join(p.Config, "plugins")))

	lock := filepath.Base(p.LockFile())

	return []OwnedDir{
		{Dir: p.Config, Entries: conf},
		{Dir: p.Data, Entries: []string{filepath.Base(p.PluginsDir()), filepath.Base(p.BinDir())}},
		{Dir: p.State, Entries: []string{
			filepath.Base(p.ReceiptFile()), lock, lock + ".lock", filepath.Base(p.ProcsFile()),
			"plugins", "plugins-host",
		}},
		{Dir: p.Cache, Entries: []string{updateCheckName, "plugins"}},
	}
}
