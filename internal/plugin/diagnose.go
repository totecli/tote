package plugin

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"syscall"
)

// Diagnosis is what `plugin check` without an argument reports, and what a future doctor shows
// for plugins: every installed plugin checked, then the things on disk that no plugin owns.
//
// Problems are the failures that concern every plugin, such as a lock or a config.toml that does
// not parse. Plugins hold one result per installed plugin, without the argv of its contributions;
// their findings include the deprecated keys, the reserved features a manifest would need
// (reserved_not_available, feature_missing) and the setting values that were rejected. Orphans are
// the entries of the plugins directory and the lock that are not plugins, Leftovers the staging
// directories, old and orphan trees and the settings, state, cache and host directories of
// uninstalled ids. Findings are about the host files: a plugin-procs that is not safe, or that
// names a plugin which is not installed.
type Diagnosis struct {
	Strict    bool
	Problems  []Reason
	Plugins   []*CheckResult
	Orphans   []Orphan
	Leftovers []Leftover
	Findings  []Finding
}

// Failed tells whether the diagnosis found a defect: a problem that concerns every plugin, a
// plugin whose check fails, or an error about the host files. A leftover is a note, never a
// failure.
func (d Diagnosis) Failed() bool {
	if len(d.Problems) > 0 {
		return true
	}

	for _, p := range d.Plugins {
		if p.Failed() {
			return true
		}
	}

	for _, f := range d.Findings {
		if f.Severity == SeverityError {
			return true
		}
	}

	return false
}

// Diagnose checks every installed plugin and reports what is left on disk. It reads the lock,
// config.toml, the trees and the host files, starts no process and writes nothing: unlike the
// cleanup that runs before a change, it deletes no leftover, it only lists them.
//
// Parameters:
//   - ctx: cancelled between two plugins
//   - strict: the --strict flag, applied to each plugin result
//
// Returns the diagnosis. A plugin that cannot be read is a result with errors in it, not a
// failure of the call.
func (r *Registry) Diagnose(ctx context.Context, strict bool) Diagnosis {
	snap := r.Load()
	lc := r.context()

	d := Diagnosis{Strict: strict, Problems: snap.Problems, Orphans: snap.Orphans}
	installed := map[string]bool{}

	for _, p := range snap.Plugins() {
		if ctx.Err() != nil {
			break
		}

		installed[p.ID] = p.Installed

		req := CheckRequest{Target: p.ID, Strict: strict, noArgv: true}
		d.Plugins = append(d.Plugins, r.checkInstalled(p, lc, req))
	}

	left, err := r.Leftovers()
	d.Leftovers = left

	if err != nil && len(snap.Problems) == 0 {
		var pe *Error
		if errors.As(err, &pe) {
			d.Findings = append(d.Findings, errorFinding(pe))
		}
	}

	d.Findings = append(d.Findings, r.procsFindings(installed)...)

	return d
}

// procsFindings inspects plugin-procs without opening it for writing: the file must be a private
// regular file of the user, and each plugin it names must be installed, else a process of a
// plugin that was removed may still be running.
//
// Parameters:
//   - installed: ids that have a lock record
func (r *Registry) procsFindings(installed map[string]bool) []Finding {
	path := r.opts.Paths.ProcsFile()

	info, err := os.Lstat(path)
	if err != nil {
		return nil
	}

	id, ok := statIDOf(info)
	if !ok || !info.Mode().IsRegular() || int(id.UID) != r.uid() || info.Mode().Perm()&0o077 != 0 {
		return []Finding{{Scope: ScopeEnv, Code: CodeProcsUnsafe, Severity: SeverityError}}
	}

	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}

	defer func() { _ = f.Close() }()

	data, _ := io.ReadAll(io.LimitReader(f, maxProcsFile))

	var out []Finding

	seen := map[string]bool{}

	for _, e := range parseProcs(string(data)) {
		if installed[e.ID] || seen[e.ID] {
			continue
		}

		seen[e.ID] = true

		out = append(out, Finding{
			Scope: ScopeEnv, Code: CodeProcsOrphan,
			Severity: SeverityWarning, Args: []string{e.ID, strconv.Itoa(e.SID)},
		})
	}

	return out
}
