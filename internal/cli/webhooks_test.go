package cli

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	extend "github.com/extend-hq/extend-go-sdk"
)

func TestWebhooksVerify_ValidSignature(t *testing.T) {
	tmp := t.TempDir()
	body := []byte(`{"event":"hi"}`)
	bodyPath := filepath.Join(tmp, "body.json")
	if err := os.WriteFile(bodyPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	secret := "wss_test"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signTest(secret, ts, body)

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("verify should not hit the API")
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "verify")
	cmd.SetArgs([]string{
		"--secret", secret,
		"--signature", sig,
		"--timestamp", ts,
		"--body-file", bodyPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !strings.Contains(ta.errOut.String(), "signature valid") {
		t.Errorf("expected 'signature valid' in stderr, got: %q", ta.errOut.String())
	}
}

func TestWebhooksVerify_TamperedBodyFails(t *testing.T) {
	tmp := t.TempDir()
	body := []byte(`{"event":"hi"}`)
	tampered := []byte(`{"event":"BYE"}`)
	bodyPath := filepath.Join(tmp, "body.json")
	if err := os.WriteFile(bodyPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	secret := "wss_test"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signTest(secret, ts, body)

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "verify")
	cmd.SetArgs([]string{
		"--secret", secret,
		"--signature", sig,
		"--timestamp", ts,
		"--body-file", bodyPath,
	})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error on tampered body")
	}
	if !strings.Contains(ta.errOut.String(), "signature mismatch") {
		t.Errorf("expected 'signature mismatch' in stderr, got: %q", ta.errOut.String())
	}
}

func TestWebhooksEndpointsCreate_ParsesCSVAndRepeatedEvents(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "whe_test", "object": "webhook_endpoint"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "endpoints", "create")
	cmd.SetArgs([]string{
		"--url", "https://x.com/hook",
		"--name", "test",
		"--events", "extract_run.processed,extract_run.failed",
		"--events", "classify_run.processed",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}
	body := string(srv.lastRequest().Body)
	for _, want := range []string{"extract_run.processed", "extract_run.failed", "classify_run.processed"} {
		if !strings.Contains(body, `"`+want+`"`) {
			t.Errorf("body missing event %q: %s", want, body)
		}
	}
	// Server schema is .strict() — extra fields cause 400. Confirm we never
	// send the now-removed `description` key.
	if strings.Contains(body, `"description"`) {
		t.Errorf("body must not contain description (server .strict() rejects unknown fields): %s", body)
	}
}

func TestWebhooksEndpointsCreate_DisabledFlagSendsStatus(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "whe_test"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "endpoints", "create")
	cmd.SetArgs([]string{
		"--url", "https://x.com/hook",
		"--name", "test",
		"--disabled",
		"--events", "extract_run.processed",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"status":"disabled"`) {
		t.Errorf("expected status:disabled in body: %s", body)
	}
}

func TestWebhooksEndpointsCreate_AdvancedHeadersAndPayloadFormat(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "wh_test"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "endpoints", "create")
	cmd.SetArgs([]string{
		"--url", "https://x.com/hook",
		"--name", "test",
		"--events", "extract_run.processed",
		"--header", "X-Tenant=acme",
		"--header", "X-Token=secret",
		"--payload-format", "url",
		"--url-threshold-bytes", "1024",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"advancedOptions":{`) {
		t.Errorf("expected advancedOptions in body: %s", body)
	}
	if !strings.Contains(body, `"X-Tenant":"acme"`) || !strings.Contains(body, `"X-Token":"secret"`) {
		t.Errorf("expected both headers in body: %s", body)
	}
	if !strings.Contains(body, `"payload":{"format":"url","urlThresholdBytes":1024}`) {
		t.Errorf("expected payload options in body: %s", body)
	}
}

func TestWebhooksEndpointsCreate_RejectsInvalidPayloadFormat(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when validation fails")
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "endpoints", "create")
	cmd.SetArgs([]string{
		"--url", "https://x.com/h",
		"--name", "t",
		"--events", "extract_run.processed",
		"--payload-format", "xml",
	})
	err := cmd.Execute()
	// The SDK rejects with "is not a valid extend.WebhookPayloadFormat";
	// the CLI wraps with "--payload-format: ...". We assert on the
	// flag name + the rejected value rather than the SDK's enum-type
	// text so this stays stable across SDK regenerations.
	if err == nil || !strings.Contains(err.Error(), "--payload-format") || !strings.Contains(err.Error(), "xml") {
		t.Errorf("expected payload-format error, got %v", err)
	}
}

func TestWebhooksEndpointsCreate_RejectsThresholdWithoutUrlFormat(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when validation fails")
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "endpoints", "create")
	cmd.SetArgs([]string{
		"--url", "https://x.com/h",
		"--name", "t",
		"--events", "extract_run.processed",
		"--url-threshold-bytes", "1024",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "payload-format=url") {
		t.Errorf("expected threshold-without-format error, got %v", err)
	}
}

func TestWebhooksEndpointsUpdate_OnlyChangedFieldsSent(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "whe_test"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "endpoints", "update")
	cmd.SetArgs([]string{"whe_test", "--name", "renamed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"name":"renamed"`) {
		t.Errorf("expected name in body: %s", body)
	}
	for _, unsent := range []string{`"url"`, `"status"`, `"description"`} {
		if strings.Contains(body, unsent) {
			t.Errorf("unset field %s should not appear (omitempty): %s", unsent, body)
		}
	}
}

