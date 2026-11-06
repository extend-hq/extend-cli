package extendx

import (
	"context"
	"encoding/json"
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

	"github.com/extend-hq/extend-cli/internal/iostreams"
	"github.com/extend-hq/extend-cli/internal/oauth"
)

// DefaultHTTPTimeout is applied to the HTTP client every API call goes
// through, except uploads (see withUpload). Tuned for the common
// case of small JSON request/response payloads.
const DefaultHTTPTimeout = 60 * time.Second

// DefaultUserAgent is the User-Agent sent when Config.UserAgent is
// empty. Cli callers should set Config.UserAgent to a version-aware
// string ("extend-cli/<version>") so server logs identify the CLI
// release; this constant exists as a safe fallback.
const DefaultUserAgent = "extend-cli/dev"

// Config is the CLI-side, env-resolved view of all knobs that affect
// how the SDK client is built. NewClient consumes it once per command
// invocation.
//
// The zero value of every field is meaningful: unset means "use the
// SDK / region default". Resolution precedence (flag > env > default)
// is the caller's responsibility — populate these from the App
// struct's already-merged values before calling NewClient.
type Config struct {
	// APIKey is the bearer token. Either it or OAuth is required for
	// any command that actually hits the API; APIKey wins when both
	// are set.
	APIKey string
	// OAuth supplies access tokens from a stored `extend login` when
	// no API key resolves. Attached as a transport that injects the
	// bearer header, silently refreshes on expiry, and retries once
	// on a 401.
	OAuth BearerSource
	// TokenContext scopes token refreshes triggered through the SDK's
	// token func, whose signature takes no context of its own. Pass
	// the command's signal-aware context so Ctrl-C aborts an in-flight
	// refresh; nil falls back to context.Background(). The bearer
	// transport is unaffected — it always uses the request's context.
	TokenContext context.Context
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
	// UserAgent overrides the User-Agent header. Empty falls back to
	// DefaultUserAgent. The CLI sets this to "extend-cli/<version>"
	// so server-side analytics see the CLI release.
	UserAgent string
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
	if cfg.APIKey == "" && cfg.OAuth == nil {
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
	if baseURL != "" {
		// Every request to the base carries a bearer; refuse bases
		// that would send it in cleartext (http to a remote host).
		if err := oauth.ValidateBaseURL(baseURL); err != nil {
			return nil, err
		}
	}

	// X-Extend-Workspace-Id has no SDK option, so it rides on the
	// SDK's default headers. User-Agent and x-extend-api-version can't:
	// the SDK overwrites both on every request, so headerTransport sets
	// them instead.
	headers := http.Header{}
	if cfg.WorkspaceID != "" {
		headers.Set("X-Extend-Workspace-Id", cfg.WorkspaceID)
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}

	// Build the underlying http.Client. Wrap with the debug transport
	// when the caller asked for it. We don't share the http.Client
	// between commands so a per-command --http-timeout doesn't leak.
	//
	// Redirects are pinned to the API base's origin. Both auth paths
	// put a live bearer on redirect hops — Go's client copies the
	// Authorization header to same-registrable-host targets, and the
	// bearer transport below re-attaches a fresh token on every hop —
	// so an off-origin Location header (open redirect, compromised
	// endpoint, misconfigured CDN) would hand the credential to
	// whatever host it names. Refuse those hops instead.
	pinnedBase := baseURL
	if pinnedBase == "" {
		// Matches the base the SDK falls back to when no
		// option.WithBaseURL is set.
		pinnedBase = extend.Environments.Production
	}
	httpClient := &http.Client{
		Transport: &headerTransport{
			next:       http.DefaultTransport,
			userAgent:  ua,
			apiVersion: cfg.APIVersion,
		},
		Timeout:       DefaultHTTPTimeout,
		CheckRedirect: oauth.CheckSameOriginRedirect(pinnedBase),
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

	opts := []option.RequestOption{option.WithHTTPHeader(headers)}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithToken(cfg.APIKey))
	} else {
		// WithTokenFunc makes the SDK stamp a fresh token on every
		// request; the bearer transport (outermost, so the debug
		// transport logs both attempts) adds the 401 refresh-and-retry.
		src := cfg.OAuth
		tokenCtx := cfg.TokenContext
		if tokenCtx == nil {
			tokenCtx = context.Background()
		}
		opts = append(opts, option.WithTokenFunc(func() (string, error) {
			return src.AccessToken(tokenCtx)
		}))
		httpClient.Transport = newBearerTransport(httpClient.Transport, cfg.OAuth)
	}
	uploadClient := uploadHTTPClient()
	uploadClient.Transport = httpClient.Transport
	opts = append(opts, option.WithHTTPClient(&routingClient{api: httpClient, upload: uploadClient}))
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

	// An already-extracted *APIError (or one wrapped in the chain)
	// passes through unchanged — our own extractor should recognize our
	// own type, which also lets callers and tests construct one directly.
	var ownErr *APIError
	if errors.As(err, &ownErr) {
		return ownErr, true
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
		return sanitized(out), true
	}
	return nil, false
}

