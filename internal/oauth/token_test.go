package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestExchangeSendsContractFields(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		got = r.PostForm
		fmt.Fprint(w, `{"access_token":"eoat_abc","refresh_token":"eort_def","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	c := &Client{
		HTTPClient: srv.Client(),
		Endpoints:  Endpoints{Token: srv.URL + "/oauth2/token"},
		ClientID:   "extend-cli",
		Resource:   "https://api.extend.ai",
	}
	tr, err := c.Exchange(context.Background(), "code1", "verifier1", "http://127.0.0.1:1234/callback")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	want := map[string]string{
		"grant_type":    "authorization_code",
		"code":          "code1",
		"code_verifier": "verifier1",
		"redirect_uri":  "http://127.0.0.1:1234/callback",
		"client_id":     "extend-cli",
		"resource":      "https://api.extend.ai",
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("form[%s] = %q, want %q", k, got.Get(k), v)
		}
	}
	if tr.AccessToken != "eoat_abc" || tr.RefreshToken != "eort_def" || tr.ExpiresIn != 3600 {
		t.Errorf("token response = %+v", tr)
	}
}

func TestRefreshSendsContractFields(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.PostForm
		fmt.Fprint(w, `{"access_token":"eoat_new","refresh_token":"eort_new","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	c := &Client{
		HTTPClient: srv.Client(),
		Endpoints:  Endpoints{Token: srv.URL},
		ClientID:   "extend-cli",
		Resource:   "https://api.extend.ai",
	}
	tr, err := c.Refresh(context.Background(), "eort_old")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	want := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": "eort_old",
		"client_id":     "extend-cli",
		"resource":      "https://api.extend.ai",
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("form[%s] = %q, want %q", k, got.Get(k), v)
		}
	}
	if tr.RefreshToken != "eort_new" {
		t.Errorf("rotated refresh token = %q, want eort_new", tr.RefreshToken)
	}
}

func TestTokenErrorParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"refresh token revoked"}`)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), Endpoints: Endpoints{Token: srv.URL}, ClientID: "extend-cli"}
	_, err := c.Refresh(context.Background(), "eort_dead")
	var te *TokenError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v, want *TokenError", err)
	}
	if te.Code != "invalid_grant" || te.StatusCode != 400 {
		t.Errorf("TokenError = %+v", te)
	}
	if te.Description != "refresh token revoked" {
		t.Errorf("Description = %q", te.Description)
	}
}

func TestRevoke(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.PostForm
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &Client{
		HTTPClient: srv.Client(),
		Endpoints:  Endpoints{Revocation: srv.URL + "/oauth2/revoke"},
		ClientID:   "extend-cli",
	}
	if err := c.Revoke(context.Background(), "eort_bye"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if got.Get("token") != "eort_bye" || got.Get("token_type_hint") != "refresh_token" {
		t.Errorf("revoke form = %v", got)
	}
}
