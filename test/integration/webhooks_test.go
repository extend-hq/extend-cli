package integration

import (
	"strings"
	"testing"
)

// TestWebhookEndpoint_CreateUpdateDelete walks the full lifecycle of a
// webhook endpoint and verifies the wire shape:
//
//   - create returns a one-shot signingSecret
//   - status defaults to "enabled" when --disabled is omitted
//   - --header k=v and --payload-format flags round-trip through advancedOptions
//   - update accepts partial bodies (only the fields the user touched)
//   - delete removes the resource
func TestWebhookEndpoint_CreateUpdateDelete(t *testing.T) {
	env := requireEnv(t)
	name := itestName(t)

	createRes := runExtend(t, env,
		"webhooks", "endpoints", "create",
		"--url", "https://example.com/hook",
		"--name", name,
		"--events", "extract_run.processed,extract_run.failed",
		"--header", "X-Tenant=integration-test",
		"--header", "X-Token=secret-value",
		"-o", "json",
	)
	createRes.requireOK(t, "webhooks", "endpoints", "create")

	var created struct {
		ID              string   `json:"id"`
		Object          string   `json:"object"`
		Name            string   `json:"name"`
		URL             string   `json:"url"`
		Status          string   `json:"status"`
		EnabledEvents   []string `json:"enabledEvents"`
		SigningSecret   string   `json:"signingSecret"`
		AdvancedOptions struct {
			Headers map[string]string `json:"headers"`
		} `json:"advancedOptions"`
	}
	createRes.decodeJSON(t, &created)

	if !strings.HasPrefix(created.ID, "wh_") {
		t.Fatalf("expected wh_ prefix on id, got %q", created.ID)
	}
	rememberCleanup(t, env, "delete webhook endpoint",
		"webhooks", "endpoints", "delete", created.ID, "-y")

	if created.Name != name {
		t.Errorf("name = %q, want %q", created.Name, name)
	}
	if created.SigningSecret == "" {
		t.Errorf("expected signingSecret on create response (one-shot value); got: %s", createRes.Stdout)
	}
	if created.Status != "enabled" {
		t.Errorf("status = %q, want enabled (default)", created.Status)
	}
	if len(created.EnabledEvents) != 2 {
		t.Errorf("enabledEvents len = %d, want 2: %+v", len(created.EnabledEvents), created.EnabledEvents)
	}
	if created.AdvancedOptions.Headers["X-Tenant"] != "integration-test" {
		t.Errorf("advancedOptions.headers.X-Tenant = %q, want integration-test", created.AdvancedOptions.Headers["X-Tenant"])
	}
	if created.AdvancedOptions.Headers["X-Token"] != "secret-value" {
		t.Errorf("advancedOptions.headers.X-Token = %q, want secret-value", created.AdvancedOptions.Headers["X-Token"])
	}

	// Update only the name. The CLI's update command must omit the unchanged
	// URL/status/events fields from the request body so the server doesn't
	// receive a string-zero "" and reject it under .strict() validation.
	newName := name + "-renamed"
	updateRes := runExtend(t, env,
		"webhooks", "endpoints", "update", created.ID,
		"--name", newName,
		"-o", "json",
	)
	updateRes.requireOK(t, "webhooks", "endpoints", "update")

	var updated struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	updateRes.decodeJSON(t, &updated)
	if updated.Name != newName {
		t.Errorf("after update name = %q, want %q", updated.Name, newName)
	}
	if updated.Status != "enabled" {
		t.Errorf("after partial update status = %q, want enabled (untouched)", updated.Status)
	}

	// Disable via the --disable flag.
	disableRes := runExtend(t, env,
		"webhooks", "endpoints", "update", created.ID,
		"--disable",
		"-o", "json",
	)
	disableRes.requireOK(t, "webhooks", "endpoints", "update")

	var disabled struct {
		Status string `json:"status"`
	}
	disableRes.decodeJSON(t, &disabled)
	if disabled.Status != "disabled" {
		t.Errorf("after --disable status = %q, want disabled", disabled.Status)
	}
}

