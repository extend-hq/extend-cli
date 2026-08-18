package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testLockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "oauth_tokens.json.lock")
}

func TestAcquireRefreshLockBlocksSecondAcquirer(t *testing.T) {
	path := testLockPath(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	release, err := acquireRefreshLock(ctx, path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release2, err := acquireRefreshLock(ctx, path)
		if err != nil {
			t.Errorf("second acquire: %v", err)
			close(acquired)
			return
		}
		close(acquired)
		release2()
	}()

	select {
	case <-acquired:
		t.Fatal("second acquirer got the lock while the first still held it")
	case <-time.After(200 * time.Millisecond):
	}

	release()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second acquirer never got the lock after release")
	}
}

func TestAcquireRefreshLockHonorsContext(t *testing.T) {
	path := testLockPath(t)
	bg, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bgCancel()
	release, err := acquireRefreshLock(bg, path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := acquireRefreshLock(ctx, path); err != context.DeadlineExceeded {
		t.Errorf("blocked acquire = %v, want DeadlineExceeded", err)
	}
}

func TestAcquireRefreshLockBreaksStaleLock(t *testing.T) {
	path := testLockPath(t)
	if err := os.WriteFile(path, []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Age the lock past the stale threshold, as a crashed process
	// would have left it.
	old := time.Now().Add(-2 * lockStaleAfter)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := acquireRefreshLock(ctx, path)
	if err != nil {
		t.Fatalf("acquire over a stale lock: %v", err)
	}
	release()
}

// TestConcurrentSourcesDoNotDoubleSpendRefreshToken simulates two CLI
// processes: two independent TokenSource instances (separate in-memory
// state) sharing one on-disk file store, both starting from the same
// stored login. The fake server enforces rotate-on-use reuse detection
// like the real one. Without the cross-process lock plus post-acquire
// store re-read, the loser of the race redeems an already-rotated
// eort_ and the family is revoked.
func TestConcurrentSourcesDoNotDoubleSpendRefreshToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var mu sync.Mutex
	current := "eort_0"
	refreshes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		got := r.PostForm.Get("refresh_token")
		if got != current {
			// Reuse detected: the real server revokes the family.
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant","error_description":"reuse detected"}`)
			t.Errorf("refresh token %q redeemed after rotation to %q (double spend)", got, current)
			return
		}
		refreshes++
		current = fmt.Sprintf("eort_%d", refreshes)
		fmt.Fprintf(w, `{"access_token":"eoat_%d","refresh_token":%q,"token_type":"Bearer","expires_in":3600}`, refreshes, current)
	}))
	defer srv.Close()

	store := &fileStore{}
	base := "https://api.example"
	rec := Record{
		AccessToken:   "eoat_expired",
		RefreshToken:  "eort_0",
		ExpiresAt:     time.Now().Add(-time.Minute),
		TokenEndpoint: srv.URL,
	}
	if err := store.Set(base, rec); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	tokens := make([]string, 2)
	errs := make([]error, 2)
	for i := range tokens {
		// Separate sources = separate processes: no shared mutex, no
		// shared record; only the disk and the lock file are common.
		src := NewTokenSource(store, base, rec)
		wg.Add(1)
		go func(i int, src *TokenSource) {
			defer wg.Done()
			tokens[i], errs[i] = src.AccessToken(ctx)
		}(i, src)
	}
	wg.Wait()

	for i := range tokens {
		if errs[i] != nil {
			t.Errorf("source %d: %v", i, errs[i])
		}
		if tokens[i] != "eoat_1" {
			t.Errorf("source %d token = %q, want eoat_1 (single refresh, shared result)", i, tokens[i])
		}
	}
	if refreshes != 1 {
		t.Errorf("server refreshes = %d, want 1 (second source adopts the first's rotation)", refreshes)
	}

	persisted, err := store.Get(base)
	if err != nil || persisted == nil {
		t.Fatalf("store.Get = (%+v, %v)", persisted, err)
	}
	if persisted.RefreshToken != "eort_1" {
		t.Errorf("persisted refresh token = %q, want eort_1", persisted.RefreshToken)
	}

	// The lock is released afterwards.
	lockPath, err := refreshLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file should be removed after refresh, stat err = %v", err)
	}
}
