package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

func newWebhooksCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhooks",
		Short: "Manage webhook endpoints, subscriptions, and verify signatures",
	}
	cmd.AddCommand(newWebhookEndpointsCommand(app))
	cmd.AddCommand(newWebhookSubscriptionsCommand(app))
	cmd.AddCommand(newWebhooksVerifyCommand(app))
	return cmd
}

func newWebhookEndpointsCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "endpoints",
		Short: "Manage webhook endpoints (the receiving URLs)",
	}
	cmd.AddCommand(newWebhookEndpointsListCommand(app))
	cmd.AddCommand(newWebhookEndpointsGetCommand(app))
	cmd.AddCommand(newWebhookEndpointsCreateCommand(app))
	cmd.AddCommand(newWebhookEndpointsUpdateCommand(app))
	cmd.AddCommand(newWebhookEndpointsDeleteCommand(app))
	return cmd
}

func newWebhookEndpointsListCommand(app *App) *cobra.Command {
	var (
		status  string
		sortDir string
		limit   int
		all     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List webhook endpoints",
		Example: `  extend webhooks endpoints list
  extend webhooks endpoints list --status enabled
  extend webhooks endpoints list --all -o id`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := client.ListWebhookEndpointsOptions{
				Status:  status,
				SortDir: sortDir,
				Limit:   limit,
			}
			var rows [][]string
			var pages []any
			for {
				page, err := cli.ListWebhookEndpoints(cmd.Context(), opts)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				for _, ep := range page.Data {
					rows = append(rows, []string{ep.ID, ep.Name, truncate(ep.URL, 40), relTime(ep.CreatedAt)})
				}
				if !all || page.NextPageToken == "" {
					break
				}
				opts.PageToken = page.NextPageToken
			}
			return renderList(app, pages, []string{"id", "name", "url", "created"}, rows, "No webhook endpoints.")
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by status: enabled|disabled")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results per page")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate")
	return cmd
}

func newWebhookEndpointsGetCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <endpoint-id>",
		Short: "Show one webhook endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			ep, err := cli.GetWebhookEndpoint(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderWithDefault(app, ep, output.FormatJSON)
		},
	}
	return cmd
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

// build returns nil when no flags were set, so omitempty drops the whole
// advancedOptions object on the wire.
func (f *webhookAdvancedFlags) build() (*client.WebhookAdvancedOptions, error) {
	if len(f.headers) == 0 && f.payloadFormat == "" && f.urlThresholdBytes == 0 {
		return nil, nil
	}
	opts := &client.WebhookAdvancedOptions{}
	if len(f.headers) > 0 {
		hdrs, err := parseKVPairs("--header", f.headers)
		if err != nil {
			return nil, err
		}
		opts.Headers = hdrs
	}
	if f.payloadFormat != "" {
		if f.payloadFormat != "json" && f.payloadFormat != "url" {
			return nil, fmt.Errorf("--payload-format: %q is not one of json|url", f.payloadFormat)
		}
		opts.Payload = &client.WebhookPayloadOptions{Format: f.payloadFormat}
		if f.urlThresholdBytes > 0 {
			if f.payloadFormat != "url" {
				return nil, errors.New("--url-threshold-bytes only applies when --payload-format=url")
			}
			opts.Payload.UrlThresholdBytes = &f.urlThresholdBytes
		}
	} else if f.urlThresholdBytes > 0 {
		return nil, errors.New("--url-threshold-bytes requires --payload-format=url")
	}
	return opts, nil
}

