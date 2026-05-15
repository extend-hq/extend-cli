package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, true},
		{"plain network error", errors.New("connection reset"), true},
		{"401 not transient", &APIError{StatusCode: 401, Code: "UNAUTHORIZED"}, false},
		{"404 not transient", &APIError{StatusCode: 404, Code: "NOT_FOUND"}, false},
		{"422 not transient", &APIError{StatusCode: 422, Code: "INVALID_REQUEST"}, false},
		{"429 is transient", &APIError{StatusCode: 429, Code: "RATE_LIMIT_EXCEEDED"}, true},
		{"500 is transient", &APIError{StatusCode: 500}, true},
		{"503 is transient", &APIError{StatusCode: 503}, true},
		{"retryable flag wins on 4xx", &APIError{StatusCode: 400, Retryable: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransient(tc.err); got != tc.want {
				t.Errorf("isTransient(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestGetJSONRetriesOnTransient(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.Header().Set("x-extend-request-id", "req_x")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"INTERNAL_ERROR","message":"try later","retryable":true}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer srv.Close()

	c := New("test-key")
	c.BaseURL = srv.URL

	var out struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(context.Background(), "/x", &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if out.ID != "ok" {
		t.Errorf("ID = %q, want ok", out.ID)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestGetJSONDoesNotRetryOnPermanent(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"NOT_FOUND","message":"gone"}`))
	}))
	defer srv.Close()

	c := New("test-key")
	c.BaseURL = srv.URL

	var out struct {
		ID string `json:"id"`
	}
	err := c.getJSON(context.Background(), "/x", &out)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", apiErr.Code)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retries on 404)", got)
	}
}

func TestGetJSONBackoffGrows(t *testing.T) {
	var (
		mu        sync.Mutex
		intervals []time.Duration
		lastSeen  time.Time
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		mu.Lock()
		if !lastSeen.IsZero() {
			intervals = append(intervals, now.Sub(lastSeen))
		}
		lastSeen = now
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"INTERNAL_ERROR","retryable":true}`))
	}))
	defer srv.Close()

	c := New("k")
	c.BaseURL = srv.URL
	var out any
	_ = c.getJSON(context.Background(), "/x", &out)

	mu.Lock()
	defer mu.Unlock()
	if len(intervals) < 2 {
		t.Skipf("not enough samples (%d) to verify backoff growth", len(intervals))
	}
	for i := 1; i < len(intervals); i++ {
		if intervals[i] < intervals[i-1]/2 {
			t.Errorf("backoff regression: i=%d want growing, got %v after %v", i, intervals[i], intervals[i-1])
		}
	}
}

func TestGetJSONRespectsContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"INTERNAL_ERROR","retryable":true}`))
	}))
	defer srv.Close()

	c := New("test-key")
	c.BaseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var out any
	err := c.getJSON(ctx, "/x", &out)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestPostJSONRetriesOn429(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"RATE_LIMIT_EXCEEDED","message":"slow down"}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"created"}`))
	}))
	defer srv.Close()

	c := New("k")
	c.BaseURL = srv.URL
	var out struct {
		ID string `json:"id"`
	}
	if err := c.postJSON(context.Background(), "/x", map[string]string{"foo": "bar"}, &out); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	if out.ID != "created" {
		t.Errorf("ID = %q, want created", out.ID)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestPostJSONDoesNotRetryOn500(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"code":"INTERNAL_ERROR","message":"oops"}`))
	}))
	defer srv.Close()

	c := New("k")
	c.BaseURL = srv.URL
	var out any
	if err := c.postJSON(context.Background(), "/x", map[string]string{}, &out); err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (POST 500 is not auto-retried)", got)
	}
}

func TestPostJSONDoesNotRetryOn400(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"code":"INVALID_REQUEST","message":"bad"}`))
	}))
	defer srv.Close()

	c := New("k")
	c.BaseURL = srv.URL
	var out any
	if err := c.postJSON(context.Background(), "/x", map[string]string{}, &out); err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (4xx not retried)", got)
	}
}

