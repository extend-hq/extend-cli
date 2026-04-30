package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

func uploadAllOrResolve(ctx context.Context, app *App, cli *client.Client, inputs []string) ([]client.FileRef, error) {
	out := make([]client.FileRef, 0, len(inputs))
	for _, in := range inputs {
		ref, err := uploadOrResolve(ctx, app, cli, in)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, nil
}

type batchFlags struct {
	using     string
	version   string
	priority  int
	filesFrom string
	meta      metaFlags
}

func (f *batchFlags) attach(cmd *cobra.Command, processorFlag string) {
	cmd.Flags().StringVar(&f.using, processorFlag, "", processorFlag+" ID (required)")
	cmd.Flags().StringVar(&f.version, "version", "", "Processor version (latest, draft, or specific)")
	cmd.Flags().IntVar(&f.priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
	cmd.Flags().StringVar(&f.filesFrom, "files-from", "", "Path to a file with one input path/URL/id per line (- for stdin)")
	f.meta.attach(cmd)
	_ = cmd.MarkFlagRequired(processorFlag)
}

func collectBatchInputs(args []string, filesFrom string) ([]string, error) {
	inputs := append([]string(nil), args...)
	if filesFrom != "" {
		var r *bufio.Scanner
		if filesFrom == "-" {
			r = bufio.NewScanner(os.Stdin)
		} else {
			f, err := os.Open(filesFrom)
			if err != nil {
				return nil, fmt.Errorf("read --files-from: %w", err)
			}
			defer f.Close()
			r = bufio.NewScanner(f)
		}
		for r.Scan() {
			line := r.Text()
			if line != "" {
				inputs = append(inputs, line)
			}
		}
		if err := r.Err(); err != nil {
			return nil, fmt.Errorf("read --files-from: %w", err)
		}
	}
	if len(inputs) == 0 {
		return nil, errors.New("no inputs provided (pass file paths/URLs/file_ids as args, or use --files-from)")
	}
	if len(inputs) > 1000 {
		return nil, fmt.Errorf("too many inputs (%d); maximum is 1000 per batch", len(inputs))
	}
	return inputs, nil
}

func newExtractBatchCommand(app *App) *cobra.Command {
	var f batchFlags
	cmd := &cobra.Command{
		Use:   "batch <input>...",
		Short: "Run extraction on up to 1,000 files in one batch",
		Long: `Run extraction on up to 1,000 files in one batch. Per-input metadata may be
attached via --metadata/--tag (applied to every input identically); the server
schema does not accept top-level metadata.`,
		Example: `  extend extract batch invoice1.pdf invoice2.pdf --using ex_abc
  extend extract batch --files-from list.txt --using ex_abc
  ls *.pdf | extend extract batch --files-from - --using ex_abc`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := collectBatchInputs(args, f.filesFrom)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			refs, err := uploadAllOrResolve(cmd.Context(), app, cli, inputs)
			if err != nil {
				return err
			}
			md, err := f.meta.build()
			if err != nil {
				return err
			}
			items := make([]client.ProcessorBatchItem, len(refs))
			for i, r := range refs {
				items[i] = client.ProcessorBatchItem{File: r, Metadata: md}
			}
			in := client.CreateExtractBatchInput{
				Extractor: &client.ExtractorRef{ID: f.using, Version: f.version},
				Inputs:    items,
			}
			if f.priority > 0 {
				in.Priority = &f.priority
			}
			br, err := cli.CreateExtractRunBatch(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderBatchSubmitted(app, br)
		},
	}
	f.attach(cmd, "using")
	SetIOAnnotations(cmd, OutputPretty, OutputJSON)
	return cmd
}

func newClassifyBatchCommand(app *App) *cobra.Command {
	var f batchFlags
	cmd := &cobra.Command{
		Use:   "batch <input>...",
		Short: "Run classification on up to 1,000 files in one batch",
		Long: `Run classification on up to 1,000 files in one batch.

Per-input metadata is set via --metadata/--tag and applied to every input
identically; the server schema does not accept top-level metadata for
processor batches. After submission, the command prints the batch ID and a
hint for following progress.

Track progress with ` + "`extend batches watch <id>`" + ` or list contained
runs with ` + "`extend runs list --type classify --batch <id>`" + `.`,
		Example: `  extend classify batch invoice1.pdf invoice2.pdf --using cl_abc
  extend classify batch --files-from list.txt --using cl_abc
  ls *.pdf | extend classify batch --files-from - --using cl_abc`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := collectBatchInputs(args, f.filesFrom)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			refs, err := uploadAllOrResolve(cmd.Context(), app, cli, inputs)
			if err != nil {
				return err
			}
			md, err := f.meta.build()
			if err != nil {
				return err
			}
			items := make([]client.ProcessorBatchItem, len(refs))
			for i, r := range refs {
				items[i] = client.ProcessorBatchItem{File: r, Metadata: md}
			}
			in := client.CreateClassifyBatchInput{
				Classifier: &client.ClassifierRef{ID: f.using, Version: f.version},
				Inputs:     items,
			}
			if f.priority > 0 {
				in.Priority = &f.priority
			}
			br, err := cli.CreateClassifyRunBatch(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderBatchSubmitted(app, br)
		},
	}
	f.attach(cmd, "using")
	SetIOAnnotations(cmd, OutputPretty, OutputJSON)
	return cmd
}

func newSplitBatchCommand(app *App) *cobra.Command {
	var f batchFlags
	cmd := &cobra.Command{
		Use:   "batch <input>...",
		Short: "Run splitting on up to 1,000 files in one batch",
		Long: `Run splitting on up to 1,000 multi-document files in one batch.

Per-input metadata is set via --metadata/--tag and applied to every input
identically; the server schema does not accept top-level metadata for
processor batches.

Track progress with ` + "`extend batches watch <id>`" + ` or list contained
runs with ` + "`extend runs list --type split --batch <id>`" + `.`,
		Example: `  extend split batch bundle1.pdf bundle2.pdf --using spl_abc
  extend split batch --files-from list.txt --using spl_abc`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := collectBatchInputs(args, f.filesFrom)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			refs, err := uploadAllOrResolve(cmd.Context(), app, cli, inputs)
			if err != nil {
				return err
			}
			md, err := f.meta.build()
			if err != nil {
				return err
			}
			items := make([]client.ProcessorBatchItem, len(refs))
			for i, r := range refs {
				items[i] = client.ProcessorBatchItem{File: r, Metadata: md}
			}
			in := client.CreateSplitBatchInput{
				Splitter: &client.SplitterRef{ID: f.using, Version: f.version},
				Inputs:   items,
			}
			if f.priority > 0 {
				in.Priority = &f.priority
			}
			br, err := cli.CreateSplitRunBatch(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderBatchSubmitted(app, br)
		},
	}
	f.attach(cmd, "using")
	SetIOAnnotations(cmd, OutputPretty, OutputJSON)
	return cmd
}

func newParseBatchCommand(app *App) *cobra.Command {
	var (
		filesFrom     string
		target        string
		engine        string
		engineVersion string
		priority      int
		meta          metaFlags
	)
	cmd := &cobra.Command{
		Use:   "batch <input>...",
		Short: "Parse up to 1,000 files in one batch",
		Long: `Parse up to 1,000 files in one batch using the specified engine.

Unlike processor batches (extract/classify/split), parse batches do not
take a processor reference; the engine is selected via --engine and
--engine-version. Per-input metadata is set via --metadata/--tag.

Track progress with ` + "`extend batches watch <id>`" + ` or list contained
runs with ` + "`extend runs list --type parse --batch <id>`" + `.`,
		Example: `  extend parse batch file_a file_b
  extend parse batch --engine parse_performance --engine-version 1.0.1 file_a file_b
  extend parse batch --target spatial --files-from list.txt`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := collectBatchInputs(args, filesFrom)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			refs, err := uploadAllOrResolve(cmd.Context(), app, cli, inputs)
			if err != nil {
				return err
			}
			md, err := meta.build()
			if err != nil {
				return err
			}
			items := make([]client.ParseBatchItem, len(refs))
			for i, r := range refs {
				items[i] = client.ParseBatchItem{File: r, Metadata: md}
			}
			in := client.CreateParseBatchInput{
				Inputs: items,
				Config: &client.ParseConfig{
					Target:        target,
					Engine:        engine,
					EngineVersion: engineVersion,
				},
			}
			if priority > 0 {
				in.Priority = &priority
			}
			br, err := cli.CreateParseRunBatch(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderBatchSubmitted(app, br)
		},
	}
	cmd.Flags().StringVar(&filesFrom, "files-from", "", "Path to a file with one input per line (- for stdin)")
	cmd.Flags().StringVar(&target, "target", "markdown", "Parse target: markdown or spatial")
	cmd.Flags().StringVar(&engine, "engine", "", "Engine: parse_performance or parse_light (default: server default)")
	cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Engine version (e.g. latest, 1.0.1, 2.0.0-beta)")
	cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
	meta.attach(cmd)
	SetIOAnnotations(cmd, OutputPretty, OutputJSON)
	return cmd
}

func newWorkflowBatchCommand(app *App) *cobra.Command {
	var f batchFlags
	cmd := &cobra.Command{
		Use:   "batch <input>...",
		Short: "Run a workflow on up to 1,000 files in one batch",
		Long: `Run a workflow on up to 1,000 files in one batch. Workflow batches return
only a batch_id; unlike processor batches there is no GET /batch_runs/{id}
endpoint for workflow batches and 'extend batches watch' will not work on
them. Track progress with:

    extend runs list --type workflow --batch <batch-id>`,
		Example: `  extend run batch doc1.pdf doc2.pdf --workflow workflow_abc
  extend run batch --files-from inputs.txt --workflow workflow_abc`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := collectBatchInputs(args, f.filesFrom)
			if err != nil {
				return err
			}
			if f.priority != 0 {
				return errors.New("workflow batch does not accept --priority (server schema does not include it)")
			}
			md, err := f.meta.build()
			if err != nil {
				return err
			}
			if md != nil {
				return errors.New("workflow batch does not accept top-level --metadata/--tag (server schema only allows per-input metadata)")
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			refs, err := uploadAllOrResolve(cmd.Context(), app, cli, inputs)
			if err != nil {
				return err
			}
			items := make([]client.WorkflowBatchItem, len(refs))
			for i, r := range refs {
				items[i] = client.WorkflowBatchItem{File: r}
			}
			in := client.CreateWorkflowBatchInput{
				Workflow: &client.WorkflowRef{ID: f.using, Version: f.version},
				Inputs:   items,
			}
			resp, err := cli.CreateWorkflowRunBatch(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderWorkflowBatchSubmitted(app, resp, len(items))
		},
	}
	f.attach(cmd, "workflow")
	SetIOAnnotations(cmd, OutputPretty, OutputJSON)
	return cmd
}

// renderWorkflowBatchSubmitted formats the workflow-batch submit response,
// which is `{batchId}` only — there's no run count, status, or createdAt
// like in processor/parse batches.
func renderWorkflowBatchSubmitted(app *App, resp *client.WorkflowBatchResponse, runCount int) error {
	if app.Format != "" || !app.IO.IsStdoutTTY() {
		return renderWithDefault(app, resp, output.FormatJSON)
	}
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.Out, "%s %s (%d run%s submitted)\n",
		pal.Cyan("⋯"), resp.BatchID, runCount, pluralize(runCount))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Track:   extend runs list --type workflow --batch %s --all", resp.BatchID))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Note:    workflow batches do not support 'extend batches watch'; use the list command above"))
	return nil
}

