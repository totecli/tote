package plugin

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// binNameRe matches the name of a program a plugin requires or declares: a name, never a path.
var binNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// Avoid lists where a program lookup must not land. Under holds directories whose whole subtree
// is refused, such as the plugins directory: a plugin could have planted the program there.
// Exactly holds directories whose own files are refused but whose subdirectories are not, such as
// the working directory: a repository may carry a program called git, yet a user whose working
// directory is their home still finds the programs in ~/.local/bin.
type Avoid struct {
	Under   []string
	Exactly []string
}

// refuses reports whether a program at a path is refused: the path lies inside an Under directory
// or its own directory is one of the Exactly directories.
//
// Parameters:
//   - path: absolute path of the program, cleaned
func (a Avoid) refuses(path string) bool {
	return underAny(path, a.Under) || slices.Contains(a.Exactly, filepath.Dir(path))
}

// LookBin finds a program on a path list the way a plugin may rely on: by name only, in the
// absolute directories of the list, as a regular file with an execute bit. It runs nothing.
//
// An empty entry and a relative entry (".", "bin") are ignored, so the working directory is never
// searched through a relative name. A candidate that lies where Avoid says is skipped, and so is a
// candidate whose real path, after the links are followed, does: a program that a plugin could
// have planted is never the answer.
//
// Parameters:
//   - pathEnv: value of PATH
//   - name: program name such as gft; a name with a slash or outside the grammar is never found
//   - avoid: places no result may lie in; paths are cleaned here, empty ones are ignored
//
// Returns the absolute path of the first match and true, or an empty string and false.
func LookBin(pathEnv, name string, avoid Avoid) (string, bool) {
	if !binNameRe.MatchString(name) {
		return "", false
	}

	avoid = avoid.cleaned()

	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}

		path := filepath.Join(filepath.Clean(dir), name)
		if avoid.refuses(path) {
			continue
		}

		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}

		if resolved, rerr := filepath.EvalSymlinks(path); rerr != nil || avoid.refuses(resolved) {
			continue
		}

		return path, true
	}

	return "", false
}

// cleaned returns the places with empty and relative entries dropped and the others cleaned.
func (a Avoid) cleaned() Avoid {
	clean := func(list []string) []string {
		var out []string

		for _, d := range list {
			if d != "" && filepath.IsAbs(d) {
				out = append(out, filepath.Clean(d))
			}
		}

		return out
	}

	return Avoid{Under: clean(a.Under), Exactly: clean(a.Exactly)}
}

// underAny reports whether a path is one of the directories or lies inside one of them.
//
// Parameters:
//   - path: absolute path, cleaned
//   - dirs: absolute directories, cleaned
func underAny(path string, dirs []string) bool {
	for _, d := range dirs {
		rel, err := filepath.Rel(d, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}

	return false
}
