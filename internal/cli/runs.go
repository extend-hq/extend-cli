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

// runsGroupSpec parameterizes the generated `extend <verb> runs`
// subgroup for one run kind. Every action verb (extract, parse,
// classify, split, edit, workflows) attaches one of these; the
// capability flags control which leaves exist so each typed command
// carries exactly the operations and flags its kind supports. There
// is no cross-kind dispatch: the kind is fixed by the command path,
// and IDs are validated against it (extendx.ValidateRunID) so a
// pasted ID of the wrong type redirects to the right command.
type runsGroupSpec struct {
	kind      extendx.RunKind
	exampleID string
	// cancellable: extract/classify/split/workflow. Parse and edit
	// runs have no cancel endpoint.
	cancellable bool
	// listable: everything except edit (the API has no LIST /edit_runs).
	listable bool
	// updatable: workflow runs only (rename + metadata patch).
	updatable bool
	// responseType: parse runs only (--response-type json|url on get).
	responseType bool
	// usingFlag names the processor-ID filter for list ("" = no
	// --using flag; parse runs have no processor reference).
	usingFlag string
	// usingExample is a processor ID for list examples (ex_abc, ...).
	usingExample string
	// sourceFilters: --source/--source-id on list. Workflow runs have
	// no source filters at the server.
	sourceFilters bool
	// sortable: --sort-by/--sort on list. Parse runs ignore both.
	sortable bool
	// watchFailures are the terminal statuses that gate non-zero exit
	// for `runs watch --exit-status` (and the lifecycle annotation).
	watchFailures []extendx.RunStatus
	// watchGotchas are appended to the generated watch gotchas to
	// document type-specific semantics (review pauses, non-cancellability).
	watchGotchas []string
}

func extractRunsSpec() runsGroupSpec {
	return runsGroupSpec{
		kind:          extendx.KindExtract,
		exampleID:     "exr_xK9mLPq",
		cancellable:   true,
		listable:      true,
		usingFlag:     "extractor",
		usingExample:  "ex_abc",
		sourceFilters: true,
		sortable:      true,
		watchFailures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
	}
}

func parseRunsSpec() runsGroupSpec {
	return runsGroupSpec{
		kind:          extendx.KindParse,
		exampleID:     "pr_pJDa8iX",
		listable:      true,
		responseType:  true,
		sourceFilters: true,
		watchFailures: []extendx.RunStatus{extendx.StatusFailed},
		watchGotchas: []string{
			"Parse runs cannot be cancelled; a watched parse run only ends in PROCESSED or FAILED.",
		},
	}
}

func classifyRunsSpec() runsGroupSpec {
	return runsGroupSpec{
		kind:          extendx.KindClassify,
		exampleID:     "clr_kMXkR",
		cancellable:   true,
		listable:      true,
		usingFlag:     "classifier",
		usingExample:  "cl_abc",
		sourceFilters: true,
		sortable:      true,
		watchFailures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
	}
}

func splitRunsSpec() runsGroupSpec {
	return runsGroupSpec{
		kind:          extendx.KindSplit,
		exampleID:     "splr_s8Yqw",
		cancellable:   true,
		listable:      true,
		usingFlag:     "splitter",
		usingExample:  "spl_abc",
		sourceFilters: true,
		sortable:      true,
		watchFailures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
	}
}

func editRunsSpec() runsGroupSpec {
	return runsGroupSpec{
		kind:          extendx.KindEdit,
		exampleID:     "edr_aB3xY",
		watchFailures: []extendx.RunStatus{extendx.StatusFailed},
		watchGotchas: []string{
			"Edit runs cannot be cancelled; a watched edit run only ends in PROCESSED or FAILED.",
		},
	}
}

func workflowRunsSpec() runsGroupSpec {
	return runsGroupSpec{
		kind:          extendx.KindWorkflow,
		exampleID:     "workflow_run_abc",
		cancellable:   true,
		listable:      true,
		updatable:     true,
		usingFlag:     "workflow",
		usingExample:  "workflow_abc",
		sortable:      true,
		watchFailures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled, extendx.StatusRejected},
		watchGotchas: []string{
			"NEEDS_REVIEW is terminal for watch: the run pauses for human review at the dashboard and the command returns.",
		},
	}
}

