package cli

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDownload_FileByID exercises the simplest dispatch path: a
// file_xxx ID resolves directly to a single-file download. The fake
// `/files/<id>` endpoint returns a presigned URL pointing at a mock
// storage server that serves the bytes.
func TestDownload_FileByID(t *testing.T) {
	storage := mockStorage(t, "hello-bytes")
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/files/file_abc" {
			writeJSON(w, 200, map[string]any{
				"id":           "file_abc",
				"name":         "hello.txt",
				"presignedUrl": storage.URL,
			})
			return
		}
		t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
	})
	ta := newTestApp(t, srv)
	tmp := t.TempDir()
	if err := runDownload(context.Background(), ta.app, "file_abc", tmp, ""); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(tmp, "hello.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello-bytes" {
		t.Errorf("file contents = %q, want %q", string(got), "hello-bytes")
	}
}

// TestDownload_FileByID_Stdout streams a single file's bytes to stdout
// when --output-file=-. No file is written to disk.
func TestDownload_FileByID_Stdout(t *testing.T) {
	storage := mockStorage(t, "stdout-bytes")
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/files/file_abc" {
			writeJSON(w, 200, map[string]any{
				"id":           "file_abc",
				"name":         "hello.txt",
				"presignedUrl": storage.URL,
			})
			return
		}
		t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
	})
	ta := newTestApp(t, srv)
	if err := runDownload(context.Background(), ta.app, "file_abc", "", "-"); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	if got := ta.out.String(); got != "stdout-bytes" {
		t.Errorf("stdout = %q, want %q", got, "stdout-bytes")
	}
}

