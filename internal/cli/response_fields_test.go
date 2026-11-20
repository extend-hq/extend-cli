package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// Fields the SDK doesn't model must still reach -o json and --jq, since
// the API adds response fields between SDK releases.
func TestJSONOutputKeepsFieldsTheSDKDoesNotModel(t *testing.T) {
	run := map[string]any{
		"object":        "extract_run",
		"id":            "exr_1",
		"status":        "PROCESSED",
		"failureReason": nil,
		"files": []any{
			map[string]any{"object": "file", "id": "file_1", "name": "a.pdf", "parseRunId": "pr_1"},
		},
		"notYetInTheSDK": map[string]any{"mode": "zero"},
	}

	t.Run("get", func(t *testing.T) {
		srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, run)
		})
		ta := newTestApp(t, srv)
		ta.app.Format = "json"
		if err := runTypedRunsGet(context.Background(), ta.app, extendx.KindExtract, "exr_1", ""); err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(ta.out.Bytes(), &got); err != nil {
			t.Fatalf("output is not JSON: %v\n%s", err, ta.out.String())
		}
		if _, ok := got["notYetInTheSDK"]; !ok {
			t.Errorf("unknown top-level field dropped:\n%s", ta.out.String())
		}
		if v, ok := got["failureReason"]; !ok || v != nil {
			t.Errorf("explicit null dropped:\n%s", ta.out.String())
		}
		file := got["files"].([]any)[0].(map[string]any)
		if file["parseRunId"] != "pr_1" {
			t.Errorf("unknown nested field dropped:\n%s", ta.out.String())
		}
	})

	t.Run("jq", func(t *testing.T) {
		srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, run)
		})
		ta := newTestApp(t, srv)
		ta.app.Format = "raw"
		ta.app.JQ = ".notYetInTheSDK.mode"
		if err := runTypedRunsGet(context.Background(), ta.app, extendx.KindExtract, "exr_1", ""); err != nil {
			t.Fatal(err)
		}
		if got := ta.out.String(); got != "zero\n" {
			t.Errorf("--jq output = %q, want %q", got, "zero\n")
		}
	})

	t.Run("list", func(t *testing.T) {
		srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"object":         "list",
				"data":           []any{run},
				"notYetInTheSDK": true,
			})
		})
		ta := newTestApp(t, srv)
		ta.app.Format = "json"
		cmd := findCmd(t, ta.app, "extract", "runs", "list")
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(ta.out.Bytes(), &got); err != nil {
			t.Fatalf("output is not JSON: %v\n%s", err, ta.out.String())
		}
		if got["notYetInTheSDK"] != true {
			t.Errorf("unknown field on the list dropped:\n%s", ta.out.String())
		}
		item := got["data"].([]any)[0].(map[string]any)
		if _, ok := item["notYetInTheSDK"]; !ok {
			t.Errorf("unknown field on a list item dropped:\n%s", ta.out.String())
		}
	})
}
