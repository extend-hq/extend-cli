package extendx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	extend "github.com/extend-hq/extend-go-sdk"
	"github.com/extend-hq/extend-go-sdk/option"
)

// startParseRun creates a parse run through StartOnce against a server
// that answers with the given statuses in order, and reports how many
// requests reached it.
func startParseRun(t *testing.T, statuses ...int) (int32, error) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		status := statuses[min(n, len(statuses))-1]
		w.Header().Set("Content-Type", "application/json")
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "0")
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"object":"parse_run","id":"pr_1","status":"PROCESSING"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":"INTERNAL_ERROR","message":"boom","retryable":true}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	file, err := BuildParseFile(FileRef{ID: "file_1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = StartOnce(ctx, func(opts ...option.RequestOption) (*extend.ParseRun, error) {
		return c.ParseRuns.Create(ctx, &extend.ParseRunsCreateRequest{File: file}, opts...)
	})
	return calls.Load(), err
}

func TestStartOnce_DoesNotRetryServerErrors(t *testing.T) {
	calls, err := startParseRun(t, http.StatusBadGateway, http.StatusOK)
	if err == nil {
		t.Fatal("expected the 502 to be returned")
	}
	if calls != 1 {
		t.Errorf("server saw %d requests, want 1: a retried 5xx can start the run twice", calls)
	}
}

func TestStartOnce_RetriesRateLimits(t *testing.T) {
	calls, err := startParseRun(t, http.StatusTooManyRequests, http.StatusOK)
	if err != nil {
		t.Fatalf("expected success after the 429, got %v", err)
	}
	if calls != 2 {
		t.Errorf("server saw %d requests, want 2", calls)
	}
}

func TestStartOnce_GivesUpAfterTwoRateLimitRetries(t *testing.T) {
	calls, err := startParseRun(t, http.StatusTooManyRequests)
	if err == nil {
		t.Fatal("expected the last 429 to be returned")
	}
	if calls != 3 {
		t.Errorf("server saw %d requests, want 3", calls)
	}
}
