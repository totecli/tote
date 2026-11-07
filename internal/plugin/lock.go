package plugin

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/store"
)

// Kinds of installed plugin, tiers excepted, as the lock records them.
const (
	KindRelease = "release"
	KindLocal   = "local"
	KindBundled = "bundled"
)

// Values of consented_by.
const (
	ConsentPrompt = "prompt"
	ConsentGrant  = "grant"
)

// Values of last_reason, the way a plugin failed.
const (
	FailCrash    = "crash"
	FailProtocol = "protocol"
	FailTimeout  = "timeout"
	FailUsage    = "usage"
)

// TableLock is the name of the array of tables of plugins.lock.
const TableLock = "plugin"

// Limits of a lock record.
const (
	maxSourceBytes = 2048
	maxFailures    = 1 << 20
	lockTimeFormat = time.RFC3339
)

// originRe matches the lower case owner/repo of a release.
var originRe = regexp.MustCompile(`^[a-z0-9._+-]+/[a-z0-9._+-]+$`)

// ErrLockRecord is wrapped by every error about a lock record that breaks the schema.
var ErrLockRecord = errors.New("invalid lock record")

// LockRecord is one [[plugin]] block of plugins.lock: the install record, the consent, the kill
// switch and the failure counters of one plugin, so that a single write switches the version and
// the consent together.
//
// Source is the release URL, the local path or empty for a bundled plugin. Origin is the lower
// case owner/repo of a release and empty for the other kinds. The three hashes are 64 lower case
// hexadecimal digits, ArchiveSHA256 being empty for a local and a bundled plugin. Caps are the
// effective capabilities the user consented to. ConsentedAt and ConsentedBy are empty together
// when the user never consented. Disabled is the kill switch that does not depend on the layout of
// config.toml. Every key is always written, zero values included, so that a user editing the file
// sees the switches. Times are RFC 3339 in UTC.
type LockRecord struct {
	ID            string   `toml:"id"`
	Version       string   `toml:"version"`
	Tier          string   `toml:"tier"`
	Kind          string   `toml:"kind"`
	Source        string   `toml:"source"`
	Origin        string   `toml:"origin"`
	ArchiveSHA256 string   `toml:"archive_sha256"`
	TreeSHA256    string   `toml:"tree_sha256"`
	SurfaceSHA256 string   `toml:"surface_sha256"`
	InstalledAt   string   `toml:"installed_at"`
	UpdatedAt     string   `toml:"updated_at"`
	Caps          []string `toml:"caps"`
	ConsentedAt   string   `toml:"consented_at"`
	ConsentedBy   string   `toml:"consented_by"`
	Disabled      bool     `toml:"disabled"`
	Failures      int      `toml:"failures"`
	LastFailureAt string   `toml:"last_failure_at"`
	LastReason    string   `toml:"last_reason"`
	Quarantined   bool     `toml:"quarantined"`
}

// Key returns the plugin id.
func (r LockRecord) Key() string { return r.ID }

// Clone returns a copy of the record that shares no slice with it.
func (r LockRecord) Clone() LockRecord {
	r.Caps = slices.Clone(r.Caps)

	return r
}

// Consented reports whether the user ever consented to this install.
func (r LockRecord) Consented() bool { return r.ConsentedAt != "" }

// Validate checks every field of the record and returns the first error found. The error wraps
// ErrLockRecord. Nothing the record says is used before it passes: an invalid record makes the
// plugin broken.
func (r LockRecord) Validate() error {
	for _, check := range []func(LockRecord) error{
		LockRecord.checkIdentity, LockRecord.checkSource, LockRecord.checkHashes,
		LockRecord.checkTimes, LockRecord.checkConsent, LockRecord.checkFailures,
	} {
		if err := check(r); err != nil {
			return fmt.Errorf("%w: %w", ErrLockRecord, err)
		}
	}

	return nil
}

// checkIdentity checks the id, the version, the tier and the kind.
func (r LockRecord) checkIdentity() error {
	if err := config.ValidPluginID(r.ID); err != nil {
		return err
	}

	if _, err := ParseVersion(r.Version); err != nil {
		return fmt.Errorf("version: %w", err)
	}

	if r.Tier != TierDeclarative && r.Tier != TierExec {
		return fmt.Errorf("tier %q is not declarative or exec", CleanLine(r.Tier, issueArgRunes))
	}

	if !slices.Contains([]string{KindRelease, KindLocal, KindBundled}, r.Kind) {
		return fmt.Errorf("kind %q is not release, local or bundled",
			CleanLine(r.Kind, issueArgRunes))
	}

	return nil
}

