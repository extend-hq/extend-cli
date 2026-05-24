package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

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
knobs verbatim (return-OCR, page ranges, parallelism, etc.) in the same forms.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend parse contract.pdf"},
			{Label: "Save raw markdown", Cmd: "extend parse contract.pdf -o markdown > contract.md"},
			{Label: "Count chunks via jq", Cmd: "extend parse contract.pdf -o json | jq '.output.chunks | length'"},
			{Label: "Specific engine", Cmd: "extend parse contract.pdf --engine parse_performance --engine-version 1.0.1"},
			{Label: "Section chunking", Cmd: "extend parse contract.pdf --chunk-strategy section --chunk-max-chars 4000"},
			{Label: "Inline advanced options", Cmd: `extend parse contract.pdf --advanced-options '{"pageRanges":"1-3"}'`},
			{Label: "Block options from file", Cmd: "extend parse contract.pdf --block-options block-opts.json"},
			{Label: "Absolute file URI", Cmd: "extend parse contract.pdf --advanced-options file:///absolute/path/advanced.json"},
		},
		Gotchas: []string{
			"--target spatial does not support --chunk-strategy section.",
			"--jq cannot be combined with -o markdown; use -o json --jq and select the markdown chunk paths.",
			"Parse runs cannot be cancelled once submitted (unlike extract/classify/split).",
		},
		SeeAlso:  []string{"extract", "classify", "parse batch", "runs watch", "runs get"},
		Output:   OutputSpec{TTY: OutputMarkdown, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed},
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
				wait:                wait,
				timeout:             timeout,
				metadata:            md,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&target, "target", "markdown", "Parse target: markdown or spatial")
			cmd.Flags().StringVar(&engine, "engine", "", "Engine: parse_performance or parse_light (default: server default)")
			cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Engine version (e.g. latest, 1.0.1, 2.0.0-beta)")
			cmd.Flags().StringVar(&chunkStrategy, "chunk-strategy", "", "Chunking strategy: page|document|section (none omits chunkingStrategy)")
			cmd.Flags().IntVar(&chunkMinChars, "chunk-min-chars", 0, "Minimum characters per chunk (server default if 0)")
			cmd.Flags().IntVar(&chunkMaxChars, "chunk-max-chars", 0, "Maximum characters per chunk (server default if 0)")
			cmd.Flags().StringVar(&blockOptionsPath, "block-options", "", "blockOptions for fine-grained block detection (figures/tables/text/barcodes/keyValue/formulas). Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&advancedOptionsPath, "advanced-options", "", "advancedOptions for parse tuning (returnOcr, pageRanges, etc.). Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run ID immediately)")
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
			meta.attach(cmd)
		},
		Subcommands: []*CommandDoc{newParseBatchDoc(app)},
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
	wait                bool
	timeout             time.Duration
	metadata            map[string]any
}

func runParse(ctx context.Context, app *App, p parseParams) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	ref, err := uploadOrResolveWith(ctx, app, cli, p.input, p.password)
	if err != nil {
		return err
	}

	cfg, err := buildParseConfig(p)
	if err != nil {
		return err
	}

	file, err := extendx.BuildParseFile(ref)
	if err != nil {
		return err
	}

	req := &extend.ParseRunsCreateRequest{
		File:   file,
		Config: cfg,
	}
	if p.metadata != nil {
		md := extend.RunMetadata(p.metadata)
		req.Metadata = &md
	}

	run, err := cli.ParseRuns.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
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
		return formatActionWaitError(err, run.ID)
	}

	if err := renderParseResult(app, final, p.target); err != nil {
		return err
	}
	if extendx.RunStatus(final.Status) == extendx.StatusFailed {
		if final.FailureMessage != nil && *final.FailureMessage != "" {
			return fmt.Errorf("run %s failed: %s", final.ID, *final.FailureMessage)
		}
		return fmt.Errorf("run %s failed", final.ID)
	}
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
		e, err := extend.NewParseConfigEngineFromString(p.engine)
		if err != nil {
			return nil, fmt.Errorf("--engine: %w", err)
		}
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