// name returns the kind as prose ("extract", "workflow", ...).
func (s runsGroupSpec) name() string { return string(s.kind) }

// verb returns the owning command group ("extract", ..., "workflows").
func (s runsGroupSpec) verb() string { return s.kind.Verb() }

// path renders a command path under this spec's group, e.g.
// path("runs get") = "extract runs get".
func (s runsGroupSpec) path(rest string) string { return s.verb() + " " + rest }

// doc returns the typed documentation tree for `extend <verb> runs`.
func (s runsGroupSpec) doc(app *App) *CommandDoc {
	subs := []*CommandDoc{s.getDoc(app)}
	if s.listable {
		subs = append(subs, s.listDoc(app))
	}
	subs = append(subs, s.watchDoc(app))
	if s.cancellable {
		subs = append(subs, s.cancelDoc(app))
	}
	subs = append(subs, s.deleteDoc(app))
	if s.updatable {
		subs = append(subs, s.updateDoc(app))
	}
	return &CommandDoc{
		Use:     "runs",
		Summary: fmt.Sprintf("Inspect and follow %s runs", s.name()),
		WhenToUse: fmt.Sprintf(`Use these commands to operate on %s runs (%s...) by ID: fetch current
state, poll to a terminal state, %sor delete the record.`,
			s.name(), extendx.RunIDPrefix(s.kind), s.optionalVerbsProse()),
		Details: fmt.Sprintf(`Operations on %s runs identified by their %s ID. Each run type has its
own runs group; an ID with a different prefix is rejected with a
pointer to the owning command.`, s.name(), extendx.RunIDPrefix(s.kind)),
		Subcommands: subs,
	}
}

// optionalVerbsProse lists the capability-dependent operations for the
// group WhenToUse sentence.
func (s runsGroupSpec) optionalVerbsProse() string {
	var parts []string
	if s.listable {
		parts = append(parts, "list with filters")
	}
	if s.cancellable {
		parts = append(parts, "cancel in-flight runs")
	}
	if s.updatable {
		parts = append(parts, "update name/metadata")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ") + ", "
}

func (s runsGroupSpec) getDoc(app *App) *CommandDoc {
	var responseType string
	details := fmt.Sprintf(`Fetches the %s run and renders it in the same per-type format as the
originating command.`, s.name())
	examples := []Example{
		{Label: "Basic", Cmd: fmt.Sprintf("extend %s %s", s.path("runs get"), s.exampleID)},
		{Label: "As JSON", Cmd: fmt.Sprintf("extend %s %s -o json", s.path("runs get"), s.exampleID)},
	}
	gotchas := []string{
		fmt.Sprintf("This command never waits; use 'extend %s' for live polling.", s.path("runs watch")),
	}
	if s.responseType {
		details += `

--response-type url returns a presigned URL to the parsed output
instead of the inline payload (useful for large documents).`
		examples = append(examples, Example{Label: "URL response", Cmd: fmt.Sprintf("extend %s %s --response-type url -o json", s.path("runs get"), s.exampleID)})
	}
	return &CommandDoc{
		Use:     "get <run-id>",
		Summary: fmt.Sprintf("Fetch a single %s run by ID", s.name()),
		Triggers: []string{
			fmt.Sprintf("fetch %s %s run by id", articleFor(s.name()), s.name()),
			fmt.Sprintf("get the current status of %s %s run", articleFor(s.name()), s.name()),
			fmt.Sprintf("check whether %s %s run completed", articleFor(s.name()), s.name()),
		},
		WhenToUse: fmt.Sprintf(`Use to retrieve the current state of %s %s run by its ID. Unlike the
action command, this never waits or polls; it returns whatever state the
run is currently in. To wait for a terminal state, use 'extend %s'.`,
			articleFor(s.name()), s.name(), s.path("runs watch")),
		Details:  details,
		Examples: examples,
		Gotchas:  gotchas,
		SeeAlso:  s.seeAlso("get"),
		Output:   OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTypedRunsGet(cmd.Context(), app, s.kind, args[0], responseType)
		},
		Configure: func(cmd *cobra.Command) {
			if s.responseType {
				cmd.Flags().StringVar(&responseType, "response-type", "", "Output payload shape: json|url (url returns a presigned link to the parsed output)")
			}
		},
	}
}