// checkSource checks the source, the origin and the archive hash against the kind.
func (r LockRecord) checkSource() error {
	if len(r.Source) > maxSourceBytes || !utf8.ValidString(r.Source) ||
		strings.IndexFunc(r.Source, isControl) >= 0 {
		return errors.New("source is too long or holds a control character")
	}

	switch r.Kind {
	case KindRelease:
		if r.Source == "" || !originRe.MatchString(r.Origin) ||
			!hexRe.MatchString(r.ArchiveSHA256) {
			return errors.New("a release needs source, origin and archive_sha256")
		}
	case KindLocal:
		if r.Source == "" || r.Origin != "" || r.ArchiveSHA256 != "" {
			return errors.New("a local plugin needs source and has no origin or archive_sha256")
		}
	default:
		if r.Source != "" || r.Origin != "" || r.ArchiveSHA256 != "" {
			return errors.New("a bundled plugin has no source, origin or archive_sha256")
		}
	}

	return nil
}

// isControl reports whether a rune is a control character, DEL and the C1 range included.
//
// Parameters:
//   - r: rune to test
func isControl(r rune) bool { return r < 0x20 || (r >= 0x7f && r < 0xa0) }

// checkHashes checks the tree and surface hashes.
func (r LockRecord) checkHashes() error {
	if !hexRe.MatchString(r.TreeSHA256) {
		return errors.New("tree_sha256 is not 64 lower case hexadecimal digits")
	}

	if !hexRe.MatchString(r.SurfaceSHA256) {
		return errors.New("surface_sha256 is not 64 lower case hexadecimal digits")
	}

	return nil
}

// checkTimes checks the three timestamps.
func (r LockRecord) checkTimes() error {
	for _, t := range []struct {
		name     string
		value    string
		optional bool
	}{
		{"installed_at", r.InstalledAt, false},
		{"updated_at", r.UpdatedAt, false},
		{"consented_at", r.ConsentedAt, true},
		{"last_failure_at", r.LastFailureAt, true},
	} {
		if t.value == "" && t.optional {
			continue
		}

		if _, err := time.Parse(lockTimeFormat, t.value); err != nil {
			return fmt.Errorf("%s is not an RFC 3339 time", t.name)
		}
	}

	return nil
}

// checkConsent checks the capabilities and the consent fields.
func (r LockRecord) checkConsent() error {
	seen := map[string]bool{}

	for _, c := range r.Caps {
		if !ValidCap(c) || seen[c] {
			return fmt.Errorf("caps holds %q, which is unknown or repeated",
				CleanLine(c, issueArgRunes))
		}

		seen[c] = true
	}

	switch {
	case r.ConsentedBy != "" && r.ConsentedBy != ConsentPrompt && r.ConsentedBy != ConsentGrant:
		return errors.New("consented_by is not prompt or grant")
	case (r.ConsentedAt == "") != (r.ConsentedBy == ""):
		return errors.New("consented_at and consented_by are set together")
	}

	return nil
}

// checkFailures checks the failure counters.
func (r LockRecord) checkFailures() error {
	if r.Failures < 0 || r.Failures > maxFailures {
		return errors.New("failures is out of range")
	}

	reasons := []string{"", FailCrash, FailProtocol, FailTimeout, FailUsage}
	if !slices.Contains(reasons, r.LastReason) {
		return errors.New("last_reason is not crash, protocol, timeout or usage")
	}

	return nil
}

// lockRetries is how many times an update starts over when another process changes the record
// between its read and its write.
const lockRetries = 5

// Lock is plugins.lock, a store collection of LockRecord.
//
// The file is flock-ed, replaced atomically and read-only when it does not parse: Err then says
// why, every plugin is broken and no write goes through. A record that decodes but fails Validate
// is kept and listed by Invalid. Unknown keys of a record are kept by the writes. A Lock is for
// one goroutine at a time; several Locks, in one process or in several, may share the file.
type Lock struct {
	col *store.Collection[LockRecord]
}

// OpenLock opens plugins.lock and reads it.
//
// Parameters:
//   - path: location of the file, see config.Paths.LockFile; it may not exist yet
//
// Returns the lock, or an error when the path is empty. A file that cannot be read or does not
// parse is not an error here: the lock is read-only and Err says why.
func OpenLock(path string) (*Lock, error) {
	col, err := store.Open[LockRecord](path, TableLock)
	if col == nil {
		return nil, err
	}

	return &Lock{col: col}, nil
}

// Err returns why the lock cannot be used, nil when it is.
func (l *Lock) Err() error { return l.col.Err() }

// Path returns the location of the file.
func (l *Lock) Path() string { return l.col.Path() }

// Reload reads the file again and returns the reason it cannot be used, if any.
func (l *Lock) Reload() error { return l.col.Load() }

// Records returns the valid records in file order.
func (l *Lock) Records() []LockRecord {
	invalid := l.col.Invalid()

	var out []LockRecord

	for _, r := range l.col.List() {
		if invalid[r.ID] == nil {
			out = append(out, r)
		}
	}

	return out
}

