package client

import (
	"context"
	"errors"
	"strings"
)

type BatchRun struct {
	ID        string    `json:"id"`
	Object    string    `json:"object"`
	Status    RunStatus `json:"status"`
	RunCount  int       `json:"runCount,omitempty"`
	CreatedAt string    `json:"createdAt,omitempty"`
	UpdatedAt string    `json:"updatedAt,omitempty"`
}

func (c *Client) GetBatchRun(ctx context.Context, id string) (*BatchRun, error) {
	if kind, ok := BatchKindFromID(id); ok && kind == BatchKindWorkflow {
		return nil, ErrWorkflowBatchNotRetrievable
	}
	var br BatchRun
	if err := c.getJSON(ctx, "/batch_runs/"+id, &br); err != nil {
		return nil, err
	}
	return &br, nil
}

func (c *Client) WaitForBatchRun(ctx context.Context, id string, opts WaitOptions, onPoll func(*BatchRun)) (*BatchRun, error) {
	return waitForRun(ctx,
		func(ctx context.Context) (*BatchRun, error) { return c.GetBatchRun(ctx, id) },
		func(r *BatchRun) RunStatus { return r.Status },
		opts, onPoll,
	)
}

// ProcessorBatchItem is one input of an extract/classify/split batch. The
// server's per-input schema is exactly {file, metadata?} — no priority, no
// processor ref, no config; those live at the batch top level.
type ProcessorBatchItem struct {
	File     FileRef        `json:"file"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ParseBatchItem is identical to ProcessorBatchItem on the wire today; we
// keep it as a distinct type so the file-shape constraints can diverge if
// the server ever drops one input variant from one endpoint but not the
// other (e.g. parse accepts URL/ID/Text/Base64; split accepts URL/ID only).
type ParseBatchItem struct {
	File     FileRef        `json:"file"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// CreateExtractBatchInput is POST /extract_runs/batch. The extractor is
// required (no `omitempty` on the JSON tag — Zod marks the field required).
// There is NO top-level `metadata`; per-input metadata only.
type CreateExtractBatchInput struct {
	Extractor *ExtractorRef        `json:"extractor"`
	Inputs    []ProcessorBatchItem `json:"inputs"`
	Priority  *int                 `json:"priority,omitempty"`
}

type CreateClassifyBatchInput struct {
	Classifier *ClassifierRef       `json:"classifier"`
	Inputs     []ProcessorBatchItem `json:"inputs"`
	Priority   *int                 `json:"priority,omitempty"`
}

type CreateSplitBatchInput struct {
	Splitter *SplitterRef         `json:"splitter"`
	Inputs   []ProcessorBatchItem `json:"inputs"`
	Priority *int                 `json:"priority,omitempty"`
}

// CreateParseBatchInput is POST /parse_runs/batch. Unlike the processor
// batches, parse batch has no top-level processor ref — `config` carries the
// parser settings instead.
type CreateParseBatchInput struct {
	Inputs   []ParseBatchItem `json:"inputs"`
	Config   *ParseConfig     `json:"config,omitempty"`
	Priority *int             `json:"priority,omitempty"`
}

// WorkflowBatchItem is a single element of CreateWorkflowBatchInput.Inputs.
// The server's per-input schema is exactly {file, metadata?, secrets?} —
// reusing CreateWorkflowRunInput here would silently ship Workflow/Files/
// Outputs/Priority that the server strips, which is misleading.
type WorkflowBatchItem struct {
	File     FileRef        `json:"file"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Secrets  map[string]any `json:"secrets,omitempty"`
}

// CreateWorkflowBatchInput is the request body for POST /workflow_runs/batch.
// Unlike the extract/classify/split batch endpoints, the schema has no
// top-level `priority` or `metadata` — metadata is per-input only.
type CreateWorkflowBatchInput struct {
	Workflow *WorkflowRef        `json:"workflow,omitempty"`
	Inputs   []WorkflowBatchItem `json:"inputs"`
}

// WorkflowBatchResponse is the response shape for POST /workflow_runs/batch.
// The server returns only {batchId}. The returned ID has the `batch_` prefix
// and is not retrievable via GET /batch_runs/{id} (that endpoint only
// handles bpr_/bpar_); use ListWorkflowRuns with the BatchID filter to poll
// workflow batch progress.
type WorkflowBatchResponse struct {
	BatchID string `json:"batchId"`
}

func (c *Client) CreateExtractRunBatch(ctx context.Context, in CreateExtractBatchInput) (*BatchRun, error) {
	var br BatchRun
	if err := c.postJSON(ctx, "/extract_runs/batch", in, &br); err != nil {
		return nil, err
	}
	return &br, nil
}

func (c *Client) CreateClassifyRunBatch(ctx context.Context, in CreateClassifyBatchInput) (*BatchRun, error) {
	var br BatchRun
	if err := c.postJSON(ctx, "/classify_runs/batch", in, &br); err != nil {
		return nil, err
	}
	return &br, nil
}

func (c *Client) CreateSplitRunBatch(ctx context.Context, in CreateSplitBatchInput) (*BatchRun, error) {
	var br BatchRun
	if err := c.postJSON(ctx, "/split_runs/batch", in, &br); err != nil {
		return nil, err
	}
	return &br, nil
}

func (c *Client) CreateParseRunBatch(ctx context.Context, in CreateParseBatchInput) (*BatchRun, error) {
	var br BatchRun
	if err := c.postJSON(ctx, "/parse_runs/batch", in, &br); err != nil {
		return nil, err
	}
	return &br, nil
}

func (c *Client) CreateWorkflowRunBatch(ctx context.Context, in CreateWorkflowBatchInput) (*WorkflowBatchResponse, error) {
	var resp WorkflowBatchResponse
	if err := c.postJSON(ctx, "/workflow_runs/batch", in, &resp); err != nil {
		return nil, err
	}
	if resp.BatchID == "" {
		return nil, errors.New("workflow batch response missing batchId")
	}
	return &resp, nil
}

type BatchKind string

const (
	// BatchKindProcessor matches IDs returned by /extract_runs/batch,
	// /classify_runs/batch, and /split_runs/batch (server prefix `bpr_`).
	BatchKindProcessor BatchKind = "processor"
	// BatchKindParse matches IDs returned by /parse_runs/batch (`bpar_`).
	BatchKindParse BatchKind = "parse"
	// BatchKindWorkflow matches IDs returned by /workflow_runs/batch (`batch_`).
	// Workflow batches do NOT support GET /batch_runs/{id}; the server has no
	// public retrieval endpoint for them. Callers must list workflow runs
	// filtered by batchId to track progress.
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

// ErrWorkflowBatchNotRetrievable is returned by GetBatchRun and WaitForBatchRun
// when called with a workflow batch ID. The server has no public retrieval
// endpoint for workflow batches; use ListWorkflowRuns with the BatchID filter
// to poll progress.
var ErrWorkflowBatchNotRetrievable = errors.New("workflow batches (batch_*) cannot be retrieved via /batch_runs/{id}; use 'extend runs list --type workflow --batch <id>' to track progress")
