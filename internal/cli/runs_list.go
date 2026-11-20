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

// listDoc returns the typed documentation for `extend <verb> runs list`.
// Lives in runs_list.go with the per-kind page funcs, the cross-kind
// common-options parser, and the summary-row formatters that implement
// it. Only listable kinds attach this leaf (detect-form runs have no
// list endpoint), and each kind registers exactly the filter flags its
// endpoint supports.
func (s runsGroupSpec) listDoc(app *App) *CommandDoc {
	var (
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
		riskLevel string
	)
	batchExample := "bpr_xK9mLPq"
	if s.kind == extendx.KindParse {
		batchExample = "bpar_xK9mLPq"
	} else if s.kind == extendx.KindWorkflow {
		batchExample = "batch_xK9mLPq"
	}
	examples := []Example{
		{Label: "First page", Cmd: fmt.Sprintf("extend %s", s.path("runs list"))},
		{Label: "Filter by status", Cmd: fmt.Sprintf("extend %s --status PROCESSED", s.path("runs list"))},
	}
	if !s.noBatches {
		examples = append(examples, Example{Label: "Runs in a batch", Cmd: fmt.Sprintf("extend %s --batch %s", s.path("runs list"), batchExample)})
	}
	examples = append(examples, Example{Label: "Next page", Cmd: fmt.Sprintf("extend %s --page-token <token-from-previous-response>", s.path("runs list"))})
	if s.usingFlag != "" {
		examples = append(examples, Example{
			Label: "Filter by " + s.usingFlag,
			Cmd:   fmt.Sprintf("extend %s --using %s", s.path("runs list"), s.usingExample),
		})
	}
	if s.sourceFilters && s.listSources == "" {
		examples = append(examples, Example{
			Label: "Runs spawned by a workflow",
			Cmd:   fmt.Sprintf("extend %s --source WORKFLOW_RUN --source-id workflow_run_x", s.path("runs list")),
		})
	}
	if s.riskLevels {
		examples = append(examples, Example{
			Label: "High-risk results",
			Cmd:   fmt.Sprintf("extend %s --risk-level HIGH", s.path("runs list")),
		})
	}
	if s.sortable {
		examples = append(examples, Example{
			Label: "Custom sort",
			Cmd:   fmt.Sprintf("extend %s --sort-by updatedAt --sort asc", s.path("runs list")),
		})
	}
	triggers := []string{
		fmt.Sprintf("list %s runs in the workspace", s.name()),
		fmt.Sprintf("page through %s runs", s.name()),
		fmt.Sprintf("filter %s runs by status", s.name()),
	}
	filters := "status, batch, and file\nname"
	if s.noBatches {
		triggers = append(triggers, fmt.Sprintf("find recent %s runs for a file", s.name()))
		filters = "status, source, and file\nname"
		if s.riskLevels {
			filters = "status, risk level, source,\nand file name"
		}
	} else {
		triggers = append(triggers, fmt.Sprintf("see %s runs in a batch", s.name()))
	}
	return &CommandDoc{
		Use:       "list",
		Summary:   fmt.Sprintf("List %s runs with filters", s.name()),
		Triggers:  triggers,
		WhenToUse: fmt.Sprintf(`Use to enumerate %s runs with filtering by %s. The ID column feeds 'extend %s <id>'.`, s.name(), filters, s.path("runs get")),
		Details: `Filter flags map directly to documented query parameters on the
run-list endpoint.

` + paginationGuidance,
		Examples: examples,
		Gotchas:  s.listGotchas,
		SeeAlso:  s.seeAlso("list"),
		Output:   OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			rows, pages, err := collectListRows(cmd.Context(), cli, s.kind, runsListParams{
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
				riskLevel: riskLevel,
			})
			if err != nil {
				return err
			}
			column := s.listColumn
			if column == "" {
				column = "processor"
			}
			headers := []string{"id", "status", column, "created"}
			if s.riskLevels {
				headers = []string{"id", "status", "risk", column, "created"}
			}
			return renderListForCmd(cmd, app, pages, headers, rows, "No runs.")
		},
		Configure: func(cmd *cobra.Command) {
			statusHelp := "Filter by status: PENDING|PROCESSING|PROCESSED|FAILED|CANCELLED"
			switch {
			case s.listStatuses != "":
				statusHelp = "Filter by status: " + s.listStatuses
			case s.kind == extendx.KindParse:
				statusHelp = "Filter by status: PENDING|PROCESSING|PROCESSED|FAILED"
			case s.kind == extendx.KindWorkflow:
				statusHelp = "Filter by status: PENDING|PROCESSING|PROCESSED|FAILED|CANCELLED|NEEDS_REVIEW|REJECTED|CANCELLING"
			}
			cmd.Flags().StringVar(&status, "status", "", statusHelp)
			if s.usingFlag != "" {
				cmd.Flags().StringVar(&using, "using", "", fmt.Sprintf("Filter by %s ID (%s...)", s.usingFlag, s.usingExample[:strings.Index(s.usingExample, "_")+1]))
			}
			if !s.noBatches {
				batchHelp := "Filter by batch run ID (" + batchExample[:strings.Index(batchExample, "_")+1] + "...)"
				cmd.Flags().StringVar(&batchID, "batch", "", batchHelp)
			}
			if s.sourceFilters || s.listSources != "" {
				sources := s.listSources
				if sources == "" {
					sources = "API|STUDIO|WORKFLOW_RUN|ADMIN|..."
				}
				cmd.Flags().StringVar(&source, "source", "", "Filter by run source: "+sources)
			}
			if s.sourceFilters {
				sourceIDHelp := "Filter by source resource ID, e.g. workflow_run_xxx"
				if s.listSources != "" {
					sourceIDHelp = "Filter by source ID: for API runs, the ID of the request that created the run"
				}
				cmd.Flags().StringVar(&sourceID, "source-id", "", sourceIDHelp)
			}
			if s.riskLevels {
				cmd.Flags().StringVar(&riskLevel, "risk-level", "", "Filter to processed runs with this risk level: LOW|MEDIUM|HIGH")
			}
			cmd.Flags().StringVar(&fileName, "file-name", "", "Filter to runs whose file name contains this substring")
			cmd.Flags().IntVar(&limit, "limit", 20, "Page size used in each API request (advanced)")
			cmd.Flags().IntVar(&maxN, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page, the default)")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&all, "all", false, "Fetch every page (use --max for a bounded fetch)")
			if s.sortable {
				cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt)")
				cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
			}
		},
	}
}

