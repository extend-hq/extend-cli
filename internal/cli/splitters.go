package cli

import (
	"context"
	"encoding/json"
	"fmt"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"
)

// splitterAccessor wires the generic processorAccessor structure
// (declared in processors.go) to the SDK's *Splitters client. See
// processorAccessor for an overview of how the four resource families
// share list/get/create/update plumbing.
func splitterAccessor() processorAccessor[*extend.Splitter, *extend.SplitterSummary, *extend.SplitterVersion, *extend.SplitterVersionSummary] {
	return processorAccessor[*extend.Splitter, *extend.SplitterSummary, *extend.SplitterVersion, *extend.SplitterVersionSummary]{
		noun:       "splitter",
		pluralNoun: "splitters",
		exampleID:  "spl_abc",
		runVerb:    "split",
		bodyDoc:    processorBodyDoc(splitConfigFields),
		rowFields: func(s *extend.SplitterSummary) []string {
			return []string{s.ID, s.Name, relTime(s.CreatedAt)}
		},
		listFn: func(ctx context.Context, c *sdkclient.Client, opts listProcessorsOptions) (any, []*extend.SplitterSummary, string, error) {
			sb, sd, ps, np, err := processorListReqOpts(opts)
			if err != nil {
				return nil, nil, "", err
			}
			r, err := c.Splitters.List(ctx, &extend.SplittersListRequest{
				SortBy: sb, SortDir: sd, MaxPageSize: ps, NextPageToken: np,
			})
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, deref(r.NextPageToken), nil
		},
		getFn: func(ctx context.Context, c *sdkclient.Client, id string) (*extend.Splitter, error) {
			return c.Splitters.Retrieve(ctx, id, &extend.SplittersRetrieveRequest{})
		},
		listVerFn: func(ctx context.Context, c *sdkclient.Client, id string, opts listProcessorVersionsOptions) (any, []*extend.SplitterVersionSummary, string, error) {
			sd, ps, np, err := processorVersionListReqOpts(opts)
			if err != nil {
				return nil, nil, "", err
			}
			r, err := c.SplitterVersions.List(ctx, id, &extend.SplitterVersionsListRequest{
				SortDir: sd, MaxPageSize: ps, NextPageToken: np,
			})
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, deref(r.NextPageToken), nil
		},
		getVerFn: func(ctx context.Context, c *sdkclient.Client, id, ver string) (*extend.SplitterVersion, error) {
			return c.SplitterVersions.Retrieve(ctx, id, ver, &extend.SplitterVersionsRetrieveRequest{})
		},
		verRowFn: func(v *extend.SplitterVersionSummary) []string {
			return []string{v.Version, v.ID, relTime(v.CreatedAt)}
		},
		createFn: func(ctx context.Context, c *sdkclient.Client, body json.RawMessage) (*extend.Splitter, error) {
			var req extend.SplittersCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.Splitters.Create(ctx, &req)
		},
		updateFn: func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (*extend.Splitter, error) {
			var req extend.SplittersUpdateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.Splitters.Update(ctx, id, &req)
		},
		createVerFn: func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (*extend.SplitterVersion, error) {
			var req extend.SplitterVersionsCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.SplitterVersions.Create(ctx, id, &req)
		},
	}
}
