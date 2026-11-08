package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	// CacheFile is the name, in the cache directory, of the file that remembers the last check.
	CacheFile = "update-check.json"

	// CheckInterval is the shortest time between two network checks made for the status line.
	CheckInterval = 24 * time.Hour

	// cacheSchema is the format of the cache file.
	cacheSchema = 1

	// maxCacheSize is the largest cache file read, in bytes.
	maxCacheSize = 4 << 10
)

// Notice is what the status line needs to say that an update exists.
//
// Current is the running version, Latest the newest release known for the channel, empty when
// the last check failed, and Available tells whether Latest is above Current. CheckedAt is when
// the network was last asked.
type Notice struct {
	Current   string
	Latest    string
	Channel   Channel
	Available bool
	CheckedAt time.Time
}

// cacheEntry is the content of the cache file.
type cacheEntry struct {
	Schema    int    `json:"schema"`
	CheckedAt string `json:"checked_at"`
	Channel   string `json:"channel"`
	Latest    string `json:"latest"`
}

// notice builds a Notice from a cache entry.
//
// Parameters:
//   - e: cache entry
//   - current: running version
//   - at: time of the check
func notice(e cacheEntry, current string, at time.Time) Notice {
	n := Notice{Current: current, Latest: e.Latest, Channel: Channel(e.Channel), CheckedAt: at}

	cur, err := ParseVersion(current)
	if err != nil {
		return n
	}

	if latest, err := ParseVersion(e.Latest); err == nil {
		n.Available = latest.Compare(cur) > 0
	}

	return n
}

// readCache reads the cache file.
//
// Parameters:
//   - path: location of the cache file
//
// Returns the entry and the time of the check, or false when the file is missing, too large,
// not valid or of another schema.
func readCache(path string) (cacheEntry, time.Time, bool) {
	// The type is checked before opening, as opening a pipe would block for ever.
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return cacheEntry{}, time.Time{}, false
	}

	f, err := os.Open(path)
	if err != nil {
		return cacheEntry{}, time.Time{}, false
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxCacheSize+1))
	if err != nil || len(data) > maxCacheSize {
		return cacheEntry{}, time.Time{}, false
	}

	var e cacheEntry
	if json.Unmarshal(data, &e) != nil || e.Schema != cacheSchema {
		return cacheEntry{}, time.Time{}, false
	}

	at, err := time.Parse(time.RFC3339, e.CheckedAt)
	if err != nil {
		return cacheEntry{}, time.Time{}, false
	}

	if e.Latest != "" {
		if _, err := ParseVersion(e.Latest); err != nil {
			return cacheEntry{}, time.Time{}, false
		}
	}

	return e, at, true
}

// ReadNotice reads what the last check found, without touching the network.
//
// Parameters:
//   - path: location of the cache file, CacheFile in the cache directory
//   - current: running version
//   - channel: channel the running install follows; a cache made for another one is ignored
//
// Returns the notice and true, or false when there is no usable cache. A version read from the
// file must be a strict version, so that nothing else can reach the screen.
func ReadNotice(path, current string, channel Channel) (Notice, bool) {
	e, at, ok := readCache(path)
	if !ok || e.Channel != string(channel) {
		return Notice{}, false
	}

	return notice(e, current, at), true
}

// NoticeDue reports whether the network may be asked again for the status line.
//
// Parameters:
//   - path: location of the cache file
//   - channel: channel the running install follows
//   - now: current time
//
// Returns true when there is no usable cache for the channel or the last check is at least
// CheckInterval old. A check time in the future counts as due, so that a wrong clock cannot
// silence the check for ever.
func NoticeDue(path string, channel Channel, now time.Time) bool {
	e, at, ok := readCache(path)
	if !ok || e.Channel != string(channel) {
		return true
	}

	age := now.Sub(at)

	return age < 0 || age >= CheckInterval
}

// RefreshNotice asks the release server if the cache is due, and records the answer.
//
// It is meant to run in the background after the interface has started, at most once a day, and
// only when the user has not turned the check off. A failed check is recorded too, with an
// empty latest version, so that an offline machine does not retry at every start.
//
// Parameters:
//   - ctx: cancels the request
//   - path: location of the cache file; its directory is created when missing
//   - now: current time
//
// Returns the notice. When the cache is fresh no request is made. The error is that of the
// check, the notice being then the one of the previous check, or empty.
func (u *Updater) RefreshNotice(ctx context.Context, path string, now time.Time) (Notice, error) {
	if !NoticeDue(path, u.channel(), now) {
		n, _ := ReadNotice(path, u.Version, u.channel())
		return n, nil
	}

	res, err := u.Check(ctx)
	if errors.Is(err, context.Canceled) {
		// The interface closed first: nothing was learnt, so the next start asks again.
		return Notice{}, err
	}

	e := cacheEntry{
		Schema: cacheSchema, CheckedAt: formatTime(now), Channel: string(u.channel()),
	}

	if err == nil {
		e.Latest = res.Latest
	}

	if werr := writeCache(path, e); werr != nil && err == nil {
		err = werr
	}

	return notice(e, u.Version, now), err
}

// writeCache stores a cache entry.
//
// Parameters:
//   - path: location of the cache file
//   - e: entry to store
func writeCache(path string, e cacheEntry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	return writeFileAtomic(path, append(data, '\n'), 0o600)
}
