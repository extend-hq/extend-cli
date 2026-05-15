package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/extend-hq/extend-cli/internal/version"
)

const (
	DefaultBaseURL    = "https://api.extend.ai"
	DefaultAPIVersion = "2026-02-09"
)

// userAgent is built lazily so the binary's version (set via -ldflags or
// resolved from build info) is reflected on every request. Tests that want
// to override it can swap UserAgent.
var UserAgent = func() string {
	return "extend-cli/" + version.Short()
}

// RetryConfig describes the request-level retry policy. The same policy
// applies to GETs (full retries on transient errors) and POSTs (only on
// 429s, never on 5xx, since POSTs are not assumed idempotent).
//
// Help topics render these numbers from DefaultRetryConfig so the docs are
// always in sync with the binary.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts (including the first).
	MaxAttempts int
	// InitialBackoff is the wait before the first retry.
	InitialBackoff time.Duration
	// MaxBackoff caps the exponential backoff between retries.
	MaxBackoff time.Duration
}

// DefaultRetryConfig is the policy used by every client method.
var DefaultRetryConfig = RetryConfig{
	MaxAttempts:    4,
	InitialBackoff: 500 * time.Millisecond,
	MaxBackoff:     5 * time.Second,
}

// DefaultHTTPTimeout caps individual non-upload HTTP requests (each
// POST/GET, not the overall wait budget). Configurable via
// --http-timeout / EXTEND_HTTP_TIMEOUT. Tuned for the common case
// of small JSON request/response payloads against a region close to
// the caller; uploads use a separate untimed client (see UploadHTTP)
// because end-to-end HTTP timeouts are hostile to large bodies.
const DefaultHTTPTimeout = 60 * time.Second

type Client struct {
	BaseURL     string
	APIKey      string
	APIVersion  string
	WorkspaceID string
	// HTTP carries the configured per-request timeout (default 60s).
	// Used for every call EXCEPT upload streams.
	HTTP *http.Client
	// UploadHTTP is used by streaming uploads (multipart bodies over
	// io.Pipe). It has no http.Client-level timeout; the caller's
	// context is the only deadline. End-to-end HTTP timeouts are
	// counterproductive here because a legitimate 100MB upload over
	// a flaky connection can easily exceed the 60s normal-request
	// budget, surfacing as a vague "context deadline exceeded"
	// instead of a fixable error.
	UploadHTTP *http.Client

	// Debug, when non-nil, receives one log line per HTTP request and
	// response. Used by `--debug` and EXTEND_DEBUG=1. The Authorization
	// header is redacted; bodies are logged only on error responses
	// (they typically include the server-side error message).
	Debug io.Writer
}

func New(apiKey string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		APIKey:     apiKey,
		APIVersion: DefaultAPIVersion,
		HTTP:       &http.Client{Timeout: DefaultHTTPTimeout},
		UploadHTTP: &http.Client{},
	}
}

// SetHTTPTimeout overrides the per-request timeout used by every call
// except upload streams. Passing 0 disables the timeout entirely
// (rely on context cancellation only). UploadHTTP is unaffected.
func (c *Client) SetHTTPTimeout(d time.Duration) {
	if c.HTTP == nil {
		c.HTTP = &http.Client{}
	}
	c.HTTP.Timeout = d
}

var Regions = map[string]string{
	"us":  "https://api.extend.ai",
	"us2": "https://api.us2.extend.app",
	"eu":  "https://api.eu1.extend.ai",
}

func RegionBaseURL(region string) (string, bool) {
	url, ok := Regions[region]
	return url, ok
}

func KnownRegions() []string {
	return []string{"us", "us2", "eu"}
}

