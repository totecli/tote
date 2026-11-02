package ui

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// minStatusLeft is the room the command and the branch keep before the update notice may take
// the right edge of the status line; a line that is too narrow shows the command alone.
const minStatusLeft = 24

// branchTTL is how long the branch of a repository is trusted before it is read again.
const branchTTL = 5 * time.Second

// statusInfo is the content of the status line that sits above the footer.
//
// Command is the exact command a connection will run, or the path or command line of the
// selected record; it is left empty when the detail panel already shows it. Repo is the absolute
// directory of the selected repository, empty for any other record: the root looks its branch up
// and fills Branch, Detached and Update before the line is drawn, so a screen never sets them.
// Branch is the git branch, or the short hash when Detached is true, and Update the notice that
// a new version is available.
type statusInfo struct {
	Command  string
	Repo     string
	Branch   string
	Detached bool
	Update   string
}

// statuser is implemented by an overlay that has its own status line, such as the palette that
// shows the command of the entry under the cursor. An overlay without it leaves the status of
// the screen below.
type statuser interface {
	Status(e *env) statusInfo
}

// branchMsg is the answer to a branch lookup started by the status line.
//
// Dir is the directory that was asked about, Name the branch or the short hash of a detached
// head, and OK is false when git is missing, the directory is not a repository or the lookup
// timed out.
type branchMsg struct {
	Dir      string
	Name     string
	Detached bool
	OK       bool
}

// updateMsg tells the status line that a new version is available. Version is the version
// number to show; an empty string removes the notice. Run sends it when Options.UpdateNotice
// finds a newer release.
type updateMsg struct {
	Version string
}

// branchEntry is what is known about the branch of one directory.
type branchEntry struct {
	name     string
	detached bool
	ok       bool
	pending  bool
	at       time.Time
}

// statusState holds what the status line learns over time: the branches of the repositories
// seen so far and the update notice. The zero value is ready to use.
//
// Lookup reads a branch and defaults to git; tests replace it. It runs off the interface
// thread, inside a command.
type statusState struct {
	branches map[string]branchEntry
	update   string
	lookup   func(dir string) branchMsg
}

// render draws the status line.
//
// Parameters:
//   - e: shared environment
//   - width: cells available
//
// Returns an empty string when there is nothing to show, else a line of at most width cells
// that starts with the indent. The branch comes before the command, because a long path would
// push it out of the line. The update notice goes to the right edge when there is room for
// it and the rest of the line; otherwise it is dropped before the command is cut.
func (s statusInfo) render(e *env, width int) string {
	var parts []string

	if s.Branch != "" {
		key := "ui_status_branch"
		if s.Detached {
			key = "ui_status_detached"
		}

		parts = append(parts, e.t(key, s.Branch))
	}

	if s.Command != "" {
		parts = append(parts, s.Command)
	}

	room := width - e.indent()
	if width < 4 || (len(parts) == 0 && s.Update == "") {
		return ""
	}

	left := strings.Join(parts, " "+e.Glyphs.sep+" ")
	update := e.Metric.fit(sanitize(s.Update), room, e.Glyphs.ellipsis)

	if left == "" {
		return e.lead() + e.Theme.Key.Render(update)
	}

	need := min(e.Metric.width(left), minStatusLeft)

	if uw := e.Metric.width(update); update != "" && room-uw-spareColumns-2 >= need {
		left = e.Metric.fit(left, room-uw-spareColumns-2, e.Glyphs.ellipsis)
		gap := room - spareColumns - e.Metric.width(left) - uw

		return e.lead() + e.Theme.Muted.Render(left+spaces(gap)) + e.Theme.Key.Render(update)
	}

	return e.lead() + e.Theme.Muted.Render(e.Metric.fit(left, room, e.Glyphs.ellipsis))
}

// commandOf returns the exact command line that Enter would run for a connection.
//
// Parameters:
//   - e: shared environment
//   - it: connection item
//   - user: login chosen for this run instead of the stored one, empty for the stored one
//
// Returns the sanitized command prefixed with the arrow glyph and true, or the sanitized reason
// it cannot run prefixed with the warning glyph and false. An item that is not a connection
// gives an empty string.
func commandOf(e *env, it item, user string) (string, bool) {
	c, ok := it.Rec.(store.Connection)
	if !ok {
		return "", false
	}

	argv, err := shell.ConnectArgv(c, user)
	if err != nil {
		return e.Glyphs.warn + " " + sanitize(err.Error()), false
	}

	return e.Glyphs.arrow + " " + sanitize(shell.PrettyCommand(argv)), true
}