// IDs returns the keys of every record in file order, the invalid ones included.
func (l *Lock) IDs() []string {
	var out []string

	for _, r := range l.col.List() {
		out = append(out, r.ID)
	}

	return out
}

// Invalid returns the records that decoded but fail Validate, by id.
func (l *Lock) Invalid() map[string]error { return l.col.Invalid() }

// Get returns the valid record of a plugin.
//
// Parameters:
//   - id: plugin id
//
// Returns the record and true, or the zero value and false when there is none or it is invalid.
func (l *Lock) Get(id string) (LockRecord, bool) {
	r, ok := l.col.Get(id)
	if !ok || l.col.Invalid()[id] != nil {
		return LockRecord{}, false
	}

	return r, true
}

// Put writes a record, replacing the one with the same id or appending a new one. This is the
// single write that switches a version, its tree and its consent together.
//
// Parameters:
//   - rec: record to write, validated before the write
//
// Returns the validation error, store.ErrReadOnly when the file does not parse, store.ErrBusy
// when the lock stayed taken, or an I/O error. Nothing is written on error.
func (l *Lock) Put(rec LockRecord) error {
	if rec.Caps == nil {
		rec.Caps = []string{}
	}

	return l.retry(func() error {
		if _, found := l.col.Get(rec.ID); found {
			return l.col.Replace(rec.ID, rec)
		}

		return l.col.Add(rec)
	})
}

// Create adds the record of a plugin that is not installed. It never replaces a record, which
// makes a second install of the same new id lose instead of overwriting the first.
//
// Parameters:
//   - rec: record to add, validated before the write
//
// Returns store.ErrExists when a record with this id is there, or the errors of Put.
func (l *Lock) Create(rec LockRecord) error {
	if rec.Caps == nil {
		rec.Caps = []string{}
	}

	return l.retryCreate(func() error { return l.col.Add(rec) })
}

// retryCreate runs an add, reloading the file and starting over only when the file moved under
// it. An id that is taken is a result, not something to retry.
//
// Parameters:
//   - write: one attempt
func (l *Lock) retryCreate(write func() error) error {
	var err error

	for range lockRetries {
		err = write()
		if !errors.Is(err, store.ErrConflict) {
			return err
		}

		if lerr := l.col.Load(); lerr != nil {
			return lerr
		}
	}

	return err
}

// Update changes the record of a plugin with a function, starting over when another process
// changed the file between the read and the write.
//
// Parameters:
//   - id: plugin id
//   - fn: receives a copy of the current record; the result is validated and written
//
// Returns store.ErrNotFound when no record has the id, the validation error of the result, or the
// errors of Put. Nothing is written on error.
func (l *Lock) Update(id string, fn func(*LockRecord)) error {
	return l.Change(id, func(r *LockRecord) error {
		fn(r)

		return nil
	})
}

// Change is Update with a function that can refuse. The function sees the record as it is on disk
// at that moment, so a decision based on it (a version, a tree) holds when the write happens: the
// write fails with store.ErrConflict, and the function runs again on the fresh record, if the
// record changed in between.
//
// Parameters:
//   - id: plugin id
//   - fn: receives a copy of the current record; the result is validated and written, unless fn
//     returns an error, which is returned as it is and writes nothing
//
// Returns store.ErrNotFound when no record has the id, the error of fn, the validation error of
// the result, or the errors of Put.
func (l *Lock) Change(id string, fn func(*LockRecord) error) error {
	return l.retry(func() error {
		rec, found := l.col.Get(id)
		if !found {
			return fmt.Errorf("plugin %q: %w", id, store.ErrNotFound)
		}

		rec = rec.Clone()

		if err := fn(&rec); err != nil {
			return err
		}

		if rec.Caps == nil {
			rec.Caps = []string{}
		}

		return l.col.Replace(id, rec)
	})
}

// SetDisabled turns the kill switch of a plugin on or off. It is one atomic write of the lock and
// does not read config.toml, so it cannot fail from the layout of a file the user edits.
//
// Parameters:
//   - id: plugin id
//   - off: true to disable the plugin
func (l *Lock) SetDisabled(id string, off bool) error {
	return l.Update(id, func(r *LockRecord) { r.Disabled = off })
}

// Delete removes the record of a plugin.
//
// Parameters:
//   - id: plugin id
//
// Returns store.ErrNotFound when there is no such record, or the errors of Put.
func (l *Lock) Delete(id string) error {
	return l.retry(func() error { return l.col.Delete(id) })
}

// retry runs a write, reloading the file and starting over while it reports a conflict or a key
// that moved.
//
// Parameters:
//   - write: one attempt, reading the current state of the collection
func (l *Lock) retry(write func() error) error {
	var err error

	for range lockRetries {
		err = write()
		if !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrExists) {
			return err
		}

		if lerr := l.col.Load(); lerr != nil {
			return lerr
		}
	}

	return err
}
