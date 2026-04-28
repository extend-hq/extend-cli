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
)

const (
	DefaultBaseURL    = "https://api.extend.ai"
	DefaultAPIVersion = "2026-02-09"
	userAgent         = "extend-cli/0.1"
)

type Client struct {
	BaseURL     string
	APIKey      string
	APIVersion  string
	WorkspaceID string
	HTTP        *http.Client
}

func New(apiKey string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		APIKey:     apiKey,
		APIVersion: DefaultAPIVersion,
		HTTP:       &http.Client{Timeout: 60 * time.Second},
	}
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
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("x-extend-api-version", c.APIVersion)
	req.Header.Set("User-Agent", userAgent)
	if c.WorkspaceID != "" {
		req.Header.Set("X-Extend-Workspace-Id", c.WorkspaceID)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}
	return resp, nil
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

	const maxAttempts = 4
	backoff := 500 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 5*time.Second)
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
	const maxAttempts = 4
	backoff := 500 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 5*time.Second)
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
