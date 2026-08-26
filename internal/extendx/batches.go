package extendx

import (
	"errors"
	"fmt"
	"strings"
)

// BatchKind identifies which server endpoint produced a batch ID.
// Batch commands are typed per verb (`extend extract batches get`,
// `extend parse batches watch`, ...); the prefix table exists only to
// validate a pasted ID against the invoked command and to surface the
// correct error for workflow batches (which have no retrieval
// endpoint).
type BatchKind string

const (
	// BatchKindProcessor matches IDs returned by /extract_runs/batch,
	// /classify_runs/batch, and /split_runs/batch (server prefix `bpr_`).
	// The prefix is shared across the three processor types, so prefix
	// validation cannot tell an extract batch from a classify batch;
	// the server is the authority on the exact type.
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

// ErrWorkflowBatchNotRetrievable is returned by batch get/watch paths
// when called with a workflow batch ID. The server has no public
// retrieval endpoint for workflow batches; use the workflow runs list
// with the BatchID filter to poll progress.
var ErrWorkflowBatchNotRetrievable = errors.New("workflow batches (batch_*) cannot be retrieved via /batch_runs/{id}; use 'extend workflows runs list --batch <id>' to track progress")

// ValidateBatchID checks that id carries the ID prefix the invoked
// typed batch command expects. expected is BatchKindProcessor for the
// extract/classify/split batch commands and BatchKindParse for parse.
// A workflow batch ID always fails with the no-retrieval-endpoint
// error. Because bpr_ is shared by extract, classify, and split, a
// processor-kind match here does not guarantee the exact type; the
// server resolves that.
func ValidateBatchID(expected BatchKind, id, action string) error {
	actual, ok := BatchKindFromID(id)
	if !ok {
		prefix := "bpr_"
		if expected == BatchKindParse {
			prefix = "bpar_"
		}
		return fmt.Errorf("%q is not a recognized batch run ID (expected %s prefix)", id, prefix)
	}
	if actual == BatchKindWorkflow {
		return ErrWorkflowBatchNotRetrievable
	}
	if actual == expected {
		return nil
	}
	if expected == BatchKindParse {
		return fmt.Errorf("%s is an ID for extract, classify, or split batches, not parse batches; use 'extend <type> batches %s %s'", id, action, id)
	}
	return fmt.Errorf("%s is an ID for parse batches; use 'extend parse batches %s %s'", id, action, id)
}
