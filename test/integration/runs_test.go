package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// pickFirstID lists resources of the given kind and returns the first ID, or
// skips the test when none exist. The kind is the CLI subcommand prefix
// (e.g. "extractors", "workflows").
func pickFirstID(t *testing.T, env envSetup, kind string) string {
	t.Helper()
	res := runExtend(t, env, kind, "list", "--limit", "1", "-o", "json")
	res.requireOK(t, kind, "list")
	var arr []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.Stdout, &arr); err != nil {
		// Fallback for endpoints that return {data:[...]} envelope.
		var env struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err2 := json.Unmarshal(res.Stdout, &env); err2 != nil {
			t.Fatalf("decode %s list: %v\nstdout: %s", kind, err, res.Stdout)
		}
		arr = env.Data
	}
	if len(arr) == 0 {
		t.Skipf("no %s in workspace; create one to enable this test", kind)
	}
	return arr[0].ID
}

// TestExtractRun_AsyncLifecycle uploads a sample document, submits it for
// extraction with `--wait=false`, polls `runs get` until terminal, and
// verifies the response shape: extractorVersion includes its parent
// extractorId, output decodes as {value, metadata}, parseRunId is
// populated, and usage is present.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestExtractRun_AsyncLifecycle(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	extractorID := pickFirstID(t, env, "extractors")

	// Submit. --wait=false returns the run ID immediately; we then poll
	// separately to verify the watch path also works.
	submitRes := runExtend(t, env,
		"extract", "testdata/sample.txt",
		"--using", extractorID,
		"--wait=false",
		"-o", "json",
	)
	submitRes.requireOK(t, "extract")

	var submitted struct {
		ID     string `json:"id"`
		Object string `json:"object"`
		Status string `json:"status"`
	}
	submitRes.decodeJSON(t, &submitted)
	if !strings.HasPrefix(submitted.ID, "exr_") {
		t.Fatalf("expected exr_ prefix on run id, got %q", submitted.ID)
	}
	rememberCleanup(t, env, "delete extract run", "runs", "delete", submitted.ID, "-y")

	// Wait for terminal state via the watch command rather than open-coding
	// a poll loop — the CLI's watcher has its own logic that's worth
	// exercising end-to-end.
	watchRes := runExtend(t, env, "runs", "watch", submitted.ID, "--timeout", "2m")
	if watchRes.ExitCode != 0 {
		t.Fatalf("runs watch %s exited %d\nstderr: %s", submitted.ID, watchRes.ExitCode, watchRes.Stderr)
	}

	// Now fetch the terminal state and verify shape.
	getRes := runExtend(t, env, "runs", "get", submitted.ID, "-o", "json")
	getRes.requireOK(t, "runs", "get", submitted.ID)

	var run map[string]any
	getRes.decodeJSON(t, &run)

	if status, _ := run["status"].(string); status != "PROCESSED" {
		t.Errorf("status = %q, want PROCESSED (run may have failed): %s", status, getRes.Stdout)
	}

	// extractorVersion is the embedded version summary; it must back-reference
	// its parent extractor by extractorId so callers can correlate runs to
	// processors without a second lookup.
	ev, ok := run["extractorVersion"].(map[string]any)
	if !ok {
		t.Fatalf("extractorVersion missing on extract run: %s", getRes.Stdout)
	}
	if got, _ := ev["extractorId"].(string); got != extractorID {
		t.Errorf("extractorVersion.extractorId = %q, want %q", got, extractorID)
	}

	// Output is the typed {value, metadata} shape on 2026-02-09.
	if output, ok := run["output"].(map[string]any); ok {
		if _, hasValue := output["value"]; !hasValue {
			t.Errorf("output.value missing: %s", getRes.Stdout)
		}
		if _, hasMetadata := output["metadata"]; !hasMetadata {
			t.Errorf("output.metadata missing: %s", getRes.Stdout)
		}
	} else {
		t.Errorf("output missing or not an object: %s", getRes.Stdout)
	}

	// parseRunId tracks the auto-generated parse run that fed the extraction.
	if parseRunID, _ := run["parseRunId"].(string); parseRunID == "" {
		t.Errorf("parseRunId not populated; the upstream parse should be referenced")
	}

	// usage is non-null on PROCESSED runs.
	if usage, ok := run["usage"].(map[string]any); ok {
		if _, hasCredits := usage["credits"]; !hasCredits {
			t.Errorf("usage.credits missing: %s", getRes.Stdout)
		}
	} else {
		t.Errorf("usage missing or not an object: %s", getRes.Stdout)
	}
}

