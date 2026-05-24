package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newRunsDoc returns the typed documentation for `extend runs` (the
// inspect-and-follow group across all processor types) and its 6 leaves.
func newRunsDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "runs",
		Summary: "Inspect and follow runs across all processor types",
		Group:   "Inspection",
		WhenToUse: `Use these commands to inspect, watch, list, update, cancel, or delete
runs by their opaque ID. The run type (extract/parse/classify/split/
workflow/edit) is auto-detected from the ID prefix, so a single
'extend runs get' or 'extend runs watch' works across all kinds.`,
		Details: `Operations on runs identified by their opaque ID. The run type is
auto-detected from the ID prefix (exr_, pr_, clr_, splr_,
workflow_run_, edr_).`,
		Subcommands: []*CommandDoc{
			newRunsGetDoc(app),
			newRunsWatchDoc(app),
			newRunsListDoc(app),
			newRunsCancelDoc(app),
			newRunsDeleteDoc(app),
			newRunsUpdateDoc(app),
		},
	}
}

func newRunsUpdateDoc(app *App) *CommandDoc {
	var (
		fromFile string
		meta     metaFlags
	)
	return &CommandDoc{
		Use:     "update <workflow-run-id>",
		Summary: "Update workflow run metadata (workflow runs only)",
		Triggers: []string{
			"update metadata on a workflow run",
			"tag a completed workflow run",
			"patch a workflow run's metadata",
		},
		WhenToUse: `Use to attach or modify metadata on an in-flight or completed
workflow run. Only workflow runs (workflow_run_...) support this; other
run types do not.`,
		Details: `Provide a JSON body with --from-file (inline JSON, path, file:// URI, or
- for stdin; overrides everything), or use --metadata and --tag to set
keys individually.`,
		Examples: []Example{
			{Label: "Add metadata + tag", Cmd: "extend runs update workflow_run_abc --metadata customer=acme --tag prod"},
			{Label: "From patch file", Cmd: "extend runs update workflow_run_abc --from-file patch.json"},
			{Label: "Inline patch", Cmd: `extend runs update workflow_run_abc --from-file '{"metadata":{"customer":"acme"}}'`},
		},
		Gotchas: []string{
			"Only workflow runs support metadata updates; the command rejects other run types.",
			"--from-file overrides --metadata/--tag if both are passed.",
			"Pass at least one of --from-file, --metadata, or --tag; otherwise the command rejects with 'nothing to update'.",
		},
		SeeAlso: []string{"runs get", "runs list"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			kind, ok := extendx.RunKindFromID(id)
			if !ok {
				return fmt.Errorf("cannot determine run type from id %q", id)
			}
			if kind != extendx.KindWorkflow {
				return fmt.Errorf("only workflow runs (workflow_run_...) support metadata updates; got %s run", kind)
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			md, err := meta.build()
			if err != nil {
				return err
			}
			var req extend.WorkflowRunsUpdateRequest
			if fromFile != "" {
				raw, err := readJSONFile(fromFile, "--from-file")
				if err != nil {
					return err
				}
				if err := json.Unmarshal(raw, &req); err != nil {
					return fmt.Errorf("--from-file: %w", err)
				}
			} else if md != nil {
				req.Metadata = md
			} else {
				return errors.New("nothing to update; pass --from-file, --metadata, or --tag")
			}
			run, err := cli.WorkflowRuns.Update(cmd.Context(), id, &req)
			if err != nil {
				return err
			}
			return renderWorkflowResult(app, run)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON patch body, path, file:// URI, or '-' for stdin")
			meta.attach(cmd)
		},
	}
}

func newRunsGetDoc(app *App) *CommandDoc {
	var responseType string
	return &CommandDoc{
		Use:     "get <run-id>",
		Summary: "Fetch a single run by ID",
		Triggers: []string{
			"fetch a run by id",
			"get the current status of a run",
			"inspect an extract or workflow run",
			"check whether a run completed",
		},
		WhenToUse: `Use to retrieve the current state of a run by its ID. Unlike action
commands, this never waits or polls; it returns whatever state the run
is currently in. To wait for a terminal state, use 'extend runs watch'.`,
		Details: `The run type is auto-detected from the ID prefix (exr_ extract, pr_
parse, clr_ classify, splr_ split, workflow_run_, edr_ edit), so a
single 'extend runs get' call works across all kinds.

For parse runs, --response-type url returns a presigned URL to the
parsed output instead of the inline payload (useful for large documents).`,
		Examples: []Example{
			{Label: "Extract run", Cmd: "extend runs get exr_xK9mLPq"},
			{Label: "Parse run as YAML", Cmd: "extend runs get pr_pJDa8iX -o yaml"},
			{Label: "Parse run, URL response", Cmd: "extend runs get pr_pJDa8iX --response-type url -o json"},
			{Label: "Just classify confidence", Cmd: "extend runs get clr_kMXk --jq '.output.confidence' -o raw"},
		},
		Gotchas: []string{
			"--response-type only applies to parse runs; the command rejects it for other types.",
			"This command never waits; use 'extend runs watch' for live polling.",
		},
		SeeAlso: []string{"runs watch", "runs list", "runs cancel", "runs delete"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsGet(cmd.Context(), app, args[0], responseType)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&responseType, "response-type", "", "Parse runs only: json|url output payload shape")
		},
	}
}

