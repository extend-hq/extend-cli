package oauth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The refresh lock serializes token refreshes across CLI processes.
// The in-process mutex on TokenSource is not enough: two concurrent
// `extend` invocations each hold their own copy of the same refresh
// token, and whichever loses the race redeems an already-rotated
// eort_, which the server treats as replay and answers with family
// revocation — both processes (and the stored login) die.
//
// The lock is a plain O_CREATE|O_EXCL file beside the file-fallback
// token store, which works on every OS and filesystem the CLI runs
// on. A crashed process cannot unlock, so a lock file older than
// lockStaleAfter is treated as abandoned and broken.

// lockStaleAfter must comfortably exceed the longest legitimate hold:
// one refresh round trip (the HTTP client caps it at 30s) plus store
// writes.
const lockStaleAfter = 60 * time.Second

// lockPollInterval is how often a waiting process re-checks the lock.
const lockPollInterval = 50 * time.Millisecond

// refreshLockPath is the lock file location: next to the fallback
// token file so it shares that directory's lifecycle and permissions.
func refreshLockPath() (string, error) {
	path, err := tokensPath()
	if err != nil {
		return "", err
	}
	return path + ".lock", nil
}

// acquireRefreshLock blocks until the cross-process refresh lock is
// held or ctx ends, and returns the release function.
func acquireRefreshLock(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire refresh lock: %w", err)
		}
		if info, statErr := os.Lstat(path); statErr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			// Abandoned by a crashed process; break it and retry.
			// Best-effort: if several waiters race the removal, the
			// O_EXCL create still admits exactly one.
			os.Remove(path)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockPollInterval):
		}
	}
}
