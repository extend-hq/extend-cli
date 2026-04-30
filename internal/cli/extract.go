package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

func newExtractCommand(app *App) *cobra.Command {
	var (
		extractorID        string
		version            string
		overrideConfigPath string
		configPath         string
		password           string
		wait               bool
		priority           int
		timeout            time.Duration
		meta               metaFlags
	)

	cmd := &cobra.Command{
		Use:   "extract <input>",
		Short: "Run extraction on a document",
		Long: `Extract structured data from a document using a configured extractor.

<input> can be:
  - a local file path (auto-uploaded)
  - a file_xxx ID (use a previously uploaded file)
  - an https:// URL (Extend fetches the document)

The extraction config can come from one of two sources:
  --using <id>       use an existing extractor (and optionally
                     --override-config to vary it for this one run)
  --config <json>    config without an extractor (inline JSON, path, or file:// URI)

The two are mutually exclusive; the server requires exactly one.

By default, the command waits until the run reaches a terminal state and
prints the result. Pass --wait=false to print only the run ID and exit
immediately, then poll with 'extend runs watch <id>' or fetch with
'extend runs get <id>'.`,
		Example: `  extend extract invoice.pdf --using ex_abc
  extend extract https://example.com/doc.pdf --using ex_abc
  extend extract file_xK9mLPq --using ex_abc --wait=false
  extend extract invoice.pdf --using ex_abc --override-config override.json
  extend extract invoice.pdf --using ex_abc --override-config '{"foo":"bar"}'
  extend extract invoice.pdf --config inline-config.json
  extend extract invoice.pdf --using ex_abc --jq '.output.value.invoice_id' -o raw`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			md, err := meta.build()
			if err != nil {
				return err
			}
			if (extractorID == "") == (configPath == "") {
				return errors.New("exactly one of --using or --config is required (server schema rejects both or neither)")
			}
			if configPath != "" && overrideConfigPath != "" {
				return errors.New("--override-config requires --using; it has no effect on inline --config")
			}
			return runExtract(cmd.Context(), app, extractParams{
				input:              args[0],
				extractorID:        extractorID,
				version:            version,
				overrideConfigPath: overrideConfigPath,
				configPath:         configPath,
				password:           password,
				wait:               wait,
				priority:           priority,
				timeout:            timeout,
				metadata:           md,
			})
		},
	}

	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	SetWaitAnnotations(cmd, client.ProfileShort, true)
	SetLifecycleFailureCodes(cmd, client.StatusFailed, client.StatusCancelled)

	cmd.Flags().StringVar(&extractorID, "using", "", "Extractor ID (mutually exclusive with --config)")
	cmd.Flags().StringVar(&version, "version", "", "Extractor version: latest, draft, or specific (e.g. 1.0)")
	cmd.Flags().StringVar(&overrideConfigPath, "override-config", "", "JSON object, path, or file:// URI for overrideConfig that varies the extractor's config for this run only")
	cmd.Flags().StringVar(&configPath, "config", "", "JSON object, path, or file:// URI for extract config (skips the extractor; mutually exclusive with --using)")
	cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run ID immediately)")
	cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum time to wait for completion")
	meta.attach(cmd)

	cmd.AddCommand(newExtractBatchCommand(app))
	return cmd
}

type extractParams struct {
	input              string
	extractorID        string
	version            string
	overrideConfigPath string
	configPath         string
	password           string
	wait               bool
	priority           int
	timeout            time.Duration
	metadata           map[string]any
}

func runExtract(ctx context.Context, app *App, p extractParams) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	ref, err := uploadOrResolveWith(ctx, app, cli, p.input, p.password)
	if err != nil {
		return err
	}

	in := client.CreateExtractRunInput{
		File:     ref,
		Metadata: p.metadata,
	}
	switch {
	case p.configPath != "":
		raw, err := readJSONFile(p.configPath, "--config")
		if err != nil {
			return err
		}
		in.Config = raw
	case p.extractorID != "":
		extractor := &client.ExtractorRef{ID: p.extractorID, Version: p.version}
		if p.overrideConfigPath != "" {
			raw, err := readJSONFile(p.overrideConfigPath, "--override-config")
			if err != nil {
				return err
			}
			extractor.OverrideConfig = raw
		}
		in.Extractor = extractor
	}
	if p.priority > 0 {
		in.Priority = &p.priority
	}

	run, err := cli.CreateExtractRun(ctx, in)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if !p.wait {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := cli.WaitForExtractRun(ctx, run.ID, client.WaitProfileOptions(client.ProfileShort, p.timeout), func(r *client.ExtractRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return fmt.Errorf("wait: %w", err)
	}

	if err := renderWithDefault(app, final, output.FormatJSON); err != nil {
		return err
	}
	if final.Status == client.StatusFailed {
		return fmt.Errorf("run %s failed", final.ID)
	}
	if final.Status == client.StatusCancelled {
		return fmt.Errorf("run %s was cancelled", final.ID)
	}
	return nil
}
