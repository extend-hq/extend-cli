package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	extend "github.com/extend-hq/extend-go-sdk"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

func TestTypedRunsGet_RoutesToKindEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		kind     extendx.RunKind
		runID    string
		wantPath string
	}{
		{"extract", extendx.KindExtract, "exr_abc", "/extract_runs/exr_abc"},
		{"parse", extendx.KindParse, "pr_abc", "/parse_runs/pr_abc"},
		{"classify", extendx.KindClassify, "clr_abc", "/classify_runs/clr_abc"},
		{"split", extendx.KindSplit, "splr_abc", "/split_runs/splr_abc"},
		{"workflow", extendx.KindWorkflow, "workflow_run_abc", "/workflow_runs/workflow_run_abc"},
		{"edit", extendx.KindEdit, "edr_abc", "/edit_runs/edr_abc"},
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

			if err := runTypedRunsGet(context.Background(), ta.app, tc.kind, tc.runID, ""); err != nil {
				t.Fatalf("runTypedRunsGet: %v", err)
			}
			if got := srv.lastRequest().Path; got != tc.wantPath {
				t.Errorf("hit %q, want %q", got, tc.wantPath)
			}
		})
	}
}

func TestTypedRunsGet_UnknownPrefixErrors(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for unknown prefix")
	})
	ta := newTestApp(t, srv)
	err := runTypedRunsGet(context.Background(), ta.app, extendx.KindExtract, "nope_xxx", "")
	if err == nil || !strings.Contains(err.Error(), "not a recognized extract run ID") {
		t.Errorf("expected unrecognized-ID error, got %v", err)
	}
}

// TestTypedRunsGet_MismatchedKindRedirects is the core no-prefix-
// inference contract: a valid run ID passed to the wrong typed group
// must fail fast (no API call) and name the owning command.
func TestTypedRunsGet_MismatchedKindRedirects(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for a mismatched ID")
	})
	ta := newTestApp(t, srv)
	err := runTypedRunsGet(context.Background(), ta.app, extendx.KindParse, "exr_abc", "")
	if err == nil || !strings.Contains(err.Error(), "extend extract runs get exr_abc") {
		t.Errorf("expected redirect to 'extend extract runs get', got %v", err)
	}
}

func TestTypedRunsGet_ParseResponseTypeQuery(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_abc", "status": "PROCESSED"})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "json"
	if err := runTypedRunsGet(context.Background(), ta.app, extendx.KindParse, "pr_abc", "url"); err != nil {
		t.Fatalf("runTypedRunsGet: %v", err)
	}
	req := srv.lastRequest()
	if req.Path != "/parse_runs/pr_abc" || req.Query != "responseType=url" {
		t.Errorf("request = %s?%s, want /parse_runs/pr_abc?responseType=url", req.Path, req.Query)
	}
}

func TestTypedRunsGet_InvalidResponseTypeRejected(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	})
	ta := newTestApp(t, srv)
	err := runTypedRunsGet(context.Background(), ta.app, extendx.KindParse, "pr_abc", "yaml")
	if err == nil || !strings.Contains(err.Error(), "json|url") {
		t.Fatalf("expected response-type validation error, got %v", err)
	}
}

func TestTypedRunsCancel_MismatchedKindRedirects(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for a mismatched ID")
	})
	ta := newTestApp(t, srv)
	// A workflow-run ID handed to extract's cancel must redirect to
	// the workflows group.
	err := runTypedRunsCancel(context.Background(), ta.app, extendx.KindExtract, "workflow_run_abc", true)
	if err == nil || !strings.Contains(err.Error(), "extend workflows runs cancel") {
		t.Errorf("expected redirect naming the workflows runs group, got %v", err)
	}
	// Parse runs have no cancel command at all; a parse ID handed to
	// another kind's cancel must not suggest a nonexistent command.
	err = runTypedRunsCancel(context.Background(), ta.app, extendx.KindExtract, "pr_abc", true)
	if err == nil || !strings.Contains(err.Error(), "parse runs do not support cancel") {
		t.Errorf("expected parse-cannot-cancel error, got %v", err)
	}
}