type APIError struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable"`
	RequestID  string `json:"requestId"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("extend api: %s: %s (request_id=%s)", e.Code, e.Message, e.RequestID)
	}
	return fmt.Sprintf("extend api: http %d: %s (request_id=%s)", e.StatusCode, e.Message, e.RequestID)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	return c.doVia(c.HTTP, ctx, method, path, body, contentType)
}

// doUpload is the variant used by streaming-upload paths
// (UploadStream). Routes through UploadHTTP so end-to-end timeouts on
// the normal client don't cut large multipart bodies short.
func (c *Client) doUpload(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	httpClient := c.UploadHTTP
	if httpClient == nil {
		// Defensive default: if a caller built a Client manually and
		// forgot to set UploadHTTP, fall back to a fresh untimed
		// client instead of accidentally inheriting HTTP's timeout.
		httpClient = &http.Client{}
	}
	return c.doVia(httpClient, ctx, method, path, body, contentType)
}

// doVia is the shared request-issuing core. The httpClient parameter
// is the only thing that varies between normal and upload paths:
// header set, debug logging, and error decoding are identical.
func (c *Client) doVia(httpClient *http.Client, ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("x-extend-api-version", c.APIVersion)
	req.Header.Set("User-Agent", UserAgent())
	if c.WorkspaceID != "" {
		req.Header.Set("X-Extend-Workspace-Id", c.WorkspaceID)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	c.debugRequest(req)
	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		c.debugTransportErr(method, c.BaseURL+path, time.Since(start), err)
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		apiErr := decodeError(resp)
		c.debugResponseErr(method, c.BaseURL+path, resp, time.Since(start), apiErr)
		return nil, apiErr
	}
	c.debugResponse(method, c.BaseURL+path, resp, time.Since(start))
	return resp, nil
}

// debugRequest logs the outbound request: method, URL, content-length,
// and the workspace header. Authorization is never logged. Called only
// when Debug is non-nil; callers don't have to guard themselves.
func (c *Client) debugRequest(req *http.Request) {
	if c.Debug == nil {
		return
	}
	contentLen := req.ContentLength
	wsTag := ""
	if v := req.Header.Get("X-Extend-Workspace-Id"); v != "" {
		wsTag = " workspace=" + v
	}
	fmt.Fprintf(c.Debug, "extend [debug] → %s %s (body=%dB)%s\n",
		req.Method, req.URL.String(), max64(contentLen, 0), wsTag)
}

// debugResponse logs a successful response: status, request id, latency.
// Body byte count when known via Content-Length.
func (c *Client) debugResponse(method, url string, resp *http.Response, dur time.Duration) {
	if c.Debug == nil {
		return
	}
	rid := resp.Header.Get("x-extend-request-id")
	clen := resp.Header.Get("content-length")
	bodyTag := ""
	if clen != "" {
		bodyTag = " body=" + clen + "B"
	}
	fmt.Fprintf(c.Debug, "extend [debug] ← %s %s %d (req=%s, %s)%s\n",
		method, url, resp.StatusCode, requestIDOrPlaceholder(rid), dur.Round(time.Millisecond), bodyTag)
}

// debugResponseErr logs an error response: status, request id, latency,
// and the server's error message (which is short — it's already an
// APIError struct from decodeError).
func (c *Client) debugResponseErr(method, url string, resp *http.Response, dur time.Duration, apiErr error) {
	if c.Debug == nil {
		return
	}
	rid := resp.Header.Get("x-extend-request-id")
	fmt.Fprintf(c.Debug, "extend [debug] ← %s %s %d (req=%s, %s) error: %v\n",
		method, url, resp.StatusCode, requestIDOrPlaceholder(rid), dur.Round(time.Millisecond), apiErr)
}

// debugTransportErr logs a transport-level failure (timeout, DNS, TLS).
// No request ID is available because no response came back.
func (c *Client) debugTransportErr(method, url string, dur time.Duration, err error) {
	if c.Debug == nil {
		return
	}
	fmt.Fprintf(c.Debug, "extend [debug] ✗ %s %s (%s) transport error: %v\n",
		method, url, dur.Round(time.Millisecond), err)
}

func requestIDOrPlaceholder(rid string) string {
	if rid == "" {
		return "-"
	}
	return rid
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func decodeError(resp *http.Response) error {
	apiErr := &APIError{StatusCode: resp.StatusCode, RequestID: resp.Header.Get("x-extend-request-id")}
	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		var wrapper struct {
			Error *APIError `json:"error"`
		}
		if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Error != nil {
			apiErr.Code = wrapper.Error.Code
			apiErr.Message = wrapper.Error.Message
			apiErr.Retryable = wrapper.Error.Retryable
			if wrapper.Error.RequestID != "" {
				apiErr.RequestID = wrapper.Error.RequestID
			}
		} else {
			_ = json.Unmarshal(body, apiErr)
			if apiErr.Message == "" {
				apiErr.Message = strings.TrimSpace(string(body))
			}
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}
	return apiErr
}

func (c *Client) postJSON(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	rc := DefaultRetryConfig
	backoff := rc.InitialBackoff
	var lastErr error
	for attempt := 0; attempt < rc.MaxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, rc.MaxBackoff)
		}
		resp, err := c.do(ctx, http.MethodPost, path, bytes.NewReader(body), "application/json")
		if err == nil {
			defer resp.Body.Close()
			if out == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				return nil
			}
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			return nil
		}
		lastErr = err
		if !isRateLimited(err) {
			return err
		}
	}
	return lastErr
}

func isRateLimited(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests
	}
	return false
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	rc := DefaultRetryConfig
	backoff := rc.InitialBackoff
	var lastErr error
	for attempt := 0; attempt < rc.MaxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, rc.MaxBackoff)
		}
		resp, err := c.do(ctx, http.MethodGet, path, nil, "")
		if err == nil {
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(out)
		}
		lastErr = err
		if !isTransient(err) {
			return err
		}
	}
	return lastErr
}

func isTransient(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Retryable {
			return true
		}
		if apiErr.StatusCode == http.StatusTooManyRequests {
			return true
		}
		if apiErr.StatusCode >= 500 && apiErr.StatusCode < 600 {
			return true
		}
		return false
	}
	return true
}

var ErrNotTerminal = errors.New("run is not in a terminal state")
