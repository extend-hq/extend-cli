package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"
	"github.com/extend-hq/extend-go-sdk/option"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// maxGenerateSamples and maxGenerateInstructions are the API's limits on
// generating an extractor's schema from samples.
const (
	maxGenerateSamples      = 5
	maxGenerateInstructions = 2500
)

func newExtractorsCreateDoc(app *App, bodyDetails string) *CommandDoc {
	var (
		fromFile     string
		name         string
		samples      []string
		instructions string
		timeout      time.Duration
	)
	return &CommandDoc{
		Use:     "create",
		Summary: "Create an extractor, or generate one from sample documents",
		Triggers: []string{
			"create a new extractor",
			"register an extractor in the workspace",
			"set up a fresh extractor draft",
			"generate an extraction schema from sample documents",
			"build an extractor from example pdfs",
		},
		WhenToUse: `Use to create an extractor in the current workspace. To get one without
writing a schema, pass 1 to 5 sample documents with --sample and Extend
generates the schema from them; to set the config yourself, pass the API
body with --from-file. The new extractor starts as a draft (no published
version); use 'extend extractors versions create' once you're ready to
deploy.`,
		Details: `With --sample (repeat it for up to 5 samples: local files, file_xxx IDs or
https:// URLs), Extend writes a JSON schema for the samples into the new
extractor's draft. It reads only the first pages of each sample: 5 for one
or two samples, 3 for three, 2 for four or five. --instructions (up to
2,500 characters) says what the documents are and which fields to extract;
it's optional but makes the schema better. Generation runs inside the
request and usually takes 30s to 2m; --timeout caps the wait. If it fails
or is stopped, Extend deletes the new extractor. The command prints the
extractor with its generated schema.

Try the draft, then publish it:

    extend extract sample.pdf --using ex_xxx --version draft
    extend extractors versions create ex_xxx --release-type major

` + bodyDetails,
		Examples: []Example{
			{Label: "From sample documents", Cmd: `extend extractors create --name "Invoices" --sample invoice-1.pdf --sample invoice-2.pdf --instructions "Vendor invoices: vendor, invoice number and date, line items, total"`},
			{Label: "From file with name", Cmd: `extend extractors create --from-file extractor.json --name "My extractor"`},
			{Label: "From stdin", Cmd: "cat extractor.json | extend extractors create --from-file -"},
		},
		Gotchas: []string{
			"--sample can't be combined with a config or cloneExtractorId in --from-file; the API rejects both together.",
			"Generating a schema is billed and the CLI doesn't retry it; if it fails, rerun the command.",
			"The new extractor starts as a draft; try it with 'extend extract --version draft' before 'versions create' publishes it.",
		},
		SeeAlso: []string{"extractors list", "extractors update", "extractors versions create", "extract"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("extractors create takes no arguments; to generate the schema from %s, pass --sample %s", args[0], args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, map[string]string{"name": name})
			if err != nil {
				return err
			}
			var req extend.ExtractorsCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return fmt.Errorf("decode body: %w", err)
			}
			if len(samples) == 0 && req.Generate == nil {
				if instructions != "" {
					return errors.New("--instructions needs --sample: it describes the documents the schema is generated from")
				}
				cli, err := app.NewClient()
				if err != nil {
					return err
				}
				ex, err := cli.Extractors.Create(cmd.Context(), &req)
				if err != nil {
					return err
				}
				return renderWithDefault(app, ex, output.FormatJSON)
			}
			if err := prepareGenerate(&req, len(samples), instructions); err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			if len(samples) > 0 {
				refs, err := uploadAllOrResolve(cmd.Context(), app, cli, samples, "")
				if err != nil {
					return err
				}
				if req.Generate.Files, err = extendx.BuildGenerateFiles(refs); err != nil {
					return err
				}
			}
			ex, err := generateExtractor(cmd.Context(), app, cli, &req, timeout)
			if err != nil {
				return err
			}
			return renderWithDefault(app, ex, output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON body, path, file:// URI, or '-' for stdin")
			cmd.Flags().StringVar(&name, "name", "", "Name (overrides body)")
			cmd.Flags().StringArrayVar(&samples, "sample", nil, "Sample document to generate the schema from: path, file_xxx ID or https:// URL (repeatable, up to 5)")
			cmd.Flags().StringVar(&instructions, "instructions", "", "What the samples are and which fields to extract, to guide schema generation (up to 2,500 characters)")
			cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Maximum time to wait while the schema is generated")
		},
	}
}

// prepareGenerate checks a request that generates its schema, before any
// sample is uploaded, and applies --instructions to it. samples is the
// number of --sample flags.
func prepareGenerate(req *extend.ExtractorsCreateRequest, samples int, instructions string) error {
	if req.Config != nil || req.CloneExtractorID != nil {
		return errors.New("generating the schema can't be combined with a config or cloneExtractorId in --from-file; remove them from the body")
	}
	if req.Name == "" {
		return errors.New("--name is required to create an extractor")
	}
	if req.Generate == nil {
		req.Generate = &extend.ExtractorsCreateRequestGenerate{}
	}
	if samples > 0 && len(req.Generate.Files) > 0 {
		return errors.New("give the samples with --sample or as generate.files in --from-file, not both")
	}
	if n := samples + len(req.Generate.Files); n < 1 || n > maxGenerateSamples {
		return fmt.Errorf("schema generation takes 1 to %d samples, got %d", maxGenerateSamples, n)
	}
	if instructions != "" {
		req.Generate.Instructions = &instructions
	}
	if req.Generate.Instructions != nil {
		if n := utf8.RuneCountInString(*req.Generate.Instructions); n > maxGenerateInstructions {
			return fmt.Errorf("the instructions are %d characters; the limit is %d", n, maxGenerateInstructions)
		}
	}
	return nil
}

// generateExtractor creates an extractor whose schema the API generates
// from samples while the request is open. That takes minutes, so the call
// skips the per-request HTTP timeout; it's billed, so it's made once
// (StartOnce retries only a 429, which the server rejected unprocessed).
// When the request is cut off, the API stops and deletes the extractor.
func generateExtractor(ctx context.Context, app *App, cli *sdkclient.Client, req *extend.ExtractorsCreateRequest, timeout time.Duration) (*extend.Extractor, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	n := len(req.Generate.Files)
	sp := app.IO.StartSpinner(fmt.Sprintf("Generating a schema from %d sample%s (usually 30s to 2m)", n, pluralize(n)))
	ex, err := extendx.StartOnce(ctx, func(opts ...option.RequestOption) (*extend.Extractor, error) {
		return cli.Extractors.Create(extendx.WithoutHTTPTimeout(ctx), req, opts...)
	})
	sp.Stop("")
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("schema generation didn't finish within %s, so it was stopped and Extend deletes the new extractor; rerun with a longer --timeout", timeout)
	}
	return ex, err
}
