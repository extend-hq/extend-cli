package cli

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCollectBatchInputs_PositionalOnly(t *testing.T) {
	got, err := collectBatchInputs([]string{"a.pdf", "b.pdf"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(got, []string{"a.pdf", "b.pdf"}) {
		t.Errorf("got %v", got)
	}
}

func TestCollectBatchInputs_FromFile(t *testing.T) {
	list := filepath.Join(t.TempDir(), "list.txt")
	if err := os.WriteFile(list, []byte("a.pdf\nb.pdf\n\nc.pdf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := collectBatchInputs(nil, list)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(got, []string{"a.pdf", "b.pdf", "c.pdf"}) {
		t.Errorf("got %v", got)
	}
}

func TestCollectBatchInputs_PositionalAndFromFileCombine(t *testing.T) {
	list := filepath.Join(t.TempDir(), "list.txt")
	if err := os.WriteFile(list, []byte("c.pdf\nd.pdf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := collectBatchInputs([]string{"a.pdf", "b.pdf"}, list)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(got, []string{"a.pdf", "b.pdf", "c.pdf", "d.pdf"}) {
		t.Errorf("got %v", got)
	}
}

func TestCollectBatchInputs_ErrorsOnEmpty(t *testing.T) {
	if _, err := collectBatchInputs(nil, ""); err == nil {
		t.Error("expected error for no inputs")
	}
}

func TestCollectBatchInputs_ErrorsOnOver1000(t *testing.T) {
	args := make([]string, 1001)
	for i := range args {
		args[i] = "f" + strconv.Itoa(i) + ".pdf"
	}
	_, err := collectBatchInputs(args, "")
	if err == nil || !strings.Contains(err.Error(), "1000") {
		t.Errorf("expected limit error, got %v", err)
	}
}

func TestExtractBatch_SubmitsBatchEndpoint(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract_runs/batch" || r.Method != http.MethodPost {
			t.Fatalf("hit %s %s, want POST /extract_runs/batch", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{
			"id":       "bpr_test",
			"object":   "batch_run",
			"status":   "PENDING",
			"runCount": 2,
		})
	})
	ta := newTestApp(t, srv)
	cmd := newExtractBatchCommand(ta.app)
	cmd.SetArgs([]string{"file_a", "file_b", "--using", "ex_abc"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"inputs"`) {
		t.Errorf("body should contain inputs, got: %s", body)
	}
	if !strings.Contains(body, `"id":"file_a"`) || !strings.Contains(body, `"id":"file_b"`) {
		t.Errorf("body should reference both file ids, got: %s", body)
	}
	if !strings.Contains(body, `"id":"ex_abc"`) {
		t.Errorf("body should reference extractor id, got: %s", body)
	}
	if !strings.Contains(ta.out.String(), "bpr_test") {
		t.Errorf("expected batch id in stdout, got: %s", ta.out.String())
	}
}

func TestParseBatch_DefaultsToMarkdownTarget(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "bpar_test", "status": "PENDING", "runCount": 1})
	})
	ta := newTestApp(t, srv)
	cmd := newParseBatchCommand(ta.app)
	cmd.SetArgs([]string{"file_a"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"target":"markdown"`) {
		t.Errorf("body should default to markdown, got: %s", body)
	}
}

func TestWorkflowBatch_ReturnsBatchIDOnly(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workflow_runs/batch" || r.Method != http.MethodPost {
			t.Fatalf("hit %s %s, want POST /workflow_runs/batch", r.Method, r.URL.Path)
		}
		// Server returns ONLY {batchId} for workflow batches — verify that.
		writeJSON(w, 200, map[string]any{"batchId": "batch_abc"})
	})
	ta := newTestApp(t, srv)
	cmd := newWorkflowBatchCommand(ta.app)
	cmd.SetArgs([]string{"file_a", "file_b", "--workflow", "workflow_xK9"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := string(srv.lastRequest().Body)
	for _, bad := range []string{`"priority"`, `"metadata":`} {
		// Server schema for workflow batch has neither top-level priority nor metadata.
		if strings.Contains(body, bad) {
			t.Errorf("workflow batch body should not contain %s; got %s", bad, body)
		}
	}
	if !strings.Contains(ta.out.String(), "batch_abc") {
		t.Errorf("expected batch_abc in stdout, got: %s", ta.out.String())
	}
}

func TestWorkflowBatch_RejectsTopLevelPriority(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when client validates")
	})
	ta := newTestApp(t, srv)
	cmd := newWorkflowBatchCommand(ta.app)
	cmd.SetArgs([]string{"file_a", "--workflow", "workflow_xK9", "--priority", "5"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "priority") {
		t.Errorf("expected priority error, got %v", err)
	}
}

func TestRunsList_BatchFilterReachesQuery(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	ta := newTestApp(t, srv)
	if err := runRunsList(context.Background(), ta.app, runsListParams{
		runType: "extract",
		batchID: "bpr_xyz",
		limit:   5,
		sortDir: "desc",
	}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(srv.lastRequest().Query, "batchId=bpr_xyz") {
		t.Errorf("query missing batchId filter: %s", srv.lastRequest().Query)
	}
}
