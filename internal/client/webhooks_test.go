package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWebhookSubscriptionResourceTypeFromID(t *testing.T) {
	cases := map[string]string{
		"ex_abc":           "extractor",
		"cl_abc":           "classifier",
		"spl_abc":          "splitter",
		"workflow_abc":     "workflow",
		"workflow_run_abc": "workflow", // workflow_ is a prefix of workflow_run_; documented behavior
		"whatever_abc":     "",
		"":                 "",
		"file_abc":         "",
	}
	for in, want := range cases {
		if got := WebhookSubscriptionResourceTypeFromID(in); got != want {
			t.Errorf("WebhookSubscriptionResourceTypeFromID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateWebhookSubscriptionMarshalsCorrectKey(t *testing.T) {
	in := CreateWebhookSubscriptionInput{
		WebhookEndpointID: "whe_abc",
		ResourceType:      "workflow",
		ResourceID:        "workflow_xyz",
		EnabledEvents:     []string{"workflow_run.completed"},
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(got), `"webhookEndpointId":"whe_abc"`) {
		t.Errorf("must use webhookEndpointId (server .strict() schema): %s", got)
	}
	if strings.Contains(string(got), `"endpointId"`) {
		t.Errorf("must not use legacy endpointId: %s", got)
	}
	if !strings.Contains(string(got), `"resourceType":"workflow"`) {
		t.Errorf("missing resourceType: %s", got)
	}
}

func TestWebhookSubscriptionDecodesCorrectKey(t *testing.T) {
	body := []byte(`{"id":"whs_x","object":"webhook_subscription","webhookEndpointId":"whe_x","resourceType":"workflow","resourceId":"workflow_x","enabledEvents":["a"],"createdAt":"2026-01-01"}`)
	var s WebhookSubscription
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.WebhookEndpointID != "whe_x" {
		t.Errorf("WebhookEndpointID = %q, want whe_x", s.WebhookEndpointID)
	}
	if s.ResourceType != "workflow" {
		t.Errorf("ResourceType = %q, want workflow", s.ResourceType)
	}
}

func sign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignatureValid(t *testing.T) {
	secret := "wss_test"
	body := []byte(`{"event":"hello"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := sign(secret, ts, body)
	if err := VerifyWebhookSignature(secret, sig, ts, body, 5*time.Minute); err != nil {
		t.Errorf("expected valid signature; got %v", err)
	}
}

func TestVerifyWebhookSignatureMismatch(t *testing.T) {
	secret := "wss_test"
	body := []byte(`{"event":"hello"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	if err := VerifyWebhookSignature(secret, "deadbeef", ts, body, 5*time.Minute); err == nil {
		t.Error("expected signature mismatch error")
	}
}

func TestVerifyWebhookSignatureBodyTampered(t *testing.T) {
	secret := "wss_test"
	body := []byte(`{"event":"hello"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := sign(secret, ts, body)
	tampered := []byte(`{"event":"goodbye"}`)
	if err := VerifyWebhookSignature(secret, sig, ts, tampered, 5*time.Minute); err == nil {
		t.Error("expected signature mismatch on body tamper")
	}
}

func TestVerifyWebhookSignatureExpired(t *testing.T) {
	secret := "wss_test"
	body := []byte(`{"event":"hello"}`)
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	sig := sign(secret, old, body)
	if err := VerifyWebhookSignature(secret, sig, old, body, 5*time.Minute); err == nil {
		t.Error("expected timestamp expired error")
	}
}

func TestVerifyWebhookSignatureSkipsTimeCheckOnZeroMaxAge(t *testing.T) {
	secret := "wss_test"
	body := []byte(`{"event":"hello"}`)
	old := strconv.FormatInt(time.Now().Add(-365*24*time.Hour).Unix(), 10)
	sig := sign(secret, old, body)
	if err := VerifyWebhookSignature(secret, sig, old, body, 0); err != nil {
		t.Errorf("maxAge=0 should skip time check; got %v", err)
	}
}

func TestVerifyWebhookSignatureMissingFields(t *testing.T) {
	body := []byte(`{}`)
	if err := VerifyWebhookSignature("", "abc", "1", body, 0); err == nil {
		t.Error("empty secret should error")
	}
	if err := VerifyWebhookSignature("s", "", "1", body, 0); err == nil {
		t.Error("empty signature should error")
	}
	if err := VerifyWebhookSignature("s", "abc", "", body, 0); err == nil {
		t.Error("empty timestamp should error")
	}
}