func newRunsWatchDoc(app *App) *CommandDoc {
	var (
		timeout    time.Duration
		exitStatus bool
	)
	return &CommandDoc{
		Use:     "watch <run-id>",
		Summary: "Poll a run until it reaches a terminal state",
		Triggers: []string{
			"watch a run until it finishes",
			"poll a run for terminal state",
			"block until extract or workflow run completes",
			"follow run progress live",
		},
		WhenToUse: `Use to block until a run reaches a terminal state. Combine with
--exit-status to gate downstream scripts on success.`,
		Details: `Block until the run reaches a terminal state, showing a spinner with
status transitions. The final result is rendered using the same per-type
natural format as the originating command.

Use --exit-status for shell composition: the command exits non-zero if
the run terminates in FAILED or CANCELLED state, so:

    extend runs watch <id> --exit-status && downstream-script.sh

works as expected.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend runs watch exr_xK9mLPq"},
			{Label: "Custom timeout", Cmd: "extend runs watch pr_pJDa8iX --timeout 5m"},
			{Label: "Gate downstream script", Cmd: "extend runs watch clr_kMXk --exit-status && deploy.sh"},
		},
		Gotchas: []string{
			"Without --exit-status, the command exits 0 on any successful poll regardless of run status.",
			"Watching uses the short polling profile uniformly, even for workflow runs (live progress is the explicit ask).",
		},
		SeeAlso:  []string{"runs get", "runs list", "batches watch"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsWatch(cmd.Context(), app, args[0], timeout, exitStatus)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
			cmd.Flags().BoolVar(&exitStatus, "exit-status", false, "Exit non-zero on FAILED or CANCELLED")
		},
	}
}

func runRunsGet(ctx context.Context, app *App, id, responseType string) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	kind, ok := extendx.RunKindFromID(id)
	if !ok {
		return fmt.Errorf("cannot determine run type from id %q (expected exr_/pr_/clr_/splr_/workflow_run_/edr_ prefix)", id)
	}
	if responseType != "" && kind != extendx.KindParse {
		return fmt.Errorf("--response-type is only supported for parse runs (pr_...); got %s run", kind)
	}
	if responseType != "" && responseType != "json" && responseType != "url" {
		return fmt.Errorf("--response-type must be one of: json|url")
	}
	switch kind {
	case extendx.KindExtract:
		run, err := cli.ExtractRuns.Retrieve(ctx, id, &extend.ExtractRunsRetrieveRequest{})
		if err != nil {
			return err
		}
		return renderWithDefault(app, run, output.FormatJSON)
	case extendx.KindParse:
		req := &extend.ParseRunsRetrieveRequest{}
		if responseType != "" {
			rt, err := extend.NewParseRunsRetrieveRequestResponseTypeFromString(responseType)
			if err != nil {
				return fmt.Errorf("--response-type: %w", err)
			}
			req.ResponseType = &rt
		}
		run, err := cli.ParseRuns.Retrieve(ctx, id, req)
		if err != nil {
			return err
		}
		return renderParseResult(app, run, "markdown")
	case extendx.KindClassify:
		run, err := cli.ClassifyRuns.Retrieve(ctx, id, &extend.ClassifyRunsRetrieveRequest{})
		if err != nil {
			return err
		}
		return renderClassifyResult(app, run)
	case extendx.KindSplit:
		run, err := cli.SplitRuns.Retrieve(ctx, id, &extend.SplitRunsRetrieveRequest{})
		if err != nil {
			return err
		}
		return renderSplitResult(app, run)
	case extendx.KindWorkflow:
		run, err := cli.WorkflowRuns.Retrieve(ctx, id, &extend.WorkflowRunsRetrieveRequest{})
		if err != nil {
			return err
		}
		return renderWorkflowResult(app, run)
	case extendx.KindEdit:
		run, err := cli.EditRuns.Retrieve(ctx, id, &extend.EditRunsRetrieveRequest{})
		if err != nil {
			return err
		}
		return renderEditResult(app, run)
	default:
		return fmt.Errorf("unsupported run kind %s", kind)
	}
}

func runRunsWatch(ctx context.Context, app *App, id string, timeout time.Duration, exitStatus bool) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	kind, ok := extendx.RunKindFromID(id)
	if !ok {
		return fmt.Errorf("cannot determine run type from id %q (expected exr_/pr_/clr_/splr_/workflow_run_/edr_ prefix)", id)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Watching %s...", id))
	// Watching uses the short profile uniformly, even for workflow
	// runs: users invoking `runs watch` are explicitly asking for live
	// progress and expect responsive updates.
	opts := extendx.WaitProfileOptions(extendx.ProfileShort, timeout)

	var status extendx.RunStatus
	var renderErr error
	switch kind {
	case extendx.KindExtract:
		final, err := waitForExtractRun(ctx, cli, id, opts, func(r *extend.ExtractRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderWithDefault(app, final, output.FormatJSON)
	case extendx.KindParse:
		final, err := waitForParseRun(ctx, cli, id, opts, func(r *extend.ParseRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderParseResult(app, final, "markdown")
	case extendx.KindClassify:
		final, err := waitForClassifyRun(ctx, cli, id, opts, func(r *extend.ClassifyRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderClassifyResult(app, final)
	case extendx.KindSplit:
		final, err := waitForSplitRun(ctx, cli, id, opts, func(r *extend.SplitRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderSplitResult(app, final)
	case extendx.KindWorkflow:
		final, err := waitForWorkflowRun(ctx, cli, id, opts, func(r *extend.WorkflowRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderWorkflowResult(app, final)
	case extendx.KindEdit:
		final, err := waitForEditRun(ctx, cli, id, opts, func(r *extend.EditRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderEditResult(app, final)
	default:
		sp.Stop("")
		return fmt.Errorf("unsupported run kind %s", kind)
	}

	if renderErr != nil {
		return renderErr
	}
	if exitStatus {
		switch status {
		case extendx.StatusFailed:
			return fmt.Errorf("run %s failed", id)
		case extendx.StatusCancelled:
			return fmt.Errorf("run %s was cancelled", id)
		}
	}
	return nil
}

// relTime renders a timestamp as a human-readable relative duration
// ("5m ago", "2h ago", "3d ago"). For timestamps older than 30 days it
// falls back to an absolute YYYY-MM-DD format so the output stays
// short. The zero time renders as "" so callers can pass a pointer
// dereference without an extra nil check.
func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

// relTimeFromISO parses an RFC 3339 string and renders it via relTime.
// Used by call paths that receive timestamps as strings — primarily
// the ParseRun.createdAt field (which the SDK's typed struct doesn't
// model, so we pull it out of extraProperties as a string) and tests
// that synthesize timestamps. An unparseable input passes through
// verbatim so test ergonomics around `not-a-date` keep working.
func relTimeFromISO(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
		if err != nil {
			return iso
		}
	}
	return relTime(t)
}

func newRunsCancelDoc(app *App) *CommandDoc {
	var yes bool
	return &CommandDoc{
		Use:     "cancel <run-id>",
		Summary: "Cancel a run by ID",
		Triggers: []string{
			"cancel an in-flight run",
			"stop a running extract or workflow run",
			"abort a non-terminal run",
		},
		WhenToUse: `Use to attempt to cancel a non-terminal run. The run type is determined
from the ID prefix.`,
		Details: `Parse runs cannot be cancelled (the API rejects the attempt).

Cancellation is best-effort: an in-flight run may still complete before
the cancellation takes effect. The terminal status will be CANCELLED if
cancellation succeeded, or the original outcome (PROCESSED/FAILED/etc.)
if the run finished first.

Cancel stops a running operation; it does not remove the historical
record. Use 'extend runs delete' for that.`,
		Examples: []Example{
			{Label: "With prompt", Cmd: "extend runs cancel exr_xK9"},
			{Label: "Skip confirmation", Cmd: "extend runs cancel workflow_run_abc --yes"},
		},
		Gotchas: []string{
			"Parse runs cannot be cancelled (API rejects).",
			"Cancellation is best-effort; an in-flight run may complete first.",
			"Cancel does not delete the run record; use 'extend runs delete' to remove the history.",
		},
		SeeAlso: []string{"runs get", "runs delete", "runs watch"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsCancel(cmd.Context(), app, args[0], yes)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
		},
	}
}

func newRunsDeleteDoc(app *App) *CommandDoc {
	var yes bool
	return &CommandDoc{
		Use:     "delete <run-id>",
		Summary: "Delete a run record (any run type)",
		Triggers: []string{
			"delete a run record",
			"remove a run from workspace history",
			"clean up old runs",
		},
		WhenToUse: `Use to permanently remove a run's historical record once it has
reached a terminal state. To stop a still-running operation, use
'extend runs cancel' instead.`,
		Details: `The run type is auto-detected from the ID prefix
(exr_/pr_/clr_/splr_/edr_/workflow_run_). Deletion is permanent and the
record cannot be recovered. Use this to clean up runs from the workspace
inventory; it does not affect billing.`,
		Examples: []Example{
			{Label: "With prompt", Cmd: "extend runs delete exr_xK9"},
			{Label: "Skip confirmation", Cmd: "extend runs delete pr_abc --yes"},
		},
		Gotchas: []string{
			"Deletion is permanent; the record cannot be recovered.",
			"Deletion does not affect billing or already-emitted webhook events.",
		},
		SeeAlso: []string{"runs get", "runs cancel", "runs list"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsDelete(cmd.Context(), app, args[0], yes)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
		},
	}
}

func runRunsDelete(ctx context.Context, app *App, id string, yes bool) error {
	if _, ok := extendx.RunKindFromID(id); !ok {
		return fmt.Errorf("cannot determine run type from id %q", id)
	}
	return deleteWithConfirm(ctx, app, "run", id, yes,
		func(ctx context.Context, id string) error {
			c, err := app.NewClient()
			if err != nil {
				return err
			}
			return deleteRun(ctx, c, id)
		})
}

// deleteRun dispatches to the right per-kind delete endpoint on the
// SDK client based on the run ID's prefix. Centralized so both
// `extend runs delete` and any other generic deleter can share the
// dispatch.
func deleteRun(ctx context.Context, c *sdkclient.Client, id string) error {
	kind, ok := extendx.RunKindFromID(id)
	if !ok {
		return fmt.Errorf("unknown run id prefix: %s", id)
	}
	switch kind {
	case extendx.KindExtract:
		_, err := c.ExtractRuns.Delete(ctx, id, &extend.ExtractRunsDeleteRequest{})
		return err
	case extendx.KindParse:
		_, err := c.ParseRuns.Delete(ctx, id, &extend.ParseRunsDeleteRequest{})
		return err
	case extendx.KindClassify:
		_, err := c.ClassifyRuns.Delete(ctx, id, &extend.ClassifyRunsDeleteRequest{})
		return err
	case extendx.KindSplit:
		_, err := c.SplitRuns.Delete(ctx, id, &extend.SplitRunsDeleteRequest{})
		return err
	case extendx.KindEdit:
		_, err := c.EditRuns.Delete(ctx, id, &extend.EditRunsDeleteRequest{})
		return err
	case extendx.KindWorkflow:
		_, err := c.WorkflowRuns.Delete(ctx, id, &extend.WorkflowRunsDeleteRequest{})
		return err
	default:
		return fmt.Errorf("unsupported run kind: %s", kind)
	}
}

// cancelRun dispatches a cancel call to the right per-kind endpoint
// based on the run ID's prefix. Parse and edit runs return
// extendx.ErrNotCancellable.
func cancelRun(ctx context.Context, c *sdkclient.Client, id string) error {
	kind, ok := extendx.RunKindFromID(id)
	if !ok {
		return fmt.Errorf("unknown run id prefix: %s", id)
	}
	switch kind {
	case extendx.KindExtract:
		_, err := c.ExtractRuns.Cancel(ctx, id, &extend.ExtractRunsCancelRequest{})
		return err
	case extendx.KindClassify:
		_, err := c.ClassifyRuns.Cancel(ctx, id, &extend.ClassifyRunsCancelRequest{})
		return err
	case extendx.KindSplit:
		_, err := c.SplitRuns.Cancel(ctx, id, &extend.SplitRunsCancelRequest{})
		return err
	case extendx.KindWorkflow:
		_, err := c.WorkflowRuns.Cancel(ctx, id, &extend.WorkflowRunsCancelRequest{})
		return err
	case extendx.KindParse, extendx.KindEdit:
		return extendx.ErrNotCancellable
	default:
		return fmt.Errorf("unsupported run kind: %s", kind)
	}
}

func runRunsCancel(ctx context.Context, app *App, id string, yes bool) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	if err := extendx.CanCancel(id); err != nil {
		return err
	}

	if !yes {
		if !app.IO.IsStdinTTY() {
			return errors.New("refusing to cancel without confirmation; pass --yes to skip prompt in non-interactive contexts")
		}
		fmt.Fprintf(app.IO.ErrOut, "Cancel run %s? [y/N]: ", id)
		reader := bufio.NewReader(app.IO.In)
		line, _ := reader.ReadString('\n')
		line = strings.ToLower(strings.TrimSpace(line))
		if line != "y" && line != "yes" {
			fmt.Fprintln(app.IO.ErrOut, "Cancelled (aborted by user)")
			return nil
		}
	}

	if err := cancelRun(ctx, cli, id); err != nil {
		return err
	}
	fmt.Fprintf(app.IO.ErrOut, "%s Cancelled %s\n", paletteFor(app.IO).Green("✓"), id)
	return nil
}
