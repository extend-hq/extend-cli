package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/extend-hq/extend-cli/internal/iostreams"
)

// DeviceCodeGrant is the RFC 8628 device_code grant type URN sent to
// the token endpoint while polling for the user's approval.
const DeviceCodeGrant = "urn:ietf:params:oauth:grant-type:device_code"

// defaultDevicePollInterval applies when the device authorization
// response omits interval, per RFC 8628 §3.2.
const defaultDevicePollInterval = 5 * time.Second

// DeviceAuthorization is the RFC 8628 device authorization response:
// the code pair and where the user goes to approve the sign-in.
type DeviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

// DeviceAuthorize starts an RFC 8628 device authorization grant. The
// returned user code and verification URI are shown to the user, who
// approves the sign-in from a browser on any device; PollDeviceToken
// then waits for that approval.
func (c *Client) DeviceAuthorize(ctx context.Context) (*DeviceAuthorization, error) {
	if c.Endpoints.DeviceAuthorization == "" {
		return nil, fmt.Errorf("the authorization server does not advertise a device authorization endpoint")
	}
	form := url.Values{
		"client_id": {c.ClientID},
		"resource":  {c.Resource},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoints.DeviceAuthorization, strings.NewReader(form.Encode()))
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
		return nil, fmt.Errorf("read device authorization response: %w", err)
	}
	var da DeviceAuthorization
	if err := json.Unmarshal(body, &da); err != nil {
		return nil, fmt.Errorf("parse device authorization response: %w", err)
	}
	if da.DeviceCode == "" || da.UserCode == "" || da.VerificationURI == "" {
		return nil, fmt.Errorf("device authorization response missing device_code, user_code, or verification_uri")
	}
	// These strings land verbatim on the user's terminal; neutralize
	// escape sequences at the source like parseTokenError does.
	da.UserCode = iostreams.SanitizeForTerminal(da.UserCode)
	da.VerificationURI = iostreams.SanitizeForTerminal(da.VerificationURI)
	da.VerificationURIComplete = iostreams.SanitizeForTerminal(da.VerificationURIComplete)
	return &da, nil
}

// PollDeviceToken polls the token endpoint with the device_code grant
// until the user approves the sign-in, the code expires, or ctx ends.
// authorization_pending keeps polling; slow_down backs the interval
// off by 5s per RFC 8628 §3.5. Any other token error (access_denied,
// expired_token, ...) is returned as a *TokenError for the caller to
// present.
func (c *Client) PollDeviceToken(ctx context.Context, da *DeviceAuthorization) (*TokenResponse, error) {
	if da.ExpiresIn > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(da.ExpiresIn)*time.Second)
		defer cancel()
	}
	interval := time.Duration(da.Interval) * time.Second
	if interval <= 0 {
		interval = defaultDevicePollInterval
	}
	form := url.Values{
		"grant_type":  {DeviceCodeGrant},
		"device_code": {da.DeviceCode},
		"client_id":   {c.ClientID},
		"resource":    {c.Resource},
	}
	// First poll fires immediately; approval cannot have happened yet,
	// but a wasted request is cheaper than always sleeping a full
	// interval on servers that answer pending instantly.
	for {
		tr, err := c.postToken(ctx, form)
		if err == nil {
			return tr, nil
		}
		var te *TokenError
		if errors.As(err, &te) {
			switch te.Code {
			case "authorization_pending":
			case "slow_down":
				interval += 5 * time.Second
			default:
				return nil, err
			}
		} else {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}
