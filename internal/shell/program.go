package shell

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/totecli/tote/internal/store"
)

// LookProgram finds a program the way tote does when it must not run anything: in a PATH value
// that it is given, never in the environment of the process, and without a shell.
//
// Parameters:
//   - pathVar: value of PATH, directories separated by the list separator of the system
//   - name: the program; a name that holds a slash is not searched for in PATH and is checked as
//     it stands, which is what the system does when it starts it
//
// Returns the full name and true for the first executable regular file found in an absolute
// directory of pathVar; relative entries, the current directory included, are skipped so that a
// program in a project is never taken for a tool. It returns an empty string and false when there
// is none or when name is empty.
func LookProgram(pathVar, name string) (string, bool) {
	if name == "" {
		return "", false
	}

	if strings.Contains(name, "/") {
		if runnable(name) {
			return name, true
		}

		return "", false
	}

	for _, dir := range filepath.SplitList(pathVar) {
		if !filepath.IsAbs(dir) {
			continue
		}

		full := filepath.Join(dir, name)
		if runnable(full) {
			return full, true
		}
	}

	return "", false
}

// runnable reports whether a path is a regular file that someone can execute.
//
// Parameters:
//   - path: file to look at, symbolic links followed
func runnable(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// ViaProgram names the program that a connection starts when it does not use plain ssh.
//
// Parameters:
//   - c: connection to look at; only its via is read
//
// Returns the first word of the via, which is the program for a verb and for a template alike,
// or an empty string when the via is empty or "ssh", when it has no word, or when that word holds
// a placeholder and so depends on the record.
func ViaProgram(c store.Connection) string {
	if c.Via == "" || c.Via == plainSSH {
		return ""
	}

	words := strings.Fields(c.Via)
	if len(words) == 0 || strings.ContainsAny(words[0], "{}") {
		return ""
	}

	return words[0]
}

// IsMissingProgram tells whether the error of starting a program means that the program cannot be
// found or cannot be executed, as opposed to a program that ran and failed.
//
// Parameters:
//   - err: error returned when the program was started, nil for none
//
// Returns true for a program that is not in PATH (exec.ErrNotFound), a path that does not exist
// and a file that is not executable, whether the error comes from a search in PATH or from the
// system.
func IsMissingProgram(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, fs.ErrPermission)
}
