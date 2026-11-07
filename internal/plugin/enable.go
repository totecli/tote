package plugin

import (
	"slices"
	"strings"
	"time"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/store"
)

// disableGrace is the time between TERM and KILL when disable stops the processes of a plugin.
const disableGrace = 2 * time.Second

// pluginTable returns the dotted table of a plugin in config.toml.
//
// Parameters:
//   - id: plugin id
func pluginTable(id string) string { return "plugins." + id }

// DisableResult is what Disable did.
//
// LockFlag tells that the kill switch in the lock is set; it does not depend on config.toml, so a
// plugin is off whenever it is true. ConfigOff tells that config.toml says enabled = false now.
// ConfigFile is the path of config.toml when it could not be edited because the user wrote the
// table in a layout the editor does not touch (dotted keys, an inline table): the command still
// succeeded through the lock flag, and the user may want to fix the file. Already tells that the
// plugin was off by both switches before the call. Killed is the number of process groups that
// were signalled.
type DisableResult struct {
	Already    bool
	LockFlag   bool
	ConfigOff  bool
	ConfigFile string
	Killed     int
}

// Disable turns a plugin off. It keeps the tree, the settings, the state and the consent.
//
//  1. The disabled flag of the lock record is set, in one atomic write that reads and parses
//     nothing else, so it cannot fail from the layout of a file the user edits. This is the kill
//     switch.
//  2. [plugins.<id>] enabled = false is written to config.toml with PatchTable. A layout the
//     editor refuses is not a failure: the result names the file and the flag alone keeps the
//     plugin off.
//  3. The process groups of the plugin recorded in plugin-procs are signalled, whichever tote
//     started them, and the running processes of this one die with them.
//
// Disable does not take the plugin mutex: an install in the middle of a download must not be able
// to delay the kill switch. The write of the lock is atomic and compared with the record, so it
// cannot lose against a commit.
//
// Parameters:
//   - id: plugin id
//
// Returns the result and nil, or an *Error: bad_id, not_installed, from_plugin, store_corrupt
// when the lock cannot be written (the other steps were still tried and are in the result).
func (r *Registry) Disable(id string) (DisableResult, error) {
	var res DisableResult

	if err := config.ValidPluginID(id); err != nil {
		return res, wrapError(CodeBadID, err, id)
	}

	if err := r.guardChange(); err != nil {
		return res, err
	}

	lk, lerr := OpenLock(r.opts.Paths.LockFile())
	if lerr != nil {
		return res, wrapError(CodeStoreCorrupt, lerr, r.opts.Paths.LockFile())
	}

	rec, installed := lk.Get(id)
	if !installed && lk.Invalid()[id] == nil && !r.hasDir(id) {
		return res, newError(CodeNotInstalled, id)
	}

	res.Already = installed && rec.Disabled && !r.configEnabled(id)

	var werr error

	if installed || lk.Invalid()[id] != nil {
		if werr = lk.SetDisabled(id, true); werr == nil {
			res.LockFlag = true
		}
	}

	res.ConfigOff, res.ConfigFile = r.setConfigEnabled(id, false)

	res.Killed, _ = r.StopPlugin(id, disableGrace)

	if werr != nil {
		return res, wrapError(CodeStoreCorrupt, werr, id)
	}

	return res, nil
}

// configEnabled reports whether config.toml leaves a plugin enabled. A file that does not parse
// counts as enabled here: the registry treats it as a failure of its own.
//
// Parameters:
//   - id: plugin id
func (r *Registry) configEnabled(id string) bool {
	cfg, err := config.LoadSettings(r.opts.Paths.ConfigFile())

	return err != nil || cfg.PluginEnabled(id)
}

// setConfigEnabled writes or removes enabled = false in the table of a plugin.
//
// Parameters:
//   - id: plugin id
//   - on: true to remove the key (enabled), false to write enabled = false
//
// Returns whether config.toml now has the wanted state, and its path when it could not be edited.
func (r *Registry) setConfigEnabled(id string, on bool) (bool, string) {
	path := r.opts.Paths.ConfigFile()

	// The error of the edit is not what decides: the file is read back, because a layout the
	// editor refuses may still say the right thing, and a file that parses but says the wrong
	// thing is the failure the caller must hear about.
	if on {
		_ = store.PatchTable(path, pluginTable(id), nil, []string{"enabled"})
	} else {
		_ = store.PatchTable(path, pluginTable(id), map[string]any{"enabled": false}, nil)
	}

	if r.configEnabled(id) == on {
		return true, ""
	}

	return false, path
}

// EnableResult is what Enable did.
//
// State and Reasons are the state of the plugin after the call. Already tells that nothing had to
// change. Consented tells that this call recorded a consent. ConfigFile is the path of config.toml
// when it says enabled = false in a layout the editor refuses: the plugin stays disabled and the
// user must edit that file.
type EnableResult struct {
	State      State
	Reasons    []Reason
	Already    bool
	Consented  bool
	ConfigFile string
}

