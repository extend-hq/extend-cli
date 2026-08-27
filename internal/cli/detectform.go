package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newDetectFormDoc returns the typed documentation for `extend
// detect-form`, the action verb wrapping POST /form_detection_runs
// (sgr_...): detect the form fields of a fillable PDF and scaffold an
// edit schema for 'extend edit --schema'. The `detect-form runs`
// subgroup (get/watch) is generated from detectFormRunsSpec.
func newDetectFormDoc(app *App) *CommandDoc {
	var (
		advancedOptionsPath string
		engineVersion       string
		instructions        string
		inputSchemaPath     string
		password            string
		wait                bool
		timeout             time.Duration
	)
	return &CommandDoc{
		Use:     "detect-form <input>",
		Summary: "Detect form fields and scaffold an edit schema",
		Group:   "Actions",
		Triggers: []string{
			"detect form fields in a pdf",
			"scaffold a schema for an extend edit run",
			"generate the json schema for a fillable pdf",
			"derive an edit schema from a form",
			"start a form detection run",
		},
		WhenToUse: `Use to detect the form fields of a PDF and scaffold an edit schema you
can hand-edit (populate extend_edit:value fields) and pass to 'extend
edit --schema'. Waits for the run by default; the schema lands under
output.schema in the printed run object.`,
		Details: `Start a form detection run (sgr_...) and, by default, poll until it
reaches PROCESSED or FAILED, then print the full run as JSON. Pass
--wait=false to print the newly created run immediately and follow it
later with 'extend detect-form runs watch <id>'.

Use --instructions to guide the schema generator about which fields to
include or how to interpret ambiguous form layouts. Use --input-schema
to seed the generator with an existing schema, in which case detected
fields are overlaid onto your starting point.

Use --engine-version to pin the Edit engine used for detection (an
exact version like 1.0.0-beta for reproducible results, or latest for
the latest stable version). Omitted runs use the server default; the
run object reports the resolved exact version under config.

Detection options ride in --advanced-options as a JSON object (omitted
fields use the server default):

  nativeFieldsOnly     bool  Only use embedded AcroForm fields; set false to also detect fields via vision.
  tableParsingEnabled  bool  Parse table regions as arrays of objects.
  radioEnumsEnabled    bool  Model a radio-button group as a single-choice enum.

` + formDetectionOutputDoc + `

` + editSchemaPropertyDoc,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend detect-form form.pdf"},
			{Label: "Just the schema", Cmd: "extend detect-form form.pdf --jq '.output.schema' -o json > schema.json"},
			{Label: "With instructions", Cmd: `extend detect-form form.pdf --instructions "skip the signature block"`},
			{Label: "Pin the engine version", Cmd: "extend detect-form form.pdf --engine-version 1.0.0-beta"},
			{Label: "Seed from existing", Cmd: "extend detect-form form.pdf --input-schema base.json"},
			{Label: "Async (return run ID)", Cmd: "extend detect-form form.pdf --wait=false"},
		},
		Gotchas: []string{
			"The schema is nested under output.schema in the run object; use --jq '.output.schema' to feed 'extend edit --schema'.",
			"With --wait=false, follow up with 'extend detect-form runs get|watch'; detection runs cannot be cancelled.",
		},
		SeeAlso:  []string{"edit", "detect-form runs get", "detect-form runs watch"},
		Output:   OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			ref, err := uploadOrResolveWith(cmd.Context(), app, cli, args[0], password)
			if err != nil {
				return err
			}
			file, err := extendx.BuildFormDetectionFile(ref)
			if err != nil {
				return err
			}
			cfg := &extend.EditSchemaGenerationConfig{}
			if engineVersion != "" {
				cfg.EngineVersion = extend.String(engineVersion)
			}
			if advancedOptionsPath != "" {
				raw, err := readJSONFile(advancedOptionsPath, "--advanced-options")
				if err != nil {
					return err
				}
				var ao extend.EditSchemaGenerationConfigAdvancedOptions
				if err := json.Unmarshal(raw, &ao); err != nil {
					return fmt.Errorf("--advanced-options: %w", err)
				}
				cfg.AdvancedOptions = &ao
			}
			if instructions != "" {
				cfg.Instructions = extend.String(instructions)
			}
			if inputSchemaPath != "" {
				raw, err := readJSONFile(inputSchemaPath, "--input-schema")
				if err != nil {
					return err
				}
				var schema extend.EditRootJSON
				if err := json.Unmarshal(raw, &schema); err != nil {
					return fmt.Errorf("--input-schema: %w", err)
				}
				cfg.InputSchema = &schema
			}
			run, err := cli.FormDetectionRuns.Create(cmd.Context(), &extend.FormDetectionRunsCreateRequest{
				File:   file,
				Config: cfg,
			})
			if err != nil {
				return fmt.Errorf("create run: %w", err)
			}
			if !wait {
				return renderWithDefault(app, run, output.FormatJSON)
			}
			sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: %s", run.ID, run.Status))
			final, err := waitForFormDetectionRun(cmd.Context(), cli, run.ID, extendx.WaitProfileOptions(extendx.ProfileShort, timeout), func(r *extend.FormDetectionRun) {
				sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
			})
			sp.Stop("")
			if err != nil {
				return formatActionWaitError(err, run.ID, "extend detect-form runs watch")
			}
			if extendx.RunStatus(final.Status) == extendx.StatusFailed {
				// Best-effort render of the failed run before returning
				// the failure error; a render error here is secondary.
				_ = renderWithDefault(app, final, output.FormatJSON)
				return runFailureError(final.ID, final.FailureReason, final.FailureMessage)
			}
			return renderWithDefault(app, final, output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&advancedOptionsPath, "advanced-options", "", "Detection options as a JSON object: nativeFieldsOnly, tableParsingEnabled, radioEnumsEnabled. Source: inline JSON, path, file:// URI, or '-' for stdin. Omitted fields use the server default.")
			cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Edit engine version (e.g. latest, 0.0.1, 1.0.0-beta). Omit for the server default; the run reports the resolved exact version.")
			cmd.Flags().StringVar(&instructions, "instructions", "", "Free-form instructions to guide schema generation")
			cmd.Flags().StringVar(&inputSchemaPath, "input-schema", "", "Starting-point JSON Schema (overlaid by detection). Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the run to reach a terminal state (--wait=false returns the run immediately)")
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
		},
		Subcommands: []*CommandDoc{detectFormRunsSpec().doc(app)},
	}
}

func waitForFormDetectionRun(ctx context.Context, c *sdkclient.Client, id string, opts extendx.WaitOptions, onPoll func(*extend.FormDetectionRun)) (*extend.FormDetectionRun, error) {
	return extendx.PollForRun(ctx,
		func(ctx context.Context) (*extend.FormDetectionRun, error) {
			return c.FormDetectionRuns.Retrieve(ctx, id, &extend.FormDetectionRunsRetrieveRequest{})
		},
		func(r *extend.FormDetectionRun) extendx.RunStatus { return extendx.RunStatus(r.Status) },
		opts, onPoll,
	)
}
