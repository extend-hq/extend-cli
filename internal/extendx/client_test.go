package extendx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkcore "github.com/extend-hq/extend-go-sdk/core"
)

func TestAsAPIError_NilAndPlain(t *testing.T) {
	if _, ok := AsAPIError(nil); ok {
		t.Error("AsAPIError(nil) reported ok=true")
	}
	if _, ok := AsAPIError(errors.New("plain")); ok {
		t.Error("AsAPIError(plain) reported ok=true; only API errors should match")
	}
}

func TestAsAPIError_CoreAPIError_StandardEnvelope(t *testing.T) {
	// Standard error envelope returned by the API. Wrapped as the
	// body of a *sdkcore.APIError — the path used for any status
	// code without a typed wrapper (400, 401, 404, 429, 500, ...).
	body := `{"code":"NOT_FOUND","message":"no such run","requestId":"req_123","retryable":false}`
	header := http.Header{}
	header.Set("x-extend-request-id", "req_123")
	err := sdkcore.NewAPIError(404, header, errors.New(body))

	got, ok := AsAPIError(err)
	if !ok {
		t.Fatal("AsAPIError(coreAPIError) ok=false; want true")
	}
	if got.StatusCode != 404 {
		t.Errorf("StatusCode = %d; want 404", got.StatusCode)
	}
	if got.Code != "NOT_FOUND" {
		t.Errorf("Code = %q; want NOT_FOUND", got.Code)
	}
	if got.Message != "no such run" {
		t.Errorf("Message = %q; want \"no such run\"", got.Message)
	}
	if got.RequestID != "req_123" {
		t.Errorf("RequestID = %q; want req_123", got.RequestID)
	}
}

func TestAsAPIError_CoreAPIError_WrappedEnvelope(t *testing.T) {
	// Some endpoints wrap the envelope as {"error": {...}}; the
	// extractor must transparently pull fields from either shape.
	body := `{"error":{"code":"FORBIDDEN","message":"no access","requestId":"req_x"}}`
	err := sdkcore.NewAPIError(403, http.Header{}, errors.New(body))

	got, ok := AsAPIError(err)
	if !ok {
		t.Fatal("AsAPIError({error:...}) ok=false")
	}
	if got.Code != "FORBIDDEN" {
		t.Errorf("Code = %q; want FORBIDDEN", got.Code)
	}
	if got.Message != "no access" {
		t.Errorf("Message = %q; want \"no access\"", got.Message)
	}
}

func TestAsAPIError_CoreAPIError_PlainTextBody(t *testing.T) {
	// Bodies that aren't JSON (e.g. a CDN-generated 502 HTML page)
	// land in Message verbatim instead of attempting a fragile
	// scrape. Lock that contract.
	body := "<html><body>Bad Gateway</body></html>"
	err := sdkcore.NewAPIError(502, http.Header{}, errors.New(body))

	got, ok := AsAPIError(err)
	if !ok {
		t.Fatal("AsAPIError(text body) ok=false")
	}
	if got.Message != body {
		t.Errorf("Message = %q; want raw body", got.Message)
	}
}

func TestAsAPIError_CoreAPIError_MalformedJSON(t *testing.T) {
	// JSON-shaped but invalid — falls back to message = full body.
	body := `{"code":"NOT_FOUND"`
	err := sdkcore.NewAPIError(400, http.Header{}, errors.New(body))

	got, ok := AsAPIError(err)
	if !ok {
		t.Fatal("AsAPIError(malformed JSON) ok=false")
	}
	if got.Message != body {
		t.Errorf("Message = %q; want raw body (malformed JSON should not be silently dropped)", got.Message)
	}
}

func TestAsAPIError_CoreAPIError_EmptyBody(t *testing.T) {
	// Some 4xx/5xx come back with empty bodies; Message should
	// fall through to http.StatusText.
	err := sdkcore.NewAPIError(503, http.Header{}, errors.New(""))

	got, ok := AsAPIError(err)
	if !ok {
		t.Fatal("AsAPIError(empty body) ok=false")
	}
	if got.Message != http.StatusText(503) {
		t.Errorf("Message = %q; want %q", got.Message, http.StatusText(503))
	}
}

func TestAsAPIError_CoreAPIError_HeaderRequestID(t *testing.T) {
	// When the body has no requestId, fall back to the
	// x-extend-request-id header.
	header := http.Header{}
	header.Set("x-extend-request-id", "header_rid")
	err := sdkcore.NewAPIError(500, header, errors.New(`{"code":"X","message":"boom"}`))

	got, ok := AsAPIError(err)
	if !ok {
		t.Fatal("AsAPIError ok=false")
	}
	if got.RequestID != "header_rid" {
		t.Errorf("RequestID = %q; want header_rid", got.RequestID)
	}
}

