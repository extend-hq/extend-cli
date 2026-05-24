package extendx

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"
	sdkcore "github.com/extend-hq/extend-go-sdk/core"
	"github.com/extend-hq/extend-go-sdk/option"
)

// DefaultHTTPTimeout is applied to the HTTP client every API call goes
// through, except uploads (see UploadTimeout). Tuned for the common
// case of small JSON request/response payloads.
const DefaultHTTPTimeout = 60 * time.Second

// UserAgent is the value sent on every request. We override the SDK's
// generic identifier so server-side analytics and logs continue to see
// the CLI as the origin. The version-resolver is wired in by the cli
// package at construction time so this package doesn't take a hard
// dependency on internal/version.
var UserAgent = "extend-cli/dev"

// Config is the CLI-side, env-resolved view of all knobs that affect
// how the SDK client is built. NewClient consumes it once per command
// invocation.
//
// The zero value of every field is meaningful: unset means "use the
// SDK / region default". Resolution precedence (flag > env > default)
// is the caller's responsibility — populate these from the App
// struct's already-merged values before calling NewClient.
type Config struct {
	// APIKey is the bearer token. Required for any command that
	// actually hits the API.
	APIKey string
	// BaseURL overrides the SDK's default. Wins over Region.
	BaseURL string
	// Region is a short selector (us|us2|eu). Resolved to a URL via
	// RegionBaseURL.
	Region string
	// APIVersion overrides the SDK's default x-extend-api-version
	// header. Empty leaves the SDK default in place.
	APIVersion string
	// WorkspaceID is sent as X-Extend-Workspace-Id on every request.
	// Required for org-scoped API keys.
	WorkspaceID string
	// Debug, when non-nil, receives one log line per HTTP request and
	// response. Hooked in via a custom http.RoundTripper.
	Debug io.Writer
	// HTTPTimeout caps each non-upload HTTP request individually.
	// Zero leaves DefaultHTTPTimeout in place; a negative value
	// disables the timeout entirely (rely on context only).
	HTTPTimeout time.Duration
}

// NewClient builds an SDK client from cfg. The returned *sdkclient.Client
// carries all our env-driven options preconfigured: bearer auth,
// region/base URL, workspace ID, API version, user agent, debug
// logging, and HTTP timeout.
func NewClient(cfg Config) (*sdkclient.Client, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("API key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" && cfg.Region != "" {
		url, ok := RegionBaseURL(cfg.Region)
		if !ok {
			return nil, fmt.Errorf("unknown region %q (known: %v)", cfg.Region, KnownRegions())
		}
		baseURL = url
	}

	// The SDK sets x-extend-api-version unconditionally in its core
	// request_option.go (to "2026-02-09"). To override it we attach a
	// default HTTPHeader; the SDK's header merge calls header.Set on
	// our values AFTER cloning, so our entry wins. Same trick is used
	// for X-Extend-Workspace-Id (no SDK option for it) and User-Agent
	// (the SDK's default identifies the SDK library, not the CLI).
	headers := http.Header{}
	headers.Set("User-Agent", UserAgent)
	if cfg.APIVersion != "" {
		headers.Set("x-extend-api-version", cfg.APIVersion)
	}
	if cfg.WorkspaceID != "" {
		headers.Set("X-Extend-Workspace-Id", cfg.WorkspaceID)
	}

	// Build the underlying http.Client. Wrap with the debug transport
	// when the caller asked for it. We don't share the http.Client
	// between commands so a per-command --http-timeout doesn't leak.
	httpClient := &http.Client{
		Timeout: DefaultHTTPTimeout,
	}
	switch {
	case cfg.HTTPTimeout < 0:
		httpClient.Timeout = 0
	case cfg.HTTPTimeout > 0:
		httpClient.Timeout = cfg.HTTPTimeout
	}
	if cfg.Debug != nil {
		httpClient.Transport = NewDebugTransport(httpClient.Transport, cfg.Debug)
	}

	opts := []option.RequestOption{
		option.WithToken(cfg.APIKey),
		option.WithHTTPHeader(headers),
		option.WithHTTPClient(httpClient),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return sdkclient.NewClient(opts...), nil
}

// APIError captures the fields the CLI's error printer needs: the
// server-side error code, message, and request ID, plus the HTTP
// status code for fallback formatting. We construct it lazily from
// whichever shape the SDK returned.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Retryable  bool
	RequestID  string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("extend api: %s: %s (request_id=%s)", e.Code, e.Message, e.RequestID)
	}
	return fmt.Sprintf("extend api: http %d: %s (request_id=%s)", e.StatusCode, e.Message, e.RequestID)
}

// AsAPIError extracts the CLI's error shape from any SDK error. It
// understands the typed wrappers the SDK emits for known status codes
// (ForbiddenError, PaymentRequiredError, UnprocessableEntityError —
// the three with a typed *extend.APIError body) as well as the
// generic *sdkcore.APIError that wraps unknown statuses. Returns
// (nil, false) if err is not an API-shaped error.
func AsAPIError(err error) (*APIError, bool) {
	if err == nil {
		return nil, false
	}

	// Try the typed wrappers first: they carry a fully-parsed
	// *extend.APIError body for free.
	if apiErr, ok := extractTypedBody(err); ok {
		return apiErr, true
	}

	// Fall back to *sdkcore.APIError. Status + header are present;
	// the body is in err.err as a raw string. Most non-typed status
	// codes (400, 401, 404, 429, 500) still return a server-shaped
	// payload — surface it best-effort.
	var coreErr *sdkcore.APIError
	if errors.As(err, &coreErr) {
		out := &APIError{
			StatusCode: coreErr.StatusCode,
			RequestID:  coreErr.Header.Get("x-extend-request-id"),
		}
		// The wrapped err's string is the raw response body. Parse
		// it if it looks like the standard error envelope.
		populateFromBodyString(out, coreErr.Unwrap())
		if out.Message == "" {
			out.Message = http.StatusText(coreErr.StatusCode)
		}
		return out, true
	}
	return nil, false
}