func TestWebhooksSubscriptionsCreate_ScopesToResource(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "whs_test"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "subscriptions", "create")
	cmd.SetArgs([]string{
		"--endpoint", "whe_xyz",
		"--resource", "workflow_abc",
		"--events", "workflow_run.completed",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"webhookEndpointId":"whe_xyz"`) {
		t.Errorf("missing webhookEndpointId (server schema requires this exact key, not endpointId): %s", body)
	}
	if !strings.Contains(body, `"resourceId":"workflow_abc"`) {
		t.Errorf("missing resourceId: %s", body)
	}
	if !strings.Contains(body, `"resourceType":"workflow"`) {
		t.Errorf("expected auto-inferred resourceType=workflow: %s", body)
	}
	if strings.Contains(body, `"endpointId"`) {
		t.Errorf("body must not contain the legacy `endpointId` key (server .strict() rejects it): %s", body)
	}
}

func TestWebhooksSubscriptionsCreate_AutoInferenceByPrefix(t *testing.T) {
	cases := []struct {
		resource string
		want     string
	}{
		{"ex_xK9", "extractor"},
		{"cl_xK9", "classifier"},
		{"spl_xK9", "splitter"},
		{"workflow_xK9", "workflow"},
	}
	for _, tc := range cases {
		t.Run(tc.resource, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{"id": "whs_test"})
			})
			ta := newTestApp(t, srv)
			cmd := findCmd(t, ta.app, "webhooks", "subscriptions", "create")
			cmd.SetArgs([]string{
				"--endpoint", "whe_xyz",
				"--resource", tc.resource,
				"--events", "extract_run.processed",
			})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("create: %v", err)
			}
			body := string(srv.lastRequest().Body)
			want := `"resourceType":"` + tc.want + `"`
			if !strings.Contains(body, want) {
				t.Errorf("expected %s for resource=%s; body: %s", want, tc.resource, body)
			}
		})
	}
}

func TestWebhooksSubscriptionsCreate_RejectsUnknownPrefix(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when validation fails")
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "subscriptions", "create")
	cmd.SetArgs([]string{
		"--endpoint", "whe_xyz",
		"--resource", "weird_abc",
		"--events", "extract_run.processed",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "could not infer resource type") {
		t.Errorf("expected resource-type error, got %v", err)
	}
}

func TestWebhooksSubscriptionsUpdate_OnlyEnabledEvents(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "whs_test"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "subscriptions", "update")
	cmd.SetArgs([]string{"whs_test", "--events", "extract_run.failed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"enabledEvents":["extract_run.failed"]`) {
		t.Errorf("body must contain enabledEvents only: %s", body)
	}
}

func TestWebhooksEndpointsList_Empty(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "webhooks", "endpoints", "list")
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	// Default output is the empty-message text (no -o flag passed); pass
	// -o json explicitly when machine-readable output is needed.
	if !strings.Contains(ta.out.String(), "No webhook endpoints") {
		t.Errorf("expected empty-list message, got: %s", ta.out.String())
	}
}

func signTest(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// TestWebhookEventTypesCoverSDK is the drift guard for the --events
// catalog. The event-type wire string is a free-form string in the SDK,
// so rather than checking strings we wrote, it asks the SDK: for every
// variant on WebhookEvent, instantiate it, marshal, and read back the
// "eventType" discriminator the SDK emits — then assert that exact wire
// value appears in webhookEventsDoc. A new event type from an SDK regen
// is then covered automatically.
func TestWebhookEventTypesCoverSDK(t *testing.T) {
	typ := reflect.TypeOf(extend.WebhookEvent{})
	checked := 0
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		// The discriminator string field and any unexported field are not
		// variant payloads; those are the exported pointer-to-struct fields.
		if f.Name == "EventType" || f.PkgPath != "" || f.Type.Kind() != reflect.Ptr {
			continue
		}
		wrapper := reflect.New(typ)
		wrapper.Elem().Field(i).Set(reflect.New(f.Type.Elem()))
		data, err := json.Marshal(wrapper.Interface())
		if err != nil {
			t.Fatalf("marshal WebhookEvent for field %s: %v", f.Name, err)
		}
		var probe struct {
			EventType string `json:"eventType"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			t.Fatalf("unmarshal probe for field %s: %v", f.Name, err)
		}
		if probe.EventType == "" {
			continue
		}
		checked++
		if !documentsToken(webhookEventsDoc, probe.EventType) {
			t.Errorf("webhookEventsDoc is missing SDK event type %q (from field %s) — document it in webhooks.go", probe.EventType, f.Name)
		}
	}
	if checked < 30 {
		t.Fatalf("only %d webhook event types cross-checked; expected the full WebhookEvent set — reflection probe likely broke", checked)
	}
}

// TestWebhookCreateHelpDocumentsEvents asserts the event catalog reaches
// the create-help for both endpoints and subscriptions.
func TestWebhookCreateHelpDocumentsEvents(t *testing.T) {
	ta := newTestApp(t, newFakeServer(t, nil))
	for _, path := range [][]string{
		{"webhooks", "endpoints", "create"},
		{"webhooks", "subscriptions", "create"},
	} {
		cmd := findCmd(t, ta.app, path...)
		for _, want := range []string{"extract_run.processed", "workflow_run.completed", "extractor.version.published"} {
			if !strings.Contains(cmd.Long, want) {
				t.Errorf("%v --help missing event %q", path, want)
			}
		}
	}
}
