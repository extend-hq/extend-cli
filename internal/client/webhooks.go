package client

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// WebhookEndpoint is the response for /webhook_endpoints. SigningSecret is
// populated only on create — read responses omit it. The server does not
// return `description` or `updatedAt` for endpoints; we therefore don't model
// them. APIVersion is required by the create schema (no server default).
type WebhookEndpoint struct {
	ID              string                  `json:"id"`
	Object          string                  `json:"object"`
	URL             string                  `json:"url"`
	Name            string                  `json:"name,omitempty"`
	Status          string                  `json:"status,omitempty"`
	EnabledEvents   []string                `json:"enabledEvents,omitempty"`
	APIVersion      string                  `json:"apiVersion,omitempty"`
	AdvancedOptions *WebhookAdvancedOptions `json:"advancedOptions,omitempty"`
	SigningSecret   string                  `json:"signingSecret,omitempty"`
	CreatedAt       string                  `json:"createdAt,omitempty"`
}

// WebhookAdvancedOptions configures custom delivery headers and payload format
// for a webhook endpoint. The Payload.Format == "url" mode triggers off-payload
// delivery once the body exceeds UrlThresholdBytes (defaults to a server-side
// constant when omitted).
type WebhookAdvancedOptions struct {
	Headers map[string]string      `json:"headers,omitempty"`
	Payload *WebhookPayloadOptions `json:"payload,omitempty"`
}

type WebhookPayloadOptions struct {
	Format            string `json:"format"`
	UrlThresholdBytes *int   `json:"urlThresholdBytes,omitempty"`
}

// CreateWebhookEndpointInput is the request body for POST /webhook_endpoints.
// The server schema is .strict() — any extra fields cause 400. Status defaults
// to "enabled" server-side when omitted.
type CreateWebhookEndpointInput struct {
	URL             string                  `json:"url"`
	Name            string                  `json:"name"`
	Status          string                  `json:"status,omitempty"`
	EnabledEvents   []string                `json:"enabledEvents"`
	APIVersion      string                  `json:"apiVersion"`
	AdvancedOptions *WebhookAdvancedOptions `json:"advancedOptions,omitempty"`
}

// UpdateWebhookEndpointInput is the request body for POST /webhook_endpoints/:id.
// All fields optional; .strict() schema rejects unknown keys.
type UpdateWebhookEndpointInput struct {
	URL             string                  `json:"url,omitempty"`
	Name            string                  `json:"name,omitempty"`
	Status          string                  `json:"status,omitempty"`
	EnabledEvents   []string                `json:"enabledEvents,omitempty"`
	AdvancedOptions *WebhookAdvancedOptions `json:"advancedOptions,omitempty"`
}