func renderBatchSubmitted(app *App, br *client.BatchRun) error {
	if app.Format != "" || !app.IO.IsStdoutTTY() {
		return renderWithDefault(app, br, output.FormatJSON)
	}
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.Out, "%s %s (%s, %d run%s)\n",
		statusIcon(pal, br.Status), br.ID, br.Status, br.RunCount, pluralize(br.RunCount))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Watch:   extend batches watch %s", br.ID))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Results: extend runs list --type <type> --batch %s", br.ID))
	return nil
}

func newBatchesCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batches",
		Short: "Inspect and follow batch runs",
	}
	cmd.AddCommand(newBatchesGetCommand(app))
	cmd.AddCommand(newBatchesWatchCommand(app))
	return cmd
}

func newBatchesGetCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <batch-id>",
		Short: "Show one batch run by ID",
		Long: `Show one processor or parse batch run, including its overall status,
member-run count, and timestamps. Workflow batches do NOT have a get
endpoint; for those, use 'extend runs list --type workflow --batch <id>'.`,
		Example: `  extend batches get bpr_abc123
  extend batches get bpar_xyz`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			br, err := cli.GetBatchRun(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderWithDefault(app, br, output.FormatJSON)
		},
	}
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}

func newBatchesWatchCommand(app *App) *cobra.Command {
	var (
		timeout    time.Duration
		exitStatus bool
	)
	cmd := &cobra.Command{
		Use:   "watch <batch-id>",
		Short: "Poll a batch run until it reaches a terminal state",
		Long: `Poll a processor or parse batch and print the final status when it
reaches a terminal state. Workflow batches do not have a get endpoint and
cannot be watched here; use 'extend runs list --type workflow --batch <id>'
to monitor them instead.

Pass --exit-status to make the command exit non-zero when the batch
finishes in FAILED or CANCELLED status, suitable for shell composition:

    extend batches watch bpr_abc --exit-status && downstream-script.sh

Polls every 2s, backing off to 30s.`,
		Example: `  extend batches watch bpr_abc123
  extend batches watch bpr_abc123 --timeout 2h
  extend batches watch bpr_abc123 --exit-status`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			id := args[0]
			sp := app.IO.StartSpinner(fmt.Sprintf("Batch %s: ?", id))
			final, err := cli.WaitForBatchRun(cmd.Context(), id, client.WaitProfileOptions(client.ProfileLong, timeout), func(r *client.BatchRun) {
				sp.Update(fmt.Sprintf("Batch %s: %s (%d run%s)", r.ID, r.Status, r.RunCount, pluralize(r.RunCount)))
			})
			sp.Stop("")
			if err != nil {
				return fmt.Errorf("wait: %w", err)
			}
			if err := renderBatchSubmitted(app, final); err != nil {
				return err
			}
			if exitStatus {
				switch final.Status {
				case client.StatusFailed:
					return fmt.Errorf("batch %s failed", id)
				case client.StatusCancelled:
					return fmt.Errorf("batch %s was cancelled", id)
				}
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 1*time.Hour, "Maximum time to wait")
	cmd.Flags().BoolVar(&exitStatus, "exit-status", false, "Exit non-zero on FAILED or CANCELLED")
	SetIOAnnotations(cmd, OutputPretty, OutputJSON)
	SetWaitAnnotations(cmd, client.ProfileLong, true)
	SetLifecycleFailureCodes(cmd, client.StatusFailed, client.StatusCancelled)
	return cmd
}
