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
// status and body. Every callback response is a branded HTML page, so
// the content type is asserted here for all variants at once.
func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
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
	// The heading apostrophe is HTML-escaped on the wire.
	for _, want := range []string{"You&#39;re signed in", "close this tab", "<svg"} {
		if !strings.Contains(body, want) {
			t.Errorf("success page missing %q, got %q", want, body)
		}
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

func TestLoopbackStateMismatchIsIgnored(t *testing.T) {
	lb, err := NewLoopback("expected-state")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	// A callback that cannot echo the state (wrong or missing) must be
	// answered without consuming the result channel: any local process
	// could otherwise abort a pending real login.
	for _, qs := range []string{
		"?code=abc&state=wrong-state",
		"?code=abc",
		"?error=access_denied&state=wrong-state",
		"?error=access_denied",
	} {
		status, body := get(t, lb.RedirectURI()+qs)
		if status != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", qs, status)
		}
		for _, want := range []string{"Sign-in failed", "extend login"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s page missing %q, got %q", qs, want, body)
			}
		}
	}

	// The channel is untouched: Wait keeps waiting...
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer shortCancel()
	if _, err := lb.Wait(shortCtx); err != context.DeadlineExceeded {
		t.Fatalf("Wait after ignored callbacks = %v, want DeadlineExceeded (still pending)", err)
	}

	// ...and the real callback still completes the login.
	get(t, lb.RedirectURI()+"?code=realcode&state=expected-state")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := lb.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != "realcode" {
		t.Errorf("code = %q, want realcode", code)
	}
}

func TestLoopbackErrorCallbackNeedsState(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	// error= without the right state is ignored (channel untouched);
	// with the right state it resolves the flow.
	get(t, lb.RedirectURI()+"?error=access_denied")
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer shortCancel()
	if _, err := lb.Wait(shortCtx); err != context.DeadlineExceeded {
		t.Fatalf("Wait = %v, want DeadlineExceeded (stateless error ignored)", err)
	}

	get(t, lb.RedirectURI()+"?error=access_denied&state=s")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := lb.Wait(ctx); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("Wait err = %v, want access_denied", err)
	}
}

func TestLoopbackRejectsNonGET(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	resp, err := http.Post(lb.RedirectURI()+"?code=x&state=s", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != http.MethodGet {
		t.Errorf("Allow = %q, want GET", allow)
	}

	// The POST must not have consumed the channel or the code.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer shortCancel()
	if _, err := lb.Wait(shortCtx); err != context.DeadlineExceeded {
		t.Errorf("Wait after POST = %v, want DeadlineExceeded", err)
	}
}

func TestLoopbackRejectsOtherPaths(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	base := strings.TrimSuffix(lb.RedirectURI(), "/callback")
	for _, path := range []string{"/", "/callback/extra", "/other"} {
		resp, err := http.Get(base + path + "?code=x&state=s")
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, resp.StatusCode)
		}
	}
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer shortCancel()
	if _, err := lb.Wait(shortCtx); err != context.DeadlineExceeded {
		t.Errorf("Wait after off-path hits = %v, want DeadlineExceeded", err)
	}
}

func TestLoopbackPagesAreUncacheable(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	resp, err := http.Get(lb.RedirectURI() + "?code=x&state=s")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestLoopbackErrorParam(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	_, body := get(t, lb.RedirectURI()+"?error=access_denied&error_description=user+said+no&state=s")
	for _, want := range []string{"Sign-in canceled", "close this tab"} {
		if !strings.Contains(body, want) {
			t.Errorf("denied page missing %q, got %q", want, body)
		}
	}

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

func TestLoopbackServerErrorPageEscapesDescription(t *testing.T) {
	lb, err := NewLoopback("s")
	if err != nil {
		t.Fatalf("NewLoopback: %v", err)
	}
	defer lb.Close()

	_, body := get(t, lb.RedirectURI()+"?error=server_error&error_description=%3Cscript%3Eboom%3C%2Fscript%3E&state=s")
	for _, want := range []string{"Sign-in failed", "extend login"} {
		if !strings.Contains(body, want) {
			t.Errorf("error page missing %q, got %q", want, body)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Errorf("error page must escape the error_description, got %q", body)
	}
	if !strings.Contains(body, "&lt;script&gt;boom&lt;/script&gt;") {
		t.Errorf("error page should show the escaped description, got %q", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := lb.Wait(ctx); err == nil || !strings.Contains(err.Error(), "server_error") {
		t.Errorf("Wait err = %v, want server_error", err)
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
