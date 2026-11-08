package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestEditRunsList_SendsFiltersAndShowsFiles(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/edit_runs" {
			t.Fatalf("hit %s %s, want GET /edit_runs", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"object": "list", "data": []map[string]any{
			{"object": "edit_run", "id": "edr_1", "status": "FAILED",
				"file":      map[string]any{"object": "file", "id": "file_1", "name": "w9.pdf"},
				"createdAt": "2026-11-06T10:00:00Z", "updatedAt": "2026-11-06T10:01:00Z"},
		}})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "table"
	cmd := findCmd(t, ta.app, "edit", "runs", "list")
	cmd.SetArgs([]string{"--status", "FAILED", "--source", "API", "--file-name", "w9", "--sort-by", "createdAt", "--sort", "asc"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	q, err := url.ParseQuery(srv.lastRequest().Query)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"status": "FAILED", "source": "API", "fileNameContains": "w9", "sortBy": "createdAt", "sortDir": "asc"}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("query %s = %q, want %q (full query %q)", k, got, v, srv.lastRequest().Query)
		}
	}
	out := ta.out.String()
	if !strings.Contains(out, "FILE") || !strings.Contains(out, "w9.pdf") || !strings.Contains(out, "edr_1") {
		t.Errorf("table = %q, want edr_1 with a FILE column showing w9.pdf", out)
	}
}

func TestEditRunsList_HasNoBatchFilter(t *testing.T) {
	ta := newTestApp(t, nil)
	cmd := findCmd(t, ta.app, "edit", "runs", "list")
	if cmd.Flags().Lookup("batch") != nil {
		t.Error("edit runs list has a --batch flag, but edit runs have no batches")
	}
	if cmd.Flags().Lookup("source-id") != nil {
		t.Error("edit runs list has a --source-id flag, but the endpoint has no such filter")
	}
}

func TestEditTemplatesList_SendsFiltersAndPaginates(t *testing.T) {
	var calls int
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/edit_templates" {
			t.Fatalf("hit %s %s, want GET /edit_templates", r.Method, r.URL.Path)
		}
		calls++
		if calls == 1 {
			writeJSON(w, 200, map[string]any{"object": "list", "nextPageToken": "tok2", "data": []map[string]any{
				{"object": "edit_template", "id": "edt_1", "name": "W-9 (2024)",
					"file":      map[string]any{"object": "file", "id": "file_1", "name": "fw9.pdf"},
					"createdAt": "2026-11-01T10:00:00Z", "updatedAt": "2026-11-04T10:00:00Z"},
			}})
			return
		}
		writeJSON(w, 200, map[string]any{"object": "list", "data": []map[string]any{
			{"object": "edit_template", "id": "edt_2", "name": "W-9 (2023)",
				"file":      map[string]any{"object": "file", "id": "file_2", "name": "fw9-2023.pdf"},
				"createdAt": "2026-10-01T10:00:00Z", "updatedAt": "2026-10-04T10:00:00Z"},
		}})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "table"
	cmd := findCmd(t, ta.app, "edit", "templates", "list")
	cmd.SetArgs([]string{"--name-contains", "W-9", "--max", "5"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if calls != 2 {
		t.Errorf("made %d list calls, want 2 (--max follows the page token)", calls)
	}
	q, _ := url.ParseQuery(srv.lastRequest().Query)
	if q.Get("nameContains") != "W-9" || q.Get("nextPageToken") != "tok2" {
		t.Errorf("second query = %q, want nameContains=W-9 and nextPageToken=tok2", srv.lastRequest().Query)
	}
	out := ta.out.String()
	for _, want := range []string{"edt_1", "edt_2", "fw9.pdf", "W-9 (2024)"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

func editTemplateServer(t *testing.T) *fakeServer {
	t.Helper()
	return newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/edit_runs":
			writeJSON(w, 200, map[string]any{"id": "edr_t", "status": "PROCESSING"})
		case r.Method == http.MethodGet && r.URL.Path == "/edit_runs/edr_t":
			writeJSON(w, 200, map[string]any{"id": "edr_t", "status": "PROCESSED",
				"output": map[string]any{"editedFile": map[string]any{"id": "file_filled"}}})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestEdit_TemplateSendsTemplateIDInsteadOfFile(t *testing.T) {
	srv := editTemplateServer(t)
	ta := newTestApp(t, srv)
	if err := runEdit(context.Background(), ta.app, editParams{
		templateID:   "edt_9fK2a",
		instructions: "Name: Acme Corp",
		wait:         true,
		timeout:      2 * time.Second,
	}); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(srv.requests[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["templateId"] != "edt_9fK2a" {
		t.Errorf("templateId = %v, want edt_9fK2a (body %s)", body["templateId"], srv.requests[0].Body)
	}
	if _, ok := body["file"]; ok {
		t.Errorf("body has a file key alongside templateId; the API rejects both (body %s)", srv.requests[0].Body)
	}
	if cfg, _ := body["config"].(map[string]any); cfg["instructions"] != "Name: Acme Corp" {
		t.Errorf("config = %v, want the instructions override", body["config"])
	}
	if !strings.Contains(ta.out.String(), "file_filled") {
		t.Errorf("stdout = %q, want the filled PDF's file ID", ta.out.String())
	}
}

func TestEdit_TemplateArgumentRules(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called, got %s %s", r.Method, r.URL.Path)
	})
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"form.pdf", "--template", "edt_x"}, "pass either <input> or --template, not both"},
		{[]string{}, "provide an input (a file path, file_xxx ID, or https:// URL) or use --template"},
		{[]string{"--template", "edt_x", "--password", "pw"}, "--password doesn't apply with --template"},
	}
	for _, tc := range cases {
		ta := newTestApp(t, srv)
		cmd := findCmd(t, ta.app, "edit")
		cmd.SetArgs(tc.args)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("extend edit %v: err = %v, want %q", tc.args, err, tc.want)
		}
	}
}
