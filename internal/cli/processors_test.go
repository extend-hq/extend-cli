package cli

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestExtractorsList_HitsExpectedEndpoint(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	ta := newTestApp(t, srv)
	cmd := extractorAccessor().listCmd(ta.app)
	cmd.SetArgs([]string{"--limit", "7", "--sort", "asc"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	req := srv.lastRequest()
	if req.Path != "/extractors" {
		t.Errorf("path = %q, want /extractors", req.Path)
	}
	if !strings.Contains(req.Query, "maxPageSize=7") || !strings.Contains(req.Query, "sortDir=asc") {
		t.Errorf("query missing filters: %s", req.Query)
	}
}

func TestExtractorsList_JSONJQSeesAPIEnvelope(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "ex_abc", "name": "Extractor", "createdAt": "2025-01-01T00:00:00Z"},
			},
		})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "raw"
	ta.app.JQ = ".data[].id"
	cmd := extractorAccessor().listCmd(ta.app)
	cmd.SetArgs([]string{"--limit", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := strings.TrimSpace(ta.out.String()); got != "ex_abc" {
		t.Errorf("jq output = %q, want ex_abc", got)
	}
}

func TestExtractorsCreate_OverlaysNameOntoFromFile(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "ex_new", "object": "extractor"})
	})
	ta := newTestApp(t, srv)
	cmd := extractorAccessor().createCmd(ta.app)

	tmp := t.TempDir() + "/body.json"
	if err := os.WriteFile(tmp, []byte(`{"description":"from file","schema":{"type":"object"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd.SetArgs([]string{"--from-file", tmp, "--name", "overlay-name"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"name":"overlay-name"`) {
		t.Errorf("expected --name to overlay onto body, got: %s", body)
	}
	if !strings.Contains(body, `"description":"from file"`) {
		t.Errorf("expected description from file preserved, got: %s", body)
	}
}

func TestExtractorVersionCreateRequiresReleaseType(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called without releaseType")
	})
	ta := newTestApp(t, srv)
	cmd := extractorAccessor().versionsCreateCmd(ta.app)
	cmd.SetArgs([]string{"ex_abc", "--description", "notes"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "releaseType is required") {
		t.Fatalf("expected releaseType validation error, got %v", err)
	}
}

func TestExtractorVersionCreateSendsReleaseType(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/extractors/ex_abc/versions" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "exv_abc", "version": "1.0"})
	})
	ta := newTestApp(t, srv)
	cmd := extractorAccessor().versionsCreateCmd(ta.app)
	cmd.SetArgs([]string{"ex_abc", "--release-type", "minor", "--description", "notes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("versions create: %v", err)
	}
	body := string(srv.lastRequest().Body)
	for _, want := range []string{`"releaseType":"minor"`, `"description":"notes"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s: %s", want, body)
		}
	}
}

func TestWorkflowVersionCreateUsesName(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/workflows/workflow_abc/versions" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "workflow_version_abc", "version": "1"})
	})
	ta := newTestApp(t, srv)
	cmd := workflowAccessor().versionsCreateCmd(ta.app)
	cmd.SetArgs([]string{"workflow_abc", "--name", "prod"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("versions create: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"name":"prod"`) || strings.Contains(body, "description") {
		t.Errorf("workflow version body should use name only, got: %s", body)
	}
}

func TestWorkflowsAccessor_HasUpdateCommand(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ta := newTestApp(t, srv)
	cmd := workflowAccessor().cmd(ta.app)
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "update" {
			found = true
		}
	}
	if !found {
		t.Error("workflows command should have update (API supports POST /workflows/{id})")
	}
}

func TestExtractorsAccessor_NoDeleteCommand(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ta := newTestApp(t, srv)
	cmd := extractorAccessor().cmd(ta.app)
	for _, sub := range cmd.Commands() {
		if sub.Name() == "delete" {
			t.Errorf("extractors command should not have 'delete' (API does not support it); found: %s", sub.Use)
		}
	}
}