func TestAsAPIError_ForbiddenError(t *testing.T) {
	// SDK-typed wrapper for 403. The CLI must read from the typed
	// Body rather than scraping the wrapped err's string.
	requestID := "req_typed"
	body := &extend.APIError{
		Code:      "FORBIDDEN",
		Message:   "scope mismatch",
		RequestID: &requestID,
	}
	wrapped := &extend.ForbiddenError{
		APIError: &sdkcore.APIError{StatusCode: 403, Header: http.Header{}},
		Body:     body,
	}

	got, ok := AsAPIError(wrapped)
	if !ok {
		t.Fatal("AsAPIError(ForbiddenError) ok=false")
	}
	if got.StatusCode != 403 {
		t.Errorf("StatusCode = %d; want 403", got.StatusCode)
	}
	if got.Code != "FORBIDDEN" {
		t.Errorf("Code = %q; want FORBIDDEN", got.Code)
	}
	if got.Message != "scope mismatch" {
		t.Errorf("Message = %q; want \"scope mismatch\"", got.Message)
	}
	if got.RequestID != "req_typed" {
		t.Errorf("RequestID = %q; want req_typed", got.RequestID)
	}
}

func TestAsAPIError_PaymentRequiredError(t *testing.T) {
	body := &extend.APIError{Code: "PAYMENT_REQUIRED", Message: "subscribe"}
	wrapped := &extend.PaymentRequiredError{
		APIError: &sdkcore.APIError{StatusCode: 402, Header: http.Header{}},
		Body:     body,
	}
	got, ok := AsAPIError(wrapped)
	if !ok {
		t.Fatal("AsAPIError(PaymentRequiredError) ok=false")
	}
	if got.Code != "PAYMENT_REQUIRED" || got.StatusCode != 402 {
		t.Errorf("got = %+v; wanted code=PAYMENT_REQUIRED status=402", got)
	}
}

func TestAsAPIError_UnprocessableEntityError(t *testing.T) {
	body := &extend.APIError{Code: "INVALID_INPUT", Message: "bad schema"}
	wrapped := &extend.UnprocessableEntityError{
		APIError: &sdkcore.APIError{StatusCode: 422, Header: http.Header{}},
		Body:     body,
	}
	got, ok := AsAPIError(wrapped)
	if !ok {
		t.Fatal("AsAPIError(UnprocessableEntityError) ok=false")
	}
	if got.Code != "INVALID_INPUT" || got.StatusCode != 422 {
		t.Errorf("got = %+v; want code=INVALID_INPUT status=422", got)
	}
}

func TestAsAPIError_SanitizesEscapeSequences(t *testing.T) {
	// Server-controlled fields reach the terminal through the CLI's
	// error printer and through Error() in %v/%w chains; embedded
	// ANSI CSI and OSC sequences must be neutralized at extraction.
	// The \u escapes are decoded by the JSON parser into real ESC
	// bytes (raw control characters are not valid inside JSON strings).
	body := `{"code":"NOT\u001b[31m_FOUND","message":"boom\u001b]0;pwned\u0007","requestId":"rid\u001b[2J"}`
	err := sdkcore.NewAPIError(404, http.Header{}, errors.New(body))

	got, ok := AsAPIError(err)
	if !ok {
		t.Fatal("AsAPIError ok=false")
	}
	if got.Code != "NOT_FOUND" {
		t.Errorf("Code = %q; want the CSI sequence stripped", got.Code)
	}
	if got.Message != "boom" {
		t.Errorf("Message = %q; want the OSC sequence stripped", got.Message)
	}
	if got.RequestID != "rid" {
		t.Errorf("RequestID = %q; want the CSI sequence stripped", got.RequestID)
	}
	if strings.ContainsRune(got.Error(), 0x1b) {
		t.Errorf("Error() = %q; leaked an escape byte", got.Error())
	}
}

func TestAsAPIError_TypedBodySanitizesEscapeSequences(t *testing.T) {
	wrapped := &extend.ForbiddenError{
		APIError: &sdkcore.APIError{StatusCode: 403, Header: http.Header{}},
		Body:     &extend.APIError{Code: "FORBIDDEN", Message: "no\x1b[2Jaccess"},
	}
	got, ok := AsAPIError(wrapped)
	if !ok {
		t.Fatal("AsAPIError(ForbiddenError) ok=false")
	}
	if got.Message != "noaccess" {
		t.Errorf("Message = %q; want the CSI sequence stripped", got.Message)
	}
}

func TestIsNotFound(t *testing.T) {
	err404 := sdkcore.NewAPIError(404, http.Header{}, errors.New("{}"))
	if !IsNotFound(err404) {
		t.Error("IsNotFound(404) = false")
	}
	err500 := sdkcore.NewAPIError(500, http.Header{}, errors.New("{}"))
	if IsNotFound(err500) {
		t.Error("IsNotFound(500) = true; want false")
	}
	if IsNotFound(nil) {
		t.Error("IsNotFound(nil) = true")
	}
	if IsNotFound(errors.New("plain")) {
		t.Error("IsNotFound(plain) = true")
	}
}

