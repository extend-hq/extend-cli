package extendx

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// debugTransport wraps an underlying http.RoundTripper to log every
// request/response pair to the configured writer. The Authorization
// header is redacted; bodies are logged only on error responses.
//
// This preserves the behaviour the old hand-rolled client provided
// when EXTEND_DEBUG=1 or --debug is set, even though the SDK no
// longer offers a built-in debug hook.
type debugTransport struct {
	inner http.RoundTripper
	w     io.Writer
}

// NewDebugTransport returns a RoundTripper that wraps base and logs
// every request to w. If base is nil the default transport is used.
// If w is nil the wrapper does nothing (returns base directly).
func NewDebugTransport(base http.RoundTripper, w io.Writer) http.RoundTripper {
	if w == nil {
		if base == nil {
			return http.DefaultTransport
		}
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &debugTransport{inner: base, w: w}
}

func (d *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	d.logRequest(req)
	start := time.Now()
	resp, err := d.inner.RoundTrip(req)
	dur := time.Since(start)
	if err != nil {
		fmt.Fprintf(d.w, "extend [debug] ✗ %s %s (%s) transport error: %v\n",
			req.Method, req.URL.String(), dur.Round(time.Millisecond), err)
		return nil, err
	}
	if resp.StatusCode >= 400 {
		// Buffer the FULL body so the SDK's error decoder still
		// sees every byte, then log a truncated copy so stderr
		// doesn't drown in multi-megabyte 500 dumps. Truncating
		// the body itself (the previous behavior in this file)
		// would break SDK error decoders on responses larger than
		// 4KB — the cap is a logging cap, not a transport cap.
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		rid := resp.Header.Get("x-extend-request-id")
		const logCap = 4 * 1024
		logBody := body
		truncated := ""
		if len(logBody) > logCap {
			logBody = logBody[:logCap]
			truncated = fmt.Sprintf(" (truncated from %dB)", len(body))
		}
		fmt.Fprintf(d.w, "extend [debug] ← %s %s %d (req=%s, %s) body=%s%s\n",
			req.Method, req.URL.String(), resp.StatusCode,
			requestIDOrPlaceholder(rid), dur.Round(time.Millisecond),
			compactJSONLine(logBody), truncated)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}
	d.logResponse(req, resp, dur)
	return resp, nil
}

func (d *debugTransport) logRequest(req *http.Request) {
	contentLen := req.ContentLength
	wsTag := ""
	if v := req.Header.Get("X-Extend-Workspace-Id"); v != "" {
		wsTag = " workspace=" + v
	}
	fmt.Fprintf(d.w, "extend [debug] → %s %s (body=%dB)%s\n",
		req.Method, req.URL.String(), max64(contentLen, 0), wsTag)
}

func (d *debugTransport) logResponse(req *http.Request, resp *http.Response, dur time.Duration) {
	rid := resp.Header.Get("x-extend-request-id")
	clen := resp.Header.Get("content-length")
	bodyTag := ""
	if clen != "" {
		bodyTag = " body=" + clen + "B"
	}
	fmt.Fprintf(d.w, "extend [debug] ← %s %s %d (req=%s, %s)%s\n",
		req.Method, req.URL.String(), resp.StatusCode,
		requestIDOrPlaceholder(rid), dur.Round(time.Millisecond), bodyTag)
}

func requestIDOrPlaceholder(rid string) string {
	if rid == "" {
		return "-"
	}
	return rid
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// compactJSONLine produces a single-line representation of body for
// the debug log. We don't try to pretty-print: the existing test
// expectations rely on the raw bytes appearing in the log line so an
// engineer can grep for them.
func compactJSONLine(body []byte) string {
	// Replace newlines so multi-line bodies don't break the log line
	// format. Tabs are left alone because they don't.
	trimmed := bytes.ReplaceAll(body, []byte("\n"), []byte(" "))
	trimmed = bytes.ReplaceAll(trimmed, []byte("\r"), []byte(" "))
	return string(trimmed)
}