func TestWorkspaceHeaderSentWhenSet(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Extend-Workspace-Id")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New("k")
	c.BaseURL = srv.URL
	c.WorkspaceID = "ws_abc"
	var out map[string]any
	if err := c.getJSON(context.Background(), "/x", &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if seen != "ws_abc" {
		t.Errorf("X-Extend-Workspace-Id = %q, want ws_abc", seen)
	}
}

func TestWorkspaceHeaderOmittedWhenEmpty(t *testing.T) {
	var present string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if vals, ok := r.Header["X-Extend-Workspace-Id"]; ok {
			present = strings.Join(vals, ",")
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New("k")
	c.BaseURL = srv.URL
	var out map[string]any
	if err := c.getJSON(context.Background(), "/x", &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if present != "" {
		t.Errorf("workspace header should be absent, got %q", present)
	}
}

// TestWaitForRun_OwnTimeoutReturnsTyped exercises the typed-error path:
// when the caller's --timeout (passed via WaitOptions.Timeout) elapses
// before the run reaches a terminal state, waitForRun must return a
// *WaitTimeoutError, not a bare context.DeadlineExceeded. This is what
// lets the CLI render an actionable retry hint ("retry with a larger
// --timeout, or rerun with --wait=false and 'extend runs watch ...'")
// instead of a confusing "wait: context deadline exceeded".
func TestWaitForRun_OwnTimeoutReturnsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONStub(w, 200, `{"id":"exr_slow","status":"PROCESSING"}`)
	}))
	defer srv.Close()

	c := New("k")
	c.BaseURL = srv.URL

	timeout := 80 * time.Millisecond
	opts := WaitOptions{Interval: 20 * time.Millisecond, MaxInterval: 20 * time.Millisecond, Timeout: timeout}
	_, err := c.WaitForExtractRun(context.Background(), "exr_slow", opts, nil)
	if err == nil {
		t.Fatal("expected wait timeout, got nil")
	}
	var wt *WaitTimeoutError
	if !errors.As(err, &wt) {
		t.Fatalf("err type = %T, want *WaitTimeoutError; got %v", err, err)
	}
	if wt.Timeout != timeout {
		t.Errorf("WaitTimeoutError.Timeout = %v, want %v", wt.Timeout, timeout)
	}
	if !strings.Contains(wt.Error(), "wait timed out") {
		t.Errorf("error message = %q, want it to contain 'wait timed out'", wt.Error())
	}
	// Preserve the existing errors.Is(err, context.DeadlineExceeded)
	// contract so legacy callers that only check the sentinel still work.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("errors.Is(err, context.DeadlineExceeded) should still hold for WaitTimeoutError")
	}
}

// TestWaitForRun_ParentCancelNotTimeout ensures we don't dress up an
// external context cancellation (Ctrl-C, parent deadline) as our own
// wait timeout. The CLI surfaces parent cancels as-is; only our own
// budget exhaustion should produce the actionable WaitTimeoutError.
func TestWaitForRun_ParentCancelNotTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONStub(w, 200, `{"id":"exr_slow","status":"PROCESSING"}`)
	}))
	defer srv.Close()

	c := New("k")
	c.BaseURL = srv.URL

	parent, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Give waitForRun a generous Timeout so the parent's deadline trips
	// first; the caller's "give up" intent should win.
	opts := WaitOptions{Interval: 10 * time.Millisecond, MaxInterval: 10 * time.Millisecond, Timeout: 30 * time.Second}
	_, err := c.WaitForExtractRun(parent, "exr_slow", opts, nil)
	if err == nil {
		t.Fatal("expected error from parent cancel")
	}
	var wt *WaitTimeoutError
	if errors.As(err, &wt) {
		t.Errorf("parent-cancel should NOT surface as WaitTimeoutError; got %v", err)
	}
}