// IsNotFound reports whether err is a 404 from the API. Used by
// commands like `extend runs get` to surface "no such run" hints.
func IsNotFound(err error) bool {
	if apiErr, ok := AsAPIError(err); ok {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// extractTypedBody pulls out the populated APIError from the three
// status-typed errors that carry a *extend.APIError body
// (Forbidden/PaymentRequired/UnprocessableEntity). Returns false for
// the typed wrappers whose body is `any` instead of *extend.APIError.
func extractTypedBody(err error) (*APIError, bool) {
	var forbidden *extend.ForbiddenError
	if errors.As(err, &forbidden) {
		return apiErrorFromTypedBody(forbidden.StatusCode, forbidden.Header, forbidden.Body), true
	}
	var payment *extend.PaymentRequiredError
	if errors.As(err, &payment) {
		return apiErrorFromTypedBody(payment.StatusCode, payment.Header, payment.Body), true
	}
	var entity *extend.UnprocessableEntityError
	if errors.As(err, &entity) {
		return apiErrorFromTypedBody(entity.StatusCode, entity.Header, entity.Body), true
	}
	return nil, false
}

func apiErrorFromTypedBody(status int, header http.Header, body *extend.APIError) *APIError {
	out := &APIError{StatusCode: status}
	if header != nil {
		out.RequestID = header.Get("x-extend-request-id")
	}
	if body != nil {
		out.Code = body.Code
		out.Message = body.Message
		out.Retryable = body.Retryable
		if body.RequestID != nil && *body.RequestID != "" {
			out.RequestID = *body.RequestID
		}
	}
	if out.Message == "" {
		out.Message = http.StatusText(status)
	}
	return out
}

// populateFromBodyString best-effort parses a string body that looks
// like {"code": "...", "message": "...", "requestId": "..."} or
// {"error": {...}} and fills the corresponding APIError fields.
// Anything else is treated as a free-form message.
func populateFromBodyString(out *APIError, bodyErr error) {
	if bodyErr == nil {
		return
	}
	s := bodyErr.Error()
	if s == "" {
		return
	}
	// Cheap structural check first to avoid pulling encoding/json
	// into a hot error path when the body is plain text.
	if !strings.HasPrefix(strings.TrimSpace(s), "{") {
		out.Message = s
		return
	}
	if parseStandardError(out, s) {
		return
	}
	out.Message = s
}

// parseStandardError decodes the {code, message, retryable,
// requestId} envelope or the {error: {...}} wrapper. We hand-roll
// the parser instead of importing encoding/json into this file
// because the SDK depends on it transitively; either is fine.
func parseStandardError(out *APIError, body string) bool {
	// Stupid-simple: look for "code":" segment and lift the value.
	// Good enough for the error path; structurally invalid JSON
	// falls through to message=full-body.
	code := scrapeStringField(body, `"code"`)
	msg := scrapeStringField(body, `"message"`)
	rid := scrapeStringField(body, `"requestId"`)
	if code == "" && msg == "" && rid == "" {
		return false
	}
	if code != "" {
		out.Code = code
	}
	if msg != "" {
		out.Message = msg
	}
	if rid != "" {
		out.RequestID = rid
	}
	return true
}

// scrapeStringField finds the first occurrence of `key:"value"` in
// body and returns value. Returns "" if not found. Handles escaped
// quotes by simply not matching past them — for our error envelopes
// this is good enough.
func scrapeStringField(body, key string) string {
	idx := strings.Index(body, key)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(key):]
	colon := strings.IndexByte(rest, ':')
	if colon < 0 {
		return ""
	}
	rest = strings.TrimLeft(rest[colon+1:], " ")
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	rest = rest[1:]
	end := indexUnescapedQuote(rest)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func indexUnescapedQuote(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

// SetUserAgent overrides the default UserAgent. The cli package wires
// this once at init so internal/version's Short() is reflected without
// importing version here.
func SetUserAgent(ua string) {
	if ua != "" {
		UserAgent = ua
	}
}

// UploadOption returns a per-request option that swaps the SDK's
// configured *http.Client for an untimed one for the duration of a
// single call. Use on file uploads: end-to-end http.Client timeouts
// are counterproductive for large multipart bodies — a legitimate
// 100MB PDF over a flaky connection will exceed any reasonable
// per-request budget and surface as a vague "context deadline
// exceeded" rather than a fixable error. The caller's context is
// the only deadline that remains in effect.
//
// The Authorization, x-extend-api-version, X-Extend-Workspace-Id,
// and User-Agent headers are still attached by the SDK because they
// live on RequestOptions, not on the HTTP client; only the timeout
// knob changes.
func UploadOption() option.RequestOption {
	return option.WithHTTPClient(&http.Client{})
}