type runsListParams struct {
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
	riskLevel string
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
			pageRows, page, nextToken, err = listEditPage(ctx, cli, p, pageToken)
		case extendx.KindVerify:
			pageRows, page, nextToken, err = listVerifyPage(ctx, cli, p, pageToken)
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
	// the workflows list command doesn't register those flags, so
	// common.SourceID is always nil here.
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

func listEditPage(ctx context.Context, cli *sdkclient.Client, p runsListParams, pageToken string) ([][]string, any, string, error) {
	common, err := parseRunsListCommon(p, pageToken)
	if err != nil {
		return nil, nil, "", err
	}
	req := &extend.EditRunsListRequest{
		FileNameContains: common.FileNameContains,
		SortBy:           common.SortBy, SortDir: common.SortDir,
		MaxPageSize: common.MaxPageSize, NextPageToken: common.NextPageToken,
	}
	if p.status != "" {
		s := extend.EditRunsListRequestStatus(p.status)
		req.Status = &s
	}
	if p.source != "" {
		s := extend.EditRunSource(p.source)
		req.Source = &s
	}
	resp, err := cli.EditRuns.List(ctx, req)
	if err != nil {
		return nil, nil, "", err
	}
	rows := make([][]string, 0, len(resp.Data))
	for _, r := range resp.Data {
		name := ""
		if r.File != nil {
			name = r.File.Name
		}
		rows = append(rows, []string{r.ID, string(r.Status), name, relTime(r.CreatedAt)})
	}
	return rows, resp, deref(resp.NextPageToken), nil
}

func listVerifyPage(ctx context.Context, cli *sdkclient.Client, p runsListParams, pageToken string) ([][]string, any, string, error) {
	common, err := parseRunsListCommon(p, pageToken)
	if err != nil {
		return nil, nil, "", err
	}
	req := &extend.VerifyRunsListRequest{
		FileNameContains: common.FileNameContains,
		SortBy:           common.SortBy, SortDir: common.SortDir,
		MaxPageSize: common.MaxPageSize, NextPageToken: common.NextPageToken,
	}
	if p.status != "" {
		s := extend.VerifyRunStatus(p.status)
		req.Status = &s
	}
	if p.source != "" {
		s := extend.VerifyRunsListRequestSource(p.source)
		req.Source = &s
	}
	if p.sourceID != "" {
		req.SourceID = extend.String(p.sourceID)
	}
	if p.riskLevel != "" {
		r := extend.VerifyRiskLevel(strings.ToUpper(p.riskLevel))
		req.RiskLevel = &r
	}
	resp, err := cli.VerifyRuns.List(ctx, req)
	if err != nil {
		return nil, nil, "", verifyNotEnabled(err)
	}
	rows := make([][]string, 0, len(resp.Data))
	for _, r := range resp.Data {
		risk, name := "", ""
		if r.RiskLevel != nil {
			risk = string(*r.RiskLevel)
		}
		if r.File != nil {
			name = r.File.Name
		}
		rows = append(rows, []string{r.ID, string(r.Status), risk, name, relTime(r.CreatedAt)})
	}
	return rows, resp, deref(resp.NextPageToken), nil
}

func parseRow(r *extend.ParseRunSummary) []string {
	name := ""
	if r.File != nil {
		name = r.File.Name
	}
	return []string{r.ID, string(r.Status), name, relTime(r.CreatedAt)}
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
