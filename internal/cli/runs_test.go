package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/extend-hq/extend-cli/internal/client"
)

func TestRunsGet_DispatchesByPrefix(t *testing.T) {
	tests := []struct {
		name     string
		runID    string
		wantPath string
	}{
		{"extract", "exr_abc", "/extract_runs/exr_abc"},
		{"parse", "pr_abc", "/parse_runs/pr_abc"},
		{"classify", "clr_abc", "/classify_runs/clr_abc"},
		{"split", "splr_abc", "/split_runs/splr_abc"},
		{"workflow", "workflow_run_abc", "/workflow_runs/workflow_run_abc"},
		{"edit", "edr_abc", "/edit_runs/edr_abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{
					"id":     tc.runID,
					"status": "PROCESSED",
					"output": map[string]any{},
				})
			})
			ta := newTestApp(t, srv)
			ta.app.Format = "json"

			if err := runRunsGet(context.Background(), ta.app, tc.runID, ""); err != nil {
				t.Fatalf("runRunsGet: %v", err)
			}
			if got := srv.lastRequest().Path; got != tc.wantPath {
				t.Errorf("hit %q, want %q", got, tc.wantPath)
			}
		})
	}
}

func TestRunsGet_UnknownPrefixErrors(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for unknown prefix")
	})
	ta := newTestApp(t, srv)
	err := runRunsGet(context.Background(), ta.app, "nope_xxx", "")
	if err == nil || !strings.Contains(err.Error(), "cannot determine run type") {
		t.Errorf("expected 'cannot determine run type' error, got %v", err)
	}
}

func TestRunsGet_ParseResponseTypeQuery(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_abc", "status": "PROCESSED"})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "json"
	if err := runRunsGet(context.Background(), ta.app, "pr_abc", "url"); err != nil {
		t.Fatalf("runRunsGet: %v", err)
	}
	req := srv.lastRequest()
	if req.Path != "/parse_runs/pr_abc" || req.Query != "responseType=url" {
		t.Errorf("request = %s?%s, want /parse_runs/pr_abc?responseType=url", req.Path, req.Query)
	}
}

func TestRunsGet_ResponseTypeRejectedForNonParse(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	})
	ta := newTestApp(t, srv)
	err := runRunsGet(context.Background(), ta.app, "exr_abc", "url")
	if err == nil || !strings.Contains(err.Error(), "only supported for parse runs") {
		t.Fatalf("expected parse-only response-type error, got %v", err)
	}
}

func TestRunsCancel_RejectsParseRuns(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when refusing parse cancel")
	})
	ta := newTestApp(t, srv)
	err := runRunsCancel(context.Background(), ta.app, "pr_abc", true)
	if err == nil || !strings.Contains(err.Error(), "parse runs cannot be cancelled") {
		t.Errorf("expected parse-rejection error, got %v", err)
	}
}

func TestRunsDelete_DispatchesByPrefix(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	ta := newTestApp(t, srv)
	if err := runRunsDelete(context.Background(), ta.app, "clr_abc", true); err != nil {
		t.Fatalf("runRunsDelete: %v", err)
	}
	req := srv.lastRequest()
	if req.Method != http.MethodDelete || req.Path != "/classify_runs/clr_abc" {
		t.Errorf("hit %s %s, want DELETE /classify_runs/clr_abc", req.Method, req.Path)
	}
}

func TestRunsList_AllAutoPaginates(t *testing.T) {
	page := 0
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		page++
		switch page {
		case 1:
			writeJSON(w, 200, map[string]any{
				"object":        "list",
				"data":          []map[string]any{{"id": "exr_1", "status": "PROCESSED"}, {"id": "exr_2", "status": "PROCESSED"}},
				"nextPageToken": "tok2",
			})
		case 2:
			writeJSON(w, 200, map[string]any{
				"object":        "list",
				"data":          []map[string]any{{"id": "exr_3", "status": "PROCESSED"}},
				"nextPageToken": "",
			})
		default:
			t.Fatal("unexpected extra page")
		}
	})
	ta := newTestApp(t, srv)
	if err := runRunsList(context.Background(), ta.app, runsListParams{
		runType: "extract",
		limit:   5,
		all:     true,
		sortDir: "desc",
	}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if page != 2 {
		t.Errorf("expected 2 pages fetched, got %d", page)
	}
}

func TestRenderWorkflow_MultiStepTable(t *testing.T) {
	ios, _, out, _ := newTTYStreams(t)
	app := &App{IO: ios}
	run := &client.WorkflowRun{
		ID:           "workflow_run_x",
		Status:       client.StatusProcessed,
		DashboardURL: "http://dash",
		StepRuns: []client.WorkflowStepRun{
			{ID: "sr_1", Status: client.StatusProcessed, Step: &client.WorkflowStep{Name: "parse1", Type: "PARSE"}},
			{ID: "sr_2", Status: client.StatusProcessed, Step: &client.WorkflowStep{Name: "extract2", Type: "EXTRACT"}},
			{ID: "sr_3", Status: client.StatusFailed, Step: &client.WorkflowStep{Name: "validate3", Type: "VALIDATE"}},
		},
	}
	if err := renderWorkflowResult(app, run); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := out.String()
	for _, want := range []string{"parse1", "extract2", "validate3", "PARSE", "EXTRACT", "VALIDATE", "FAILED"} {
		if !strings.Contains(got, want) {
			t.Errorf("multi-step output missing %q:\n%s", want, got)
		}
	}
}

