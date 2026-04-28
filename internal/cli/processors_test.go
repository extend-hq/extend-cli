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

func TestWorkflowsAccessor_HasNoUpdateCommand(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ta := newTestApp(t, srv)
	cmd := workflowAccessor().cmd(ta.app)
	for _, sub := range cmd.Commands() {
		if sub.Name() == "update" {
			t.Errorf("workflows command should not have 'update' (API does not support it); found: %s", sub.Use)
		}
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
