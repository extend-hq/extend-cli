package extendx

import "strings"

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

// RunKind names a run resource type. Used by `extend runs get/watch/cancel`
// to dispatch on the run's ID prefix to the right SDK sub-client.
type RunKind string

const (
	KindExtract  RunKind = "extract"
	KindParse    RunKind = "parse"
	KindClassify RunKind = "classify"
	KindSplit    RunKind = "split"
	KindWorkflow RunKind = "workflow"
	KindEdit     RunKind = "edit"
)

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
	}
	return "", false
}
