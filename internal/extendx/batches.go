package extendx

import (
	"errors"
	"strings"
)

// BatchKind identifies which server endpoint produced a batch ID. Used
// by `extend batches get/watch` to dispatch on prefix and surface the
// correct error for workflow batches (which have no retrieval endpoint).
type BatchKind string

const (
	// BatchKindProcessor matches IDs returned by /extract_runs/batch,
	// /classify_runs/batch, and /split_runs/batch (server prefix `bpr_`).
	BatchKindProcessor BatchKind = "processor"
	// BatchKindParse matches IDs returned by /parse_runs/batch (`bpar_`).
	BatchKindParse BatchKind = "parse"
	// BatchKindWorkflow matches IDs returned by /workflow_runs/batch
	// (`batch_`). Workflow batches do NOT support GET /batch_runs/{id};
	// the server has no public retrieval endpoint for them. Callers
	// must list workflow runs filtered by batchId to track progress.
	BatchKindWorkflow BatchKind = "workflow"
)

func BatchKindFromID(id string) (BatchKind, bool) {
	switch {
	case strings.HasPrefix(id, "bpr_"):
		return BatchKindProcessor, true
	case strings.HasPrefix(id, "bpar_"):
		return BatchKindParse, true
	case strings.HasPrefix(id, "batch_"):
		return BatchKindWorkflow, true
	}
	return "", false
}

// ErrWorkflowBatchNotRetrievable is returned by GetBatchRun and
// WaitForBatchRun when called with a workflow batch ID. The server has
// no public retrieval endpoint for workflow batches; use
// ListWorkflowRuns with the BatchID filter to poll progress.
var ErrWorkflowBatchNotRetrievable = errors.New("workflow batches (batch_*) cannot be retrieved via /batch_runs/{id}; use 'extend runs list --type workflow --batch <id>' to track progress")

// ErrNotCancellable signals that the run referred to by an ID prefix
// has no cancel endpoint. Parse and edit runs fall into this bucket.
var ErrNotCancellable = errors.New("run type is not cancellable")

// CanCancel validates that a run ID points to a cancellable run kind.
// Returns nil if the ID is recognized AND the kind supports cancel.
func CanCancel(id string) error {
	kind, ok := RunKindFromID(id)
	if !ok {
		return errors.New("unknown run id prefix")
	}
	if kind == KindParse {
		return errors.New("parse runs cannot be cancelled")
	}
	if kind == KindEdit {
		return errors.New("edit runs cannot be cancelled")
	}
	return nil
}