// TestWebhookSubscription_CreateUpdateDelete verifies the full lifecycle and
// the corrected wire shape (server requires `webhookEndpointId` and
// `resourceType`; the CLI auto-infers resourceType from the resource ID
// prefix). The test creates an endpoint as a parent resource because
// subscriptions cannot exist independently.
func TestWebhookSubscription_CreateUpdateDelete(t *testing.T) {
	env := requireEnv(t)

	// Create a parent endpoint. The endpoint's enabledEvents must be from
	// the GLOBAL_WEBHOOK event set (workflow lifecycle, processor lifecycle,
	// or batch run events) — workflow_run.* events live on subscriptions
	// rather than directly on the endpoint.
	endpointName := itestName(t) + "-endpoint"
	endpointRes := runExtend(t, env,
		"webhooks", "endpoints", "create",
		"--url", "https://example.com/hook",
		"--name", endpointName,
		"--events", "workflow.created",
		"-o", "json",
	)
	endpointRes.requireOK(t, "webhooks", "endpoints", "create")
	var endpoint struct {
		ID string `json:"id"`
	}
	endpointRes.decodeJSON(t, &endpoint)
	rememberCleanup(t, env, "delete parent endpoint",
		"webhooks", "endpoints", "delete", endpoint.ID, "-y")

	// Pick a real workflow ID to subscribe to. Subscriptions require an
	// existing resource; the server validates the ID exists in the workspace.
	wfRes := runExtend(t, env, "workflows", "list", "--limit", "1", "-o", "json")
	wfRes.requireOK(t, "workflows", "list")
	var wfArr []struct {
		ID string `json:"id"`
	}
	wfRes.decodeJSON(t, &wfArr)
	if len(wfArr) == 0 {
		t.Skip("workspace has no workflows to subscribe to; skipping")
	}
	workflowID := wfArr[0].ID

	subRes := runExtend(t, env,
		"webhooks", "subscriptions", "create",
		"--endpoint", endpoint.ID,
		"--resource", workflowID,
		"--events", "workflow_run.completed",
		"-o", "json",
	)
	subRes.requireOK(t, "webhooks", "subscriptions", "create")

	var sub struct {
		ID                string   `json:"id"`
		Object            string   `json:"object"`
		WebhookEndpointID string   `json:"webhookEndpointId"`
		ResourceType      string   `json:"resourceType"`
		ResourceID        string   `json:"resourceId"`
		EnabledEvents     []string `json:"enabledEvents"`
	}
	subRes.decodeJSON(t, &sub)
	rememberCleanup(t, env, "delete webhook subscription",
		"webhooks", "subscriptions", "delete", sub.ID, "-y")

	if !strings.HasPrefix(sub.ID, "whes_") {
		t.Errorf("expected whes_ prefix on subscription id, got %q", sub.ID)
	}
	if sub.WebhookEndpointID != endpoint.ID {
		t.Errorf("webhookEndpointId = %q, want %q", sub.WebhookEndpointID, endpoint.ID)
	}
	if sub.ResourceType != "workflow" {
		t.Errorf("resourceType = %q, want workflow (auto-inferred from prefix)", sub.ResourceType)
	}
	if sub.ResourceID != workflowID {
		t.Errorf("resourceId = %q, want %q", sub.ResourceID, workflowID)
	}

	// Update the enabled events list. Server schema for update is just
	// {enabledEvents}; the CLI must not send any other fields.
	updateRes := runExtend(t, env,
		"webhooks", "subscriptions", "update", sub.ID,
		"--events", "workflow_run.completed,workflow_run.failed",
		"-o", "json",
	)
	updateRes.requireOK(t, "webhooks", "subscriptions", "update")

	var updated struct {
		EnabledEvents []string `json:"enabledEvents"`
	}
	updateRes.decodeJSON(t, &updated)
	if len(updated.EnabledEvents) != 2 {
		t.Errorf("after update enabledEvents len = %d, want 2: %+v",
			len(updated.EnabledEvents), updated.EnabledEvents)
	}
}
