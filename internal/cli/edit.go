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

// newEditDoc returns the typed documentation for `extend edit`, its
// `extend edit schema` group, and the `extend edit schema generate` leaf.
func newEditDoc(app *App) *CommandDoc {
	var (
		schemaPath            string
		instructions          string
		schemaGenInstructions string
		outputFile            string
		password              string
		wait                  bool
		nativeOnly            bool
		flatten               bool
		timeout               time.Duration
	)

	return &CommandDoc{
		Use:     "edit <input>",
		Summary: "Fill a PDF form using a schema with values",
		Group:   "Actions",
		Triggers: []string{
			"fill a pdf form with values",
			"populate form fields in a pdf",
			"auto-fill a fillable pdf",
			"flatten a filled-out pdf form",
			"run an edit operation against a pdf form",
		},
		WhenToUse: `Use to fill the form fields of a PDF and emit a filled PDF. Two
ways to provide values: pass --instructions for simple natural-language
fills, or pass --schema with a scaffolded schema for structured fills.
For schema scaffolding only, use 'extend edit schema generate'.`,
		Details: `Fill PDF form fields and produce a filled PDF.

There are two ways to provide values:

  1. Instructions-only (simplest; recommended when chaining from
     other commands or for one-off fills):

         extend edit form.pdf --instructions "name is Acme Corp; date is 2026-04-15"

     The server detects the form fields and applies your prose. No
     schema-authoring required.

  2. Schema + values (recommended for repeatable, structured fills):

         extend edit schema generate form.pdf > schema.json
         # populate values on each field per the generated shape, then:
         extend edit form.pdf --schema schema.json

     Use --instructions alongside --schema for formatting rules
     ("dates as MM/DD/YYYY"), conditional logic ("if marital status is
     'single', leave the spouse section blank"), or disambiguation
     between similarly-named fields the schema cannot express.

By default, the command waits for the run to complete and prints a summary.
Pass --output-file to auto-download the filled PDF, or --wait=false to
return the run ID immediately and fetch the filled PDF later via 'extend
files download'.`,
		Examples: []Example{
			{Label: "Inline instructions", Cmd: `extend edit form.pdf --instructions "name is Acme Corp; date is 2026-04-15" --output-file filled.pdf`},
			{Label: "Two-step: scaffold then fill", Cmd: "extend edit schema generate form.pdf > schema.json", Note: "Populate values on each field per the generated schema shape, then run the next example."},
			{Label: "Fill from schema", Cmd: "extend edit form.pdf --schema schema.json --output-file filled.pdf"},
			{Label: "Schema + fill-time instructions", Cmd: `extend edit form.pdf --schema schema.json --instructions "format dates as MM/DD/YYYY; check 'individual' in section 2"`},
			{Label: "Async (return run ID)", Cmd: "extend edit form.pdf --schema schema.json --wait=false"},
		},
		Gotchas: []string{
			"--schema and --instructions can be combined; for simple fills, --instructions alone is enough.",
			"Populate values per the shape emitted by 'extend edit schema generate' — do not invent field names; inspect the generated schema first.",
			"--output-file '-' streams the filled PDF to stdout; combine with redirection.",
			"Edit runs cannot have a CANCELLED status; only FAILED or PROCESSED.",
		},
		SeeAlso:  []string{"edit schema generate", "runs watch", "runs get", "files download"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: client.ProfileShort, DefaultsToWait: true},
		Failures: []client.RunStatus{client.StatusFailed},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(cmd.Context(), app, editParams{
				input:                 args[0],
				schemaPath:            schemaPath,
				instructions:          instructions,
				schemaGenInstructions: schemaGenInstructions,
				outputFile:            outputFile,
				password:              password,
				wait:                  wait,
				nativeOnly:            nativeOnly,
				flatten:               flatten,
				timeout:               timeout,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&schemaPath, "schema", "", "Inline JSON, path, file:// URI, or '-' for a schema with values populated per the shape emitted by 'extend edit schema generate'. Omit to let the server auto-detect form fields.")
			cmd.Flags().StringVar(&instructions, "instructions", "", "Free-form prose values and rules (e.g. \"name is Acme Corp; format dates as MM/DD/YYYY\"). Use alone for simple fills, or alongside --schema for fills that need conditional or formatting guidance the schema cannot express.")
			cmd.Flags().StringVar(&schemaGenInstructions, "schema-instructions", "", "Free-form prose applied only to the schema-generation step when --schema is omitted (which fields to include, how to interpret ambiguous layouts).")
			cmd.Flags().StringVarP(&outputFile, "output-file", "O", "", "Path to write the filled PDF to (auto-downloads); '-' for stdout. Default: leave the PDF on the server; fetch later with 'extend files download <file-id>'.")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run ID immediately)")
			cmd.Flags().BoolVar(&nativeOnly, "native-fields-only", true, "Only fill native PDF form fields (set false to detect via vision)")
			cmd.Flags().BoolVar(&flatten, "flatten", true, "Flatten the PDF after filling")
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
		},
		Subcommands: []*CommandDoc{newEditSchemaDoc(app)},
	}
}

