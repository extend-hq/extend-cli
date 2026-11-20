package integration

import (
	"strings"
	"testing"
)

// verifyUnavailable is the CLI's message when Extend hasn't enabled Verify
// for the API key's organization (the endpoints return 404).
const verifyUnavailable = "Verify isn't enabled"

func TestVerifyRuns_List(t *testing.T) {
	env := requireEnv(t)

	res := runExtend(t, env, "verify", "runs", "list", "--limit", "5", "-o", "json")
	skipIfUnavailable(t, res, verifyUnavailable, "Verify")
	res.requireOK(t, "verify", "runs", "list")
	for i, run := range listItems(t, res) {
		if id, _ := run["id"].(string); !strings.HasPrefix(id, "vr_") {
			t.Errorf("item %d id = %q, want a vr_ prefix", i, id)
		}
	}
}

// TestVerify_Lifecycle verifies a PDF, reads the run back, and checks that
// --fail-on agrees with the reported risk level.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestVerify_Lifecycle(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	pdf := writeTempFile(t, "invoice.pdf", invoicePDF())
	res := runExtend(t, env, "verify", pdf, "--timeout", "5m", "-o", "json")
	skipIfUnavailable(t, res, verifyUnavailable, "Verify")
	res.requireOK(t, "verify")

	var run struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Output struct {
			RiskLevel string `json:"riskLevel"`
			Signals   []any  `json:"signals"`
		} `json:"output"`
	}
	res.decodeJSON(t, &run)
	if !strings.HasPrefix(run.ID, "vr_") {
		t.Fatalf("expected vr_ prefix on run id, got %q", run.ID)
	}
	rememberCleanup(t, env, "delete verify run", "verify", "runs", "delete", run.ID, "-y")

	if run.Status != "PROCESSED" {
		t.Fatalf("status = %q, want PROCESSED: %s", run.Status, res.Stdout)
	}
	risk := run.Output.RiskLevel
	if risk != "LOW" && risk != "MEDIUM" && risk != "HIGH" {
		t.Fatalf("output.riskLevel = %q, want LOW, MEDIUM or HIGH: %s", risk, res.Stdout)
	}

	// The summary view is what a person or agent reads by default.
	getRes := runExtend(t, env, "verify", "runs", "get", run.ID)
	getRes.requireOK(t, "verify", "runs", "get", run.ID)
	if !strings.Contains(string(getRes.Stdout), risk) {
		t.Errorf("verify runs get doesn't show the risk level %s:\n%s", risk, getRes.Stdout)
	}

	// Watching a finished run costs nothing; --fail-on medium must exit
	// non-zero exactly when the risk is MEDIUM or HIGH.
	gateRes := runExtend(t, env, "verify", "runs", "watch", run.ID, "--fail-on", "medium")
	if wantFail := risk != "LOW"; (gateRes.ExitCode != 0) != wantFail {
		t.Errorf("--fail-on medium exited %d for a %s-risk run\nstderr: %s", gateRes.ExitCode, risk, gateRes.Stderr)
	}
}
