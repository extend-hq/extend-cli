package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// emitJSON marshals v to stdout as compact JSON. The real CLI mostly
// emits TTY-styled output; we emit JSON unconditionally because graders
// parse stdout directly. Real CLI behaviour differences (TTY vs pipe)
// don't affect what the agent's harness sees.
//
// Writes go through the package-level `stdout` writer (a MultiWriter
// to real stdout + the recording buffer) so the response is captured
// in the recording.
func emitJSON(v any) {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "stub: encode response: %v\n", err)
	}
}

// jsonOutput reports whether the agent asked for JSON output. Affects
// only minor presentation; graders walk argv anyway, so this is mostly
// for human-readable transcripts.
func jsonOutput(args []string) bool {
	if v := flagValue(args, "output"); v == "json" {
		return true
	}
	if v := shortFlagValue(args, "o"); v == "json" {
		return true
	}
	return false
}

func shortFlagValue(args []string, name string) string {
	prefix := "-" + name
	for i, a := range args {
		if a == prefix && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, prefix+"=") {
			return strings.TrimPrefix(a, prefix+"=")
		}
	}
	return ""
}

// nowID returns a fresh time-based ID with the given prefix. Used when
// stubbing creates so the agent can chain create→use without us caring
// about uniqueness.
func nowID(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
}

