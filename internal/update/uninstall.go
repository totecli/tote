package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const (
	// maxShellFileSize is the largest shell startup file edited, in bytes.
	maxShellFileSize = 4 << 20

	// accessWrite is the W_OK flag of access(2), which the syscall package does not export.
	accessWrite = 0x2

	// accessSearch is the X_OK flag of access(2), which the syscall package does not export.
	accessSearch = 0x1
)

// ErrUnsafePath reports a directory that tote refuses to purge.
var ErrUnsafePath = errors.New("unsafe path")

// shellInit is the command that prints the shell code of tote, with the optional command word
// and the flags a user may add.
const shellInit = `(?:command )?tote init (?:bash|zsh)(?: --?[A-Za-z][A-Za-z-]*)*`

// shellLoad is one way to run that code: eval of the output, quoted or not, in the $( ) or the
// backtick form, or source of a process substitution.
const shellLoad = `(?:eval (?:"(?:\$\(` + shellInit + `\)|` + "`" + shellInit + "`" + `)"|` +
	`\$\(` + shellInit + `\)|` + "`" + shellInit + "`" + `)|(?:source|\.) <\(` +
	shellInit + `\))`

// shellGuard is the test that a conditional form puts before the load: tote is on the PATH.
const shellGuard = `(?:command -v|type|hash|which) tote(?: +(?:&>|[12]?>)(?:/dev/null|&1))*`

// shellLine matches a whole line whose only job is to load tote, and nothing else: the load by
// itself, after a guard with && or in an if on one line, with an optional ; and an optional
// trailing comment. The line is trimmed first. Anything that does more, such as a command
// chained after the load, is not matched, as deleting the line would delete that too.
var shellLine = regexp.MustCompile(`^(?:if ` + shellGuard + `; then ` + shellLoad + `;? fi|` +
	shellGuard + ` &&  *` + shellLoad + `|` + shellLoad + `)(?:;(?: *#.*)?|(?: +#.*)?)$`)

// blockOpen matches a line that ends by opening a block, which needs a command before it closes.
var blockOpen = regexp.MustCompile(`(?:^|[\s;{])(?:then|else|do|\{)(?:\s+#.*)?\s*$`)

// blockClose matches a line that closes or continues a block.
var blockClose = regexp.MustCompile(`^\s*(?:(?:fi|done|esac|else|elif)\b|\})`)

