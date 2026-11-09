package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// staleHandoff is how old a hand-off file must be to count as left behind: a shell that is
	// still running its wrapper removes the file within the time of one command.
	staleHandoff = time.Hour

	// handoffNameLen is the length of "tote." followed by the eight random characters that mktemp
	// puts in the name of a hand-off file.
	handoffNameLen = len("tote.") + 8

	// maxScanned is the most entries of the hand-off directory looked at.
	maxScanned = 20000

	// maxListed is the most leftover files named in a report.
	maxListed = 5
)

// dirs checks the four directories where tote keeps its files.
func (d *diagnosis) dirs() {
	specs := []struct {
		id      string
		label   string
		path    string
		private bool
		plugins bool
	}{
		{"dir.config", "cli_doctor_dir_config", d.paths.Config, true, false},
		{"dir.data", "cli_doctor_dir_data", d.paths.Data, true, false},
		{"dir.state", "cli_doctor_dir_state", d.paths.State, true, false},
		{"dir.cache", "cli_doctor_dir_cache", d.paths.Cache, false, false},
		{"dir.plugins", "cli_doctor_dir_plugins", d.paths.PluginsDir(), false, true},
	}

	for _, s := range specs {
		d.add(d.dir(s.id, s.label, s.path, s.private, s.plugins))
	}
}

// dir checks one directory of tote: that it exists or can be created, that it is a directory
// (or a link to one), that its owner is the user and that nobody else can change it.
//
// Parameters:
//   - id: stable name of the check
//   - label: catalog key of the name of the directory
//   - path: absolute path
//   - private: true when only the owner should be able to enter it, as for the files that hold
//     the user's hosts and scripts
//   - plugins: true for the plugins directory, which others must not be able to change: plugins
//     are off while they can, so that is a failure and not a warning
//
// Returns a failure when the directory cannot be used, a warning when it is open to others.
func (d *diagnosis) dir(id, label, path string, private, plugins bool) finding {
	where := d.tilde(path)
	lab := msg{Key: label}

	li, err := os.Lstat(path)
	if err != nil {
		if missing(err) {
			return note(id, levelOK, "cli_doctor_dir_missing", lab, where)
		}

		return note(id, levelFail, "cli_doctor_dir_error", lab, where, d.scrub(err.Error()))
	}

	info, target := li, ""

	if li.Mode()&os.ModeSymlink != 0 {
		target, _ = os.Readlink(path)

		if info, err = os.Stat(path); err != nil {
			return note(id, levelFail, "cli_doctor_dir_broken_link", lab, where, d.tilde(target)).
				fix("cli_doctor_fix_link")
		}
	}

	if !info.IsDir() {
		return note(id, levelFail, "cli_doctor_dir_not_dir", lab, where).
			fix("cli_doctor_fix_not_dir")
	}

	var issues []finding

	perm := info.Mode().Perm()
	owner, known := ownerOf(info)
	chmod := "cli_doctor_fix_chmod"

	if known && owner != d.env.UID {
		l := levelFail
		if d.env.UID == 0 {
			l = levelWarn
		}

		issues = append(issues, note(id, l, "cli_doctor_dir_foreign", lab, where, owner,
			d.env.UID).fix("cli_doctor_fix_chown", d.word(path)))
	}

	if known && owner == d.env.UID && d.env.UID != 0 && perm&0o700 != 0o700 {
		issues = append(issues, note(id, levelFail, "cli_doctor_dir_no_access", lab, where,
			modeText(perm)).fix(chmod, d.word(path)))
	}

	switch {
	case perm&0o022 != 0 && plugins:
		issues = append(issues, note(id, levelFail, "cli_doctor_dir_plugins_shared", lab, where,
			modeText(perm)).fix("cli_doctor_fix_chmod_gow", d.word(path)))
	case perm&0o022 != 0:
		issues = append(issues, note(id, levelWarn, "cli_doctor_dir_shared", lab, where,
			modeText(perm)).fix(chmod, d.word(path)))
	case private && perm&0o077 != 0:
		issues = append(issues, note(id, levelWarn, "cli_doctor_dir_open", lab, where,
			modeText(perm)).fix(chmod, d.word(path)))
	}

	if len(issues) > 0 {
		return combine(id, issues...)
	}

	if target != "" {
		return note(id, levelOK, "cli_doctor_dir_link", lab, where, d.tilde(target),
			modeText(perm))
	}

	return note(id, levelOK, "cli_doctor_dir_ok", lab, where, modeText(perm))
}

// handoffDir returns the directory where the shell wrapper makes its hand-off file, chosen the
// way the wrapper does: XDG_RUNTIME_DIR, else TMPDIR, else /tmp.
//
// Returns the directory and whether it came from XDG_RUNTIME_DIR, which should be private.
func (d *diagnosis) handoffDir() (string, bool) {
	if v := d.env.Getenv("XDG_RUNTIME_DIR"); v != "" {
		return v, true
	}

	if v := d.env.Getenv("TMPDIR"); v != "" {
		return v, false
	}

	return "/tmp", false
}

