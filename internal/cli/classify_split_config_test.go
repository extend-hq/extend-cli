package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// These tests lock in the inline --config path added for classify and
// split. The Extend API accepts a standalone Config on classify/split run
// creation (mirroring extract), but the CLI previously only allowed
// --using + --patch, leaving one-off configless runs unreachable.

func TestClassify_InlineConfigNestsUnderConfig(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/classify_runs" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "clr_x", "object": "classify_run", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runClassify(context.Background(), ta.app, classifyParams{
		input: "file_a",
		// classificationRules carries a marker so we can prove the nested
		// config CONTENT survives the unmarshal→marshal round-trip, not
		// just that a `config` key is present.
		configPath: `{"baseProcessor":"classification_performance","classificationRules":"ROUNDTRIP_MARKER"}`,
		wait:       false,
	}); err != nil {
		t.Fatalf("runClassify: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"config":`) {
		t.Errorf("inline config must nest under config; got %s", body)
	}
	if !strings.Contains(body, `"baseProcessor":"classification_performance"`) {
		t.Errorf("config content must round-trip (baseProcessor); got %s", body)
	}
	if !strings.Contains(body, `"classificationRules":"ROUNDTRIP_MARKER"`) {
		t.Errorf("config content must round-trip (classificationRules marker); got %s", body)
	}
	if strings.Contains(body, `"classifier":`) {
		t.Errorf("inline config must not also send a classifier ref; got %s", body)
	}
}

func TestClassify_UsingBuildsClassifierRef(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "clr_x", "object": "classify_run", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runClassify(context.Background(), ta.app, classifyParams{
		input:        "file_a",
		classifierID: "cl_abc",
		wait:         false,
	}); err != nil {
		t.Fatalf("runClassify: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"classifier":{"id":"cl_abc"`) {
		t.Errorf("saved classifier must send a classifier ref; got %s", body)
	}
	if strings.Contains(body, `"config":`) {
		t.Errorf("saved-classifier run must not send an inline config; got %s", body)
	}
}

func TestClassify_RequiresExactlyOneOfUsingOrConfig(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("no request expected; got %s %s", r.Method, r.URL.Path)
	})
	ta := newTestApp(t, srv)

	// Neither --using nor --config.
	cmd := findCmd(t, ta.app, "classify")
	cmd.SetArgs([]string{"file_a"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "exactly one of --using or --config") {
		t.Errorf("neither: expected mutual-exclusion error, got %v", err)
	}

	// Both --using and --config.
	cmd = findCmd(t, ta.app, "classify")
	cmd.SetArgs([]string{"file_a", "--using", "cl_a", "--config", "{}"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "exactly one of --using or --config") {
		t.Errorf("both: expected mutual-exclusion error, got %v", err)
	}
}

func TestSplit_InlineConfigNestsUnderConfig(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/split_runs" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "splr_x", "object": "split_run", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runSplit(context.Background(), ta.app, splitParams{
		input: "file_a",
		// splitRules carries a marker so we can prove the nested config
		// CONTENT survives the round-trip, not just the `config` key.
		configPath: `{"baseProcessor":"splitting_performance","splitRules":"ROUNDTRIP_MARKER"}`,
		wait:       false,
	}); err != nil {
		t.Fatalf("runSplit: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"config":`) {
		t.Errorf("inline config must nest under config; got %s", body)
	}
	if !strings.Contains(body, `"baseProcessor":"splitting_performance"`) {
		t.Errorf("config content must round-trip (baseProcessor); got %s", body)
	}
	if !strings.Contains(body, `"splitRules":"ROUNDTRIP_MARKER"`) {
		t.Errorf("config content must round-trip (splitRules marker); got %s", body)
	}
	if strings.Contains(body, `"splitter":`) {
		t.Errorf("inline config must not also send a splitter ref; got %s", body)
	}
}

func TestSplit_UsingBuildsSplitterRef(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "splr_x", "object": "split_run", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runSplit(context.Background(), ta.app, splitParams{
		input:      "file_a",
		splitterID: "spl_abc",
		wait:       false,
	}); err != nil {
		t.Fatalf("runSplit: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"splitter":{"id":"spl_abc"`) {
		t.Errorf("saved splitter must send a splitter ref; got %s", body)
	}
	if strings.Contains(body, `"config":`) {
		t.Errorf("saved-splitter run must not send an inline config; got %s", body)
	}
}

func TestSplit_RequiresExactlyOneOfUsingOrConfig(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("no request expected; got %s %s", r.Method, r.URL.Path)
	})
	ta := newTestApp(t, srv)

	cmd := findCmd(t, ta.app, "split")
	cmd.SetArgs([]string{"file_a"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "exactly one of --using or --config") {
		t.Errorf("neither: expected mutual-exclusion error, got %v", err)
	}

	cmd = findCmd(t, ta.app, "split")
	cmd.SetArgs([]string{"file_a", "--using", "spl_a", "--config", "{}"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "exactly one of --using or --config") {
		t.Errorf("both: expected mutual-exclusion error, got %v", err)
	}
}