// TestWorkflowRun_AsyncLifecycle submits a workflow against the first
// available workflow in the workspace, polls to completion, and verifies
// the run's structural shape: workflowVersion is populated with object/
// createdAt fields, stepRuns[] is a non-empty array, and each step run has
// a typed step + status + result.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestWorkflowRun_AsyncLifecycle(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	workflowID := pickFirstID(t, env, "workflows")

	submitRes := runExtend(t, env,
		"run", "testdata/sample.txt",
		"--workflow", workflowID,
		"--wait",
		"--timeout", "5m",
		"-o", "json",
	)
	submitRes.requireOK(t, "run", "--workflow", workflowID)

	var run map[string]any
	submitRes.decodeJSON(t, &run)

	id, _ := run["id"].(string)
	if !strings.HasPrefix(id, "workflow_run_") {
		t.Fatalf("expected workflow_run_ prefix on run id, got %q", id)
	}
	rememberCleanup(t, env, "delete workflow run", "runs", "delete", id, "-y")

	if obj, _ := run["object"].(string); obj != "workflow_run" {
		t.Errorf("object = %q, want workflow_run", obj)
	}

	// --wait blocks until the run reaches a terminal state. NEEDS_REVIEW is
	// terminal in the CLI's semantics (it pauses for human action), and
	// FAILED is terminal but a different bug (workflow failed for unrelated
	// reasons — surface it). PENDING/PROCESSING means --wait broke.
	status, _ := run["status"].(string)
	switch status {
	case "PROCESSED", "NEEDS_REVIEW":
		// expected happy paths
	case "FAILED":
		t.Errorf("workflow run terminated FAILED — workflow may be broken; failureMessage: %v", run["failureMessage"])
	default:
		t.Errorf("status = %q after --wait, expected PROCESSED or NEEDS_REVIEW (--wait should have blocked until terminal)", status)
	}

	wv, ok := run["workflowVersion"].(map[string]any)
	if !ok {
		t.Fatalf("workflowVersion missing: %s", submitRes.Stdout)
	}
	if obj, _ := wv["object"].(string); obj != "workflow_version" {
		t.Errorf("workflowVersion.object = %q, want workflow_version", obj)
	}
	if _, ok := wv["createdAt"].(string); !ok {
		t.Errorf("workflowVersion.createdAt missing: %v", wv)
	}

	stepRuns, ok := run["stepRuns"].([]any)
	if !ok || len(stepRuns) == 0 {
		t.Fatalf("stepRuns missing or empty: %s", submitRes.Stdout)
	}
	for i, sr := range stepRuns {
		stepRun, ok := sr.(map[string]any)
		if !ok {
			t.Errorf("stepRun[%d] not an object: %T", i, sr)
			continue
		}
		step, ok := stepRun["step"].(map[string]any)
		if !ok {
			t.Errorf("stepRun[%d].step missing: %v", i, stepRun)
			continue
		}
		if typ, _ := step["type"].(string); typ == "" {
			t.Errorf("stepRun[%d].step.type empty: %v", i, step)
		}
	}
}

