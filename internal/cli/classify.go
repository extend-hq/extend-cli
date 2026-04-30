package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

func newClassifyCommand(app *App) *cobra.Command {
	var (
		classifierID       string
		version            string
		overrideConfigPath string
		password           string
		async              bool
		priority           int
		timeout            time.Duration
		meta               metaFlags
	)

	cmd := &cobra.Command{
		Use:   "classify <input>",
		Short: "Classify a document into a configured category",
		Long: `Run a classifier against a document and return the predicted class
with a confidence score.

<input> can be:
  - a local file path (auto-uploaded)
  - a file_xxx ID (use a previously uploaded file)
  - an https:// URL (Extend fetches the document)

Pass --override-config as inline JSON, a plain file path, or an absolute
file:// URI to vary the classifier's config for this one run without modifying
the persisted classifier.

By default, the command waits until the run reaches a terminal state and
prints the result. Pass --async to print only the run ID and exit.`,
		Example: `  extend classify invoice.pdf --using cl_abc
  extend classify https://example.com/x.pdf --using cl_abc -o json
  extend classify invoice.pdf --using cl_abc --override-config override.json
  extend classify invoice.pdf --using cl_abc --override-config '{"foo":"bar"}'
  extend classify invoice.pdf --using cl_abc --jq '.output.id' -o raw`,
		Args: cobra.ExactArgs(1),
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
				async:              async,
				priority:           priority,
				timeout:            timeout,
				metadata:           md,
			})
		},
	}

	SetIOAnnotations(cmd, OutputPretty, OutputJSON)
	SetWaitAnnotations(cmd, client.ProfileShort, true)
	SetLifecycleFailureCodes(cmd, client.StatusFailed, client.StatusCancelled)

	cmd.Flags().StringVar(&classifierID, "using", "", "Classifier ID (required)")
	cmd.Flags().StringVar(&version, "version", "", "Classifier version: latest, draft, or specific (e.g. 1.0)")
	cmd.Flags().StringVar(&overrideConfigPath, "override-config", "", "JSON object, path, or file:// URI for overrideConfig that varies the classifier's config for this run only")
	cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
	cmd.Flags().BoolVar(&async, "async", false, "Return run ID immediately without waiting")
	cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum time to wait for completion")
	meta.attach(cmd)
	_ = cmd.MarkFlagRequired("using")

	cmd.AddCommand(newClassifyBatchCommand(app))
	return cmd
}

type classifyParams struct {
	input              string
	classifierID       string
	version            string
	overrideConfigPath string
	password           string
	async              bool
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

	if p.async {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := cli.WaitForClassifyRun(ctx, run.ID, client.WaitProfileOptions(client.ProfileShort, p.timeout), func(r *client.ClassifyRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return fmt.Errorf("wait: %w", err)
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
	if app.Format != "" || app.JQ != "" || !app.IO.IsStdoutTTY() {
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
