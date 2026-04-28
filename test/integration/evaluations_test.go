package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEvaluationsList exercises GET /evaluation_sets paginated. Skips when
// the workspace has no evaluation sets (eval sets cannot be created via the
// CLI today, so we don't auto-create a fixture).
func TestEvaluationsList(t *testing.T) {
	env := requireEnv(t)
	res := runExtend(t, env, "evaluations", "list", "--limit", "5", "-o", "json")
	res.requireOK(t, "evaluations", "list")

	// `evaluations list` returns a paginated envelope `{data:[], nextPageToken}`.
	var page struct {
		Data []map[string]any `json:"data"`
	}
	res.decodeJSON(t, &page)
	if len(page.Data) == 0 {
		t.Skip("workspace has no evaluation sets; this test needs at least one to verify list shape")
	}
	for i, e := range page.Data {
		if id, _ := e["id"].(string); !strings.HasPrefix(id, "ev_") {
			t.Errorf("item %d id = %q, want ev_ prefix", i, id)
		}
		if obj, _ := e["object"].(string); obj != "evaluation_set" {
			t.Errorf("item %d object = %q, want evaluation_set", i, obj)
		}
		if name, _ := e["name"].(string); name == "" {
			t.Errorf("item %d name is empty: %+v", i, e)
		}
		if createdAt, _ := e["createdAt"].(string); createdAt == "" {
			t.Errorf("item %d createdAt is empty: %+v", i, e)
		}
	}
}

// TestEvaluationRunsGet_UsesEvaluationSetRunsRoute verifies the route is
// /evaluation_set_runs/{id} (not /evaluation_sets/{set}/runs/{id}, which
// 404s on 2026-02-09). Picks an existing eval set run from the workspace;
// skips if none exist.
func TestEvaluationRunsGet_UsesEvaluationSetRunsRoute(t *testing.T) {
	env := requireEnv(t)

	// Find an evaluation set first so we can scope the run lookup.
	listRes := runExtend(t, env, "evaluations", "list", "--limit", "10", "-o", "json")
	listRes.requireOK(t, "evaluations", "list")
	var listPage struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	listRes.decodeJSON(t, &listPage)
	if len(listPage.Data) == 0 {
		t.Skip("no evaluation sets in workspace; skipping eval run route test")
	}

	// Probe each eval set in the page until we find one with at least one run.
	// We use the CLI's get-set command which embeds recent runs (no separate
	// list-runs endpoint exists per the API surface).
	var runID string
	for _, set := range listPage.Data {
		setRes := runExtend(t, env, "evaluations", "get", set.ID, "-o", "json")
		setRes.requireOK(t, "evaluations", "get", set.ID)
		var setBody map[string]any
		setRes.decodeJSON(t, &setBody)
		// `runs` is an array on a fully-detailed eval set, if the workspace
		// version returns it; otherwise we fall back to skipping.
		runs, _ := setBody["runs"].([]any)
		if len(runs) == 0 {
			continue
		}
		first, ok := runs[0].(map[string]any)
		if !ok {
			continue
		}
		if id, _ := first["id"].(string); id != "" {
			runID = id
			break
		}
	}
	if runID == "" {
		t.Skip("no evaluation set runs found in workspace; skipping route test")
	}

	res := runExtend(t, env, "evaluations", "runs", "get", runID, "-o", "json")
	res.requireOK(t, "evaluations", "runs", "get", runID)

	var got map[string]any
	res.decodeJSON(t, &got)
	if obj, _ := got["object"].(string); obj != "evaluation_set_run" {
		t.Errorf("object = %q, want evaluation_set_run", obj)
	}
	if id, _ := got["id"].(string); id != runID {
		t.Errorf("id = %q, want %q (route should resolve to the right run)", id, runID)
	}
}

// TestEvaluationItemsCreate_ReturnsEnvelope verifies that bulk-create returns
// the `{evaluationSetItems:[...]}` envelope (not a bare item) and that we
// decode the items correctly. Requires an existing evaluation set with a
// pre-uploaded sample file.
func TestEvaluationItemsCreate_ReturnsEnvelope(t *testing.T) {
	env := requireEnv(t)

	// Reuse an existing eval set; if none, skip rather than create one
	// (eval sets can't be deleted, so creating leaks state).
	listRes := runExtend(t, env, "evaluations", "list", "--limit", "1", "-o", "json")
	listRes.requireOK(t, "evaluations", "list")
	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	listRes.decodeJSON(t, &page)
	if len(page.Data) == 0 {
		t.Skip("no evaluation sets to attach an item to; skipping")
	}
	setID := page.Data[0].ID

	// Upload a sample file to use as the item payload. Files support delete,
	// so we don't leak.
	upRes := runExtend(t, env, "files", "upload", "testdata/sample.txt", "-o", "json")
	upRes.requireOK(t, "files", "upload")
	var uploaded struct {
		ID string `json:"id"`
	}
	upRes.decodeJSON(t, &uploaded)
	rememberCleanup(t, env, "delete file", "files", "delete", uploaded.ID, "-y")

	// Build the bulk body. The server schema is {items:[{fileId, expectedOutput}]}.
	body := []byte(`{"items":[{"fileId":"` + uploaded.ID + `","expectedOutput":{"value":{"any":"value"}}}]}`)
	createRes := runExtendWithStdin(t, env, body, "evaluations", "items", "create", setID, "--from-file", "-", "-o", "json")
	createRes.requireOK(t, "evaluations", "items", "create")

	// The CLI re-emits the server's envelope verbatim under -o json; verify
	// the wrapper key is present and contains a populated array.
	var resp struct {
		EvaluationSetItems []struct {
			ID     string         `json:"id"`
			Object string         `json:"object"`
			File   map[string]any `json:"file"`
		} `json:"evaluationSetItems"`
	}
	if err := json.Unmarshal(createRes.Stdout, &resp); err != nil {
		t.Fatalf("decode envelope: %v\nstdout: %s", err, createRes.Stdout)
	}
	if len(resp.EvaluationSetItems) != 1 {
		t.Fatalf("expected 1 item in envelope, got %d: %s", len(resp.EvaluationSetItems), createRes.Stdout)
	}
	item := resp.EvaluationSetItems[0]
	if !strings.HasPrefix(item.ID, "evi_") {
		t.Errorf("item id = %q, want evi_ prefix", item.ID)
	}
	if item.File == nil {
		t.Errorf("item should embed file summary; got: %s", createRes.Stdout)
	}
	rememberCleanup(t, env, "delete eval item",
		"evaluations", "items", "delete", setID, item.ID, "-y")
}
