package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

func TestWorkspaces_ListPaginatesAndFormatsDates(t *testing.T) {
	var queries []string
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/workspaces" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("nextPageToken") == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"object": "list",
				"data": []map[string]any{{
					"object":    "workspace",
					"id":        "ws_1",
					"name":      "Acme US",
					"createdAt": "2025-01-01T08:30:00Z",
					"updatedAt": "2025-02-01T09:00:00Z",
				}},
				"nextPageToken": "tok_2",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"data": []map[string]any{{
				"object":    "workspace",
				"id":        "ws_2",
				"name":      "Acme EU",
				"createdAt": "2025-03-01T10:00:00Z",
				"updatedAt": "2025-04-01T11:00:00Z",
			}},
		})
	})

	ta := newTestApp(t, srv)
	ta.app.Workspace = "ws_1"
	cmd := findCmd(t, ta.app, "workspaces", "list")
	cmd.SetArgs([]string{"--sort", "asc", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workspaces list: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("expected 2 paginated requests, got %d (%v)", len(queries), queries)
	}
	if !strings.Contains(queries[0], "sortDir=asc") || !strings.Contains(queries[1], "nextPageToken=tok_2") {
		t.Errorf("unexpected queries: %v", queries)
	}
	// Piped table keeps full RFC 3339 timestamps.
	out := ta.out.String()
	for _, want := range []string{"ws_1", "Acme US", "2025-01-01T08:30:00Z", "ws_2", "Acme EU", "2025-04-01T11:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("piped list missing %q:\n%s", want, out)
		}
	}
	// Terminal table shortens timestamps to dates / relative times.
	ta2 := newTestApp(t, srv)
	ta2.app.IO.SetStdoutTTY(true)
	cmd2 := findCmd(t, ta2.app, "workspaces", "list")
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("workspaces list (tty): %v", err)
	}
	ttyOut := ta2.out.String()
	if strings.Contains(ttyOut, "2025-01-01T08:30:00Z") || !strings.Contains(ttyOut, "2025-01-01") {
		t.Errorf("tty list should shorten timestamps:\n%s", ttyOut)
	}
}

func TestWorkspaces_Get(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/workspaces/ws_eu" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"object":    "workspace",
			"id":        "ws_eu",
			"name":      "Acme EU",
			"createdAt": "2025-03-01T10:00:00Z",
			"updatedAt": "2025-04-01T11:00:00Z",
			"extraKey":  "preserved",
		})
	})

	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "workspaces", "get")
	cmd.SetArgs([]string{"ws_eu"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workspaces get: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(ta.out.Bytes(), &got); err != nil {
		t.Fatalf("decode json: %v (%q)", err, ta.out.String())
	}
	if got["id"] != "ws_eu" || got["name"] != "Acme EU" || got["extraKey"] != "preserved" {
		t.Errorf("unexpected response: %v", got)
	}
}

func TestWorkspaces_CreateTrimsValidatesAndDoesNotRetry5xx(t *testing.T) {
	var attempts atomic.Int32
	var gotName string
	var fail atomic.Bool
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/workspaces" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		attempts.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"code":"INTERNAL_ERROR","message":"boom","requestId":"req_500","retryable":true}`)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		gotName = body.Name
		writeJSON(w, http.StatusOK, map[string]any{
			"object":    "workspace",
			"id":        "ws_new",
			"name":      body.Name,
			"createdAt": "2026-11-20T00:00:00Z",
			"updatedAt": "2026-11-20T00:00:00Z",
		})
	})

	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "workspaces", "create")
	cmd.SetArgs([]string{"--name", "  Acme EU  "})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workspaces create: %v", err)
	}
	if gotName != "Acme EU" {
		t.Errorf("sent name = %q, want trimmed 'Acme EU'", gotName)
	}

	// Blank or >255-char name is rejected before hitting the server.
	attempts.Store(0)
	for _, bad := range []string{"   ", strings.Repeat("a", 256)} {
		ta2 := newTestApp(t, srv)
		cmd2 := findCmd(t, ta2.app, "workspaces", "create")
		cmd2.SetArgs([]string{"--name", bad})
		cmd2.SilenceUsage, cmd2.SilenceErrors = true, true
		if err := cmd2.Execute(); err == nil {
			t.Errorf("name len=%d should have failed validation", len(bad))
		}
	}
	if attempts.Load() != 0 {
		t.Errorf("validation failures made %d server calls, want 0", attempts.Load())
	}

	// 5xx must not be retried so a transient failure cannot create duplicates.
	fail.Store(true)
	ta3 := newTestApp(t, srv)
	cmd3 := findCmd(t, ta3.app, "workspaces", "create")
	cmd3.SetArgs([]string{"--name", "No Dupe"})
	cmd3.SilenceUsage, cmd3.SilenceErrors = true, true
	if err := cmd3.Execute(); err == nil {
		t.Fatal("create against 500 should fail")
	}
	if n := attempts.Load(); n != 1 {
		t.Errorf("create made %d attempts on 500, want 1 (no retries)", n)
	}
}

func TestWorkspaces_SurfacesBlocked400Error(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"code":"INVALID_REQUEST","message":"Workspace management requires an organization-level API key.","requestId":"req_ws_400","retryable":false}`)
	})

	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "workspaces", "list")
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	apiErr, ok := extendx.AsAPIError(err)
	if !ok {
		t.Fatalf("err = %v, want *extendx.APIError", err)
	}
	if apiErr.StatusCode != 400 || !strings.Contains(apiErr.Message, "organization-level API key") {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
}