func (s runsGroupSpec) watchDoc(app *App) *CommandDoc {
	var (
		timeout    time.Duration
		exitStatus bool
	)
	gotchas := append([]string{
		"Without --exit-status, the command exits 0 on any successful poll regardless of run status.",
	}, s.watchGotchas...)
	return &CommandDoc{
		Use:     "watch <run-id>",
		Summary: fmt.Sprintf("Poll %s %s run until it reaches a terminal state", articleFor(s.name()), s.name()),
		Triggers: []string{
			fmt.Sprintf("watch %s %s run until it finishes", articleFor(s.name()), s.name()),
			fmt.Sprintf("poll %s %s run for terminal state", articleFor(s.name()), s.name()),
			fmt.Sprintf("follow %s run progress live", s.name()),
		},
		WhenToUse: fmt.Sprintf(`Use to block until %s %s run reaches a terminal state. Combine with
--exit-status to gate downstream scripts on success.`, articleFor(s.name()), s.name()),
		Details: fmt.Sprintf(`Block until the run reaches a terminal state, showing a spinner with
status transitions. The final result is rendered using the same format
as the originating command.

Use --exit-status for shell composition: the command exits non-zero if
the run terminates in %s, so:

    extend %s <id> --exit-status && downstream-script.sh

works as expected.`, statusListProse(s.watchFailures), s.path("runs watch")),
		Examples: []Example{
			{Label: "Basic", Cmd: fmt.Sprintf("extend %s %s", s.path("runs watch"), s.exampleID)},
			{Label: "Custom timeout", Cmd: fmt.Sprintf("extend %s %s --timeout 5m", s.path("runs watch"), s.exampleID)},
			{Label: "Gate downstream script", Cmd: fmt.Sprintf("extend %s %s --exit-status && deploy.sh", s.path("runs watch"), s.exampleID)},
		},
		Gotchas:  gotchas,
		SeeAlso:  s.seeAlso("watch"),
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: s.watchFailures,
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTypedRunsWatch(cmd.Context(), app, s, args[0], timeout, exitStatus)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum total time to wait for the run to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
			cmd.Flags().BoolVar(&exitStatus, "exit-status", false, "Exit non-zero when the run terminates in "+statusListProse(s.watchFailures))
		},
	}
}

func (s runsGroupSpec) cancelDoc(app *App) *CommandDoc {
	var yes bool
	return &CommandDoc{
		Use:     "cancel <run-id>",
		Summary: fmt.Sprintf("Cancel %s %s run by ID", articleFor(s.name()), s.name()),
		Triggers: []string{
			fmt.Sprintf("cancel an in-flight %s run", s.name()),
			fmt.Sprintf("stop a running %s run", s.name()),
			fmt.Sprintf("abort a non-terminal %s run", s.name()),
		},
		WhenToUse: fmt.Sprintf(`Use to attempt to cancel a non-terminal %s run.`, s.name()),
		Details: fmt.Sprintf(`Cancellation is best-effort: an in-flight run may still complete before
the cancellation takes effect. The terminal status will be CANCELLED if
cancellation succeeded, or the original outcome (PROCESSED/FAILED/etc.)
if the run finished first.

Cancel stops a running operation; it does not remove the historical
record. Use 'extend %s' for that.`, s.path("runs delete")),
		Examples: []Example{
			{Label: "With prompt", Cmd: fmt.Sprintf("extend %s %s", s.path("runs cancel"), s.exampleID)},
			{Label: "Skip confirmation", Cmd: fmt.Sprintf("extend %s %s --yes", s.path("runs cancel"), s.exampleID)},
		},
		Gotchas: []string{
			"Cancellation is best-effort; an in-flight run may complete first.",
			fmt.Sprintf("Cancel does not delete the run record; use 'extend %s' to remove the history.", s.path("runs delete")),
		},
		SeeAlso: s.seeAlso("cancel"),
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTypedRunsCancel(cmd.Context(), app, s.kind, args[0], yes)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
		},
	}
}

