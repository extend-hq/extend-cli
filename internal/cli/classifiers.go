package cli

import (
	"context"
	"encoding/json"
	"fmt"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"
)

// classifierAccessor wires the generic processorAccessor structure
// (declared in processors.go) to the SDK's *Classifiers client. See
// processorAccessor for an overview of how the four resource families
// share list/get/create/update plumbing.
func classifierAccessor() processorAccessor[*extend.Classifier, *extend.ClassifierSummary, *extend.ClassifierVersion, *extend.ClassifierVersionSummary] {
	return processorAccessor[*extend.Classifier, *extend.ClassifierSummary, *extend.ClassifierVersion, *extend.ClassifierVersionSummary]{
		noun:       "classifier",
		pluralNoun: "classifiers",
		exampleID:  "cl_abc",
		runVerb:    "classify",
		bodyDoc:    processorBodyDoc(classifyConfigFields),
		rowFields: func(c *extend.ClassifierSummary) []string {
			return []string{c.ID, c.Name, relTime(c.CreatedAt)}
		},
		listFn: func(ctx context.Context, c *sdkclient.Client, opts listProcessorsOptions) (any, []*extend.ClassifierSummary, string, error) {
			sb, sd, ps, np, err := processorListReqOpts(opts)
			if err != nil {
				return nil, nil, "", err
			}
			r, err := c.Classifiers.List(ctx, &extend.ClassifiersListRequest{
				SortBy: sb, SortDir: sd, MaxPageSize: ps, NextPageToken: np,
			})
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, deref(r.NextPageToken), nil
		},
		getFn: func(ctx context.Context, c *sdkclient.Client, id string) (*extend.Classifier, error) {
			return c.Classifiers.Retrieve(ctx, id, &extend.ClassifiersRetrieveRequest{})
		},
		listVerFn: func(ctx context.Context, c *sdkclient.Client, id string, opts listProcessorVersionsOptions) (any, []*extend.ClassifierVersionSummary, string, error) {
			sd, ps, np, err := processorVersionListReqOpts(opts)
			if err != nil {
				return nil, nil, "", err
			}
			r, err := c.ClassifierVersions.List(ctx, id, &extend.ClassifierVersionsListRequest{
				SortDir: sd, MaxPageSize: ps, NextPageToken: np,
			})
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, deref(r.NextPageToken), nil
		},
		getVerFn: func(ctx context.Context, c *sdkclient.Client, id, ver string) (*extend.ClassifierVersion, error) {
			return c.ClassifierVersions.Retrieve(ctx, id, ver, &extend.ClassifierVersionsRetrieveRequest{})
		},
		verRowFn: func(v *extend.ClassifierVersionSummary) []string {
			return []string{v.Version, v.ID, relTime(v.CreatedAt)}
		},
		createFn: func(ctx context.Context, c *sdkclient.Client, body json.RawMessage) (*extend.Classifier, error) {
			var req extend.ClassifiersCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.Classifiers.Create(ctx, &req)
		},
		updateFn: func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (*extend.Classifier, error) {
			var req extend.ClassifiersUpdateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.Classifiers.Update(ctx, id, &req)
		},
		createVerFn: func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (*extend.ClassifierVersion, error) {
			var req extend.ClassifierVersionsCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.ClassifierVersions.Create(ctx, id, &req)
		},
	}
}
