package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/iostreams"
	"github.com/extend-hq/extend-cli/internal/oauth"
)

// loginTestEnv isolates every configuration source runLogin consults:
// config dir, token store (forced to the file fallback), and the
// EXTEND_* environment.
func loginTestEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(oauth.EnvNoKeyring, "1")
	t.Setenv("EXTEND_BASE_URL", baseURL)
	// The fake server plays both the authorization server (issuer) and
	// the API; in production these are different origins.
	t.Setenv("EXTEND_OAUTH_ISSUER", baseURL)
	t.Setenv("EXTEND_OAUTH_CLIENT_ID", "")
	t.Setenv("EXTEND_API_KEY", "")
	t.Setenv("EXTEND_REGION", "")
	t.Setenv("EXTEND_WORKSPACE_ID", "")
	t.Setenv("EXTEND_ENV", "")
}

// fakeAuthServer implements the token endpoint side of the contract:
// it verifies the PKCE verifier against the challenge captured from
// the authorize URL and returns eoat_/eort_ tokens.
type fakeAuthServer struct {
	srv *httptest.Server
	// challenge and redirectURI are captured by the fake browser from
	// the authorize URL.
	challenge   string
	redirectURI string
	// exchanged records the code redeemed at the token endpoint.
	exchanged string
	// accessToken overrides the token endpoint's access_token when set.
	accessToken string
	// revoked records the bearer tokens presented to /oauth/revoke-current.
	revoked []string
	// meHandler, when set, serves GET /me; unset answers 404 so tests
	// exercise the generic-success fallback by default.
	meHandler http.HandlerFunc
	// meAuth records the Authorization header of the last /me call.
	meAuth string
}

func newFakeAuthServer(t *testing.T) *fakeAuthServer {
	t.Helper()
	f := &fakeAuthServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if got := r.PostForm.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q", got)
		}
		if got := r.PostForm.Get("resource"); got != f.srv.URL {
			t.Errorf("resource = %q, want %q", got, f.srv.URL)
		}
		if got := r.PostForm.Get("redirect_uri"); got != f.redirectURI {
			t.Errorf("redirect_uri = %q, want %q", got, f.redirectURI)
		}
		verifier := r.PostForm.Get("code_verifier")
		if oauth.ChallengeS256(verifier) != f.challenge {
			t.Errorf("code_verifier does not match the challenge sent to authorize")
		}
		f.exchanged = r.PostForm.Get("code")
		accessToken := f.accessToken
		if accessToken == "" {
			accessToken = "eoat_test"
		}
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"eort_test","token_type":"Bearer","expires_in":3600}`, accessToken)
	})
	mux.HandleFunc("/oauth/revoke-current", func(w http.ResponseWriter, r *http.Request) {
		f.revoked = append(f.revoked, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		f.meAuth = r.Header.Get("Authorization")
		if f.meHandler != nil {
			f.meHandler(w, r)
			return
		}
		http.NotFound(w, r)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// browserFor returns an openBrowser stub that plays the user approving
// consent: it captures the PKCE challenge, then redirects back to the
// loopback with the given code and state (state == "" echoes the real
// one).
func (f *fakeAuthServer) browserFor(t *testing.T, code, stateOverride string) func(string) error {
	t.Helper()
	return func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		if got := q.Get("client_id"); got != oauth.DefaultClientID {
			t.Errorf("client_id = %q", got)
		}
		if got := q.Get("code_challenge_method"); got != "S256" {
			t.Errorf("code_challenge_method = %q", got)
		}
		if got := q.Get("resource"); got != f.srv.URL {
			t.Errorf("authorize resource = %q, want %q", got, f.srv.URL)
		}
		f.challenge = q.Get("code_challenge")
		f.redirectURI = q.Get("redirect_uri")
		state := stateOverride
		if state == "" {
			state = q.Get("state")
		}
		cb := fmt.Sprintf("%s?code=%s&state=%s", f.redirectURI, url.QueryEscape(code), url.QueryEscape(state))
		// The redirect happens on the browser's own timeline.
		go http.Get(cb) //nolint:errcheck
		return nil
	}
}

func testAppForLogin() (*App, func() string) {
	ios, _, _, errBuf := iostreams.Test()
	app := &App{IO: ios}
	return app, errBuf.String
}

func TestRunLoginHappyPath(t *testing.T) {
	f := newFakeAuthServer(t)
	loginTestEnv(t, f.srv.URL)
	app, stderr := testAppForLogin()

	store := oauth.DefaultStore()
	err := runLogin(context.Background(), app, loginOptions{
		openBrowser: f.browserFor(t, "code-xyz", ""),
		store:       store,
	})
	if err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if f.exchanged != "code-xyz" {
		t.Errorf("exchanged code = %q, want code-xyz", f.exchanged)
	}

	rec, err := store.Get(f.srv.URL)
	if err != nil || rec == nil {
		t.Fatalf("stored record = (%+v, %v)", rec, err)
	}
	if rec.AccessToken != "eoat_test" || rec.RefreshToken != "eort_test" {
		t.Errorf("stored tokens = %+v", rec)
	}
	if rec.Resource != f.srv.URL {
		t.Errorf("stored resource = %q, want %q", rec.Resource, f.srv.URL)
	}
	if !rec.ExpiresAt.After(time.Now().Add(30 * time.Minute)) {
		t.Errorf("stored expiry %v not in the future", rec.ExpiresAt)
	}

	// The fake serves no /me, so the success line is the generic one.
	out := stderr()
	if !strings.Contains(out, "Signed in to "+f.srv.URL) {
		t.Errorf("stderr missing success line: %q", out)
	}
	if strings.Contains(out, "eoat_") || strings.Contains(out, "eort_") {
		t.Errorf("stderr leaked a token: %q", out)
	}
}

func TestRunLoginPersonalizedSuccessLine(t *testing.T) {
	f := newFakeAuthServer(t)
	f.meHandler = func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"object": "me",
			"organization": {"id": "org_1", "name": "Acme Inc"},
			"workspace": {"id": "ws_1", "name": "Acme Corp"},
			"user": {"id": "user_1", "email": "jam@example.com"},
			"grantedTargets": [
				{"workspace": {"id": "ws_1", "name": "Acme Corp"}, "environments": ["PRODUCTION"]}
			]
		}`)
	}
	loginTestEnv(t, f.srv.URL)
	app, stderr := testAppForLogin()

	err := runLogin(context.Background(), app, loginOptions{
		openBrowser: f.browserFor(t, "code-xyz", ""),
		store:       oauth.DefaultStore(),
	})
	if err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if f.meAuth != "Bearer eoat_test" {
		t.Errorf("/me Authorization = %q, want the fresh access token", f.meAuth)
	}
	out := stderr()
	if !strings.Contains(out, "Signed in to Acme Corp (Production) as jam@example.com on "+f.srv.URL+".") {
		t.Errorf("stderr missing personalized success line: %q", out)
	}
}

