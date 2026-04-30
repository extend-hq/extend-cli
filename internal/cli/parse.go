package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

func newParseCommand(app *App) *cobra.Command {
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
		async               bool
		timeout             time.Duration
		meta                metaFlags
	)

	cmd := &cobra.Command{
		Use:   "parse <input>",
		Short: "Parse a document into structured text",
		Long: `Convert a document into LLM-ready text or spatial layout.

<input> can be:
  - a local file path (auto-uploaded)
  - a file_xxx ID (use a previously uploaded file)
  - an https:// URL (Extend fetches the document)

By default, output is rendered as Markdown:
  - When stdout is a terminal, it is rendered with styling.
  - When piped, raw Markdown is emitted (composes with downstream tools).
Pass -o json/yaml for the full run object, or -o markdown to force raw.

Chunking is controlled by --chunk-strategy + --chunk-min-chars/--chunk-max-chars.
For finer-grained block detection (figures, tables, barcodes, etc.), pass
--block-options as inline JSON, a plain file path, or an absolute file:// URI.
--advanced-options accepts the remaining tuning knobs verbatim (return-OCR,
page ranges, parallelism, etc.) in the same forms.`,
		Example: `  extend parse contract.pdf
  extend parse contract.pdf -o markdown > contract.md
  extend parse contract.pdf -o json | jq '.output.chunks | length'
  extend parse contract.pdf --engine parse_performance --engine-version 1.0.1
  extend parse contract.pdf --chunk-strategy section --chunk-max-chars 4000
  extend parse contract.pdf --advanced-options '{"pageRanges":"1-3"}'
  extend parse contract.pdf --block-options block-opts.json
  extend parse contract.pdf --advanced-options file:///absolute/path/advanced.json`,
		Args: cobra.ExactArgs(1),
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
				async:               async,
				timeout:             timeout,
				metadata:            md,
			})
		},
	}

	cmd.Flags().StringVar(&target, "target", "markdown", "Parse target: markdown or spatial")
	cmd.Flags().StringVar(&engine, "engine", "", "Engine: parse_performance or parse_light (default: server default)")
	cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Engine version (e.g. latest, 1.0.1, 2.0.0-beta)")
	cmd.Flags().StringVar(&chunkStrategy, "chunk-strategy", "", "Chunking strategy: page|document|section (none omits chunkingStrategy)")
	cmd.Flags().IntVar(&chunkMinChars, "chunk-min-chars", 0, "Minimum characters per chunk (server default if 0)")
	cmd.Flags().IntVar(&chunkMaxChars, "chunk-max-chars", 0, "Maximum characters per chunk (server default if 0)")
	cmd.Flags().StringVar(&blockOptionsPath, "block-options", "", "JSON object, path, or file:// URI for blockOptions (figures/tables/text/barcodes/keyValue/formulas)")
	cmd.Flags().StringVar(&advancedOptionsPath, "advanced-options", "", "JSON object, path, or file:// URI for advancedOptions (returnOcr, pageRanges, etc.)")
	cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
	cmd.Flags().BoolVar(&async, "async", false, "Return run ID immediately without waiting")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum time to wait for completion")
	meta.attach(cmd)

	cmd.AddCommand(newParseBatchCommand(app))
	return cmd
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
	async               bool
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

	in := client.CreateParseRunInput{
		File:     ref,
		Config:   cfg,
		Metadata: p.metadata,
	}
	run, err := cli.CreateParseRun(ctx, in)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if p.async {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := cli.WaitForParseRun(ctx, run.ID, client.WaitProfileOptions(client.ProfileShort, p.timeout), func(r *client.ParseRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return fmt.Errorf("wait: %w", err)
	}

	if err := renderParseResult(app, final, p.target); err != nil {
		return err
	}
	if final.Status == client.StatusFailed {
		if final.FailureMessage != "" {
			return fmt.Errorf("run %s failed: %s", final.ID, final.FailureMessage)
		}
		return fmt.Errorf("run %s failed", final.ID)
	}
	return nil
}

// buildParseConfig assembles a ParseConfig from CLI parseParams. Returns
// nil when no config-relevant flags are set so the on-the-wire body omits
// `config` entirely (server falls back to defaults).
func buildParseConfig(p parseParams) (*client.ParseConfig, error) {
	cfg := &client.ParseConfig{}
	if p.target != "" {
		cfg.Target = p.target
	}
	if p.engine != "" {
		cfg.Engine = p.engine
	}
	if p.engineVersion != "" {
		cfg.EngineVersion = p.engineVersion
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
		cs := &client.ChunkingStrategy{Type: chunkStrategy}
		if p.chunkMinChars > 0 || p.chunkMaxChars > 0 {
			opts := &client.ChunkingStrategyOptions{}
			if p.chunkMinChars > 0 {
				min := p.chunkMinChars
				opts.MinCharacters = &min
			}
			if p.chunkMaxChars > 0 {
				max := p.chunkMaxChars
				opts.MaxCharacters = &max
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
		cfg.BlockOptions = raw
	}
	if p.advancedOptionsPath != "" {
		raw, err := readJSONFile(p.advancedOptionsPath, "--advanced-options")
		if err != nil {
			return nil, err
		}
		cfg.AdvancedOptions = raw
	}
	// If only Target is set with the default value and no other knobs were
	// touched, the config is effectively a no-op; still return it because
	// the existing CLI behavior was to send {target:"markdown"} explicitly.
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

func renderParseResult(app *App, run *client.ParseRun, target string) error {
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

func renderMarkdown(app *App, run *client.ParseRun) error {
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

func concatChunks(run *client.ParseRun) string {
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
