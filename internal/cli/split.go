package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newSplitDoc returns the typed documentation for `extend split` and its
// `extend split batch` subcommand.
func newSplitDoc(app *App) *CommandDoc {
	var (
		splitterID string
		version    string
		patchPath  string
		password   string
		wait       bool
		priority   int
		timeout    time.Duration
		meta       metaFlags
	)

	return &CommandDoc{
		Use:     "split <input>",
		Summary: "Split a multi-document PDF into segments",
		Group:   "Actions",
		Triggers: []string{
			"split a multi-document pdf into segments",
			"separate concatenated pdfs into individual documents",
			"detect document boundaries in a bundle",
			"break a combined pdf into per-document pieces",
			"run a splitter on a pdf",
		},
		WhenToUse: `Use when you have a single PDF that contains multiple distinct documents
and need to identify the page ranges and types of each. Prefer 'classify'
when the input is a single document and you only need its category.`,
		Details: `Run a splitter against a multi-document PDF and return the segments
(page ranges + classification per segment).

<input> can be:
  - a local file path (auto-uploaded)
  - a file_xxx ID (use a previously uploaded file)
  - an https:// URL (Extend fetches the document)

--patch applies a per-run partial merge onto the --using splitter's
saved config. Source: inline JSON, a plain file path, an absolute
file:// URI, or '-' to read from stdin. --patch does NOT replace the
splitter; pass --using to pick the splitter and --patch to vary it
without modifying the persisted version.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend split combined.pdf --using spl_abc"},
			{Label: "JSON output", Cmd: "extend split combined.pdf --using spl_abc -o json"},
			{Label: "Patch a saved splitter for this run", Cmd: "extend split combined.pdf --using spl_abc --patch tweaks.json"},
			{Label: "Inline patch", Cmd: `extend split combined.pdf --using spl_abc --patch '{"foo":"bar"}'`},
			{Label: "Count segments via jq", Cmd: "extend split combined.pdf --using spl_abc --jq '.output.splits | length' -o raw"},
		},
		Gotchas: []string{
			"--using is required (no inline-config option for split).",
		},
		SeeAlso:  []string{"parse", "split batch", "runs watch", "runs get"},
		Output:   OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: client.ProfileShort, DefaultsToWait: true},
		Failures: []client.RunStatus{client.StatusFailed, client.StatusCancelled},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			md, err := meta.build()
			if err != nil {
				return err
			}
			return runSplit(cmd.Context(), app, splitParams{
				input:      args[0],
				splitterID: splitterID,
				version:    version,
				patchPath:  patchPath,
				password:   password,
				wait:       wait,
				priority:   priority,
				timeout:    timeout,
				metadata:   md,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&splitterID, "using", "", "Splitter ID (required)")
			cmd.Flags().StringVar(&version, "version", "", "Splitter version: latest, draft, or specific (e.g. 1.0)")
			cmd.Flags().StringVar(&patchPath, "patch", "", "Per-run patch merged onto the --using splitter's saved config. Requires --using. Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run ID immediately)")
			cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
			meta.attach(cmd)
			_ = cmd.MarkFlagRequired("using")
		},
		Subcommands: []*CommandDoc{newSplitBatchDoc(app)},
	}
}

type splitParams struct {
	input      string
	splitterID string
	version    string
	patchPath  string
	password   string
	wait       bool
	priority   int
	timeout    time.Duration
	metadata   map[string]any
}

func runSplit(ctx context.Context, app *App, p splitParams) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	ref, err := uploadOrResolveWith(ctx, app, cli, p.input, p.password)
	if err != nil {
		return err
	}

	splitter := &client.SplitterRef{ID: p.splitterID, Version: p.version}
	if p.patchPath != "" {
		raw, err := readJSONFile(p.patchPath, "--patch")
		if err != nil {
			return err
		}
		splitter.OverrideConfig = raw
	}
	in := client.CreateSplitRunInput{
		Splitter: splitter,
		File:     ref,
		Metadata: p.metadata,
	}
	if p.priority > 0 {
		in.Priority = &p.priority
	}

	run, err := cli.CreateSplitRun(ctx, in)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if !p.wait {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := cli.WaitForSplitRun(ctx, run.ID, client.WaitProfileOptions(client.ProfileShort, p.timeout), func(r *client.SplitRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return formatActionWaitError(err, run.ID)
	}

	if err := renderSplitResult(app, final); err != nil {
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

func renderSplitResult(app *App, run *client.SplitRun) error {
	if app.Format != "" || app.JQ != "" {
		return renderWithDefault(app, run, output.FormatJSON)
	}
	if run.Output == nil || len(run.Output.Splits) == 0 {
		fmt.Fprintln(app.IO.Out, "No segments returned.")
		return nil
	}
	rows := make([][]string, 0, len(run.Output.Splits))
	for _, s := range run.Output.Splits {
		rows = append(rows, []string{
			pageRange(s.StartPage, s.EndPage),
			s.Type,
			s.Identifier,
		})
	}
	return output.RenderTable(app.IO.Out, []string{"pages", "type", "identifier"}, rows)
}

func pageRange(start, end int) string {
	if start == end {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}
