package client

import (
	"context"
	"net/url"
	"strconv"
)

type ListResponse[T any] struct {
	Object        string `json:"object"`
	Data          []T    `json:"data"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// setIf adds key=val to v only when val is non-empty. Most list endpoints
// distinguish "field omitted" from "field present but empty"; default-empty
// strings should never go on the wire.
func setIf(v url.Values, key, val string) {
	if val != "" {
		v.Set(key, val)
	}
}

// encodeQuery returns "?<encoded>" or "" if no params were set, so callers
// can blindly append the result to a path.
func encodeQuery(v url.Values) string {
	if encoded := v.Encode(); encoded != "" {
		return "?" + encoded
	}
	return ""
}

// ListRunsOptions is shared across all five run-list endpoints, but the
// per-endpoint server schemas are not identical: parse_runs uses a different
// page-size param name and rejects sortBy/sortDir; workflow_runs has no
// source/sourceId; the processor-id wire param name varies (extractorId vs
// classifierId vs splitterId vs workflowId). query() takes the run kind so
// it can render the right wire shape and silently drop fields that don't
// apply to a given kind.
type ListRunsOptions struct {
	Status           string
	ProcessorID      string // extractor/classifier/splitter/workflow ID; not applicable to parse runs
	BatchID          string
	Source           string // run-creation source enum; not applicable to workflow runs
	SourceID         string // resource ID that created the run; not applicable to workflow runs
	FileNameContains string
	Limit            int
	PageToken        string
	SortBy           string // updatedAt|createdAt; not applicable to parse runs
	SortDir          string // asc|desc; not applicable to parse runs
}

func (o ListRunsOptions) query(kind RunKind) string {
	v := url.Values{}
	setIf(v, "status", o.Status)
	setIf(v, "batchId", o.BatchID)
	setIf(v, "fileNameContains", o.FileNameContains)
	setIf(v, "nextPageToken", o.PageToken)

	// Page-size parameter name differs only on /parse_runs (`limit`); every
	// other run endpoint uses `maxPageSize`. Sending `maxPageSize` to
	// /parse_runs gets silently dropped by the server's Zod schema, which
	// is why --limit was previously a no-op for parse runs.
	pageKey := "maxPageSize"
	if kind == KindParse {
		pageKey = "limit"
	}
	if o.Limit > 0 {
		v.Set(pageKey, strconv.Itoa(o.Limit))
	}

	switch kind {
	case KindExtract:
		setIf(v, "extractorId", o.ProcessorID)
	case KindClassify:
		setIf(v, "classifierId", o.ProcessorID)
	case KindSplit:
		setIf(v, "splitterId", o.ProcessorID)
	case KindWorkflow:
		setIf(v, "workflowId", o.ProcessorID)
		// KindParse intentionally omitted — parse runs have no processor.
	}

	if kind != KindWorkflow {
		// Workflow runs have no source/sourceId per server schema.
		setIf(v, "source", o.Source)
		setIf(v, "sourceId", o.SourceID)
	}

	if kind != KindParse {
		// Parse runs have no sortBy/sortDir per server schema.
		setIf(v, "sortBy", o.SortBy)
		setIf(v, "sortDir", o.SortDir)
	}

	return encodeQuery(v)
}

func listRuns[T any](ctx context.Context, c *Client, kind RunKind, path string, opts ListRunsOptions) (*ListResponse[T], error) {
	var out ListResponse[T]
	if err := c.getJSON(ctx, path+opts.query(kind), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListExtractRuns(ctx context.Context, opts ListRunsOptions) (*ListResponse[*ExtractRun], error) {
	return listRuns[*ExtractRun](ctx, c, KindExtract, "/extract_runs", opts)
}

func (c *Client) ListParseRuns(ctx context.Context, opts ListRunsOptions) (*ListResponse[*ParseRun], error) {
	return listRuns[*ParseRun](ctx, c, KindParse, "/parse_runs", opts)
}

func (c *Client) ListClassifyRuns(ctx context.Context, opts ListRunsOptions) (*ListResponse[*ClassifyRun], error) {
	return listRuns[*ClassifyRun](ctx, c, KindClassify, "/classify_runs", opts)
}

func (c *Client) ListSplitRuns(ctx context.Context, opts ListRunsOptions) (*ListResponse[*SplitRun], error) {
	return listRuns[*SplitRun](ctx, c, KindSplit, "/split_runs", opts)
}
