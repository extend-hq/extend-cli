package oauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestClientRefusesCrossOriginRedirect covers the redirect pin on every
// body-carrying OAuth call: a 307 to a foreign origin, which the
// default http.Client would follow while replaying the POST body, must
// be refused before the foreign host sees anything.
func TestClientRefusesCrossOriginRedirect(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		t.Errorf("foreign origin received %s %s with body %q; the redirect must not be followed", r.Method, r.URL.Path, body)
	}))
	defer foreign.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	c := &Client{
		HTTPClient: NewHTTPClient(srv.URL),
		Endpoints: Endpoints{
			Token:      srv.URL + "/oauth2/token",
			Revocation: srv.URL + "/oauth2/revoke",
		},
		ClientID: "extend-cli",
		Resource: srv.URL,
	}

	if _, err := c.Exchange(context.Background(), "code1", "verifier1", "http://127.0.0.1:1/cb"); err == nil {
		t.Error("Exchange followed a cross-origin redirect")
	} else if !strings.Contains(err.Error(), "pinned API origin") {
		t.Errorf("Exchange error = %v, want the redirect refusal", err)
	}
	if _, err := c.Refresh(context.Background(), "eort_live"); err == nil {
		t.Error("Refresh followed a cross-origin redirect")
	}
	if err := c.Revoke(context.Background(), "eort_bye"); err == nil {
		t.Error("Revoke followed a cross-origin redirect")
	}
}

// TestDiscoverDoesNotFollowCrossOriginRedirect: discovery shares the
// pinned client; a redirected well-known fetch is treated like any
// other fetch failure and falls back to the default endpoints instead
// of following the redirect.
func TestDiscoverDoesNotFollowCrossOriginRedirect(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("foreign origin received %s %s; the redirect must not be followed", r.Method, r.URL.Path)
	}))
	defer foreign.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	eps, err := Discover(context.Background(), NewHTTPClient(srv.URL), srv.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if eps != DefaultEndpoints(srv.URL) {
		t.Errorf("endpoints = %+v, want the defaults for %s", eps, srv.URL)
	}
}

func TestClientFollowsSameOriginRedirect(t *testing.T) {
	var gotForm url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/token-v2", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/token-v2", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotForm = r.PostForm
		fmt.Fprint(w, `{"access_token":"eoat_new","refresh_token":"eort_new","token_type":"Bearer","expires_in":3600}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{
		HTTPClient: NewHTTPClient(srv.URL),
		Endpoints:  Endpoints{Token: srv.URL + "/oauth2/token"},
		ClientID:   "extend-cli",
	}
	tr, err := c.Refresh(context.Background(), "eort_old")
	if err != nil {
		t.Fatalf("Refresh through a same-origin redirect: %v", err)
	}
	if tr.AccessToken != "eoat_new" {
		t.Errorf("access token = %q", tr.AccessToken)
	}
	if gotForm.Get("refresh_token") != "eort_old" {
		t.Errorf("redirected form = %v, want the replayed refresh grant", gotForm)
	}
}

// TestClientRevalidatesEveryRedirectHop: an allowed same-origin hop
// must not open the door for a later cross-origin hop in the same
// chain.
func TestClientRevalidatesEveryRedirectHop(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("foreign origin received %s %s; hop revalidation failed", r.Method, r.URL.Path)
	}))
	defer foreign.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/hop", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/hop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/oauth2/token", http.StatusTemporaryRedirect)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{
		HTTPClient: NewHTTPClient(srv.URL),
		Endpoints:  Endpoints{Token: srv.URL + "/oauth2/token"},
		ClientID:   "extend-cli",
	}
	if _, err := c.Refresh(context.Background(), "eort_live"); err == nil {
		t.Error("Refresh followed a chain ending on a foreign origin")
	}
}
