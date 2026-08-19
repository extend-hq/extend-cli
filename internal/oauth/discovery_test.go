package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverUsesMetadata(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-authorization-server" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{
			"issuer": %[1]q,
			"authorization_endpoint": "%[1]s/custom/authorize",
			"token_endpoint": "%[1]s/custom/token",
			"revocation_endpoint": "%[1]s/custom/revoke"
		}`, srv.URL)
	}))
	defer srv.Close()

	eps, err := Discover(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := srv.URL + "/custom/authorize"; eps.Authorization != want {
		t.Errorf("Authorization = %q, want %q", eps.Authorization, want)
	}
	if want := srv.URL + "/custom/token"; eps.Token != want {
		t.Errorf("Token = %q, want %q", eps.Token, want)
	}
	if want := srv.URL + "/custom/revoke"; eps.Revocation != want {
		t.Errorf("Revocation = %q, want %q", eps.Revocation, want)
	}
}

func TestDiscoverFallsBackOn404(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	eps, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := DefaultEndpoints(srv.URL)
	if eps != want {
		t.Errorf("Discover = %+v, want fallback %+v", eps, want)
	}
	if eps.Authorization != srv.URL+"/oauth2/authorize" {
		t.Errorf("fallback authorize = %q", eps.Authorization)
	}
}

func TestDiscoverFallsBackOnUnreachableServer(t *testing.T) {
	// A closed server yields a connection error.
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	eps, err := Discover(context.Background(), &http.Client{}, base)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := DefaultEndpoints(base); eps != want {
		t.Errorf("Discover = %+v, want fallback %+v", eps, want)
	}
}

func TestDiscoverFillsMissingFieldsFromFallback(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"token_endpoint": "%s/custom/token"}`, srv.URL)
	}))
	defer srv.Close()

	eps, err := Discover(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := srv.URL + "/custom/token"; eps.Token != want {
		t.Errorf("Token = %q, want metadata value %q", eps.Token, want)
	}
	if want := srv.URL + "/oauth2/authorize"; eps.Authorization != want {
		t.Errorf("Authorization = %q, want fallback %q", eps.Authorization, want)
	}
}

func TestDiscoverRejectsForeignHostEndpoints(t *testing.T) {
	cases := map[string]string{
		"foreign token endpoint":     `{"token_endpoint": "https://evil.example/token"}`,
		"foreign authorize endpoint": `{"authorization_endpoint": "https://evil.example/authorize"}`,
		"foreign revocation":         `{"revocation_endpoint": "https://evil.example/revoke"}`,
		"same host different port":   `{"token_endpoint": "https://127.0.0.1:1/token"}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, doc)
			}))
			defer srv.Close()

			_, err := Discover(context.Background(), srv.Client(), srv.URL)
			if err == nil {
				t.Fatal("Discover should reject metadata pointing off the API host")
			}
			if !strings.Contains(err.Error(), "refusing to use it") {
				t.Errorf("err = %v, want a host-pinning rejection", err)
			}
		})
	}
}

func TestDiscoverRejectsSchemeDowngrade(t *testing.T) {
	// An https base must not accept an http endpoint, even on the same
	// host. (Local http dev bases may keep their own scheme; httptest
	// bases are http, so this is asserted through validateEndpointHost
	// directly.)
	err := validateEndpointHost("token_endpoint", "http://api.extend.ai/oauth2/token", "https://api.extend.ai")
	if err == nil {
		t.Fatal("an http endpoint under an https base must be rejected")
	}
	if err := validateEndpointHost("token_endpoint", "https://api.extend.ai/oauth2/token", "https://api.extend.ai"); err != nil {
		t.Errorf("same-host https endpoint should be accepted, got %v", err)
	}
	// A local http dev base keeps its own scheme.
	if err := validateEndpointHost("token_endpoint", "http://127.0.0.1:8080/token", "http://127.0.0.1:8080"); err != nil {
		t.Errorf("http endpoint under an http dev base should be accepted, got %v", err)
	}
}

func TestNormalizeBase(t *testing.T) {
	cases := map[string]string{
		"https://api.extend.ai":    "https://api.extend.ai",
		"https://api.extend.ai/":   "https://api.extend.ai",
		"https://api.extend.ai//":  "https://api.extend.ai",
		" https://api.extend.ai/ ": "https://api.extend.ai",
	}
	for in, want := range cases {
		if got := NormalizeBase(in); got != want {
			t.Errorf("NormalizeBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateBaseURL(t *testing.T) {
	accepted := []string{
		"https://api.extend.ai",
		"https://api.extend.ai/",
		"https://api.internal.example:8443",
		// Loopback hosts may use plain http: local dev servers.
		"http://localhost",
		"http://localhost:3000",
		"http://LOCALHOST:3000",
		"http://127.0.0.1:8080",
		"http://127.5.6.7",
		"http://[::1]:9999",
	}
	for _, base := range accepted {
		if err := ValidateBaseURL(base); err != nil {
			t.Errorf("ValidateBaseURL(%q) = %v, want accepted", base, err)
		}
	}

	rejected := []string{
		// Remote http would carry tokens, codes, and the PKCE
		// verifier in cleartext.
		"http://api.extend.ai",
		"http://api.internal.example",
		"http://api.internal.example:8080",
		"http://192.168.1.10",
		"http://10.0.0.5:3000",
		// Not loopback despite the name.
		"http://localhost.evil.example",
		// Missing or unsupported scheme.
		"api.extend.ai",
		"ftp://api.extend.ai",
	}
	for _, base := range rejected {
		err := ValidateBaseURL(base)
		if err == nil {
			t.Errorf("ValidateBaseURL(%q) = nil, want rejected", base)
			continue
		}
		if !strings.Contains(err.Error(), "https") {
			t.Errorf("ValidateBaseURL(%q) error = %v, want it to point at https", base, err)
		}
	}
}
