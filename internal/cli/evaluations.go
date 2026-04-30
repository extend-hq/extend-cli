package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

func newEvaluationsCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "evaluations",
		Aliases: []string{"evals"},
		Short:   "Manage evaluation sets and items",
	}
	cmd.AddCommand(newEvaluationsListCommand(app))
	cmd.AddCommand(newEvaluationsGetCommand(app))
	cmd.AddCommand(newEvaluationsCreateCommand(app))
	cmd.AddCommand(newEvaluationItemsCommand(app))
	cmd.AddCommand(newEvaluationRunsCommand(app))
	return cmd
}

func newEvaluationsListCommand(app *App) *cobra.Command {
	var (
		entity    string
		sortBy    string
		sortDir   string
		limit     int
		all       bool
		pageToken string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List evaluation sets",
		Long: `List evaluation sets in the current workspace.

Filter to those scoped to a specific extractor, classifier, or splitter
with --entity. Evaluation sets contain ground-truth items used to measure
processor accuracy via 'extend evaluations runs get'.

` + paginationGuidance,
		Example: `  extend evaluations list
  extend evaluations list --entity ex_abc --sort-by updatedAt
  extend evaluations list --page-token <token-from-previous-response>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := client.ListEvaluationSetsOptions{
				EntityID:  entity,
				SortBy:    sortBy,
				SortDir:   sortDir,
				Limit:     limit,
				PageToken: pageToken,
			}
			var rows [][]string
			var pages []any
			for {
				page, err := cli.ListEvaluationSets(cmd.Context(), opts)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				for _, s := range page.Data {
					rows = append(rows, []string{s.ID, s.Name, relTime(s.CreatedAt)})
				}
				if !all || page.NextPageToken == "" {
					break
				}
				opts.PageToken = page.NextPageToken
			}
			return renderListForCmd(cmd, app, pages, []string{"id", "name", "created"}, rows, "No evaluation sets.")
		},
	}
	cmd.Flags().StringVar(&entity, "entity", "", "Filter by extractor/classifier/splitter ID (ex_/cl_/spl_)")
	cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt)")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results per page")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "Fetch a specific page (token from a previous response's nextPageToken)")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate every page into one response (avoid for agent use; prefer --page-token)")
	SetIOAnnotations(cmd, OutputTable, OutputJSON)
	return cmd
}

func newEvaluationsGetCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <evaluation-set-id>",
		Short: "Show one evaluation set",
		Long: `Show metadata for one evaluation set: its name, description, and the
processor it is scoped to. Use 'extend evaluations items list <id>' to see
the items it contains.`,
		Example: `  extend evaluations get evs_abc`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.GetEvaluationSet(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
	}
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}

func newEvaluationsCreateCommand(app *App) *cobra.Command {
	var (
		fromFile    string
		name        string
		description string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an evaluation set",
		Long: `Create an evaluation set scoped to one extractor, classifier, or
splitter. The set is created empty; add ground-truth items afterward with
'extend evaluations items create <set-id>'.

Pass --from-file with the API body (inline JSON, path, file:// URI, or -
for stdin); --name and --description override their counterparts in the
body.`,
		Example: `  extend evaluations create --name "Q3 invoices" --from-file body.json
  extend evaluations create --from-file '{"name":"smoke","entityId":"ex_abc"}'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, map[string]string{"name": name, "description": description})
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.CreateEvaluationSet(cmd.Context(), body)
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON body, path, file:// URI, or '-' for stdin")
	cmd.Flags().StringVar(&name, "name", "", "Name (overrides body)")
	cmd.Flags().StringVar(&description, "description", "", "Description (overrides body)")
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}

func newEvaluationItemsCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "items",
		Short: "Manage items inside an evaluation set",
	}
	cmd.AddCommand(newEvaluationItemsListCommand(app))
	cmd.AddCommand(newEvaluationItemsGetCommand(app))
	cmd.AddCommand(newEvaluationItemsCreateCommand(app))
	cmd.AddCommand(newEvaluationItemsUpdateCommand(app))
	cmd.AddCommand(newEvaluationItemsDeleteCommand(app))
	return cmd
}

