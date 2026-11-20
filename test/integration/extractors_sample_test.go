package integration

import (
	"strings"
	"testing"
)

// TestExtractorsCreate_GeneratesSchemaFromSample has Extend write an
// extractor's schema from a sample document. Generation runs inside the
// create request and takes far longer than the CLI's default per-request
// timeout, so this also checks the request isn't cut off.
//
// Leaves one extractor behind per run (the API has no DELETE for
// extractors) and costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestExtractorsCreate_GeneratesSchemaFromSample(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	name := itestName(t)
	sample := writeTempFile(t, "invoice.pdf", invoicePDF())
	res := runExtend(t, env,
		"extractors", "create",
		"--name", name,
		"--sample", sample,
		"--instructions", "Invoices: invoice number, date, customer, and total.",
		"-o", "json",
	)
	res.requireOK(t, "extractors", "create", "--sample")

	var created struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		DraftVersion struct {
			Config struct {
				Schema struct {
					Properties map[string]any `json:"properties"`
				} `json:"schema"`
			} `json:"config"`
		} `json:"draftVersion"`
	}
	res.decodeJSON(t, &created)
	if !strings.HasPrefix(created.ID, "ex_") {
		t.Fatalf("expected ex_ prefix on extractor id, got %q", created.ID)
	}
	if created.Name != name {
		t.Errorf("name = %q, want %q", created.Name, name)
	}
	if len(created.DraftVersion.Config.Schema.Properties) == 0 {
		t.Errorf("draftVersion.config.schema has no properties; expected a generated schema: %s", res.Stdout)
	}
}