func newWebhookEndpointsCreateCommand(app *App) *cobra.Command {
	var (
		url        string
		name       string
		status     string
		events     []string
		apiVersion string
		disable    bool
		advanced   webhookAdvancedFlags
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a webhook endpoint",
		Long: `Create a webhook endpoint. The signingSecret in the response is shown
ONLY ONCE; store it securely. Use 'extend webhooks verify' to validate
incoming payloads against it.

Custom delivery headers (e.g. shared secrets, tenant identifiers) can be
attached via --header key=value. The default payload format is the full
JSON event body; pass --payload-format=url to swap to a link-only payload
once the body exceeds --url-threshold-bytes.`,
		Example: `  extend webhooks endpoints create --url https://x.com/hook \
    --name prod --events extract_run.processed,extract_run.failed
  extend webhooks endpoints create --url https://x.com/hook --name prod \
    --events extract_run.processed --header X-Tenant=acme --header X-Token=$WT`,
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
			if disable {
				status = "disabled"
			}
			adv, err := advanced.build()
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			ep, err := cli.CreateWebhookEndpoint(cmd.Context(), client.CreateWebhookEndpointInput{
				URL:             url,
				Name:            name,
				Status:          status,
				EnabledEvents:   splitCSV(events),
				APIVersion:      apiVersion,
				AdvancedOptions: adv,
			})
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
	}
	cmd.Flags().StringVar(&url, "url", "", "Receiving URL (required)")
	cmd.Flags().StringVar(&name, "name", "", "Display name (required)")
	cmd.Flags().BoolVar(&disable, "disabled", false, "Create the endpoint in 'disabled' state (defaults to 'enabled')")
	cmd.Flags().StringArrayVar(&events, "events", nil, "Enabled events (comma-separated or repeated; required)")
	cmd.Flags().StringVar(&apiVersion, "api-version", client.DefaultAPIVersion, "API version for events")
	advanced.attach(cmd)
	return cmd
}