// TestEffectiveBaseURLRejectsCleartextRemote: an http base to a remote
// host would run the whole handshake (verifier, code, tokens) in
// cleartext, so login/logout/refresh must refuse it before any request
// is made. Loopback http keeps working for local dev servers.
func TestEffectiveBaseURLRejectsCleartextRemote(t *testing.T) {
	_, err := effectiveBaseURL(resolved{baseURL: sourced{val: "http://api.internal.example", src: "env"}})
	if err == nil {
		t.Fatal("effectiveBaseURL(http remote) = nil error, want a cleartext refusal")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("error = %v, want it to point at https", err)
	}

	for _, base := range []string{"http://localhost:3000", "http://127.0.0.1:8080", "http://[::1]:8080", "https://api.internal.example"} {
		got, err := effectiveBaseURL(resolved{baseURL: sourced{val: base, src: "env"}})
		if err != nil {
			t.Errorf("effectiveBaseURL(%q) = %v, want accepted", base, err)
		}
		if got != base {
			t.Errorf("effectiveBaseURL(%q) = %q", base, got)
		}
	}
}

func TestRunLoginSanitizesMeOutput(t *testing.T) {
	f := newFakeAuthServer(t)
	f.meHandler = func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"workspace": {"id": "ws_1", "name": "\u001b[31mEvil\u001b[0m Corp"},
			"user": {"id": "user_1", "email": "a@b.c\u001b]0;pwned\u0007"},
			"grantedTargets": [
				{"workspace": {"id": "ws_1"}, "environments": ["TEST"]}
			]
		}`)
	}
	loginTestEnv(t, f.srv.URL)
	app, stderr := testAppForLogin()

	err := runLogin(context.Background(), app, loginOptions{
		openBrowser: f.browserFor(t, "code-xyz", ""),
		store:       oauth.DefaultStore(),
	})
	if err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	out := stderr()
	if strings.Contains(out, "\x1b[31m") || strings.Contains(out, "\x1b]0;") || strings.Contains(out, "pwned") {
		t.Errorf("stderr leaked an escape sequence from /me: %q", out)
	}
	if !strings.Contains(out, "Signed in to Evil Corp (Test) as a@b.c on "+f.srv.URL+".") {
		t.Errorf("stderr missing the sanitized success line: %q", out)
	}
}

func TestRunLoginMeFailureFallsBackToGenericLine(t *testing.T) {
	f := newFakeAuthServer(t)
	f.meHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"INTERNAL","message":"boom"}`, http.StatusInternalServerError)
	}
	loginTestEnv(t, f.srv.URL)
	app, stderr := testAppForLogin()

	store := oauth.DefaultStore()
	err := runLogin(context.Background(), app, loginOptions{
		openBrowser: f.browserFor(t, "code-xyz", ""),
		store:       store,
	})
	if err != nil {
		t.Fatalf("a /me failure must not fail the login, got %v", err)
	}
	if rec, _ := store.Get(f.srv.URL); rec == nil || rec.AccessToken != "eoat_test" {
		t.Errorf("tokens should be stored despite the /me failure, got %+v", rec)
	}
	if out := stderr(); !strings.Contains(out, "Signed in to "+f.srv.URL) {
		t.Errorf("stderr missing generic fallback line: %q", out)
	}
}

