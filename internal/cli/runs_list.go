package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// newRunsListDoc returns the typed documentation for `extend runs list`.
// Pulled out of runs.go because the listing path (per-kind page funcs,
// the cross-kind common-options parser, and the summary-row formatters)
// is half of the file by line count and stands alone from the rest of
// the runs subcommand surface.
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
	return rows, resp, deref(resp.NextPageToken), nil
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
	return rows, resp, deref(resp.NextPageToken), nil
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
	return rows, resp, deref(resp.NextPageToken), nil
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
	return rows, resp, deref(resp.NextPageToken), nil
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
	return rows, resp, deref(resp.NextPageToken), nil
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
