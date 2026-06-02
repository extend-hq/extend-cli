package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newExtractDoc returns the typed documentation for `extend extract` and
// its `extend extract batch` subcommand. This is the source of truth for
// the command's surface; the cobra.Command produced by Build() (via
// RootDoc) is one projection. The future SKILL.md generator and any other
// consumer reads from this CommandDoc directly via Walk.
func newExtractDoc(app *App) *CommandDoc {
	var (
		extractorID string
		version     string
		patchPath   string
		configPath  string
		password    string
		text        string
		name        string
		wait        bool
		priority    int
		timeout     time.Duration
		meta        metaFlags
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
  - omitted, with --text "<inline text>" to extract from raw text directly

The extraction config comes from one of these three forms. Exactly one
of --using or --config is required:

  # Run a saved extractor as-is.
  extend extract <input> --using ex_xxx

  # Run a saved extractor, applying per-run tweaks on top of its saved config.
  extend extract <input> --using ex_xxx --patch patch.json

  # Run a one-off config without a saved extractor (good for prototyping
  # or short-lived agent workflows).
  extend extract <input> --config inline-config.json

For both --patch and --config, the value may be inline JSON, a path,
a file:// URI, or '-' to read from stdin. They are NOT interchangeable:
--patch is a partial merge onto a --using extractor; --config is a
complete standalone config that replaces the need for one entirely.

` + actionConfigDoc(extractConfigFields),
		Examples: []Example{
			{Label: "Basic", Cmd: "extend extract invoice.pdf --using ex_abc"},
			{Label: "URL input", Cmd: "extend extract https://example.com/doc.pdf --using ex_abc"},
			{Label: "Async", Cmd: "extend extract file_xK9mLPq --using ex_abc --wait=false", Note: "Returns the run ID immediately; poll with `extend runs watch`."},
			{Label: "Patch a saved extractor for this run", Cmd: "extend extract invoice.pdf --using ex_abc --patch tweaks.json"},
			{Label: "Inline patch", Cmd: `extend extract invoice.pdf --using ex_abc --patch '{"foo":"bar"}'`},
			{Label: "One-off run with no saved extractor", Cmd: "extend extract invoice.pdf --config inline-config.json"},
			{Label: "Filter output with jq", Cmd: "extend extract invoice.pdf --using ex_abc --jq '.output.value.invoice_id' -o raw"},
		},
		Gotchas: []string{
			"--config and --patch are different: --config runs without a saved extractor; --patch merges onto a --using extractor.",
			"Exactly one of --using or --config is required (server schema rejects both or neither).",
			"--patch requires --using; for a standalone config use --config instead.",
		},
		SeeAlso:  []string{"parse", "classify", "extract batch", "runs watch", "runs get"},
		Output:   OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
		Args:     cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			md, err := meta.build()
			if err != nil {
				return err
			}
			if (extractorID == "") == (configPath == "") {
				return errors.New("exactly one of --using or --config is required: --using <id> runs a saved extractor; --config <json> provides a standalone inline config")
			}
			if configPath != "" && patchPath != "" {
				return errors.New("exactly one of --using or --config is required: --patch needs --using; for a standalone config use --config instead")
			}
			input := ""
			if len(args) > 0 {
				input = args[0]
			}
			return runExtract(cmd.Context(), app, extractParams{
				input:       input,
				text:        text,
				name:        name,
				extractorID: extractorID,
				version:     version,
				patchPath:   patchPath,
				configPath:  configPath,
				password:    password,
				wait:        wait,
				priority:    priority,
				timeout:     timeout,
				metadata:    md,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&extractorID, "using", "", "Saved extractor ID (mutually exclusive with --config)")
			cmd.Flags().StringVar(&version, "version", "", "Extractor version: latest, draft, or specific (e.g. 1.0)")
			cmd.Flags().StringVar(&patchPath, "patch", "", "Per-run patch merged onto the --using extractor's saved config. Requires --using. Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&configPath, "config", "", "Complete one-off extract config used INSTEAD of a saved extractor. Mutually exclusive with --using. Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&text, "text", "", "Extract from this inline text instead of a file/URL/ID input (omit the positional <input>)")
			cmd.Flags().StringVar(&name, "name", "", "Display name for the input (honored for --text and URL inputs)")
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
	input       string
	text        string
	name        string
	extractorID string
	version     string
	patchPath   string
	configPath  string
	password    string
	wait        bool
	priority    int
	timeout     time.Duration
	metadata    map[string]any
}

func runExtract(ctx context.Context, app *App, p extractParams) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	ref, err := resolveInputOrText(ctx, app, cli, p.input, p.text, p.name, p.password)
	if err != nil {
		return err
	}

	file, err := extendx.BuildExtractFile(ref)
	if err != nil {
		return err
	}

	req := &extend.ExtractRunsCreateRequest{
		File: file,
	}
	if p.metadata != nil {
		md := extend.RunMetadata(p.metadata)
		req.Metadata = &md
	}
	switch {
	case p.configPath != "":
		raw, err := readJSONFile(p.configPath, "--config")
		if err != nil {
			return err
		}
		var cfg extend.ExtractConfigJSON
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("--config: %w", err)
		}
		req.Config = &cfg
	case p.extractorID != "":
		extractor := &extend.ExtractRunsCreateRequestExtractor{ID: p.extractorID}
		if p.version != "" {
			v := extend.ProcessorVersionString(p.version)
			extractor.Version = &v
		}
		if p.patchPath != "" {
			raw, err := readJSONFile(p.patchPath, "--patch")
			if err != nil {
				return err
			}
			var override extend.ExtractOverrideConfigJSON
			if err := json.Unmarshal(raw, &override); err != nil {
				return fmt.Errorf("--patch: %w", err)
			}
			extractor.OverrideConfig = &override
		}
		req.Extractor = extractor
	}
	if p.priority > 0 {
		pr := extend.RunPriority(p.priority)
		req.Priority = &pr
	}

	run, err := cli.ExtractRuns.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if !p.wait {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := waitForExtractRun(ctx, cli, run.ID, extendx.WaitProfileOptions(extendx.ProfileShort, p.timeout), func(r *extend.ExtractRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return formatActionWaitError(err, run.ID)
	}

	if err := renderWithDefault(app, final, output.FormatJSON); err != nil {
		return err
	}
	switch extendx.RunStatus(final.Status) {
	case extendx.StatusFailed:
		return runFailureError(final.ID, final.FailureReason, final.FailureMessage)
	case extendx.StatusCancelled:
		return fmt.Errorf("run %s was cancelled", final.ID)
	}
	return nil
}

// waitForExtractRun polls c.ExtractRuns.Retrieve until the run reaches
// a terminal state. Centralized here so multiple commands (extract,
// runs watch) can share the same getter shape without duplicating the
// PollForRun boilerplate.
func waitForExtractRun(ctx context.Context, c *sdkclient.Client, id string, opts extendx.WaitOptions, onPoll func(*extend.ExtractRun)) (*extend.ExtractRun, error) {
	return extendx.PollForRun(ctx,
		func(ctx context.Context) (*extend.ExtractRun, error) {
			return c.ExtractRuns.Retrieve(ctx, id, &extend.ExtractRunsRetrieveRequest{})
		},
		func(r *extend.ExtractRun) extendx.RunStatus { return extendx.RunStatus(r.Status) },
		opts, onPoll,
	)
}
