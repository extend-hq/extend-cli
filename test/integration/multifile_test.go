package integration

import (
	"strings"
	"testing"
)

// TestExtract_Package extracts across two files in one run with an inline
// JSON-schema config and checks the run covers both files and returns a
// single output.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestExtract_Package(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	amendment := writeTempFile(t, "amendment.txt", []byte("Amendment to invoice INV-12345\nThe total is revised to $249.99.\n"))
	cfg := `{"baseProcessor":"extraction_performance","schema":{"type":"object","properties":{"invoice_number":{"type":["string","null"]},"final_total":{"type":["number","null"],"description":"The total after any amendment"}}}}`
	res := runExtend(t, env,
		"extract", "testdata/sample.txt", amendment,
		"--package",
		"--config", cfg,
		"--timeout", "3m",
		"-o", "json",
	)
	res.requireOK(t, "extract", "--package")

	var run struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Files  []any  `json:"files"`
		Output struct {
			Value map[string]any `json:"value"`
		} `json:"output"`
	}
	res.decodeJSON(t, &run)
	if !strings.HasPrefix(run.ID, "exr_") {
		t.Fatalf("expected exr_ prefix on run id, got %q", run.ID)
	}
	rememberCleanup(t, env, "delete extract run", "extract", "runs", "delete", run.ID, "-y")

	if run.Status != "PROCESSED" {
		t.Errorf("status = %q, want PROCESSED: %s", run.Status, res.Stdout)
	}
	if len(run.Files) != 2 {
		t.Errorf("run has %d files, want 2: %s", len(run.Files), res.Stdout)
	}
	if run.Output.Value == nil {
		t.Errorf("output.value missing: %s", res.Stdout)
	}
}

// workflowWithStep returns the ID of a workflow whose draft has a step of
// the given type, looking at the 20 most recent workflows, or "" if none.
func workflowWithStep(t *testing.T, env envSetup, stepType string) string {
	t.Helper()
	listRes := runExtend(t, env, "workflows", "list", "--limit", "20", "-o", "json")
	listRes.requireOK(t, "workflows", "list")
	for _, w := range listItems(t, listRes) {
		id, _ := w["id"].(string)
		getRes := runExtend(t, env, "workflows", "get", id, "-o", "json")
		getRes.requireOK(t, "workflows", "get", id)
		var got struct {
			DraftVersion struct {
				Steps []struct {
					Type string `json:"type"`
				} `json:"steps"`
			} `json:"draftVersion"`
		}
		getRes.decodeJSON(t, &got)
		for _, s := range got.DraftVersion.Steps {
			if s.Type == stepType {
				return id
			}
		}
	}
	return ""
}

// TestWorkflowsRun_Package starts one workflow run over two files and
// checks the API accepted the package. It needs a workflow with a COLLECT
// step, and cancels the run instead of waiting for it.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestWorkflowsRun_Package(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	workflowID := workflowWithStep(t, env, "COLLECT")
	if workflowID == "" {
		t.Skip("no workflow with a COLLECT step among the 20 most recent; create one to enable this test")
	}

	second := writeTempFile(t, "second.txt", []byte("Invoice\nNumber: INV-67890\nTotal: $20.00\n"))
	res := runExtend(t, env,
		"workflows", "run", "testdata/sample.txt", second,
		"--package",
		"--using", workflowID,
		"--version", "draft",
		"-o", "json",
	)
	res.requireOK(t, "workflows", "run", "--package")

	var run struct {
		ID    string `json:"id"`
		Files []any  `json:"files"`
	}
	res.decodeJSON(t, &run)
	if !strings.HasPrefix(run.ID, "workflow_run_") {
		t.Fatalf("expected workflow_run_ prefix on run id, got %q", run.ID)
	}
	// Cleanups run last-in first-out: cancel, then delete.
	rememberCleanup(t, env, "delete workflow run", "workflows", "runs", "delete", run.ID, "-y")
	rememberCleanup(t, env, "cancel workflow run", "workflows", "runs", "cancel", run.ID, "-y")

	if len(run.Files) != 2 {
		t.Errorf("run has %d files, want 2: %s", len(run.Files), res.Stdout)
	}
}