func TestLoginSuccessLine(t *testing.T) {
	// A base that is not an advertised region is echoed in personalized
	// lines: the environment parenthetical alone would misread as the
	// production deployment when signed in to staging or a rig.
	cases := []struct {
		name string
		id   *loginIdentity
		want string
	}{
		{"nil identity", nil, "Signed in to https://api.example."},
		{"no workspace", &loginIdentity{email: "a@b.c"}, "Signed in to https://api.example."},
		{"full", &loginIdentity{workspace: "Acme", environment: "TEST", email: "a@b.c"}, "Signed in to Acme (Test) as a@b.c on https://api.example."},
		{"unknown env", &loginIdentity{workspace: "Acme", environment: "WEIRD", email: "a@b.c"}, "Signed in to Acme as a@b.c on https://api.example."},
		{"no email", &loginIdentity{workspace: "Acme", environment: "PRODUCTION"}, "Signed in to Acme (Production) on https://api.example."},
	}
	for _, tc := range cases {
		if got := loginSuccessLine("https://api.example", tc.id); got != tc.want {
			t.Errorf("%s: loginSuccessLine = %q, want %q", tc.name, got, tc.want)
		}
	}

	usBase, ok := extendx.RegionBaseURL("us")
	if !ok {
		t.Fatal("region us must resolve")
	}
	id := &loginIdentity{workspace: "Acme", environment: "PRODUCTION", email: "a@b.c"}
	if got, want := loginSuccessLine(usBase, id), "Signed in to Acme (Production) as a@b.c."; got != want {
		t.Errorf("region base: loginSuccessLine = %q, want %q (no base suffix)", got, want)
	}
}

func TestRunLoginIgnoresForgedCallback(t *testing.T) {
	f := newFakeAuthServer(t)
	loginTestEnv(t, f.srv.URL)
	app, _ := testAppForLogin()

	// A forged callback (wrong state, from some other local process)
	// fires before the real redirect. It must be ignored: the pending
	// login completes with the real code, not aborted or hijacked.
	openBrowser := func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		f.challenge = q.Get("code_challenge")
		f.redirectURI = q.Get("redirect_uri")
		go func() {
			forged := fmt.Sprintf("%s?code=evil-code&state=attacker-state", f.redirectURI)
			if resp, err := http.Get(forged); err == nil {
				resp.Body.Close()
			}
			real := fmt.Sprintf("%s?code=code-xyz&state=%s", f.redirectURI, url.QueryEscape(q.Get("state")))
			if resp, err := http.Get(real); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	store := oauth.DefaultStore()
	err := runLogin(context.Background(), app, loginOptions{
		openBrowser: openBrowser,
		store:       store,
	})
	if err != nil {
		t.Fatalf("a forged callback must not abort the login, got %v", err)
	}
	if f.exchanged != "code-xyz" {
		t.Errorf("exchanged code = %q, want the real code-xyz (never the forged one)", f.exchanged)
	}
	if rec, _ := store.Get(f.srv.URL); rec == nil || rec.AccessToken != "eoat_test" {
		t.Errorf("tokens should be stored from the real callback, got %+v", rec)
	}
}

func TestRunLoginAuthorizationDenied(t *testing.T) {
	f := newFakeAuthServer(t)
	loginTestEnv(t, f.srv.URL)
	app, _ := testAppForLogin()

	openBrowser := func(authURL string) error {
		u, _ := url.Parse(authURL)
		q := u.Query()
		cb := fmt.Sprintf("%s?error=access_denied&error_description=%s&state=%s",
			q.Get("redirect_uri"), url.QueryEscape("the user declined"), url.QueryEscape(q.Get("state")))
		go http.Get(cb) //nolint:errcheck
		return nil
	}
	err := runLogin(context.Background(), app, loginOptions{
		openBrowser: openBrowser,
		store:       oauth.DefaultStore(),
	})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v, want access_denied", err)
	}
}

