package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestExtractHelpDistinguishesConfigFlags locks in the disambiguation
// between --config (standalone, no saved extractor) and --patch (per-run
// partial merge onto a --using extractor). The names alone don't tell
// the whole story, so Details, Gotchas, and flag usage must each carry
// the distinction; if a refactor loses it, this test fires.
func TestExtractHelpDistinguishesConfigFlags(t *testing.T) {
	app := &App{}
	doc := newExtractDoc(app)

	// Details must explain that the two flags are NOT interchangeable.
	if !strings.Contains(doc.Details, "NOT") || !strings.Contains(doc.Details, "interchangeable") {
		t.Errorf("extract Details should call out that --config and --patch are not interchangeable; got:\n%s", doc.Details)
	}

	// At least one gotcha must contrast the two flags.
	var gotchaFound bool
	for _, g := range doc.Gotchas {
		if strings.Contains(g, "--config") && strings.Contains(g, "--patch") {
			gotchaFound = true
			break
		}
	}
	if !gotchaFound {
		t.Errorf("extract should have a gotcha contrasting --config and --patch; got: %v", doc.Gotchas)
	}
}

func TestExtract_AsyncReturnsRunID(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract_runs" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{
			"id":     "exr_abc",
			"object": "extract_run",
			"status": "PENDING",
		})
	})
	ta := newTestApp(t, srv)

	err := runExtract(context.Background(), ta.app, extractParams{
		input:       "file_xK9",
		extractorID: "ex_abc",
		wait:        false,
	})
	if err != nil {
		t.Fatalf("runExtract: %v", err)
	}
	if !strings.Contains(ta.out.String(), `"id":"exr_abc"`) {
		t.Errorf("expected exr_abc in stdout, got: %s", ta.out.String())
	}
	if got := srv.lastRequest().Header.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", got)
	}
	if got := srv.lastRequest().Header.Get("x-extend-api-version"); got == "" {
		t.Errorf("expected x-extend-api-version header, got empty")
	}
}

func TestExtract_WaitPath(t *testing.T) {
	getCalls := 0
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/extract_runs":
			writeJSON(w, 200, map[string]any{"id": "exr_xyz", "status": "PENDING"})
		case r.Method == http.MethodGet && r.URL.Path == "/extract_runs/exr_xyz":
			getCalls++
			status := "PROCESSING"
			if getCalls >= 2 {
				status = "PROCESSED"
			}
			writeJSON(w, 200, map[string]any{
				"id":     "exr_xyz",
				"status": status,
				"output": map[string]any{"value": "hello"},
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ta := newTestApp(t, srv)
	ta.app.JQ = ".output.value"
	ta.app.Format = "raw"

	err := runExtract(context.Background(), ta.app, extractParams{
		input:       "file_xK9",
		extractorID: "ex_abc",
		wait:        true,
		timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatalf("runExtract: %v", err)
	}
	if got := strings.TrimSpace(ta.out.String()); got != "hello" {
		t.Errorf("stdout = %q, want hello", got)
	}
	if getCalls < 2 {
		t.Errorf("expected at least 2 GET calls, got %d", getCalls)
	}
}

// TestExtract_WaitTimeoutSurfacesActionableMessage locks in the action-
// verb wait timeout wording. Agents previously got back a bare "wait:
// context deadline exceeded" with no recovery action, which led them to
// silently rerun the same blocking command. The new message must name
// the run ID, the --timeout flag, and the --wait=false / runs watch
// alternative.
func TestExtract_WaitTimeoutSurfacesActionableMessage(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			writeJSON(w, 200, map[string]any{"id": "exr_slow", "status": "PENDING"})
		case r.Method == http.MethodGet:
			writeJSON(w, 200, map[string]any{"id": "exr_slow", "status": "PROCESSING"})
		}
	})
	ta := newTestApp(t, srv)

	err := runExtract(context.Background(), ta.app, extractParams{
		input:       "file_xK9",
		extractorID: "ex_abc",
		wait:        true,
		timeout:     150 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	msg := err.Error()
	for _, want := range []string{"exr_slow", "--timeout", "--wait=false", "extend runs watch exr_slow"} {
		if !strings.Contains(msg, want) {
			t.Errorf("action wait timeout error missing %q: %s", want, msg)
		}
	}
}

func TestExtract_FailedRunSurfacesAsError(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			writeJSON(w, 200, map[string]any{"id": "exr_fail", "status": "PENDING"})
		case r.Method == http.MethodGet:
			writeJSON(w, 200, map[string]any{"id": "exr_fail", "status": "FAILED"})
		}
	})
	ta := newTestApp(t, srv)

	err := runExtract(context.Background(), ta.app, extractParams{
		input:       "file_xK9",
		extractorID: "ex_abc",
		wait:        true,
		timeout:     2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Errorf("expected 'failed' error, got %v", err)
	}
}

