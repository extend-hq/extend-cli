package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// EvaluationEntity is the embedded processor reference inside an
// EvaluationSet/EvaluationSetRun. The four processor types share the same
// shape ({object, id, name, createdAt, updatedAt}); we model it as a single
// type and let `object` discriminate.
type EvaluationEntity struct {
	Object    string `json:"object"`
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type EvaluationSet struct {
	ID          string            `json:"id"`
	Object      string            `json:"object"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Entity      *EvaluationEntity `json:"entity,omitempty"`
	CreatedAt   string            `json:"createdAt,omitempty"`
	UpdatedAt   string            `json:"updatedAt,omitempty"`
}

// EvaluationItem is the response shape for /evaluation_sets/{set}/items
// (single GET, list, and update). The server's `PublicEvaluationSetItem`
// is `{object, id, evaluationSetId, file, expectedOutput}` — no createdAt/
// updatedAt. The list endpoint emits a slimmer summary `{object, id, file}`,
// but the same struct decodes both because the extra fields are omitempty.
//
// expectedOutput is a discriminated union (extract: {value}, classify:
// {id, type, confidence}, split: {splits[]}); we keep it raw so callers can
// pass it through and `--jq` into it.
type EvaluationItem struct {
	ID              string          `json:"id"`
	Object          string          `json:"object"`
	EvaluationSetID string          `json:"evaluationSetId,omitempty"`
	File            *File           `json:"file,omitempty"`
	ExpectedOutput  json.RawMessage `json:"expectedOutput,omitempty"`
}

// EvaluationItemsCreateResponse wraps the bulk-create response. The server
// returns `{evaluationSetItems: [...]}` rather than a bare item or a
// `{data:[...]}` envelope, so the wrapper key must be modelled explicitly.
type EvaluationItemsCreateResponse struct {
	EvaluationSetItems []EvaluationItem `json:"evaluationSetItems"`
}

// EvaluationRunMetrics covers the entire `metrics` block on an evaluation
// set run (apps/api/.../types.ts:606). All seven numeric fields are optional
// — they are populated only after the run reaches PROCESSED.
type EvaluationRunMetrics struct {
	NumFiles      *int     `json:"numFiles,omitempty"`
	NumPages      *int     `json:"numPages,omitempty"`
	MeanLatencyMs *float64 `json:"meanLatencyMs,omitempty"`
	P50LatencyMs  *float64 `json:"p50LatencyMs,omitempty"`
	P90LatencyMs  *float64 `json:"p90LatencyMs,omitempty"`
	P95LatencyMs  *float64 `json:"p95LatencyMs,omitempty"`
	P99LatencyMs  *float64 `json:"p99LatencyMs,omitempty"`
}

// EvaluationRun is the response shape for GET /evaluation_set_runs/{id}.
// The route key is `/evaluation_set_runs/`, not `/evaluation_sets/{set}/runs/`.
type EvaluationRun struct {
	ID            string                `json:"id"`
	Object        string                `json:"object"`
	Entity        *EvaluationEntity     `json:"entity,omitempty"`
	EntityVersion json.RawMessage       `json:"entityVersion,omitempty"`
	Status        string                `json:"status,omitempty"`
	Metrics       *EvaluationRunMetrics `json:"metrics,omitempty"`
	CreatedAt     string                `json:"createdAt,omitempty"`
	UpdatedAt     string                `json:"updatedAt,omitempty"`
}

func (c *Client) ListEvaluationSets(ctx context.Context, opts ListProcessorsOptions) (*ListResponse[*EvaluationSet], error) {
	var out ListResponse[*EvaluationSet]
	if err := c.getJSON(ctx, "/evaluation_sets"+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEvaluationSet(ctx context.Context, id string) (*EvaluationSet, error) {
	var s EvaluationSet
	if err := c.getJSON(ctx, "/evaluation_sets/"+id, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) CreateEvaluationSet(ctx context.Context, body json.RawMessage) (*EvaluationSet, error) {
	var s EvaluationSet
	if err := c.postRaw(ctx, "/evaluation_sets", body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) ListEvaluationItems(ctx context.Context, setID string, opts ListProcessorsOptions) (*ListResponse[*EvaluationItem], error) {
	var out ListResponse[*EvaluationItem]
	if err := c.getJSON(ctx, "/evaluation_sets/"+setID+"/items"+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEvaluationItem(ctx context.Context, setID, itemID string) (*EvaluationItem, error) {
	var item EvaluationItem
	if err := c.getJSON(ctx, "/evaluation_sets/"+setID+"/items/"+itemID, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// CreateEvaluationItems POSTs a bulk-create request and returns the typed
// `evaluationSetItems` array from the server. Server schema is
// `{items: [{fileId, expectedOutput}]}`; this method takes a raw body so the
// caller can pass that envelope directly.
func (c *Client) CreateEvaluationItems(ctx context.Context, setID string, body json.RawMessage) (*EvaluationItemsCreateResponse, error) {
	var resp EvaluationItemsCreateResponse
	if err := c.postRaw(ctx, "/evaluation_sets/"+setID+"/items", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateEvaluationItem updates a single item via POST (NOT PATCH — the server
// route is POST /evaluation_sets/{setId}/items/{itemId}).
func (c *Client) UpdateEvaluationItem(ctx context.Context, setID, itemID string, body json.RawMessage) (*EvaluationItem, error) {
	var item EvaluationItem
	if err := c.postRaw(ctx, "/evaluation_sets/"+setID+"/items/"+itemID, body, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (c *Client) DeleteEvaluationItem(ctx context.Context, setID, itemID string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/evaluation_sets/"+setID+"/items/"+itemID, nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// GetEvaluationRun fetches an evaluation run by ID. The path is
// `/evaluation_set_runs/{id}`, NOT `/evaluation_sets/{set}/runs/{id}` — the
// latter does not exist on 2026-02-09 and 404s. Note that the eval-set ID
// is therefore unused; callers are expected to track the run-set association
// via the evalSet they came from.
func (c *Client) GetEvaluationRun(ctx context.Context, runID string) (*EvaluationRun, error) {
	var run EvaluationRun
	if err := c.getJSON(ctx, "/evaluation_set_runs/"+runID, &run); err != nil {
		return nil, err
	}
	return &run, nil
}
