package extendx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// VerifyWebhookSignature reproduces the server's signing scheme:
//
//	message   = "v0:" + timestamp + ":" + body
//	signature = hex(HMAC_SHA256(secret, message))
//
// timestamp must be within maxAge of now; pass 0 to skip the time
// check. This is the only thing the CLI does without the SDK because
// the SDK is for outgoing requests; webhook verification operates on
// incoming HTTP requests received by the caller.
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

// WebhookSubscriptionResourceTypeFromID infers the server-side resource
// type from the resource ID prefix (`ex_`, `cl_`, `spl_`, `workflow_`).
// Returns the empty string if the prefix is unrecognized; callers
// should then require the user to pass --resource-type explicitly.
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