func TestRunLoginNoBrowserPrintsURL(t *testing.T) {
	f := newFakeAuthServer(t)
	loginTestEnv(t, f.srv.URL)
	app, stderr := testAppForLogin()

	// Cancel quickly: this test only checks the printed URL, not the
	// full round trip.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := runLogin(ctx, app, loginOptions{
		noBrowser: true,
		openBrowser: func(string) error {
			t.Error("openBrowser must not be called with --no-browser")
			return nil
		},
		store: oauth.DefaultStore(),
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	out := stderr()
	if !strings.Contains(out, f.srv.URL+"/oauth2/authorize?") {
		t.Errorf("stderr should print the authorize URL, got %q", out)
	}
}

// Re-login must never revoke the grant it replaces: /oauth/revoke-current
// deletes the CLI client's whole WorkOS authorization, which the new
// login shares, so revoking the old grant would kill the new session's
// refresh ability too.
func TestRunLoginDoesNotRevokeReplacedGrant(t *testing.T) {
	f := newFakeAuthServer(t)
	loginTestEnv(t, f.srv.URL)
	app, _ := testAppForLogin()

	store := oauth.DefaultStore()
	if err := store.Set(f.srv.URL, oauth.Record{
		AccessToken:  "eoat_old",
		RefreshToken: "eort_old",
		ExpiresAt:    time.Now().Add(time.Hour),
		ClientID:     oauth.DefaultClientID,
		Resource:     f.srv.URL,
	}); err != nil {
		t.Fatal(err)
	}

	err := runLogin(context.Background(), app, loginOptions{
		openBrowser: f.browserFor(t, "code-xyz", ""),
		store:       store,
	})
	if err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if len(f.revoked) != 0 {
		t.Errorf("revoked = %v, want none: revoking a replaced grant would delete the shared authorization", f.revoked)
	}
	if rec, _ := store.Get(f.srv.URL); rec == nil || rec.RefreshToken != "eort_test" {
		t.Errorf("stored record = %+v, want the new login", rec)
	}
}