// TestDownload_EditRun resolves an edit-run ID to its editedFile and
// writes the result with the file's server-side name.
func TestDownload_EditRun(t *testing.T) {
	storage := mockStorage(t, "filled-pdf")
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/edit_runs/edr_xyz":
			writeJSON(w, 200, map[string]any{
				"id":     "edr_xyz",
				"status": "PROCESSED",
				"output": map[string]any{
					"editedFile": map[string]any{"id": "file_filled"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/files/file_filled":
			writeJSON(w, 200, map[string]any{
				"id":           "file_filled",
				"name":         "filled.pdf",
				"presignedUrl": storage.URL,
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ta := newTestApp(t, srv)
	tmp := t.TempDir()
	if err := runDownload(context.Background(), ta.app, "edr_xyz", tmp, ""); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(tmp, "filled.pdf"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "filled-pdf" {
		t.Errorf("file contents = %q, want %q", string(got), "filled-pdf")
	}
}

// TestDownload_SplitRun walks the split output, downloads each segment,
// and disambiguates server-side name collisions with -2, -3, etc.
func TestDownload_SplitRun(t *testing.T) {
	storage := mockStorage(t, "split-bytes")
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/split_runs/splr_xyz":
			writeJSON(w, 200, map[string]any{
				"id":     "splr_xyz",
				"status": "PROCESSED",
				"output": map[string]any{
					"splits": []map[string]any{
						{"fileId": "file_a"},
						{"fileId": "file_b"},
						{"fileId": "file_c"},
					},
				},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/file_"):
			id := strings.TrimPrefix(r.URL.Path, "/files/")
			// All three splits return the same name to exercise the
			// uniqueName collision-handling path.
			writeJSON(w, 200, map[string]any{
				"id":           id,
				"name":         "page.pdf",
				"presignedUrl": storage.URL,
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ta := newTestApp(t, srv)
	tmp := t.TempDir()
	if err := runDownload(context.Background(), ta.app, "splr_xyz", tmp, ""); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	for _, name := range []string{"page.pdf", "page-2.pdf", "page-3.pdf"} {
		got, err := os.ReadFile(filepath.Join(tmp, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != "split-bytes" {
			t.Errorf("%s = %q, want %q", name, string(got), "split-bytes")
		}
	}
}

// TestDownload_WorkflowRun walks step-run output files (e.g. split
// segments emitted by a split step) and writes each to disk.
func TestDownload_WorkflowRun(t *testing.T) {
	storage := mockStorage(t, "wfr-bytes")
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workflow_runs/workflow_run_xyz":
			writeJSON(w, 200, map[string]any{
				"id":     "workflow_run_xyz",
				"status": "PROCESSED",
				"stepRuns": []map[string]any{
					{
						"stepType": "SPLIT",
						"id":       "wsr_1",
						"object":   "workflow_step_run",
						"status":   "PROCESSED",
						"step":     map[string]any{"id": "step_1", "name": "split-step", "type": "SPLIT", "object": "workflow_step"},
						"files":    []map[string]any{{"id": "file_w1", "name": "part1.pdf", "object": "file"}},
					},
					{
						"stepType": "EXTRACT",
						"id":       "wsr_2",
						"object":   "workflow_step_run",
						"status":   "PROCESSED",
						"step":     map[string]any{"id": "step_2", "name": "extract-step", "type": "EXTRACT", "object": "workflow_step"},
						"files":    []map[string]any{},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/files/file_w1":
			writeJSON(w, 200, map[string]any{
				"id":           "file_w1",
				"name":         "part1.pdf",
				"presignedUrl": storage.URL,
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ta := newTestApp(t, srv)
	tmp := t.TempDir()
	if err := runDownload(context.Background(), ta.app, "workflow_run_xyz", tmp, ""); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(tmp, "part1.pdf"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "wfr-bytes" {
		t.Errorf("file = %q, want %q", string(got), "wfr-bytes")
	}
}

// TestDownload_ExtractRunErrors confirms that extract/parse/classify
// run IDs are rejected up front with a pointer to `runs get`, before
// any network call is made.
func TestDownload_ExtractRunErrors(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("server should not be called for an extract run; got %s %s", r.Method, r.URL.Path)
	})
	ta := newTestApp(t, srv)
	err := runDownload(context.Background(), ta.app, "exr_abc", "", "")
	if err == nil || !strings.Contains(err.Error(), "JSON, not files") {
		t.Errorf("expected JSON-not-files error for exr_, got %v", err)
	}
}

// TestDownload_UnknownPrefixErrors rejects unrecognized IDs cleanly
// instead of attempting a doomed lookup.
func TestDownload_UnknownPrefixErrors(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("server should not be called for an unknown prefix; got %s %s", r.Method, r.URL.Path)
	})
	ta := newTestApp(t, srv)
	err := runDownload(context.Background(), ta.app, "blob_abc", "", "")
	if err == nil || !strings.Contains(err.Error(), "unrecognized ID prefix") {
		t.Errorf("expected unrecognized-prefix error, got %v", err)
	}
}

// TestDownload_OutputFileRejectedForMultiFile guards the safety check:
// --output-file is incompatible with multi-file sources because the
// caller can't name N files with a single path.
func TestDownload_OutputFileRejectedForMultiFile(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/split_runs/splr_xyz" {
			writeJSON(w, 200, map[string]any{
				"id":     "splr_xyz",
				"status": "PROCESSED",
				"output": map[string]any{
					"splits": []map[string]any{{"fileId": "file_a"}, {"fileId": "file_b"}},
				},
			})
			return
		}
		t.Fatalf("server should not be called past the splits listing; got %s %s", r.Method, r.URL.Path)
	})
	ta := newTestApp(t, srv)
	err := runDownload(context.Background(), ta.app, "splr_xyz", "", "out.pdf")
	if err == nil || !strings.Contains(err.Error(), "--output-file requires a single-file source") {
		t.Errorf("expected single-file-source error, got %v", err)
	}
}

// TestUniqueName exercises the collision-suffix logic in isolation.
func TestUniqueName(t *testing.T) {
	used := map[string]bool{
		"page.pdf":   true,
		"page-2.pdf": true,
	}
	if got := uniqueName("page.pdf", used); got != "page-3.pdf" {
		t.Errorf("uniqueName(page.pdf, …) = %q, want page-3.pdf", got)
	}
	if got := uniqueName("other.pdf", used); got != "other.pdf" {
		t.Errorf("uniqueName(other.pdf, …) = %q, want other.pdf", got)
	}
	// File with no extension: suffix slot goes before the empty ext.
	if got := uniqueName("blob", map[string]bool{"blob": true}); got != "blob-2" {
		t.Errorf("uniqueName(blob, …) = %q, want blob-2", got)
	}
}