type editParams struct {
	input                 string
	schemaPath            string
	instructions          string
	schemaGenInstructions string
	outputFile            string
	password              string
	wait                  bool
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
		raw, err := readJSONFile(p.schemaPath, "--schema")
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

	if !p.wait {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := cli.WaitForEditRun(ctx, run.ID, client.WaitProfileOptions(client.ProfileShort, p.timeout), func(r *client.EditRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return formatActionWaitError(err, run.ID)
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

// generatedEditSchema unwraps the documented response shape from
// POST /edit_schemas/generate:
//
//	{"schema": {...}, "annotatedSchema": ..., "mappingResult": ...}
//
// Only the inner `schema` field is exposed to users; the rest is debug data.
func generatedEditSchema(raw json.RawMessage) (json.RawMessage, error) {
	var env struct {
		Schema json.RawMessage `json:"schema"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode generated edit schema: %w", err)
	}
	if len(env.Schema) == 0 {
		return nil, fmt.Errorf("generated edit schema response missing 'schema' field")
	}
	if !json.Valid(env.Schema) {
		return nil, fmt.Errorf("generated edit schema response contains invalid schema")
	}
	return env.Schema, nil
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
	// Surface the no-output-file case loudly in every render path
	// (pretty, JSON, jq), because a PROCESSED run with no edited PDF
	// is the failure mode the May 2026 agent-experience transcripts
	// flagged: a run completes "successfully" but never produces a
	// filled document, and the agent reports victory anyway. Warn to
	// stderr so machine-readable stdout is unaffected.
	maybeWarnEmptyEditOutput(app, run)

	if app.Format != "" || app.JQ != "" {
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

// maybeWarnEmptyEditOutput prints a stderr warning when the server
// reports PROCESSED but did not attach an edited file to the run
// output. This usually indicates a schema-shape mismatch — the server
// accepted the request but found nothing to fill — and historically
// agents misread the lack of an error as success. The warning is
// strictly informational: exit code is unchanged, since the API
// itself reported a terminal-success state.
func maybeWarnEmptyEditOutput(app *App, run *client.EditRun) {
	if run == nil || run.Status != client.StatusProcessed {
		return
	}
	if outputFileID(run) != "" {
		return
	}
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.ErrOut, "%s edit run %s reported PROCESSED but produced no filled PDF (output.editedFile is missing).\n",
		pal.Yellow("warning:"), run.ID)
	fmt.Fprintln(app.IO.ErrOut, pal.Dimf("  This usually means the server detected no fields to fill — double-check your --schema or --instructions. Inspect the full run with: extend runs get %s -o json", run.ID))
}

// newEditSchemaDoc returns the typed documentation for the
// `extend edit schema` group (a pure umbrella; only generate is meaningful).
func newEditSchemaDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:       "schema",
		Summary:   "Generate or operate on edit schemas",
		WhenToUse: `Use this group's 'generate' subcommand to scaffold a schema from a fillable PDF. There is currently only one operation in the group.`,
		Details:   `Schema operations are synchronous; there is no async variant.`,
		Subcommands: []*CommandDoc{
			newEditSchemaGenerateDoc(app),
		},
	}
}

// newEditSchemaGenerateDoc returns the typed documentation for
// `extend edit schema generate`.
func newEditSchemaGenerateDoc(app *App) *CommandDoc {
	var (
		nativeOnly      bool
		instructions    string
		inputSchemaPath string
		password        string
	)
	return &CommandDoc{
		Use:     "generate <input>",
		Summary: "Detect form fields and scaffold an edit schema (sync)",
		Triggers: []string{
			"detect form fields in a pdf",
			"scaffold a schema for an extend edit run",
			"generate the json schema for a fillable pdf",
			"derive an edit schema from a form",
		},
		WhenToUse: `Use to scaffold a schema you can hand-edit (populate 'default' values)
and pass to 'extend edit --schema'. This is the one synchronous endpoint
in the edit family; there is no async variant.`,
		Details: `Detect form fields in a PDF and emit a starting-point schema that can be
passed directly to 'extend edit --schema'.

Use --instructions to guide the schema generator about which fields to
include or how to interpret ambiguous form layouts. Use --input-schema to
seed the generator with an existing schema, in which case detected fields
are overlaid onto your starting point.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend edit schema generate form.pdf > schema.json"},
			{Label: "With instructions", Cmd: `extend edit schema generate form.pdf --instructions "skip the signature block"`},
			{Label: "Seed from existing", Cmd: "extend edit schema generate form.pdf --input-schema base.json > merged.json"},
		},
		Gotchas: []string{
			"This is the only synchronous endpoint in the edit family; do not pass --wait flags.",
			"--input-schema entries are merged with detection; detected fields can override seeded ones.",
		},
		SeeAlso: []string{"edit"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
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
				raw, err := readJSONFile(inputSchemaPath, "--input-schema")
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
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&nativeOnly, "native-fields-only", true, "Only detect native PDF form fields (set false to detect via vision)")
			cmd.Flags().StringVar(&instructions, "instructions", "", "Free-form instructions to guide schema generation")
			cmd.Flags().StringVar(&inputSchemaPath, "input-schema", "", "Starting-point JSON Schema (overlaid by detection). Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
		},
	}
}