// Enable turns a plugin on, or hands the decision to the allow flow.
//
// A broken or incompatible plugin is refused with the reasons; nothing is changed. A plugin that
// needs consent is enabled only with a grant that covers its Diff (see Allow). The call then
// clears, in one atomic write of the lock record, the kill switch, the quarantine and the failure
// counters, and records the consent when one was given. enabled is removed from config.toml; when
// the file says enabled = false in a layout the editor refuses, the plugin stays disabled and the
// result names the file. A plugin with a missing program is enabled and the state says so. Under
// the session switch the call is refused.
//
// Parameters:
//   - id: plugin id
//   - g: consent, used only when the plugin needs one
//
// Returns the result, or an *Error: bad_id, not_installed, from_plugin, enable_refused (the args
// are the reason codes), consent_needed, grant_mismatch, store_corrupt, stale.
func (r *Registry) Enable(id string, g Grant) (EnableResult, error) {
	var res EnableResult

	if err := config.ValidPluginID(id); err != nil {
		return res, wrapError(CodeBadID, err, id)
	}

	if err := r.guardChange(); err != nil {
		return res, err
	}

	if r.opts.NoPlugins {
		return res, enableRefusal(id, []Reason{{Code: ReasonSessionOff}})
	}

	p, ok := r.Recheck(id)
	if !ok {
		return res, newError(CodeNotInstalled, id)
	}

	f := p.facts
	f.Disabled = nil

	eff, reasons := Resolve(f)
	if !p.Installed || eff == StateBroken || eff == StateIncompatible {
		return res, enableRefusal(id, reasons)
	}

	by, err := r.enableConsent(p, eff, g)
	if err != nil {
		return res, err
	}

	res.Already = !p.Record.Disabled && !p.Record.Quarantined && by == "" &&
		r.configEnabled(id) && p.State == eff

	if !res.Already {
		if err := r.clearSwitches(id, p, by); err != nil {
			return res, err
		}

		res.Consented = by != ""
		_, res.ConfigFile = r.setConfigEnabled(id, true)
	}

	if now, found := r.Recheck(id); found {
		res.State, res.Reasons = now.State, now.Reasons
	}

	return res, nil
}

// enableConsent checks the grant of an enable when the plugin needs consent.
//
// Parameters:
//   - p: the plugin as Recheck sees it
//   - eff: its state when the switches are ignored
//   - g: the consent given
func (r *Registry) enableConsent(p Plugin, eff State, g Grant) (string, error) {
	if eff != StateNeedsConsent {
		return "", nil
	}

	return checkGrant(allowDiff(p), g)
}

// ReasonsError is the cause of an enable_refused error: the plugin and the reasons, with their
// arguments, that the error carries as bare codes. The host renders the message from it.
type ReasonsError struct {
	ID      string
	Reasons []Reason
}

// Error implements error. The text is for logs and tests, not for the user.
func (e *ReasonsError) Error() string {
	codes := make([]string, len(e.Reasons))
	for i, r := range e.Reasons {
		codes[i] = r.Code
	}

	return e.ID + ": " + strings.Join(codes, ", ")
}

// enableRefusal builds the enable_refused error of a plugin that cannot be enabled.
//
// Parameters:
//   - id: plugin id
//   - rs: why, as Resolve or Recheck gave it
func enableRefusal(id string, rs []Reason) *Error {
	return wrapError(CodeEnableRefused, &ReasonsError{ID: id, Reasons: rs}, reasonCodesOf(rs)...)
}

// reasonCodesOf lists the codes of reasons, at most maxReasonArgs of them.
//
// Parameters:
//   - rs: reasons
func reasonCodesOf(rs []Reason) []string {
	var out []string

	for _, r := range rs {
		if len(out) == maxReasonArgs {
			break
		}

		out = append(out, r.Code)
	}

	return out
}

// clearSwitches clears the kill switch, the quarantine and the failure counters of a record and,
// when by is set, records the consent for the tree the plugin was read from. It is one write,
// compared with the tree the caller saw.
//
// Parameters:
//   - id: plugin id
//   - p: the plugin as Recheck saw it
//   - by: consented_by to record, empty to keep the consent on record
func (r *Registry) clearSwitches(id string, p Plugin, by string) error {
	lk, err := r.openLockForWrite()
	if err != nil {
		return err
	}

	now := r.now().Format(lockTimeFormat)

	return writeError(lk.Change(id, func(rec *LockRecord) error {
		if rec.TreeSHA256 != p.Record.TreeSHA256 {
			return errStaleBase
		}

		rec.Disabled, rec.Quarantined, rec.Failures = false, false, 0
		rec.LastFailureAt, rec.LastReason = "", ""

		if by != "" {
			recordConsent(rec, p, by, now)
		}

		return nil
	}))
}