func newEvaluationItemsListCommand(app *App) *cobra.Command {
	var (
		sortBy    string
		sortDir   string
		limit     int
		all       bool
		pageToken string
	)
	cmd := &cobra.Command{
		Use:   "list <evaluation-set-id>",
		Short: "List items in an evaluation set",
		Long: `List the ground-truth items in an evaluation set. Each item pairs a
file with its expected output; the set runs every item against a processor
version to produce an accuracy score.

` + paginationGuidance,
		Example: `  extend evaluations items list evs_abc
  extend evaluations items list evs_abc --page-token <token-from-previous-response>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := client.ListProcessorsOptions{
				Limit:     limit,
				SortBy:    sortBy,
				SortDir:   sortDir,
				PageToken: pageToken,
			}
			var rows [][]string
			var pages []any
			for {
				page, err := cli.ListEvaluationItems(cmd.Context(), args[0], opts)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				for _, it := range page.Data {
					name := ""
					if it.File != nil {
						name = it.File.Name
					}
					rows = append(rows, []string{it.ID, name})
				}
				if !all || page.NextPageToken == "" {
					break
				}
				opts.PageToken = page.NextPageToken
			}
			return renderListForCmd(cmd, app, pages, []string{"id", "file"}, rows, "No items.")
		},
	}
	cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt)")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results per page")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "Fetch a specific page (token from a previous response's nextPageToken)")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate every page into one response (avoid for agent use; prefer --page-token)")
	SetIOAnnotations(cmd, OutputTable, OutputJSON)
	return cmd
}

func newEvaluationItemsGetCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <evaluation-set-id> <item-id>",
		Short: "Show one evaluation item",
		Long: `Show one item in an evaluation set: its file reference and expected
output (the ground-truth that processor runs are scored against).`,
		Example: `  extend evaluations items get evs_abc esi_xyz`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			it, err := cli.GetEvaluationItem(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return renderWithDefault(app, it, output.FormatJSON)
		},
	}
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}

func newEvaluationItemsCreateCommand(app *App) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "create <evaluation-set-id>",
		Short: "Add one or more items to an evaluation set (bulk create)",
		Long: `Add one or more items to an evaluation set in a single request.

The body must match the server's bulk schema:

    {"items":[{"fileId":"file_xxx","expectedOutput":{...}}, ...]}

--from-file accepts inline JSON, a plain path, an absolute file:// URI, or
- for stdin. The response wraps the created items in
{"evaluationSetItems":[...]}; this command surfaces that envelope verbatim.`,
		Example: `  extend evaluations items create evs_abc --from-file items.json
  cat items.json | extend evaluations items create evs_abc --from-file -`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, nil)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			resp, err := cli.CreateEvaluationItems(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			return renderWithDefault(app, resp, output.FormatJSON)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON bulk body, path, file:// URI, or '-' for stdin")
	_ = cmd.MarkFlagRequired("from-file")
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}

func newEvaluationItemsUpdateCommand(app *App) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "update <evaluation-set-id> <item-id>",
		Short: "Update an evaluation item",
		Long: `Update one item in an evaluation set, typically to change the expected
output as the ground truth evolves. --from-file accepts inline JSON, a
plain path, an absolute file:// URI, or - for stdin.`,
		Example: `  extend evaluations items update evs_abc esi_xyz --from-file patch.json`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, nil)
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			it, err := cli.UpdateEvaluationItem(cmd.Context(), args[0], args[1], body)
			if err != nil {
				return err
			}
			return renderWithDefault(app, it, output.FormatJSON)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON patch body, path, file:// URI, or '-' for stdin")
	_ = cmd.MarkFlagRequired("from-file")
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}

func newEvaluationItemsDeleteCommand(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <evaluation-set-id> <item-id>",
		Short: "Delete an evaluation item",
		Long: `Delete one item from an evaluation set. The set is left in place;
only that ground-truth pair is removed.

Prompts for confirmation when stdin is a TTY; pass --yes to skip the
prompt (required in non-interactive scripts).`,
		Example: `  extend evaluations items delete evs_abc esi_xyz
  extend evaluations items delete evs_abc esi_xyz --yes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			setID, itemID := args[0], args[1]
			return deleteWithConfirm(cmd.Context(), app, "evaluation item", itemID, yes,
				func(ctx context.Context, _ string) error {
					c, err := app.NewClient()
					if err != nil {
						return err
					}
					return c.DeleteEvaluationItem(ctx, setID, itemID)
				})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	SetIOAnnotations(cmd, OutputNone, OutputNone)
	return cmd
}

func newEvaluationRunsCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Inspect evaluation runs (read-only)",
	}
	getRunCmd := &cobra.Command{
		Use:   "get <run-id>",
		Short: "Show one evaluation run",
		Long: `Show one evaluation run by ID. The server route is
/evaluation_set_runs/{run-id} (no eval-set ID needed in the path).

Evaluation runs are read-only here; create them via the dashboard. This
command surfaces the per-item results, accuracy metrics, and any diffs
the run produced.`,
		Example: `  extend evaluations runs get esr_abc
  extend evaluations runs get esr_abc --jq '.accuracy' -o raw`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			run, err := cli.GetEvaluationRun(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderWithDefault(app, run, output.FormatJSON)
		},
	}
	SetIOAnnotations(getRunCmd, OutputJSON, OutputJSON)
	cmd.AddCommand(getRunCmd)
	return cmd
}
