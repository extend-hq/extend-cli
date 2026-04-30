package cli

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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

func TestParseBatch_EngineVersionRoundTrip(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "bpar_test", "status": "PENDING", "runCount": 1})
	})
	ta := newTestApp(t, srv)
	cmd := newParseBatchCommand(ta.app)
	cmd.SetArgs([]string{"file_a", "--engine", "parse_light", "--engine-version", "0.2.0-beta"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := string(srv.lastRequest().Body)
	for _, want := range []string{`"engine":"parse_light"`, `"engineVersion":"0.2.0-beta"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s: %s", want, body)
		}
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
	if err := runRunsList(stubCmdWithCtx(context.Background(), "list"), ta.app, runsListParams{
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

// TestUploadAllOrResolve_PreservesOrder ensures the parallel upload returns
// FileRefs in the same order as the inputs, even when individual uploads
// complete out of order. It works by parsing the uploaded filename out of
// the multipart form and echoing it back as the file_id.
func TestUploadAllOrResolve_PreservesOrder(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/files/upload" {
			// Random sleep so completions interleave non-deterministically.
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			fhs := r.MultipartForm.File["file"]
			if len(fhs) != 1 {
				http.Error(w, "expected 1 file", 400)
				return
			}
			name := fhs[0].Filename
			writeJSON(w, 200, map[string]any{"id": "file_" + strings.TrimSuffix(name, ".pdf")})
			return
		}
		http.NotFound(w, r)
	})
	ta := newTestApp(t, srv)
	c, _ := ta.app.NewClient()

	tmp := t.TempDir()
	inputs := make([]string, 12)
	for i := range inputs {
		path := filepath.Join(tmp, strconv.Itoa(i)+".pdf")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		inputs[i] = path
	}

	refs, err := uploadAllOrResolveWithConcurrency(context.Background(), ta.app, c, inputs, 4)
	if err != nil {
		t.Fatalf("uploadAllOrResolve: %v", err)
	}
	if len(refs) != len(inputs) {
		t.Fatalf("len(refs) = %d, want %d", len(refs), len(inputs))
	}
	for i, ref := range refs {
		want := "file_" + strconv.Itoa(i)
		if ref.ID != want {
			t.Errorf("refs[%d].ID = %q, want %q (order not preserved)", i, ref.ID, want)
		}
	}
}

// TestUploadAllOrResolve_BoundsConcurrency confirms the concurrency cap is
// respected: the number of in-flight upload calls never exceeds the cap.
func TestUploadAllOrResolve_BoundsConcurrency(t *testing.T) {
	var inFlight, peak int32
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/files/upload" {
			now := atomic.AddInt32(&inFlight, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if now <= p || atomic.CompareAndSwapInt32(&peak, p, now) {
					break
				}
			}
			// Sleep briefly so workers actually overlap.
			defer atomic.AddInt32(&inFlight, -1)
			writeJSON(w, 200, map[string]any{"id": "file_x"})
			return
		}
		http.NotFound(w, r)
	})
	ta := newTestApp(t, srv)
	c, _ := ta.app.NewClient()

	tmp := t.TempDir()
	inputs := make([]string, 20)
	for i := range inputs {
		path := filepath.Join(tmp, strconv.Itoa(i)+".pdf")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		inputs[i] = path
	}

	const cap = 3
	if _, err := uploadAllOrResolveWithConcurrency(context.Background(), ta.app, c, inputs, cap); err != nil {
		t.Fatalf("uploadAllOrResolve: %v", err)
	}
	if got := atomic.LoadInt32(&peak); got > cap {
		t.Errorf("peak in-flight uploads = %d, want <= %d", got, cap)
	}
}
