package integration

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLoginFlowAgainstFakeOAuthServer wires the whole browser-login
// lifecycle against a local fake authorization server: login with
// --no-browser (the test plays the browser by following the printed
// URL), an authenticated API call using the stored bearer token, and
// logout with server-side revocation. It needs no real credentials,
// so it does not go through requireEnv.
func TestLoginFlowAgainstFakeOAuthServer(t *testing.T) {
	fake := newFakeOAuthAPI(t)
	confDir := t.TempDir()
	env := map[string]string{
		"EXTEND_BASE_URL": fake.srv.URL,
		"XDG_CONFIG_HOME": confDir,
		// Force the file token store: the OS keychain must never be
		// touched by tests.
		"EXTEND_OAUTH_NO_KEYRING": "1",
	}

	// Step 1: extend login --no-browser, driving the printed URL.
	runLoginDrivingBrowser(t, env)
	tokensFile := filepath.Join(confDir, "extend", "oauth_tokens.json")
	raw, err := os.ReadFile(tokensFile)
	if err != nil {
		t.Fatalf("tokens file after login: %v", err)
	}
	if !strings.Contains(string(raw), "eoat_itest_1") {
		t.Fatalf("tokens file missing access token: %s", raw)
	}

	// Step 2: an API command authenticates with the stored bearer
	// token and no API key anywhere in the environment.
	res := runExtendBare(t, env, "workflows", "list", "-o", "json")
	res.requireOK(t, "workflows", "list")
	if got := fake.lastAPIAuth(); got != "Bearer eoat_itest_1" {
		t.Errorf("API call Authorization = %q, want the stored bearer token", got)
	}

	// Step 3: extend config reports the OAuth login as the auth method.
	res = runExtendBare(t, env, "config")
	res.requireOK(t, "config")
	if !strings.Contains(string(res.Stdout), "OAuth login") {
		t.Errorf("extend config should report OAuth login, got: %s", res.Stdout)
	}

	// Step 4: logout revokes server-side and clears local state.
	res = runExtendBare(t, env, "logout")
	res.requireOK(t, "logout")
	if got := fake.revokedTokens(); len(got) != 1 || got[0] != "eort_itest_1" {
		t.Errorf("revoked tokens = %v, want the refresh token", got)
	}
	if raw, err := os.ReadFile(tokensFile); err == nil && strings.Contains(string(raw), "eoat_") {
		t.Errorf("tokens file still holds a token after logout: %s", raw)
	}

	// Step 5: logging out again is a no-op, not an error.
	res = runExtendBare(t, env, "logout")
	res.requireOK(t, "logout")
	if !strings.Contains(string(res.Stderr), "Not logged in") {
		t.Errorf("second logout stderr = %s", res.Stderr)
	}
}

// runLoginDrivingBrowser starts `extend login --no-browser`, scrapes the
// authorize URL from stderr, follows it like a browser would (the fake
// server 302s straight back to the loopback), and waits for the CLI to
// finish the exchange.
func runLoginDrivingBrowser(t *testing.T, env map[string]string) {
	t.Helper()
	cmd := exec.Command(extendBinary, "login", "--no-browser")
	full := []string{"NO_COLOR=1", "TERM=dumb"}
	for k, v := range env {
		full = append(full, k+"="+v)
	}
	cmd.Env = full
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start extend login: %v", err)
	}

	urlRe := regexp.MustCompile(`https?://\S+/oauth2/authorize\S*`)
	var collected bytes.Buffer
	scanner := bufio.NewScanner(io.TeeReader(stderr, &collected))
	authURL := ""
	deadline := time.After(15 * time.Second)
	found := make(chan string, 1)
	go func() {
		for scanner.Scan() {
			if m := urlRe.FindString(scanner.Text()); m != "" {
				select {
				case found <- m:
				default:
				}
			}
		}
	}()
	select {
	case authURL = <-found:
	case <-deadline:
		cmd.Process.Kill()
		t.Fatalf("login never printed the authorize URL; stderr so far: %s", collected.String())
	}

	// Play the browser: GET the authorize URL and follow the redirect
	// chain back to the CLI's loopback listener.
	resp, err := http.Get(authURL)
	if err != nil {
		cmd.Process.Kill()
		t.Fatalf("drive authorize URL: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		cmd.Process.Kill()
		t.Fatalf("browser flow ended with status %d: %s", resp.StatusCode, body)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("extend login exited with error: %v\nstderr: %s", err, collected.String())
	}
	if !strings.Contains(collected.String(), "Logged in to") {
		t.Errorf("login stderr missing success line: %s", collected.String())
	}
}

// fakeOAuthAPI is a single server standing in for both the Extend API
// and its authorization endpoints, per the wire contract: hardcoded
// /oauth2/* paths (no discovery document, exercising the 404 fallback),
// PKCE-checked code exchange, and a bearer-authenticated /workflows.
type fakeOAuthAPI struct {
	srv *httptest.Server

	mu        sync.Mutex
	challenge string
	issued    int
	apiAuth   string
	revoked   []string
}

func newFakeOAuthAPI(t *testing.T) *fakeOAuthAPI {
	t.Helper()
	f := &fakeOAuthAPI{}
	mux := http.NewServeMux()

	mux.HandleFunc("/oauth2/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		f.challenge = q.Get("code_challenge")
		f.mu.Unlock()
		if q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
			http.Error(w, "bad authorize request", http.StatusBadRequest)
			return
		}
		if q.Get("resource") == "" {
			http.Error(w, "resource parameter is required", http.StatusBadRequest)
			return
		}
		redirect := fmt.Sprintf("%s?code=%s&state=%s",
			q.Get("redirect_uri"), "itest-code", url.QueryEscape(q.Get("state")))
		http.Redirect(w, r, redirect, http.StatusFound)
	})

	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("resource") == "" {
			http.Error(w, `{"error":"invalid_request","error_description":"resource is required"}`, http.StatusBadRequest)
			return
		}
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") != "itest-code" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			f.mu.Lock()
			challengeOK := base64.RawURLEncoding.EncodeToString(sum[:]) == f.challenge
			f.mu.Unlock()
			if !challengeOK {
				http.Error(w, `{"error":"invalid_grant","error_description":"pkce verification failed"}`, http.StatusBadRequest)
				return
			}
		case "refresh_token":
			// Accepted so a silent refresh during the test cannot fail.
		default:
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.issued++
		n := f.issued
		f.mu.Unlock()
		fmt.Fprintf(w, `{"access_token":"eoat_itest_%[1]d","refresh_token":"eort_itest_%[1]d","token_type":"Bearer","expires_in":3600}`, n)
	})

	mux.HandleFunc("/oauth2/revoke", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		f.revoked = append(f.revoked, r.PostForm.Get("token"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/workflows", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.apiAuth = r.Header.Get("Authorization")
		f.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer eoat_") {
			http.Error(w, `{"code":"UNAUTHORIZED","message":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		// The SDK asserts the "object":"list" literal when decoding.
		fmt.Fprint(w, `{"object":"list","data":[]}`)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOAuthAPI) lastAPIAuth() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.apiAuth
}

func (f *fakeOAuthAPI) revokedTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.revoked...)
}
