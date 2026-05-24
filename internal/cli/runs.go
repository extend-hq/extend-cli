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
		maxN      int
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
				max:       maxN,
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
			cmd.Flags().IntVar(&limit, "limit", 20, "Page size used in each API request (advanced)")
			cmd.Flags().IntVar(&maxN, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page, the default)")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&all, "all", false, "Fetch every page (use --max for a bounded fetch)")
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
	max       int
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

	rows, pages, err := collectListRows(ctx, cli, kind, p)
	if err != nil {
		return err
	}

	return renderListForCmd(cmd, app, pages, []string{"id", "status", "processor", "created"}, rows, "No runs.")
}

func parseRunKind(s string) (extendx.RunKind, error) {
	switch strings.ToLower(s) {
	case "extract":
		return extendx.KindExtract, nil
	case "parse":
		return extendx.KindParse, nil
	case "classify":
		return extendx.KindClassify, nil
	case "split":
		return extendx.KindSplit, nil
	case "workflow":
		return extendx.KindWorkflow, nil
	case "edit":
		return extendx.KindEdit, nil
	}
	return "", fmt.Errorf("unknown run type %q (want extract|parse|classify|split|workflow|edit)", s)
}

func collectListRows(ctx context.Context, cli *sdkclient.Client, kind extendx.RunKind, p runsListParams) ([][]string, []any, error) {
	var rows [][]string
	var rawPages []any
	pageToken := p.pageToken
	for {
		var (
			pageRows  [][]string
			page      any
			nextToken string
			err       error
		)
		switch kind {
		case extendx.KindExtract:
			pageRows, page, nextToken, err = listExtractPage(ctx, cli, p, pageToken)
		case extendx.KindParse:
			pageRows, page, nextToken, err = listParsePage(ctx, cli, p, pageToken)
		case extendx.KindClassify:
			pageRows, page, nextToken, err = listClassifyPage(ctx, cli, p, pageToken)
		case extendx.KindSplit:
			pageRows, page, nextToken, err = listSplitPage(ctx, cli, p, pageToken)
		case extendx.KindWorkflow:
			pageRows, page, nextToken, err = listWorkflowPage(ctx, cli, p, pageToken)
		case extendx.KindEdit:
			return nil, nil, fmt.Errorf("listing edit runs is not supported by the API; use 'extend runs get edr_...' for individual edit runs")
		}
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, pageRows...)
		rawPages = append(rawPages, page)
		if paginationDone(p.all, p.max, len(rows), nextToken) {
			break
		}
		pageToken = nextToken
	}
	rows = capRowsToMax(rows, p.max)
	return rows, rawPages, nil
}

// runsListCommon is the cross-kind subset of runsListParams parsed
// into the SDK's typed pointer fields. Each per-kind page function
// assigns these onto its specific SDK request struct (the request
// types share field names but not types, so the assignment can't be
// further DRY'd without reflection).
type runsListCommon struct {
	BatchID          *string
	SourceID         *extend.RunSourceID
	FileNameContains *string
	SortBy           *extend.SortBy
	SortDir          *extend.SortDir
	MaxPageSize      *extend.MaxPageSize
	NextPageToken    *extend.NextPageToken
}

// parseRunsListCommon parses the kind-independent slice of
// runsListParams. status, source, and the processor-ID field stay
// kind-local because their SDK types differ (extract/classify/split
// use ProcessorRunStatus + RunSource; parse has its own
// ParseRunsListRequestStatus + ParseRunSource; workflow has
// WorkflowRunStatus and no source field at all).
func parseRunsListCommon(p runsListParams, pageToken string) (*runsListCommon, error) {
	c := &runsListCommon{}
	if p.batchID != "" {
		c.BatchID = extend.String(p.batchID)
	}
	if p.sourceID != "" {
		sid := extend.RunSourceID(p.sourceID)
		c.SourceID = &sid
	}
	if p.fileName != "" {
		c.FileNameContains = extend.String(p.fileName)
	}
	if p.sortBy != "" {
		sb, err := extend.NewSortByFromString(p.sortBy)
		if err != nil {
			return nil, fmt.Errorf("--sort-by: %w", err)
		}
		c.SortBy = &sb
	}
	if p.sortDir != "" {
		sd, err := extend.NewSortDirFromString(p.sortDir)
		if err != nil {
			return nil, fmt.Errorf("--sort: %w", err)
		}
		c.SortDir = &sd
	}
	if p.limit > 0 {
		ps := extend.MaxPageSize(p.limit)
		c.MaxPageSize = &ps
	}
	if pageToken != "" {
		c.NextPageToken = extend.String(pageToken)
	}
	return c, nil
}

