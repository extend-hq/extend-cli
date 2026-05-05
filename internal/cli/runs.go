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

	"github.com/extend-hq/extend-cli/internal/client"
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
			kind, ok := client.RunKindFromID(id)
			if !ok {
				return fmt.Errorf("cannot determine run type from id %q", id)
			}
			if kind != client.KindWorkflow {
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
			var body []byte
			if fromFile != "" {
				body, err = readJSONFile(fromFile, "--from-file")
				if err != nil {
					return err
				}
			} else if md != nil {
				body, err = jsonMarshal(map[string]any{"metadata": md})
				if err != nil {
					return err
				}
			} else {
				return errors.New("nothing to update; pass --from-file, --metadata, or --tag")
			}
			run, err := cli.UpdateWorkflowRun(cmd.Context(), id, body)
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

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
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
		Wait:     &WaitSpec{Profile: client.ProfileShort, DefaultsToWait: true},
		Failures: []client.RunStatus{client.StatusFailed, client.StatusCancelled},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsWatch(cmd.Context(), app, args[0], timeout, exitStatus)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum time to wait")
			cmd.Flags().BoolVar(&exitStatus, "exit-status", false, "Exit non-zero on FAILED or CANCELLED")
		},
	}
}

func runRunsGet(ctx context.Context, app *App, id, responseType string) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	kind, ok := client.RunKindFromID(id)
	if !ok {
		return fmt.Errorf("cannot determine run type from id %q (expected exr_/pr_/clr_/splr_/workflow_run_/edr_ prefix)", id)
	}
	if responseType != "" && kind != client.KindParse {
		return fmt.Errorf("--response-type is only supported for parse runs (pr_...); got %s run", kind)
	}
	if responseType != "" && responseType != "json" && responseType != "url" {
		return fmt.Errorf("--response-type must be one of: json|url")
	}
	switch kind {
	case client.KindExtract:
		run, err := cli.GetExtractRun(ctx, id)
		if err != nil {
			return err
		}
		return renderWithDefault(app, run, output.FormatJSON)
	case client.KindParse:
		run, err := cli.GetParseRunWithOptions(ctx, id, client.GetParseRunOptions{ResponseType: responseType})
		if err != nil {
			return err
		}
		return renderParseResult(app, run, "markdown")
	case client.KindClassify:
		run, err := cli.GetClassifyRun(ctx, id)
		if err != nil {
			return err
		}
		return renderClassifyResult(app, run)
	case client.KindSplit:
		run, err := cli.GetSplitRun(ctx, id)
		if err != nil {
			return err
		}
		return renderSplitResult(app, run)
	case client.KindWorkflow:
		run, err := cli.GetWorkflowRun(ctx, id)
		if err != nil {
			return err
		}
		return renderWorkflowResult(app, run)
	case client.KindEdit:
		run, err := cli.GetEditRun(ctx, id)
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
	kind, ok := client.RunKindFromID(id)
	if !ok {
		return fmt.Errorf("cannot determine run type from id %q (expected exr_/pr_/clr_/splr_/workflow_run_/edr_ prefix)", id)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Watching %s...", id))
	// Watching uses the short profile uniformly, even for workflow runs:
	// users invoking `runs watch` are explicitly asking for live progress and
	// expect responsive updates.
	opts := client.WaitProfileOptions(client.ProfileShort, timeout)

	var status client.RunStatus
	var renderErr error
	switch kind {
	case client.KindExtract:
		final, err := cli.WaitForExtractRun(ctx, id, opts, func(r *client.ExtractRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		status = final.Status
		renderErr = renderWithDefault(app, final, output.FormatJSON)
	case client.KindParse:
		final, err := cli.WaitForParseRun(ctx, id, opts, func(r *client.ParseRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		status = final.Status
		renderErr = renderParseResult(app, final, "markdown")
	case client.KindClassify:
		final, err := cli.WaitForClassifyRun(ctx, id, opts, func(r *client.ClassifyRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		status = final.Status
		renderErr = renderClassifyResult(app, final)
	case client.KindSplit:
		final, err := cli.WaitForSplitRun(ctx, id, opts, func(r *client.SplitRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		status = final.Status
		renderErr = renderSplitResult(app, final)
	case client.KindWorkflow:
		final, err := cli.WaitForWorkflowRun(ctx, id, opts, func(r *client.WorkflowRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		status = final.Status
		renderErr = renderWorkflowResult(app, final)
	case client.KindEdit:
		final, err := cli.WaitForEditRun(ctx, id, opts, func(r *client.EditRun) {
			sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
		})
		sp.Stop("")
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		status = final.Status
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
		case client.StatusFailed:
			return fmt.Errorf("run %s failed", id)
		case client.StatusCancelled:
			return fmt.Errorf("run %s was cancelled", id)
		}
	}
	return nil
}

func newRunsListDoc(app *App) *CommandDoc {
	var (
		runType   string
		status    string
		using     string
		batchID   string
		source    string
		sourceID  string
		fileName  string
		limit     int
		all       bool
		pageToken string
		sortBy    string
		sortDir   string
	)
	return &CommandDoc{
		Use:     "list",
		Summary: "List runs of a given processor type",
		Triggers: []string{
			"list runs by processor type",
			"find recent runs of a workflow",
			"page through extract runs",
			"see runs in a batch",
			"filter runs by status or processor",
		},
		WhenToUse: `Use to enumerate runs of a single type with rich filtering. Pass
--type extract|parse|classify|split|workflow (edit is not listable; use
'extend runs get' for individual edit runs).`,
		Details: `Most filter flags map directly to documented query parameters on the
run-list endpoints; the wire shape varies slightly by type (e.g. parse
runs ignore --using, --sort-by, and --sort; workflow runs ignore
--source and --source-id).

` + paginationGuidance,
		Examples: []Example{
			{Label: "All extract runs", Cmd: "extend runs list --type extract"},
			{Label: "Filter by status + processor", Cmd: "extend runs list --type extract --using ex_abc --status PROCESSED"},
			{Label: "Workflow runs by file name", Cmd: "extend runs list --type workflow --using workflow_abc --file-name invoice"},
			{Label: "Runs spawned by a workflow", Cmd: "extend runs list --type extract --source WORKFLOW_RUN --source-id workflow_run_x"},
			{Label: "Runs in a batch", Cmd: "extend runs list --type extract --batch bpr_xK9mLPq"},
			{Label: "Next page", Cmd: "extend runs list --type extract --page-token <token-from-previous-response>"},
			{Label: "Custom sort", Cmd: "extend runs list --type extract --sort-by updatedAt --sort asc"},
		},
		Gotchas: []string{
			"--type is required.",
			"Edit runs are not listable; use 'extend runs get edr_...' for individual edit runs.",
			"Parse runs ignore --using, --sort-by, and --sort; workflow runs ignore --source and --source-id.",
		},
		SeeAlso: []string{"runs get", "runs watch", "batches get"},
		Output:  OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsList(cmd, app, runsListParams{
				runType:   runType,
				status:    status,
				using:     using,
				batchID:   batchID,
				source:    source,
				sourceID:  sourceID,
				fileName:  fileName,
				limit:     limit,
				all:       all,
				pageToken: pageToken,
				sortBy:    sortBy,
				sortDir:   sortDir,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&runType, "type", "", "Run type: extract|parse|classify|split|workflow (edit is not listable; use 'extend runs get')")
			cmd.Flags().StringVar(&status, "status", "", "Filter by status (varies by type; workflow also supports NEEDS_REVIEW|REJECTED|CANCELLING; parse excludes CANCELLED)")
			cmd.Flags().StringVar(&using, "using", "", "Filter by processor ID (ex_/cl_/spl_/workflow_; ignored for parse)")
			cmd.Flags().StringVar(&batchID, "batch", "", "Filter by batch run ID (bpr_..., or bpar_... for parse)")
			cmd.Flags().StringVar(&source, "source", "", "Filter by run source: API|STUDIO|WORKFLOW_RUN|ADMIN|... (ignored for workflow)")
			cmd.Flags().StringVar(&sourceID, "source-id", "", "Filter by source resource ID, e.g. workflow_run_xxx (ignored for workflow)")
			cmd.Flags().StringVar(&fileName, "file-name", "", "Filter to runs whose file name contains this substring")
			cmd.Flags().IntVar(&limit, "limit", 20, "Maximum runs to return per page")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Fetch a specific page (token from a previous response's nextPageToken)")
			cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate every page into one response (avoid for agent use; prefer --page-token)")
			cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt; ignored for parse)")
			cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc (ignored for parse)")
			_ = cmd.MarkFlagRequired("type")
		},
	}
}

type runsListParams struct {
	runType   string
	status    string
	using     string
	batchID   string
	source    string
	sourceID  string
	fileName  string
	limit     int
	all       bool
	pageToken string
	sortBy    string
	sortDir   string
}

func runRunsList(cmd *cobra.Command, app *App, p runsListParams) error {
	ctx := cmd.Context()
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	kind, err := parseRunKind(p.runType)
	if err != nil {
		return err
	}

	opts := client.ListRunsOptions{
		Status:           p.status,
		ProcessorID:      p.using,
		BatchID:          p.batchID,
		Source:           p.source,
		SourceID:         p.sourceID,
		FileNameContains: p.fileName,
		Limit:            p.limit,
		PageToken:        p.pageToken,
		SortBy:           p.sortBy,
		SortDir:          p.sortDir,
	}

	rows, pages, err := collectListRows(ctx, cli, kind, opts, p.all)
	if err != nil {
		return err
	}

	return renderListForCmd(cmd, app, pages, []string{"id", "status", "processor", "created"}, rows, "No runs.")
}

func parseRunKind(s string) (client.RunKind, error) {
	switch strings.ToLower(s) {
	case "extract":
		return client.KindExtract, nil
	case "parse":
		return client.KindParse, nil
	case "classify":
		return client.KindClassify, nil
	case "split":
		return client.KindSplit, nil
	case "workflow":
		return client.KindWorkflow, nil
	case "edit":
		return client.KindEdit, nil
	}
	return "", fmt.Errorf("unknown run type %q (want extract|parse|classify|split|workflow|edit)", s)
}

func collectListRows(ctx context.Context, cli *client.Client, kind client.RunKind, opts client.ListRunsOptions, all bool) ([][]string, []any, error) {
	var rows [][]string
	var rawPages []any
	for {
		var (
			pageRows  [][]string
			page      any
			pageToken string
		)
		switch kind {
		case client.KindExtract:
			r, err := cli.ListExtractRuns(ctx, opts)
			if err != nil {
				return nil, nil, err
			}
			page = r
			pageToken = r.NextPageToken
			for _, run := range r.Data {
				pageRows = append(pageRows, extractRow(run))
			}
		case client.KindParse:
			r, err := cli.ListParseRuns(ctx, opts)
			if err != nil {
				return nil, nil, err
			}
			page = r
			pageToken = r.NextPageToken
			for _, run := range r.Data {
				pageRows = append(pageRows, parseRow(run))
			}
		case client.KindClassify:
			r, err := cli.ListClassifyRuns(ctx, opts)
			if err != nil {
				return nil, nil, err
			}
			page = r
			pageToken = r.NextPageToken
			for _, run := range r.Data {
				pageRows = append(pageRows, classifyRow(run))
			}
		case client.KindSplit:
			r, err := cli.ListSplitRuns(ctx, opts)
			if err != nil {
				return nil, nil, err
			}
			page = r
			pageToken = r.NextPageToken
			for _, run := range r.Data {
				pageRows = append(pageRows, splitRow(run))
			}
		case client.KindWorkflow:
			r, err := cli.ListWorkflowRuns(ctx, opts)
			if err != nil {
				return nil, nil, err
			}
			page = r
			pageToken = r.NextPageToken
			for _, run := range r.Data {
				pageRows = append(pageRows, workflowRow(run))
			}
		case client.KindEdit:
			return nil, nil, fmt.Errorf("listing edit runs is not supported by the API; use 'extend runs get edr_...' for individual edit runs")
		}
		rows = append(rows, pageRows...)
		rawPages = append(rawPages, page)
		if !all || pageToken == "" {
			break
		}
		opts.PageToken = pageToken
	}
	return rows, rawPages, nil
}

func extractRow(r *client.ExtractRun) []string {
	name := ""
	if r.Extractor != nil {
		name = r.Extractor.Name
	}
	return []string{r.ID, string(r.Status), name, relTime(r.CreatedAt)}
}

func parseRow(r *client.ParseRun) []string {
	return []string{r.ID, string(r.Status), "", relTime(r.CreatedAt)}
}

func classifyRow(r *client.ClassifyRun) []string {
	name := ""
	if r.Classifier != nil {
		name = r.Classifier.Name
	}
	return []string{r.ID, string(r.Status), name, relTime(r.CreatedAt)}
}

func splitRow(r *client.SplitRun) []string {
	name := ""
	if r.Splitter != nil {
		name = r.Splitter.Name
	}
	return []string{r.ID, string(r.Status), name, relTime(r.CreatedAt)}
}

func workflowRow(r *client.WorkflowRun) []string {
	name := ""
	if r.Workflow != nil {
		name = r.Workflow.Name
	}
	created := r.CreatedAt
	if created == "" {
		created = r.InitialRunAt
	}
	return []string{r.ID, string(r.Status), name, relTime(created)}
}

// editRow is currently unused: there is no LIST /edit_runs endpoint, so
// `extend runs list --type edit` errors out rather than calling this. Kept
// (with a no-op CreatedAt placeholder) so a future LIST endpoint can wire it
// back into collectListRows without re-deriving the formatting.
func editRow(r *client.EditRun) []string {
	name := ""
	if r.File != nil {
		name = r.File.Name
	}
	return []string{r.ID, string(r.Status), name, ""}
}

func relTime(iso string) string {
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
	if _, ok := client.RunKindFromID(id); !ok {
		return fmt.Errorf("cannot determine run type from id %q", id)
	}
	return deleteWithConfirm(ctx, app, "run", id, yes,
		func(ctx context.Context, id string) error {
			c, err := app.NewClient()
			if err != nil {
				return err
			}
			return c.DeleteRun(ctx, id)
		})
}

func runRunsCancel(ctx context.Context, app *App, id string, yes bool) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	if err := client.CanCancel(id); err != nil {
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

	if err := cli.CancelRun(ctx, id); err != nil {
		return err
	}
	fmt.Fprintf(app.IO.ErrOut, "%s Cancelled %s\n", paletteFor(app.IO).Green("✓"), id)
	return nil
}
