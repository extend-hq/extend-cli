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

// TestEditHelpDoesNotPrescribeDefaultField guards against drift in the
// edit help text. Earlier copy claimed values lived in a "default" field
// on each schema entry; agent feedback (recorded in the May 2026 team
// transcript) confirmed that wording sent agents to the wrong API
// shape. The neutral phrasing pointing at `extend edit schema generate`
// is intentional — if a future edit needs to name a specific field,
// confirm it against the server schema first and update this test.
func TestEditHelpDoesNotPrescribeDefaultField(t *testing.T) {
	app := &App{}
	doc := newEditDoc(app)
	for _, blob := range []string{doc.Details, doc.WhenToUse} {
		if strings.Contains(blob, "'default'") {
			t.Errorf("edit help should not name a 'default' field; got:\n%s", blob)
		}
	}
	for _, ex := range doc.Examples {
		if strings.Contains(ex.Note, "default values") {
			t.Errorf("edit example note should not reference 'default values'; got: %q", ex.Note)
		}
	}
	for _, g := range doc.Gotchas {
		if strings.Contains(g, "'default'") {
			t.Errorf("edit gotcha should not name a 'default' field; got: %q", g)
		}
	}
}

// TestSkillFillPDFRecipeMatchesEditHelp ensures the skill's "Fill a PDF
// form" recipe stays consistent with the edit command's help: both must
// mention the two supported paths (--instructions for simple fills,
// --schema for structured fills). Drift between these two surfaces is
// what tripped up agents in the May 2026 agent-experience transcripts.
func TestSkillFillPDFRecipeMatchesEditHelp(t *testing.T) {
	app := &App{}
	skill := RenderSkill(RootDoc(app))
	// Recipe must cover both paths the edit help describes.
	for _, want := range []string{
		"### Fill a PDF form",
		"--instructions",
		"--schema",
		"### Fill a PDF form from values in another document",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("skill missing %q", want)
		}
	}
}

func TestEdit_NestsConfigUnderConfigKey(t *testing.T) {
	tmp := t.TempDir()
	schema := filepath.Join(tmp, "schema.json")
	if err := os.WriteFile(schema, []byte(`{"type":"object","properties":{"foo":{"type":"string"}}}`), 0o600); err != nil {
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
		wait:         true,
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
	// The SDK re-serializes schema field order (typed structs
	// marshal in declared field order, not source order). Assert
	// structural containment rather than exact byte equality.
	if !strings.Contains(postBody, `"schema":{`) || !strings.Contains(postBody, `"foo":{"type":"string"}`) {
		t.Errorf("schema must be inside config and carry the foo property; got %s", postBody)
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
	if err := os.WriteFile(schema, []byte(`{"type":"object","properties":{}}`), 0o600); err != nil {
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
		wait:       true,
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

func TestEdit_OutputFileStdoutDoesNotAppendRunJSON(t *testing.T) {
	tmp := t.TempDir()
	schema := filepath.Join(tmp, "schema.json")
	if err := os.WriteFile(schema, []byte(`{"type":"object","properties":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
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
		outputFile: "-",
		wait:       true,
		nativeOnly: true,
		flatten:    true,
		timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	if got := ta.out.String(); got != "filled-pdf-bytes" {
		t.Errorf("stdout should contain only downloaded bytes, got %q", got)
	}
}

func TestEditSchemaGenerate_HitsSyncEndpoint(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/edit_schemas/generate" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{
			"schema":          map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
			"annotatedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"mappingResult":   nil,
		})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "edit", "schema", "generate")
	cmd.SetArgs([]string{"file_xK9"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("schema generate: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"file":{"id":"file_xK9"}`) {
		t.Errorf("body missing file ref: %s", body)
	}
	out := ta.out.String()
	if !strings.Contains(out, `"type":"object"`) || !strings.Contains(out, `"name"`) {
		t.Errorf("output should contain inner schema: %s", out)
	}
	if strings.Contains(out, "annotatedSchema") || strings.Contains(out, "mappingResult") {
		t.Errorf("schema generate should output directly reusable schema, got envelope: %s", out)
	}
}

// TestEdit_ProcessedButNoOutputFileEmitsWarning is the regression for a
// failure mode flagged in the May 2026 agent-experience transcripts:
// the server returned PROCESSED but the run carried no editedFile, and
// agents reported the edit as successful without noticing the absence
// of a filled PDF. The CLI must surface a stderr warning so the agent
// (and any humans) catch this on the way through.
func TestEdit_ProcessedButNoOutputFileEmitsWarning(t *testing.T) {
	tmp := t.TempDir()
	schema := filepath.Join(tmp, "schema.json")
	if err := os.WriteFile(schema, []byte(`{"type":"object","properties":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/edit_runs":
			writeJSON(w, 200, map[string]any{"id": "edr_empty", "status": "PENDING"})
		case r.Method == http.MethodGet && r.URL.Path == "/edit_runs/edr_empty":
			writeJSON(w, 200, map[string]any{
				"id":     "edr_empty",
				"status": "PROCESSED",
				// No output.editedFile — the failure mode we care about.
				"output": map[string]any{"filledValues": map[string]any{}},
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ta := newTestApp(t, srv)

	err := runEdit(context.Background(), ta.app, editParams{
		input:      "file_a",
		schemaPath: schema,
		wait:       true,
		nativeOnly: true,
		flatten:    true,
		timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	warn := ta.errOut.String()
	for _, want := range []string{"warning", "edr_empty", "no filled PDF", "extend runs get edr_empty"} {
		if !strings.Contains(warn, want) {
			t.Errorf("stderr warning missing %q:\n%s", want, warn)
		}
	}
}

// TestEdit_ProcessedWithOutputFileSilent ensures the warning above is
// suppressed on the happy path (PROCESSED + editedFile present). Drift
// here would mean we're noisy for every successful run, training agents
// to ignore the warning we actually want them to notice.
func TestEdit_ProcessedWithOutputFileSilent(t *testing.T) {
	tmp := t.TempDir()
	schema := filepath.Join(tmp, "schema.json")
	if err := os.WriteFile(schema, []byte(`{"type":"object","properties":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	storage := mockStorage(t, "filled-bytes")

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/edit_runs":
			writeJSON(w, 200, map[string]any{"id": "edr_ok", "status": "PENDING"})
		case r.Method == http.MethodGet && r.URL.Path == "/edit_runs/edr_ok":
			writeJSON(w, 200, map[string]any{
				"id":     "edr_ok",
				"status": "PROCESSED",
				"output": map[string]any{"editedFile": map[string]any{"id": "file_out", "presignedUrl": storage.URL}},
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ta := newTestApp(t, srv)

	err := runEdit(context.Background(), ta.app, editParams{
		input:      "file_a",
		schemaPath: schema,
		wait:       true,
		nativeOnly: true,
		flatten:    true,
		timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	if strings.Contains(ta.errOut.String(), "warning") {
		t.Errorf("happy-path PROCESSED + editedFile should not warn; stderr:\n%s", ta.errOut.String())
	}
}

func TestEdit_FailedRunSurfacesFailureMessage(t *testing.T) {
	tmp := t.TempDir()
	schema := filepath.Join(tmp, "schema.json")
	_ = os.WriteFile(schema, []byte(`{"type":"object","properties":{}}`), 0o600)

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
		wait:       true,
		timeout:    2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "No form fields detected") {
		t.Errorf("expected failure message in error, got %v", err)
	}
}
