package integration

import (
	"strings"
	"testing"
)

// These tests cover the P0 surfaces added to close docs→CLI coverage gaps:
// inline `--config` on classify/split (one-off configless runs), and the
// edit advanced-detection toggles. They come in two flavours:
//
//   - Validation/help checks that need no API and no credits, so they run on
//     every `go test ./...` and give black-box signal that the shipped binary
//     exposes and guards the new surface.
//   - Gated live submissions (requireEnv + requireRunOps) that prove the real
//     API accepts the inline config shape the CLI builds.

// TestClassifyConfig_MutualExclusionValidation drives the real binary and
// asserts the --using/--config guard fires client-side (before any HTTP), so
// it needs neither creds nor credits.
func TestClassifyConfig_MutualExclusionValidation(t *testing.T) {
	// Neither --using nor --config.
	neither := runExtend(t, envSetup{}, "classify", "testdata/sample.txt")
	if neither.ExitCode == 0 {
		t.Fatalf("classify with neither --using nor --config should fail; got success: %s", neither.Stdout)
	}
	if !strings.Contains(string(neither.Stderr), "exactly one of --using or --config") {
		t.Errorf("neither: expected mutual-exclusion error; stderr: %s", neither.Stderr)
	}

	// Both --using and --config.
	both := runExtend(t, envSetup{}, "classify", "testdata/sample.txt", "--using", "cl_x", "--config", "{}")
	if both.ExitCode == 0 {
		t.Fatalf("classify with both --using and --config should fail; got success: %s", both.Stdout)
	}
	if !strings.Contains(string(both.Stderr), "exactly one of --using or --config") {
		t.Errorf("both: expected mutual-exclusion error; stderr: %s", both.Stderr)
	}
}

// TestSplitConfig_MutualExclusionValidation mirrors the classify guard.
func TestSplitConfig_MutualExclusionValidation(t *testing.T) {
	neither := runExtend(t, envSetup{}, "split", "testdata/sample.txt")
	if neither.ExitCode == 0 {
		t.Fatalf("split with neither --using nor --config should fail; got success: %s", neither.Stdout)
	}
	if !strings.Contains(string(neither.Stderr), "exactly one of --using or --config") {
		t.Errorf("neither: expected mutual-exclusion error; stderr: %s", neither.Stderr)
	}

	both := runExtend(t, envSetup{}, "split", "testdata/sample.txt", "--using", "spl_x", "--config", "{}")
	if both.ExitCode == 0 {
		t.Fatalf("split with both --using and --config should fail; got success: %s", both.Stdout)
	}
	if !strings.Contains(string(both.Stderr), "exactly one of --using or --config") {
		t.Errorf("both: expected mutual-exclusion error; stderr: %s", both.Stderr)
	}
}

// TestEditAdvancedOptions_ExposedInBinary asserts the shipped binary
// documents the edit detection options under --advanced-options, naming the
// JSON fields so they're discoverable. Pure --help inspection: no API, no credits.
func TestEditAdvancedOptions_ExposedInBinary(t *testing.T) {
	res := runExtend(t, envSetup{}, "edit", "--help")
	res.requireOK(t, "edit", "--help")
	for _, want := range []string{"--advanced-options", "tableParsingEnabled", "radioEnumsEnabled"} {
		if !strings.Contains(string(res.Stdout), want) {
			t.Errorf("edit --help missing %q; got:\n%s", want, res.Stdout)
		}
	}
}

// TestEvaluationRunsCreate_ExposedInBinary asserts the new command is wired
// into the binary and documents --entity/--item. No API, no credits.
func TestEvaluationRunsCreate_ExposedInBinary(t *testing.T) {
	res := runExtend(t, envSetup{}, "evaluations", "runs", "create", "--help")
	res.requireOK(t, "evaluations", "runs", "create", "--help")
	for _, want := range []string{"--entity", "--entity-version", "--item"} {
		if !strings.Contains(string(res.Stdout), want) {
			t.Errorf("evaluations runs create --help missing %q; got:\n%s", want, res.Stdout)
		}
	}
}

// TestClassifyRun_InlineConfig submits a one-off classify with an inline
// --config (no saved classifier) and asserts the live API accepts the shape
// the CLI builds, returning a clr_ run. Uses --wait=false so the test
// validates submission/acceptance, not downstream processing.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestClassifyRun_InlineConfig(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	cfg := `{"baseProcessor":"classification_performance","classifications":[{"id":"c1","type":"other","description":"Any document"}]}`
	res := runExtend(t, env,
		"classify", "testdata/sample.txt",
		"--config", cfg,
		"--wait=false",
		"-o", "json",
	)
	res.requireOK(t, "classify", "--config")

	var submitted struct {
		ID string `json:"id"`
	}
	res.decodeJSON(t, &submitted)
	if !strings.HasPrefix(submitted.ID, "clr_") {
		t.Fatalf("expected clr_ prefix on run id, got %q", submitted.ID)
	}
	rememberCleanup(t, env, "delete classify run", "runs", "delete", submitted.ID, "-y")
}

// TestSplitRun_InlineConfig is the split analog of the classify test above.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestSplitRun_InlineConfig(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	cfg := `{"baseProcessor":"splitting_performance","splitClassifications":[{"id":"s1","type":"other","description":"Any document"}]}`
	res := runExtend(t, env,
		"split", "testdata/sample.txt",
		"--config", cfg,
		"--wait=false",
		"-o", "json",
	)
	res.requireOK(t, "split", "--config")

	var submitted struct {
		ID string `json:"id"`
	}
	res.decodeJSON(t, &submitted)
	if !strings.HasPrefix(submitted.ID, "splr_") {
		t.Fatalf("expected splr_ prefix on run id, got %q", submitted.ID)
	}
	rememberCleanup(t, env, "delete split run", "runs", "delete", submitted.ID, "-y")
}
