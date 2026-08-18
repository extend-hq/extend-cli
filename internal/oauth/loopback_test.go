package oauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// get performs a plain GET against the loopback listener and returns
// status and body.
func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

func TestLoopbackSuccess(t *testing.T) {
	lb, err := NewLoopback("state123")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	uri := lb.RedirectURI()
	if !strings.HasPrefix(uri, "http://127.0.0.1:") || !strings.HasSuffix(uri, "/callback") {
		t.Fatalf("RedirectURI = %q, want http://127.0.0.1:{port}/callback", uri)
	}

	status, body := get(t, uri+"?code=authcode42&state=state123")
	if status != http.StatusOK {
		t.Errorf("callback status = %d, want 200", status)
	}
	if !strings.Contains(body, "close this tab") {
		t.Errorf("callback body should tell the user to close the tab, got %q", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := lb.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != "authcode42" {
		t.Errorf("code = %q, want authcode42", code)
	}
}

func TestLoopbackStateMismatch(t *testing.T) {
	lb, err := NewLoopback("expected-state")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	status, _ := get(t, lb.RedirectURI()+"?code=abc&state=wrong-state")
	if status != http.StatusBadRequest {
		t.Errorf("state mismatch status = %d, want 400", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := lb.Wait(ctx); err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("Wait err = %v, want state mismatch error", err)
	}
}

func TestLoopbackErrorParam(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	get(t, lb.RedirectURI()+"?error=access_denied&error_description=user+said+no&state=s")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = lb.Wait(ctx)
	if err == nil {
		t.Fatal("Wait should return the authorization error")
	}
	if !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "user said no") {
		t.Errorf("Wait err = %v, want access_denied with description", err)
	}
}

func TestLoopbackMissingCode(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	status, _ := get(t, lb.RedirectURI()+"?state=s")
	if status != http.StatusBadRequest {
		t.Errorf("missing code status = %d, want 400", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := lb.Wait(ctx); err == nil || !strings.Contains(err.Error(), "missing code") {
		t.Errorf("Wait err = %v, want missing code error", err)
	}
}

func TestLoopbackFirstResultWins(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	get(t, lb.RedirectURI()+"?code=first&state=s")
	status, _ := get(t, lb.RedirectURI()+"?code=second&state=s")
	if status != http.StatusConflict {
		t.Errorf("second callback status = %d, want 409", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := lb.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != "first" {
		t.Errorf("code = %q, want first", code)
	}
}

func TestLoopbackWaitHonorsContext(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := lb.Wait(ctx); err != context.DeadlineExceeded {
		t.Errorf("Wait err = %v, want context.DeadlineExceeded", err)
	}
}

func TestAuthorizeURL(t *testing.T) {
	u := AuthorizeURL("https://api.example/oauth2/authorize", AuthorizeParams{
		ClientID:    "extend-cli",
		RedirectURI: "http://127.0.0.1:9999/callback",
		State:       "st",
		Challenge:   "ch",
		Resource:    "https://api.example",
	})
	for _, want := range []string{
		"https://api.example/oauth2/authorize?",
		"response_type=code",
		"client_id=extend-cli",
		"redirect_uri=http%3A%2F%2F127.0.0.1%3A9999%2Fcallback",
		"state=st",
		"code_challenge=ch",
		"code_challenge_method=S256",
		"resource=https%3A%2F%2Fapi.example",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("AuthorizeURL missing %q in %q", want, u)
		}
	}
}
