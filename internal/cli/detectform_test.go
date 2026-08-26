package cli

import (
	"net/http"
	"strings"
	"testing"
)

// TestDetectForm_HitsFormDetectionEndpoint drives the cobra command
// end to end: POST /form_detection_runs with the config nested under
// "config", then poll GET /form_detection_runs/<id> until PROCESSED
// and print the full run (schema under output.schema).
func TestDetectForm_HitsFormDetectionEndpoint(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/form_detection_runs":
			writeJSON(w, 200, map[string]any{"object": "form_detection_run", "id": "sgr_x", "status": "PROCESSING"})
		case r.Method == http.MethodGet && r.URL.Path == "/form_detection_runs/sgr_x":
			writeJSON(w, 200, map[string]any{
				"object": "form_detection_run",
				"id":     "sgr_x",
				"status": "PROCESSED",
				"output": map[string]any{
					"schema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
				},
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "detect-form")
	cmd.SetArgs([]string{"file_xK9", "--instructions", "skip signatures"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("detect-form: %v", err)
	}
	body := string(srv.requests[0].Body)
	if !strings.Contains(body, `"file":{"id":"file_xK9"}`) {
		t.Errorf("body missing file ref: %s", body)
	}
	if !strings.Contains(body, `"config":{`) || !strings.Contains(body, `"instructions":"skip signatures"`) {
		t.Errorf("instructions must nest under config: %s", body)
	}
	out := ta.out.String()
	if !strings.Contains(out, "sgr_x") || !strings.Contains(out, `"schema"`) {
		t.Errorf("output should contain the run with output.schema: %s", out)
	}
}

// TestDetectFormRunsGet_ValidatesPrefix pins the typed-ID contract on
// the generated runs subgroup: a non-sgr_ ID is rejected client-side
// with a redirect to the owning command.
func TestDetectFormRunsGet_ValidatesPrefix(t *testing.T) {
	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "detect-form", "runs", "get")
	cmd.SetArgs([]string{"edr_x"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "extend edit runs get edr_x") {
		t.Errorf("error should redirect to the edit runs command; got %q", err)
	}
}
