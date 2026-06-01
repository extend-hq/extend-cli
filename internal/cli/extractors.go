package cli

import (
	"context"
	"encoding/json"
	"fmt"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"
)

// extractorAccessor wires the generic processorAccessor structure
// (declared in processors.go) to the SDK's *Extractors client. See
// processorAccessor for an overview of how the four resource families
// share list/get/create/update plumbing.
func extractorAccessor() processorAccessor[*extend.Extractor, *extend.ExtractorSummary, *extend.ExtractorVersion, *extend.ExtractorVersionSummary] {
	return processorAccessor[*extend.Extractor, *extend.ExtractorSummary, *extend.ExtractorVersion, *extend.ExtractorVersionSummary]{
		noun:       "extractor",
		pluralNoun: "extractors",
		exampleID:  "ex_abc",
		runVerb:    "extract",
		bodyDoc:    processorBodyDoc(extractConfigFields),
		rowFields: func(e *extend.ExtractorSummary) []string {
			return []string{e.ID, e.Name, relTime(e.CreatedAt)}
		},
		listFn: func(ctx context.Context, c *sdkclient.Client, opts listProcessorsOptions) (any, []*extend.ExtractorSummary, string, error) {
			sb, sd, ps, np, err := processorListReqOpts(opts)
			if err != nil {
				return nil, nil, "", err
			}
			r, err := c.Extractors.List(ctx, &extend.ExtractorsListRequest{
				SortBy: sb, SortDir: sd, MaxPageSize: ps, NextPageToken: np,
			})
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, deref(r.NextPageToken), nil
		},
		getFn: func(ctx context.Context, c *sdkclient.Client, id string) (*extend.Extractor, error) {
			return c.Extractors.Retrieve(ctx, id, &extend.ExtractorsRetrieveRequest{})
		},
		listVerFn: func(ctx context.Context, c *sdkclient.Client, id string, opts listProcessorVersionsOptions) (any, []*extend.ExtractorVersionSummary, string, error) {
			sd, ps, np, err := processorVersionListReqOpts(opts)
			if err != nil {
				return nil, nil, "", err
			}
			r, err := c.ExtractorVersions.List(ctx, id, &extend.ExtractorVersionsListRequest{
				SortDir: sd, MaxPageSize: ps, NextPageToken: np,
			})
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, deref(r.NextPageToken), nil
		},
		getVerFn: func(ctx context.Context, c *sdkclient.Client, id, ver string) (*extend.ExtractorVersion, error) {
			return c.ExtractorVersions.Retrieve(ctx, id, ver, &extend.ExtractorVersionsRetrieveRequest{})
		},
		verRowFn: func(v *extend.ExtractorVersionSummary) []string {
			return []string{v.Version, v.ID, relTime(v.CreatedAt)}
		},
		createFn: func(ctx context.Context, c *sdkclient.Client, body json.RawMessage) (*extend.Extractor, error) {
			var req extend.ExtractorsCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.Extractors.Create(ctx, &req)
		},
		updateFn: func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (*extend.Extractor, error) {
			var req extend.ExtractorsUpdateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.Extractors.Update(ctx, id, &req)
		},
		createVerFn: func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (*extend.ExtractorVersion, error) {
			var req extend.ExtractorVersionsCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.ExtractorVersions.Create(ctx, id, &req)
		},
	}
}
