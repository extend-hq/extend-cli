package cli

import (
	"context"
	"encoding/json"
	"fmt"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"
)

// workflowAccessor wires the generic processorAccessor structure
// (declared in processors.go) to the SDK's *Workflows client. See
// processorAccessor for an overview of how the four resource families
// share list/get/create/update plumbing.
//
// Workflows differ from the three processor families in two places:
// Workflows.Retrieve and WorkflowVersions.Retrieve take no request
// struct (the workspace override isn't exposed for these endpoints).
func workflowAccessor() processorAccessor[*extend.Workflow, *extend.WorkflowSummary, *extend.WorkflowVersion, *extend.WorkflowVersionSummary] {
	return processorAccessor[*extend.Workflow, *extend.WorkflowSummary, *extend.WorkflowVersion, *extend.WorkflowVersionSummary]{
		noun:       "workflow",
		pluralNoun: "workflows",
		exampleID:  "workflow_abc",
		runVerb:    "workflows run",
		bodyDoc:    workflowBodyDoc,
		rowFields: func(w *extend.WorkflowSummary) []string {
			return []string{w.ID, w.Name, relTime(w.CreatedAt)}
		},
		listFn: func(ctx context.Context, c *sdkclient.Client, opts listProcessorsOptions) (any, []*extend.WorkflowSummary, string, error) {
			sb, sd, ps, np, err := processorListReqOpts(opts)
			if err != nil {
				return nil, nil, "", err
			}
			r, err := c.Workflows.List(ctx, &extend.WorkflowsListRequest{
				SortBy: sb, SortDir: sd, MaxPageSize: ps, NextPageToken: np,
			})
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, deref(r.NextPageToken), nil
		},
		getFn: func(ctx context.Context, c *sdkclient.Client, id string) (*extend.Workflow, error) {
			// Workflows.Retrieve takes no request struct (no workspace
			// override path on this endpoint) — unlike the processor
			// resource families.
			return c.Workflows.Retrieve(ctx, id)
		},
		listVerFn: func(ctx context.Context, c *sdkclient.Client, id string, opts listProcessorVersionsOptions) (any, []*extend.WorkflowVersionSummary, string, error) {
			sd, ps, np, err := processorVersionListReqOpts(opts)
			if err != nil {
				return nil, nil, "", err
			}
			r, err := c.WorkflowVersions.List(ctx, id, &extend.WorkflowVersionsListRequest{
				SortDir: sd, MaxPageSize: ps, NextPageToken: np,
			})
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, deref(r.NextPageToken), nil
		},
		getVerFn: func(ctx context.Context, c *sdkclient.Client, id, ver string) (*extend.WorkflowVersion, error) {
			return c.WorkflowVersions.Retrieve(ctx, id, ver)
		},
		verRowFn: func(v *extend.WorkflowVersionSummary) []string {
			return []string{v.Version, v.ID, relTime(v.CreatedAt)}
		},
		createFn: func(ctx context.Context, c *sdkclient.Client, body json.RawMessage) (*extend.Workflow, error) {
			var req extend.WorkflowsCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.Workflows.Create(ctx, &req)
		},
		updateFn: func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (*extend.Workflow, error) {
			var req extend.WorkflowsUpdateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.Workflows.Update(ctx, id, &req)
		},
		createVerFn: func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (*extend.WorkflowVersion, error) {
			var req extend.WorkflowVersionsCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode body: %w", err)
			}
			return c.WorkflowVersions.Create(ctx, id, &req)
		},
	}
}

// newWorkflowsDoc assembles the full `extend workflows` group: the
// generic resource accessor (list/get/create/update/versions) plus the
// run launcher (`workflows run`, with `workflows run batch`) and the
// typed workflow runs subgroup (`workflows runs get|list|watch|cancel|
// delete|update`). Workflow batches have no retrieval endpoint, so
// there is deliberately no `workflows batches` group; track a batch
// with `workflows runs list --batch <id>`.
func newWorkflowsDoc(app *App) *CommandDoc {
	doc := workflowAccessor().doc(app)
	doc.Summary = "Run, inspect, and manage workflows"
	doc.WhenToUse = `Use these commands to run workflows on documents, follow their runs,
and discover, inspect, create, update, and version workflows in the
workspace.`
	doc.Subcommands = append(doc.Subcommands,
		newWorkflowsRunDoc(app),
		workflowRunsSpec().doc(app),
	)
	return doc
}
