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

	extendcli "github.com/extend-hq/extend-cli/internal/cli"
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
	case match(args, "edit", "templates", "get"):
		emitEditTemplatesGet(args, mode)
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
	case match(args, "workflows", "create"):
		emitWorkflowsCreate(args, mode)
	case match(args, "workflows", "update"):
		emitWorkflowsUpdate(args, mode)
	case match(args, "workflows", "versions", "create"):
		emitWorkflowVersionCreate(args, mode)
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

// emitHelp delegates to the real CLI command tree so help-discovery evals
// observe the same --help text users see, while non-help commands still hit
// the deterministic stub responses below.
func emitHelp(args []string) {
	root := extendcli.NewRoot()
	root.SetOut(stdout)
	root.SetErr(stderr)
	if len(args) == 0 {
		args = []string{"--help"}
	}
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(stderr, "stub: render real help: %v\n", err)
		exitCode = 1
	}
}

func emitUnknown(args []string) {
	// Fall through to a benign success: the agent's invocation was something
	// we don't model. Print a short notice so transcripts make this visible.
	fmt.Printf("ok (stub): %s\n", strings.Join(args, " "))
}
