package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// expirySkew is how early a token is treated as expired, absorbing
// clock drift and request latency so a token never dies mid-flight.
const expirySkew = 60 * time.Second

// ReauthError signals that the stored login can no longer mint access
// tokens (expired or revoked refresh token) and the user must run
// `extend login` again. The CLI's error printer renders it verbatim.
type ReauthError struct {
	Cause error
}

func (e *ReauthError) Error() string {
	return "your Extend login has expired or was revoked; run 'extend login' to sign in again"
}

func (e *ReauthError) Unwrap() error { return e.Cause }

// TokenSource yields valid access tokens for one stored login,
// refreshing through the token endpoint when the cached token is
// expired (or rejected) and persisting every rotated refresh token
// before releasing the new access token. Safe for concurrent use;
// batch commands issue parallel requests through one source.
type TokenSource struct {
	mu      sync.Mutex
	store   Store
	apiBase string
	rec     Record
	client  *http.Client
	now     func() time.Time
}

// NewTokenSource builds a source over a stored record.
func NewTokenSource(store Store, apiBase string, rec Record) *TokenSource {
	return &TokenSource{
		store:   store,
		apiBase: NormalizeBase(apiBase),
		rec:     rec,
		client:  &http.Client{Timeout: 30 * time.Second},
		now:     time.Now,
	}
}

// AccessToken returns a currently valid access token, refreshing first
// when the cached one is at or past its expiry skew window.
func (s *TokenSource) AccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec.AccessToken != "" && s.now().Add(expirySkew).Before(s.rec.ExpiresAt) {
		return s.rec.AccessToken, nil
	}
	if err := s.refreshLocked(ctx); err != nil {
		return "", err
	}
	return s.rec.AccessToken, nil
}

// ForceRefresh discards the access token the caller just saw rejected
// (a 401 despite a fresh-looking expiry) and refreshes. When a
// concurrent request already refreshed, the newer token is returned
// without another round trip.
func (s *TokenSource) ForceRefresh(ctx context.Context, rejected string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec.AccessToken != "" && s.rec.AccessToken != rejected {
		return s.rec.AccessToken, nil
	}
	if err := s.refreshLocked(ctx); err != nil {
		return "", err
	}
	return s.rec.AccessToken, nil
}

// refreshLocked performs one refresh grant and persists the rotated
// token pair. Callers hold s.mu.
func (s *TokenSource) refreshLocked(ctx context.Context) error {
	if s.rec.RefreshToken == "" {
		return &ReauthError{Cause: errors.New("no refresh token stored")}
	}
	resource := s.rec.Resource
	if resource == "" {
		resource = s.apiBase
	}
	c := &Client{
		HTTPClient: s.client,
		Endpoints:  s.endpoints(),
		ClientID:   s.clientID(),
		Resource:   resource,
	}
	tr, err := c.Refresh(ctx, s.rec.RefreshToken)
	if err != nil {
		var te *TokenError
		if errors.As(err, &te) && te.StatusCode >= 400 && te.StatusCode < 500 {
			return &ReauthError{Cause: err}
		}
		return fmt.Errorf("refresh access token: %w", err)
	}
	s.rec.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		s.rec.RefreshToken = tr.RefreshToken
	}
	s.rec.ExpiresAt = s.now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	// Persisting the rotated refresh token is not optional: the old one
	// is dead server-side, so losing the new one strands the login.
	if err := s.store.Set(s.apiBase, s.rec); err != nil {
		return fmt.Errorf("persist rotated refresh token: %w", err)
	}
	return nil
}

func (s *TokenSource) endpoints() Endpoints {
	eps := DefaultEndpoints(s.apiBase)
	if s.rec.TokenEndpoint != "" {
		eps.Token = s.rec.TokenEndpoint
	}
	if s.rec.RevocationEndpoint != "" {
		eps.Revocation = s.rec.RevocationEndpoint
	}
	return eps
}

func (s *TokenSource) clientID() string {
	if s.rec.ClientID != "" {
		return s.rec.ClientID
	}
	return DefaultClientID
}
