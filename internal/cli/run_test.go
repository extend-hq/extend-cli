package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRun_AsyncByDefault(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workflow_runs" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "workflow_run_x", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "id"
	if err := runWorkflow(context.Background(), ta.app, workflowParams{
		input:      "file_a",
		workflowID: "workflow_abc",
	}); err != nil {
		t.Fatalf("runWorkflow: %v", err)
	}
	if got := strings.TrimSpace(ta.out.String()); got != "workflow_run_x" {
		t.Errorf("stdout = %q, want workflow_run_x", got)
	}
}

func TestRun_WaitPathPolls(t *testing.T) {
	getCalls := 0
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/workflow_runs":
			writeJSON(w, 200, map[string]any{"id": "workflow_run_w", "status": "PENDING"})
		case r.Method == http.MethodGet && r.URL.Path == "/workflow_runs/workflow_run_w":
			getCalls++
			status := "PROCESSING"
			if getCalls >= 2 {
				status = "PROCESSED"
			}
			writeJSON(w, 200, map[string]any{
				"id":           "workflow_run_w",
				"status":       status,
				"dashboardUrl": "http://dashboard/wf",
			})
		}
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "json"

	if err := runWorkflow(context.Background(), ta.app, workflowParams{
		input:      "file_a",
		workflowID: "workflow_abc",
		wait:       true,
		timeout:    5 * time.Second,
	}); err != nil {
		t.Fatalf("runWorkflow: %v", err)
	}
	if getCalls < 2 {
		t.Errorf("expected at least 2 GETs, got %d", getCalls)
	}
}

func TestRun_NeedsReviewIsTerminal(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeJSON(w, 200, map[string]any{"id": "workflow_run_nr", "status": "PENDING"})
		case http.MethodGet:
			writeJSON(w, 200, map[string]any{
				"id":           "workflow_run_nr",
				"status":       "NEEDS_REVIEW",
				"dashboardUrl": "http://dashboard/review",
			})
		}
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "json"

	if err := runWorkflow(context.Background(), ta.app, workflowParams{
		input:      "file_a",
		workflowID: "workflow_abc",
		wait:       true,
		timeout:    2 * time.Second,
	}); err != nil {
		t.Fatalf("NEEDS_REVIEW should be treated as terminal (no error), got: %v", err)
	}
}

func TestRun_RejectedSurfacesError(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeJSON(w, 200, map[string]any{"id": "workflow_run_r", "status": "PENDING"})
		case http.MethodGet:
			writeJSON(w, 200, map[string]any{
				"id":     "workflow_run_r",
				"status": "REJECTED",
			})
		}
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "json"

	err := runWorkflow(context.Background(), ta.app, workflowParams{
		input:      "file_a",
		workflowID: "workflow_abc",
		wait:       true,
		timeout:    2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Errorf("expected rejected error, got %v", err)
	}
}

func TestRun_WorkflowAndFileFlatInRequest(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "workflow_run_x", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runWorkflow(context.Background(), ta.app, workflowParams{
		input:      "file_a",
		workflowID: "workflow_abc",
		version:    "3",
	}); err != nil {
		t.Fatalf("runWorkflow: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"id":"workflow_abc"`) || !strings.Contains(body, `"version":"3"`) {
		t.Errorf("body should contain workflow ref with version: %s", body)
	}
	if !strings.Contains(body, `"id":"file_a"`) {
		t.Errorf("body should contain file ref: %s", body)
	}
}

func TestRun_MetadataInRequestBody(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "workflow_run_x", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runWorkflow(context.Background(), ta.app, workflowParams{
		input:      "file_a",
		workflowID: "workflow_abc",
		metadata:   map[string]any{"customer": "acme"},
	}); err != nil {
		t.Fatalf("runWorkflow: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"metadata":{"customer":"acme"}`) {
		t.Errorf("body missing metadata: %s", body)
	}
}

func TestRun_OutputsAndSecretsInBody(t *testing.T) {
	tmp := t.TempDir() + "/outputs.json"
	if err := writeFileForTest(tmp, []byte(`[{"processorId":"ex_root","output":{"value":{"foo":"bar"}}}]`)); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "workflow_run_x", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runWorkflow(context.Background(), ta.app, workflowParams{
		input:       "file_a",
		workflowID:  "workflow_abc",
		outputsPath: tmp,
		secrets:     []string{"API_KEY=secret-1", "DB_URL=postgres://x"},
	}); err != nil {
		t.Fatalf("runWorkflow: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"outputs":[{"processorId":"ex_root","output":{"value":{"foo":"bar"}}}]`) {
		t.Errorf("body missing outputs: %s", body)
	}
	if !strings.Contains(body, `"API_KEY":"secret-1"`) || !strings.Contains(body, `"DB_URL":"postgres://x"`) {
		t.Errorf("body missing secrets: %s", body)
	}
}

func TestRun_OutputsRejectsNonArrayJSON(t *testing.T) {
	tmp := t.TempDir() + "/bad.json"
	if err := writeFileForTest(tmp, []byte(`{"not":"an array"}`)); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when validation fails")
	})
	ta := newTestApp(t, srv)
	err := runWorkflow(context.Background(), ta.app, workflowParams{
		input:       "file_a",
		workflowID:  "workflow_abc",
		outputsPath: tmp,
	})
	if err == nil || !strings.Contains(err.Error(), "--outputs") {
		t.Errorf("expected --outputs error, got %v", err)
	}
}
