package extendx

import (
	"context"
	"net/http"
)

// BearerSource supplies OAuth access tokens for API requests. It is the
// small interface extendx needs from internal/oauth's TokenSource,
// declared here so extendx stays decoupled from the flow machinery and
// tests can substitute fakes.
type BearerSource interface {
	// AccessToken returns a currently valid access token, refreshing
	// silently when the cached one has expired.
	AccessToken(ctx context.Context) (string, error)
	// ForceRefresh reports that the given token was rejected with a 401
	// and returns a replacement (refreshing at most once; a concurrent
	// refresh's newer token is reused).
	ForceRefresh(ctx context.Context, rejected string) (string, error)
}

// bearerTransport injects "Authorization: Bearer <access token>" into
// every request and, on a 401, refreshes once and retries. It sits
// outermost on the client's transport chain so the debug transport
// underneath logs both attempts.
type bearerTransport struct {
	next http.RoundTripper
	src  BearerSource
}

func newBearerTransport(next http.RoundTripper, src BearerSource) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &bearerTransport{next: next, src: src}
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.src.AccessToken(req.Context())
	if err != nil {
		return nil, err
	}
	attempt := req.Clone(req.Context())
	attempt.Header.Set("Authorization", "Bearer "+tok)
	resp, err := t.next.RoundTrip(attempt)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}

	// One retry after a forced refresh. Requests with a body need
	// GetBody to replay it; streaming bodies (large uploads) cannot be
	// replayed, so the 401 stands.
	if req.Body != nil && req.GetBody == nil {
		return resp, nil
	}
	newTok, refreshErr := t.src.ForceRefresh(req.Context(), tok)
	if refreshErr != nil {
		resp.Body.Close()
		return nil, refreshErr
	}
	resp.Body.Close()

	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		retry.Body = body
	}
	retry.Header.Set("Authorization", "Bearer "+newTok)
	return t.next.RoundTrip(retry)
}
