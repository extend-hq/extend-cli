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

// DefaultClientID is the static first-party public client the Extend
// API registers for the CLI. Public client: PKCE required, no secret,
// loopback redirects only. Migration-seeded identically in every
// environment, so it is never overridden.
const DefaultClientID = "extend-cli"

// TokenResponse is the token endpoint's success payload for both the
// authorization_code and refresh_token grants.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// TokenError is the token endpoint's RFC 6749 error payload, carried
// with the HTTP status for callers that branch on it (invalid_grant
// means the refresh token is dead and the user must log in again).
type TokenError struct {
	StatusCode  int
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *TokenError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("oauth token endpoint: %s: %s", e.Code, e.Description)
	}
	if e.Code != "" {
		return fmt.Sprintf("oauth token endpoint: %s", e.Code)
	}
	return fmt.Sprintf("oauth token endpoint: http %d", e.StatusCode)
}

// Client performs the HTTP calls of the OAuth flow: code exchange,
// refresh, and revocation. It is deliberately independent of the SDK
// client since these endpoints sit outside the versioned API surface.
type Client struct {
	HTTPClient *http.Client
	Endpoints  Endpoints
	ClientID   string
	// Resource is the RFC 8707 resource indicator, the API base URL
	// this login targets. The server requires it on authorize, exchange,
	// and refresh.
	Resource string
}

// Exchange redeems an authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
		"client_id":     {c.ClientID},
		"resource":      {c.Resource},
	}
	return c.postToken(ctx, form)
}

// Refresh redeems a refresh token for a new token pair. The server
// rotates refresh tokens: the response carries a new one that replaces
// the token sent here, so callers must persist the returned pair.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.ClientID},
		"resource":      {c.Resource},
	}
	return c.postToken(ctx, form)
}

// Revoke invalidates a refresh token (and with it the whole grant
// family) per RFC 7009.
func (c *Client) Revoke(ctx context.Context, refreshToken string) error {
	form := url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {c.ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoints.Revocation, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseTokenError(resp)
	}
	return nil
}

func (c *Client) postToken(ctx context.Context, form url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoints.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, parseTokenError(resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	return &tr, nil
}

func parseTokenError(resp *http.Response) error {
	te := &TokenError{StatusCode: resp.StatusCode}
	// Error bodies feed error messages only; 64KB is plenty and keeps
	// a misbehaving endpoint (or intercepting proxy) from streaming an
	// unbounded body into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err == nil && len(body) > 0 {
		// Best-effort decode; a non-JSON body (proxy error page)
		// surfaces as the description.
		if json.Unmarshal(body, te) != nil || (te.Code == "" && te.Description == "") {
			te.Description = strings.TrimSpace(string(body))
		}
	}
	return te
}
