package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWorkflowsList exercises `extend workflows list -o json` end-to-end.
// Workflows cannot be created in this test (the create body is complex and
// requires a full step graph), so when the workspace is empty the test
// skips with an actionable message rather than passing trivially. When
// items ARE present, each is checked for a populated id/name/createdAt.
func TestWorkflowsList(t *testing.T) {
	env := requireEnv(t)
	res := runExtend(t, env, "workflows", "list", "--limit", "5", "-o", "json")
	res.requireOK(t, "workflows", "list")

	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(res.Stdout, &page); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, res.Stdout)
	}
	if len(page.Data) == 0 {
		t.Skip("workspace has no workflows; this test needs at least one to verify list shape")
	}
	for i, w := range page.Data {
		if id, _ := w["id"].(string); !strings.HasPrefix(id, "workflow_") {
			t.Errorf("item %d id = %q, want workflow_ prefix", i, id)
		}
		if obj, _ := w["object"].(string); obj != "workflow" {
			t.Errorf("item %d object = %q, want workflow", i, obj)
		}
		if name, _ := w["name"].(string); name == "" {
			t.Errorf("item %d name is empty: %+v", i, w)
		}
		if createdAt, _ := w["createdAt"].(string); createdAt == "" {
			t.Errorf("item %d createdAt is empty: %+v", i, w)
		}
	}
}

// TestWorkflowGet_ExposesDraftVersionWithSteps verifies that a full GET on a
// workflow returns its draftVersion with a populated steps[] array. Skips
// (with an actionable message) when the workspace has no workflows.
func TestWorkflowGet_ExposesDraftVersionWithSteps(t *testing.T) {
	env := requireEnv(t)
	listRes := runExtend(t, env, "workflows", "list", "--limit", "1", "-o", "json")
	listRes.requireOK(t, "workflows", "list")

	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRes.Stdout, &page); err != nil {
		t.Fatalf("decode list: %v\nstdout: %s", err, listRes.Stdout)
	}
	if len(page.Data) == 0 {
		t.Skip("workspace has no workflows; this test needs at least one to verify GET draftVersion shape")
	}

	id := page.Data[0].ID
	getRes := runExtend(t, env, "workflows", "get", id, "-o", "json")
	getRes.requireOK(t, "workflows", "get", id)

	var got struct {
		ID           string         `json:"id"`
		Object       string         `json:"object"`
		Name         string         `json:"name"`
		DraftVersion map[string]any `json:"draftVersion"`
	}
	getRes.decodeJSON(t, &got)

	if got.ID != id {
		t.Errorf("get.id = %q, want %q", got.ID, id)
	}
	if got.Object != "workflow" {
		t.Errorf("get.object = %q, want workflow", got.Object)
	}
	if got.DraftVersion == nil {
		t.Fatalf("draftVersion missing from workflows get response; got: %s", getRes.Stdout)
	}
	if vid, _ := got.DraftVersion["id"].(string); !strings.HasPrefix(vid, "workflow_version_") {
		t.Errorf("draftVersion.id = %q, want workflow_version_ prefix", vid)
	}
	steps, ok := got.DraftVersion["steps"].([]any)
	if !ok {
		t.Fatalf("draftVersion.steps missing or not an array: %v", got.DraftVersion)
	}
	if len(steps) == 0 {
		t.Errorf("draftVersion.steps is empty; expected at least one step on a deployable workflow")
	}
	// Each step has a `type` discriminator from the closed enum (PARSE,
	// EXTRACT, CLASSIFY, SPLIT, MERGE_EXTRACT, CONDITIONAL_EXTRACT,
	// RULE_VALIDATION, EXTERNAL_DATA_VALIDATION, TRIGGER).
	validStepTypes := map[string]bool{
		"PARSE": true, "EXTRACT": true, "CLASSIFY": true, "SPLIT": true,
		"MERGE_EXTRACT": true, "CONDITIONAL_EXTRACT": true,
		"RULE_VALIDATION": true, "EXTERNAL_DATA_VALIDATION": true,
		"TRIGGER": true,
	}
	for i, s := range steps {
		stepMap, ok := s.(map[string]any)
		if !ok {
			t.Errorf("step %d not an object: %T", i, s)
			continue
		}
		typ, _ := stepMap["type"].(string)
		if !validStepTypes[typ] {
			t.Errorf("step %d type = %q, not in known step-type enum", i, typ)
		}
	}
}
