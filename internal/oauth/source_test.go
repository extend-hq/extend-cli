package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// memStore is an in-memory Store for source tests.
type memStore struct {
	mu   sync.Mutex
	recs map[string]Record
}

func newMemStore() *memStore { return &memStore{recs: map[string]Record{}} }

func (m *memStore) Get(apiBase string) (*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.recs[NormalizeBase(apiBase)]
	if !ok {
		return nil, nil
	}
	return &rec, nil
}

func (m *memStore) Set(apiBase string, rec Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recs[NormalizeBase(apiBase)] = rec
	return nil
}

func (m *memStore) Delete(apiBase string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.recs, NormalizeBase(apiBase))
	return nil
}

// newRefreshServer returns a token endpoint that rotates tokens on each
// refresh and counts calls.
func newRefreshServer(t *testing.T, wantRefreshToken string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		if got := r.PostForm.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q", got)
		}
		if got := r.PostForm.Get("refresh_token"); got != wantRefreshToken {
			t.Errorf("refresh_token = %q, want %q", got, wantRefreshToken)
		}
		fmt.Fprintf(w, `{"access_token":"eoat_new_%[1]d","refresh_token":"eort_new_%[1]d","token_type":"Bearer","expires_in":3600}`, calls)
	}))
	return srv, &calls
}

func sourceWithRecord(store Store, base string, rec Record) *TokenSource {
	s := NewTokenSource(store, base, rec)
	return s
}

func TestAccessTokenFreshTokenNoRefresh(t *testing.T) {
	store := newMemStore()
	rec := Record{
		AccessToken:  "eoat_fresh",
		RefreshToken: "eort_r",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	s := sourceWithRecord(store, "https://api.example", rec)
	tok, err := s.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "eoat_fresh" {
		t.Errorf("token = %q, want the cached one", tok)
	}
}

func TestAccessTokenRefreshesExpiredAndPersistsRotation(t *testing.T) {
	srv, calls := newRefreshServer(t, "eort_old")
	defer srv.Close()
	store := newMemStore()
	base := "https://api.example"
	rec := Record{
		AccessToken:   "eoat_expired",
		RefreshToken:  "eort_old",
		ExpiresAt:     time.Now().Add(-time.Minute),
		TokenEndpoint: srv.URL,
		ClientID:      "extend-cli",
		Resource:      base,
	}
	s := sourceWithRecord(store, base, rec)

	tok, err := s.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "eoat_new_1" {
		t.Errorf("token = %q, want eoat_new_1", tok)
	}
	if *calls != 1 {
		t.Errorf("refresh calls = %d, want 1", *calls)
	}

	persisted, err := store.Get(base)
	if err != nil || persisted == nil {
		t.Fatalf("store.Get = (%v, %v)", persisted, err)
	}
	if persisted.RefreshToken != "eort_new_1" {
		t.Errorf("persisted refresh token = %q, want the rotated eort_new_1", persisted.RefreshToken)
	}
	if persisted.AccessToken != "eoat_new_1" {
		t.Errorf("persisted access token = %q", persisted.AccessToken)
	}
	if !persisted.ExpiresAt.After(time.Now().Add(30 * time.Minute)) {
		t.Errorf("persisted expiry %v not advanced", persisted.ExpiresAt)
	}

	// A second call within the fresh window must not refresh again.
	if _, err := s.AccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Errorf("refresh calls after cached read = %d, want 1", *calls)
	}
}

func TestAccessTokenWithinSkewRefreshes(t *testing.T) {
	srv, calls := newRefreshServer(t, "eort_old")
	defer srv.Close()
	store := newMemStore()
	rec := Record{
		AccessToken:   "eoat_dying",
		RefreshToken:  "eort_old",
		ExpiresAt:     time.Now().Add(10 * time.Second), // inside the 60s skew
		TokenEndpoint: srv.URL,
	}
	s := sourceWithRecord(store, "https://api.example", rec)
	if _, err := s.AccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Errorf("a token inside the expiry skew should refresh; calls = %d", *calls)
	}
}

func TestForceRefreshReusesConcurrentRefresh(t *testing.T) {
	srv, calls := newRefreshServer(t, "eort_old")
	defer srv.Close()
	store := newMemStore()
	rec := Record{
		AccessToken:   "eoat_rejected",
		RefreshToken:  "eort_old",
		ExpiresAt:     time.Now().Add(time.Hour),
		TokenEndpoint: srv.URL,
	}
	s := sourceWithRecord(store, "https://api.example", rec)

	tok, err := s.ForceRefresh(context.Background(), "eoat_rejected")
	if err != nil {
		t.Fatalf("ForceRefresh: %v", err)
	}
	if tok != "eoat_new_1" || *calls != 1 {
		t.Fatalf("first ForceRefresh = %q (calls %d)", tok, *calls)
	}

	// A second caller still holding the stale token gets the new one
	// without another round trip.
	tok, err = s.ForceRefresh(context.Background(), "eoat_rejected")
	if err != nil {
		t.Fatalf("second ForceRefresh: %v", err)
	}
	if tok != "eoat_new_1" {
		t.Errorf("token = %q, want reuse of eoat_new_1", tok)
	}
	if *calls != 1 {
		t.Errorf("refresh calls = %d, want 1 (no double refresh)", *calls)
	}
}

func TestRefreshRejectionYieldsReauthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"grant revoked"}`)
	}))
	defer srv.Close()
	store := newMemStore()
	rec := Record{
		AccessToken:   "eoat_expired",
		RefreshToken:  "eort_dead",
		ExpiresAt:     time.Now().Add(-time.Minute),
		TokenEndpoint: srv.URL,
	}
	s := sourceWithRecord(store, "https://api.example", rec)

	_, err := s.AccessToken(context.Background())
	var reauth *ReauthError
	if !errors.As(err, &reauth) {
		t.Fatalf("err = %v, want *ReauthError", err)
	}
	var te *TokenError
	if !errors.As(err, &te) || te.Code != "invalid_grant" {
		t.Errorf("ReauthError should wrap the TokenError cause, got %v", err)
	}
}

func TestMissingRefreshTokenYieldsReauthError(t *testing.T) {
	s := sourceWithRecord(newMemStore(), "https://api.example", Record{
		AccessToken: "eoat_expired",
		ExpiresAt:   time.Now().Add(-time.Minute),
	})
	_, err := s.AccessToken(context.Background())
	var reauth *ReauthError
	if !errors.As(err, &reauth) {
		t.Fatalf("err = %v, want *ReauthError", err)
	}
}