func TestAPIError_Format(t *testing.T) {
	// Error() output is user-facing; lock both branches.
	withCode := (&APIError{StatusCode: 404, Code: "NOT_FOUND", Message: "missing", RequestID: "rid"}).Error()
	if want := "extend api: NOT_FOUND: missing (request_id=rid)"; withCode != want {
		t.Errorf("Error(withCode) = %q; want %q", withCode, want)
	}
	noCode := (&APIError{StatusCode: 502, Message: "Bad Gateway", RequestID: "rid"}).Error()
	if want := "extend api: http 502: Bad Gateway (request_id=rid)"; noCode != want {
		t.Errorf("Error(noCode) = %q; want %q", noCode, want)
	}
}

func TestNewClient_NoAPIKey(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Error("NewClient(empty Config) = nil; want non-nil")
	}
}

func TestNewClient_UnknownRegion(t *testing.T) {
	_, err := NewClient(Config{APIKey: "k", Region: "mars"})
	if err == nil {
		t.Error("NewClient(unknown region) = nil; want non-nil")
	}
}

func TestNewClient_HappyPath(t *testing.T) {
	// All defaults: APIKey only. No env hits, no network calls; the
	// SDK's constructor is synchronous and just wires options.
	c, err := NewClient(Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("NewClient = %v", err)
	}
	if c == nil {
		t.Fatal("NewClient returned nil client")
	}
}

func TestNewClient_KnownRegion(t *testing.T) {
	c, err := NewClient(Config{APIKey: "k", Region: "us2"})
	if err != nil {
		t.Fatalf("NewClient(us2) = %v", err)
	}
	if c == nil {
		t.Fatal("NewClient(us2) returned nil")
	}
}

func TestNewClient_RejectsCleartextRemoteBaseURL(t *testing.T) {
	_, err := NewClient(Config{APIKey: "k", BaseURL: "http://api.internal.example"})
	if err == nil {
		t.Fatal("NewClient(http remote base) = nil error; want a cleartext refusal")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("error = %v, want it to point at https", err)
	}
}

func TestNewClient_AllowsCleartextLoopbackBaseURL(t *testing.T) {
	for _, base := range []string{"http://localhost:3000", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		if _, err := NewClient(Config{APIKey: "k", BaseURL: base}); err != nil {
			t.Errorf("NewClient(BaseURL=%s) = %v; want loopback http accepted", base, err)
		}
	}
}

// TestNewClient_RefusesCrossOriginRedirect covers the redirect pin on
// the general API client. The OAuth-authenticated variant is the worst
// case: the bearer transport stamps a fresh Authorization header on
// every hop, so a followed off-origin redirect would hand a live
// access token to the foreign host.
func TestNewClient_RefusesCrossOriginRedirect(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("foreign origin received %s %s with Authorization %q; the redirect must not be followed",
			r.Method, r.URL.Path, r.Header.Get("Authorization"))
	}))
	defer foreign.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	c, err := NewClient(Config{OAuth: &fakeSource{token: "eoat_live"}, BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.Workflows.List(context.Background(), &extend.WorkflowsListRequest{})
	if err == nil {
		t.Fatal("List followed a cross-origin redirect")
	}
	if !strings.Contains(err.Error(), "pinned API origin") {
		t.Errorf("List error = %v, want the redirect refusal", err)
	}
}

func TestNewClient_FollowsSameOriginRedirect(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/workflows", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/workflows-v2", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/workflows-v2", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"object":"list","data":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClient(Config{APIKey: "sk_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Workflows.List(context.Background(), &extend.WorkflowsListRequest{}); err != nil {
		t.Fatalf("List through a same-origin redirect: %v", err)
	}
	if gotAuth != "Bearer sk_test" {
		t.Errorf("redirected request Authorization = %q, want the bearer to survive a same-origin hop", gotAuth)
	}
}

// TestNewClient_RevalidatesEveryRedirectHop: an allowed same-origin hop
// must not open the door for a later cross-origin hop in the same
// chain.
func TestNewClient_RevalidatesEveryRedirectHop(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("foreign origin received %s %s; hop revalidation failed", r.Method, r.URL.Path)
	}))
	defer foreign.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/workflows", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/hop", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/hop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/workflows", http.StatusTemporaryRedirect)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClient(Config{APIKey: "sk_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Workflows.List(context.Background(), &extend.WorkflowsListRequest{}); err == nil {
		t.Error("List followed a chain ending on a foreign origin")
	}
}

func TestUploadClientRefusesRedirect(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("foreign origin received %s %s; upload redirects must not be followed", r.Method, r.URL.Path)
	}))
	defer foreign.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	resp, err := uploadHTTPClient().Get(srv.URL + "/files/upload")
	if err == nil {
		resp.Body.Close()
		t.Fatal("upload client followed a redirect")
	}
	if !strings.Contains(err.Error(), "refusing redirect") {
		t.Errorf("error = %v, want the upload redirect refusal", err)
	}
}