func newWebhookEndpointsUpdateCommand(app *App) *cobra.Command {
	var (
		url      string
		name     string
		status   string
		events   []string
		enable   bool
		disable  bool
		advanced webhookAdvancedFlags
	)
	cmd := &cobra.Command{
		Use:   "update <endpoint-id>",
		Short: "Update mutable fields on a webhook endpoint",
		Long: `Update mutable fields on a webhook endpoint. Pass only the flags you want
to change; omitted fields are left untouched. The api-version field cannot
be updated; create a new endpoint to migrate.

Setting --header replaces the entire custom-headers map; pass each header
to keep, plus any new ones. To clear all custom headers, pass --header
with an empty value? — not supported by the server; recreate the endpoint
without --header instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if enable && disable {
				return errors.New("--enable and --disable are mutually exclusive")
			}
			if enable {
				status = "enabled"
			}
			if disable {
				status = "disabled"
			}
			adv, err := advanced.build()
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			ep, err := cli.UpdateWebhookEndpoint(cmd.Context(), args[0], client.UpdateWebhookEndpointInput{
				URL:             url,
				Name:            name,
				Status:          status,
				EnabledEvents:   splitCSV(events),
				AdvancedOptions: adv,
			})
			if err != nil {
				return err
			}
			return renderWithDefault(app, ep, output.FormatJSON)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "New receiving URL")
	cmd.Flags().StringVar(&name, "name", "", "New display name")
	cmd.Flags().BoolVar(&enable, "enable", false, "Set status to 'enabled'")
	cmd.Flags().BoolVar(&disable, "disable", false, "Set status to 'disabled'")
	cmd.Flags().StringArrayVar(&events, "events", nil, "Replace enabled events list")
	advanced.attach(cmd)
	return cmd
}

func newWebhookEndpointsDeleteCommand(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <endpoint-id>",
		Short: "Delete a webhook endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteWithConfirm(cmd.Context(), app, "webhook endpoint", args[0], yes,
				func(ctx context.Context, id string) error {
					c, err := app.NewClient()
					if err != nil {
						return err
					}
					return c.DeleteWebhookEndpoint(ctx, id)
				})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func newWebhookSubscriptionsCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subscriptions",
		Short: "Manage webhook subscriptions (resource-scoped event filters)",
	}
	cmd.AddCommand(newWebhookSubscriptionsListCommand(app))
	cmd.AddCommand(newWebhookSubscriptionsGetCommand(app))
	cmd.AddCommand(newWebhookSubscriptionsCreateCommand(app))
	cmd.AddCommand(newWebhookSubscriptionsUpdateCommand(app))
	cmd.AddCommand(newWebhookSubscriptionsDeleteCommand(app))
	return cmd
}

func newWebhookSubscriptionsListCommand(app *App) *cobra.Command {
	var (
		endpointID string
		resourceID string
		sortDir    string
		limit      int
		all        bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List webhook subscriptions",
		Example: `  extend webhooks subscriptions list
  extend webhooks subscriptions list --endpoint we_abc
  extend webhooks subscriptions list --resource ex_abc --all`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := client.ListWebhookSubscriptionsOptions{
				WebhookEndpointID: endpointID,
				ResourceID:        resourceID,
				SortDir:           sortDir,
				Limit:             limit,
			}
			var rows [][]string
			var pages []any
			for {
				page, err := cli.ListWebhookSubscriptions(cmd.Context(), opts)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				for _, s := range page.Data {
					rows = append(rows, []string{s.ID, s.WebhookEndpointID, s.ResourceType, s.ResourceID, fmt.Sprintf("%d events", len(s.EnabledEvents)), relTime(s.CreatedAt)})
				}
				if !all || page.NextPageToken == "" {
					break
				}
				opts.PageToken = page.NextPageToken
			}
			return renderList(app, pages, []string{"id", "endpoint", "type", "resource", "events", "created"}, rows, "No webhook subscriptions.")
		},
	}
	cmd.Flags().StringVar(&endpointID, "endpoint", "", "Filter by webhook endpoint ID (we_...)")
	cmd.Flags().StringVar(&resourceID, "resource", "", "Filter by resource ID (extractor/classifier/splitter/workflow)")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results per page")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate")
	return cmd
}

func newWebhookSubscriptionsGetCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <subscription-id>",
		Short: "Show one webhook subscription",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.GetWebhookSubscription(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
	}
	return cmd
}

func newWebhookSubscriptionsCreateCommand(app *App) *cobra.Command {
	var (
		endpointID   string
		resourceID   string
		resourceType string
		events       []string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Subscribe an endpoint to events for a specific resource",
		Long: `Subscribe an endpoint to events for a specific resource. The resource type
is auto-detected from the resource ID prefix (ex_=extractor, cl_=classifier,
spl_=splitter, workflow_=workflow); pass --resource-type to override or for
unknown prefixes.`,
		Example: `  extend webhooks subscriptions create --endpoint whe_x --resource workflow_abc \
    --events workflow_run.completed
  extend webhooks subscriptions create --endpoint whe_x --resource ex_abc \
    --resource-type extractor --events extract_run.processed`,
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
				resourceType = client.WebhookSubscriptionResourceTypeFromID(resourceID)
				if resourceType == "" {
					return fmt.Errorf("could not infer resource type from %q; pass --resource-type explicitly (extractor|classifier|splitter|workflow)", resourceID)
				}
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.CreateWebhookSubscription(cmd.Context(), client.CreateWebhookSubscriptionInput{
				WebhookEndpointID: endpointID,
				ResourceType:      resourceType,
				ResourceID:        resourceID,
				EnabledEvents:     splitCSV(events),
			})
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
	}
	cmd.Flags().StringVar(&endpointID, "endpoint", "", "Webhook endpoint ID (required)")
	cmd.Flags().StringVar(&resourceID, "resource", "", "Resource ID to scope events to, e.g. workflow_xxx (required)")
	cmd.Flags().StringVar(&resourceType, "resource-type", "", "Resource type: extractor|classifier|splitter|workflow (auto-detected from --resource prefix)")
	cmd.Flags().StringArrayVar(&events, "events", nil, "Enabled events (comma-separated or repeated; required)")
	return cmd
}

func newWebhookSubscriptionsUpdateCommand(app *App) *cobra.Command {
	var events []string
	cmd := &cobra.Command{
		Use:   "update <subscription-id>",
		Short: "Replace the enabled events on a webhook subscription",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(events) == 0 {
				return errors.New("--events is required (server only allows updating enabledEvents)")
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.UpdateWebhookSubscription(cmd.Context(), args[0], client.UpdateWebhookSubscriptionInput{
				EnabledEvents: splitCSV(events),
			})
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
	}
	cmd.Flags().StringArrayVar(&events, "events", nil, "Replacement enabled events (comma-separated or repeated; required)")
	return cmd
}

func newWebhookSubscriptionsDeleteCommand(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <subscription-id>",
		Short: "Delete a webhook subscription",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteWithConfirm(cmd.Context(), app, "webhook subscription", args[0], yes,
				func(ctx context.Context, id string) error {
					c, err := app.NewClient()
					if err != nil {
						return err
					}
					return c.DeleteWebhookSubscription(ctx, id)
				})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func newWebhooksVerifyCommand(app *App) *cobra.Command {
	var (
		secret    string
		signature string
		timestamp string
		bodyFile  string
		maxAge    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify the HMAC-SHA256 signature on a webhook payload",
		Long: `Verify a webhook payload's signature against a signing secret. The
signature is HMAC-SHA256 over "v0:<timestamp>:<body>".

The body is read from --body-file or stdin. The signing secret can come from
--secret or the EXTEND_WEBHOOK_SECRET env var.`,
		Example: `  extend webhooks verify \
    --signature "$X_EXTEND_REQUEST_SIGNATURE" \
    --timestamp "$X_EXTEND_REQUEST_TIMESTAMP" \
    --secret "$WSS_SECRET" \
    --body-file payload.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if secret == "" {
				secret = os.Getenv("EXTEND_WEBHOOK_SECRET")
			}
			if secret == "" {
				return errors.New("signing secret required (--secret or EXTEND_WEBHOOK_SECRET env)")
			}
			body, err := readBody(app, bodyFile)
			if err != nil {
				return err
			}
			pal := paletteFor(app.IO)
			if err := client.VerifyWebhookSignature(secret, signature, timestamp, body, maxAge); err != nil {
				fmt.Fprintf(app.IO.ErrOut, "%s %v\n", pal.Red("✗"), err)
				return fmt.Errorf("signature invalid")
			}
			fmt.Fprintln(app.IO.Out, pal.Green("✓")+" signature valid")
			return nil
		},
	}
	cmd.Flags().StringVar(&secret, "secret", "", "Signing secret (or EXTEND_WEBHOOK_SECRET env)")
	cmd.Flags().StringVar(&signature, "signature", "", "Value of x-extend-request-signature header")
	cmd.Flags().StringVar(&timestamp, "timestamp", "", "Value of x-extend-request-timestamp header")
	cmd.Flags().StringVar(&bodyFile, "body-file", "-", "Path to raw webhook body ('-' for stdin)")
	cmd.Flags().DurationVar(&maxAge, "max-age", 5*time.Minute, "Reject if timestamp is older than this; 0 to skip the time check")
	_ = cmd.MarkFlagRequired("signature")
	_ = cmd.MarkFlagRequired("timestamp")
	return cmd
}

func readBody(app *App, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(app.IO.In)
	}
	return os.ReadFile(path)
}

func deleteWithConfirm(ctx context.Context, app *App, label, id string, yes bool, fn func(context.Context, string) error) error {
	if !yes {
		if !app.IO.IsStdinTTY() {
			return fmt.Errorf("refusing to delete %s without confirmation; pass --yes", label)
		}
		fmt.Fprintf(app.IO.ErrOut, "Delete %s %s? [y/N]: ", label, id)
		reader := bufio.NewReader(app.IO.In)
		line, _ := reader.ReadString('\n')
		line = strings.ToLower(strings.TrimSpace(line))
		if line != "y" && line != "yes" {
			fmt.Fprintln(app.IO.ErrOut, "Aborted.")
			return nil
		}
	}
	if err := fn(ctx, id); err != nil {
		return err
	}
	fmt.Fprintf(app.IO.Out, "%s Deleted %s %s\n", paletteFor(app.IO).Green("✓"), label, id)
	return nil
}

func splitCSV(in []string) []string {
	var out []string
	for _, s := range in {
		for _, part := range strings.Split(s, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}
