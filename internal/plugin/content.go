package plugin

import (
	"slices"
	"sort"
	"strings"
)

// declarativeFiles lists the files, besides README* and LICENSE*, that a declarative package may
// hold.
var declarativeFiles = []string{"plugin.toml", "locales/en.toml", "locales/fr.toml"}

// ExecFiles lists the files of a package that get the execute bit: the program of [exec] and the
// programs of the [[platform]] tables, and nothing else.
//
// Parameters:
//   - m: validated manifest
//
// Returns the sorted, unique paths relative to the package root.
func ExecFiles(m *Manifest) []string {
	var out []string

	if m.Exec != nil && m.Exec.Bin != "" {
		out = append(out, m.Exec.Bin)
	}

	for _, b := range m.Builds {
		out = append(out, b.Bin)
	}

	sort.Strings(out)

	return slices.Compact(out)
}

// CheckPackage checks the files of an extracted package against the manifest.
//
// A declarative package may hold plugin.toml, locales/en.toml, locales/fr.toml and files named
// README* or LICENSE* at the root; any other file is declarative_extra_file, so that no program
// travels under the declarative label. An exec package must hold its program: [exec].bin, or the
// bin of the [[platform]] entry for the host (bin_missing).
//
// Parameters:
//   - m: validated manifest
//   - files: slash separated paths of the regular files of the tree, relative to its root
//   - host: running tote, to pick the [[platform]] entry
//
// Returns the issues, sorted by path.
func CheckPackage(m *Manifest, files []string, host Host) []Issue {
	c := &collector{}

	sorted := slices.Clone(files)
	sort.Strings(sorted)

	if m.Tier == TierDeclarative {
		for _, f := range sorted {
			if !declarativeFile(f) {
				c.add(CodeDeclarativeExtra, f, f)
			}
		}

		return c.issues
	}

	for _, want := range requiredBins(m, host) {
		if !slices.Contains(files, want.path) {
			c.add(CodeBinMissing, want.at, want.path)
		}
	}

	return c.issues
}

// CheckExecBits checks the execute bits of a verified tree against the manifest: only the
// programs of [exec] and [[platform]] have one (exec_undeclared otherwise), and the program for
// the host has it (bin_not_executable otherwise). A file that is missing is CheckPackage's
// finding, not this one's.
//
// Parameters:
//   - m: validated manifest
//   - files: regular files of the tree with their exec flag, as hashed
//   - host: running tote, to pick the [[platform]] entry
//
// Returns the issues, in file order.
func CheckExecBits(m *Manifest, files []TreeFile, host Host) []Issue {
	c := &collector{}
	declared := ExecFiles(m)

	for _, tf := range files {
		if tf.Exec && !slices.Contains(declared, tf.Path) {
			c.add(CodeExecUndeclared, tf.Path, tf.Path)
		}
	}

	for _, want := range requiredBins(m, host) {
		for _, tf := range files {
			if tf.Path == want.path && !tf.Exec {
				c.add(CodeBinNotExec, want.at, want.path)
			}
		}
	}

	return c.issues
}

// binRef is a program that a package must hold and the manifest key that names it.
type binRef struct {
	at   string
	path string
}

// requiredBins lists the programs that must exist for the running host.
//
// Parameters:
//   - m: validated manifest
//   - host: running tote
func requiredBins(m *Manifest, host Host) []binRef {
	if m.Exec != nil && m.Exec.Bin != "" {
		return []binRef{{"exec.bin", m.Exec.Bin}}
	}

	for i, b := range m.Builds {
		if b.OS == host.OS && b.Arch == host.Arch {
			return []binRef{{"platform[" + itoa(i) + "].bin", b.Bin}}
		}
	}

	return nil
}

// declarativeFile reports whether a path may appear in a declarative package.
//
// Parameters:
//   - f: slash separated path relative to the package root
func declarativeFile(f string) bool {
	if slices.Contains(declarativeFiles, f) {
		return true
	}

	if strings.Contains(f, "/") {
		return false
	}

	return strings.HasPrefix(f, "README") || strings.HasPrefix(f, "LICENSE")
}