// recordConsent writes the consent for a verified tree into a record: the effective capabilities
// and the surface that the user saw.
//
// Parameters:
//   - rec: record to change
//   - p: the plugin, whose capabilities and surface are consented
//   - by: consented_by
//   - now: time of the consent
func recordConsent(rec *LockRecord, p Plugin, by, now string) {
	rec.Caps = slices.Clone(p.Caps)
	rec.SurfaceSHA256 = p.Surface
	rec.ConsentedAt, rec.ConsentedBy = now, by
}

// allowDiff builds the consent view of an installed plugin: what the user is asked to allow.
//
// The old version is gone when this is asked, so there is no command diff; the capabilities are
// compared with the consent on record. NeedsConsent is true when the plugin has no consent or its
// capabilities or surface are not the consented ones.
//
// Parameters:
//   - p: the plugin as Recheck sees it, with a manifest
func allowDiff(p Plugin) Diff {
	m, rec := p.Manifest, p.Record

	d := Diff{
		ID: p.ID, Title: m.Title, Description: m.Description, Version: m.Version,
		Tier: m.Tier, Program: programOf(m), Kind: rec.Kind, Origin: rec.Origin,
		Class:  originClass(rec.Kind, rec.Origin, p.ID),
		Source: rec.Source, TreeSHA256: rec.TreeSHA256, ArchiveSHA256: rec.ArchiveSHA256,
		SurfaceSHA256: p.Surface, Caps: p.Caps, Commands: Commands(m), Missing: p.Missing,
		Warnings: slices.Clone(p.Warnings), Issues: nil, Grant: p.Grant,
	}

	d.Added, d.Removed, d.Unchanged = capsDelta(rec.Caps, p.Caps)

	for _, rs := range p.Reasons {
		switch rs.Code {
		case ReasonNotConsented:
			d.Why = append(d.Why, WhyUnconsent)
		case ReasonCapsEscalated:
			d.Why = append(d.Why, WhyCaps)
		case ReasonEscalated:
			d.Why = append(d.Why, WhySurface)
		}
	}

	d.NeedsConsent = len(d.Why) > 0

	for _, rq := range p.Missing {
		d.Warnings = append(d.Warnings, Reason{ReasonMissingDep, []string{rq.Bin}})
	}

	return d
}

// verified reads an installed plugin whose tree was verified, for the allow flow.
//
// Parameters:
//   - id: plugin id
//
// Returns the plugin, or an *Error: bad_id, not_installed, enable_refused when the plugin has no
// verified manifest.
func (r *Registry) verified(id string) (Plugin, error) {
	if err := config.ValidPluginID(id); err != nil {
		return Plugin{}, wrapError(CodeBadID, err, id)
	}

	p, ok := r.Recheck(id)
	if !ok {
		return Plugin{}, newError(CodeNotInstalled, id)
	}

	if !p.Installed || p.Manifest == nil {
		return Plugin{}, enableRefusal(id, p.Reasons)
	}

	return p, nil
}

// Allow builds the consent view of an installed plugin, for the allow screen and for the grant
// token a script passes to Enable or Grant. It reads the plugin as Recheck does and changes
// nothing.
//
// Parameters:
//   - id: plugin id
//
// Returns the view, or an *Error: bad_id, not_installed, enable_refused when the plugin has no
// verified manifest.
func (r *Registry) Allow(id string) (Diff, error) {
	p, err := r.verified(id)
	if err != nil {
		return Diff{}, err
	}

	return allowDiff(p), nil
}

// GrantResult is what Grant did: Already tells that the plugin needed no consent.
type GrantResult struct {
	State   State
	Reasons []Reason
	Already bool
}

// Grant records the user's consent to the capabilities and the command surface of an installed
// plugin that needs it, without changing its other switches. The grant is checked against the
// view Allow returns, and the write is compared with the tree the plugin was read from.
//
// Parameters:
//   - id: plugin id
//   - g: the consent
//
// Returns the result, or an *Error: bad_id, not_installed, from_plugin, enable_refused,
// consent_needed, grant_mismatch, stale, store_corrupt.
func (r *Registry) Grant(id string, g Grant) (GrantResult, error) {
	var res GrantResult

	p, err := r.verified(id)
	if err != nil {
		return res, err
	}

	if err := r.guardChange(); err != nil {
		return res, err
	}

	d := allowDiff(p)

	if !d.NeedsConsent {
		res.Already, res.State, res.Reasons = true, p.State, p.Reasons

		return res, nil
	}

	by, err := checkGrant(d, g)
	if err != nil {
		return res, err
	}

	lk, err := r.openLockForWrite()
	if err != nil {
		return res, err
	}

	now := r.now().Format(lockTimeFormat)

	err = lk.Change(id, func(rec *LockRecord) error {
		if rec.TreeSHA256 != p.Record.TreeSHA256 {
			return errStaleBase
		}

		recordConsent(rec, p, by, now)

		return nil
	})
	if err != nil {
		return res, writeError(err)
	}

	if now, found := r.Recheck(id); found {
		res.State, res.Reasons = now.State, now.Reasons
	}

	return res, nil
}
