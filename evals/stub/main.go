// Command stub is a fake `extend` binary for the eval runner. It records
// every invocation into the file named by EXTEND_EVAL_RECORD as JSONL,
// then returns a canned response shaped like the real CLI would emit.
//
// The stub is the linchpin of deterministic skill evaluation: real evals
// can run against it on any developer machine without API credentials,
// without burning credits, and without mutating any real workspace.
//
// The behaviour is selected by EXTEND_EVAL_MODE:
//
//	real_responses (default) — small fixture set; list/get/upload return
//	                            canned realistic data; runs return terminal
//	                            results.
//	paginated                — list calls return pages with nextPageToken
//	                            so pagination-discipline tests have multiple
//	                            pages to iterate over.
//	auth_error               — every call exits with a 401 envelope on
//	                            stderr, mirroring the real CLI's error
//	                            shape.
//
// The stub is invoked the same way the real CLI is — argv is parsed
// minimally to dispatch to the right canned response, not exhaustively.
// Anything we don't model is logged and falls through to a generic
// success exit.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// stdout and stderr are the writers all emit*() functions go through.
// main() replaces them with io.MultiWriter(real, &buf) so the per-call
// response is captured into the recording entry. Tests that exercise
// emit functions directly default to the real fds.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

// exitCode is set by emit functions that need to fail (e.g. parse-run
// cancel attempts). main() reads it after dispatch and propagates via
// os.Exit so flushRecord runs first.
var exitCode int