func TestRunsList_TypeRoutesToCorrectEndpoint(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	ta := newTestApp(t, srv)
	if err := runRunsList(context.Background(), ta.app, runsListParams{
		runType: "extract",
		status:  "PROCESSED",
		batchID: "bpr_xyz",
		limit:   5,
		sortDir: "desc",
	}); err != nil {
		t.Fatalf("runRunsList: %v", err)
	}
	req := srv.lastRequest()
	if req.Path != "/extract_runs" {
		t.Errorf("path = %q, want /extract_runs", req.Path)
	}
	q := req.Query
	if !strings.Contains(q, "status=PROCESSED") || !strings.Contains(q, "batchId=bpr_xyz") || !strings.Contains(q, "maxPageSize=5") {
		t.Errorf("query missing filters: %s", q)
	}
}

// TestRunsList_AllFiltersOnExtract asserts every new filter flag flows
// through to the wire as the right query param. Belt-and-braces against the
// path-aware ListRunsOptions.query() — extractor uses extractorId, includes
// source/sourceId, fileNameContains, sortBy/sortDir.
func TestRunsList_AllFiltersOnExtract(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	ta := newTestApp(t, srv)
	if err := runRunsList(context.Background(), ta.app, runsListParams{
		runType:  "extract",
		using:    "ex_abc",
		source:   "WORKFLOW_RUN",
		sourceID: "workflow_run_x",
		fileName: "invoice",
		sortBy:   "updatedAt",
		sortDir:  "asc",
		limit:    20,
	}); err != nil {
		t.Fatalf("runRunsList: %v", err)
	}
	q := srv.lastRequest().Query
	for _, expected := range []string{
		"extractorId=ex_abc",
		"source=WORKFLOW_RUN",
		"sourceId=workflow_run_x",
		"fileNameContains=invoice",
		"sortBy=updatedAt",
		"sortDir=asc",
		"maxPageSize=20",
	} {
		if !strings.Contains(q, expected) {
			t.Errorf("query missing %q (full: %s)", expected, q)
		}
	}
}

// TestRunsList_ParseUsesLimitAndDropsSort exercises the parse-runs quirks:
// the wire param is `limit` (not `maxPageSize`), and the server doesn't
// accept sortBy/sortDir/processorId. Regression against the previously-silent
// bug where --limit was ignored on parse runs.
func TestRunsList_ParseUsesLimitAndDropsSort(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	ta := newTestApp(t, srv)
	if err := runRunsList(context.Background(), ta.app, runsListParams{
		runType: "parse",
		using:   "ex_abc", // ignored — parse has no processor
		sortBy:  "createdAt",
		sortDir: "asc",
		limit:   3,
	}); err != nil {
		t.Fatalf("runRunsList: %v", err)
	}
	q := srv.lastRequest().Query
	if !strings.Contains(q, "limit=3") {
		t.Errorf("parse runs must use ?limit= (got %s)", q)
	}
	for _, leaked := range []string{"maxPageSize", "sortBy", "sortDir", "extractorId"} {
		if strings.Contains(q, leaked+"=") {
			t.Errorf("parse runs leaked unsupported param %q in query: %s", leaked, q)
		}
	}
}

func TestRunsWatch_ExitStatusOnFailedRun(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "exr_fail", "status": "FAILED"})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "json"
	err := runRunsWatch(context.Background(), ta.app, "exr_fail", 5*time.Second, true)
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Errorf("expected --exit-status to surface FAILED as error, got %v", err)
	}
}

func TestRunsWatch_ExitStatusFalseHidesFailure(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "exr_fail", "status": "FAILED"})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "json"
	if err := runRunsWatch(context.Background(), ta.app, "exr_fail", 5*time.Second, false); err != nil {
		t.Errorf("without --exit-status, FAILED should not error; got %v", err)
	}
}

func TestRunsWatch_TimeoutCancelsPolling(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "exr_slow", "status": "PROCESSING"})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "json"
	start := time.Now()
	err := runRunsWatch(context.Background(), ta.app, "exr_slow", 200*time.Millisecond, false)
	elapsed := time.Since(start)
	if err == nil {
		t.Error("expected timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("watch should have respected --timeout; elapsed %v", elapsed)
	}
}

func TestRelTime_Buckets(t *testing.T) {
	now := time.Now()
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{2 * time.Hour, "2h ago"},
		{3 * 24 * time.Hour, "3d ago"},
	}
	for _, tc := range cases {
		ts := now.Add(-tc.ago).Format(time.RFC3339Nano)
		if got := relTime(ts); got != tc.want {
			t.Errorf("relTime(%v ago) = %q, want %q", tc.ago, got, tc.want)
		}
	}
	if got := relTime(""); got != "" {
		t.Errorf("relTime('') = %q, want ''", got)
	}
	if got := relTime("not-a-date"); got != "not-a-date" {
		t.Errorf("relTime should pass through unparseable input, got %q", got)
	}
}
