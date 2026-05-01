package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestExtractors exercises the full create -> get -> list flow against a
// single freshly-created extractor. Subtests share that fixture so we don't
// leak multiple resources per run (the API has no DELETE for extractors).
//
// Each subtest must be self-contained in its assertions but may rely on the
// fixture being populated by the parent.
func TestExtractors(t *testing.T) {
	env := requireEnv(t)

	name := itestName(t)
	body := []byte(`{
		"name": "` + name + `",
		"config": {
			"baseProcessor": "extraction_performance",
			"baseVersion": "4.7.0",
			"schema": {
				"type": "object",
				"properties": {"invoice_id": {"type": ["string","null"]}},
				"required": ["invoice_id"],
				"additionalProperties": false
			}
		}
	}`)
	createRes := runExtendWithStdin(t, env, body, "extractors", "create", "--from-file", "-", "-o", "json")
	createRes.requireOK(t, "extractors", "create")

	var created struct {
		ID           string         `json:"id"`
		Object       string         `json:"object"`
		Name         string         `json:"name"`
		DraftVersion map[string]any `json:"draftVersion"`
	}
	createRes.decodeJSON(t, &created)

	t.Run("CreateReturnsExpectedShape", func(t *testing.T) {
		if !strings.HasPrefix(created.ID, "ex_") {
			t.Fatalf("expected created.id with ex_ prefix, got %q", created.ID)
		}
		if created.Object != "extractor" {
			t.Errorf("object = %q, want extractor", created.Object)
		}
		if created.Name != name {
			t.Errorf("name = %q, want %q", created.Name, name)
		}
		if created.DraftVersion == nil {
			t.Fatalf("create response missing draftVersion: %s", createRes.Stdout)
		}
		if vid, _ := created.DraftVersion["id"].(string); !strings.HasPrefix(vid, "exv_") {
			t.Errorf("draftVersion.id = %q, want exv_ prefix", vid)
		}
		if eid, _ := created.DraftVersion["extractorId"].(string); eid != created.ID {
			t.Errorf("draftVersion.extractorId = %q, want %q", eid, created.ID)
		}
		if _, ok := created.DraftVersion["config"].(map[string]any); !ok {
			t.Errorf("draftVersion.config missing or not an object: %v", created.DraftVersion)
		}
	})

	t.Run("GetReturnsPersistedDraftVersion", func(t *testing.T) {
		getRes := runExtend(t, env, "extractors", "get", created.ID, "-o", "json")
		getRes.requireOK(t, "extractors", "get", created.ID)

		var got struct {
			ID           string         `json:"id"`
			Name         string         `json:"name"`
			DraftVersion map[string]any `json:"draftVersion"`
		}
		getRes.decodeJSON(t, &got)

		if got.ID != created.ID {
			t.Errorf("get.id = %q, want %q", got.ID, created.ID)
		}
		if got.Name != name {
			t.Errorf("get.name = %q, want %q", got.Name, name)
		}
		if got.DraftVersion == nil {
			t.Fatalf("get response missing draftVersion: %s", getRes.Stdout)
		}
		if eid, _ := got.DraftVersion["extractorId"].(string); eid != created.ID {
			t.Errorf("get draftVersion.extractorId = %q, want %q", eid, created.ID)
		}
		// The config must include the schema we POSTed.
		cfg, ok := got.DraftVersion["config"].(map[string]any)
		if !ok {
			t.Fatalf("draftVersion.config missing on GET: %s", getRes.Stdout)
		}
		schema, ok := cfg["schema"].(map[string]any)
		if !ok {
			t.Fatalf("draftVersion.config.schema missing on GET: %v", cfg)
		}
		props, _ := schema["properties"].(map[string]any)
		if _, hasField := props["invoice_id"]; !hasField {
			t.Errorf("schema.properties.invoice_id missing; create payload was lossy: %v", props)
		}
	})

	t.Run("ListIncludesCreatedExtractor", func(t *testing.T) {
		// Workspace sort defaults to createdAt desc, so the just-created
		// fixture should always appear in the first page. Use a generous
		// limit so the test isn't sensitive to the exact ordering when many
		// resources are created in quick succession (parallel test runs,
		// etc.).
		listRes := runExtend(t, env, "extractors", "list", "--limit", "20", "-o", "json")
		listRes.requireOK(t, "extractors", "list")

		var page struct {
			Data []struct {
				ID     string `json:"id"`
				Object string `json:"object"`
				Name   string `json:"name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(listRes.Stdout, &page); err != nil {
			t.Fatalf("decode list: %v\nstdout: %s", err, listRes.Stdout)
		}
		var found *struct {
			ID     string `json:"id"`
			Object string `json:"object"`
			Name   string `json:"name"`
		}
		for i := range page.Data {
			if page.Data[i].ID == created.ID {
				found = &page.Data[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("created extractor %s not in first 20 of desc-sorted list", created.ID)
		}
		if found.Object != "extractor" {
			t.Errorf("list item object = %q, want extractor", found.Object)
		}
		if found.Name != name {
			t.Errorf("list item name = %q, want %q", found.Name, name)
		}
	})
}