func main() {
	args := os.Args[1:]
	mode := os.Getenv("EXTEND_EVAL_MODE")
	if mode == "" {
		mode = "real_responses"
	}

	// Always record the invocation, even when the dispatch ends in an
	// auth_error. Recording is what graders walk over to make assertions.
	record(args, mode)

	// stdoutBuf/stderrBuf are appended to as emit*() functions write
	// (via emitJSON, fmt.Fprintln, etc.); flushRecord serializes them
	// alongside the argv into the recording. The fabrication grader
	// scans these for legitimate IDs.
	defer func() {
		flushRecord(exitCode)
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()

	// Wire the global stdout/stderr writers to tee output through the
	// per-call buffers and the real fds. emit functions write through
	// these so the recording captures responses verbatim.
	stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	switch mode {
	case "auth_error":
		emitAuthError()
		exitCode = 1
		return
	}

	// Dispatch by command shape. Order matters: the most specific
	// matchers (e.g. `runs list`) come before generic ones.
	switch {
	case len(args) == 0, isHelpOnly(args):
		emitHelp(args)
		return
	case match(args, "extract", "batch"):
		emitExtractBatch(args, mode)
	case match(args, "extract"):
		emitExtract(args, mode)
	case match(args, "parse"):
		emitParse(args, mode)
	case match(args, "classify"):
		emitClassify(args, mode)
	case match(args, "split"):
		emitSplit(args, mode)
	case match(args, "edit", "schema", "generate"):
		emitEditSchemaGenerate(args, mode)
	case match(args, "edit"):
		emitEdit(args, mode)
	case match(args, "run"):
		emitWorkflowRun(args, mode)
	case match(args, "runs", "list"):
		emitRunsList(args, mode)
	case match(args, "runs", "watch"):
		emitRunsWatch(args, mode)
	case match(args, "runs", "get"):
		emitRunsGet(args, mode)
	case match(args, "runs", "cancel"):
		emitRunsCancel(args, mode)
	case match(args, "extractors", "list"):
		emitExtractorsList(args, mode)
	case match(args, "extractors", "get"):
		emitExtractorsGet(args, mode)
	case match(args, "extractors", "create"):
		emitExtractorsCreate(args, mode)
	case match(args, "extractors", "versions", "create"):
		emitExtractorVersionCreate(args, mode)
	case match(args, "classifiers", "list"):
		emitClassifiersList(args, mode)
	case match(args, "splitters", "list"):
		emitSplittersList(args, mode)
	case match(args, "workflows", "list"):
		emitWorkflowsList(args, mode)
	case match(args, "files", "upload"):
		emitFilesUpload(args, mode)
	case match(args, "files", "list"):
		emitFilesList(args, mode)
	case match(args, "evaluations", "create"):
		emitEvaluationsCreate(args, mode)
	case match(args, "evaluations", "items", "create"):
		emitEvaluationsItemsCreate(args, mode)
	case match(args, "evaluations", "runs", "create"):
		emitEvaluationRunsCreate(args, mode)
	case match(args, "webhooks", "endpoints", "create"):
		emitWebhookEndpointsCreate(args, mode)
	case match(args, "webhooks", "subscriptions", "create"):
		emitWebhookSubscriptionsCreate(args, mode)
	default:
		emitUnknown(args)
	}
}

// match reports whether the leading positional words of argv (skipping
// flags) match the given verb path. Flags are anything starting with "-";
// flag values that follow a flag without "=" are also skipped. This is a
// heuristic — sufficient for our limited dispatch surface, not a full
// flag parser.
func match(args []string, verbs ...string) bool {
	pos := positional(args)
	if len(pos) < len(verbs) {
		return false
	}
	for i, v := range verbs {
		if pos[i] != v {
			return false
		}
	}
	return true
}

func positional(args []string) []string {
	out := make([]string, 0, len(args))
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(a, "--") {
			// `--flag=val` is one token; `--flag val` is two.
			if !strings.Contains(a, "=") {
				skip = true
			}
			continue
		}
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			if !strings.Contains(a, "=") && len(a) == 2 {
				skip = true // short flag with separate value, e.g. `-o json`
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func isHelpOnly(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "help" {
			return true
		}
	}
	return false
}

// flagValue returns the value of a long flag if present, supporting both
// `--flag=val` and `--flag val` forms. Returns "" if absent.
func flagValue(args []string, name string) string {
	prefix := "--" + name
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

// hasFlag reports whether a long flag is present (with or without value).
func hasFlag(args []string, name string) bool {
	prefix := "--" + name
	for _, a := range args {
		if a == prefix || strings.HasPrefix(a, prefix+"=") {
			return true
		}
	}
	return false
}

// record appends one JSONL entry per invocation to EXTEND_EVAL_RECORD.
// The entry is finalized at process exit so the response (captured
// stdout) can be included; see the deferred wrap in main().
func record(args []string, mode string) {
	if os.Getenv("EXTEND_EVAL_RECORD") == "" {
		return
	}
	currentRecord = &recordEntry{
		TS:   time.Now().UTC().Format(time.RFC3339Nano),
		Argv: args,
		Mode: mode,
		CWD:  must(os.Getwd()),
	}
}

type recordEntry struct {
	TS       string   `json:"ts"`
	Argv     []string `json:"argv"`
	Mode     string   `json:"mode"`
	CWD      string   `json:"cwd"`
	ExitCode int      `json:"exit_code"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
}

// currentRecord, stdoutBuf, stderrBuf are populated during dispatch
// and flushed at process exit. Capturing stdout/stderr is what lets
// the fabrication grader verify which IDs the agent legitimately saw
// in prior responses without us tracking it call-by-call.
var (
	currentRecord *recordEntry
	stdoutBuf     strings.Builder
	stderrBuf     strings.Builder
)

// flushRecord finalizes the in-progress record entry and appends it
// to the JSONL file. Idempotent; safe to call multiple times (a
// no-op after the first call).
func flushRecord(exitCode int) {
	if currentRecord == nil {
		return
	}
	currentRecord.ExitCode = exitCode
	currentRecord.Stdout = stdoutBuf.String()
	currentRecord.Stderr = stderrBuf.String()

	path := os.Getenv("EXTEND_EVAL_RECORD")
	if path == "" {
		currentRecord = nil
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "stub: mkdir record dir: %v\n", err)
		currentRecord = nil
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stub: open record: %v\n", err)
		currentRecord = nil
		return
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(currentRecord); err != nil {
		fmt.Fprintf(os.Stderr, "stub: write record: %v\n", err)
	}
	currentRecord = nil
}

func must(s string, err error) string {
	if err != nil {
		return ""
	}
	return s
}

func emitAuthError() {
	fmt.Fprintln(stderr, "Error: 401 Unauthorized: missing or invalid API key")
	fmt.Fprintln(stderr, "Hint: set EXTEND_API_KEY in your environment to authenticate.")
}

// emitHelp produces a help-shaped string realistic enough for an agent
// to read and quote when the user asks "what flags does X accept?". The
// contents intentionally mirror the real CLI's flag set so help-discovery
// (H-*) cases test true discoverability: an agent that consults
// `extend <cmd> --help` should see the same flags the real binary exposes.
//
// It is command-aware: `extend edit --help` and `extend help edit` both
// surface edit's flags only, the way the real CLI scopes per-command help.
// When kept in sync with the real CLI's flags, this is the contract that
// makes a help-discovery eval meaningful; the integration test
// TestEditAdvancedOptions_ExposedInBinary guards that the real `--help`
// carries the same flags.
func emitHelp(args []string) {
	if body, ok := commandHelp[helpTopic(args)]; ok {
		fmt.Println(body)
		return
	}
	fmt.Println(generalHelp)
}

// helpTopic returns the command whose help was requested, handling both
// `extend <cmd> --help` (positional[0]) and `extend help <cmd>`
// (positional after "help"). Returns "" for top-level/unknown help.
func helpTopic(args []string) string {
	pos := positional(args)
	if len(pos) == 0 {
		return ""
	}
	if pos[0] == "help" {
		if len(pos) > 1 {
			return pos[1]
		}
		return ""
	}
	return pos[0]
}

const generalHelp = `extend — Extend document AI CLI (eval stub).

Common commands:
  extract <input>     Run extraction on a document.
  parse <input>       Parse a document into raw text/markdown.
  classify <input>    Classify a document into a category.
  split <input>       Split a multi-document PDF.
  edit <input>        Fill PDF form fields via a values schema.
  run <input>         Run a configured workflow.
  runs list|get|watch Inspect runs.
  evaluations ...     Manage evaluation sets, items, and runs.

Run 'extend <command> --help' for that command's flags.

Help topics:
  extend help auth | output | lifecycle | errors`

// commandHelp mirrors the real CLI's per-command flag set for the
// commands exercised by help-discovery evals. Keep in sync with the real
// command docs in internal/cli/.
var commandHelp = map[string]string{
	"extract": `extend extract <input> — Run extraction on a document.

Flags:
  --using <id>          Saved extractor ID (mutually exclusive with --config)
  --config <json>       Complete one-off config used INSTEAD of a saved extractor
  --patch <json>        Per-run partial merge onto the --using extractor's config
  --version <v>         Extractor version: latest, draft, or specific
  --wait[=true|false]   Block until terminal (default: true)
  --priority <0-100>    Lower = higher priority
  --metadata key=value  Repeatable
  --tag <name>          Usage tag(s); repeatable
  --password <pw>       Password for a protected PDF (URL inputs only)
  -o, --output <fmt>    json|yaml|raw|id|table|markdown
  --jq <expr>           Filter output with jq`,

	"classify": `extend classify <input> — Classify a document into a category.

Flags:
  --using <id>          Saved classifier ID (mutually exclusive with --config)
  --config <json>       Complete one-off classify config used INSTEAD of a saved classifier
  --patch <json>        Per-run partial merge onto the --using classifier's config
  --version <v>         Classifier version: latest, draft, or specific
  --wait[=true|false]   Block until terminal (default: true)
  --priority <0-100>    Lower = higher priority
  --metadata key=value  Repeatable
  --tag <name>          Usage tag(s); repeatable
  -o, --output <fmt>    json|yaml|raw|id|table|markdown`,

	"split": `extend split <input> — Split a multi-document PDF into segments.

Flags:
  --using <id>          Saved splitter ID (mutually exclusive with --config)
  --config <json>       Complete one-off split config used INSTEAD of a saved splitter
  --patch <json>        Per-run partial merge onto the --using splitter's config
  --version <v>         Splitter version: latest, draft, or specific
  --wait[=true|false]   Block until terminal (default: true)
  --priority <0-100>    Lower = higher priority
  -o, --output <fmt>    json|yaml|raw|id|table|markdown`,

	"parse": `extend parse <input> — Parse a document into text/markdown/spatial.

Flags:
  --target <t>              markdown (default) or spatial
  --engine <e>              parse_performance or parse_light
  --chunk-strategy <s>      page|document|section|none
  --block-options <json>    Per-block detection. Fields:
                              figures.advancedChartExtractionEnabled, figures.figureImageClippingEnabled
                              tables.targetFormat ("markdown"|"html"), tables.cellBlocksEnabled,
                              tables.tableHeaderContinuationEnabled, text.signatureDetectionEnabled,
                              barcodes.readingEnabled, formulas.enabled, keyValue.blankFieldFormattingEnabled
  --advanced-options <json> Parse tuning. Fields:
                              pageRanges, pageRotationEnabled, returnOcr.words,
                              excelParsingMode ("basic"|"advanced"), verticalGroupingThreshold,
                              formattingDetection ([{"type":"change_tracking"}])
  --wait[=true|false]       Block until terminal (default: true)
  -o, --output <fmt>        json|yaml|raw|markdown`,

	"webhooks": `extend webhooks — Manage webhook endpoints and subscriptions.

  webhooks endpoints create --url <u> --name <n> --events <e,...>
  webhooks subscriptions create --endpoint <whe> --resource <id> --events <e,...>

Valid --events values:
  Runs:    parse_run.processed, parse_run.failed, extract_run.processed, extract_run.failed,
           classify_run.processed, classify_run.failed, split_run.processed, split_run.failed,
           edit_run.processed, edit_run.failed
  Batch:   batch_parse_run.processed, batch_parse_run.failed,
           batch_processor_run.processed, batch_processor_run.failed
  Workflow runs: workflow_run.completed, workflow_run.failed, workflow_run.needs_review,
           workflow_run.rejected, workflow_run.cancelled, workflow_run.step_run.processed
  Processor lifecycle: extractor.created, extractor.updated, extractor.deleted,
           extractor.draft.updated, extractor.version.published
           (classifier.* and splitter.* take the same five suffixes)
  Workflow lifecycle: workflow.created, workflow.deployed, workflow.deleted`,

	"edit": `extend edit <input> — Fill PDF form fields and emit a filled PDF.

Flags:
  --schema <json>          Schema with values populated per 'extend edit schema generate'
  --instructions <text>    Free-form prose values and rules
  --schema-instructions    Prose applied only to the schema-generation step
  --advanced-options <json> Detection options as a JSON object; omitted fields use the server default:
                              flattenPdf           bool  Make the filled form non-editable
                              nativeFieldsOnly     bool  Only use embedded AcroForm fields (false also detects via vision)
                              tableParsingEnabled  bool  Parse table regions as arrays of objects
                              radioEnumsEnabled    bool  Model a radio group as a single-choice enum
  -O, --output-file <path> Write the filled PDF (auto-downloads); '-' for stdout
  --wait[=true|false]      Block until terminal (default: true)

Schema fields ('extend edit schema generate' emits these per field; populate the value keys):
  extend_edit:value         The value to fill into the field (omit to infer from --instructions)
  extend_edit:image         Image fill for signature fields (PNG/JPEG URL)
  extend_edit:field_type    text|signature|checkbox|radio|dropdown|optionList|table
  extend_edit:bbox, extend_edit:bboxes, extend_edit:page_index, extend_edit:text_edit_options,
  extend_edit:column_width, extend_edit:row_heights`,
}

func emitUnknown(args []string) {
	// Fall through to a benign success: the agent's invocation was something
	// we don't model. Print a short notice so transcripts make this visible.
	fmt.Printf("ok (stub): %s\n", strings.Join(args, " "))
}
