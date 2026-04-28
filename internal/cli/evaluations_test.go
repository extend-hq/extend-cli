package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestEvaluationsList_HitsExpectedEndpoint(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	ta := newTestApp(t, srv)
	cmd := newEvaluationsListCommand(ta.app)
	cmd.SetArgs([]string{"--limit", "5"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if srv.lastRequest().Path != "/evaluation_sets" {
		t.Errorf("path = %q, want /evaluation_sets", srv.lastRequest().Path)
	}
}

func TestEvaluationsCreate_BuildsBodyFromFlags(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "ev_new", "object": "evaluation_set"})
	})
	ta := newTestApp(t, srv)
	cmd := newEvaluationsCreateCommand(ta.app)
	cmd.SetArgs([]string{"--name", "from-flag", "--description", "test"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"name":"from-flag"`) {
		t.Errorf("body missing name: %s", body)
	}
	if !strings.Contains(body, `"description":"test"`) {
		t.Errorf("body missing description: %s", body)
	}
}

func TestEvaluationItemsUpdate_UsesPOST(t *testing.T) {
	// Server route is `POST /evaluation_sets/:setId/items/:itemId`, not PATCH.
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/evaluation_sets/ev_a/items/it_b" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "it_b", "object": "evaluation_set_item"})
	})
	ta := newTestApp(t, srv)
	tmp := t.TempDir() + "/patch.json"
	if err := writeFile(tmp, []byte(`{"expectedOutput":{}}`)); err != nil {
		t.Fatal(err)
	}
	cmd := newEvaluationItemsUpdateCommand(ta.app)
	cmd.SetArgs([]string{"ev_a", "it_b", "--from-file", tmp})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update: %v", err)
	}
}

func TestEvaluationItemsCreate_DecodesEvaluationSetItemsEnvelope(t *testing.T) {
	// Server returns `{evaluationSetItems: [...]}` rather than a bare item or
	// `{data:[...]}`. Verify we decode that wrapper.
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/evaluation_sets/ev_a/items" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{
			"evaluationSetItems": []any{
				map[string]any{
					"id": "it_1", "object": "evaluation_set_item",
					"evaluationSetId": "ev_a",
					"file":            map[string]any{"id": "file_1", "name": "a.pdf"},
					"expectedOutput":  map[string]any{"value": map[string]any{}},
				},
			},
		})
	})
	ta := newTestApp(t, srv)
	tmp := t.TempDir() + "/items.json"
	if err := writeFile(tmp, []byte(`{"items":[{"fileId":"file_1","expectedOutput":{"value":{}}}]}`)); err != nil {
		t.Fatal(err)
	}
	cmd := newEvaluationItemsCreateCommand(ta.app)
	cmd.SetArgs([]string{"ev_a", "--from-file", tmp})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(ta.out.String(), `"evaluationSetItems"`) {
		t.Errorf("output should expose envelope key, got: %s", ta.out.String())
	}
}

func TestEvaluationRunsGet_UsesEvaluationSetRunsRoute(t *testing.T) {
	// Server route is /evaluation_set_runs/:id (no setId in path).
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/evaluation_set_runs/esr_x" {
			t.Errorf("path = %q, want /evaluation_set_runs/esr_x", r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "esr_x", "object": "evaluation_set_run"})
	})
	ta := newTestApp(t, srv)
	cmd := newEvaluationRunsCommand(ta.app)
	cmd.SetArgs([]string{"get", "esr_x"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("get: %v", err)
	}
}

func writeFile(path string, data []byte) error {
	return writeFileForTest(path, data)
}
