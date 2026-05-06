package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

// defaultUploadConcurrency is the parallelism for `uploadAllOrResolve` when a
// command's --upload-concurrency flag isn't passed. Five strikes a balance
// between making batch uploads materially faster (a 100-file batch finishes
// in roughly 1/5 the time vs. serial) and not pummeling the upload endpoint
// from a single CLI invocation.
const defaultUploadConcurrency = 5

// uploadAllOrResolve resolves every input concurrently. file_xxx and https://
// inputs are cheap (no I/O), but local file paths each upload to the API; the
// concurrency cap bounds in-flight POSTs to the upload endpoint.
//
// Order of the returned slice matches the order of inputs. The first error
// returned by any worker cancels the remaining work and is propagated to the
// caller.
func uploadAllOrResolve(ctx context.Context, app *App, cli *client.Client, inputs []string) ([]client.FileRef, error) {
	return uploadAllOrResolveWithConcurrency(ctx, app, cli, inputs, defaultUploadConcurrency)
}

func uploadAllOrResolveWithConcurrency(ctx context.Context, app *App, cli *client.Client, inputs []string, concurrency int) ([]client.FileRef, error) {
	if concurrency <= 0 {
		concurrency = defaultUploadConcurrency
	}
	if concurrency > len(inputs) {
		concurrency = len(inputs)
	}
	if concurrency <= 1 || len(inputs) <= 1 {
		// Fast path: no goroutines for tiny batches.
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

	out := make([]client.FileRef, len(inputs))
	jobs := make(chan int, len(inputs))
	for i := range inputs {
		jobs <- i
	}
	close(jobs)

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if cancelCtx.Err() != nil {
					return
				}
				ref, err := uploadOrResolve(cancelCtx, app, cli, inputs[i])
				if err != nil {
					errOnce.Do(func() {
						firstErr = err
						cancel()
					})
					return
				}
				out[i] = ref
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

type batchFlags struct {
	using             string
	version           string
	priority          int
	filesFrom         string
	uploadConcurrency int
	meta              metaFlags
}

func (f *batchFlags) attach(cmd *cobra.Command, processorFlag string) {
	cmd.Flags().StringVar(&f.using, processorFlag, "", processorFlag+" ID (required)")
	cmd.Flags().StringVar(&f.version, "version", "", "Processor version (latest, draft, or specific)")
	cmd.Flags().IntVar(&f.priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
	cmd.Flags().StringVar(&f.filesFrom, "files-from", "", "Path to a file with one input path/URL/id per line (- for stdin)")
	cmd.Flags().IntVar(&f.uploadConcurrency, "upload-concurrency", defaultUploadConcurrency, "Concurrent uploads when local file paths are passed")
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

// newExtractBatchDoc returns the typed documentation for the
// `extend extract batch` subcommand. Composed under newExtractDoc via
// CommandDoc.Subcommands.
func newExtractBatchDoc(app *App) *CommandDoc {
	var f batchFlags
	return &CommandDoc{
		Use:     "batch <input>...",
		Summary: "Run extraction on up to 1,000 files in one batch",
		Triggers: []string{
			"extract from many documents at once",
			"bulk extract a folder of pdfs",
			"submit an extract batch run",
			"process up to 1000 files in one extract batch",
		},
		WhenToUse: `Use when you have many inputs to extract against the same extractor and
want a single batch run to track. Prefer single-document 'extract' for
one-off requests; prefer 'run batch' if you need a multi-step workflow.`,
		Details: `Per-input metadata may be attached via --metadata/--tag (applied to
every input identically); the server schema does not accept top-level
metadata for processor batches. After submission, the command prints the
batch ID and a hint for following progress.

Track progress with ` + "`extend batches watch <id>`" + ` or list contained
runs with ` + "`extend runs list --type extract --batch <id>`" + `.`,
		Examples: []Example{
			{Label: "From positional args", Cmd: "extend extract batch invoice1.pdf invoice2.pdf --using ex_abc"},
			{Label: "From a list file", Cmd: "extend extract batch --files-from list.txt --using ex_abc"},
			{Label: "From stdin", Cmd: "ls *.pdf | extend extract batch --files-from - --using ex_abc"},
		},
		Gotchas: []string{
			"Maximum 1,000 inputs per batch; submit multiple batches for larger sets.",
			"--metadata and --tag apply to every input identically; per-item metadata is not supported.",
			"Batch submission returns immediately; use 'extend batches watch <id>' to follow progress.",
		},
		SeeAlso: []string{"extract", "batches watch", "batches get", "runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := collectBatchInputs(args, f.filesFrom)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			refs, err := uploadAllOrResolveWithConcurrency(cmd.Context(), app, cli, inputs, f.uploadConcurrency)
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
		Configure: func(cmd *cobra.Command) {
			f.attach(cmd, "using")
		},
	}
}

// newClassifyBatchDoc returns the typed documentation for `extend classify
// batch`. Composed under newClassifyDoc via CommandDoc.Subcommands.
func newClassifyBatchDoc(app *App) *CommandDoc {
	var f batchFlags
	return &CommandDoc{
		Use:     "batch <input>...",
		Summary: "Run classification on up to 1,000 files in one batch",
		Triggers: []string{
			"classify many documents at once",
			"bulk label a folder of pdfs",
			"submit a classify batch run",
			"classify up to 1000 files in one batch",
		},
		WhenToUse: `Use when you have many inputs to classify against the same classifier
and want a single batch run to track. Prefer single-document 'classify'
for one-off requests.`,
		Details: `Per-input metadata is set via --metadata/--tag and applied to every input
identically; the server schema does not accept top-level metadata for
processor batches. After submission, the command prints the batch ID and a
hint for following progress.

Track progress with ` + "`extend batches watch <id>`" + ` or list contained
runs with ` + "`extend runs list --type classify --batch <id>`" + `.`,
		Examples: []Example{
			{Label: "From positional args", Cmd: "extend classify batch invoice1.pdf invoice2.pdf --using cl_abc"},
			{Label: "From a list file", Cmd: "extend classify batch --files-from list.txt --using cl_abc"},
			{Label: "From stdin", Cmd: "ls *.pdf | extend classify batch --files-from - --using cl_abc"},
		},
		Gotchas: []string{
			"Maximum 1,000 inputs per batch.",
			"--metadata and --tag apply to every input identically; per-item metadata is not supported.",
			"Batch submission returns immediately; use 'extend batches watch <id>' to follow progress.",
		},
		SeeAlso: []string{"classify", "batches watch", "batches get", "runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := collectBatchInputs(args, f.filesFrom)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			refs, err := uploadAllOrResolveWithConcurrency(cmd.Context(), app, cli, inputs, f.uploadConcurrency)
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
		Configure: func(cmd *cobra.Command) {
			f.attach(cmd, "using")
		},
	}
}

// newSplitBatchDoc returns the typed documentation for `extend split batch`.
// Composed under newSplitDoc via CommandDoc.Subcommands.
func newSplitBatchDoc(app *App) *CommandDoc {
	var f batchFlags
	return &CommandDoc{
		Use:     "batch <input>...",
		Summary: "Run splitting on up to 1,000 files in one batch",
		Triggers: []string{
			"split many multi-document pdfs at once",
			"bulk split a folder of bundles",
			"submit a split batch run",
			"split up to 1000 files in one batch",
		},
		WhenToUse: `Use when you have many bundle PDFs to split against the same splitter and
want a single batch run to track. Prefer single-document 'split' for
one-off requests.`,
		Details: `Per-input metadata is set via --metadata/--tag and applied to every input
identically; the server schema does not accept top-level metadata for
processor batches.

Track progress with ` + "`extend batches watch <id>`" + ` or list contained
runs with ` + "`extend runs list --type split --batch <id>`" + `.`,
		Examples: []Example{
			{Label: "From positional args", Cmd: "extend split batch bundle1.pdf bundle2.pdf --using spl_abc"},
			{Label: "From a list file", Cmd: "extend split batch --files-from list.txt --using spl_abc"},
		},
		Gotchas: []string{
			"Maximum 1,000 inputs per batch.",
			"--metadata and --tag apply to every input identically; per-item metadata is not supported.",
			"Batch submission returns immediately; use 'extend batches watch <id>' to follow progress.",
		},
		SeeAlso: []string{"split", "batches watch", "batches get", "runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := collectBatchInputs(args, f.filesFrom)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			refs, err := uploadAllOrResolveWithConcurrency(cmd.Context(), app, cli, inputs, f.uploadConcurrency)
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
		Configure: func(cmd *cobra.Command) {
			f.attach(cmd, "using")
		},
	}
}

// newParseBatchDoc returns the typed documentation for `extend parse batch`.
// Composed under newParseDoc via CommandDoc.Subcommands.
func newParseBatchDoc(app *App) *CommandDoc {
	var (
		filesFrom         string
		target            string
		engine            string
		engineVersion     string
		priority          int
		uploadConcurrency int
		meta              metaFlags
	)
	return &CommandDoc{
		Use:     "batch <input>...",
		Summary: "Parse up to 1,000 files in one batch",
		Triggers: []string{
			"parse many documents at once",
			"bulk convert pdfs to markdown",
			"submit a parse batch run",
			"parse up to 1000 files in one batch",
		},
		WhenToUse: `Use when you have many inputs to parse and want a single batch run to
track. Prefer single-document 'parse' for one-off requests.`,
		Details: `Unlike processor batches (extract/classify/split), parse batches do not
take a processor reference; the engine is selected via --engine and
--engine-version. Per-input metadata is set via --metadata/--tag.

Track progress with ` + "`extend batches watch <id>`" + ` or list contained
runs with ` + "`extend runs list --type parse --batch <id>`" + `.`,
		Examples: []Example{
			{Label: "From positional args", Cmd: "extend parse batch file_a file_b"},
			{Label: "Specific engine", Cmd: "extend parse batch --engine parse_performance --engine-version 1.0.1 file_a file_b"},
			{Label: "Spatial target from list", Cmd: "extend parse batch --target spatial --files-from list.txt"},
		},
		Gotchas: []string{
			"Maximum 1,000 inputs per batch.",
			"No processor reference is required; engine is selected via --engine/--engine-version.",
			"Parse runs cannot be cancelled once submitted.",
		},
		SeeAlso: []string{"parse", "batches watch", "batches get", "runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := collectBatchInputs(args, filesFrom)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			refs, err := uploadAllOrResolveWithConcurrency(cmd.Context(), app, cli, inputs, uploadConcurrency)
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
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&filesFrom, "files-from", "", "Path to a file with one input per line (- for stdin)")
			cmd.Flags().StringVar(&target, "target", "markdown", "Parse target: markdown or spatial")
			cmd.Flags().StringVar(&engine, "engine", "", "Engine: parse_performance or parse_light (default: server default)")
			cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Engine version (e.g. latest, 1.0.1, 2.0.0-beta)")
			cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
			cmd.Flags().IntVar(&uploadConcurrency, "upload-concurrency", defaultUploadConcurrency, "Concurrent uploads when local file paths are passed")
			meta.attach(cmd)
		},
	}
}

// newWorkflowBatchDoc returns the typed documentation for `extend run
// batch`. Composed under newRunDoc via CommandDoc.Subcommands.
func newWorkflowBatchDoc(app *App) *CommandDoc {
	var f batchFlags
	return &CommandDoc{
		Use:     "batch <input>...",
		Summary: "Run a workflow on up to 1,000 files in one batch",
		Triggers: []string{
			"start a workflow run on many documents",
			"bulk submit a workflow batch",
			"run an extend workflow on a folder of files",
			"trigger up to 1000 workflow runs at once",
		},
		WhenToUse: `Use when you have many inputs to feed through the same workflow and want
to fan them out as a single batch. Prefer single-input 'run' for one-off
runs.`,
		Details: `Workflow batches return only a batch_id; unlike processor batches there
is no GET /batch_runs/{id} endpoint for workflow batches and
'extend batches watch' will not work on them. Track progress with:

    extend runs list --type workflow --batch <batch-id>`,
		Examples: []Example{
			{Label: "From positional args", Cmd: "extend run batch doc1.pdf doc2.pdf --using workflow_abc"},
			{Label: "From a list file", Cmd: "extend run batch --files-from inputs.txt --using workflow_abc"},
		},
		Gotchas: []string{
			"Workflow batch does not accept --priority (server schema omits it).",
			"Workflow batch does not accept top-level --metadata/--tag (only per-input metadata is allowed; the CLI rejects top-level use).",
			"'extend batches watch' does not work on workflow batches; use 'extend runs list --type workflow --batch <id>' to follow progress.",
		},
		SeeAlso: []string{"run", "runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
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
			refs, err := uploadAllOrResolveWithConcurrency(cmd.Context(), app, cli, inputs, f.uploadConcurrency)
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
		Configure: func(cmd *cobra.Command) {
			f.attach(cmd, "using")
		},
	}
}

// renderWorkflowBatchSubmitted formats the workflow-batch submit response,
// which is `{batchId}` only — there's no run count, status, or createdAt
// like in processor/parse batches.
func renderWorkflowBatchSubmitted(app *App, resp *client.WorkflowBatchResponse, runCount int) error {
	if app.Format != "" {
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
	if app.Format != "" {
		return renderWithDefault(app, br, output.FormatJSON)
	}
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.Out, "%s %s (%s, %d run%s)\n",
		statusIcon(pal, br.Status), br.ID, br.Status, br.RunCount, pluralize(br.RunCount))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Watch:   extend batches watch %s", br.ID))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Results: extend runs list --type <type> --batch %s", br.ID))
	return nil
}

// newBatchesDoc returns the typed documentation for `extend batches` (the
// inspect-and-follow group for batch runs) and its 2 leaves: get, watch.
func newBatchesDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "batches",
		Summary: "Inspect and follow batch runs",
		Group:   "Inspection",
		WhenToUse: `Use these commands to inspect or watch a processor or parse batch run
by its batch ID. Workflow batches do not have a get/watch endpoint; use
'extend runs list --type workflow --batch <id>' for those.`,
		Details: `Operations on batch runs identified by their bpr_/bpar_ ID. Workflow
batches (returned by 'extend run batch') do NOT have a get endpoint;
list their member runs with 'extend runs list --type workflow --batch
<id>' instead.`,
		Subcommands: []*CommandDoc{
			newBatchesGetDoc(app),
			newBatchesWatchDoc(app),
		},
	}
}

func newBatchesGetDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "get <batch-id>",
		Summary: "Show one batch run by ID",
		Triggers: []string{
			"show one batch run",
			"inspect a processor or parse batch",
			"check batch status by id",
		},
		WhenToUse: `Use to retrieve the current status, member-run count, and timestamps
for a single batch run. Does not poll; for live progress use
'extend batches watch'.`,
		Details: `Show one processor or parse batch run, including its overall status,
member-run count, and timestamps. Workflow batches do NOT have a get
endpoint; for those, use 'extend runs list --type workflow --batch <id>'.`,
		Examples: []Example{
			{Label: "Processor batch", Cmd: "extend batches get bpr_abc123"},
			{Label: "Parse batch", Cmd: "extend batches get bpar_xyz"},
		},
		Gotchas: []string{
			"Workflow batches do not have a get endpoint; use 'extend runs list --type workflow --batch <id>' instead.",
		},
		SeeAlso: []string{"batches watch", "runs list"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
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
}

func newBatchesWatchDoc(app *App) *CommandDoc {
	var (
		timeout    time.Duration
		exitStatus bool
	)
	return &CommandDoc{
		Use:     "watch <batch-id>",
		Summary: "Poll a batch run until it reaches a terminal state",
		Triggers: []string{
			"watch a batch run until it finishes",
			"poll a processor or parse batch",
			"block until extract batch completes",
			"follow batch progress live",
		},
		WhenToUse: `Use to block until a processor or parse batch reaches a terminal
state. Combine with --exit-status to gate downstream scripts on success.`,
		Details: `Poll a processor or parse batch and print the final status when it
reaches a terminal state. Workflow batches do not have a get endpoint and
cannot be watched here; use 'extend runs list --type workflow --batch <id>'
to monitor them instead.

Pass --exit-status to make the command exit non-zero when the batch
finishes in FAILED or CANCELLED status, suitable for shell composition:

    extend batches watch bpr_abc --exit-status && downstream-script.sh

Polls every 2s, backing off to 30s.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend batches watch bpr_abc123"},
			{Label: "Custom timeout", Cmd: "extend batches watch bpr_abc123 --timeout 2h"},
			{Label: "Gate downstream script", Cmd: "extend batches watch bpr_abc123 --exit-status"},
		},
		Gotchas: []string{
			"Workflow batches cannot be watched here; use 'extend runs list --type workflow --batch <id>'.",
			"Without --exit-status, the command exits 0 on terminal regardless of FAILED/CANCELLED.",
		},
		SeeAlso:  []string{"batches get", "runs list", "runs watch"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: client.ProfileLong, DefaultsToWait: true},
		Failures: []client.RunStatus{client.StatusFailed, client.StatusCancelled},
		Args:     cobra.ExactArgs(1),
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
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().DurationVar(&timeout, "timeout", 1*time.Hour, "Maximum time to wait")
			cmd.Flags().BoolVar(&exitStatus, "exit-status", false, "Exit non-zero on FAILED or CANCELLED")
		},
	}
}