// writable tells whether the user can probably create files in a directory, from its mode and
// owner alone, since nothing may be written to find out.
//
// Parameters:
//   - info: directory
func (d *diagnosis) writable(info os.FileInfo) bool {
	if d.env.UID == 0 {
		return true
	}

	perm := info.Mode().Perm()

	if owner, ok := ownerOf(info); ok && owner == d.env.UID {
		return perm&0o200 != 0
	}

	return perm&0o022 != 0
}

// runtime checks the directory of the hand-off file, which the wrapper needs to change the
// directory of the shell or to fill its command line.
//
// Returns a failure when the wrapper cannot create a file there, a warning when it is not
// private or is shared without the sticky bit.
func (d *diagnosis) runtime() finding {
	dir, private := d.handoffDir()
	where := d.tilde(dir)

	info, err := os.Stat(dir)
	if err != nil {
		return note("runtime", levelFail, "cli_doctor_runtime_missing", where).
			fix("cli_doctor_fix_runtime")
	}

	if !info.IsDir() {
		return note("runtime", levelFail, "cli_doctor_runtime_not_dir", where).
			fix("cli_doctor_fix_runtime")
	}

	if !d.writable(info) {
		return note("runtime", levelFail, "cli_doctor_runtime_readonly", where,
			modeText(info.Mode())).fix("cli_doctor_fix_runtime")
	}

	perm := info.Mode().Perm()
	owner, known := ownerOf(info)

	switch {
	case private && known && owner != d.env.UID && d.env.UID != 0:
		return note("runtime", levelWarn, "cli_doctor_runtime_foreign", where, owner).
			fix("cli_doctor_fix_runtime")
	case private && perm&0o077 != 0:
		return note("runtime", levelWarn, "cli_doctor_runtime_open", where, modeText(perm)).
			fix("cli_doctor_fix_chmod", d.word(dir))
	case !private && perm&0o002 != 0 && info.Mode()&os.ModeSticky == 0:
		return note("runtime", levelWarn, "cli_doctor_runtime_sticky", where, modeText(perm)).
			fix("cli_doctor_fix_sticky", d.word(dir))
	}

	return note("runtime", levelOK, "cli_doctor_runtime_ok", where, modeText(perm))
}

// handoff looks for hand-off files that a killed shell left behind, and checks the one this run
// was given.
//
// Returns a warning when TOTE_HANDOFF names a file that cannot be used, or when files named
// like the ones of the wrapper, owned by the user and older than an hour are in the directory.
func (d *diagnosis) handoff() finding {
	dir, _ := d.handoffDir()
	current := d.env.Getenv("TOTE_HANDOFF")

	var parts []finding

	if current != "" {
		if info, err := os.Lstat(current); err != nil || !info.Mode().IsRegular() {
			parts = append(parts, note("handoff", levelWarn, "cli_doctor_handoff_bad",
				d.tilde(current)).fix("cli_doctor_fix_handoff"))
		}
	}

	if old := d.leftovers(dir, current); len(old) > 0 {
		f := note("handoff", levelWarn, "cli_doctor_handoff_stale", len(old), d.tilde(dir))

		args := make([]string, 0, maxListed)

		for i, p := range old {
			if i < maxListed {
				f = f.detail("cli_doctor_handoff_item", d.tilde(p))
				args = append(args, d.word(p))
			}
		}

		if len(old) > maxListed {
			f = f.detail("cli_doctor_handoff_more", len(old)-maxListed)
		}

		f = f.fix("cli_doctor_fix_stale", strings.Join(args, " "))
		parts = append(parts, f)
	}

	if len(parts) == 0 {
		return note("handoff", levelOK, "cli_doctor_handoff_ok", d.tilde(dir))
	}

	return combine("handoff", parts...)
}

// leftovers lists the hand-off files of the user that are old enough to be abandoned.
//
// Parameters:
//   - dir: directory the wrapper makes its files in
//   - current: hand-off file of this run, which is never listed
//
// Returns the paths in name order. A directory that cannot be listed has none.
func (d *diagnosis) leftovers(dir, current string) []string {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}

	defer func() { _ = f.Close() }()

	names, err := f.Readdirnames(maxScanned)
	if err != nil && len(names) == 0 {
		return nil
	}

	var out []string

	for _, n := range names {
		if len(n) != handoffNameLen || !strings.HasPrefix(n, "tote.") {
			continue
		}

		p := filepath.Join(dir, n)
		if p == current {
			continue
		}

		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		if owner, ok := ownerOf(info); !ok || owner != d.env.UID {
			continue
		}

		if d.env.Now.Sub(info.ModTime()) >= staleHandoff {
			out = append(out, p)
		}
	}

	slices.Sort(out)

	return out
}