// terminalRun returns a synthetic terminal-state run with the given
// type prefix. Used for action-verb invocations (extract/parse/classify
// /split/edit) so the agent sees a "successful" run record without us
// having to model every flag.
func terminalRun(typePrefix, status string) Run {
	id := nowID(typePrefix)
	r := Run{
		ID:          id,
		Type:        typeFromPrefix(typePrefix),
		Status:      status,
		ProcessedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// Default empty output — caller fills in something realistic.
	if status == "PROCESSED" {
		r.Output = map[string]any{"value": map[string]any{}}
	}
	return r
}

func typeFromPrefix(prefix string) string {
	switch prefix {
	case "exr_":
		return "extract"
	case "pr_":
		return "parse"
	case "clr_":
		return "classify"
	case "splr_":
		return "split"
	case "edr_":
		return "edit"
	case "workflow_run_":
		return "workflow"
	default:
		return "unknown"
	}
}

func emitExtract(args []string, mode string) {
	wait := !equalsFalse(flagValue(args, "wait"))
	if !wait {
		// Async: agent passed --wait=false; emit just the run ID.
		r := terminalRun("exr_", "PROCESSING")
		emitJSON(r)
		return
	}
	r := terminalRun("exr_", "PROCESSED")
	// Return realistic line-item content matching the invoice fixture
	// PDF so the agent doesn't second-guess the result and fall back to
	// `parse` to "recover" the data.
	r.Output = map[string]any{
		"value": map[string]any{
			"invoice_id": "INV-1024",
			"date":       "2026-04-15",
			"bill_to":    "Wile E. Coyote",
			"line_items": []map[string]any{
				{"description": "Widget", "quantity": 2, "amount": 29.99},
				{"description": "Sprocket", "quantity": 1, "amount": 14.50},
				{"description": "Cog (large)", "quantity": 3, "amount": 9.50},
			},
			"total": 102.97,
		},
	}
	emitJSON(r)
}

func emitExtractBatch(args []string, mode string) {
	id := nowID("batch_")
	emitJSON(map[string]any{
		"id":     id,
		"status": "PROCESSING",
		"counts": map[string]any{"submitted": 1, "processed": 0, "failed": 0},
	})
}

func emitParse(args []string, mode string) {
	r := terminalRun("pr_", "PROCESSED")
	r.Output = map[string]any{
		"chunks": []map[string]any{
			{"type": "text", "content": "# Document\n\nStub-generated markdown body."},
		},
	}
	emitJSON(r)
}

func emitClassify(args []string, mode string) {
	r := terminalRun("clr_", "PROCESSED")
	r.Output = map[string]any{
		"value": map[string]any{
			"id":         "MSA",
			"type":       "Master Service Agreement",
			"confidence": 0.94,
		},
	}
	emitJSON(r)
}

func emitSplit(args []string, mode string) {
	r := terminalRun("splr_", "PROCESSED")
	r.Output = map[string]any{
		"value": map[string]any{
			"splits": []map[string]any{
				{"pages": "1-3", "type": "statement"},
				{"pages": "4-6", "type": "statement"},
			},
		},
	}
	emitJSON(r)
}

func emitEdit(args []string, mode string) {
	r := terminalRun("edr_", "PROCESSED")
	emitJSON(r)
}

func emitEditSchemaGenerate(args []string, mode string) {
	emitJSON(map[string]any{
		"fields": []map[string]any{
			{"name": "name", "type": "string", "default": ""},
			{"name": "date", "type": "string", "default": ""},
		},
	})
}

func emitWorkflowRun(args []string, mode string) {
	r := terminalRun("workflow_run_", "PROCESSED")
	emitJSON(r)
}

// emitRunsList is the centerpiece for pagination tests. In paginated
// mode we split fixtureFailedRuns across pages keyed by --page-token.
//
// Mirrors a real-CLI behaviour: --type edit is rejected (the API has
// no list-edit-runs endpoint). Agents are expected to recognize this
// and use `runs get edr_xxx` per ID instead.
func emitRunsList(args []string, mode string) {
	if flagValue(args, "type") == "edit" {
		fmt.Fprintln(stderr,
			"Error: --type edit is not supported (edit runs are not listable; use 'extend runs get edr_...' for individual edit runs)")
		exitCode = 1
		return
	}
	pageSize := atoiOr(flagValue(args, "page-size"), 0)
	if pageSize == 0 {
		pageSize = 3
	}
	if mode != "paginated" {
		// Single-page mode: return all matching runs at once.
		emitJSON(map[string]any{
			"data":          filterRuns(args, fixtureFailedRuns),
			"nextPageToken": nil,
		})
		return
	}
	pageToken := flagValue(args, "page-token")
	all := filterRuns(args, fixtureFailedRuns)
	pages := chunk(all, pageSize)
	if len(pages) == 0 {
		emitJSON(map[string]any{"data": []Run{}, "nextPageToken": nil})
		return
	}
	idx := 0
	if pageToken != "" {
		// Token format: "page-N".
		idx = atoiOr(strings.TrimPrefix(pageToken, "page-"), 0)
	}
	if idx >= len(pages) {
		emitJSON(map[string]any{"data": []Run{}, "nextPageToken": nil})
		return
	}
	out := map[string]any{
		"data":          pages[idx],
		"nextPageToken": nil,
	}
	if idx+1 < len(pages) {
		out["nextPageToken"] = fmt.Sprintf("page-%d", idx+1)
	}
	emitJSON(out)
}

func filterRuns(args []string, in []Run) []Run {
	status := flagValue(args, "status")
	if status == "" {
		return in
	}
	out := make([]Run, 0, len(in))
	for _, r := range in {
		if r.Status == status {
			out = append(out, r)
		}
	}
	return out
}

func chunk(in []Run, size int) [][]Run {
	if size <= 0 {
		return [][]Run{in}
	}
	var out [][]Run
	for i := 0; i < len(in); i += size {
		end := i + size
		if end > len(in) {
			end = len(in)
		}
		out = append(out, in[i:end])
	}
	return out
}

func emitRunsWatch(args []string, mode string) {
	pos := positional(args)
	id := ""
	if len(pos) >= 3 {
		id = pos[2]
	}
	if r, ok := fixtureRuns[id]; ok {
		emitJSON(r)
		return
	}
	// Synthesize a terminal run from the prefix.
	emitJSON(synthesizeRun(id, "PROCESSED"))
}

func emitRunsGet(args []string, mode string) {
	pos := positional(args)
	id := ""
	if len(pos) >= 3 {
		id = pos[2]
	}
	if r, ok := fixtureRuns[id]; ok {
		emitJSON(r)
		return
	}
	emitJSON(synthesizeRun(id, "PROCESSED"))
}

func emitRunsCancel(args []string, mode string) {
	pos := positional(args)
	id := ""
	if len(pos) >= 3 {
		id = pos[2]
	}
	// Parse runs cannot be cancelled in real life. Mirror that.
	if strings.HasPrefix(id, "pr_") {
		fmt.Fprintln(stderr, "Error: parse runs cannot be cancelled (use 'runs delete' to remove the record)")
		exitCode = 1
		return
	}
	emitJSON(synthesizeRun(id, "CANCELLED"))
}

func synthesizeRun(id, status string) Run {
	prefix := "exr_"
	for _, p := range []string{"exr_", "pr_", "clr_", "splr_", "edr_", "workflow_run_"} {
		if strings.HasPrefix(id, p) {
			prefix = p
			break
		}
	}
	return Run{
		ID:          id,
		Type:        typeFromPrefix(prefix),
		Status:      status,
		ProcessedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func emitExtractorsList(args []string, mode string) {
	if mode != "paginated" {
		emitJSON(map[string]any{"data": fixtureExtractors, "nextPageToken": nil})
		return
	}
	emitPaginatedExtractors(args)
}

func emitPaginatedExtractors(args []string) {
	pageSize := atoiOr(flagValue(args, "page-size"), 1)
	if pageSize <= 0 {
		pageSize = 1
	}
	pageToken := flagValue(args, "page-token")
	idx := 0
	if pageToken != "" {
		idx = atoiOr(strings.TrimPrefix(pageToken, "page-"), 0)
	}
	start := idx * pageSize
	end := start + pageSize
	if end > len(fixtureExtractors) {
		end = len(fixtureExtractors)
	}
	if start >= len(fixtureExtractors) {
		emitJSON(map[string]any{"data": []Extractor{}, "nextPageToken": nil})
		return
	}
	out := map[string]any{
		"data":          fixtureExtractors[start:end],
		"nextPageToken": nil,
	}
	if end < len(fixtureExtractors) {
		out["nextPageToken"] = fmt.Sprintf("page-%d", idx+1)
	}
	emitJSON(out)
}

func emitExtractorsGet(args []string, mode string) {
	pos := positional(args)
	id := ""
	if len(pos) >= 3 {
		id = pos[2]
	}
	for _, ex := range fixtureExtractors {
		if ex.ID == id {
			emitJSON(ex)
			return
		}
	}
	fmt.Fprintf(stderr, "Error: extractor %q not found\n", id)
	exitCode = 1
}

func emitExtractorsCreate(args []string, mode string) {
	name := flagValue(args, "name")
	if name == "" {
		name = "Stub extractor"
	}
	emitJSON(Extractor{ID: nowID("ex_"), Name: name})
}

func emitExtractorVersionCreate(args []string, mode string) {
	emitJSON(map[string]any{
		"id":          nowID("exv_"),
		"version":     1,
		"releaseType": flagValue(args, "release-type"),
	})
}

func emitClassifiersList(args []string, mode string) {
	emitJSON(map[string]any{"data": fixtureClassifiers, "nextPageToken": nil})
}

func emitSplittersList(args []string, mode string) {
	emitJSON(map[string]any{"data": fixtureSplitters, "nextPageToken": nil})
}

func emitWorkflowsList(args []string, mode string) {
	emitJSON(map[string]any{"data": fixtureWorkflows, "nextPageToken": nil})
}

func emitFilesUpload(args []string, mode string) {
	emitJSON(File{ID: nowID("file_"), Name: "uploaded.pdf", MIMEType: "application/pdf"})
}

func emitFilesList(args []string, mode string) {
	emitJSON(map[string]any{"data": fixtureFiles, "nextPageToken": nil})
}

func emitEvaluationsCreate(args []string, mode string) {
	emitJSON(map[string]any{
		"id":   nowID("evs_"),
		"name": flagValue(args, "name"),
	})
}

func emitEvaluationsItemsCreate(args []string, mode string) {
	emitJSON(map[string]any{
		"created": 3,
		"failed":  0,
	})
}

// emitEvaluationRunsCreate stubs `extend evaluations runs create <evs_id>`.
// The eval set ID is the 4th positional (evaluations runs create <id>); it
// typically arrives from the prompt in Path-B cases. Returns a synthetic
// evaluation_set_run so the agent sees a created run rather than an error.
func emitEvaluationRunsCreate(args []string, mode string) {
	pos := positional(args)
	setID := ""
	if len(pos) >= 4 {
		setID = pos[3]
	}
	out := map[string]any{
		"id":              nowID("esr_"),
		"object":          "evaluation_set_run",
		"status":          "PENDING",
		"evaluationSetId": setID,
	}
	if entity := flagValue(args, "entity"); entity != "" {
		out["entity"] = map[string]any{"id": entity}
	}
	emitJSON(out)
}

func emitWebhookEndpointsCreate(args []string, mode string) {
	emitJSON(map[string]any{
		"id":            nowID("webhook_"),
		"url":           flagValue(args, "url"),
		"signingSecret": "whsec_" + nowID("")[:16],
	})
}

func emitWebhookSubscriptionsCreate(args []string, mode string) {
	emitJSON(map[string]any{
		"id":         nowID("webhook_subscription_"),
		"endpointId": flagValue(args, "endpoint"),
		"resourceId": flagValue(args, "resource"),
	})
}

// equalsFalse reports whether the given value is an explicit "false".
// Used to distinguish `--wait` (true), absent (true; default), and
// `--wait=false` (false).
func equalsFalse(v string) bool {
	return v == "false" || v == "0" || v == "no"
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
