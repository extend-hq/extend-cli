package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newEditDoc returns the typed documentation for `extend edit` and its
// templates/runs subgroups. Schema scaffolding lives at the top-level
// `extend detect-form` verb (detectform.go).
func newEditDoc(app *App) *CommandDoc {
	var (
		schemaPath            string
		instructions          string
		schemaGenInstructions string
		advancedOptionsPath   string
		engineVersion         string
		outputFile            string
		password              string
		wait                  bool
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
For schema scaffolding only, use 'extend detect-form'.`,
		Details: `Fill PDF form fields and produce a filled PDF.

There are two ways to provide values:

  1. Instructions-only (simplest; recommended when chaining from
     other commands or for one-off fills):

         extend edit form.pdf --instructions "name is Acme Corp; date is 2026-04-15"

     The server detects the form fields and applies your prose. No
     schema-authoring required.

  2. Schema + values (recommended for repeatable, structured fills):

         extend detect-form form.pdf --jq '.output.schema' -o json > schema.json
         # populate values on each field per the generated shape, then:
         extend edit form.pdf --schema schema.json

     Use --instructions alongside --schema for formatting rules
     ("dates as MM/DD/YYYY"), conditional logic ("if marital status is
     'single', leave the spouse section blank"), or disambiguation
     between similarly-named fields the schema cannot express.

     Each field in a --schema carries the extend_edit:* keys emitted by
     'extend detect-form' (set extend_edit:value to force a value); run
     that command's --help for the full key reference.

By default, the command waits for the run to complete and prints a summary.
Pass --output-file to auto-download the filled PDF, or --wait=false to
return the run ID immediately and fetch the filled PDF later via 'extend
files download'.

Config fields:

  - instructions (string) - natural-language values and rules. Use alone for
    fast one-off fills, or combine with --schema to steer ambiguous fields.
  - schemaGenerationInstructions (string) - extra instructions used when the
    server generates a schema because --schema was omitted.
  - schema (object) - explicit edit schema. Use this for precise, repeatable
    fills of the same form type.
  - advancedOptions (object) - detection and output options. Pass through
    --advanced-options as inline JSON, a path, a file:// URI, or '-' for stdin.
    Omitted fields use the server default.
  - engineVersion (string) - the Edit engine version to run (an exact version
    like 1.0.0-beta for reproducible results, or latest for the latest stable
    version). Pass through --engine-version; omitted runs use the server
    default. The run object reports the resolved exact version under config.

  flattenPdf           bool  Make the filled form non-editable (server default: true).
  nativeFieldsOnly     bool  Only use embedded AcroForm fields; set false to also detect fields via vision.
  tableParsingEnabled  bool  Parse table regions as arrays of objects so their cells can be filled.
  radioEnumsEnabled    bool  Model a radio-button group as a single-choice enum so only one option fills.

Prefer schema-driven fills for production/repeated forms. Natural-language
instructions are fastest for prototypes; schemas with extend_edit:value are
more deterministic. Flatten final documents; set flattenPdf false only when
the output must remain editable.

` + editSchemaPropertyDoc + `

` + editOutputDoc,
		Examples: []Example{
			{Label: "Inline instructions", Cmd: `extend edit form.pdf --instructions "name is Acme Corp; date is 2026-04-15" --output-file filled.pdf`},
			{Label: "Two-step: scaffold then fill", Cmd: "extend detect-form form.pdf --jq '.output.schema' -o json > schema.json", Note: "Populate values on each field per the generated schema shape, then run the next example."},
			{Label: "Fill from schema", Cmd: "extend edit form.pdf --schema schema.json --output-file filled.pdf"},
			{Label: "Schema + fill-time instructions", Cmd: `extend edit form.pdf --schema schema.json --instructions "format dates as MM/DD/YYYY; check 'individual' in section 2"`},
			{Label: "Tune detection", Cmd: `extend edit form.pdf --advanced-options '{"tableParsingEnabled":true,"radioEnumsEnabled":true}'`},
			{Label: "Pin the engine version", Cmd: "extend edit form.pdf --schema schema.json --engine-version 1.0.0-beta"},
			{Label: "Async (return run ID)", Cmd: "extend edit form.pdf --schema schema.json --wait=false"},
		},
		Gotchas: []string{
			"--schema and --instructions can be combined; for simple fills, --instructions alone is enough.",
			"Populate values per the shape emitted by 'extend detect-form' — do not invent field names; inspect the generated schema first.",
			"--output-file '-' streams the filled PDF to stdout; combine with redirection.",
			"Detection toggles (flattenPdf/nativeFieldsOnly/tableParsingEnabled/radioEnumsEnabled) go in --advanced-options JSON; omitted fields use the server default.",
			"Edit runs cannot have a CANCELLED status; only FAILED or PROCESSED.",
		},
		SeeAlso:  []string{"detect-form", "edit runs watch", "edit runs get", "files download"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed},
		// Schema scaffolding used to live under this verb ('edit schema
		// generate'); stale scripts and skills still invoke it, so name
		// the replacement instead of failing with an arg-count error.
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && (args[0] == "schema" || args[0] == "detections") {
				return fmt.Errorf("unknown command %q for \"extend edit\": schema scaffolding moved; use 'extend detect-form'", args[0])
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(cmd.Context(), app, editParams{
				input:                 args[0],
				schemaPath:            schemaPath,
				instructions:          instructions,
				schemaGenInstructions: schemaGenInstructions,
				advancedOptionsPath:   advancedOptionsPath,
				engineVersion:         engineVersion,
				outputFile:            outputFile,
				password:              password,
				wait:                  wait,
				timeout:               timeout,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&schemaPath, "schema", "", "Inline JSON, path, file:// URI, or '-' for a schema with values populated per the shape emitted by 'extend detect-form'. Omit to let the server auto-detect form fields.")
			cmd.Flags().StringVar(&instructions, "instructions", "", "Free-form prose values and rules (e.g. \"name is Acme Corp; format dates as MM/DD/YYYY\"). Use alone for simple fills, or alongside --schema for fills that need conditional or formatting guidance the schema cannot express.")
			cmd.Flags().StringVar(&schemaGenInstructions, "schema-instructions", "", "Free-form prose applied only to the schema-generation step when --schema is omitted (which fields to include, how to interpret ambiguous layouts).")
			cmd.Flags().StringVar(&advancedOptionsPath, "advanced-options", "", "Detection options as a JSON object: flattenPdf, nativeFieldsOnly, tableParsingEnabled, radioEnumsEnabled. Source: inline JSON, path, file:// URI, or '-' for stdin. Omitted fields use the server default.")
			cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Edit engine version (e.g. latest, 0.0.1, 1.0.0-beta). Omit for the server default; the run reports the resolved exact version.")
			cmd.Flags().StringVarP(&outputFile, "output-file", "O", "", "Path to write the filled PDF to (auto-downloads); '-' for stdout. Default: leave the PDF on the server; fetch later with 'extend files download <file-id>'.")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run ID immediately)")
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
		},
		Subcommands: []*CommandDoc{newEditTemplatesDoc(app), editRunsSpec().doc(app)},
	}
}

type editParams struct {
	input                 string
	schemaPath            string
	instructions          string
	schemaGenInstructions string
	advancedOptionsPath   string
	engineVersion         string
	outputFile            string
	password              string
	wait                  bool
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

	file, err := extendx.BuildEditFile(ref)
	if err != nil {
		return err
	}

	cfg := &extend.EditConfig{}
	if p.engineVersion != "" {
		cfg.EngineVersion = extend.String(p.engineVersion)
	}
	if p.advancedOptionsPath != "" {
		raw, err := readJSONFile(p.advancedOptionsPath, "--advanced-options")
		if err != nil {
			return err
		}
		var ao extend.EditConfigAdvancedOptions
		if err := json.Unmarshal(raw, &ao); err != nil {
			return fmt.Errorf("--advanced-options: %w", err)
		}
		cfg.AdvancedOptions = &ao
	}
	if p.instructions != "" {
		cfg.Instructions = extend.String(p.instructions)
	}
	if p.schemaGenInstructions != "" {
		cfg.SchemaGenerationInstructions = extend.String(p.schemaGenInstructions)
	}
	if p.schemaPath != "" {
		raw, err := readJSONFile(p.schemaPath, "--schema")
		if err != nil {
			return err
		}
		var schema extend.EditRootJSON
		if err := json.Unmarshal(raw, &schema); err != nil {
			return fmt.Errorf("--schema: %w", err)
		}
		cfg.Schema = &schema
	}

	req := &extend.EditRunsCreateRequest{
		File:   file,
		Config: cfg,
	}

	run, err := cli.EditRuns.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if !p.wait {
		return renderWithDefault(app, run, output.FormatJSON)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := waitForEditRun(ctx, cli, run.ID, extendx.WaitProfileOptions(extendx.ProfileShort, p.timeout), func(r *extend.EditRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return formatActionWaitError(err, run.ID, "extend edit runs watch")
	}

	if extendx.RunStatus(final.Status) == extendx.StatusFailed {
		// Best-effort render of the (failed) result before returning the
		// failure error; a render error here is secondary to the run
		// failure we're about to report.
		_ = renderEditResult(app, final)
		return runFailureError(final.ID, final.FailureReason, final.FailureMessage)
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

func waitForEditRun(ctx context.Context, c *sdkclient.Client, id string, opts extendx.WaitOptions, onPoll func(*extend.EditRun)) (*extend.EditRun, error) {
	return extendx.PollForRun(ctx,
		func(ctx context.Context) (*extend.EditRun, error) {
			return c.EditRuns.Retrieve(ctx, id, &extend.EditRunsRetrieveRequest{})
		},
		func(r *extend.EditRun) extendx.RunStatus { return extendx.RunStatus(r.Status) },
		opts, onPoll,
	)
}

func outputFileID(run *extend.EditRun) string {
	if run.Output == nil || run.Output.EditedFile == nil {
		return ""
	}
	return run.Output.EditedFile.ID
}

func downloadEditOutput(ctx context.Context, app *App, cli *sdkclient.Client, fileID, outPath string) error {
	if outPath == "-" {
		_, err := extendx.DownloadFile(ctx, cli, fileID, app.IO.Out)
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".extend-edit-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	n, err := extendx.DownloadFile(ctx, cli, fileID, tmp)
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

func renderEditResult(app *App, run *extend.EditRun) error {
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
	status := extendx.RunStatus(run.Status)
	fmt.Fprintf(app.IO.Out, "%s %s (%s)\n", statusIcon(pal, status), run.ID, run.Status)
	if status == extendx.StatusFailed && run.FailureMessage != nil && *run.FailureMessage != "" {
		fmt.Fprintf(app.IO.Out, "  %s\n", *run.FailureMessage)
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
func maybeWarnEmptyEditOutput(app *App, run *extend.EditRun) {
	if run == nil || extendx.RunStatus(run.Status) != extendx.StatusProcessed {
		return
	}
	if outputFileID(run) != "" {
		return
	}
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.ErrOut, "%s edit run %s reported PROCESSED but produced no filled PDF (output.editedFile is missing).\n",
		pal.Yellow("warning:"), run.ID)
	fmt.Fprintln(app.IO.ErrOut, pal.Dimf("  This usually means the server detected no fields to fill — double-check your --schema or --instructions. Inspect the full run with: extend edit runs get %s -o json", run.ID))
}

// newEditTemplatesDoc is the `extend edit templates` group: read-only
// access to saved edit templates (EditTemplates.Retrieve in the SDK).
func newEditTemplatesDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "templates",
		Summary: "Inspect saved edit templates",
		WhenToUse: `Use this group to fetch a saved edit template's source file, default
edit config, and schema-generation config so you can reuse them with
'extend edit' and 'extend detect-form'.`,
		Details: `Edit templates are authored in the dashboard; the CLI exposes read-only
retrieval. Only 'get <template-id>' is available.`,
		Subcommands: []*CommandDoc{
			newEditTemplatesGetDoc(app),
		},
	}
}

func newEditTemplatesGetDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "get <template-id>",
		Summary: "Fetch a saved edit template by ID",
		Triggers: []string{
			"get an edit template by id",
			"fetch a saved edit template",
			"inspect an edit template's config and schema",
			"reuse an edt_ template's edit config",
		},
		WhenToUse: `Use to retrieve a saved edit template (edt_...) — its source file, default
edit 'config', and optional 'schemaConfig'. Reuse the returned config with
'extend edit' and the schemaConfig with 'extend detect-form'.`,
		Details: `Returns the full edit template object as JSON.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend edit templates get edt_abc"},
			{Label: "Extract the default config", Cmd: "extend edit templates get edt_abc --jq '.config' -o json"},
		},
		Gotchas: []string{
			"Edit templates are authored in the dashboard; the CLI is read-only on them.",
		},
		SeeAlso: []string{"edit", "detect-form"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			tmpl, err := cli.EditTemplates.Retrieve(cmd.Context(), args[0], &extend.EditTemplatesRetrieveRequest{})
			if err != nil {
				return err
			}
			return renderWithDefault(app, tmpl, output.FormatJSON)
		},
	}
}