func (s runsGroupSpec) deleteDoc(app *App) *CommandDoc {
	var yes bool
	stopHint := ""
	if s.cancellable {
		stopHint = fmt.Sprintf(" To stop a still-running operation, use 'extend %s' instead.", s.path("runs cancel"))
	}
	return &CommandDoc{
		Use:     "delete <run-id>",
		Summary: fmt.Sprintf("Delete %s %s run record", articleFor(s.name()), s.name()),
		Triggers: []string{
			fmt.Sprintf("delete %s %s run record", articleFor(s.name()), s.name()),
			fmt.Sprintf("remove %s %s run from workspace history", articleFor(s.name()), s.name()),
			fmt.Sprintf("clean up old %s runs", s.name()),
		},
		WhenToUse: fmt.Sprintf(`Use to permanently remove %s %s run's historical record once it has
reached a terminal state.%s`, articleFor(s.name()), s.name(), stopHint),
		Details: `Deletion is permanent and the record cannot be recovered. Use this to
clean up runs from the workspace inventory; it does not affect billing.`,
		Examples: []Example{
			{Label: "With prompt", Cmd: fmt.Sprintf("extend %s %s", s.path("runs delete"), s.exampleID)},
			{Label: "Skip confirmation", Cmd: fmt.Sprintf("extend %s %s --yes", s.path("runs delete"), s.exampleID)},
		},
		Gotchas: []string{
			"Deletion is permanent; the record cannot be recovered.",
			"Deletion does not affect billing or already-emitted webhook events.",
		},
		SeeAlso: s.seeAlso("delete"),
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := extendx.ValidateRunID(s.kind, id, "delete"); err != nil {
				return err
			}
			return deleteWithConfirm(cmd.Context(), app, "run", id, yes,
				func(ctx context.Context, id string) error {
					c, err := app.NewClient()
					if err != nil {
						return err
					}
					return deleteRun(ctx, c, s.kind, id)
				})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
		},
	}
}

// updateDoc is workflow-only: workflow runs are the single kind with a
// PATCH endpoint (rename + metadata).
func (s runsGroupSpec) updateDoc(app *App) *CommandDoc {
	var (
		fromFile string
		name     string
		meta     metaFlags
	)
	return &CommandDoc{
		Use:     "update <run-id>",
		Summary: "Update workflow run name and metadata",
		Triggers: []string{
			"update metadata on a workflow run",
			"rename a workflow run",
			"tag a completed workflow run",
			"patch a workflow run's metadata",
		},
		WhenToUse: `Use to rename a workflow run or attach/modify its metadata, in-flight or
completed. Workflow runs are the only run type with an update endpoint.`,
		Details: `Provide a JSON body with --from-file (inline JSON, path, file:// URI, or
- for stdin; overrides everything), or set fields individually with
--name (rename the run), --metadata, and --tag.`,
		Examples: []Example{
			{Label: "Rename", Cmd: `extend workflows runs update workflow_run_abc --name "Q3 reprocess"`},
			{Label: "Add metadata + tag", Cmd: "extend workflows runs update workflow_run_abc --metadata customer=acme --tag prod"},
			{Label: "From patch file", Cmd: "extend workflows runs update workflow_run_abc --from-file patch.json"},
			{Label: "Inline patch", Cmd: `extend workflows runs update workflow_run_abc --from-file '{"metadata":{"customer":"acme"}}'`},
		},
		Gotchas: []string{
			"--from-file overrides --name/--metadata/--tag if both are passed.",
			"Pass at least one of --from-file, --name, --metadata, or --tag; otherwise the command rejects with 'nothing to update'.",
		},
		SeeAlso: s.seeAlso("update"),
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := extendx.ValidateRunID(extendx.KindWorkflow, id, "update"); err != nil {
				return err
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
			} else {
				if name != "" {
					req.Name = extend.String(name)
				}
				if md != nil {
					req.Metadata = md
				}
				if name == "" && md == nil {
					return errors.New("nothing to update; pass --from-file, --name, --metadata, or --tag")
				}
			}
			run, err := cli.WorkflowRuns.Update(cmd.Context(), id, &req)
			if err != nil {
				return err
			}
			return renderWorkflowResult(app, run)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON patch body, path, file:// URI, or '-' for stdin")
			cmd.Flags().StringVar(&name, "name", "", "New display name for the workflow run")
			meta.attach(cmd)
		},
	}
}