func TestRunWhoami(t *testing.T) {
	f := newFakeAuthServer(t)
	f.meHandler = func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"workspace": {"id": "ws_1", "name": "Acme Corp"},
			"user": {"id": "user_1", "email": "jam@example.com"},
			"grantedTargets": [
				{"workspace": {"id": "ws_1"}, "environments": ["PRODUCTION"]}
			]
		}`)
	}
	loginTestEnv(t, f.srv.URL)

	t.Run("no credentials", func(t *testing.T) {
		ios, _, _, _ := iostreams.Test()
		err := runWhoami(context.Background(), &App{IO: ios})
		if err == nil || !strings.Contains(err.Error(), "not logged in") {
			t.Fatalf("err = %v, want the unconfigured-credentials error", err)
		}
	})

	t.Run("stored login", func(t *testing.T) {
		if err := oauth.DefaultStore().Set(f.srv.URL, oauth.Record{
			AccessToken:  "eoat_x",
			RefreshToken: "eort_x",
			ExpiresAt:    time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
		ios, _, out, _ := iostreams.Test()
		if err := runWhoami(context.Background(), &App{IO: ios}); err != nil {
			t.Fatalf("runWhoami: %v", err)
		}
		got := out.String()
		for _, want := range []string{"Acme Corp (Production)", "jam@example.com", "OAuth login", f.srv.URL} {
			if !strings.Contains(got, want) {
				t.Errorf("stdout missing %q: %q", want, got)
			}
		}
		if f.meAuth != "Bearer eoat_x" {
			t.Errorf("/me Authorization = %q, want the stored access token", f.meAuth)
		}
	})

	t.Run("api key wins", func(t *testing.T) {
		t.Setenv("EXTEND_API_KEY", "sk_whoami")
		ios, _, out, _ := iostreams.Test()
		if err := runWhoami(context.Background(), &App{IO: ios}); err != nil {
			t.Fatalf("runWhoami: %v", err)
		}
		if f.meAuth != "Bearer sk_whoami" {
			t.Errorf("/me Authorization = %q, want the API key", f.meAuth)
		}
		if got := out.String(); !strings.Contains(got, "API key") {
			t.Errorf("stdout should name the API key method: %q", got)
		}
	})
}

func TestRunLogoutRevokesAndClears(t *testing.T) {
	f := newFakeAuthServer(t)
	loginTestEnv(t, f.srv.URL)
	app, stderr := testAppForLogin()

	store := oauth.DefaultStore()
	if err := store.Set(f.srv.URL, oauth.Record{
		AccessToken:  "eoat_x",
		RefreshToken: "eort_x",
		ExpiresAt:    time.Now().Add(time.Hour),
		ClientID:     oauth.DefaultClientID,
		Resource:     f.srv.URL,
	}); err != nil {
		t.Fatal(err)
	}

	if err := runLogout(context.Background(), app, store); err != nil {
		t.Fatalf("runLogout: %v", err)
	}
	if len(f.revoked) != 1 || f.revoked[0] != "eoat_x" {
		t.Errorf("revoked = %v, want the login's access token", f.revoked)
	}
	if rec, _ := store.Get(f.srv.URL); rec != nil {
		t.Errorf("record should be cleared, got %+v", rec)
	}
	if out := stderr(); !strings.Contains(out, "Logged out of "+f.srv.URL) {
		t.Errorf("stderr = %q", out)
	}
}

func TestRunLogoutWhenNotLoggedIn(t *testing.T) {
	f := newFakeAuthServer(t)
	loginTestEnv(t, f.srv.URL)
	app, stderr := testAppForLogin()

	if err := runLogout(context.Background(), app, oauth.DefaultStore()); err != nil {
		t.Fatalf("logout while logged out should succeed, got %v", err)
	}
	if out := stderr(); !strings.Contains(out, "Not logged in") {
		t.Errorf("stderr = %q", out)
	}
}

func TestRunLogoutClearsLocallyWhenRevokeFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	loginTestEnv(t, srv.URL)
	app, stderr := testAppForLogin()

	store := oauth.DefaultStore()
	if err := store.Set(srv.URL, oauth.Record{
		AccessToken:  "eoat_x",
		RefreshToken: "eort_x",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	if err := runLogout(context.Background(), app, store); err != nil {
		t.Fatalf("runLogout: %v", err)
	}
	if rec, _ := store.Get(srv.URL); rec != nil {
		t.Errorf("local tokens should be cleared despite revoke failure, got %+v", rec)
	}
	if out := stderr(); !strings.Contains(out, "clearing local tokens anyway") {
		t.Errorf("stderr = %q", out)
	}
}

func TestResolveOAuthSource(t *testing.T) {
	f := newFakeAuthServer(t)
	loginTestEnv(t, f.srv.URL)

	t.Run("no login stored", func(t *testing.T) {
		if src, _ := resolveOAuthSource("", resolved{baseURL: sourced{val: f.srv.URL}}, io.Discard); src != nil {
			t.Error("expected nil source with empty store")
		}
	})

	if err := oauth.DefaultStore().Set(f.srv.URL, oauth.Record{
		AccessToken:  "eoat_x",
		RefreshToken: "eort_x",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	t.Run("login stored", func(t *testing.T) {
		src, _ := resolveOAuthSource("", resolved{baseURL: sourced{val: f.srv.URL}}, io.Discard)
		if src == nil {
			t.Fatal("expected a source for the stored login")
		}
		tok, err := src.AccessToken(context.Background())
		if err != nil || tok != "eoat_x" {
			t.Errorf("AccessToken = (%q, %v)", tok, err)
		}
	})
	t.Run("env label disables oauth fallback", func(t *testing.T) {
		if src, _ := resolveOAuthSource("test", resolved{baseURL: sourced{val: f.srv.URL}}, io.Discard); src != nil {
			t.Error("a non-default env label must not fall back to the stored login")
		}
	})
}
