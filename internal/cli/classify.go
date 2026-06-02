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

// newClassifyDoc returns the typed documentation for `extend classify`
// and its `extend classify batch` subcommand.
func newClassifyDoc(app *App) *CommandDoc {
	var (
		classifierID string
		version      string
		patchPath    string
		configPath   string
		password     string
		text         string
		name         string
		wait         bool
		priority     int
		timeout      time.Duration
		meta         metaFlags
	)

	return &CommandDoc{
		Use:     "classify <input>",
		Summary: "Classify a document into a configured category",
		Group:   "Actions",
		Triggers: []string{
			"classify a document into a category",
			"detect document type with a classifier",
			"label an invoice or receipt automatically",
			"identify which kind of form a pdf is",
			"run a classifier on a document",
		},
		WhenToUse: `Use when you only need a category label (e.g. "invoice", "receipt",
"contract") for a document. Prefer 'extract' when you need typed fields
back; prefer 'parse' when you need raw text or markdown.`,
		Details: `Run a classifier against a document and return the predicted class
with a confidence score.

<input> can be:
  - a local file path (auto-uploaded)
  - a file_xxx ID (use a previously uploaded file)
  - an https:// URL (Extend fetches the document)
  - omitted, with --text "<inline text>" to classify raw text directly

The classifier comes from one of two forms. Exactly one of --using or
--config is required:

  # Run a saved classifier as-is (optionally tweaked with --patch).
  extend classify <input> --using cl_xxx

  # Run a one-off classifier config without saving it (prototyping).
  extend classify <input> --config inline-config.json

--patch applies a per-run partial merge onto the --using classifier's
saved config; it requires --using and is NOT interchangeable with --config
(which is a complete standalone config). For both, the value may be inline
JSON, a path, a file:// URI, or '-' to read from stdin.

` + actionConfigDoc(classifyConfigFields),
		Examples: []Example{
			{Label: "Basic", Cmd: "extend classify invoice.pdf --using cl_abc"},
			{Label: "URL with JSON output", Cmd: "extend classify https://example.com/x.pdf --using cl_abc -o json"},
			{Label: "Patch a saved classifier for this run", Cmd: "extend classify invoice.pdf --using cl_abc --patch tweaks.json"},
			{Label: "Inline patch", Cmd: `extend classify invoice.pdf --using cl_abc --patch '{"foo":"bar"}'`},
			{Label: "One-off config with no saved classifier", Cmd: "extend classify invoice.pdf --config inline-config.json"},
			{Label: "Filter via jq", Cmd: "extend classify invoice.pdf --using cl_abc --jq '.output.id' -o raw"},
		},
		Gotchas: []string{
			"Exactly one of --using or --config is required (server schema rejects both or neither).",
			"--patch requires --using; for a standalone config use --config instead.",
		},
		SeeAlso:  []string{"extract", "parse", "classify batch", "runs watch", "runs get"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
		Args:     cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			md, err := meta.build()
			if err != nil {
				return err
			}
			if (classifierID == "") == (configPath == "") {
				return errors.New("exactly one of --using or --config is required: --using <id> runs a saved classifier; --config <json> provides a standalone inline config")
			}
			if configPath != "" && patchPath != "" {
				return errors.New("exactly one of --using or --config is required: --patch needs --using; for a standalone config use --config instead")
			}
			input := ""
			if len(args) > 0 {
				input = args[0]
			}
			return runClassify(cmd.Context(), app, classifyParams{
				input:        input,
				text:         text,
				name:         name,
				classifierID: classifierID,
				version:      version,
				patchPath:    patchPath,
				configPath:   configPath,
				password:     password,
				wait:         wait,
				priority:     priority,
				timeout:      timeout,
				metadata:     md,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&classifierID, "using", "", "Saved classifier ID (mutually exclusive with --config)")
			cmd.Flags().StringVar(&version, "version", "", "Classifier version: latest, draft, or specific (e.g. 1.0)")
			cmd.Flags().StringVar(&patchPath, "patch", "", "Per-run patch merged onto the --using classifier's saved config. Requires --using. Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&configPath, "config", "", "Complete one-off classify config used INSTEAD of a saved classifier. Mutually exclusive with --using. Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&text, "text", "", "Classify this inline text instead of a file/URL/ID input (omit the positional <input>)")
			cmd.Flags().StringVar(&name, "name", "", "Display name for the input (honored for --text and URL inputs)")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run ID immediately)")
			cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
			meta.attach(cmd)
		},
		Subcommands: []*CommandDoc{newClassifyBatchDoc(app)},
	}
}

