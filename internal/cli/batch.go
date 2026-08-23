package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
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
func uploadAllOrResolve(ctx context.Context, app *App, cli *sdkclient.Client, inputs []string, password string) ([]extendx.FileRef, error) {
	return uploadAllOrResolveWithConcurrency(ctx, app, cli, inputs, defaultUploadConcurrency, password)
}

func uploadAllOrResolveWithConcurrency(ctx context.Context, app *App, cli *sdkclient.Client, inputs []string, concurrency int, password string) ([]extendx.FileRef, error) {
	if concurrency <= 0 {
		concurrency = defaultUploadConcurrency
	}
	if concurrency > len(inputs) {
		concurrency = len(inputs)
	}
	if concurrency <= 1 || len(inputs) <= 1 {
		// Fast path: no goroutines for tiny batches.
		out := make([]extendx.FileRef, 0, len(inputs))
		for _, in := range inputs {
			ref, err := uploadOrResolveWith(ctx, app, cli, in, password)
			if err != nil {
				return nil, err
			}
			out = append(out, ref)
		}
		return out, nil
	}

	out := make([]extendx.FileRef, len(inputs))
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
				ref, err := uploadOrResolveWith(cancelCtx, app, cli, inputs[i], password)
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
	patch             string
	priority          int
	filesFrom         string
	password          string
	uploadConcurrency int
	meta              metaFlags
}

