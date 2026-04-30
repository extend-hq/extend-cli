package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

func newEditCommand(app *App) *cobra.Command {
	var (
		schemaPath            string
		instructions          string
		schemaGenInstructions string
		outputFile            string
		password              string
		async                 bool
		nativeOnly            bool
		flatten               bool
		timeout               time.Duration
	)

	cmd := &cobra.Command{
		Use:   "edit <input>",
		Short: "Fill a PDF form using a schema with values",
		Long: `Fill PDF form fields using a schema (with values) and produce a filled PDF.

Use 'extend edit schema generate <input>' first to detect form fields and
scaffold a schema; populate the values inline (as 'default' on each field);
then run 'extend edit <input> --schema schema.json'.

By default, the command waits for the run to complete and prints a summary.
Pass --output-file to auto-download the filled PDF.`,
		Example: `  extend edit schema generate form.pdf > schema.json
  # populate default values inline in schema.json, then:
  extend edit form.pdf --schema schema.json --output-file filled.pdf`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(cmd.Context(), app, editParams{
				input:                 args[0],
				schemaPath:            schemaPath,
				instructions:          instructions,
				schemaGenInstructions: schemaGenInstructions,
				outputFile:            outputFile,
				password:              password,
				async:                 async,
				nativeOnly:            nativeOnly,
				flatten:               flatten,
				timeout:               timeout,
			})
		},
	}

	cmd.Flags().StringVar(&schemaPath, "schema", "", "Path to schema JSON (with values inline as 'default'); omit to auto-generate")
	cmd.Flags().StringVar(&instructions, "instructions", "", "Free-form instructions to guide field filling")
	cmd.Flags().StringVar(&schemaGenInstructions, "schema-instructions", "", "Instructions used only when auto-generating the schema (no --schema)")
	cmd.Flags().StringVarP(&outputFile, "output-file", "O", "", "Path to write the filled PDF to (auto-downloads); '-' for stdout")
	cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
	cmd.Flags().BoolVar(&async, "async", false, "Return run ID immediately without waiting")
	cmd.Flags().BoolVar(&nativeOnly, "native-fields-only", true, "Only fill native PDF form fields (set false to detect via vision)")
	cmd.Flags().BoolVar(&flatten, "flatten", true, "Flatten the PDF after filling")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum time to wait for completion")

	SetIOAnnotations(cmd, OutputPretty, OutputJSON)
	SetWaitAnnotations(cmd, client.ProfileShort, true)
	SetLifecycleFailureCodes(cmd, client.StatusFailed)

	cmd.AddCommand(newEditSchemaCommand(app))
	return cmd
}

type editParams struct {
	input                 string
	schemaPath            string
	instructions          string
	schemaGenInstructions string
	outputFile            string
	password              string
	async                 bool
	nativeOnly            bool
	flatten               bool
	timeout               time.Duration
}