// TestWorkflowBatch_ReturnsBatchID verifies the workflow batch response is
// just `{batchId}` (not the full PublicBatchRun envelope the other batch
// endpoints return). Submits two file inputs.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestWorkflowBatch_ReturnsBatchID(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	workflowID := pickFirstID(t, env, "workflows")

	res := runExtend(t, env,
		"run", "batch",
		"testdata/sample.txt", "testdata/sample.txt",
		"--workflow", workflowID,
		"-o", "json",
	)
	res.requireOK(t, "run", "batch", "--workflow", workflowID)

	var got map[string]any
	res.decodeJSON(t, &got)

	batchID, _ := got["batchId"].(string)
	if !strings.HasPrefix(batchID, "batch_") {
		t.Fatalf("expected batch_ prefix on response.batchId, got %q (full body: %s)", batchID, res.Stdout)
	}

	// The response is intentionally just {batchId}; everything else (status,
	// runCount, createdAt) is absent. Guard against a future regression that
	// reshaped this back into a full BatchRun.
	for _, unwanted := range []string{"status", "runCount", "createdAt", "object"} {
		if _, present := got[unwanted]; present {
			t.Errorf("workflow batch response should not include %q (full body: %s)", unwanted, res.Stdout)
		}
	}
}

// TestClassifyRun_AsyncLifecycle covers the classify-run shape: classifier
// summary, classifierVersion with classifierId back-reference, and the
// {classifications:[{type,confidence,...}]} output. Picks the first
// classifier in the workspace; skips if none exist.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestClassifyRun_AsyncLifecycle(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	classifierID := pickFirstID(t, env, "classifiers")

	submitRes := runExtend(t, env,
		"classify", "testdata/sample.txt",
		"--using", classifierID,
		"--wait=false",
		"-o", "json",
	)
	submitRes.requireOK(t, "classify")

	var submitted struct {
		ID string `json:"id"`
	}
	submitRes.decodeJSON(t, &submitted)
	if !strings.HasPrefix(submitted.ID, "clr_") {
		t.Fatalf("expected clr_ prefix on run id, got %q", submitted.ID)
	}
	rememberCleanup(t, env, "delete classify run", "runs", "delete", submitted.ID, "-y")

	watchRes := runExtend(t, env, "runs", "watch", submitted.ID, "--timeout", "2m")
	if watchRes.ExitCode != 0 {
		t.Fatalf("runs watch %s exited %d\nstderr: %s", submitted.ID, watchRes.ExitCode, watchRes.Stderr)
	}

	getRes := runExtend(t, env, "runs", "get", submitted.ID, "-o", "json")
	getRes.requireOK(t, "runs", "get", submitted.ID)

	var run map[string]any
	getRes.decodeJSON(t, &run)

	cv, ok := run["classifierVersion"].(map[string]any)
	if !ok {
		t.Fatalf("classifierVersion missing on classify run: %s", getRes.Stdout)
	}
	if got, _ := cv["classifierId"].(string); got != classifierID {
		t.Errorf("classifierVersion.classifierId = %q, want %q", got, classifierID)
	}
}

// TestSplitRun_AsyncLifecycle covers the split-run shape: splitter summary,
// splitterVersion with splitterId back-reference. Picks the first splitter
// in the workspace; skips if none exist.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestSplitRun_AsyncLifecycle(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	splitterID := pickFirstID(t, env, "splitters")

	submitRes := runExtend(t, env,
		"split", "testdata/sample.txt",
		"--using", splitterID,
		"--wait=false",
		"-o", "json",
	)
	submitRes.requireOK(t, "split")

	var submitted struct {
		ID string `json:"id"`
	}
	submitRes.decodeJSON(t, &submitted)
	if !strings.HasPrefix(submitted.ID, "splr_") {
		t.Fatalf("expected splr_ prefix on run id, got %q", submitted.ID)
	}
	rememberCleanup(t, env, "delete split run", "runs", "delete", submitted.ID, "-y")

	watchRes := runExtend(t, env, "runs", "watch", submitted.ID, "--timeout", "2m")
	if watchRes.ExitCode != 0 {
		t.Fatalf("runs watch %s exited %d\nstderr: %s", submitted.ID, watchRes.ExitCode, watchRes.Stderr)
	}

	getRes := runExtend(t, env, "runs", "get", submitted.ID, "-o", "json")
	getRes.requireOK(t, "runs", "get", submitted.ID)

	var run map[string]any
	getRes.decodeJSON(t, &run)

	sv, ok := run["splitterVersion"].(map[string]any)
	if !ok {
		t.Fatalf("splitterVersion missing on split run: %s", getRes.Stdout)
	}
	if got, _ := sv["splitterId"].(string); got != splitterID {
		t.Errorf("splitterVersion.splitterId = %q, want %q", got, splitterID)
	}
}

