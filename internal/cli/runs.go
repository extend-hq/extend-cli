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

func newRunsCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Inspect and follow runs across all processor types",
		Long: `Operations on runs identified by their opaque ID. The run type is
auto-detected from the ID prefix (exr_, pr_, clr_, splr_).`,
	}
	cmd.AddCommand(newRunsGetCommand(app))
	cmd.AddCommand(newRunsWatchCommand(app))
	cmd.AddCommand(newRunsListCommand(app))
	cmd.AddCommand(newRunsCancelCommand(app))
	cmd.AddCommand(newRunsDeleteCommand(app))
	cmd.AddCommand(newRunsUpdateCommand(app))
	return cmd
}

func newRunsUpdateCommand(app *App) *cobra.Command {
	var (
		fromFile string
		meta     metaFlags
	)
	cmd := &cobra.Command{
		Use:   "update <workflow-run-id>",
		Short: "Update workflow run metadata (workflow runs only)",
		Long: `Update the metadata on an in-flight or completed workflow run. Only workflow
runs (workflow_run_...) support this; other run types do not.

Provide a JSON body with --from-file (overrides everything), or use --metadata
and --tag to set keys individually.`,
		Example: `  extend runs update workflow_run_abc --metadata customer=acme --tag prod
  extend runs update workflow_run_abc --from-file patch.json`,
		Args: cobra.ExactArgs(1),
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
				body, err = readBodyFile(fromFile)
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
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON patch body (- for stdin)")
	meta.attach(cmd)
	return cmd
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func newRunsGetCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <run-id>",
		Short: "Fetch a single run by ID",
		Example: `  extend runs get exr_xK9mLPq
  extend runs get pr_pJDa8iX -o yaml
  extend runs get clr_kMXk --jq '.output.confidence' -o raw`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsGet(cmd.Context(), app, args[0])
		},
	}
	return cmd
}

func newRunsWatchCommand(app *App) *cobra.Command {
	var (
		timeout    time.Duration
		exitStatus bool
	)
	cmd := &cobra.Command{
		Use:   "watch <run-id>",
		Short: "Poll a run until it reaches a terminal state",
		Long: `Block until the run reaches a terminal state, showing a spinner
with status transitions. The final result is rendered using the same per-type
natural format as the originating command.

Use --exit-status for shell composition: the command exits non-zero if the
run terminates in FAILED or CANCELLED state, so:

    extend runs watch <id> --exit-status && downstream-script.sh

works as expected.`,
		Example: `  extend runs watch exr_xK9mLPq
  extend runs watch pr_pJDa8iX --timeout 5m
  extend runs watch clr_kMXk --exit-status && deploy.sh`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsWatch(cmd.Context(), app, args[0], timeout, exitStatus)
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum time to wait")
	cmd.Flags().BoolVar(&exitStatus, "exit-status", false, "Exit non-zero on FAILED or CANCELLED")
	return cmd
}

func runRunsGet(ctx context.Context, app *App, id string) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	kind, ok := client.RunKindFromID(id)
	if !ok {
		return fmt.Errorf("cannot determine run type from id %q (expected exr_/pr_/clr_/splr_ prefix)", id)
	}
	switch kind {
	case client.KindExtract:
		run, err := cli.GetExtractRun(ctx, id)
		if err != nil {
			return err
		}
		return renderWithDefault(app, run, output.FormatJSON)
	case client.KindParse:
		run, err := cli.GetParseRun(ctx, id)
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
		return fmt.Errorf("cannot determine run type from id %q (expected exr_/pr_/clr_/splr_ prefix)", id)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Watching %s...", id))
	opts := client.WaitOptions{
		Interval:    1 * time.Second,
		MaxInterval: 10 * time.Second,
		Timeout:     timeout,
	}

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

func newRunsListCommand(app *App) *cobra.Command {
	var (
		runType     string
		status      string
		using       string
		batchID     string
		source      string
		sourceID    string
		fileName    string
		limit       int
		all         bool
		sortBy      string
		sortDir     string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List runs of a given processor type",
		Long: `List runs for a given processor type. Most filter flags map directly to
documented query parameters on the run-list endpoints; the wire shape varies
slightly by type (e.g. parse runs ignore --using, --sort-by, and --sort;
workflow runs ignore --source and --source-id).`,
		Example: `  extend runs list --type extract
  extend runs list --type extract --using ex_abc --status PROCESSED
  extend runs list --type workflow --using workflow_abc --file-name invoice
  extend runs list --type extract --source WORKFLOW_RUN --source-id workflow_run_x
  extend runs list --type extract --batch bpr_xK9mLPq --all
  extend runs list --type extract --sort-by updatedAt --sort asc`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsList(cmd.Context(), app, runsListParams{
				runType:  runType,
				status:   status,
				using:    using,
				batchID:  batchID,
				source:   source,
				sourceID: sourceID,
				fileName: fileName,
				limit:    limit,
				all:      all,
				sortBy:   sortBy,
				sortDir:  sortDir,
			})
		},
	}
	cmd.Flags().StringVar(&runType, "type", "", "Run type: extract|parse|classify|split|workflow (required)")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status: PENDING|PROCESSING|PROCESSED|FAILED|CANCELLED")
	cmd.Flags().StringVar(&using, "using", "", "Filter by processor ID (ex_/cl_/spl_/workflow_; ignored for parse)")
	cmd.Flags().StringVar(&batchID, "batch", "", "Filter by batch run ID (bpr_... or bpar_...)")
	cmd.Flags().StringVar(&source, "source", "", "Filter by run source: API|STUDIO|WORKFLOW_RUN|ADMIN|... (ignored for workflow)")
	cmd.Flags().StringVar(&sourceID, "source-id", "", "Filter by source resource ID, e.g. workflow_run_xxx (ignored for workflow)")
	cmd.Flags().StringVar(&fileName, "file-name", "", "Filter to runs whose file name contains this substring")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum runs to return per page")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate to fetch every run matching filters")
	cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt; ignored for parse)")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc (ignored for parse)")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

type runsListParams struct {
	runType  string
	status   string
	using    string
	batchID  string
	source   string
	sourceID string
	fileName string
	limit    int
	all      bool
	sortBy   string
	sortDir  string
}

func runRunsList(ctx context.Context, app *App, p runsListParams) error {
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
		SortBy:           p.sortBy,
		SortDir:          p.sortDir,
	}

	rows, pages, err := collectListRows(ctx, cli, kind, opts, p.all)
	if err != nil {
		return err
	}

	return renderList(app, pages, []string{"id", "status", "processor", "created"}, rows, "No runs.")
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

func newRunsCancelCommand(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel a run by ID",
		Long: `Cancel a non-terminal run by ID. The run type is determined from
the ID prefix. Parse runs cannot be cancelled.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsCancel(cmd.Context(), app, args[0], yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func newRunsDeleteCommand(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <run-id>",
		Short: "Delete a run record (any run type)",
		Long: `Delete a run record by ID. Cancel stops a running operation; delete
removes the historical record once it has reached a terminal state.

The run type is auto-detected from the ID prefix (exr_/pr_/clr_/splr_/edr_/workflow_run_).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsDelete(cmd.Context(), app, args[0], yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
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
	fmt.Fprintf(app.IO.Out, "%s Cancelled %s\n", paletteFor(app.IO).Green("✓"), id)
	return nil
}
