package extendx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"
)

// sign produces a valid signature for the (timestamp, body) pair using
// the same scheme VerifyWebhookSignature expects. Kept in the test
// file (not exported) so this test is the only client of the signing
// path; production code always verifies, never signs.
func sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignature_Valid(t *testing.T) {
	secret := "whsec_test"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"event":"run.completed"}`)
	sig := sign(secret, ts, body)

	// maxAge=5m is the recommended Stripe-style window; the
	// freshly-stamped timestamp must pass.
	if err := VerifyWebhookSignature(secret, sig, ts, body, 5*time.Minute); err != nil {
		t.Errorf("VerifyWebhookSignature(valid) = %v; want nil", err)
	}
	// maxAge=0 disables the time check entirely.
	if err := VerifyWebhookSignature(secret, sig, ts, body, 0); err != nil {
		t.Errorf("VerifyWebhookSignature(maxAge=0) = %v; want nil", err)
	}
}

func TestVerifyWebhookSignature_BadInputs(t *testing.T) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{}`)
	good := sign("whsec_test", ts, body)

	cases := []struct {
		name      string
		secret    string
		signature string
		timestamp string
		body      []byte
	}{
		{"empty secret", "", good, ts, body},
		{"empty signature", "whsec_test", "", ts, body},
		{"empty timestamp", "whsec_test", good, "", body},
		{"wrong secret", "whsec_other", good, ts, body},
		// Tampered body produces a fresh HMAC that diverges.
		{"tampered body", "whsec_test", good, ts, []byte(`{"x":1}`)},
		// Tampered timestamp invalidates the HMAC.
		{"tampered timestamp", "whsec_test", good, strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10), body},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyWebhookSignature(tc.secret, tc.signature, tc.timestamp, tc.body, 0)
			if err == nil {
				t.Errorf("%s: VerifyWebhookSignature = nil; want non-nil", tc.name)
			}
		})
	}
}

func TestVerifyWebhookSignature_Expired(t *testing.T) {
	secret := "whsec_test"
	// Timestamp 1 hour in the past — exceeds a 5 minute window.
	oldTS := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	body := []byte(`{}`)
	sig := sign(secret, oldTS, body)

	if err := VerifyWebhookSignature(secret, sig, oldTS, body, 5*time.Minute); err == nil {
		t.Error("VerifyWebhookSignature(stale) = nil; want non-nil")
	}
	// With maxAge=0 the same stale request validates — caller is
	// opting out of the time check.
	if err := VerifyWebhookSignature(secret, sig, oldTS, body, 0); err != nil {
		t.Errorf("VerifyWebhookSignature(stale, maxAge=0) = %v; want nil", err)
	}
}

func TestVerifyWebhookSignature_NonNumericTimestamp(t *testing.T) {
	// Non-numeric timestamps are surface-level rejected only when
	// maxAge > 0 forces a parse; with maxAge=0 the timestamp is
	// fed into the HMAC verbatim. Lock both behaviours.
	body := []byte(`{}`)
	secret := "whsec_test"
	sig := sign(secret, "garbage", body)

	if err := VerifyWebhookSignature(secret, sig, "garbage", body, 5*time.Minute); err == nil {
		t.Error("VerifyWebhookSignature(non-numeric, maxAge>0) = nil; want non-nil")
	}
	if err := VerifyWebhookSignature(secret, sig, "garbage", body, 0); err != nil {
		t.Errorf("VerifyWebhookSignature(non-numeric, maxAge=0) = %v; want nil", err)
	}
}

func TestWebhookSubscriptionResourceTypeFromID(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"ex_xyz", "extractor"},
		{"cl_xyz", "classifier"},
		{"spl_xyz", "splitter"},
		{"workflow_xyz", "workflow"},
		// Workflow version IDs share the workflow_ prefix; the
		// helper does NOT distinguish workflow vs workflow_version
		// vs workflow_run. The CLI relies on the user passing the
		// right resource ID; verify the broad-prefix behaviour.
		{"workflow_version_xyz", "workflow"},
		{"workflow_run_xyz", "workflow"},
		// Unknown / empty → "".
		{"", ""},
		{"file_xyz", ""},
		{"evs_xyz", ""},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got := WebhookSubscriptionResourceTypeFromID(tc.id)
			if got != tc.want {
				t.Errorf("WebhookSubscriptionResourceTypeFromID(%q) = %q; want %q", tc.id, got, tc.want)
			}
		})
	}
}

// TestSignaturePrefixGuard locks the literal "v0:" prefix that the
// server signs. Forgetting the prefix is the classic webhook impl
// bug; this test catches it independently of any external reference
// hash by recomputing the HMAC with the prefix removed and asserting
// verification fails.
func TestSignaturePrefixGuard(t *testing.T) {
	secret := "whsec_test"
	ts := "1700000000"
	body := []byte(`{"x":1}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + ":")) // missing "v0:" prefix
	mac.Write(body)
	wrong := hex.EncodeToString(mac.Sum(nil))
	if err := VerifyWebhookSignature(secret, wrong, ts, body, 0); err == nil {
		t.Error("VerifyWebhookSignature accepted prefix-less signature; the v0: prefix is part of the scheme")
	}
}

func TestSignatureRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 16, 1024, 65536} {
		body := make([]byte, n)
		for i := range body {
			body[i] = byte(i)
		}
		ts := fmt.Sprintf("%d", time.Now().Unix())
		sig := sign("k", ts, body)
		if err := VerifyWebhookSignature("k", sig, ts, body, 0); err != nil {
			t.Errorf("round-trip body len=%d: %v", n, err)
		}
	}
}
