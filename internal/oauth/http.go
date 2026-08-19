package oauth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// NewHTTPClient returns the HTTP client used for every OAuth call
// against apiBase: discovery, code exchange, refresh, and revocation.
//
// Its redirect policy pins the flow to the API base's origin. Go's
// default client follows 307/308 redirects by replaying the original
// request — including the body, which on these calls carries the
// authorization code and PKCE verifier, a live refresh token, or the
// token being revoked. Followed blindly, a redirect off the API host
// would re-send that material to whatever host the Location header
// names. So every hop is checked: a target whose origin (scheme +
// host + port) differs from apiBase is refused with an error, and
// same-origin hops are re-validated one by one.
func NewHTTPClient(apiBase string) *http.Client {
	return &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: CheckSameOriginRedirect(apiBase),
	}
}

// CheckSameOriginRedirect builds an http.Client CheckRedirect that
// admits only redirects staying on apiBase's origin. It is called by
// the client before each hop of a chain, so the origin is enforced on
// every hop, not just the first. Install it on any client whose
// requests carry credentials bound to apiBase: the OAuth clients here
// re-send token material in request bodies, and the API client's
// transport re-attaches the Authorization header on every hop.
func CheckSameOriginRedirect(apiBase string) func(req *http.Request, via []*http.Request) error {
	base, baseErr := url.Parse(NormalizeBase(apiBase))
	return func(req *http.Request, via []*http.Request) error {
		// Re-impose the default chain limit, which installing a
		// custom CheckRedirect would otherwise replace.
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if baseErr != nil {
			return fmt.Errorf("refusing redirect: parse api base url %q: %w", apiBase, baseErr)
		}
		if req.URL.Scheme != base.Scheme || req.URL.Host != base.Host {
			return fmt.Errorf("refusing redirect to %s://%s: not the pinned API origin %s://%s",
				req.URL.Scheme, req.URL.Host, base.Scheme, base.Host)
		}
		return nil
	}
}