func runEdit(ctx context.Context, app *App, p editParams) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	ref, err := uploadOrResolveWith(ctx, app, cli, p.input, p.password)
	if err != nil {
		return err
	}

	cfg := &client.EditRunConfig{
		Instructions:                 p.instructions,
		SchemaGenerationInstructions: p.schemaGenInstructions,
		AdvancedOptions: &client.EditAdvancedOptions{
			NativeFieldsOnly: &p.nativeOnly,
			FlattenPdf:       &p.flatten,
		},
	}
	if p.schemaPath != "" {
		raw, err := readEditSchema(p.schemaPath, "--schema")
		if err != nil {
			return err
		}
		cfg.Schema = raw
	}
	in := client.CreateEditRunInput{
		File:   ref,
		Config: cfg,
	}

	run, err := cli.CreateEditRun(ctx, in)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if p.async {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := cli.WaitForEditRun(ctx, run.ID, client.WaitProfileOptions(client.ProfileShort, p.timeout), func(r *client.EditRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return fmt.Errorf("wait: %w", err)
	}

	if final.Status == client.StatusFailed {
		_ = renderEditResult(app, final)
		if final.FailureMessage != "" {
			return fmt.Errorf("run %s failed: %s", final.ID, final.FailureMessage)
		}
		return fmt.Errorf("run %s failed", final.ID)
	}

	if p.outputFile != "" {
		fileID := outputFileID(final)
		if fileID == "" {
			return fmt.Errorf("run %s succeeded but output has no file id", final.ID)
		}
		if err := downloadEditOutput(ctx, app, cli, fileID, p.outputFile); err != nil {
			return err
		}
		if p.outputFile == "-" {
			return nil
		}
	}

	return renderEditResult(app, final)
}

func outputFileID(run *client.EditRun) string {
	if run.Output == nil || run.Output.EditedFile == nil {
		return ""
	}
	return run.Output.EditedFile.ID
}

// readEditSchema accepts either a raw EditRootJSON schema or the response
// envelope returned by POST /edit_schemas/generate:
// {"schema": {...}, "annotatedSchema": ..., "mappingResult": ...}.
// Older CLI versions wrote that envelope to disk, so unwrap it here to make
// those generated schema.json files usable with `extend edit --schema`.
func readEditSchema(source, flag string) (json.RawMessage, error) {
	raw, err := readJSONFile(source, flag)
	if err != nil {
		return nil, err
	}
	return unwrapEditSchemaEnvelope(raw)
}

func unwrapEditSchemaEnvelope(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if _, hasType := obj["type"]; hasType {
		return raw, nil
	}
	inner, ok := obj["schema"]
	if !ok {
		return raw, nil
	}
	if !json.Valid(inner) {
		return nil, fmt.Errorf("generated edit schema envelope contains invalid schema")
	}
	return inner, nil
}

func generatedEditSchema(raw json.RawMessage) (json.RawMessage, error) {
	inner, err := unwrapEditSchemaEnvelope(raw)
	if err != nil {
		return nil, err
	}
	return inner, nil
}

func downloadEditOutput(ctx context.Context, app *App, cli *client.Client, fileID, outPath string) error {
	if outPath == "-" {
		_, err := cli.DownloadFile(ctx, fileID, app.IO.Out)
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".extend-edit-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	n, err := cli.DownloadFile(ctx, fileID, tmp)
	tmp.Close()
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, outPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	fmt.Fprintf(app.IO.ErrOut, "Wrote %d bytes to %s\n", n, outPath)
	return nil
}

func renderEditResult(app *App, run *client.EditRun) error {
	if app.Format != "" || app.JQ != "" || !app.IO.IsStdoutTTY() {
		return renderWithDefault(app, run, output.FormatJSON)
	}
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.Out, "%s %s (%s)\n", statusIcon(pal, run.Status), run.ID, run.Status)
	if run.Status == client.StatusFailed && run.FailureMessage != "" {
		fmt.Fprintf(app.IO.Out, "  %s\n", run.FailureMessage)
		return nil
	}
	if fid := outputFileID(run); fid != "" {
		fmt.Fprintf(app.IO.Out, "  Filled PDF: %s\n", fid)
		fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Download:   extend files download %s -O filled.pdf", fid))
	}
	return nil
}

func newEditSchemaCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Generate or operate on edit schemas",
	}
	cmd.AddCommand(newEditSchemaGenerateCommand(app))
	return cmd
}

func newEditSchemaGenerateCommand(app *App) *cobra.Command {
	var (
		nativeOnly      bool
		instructions    string
		inputSchemaPath string
		password        string
	)
	cmd := &cobra.Command{
		Use:   "generate <input>",
		Short: "Detect form fields and scaffold an edit schema (sync)",
		Long: `Detect form fields in a PDF and emit a starting-point schema that can be
passed directly to 'extend edit --schema'. This is the one synchronous endpoint
in the edit family; there is no async variant.

Use --instructions to guide the schema generator about which fields to
include or how to interpret ambiguous form layouts. Use --input-schema to
seed the generator with an existing schema, in which case detected fields
are overlaid onto your starting point.`,
		Example: `  extend edit schema generate form.pdf > schema.json
  extend edit schema generate form.pdf --instructions "skip the signature block"
  extend edit schema generate form.pdf --input-schema base.json > merged.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			ref, err := uploadOrResolveWith(cmd.Context(), app, cli, args[0], password)
			if err != nil {
				return err
			}
			cfg := &client.EditSchemaGenerationConfig{
				Instructions: instructions,
				AdvancedOptions: &client.EditAdvancedOptions{
					NativeFieldsOnly: &nativeOnly,
				},
			}
			if inputSchemaPath != "" {
				raw, err := readEditSchema(inputSchemaPath, "--input-schema")
				if err != nil {
					return err
				}
				cfg.InputSchema = raw
			}
			resp, err := cli.GenerateEditSchema(cmd.Context(), client.GenerateEditSchemaInput{
				File:   ref,
				Config: cfg,
			})
			if err != nil {
				return err
			}
			schema, err := generatedEditSchema(resp)
			if err != nil {
				return err
			}
			var pretty any
			if err := json.Unmarshal(schema, &pretty); err != nil {
				_, werr := app.IO.Out.Write(schema)
				return werr
			}
			return renderWithDefault(app, pretty, output.FormatJSON)
		},
	}
	cmd.Flags().BoolVar(&nativeOnly, "native-fields-only", true, "Only detect native PDF form fields (set false to detect via vision)")
	cmd.Flags().StringVar(&instructions, "instructions", "", "Free-form instructions to guide schema generation")
	cmd.Flags().StringVar(&inputSchemaPath, "input-schema", "", "Path to a starting-point JSON Schema (overlaid by detection)")
	cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}