func listExtractPage(ctx context.Context, cli *sdkclient.Client, p runsListParams, pageToken string) ([][]string, any, string, error) {
	common, err := parseRunsListCommon(p, pageToken)
	if err != nil {
		return nil, nil, "", err
	}
	req := &extend.ExtractRunsListRequest{
		BatchID: common.BatchID, SourceID: common.SourceID,
		FileNameContains: common.FileNameContains,
		SortBy:           common.SortBy, SortDir: common.SortDir,
		MaxPageSize: common.MaxPageSize, NextPageToken: common.NextPageToken,
	}
	if p.status != "" {
		s := extend.ProcessorRunStatus(p.status)
		req.Status = &s
	}
	if p.using != "" {
		req.ExtractorID = extend.String(p.using)
	}
	if p.source != "" {
		s := extend.RunSource(p.source)
		req.Source = &s
	}
	resp, err := cli.ExtractRuns.List(ctx, req)
	if err != nil {
		return nil, nil, "", err
	}
	rows := make([][]string, 0, len(resp.Data))
	for _, r := range resp.Data {
		rows = append(rows, extractSummaryRow(r))
	}
	return rows, resp, extendx.Deref(resp.NextPageToken), nil
}

func listParsePage(ctx context.Context, cli *sdkclient.Client, p runsListParams, pageToken string) ([][]string, any, string, error) {
	// Parse runs ignore SortBy/SortDir at the server, but we still
	// parse them so an invalid string surfaces a friendly error
	// rather than being silently dropped.
	common, err := parseRunsListCommon(p, pageToken)
	if err != nil {
		return nil, nil, "", err
	}
	req := &extend.ParseRunsListRequest{
		BatchID: common.BatchID, SourceID: common.SourceID,
		FileNameContains: common.FileNameContains,
		MaxPageSize:      common.MaxPageSize, NextPageToken: common.NextPageToken,
	}
	if p.status != "" {
		s := extend.ParseRunsListRequestStatus(p.status)
		req.Status = &s
	}
	if p.source != "" {
		s := extend.ParseRunSource(p.source)
		req.Source = &s
	}
	resp, err := cli.ParseRuns.List(ctx, req)
	if err != nil {
		return nil, nil, "", err
	}
	rows := make([][]string, 0, len(resp.Data))
	for _, r := range resp.Data {
		rows = append(rows, parseRow(r))
	}
	return rows, resp, extendx.Deref(resp.NextPageToken), nil
}

func listClassifyPage(ctx context.Context, cli *sdkclient.Client, p runsListParams, pageToken string) ([][]string, any, string, error) {
	common, err := parseRunsListCommon(p, pageToken)
	if err != nil {
		return nil, nil, "", err
	}
	req := &extend.ClassifyRunsListRequest{
		BatchID: common.BatchID, SourceID: common.SourceID,
		FileNameContains: common.FileNameContains,
		SortBy:           common.SortBy, SortDir: common.SortDir,
		MaxPageSize: common.MaxPageSize, NextPageToken: common.NextPageToken,
	}
	if p.status != "" {
		s := extend.ProcessorRunStatus(p.status)
		req.Status = &s
	}
	if p.using != "" {
		req.ClassifierID = extend.String(p.using)
	}
	if p.source != "" {
		s := extend.RunSource(p.source)
		req.Source = &s
	}
	resp, err := cli.ClassifyRuns.List(ctx, req)
	if err != nil {
		return nil, nil, "", err
	}
	rows := make([][]string, 0, len(resp.Data))
	for _, r := range resp.Data {
		rows = append(rows, classifySummaryRow(r))
	}
	return rows, resp, extendx.Deref(resp.NextPageToken), nil
}