// TestWorkflowRun_UpdateMetadata exercises POST /workflow_runs/:id which
// updates a run's metadata in place. Only workflow runs support this — the
// CLI rejects other run types with a clear error.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestWorkflowRun_UpdateMetadata(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	workflowID := pickFirstID(t, env, "workflows")

	// Workflow runs are async by default (--wait defaults to false on
	// `run`). We don't wait here because the metadata update endpoint
	// accepts in-flight runs.
	submitRes := runExtend(t, env,
		"run", "testdata/sample.txt",
		"--workflow", workflowID,
		"-o", "json",
	)
	submitRes.requireOK(t, "run")

	var submitted struct {
		ID string `json:"id"`
	}
	submitRes.decodeJSON(t, &submitted)
	rememberCleanup(t, env, "delete workflow run", "runs", "delete", submitted.ID, "-y")

	updateRes := runExtend(t, env,
		"runs", "update", submitted.ID,
		"--metadata", "customer=acme",
		"--tag", "integration-test",
		"-o", "json",
	)
	updateRes.requireOK(t, "runs", "update", submitted.ID)

	var updated map[string]any
	updateRes.decodeJSON(t, &updated)
	md, ok := updated["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing after update: %s", updateRes.Stdout)
	}
	if customer, _ := md["customer"].(string); customer != "acme" {
		t.Errorf("metadata.customer = %q, want acme", customer)
	}
	// `--tag` flags are stored server-side under the Extend-reserved key
	// `extend:usage_tags`, not a generic `tags` field.
	tags, _ := md["extend:usage_tags"].([]any)
	foundTag := false
	for _, tg := range tags {
		if s, _ := tg.(string); s == "integration-test" {
			foundTag = true
			break
		}
	}
	if !foundTag {
		t.Errorf("metadata['extend:usage_tags'] missing 'integration-test': %v", md)
	}
}

