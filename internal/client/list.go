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

type ListRunsOptions struct {
	Status    string
	BatchID   string
	Limit     int
	PageToken string
	SortBy    string
	SortDir   string
}

func (o ListRunsOptions) query() string {
	v := url.Values{}
	if o.Status != "" {
		v.Set("status", o.Status)
	}
	if o.BatchID != "" {
		v.Set("batchId", o.BatchID)
	}
	if o.Limit > 0 {
		v.Set("maxPageSize", strconv.Itoa(o.Limit))
	}
	if o.PageToken != "" {
		v.Set("nextPageToken", o.PageToken)
	}
	if o.SortBy != "" {
		v.Set("sortBy", o.SortBy)
	}
	if o.SortDir != "" {
		v.Set("sortDir", o.SortDir)
	}
	if encoded := v.Encode(); encoded != "" {
		return "?" + encoded
	}
	return ""
}

func listRuns[T any](ctx context.Context, c *Client, path string, opts ListRunsOptions) (*ListResponse[T], error) {
	var out ListResponse[T]
	if err := c.getJSON(ctx, path+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListExtractRuns(ctx context.Context, opts ListRunsOptions) (*ListResponse[*ExtractRun], error) {
	return listRuns[*ExtractRun](ctx, c, "/extract_runs", opts)
}

func (c *Client) ListParseRuns(ctx context.Context, opts ListRunsOptions) (*ListResponse[*ParseRun], error) {
	return listRuns[*ParseRun](ctx, c, "/parse_runs", opts)
}

func (c *Client) ListClassifyRuns(ctx context.Context, opts ListRunsOptions) (*ListResponse[*ClassifyRun], error) {
	return listRuns[*ClassifyRun](ctx, c, "/classify_runs", opts)
}

func (c *Client) ListSplitRuns(ctx context.Context, opts ListRunsOptions) (*ListResponse[*SplitRun], error) {
	return listRuns[*SplitRun](ctx, c, "/split_runs", opts)
}
