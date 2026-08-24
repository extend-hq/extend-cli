package extendx

import (
	"fmt"
	"strings"
)

// RunStatus is the canonical CLI representation of a run's lifecycle
// state. The SDK exposes a per-kind enum (ProcessorRunStatus,
// WorkflowRunStatus, ...), each declared as `string`; we collapse them
// onto a single RunStatus value so command code can compare against
// shared constants and so the help topics can document a single state
// table. Conversion is a cheap string cast in both directions.
type RunStatus string

const (
	StatusPending     RunStatus = "PENDING"
	StatusProcessing  RunStatus = "PROCESSING"
	StatusProcessed   RunStatus = "PROCESSED"
	StatusFailed      RunStatus = "FAILED"
	StatusCancelled   RunStatus = "CANCELLED"
	StatusNeedsReview RunStatus = "NEEDS_REVIEW"
	StatusRejected    RunStatus = "REJECTED"
	StatusCancelling  RunStatus = "CANCELLING"
)

// TerminalSuccessStates lists the run statuses that represent a
// successful terminal outcome. Action commands (extract, classify, ...)
// exit zero on these.
var TerminalSuccessStates = []RunStatus{StatusProcessed}

// TerminalFailureStates lists the run statuses that represent a failed
// terminal outcome and cause action commands to exit non-zero.
// NEEDS_REVIEW is intentionally not in this list: it pauses for human
// action but the run itself has not failed. Per-kind subsets apply
// (parse runs cannot be CANCELLED or REJECTED), but commands check
// membership here.
var TerminalFailureStates = []RunStatus{StatusFailed, StatusCancelled, StatusRejected}

// TerminalReviewStates lists statuses that are terminal but indicate
// the run is awaiting a human decision rather than complete or failed.
var TerminalReviewStates = []RunStatus{StatusNeedsReview}

// IsTerminalFailure reports whether s is a terminal-failure state. Use
// this in exit-code logic instead of comparing against statuses
// individually.
func (s RunStatus) IsTerminalFailure() bool {
	for _, t := range TerminalFailureStates {
		if s == t {
			return true
		}
	}
	return false
}

func (s RunStatus) IsTerminal() bool {
	switch s {
	case StatusProcessed, StatusFailed, StatusCancelled, StatusNeedsReview, StatusRejected:
		return true
	}
	return false
}

// RunKind names a run resource type. Commands are typed per kind
// (`extend extract runs get`, `extend workflows runs watch`, ...), so
// the kind is always chosen by the invoked command; the ID-prefix
// table below exists only to fail fast with a pointer to the right
// command when an ID of a different type is pasted.
type RunKind string

const (
	KindExtract    RunKind = "extract"
	KindParse      RunKind = "parse"
	KindClassify   RunKind = "classify"
	KindSplit      RunKind = "split"
	KindWorkflow   RunKind = "workflow"
	KindEdit       RunKind = "edit"
	KindDetectForm RunKind = "detect-form"
)

// Verb returns the CLI command group that owns this kind's typed runs
// subcommands ("extend <verb> runs ..."). It differs from the kind
// name only for workflows, whose resource group is plural.
func (k RunKind) Verb() string {
	if k == KindWorkflow {
		return "workflows"
	}
	return string(k)
}

// RunIDPrefix returns the server-issued ID prefix for a run kind.
func RunIDPrefix(k RunKind) string {
	switch k {
	case KindExtract:
		return "exr_"
	case KindParse:
		return "pr_"
	case KindClassify:
		return "clr_"
	case KindSplit:
		return "splr_"
	case KindWorkflow:
		return "workflow_run_"
	case KindEdit:
		return "edr_"
	case KindDetectForm:
		return "sgr_"
	}
	return ""
}

// RunKindFromID maps an ID prefix back to its run kind. Used only for
// mismatch validation (ValidateRunID) and human-readable error
// messages, never for command dispatch.
func RunKindFromID(id string) (RunKind, bool) {
	switch {
	case strings.HasPrefix(id, "exr_"):
		return KindExtract, true
	case strings.HasPrefix(id, "pr_"):
		return KindParse, true
	case strings.HasPrefix(id, "clr_"):
		return KindClassify, true
	case strings.HasPrefix(id, "splr_"):
		return KindSplit, true
	case strings.HasPrefix(id, "workflow_run_"):
		return KindWorkflow, true
	case strings.HasPrefix(id, "edr_"):
		return KindEdit, true
	case strings.HasPrefix(id, "sgr_"):
		return KindDetectForm, true
	}
	return "", false
}

// SupportsRunAction reports whether a run kind has the given typed
// runs subcommand. Mirrors the capability flags in the CLI's
// runsGroupSpec table: parse, edit, and detect-form runs have no
// cancel endpoint, edit and detect-form runs have no list endpoint,
// detect-form runs have no delete endpoint, and only workflow runs
// support update. Used so mismatch errors never redirect to a command
// that doesn't exist for the ID's actual kind.
func SupportsRunAction(k RunKind, action string) bool {
	switch action {
	case "cancel":
		return k != KindParse && k != KindEdit && k != KindDetectForm
	case "list":
		return k != KindEdit && k != KindDetectForm
	case "delete":
		return k != KindDetectForm
	case "update":
		return k == KindWorkflow
	}
	return true
}

// ValidateRunID checks that id carries the ID prefix for kind. Typed
// run commands call this before hitting the API so an ID of the wrong
// type fails fast with a pointer to the right command instead of a
// confusing server-side 404. action is the invoked leaf ("get",
// "watch", ...) and is echoed into the redirect hint when the ID's
// actual kind supports it.
func ValidateRunID(kind RunKind, id, action string) error {
	actual, ok := RunKindFromID(id)
	if !ok {
		return fmt.Errorf("%q is not a recognized %s run ID (expected %s prefix)", id, kind, RunIDPrefix(kind))
	}
	if actual == kind {
		return nil
	}
	if SupportsRunAction(actual, action) {
		return fmt.Errorf("%s is an ID for %s runs, not %s runs; use 'extend %s runs %s %s'",
			id, actual, kind, actual.Verb(), action, id)
	}
	return fmt.Errorf("%s is an ID for %s runs, not %s runs; %s runs do not support %s",
		id, actual, kind, actual, action)
}