// sanitized neutralizes terminal escape sequences in the
// server-controlled fields of an APIError. Applied at construction so
// every path that renders the error — the CLI error printer as well as
// %v/%w chains through Error() — inherits it.
func sanitized(e *APIError) *APIError {
	e.Code = iostreams.SanitizeForTerminal(e.Code)
	e.Message = iostreams.SanitizeForTerminal(e.Message)
	e.RequestID = iostreams.SanitizeForTerminal(e.RequestID)
	return e
}

// IsNotFound reports whether err is a 404 from the API. Used by
// commands like `extend extract runs get` to surface "no such run" hints.
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
	return sanitized(out)
}

// populateFromBodyString best-effort parses a string body that looks
// like {"code": "...", "message": "...", "requestId": "..."} or
// {"error": {...}} and fills the corresponding APIError fields. The
// SDK delivers the raw response body (after status codes it doesn't
// have a typed wrapper for) as the .err of a *core.APIError; we read
// .Error() to recover the body bytes.
//
// Anything that doesn't decode as the standard envelope falls
// through to treating the entire string as the message field, which
// is the right behaviour for plain-text 502s from a CDN.
func populateFromBodyString(out *APIError, bodyErr error) {
	if bodyErr == nil {
		return
	}
	body := bodyErr.Error()
	if body == "" {
		return
	}
	// Cheap structural check first: only attempt JSON parsing when
	// the body actually looks like an object. Plain-text bodies
	// (e.g. CDN-generated 502 HTML or proxy errors) skip the parse
	// and surface verbatim as the message.
	if !strings.HasPrefix(strings.TrimSpace(body), "{") {
		out.Message = body
		return
	}
	// The standard error envelope is {"code","message","requestId","retryable"}.
	// Some endpoints wrap it as {"error": {...}}. Decode into a
	// struct that captures both forms; the parser takes whichever
	// is populated. If neither produces useful fields, fall through
	// to message=body.
	var env struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId"`
		Retryable bool   `json:"retryable"`
		Error     *struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		out.Message = body
		return
	}
	src := env
	if env.Error != nil {
		src.Code = env.Error.Code
		src.Message = env.Error.Message
		src.RequestID = env.Error.RequestID
		src.Retryable = env.Error.Retryable
	}
	if src.Code == "" && src.Message == "" && src.RequestID == "" {
		out.Message = body
		return
	}
	if src.Code != "" {
		out.Code = src.Code
	}
	if src.Message != "" {
		out.Message = src.Message
	}
	if src.RequestID != "" {
		out.RequestID = src.RequestID
	}
	out.Retryable = src.Retryable
}

// headerTransport sets the CLI's User-Agent, and the API version when
// one is configured, on every request.
type headerTransport struct {
	next       http.RoundTripper
	userAgent  string
	apiVersion string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", t.userAgent)
	if t.apiVersion != "" {
		req.Header.Set("x-extend-api-version", t.apiVersion)
	}
	return t.next.RoundTrip(req)
}

// routingClient sends requests made with a withUpload context through
// the upload client and everything else through the API client. Both
// share one transport chain.
type routingClient struct {
	api    *http.Client
	upload *http.Client
}

func (c *routingClient) Do(req *http.Request) (*http.Response, error) {
	if req.Context().Value(uploadKey{}) != nil {
		return c.upload.Do(req)
	}
	return c.api.Do(req)
}

type uploadKey struct{}

// withUpload marks ctx so the request made with it goes through the
// untimed upload client. Use on file uploads: end-to-end http.Client
// timeouts are counterproductive for large multipart bodies — a
// legitimate 100MB PDF over a flaky connection will exceed any
// reasonable per-request budget and surface as a vague "context
// deadline exceeded" rather than a fixable error. The caller's context
// is the only deadline that remains in effect.
func withUpload(ctx context.Context) context.Context {
	return context.WithValue(ctx, uploadKey{}, true)
}

// uploadHTTPClient is the untimed client uploads go through. It
// refuses redirects outright: upload requests carry the Authorization
// header, and following a redirect would replay it (and the multipart
// body) to whatever host the Location header names. The upload
// endpoint never redirects legitimately, so refusing is the safe
// policy.
func uploadHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("refusing redirect to %s://%s on an upload request", req.URL.Scheme, req.URL.Host)
		},
	}
}
