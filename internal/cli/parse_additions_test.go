package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// parseServer answers parse runs with the given final status and file,
// accepts uploads, deletes and downloads, and counts what it saw.
type parseServer struct {
	*fakeServer
	deleted []string
}

func newParseServer(t *testing.T, finalStatus string, file map[string]any) *parseServer {
	t.Helper()
	ps := &parseServer{}
	pdf := []byte("%PDF-1.7 searchable")
	ps.fakeServer = newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files/upload":
			writeJSON(w, 200, map[string]any{"object": "file", "id": "file_up", "name": "scan.pdf", "type": "PDF"})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/files/"):
			id := strings.TrimPrefix(r.URL.Path, "/files/")
			ps.deleted = append(ps.deleted, id)
			writeJSON(w, 200, map[string]any{"id": id})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/"):
			writeJSON(w, 200, map[string]any{"object": "file", "id": "file_up", "name": "scan.pdf", "type": "PDF",
				"presignedUrl": ps.URL() + "/download/scan.pdf"})
		case r.Method == http.MethodGet && r.URL.Path == "/download/scan.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			w.Write(pdf)
		case r.Method == http.MethodPost && r.URL.Path == "/parse_runs":
			writeJSON(w, 200, map[string]any{"id": "pr_new", "status": "PROCESSING", "file": file})
		case r.Method == http.MethodGet && r.URL.Path == "/parse_runs/pr_new":
			writeJSON(w, 200, map[string]any{
				"id": "pr_new", "status": finalStatus, "file": file,
				"output": map[string]any{"chunks": []map[string]any{{"object": "chunk", "type": "page", "content": "# Scan"}}},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return ps
}

func (ps *parseServer) createBody(t *testing.T) map[string]any {
	t.Helper()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, r := range ps.requests {
		if r.Method == http.MethodPost && r.Path == "/parse_runs" {
			var body map[string]any
			if err := json.Unmarshal(r.Body, &body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			return body
		}
	}
	t.Fatal("no parse run was created")
	return nil
}

func localPDF(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var pdfFile = map[string]any{"object": "file", "id": "file_up", "name": "scan.pdf", "type": "PDF"}

func TestParseRunsCancel_HitsParseCancelEndpoint(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_abc", "status": "CANCELLED"})
	})
	ta := newTestApp(t, srv)
	if err := runTypedRunsCancel(context.Background(), ta.app, extendx.KindParse, "pr_abc", true); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	req := srv.lastRequest()
	if req.Method != http.MethodPost || req.Path != "/parse_runs/pr_abc/cancel" {
		t.Errorf("hit %s %s, want POST /parse_runs/pr_abc/cancel", req.Method, req.Path)
	}
	if !strings.Contains(ta.errOut.String(), "Cancelled pr_abc") {
		t.Errorf("stderr = %q, want the cancel confirmation", ta.errOut.String())
	}
}

func TestParse_CancelledRunExitsNonZero(t *testing.T) {
	ps := newParseServer(t, "CANCELLED", pdfFile)
	ta := newTestApp(t, ps.fakeServer)
	err := runParse(context.Background(), ta.app, parseParams{input: "file_up", target: "markdown", wait: true, timeout: 2 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "run pr_new was cancelled") {
		t.Fatalf("err = %v, want the cancelled-run error", err)
	}
}

func TestParse_ZeroRetentionSendsModeAndDeletesTheUpload(t *testing.T) {
	ps := newParseServer(t, "PROCESSED", pdfFile)
	ta := newTestApp(t, ps.fakeServer)
	err := runParse(context.Background(), ta.app, parseParams{
		input: localPDF(t), target: "markdown", zeroRetention: true, wait: true, timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("runParse: %v", err)
	}
	body := ps.createBody(t)
	if got, _ := body["dataRetention"].(map[string]any); got["mode"] != "zero" {
		t.Errorf("dataRetention = %v, want mode zero", body["dataRetention"])
	}
	if len(ps.deleted) != 1 || ps.deleted[0] != "file_up" {
		t.Errorf("deleted files = %v, want [file_up]", ps.deleted)
	}
	if !strings.Contains(ta.out.String(), "# Scan") {
		t.Errorf("stdout = %q, want the parsed markdown", ta.out.String())
	}
	if !strings.Contains(ta.errOut.String(), "Deleted file_up (uploaded for this run)") {
		t.Errorf("stderr = %q, want the deletion line", ta.errOut.String())
	}
}

func TestParse_ZeroRetentionDeletesTheUploadWhenTheRunFails(t *testing.T) {
	ps := newParseServer(t, "FAILED", pdfFile)
	ta := newTestApp(t, ps.fakeServer)
	err := runParse(context.Background(), ta.app, parseParams{
		input: localPDF(t), target: "markdown", zeroRetention: true, wait: true, timeout: 2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "run pr_new failed") {
		t.Fatalf("err = %v, want the failed-run error", err)
	}
	if len(ps.deleted) != 1 {
		t.Errorf("deleted files = %v, want the upload deleted", ps.deleted)
	}
}

func TestParse_ZeroRetentionKeepsFileIDInputs(t *testing.T) {
	ps := newParseServer(t, "PROCESSED", pdfFile)
	ta := newTestApp(t, ps.fakeServer)
	if err := runParse(context.Background(), ta.app, parseParams{
		input: "file_up", target: "markdown", zeroRetention: true, wait: true, timeout: 2 * time.Second,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	if len(ps.deleted) != 0 {
		t.Errorf("deleted files = %v, want none for a file ID input", ps.deleted)
	}
}

func TestParse_ZeroRetentionRefusesNoWaitForLocalFiles(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called, got %s %s", r.Method, r.URL.Path)
	})
	ta := newTestApp(t, srv)
	err := runParse(context.Background(), ta.app, parseParams{input: localPDF(t), target: "markdown", zeroRetention: true})
	if err == nil || !strings.Contains(err.Error(), "--wait=false can't be used with a local file and --zero-retention") {
		t.Fatalf("err = %v, want the local-file refusal", err)
	}
}

func TestParse_NewFlagsAreWired(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called, got %s %s", r.Method, r.URL.Path)
	})
	cases := []struct {
		args []string
		want string
	}{
		{[]string{localPDF(t), "--zero-retention", "--wait=false"}, "--wait=false can't be used with a local file and --zero-retention"},
		{[]string{"file_up", "--searchable-pdf", "out.pdf", "--wait=false"}, "--searchable-pdf needs the run to finish"},
		{[]string{"https://example.com/a.pdf", "--searchable-pdf", "out.pdf", "--zero-retention"}, "needs a local file or file ID"},
	}
	for _, tc := range cases {
		ta := newTestApp(t, srv)
		cmd := findCmd(t, ta.app, "parse")
		cmd.SetArgs(tc.args)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("extend parse %v: err = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestParse_SearchablePDFSetsTheOptionAndSavesThePDF(t *testing.T) {
	ps := newParseServer(t, "PROCESSED", pdfFile)
	ta := newTestApp(t, ps.fakeServer)
	out := filepath.Join(t.TempDir(), "searchable.pdf")
	if err := runParse(context.Background(), ta.app, parseParams{
		input: "file_up", target: "markdown", searchablePDF: out, wait: true, timeout: 2 * time.Second,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	body := ps.createBody(t)
	ao, _ := body["config"].(map[string]any)["advancedOptions"].(map[string]any)
	if ao["embedOcrInPdf"] != true {
		t.Errorf("advancedOptions = %v, want embedOcrInPdf true", ao)
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "%PDF-1.7 searchable" {
		t.Errorf("saved PDF = %q, %v; want the processed file's bytes", got, err)
	}
	if !strings.Contains(ta.out.String(), "# Scan") {
		t.Errorf("stdout = %q, want the parsed markdown too", ta.out.String())
	}
}

func TestParse_SearchablePDFToStdoutReplacesTheMarkdown(t *testing.T) {
	ps := newParseServer(t, "PROCESSED", pdfFile)
	ta := newTestApp(t, ps.fakeServer)
	if err := runParse(context.Background(), ta.app, parseParams{
		input: "file_up", target: "markdown", searchablePDF: "-", wait: true, timeout: 2 * time.Second,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	if got := ta.out.String(); got != "%PDF-1.7 searchable" {
		t.Errorf("stdout = %q, want only the PDF bytes", got)
	}
}

func TestParse_SearchablePDFRejectsNonPDFInputs(t *testing.T) {
	ps := newParseServer(t, "PROCESSED", map[string]any{"object": "file", "id": "file_up", "name": "photo.png", "type": "IMAGE"})
	ta := newTestApp(t, ps.fakeServer)
	err := runParse(context.Background(), ta.app, parseParams{
		input: "file_up", target: "markdown", searchablePDF: filepath.Join(t.TempDir(), "x.pdf"), wait: true, timeout: 2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "only works on PDFs; photo.png is type IMAGE") {
		t.Fatalf("err = %v, want the PDF-only error", err)
	}
}

func TestParse_SearchablePDFConflictsWithEmbedFalse(t *testing.T) {
	_, err := buildParseConfig(parseParams{searchablePDF: "out.pdf", advancedOptionsPath: `{"embedOcrInPdf":false}`})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("err = %v, want the conflict error", err)
	}
}

func TestParseRunsList_ShowsFileNames(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []map[string]any{
			{"object": "parse_run", "id": "pr_1", "status": "PROCESSED", "file": pdfFile, "createdAt": "2026-11-06T10:00:00Z"},
		}})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "table"
	cmd := findCmd(t, ta.app, "parse", "runs", "list")
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	got := ta.out.String()
	if !strings.Contains(got, "FILE") || !strings.Contains(got, "scan.pdf") {
		t.Errorf("table = %q, want a FILE column with scan.pdf", got)
	}
}
