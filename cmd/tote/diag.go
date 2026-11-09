package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/store"
	"github.com/totecli/tote/internal/ui"
	"github.com/totecli/tote/internal/update"
)

// level is how serious a finding of "tote doctor" is.
type level int

const (
	// levelOK is a check that passed.
	levelOK level = iota

	// levelWarn is a problem that leaves tote working, possibly with less than it could.
	levelWarn

	// levelFail is a problem that stops a part of tote from working.
	levelFail
)

// String returns the stable name of the level used in the JSON report.
func (l level) String() string {
	switch l {
	case levelWarn:
		return "warn"
	case levelFail:
		return "fail"
	}

	return "ok"
}

// msg is a message of the catalog with its values, kept untranslated until it is shown.
//
// A value that is itself a msg is translated first, which lets a message name the kind of file
// or directory it is about. A string value is sanitized when the message is shown.
type msg struct {
	Key  string
	Args []any
}

// finding is the result of one check.
//
// ID is the stable name of the check, Level its outcome, Msg the one line reason, Fix the hint
// that says what to do, empty when there is nothing to do, and Details the further lines.
type finding struct {
	ID      string
	Level   level
	Msg     msg
	Fix     msg
	Details []msg
}

// note builds a finding.
//
// Parameters:
//   - id: stable name of the check
//   - l: outcome
//   - key: catalog key of the message
//   - args: values for the Printf directives of the message
func note(id string, l level, key string, args ...any) finding {
	return finding{ID: id, Level: l, Msg: msg{Key: key, Args: args}}
}

// fix returns the finding with a hint.
//
// Parameters:
//   - key: catalog key of the hint
//   - args: values for the Printf directives of the hint
func (f finding) fix(key string, args ...any) finding {
	f.Fix = msg{Key: key, Args: args}

	return f
}

// detail returns the finding with one more detail line.
//
// Parameters:
//   - key: catalog key of the line
//   - args: values for the Printf directives of the line
func (f finding) detail(key string, args ...any) finding {
	f.Details = append(slices.Clone(f.Details), msg{Key: key, Args: args})

	return f
}

// combine merges the results of the several tests of one check.
//
// Parameters:
//   - id: stable name of the check
//   - parts: results of the tests, at least one
//
// Returns the most serious part, the first one on a tie, with the message of every other part
// that is not OK added as a detail and the hint of the first part that has one when the most
// serious has none.
func combine(id string, parts ...finding) finding {
	if len(parts) == 0 {
		return finding{ID: id}
	}

	head := 0
	out := parts[0]

	for i, p := range parts {
		if p.Level > out.Level {
			head, out = i, p
		}
	}

	out.ID = id
	out.Details = slices.Clone(out.Details)

	for i, p := range parts {
		if i == head || p.Level == levelOK {
			continue
		}

		out.Details = append(out.Details, p.Msg)

		if out.Fix.Key == "" {
			out.Fix = p.Fix
		}
	}

	return out
}

// diagEnv is everything the checks read from the machine, so that they run on a fixture.
//
// Getenv and Environ are the environment and Home the home directory, from which every path
// of tote is derived; the tests give a temporary directory for it. Version is the release
// string, GOOS and GOARCH the platform, Exe the path of the running program with links resolved
// and ExeErr why it is unknown. Now is the clock, UID the user id the files must belong to,
// Keys the release keys of the build and LookPath finds a program in PATH. Marker is the
// package marker file. TmuxClip gives the value of the tmux option set-clipboard, nil for the real
// query. Term is what the interface would choose for the terminal, Interactive
// tells whether the output is a terminal, and Width and Height are its size when SizeOK is true.
type diagEnv struct {
	Getenv      func(string) string
	Environ     []string
	Home        string
	Version     string
	GOOS        string
	GOARCH      string
	Exe         string
	ExeErr      error
	Now         time.Time
	UID         int
	Keys        func() ([]update.PublicKey, error)
	LookPath    func(string) (string, error)
	Marker      string
	Term        ui.Terminal
	TmuxClip    func() string
	Interactive bool
	Width       int
	Height      int
	SizeOK      bool
}

