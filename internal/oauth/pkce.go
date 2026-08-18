package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// randomToken returns n cryptographically random bytes encoded as
// unpadded base64url, the alphabet RFC 7636 permits for verifiers and
// the natural choice for state values.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NewVerifier returns a fresh PKCE code verifier: 32 random bytes as
// base64url, yielding 43 characters, the RFC 7636 minimum length and
// the standard choice for S256.
func NewVerifier() (string, error) {
	return randomToken(32)
}

// ChallengeS256 derives the code challenge for a verifier using the
// S256 method: BASE64URL(SHA256(verifier)) without padding.
func ChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// NewState returns a fresh CSRF (cross-site request forgery) state
// value for the authorization request. It is verified against the
// state echoed back on the loopback redirect.
func NewState() (string, error) {
	return randomToken(32)
}