func TestTypedRunsDelete_HitsKindEndpoint(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "clr_abc"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "classify", "runs", "delete")
	cmd.SetArgs([]string{"clr_abc", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
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
	cli, err := ta.app.NewClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	rows, _, err := collectListRows(context.Background(), cli, extendx.KindExtract, runsListParams{
		limit:   5,
		all:     true,
		sortDir: "desc",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows across pages, got %d", len(rows))
	}
	if page != 2 {
		t.Errorf("expected 2 pages fetched, got %d", page)
	}
}

func TestRenderWorkflow_MultiStepTable(t *testing.T) {
	ios, _, out, _ := newTTYStreams(t)
	app := &App{IO: ios}
	// The SDK models step runs as a discriminated union (*extend.StepRun)
	// keyed on StepType. Each variant has its own typed Step substruct.
	// We build the union members by hand here to mirror what the SDK
	// would unmarshal off the wire.
	parseType := "PARSE"
	extractType := "EXTRACT"
	validateType := "RULE_VALIDATION"
	run := &extend.WorkflowRun{
		ID:           "workflow_run_x",
		Status:       extend.WorkflowRunStatusProcessed,
		DashboardURL: "http://dash",
		StepRuns: []*extend.StepRun{
			{
				StepType: "PARSE",
				Parse: &extend.ParseStepRun{
					ID:     "sr_1",
					Status: extend.StepRunBaseStatusProcessed,
					Step:   &extend.ParseStepRunStep{Name: "parse1", Type: &parseType},
				},
			},
			{
				StepType: "EXTRACT",
				Extract: &extend.ExtractStepRun{
					ID:     "sr_2",
					Status: extend.StepRunBaseStatusProcessed,
					Step:   &extend.ExtractStepRunStep{Name: "extract2", Type: &extractType},
				},
			},
			{
				StepType: "RULE_VALIDATION",
				RuleValidation: &extend.RuleValidationStepRun{
					ID:     "sr_3",
					Status: extend.StepRunBaseStatusFailed,
					Step:   &extend.RuleValidationStepRunStep{Name: "validate3", Type: &validateType},
				},
			},
		},
	}
	if err := renderWorkflowResult(app, run); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := out.String()
	for _, want := range []string{"parse1", "extract2", "validate3", "PARSE", "EXTRACT", "RULE_VALIDATION", "FAILED"} {
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
	cli, err := ta.app.NewClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, _, err := collectListRows(context.Background(), cli, extendx.KindExtract, runsListParams{
		status:  "PROCESSED",
		batchID: "bpr_xyz",
		limit:   5,
		sortDir: "desc",
	}); err != nil {
		t.Fatalf("collectListRows: %v", err)
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
// through to the wire as the right query param. Belt-and-braces against
// the per-kind list request building in listExtractPage et al.
func TestRunsList_AllFiltersOnExtract(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	ta := newTestApp(t, srv)
	cli, err := ta.app.NewClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, _, err := collectListRows(context.Background(), cli, extendx.KindExtract, runsListParams{
		using:    "ex_abc",
		source:   "WORKFLOW_RUN",
		sourceID: "workflow_run_x",
		fileName: "invoice",
		sortBy:   "updatedAt",
		sortDir:  "asc",
		limit:    20,
	}); err != nil {
		t.Fatalf("collectListRows: %v", err)
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

// TestRunsList_ParseDropsSort exercises the parse-runs quirks:
// the server doesn't accept sortBy/sortDir/processorId, and the SDK's
// typed ParseRunsListRequest omits those fields entirely so they can't
// leak. Regression against the pre-SDK silent dropping behavior.
func TestRunsList_ParseDropsSort(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	ta := newTestApp(t, srv)
	cli, err := ta.app.NewClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, _, err := collectListRows(context.Background(), cli, extendx.KindParse, runsListParams{
		using:   "ex_abc", // ignored — parse has no processor
		sortBy:  "createdAt",
		sortDir: "asc",
		limit:   3,
	}); err != nil {
		t.Fatalf("collectListRows: %v", err)
	}
	q := srv.lastRequest().Query
	if !strings.Contains(q, "maxPageSize=3") {
		t.Errorf("parse runs missing maxPageSize=3 (got %s)", q)
	}
	for _, leaked := range []string{"sortBy", "sortDir", "extractorId"} {
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
	err := runTypedRunsWatch(context.Background(), ta.app, extractRunsSpec(), "exr_fail", 5*time.Second, true)
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
	if err := runTypedRunsWatch(context.Background(), ta.app, extractRunsSpec(), "exr_fail", 5*time.Second, false); err != nil {
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
	err := runTypedRunsWatch(context.Background(), ta.app, extractRunsSpec(), "exr_slow", 200*time.Millisecond, false)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("watch should have respected --timeout; elapsed %v", elapsed)
	}
	// The error message must guide the user toward raising --timeout
	// rather than surfacing a bare "context deadline exceeded". Agents
	// in the wild were spinning on the same failing watch invocation
	// because the prior message gave them no recovery action.
	msg := err.Error()
	for _, want := range []string{"exr_slow", "--timeout", "200ms"} {
		if !strings.Contains(msg, want) {
			t.Errorf("watch timeout error missing %q: %s", want, msg)
		}
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
		// Exercise both entry points (Time and ISO string) per case
		// so they stay in sync if relTime's bucketing logic changes.
		past := now.Add(-tc.ago)
		if got := relTime(past); got != tc.want {
			t.Errorf("relTime(%v ago) = %q, want %q", tc.ago, got, tc.want)
		}
		if got := relTimeFromISO(past.Format(time.RFC3339Nano)); got != tc.want {
			t.Errorf("relTimeFromISO(%v ago) = %q, want %q", tc.ago, got, tc.want)
		}
	}
	if got := relTime(time.Time{}); got != "" {
		t.Errorf("relTime(zero) = %q, want ''", got)
	}
	if got := relTimeFromISO(""); got != "" {
		t.Errorf("relTimeFromISO('') = %q, want ''", got)
	}
	if got := relTimeFromISO("not-a-date"); got != "not-a-date" {
		t.Errorf("relTimeFromISO should pass through unparseable input, got %q", got)
	}
}

// TestRunsUpdate_NameFlag verifies the new --name flag reaches the workflow
// run update body (previously settable only via raw --from-file).
func TestRunsUpdate_NameFlag(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workflow_runs/workflow_run_abc" || r.Method != http.MethodPost {
			t.Fatalf("hit %s %s, want POST /workflow_runs/workflow_run_abc", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "workflow_run_abc", "object": "workflow_run", "status": "PROCESSED"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "workflows", "runs", "update")
	cmd.SetArgs([]string{"workflow_run_abc", "--name", "Q3 reprocess"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"name":"Q3 reprocess"`) {
		t.Errorf("update body missing name; got %s", body)
	}
}
