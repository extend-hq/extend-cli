package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Endpoints holds the three authorization-server endpoints the CLI
// talks to. All are absolute URLs.
type Endpoints struct {
	Authorization string
	Token         string
	Revocation    string
}

// DefaultEndpoints returns the hardcoded /oauth2/* endpoints for an API
// base URL. Used directly when RFC 8414 discovery is unavailable.
func DefaultEndpoints(apiBase string) Endpoints {
	base := NormalizeBase(apiBase)
	return Endpoints{
		Authorization: base + "/oauth2/authorize",
		Token:         base + "/oauth2/token",
		Revocation:    base + "/oauth2/revoke",
	}
}

// metadata is the subset of the RFC 8414 authorization-server metadata
// document the CLI consumes.
type metadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
}

// Discover fetches {apiBase}/.well-known/oauth-authorization-server and
// returns the advertised endpoints. Discovery is best-effort per the
// wire contract: any fetch failure (404 from a server predating
// discovery, a network error, malformed metadata) falls back to
// DefaultEndpoints. Fields missing from an otherwise valid document
// also fall back per-field.
//
// Successfully fetched metadata is host-pinned: every advertised
// endpoint must live on the API base URL's host. A well-known document
// pointing at a foreign host is not a soft failure to fall back from —
// it would send the authorization code and PKCE verifier to that host —
// so it is rejected with an error.
func Discover(ctx context.Context, client *http.Client, apiBase string) (Endpoints, error) {
	fallback := DefaultEndpoints(apiBase)
	wellKnown := NormalizeBase(apiBase) + "/.well-known/oauth-authorization-server"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return fallback, nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return fallback, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fallback, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fallback, nil
	}
	var m metadata
	if err := json.Unmarshal(body, &m); err != nil {
		return fallback, nil
	}
	out := fallback
	for _, ep := range []struct {
		name string
		val  string
		dst  *string
	}{
		{"authorization_endpoint", m.AuthorizationEndpoint, &out.Authorization},
		{"token_endpoint", m.TokenEndpoint, &out.Token},
		{"revocation_endpoint", m.RevocationEndpoint, &out.Revocation},
	} {
		if ep.val == "" {
			continue
		}
		if err := validateEndpointHost(ep.name, ep.val, apiBase); err != nil {
			return Endpoints{}, err
		}
		*ep.dst = ep.val
	}
	return out, nil
}

// validateEndpointHost enforces the discovery host pin: the endpoint
// must be on the API base's exact host (including port), over https or
// the base's own scheme (which permits plain http only for local dev
// bases that are themselves http).
func validateEndpointHost(name, endpoint, apiBase string) error {
	base, err := url.Parse(NormalizeBase(apiBase))
	if err != nil {
		return fmt.Errorf("oauth discovery: parse api base url %q: %w", apiBase, err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("oauth discovery: %s %q is not a valid url: %w", name, endpoint, err)
	}
	if u.Host != base.Host {
		return fmt.Errorf("oauth discovery: %s %q is not on the API host %q; refusing to use it", name, endpoint, base.Host)
	}
	if u.Scheme != "https" && u.Scheme != base.Scheme {
		return fmt.Errorf("oauth discovery: %s %q must use https; refusing to use it", name, endpoint)
	}
	return nil
}

// NormalizeBase canonicalizes an API base URL for use as a storage key,
// a resource parameter, and an endpoint prefix: trailing slashes are
// stripped so "https://api.extend.ai/" and "https://api.extend.ai"
// address the same login.
func NormalizeBase(apiBase string) string {
	return strings.TrimRight(strings.TrimSpace(apiBase), "/")
}
