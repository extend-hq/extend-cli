package cli

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func verifySignal(id, typ, level, summary string) map[string]any {
	return map[string]any{
		"id": id, "type": typ, "summary": summary, "evidence": []any{},
		"riskAssessment": map[string]any{"type": "LEVEL", "level": level},
	}
}

// verifiedRun is a processed run at the given risk level: one MEDIUM
// signal listed before one HIGH signal.
func verifiedRun(level string) map[string]any {
	return map[string]any{
		"id": "vr_x", "status": "PROCESSED", "source": "API",
		"file": map[string]any{"id": "file_stmt", "name": "statement.pdf"},
		"output": map[string]any{
			"riskLevel": level,
			"summary":   "A row was deleted from the fees table after signing.",
			"signals": []any{
				verifySignal("vs_1", "REVISION_AND_PRODUCTION_HISTORY", "MEDIUM", "The file was saved twice by different tools."),
				verifySignal("vs_2", "DIGITAL_SIGNATURE_INTEGRITY", "HIGH", "The document changed after a no-changes certification."),
			},
		},
	}
}

// verifyServer accepts uploads, creates run vr_x, and returns final on
// every poll.
func verifyServer(t *testing.T, final map[string]any) *fakeServer {
	t.Helper()
	return newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files/upload":
			writeJSON(w, 200, map[string]any{"id": "file_stmt", "name": "statement.pdf", "type": "PDF"})
		case r.Method == http.MethodPost && r.URL.Path == "/verify_runs":
			writeJSON(w, 200, map[string]any{"id": "vr_x", "status": "PROCESSING", "source": "API"})
		case r.Method == http.MethodGet && r.URL.Path == "/verify_runs/vr_x":
			writeJSON(w, 200, final)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestVerify_UploadsAndSendsConfig(t *testing.T) {
	srv := verifyServer(t, verifiedRun("HIGH"))
	ta := newTestApp(t, srv)
	paths := writePDFs(t, "statement.pdf")
	cmd := findCmd(t, ta.app, "verify")
	cmd.SetArgs([]string{paths[0], "--engine", "verify_standard", "--engine-version", "0.1.0",
		"--advanced-options", `{"requireParsing":false}`, "--metadata", "customer=acme", "--tag", "prod"})
	// A finished run exits 0 whatever its risk.
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	body := createBody(t, srv, "/verify_runs")
	want := map[string]any{
		"file": map[string]any{"id": "file_stmt"},
		"config": map[string]any{
			"engine":          "verify_standard",
			"engineVersion":   "0.1.0",
			"advancedOptions": map[string]any{"requireParsing": false},
		},
		"metadata": map[string]any{"customer": "acme", "extend:usage_tags": []any{"prod"}},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v, want %v", body, want)
	}
	// Piped output is the run's JSON.
	if out := ta.out.String(); !strings.Contains(out, `"riskLevel":"HIGH"`) {
		t.Errorf("stdout = %q, want the run's JSON", out)
	}
}

func TestVerify_TerminalShowsRiskThenSignalsByLevel(t *testing.T) {
	srv := verifyServer(t, verifiedRun("HIGH"))
	ta := newTestApp(t, srv)
	ta.app.IO.SetStdoutTTY(true)
	cmd := findCmd(t, ta.app, "verify")
	cmd.SetArgs([]string{"file_stmt"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	got := ta.out.String()
	for _, want := range []string{
		"! HIGH risk: statement.pdf (2 signals)",
		"Summary: A row was deleted from the fees table after signing.",
		"Details: extend verify runs get vr_x -o json",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	high := strings.Index(got, "DIGITAL_SIGNATURE_INTEGRITY")
	medium := strings.Index(got, "REVISION_AND_PRODUCTION_HISTORY")
	if high < 0 || medium < 0 || high > medium {
		t.Errorf("want the HIGH signal listed before the MEDIUM one:\n%s", got)
	}
}

func TestVerify_FailOn(t *testing.T) {
	cases := []struct {
		risk, failOn, wantErr string
	}{
		{"HIGH", "high", "found HIGH risk, at or above --fail-on high"},
		{"MEDIUM", "medium", "found MEDIUM risk, at or above --fail-on medium"},
		{"HIGH", "MEDIUM", "found HIGH risk, at or above --fail-on medium"},
		{"MEDIUM", "high", ""},
		{"LOW", "medium", ""},
	}
	for _, tc := range cases {
		srv := verifyServer(t, verifiedRun(tc.risk))
		ta := newTestApp(t, srv)
		cmd := findCmd(t, ta.app, "verify")
		cmd.SetArgs([]string{"file_stmt", "--fail-on", tc.failOn})
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		err := cmd.Execute()
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s risk with --fail-on %s: err = %v, want nil", tc.risk, tc.failOn, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s risk with --fail-on %s: err = %v, want %q", tc.risk, tc.failOn, err, tc.wantErr)
		}
	}
}

func TestVerify_FailedRunReportsTheReason(t *testing.T) {
	srv := verifyServer(t, map[string]any{
		"id": "vr_x", "status": "FAILED", "source": "API",
		"failureReason": "FILE_TYPE_NOT_SUPPORTED", "failureMessage": "The file is not a PDF, JPEG or PNG.",
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "verify")
	cmd.SetArgs([]string{"file_stmt", "--fail-on", "high"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "run vr_x failed: FILE_TYPE_NOT_SUPPORTED") {
		t.Fatalf("err = %v, want the failure reason", err)
	}
}

func TestVerify_ArgumentRules(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called, got %s %s", r.Method, r.URL.Path)
	})
	cases := []struct {
		args []string
		want string
	}{
		{[]string{}, "verify takes one input"},
		{[]string{"a.pdf", "b.pdf"}, "verify takes one input"},
		{[]string{"file_stmt", "--fail-on", "low"}, "--fail-on must be medium or high"},
		{[]string{"file_stmt", "--fail-on", "high", "--wait=false"}, "can't be used with --wait=false"},
		{[]string{"file_stmt", "--advanced-options", "{"}, "--advanced-options"},
	}
	for _, tc := range cases {
		ta := newTestApp(t, srv)
		cmd := findCmd(t, ta.app, "verify")
		cmd.SetArgs(tc.args)
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("extend verify %v: err = %v, want %q", tc.args, err, tc.want)
		}
	}
}

// Until Extend enables Verify, every endpoint returns a bare 404; a
// missing run's 404 names the run and passes through unchanged.
func TestVerify_ExplainsVerifyNotEnabled(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/verify_runs/vr_missing" {
			writeAPIError(w, http.StatusNotFound, "NOT_FOUND", "Verify run not found.")
			return
		}
		writeAPIError(w, http.StatusNotFound, "NOT_FOUND", "Not found.")
	})
	for _, tc := range []struct {
		path, args []string
	}{
		{[]string{"verify"}, []string{"file_stmt"}},
		{[]string{"verify", "runs", "get"}, []string{"vr_x"}},
		{[]string{"verify", "runs", "list"}, []string{}},
		{[]string{"verify", "runs", "cancel"}, []string{"vr_x", "--yes"}},
		{[]string{"verify", "runs", "delete"}, []string{"vr_x", "--yes"}},
	} {
		ta := newTestApp(t, srv)
		cmd := findCmd(t, ta.app, tc.path...)
		cmd.SetArgs(tc.args)
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "Verify isn't enabled for this organization") {
			t.Errorf("extend %s: err = %v, want the not-enabled explanation", strings.Join(tc.path, " "), err)
		}
	}

	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "verify", "runs", "get")
	cmd.SetArgs([]string{"vr_missing"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	if err == nil || strings.Contains(err.Error(), "isn't enabled") || !strings.Contains(err.Error(), "Verify run not found.") {
		t.Errorf("missing run: err = %v, want the API's not-found error", err)
	}
}

func TestVerifyRunsList_FiltersAndRiskColumn(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []any{
			map[string]any{"id": "vr_a", "status": "PROCESSED", "riskLevel": "HIGH", "source": "API",
				"file": map[string]any{"id": "file_a", "name": "statement.pdf"}},
		}})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "verify", "runs", "list")
	cmd.SetArgs([]string{"--risk-level", "high", "--status", "PROCESSED", "--source", "API", "--source-id", "apireq_1", "--file-name", "statement"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify runs list: %v", err)
	}
	q, _ := url.ParseQuery(srv.lastRequest().Query)
	for k, want := range map[string]string{"riskLevel": "HIGH", "status": "PROCESSED", "source": "API", "sourceId": "apireq_1", "fileNameContains": "statement"} {
		if got := q.Get(k); got != want {
			t.Errorf("query %s = %q, want %q", k, got, want)
		}
	}
	lines := strings.Split(strings.TrimSpace(ta.out.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(strings.Join(strings.Fields(lines[0]), " "), "ID STATUS RISK FILE CREATED") ||
		!strings.Contains(lines[1], "HIGH") || !strings.Contains(lines[1], "statement.pdf") {
		t.Errorf("table:\n%s", ta.out.String())
	}
}

func TestVerifyRunsWatch_FailOn(t *testing.T) {
	cases := []struct {
		run     map[string]any
		wantErr string
	}{
		{verifiedRun("HIGH"), "found HIGH risk, at or above --fail-on medium"},
		{verifiedRun("LOW"), ""},
		{map[string]any{"id": "vr_x", "status": "CANCELLED", "source": "API"}, "has no result to check against --fail-on"},
	}
	for _, tc := range cases {
		srv := verifyServer(t, tc.run)
		ta := newTestApp(t, srv)
		cmd := findCmd(t, ta.app, "verify", "runs", "watch")
		cmd.SetArgs([]string{"vr_x", "--fail-on", "medium"})
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		err := cmd.Execute()
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%v: err = %v, want nil", tc.run["status"], err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%v: err = %v, want %q", tc.run["status"], err, tc.wantErr)
		}
	}
}

// Cancelling a finished verify run is a no-op at the API, not an error.
func TestVerifyRunsCancel_FinishedRun(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, verifiedRun("LOW"))
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "verify", "runs", "cancel")
	cmd.SetArgs([]string{"vr_x", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := srv.lastRequest(); got.Method != http.MethodPost || got.Path != "/verify_runs/vr_x/cancel" {
		t.Errorf("request = %s %s", got.Method, got.Path)
	}
	if !strings.Contains(ta.errOut.String(), "vr_x had already finished (PROCESSED); nothing to cancel") {
		t.Errorf("stderr = %q", ta.errOut.String())
	}
}
