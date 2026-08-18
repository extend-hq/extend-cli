package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
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
	// warn surfaces non-fatal but user-actionable problems (a rotated
	// refresh token that could not be persisted). Defaults to stderr.
	warn func(format string, args ...any)
}

// NewTokenSource builds a source over a stored record.
func NewTokenSource(store Store, apiBase string, rec Record) *TokenSource {
	return &TokenSource{
		store:   store,
		apiBase: NormalizeBase(apiBase),
		rec:     rec,
		client:  &http.Client{Timeout: 30 * time.Second},
		now:     time.Now,
		warn: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		},
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
		if isGrantRejection(err) {
			return &ReauthError{Cause: err}
		}
		return fmt.Errorf("refresh access token: %w", err)
	}
	newRec := s.rec
	newRec.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		newRec.RefreshToken = tr.RefreshToken
	}
	newRec.ExpiresAt = s.now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	// Persist the rotated pair before handing the new access token out:
	// the old refresh token is dead server-side the moment rotation
	// succeeds, so a pair that exists only in this process's memory
	// strands the login when the process exits. One retry absorbs a
	// transient store hiccup; past that, keep working in memory but
	// tell the user loudly instead of failing (failing would strand
	// them harder — the rotation has already happened).
	persistErr := s.store.Set(s.apiBase, newRec)
	if persistErr != nil {
		persistErr = s.store.Set(s.apiBase, newRec)
	}
	s.rec = newRec
	if persistErr != nil {
		s.warn("! Could not save your refreshed Extend login (%v). The refreshed session is available to this command only; if later commands cannot authenticate, run 'extend login' to sign in again.", persistErr)
	}
	return nil
}

// isGrantRejection reports whether the token endpoint definitively
// rejected the grant itself, meaning re-login is the only way forward.
// Per RFC 6749 that is a 400/401 carrying invalid_grant (dead or
// revoked refresh token) or invalid_client. Everything else — 429s,
// 5xxs, 408s, network timeouts, proxy error pages — is transient and
// must not be presented as an expired login.
func isGrantRejection(err error) bool {
	var te *TokenError
	if !errors.As(err, &te) {
		return false
	}
	if te.StatusCode != http.StatusBadRequest && te.StatusCode != http.StatusUnauthorized {
		return false
	}
	return te.Code == "invalid_grant" || te.Code == "invalid_client"
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
