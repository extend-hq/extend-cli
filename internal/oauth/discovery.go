package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
// wire contract: any failure (404 from a server predating discovery, a
// network error, malformed metadata) falls back to DefaultEndpoints.
// Fields missing from an otherwise valid document also fall back
// per-field.
func Discover(ctx context.Context, client *http.Client, apiBase string) Endpoints {
	fallback := DefaultEndpoints(apiBase)
	url := NormalizeBase(apiBase) + "/.well-known/oauth-authorization-server"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fallback
	}
	resp, err := client.Do(req)
	if err != nil {
		return fallback
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fallback
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fallback
	}
	var m metadata
	if err := json.Unmarshal(body, &m); err != nil {
		return fallback
	}
	out := fallback
	if m.AuthorizationEndpoint != "" {
		out.Authorization = m.AuthorizationEndpoint
	}
	if m.TokenEndpoint != "" {
		out.Token = m.TokenEndpoint
	}
	if m.RevocationEndpoint != "" {
		out.Revocation = m.RevocationEndpoint
	}
	return out
}

// NormalizeBase canonicalizes an API base URL for use as a storage key,
// a resource parameter, and an endpoint prefix: trailing slashes are
// stripped so "https://api.extend.ai/" and "https://api.extend.ai"
// address the same login.
func NormalizeBase(apiBase string) string {
	return strings.TrimRight(strings.TrimSpace(apiBase), "/")
}