// shellMention matches a command word that runs "tote init" for bash or zsh anywhere in a line.
var shellMention = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./-])tote init (?:bash|zsh)\b`)

// ownDirName is the name of the directory tote creates below each base directory. A directory
// with another name was chosen by the user with a TOTE_*_DIR variable and may be shared.
const ownDirName = "tote"

// DataDir is one of the directories where tote keeps files, with the names it created in it.
//
// Dir is the absolute path of the directory and Entries the names, files or directories, that
// belong to tote. Purging removes those entries only and then the directory if it is empty:
// anything else found there is the user's and stays.
type DataDir struct {
	Dir     string
	Entries []string
}

// Purged is the outcome of PurgeDir.
//
// Removed lists the entries that existed and were removed, Left the directory entries that
// still hold files and were therefore not removed, Failed the entries that could not be removed
// and Kept is true when the directory itself still holds other files and was therefore left in
// place.
type Purged struct {
	Removed []string
	Left    []string
	Failed  []string
	Kept    bool
}

// Own reports whether the directory is one that tote created, named tote, as opposed to one the
// user chose with a TOTE_*_DIR variable, which may be shared with other programs such as
// ~/.local. Only in an own directory is a directory entry removed with what it holds; elsewhere
// it is removed only when it is empty, so that files of other tools are never deleted.
func (d DataDir) Own() bool {
	return filepath.Base(d.Dir) == ownDirName
}

// Existing lists what a purge would do with the entries that exist.
//
// Returns the full paths that PurgeDir would remove and the full paths of the directory entries
// it would leave because they hold files and the directory is not an own one.
func (d DataDir) Existing() (remove, leave []string) {
	for _, name := range d.Entries {
		p := filepath.Join(d.Dir, name)

		info, err := os.Lstat(p)
		if err != nil {
			continue
		}

		if info.IsDir() && !d.Own() && !emptyDir(p) {
			leave = append(leave, p)

			continue
		}

		remove = append(remove, p)
	}

	return remove, leave
}

// emptyDir reports whether a directory holds nothing.
//
// Parameters:
//   - p: directory path; an unreadable one counts as not empty
func emptyDir(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	_, err = f.Readdirnames(1)

	return errors.Is(err, io.EOF)
}

// CheckDir tells whether PurgeDir would accept a directory, without touching anything.
//
// Parameters:
//   - dir: directory to purge
//   - home: home directory of the user
//
// Returns nil, or an error wrapping ErrUnsafePath.
func CheckDir(dir, home string) error {
	return safeDir(dir, home)
}

// PurgeDir removes the entries tote created in one of its directories.
//
// Each entry is a plain name, never a path; all of them are checked before anything is removed.
// A symbolic link is unlinked and never followed. In a directory named tote a directory entry is
// removed with everything below it, read-only trees such as the installed plugins included,
// which is why a directory that is the root, the home directory or one of its parents is
// refused. In any other directory, one chosen by the user and possibly shared, a directory entry
// is removed only when it is empty.
//
// An entry that cannot be removed does not stop the others: it is listed in Failed and its
// error, with the path, is part of the returned one.
//
// Parameters:
//   - d: directory and entries to remove
//   - home: home directory of the user, which no purged directory may be or contain
//
// Returns what was removed, also when the error is not nil. The error wraps ErrUnsafePath for a
// refused directory or entry.
func PurgeDir(d DataDir, home string) (Purged, error) {
	if err := safeDir(d.Dir, home); err != nil {
		return Purged{}, err
	}

	for _, name := range d.Entries {
		if name == "" || name == "." || name == ".." ||
			strings.ContainsRune(name, filepath.Separator) {
			return Purged{}, fmt.Errorf("%w: entry %q", ErrUnsafePath, name)
		}
	}

	root, err := os.OpenRoot(d.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return Purged{}, nil
	}

	if err != nil {
		return Purged{}, err
	}

	defer func() { _ = root.Close() }()

	var (
		out  Purged
		errs []error
	)

	for _, name := range d.Entries {
		p := filepath.Join(d.Dir, name)

		err := purgeEntry(root, name, p, d.Own(), &out)
		if err != nil {
			out.Failed = append(out.Failed, p)
			errs = append(errs, err)
		}
	}

	if len(errs) == 0 {
		if err := os.Remove(d.Dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			out.Kept = true
		}
	} else {
		out.Kept = true
	}

	return out, errors.Join(errs...)
}

// purgeEntry removes one entry of a directory and records it in the outcome.
//
// Parameters:
//   - root: the directory, opened as a root
//   - name: the entry
//   - p: full path of the entry
//   - own: true for a directory named tote, where a directory entry goes with what it holds
//   - out: outcome to add the entry to
//
// Returns nil when the entry is gone or deliberately left, or the error of what stayed.
func purgeEntry(root *os.Root, name, p string, own bool, out *Purged) error {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("%s: %w", p, unwrapPath(err))
	}

	switch {
	case info.IsDir() && own:
		err = wipeEntry(root, name, p)
	case info.IsDir() && !emptyDir(p):
		out.Left = append(out.Left, p)

		return nil
	default:
		if err = root.Remove(name); err != nil {
			err = fmt.Errorf("%s: %w", p, unwrapPath(err))
		}
	}

	if err != nil {
		return err
	}

	out.Removed = append(out.Removed, p)

	return nil
}

// unwrapPath strips the path that an operation on a root adds to its error, which is relative to
// the root and would only confuse next to the full path.
//
// Parameters:
//   - err: error of an operation
func unwrapPath(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}

	return err
}

// safeDir checks that a directory may be purged.
//
// Parameters:
//   - dir: directory to purge
//   - home: home directory of the user
//
// Returns nil, or an error wrapping ErrUnsafePath when dir is not an absolute cleaned path,
// is the root, is the home directory or one of its parents, or is a symbolic link.
func safeDir(dir, home string) error {
	switch {
	case dir == "" || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir:
		return fmt.Errorf("%w: %q", ErrUnsafePath, dir)
	case filepath.Dir(dir) == dir:
		return fmt.Errorf("%w: the root directory", ErrUnsafePath)
	case home != "" && holds(dir, filepath.Clean(home)):
		return fmt.Errorf("%w: %s holds the home directory", ErrUnsafePath, dir)
	}

	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symbolic link", ErrUnsafePath, dir)
	}

	return nil
}

// holds reports whether a directory is a path or one of the parents of a path.
//
// Parameters:
//   - dir: cleaned directory
//   - p: cleaned path
func holds(dir, p string) bool {
	sep := string(filepath.Separator)

	return strings.HasPrefix(p+sep, dir+sep)
}

// LoadsTote reports whether a line of a startup file only loads tote, which is the line that
// "tote uninstall --shell" deletes.
//
// The forms are eval of "$(tote init bash)" or of the zsh form, quoted or not, with backticks or
// $( ), with or without command before tote and flags after the shell, source <(tote init bash),
// the same behind a guard such as command -v tote &&, or in a one-line if ... then ... fi, with
// an optional trailing ; or comment. A line that does anything more, or that only mentions the
// command, is not one.
//
// Parameters:
//   - line: one line of the file, with or without its line ending
func LoadsTote(line string) bool {
	return shellLine.MatchString(strings.TrimSpace(line))
}

// ShellLines finds the lines of a startup file that load tote.
//
// Parameters:
//   - path: startup file such as ~/.bashrc; a missing file has none
//
// Returns the 1-based number of each line for which LoadsTote is true. A missing file has none;
// an unreadable or oversized file is an error.
func ShellLines(path string) ([]int, error) {
	lines, _, err := readShellFile(path)
	if err != nil {
		return nil, err
	}

	var found []int

	for i, l := range lines {
		if LoadsTote(l) {
			found = append(found, i+1)
		}
	}

	return found, nil
}

// ShellMentions finds the lines of a startup file that run "tote init" for bash or zsh without
// being a line that "tote uninstall --shell" deletes: a load that does something more. They stay
// in the file and print an error at every start once the program is gone, so the user is told
// to remove them by hand. Comment lines are skipped.
//
// Parameters:
//   - path: startup file such as ~/.bashrc; a missing file has none
//
// Returns the 1-based line numbers. A missing file has none; an unreadable or oversized file is
// an error.
func ShellMentions(path string) ([]int, error) {
	lines, _, err := readShellFile(path)
	if err != nil {
		return nil, err
	}

	var found []int

	for i, l := range lines {
		t := strings.TrimSpace(l)

		if !strings.HasPrefix(t, "#") && !LoadsTote(t) && shellMention.MatchString(t) {
			found = append(found, i+1)
		}
	}

	return found, nil
}

// RemoveShellLines deletes the lines that load tote from a startup file.
//
// The lines are those of LoadsTote. The file is rewritten atomically with its mode, and through
// a symbolic link when a dotfiles manager keeps it elsewhere. Nothing else is changed. When no
// line matches the file is not written at all.
//
// Parameters:
//   - path: startup file
//
// Returns the number of lines removed, or an error with the file unchanged.
func RemoveShellLines(path string) (int, error) {
	lines, info, err := readShellFile(path)
	if err != nil || info == nil {
		return 0, err
	}

	kept, removed := dropLoads(lines)
	if removed == 0 {
		return 0, nil
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return 0, err
	}

	return removed, writeFileAtomic(resolved, []byte(strings.Join(kept, "")), info.Mode().Perm())
}

// dropLoads removes the lines that load tote from the lines of a startup file.
//
// A load that is the only command of a block, such as the body of a multi-line if, is replaced
// by a ":" line with the same indentation: an empty block is a syntax error of the shell, and
// the whole file would fail at every start.
//
// Parameters:
//   - lines: lines of the file, each with its line ending
//
// Returns the lines to write and the number of lines removed.
func dropLoads(lines []string) ([]string, int) {
	kept := make([]string, 0, len(lines))
	removed := 0
	last := ""
	pending := ""
	dropped := false

	for _, l := range lines {
		t := strings.TrimSpace(l)

		switch {
		case LoadsTote(l):
			removed++
			dropped = true
			pending = l[:len(l)-len(strings.TrimLeft(l, " \t"))]

			continue
		case t == "" || strings.HasPrefix(t, "#"):
		default:
			if dropped && blockOpen.MatchString(last) && blockClose.MatchString(l) {
				kept = append(kept, pending+":\n")
			}

			dropped = false
			last = t
		}

		kept = append(kept, l)
	}

	return kept, removed
}

// readShellFile reads a startup file as lines that keep their line ending.
//
// Parameters:
//   - path: startup file
//
// Returns the lines and the file information, or nil information for a missing file. The file
// must be regular and at most maxShellFileSize bytes.
func readShellFile(path string) ([]string, os.FileInfo, error) {
	// The type is checked before opening, as opening a pipe would block for ever.
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}

	if err != nil {
		return nil, nil, err
	}

	if !info.Mode().IsRegular() || info.Size() > maxShellFileSize {
		return nil, nil, fmt.Errorf("%s is not a regular file of a usual size", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxShellFileSize+1))
	if err != nil {
		return nil, nil, err
	}

	return strings.SplitAfter(string(data), "\n"), info, nil
}

// CanRemove checks that the directory of a file accepts the removal of that file.
//
// Parameters:
//   - path: file to remove
//
// Returns nil, or an error wrapping ErrNotWritable. The check is made with access(2) on the
// directory, which is what unlink needs.
func CanRemove(path string) error {
	dir := filepath.Dir(path)

	if err := syscall.Access(dir, accessWrite|accessSearch); err != nil {
		return &NotWritableError{Dir: dir, Err: err}
	}

	return nil
}