// seeAlso builds the sibling cross-references for one leaf, excluding
// itself and filtered to the capabilities this kind actually has.
func (s runsGroupSpec) seeAlso(self string) []string {
	var out []string
	add := func(action string) {
		if action != self {
			out = append(out, s.path("runs "+action))
		}
	}
	add("get")
	add("watch")
	if s.listable {
		add("list")
	}
	if s.cancellable {
		add("cancel")
	}
	add("delete")
	return out
}

// statusListProse renders a status slice as "FAILED or CANCELLED" /
// "FAILED, CANCELLED, or REJECTED" for help prose.
func statusListProse(statuses []extendx.RunStatus) string {
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = string(s)
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " or " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

func runTypedRunsGet(ctx context.Context, app *App, kind extendx.RunKind, id, responseType string) error {
	if err := extendx.ValidateRunID(kind, id, "get"); err != nil {
		return err
	}
	if responseType != "" && responseType != "json" && responseType != "url" {
		return fmt.Errorf("--response-type must be one of: json|url")
	}
	cli, err := app.NewClient()
	if err != nil {
		return err
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

func runTypedRunsWatch(ctx context.Context, app *App, s runsGroupSpec, id string, timeout time.Duration, exitStatus bool) error {
	if err := extendx.ValidateRunID(s.kind, id, "watch"); err != nil {
		return err
	}
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Watching %s...", id))
	// Watching uses the short profile uniformly, even for workflow
	// runs: users invoking `runs watch` are explicitly asking for live
	// progress and expect responsive updates.
	opts := extendx.WaitProfileOptions(extendx.ProfileShort, timeout)
	watchCmd := "extend " + s.path("runs watch")

	var status extendx.RunStatus
	var renderErr error
	switch s.kind {
	case extendx.KindExtract:
		final, err := waitForExtractRun(ctx, cli, id, opts, func(r *extend.ExtractRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id, watchCmd)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderWithDefault(app, final, output.FormatJSON)
	case extendx.KindParse:
		final, err := waitForParseRun(ctx, cli, id, opts, func(r *extend.ParseRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id, watchCmd)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderParseResult(app, final, "markdown")
	case extendx.KindClassify:
		final, err := waitForClassifyRun(ctx, cli, id, opts, func(r *extend.ClassifyRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id, watchCmd)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderClassifyResult(app, final)
	case extendx.KindSplit:
		final, err := waitForSplitRun(ctx, cli, id, opts, func(r *extend.SplitRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id, watchCmd)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderSplitResult(app, final)
	case extendx.KindWorkflow:
		final, err := waitForWorkflowRun(ctx, cli, id, opts, func(r *extend.WorkflowRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id, watchCmd)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderWorkflowResult(app, final)
	case extendx.KindEdit:
		final, err := waitForEditRun(ctx, cli, id, opts, func(r *extend.EditRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return formatWatchWaitError(err, id, watchCmd)
		}
		status = extendx.RunStatus(final.Status)
		renderErr = renderEditResult(app, final)
	default:
		sp.Stop("")
		return fmt.Errorf("unsupported run kind %s", s.kind)
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
		case extendx.StatusRejected:
			return fmt.Errorf("run %s was rejected", id)
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

// deleteRun calls the per-kind delete endpoint on the SDK client. The
// kind comes from the invoked typed command; the ID has already been
// validated against it.
func deleteRun(ctx context.Context, c *sdkclient.Client, kind extendx.RunKind, id string) error {
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

// cancelRun calls the per-kind cancel endpoint. Only kinds with a
// cancel endpoint have a cancel command, so parse/edit never reach
// this switch.
func cancelRun(ctx context.Context, c *sdkclient.Client, kind extendx.RunKind, id string) error {
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
	default:
		return fmt.Errorf("unsupported run kind: %s", kind)
	}
}

func runTypedRunsCancel(ctx context.Context, app *App, kind extendx.RunKind, id string, yes bool) error {
	if err := extendx.ValidateRunID(kind, id, "cancel"); err != nil {
		return err
	}
	cli, err := app.NewClient()
	if err != nil {
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

	if err := cancelRun(ctx, cli, kind, id); err != nil {
		return err
	}
	fmt.Fprintf(app.IO.ErrOut, "%s Cancelled %s\n", paletteFor(app.IO).Green("✓"), id)
	return nil
}
