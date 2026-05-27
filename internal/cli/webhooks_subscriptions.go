package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newWebhookSubscriptionsDoc returns the typed documentation for the
// `extend webhooks subscriptions` group. Split out of webhooks.go for
// the same reason as webhooks_endpoints.go: the endpoint and
// subscription subtrees each have 5 leaves and stand alone cleanly.
func newWebhookSubscriptionsDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "subscriptions",
		Summary: "Manage webhook subscriptions (resource-scoped event filters)",
		WhenToUse: `Use this group to bind specific resources (extractors, classifiers,
splitters, workflows) to webhook endpoints with a configured event set.`,
		Details: `A subscription is the {endpoint, resource, events} triple that decides
which events fire for which resources at which destinations.`,
		Subcommands: []*CommandDoc{
			newWebhookSubscriptionsListDoc(app),
			newWebhookSubscriptionsGetDoc(app),
			newWebhookSubscriptionsCreateDoc(app),
			newWebhookSubscriptionsUpdateDoc(app),
			newWebhookSubscriptionsDeleteDoc(app),
		},
	}
}

func newWebhookSubscriptionsListDoc(app *App) *CommandDoc {
	var (
		endpointID string
		resourceID string
		sortDir    string
		limit      int
		maxN       int
		all        bool
		pageToken  string
	)
	return &CommandDoc{
		Use:     "list",
		Summary: "List webhook subscriptions",
		Triggers: []string{
			"list webhook subscriptions",
			"see what an endpoint is subscribed to",
			"find which webhook is bound to a resource",
		},
		WhenToUse: `Use to enumerate webhook subscriptions, optionally filtered by their
endpoint or the resource they're bound to.`,
		Details: `A subscription binds an endpoint to a specific resource (extractor,
classifier, splitter, or workflow) and a set of event types. Use
--endpoint or --resource to filter.

` + paginationGuidance,
		Examples: []Example{
			{Label: "All subscriptions", Cmd: "extend webhooks subscriptions list"},
			{Label: "By endpoint", Cmd: "extend webhooks subscriptions list --endpoint we_abc"},
			{Label: "By resource", Cmd: "extend webhooks subscriptions list --resource ex_abc"},
			{Label: "Next page", Cmd: "extend webhooks subscriptions list --page-token <token-from-previous-response>"},
		},
		SeeAlso: []string{"webhooks subscriptions get", "webhooks subscriptions create", "webhooks endpoints list"},
		Output:  OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			req := &extend.WebhookSubscriptionsListRequest{}
			if endpointID != "" {
				req.WebhookEndpointID = extend.String(endpointID)
			}
			if resourceID != "" {
				req.ResourceID = extend.String(resourceID)
			}
			if sortDir != "" {
				sd, err := extend.NewSortDirFromString(sortDir)
				if err != nil {
					return fmt.Errorf("--sort: %w", err)
				}
				req.SortDir = &sd
			}
			if limit > 0 {
				ps := extend.MaxPageSize(limit)
				req.MaxPageSize = &ps
			}
			if pageToken != "" {
				req.NextPageToken = extend.String(pageToken)
			}

			var rows [][]string
			var pages []any
			for {
				page, err := cli.WebhookSubscriptions.List(cmd.Context(), req)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				for _, s := range page.Data {
					rows = append(rows, []string{s.ID, s.WebhookEndpointID, string(s.ResourceType), s.ResourceID, fmt.Sprintf("%d events", len(s.EnabledEvents)), relTime(s.CreatedAt)})
				}
				next := deref(page.NextPageToken)
				if paginationDone(all, maxN, len(rows), next) {
					break
				}
				req.NextPageToken = extend.String(next)
			}
			rows = capRowsToMax(rows, maxN)
			return renderListForCmd(cmd, app, pages, []string{"id", "endpoint", "type", "resource", "events", "created"}, rows, "No webhook subscriptions.")
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&endpointID, "endpoint", "", "Filter by webhook endpoint ID (we_...)")
			cmd.Flags().StringVar(&resourceID, "resource", "", "Filter by resource ID (extractor/classifier/splitter/workflow)")
			cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
			cmd.Flags().IntVar(&limit, "limit", 20, "Page size used in each API request (advanced)")
			cmd.Flags().IntVar(&maxN, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page)")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&all, "all", false, "Fetch every page (use --max for a bounded fetch)")
		},
	}
}

func newWebhookSubscriptionsGetDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "get <subscription-id>",
		Summary: "Show one webhook subscription",
		Triggers: []string{
			"show one webhook subscription",
			"inspect a webhook subscription configuration",
			"see what events fire for a subscription",
		},
		WhenToUse: `Use to retrieve the full configuration of one subscription: endpoint,
target resource, and the list of enabled event types.`,
		Details: `Returns the full webhook subscription object as JSON.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend webhooks subscriptions get whs_abc"},
		},
		SeeAlso: []string{"webhooks subscriptions list", "webhooks subscriptions update", "webhooks subscriptions delete"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.WebhookSubscriptions.Retrieve(cmd.Context(), args[0], &extend.WebhookSubscriptionsRetrieveRequest{})
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
	}
}

func newWebhookSubscriptionsCreateDoc(app *App) *CommandDoc {
	var (
		endpointID   string
		resourceID   string
		resourceType string
		events       []string
	)
	return &CommandDoc{
		Use:     "create",
		Summary: "Subscribe an endpoint to events for a specific resource",
		Triggers: []string{
			"subscribe an endpoint to webhook events",
			"create a webhook subscription",
			"bind a webhook to a workflow or extractor",
		},
		WhenToUse: `Use to bind an existing endpoint to a specific resource (extractor,
classifier, splitter, or workflow) for a chosen event set.`,
		Details: `The resource type is auto-detected from the resource ID prefix
(ex_=extractor, cl_=classifier, spl_=splitter, workflow_=workflow);
pass --resource-type to override or for unknown prefixes.`,
		Examples: []Example{
			{Label: "Workflow events", Cmd: "extend webhooks subscriptions create --endpoint whe_x --resource workflow_abc --events workflow_run.completed"},
			{Label: "Override resource type", Cmd: "extend webhooks subscriptions create --endpoint whe_x --resource ex_abc --resource-type extractor --events extract_run.processed"},
		},
		Gotchas: []string{
			"--endpoint, --resource, and --events are all required.",
			"Resource type is inferred from the resource ID prefix; unknown prefixes require --resource-type.",
		},
		SeeAlso: []string{"webhooks endpoints create", "webhooks subscriptions list"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			if endpointID == "" {
				return errors.New("--endpoint is required")
			}
			if resourceID == "" {
				return errors.New("--resource is required")
			}
			if len(events) == 0 {
				return errors.New("--events is required")
			}
			if resourceType == "" {
				resourceType = extendx.WebhookSubscriptionResourceTypeFromID(resourceID)
				if resourceType == "" {
					return fmt.Errorf("could not infer resource type from %q; pass --resource-type explicitly (extractor|classifier|splitter|workflow)", resourceID)
				}
			}
			rt, err := extend.NewWebhookSubscriptionResourceTypeFromString(resourceType)
			if err != nil {
				return fmt.Errorf("--resource-type: %w", err)
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.WebhookSubscriptions.Create(cmd.Context(), &extend.WebhookSubscriptionsCreateRequest{
				WebhookEndpointID: endpointID,
				ResourceType:      rt,
				ResourceID:        resourceID,
				EnabledEvents:     toWebhookSubscriptionEvents(splitCSV(events)),
			})
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&endpointID, "endpoint", "", "Webhook endpoint ID (required)")
			cmd.Flags().StringVar(&resourceID, "resource", "", "Resource ID to scope events to, e.g. workflow_xxx (required)")
			cmd.Flags().StringVar(&resourceType, "resource-type", "", "Resource type: extractor|classifier|splitter|workflow (auto-detected from --resource prefix)")
			cmd.Flags().StringArrayVar(&events, "events", nil, "Enabled events (comma-separated or repeated; required)")
		},
	}
}

func newWebhookSubscriptionsUpdateDoc(app *App) *CommandDoc {
	var events []string
	return &CommandDoc{
		Use:     "update <subscription-id>",
		Summary: "Replace the enabled events on a webhook subscription",
		Triggers: []string{
			"update enabled events on a webhook subscription",
			"change which events fire for a subscription",
			"replace the event list on a webhook subscription",
		},
		WhenToUse: `Use to change which events a subscription delivers. The bound endpoint
