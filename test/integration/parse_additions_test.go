package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestParse_ZeroRetention parses a local file with --zero-retention and
// checks that the API accepted the mode, that the one-time result was
// printed, and that the CLI deleted its own upload.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestParse_ZeroRetention(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	res := runExtend(t, env, "parse", "testdata/sample.txt", "--zero-retention", "-o", "json")
	skipIfUnavailable(t, res, "Zero data retention is not available", "zero data retention")
	res.requireOK(t, "parse", "--zero-retention")

	var run struct {
		ID            string `json:"id"`
		Status        string `json:"status"`
		DataRetention struct {
			Mode string `json:"mode"`
		} `json:"dataRetention"`
		Output struct {
			Chunks []any `json:"chunks"`
		} `json:"output"`
	}
	res.decodeJSON(t, &run)
	rememberCleanup(t, env, "delete parse run", "parse", "runs", "delete", run.ID, "-y")

	if run.Status != "PROCESSED" {
		t.Errorf("status = %q, want PROCESSED: %s", run.Status, res.Stdout)
	}
	if run.DataRetention.Mode != "zero" {
		t.Errorf("dataRetention.mode = %q, want zero: %s", run.DataRetention.Mode, res.Stdout)
	}
	if len(run.Output.Chunks) == 0 {
		t.Errorf("output.chunks is empty; the result is only returned once: %s", res.Stdout)
	}
	if !regexp.MustCompile(`Deleted file_\S+ \(uploaded for this run\)`).Match(res.Stderr) {
		t.Errorf("stderr doesn't report deleting the upload: %s", res.Stderr)
	}
}

// TestParse_SearchablePDF asks for a searchable copy of a PDF and checks
// that the CLI downloaded one and printed where it went, not the document.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestParse_SearchablePDF(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	in := writeTempFile(t, "invoice.pdf", invoicePDF())
	out := filepath.Join(t.TempDir(), "invoice-searchable.pdf")
	res := runExtend(t, env, "parse", in, "--searchable-pdf", out)
	res.requireOK(t, "parse", "--searchable-pdf")

	if id := regexp.MustCompile(`pr_[A-Za-z0-9_-]+`).Find(res.Stdout); id != nil {
		rememberCleanup(t, env, "delete parse run", "parse", "runs", "delete", string(id), "-y")
	}
	if !strings.Contains(string(res.Stdout), "searchable PDF saved to "+out) {
		t.Errorf("stdout = %q, want one line saying where the PDF went", res.Stdout)
	}
	pdf, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the searchable PDF: %v", err)
	}
	if !strings.HasPrefix(string(pdf), "%PDF") {
		t.Errorf("%s doesn't start with %%PDF (%d bytes)", out, len(pdf))
	}
}

// TestParseRuns_Cancel cancels a parse run right after submitting it. The
// run may finish first, in which case the API refuses with a message naming
// its status; a cancel that succeeds must leave the run CANCELLED.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestParseRuns_Cancel(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	submitRes := runExtend(t, env, "parse", "testdata/sample.txt", "--wait=false", "-o", "json")
	submitRes.requireOK(t, "parse", "--wait=false")
	var submitted struct {
		ID string `json:"id"`
	}
	submitRes.decodeJSON(t, &submitted)
	rememberCleanup(t, env, "delete parse run", "parse", "runs", "delete", submitted.ID, "-y")

	cancelRes := runExtend(t, env, "parse", "runs", "cancel", submitted.ID, "-y")
	if cancelRes.ExitCode != 0 && !strings.Contains(string(cancelRes.Stderr), "Cannot cancel parse run") {
		t.Fatalf("cancel exited %d with an unexpected error: %s", cancelRes.ExitCode, cancelRes.Stderr)
	}

	watchRes := runExtend(t, env, "parse", "runs", "watch", submitted.ID, "--timeout", "2m", "-o", "json")
	watchRes.requireOK(t, "parse", "runs", "watch", submitted.ID)
	var final struct {
		Status string `json:"status"`
	}
	watchRes.decodeJSON(t, &final)
	if cancelRes.ExitCode == 0 && final.Status != "CANCELLED" {
		t.Errorf("status = %q after a successful cancel, want CANCELLED", final.Status)
	}
	if cancelRes.ExitCode != 0 && final.Status != "PROCESSED" && final.Status != "FAILED" {
		t.Errorf("status = %q after a refused cancel, want a finished run", final.Status)
	}
}