type classifyParams struct {
	input        string
	text         string
	name         string
	classifierID string
	version      string
	patchPath    string
	configPath   string
	password     string
	wait         bool
	priority     int
	timeout      time.Duration
	metadata     map[string]any
}

func runClassify(ctx context.Context, app *App, p classifyParams) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	ref, err := resolveInputOrText(ctx, app, cli, p.input, p.text, p.name, p.password)
	if err != nil {
		return err
	}

	file, err := extendx.BuildClassifyFile(ref)
	if err != nil {
		return err
	}

	req := &extend.ClassifyRunsCreateRequest{
		File: file,
	}
	switch {
	case p.configPath != "":
		raw, err := readJSONFile(p.configPath, "--config")
		if err != nil {
			return err
		}
		var cfg extend.ClassifyConfig
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("--config: %w", err)
		}
		req.Config = &cfg
	case p.classifierID != "":
		classifier := &extend.ClassifyRunsCreateRequestClassifier{ID: p.classifierID}
		if p.version != "" {
			v := extend.ProcessorVersionString(p.version)
			classifier.Version = &v
		}
		if p.patchPath != "" {
			raw, err := readJSONFile(p.patchPath, "--patch")
			if err != nil {
				return err
			}
			var override extend.ClassifyOverrideConfig
			if err := json.Unmarshal(raw, &override); err != nil {
				return fmt.Errorf("--patch: %w", err)
			}
			classifier.OverrideConfig = &override
		}
		req.Classifier = classifier
	}
	if p.metadata != nil {
		md := extend.RunMetadata(p.metadata)
		req.Metadata = &md
	}
	if p.priority > 0 {
		pr := extend.RunPriority(p.priority)
		req.Priority = &pr
	}

	run, err := cli.ClassifyRuns.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if !p.wait {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := waitForClassifyRun(ctx, cli, run.ID, extendx.WaitProfileOptions(extendx.ProfileShort, p.timeout), func(r *extend.ClassifyRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return formatActionWaitError(err, run.ID)
	}

	if err := renderClassifyResult(app, final); err != nil {
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

func waitForClassifyRun(ctx context.Context, c *sdkclient.Client, id string, opts extendx.WaitOptions, onPoll func(*extend.ClassifyRun)) (*extend.ClassifyRun, error) {
	return extendx.PollForRun(ctx,
		func(ctx context.Context) (*extend.ClassifyRun, error) {
			return c.ClassifyRuns.Retrieve(ctx, id, &extend.ClassifyRunsRetrieveRequest{})
		},
		func(r *extend.ClassifyRun) extendx.RunStatus { return extendx.RunStatus(r.Status) },
		opts, onPoll,
	)
}

func renderClassifyResult(app *App, run *extend.ClassifyRun) error {
	if app.Format != "" || app.JQ != "" {
		return renderWithDefault(app, run, output.FormatJSON)
	}
	if run.Output == nil {
		return renderWithDefault(app, run, output.FormatJSON)
	}
	o := run.Output
	pct := int(o.Confidence*100 + 0.5)
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.Out, "%s %s %s\n", pal.Green("✓"), o.Type, pal.Dimf("(%d%% confidence)", pct))
	for _, ins := range o.Insights {
		if ins.Type() == "reasoning" && ins.Content != "" {
			fmt.Fprintln(app.IO.Out)
			fmt.Fprintln(app.IO.Out, "Reasoning:")
			fmt.Fprintln(app.IO.Out, ins.Content)
		}
	}
	return nil
}
