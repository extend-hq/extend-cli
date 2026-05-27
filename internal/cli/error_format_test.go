package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	sdkcore "github.com/extend-hq/extend-go-sdk/core"
)

// noColor renders formatError output without ANSI codes so assertions
// match plain substrings.
var noColor = palette{enabled: false}

func format(err error) string {
	var b bytes.Buffer
	formatError(&b, noColor, err)
	return b.String()
}

func TestFormatError_APIError(t *testing.T) {
	header := http.Header{}
	header.Set("x-extend-request-id", "req_abc")
	err := sdkcore.NewAPIError(404, header, errors.New(`{"code":"NOT_FOUND","message":"no such run","requestId":"req_abc"}`))
	out := format(err)
	for _, want := range []string{"Error:", "NOT_FOUND", "no such run", "req_abc"} {
		if !strings.Contains(out, want) {
			t.Errorf("API error output %q missing %q", out, want)
		}
	}
}

func TestFormatError_TransportTimeout(t *testing.T) {
	// url.Error.Timeout() is true when the wrapped error reports a
	// timeout; context.DeadlineExceeded does exactly that.
	err := &url.Error{Op: "Post", URL: "https://api.extend.ai/v1/extract_runs", Err: context.DeadlineExceeded}
	out := format(err)
	if !strings.Contains(out, "request timed out") {
		t.Errorf("expected 'request timed out', got %q", out)
	}
	if !strings.Contains(out, "--http-timeout") {
		t.Errorf("expected --http-timeout hint, got %q", out)
	}
	// The underlying detail is preserved on a follow-up line.
	if !strings.Contains(out, "detail:") {
		t.Errorf("expected detail line, got %q", out)
	}
}

func TestFormatError_TransportUnreachable(t *testing.T) {
	// A non-timeout transport failure (DNS / refused) → "could not reach".
	err := &url.Error{Op: "Post", URL: "https://api.extend.ai/v1/extract_runs", Err: errors.New("dial tcp: lookup api.extend.ai: no such host")}
	out := format(err)
	if !strings.Contains(out, "could not reach the Extend API") {
		t.Errorf("expected 'could not reach', got %q", out)
	}
	if !strings.Contains(out, "no such host") {
		t.Errorf("expected underlying detail, got %q", out)
	}
	// Must NOT be misclassified as a timeout.
	if strings.Contains(out, "request timed out") {
		t.Errorf("DNS failure misclassified as timeout: %q", out)
	}
}

func TestFormatError_BareDeadline(t *testing.T) {
	out := format(context.DeadlineExceeded)
	if !strings.Contains(out, "request timed out") || !strings.Contains(out, "--http-timeout") {
		t.Errorf("bare deadline not classified: %q", out)
	}
}

func TestFormatError_Generic(t *testing.T) {
	out := format(errors.New("something broke"))
	if !strings.Contains(out, "Error: something broke") {
		t.Errorf("generic error output = %q", out)
	}
}