// diagnosis is one run of the checks, with what the first ones learned for the later ones.
//
// settings holds config.toml, the defaults when it is missing or unusable, receipt the install
// receipt when it describes the running program, and the four slices the records of the data
// files that could be read.
type diagnosis struct {
	env      diagEnv
	paths    config.Paths
	pathsErr error
	settings config.Settings
	receipt  *update.Receipt
	conns    []store.Connection
	aliases  []store.Alias
	repos    []store.Repo
	funcs    []store.Func
	out      []finding
}

// diagnose runs every check.
//
// Parameters:
//   - env: what the checks read; nothing else of the machine is looked at
//
// Returns one finding per check, in the order they are shown. It writes nothing and does not
// use the network.
func diagnose(env diagEnv) []finding {
	d := &diagnosis{env: env, settings: config.DefaultSettings()}
	d.paths, d.pathsErr = config.Resolve(env.Getenv, env.Home)

	d.add(d.version())
	d.add(d.install())
	d.add(d.onPath())

	if d.pathsErr != nil {
		d.add(note("dirs", levelFail, "cli_doctor_dirs_unresolved", d.pathsErr.Error()).
			fix("cli_doctor_fix_dirs"))
	} else {
		d.dirs()
		d.files()
		d.add(d.names())
	}

	d.add(d.shellIntegration())
	d.add(d.tool("ssh", "cli_doctor_ssh", "cli_doctor_ssh_missing", d.needsSSH()))
	d.add(d.tool("git", "cli_doctor_git", "cli_doctor_git_missing", false))
	d.terminal()
	d.add(d.runtime())
	d.add(d.handoff())

	if d.pathsErr == nil {
		d.add(d.updateCache())
	}

	return d.out
}

// add records the findings of a check.
//
// Parameters:
//   - f: finding to append
func (d *diagnosis) add(f finding) {
	d.out = append(d.out, f)
}

// tilde shortens a path under the home directory to "~", so that a report can be pasted.
//
// Parameters:
//   - p: absolute path
func (d *diagnosis) tilde(p string) string {
	home := d.env.Home

	switch {
	case home == "" || home == "/":
		return p
	case p == home:
		return "~"
	case strings.HasPrefix(p, home+"/"):
		return "~" + p[len(home):]
	}

	return p
}

// scrub shortens the home directory in the text of an error, which the system fills with the
// absolute path of the file.
//
// Parameters:
//   - s: text of an error
func (d *diagnosis) scrub(s string) string {
	if d.env.Home == "" || d.env.Home == "/" {
		return s
	}

	return strings.ReplaceAll(s, d.env.Home+"/", "~/")
}

// word writes a path for a command line the user may copy: with "~" when it is under the home
// directory and nothing else needs quoting, else quoted.
//
// Parameters:
//   - p: absolute path
func (d *diagnosis) word(p string) string {
	short := d.tilde(p)
	rest := strings.TrimPrefix(short, "~")

	if short != p && !strings.ContainsAny(rest, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return short
	}

	return quoteWord(p)
}

// quoteWord quotes a word for a POSIX shell unless it is made of harmless characters only.
//
// Parameters:
//   - s: word to display
func quoteWord(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		plain := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.IndexByte("_-./,:@%+", c) >= 0

		if !plain {
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}

	if s == "" {
		return "''"
	}

	return s
}

// ownerOf gives the user id that owns a file.
//
// Parameters:
//   - info: result of Stat or Lstat
//
// Returns the id and true, or false when the system does not say.
func ownerOf(info fs.FileInfo) (int, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}

	return int(st.Uid), true
}

// modeText writes the permission bits of a file the way chmod takes them.
//
// Parameters:
//   - m: mode of the file
func modeText(m fs.FileMode) string {
	return fmt.Sprintf("%04o", uint32(m.Perm()))
}

// missing reports whether an error means that a file does not exist.
//
// Parameters:
//   - err: error of a stat or an open
func missing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// readCapped reads a small file whole.
//
// Parameters:
//   - path: file to read, symbolic links followed
//   - limit: most bytes accepted
//
// Returns the content, or an error when the file cannot be read or is larger than limit.
func readCapped(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("%s: not a regular file of at most %d bytes", path, limit)
	}

	return os.ReadFile(path)
}