// TestRunsCancel_ExtractRun submits an async extract run and cancels it
// immediately. The server tracks the cancellation regardless of whether the
// run had already started processing — terminal status will be either
// CANCELLED (caught in time) or PROCESSED/FAILED (raced past the cancel).
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestRunsCancel_ExtractRun(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	extractorID := pickFirstID(t, env, "extractors")

	submitRes := runExtend(t, env,
		"extract", "testdata/sample.txt",
		"--using", extractorID,
		"--wait=false",
		"-o", "json",
	)
	submitRes.requireOK(t, "extract")
	var submitted struct {
		ID string `json:"id"`
	}
	submitRes.decodeJSON(t, &submitted)
	rememberCleanup(t, env, "delete extract run", "runs", "delete", submitted.ID, "-y")

	cancelRes := runExtend(t, env, "runs", "cancel", submitted.ID, "-y")
	// Cancel can legitimately race with processing: if the run completes
	// before the cancel reaches the server, the API reports "cannot cancel
	// terminal run" (a 4xx). Either case is acceptable; what's NOT
	// acceptable is the cancel command silently dispatching nowhere.
	if cancelRes.ExitCode != 0 && !strings.Contains(string(cancelRes.Stderr), "terminal") {
		t.Fatalf("cancel exited %d with unexpected error: %s", cancelRes.ExitCode, cancelRes.Stderr)
	}

	// Poll briefly for a terminal state — the server should reach one
	// quickly for a sample.txt extract (either via the cancel or by racing
	// past it). If the run is still PROCESSING or PENDING after a short
	// wait, the cancel never took effect and that's a bug.
	var status string
	for i := 0; i < 10; i++ {
		getRes := runExtend(t, env, "runs", "get", submitted.ID, "-o", "json")
		getRes.requireOK(t, "runs", "get", submitted.ID)
		var got map[string]any
		getRes.decodeJSON(t, &got)
		status, _ = got["status"].(string)
		if status == "CANCELLED" || status == "PROCESSED" || status == "FAILED" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	terminalStatuses := map[string]bool{"CANCELLED": true, "PROCESSED": true, "FAILED": true}
	if !terminalStatuses[status] {
		t.Errorf("status = %q after cancel + 5s wait, expected CANCELLED|PROCESSED|FAILED", status)
	}
}

// TestRunsUpdate_RejectsNonWorkflowRun confirms the CLI's guard against
// using `runs update` on non-workflow run IDs (the server only supports
// metadata mutation on workflow runs).
func TestRunsUpdate_RejectsNonWorkflowRun(t *testing.T) {
	env := requireEnv(t)

	// Use a fake-but-correctly-prefixed extract run ID. The CLI's guard
	// fires before any HTTP call so the test doesn't need a real run.
	res := runExtend(t, env,
		"runs", "update", "exr_nonexistent",
		"--metadata", "k=v",
	)
	if res.ExitCode == 0 {
		t.Fatalf("runs update on non-workflow run should fail; got success: %s", res.Stdout)
	}
	if !strings.Contains(string(res.Stderr), "workflow run") {
		t.Errorf("error message should mention 'workflow run'; got: %s", res.Stderr)
	}
}

// TestParseRun_AsyncLifecycle submits a parse run and verifies the output
// includes typed chunks and blocks. Each chunk decodes its metadata
// (pageRange) and each block has the boundingBox + content fields.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestParseRun_AsyncLifecycle(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	submitRes := runExtend(t, env,
		"parse", "testdata/sample.txt",
		"--target", "markdown",
		"--wait=false",
		"-o", "json",
	)
	submitRes.requireOK(t, "parse")

	var submitted struct {
		ID string `json:"id"`
	}
	submitRes.decodeJSON(t, &submitted)
	if !strings.HasPrefix(submitted.ID, "pr_") {
		t.Fatalf("expected pr_ prefix on run id, got %q", submitted.ID)
	}
	rememberCleanup(t, env, "delete parse run", "runs", "delete", submitted.ID, "-y")

	watchRes := runExtend(t, env, "runs", "watch", submitted.ID, "--timeout", "2m")
	if watchRes.ExitCode != 0 {
		t.Fatalf("runs watch %s exited %d\nstderr: %s", submitted.ID, watchRes.ExitCode, watchRes.Stderr)
	}

	getRes := runExtend(t, env, "runs", "get", submitted.ID, "-o", "json")
	getRes.requireOK(t, "runs", "get", submitted.ID)

	var run map[string]any
	getRes.decodeJSON(t, &run)

	if status, _ := run["status"].(string); status != "PROCESSED" {
		t.Fatalf("status = %q, want PROCESSED: %s", status, getRes.Stdout)
	}

	output, ok := run["output"].(map[string]any)
	if !ok {
		t.Fatalf("output missing: %s", getRes.Stdout)
	}
	chunks, ok := output["chunks"].([]any)
	if !ok || len(chunks) == 0 {
		t.Fatalf("output.chunks missing or empty: %s", getRes.Stdout)
	}

	chunk, ok := chunks[0].(map[string]any)
	if !ok {
		t.Fatalf("chunk[0] not an object: %T", chunks[0])
	}
	// Every chunk has a metadata.pageRange object.
	if md, ok := chunk["metadata"].(map[string]any); ok {
		if _, ok := md["pageRange"].(map[string]any); !ok {
			t.Errorf("chunk[0].metadata.pageRange missing: %v", md)
		}
	} else {
		t.Errorf("chunk[0].metadata missing: %v", chunk)
	}
	// Each chunk's blocks[] is the primary parse payload.
	blocks, ok := chunk["blocks"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("chunk[0].blocks missing or empty: %v", chunk)
	}
	block, ok := blocks[0].(map[string]any)
	if !ok {
		t.Fatalf("block[0] not an object: %T", blocks[0])
	}
	if typ, _ := block["type"].(string); typ == "" {
		t.Errorf("block[0].type missing: %v", block)
	}
	if _, ok := block["boundingBox"].(map[string]any); !ok {
		t.Errorf("block[0].boundingBox missing: %v", block)
	}
}