func writeJSONStub(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// TestNewClientDefaultTimeouts pins the two-client design: regular API
// calls inherit DefaultHTTPTimeout (60s today), but uploads use a
// separate untimed http.Client so multipart bodies aren't capped by
// the same budget as small JSON requests. Drift here usually means
// someone replaced one of the clients without thinking about the
// other.
func TestNewClientDefaultTimeouts(t *testing.T) {
	c := New("k")
	if c.HTTP == nil {
		t.Fatal("HTTP client should not be nil")
	}
	if c.HTTP.Timeout != DefaultHTTPTimeout {
		t.Errorf("HTTP.Timeout = %v, want %v", c.HTTP.Timeout, DefaultHTTPTimeout)
	}
	if c.UploadHTTP == nil {
		t.Fatal("UploadHTTP client should not be nil")
	}
	if c.UploadHTTP.Timeout != 0 {
		t.Errorf("UploadHTTP.Timeout = %v, want 0 (untimed)", c.UploadHTTP.Timeout)
	}
	if c.HTTP == c.UploadHTTP {
		t.Error("HTTP and UploadHTTP must be distinct clients; uploads bypass HTTP's end-to-end timeout")
	}
}

// TestSetHTTPTimeoutLeavesUploadAlone documents the asymmetry: the
// CLI's --http-timeout flag tunes only the request budget for normal
// calls; uploads stay untimed regardless. If a future change ever
// surfaces a --upload-timeout, this test should expand rather than
// disappear.
func TestSetHTTPTimeoutLeavesUploadAlone(t *testing.T) {
	c := New("k")
	c.SetHTTPTimeout(5 * time.Second)
	if c.HTTP.Timeout != 5*time.Second {
		t.Errorf("HTTP.Timeout = %v, want 5s", c.HTTP.Timeout)
	}
	if c.UploadHTTP.Timeout != 0 {
		t.Errorf("UploadHTTP.Timeout = %v, want 0 (untimed) after SetHTTPTimeout(5s)", c.UploadHTTP.Timeout)
	}
}

// TestUploadStreamSurvivesPastHTTPTimeout verifies the practical
// payoff of the untimed upload client. Set the normal HTTP timeout
// short, stage a server that holds the upload longer than that
// timeout, and confirm the upload still completes. Without the
// untimed UploadHTTP client this would surface as "context deadline
// exceeded" partway through a large multipart body.
func TestUploadStreamSurvivesPastHTTPTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the body, then sleep past HTTP.Timeout before
		// responding. A 60s default timeout would obviously never
		// trip in CI; we set a tiny one explicitly below.
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(120 * time.Millisecond)
		writeJSONStub(w, 200, `{"id":"file_ok","name":"x.pdf","object":"file"}`)
	}))
	defer srv.Close()

	c := New("k")
	c.BaseURL = srv.URL
	c.SetHTTPTimeout(30 * time.Millisecond)

	body := strings.NewReader("hello-bytes")
	f, err := c.UploadStream(context.Background(), body, "x.pdf", "application/pdf")
	if err != nil {
		t.Fatalf("UploadStream should bypass --http-timeout for upload streams; got %v", err)
	}
	if f.ID != "file_ok" {
		t.Errorf("File.ID = %q, want file_ok", f.ID)
	}
}

func TestResolveInputDispatch(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantRef   FileRef
		wantLocal string
		wantErr   bool
	}{
		{"file id", "file_xK9", FileRef{ID: "file_xK9"}, "", false},
		{"https url", "https://x.com/a.pdf", FileRef{URL: "https://x.com/a.pdf"}, "", false},
		{"http url", "http://x.com/a.pdf", FileRef{URL: "http://x.com/a.pdf"}, "", false},
		{"stdin", "-", FileRef{}, "-", false},
		{"missing local", "/does/not/exist.pdf", FileRef{}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ref, local, err := ResolveInput(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ResolveInput(%q) err = %v, wantErr=%v", tc.input, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if ref != tc.wantRef {
				t.Errorf("ref = %+v, want %+v", ref, tc.wantRef)
			}
			if local != tc.wantLocal {
				t.Errorf("local = %q, want %q", local, tc.wantLocal)
			}
		})
	}
}
