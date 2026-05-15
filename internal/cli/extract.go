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

// newExtractDoc returns the typed documentation for `extend extract` and
// its `extend extract batch` subcommand. This is the source of truth for
// the command's surface; the cobra.Command produced by Build() (via
// RootDoc) is one projection. The future SKILL.md generator and any other
// consumer reads from this CommandDoc directly via Walk.
func newExtractDoc(app *App) *CommandDoc {
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

	return &CommandDoc{
		Use:     "extract <input>",
		Summary: "Run extraction on a document",
		Group:   "Actions",
		Triggers: []string{
			"extract structured data from a document",
			"pull fields from an invoice or receipt",
			"schema-driven document extraction",
			"ocr a contract with a defined schema",
			"run an extractor against a pdf",
		},
		WhenToUse: `Use when you have a configured extractor (or inline JSON config) and need
typed structured fields back. Prefer 'parse' for raw text or markdown,
'classify' for a single category label, and 'edit' for filling form fields.`,
		Details: `<input> can be:
  - a local file path (auto-uploaded)
  - a file_xxx ID (use a previously uploaded file)
  - an https:// URL (Extend fetches the document)

The extraction config comes from one of these three forms. Exactly one
of --using or --config is required:

  # Run a saved extractor as-is.
  extend extract <input> --using ex_xxx

  # Run a saved extractor, merging per-run tweaks onto its saved config.
  extend extract <input> --using ex_xxx --override-config patch.json

  # Run a one-off config without a saved extractor (good for prototyping
  # or short-lived agent workflows).
  extend extract <input> --config inline-config.json

For both --override-config and --config, the value may be inline JSON,
a path, a file:// URI, or '-' to read from stdin. They are NOT
interchangeable: --override-config merges onto a --using extractor;
--config replaces the need for one entirely.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend extract invoice.pdf --using ex_abc"},
			{Label: "URL input", Cmd: "extend extract https://example.com/doc.pdf --using ex_abc"},
			{Label: "Async", Cmd: "extend extract file_xK9mLPq --using ex_abc --wait=false", Note: "Returns the run ID immediately; poll with `extend runs watch`."},
			{Label: "Merge per-run tweaks onto a saved extractor", Cmd: "extend extract invoice.pdf --using ex_abc --override-config override.json"},
			{Label: "Inline per-run override", Cmd: `extend extract invoice.pdf --using ex_abc --override-config '{"foo":"bar"}'`},
			{Label: "One-off run with no saved extractor", Cmd: "extend extract invoice.pdf --config inline-config.json"},
			{Label: "Filter output with jq", Cmd: "extend extract invoice.pdf --using ex_abc --jq '.output.value.invoice_id' -o raw"},
		},
		Gotchas: []string{
			"--config and --override-config are different: --config runs without a saved extractor, --override-config merges onto a --using extractor.",
			"Exactly one of --using or --config is required (server schema rejects both or neither).",
			"--override-config requires --using; passing it alongside --config is rejected.",
		},
		SeeAlso:  []string{"parse", "classify", "extract batch", "runs watch", "runs get"},
		Output:   OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: client.ProfileShort, DefaultsToWait: true},
		Failures: []client.RunStatus{client.StatusFailed, client.StatusCancelled},
		Args:     cobra.ExactArgs(1),
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
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&extractorID, "using", "", "Saved extractor ID (mutually exclusive with --config)")
			cmd.Flags().StringVar(&version, "version", "", "Extractor version: latest, draft, or specific (e.g. 1.0)")
			cmd.Flags().StringVar(&overrideConfigPath, "override-config", "", "Per-run overrides merged onto the --using extractor's saved config. Requires --using. Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&configPath, "config", "", "Complete one-off extract config used INSTEAD of a saved extractor. Mutually exclusive with --using. Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run ID immediately)")
			cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
			meta.attach(cmd)
		},
		Subcommands: []*CommandDoc{newExtractBatchDoc(app)},
	}
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
		return formatActionWaitError(err, run.ID)
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
