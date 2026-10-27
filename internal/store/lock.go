//go:build linux || darwin

package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// DefaultLockWait is how long a write waits for the lock of a file by default.
const DefaultLockWait = 10 * time.Second

// lockPoll is the pause between two attempts to take a busy lock.
const lockPoll = 5 * time.Millisecond

// acquireLock opens the sibling lock file of a data file and takes an exclusive flock on it.
//
// The lock file is "<target>.lock", created with mode 0600 and never removed, because removing
// it would let two writers lock two different inodes. The call waits at most wait for the lock
// to be free.
//
// Parameters:
//   - target: resolved path of the data file, not the lock file
//   - wait: longest wait, zero or negative to try once
//
// Returns the open lock file, to be handed to releaseLock, or an error wrapping ErrBusy when the
// lock stayed taken.
func acquireLock(target string, wait time.Duration) (*os.File, error) {
	f, err := os.OpenFile(target+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	deadline := time.Now().Add(wait)

	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EWOULDBLOCK) && time.Now().Before(deadline) {
			time.Sleep(lockPoll)
			continue
		}
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("lock %s: %w", f.Name(), ErrBusy)
		}

		return nil, fmt.Errorf("lock %s: %w", f.Name(), err)
	}

	return f, nil
}

// releaseLock drops the flock held on a lock file and closes it.
//
// Parameters:
//   - f: lock file returned by acquireLock
func releaseLock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}
