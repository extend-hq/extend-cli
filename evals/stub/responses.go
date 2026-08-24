package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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
	if err := validateFastInvoiceExtractConfig(flagValue(args, "config")); err != nil {
		fmt.Fprintf(stderr, "Error: invalid extract config: %v\n", err)
		exitCode = 1
		return
	}
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
	// Processor batches carry the bpr_ prefix; the real CLI's
	// ValidateBatchID rejects anything else on `extract batches
	// get|watch`, so the fixture ID must be followable.
	id := nowID("bpr_")
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

// emitEditRemoved mirrors the real CLI's migration hint for the retired
// `edit schema` and `edit detections` subgroups.
func emitEditRemoved(args []string) {
	fmt.Fprintf(stderr, "Error: unknown command %q for \"extend edit\": schema scaffolding moved; use 'extend detect-form'\n", positional(args)[1])
	exitCode = 1
}

func emitWorkflowRun(args []string, mode string) {
	r := terminalRun("workflow_run_", "PROCESSED")
	emitJSON(r)
}

// emitRunsList is the centerpiece for pagination tests. In paginated
// mode we split fixtureFailedRuns across pages keyed by --page-token.
//
// Mirrors a real-CLI behaviour: `edit runs list` does not exist (the
// API has no list-edit-runs endpoint). Agents are expected to
// recognize this and use `edit runs get edr_xxx` per ID instead.
func emitRunsList(args []string, mode string) {
	if verb := positional(args)[0]; verb == "edit" || verb == "detect-form" {
		fmt.Fprintf(stderr, "Error: unknown command \"list\" for \"extend %s runs\"\n", verb)
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
	kind := positional(args)[0]
	if kind == "workflows" {
		kind = "workflow"
	}
	out := make([]Run, 0, len(in))
	for _, r := range in {
		if r.Type != kind {
			continue
		}
		if status != "" && r.Status != status {
			continue
		}
		out = append(out, r)
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

// typedRunID pulls the run ID from `<verb> runs <action> <id>` argv.
func typedRunID(args []string) string {
	pos := positional(args)
	if len(pos) >= 4 {
		return pos[3]
	}
	return ""
}

func emitRunsWatch(args []string, mode string) {
	id := typedRunID(args)
	if positional(args)[0] == "detect-form" {
		emitDetectionRun(id)
		return
	}
	if r, ok := fixtureRuns[id]; ok {
		emitJSON(r)
		return
	}
	// Synthesize a terminal run from the prefix.
	emitJSON(synthesizeRun(id, "PROCESSED"))
}

func emitRunsGet(args []string, mode string) {
	id := typedRunID(args)
	if positional(args)[0] == "detect-form" {
		emitDetectionRun(id)
		return
	}
	if r, ok := fixtureRuns[id]; ok {
		emitJSON(r)
		return
	}
	emitJSON(synthesizeRun(id, "PROCESSED"))
}

func emitRunsCancel(args []string, mode string) {
	// Parse, edit, and detect-form runs have no cancel command in the
	// real CLI; the dispatch reaches here for them too, so mirror
	// cobra's unknown-command failure verbatim.
	verb := positional(args)[0]
	if verb == "parse" || verb == "edit" || verb == "detect-form" {
		fmt.Fprintf(stderr, "Error: unknown command \"cancel\" for \"extend %s runs\"\n", verb)
		exitCode = 1
		return
	}
	emitJSON(synthesizeRun(typedRunID(args), "CANCELLED"))
}

// emitRunsDelete mirrors `<verb> runs delete <id>`: the stub is never
// a TTY, so like the real CLI it refuses without --yes/-y and reports
// the deletion to stderr on success.
func emitRunsDelete(args []string, mode string) {
	if verb := positional(args)[0]; verb == "detect-form" {
		fmt.Fprintf(stderr, "Error: unknown command \"delete\" for \"extend %s runs\"\n", verb)
		exitCode = 1
		return
	}
	confirmed := hasFlag(args, "yes")
	for _, a := range args {
		if a == "-y" {
			confirmed = true
		}
	}
	if !confirmed {
		fmt.Fprintln(stderr, "Error: refusing to delete run without confirmation; pass --yes")
		exitCode = 1
		return
	}
	// positional() treats `-y <id>` as a flag/value pair, so fall back
	// to scanning for a run-prefixed token when the ID lands adjacent
	// to the short flag.
	id := typedRunID(args)
	if id == "" {
		for _, a := range args {
			for _, p := range []string{"exr_", "pr_", "clr_", "splr_", "edr_", "workflow_run_"} {
				if strings.HasPrefix(a, p) {
					id = a
				}
			}
		}
	}
	fmt.Fprintf(stderr, "✓ Deleted run %s\n", id)
}

// emitDetectForm mirrors `extend detect-form <input>`, which waits by
// default and prints the PROCESSED form detection run; output.schema
// carries the generated edit schema. With --wait=false it returns the
// fresh PROCESSING run for later polling via `detect-form runs`.
func emitDetectForm(args []string, mode string) {
	id := nowID("sgr_")
	if equalsFalse(flagValue(args, "wait")) {
		emitJSON(map[string]any{
			"object": "form_detection_run",
			"id":     id,
			"status": "PROCESSING",
		})
		return
	}
	emitDetectionRun(id)
}

func emitDetectionRun(id string) {
	emitJSON(map[string]any{
		"object": "form_detection_run",
		"id":     id,
		"status": "PROCESSED",
		"output": map[string]any{
			"schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "extend_edit:value": nil},
					"date": map[string]any{"type": "string", "extend_edit:value": nil},
				},
			},
		},
	})
}

// emitBatchesStatus serves `<verb> batches get|watch` with a terminal
// processor batch.
func emitBatchesStatus(args []string, mode string) {
	pos := positional(args)
	id := ""
	if len(pos) >= 4 {
		id = pos[3]
	}
	emitJSON(map[string]any{
		"id":     id,
		"status": "PROCESSED",
		"counts": map[string]any{"submitted": 1, "processed": 1, "failed": 0},
	})
}

// emitWorkflowRunBatch mirrors the real workflow batch response, which
// is just {batchId} — there is no status endpoint for workflow batches.
func emitWorkflowRunBatch(args []string, mode string) {
	emitJSON(map[string]any{"batchId": nowID("batch_")})
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

func emitClassifiersCreate(args []string, mode string) {
	if err := validateContractClassifierConfig(flagValue(args, "from-file")); err != nil {
		fmt.Fprintf(stderr, "Error: invalid classifier config: %v\n", err)
		exitCode = 1
		return
	}
	name := flagValue(args, "name")
	if name == "" {
		name = "Stub classifier"
	}
	emitJSON(Classifier{ID: nowID("cl_"), Name: name})
}

func emitSplittersList(args []string, mode string) {
	emitJSON(map[string]any{"data": fixtureSplitters, "nextPageToken": nil})
}

func emitSplittersCreate(args []string, mode string) {
	if err := validateStatementSplitterConfig(flagValue(args, "from-file")); err != nil {
		fmt.Fprintf(stderr, "Error: invalid splitter config: %v\n", err)
		exitCode = 1
		return
	}
	name := flagValue(args, "name")
	if name == "" {
		name = "Stub splitter"
	}
	emitJSON(Splitter{ID: nowID("spl_"), Name: name})
}

func emitWorkflowsList(args []string, mode string) {
	emitJSON(map[string]any{"data": fixtureWorkflows, "nextPageToken": nil})
}

func emitWorkflowsCreate(args []string, mode string) {
	name := flagValue(args, "name")
	if name == "" {
		name = "Stub workflow"
	}
	emitJSON(map[string]any{
		"id":     nowID("workflow_"),
		"object": "workflow",
		"name":   name,
	})
}

func emitWorkflowsUpdate(args []string, mode string) {
	pos := positional(args)
	id := "workflow_stub"
	if len(pos) >= 3 {
		id = pos[2]
	}
	if err := validateWorkflowUpdate(id, flagValue(args, "from-file")); err != nil {
		fmt.Fprintf(stderr, "Error: invalid workflow steps: %v\n", err)
		exitCode = 1
		return
	}
	emitJSON(map[string]any{
		"id":     id,
		"object": "workflow",
		"name":   "Stub workflow",
	})
}

func validateWorkflowUpdate(id, source string) error {
	source = strings.TrimSpace(source)
	switch {
	case id == "workflow_docrouter" || strings.HasSuffix(source, "router-workflow.json"):
		body, err := readWorkflowJSONSource(source)
		if err != nil {
			return err
		}
		return validateRouterWorkflow(body)
	case id == "workflow_vendorcheck" || strings.HasSuffix(source, "vendor-check-workflow.json"):
		body, err := readWorkflowJSONSource(source)
		if err != nil {
			return err
		}
		return validateVendorCheckWorkflow(body)
	}
	return nil
}

func readWorkflowJSONSource(source string) ([]byte, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("--from-file is required")
	}
	if strings.HasPrefix(source, "{") || strings.HasPrefix(source, "[") {
		return []byte(source), nil
	}
	if strings.HasPrefix(strings.ToLower(source), "file://") {
		u, err := url.Parse(source)
		if err != nil {
			return nil, fmt.Errorf("parse file URI %q: %w", source, err)
		}
		if u.Host != "" && u.Host != "localhost" {
			return nil, fmt.Errorf("file URI host %q is not supported", u.Host)
		}
		source = filepath.FromSlash(u.Path)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	return data, nil
}

func validateContractClassifierConfig(source string) error {
	if !strings.HasSuffix(strings.TrimSpace(source), "contract-classifier.json") {
		return nil
	}
	body, err := readWorkflowJSONSource(source)
	if err != nil {
		return err
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	config := mapAt(req, "config")
	classifications, _ := config["classifications"].([]any)
	if len(classifications) < 4 {
		return fmt.Errorf("classifications must include MSA, SOW, NDA, and Other")
	}
	want := map[string]bool{"msa": false, "sow": false, "nda": false, "other": false}
	for _, item := range classifications {
		c, _ := item.(map[string]any)
		idType := strings.ToLower(nestedString(c, "id") + " " + nestedString(c, "type"))
		description := strings.ToLower(nestedString(c, "description"))
		switch {
		case strings.Contains(idType, "msa") || strings.Contains(idType, "master service") || strings.Contains(idType, "master agreement"):
			want["msa"] = true
		case strings.Contains(idType, "sow") || strings.Contains(idType, "statement of work"):
			want["sow"] = true
		case strings.Contains(idType, "nda") || strings.Contains(idType, "non-disclosure") || strings.Contains(idType, "nondisclosure"):
			want["nda"] = true
		case strings.Contains(idType, "other"):
			want["other"] = true
		case strings.Contains(description, "master service") || strings.Contains(description, "master agreement"):
			want["msa"] = true
		case strings.Contains(description, "statement of work"):
			want["sow"] = true
		case strings.Contains(description, "non-disclosure") || strings.Contains(description, "nondisclosure"):
			want["nda"] = true
		case strings.Contains(description, "catch-all") || strings.Contains(description, "catchall"):
			want["other"] = true
		}
		if nestedString(c, "id") == "" || nestedString(c, "type") == "" || nestedString(c, "description") == "" {
			return fmt.Errorf("each classification needs id, type, and description")
		}
	}
	for name, ok := range want {
		if !ok {
			return fmt.Errorf("missing %s classification", name)
		}
	}
	if rules := strings.ToLower(nestedString(config, "classificationRules")); !strings.Contains(rules, "contract") {
		return fmt.Errorf("classificationRules should describe contract classification")
	}
	return nil
}

func validateStatementSplitterConfig(source string) error {
	if !strings.HasSuffix(strings.TrimSpace(source), "statement-splitter.json") {
		return nil
	}
	body, err := readWorkflowJSONSource(source)
	if err != nil {
		return err
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	config := mapAt(req, "config")
	classifications, _ := config["splitClassifications"].([]any)
	if len(classifications) < 2 {
		return fmt.Errorf("splitClassifications must include statement and other")
	}
	foundStatement, foundOther, hasIdentifier := false, false, false
	for _, item := range classifications {
		c, _ := item.(map[string]any)
		label := strings.ToLower(nestedString(c, "id") + " " + nestedString(c, "type") + " " + nestedString(c, "description"))
		if strings.Contains(label, "statement") {
			foundStatement = true
			if nestedString(c, "identifierKey") != "" {
				hasIdentifier = true
			}
		}
		if strings.Contains(label, "other") {
			foundOther = true
		}
		if nestedString(c, "id") == "" || nestedString(c, "type") == "" || nestedString(c, "description") == "" {
			return fmt.Errorf("each split classification needs id, type, and description")
		}
	}
	if !foundStatement || !foundOther {
		return fmt.Errorf("missing statement or other split classification")
	}
	if !hasIdentifier {
		return fmt.Errorf("statement classification should include identifierKey")
	}
	rules := strings.ToLower(nestedString(config, "splitRules"))
	if !strings.Contains(rules, "customer") || !strings.Contains(rules, "statement") {
		return fmt.Errorf("splitRules should mention grouping customer statements")
	}
	return nil
}

func validateFastInvoiceExtractConfig(source string) error {
	if !strings.HasSuffix(strings.TrimSpace(source), "fast-invoice-config.json") {
		return nil
	}
	body, err := readWorkflowJSONSource(source)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		return err
	}
	if got := nestedString(cfg, "baseProcessor"); got != "extraction_light" {
		return fmt.Errorf("baseProcessor = %q, want extraction_light", got)
	}
	advanced := mapAt(cfg, "advancedOptions")
	for _, key := range []string{"citationsEnabled", "modelReasoningInsightsEnabled", "advancedMultimodalEnabled"} {
		if got, ok := advanced[key].(bool); !ok || got {
			return fmt.Errorf("advancedOptions.%s must be false", key)
		}
	}
	schema := mapAt(cfg, "schema")
	if nestedString(schema, "type") != "object" {
		return fmt.Errorf("schema root must be object")
	}
	props := mapAt(schema, "properties")
	if !nullablePrimitive(mapAt(props, "invoice_number"), "string") {
		return fmt.Errorf("invoice_number must be nullable string")
	}
	invoiceDate := mapAt(props, "invoice_date")
	if !nullablePrimitive(invoiceDate, "string") || nestedString(invoiceDate, "extend:type") != "date" {
		return fmt.Errorf("invoice_date must be nullable date string")
	}
	if !currencyField(mapAt(props, "total")) {
		return fmt.Errorf("total must be an object with extend:type currency, amount, and iso_4217_currency_code")
	}
	lineItems := mapAt(props, "line_items")
	if nestedString(lineItems, "type") != "array" {
		return fmt.Errorf("line_items must be an array")
	}
	itemProps := mapAt(mapAt(lineItems, "items"), "properties")
	if !nullablePrimitive(mapAt(itemProps, "description"), "string") {
		return fmt.Errorf("line_items.description must be nullable string")
	}
	if !nullablePrimitive(mapAt(itemProps, "quantity"), "number") {
		return fmt.Errorf("line_items.quantity must be nullable number")
	}
	if !currencyField(mapAt(itemProps, "line_total")) {
		return fmt.Errorf("line_items.line_total must be an object with extend:type currency, amount, and iso_4217_currency_code")
	}
	return nil
}

func mapAt(m map[string]any, key string) map[string]any {
	obj, _ := m[key].(map[string]any)
	if obj == nil {
		return map[string]any{}
	}
	return obj
}

func nullablePrimitive(m map[string]any, typ string) bool {
	vals, ok := m["type"].([]any)
	if !ok {
		return false
	}
	seenType, seenNull := false, false
	for _, v := range vals {
		s, _ := v.(string)
		if s == typ {
			seenType = true
		}
		if s == "null" {
			seenNull = true
		}
	}
	return seenType && seenNull
}

func currencyField(m map[string]any) bool {
	if nestedString(m, "type") != "object" || nestedString(m, "extend:type") != "currency" {
		return false
	}
	props := mapAt(m, "properties")
	return nullablePrimitive(mapAt(props, "amount"), "number") &&
		nullablePrimitive(mapAt(props, "iso_4217_currency_code"), "string")
}

type workflowStepProbe struct {
	Name   string           `json:"name"`
	Type   string           `json:"type"`
	Config map[string]any   `json:"config"`
	Next   []map[string]any `json:"next"`
}

func validateRouterWorkflow(body []byte) error {
	var req struct {
		Steps []workflowStepProbe `json:"steps"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	if len(req.Steps) == 0 {
		return fmt.Errorf("body must contain steps")
	}

	types := map[string]bool{}
	stepsByName := map[string]*workflowStepProbe{}
	extractors := map[string]bool{}
	var classifier *workflowStepProbe
	for i := range req.Steps {
		step := &req.Steps[i]
		types[step.Type] = true
		stepsByName[step.Name] = step
		if step.Type == "CLASSIFY" {
			classifier = step
		}
		if step.Type == "EXTRACT" {
			if id := nestedString(step.Config, "extractor", "id"); id != "" {
				extractors[id] = true
			}
		}
		if step.Type == "WEBHOOK_RESPONSE" && len(step.Next) > 0 {
			return fmt.Errorf("WEBHOOK_RESPONSE must not have next")
		}
	}
	for _, typ := range []string{"TRIGGER", "PARSE", "CLASSIFY", "HUMAN_REVIEW", "COLLECT", "WEBHOOK_RESPONSE"} {
		if !types[typ] {
			return fmt.Errorf("missing %s step", typ)
		}
	}
	for _, id := range []string{"ex_invoiceQ3", "ex_receiptsQ3"} {
		if !extractors[id] {
			return fmt.Errorf("missing extractor %s", id)
		}
	}
	if classifier == nil {
		return fmt.Errorf("missing CLASSIFY step")
	}
	if got := nestedString(classifier.Config, "classifier", "id"); got != "cl_docTypes" {
		return fmt.Errorf("CLASSIFY classifier id = %q, want cl_docTypes", got)
	}
	if got := nestedString(classifier.Config, "classifier", "version"); got != "0.1" {
		return fmt.Errorf("CLASSIFY classifier version = %q, want 0.1", got)
	}
	for _, id := range []string{"cls_invoice", "cls_receipt", "cls_other"} {
		if !nextHas(classifier.Next, "classificationId", id) {
			return fmt.Errorf("CLASSIFY next missing classificationId %s", id)
		}
	}
	if err := validateLinearRoute(stepsByName, "TRIGGER", "PARSE"); err != nil {
		return err
	}
	if err := validateLinearRoute(stepsByName, "PARSE", "CLASSIFY"); err != nil {
		return err
	}
	collect := firstStepOfType(stepsByName, "COLLECT")
	webhook := firstStepOfType(stepsByName, "WEBHOOK_RESPONSE")
	if collect == nil || webhook == nil {
		return fmt.Errorf("missing collect or webhook step")
	}
	if !nextRoutesToType(stepsByName, collect, "WEBHOOK_RESPONSE") {
		return fmt.Errorf("COLLECT must route to WEBHOOK_RESPONSE")
	}
	checks := map[string]struct {
		typeName    string
		extractorID string
	}{
		"cls_invoice": {typeName: "EXTRACT", extractorID: "ex_invoiceQ3"},
		"cls_receipt": {typeName: "EXTRACT", extractorID: "ex_receiptsQ3"},
		"cls_other":   {typeName: "HUMAN_REVIEW"},
	}
	for classID, want := range checks {
		branch := targetForRoute(stepsByName, classifier.Next, "classificationId", classID)
		if branch == nil {
			return fmt.Errorf("classificationId %s does not route to a step", classID)
		}
		if branch.Type != want.typeName {
			return fmt.Errorf("classificationId %s routes to %s, want %s", classID, branch.Type, want.typeName)
		}
		if want.extractorID != "" && nestedString(branch.Config, "extractor", "id") != want.extractorID {
			return fmt.Errorf("classificationId %s extractor mismatch", classID)
		}
		if !nextRoutesToType(stepsByName, branch, "COLLECT") {
			return fmt.Errorf("branch %s must route to COLLECT", branch.Name)
		}
	}
	return nil
}

func validateVendorCheckWorkflow(body []byte) error {
	stepsByName, err := parseWorkflowSteps(body)
	if err != nil {
		return err
	}
	for _, typ := range []string{"TRIGGER", "PARSE", "EXTRACT", "EXTERNAL_DATA_VALIDATION", "CONDITIONAL", "HUMAN_REVIEW", "WEBHOOK_RESPONSE"} {
		if firstStepOfType(stepsByName, typ) == nil {
			return fmt.Errorf("missing %s step", typ)
		}
	}
	if err := validateLinearRoute(stepsByName, "TRIGGER", "PARSE"); err != nil {
		return err
	}
	if err := validateLinearRoute(stepsByName, "PARSE", "EXTRACT"); err != nil {
		return err
	}
	parse := firstStepOfType(stepsByName, "PARSE")
	if got := nestedString(parse.Config, "parseConfig", "target"); got != "markdown" {
		return fmt.Errorf("PARSE parseConfig.target = %q, want markdown", got)
	}
	extract := firstStepOfType(stepsByName, "EXTRACT")
	if got := nestedString(extract.Config, "extractor", "id"); got != "ex_invoiceQ3" {
		return fmt.Errorf("EXTRACT extractor id = %q, want ex_invoiceQ3", got)
	}
	if got := nestedString(extract.Config, "extractor", "version"); got != "latest" {
		return fmt.Errorf("EXTRACT extractor version = %q, want latest", got)
	}
	if !nextRoutesToType(stepsByName, extract, "EXTERNAL_DATA_VALIDATION") {
		return fmt.Errorf("EXTRACT must route to EXTERNAL_DATA_VALIDATION")
	}
	external := firstStepOfType(stepsByName, "EXTERNAL_DATA_VALIDATION")
	if got := nestedString(external.Config, "requestOptions", "url"); got != "https://api.example.com/vendor-check" {
		return fmt.Errorf("external validation url = %q", got)
	}
	if got := nestedString(external.Config, "requestOptions", "method"); !strings.EqualFold(got, "POST") {
		return fmt.Errorf("external validation method = %q, want POST", got)
	}
	if got := nestedString(external.Config, "requestOptions", "contentType"); got != "application/json" {
		return fmt.Errorf("external validation contentType = %q, want application/json", got)
	}
	if !nextRoutesToType(stepsByName, external, "CONDITIONAL") {
		return fmt.Errorf("EXTERNAL_DATA_VALIDATION must route to CONDITIONAL")
	}
	conditional := firstStepOfType(stepsByName, "CONDITIONAL")
	reviewConditionIDs := conditionIDsReferencing(conditional.Config, "output.response.data.requires_review")
	if len(reviewConditionIDs) == 0 {
		return fmt.Errorf("CONDITIONAL must reference external validation requires_review")
	}
	if !nextConditionRoutesToType(stepsByName, conditional, reviewConditionIDs, "HUMAN_REVIEW") {
		return fmt.Errorf("CONDITIONAL requires_review branch must route to HUMAN_REVIEW with matching conditionId")
	}
	defaultIDs := defaultConditionIDs(conditional.Config)
	if len(defaultIDs) == 0 {
		return fmt.Errorf("CONDITIONAL must include a default/NO_OP condition")
	}
	if !nextConditionRoutesToType(stepsByName, conditional, defaultIDs, "WEBHOOK_RESPONSE") {
		return fmt.Errorf("CONDITIONAL default branch must route to WEBHOOK_RESPONSE with matching conditionId")
	}
	review := firstStepOfType(stepsByName, "HUMAN_REVIEW")
	if !nextRoutesToType(stepsByName, review, "WEBHOOK_RESPONSE") {
		return fmt.Errorf("HUMAN_REVIEW must route to WEBHOOK_RESPONSE")
	}
	webhook := firstStepOfType(stepsByName, "WEBHOOK_RESPONSE")
	if len(webhook.Next) > 0 {
		return fmt.Errorf("WEBHOOK_RESPONSE must not have next")
	}
	return nil
}

func parseWorkflowSteps(body []byte) (map[string]*workflowStepProbe, error) {
	var req struct {
		Steps []workflowStepProbe `json:"steps"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	if len(req.Steps) == 0 {
		return nil, fmt.Errorf("body must contain steps")
	}
	stepsByName := map[string]*workflowStepProbe{}
	for i := range req.Steps {
		step := &req.Steps[i]
		stepsByName[step.Name] = step
	}
	return stepsByName, nil
}

func conditionIDsReferencing(config map[string]any, needle string) []string {
	var ids []string
	conditions, _ := config["conditions"].([]any)
	for _, item := range conditions {
		condition, _ := item.(map[string]any)
		for _, key := range []string{"leftOperand", "rightOperand"} {
			value, _ := condition[key].(string)
			if strings.Contains(value, needle) {
				if id, _ := condition["id"].(string); id != "" {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids

}

func defaultConditionIDs(config map[string]any) []string {
	var ids []string
	conditions, _ := config["conditions"].([]any)
	for _, item := range conditions {
		condition, _ := item.(map[string]any)
		id, _ := condition["id"].(string)
		if id == "" {
			continue
		}
		typ, _ := condition["type"].(string)
		operation, _ := condition["operation"].(string)
		formula, _ := condition["formula"].(string)
		if strings.EqualFold(typ, "ELSE") || strings.EqualFold(operation, "NO_OP") || strings.EqualFold(formula, "TRUE") {
			ids = append(ids, id)
		}
	}
	return ids
}

func validateLinearRoute(steps map[string]*workflowStepProbe, fromType, toType string) error {
	from := firstStepOfType(steps, fromType)
	if from == nil {
		return fmt.Errorf("missing %s step", fromType)
	}
	if !nextRoutesToType(steps, from, toType) {
		return fmt.Errorf("%s must route to %s", fromType, toType)
	}
	return nil
}

func firstStepOfType(steps map[string]*workflowStepProbe, typeName string) *workflowStepProbe {
	for _, step := range steps {
		if step.Type == typeName {
			return step
		}
	}
	return nil
}

func nextRoutesToType(steps map[string]*workflowStepProbe, from *workflowStepProbe, toType string) bool {
	for _, route := range from.Next {
		name, _ := route["step"].(string)
		if step := steps[name]; step != nil && step.Type == toType {
			return true
		}
	}
	return false
}

func nextConditionRoutesToType(steps map[string]*workflowStepProbe, from *workflowStepProbe, conditionIDs []string, toType string) bool {
	want := map[string]bool{}
	for _, id := range conditionIDs {
		want[id] = true
	}
	for _, route := range from.Next {
		conditionID, _ := route["conditionId"].(string)
		if !want[conditionID] {
			continue
		}
		name, _ := route["step"].(string)
		if step := steps[name]; step != nil && step.Type == toType {
			return true
		}
	}
	return false
}

func targetForRoute(steps map[string]*workflowStepProbe, next []map[string]any, key, value string) *workflowStepProbe {
	for _, route := range next {
		if got, _ := route[key].(string); got != value {
			continue
		}
		name, _ := route["step"].(string)
		return steps[name]
	}
	return nil
}

func nestedString(m map[string]any, keys ...string) string {
	var cur any = m
	for _, key := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[key]
	}
	s, _ := cur.(string)
	return s
}

func nextHas(next []map[string]any, key, value string) bool {
	for _, route := range next {
		if got, _ := route[key].(string); got == value {
			return true
		}
	}
	return false
}

func emitWorkflowVersionCreate(args []string, mode string) {
	name := flagValue(args, "name")
	if name == "" {
		name = "v1"
	}
	emitJSON(map[string]any{
		"id":      nowID("workflow_version_"),
		"object":  "workflow_version",
		"version": name,
	})
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

// emitEditTemplatesGet stubs `extend edit templates get <edt_id>`. The ID is
// the 4th positional (edit templates get <id>); echo it back with a synthetic
// config/schemaConfig so the agent can answer about the template's defaults.
func emitEditTemplatesGet(args []string, mode string) {
	pos := positional(args)
	id := "edt_stub"
	if len(pos) >= 4 {
		id = pos[3]
	}
	emitJSON(map[string]any{
		"id":     id,
		"object": "edit_template",
		"config": map[string]any{
			"instructions": "Fill the form using the provided values.",
		},
		"schemaConfig": map[string]any{},
	})
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

func emitWebhookVerify(args []string, mode string) {
	for _, flag := range []string{"signature", "timestamp", "secret", "body-file"} {
		if flagValue(args, flag) == "" {
			fmt.Fprintf(stderr, "Error: --%s is required\n", flag)
			exitCode = 1
			return
		}
	}
	if !strings.HasSuffix(flagValue(args, "body-file"), "payload.json") {
		fmt.Fprintln(stderr, "Error: --body-file must point at payload.json for this eval")
		exitCode = 1
		return
	}
	fmt.Fprintln(stderr, "✓ signature valid")
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
