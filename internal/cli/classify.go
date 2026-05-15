package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newClassifyDoc returns the typed documentation for `extend classify`
// and its `extend classify batch` subcommand.
func newClassifyDoc(app *App) *CommandDoc {
	var (
		classifierID       string
		version            string
		overrideConfigPath string
		password           string
		wait               bool
		priority           int
		timeout            time.Duration
		meta               metaFlags
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

--override-config merges per-run tweaks onto the --using classifier's
saved config, just for this one run. Source: inline JSON, a plain file
path, an absolute file:// URI, or '-' to read from stdin. It does NOT
replace the classifier; pass --using to pick the classifier and
--override-config to vary it without modifying
the persisted classifier.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend classify invoice.pdf --using cl_abc"},
			{Label: "URL with JSON output", Cmd: "extend classify https://example.com/x.pdf --using cl_abc -o json"},
			{Label: "Merge per-run tweaks onto a saved classifier", Cmd: "extend classify invoice.pdf --using cl_abc --override-config override.json"},
			{Label: "Inline per-run override", Cmd: `extend classify invoice.pdf --using cl_abc --override-config '{"foo":"bar"}'`},
			{Label: "Filter via jq", Cmd: "extend classify invoice.pdf --using cl_abc --jq '.output.id' -o raw"},
		},
		Gotchas: []string{
			"--using is required (no inline-config option for classify).",
		},
		SeeAlso:  []string{"extract", "parse", "classify batch", "runs watch", "runs get"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: client.ProfileShort, DefaultsToWait: true},
		Failures: []client.RunStatus{client.StatusFailed, client.StatusCancelled},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			md, err := meta.build()
			if err != nil {
				return err
			}
			return runClassify(cmd.Context(), app, classifyParams{
				input:              args[0],
				classifierID:       classifierID,
				version:            version,
				overrideConfigPath: overrideConfigPath,
				password:           password,
				wait:               wait,
				priority:           priority,
				timeout:            timeout,
				metadata:           md,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&classifierID, "using", "", "Classifier ID (required)")
			cmd.Flags().StringVar(&version, "version", "", "Classifier version: latest, draft, or specific (e.g. 1.0)")
			cmd.Flags().StringVar(&overrideConfigPath, "override-config", "", "Per-run overrides merged onto the --using classifier's saved config. Requires --using. Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run ID immediately)")
			cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
			meta.attach(cmd)
			_ = cmd.MarkFlagRequired("using")
		},
		Subcommands: []*CommandDoc{newClassifyBatchDoc(app)},
	}
}

type classifyParams struct {
	input              string
	classifierID       string
	version            string
	overrideConfigPath string
	password           string
	wait               bool
	priority           int
	timeout            time.Duration
	metadata           map[string]any
}

func runClassify(ctx context.Context, app *App, p classifyParams) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	ref, err := uploadOrResolveWith(ctx, app, cli, p.input, p.password)
	if err != nil {
		return err
	}

	classifier := &client.ClassifierRef{ID: p.classifierID, Version: p.version}
	if p.overrideConfigPath != "" {
		raw, err := readJSONFile(p.overrideConfigPath, "--override-config")
		if err != nil {
			return err
		}
		classifier.OverrideConfig = raw
	}
	in := client.CreateClassifyRunInput{
		Classifier: classifier,
		File:       ref,
		Metadata:   p.metadata,
	}
	if p.priority > 0 {
		in.Priority = &p.priority
	}

	run, err := cli.CreateClassifyRun(ctx, in)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if !p.wait {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := cli.WaitForClassifyRun(ctx, run.ID, client.WaitProfileOptions(client.ProfileShort, p.timeout), func(r *client.ClassifyRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return formatActionWaitError(err, run.ID)
	}

	if err := renderClassifyResult(app, final); err != nil {
		return err
	}
	if final.Status == client.StatusFailed {
		if final.FailureMessage != "" {
			return fmt.Errorf("run %s failed: %s", final.ID, final.FailureMessage)
		}
		return fmt.Errorf("run %s failed", final.ID)
	}
	if final.Status == client.StatusCancelled {
		return fmt.Errorf("run %s was cancelled", final.ID)
	}
	return nil
}

func renderClassifyResult(app *App, run *client.ClassifyRun) error {
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
		if ins.Type == "reasoning" && ins.Content != "" {
			fmt.Fprintln(app.IO.Out)
			fmt.Fprintln(app.IO.Out, "Reasoning:")
			fmt.Fprintln(app.IO.Out, ins.Content)
		}
	}
	return nil
}