func listSplitPage(ctx context.Context, cli *sdkclient.Client, p runsListParams, pageToken string) ([][]string, any, string, error) {
	common, err := parseRunsListCommon(p, pageToken)
	if err != nil {
		return nil, nil, "", err
	}
	req := &extend.SplitRunsListRequest{
		BatchID: common.BatchID, SourceID: common.SourceID,
		FileNameContains: common.FileNameContains,
		SortBy:           common.SortBy, SortDir: common.SortDir,
		MaxPageSize: common.MaxPageSize, NextPageToken: common.NextPageToken,
	}
	if p.status != "" {
		s := extend.ProcessorRunStatus(p.status)
		req.Status = &s
	}
	if p.using != "" {
		req.SplitterID = extend.String(p.using)
	}
	if p.source != "" {
		s := extend.RunSource(p.source)
		req.Source = &s
	}
	resp, err := cli.SplitRuns.List(ctx, req)
	if err != nil {
		return nil, nil, "", err
	}
	rows := make([][]string, 0, len(resp.Data))
	for _, r := range resp.Data {
		rows = append(rows, splitSummaryRow(r))
	}
	return rows, resp, extendx.Deref(resp.NextPageToken), nil
}

func listWorkflowPage(ctx context.Context, cli *sdkclient.Client, p runsListParams, pageToken string) ([][]string, any, string, error) {
	// Workflow runs have no source/sourceId filters at the server;
	// common.SourceID is ignored here (silently — the CLI flag is
	// documented as ignored for --type workflow).
	common, err := parseRunsListCommon(p, pageToken)
	if err != nil {
		return nil, nil, "", err
	}
	req := &extend.WorkflowRunsListRequest{
		BatchID:          common.BatchID,
		FileNameContains: common.FileNameContains,
		SortBy:           common.SortBy, SortDir: common.SortDir,
		MaxPageSize: common.MaxPageSize, NextPageToken: common.NextPageToken,
	}
	if p.status != "" {
		s := extend.WorkflowRunStatus(p.status)
		req.Status = &s
	}
	if p.using != "" {
		req.WorkflowID = extend.String(p.using)
	}
	resp, err := cli.WorkflowRuns.List(ctx, req)
	if err != nil {
		return nil, nil, "", err
	}
	rows := make([][]string, 0, len(resp.Data))
	for _, r := range resp.Data {
		rows = append(rows, workflowSummaryRow(r))
	}
	return rows, resp, extendx.Deref(resp.NextPageToken), nil
}

func extractSummaryRow(r *extend.ExtractRunSummary) []string {
	name := ""
	if r.Extractor != nil {
		name = r.Extractor.Name
	}
	return []string{r.ID, string(r.Status), name, relTime(r.CreatedAt)}
}

func parseRow(r *extend.ParseRun) []string {
	// The SDK's *extend.ParseRun does not declare a CreatedAt
	// field — the server still emits one, but it lives in
	// extraProperties because Fern's spec doesn't model it. Pull
	// it out so the "created" column matches the other run kinds.
	// If/when the SDK adds CreatedAt as a typed field, swap this
	// back to a direct field read.
	created := ""
	if r != nil {
		if t, ok := r.GetExtraProperties()["createdAt"].(string); ok {
			created = relTimeFromISO(t)
		}
	}
	// Parse runs have no processor reference, so the "processor"
	// column is always empty (matches the old client).
	return []string{r.ID, string(r.Status), "", created}
}

func classifySummaryRow(r *extend.ClassifyRunSummary) []string {
	name := ""
	if r.Classifier != nil {
		name = r.Classifier.Name
	}
	return []string{r.ID, string(r.Status), name, relTime(r.CreatedAt)}
}

func splitSummaryRow(r *extend.SplitRunSummary) []string {
	name := ""
	if r.Splitter != nil {
		name = r.Splitter.Name
	}
	return []string{r.ID, string(r.Status), name, relTime(r.CreatedAt)}
}

func workflowSummaryRow(r *extend.WorkflowRunSummary) []string {
	name := ""
	if r.Workflow != nil {
		name = r.Workflow.Name
	}
	// WorkflowRunSummary has no CreatedAt; InitialRunAt is the
	// closest proxy and matches what the old hand-rolled client
	// rendered. It's *time.Time; relTime tolerates the zero value
	// so we can dereference unconditionally with a nil guard.
	var created time.Time
	if r.InitialRunAt != nil {
		created = *r.InitialRunAt
	}
	return []string{r.ID, string(r.Status), name, relTime(created)}
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
