package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/totecli/tote/internal/update"
)

// version reports the release string and the platform.
func (d *diagnosis) version() finding {
	if d.env.Version == "dev" {
		return note("version", levelOK, "cli_doctor_version_dev", d.env.GOOS, d.env.GOARCH)
	}

	return note("version", levelOK, "cli_doctor_version", d.env.Version, d.env.GOOS, d.env.GOARCH)
}

// install reports how the running program was installed: the receipt, the package marker or
// the place of the file.
//
// Returns a warning when the receipt cannot be read or describes another program.
func (d *diagnosis) install() finding {
	if d.env.ExeErr != nil || d.env.Exe == "" {
		err := d.env.ExeErr
		if err == nil {
			err = errors.New("empty path")
		}

		return note("install", levelWarn, "cli_doctor_install_noexe", d.scrub(err.Error()))
	}

	exe := d.env.Exe
	where := d.tilde(exe)
	foreign := ""

	var bad error

	if d.pathsErr == nil {
		r, err := update.ReadReceipt(d.paths.ReceiptFile())

		switch {
		case err == nil && r.Describes(exe):
			d.receipt = &r
		case err == nil:
			foreign = r.Path
		case !missing(err):
			bad = err
		}
	}

	inst := update.Detect(exe, d.receipt, d.env.Marker)
	method := string(inst.Method)

	switch {
	case bad != nil:
		return note("install", levelWarn, "cli_doctor_install_bad_receipt", d.scrub(bad.Error())).
			fix("cli_doctor_fix_receipt", d.word(d.paths.ReceiptFile())).
			detail("cli_doctor_install_guess", method, where)
	case foreign != "":
		return note("install", levelWarn, "cli_doctor_install_foreign", d.tilde(foreign), where).
			detail("cli_doctor_install_guess", method, where)
	case inst.Managed:
		return note("install", levelOK, "cli_doctor_install_managed", method, where,
			inst.UpdateCommand)
	case d.receipt != nil && d.receipt.Version != "" && d.receipt.Version != d.env.Version &&
		isRelease(d.env.Version):
		return note("install", levelWarn, "cli_doctor_install_stale_receipt",
			d.receipt.Version, d.env.Version).fix("cli_doctor_fix_receipt_stale")
	case d.receipt != nil:
		if m := d.receipt.InstallMethod; m != "" {
			method = m
		}

		return note("install", levelOK, "cli_doctor_install_receipt", method, where,
			d.receipt.Version, d.receipt.Channel)
	}

	return note("install", levelOK, "cli_doctor_install_none", where)
}

// onPath reports whether "tote" in PATH is the running program.
//
// Returns a warning when tote is not in PATH or when another program of that name comes first.
func (d *diagnosis) onPath() finding {
	exe := d.env.Exe
	found, err := d.env.LookPath("tote")
	if err != nil {
		f := note("path", levelWarn, "cli_doctor_path_missing")
		if exe != "" {
			f = f.fix("cli_doctor_fix_path", d.word(filepath.Dir(exe)))
		}

		return f
	}

	if exe != "" && !sameFile(found, exe) {
		return note("path", levelWarn, "cli_doctor_path_other", d.tilde(found), d.tilde(exe)).
			fix("cli_doctor_fix_shadow", d.word(filepath.Dir(exe)))
	}

	return note("path", levelOK, "cli_doctor_path_ok", d.tilde(found))
}

// sameFile reports whether two paths lead to the same file.
//
// Parameters:
//   - a: first path
//   - b: second path
func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}

	ib, err := os.Stat(b)

	return err == nil && os.SameFile(ia, ib)
}

// needsSSH reports whether a saved connection runs ssh, which is the default program.
func (d *diagnosis) needsSSH() bool {
	for _, c := range d.conns {
		if c.Via == "" || c.Via == "ssh" {
			return true
		}
	}

	return false
}

// tool reports whether a program that tote starts is installed.
//
// Parameters:
//   - name: program, looked up in PATH
//   - found: catalog key of the message when the program is there, which takes the path
//   - lacking: catalog key of the message when it is not
//   - needed: true when a saved record depends on the program, which turns its absence into a
//     failure instead of a warning
func (d *diagnosis) tool(name, found, lacking string, needed bool) finding {
	path, err := d.env.LookPath(name)
	if err == nil {
		return note(name, levelOK, found, d.tilde(path))
	}

	l := levelWarn
	if needed {
		l = levelFail
	}

	return note(name, l, lacking).fix("cli_doctor_fix_install", name)
}

// isRelease reports whether a version string is a release, as opposed to a development build.
//
// Parameters:
//   - version: release string of the running program
func isRelease(version string) bool {
	_, err := update.ParseVersion(version)

	return err == nil
}

// updateCache reports the state of the update check that the status line reads.
//
// Returns a warning when this release build cannot verify an update because it has no usable
// release key, and when the cache file is unusable or dated in the future.
func (d *diagnosis) updateCache() finding {
	if isRelease(d.env.Version) && d.env.Keys != nil {
		if _, err := d.env.Keys(); err != nil {
			return note("update", levelWarn, "cli_doctor_update_nokey").fix("cli_doctor_fix_nokey")
		}
	}

	if !d.settings.Update.Check {
		return note("update", levelOK, "cli_doctor_update_off")
	}

	path := filepath.Join(d.paths.Cache, update.CacheFile)

	if _, err := os.Lstat(path); err != nil {
		if missing(err) {
			return note("update", levelOK, "cli_doctor_update_never")
		}

		return note("update", levelWarn, "cli_doctor_update_unreadable", d.tilde(path)).
			fix("cli_doctor_fix_cache", d.word(path))
	}

	channel := channelOf(d.env.Version, d.receipt, false)

	n, ok := update.ReadNotice(path, d.env.Version, channel)
	if !ok {
		return note("update", levelWarn, "cli_doctor_update_unreadable", d.tilde(path)).
			fix("cli_doctor_fix_cache", d.word(path))
	}

	day := n.CheckedAt.UTC().Format(time.DateOnly)

	switch {
	case n.CheckedAt.After(d.env.Now.Add(time.Hour)):
		return note("update", levelWarn, "cli_doctor_update_future", day).
			fix("cli_doctor_fix_clock")
	case n.Latest == "":
		return note("update", levelOK, "cli_doctor_update_failed", day)
	case n.Available:
		return note("update", levelOK, "cli_doctor_update_available", n.Latest, day)
	}

	return note("update", levelOK, "cli_doctor_update_current", day)
}
