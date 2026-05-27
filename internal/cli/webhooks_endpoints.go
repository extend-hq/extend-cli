package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"

	"github.com/extend-hq/extend-cli/internal/output"
)

// newWebhookEndpointsDoc returns the typed documentation for the
// `extend webhooks endpoints` group. Split out of webhooks.go so the
// endpoint and subscription subtrees can evolve independently; both
// have 5 leaves and roughly the same size.
func newWebhookEndpointsDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "endpoints",
		Summary: "Manage webhook endpoints (the receiving URLs)",
		WhenToUse: `Use this group to list, inspect, create, update, or delete the
receiving URLs that webhook events deliver to.`,
		Details: `An endpoint is a destination URL plus its delivery configuration
(headers, payload format, API version). Subscriptions bind endpoints to
resources.`,
		Subcommands: []*CommandDoc{
			newWebhookEndpointsListDoc(app),
			newWebhookEndpointsGetDoc(app),
			newWebhookEndpointsCreateDoc(app),
			newWebhookEndpointsUpdateDoc(app),
			newWebhookEndpointsDeleteDoc(app),
		},
	}
}

func newWebhookEndpointsListDoc(app *App) *CommandDoc {
	var (
		status    string
		sortDir   string
		limit     int
		maxN      int
		all       bool
		pageToken string
	)
	return &CommandDoc{
		Use:     "list",
		Summary: "List webhook endpoints",
		Triggers: []string{
			"list webhook endpoints in the workspace",
			"find configured webhook receiver urls",
			"page through webhook endpoints",
		},
		WhenToUse: `Use to enumerate the receiving URLs configured in the workspace,
optionally filtered by status.`,
		Details: `Endpoints are the recipients of webhook events; subscriptions bind an
endpoint to a specific resource and event set. Use 'extend webhooks
subscriptions list' to see what each endpoint is subscribed to.

` + paginationGuidance,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend webhooks endpoints list"},
			{Label: "Only enabled", Cmd: "extend webhooks endpoints list --status enabled"},
			{Label: "Next page", Cmd: "extend webhooks endpoints list --page-token <token-from-previous-response>"},
		},
		SeeAlso: []string{"webhooks endpoints get", "webhooks endpoints create", "webhooks subscriptions list"},
		Output:  OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			req := &extend.WebhookEndpointsListRequest{}
			if status != "" {
				s, err := extend.NewWebhookEndpointStatusFromString(status)
				if err != nil {
					return fmt.Errorf("--status: %w", err)
				}
				req.Status = &s
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
				page, err := cli.WebhookEndpoints.List(cmd.Context(), req)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				for _, ep := range page.Data {
					rows = append(rows, []string{ep.ID, ep.Name, truncate(ep.URL, 40), relTime(ep.CreatedAt)})
				}
				next := deref(page.NextPageToken)
				if paginationDone(all, maxN, len(rows), next) {
					break
				}
				req.NextPageToken = extend.String(next)
			}
			rows = capRowsToMax(rows, maxN)
			return renderListForCmd(cmd, app, pages, []string{"id", "name", "url", "created"}, rows, "No webhook endpoints.")
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&status, "status", "", "Filter by status: enabled|disabled")
			cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
			cmd.Flags().IntVar(&limit, "limit", 20, "Page size used in each API request (advanced)")
			cmd.Flags().IntVar(&maxN, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page)")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&all, "all", false, "Fetch every page (use --max for a bounded fetch)")
		},
	}
}

func newWebhookEndpointsGetDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "get <endpoint-id>",
		Summary: "Show one webhook endpoint",
		Triggers: []string{
			"show one webhook endpoint",
			"inspect a webhook receiver url",
			"see endpoint configuration headers and payload format",
		},
		WhenToUse: `Use to retrieve the full configuration of one endpoint: URL, name,
status, enabled events, custom headers, payload format, api-version.`,
		Details: `The signing secret is not included in this response; it is shown only once
at creation time and cannot be retrieved later. To rotate the secret,
delete and recreate the endpoint.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend webhooks endpoints get whe_abc"},
			{Label: "Just enabled events", Cmd: "extend webhooks endpoints get whe_abc --jq '.enabledEvents' -o json"},
		},
		Gotchas: []string{
			"The signing secret is shown only once at creation; this command does not return it.",
		},
		SeeAlso: []string{"webhooks endpoints list", "webhooks endpoints update", "webhooks endpoints delete", "webhooks subscriptions list"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			ep, err := cli.WebhookEndpoints.Retrieve(cmd.Context(), args[0], &extend.WebhookEndpointsRetrieveRequest{})
			if err != nil {
				return err
			}
			return renderWithDefault(app, ep, output.FormatJSON)
		},
	}
}

// webhookAdvancedFlags collects the small set of flags that map to
// WebhookAdvancedOptions on the wire. Both create and update use these.
type webhookAdvancedFlags struct {
	headers           []string
	payloadFormat     string
	urlThresholdBytes int
}

func (f *webhookAdvancedFlags) attach(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.headers, "header", nil, "Custom delivery header as key=value (repeatable)")
	cmd.Flags().StringVar(&f.payloadFormat, "payload-format", "", "Payload delivery format: json (default) or url (link-only payload)")
	cmd.Flags().IntVar(&f.urlThresholdBytes, "url-threshold-bytes", 0, "When --payload-format=url, swap to URL delivery once the body exceeds this many bytes (server default if 0)")
}

// build returns nil when no flags were set, so omitempty drops the
// whole advancedOptions object on the wire.
func (f *webhookAdvancedFlags) build() (*extend.WebhookAdvancedOptions, error) {
	if len(f.headers) == 0 && f.payloadFormat == "" && f.urlThresholdBytes == 0 {
		return nil, nil
	}
	opts := &extend.WebhookAdvancedOptions{}
	if len(f.headers) > 0 {
		hdrs, err := parseKVPairs("--header", f.headers)
		if err != nil {
			return nil, err
		}
		opts.Headers = hdrs
	}
	if f.payloadFormat != "" {
		pf, err := extend.NewWebhookPayloadFormatFromString(f.payloadFormat)
		if err != nil {
			return nil, fmt.Errorf("--payload-format: %w", err)
		}
		opts.Payload = &extend.WebhookPayloadOptions{Format: pf}
		if f.urlThresholdBytes > 0 {
			if pf != extend.WebhookPayloadFormatURL {
				return nil, errors.New("--url-threshold-bytes only applies when --payload-format=url")
			}
			opts.Payload.URLThresholdBytes = &f.urlThresholdBytes
		}
	} else if f.urlThresholdBytes > 0 {
		return nil, errors.New("--url-threshold-bytes requires --payload-format=url")
	}
	return opts, nil
}

func newWebhookEndpointsCreateDoc(app *App) *CommandDoc {
	var (
		url        string
		name       string
		events     []string
		apiVersion string
		disable    bool
		advanced   webhookAdvancedFlags
	)
	return &CommandDoc{
		Use:     "create",
		Summary: "Create a webhook endpoint",
		Triggers: []string{
			"create a webhook endpoint",
			"register a webhook receiver url",
			"add a new webhook receiver to extend",
		},
		WhenToUse: `Use to register a new receiving URL with Extend. The signing secret is
shown only in the response and cannot be retrieved later — store it.`,
		Details: `Custom delivery headers (e.g. shared secrets, tenant identifiers) can be
attached via --header key=value. The default payload format is the full
JSON event body; pass --payload-format=url to swap to a link-only payload
once the body exceeds --url-threshold-bytes.

Use 'extend webhooks verify' to validate incoming payloads against the
returned signing secret.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend webhooks endpoints create --url https://x.com/hook --name prod --events extract_run.processed,extract_run.failed"},
			{Label: "With custom headers", Cmd: "extend webhooks endpoints create --url https://x.com/hook --name prod --events extract_run.processed --header X-Tenant=acme --header X-Token=$WT"},
		},
		Gotchas: []string{
			"The signing secret in the response is shown ONLY ONCE; store it securely.",
			"--url, --name, and --events are all required.",
			"--url-threshold-bytes only applies when --payload-format=url.",
		},
		SeeAlso: []string{"webhooks endpoints list", "webhooks subscriptions create", "webhooks verify"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				return errors.New("--url is required")
			}
			if name == "" {
				return errors.New("--name is required")
			}
			if len(events) == 0 {
				return errors.New("--events is required")
			}
			adv, err := advanced.build()
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			req := &extend.WebhookEndpointsCreateRequest{
				URL:             url,
				Name:            name,
				EnabledEvents:   toWebhookEventTypes(splitCSV(events)),
				APIVersion:      apiVersion,
				AdvancedOptions: adv,
			}
			if disable {
				s := extend.WebhookEndpointStatusDisabled
				req.Status = &s
			}
			ep, err := cli.WebhookEndpoints.Create(cmd.Context(), req)
			if err != nil {
				return err
			}
			if err := renderWithDefault(app, ep, output.FormatJSON); err != nil {
				return err
			}
			if app.IO.IsStderrTTY() && ep.SigningSecret != "" {
				fmt.Fprintln(app.IO.ErrOut)
				fmt.Fprintln(app.IO.ErrOut, "Save the signingSecret above; it is not retrievable later.")
			}
			return nil
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&url, "url", "", "Receiving URL (required)")
			cmd.Flags().StringVar(&name, "name", "", "Display name (required)")
			cmd.Flags().BoolVar(&disable, "disabled", false, "Create the endpoint in 'disabled' state (defaults to 'enabled')")
			cmd.Flags().StringArrayVar(&events, "events", nil, "Enabled events (comma-separated or repeated; required)")
			cmd.Flags().StringVar(&apiVersion, "api-version", defaultAPIVersion, "API version for events")
			advanced.attach(cmd)
		},
	}
}