// attach registers the shared batch flags. withPatch controls whether the
// per-run --patch (overrideConfig) flag is offered — it applies to the
// processor batches (extract/classify/split) but not to workflow or parse
// batches, which have no per-run overrideConfig.
func (f *batchFlags) attach(cmd *cobra.Command, processorFlag string, withPatch bool) {
	cmd.Flags().StringVar(&f.using, processorFlag, "", processorFlag+" ID (required)")
	cmd.Flags().StringVar(&f.version, "version", "", "Processor version (latest, draft, or specific)")
	if withPatch {
		cmd.Flags().StringVar(&f.patch, "patch", "", "Per-run patch merged onto the --using processor's saved config (applied to every input). Source: inline JSON, path, file:// URI, or '-' for stdin.")
	}
	cmd.Flags().IntVar(&f.priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
	cmd.Flags().StringVar(&f.filesFrom, "files-from", "", "Path to a file with one input path/URL/id per line (- for stdin)")
	cmd.Flags().StringVar(&f.password, "password", "", "Password for password-protected PDFs (URL inputs only; all inputs must be URLs)")
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

// batchSubmitPrep packages the shared scaffolding every batch-submit
// command needs: resolved input refs, parsed metadata, and the SDK
// client to call CreateBatch on. Each per-kind RunE closure then
// builds the typed request and submits.
type batchSubmitPrep struct {
	Client   *sdkclient.Client
	Refs     []extendx.FileRef
	Metadata map[string]any
}

// prepBatchSubmit runs the boilerplate every "extend X batch" command
// shares: collect inputs (positional + --files-from), build the API
// client, upload-or-resolve each input concurrently, and parse
// --metadata/--tag. The caller then builds its typed SDK request
// (each kind has different request/item types) and calls CreateBatch.
//
// Pulling this out collapses ~15 lines of identical orchestration per
// batch command into one helper call.
func prepBatchSubmit(ctx context.Context, app *App, args []string, f batchFlags) (*batchSubmitPrep, error) {
	return prepBatchSubmitArgs(ctx, app, args, f.filesFrom, f.uploadConcurrency, f.password, f.meta)
}

// prepBatchSubmitArgs is the underlying helper used by both the
// shared batchFlags shape (extract/classify/split/workflow batches)
// and the parse batch, which carries its own flag vars because it has
// different per-kind flags (--engine, --target, etc.) rather than the
// `--using <processor-id>` shape.
func prepBatchSubmitArgs(ctx context.Context, app *App, args []string, filesFrom string, uploadConcurrency int, password string, meta metaFlags) (*batchSubmitPrep, error) {
	inputs, err := collectBatchInputs(args, filesFrom)
	if err != nil {
		return nil, err
	}
	cli, err := app.NewClient()
	if err != nil {
		return nil, err
	}
	refs, err := uploadAllOrResolveWithConcurrency(ctx, app, cli, inputs, uploadConcurrency, password)
	if err != nil {
		return nil, err
	}
	md, err := meta.build()
	if err != nil {
		return nil, err
	}
	return &batchSubmitPrep{Client: cli, Refs: refs, Metadata: md}, nil
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

Track progress with ` + "`extend extract batches watch <id>`" + ` or list
contained runs with ` + "`extend extract runs list --batch <id>`" + `.`,
		Examples: []Example{
			{Label: "From positional args", Cmd: "extend extract batch invoice1.pdf invoice2.pdf --using ex_abc"},
			{Label: "From a list file", Cmd: "extend extract batch --files-from list.txt --using ex_abc"},
			{Label: "From stdin", Cmd: "ls *.pdf | extend extract batch --files-from - --using ex_abc"},
		},
		Gotchas: []string{
			"Maximum 1,000 inputs per batch; submit multiple batches for larger sets.",
			"--metadata and --tag apply to every input identically; per-item metadata is not supported.",
			"Batch submission returns immediately; use 'extend extract batches watch <id>' to follow progress.",
		},
		SeeAlso: []string{"extract", "extract batches watch", "extract batches get", "extract runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			prep, err := prepBatchSubmit(cmd.Context(), app, args, f)
			if err != nil {
				return err
			}
			items := make([]*extend.ExtractRunsCreateBatchRequestInputsItem, len(prep.Refs))
			for i, r := range prep.Refs {
				file, err := extendx.BuildExtractBatchFile(r)
				if err != nil {
					return err
				}
				items[i] = &extend.ExtractRunsCreateBatchRequestInputsItem{
					File:     file,
					Metadata: metadataPtr(prep.Metadata),
				}
			}
			extractor := &extend.ExtractRunsCreateBatchRequestExtractor{
				ID:      f.using,
				Version: versionPtr(f.version),
			}
			if f.patch != "" {
				var override extend.ExtractOverrideConfigJSON
				if err := readJSONInto(f.patch, "--patch", &override); err != nil {
					return err
				}
				extractor.OverrideConfig = &override
			}
			br, err := prep.Client.ExtractRuns.CreateBatch(cmd.Context(), &extend.ExtractRunsCreateBatchRequest{
				Extractor: extractor,
				Inputs:    items,
				Priority:  priorityPtr(f.priority),
			})
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderBatchSubmitted(app, br, "extract")
		},
		Configure: func(cmd *cobra.Command) {
			f.attach(cmd, "using", true)
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

Track progress with ` + "`extend classify batches watch <id>`" + ` or list
contained runs with ` + "`extend classify runs list --batch <id>`" + `.`,
		Examples: []Example{
			{Label: "From positional args", Cmd: "extend classify batch invoice1.pdf invoice2.pdf --using cl_abc"},
			{Label: "From a list file", Cmd: "extend classify batch --files-from list.txt --using cl_abc"},
			{Label: "From stdin", Cmd: "ls *.pdf | extend classify batch --files-from - --using cl_abc"},
		},
		Gotchas: []string{
			"Maximum 1,000 inputs per batch.",
			"--metadata and --tag apply to every input identically; per-item metadata is not supported.",
			"Batch submission returns immediately; use 'extend classify batches watch <id>' to follow progress.",
		},
		SeeAlso: []string{"classify", "classify batches watch", "classify batches get", "classify runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			prep, err := prepBatchSubmit(cmd.Context(), app, args, f)
			if err != nil {
				return err
			}
			items := make([]*extend.ClassifyRunsCreateBatchRequestInputsItem, len(prep.Refs))
			for i, r := range prep.Refs {
				file, err := extendx.BuildClassifyBatchFile(r)
				if err != nil {
					return err
				}
				items[i] = &extend.ClassifyRunsCreateBatchRequestInputsItem{
					File:     file,
					Metadata: metadataPtr(prep.Metadata),
				}
			}
			classifier := &extend.ClassifyRunsCreateBatchRequestClassifier{
				ID:      f.using,
				Version: versionPtr(f.version),
			}
			if f.patch != "" {
				var override extend.ClassifyOverrideConfig
				if err := readJSONInto(f.patch, "--patch", &override); err != nil {
					return err
				}
				classifier.OverrideConfig = &override
			}
			br, err := prep.Client.ClassifyRuns.CreateBatch(cmd.Context(), &extend.ClassifyRunsCreateBatchRequest{
				Classifier: classifier,
				Inputs:     items,
				Priority:   priorityPtr(f.priority),
			})
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderBatchSubmitted(app, br, "classify")
		},
		Configure: func(cmd *cobra.Command) {
			f.attach(cmd, "using", true)
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

Track progress with ` + "`extend split batches watch <id>`" + ` or list
contained runs with ` + "`extend split runs list --batch <id>`" + `.`,
		Examples: []Example{
			{Label: "From positional args", Cmd: "extend split batch bundle1.pdf bundle2.pdf --using spl_abc"},
			{Label: "From a list file", Cmd: "extend split batch --files-from list.txt --using spl_abc"},
		},
		Gotchas: []string{
			"Maximum 1,000 inputs per batch.",
			"--metadata and --tag apply to every input identically; per-item metadata is not supported.",
			"Batch submission returns immediately; use 'extend split batches watch <id>' to follow progress.",
		},
		SeeAlso: []string{"split", "split batches watch", "split batches get", "split runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			prep, err := prepBatchSubmit(cmd.Context(), app, args, f)
			if err != nil {
				return err
			}
			items := make([]*extend.SplitRunsCreateBatchRequestInputsItem, len(prep.Refs))
			for i, r := range prep.Refs {
				file, err := extendx.BuildSplitBatchFile(r)
				if err != nil {
					return err
				}
				items[i] = &extend.SplitRunsCreateBatchRequestInputsItem{
					File:     file,
					Metadata: metadataPtr(prep.Metadata),
				}
			}
			splitter := &extend.SplitRunsCreateBatchRequestSplitter{
				ID:      f.using,
				Version: versionPtr(f.version),
			}
			if f.patch != "" {
				var override extend.SplitOverrideConfig
				if err := readJSONInto(f.patch, "--patch", &override); err != nil {
					return err
				}
				splitter.OverrideConfig = &override
			}
			br, err := prep.Client.SplitRuns.CreateBatch(cmd.Context(), &extend.SplitRunsCreateBatchRequest{
				Splitter: splitter,
				Inputs:   items,
				Priority: priorityPtr(f.priority),
			})
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderBatchSubmitted(app, br, "split")
		},
		Configure: func(cmd *cobra.Command) {
			f.attach(cmd, "using", true)
		},
	}
}

// newParseBatchDoc returns the typed documentation for `extend parse batch`.
// Composed under newParseDoc via CommandDoc.Subcommands.
func newParseBatchDoc(app *App) *CommandDoc {
	var (
		filesFrom           string
		target              string
		engine              string
		engineVersion       string
		chunkStrategy       string
		chunkMinChars       int
		chunkMaxChars       int
		blockOptionsPath    string
		advancedOptionsPath string
		password            string
		priority            int
		uploadConcurrency   int
		meta                metaFlags
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

The same parse tuning as single 'parse' applies to every input:
--chunk-strategy/--chunk-min-chars/--chunk-max-chars, plus --block-options
and --advanced-options (see 'extend parse --help' for the JSON field
catalogs). --password applies to URL inputs only.

Track progress with ` + "`extend parse batches watch <id>`" + ` or list
contained runs with ` + "`extend parse runs list --batch <id>`" + `.`,
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
		SeeAlso: []string{"parse", "parse batches watch", "parse batches get", "parse runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Build (and validate) the config before uploading, so a bad
			// --chunk-strategy/--target combination fails fast rather than
			// after a costly upload of up to 1,000 inputs.
			cfg, err := buildParseConfig(parseParams{
				target:              target,
				engine:              engine,
				engineVersion:       engineVersion,
				chunkStrategy:       chunkStrategy,
				chunkMinChars:       chunkMinChars,
				chunkMaxChars:       chunkMaxChars,
				blockOptionsPath:    blockOptionsPath,
				advancedOptionsPath: advancedOptionsPath,
			})
			if err != nil {
				return err
			}
			prep, err := prepBatchSubmitArgs(cmd.Context(), app, args, filesFrom, uploadConcurrency, password, meta)
			if err != nil {
				return err
			}
			items := make([]*extend.ParseRunsCreateBatchRequestInputsItem, len(prep.Refs))
			for i, r := range prep.Refs {
				file, err := extendx.BuildParseBatchFile(r)
				if err != nil {
					return err
				}
				items[i] = &extend.ParseRunsCreateBatchRequestInputsItem{
					File:     file,
					Metadata: metadataPtr(prep.Metadata),
				}
			}
			br, err := prep.Client.ParseRuns.CreateBatch(cmd.Context(), &extend.ParseRunsCreateBatchRequest{
				Inputs:   items,
				Config:   cfg,
				Priority: priorityPtr(priority),
			})
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderBatchSubmitted(app, br, "parse")
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&filesFrom, "files-from", "", "Path to a file with one input per line (- for stdin)")
			cmd.Flags().StringVar(&target, "target", "markdown", "Parse target: markdown or spatial")
			cmd.Flags().StringVar(&engine, "engine", "", "Engine: parse_performance or parse_light (default: server default)")
			cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Engine version (e.g. latest, 1.0.1, 2.0.0-beta)")
			cmd.Flags().StringVar(&chunkStrategy, "chunk-strategy", "", "Chunking strategy: page|document|section (none omits chunkingStrategy)")
			cmd.Flags().IntVar(&chunkMinChars, "chunk-min-chars", 0, "Minimum characters per chunk (server default if 0)")
			cmd.Flags().IntVar(&chunkMaxChars, "chunk-max-chars", 0, "Maximum characters per chunk (server default if 0)")
			cmd.Flags().StringVar(&blockOptionsPath, "block-options", "", "blockOptions for fine-grained block detection (figures/tables/text/barcodes/keyValue/formulas). Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&advancedOptionsPath, "advanced-options", "", "advancedOptions for parse tuning (returnOcr, pageRanges, etc.). Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&password, "password", "", "Password for password-protected PDFs (URL inputs only; all inputs must be URLs)")
			cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
			cmd.Flags().IntVar(&uploadConcurrency, "upload-concurrency", defaultUploadConcurrency, "Concurrent uploads when local file paths are passed")
			meta.attach(cmd)
		},
	}
}

// newWorkflowBatchDoc returns the typed documentation for `extend
// workflows run batch`. Composed under newWorkflowsRunDoc via
// CommandDoc.Subcommands.
func newWorkflowBatchDoc(app *App) *CommandDoc {
	var (
		f       batchFlags
		secrets []string
	)
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
to fan them out as a single batch. Prefer single-input 'workflows run'
for one-off runs.`,
		Details: `Workflow batches return only a batch_id; unlike processor batches there
is no GET /batch_runs/{id} endpoint for workflow batches, so there are
no 'workflows batches' commands. Track progress with:

    extend workflows runs list --batch <batch-id>`,
		Examples: []Example{
			{Label: "From positional args", Cmd: "extend workflows run batch doc1.pdf doc2.pdf --using workflow_abc"},
			{Label: "From a list file", Cmd: "extend workflows run batch --files-from inputs.txt --using workflow_abc"},
		},
		Gotchas: []string{
			"Workflow batch does not accept --priority (server schema omits it).",
			"Workflow batch does not accept top-level --metadata/--tag (only per-input metadata is allowed; the CLI rejects top-level use).",
			"Workflow batches have no get/watch endpoint; use 'extend workflows runs list --batch <id>' to follow progress.",
		},
		SeeAlso: []string{"workflows run", "workflows runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Workflow batch has two CLI-level rejections that must
			// fire BEFORE we open the API client / upload anything:
			// the server schema rejects top-level priority and
			// metadata. Surface them as flag-validation errors so
			// the user fixes the invocation instead of getting an
			// opaque server-side error after a costly upload.
			if f.priority != 0 {
				return errors.New("workflow batch does not accept --priority (server schema does not include it)")
			}
			if md, err := f.meta.build(); err != nil {
				return err
			} else if md != nil {
				return errors.New("workflow batch does not accept top-level --metadata/--tag (server schema only allows per-input metadata)")
			}
			prep, err := prepBatchSubmit(cmd.Context(), app, args, f)
			if err != nil {
				return err
			}
			// Secrets are a per-input field (the only per-input knob
			// workflow batch allows besides metadata); the same set is
			// applied to every input, mirroring how --metadata works on
			// the processor batches.
			var secretsPtr *extend.RunSecrets
			if len(secrets) > 0 {
				pairs, err := parseKVPairs("--secret", secrets)
				if err != nil {
					return err
				}
				s := make(extend.RunSecrets, len(pairs))
				for k, v := range pairs {
					s[k] = v
				}
				secretsPtr = &s
			}
			items := make([]*extend.WorkflowRunsCreateBatchRequestInputsItem, len(prep.Refs))
			for i, r := range prep.Refs {
				file, err := extendx.BuildWorkflowBatchFile(r)
				if err != nil {
					return err
				}
				items[i] = &extend.WorkflowRunsCreateBatchRequestInputsItem{File: file, Secrets: secretsPtr}
			}
			resp, err := prep.Client.WorkflowRuns.CreateBatch(cmd.Context(), &extend.WorkflowRunsCreateBatchRequest{
				Workflow: &extend.WorkflowReference{
					ID:      f.using,
					Version: versionPtr(f.version),
				},
				Inputs: items,
			})
			if err != nil {
				return fmt.Errorf("submit batch: %w", err)
			}
			return renderWorkflowBatchSubmitted(app, resp, len(items))
		},
		Configure: func(cmd *cobra.Command) {
			f.attach(cmd, "using", false)
			cmd.Flags().StringArrayVar(&secrets, "secret", nil, "key=value secret available to step actions, applied to every input (repeatable)")
		},
	}
}

// renderWorkflowBatchSubmitted formats the workflow-batch submit
// response, which is `{batchId}` only — there's no run count, status,
// or createdAt like in processor/parse batches.
func renderWorkflowBatchSubmitted(app *App, resp *extend.WorkflowRunsCreateBatchResponse, runCount int) error {
	if app.Format != "" {
		return renderWithDefault(app, resp, output.FormatJSON)
	}
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.Out, "%s %s (%d run%s submitted)\n",
		pal.Cyan("⋯"), resp.BatchID, runCount, pluralize(runCount))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Track:   extend workflows runs list --batch %s --all", resp.BatchID))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Note:    workflow batches have no get/watch endpoint; use the list command above"))
	return nil
}

// renderBatchSubmitted formats a processor or parse batch, pointing at
// the typed follow-up commands under verb ("extract", "parse", ...).
func renderBatchSubmitted(app *App, br *extend.BatchRun, verb string) error {
	if app.Format != "" {
		return renderWithDefault(app, br, output.FormatJSON)
	}
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.Out, "%s %s (%s, %d run%s)\n",
		statusIcon(pal, extendx.RunStatus(br.Status)), br.ID, br.Status, br.RunCount, pluralize(br.RunCount))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Watch:   extend %s batches watch %s", verb, br.ID))
	fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Results: extend %s runs list --batch %s", verb, br.ID))
	return nil
}
