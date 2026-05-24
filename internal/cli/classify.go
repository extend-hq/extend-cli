package cli

import (
	"context"
	"encoding/json"
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
		password     string
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

--patch applies a per-run partial merge onto the --using classifier's
saved config. Source: inline JSON, a plain file path, an absolute
file:// URI, or '-' to read from stdin. --patch does NOT replace the
classifier; pass --using to pick the classifier and --patch to vary it
without modifying the persisted version.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend classify invoice.pdf --using cl_abc"},
			{Label: "URL with JSON output", Cmd: "extend classify https://example.com/x.pdf --using cl_abc -o json"},
			{Label: "Patch a saved classifier for this run", Cmd: "extend classify invoice.pdf --using cl_abc --patch tweaks.json"},
			{Label: "Inline patch", Cmd: `extend classify invoice.pdf --using cl_abc --patch '{"foo":"bar"}'`},
			{Label: "Filter via jq", Cmd: "extend classify invoice.pdf --using cl_abc --jq '.output.id' -o raw"},
		},
		Gotchas: []string{
			"--using is required (no inline-config option for classify).",
		},
		SeeAlso:  []string{"extract", "parse", "classify batch", "runs watch", "runs get"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			md, err := meta.build()
			if err != nil {
				return err
			}
			return runClassify(cmd.Context(), app, classifyParams{
				input:        args[0],
				classifierID: classifierID,
				version:      version,
				patchPath:    patchPath,
				password:     password,
				wait:         wait,
				priority:     priority,
				timeout:      timeout,
				metadata:     md,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&classifierID, "using", "", "Classifier ID (required)")
			cmd.Flags().StringVar(&version, "version", "", "Classifier version: latest, draft, or specific (e.g. 1.0)")
			cmd.Flags().StringVar(&patchPath, "patch", "", "Per-run patch merged onto the --using classifier's saved config. Requires --using. Source: inline JSON, path, file:// URI, or '-' for stdin.")
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
	input        string
	classifierID string
	version      string
	patchPath    string
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

	ref, err := uploadOrResolveWith(ctx, app, cli, p.input, p.password)
	if err != nil {
		return err
	}

	file, err := extendx.BuildClassifyFile(ref)
	if err != nil {
		return err
	}

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

	req := &extend.ClassifyRunsCreateRequest{
		Classifier: classifier,
		File:       file,
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
		if final.FailureMessage != nil && *final.FailureMessage != "" {
			return fmt.Errorf("run %s failed: %s", final.ID, *final.FailureMessage)
		}
		return fmt.Errorf("run %s failed", final.ID)
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