func TestExtract_APIErrorRenderedNicely(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeAPIError(w, 404, "NOT_FOUND", "Resource ex_doesnotexist not found")
	})
	ta := newTestApp(t, srv)

	err := runExtract(context.Background(), ta.app, extractParams{
		input:       "file_xK9",
		extractorID: "ex_doesnotexist",
		wait:        false,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Errorf("expected NOT_FOUND in error, got: %v", err)
	}
}

func TestExtract_URLInputPassedThrough(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "exr_url", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)

	if err := runExtract(context.Background(), ta.app, extractParams{
		input:       "https://example.com/x.pdf",
		extractorID: "ex_abc",
		wait:        false,
	}); err != nil {
		t.Fatalf("runExtract: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"url":"https://example.com/x.pdf"`) {
		t.Errorf("body should pass URL through; got: %s", body)
	}
}

func TestExtract_MetadataAndTagsInRequestBody(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "exr_md", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runExtract(context.Background(), ta.app, extractParams{
		input:       "file_xK9",
		extractorID: "ex_abc",
		wait:        false,
		metadata: map[string]any{
			"customer":          "acme",
			"env":               "staging",
			"extend:usage_tags": []string{"prod", "team-eng"},
		},
	}); err != nil {
		t.Fatalf("runExtract: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"customer":"acme"`) {
		t.Errorf("body missing customer metadata: %s", body)
	}
	if !strings.Contains(body, `"env":"staging"`) {
		t.Errorf("body missing env metadata: %s", body)
	}
	if !strings.Contains(body, `"extend:usage_tags":["prod","team-eng"]`) {
		t.Errorf("body missing usage tags: %s", body)
	}
}

func TestExtract_PatchFromFile(t *testing.T) {
	tmp := t.TempDir() + "/patch.json"
	if err := writeFileForTest(tmp, []byte(`{"fields":[{"key":"foo"}]}`)); err != nil {
		t.Fatal(err)
	}

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "exr_oc", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runExtract(context.Background(), ta.app, extractParams{
		input:       "file_xK9",
		extractorID: "ex_abc",
		patchPath:   tmp,
		wait:        false,
	}); err != nil {
		t.Fatalf("runExtract: %v", err)
	}
	// Wire field name is still `overrideConfig` — only the CLI flag changed.
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"overrideConfig":{"fields":[{"key":"foo"}]}`) {
		t.Errorf("body should embed overrideConfig under extractor: %s", body)
	}
}

func TestExtract_InlineConfigSkipsExtractor(t *testing.T) {
	tmp := t.TempDir() + "/config.json"
	if err := writeFileForTest(tmp, []byte(`{"fields":[{"key":"bar","type":"string"}]}`)); err != nil {
		t.Fatal(err)
	}

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "exr_inline", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runExtract(context.Background(), ta.app, extractParams{
		input:      "file_xK9",
		configPath: tmp,
		wait:       false,
	}); err != nil {
		t.Fatalf("runExtract: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"config":{"fields":[`) {
		t.Errorf("body should include inline config: %s", body)
	}
	if strings.Contains(body, `"extractor"`) {
		t.Errorf("body must not include extractor when --config is set: %s", body)
	}
}

func TestExtract_RequiresUsingOrConfig(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when validation fails")
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "extract")
	cmd.SetArgs([]string{"file_xK9"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--using or --config") {
		t.Errorf("expected --using-or-config error, got %v", err)
	}
}

func TestExtract_RejectsBothUsingAndConfig(t *testing.T) {
	tmp := t.TempDir() + "/c.json"
	if err := writeFileForTest(tmp, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when both flags are set")
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "extract")
	cmd.SetArgs([]string{"file_xK9", "--using", "ex_abc", "--config", tmp})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--using or --config") {
		t.Errorf("expected mutex error, got %v", err)
	}
}

func TestExtract_InvalidJSONPatchErrorsClearly(t *testing.T) {
	tmp := t.TempDir() + "/bad.json"
	if err := writeFileForTest(tmp, []byte(`{not json`)); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when JSON is invalid")
	})
	ta := newTestApp(t, srv)
	err := runExtract(context.Background(), ta.app, extractParams{
		input:       "file_xK9",
		extractorID: "ex_abc",
		patchPath:   tmp,
		wait:        false,
	})
	if err == nil || !strings.Contains(err.Error(), "--patch") {
		t.Errorf("expected --patch error, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("expected JSON validity message, got %v", err)
	}
}
