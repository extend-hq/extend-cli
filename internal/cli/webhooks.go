package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// newWebhooksDoc returns the typed documentation for `extend webhooks` and
// the full subtree underneath: endpoints (5 leaves, in
// webhooks_endpoints.go), subscriptions (5 leaves, in
// webhooks_subscriptions.go), and the verify leaf (in this file).
func newWebhooksDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "webhooks",
		Summary: "Manage webhook endpoints, subscriptions, and signature verification",
		WhenToUse: `Use this command group to register receiving URLs, bind specific
resources to event streams, and verify the HMAC signature on incoming
webhook payloads.`,
		Details: `Webhooks in Extend are split into two resources:

- Endpoints define the receiving URL plus its delivery configuration
  (custom headers, payload format, API version).
- Subscriptions bind an endpoint to a specific resource and event set.

The 'verify' leaf validates incoming payloads against the signing
secret returned at endpoint creation.`,
		Subcommands: []*CommandDoc{
			newWebhookEndpointsDoc(app),
			newWebhookSubscriptionsDoc(app),
			newWebhooksVerifyDoc(app),
		},
	}
}

func newWebhooksVerifyDoc(app *App) *CommandDoc {
	var (
		secret    string
		signature string
		timestamp string
		bodyFile  string
		maxAge    time.Duration
	)
	return &CommandDoc{
		Use:     "verify",
		Summary: "Verify the HMAC-SHA256 signature on a webhook payload",
		Triggers: []string{
			"verify a webhook signature",
			"validate hmac-sha256 webhook payload",
			"check x-extend-request-signature header",
			"authenticate an incoming webhook delivery",
		},
		WhenToUse: `Use in your webhook receiver to validate that an incoming payload
came from Extend and was not tampered with. Pass the signature header,
timestamp header, the raw body, and the signing secret stored from
endpoint creation.`,
		Details: `Verify a webhook payload's signature against a signing secret. The
signature is HMAC-SHA256 over "v0:<timestamp>:<body>".

The body is read from --body-file or stdin. The signing secret can come from
--secret or the EXTEND_WEBHOOK_SECRET env var.`,
		Examples: []Example{
			{Label: "From file with env secret", Cmd: `extend webhooks verify --signature "$X_EXTEND_REQUEST_SIGNATURE" --timestamp "$X_EXTEND_REQUEST_TIMESTAMP" --secret "$WSS_SECRET" --body-file payload.json`},
		},
		Gotchas: []string{
			"--signature and --timestamp are required; the headers must match the literal request headers.",
			"--secret is required (or set EXTEND_WEBHOOK_SECRET); secrets are not retrievable later.",
			"The default --max-age (5m) protects against replay; pass 0 to disable.",
		},
		SeeAlso: []string{"webhooks endpoints create", "webhooks endpoints get"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		RunE: func(cmd *cobra.Command, args []string) error {
			if secret == "" {
				secret = os.Getenv(envWebhookSecret)
			}
			if secret == "" {
				return fmt.Errorf("signing secret required (--secret or %s env)", envWebhookSecret)
			}
			body, err := readBody(app, bodyFile)
			if err != nil {
				return err
			}
			pal := paletteFor(app.IO)
			if err := extendx.VerifyWebhookSignature(secret, signature, timestamp, body, maxAge); err != nil {
				fmt.Fprintf(app.IO.ErrOut, "%s %v\n", pal.Red("✗"), err)
				return fmt.Errorf("signature invalid")
			}
			fmt.Fprintln(app.IO.ErrOut, pal.Green("✓")+" signature valid")
			return nil
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&secret, "secret", "", "Signing secret (or EXTEND_WEBHOOK_SECRET env)")
			cmd.Flags().StringVar(&signature, "signature", "", "Value of x-extend-request-signature header")
			cmd.Flags().StringVar(&timestamp, "timestamp", "", "Value of x-extend-request-timestamp header")
			cmd.Flags().StringVar(&bodyFile, "body-file", "-", "Path to raw webhook body ('-' for stdin)")
			cmd.Flags().DurationVar(&maxAge, "max-age", 5*time.Minute, "Reject if timestamp is older than this; 0 to skip the time check")
			_ = cmd.MarkFlagRequired("signature")
			_ = cmd.MarkFlagRequired("timestamp")
		},
	}
}

// readBody returns the body bytes for `extend webhooks verify`. When
// path is "-" the input comes from app.IO.In so tests can inject a
// fake stdin; otherwise it's read straight from disk.
func readBody(app *App, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(app.IO.In)
	}
	return os.ReadFile(path)
}

// deleteWithConfirm is shared between webhook endpoint and webhook
// subscription delete commands. The TTY-aware confirmation matches the
// pattern used elsewhere (extend files delete, extend runs delete).
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
	fmt.Fprintf(app.IO.ErrOut, "%s Deleted %s %s\n", paletteFor(app.IO).Green("✓"), label, id)
	return nil
}

// splitCSV expands a slice of strings that may contain comma-separated
// values into a flat slice of trimmed, non-empty tokens. Lets users
// pass --events extract_run.processed,extract_run.failed in one flag
// or --events extract_run.processed --events extract_run.failed as
// repeated flags; the result is the same.
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