// toWebhookEventTypes casts a []string of event names into the SDK's
// typed []WebhookEndpointEventType slice. The SDK alias is a `string`
// underlying type so this is a zero-cost cast; we keep it in a helper
// because the conversion is needed in create + update.
func toWebhookEventTypes(in []string) []extend.WebhookEndpointEventType {
	if len(in) == 0 {
		return nil
	}
	out := make([]extend.WebhookEndpointEventType, len(in))
	for i, s := range in {
		out[i] = extend.WebhookEndpointEventType(s)
	}
	return out
}

func newWebhookEndpointsUpdateDoc(app *App) *CommandDoc {
	var (
		url      string
		name     string
		events   []string
		enable   bool
		disable  bool
		advanced webhookAdvancedFlags
	)
	return &CommandDoc{
		Use:     "update <endpoint-id>",
		Summary: "Update mutable fields on a webhook endpoint",
		Triggers: []string{
			"update a webhook endpoint",
			"change the url or events on a webhook endpoint",
			"enable or disable a webhook endpoint",
		},
		WhenToUse: `Use to change mutable fields on an existing endpoint. Pass only the
flags you want to change; omitted fields are left untouched.`,
		Details: `The api-version field cannot be updated; create a new endpoint to
migrate.

Setting --header replaces the entire custom-headers map; pass each header
to keep, plus any new ones. The server does not support clearing all
custom headers via --header. To remove every custom header, recreate the
endpoint without --header instead.`,
		Examples: []Example{
			{Label: "Change URL", Cmd: "extend webhooks endpoints update whe_abc --url https://new.example.com/hook"},
			{Label: "Disable", Cmd: "extend webhooks endpoints update whe_abc --disable"},
			{Label: "Replace events", Cmd: "extend webhooks endpoints update whe_abc --events extract_run.processed,extract_run.failed"},
		},
		Gotchas: []string{
			"--enable and --disable are mutually exclusive.",
			"api-version is immutable; create a new endpoint to migrate.",
			"--header replaces the entire custom-headers map; include each header to keep.",
		},
		SeeAlso: []string{"webhooks endpoints get", "webhooks endpoints list"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if enable && disable {
				return errors.New("--enable and --disable are mutually exclusive")
			}
			adv, err := advanced.build()
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			req := &extend.WebhookEndpointsUpdateRequest{
				AdvancedOptions: adv,
			}
			if url != "" {
				req.URL = extend.String(url)
			}
			if name != "" {
				req.Name = extend.String(name)
			}
			if enable {
				s := extend.WebhookEndpointStatusEnabled
				req.Status = &s
			} else if disable {
				s := extend.WebhookEndpointStatusDisabled
				req.Status = &s
			}
			req.EnabledEvents = toWebhookEventTypes(splitCSV(events))
			ep, err := cli.WebhookEndpoints.Update(cmd.Context(), args[0], req)
			if err != nil {
				return err
			}
			return renderWithDefault(app, ep, output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&url, "url", "", "New receiving URL")
			cmd.Flags().StringVar(&name, "name", "", "New display name")
			cmd.Flags().BoolVar(&enable, "enable", false, "Set status to 'enabled'")
			cmd.Flags().BoolVar(&disable, "disable", false, "Set status to 'disabled'")
			cmd.Flags().StringArrayVar(&events, "events", nil, "Replace enabled events list")
			advanced.attach(cmd)
		},
	}
}

func newWebhookEndpointsDeleteDoc(app *App) *CommandDoc {
	var yes bool
	return &CommandDoc{
		Use:     "delete <endpoint-id>",
		Summary: "Delete a webhook endpoint",
		Triggers: []string{
			"delete a webhook endpoint",
			"remove a webhook receiver url",
			"unregister a webhook destination",
		},
		WhenToUse: `Use to permanently delete an endpoint. All subscriptions bound to it
are deleted along with it; future events for those resources will not
fire on this endpoint or any other.`,
		Details: `Prompts for confirmation when stdin is a TTY; pass --yes to skip the
prompt (required in non-interactive scripts).`,
		Examples: []Example{
			{Label: "With prompt", Cmd: "extend webhooks endpoints delete whe_abc"},
			{Label: "Skip confirmation", Cmd: "extend webhooks endpoints delete whe_abc --yes"},
		},
		Gotchas: []string{
			"Deletion cascades to subscriptions; events for bound resources stop firing.",
			"Without --yes in non-TTY contexts, the command refuses to delete.",
		},
		SeeAlso: []string{"webhooks endpoints list", "webhooks subscriptions list"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteWithConfirm(cmd.Context(), app, "webhook endpoint", args[0], yes,
				func(ctx context.Context, id string) error {
					c, err := app.NewClient()
					if err != nil {
						return err
					}
					_, err = c.WebhookEndpoints.Delete(ctx, id, &extend.WebhookEndpointsDeleteRequest{})
					return err
				})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
		},
	}
}
