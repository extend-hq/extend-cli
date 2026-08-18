package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	eps := Discover(context.Background(), srv.Client(), srv.URL)
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

	eps := Discover(context.Background(), srv.Client(), srv.URL+"/")
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

	eps := Discover(context.Background(), &http.Client{}, base)
	if want := DefaultEndpoints(base); eps != want {
		t.Errorf("Discover = %+v, want fallback %+v", eps, want)
	}
}

func TestDiscoverFillsMissingFieldsFromFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token_endpoint": "https://tokens.example/token"}`)
	}))
	defer srv.Close()

	eps := Discover(context.Background(), srv.Client(), srv.URL)
	if eps.Token != "https://tokens.example/token" {
		t.Errorf("Token = %q, want metadata value", eps.Token)
	}
	if want := srv.URL + "/oauth2/authorize"; eps.Authorization != want {
		t.Errorf("Authorization = %q, want fallback %q", eps.Authorization, want)
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
