package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"testing"
)

// verifierAlphabet is the RFC 7636 unreserved character set; base64url
// output is a strict subset of it.
var verifierAlphabet = regexp.MustCompile(`^[A-Za-z0-9\-._~]+$`)

func TestNewVerifierShape(t *testing.T) {
	v, err := NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Errorf("verifier length = %d, want 43..128", len(v))
	}
	if !verifierAlphabet.MatchString(v) {
		t.Errorf("verifier %q contains characters outside the RFC 7636 alphabet", v)
	}
}

func TestNewVerifierUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		v, err := NewVerifier()
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		if seen[v] {
			t.Fatalf("duplicate verifier generated: %q", v)
		}
		seen[v] = true
	}
}

func TestChallengeS256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := ChallengeS256(verifier); got != want {
		t.Errorf("ChallengeS256 = %q, want %q", got, want)
	}
	// The RFC 7636 appendix B reference value for this verifier.
	if got := ChallengeS256(verifier); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Errorf("ChallengeS256 = %q, want RFC 7636 reference value", got)
	}
}

func TestNewStateUniqueAndNonEmpty(t *testing.T) {
	a, err := NewState()
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	b, err := NewState()
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if a == "" || b == "" {
		t.Fatal("state must be non-empty")
	}
	if a == b {
		t.Fatalf("two states collided: %q", a)
	}
}