func (c *Client) ListWebhookEndpoints(ctx context.Context, opts ListProcessorsOptions) (*ListResponse[*WebhookEndpoint], error) {
	var out ListResponse[*WebhookEndpoint]
	if err := c.getJSON(ctx, "/webhook_endpoints"+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetWebhookEndpoint(ctx context.Context, id string) (*WebhookEndpoint, error) {
	var ep WebhookEndpoint
	if err := c.getJSON(ctx, "/webhook_endpoints/"+id, &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

func (c *Client) CreateWebhookEndpoint(ctx context.Context, in CreateWebhookEndpointInput) (*WebhookEndpoint, error) {
	var ep WebhookEndpoint
	if err := c.postJSON(ctx, "/webhook_endpoints", in, &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

func (c *Client) UpdateWebhookEndpoint(ctx context.Context, id string, in UpdateWebhookEndpointInput) (*WebhookEndpoint, error) {
	var ep WebhookEndpoint
	if err := c.postJSON(ctx, "/webhook_endpoints/"+id, in, &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

func (c *Client) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/webhook_endpoints/"+id, nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// WebhookSubscription is the response shape for /webhook_subscriptions. The
// endpoint reference is keyed `webhookEndpointId` (not `endpointId`).
// resourceType is one of "extractor" | "classifier" | "splitter" | "workflow".
type WebhookSubscription struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	WebhookEndpointID string   `json:"webhookEndpointId,omitempty"`
	ResourceType      string   `json:"resourceType,omitempty"`
	ResourceID        string   `json:"resourceId,omitempty"`
	EnabledEvents     []string `json:"enabledEvents,omitempty"`
	CreatedAt         string   `json:"createdAt,omitempty"`
}

// CreateWebhookSubscriptionInput is the request body for POST
// /webhook_subscriptions. The server schema is .strict() — every field is
// required and unknown keys cause 400.
type CreateWebhookSubscriptionInput struct {
	WebhookEndpointID string   `json:"webhookEndpointId"`
	ResourceType      string   `json:"resourceType"`
	ResourceID        string   `json:"resourceId"`
	EnabledEvents     []string `json:"enabledEvents"`
}

// UpdateWebhookSubscriptionInput is the request body for POST
// /webhook_subscriptions/:id. Only enabledEvents is mutable.
type UpdateWebhookSubscriptionInput struct {
	EnabledEvents []string `json:"enabledEvents"`
}

func (c *Client) ListWebhookSubscriptions(ctx context.Context, opts ListProcessorsOptions) (*ListResponse[*WebhookSubscription], error) {
	var out ListResponse[*WebhookSubscription]
	if err := c.getJSON(ctx, "/webhook_subscriptions"+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetWebhookSubscription(ctx context.Context, id string) (*WebhookSubscription, error) {
	var s WebhookSubscription
	if err := c.getJSON(ctx, "/webhook_subscriptions/"+id, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) CreateWebhookSubscription(ctx context.Context, in CreateWebhookSubscriptionInput) (*WebhookSubscription, error) {
	var s WebhookSubscription
	if err := c.postJSON(ctx, "/webhook_subscriptions", in, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) UpdateWebhookSubscription(ctx context.Context, id string, in UpdateWebhookSubscriptionInput) (*WebhookSubscription, error) {
	var s WebhookSubscription
	if err := c.postJSON(ctx, "/webhook_subscriptions/"+id, in, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// WebhookSubscriptionResourceTypeFromID infers the server-side resource type
// from the resource ID prefix (`ex_`, `cl_`, `spl_`, `workflow_`). Returns
// the empty string if the prefix is unrecognized; callers should then require
// the user to pass --resource-type explicitly.
func WebhookSubscriptionResourceTypeFromID(resourceID string) string {
	switch {
	case strings.HasPrefix(resourceID, "ex_"):
		return "extractor"
	case strings.HasPrefix(resourceID, "cl_"):
		return "classifier"
	case strings.HasPrefix(resourceID, "spl_"):
		return "splitter"
	case strings.HasPrefix(resourceID, "workflow_"):
		return "workflow"
	}
	return ""
}

func (c *Client) DeleteWebhookSubscription(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/webhook_subscriptions/"+id, nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// VerifyWebhookSignature reproduces the server's signing scheme:
//
//	message   = "v0:" + timestamp + ":" + body
//	signature = hex(HMAC_SHA256(secret, message))
//
// timestamp must be within maxAge of now; pass 0 to skip the time check.
func VerifyWebhookSignature(secret, signature, timestamp string, body []byte, maxAge time.Duration) error {
	if secret == "" {
		return errors.New("signing secret is empty")
	}
	if signature == "" {
		return errors.New("signature is empty")
	}
	if timestamp == "" {
		return errors.New("timestamp is empty")
	}
	if maxAge > 0 {
		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid timestamp %q: %w", timestamp, err)
		}
		age := time.Since(time.Unix(ts, 0))
		if age < 0 {
			age = -age
		}
		if age > maxAge {
			return fmt.Errorf("timestamp is %s old, exceeds maxAge %s", age, maxAge)
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return errors.New("signature mismatch")
	}
	return nil
}