// statusFor builds the status of the selected item.
//
// Parameters:
//   - e: shared environment
//   - it: selected item, nil when the list is empty
//   - panel: true when the detail panel is on screen and already shows the command
//   - user: login chosen for the next connection of the item, empty for the stored one
//
// Returns the command of the item unless the panel shows it, and the directory of a repository
// so that its branch can be looked up.
func statusFor(e *env, it *item, panel bool, user string) statusInfo {
	if it == nil {
		return statusInfo{}
	}

	s := statusInfo{}

	if !panel {
		s.Command = summaryOf(e, *it, user)
	}

	if r, ok := it.Rec.(store.Repo); ok {
		s.Repo = repoDir(e, r)
	}

	return s
}

// repoDir returns the absolute directory of a repository, with a leading "~" expanded.
//
// Parameters:
//   - e: shared environment, which gives the home directory
//   - r: repository
//
// Returns an empty string when the path is relative, since it would depend on the directory the
// program was started from.
func repoDir(e *env, r store.Repo) string {
	path := shell.ExpandHome(r.Path, e.Home)
	if !filepath.IsAbs(path) {
		return ""
	}

	return filepath.Clean(path)
}

// summaryOf returns the one line summary of an item: the command of a connection, the path of a
// repository, the command line of an alias or the script of a function.
//
// Parameters:
//   - e: shared environment
//   - it: item to summarize
//   - user: login chosen for the next connection, empty for the stored one
func summaryOf(e *env, it item, user string) string {
	switch r := it.Rec.(type) {
	case store.Connection:
		text, _ := commandOf(e, it, user)
		return text
	case store.Repo:
		return sanitize(r.Path)
	case store.Alias:
		return sanitize(r.Cmd)
	case store.Func:
		return sanitize(r.Script)
	}

	return ""
}

// statusOf returns the status line content for the current state: the status of the overlay on
// top when it has one, else of the screen, completed with the cached branch and the update
// notice.
func (r *root) statusOf() statusInfo {
	s := r.baseStatus()
	s.Branch, s.Detached = r.status.branch(s.Repo)
	s.Update = r.status.notice(r.e)

	return s
}

// baseStatus returns the status that the overlay on top or the screen reports, before the
// branch and the update notice are added.
func (r *root) baseStatus() statusInfo {
	if n := len(r.overlays); n > 0 {
		if st, ok := r.overlays[n-1].(statuser); ok {
			return st.Status(r.e)
		}
	}

	return r.scr.Status(r.e)
}

// watchStatus returns the command that reads the branch of the repository on the status line,
// nil when nothing needs reading.
func (r *root) watchStatus() tea.Cmd {
	return r.status.watch(r.e, r.baseStatus().Repo)
}

// watch decides whether the branch of a directory must be read.
//
// Parameters:
//   - e: shared environment, for the clock
//   - dir: absolute directory of the repository on the status line, empty for none
//
// Returns a command that reads it off the interface thread, or nil when the directory is empty,
// a lookup is already running or the cached answer is younger than branchTTL. The old answer
// stays on screen until the new one arrives, and a failed lookup is not repeated before
// branchTTL has passed.
func (st *statusState) watch(e *env, dir string) tea.Cmd {
	if dir == "" {
		return nil
	}

	if st.branches == nil {
		st.branches = map[string]branchEntry{}
	}

	now := e.Now()
	old, seen := st.branches[dir]

	if old.pending || (seen && !now.Before(old.at) && now.Sub(old.at) < branchTTL) {
		return nil
	}

	old.pending, old.at = true, now
	st.branches[dir] = old

	lookup := st.lookup
	if lookup == nil {
		lookup = gitBranch
	}

	return func() tea.Msg {
		m := lookup(dir)
		m.Dir = dir

		return m
	}
}

// apply stores the answer of a lookup or a new update notice.
//
// Parameters:
//   - e: shared environment, for the clock
//   - msg: a branchMsg or an updateMsg; anything else is ignored
func (st *statusState) apply(e *env, msg tea.Msg) {
	switch m := msg.(type) {
	case branchMsg:
		if st.branches == nil {
			st.branches = map[string]branchEntry{}
		}

		st.branches[m.Dir] = branchEntry{
			name: m.Name, detached: m.Detached, ok: m.OK, at: e.Now(),
		}
	case updateMsg:
		st.update = sanitize(m.Version)
	}
}

// branch returns the cached branch of a directory.
//
// Parameters:
//   - dir: absolute directory, empty for none
//
// Returns the sanitized name and whether it is a detached head; the name is empty when nothing
// is known or the directory is not a repository.
func (st *statusState) branch(dir string) (string, bool) {
	b, ok := st.branches[dir]
	if dir == "" || !ok || !b.ok {
		return "", false
	}

	return sanitize(b.name), b.detached
}

// notice returns the update notice to show, empty when there is none.
//
// Parameters:
//   - e: shared environment
func (st *statusState) notice(e *env) string {
	if st.update == "" {
		return ""
	}

	return e.t("ui_update_available", st.update)
}
