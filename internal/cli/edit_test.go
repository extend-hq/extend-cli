package cli

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEdit_NestsConfigUnderConfigKey(t *testing.T) {
	tmp := t.TempDir()
	schema := filepath.Join(tmp, "schema.json")
	if err := os.WriteFile(schema, []byte(`{"fields":[{"key":"foo"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/edit_runs":
			writeJSON(w, 200, map[string]any{"id": "edr_x", "status": "PROCESSED"})
		case r.Method == http.MethodGet && r.URL.Path == "/edit_runs/edr_x":
			writeJSON(w, 200, map[string]any{"id": "edr_x", "status": "PROCESSED"})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ta := newTestApp(t, srv)
	if err := runEdit(context.Background(), ta.app, editParams{
		input:        "file_a",
		schemaPath:   schema,
		instructions: "be thorough",
		nativeOnly:   true,
		flatten:      true,
		timeout:      2 * time.Second,
	}); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	// First request is the POST to /edit_runs (subsequent are polls).
	postBody := string(srv.requests[0].Body)
	if !strings.Contains(postBody, `"config":{`) {
		t.Errorf("body must nest config; got %s", postBody)
	}
	if !strings.Contains(postBody, `"schema":{"fields":[{"key":"foo"}]}`) {
		t.Errorf("schema must be inside config; got %s", postBody)
	}
	if !strings.Contains(postBody, `"instructions":"be thorough"`) {
		t.Errorf("instructions must be inside config; got %s", postBody)
	}
	if strings.Contains(postBody, `"edit":`) || strings.Contains(postBody, `"values":`) {
		t.Errorf("edit/values fields must not appear (server schema rejects them); got %s", postBody)
	}
	if strings.Contains(postBody, `"priority":`) || strings.Contains(postBody, `"metadata":`) {
		t.Errorf("priority/metadata not supported on edit runs; got %s", postBody)
	}
	if strings.Contains(postBody, `"flattenPdf":true`) == false {
		t.Errorf("flattenPdf should be inside config.advancedOptions; got %s", postBody)
	}
}

func TestEdit_AutoDownloadsOnSuccess(t *testing.T) {
	tmp := t.TempDir()
	schema := filepath.Join(tmp, "schema.json")
	if err := os.WriteFile(schema, []byte(`{"fields":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "filled.pdf")

	storage := mockStorage(t, "filled-pdf-bytes")

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/edit_runs":
			writeJSON(w, 200, map[string]any{"id": "edr_x", "status": "PENDING"})
		case r.Method == http.MethodGet && r.URL.Path == "/edit_runs/edr_x":
			writeJSON(w, 200, map[string]any{
				"id":     "edr_x",
				"status": "PROCESSED",
				"output": map[string]any{"editedFile": map[string]any{"id": "file_filled", "presignedUrl": storage.URL}},
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

	err := runEdit(context.Background(), ta.app, editParams{
		input:      "file_a",
		schemaPath: schema,
		outputFile: out,
		nativeOnly: true,
		flatten:    true,
		timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "filled-pdf-bytes" {
		t.Errorf("downloaded contents = %q", string(got))
	}
}

func TestEditSchemaGenerate_HitsSyncEndpoint(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/edit_schemas/generate" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"fields": []any{}})
	})
	ta := newTestApp(t, srv)
	cmd := newEditSchemaGenerateCommand(ta.app)
	cmd.SetArgs([]string{"file_xK9"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("schema generate: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"file":{"id":"file_xK9"}`) {
		t.Errorf("body missing file ref: %s", body)
	}
	if !strings.Contains(ta.out.String(), `"fields"`) {
		t.Errorf("output should contain schema fields: %s", ta.out.String())
	}
}

func TestEdit_FailedRunSurfacesFailureMessage(t *testing.T) {
	tmp := t.TempDir()
	schema := filepath.Join(tmp, "schema.json")
	_ = os.WriteFile(schema, []byte(`{}`), 0o600)

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			writeJSON(w, 200, map[string]any{"id": "edr_fail", "status": "PENDING"})
		case r.Method == http.MethodGet:
			writeJSON(w, 200, map[string]any{
				"id":             "edr_fail",
				"status":         "FAILED",
				"failureReason":  "EMPTY_SCHEMA",
				"failureMessage": "No form fields detected.",
			})
		}
	})
	ta := newTestApp(t, srv)

	err := runEdit(context.Background(), ta.app, editParams{
		input:      "file_a",
		schemaPath: schema,
		timeout:    2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "No form fields detected") {
		t.Errorf("expected failure message in error, got %v", err)
	}
}
