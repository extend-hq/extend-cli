package oauth

import (
	"context"
	"crypto/sha256"
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
// The lock is a plain O_CREATE|O_EXCL file, which works on every OS
// and filesystem the CLI runs on. A crashed process cannot unlock, so
// a lock file older than lockStaleAfter is treated as abandoned and
// broken.

// lockStaleAfter must comfortably exceed the longest legitimate hold:
// one refresh round trip (the HTTP client caps it at 30s) plus store
// writes.
const lockStaleAfter = 60 * time.Second

// lockPollInterval is how often a waiting process re-checks the lock.
const lockPollInterval = 50 * time.Millisecond

// lockAcquireTimeout bounds how long a refresh waits for the lock
// before failing closed. It must exceed lockStaleAfter so a lock file
// abandoned by a crashed process ages past the stale threshold and is
// broken before a waiter gives up.
const lockAcquireTimeout = 90 * time.Second

// lockDir resolves the directory holding refresh lock files. It is a
// variable so tests can point locks at a scratch directory.
var lockDir = defaultLockDir

// defaultLockDir is deliberately independent of XDG_CONFIG_HOME even
// though the fallback token file honors it: the OS keychain store is
// shared per OS user regardless of environment variables, so two CLI
// processes launched with different environments (an interactive shell
// with XDG_CONFIG_HOME set, a cron job without) can refresh the same
// stored login and must contend on the same lock file.
func defaultLockDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "extend"), nil
}

// refreshLockPath is the lock file for one stored login. The name is
// keyed on the normalized API base URL so refreshes of the same
// credentials always contend on the same file, while logins against
// different bases never serialize each other.
func refreshLockPath(apiBase string) (string, error) {
	dir, err := lockDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(NormalizeBase(apiBase)))
	return filepath.Join(dir, fmt.Sprintf("refresh-%x.lock", sum[:8])), nil
}

// acquireRefreshLock blocks until the cross-process refresh lock is
// held or ctx ends, and returns the release function. Any failure to
// create the lock file other than "it already exists" is returned
// immediately: proceeding without the lock would silently allow the
// double-spend the lock exists to prevent.
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
