package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"
	"github.com/extend-hq/extend-go-sdk/option"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newParseDoc returns the typed documentation for `extend parse` and its
// `extend parse batch` subcommand.
func newParseDoc(app *App) *CommandDoc {
	var (
		target              string
		engine              string
		engineVersion       string
		chunkStrategy       string
		chunkMinChars       int
		chunkMaxChars       int
		blockOptionsPath    string
		advancedOptionsPath string
		password            string
		zeroRetention       bool
		searchablePDF       string
		wait                bool
		timeout             time.Duration
		meta                metaFlags
	)

	return &CommandDoc{
		Use:     "parse <input>",
		Summary: "Parse a document into structured text",
		Group:   "Actions",
		Triggers: []string{
			"convert a pdf to llm-ready markdown",
			"parse a document into structured text",
			"extract raw text or layout from a document",
			"chunk a document for retrieval",
			"render a pdf as markdown",
		},
		WhenToUse: `Use when you need raw text, markdown, or spatial layout from a document
without a predefined schema. Prefer 'extract' when you have a configured
extractor and need typed fields back; prefer 'classify' when all you need
is a category label.`,
		Details: `<input> can be:
  - a local file path (auto-uploaded)
  - a file_xxx ID (use a previously uploaded file)
  - an https:// URL (Extend fetches the document)

By default, output is rendered as Markdown:
  - When stdout is a terminal, it is rendered with styling.
  - When piped, raw Markdown is emitted (composes with downstream tools).
Pass -o json/yaml for the full run object, or -o markdown to force raw.

Chunking is controlled by --chunk-strategy + --chunk-min-chars/--chunk-max-chars.
For finer-grained block detection (figures, tables, barcodes, etc.), pass
--block-options as inline JSON, a plain file path, an absolute file:// URI,
or '-' to read from stdin. --advanced-options accepts the remaining tuning
knobs in the same forms.

Zero data retention (--zero-retention): Extend deletes the run's data once
its result has been read. The result can be read once: by the first fetch
after the run finishes, and only within 60 minutes. With --wait (the
default) the CLI prints it, so save it then; a later 'extend parse runs get'
returns an empty output. With --wait=false, fetch it once with
'extend parse runs get <id>' within the hour.
  - A URL input becomes a file that Extend deletes with the run.
  - A file_xxx ID keeps its file; only the run's data is deleted.
  - A local file is uploaded, then deleted by the CLI once the run ends,
    so --wait=false can't be used with a local file.
Available on the Scale plan and above, or when Extend enables it.

Searchable PDF (--searchable-pdf <path>): adds the recognized words to the
PDF as an invisible text layer, so it can be searched and copied, and saves
the PDF to <path> ('-' writes the PDF to stdout instead of the markdown).
Pages that already have text are left as they are. PDF inputs only; it sets
advancedOptions.embedOcrInPdf.

` + parseOptionsDoc(),
		Examples: []Example{
			{Label: "Basic", Cmd: "extend parse contract.pdf"},
			{Label: "Save raw markdown", Cmd: "extend parse contract.pdf -o markdown > contract.md"},
			{Label: "Count chunks via jq", Cmd: "extend parse contract.pdf -o json | jq '.output.chunks | length'"},
			{Label: "Specific engine", Cmd: "extend parse contract.pdf --engine parse_performance --engine-version 1.0.1"},
			{Label: "Section chunking", Cmd: "extend parse contract.pdf --chunk-strategy section --chunk-max-chars 4000"},
			{Label: "Inline advanced options", Cmd: `extend parse contract.pdf --advanced-options '{"pageRanges":"1-3"}'`},
			{Label: "Block options from file", Cmd: "extend parse contract.pdf --block-options block-opts.json"},
			{Label: "Absolute file URI", Cmd: "extend parse contract.pdf --advanced-options file:///absolute/path/advanced.json"},
			{Label: "Zero data retention", Cmd: "extend parse lab-report.pdf --zero-retention -o markdown > lab-report.md"},
			{Label: "Searchable PDF", Cmd: "extend parse scan.pdf --searchable-pdf scan-searchable.pdf"},
		},
		Gotchas: []string{
			"--target spatial does not support --chunk-strategy section.",
			"--jq cannot be combined with -o markdown; use -o json --jq and select the markdown chunk paths.",
			"With --zero-retention the output can be read only once, so save what the command prints.",
			"--searchable-pdf adds the text layer to the stored file itself, so a file_xxx input is changed in place.",
		},
		SeeAlso:  []string{"extract", "classify", "parse batch", "parse runs watch", "parse runs get", "parse runs cancel"},
		Output:   OutputSpec{TTY: OutputMarkdown, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			md, err := meta.build()
			if err != nil {
				return err
			}
			return runParse(cmd.Context(), app, parseParams{
				input:               args[0],
				target:              target,
				engine:              engine,
				engineVersion:       engineVersion,
				chunkStrategy:       chunkStrategy,
				chunkMinChars:       chunkMinChars,
				chunkMaxChars:       chunkMaxChars,
				blockOptionsPath:    blockOptionsPath,
				advancedOptionsPath: advancedOptionsPath,
				password:            password,
				zeroRetention:       zeroRetention,
				searchablePDF:       searchablePDF,
				wait:                wait,
				timeout:             timeout,
				metadata:            md,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&target, "target", "markdown", "Parse target: markdown or spatial")
			cmd.Flags().StringVar(&engine, "engine", "", "Engine: parse_auto, parse_performance, or parse_light (default: server default)")
			cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Engine version (e.g. latest, 1.0.1, 2.0.0-beta)")
			cmd.Flags().StringVar(&chunkStrategy, "chunk-strategy", "", "Chunking strategy: page|document|section (none omits chunkingStrategy)")
			cmd.Flags().IntVar(&chunkMinChars, "chunk-min-chars", 0, "Minimum characters per chunk (server default if 0)")
			cmd.Flags().IntVar(&chunkMaxChars, "chunk-max-chars", 0, "Maximum characters per chunk (server default if 0)")
			cmd.Flags().StringVar(&blockOptionsPath, "block-options", "", "blockOptions for fine-grained block detection (figures/tables/text/barcodes/keyValue/formulas). Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&advancedOptionsPath, "advanced-options", "", "advancedOptions for parse tuning (returnOcr, pageRanges, etc.). Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().BoolVar(&zeroRetention, "zero-retention", false, "Have Extend delete the run's data once the result is read (dataRetention mode zero); a local file's upload is deleted when the run ends")
			cmd.Flags().StringVar(&searchablePDF, "searchable-pdf", "", "Add a searchable text layer to the PDF and save it to this path ('-' for stdout)")
			cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run ID immediately)")
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
			meta.attach(cmd)
		},
		Subcommands: []*CommandDoc{newParseBatchDoc(app), parseRunsSpec().doc(app), parseBatchesSpec().doc(app)},
	}
}

type parseParams struct {
	input               string
	target              string
	engine              string
	engineVersion       string
	chunkStrategy       string
	chunkMinChars       int
	chunkMaxChars       int
	blockOptionsPath    string
	advancedOptionsPath string
	password            string
	zeroRetention       bool
	searchablePDF       string
	wait                bool
	timeout             time.Duration
	metadata            map[string]any
}

func runParse(ctx context.Context, app *App, p parseParams) error {
	inputRef, localPath, err := extendx.ResolveInput(p.input)
	if err != nil {
		return err
	}
	localUpload := localPath != "" && localPath != "-"
	if p.zeroRetention && localUpload && !p.wait {
		return errors.New("--wait=false can't be used with a local file and --zero-retention, because the CLI deletes the upload when the run ends; pass a URL or file ID instead")
	}
	if p.searchablePDF != "" && !p.wait {
		return errors.New("--searchable-pdf needs the run to finish; drop --wait=false, or set advancedOptions.embedOcrInPdf and download the run's file later with 'extend files download <file-id>'")
	}
	if p.searchablePDF != "" && p.zeroRetention && inputRef.URL != "" {
		return errors.New("--searchable-pdf with --zero-retention needs a local file or file ID; Extend deletes a URL input's file with the run, so the PDF can't be downloaded")
	}

	cfg, err := buildParseConfig(p)
	if err != nil {
		return err
	}

	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	ref, err := uploadOrResolveWith(ctx, app, cli, p.input, p.password)
	if err != nil {
		return err
	}
	// With --zero-retention the CLI's own upload must not outlive the run.
	uploadToDelete := ""
	if p.zeroRetention && localUpload {
		uploadToDelete = ref.ID
	}

	file, err := extendx.BuildParseFile(ref)
	if err != nil {
		return errors.Join(err, deleteRunUpload(ctx, app, cli, uploadToDelete))
	}

	req := &extend.ParseRunsCreateRequest{
		File:   file,
		Config: cfg,
	}
	if p.metadata != nil {
		md := extend.RunMetadata(p.metadata)
		req.Metadata = &md
	}
	if p.zeroRetention {
		req.DataRetention = &extend.DataRetention{Mode: extend.DataRetentionModeZero.Ptr()}
	}

	run, err := extendx.StartOnce(ctx, func(opts ...option.RequestOption) (*extend.ParseRun, error) {
		return cli.ParseRuns.Create(ctx, req, opts...)
	})
	if err != nil {
		return errors.Join(fmt.Errorf("create run: %w", err), deleteRunUpload(ctx, app, cli, uploadToDelete))
	}

	if !p.wait {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := waitForParseRun(ctx, cli, run.ID, extendx.WaitProfileOptions(extendx.ProfileShort, p.timeout), func(r *extend.ParseRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		if uploadToDelete != "" {
			fmt.Fprintf(app.IO.ErrOut, "Left %s in place because run %s hasn't finished; delete it with 'extend files delete %s' once it ends\n", uploadToDelete, run.ID, uploadToDelete)
		}
		return formatActionWaitError(err, run.ID, "extend parse runs watch")
	}

	status := extendx.RunStatus(final.Status)
	resultErr := writeParseResult(ctx, app, cli, final, p)
	cleanupErr := deleteRunUpload(ctx, app, cli, uploadToDelete)
	switch {
	case status == extendx.StatusFailed:
		return errors.Join(runFailureError(final.ID, final.FailureReason, final.FailureMessage), cleanupErr)
	case status == extendx.StatusCancelled:
		return errors.Join(fmt.Errorf("run %s was cancelled", final.ID), cleanupErr)
	}
	return errors.Join(resultErr, cleanupErr)
}

// writeParseResult prints the finished run and, with --searchable-pdf,
// saves the processed PDF. A PDF sent to stdout replaces the printed run.
func writeParseResult(ctx context.Context, app *App, cli *sdkclient.Client, run *extend.ParseRun, p parseParams) error {
	if p.searchablePDF == "" || extendx.RunStatus(run.Status) != extendx.StatusProcessed {
		return renderParseResult(app, run, p.target)
	}
	if run.File == nil || run.File.ID == "" {
		return fmt.Errorf("run %s has no file to save as a searchable PDF", run.ID)
	}
	if run.File.Type == nil || *run.File.Type != extend.FileTypePdf {
		fileType := "unknown"
		if run.File.Type != nil {
			fileType = string(*run.File.Type)
		}
		return fmt.Errorf("--searchable-pdf only works on PDFs; %s is type %s, so no text layer was added", run.File.Name, fileType)
	}
	if p.searchablePDF == "-" {
		_, err := extendx.DownloadFile(ctx, cli, run.File.ID, app.IO.Out)
		return err
	}
	if err := renderParseResult(app, run, p.target); err != nil {
		return err
	}
	return writeFileFromExtend(ctx, app, cli, run.File.ID, p.searchablePDF)
}

// deleteRunUpload deletes a file the CLI uploaded for a single run. An
// empty id is a no-op.
func deleteRunUpload(ctx context.Context, app *App, cli *sdkclient.Client, id string) error {
	if id == "" {
		return nil
	}
	if _, err := cli.Files.Delete(ctx, id, &extend.FilesDeleteRequest{}); err != nil {
		return fmt.Errorf("delete uploaded file %s: %w; delete it with 'extend files delete %s'", id, err, id)
	}
	printUploadProgress(app, "Deleted %s (uploaded for this run)\n", id)
	return nil
}

func waitForParseRun(ctx context.Context, c *sdkclient.Client, id string, opts extendx.WaitOptions, onPoll func(*extend.ParseRun)) (*extend.ParseRun, error) {
	return extendx.PollForRun(ctx,
		func(ctx context.Context) (*extend.ParseRun, error) {
			return c.ParseRuns.Retrieve(ctx, id, &extend.ParseRunsRetrieveRequest{})
		},
		func(r *extend.ParseRun) extendx.RunStatus { return extendx.RunStatus(r.Status) },
		opts, onPoll,
	)
}

// buildParseConfig assembles a ParseConfig from CLI parseParams. Returns
// nil when no config-relevant flags are set so the on-the-wire body
// omits `config` entirely (server falls back to defaults).
func buildParseConfig(p parseParams) (*extend.ParseConfig, error) {
	cfg := &extend.ParseConfig{}
	if p.target != "" {
		t, err := extend.NewParseConfigTargetFromString(p.target)
		if err != nil {
			return nil, fmt.Errorf("--target: %w", err)
		}
		cfg.Target = &t
	}
	if p.engine != "" {
		// Passed through unchecked so an engine the API adds works before
		// the SDK lists it; the API rejects unknown names itself.
		e := extend.ParseConfigEngine(p.engine)
		cfg.Engine = &e
	}
	if p.engineVersion != "" {
		cfg.EngineVersion = extend.String(p.engineVersion)
	}
	chunkStrategy := p.chunkStrategy
	if chunkStrategy == "none" {
		if p.chunkMinChars > 0 || p.chunkMaxChars > 0 {
			return nil, fmt.Errorf("--chunk-min-chars/--chunk-max-chars cannot be used with --chunk-strategy none")
		}
		chunkStrategy = ""
	}
	if err := validateParseChunkStrategy(chunkStrategy); err != nil {
		return nil, err
	}
	if chunkStrategy == "section" && p.target == "spatial" {
		return nil, fmt.Errorf("--chunk-strategy section is not supported with --target spatial")
	}
	if chunkStrategy != "" || p.chunkMinChars > 0 || p.chunkMaxChars > 0 {
		cs := &extend.ParseConfigChunkingStrategy{}
		if chunkStrategy != "" {
			st, err := extend.NewParseConfigChunkingStrategyTypeFromString(chunkStrategy)
			if err != nil {
				return nil, fmt.Errorf("--chunk-strategy: %w", err)
			}
			cs.Type = &st
		}
		if p.chunkMinChars > 0 || p.chunkMaxChars > 0 {
			opts := &extend.ParseConfigChunkingStrategyOptions{}
			if p.chunkMinChars > 0 {
				m := p.chunkMinChars
				opts.MinCharacters = &m
			}
			if p.chunkMaxChars > 0 {
				m := p.chunkMaxChars
				opts.MaxCharacters = &m
			}
			cs.Options = opts
		}
		cfg.ChunkingStrategy = cs
	}
	if p.blockOptionsPath != "" {
		raw, err := readJSONFile(p.blockOptionsPath, "--block-options")
		if err != nil {
			return nil, err
		}
		var bo extend.ParseConfigBlockOptions
		if err := json.Unmarshal(raw, &bo); err != nil {
			return nil, fmt.Errorf("--block-options: %w", err)
		}
		cfg.BlockOptions = &bo
	}
	if p.advancedOptionsPath != "" {
		raw, err := readJSONFile(p.advancedOptionsPath, "--advanced-options")
		if err != nil {
			return nil, err
		}
		var ao extend.ParseConfigAdvancedOptions
		if err := json.Unmarshal(raw, &ao); err != nil {
			return nil, fmt.Errorf("--advanced-options: %w", err)
		}
		cfg.AdvancedOptions = &ao
	}
	if p.searchablePDF != "" {
		if cfg.AdvancedOptions == nil {
			cfg.AdvancedOptions = &extend.ParseConfigAdvancedOptions{}
		}
		if e := cfg.AdvancedOptions.EmbedOcrInPdf; e != nil && !*e {
			return nil, errors.New(`--searchable-pdf conflicts with "embedOcrInPdf": false in --advanced-options`)
		}
		cfg.AdvancedOptions.EmbedOcrInPdf = extend.Bool(true)
	}
	return cfg, nil
}

func validateParseChunkStrategy(strategy string) error {
	switch strategy {
	case "", "page", "document", "section":
		return nil
	default:
		return fmt.Errorf("unknown --chunk-strategy %q (want page|document|section|none)", strategy)
	}
}

func renderParseResult(app *App, run *extend.ParseRun, target string) error {
	if app.JQ != "" {
		if app.Format == string(output.FormatMarkdown) || app.Format == "md" {
			return fmt.Errorf("--jq cannot be combined with -o markdown")
		}
		return renderWithDefault(app, run, output.FormatJSON)
	}
	if app.Format != "" && app.Format != string(output.FormatMarkdown) {
		return renderWithDefault(app, run, output.FormatJSON)
	}
	if target != "markdown" {
		return renderWithDefault(app, run, output.FormatJSON)
	}
	return renderMarkdown(app, run)
}

func renderMarkdown(app *App, run *extend.ParseRun) error {
	md := concatChunks(run)
	if md == "" {
		return nil
	}
	if app.IO.IsStdoutTTY() && app.IO.ColorEnabled() {
		styled, err := glamour.Render(md, "auto")
		if err != nil {
			return fmt.Errorf("render markdown: %w", err)
		}
		_, err = fmt.Fprint(app.IO.Out, styled)
		return err
	}
	_, err := fmt.Fprint(app.IO.Out, md)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(md, "\n") {
		_, err = fmt.Fprintln(app.IO.Out)
	}
	return err
}

func concatChunks(run *extend.ParseRun) string {
	if run.Output == nil {
		return ""
	}
	var b strings.Builder
	for i, c := range run.Output.Chunks {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString(c.Content)
	}
	return b.String()
}