and resource are immutable; recreate the subscription to change those.`,
		Details: `The server only allows updating the enabledEvents field; the bound
endpoint and resource are immutable. To change endpoint or resource,
delete and recreate the subscription.`,
		Examples: []Example{
			{Label: "Replace events", Cmd: "extend webhooks subscriptions update whs_abc --events extract_run.processed,extract_run.failed"},
		},
		Gotchas: []string{
			"Only enabledEvents is mutable; endpoint and resource are immutable.",
			"--events is required; passing nothing returns an error rather than clearing the list.",
		},
		SeeAlso: []string{"webhooks subscriptions get", "webhooks subscriptions list"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(events) == 0 {
				return errors.New("--events is required (server only allows updating enabledEvents)")
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.WebhookSubscriptions.Update(cmd.Context(), args[0], &extend.WebhookSubscriptionsUpdateRequest{
				EnabledEvents: toWebhookSubscriptionEvents(splitCSV(events)),
			})
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringArrayVar(&events, "events", nil, "Replacement enabled events (comma-separated or repeated; required)")
		},
	}
}

func newWebhookSubscriptionsDeleteDoc(app *App) *CommandDoc {
	var yes bool
	return &CommandDoc{
		Use:     "delete <subscription-id>",
		Summary: "Delete a webhook subscription",
		Triggers: []string{
			"delete a webhook subscription",
			"remove a webhook event binding",
			"stop a webhook from firing for a resource",
		},
		WhenToUse: `Use to remove the binding between an endpoint and a resource. The
endpoint itself is left in place; only this subscription is deleted.`,
		Details: `After deletion, no further events for that resource fire on this
endpoint.

Prompts for confirmation when stdin is a TTY; pass --yes to skip the
prompt (required in non-interactive scripts).`,
		Examples: []Example{
			{Label: "With prompt", Cmd: "extend webhooks subscriptions delete whs_abc"},
			{Label: "Skip confirmation", Cmd: "extend webhooks subscriptions delete whs_abc --yes"},
		},
		Gotchas: []string{
			"The endpoint stays; only this resource binding is removed.",
			"Without --yes in non-TTY contexts, the command refuses to delete.",
		},
		SeeAlso: []string{"webhooks subscriptions list", "webhooks endpoints get"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteWithConfirm(cmd.Context(), app, "webhook subscription", args[0], yes,
				func(ctx context.Context, id string) error {
					c, err := app.NewClient()
					if err != nil {
						return err
					}
					_, err = c.WebhookSubscriptions.Delete(ctx, id, &extend.WebhookSubscriptionsDeleteRequest{})
					return err
				})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
		},
	}
}

// toWebhookSubscriptionEvents is the subscriptions-side equivalent of
// toWebhookEventTypes (in webhooks_endpoints.go). Kept separate because
// the SDK uses distinct typed slices for endpoint events vs.
// subscription events.
func toWebhookSubscriptionEvents(in []string) []extend.WebhookSubscriptionEventType {
	if len(in) == 0 {
		return nil
	}
	out := make([]extend.WebhookSubscriptionEventType, len(in))
	for i, s := range in {
		out[i] = extend.WebhookSubscriptionEventType(s)
	}
	return out
}
