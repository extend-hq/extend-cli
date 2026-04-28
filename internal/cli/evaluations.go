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
		entity  string
		sortBy  string
		sortDir string
		limit   int
		all     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List evaluation sets",
		Example: `  extend evaluations list
  extend evaluations list --entity ex_abc --sort-by updatedAt
  extend evaluations list --all -o id`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := client.ListEvaluationSetsOptions{
				EntityID: entity,
				SortBy:   sortBy,
				SortDir:  sortDir,
				Limit:    limit,
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
			return renderList(app, pages, []string{"id", "name", "created"}, rows, "No evaluation sets.")
		},
	}
	cmd.Flags().StringVar(&entity, "entity", "", "Filter by extractor/classifier/splitter ID (ex_/cl_/spl_)")
	cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt)")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results per page")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate")
	return cmd
}

func newEvaluationsGetCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <evaluation-set-id>",
		Short: "Show one evaluation set",
		Args:  cobra.ExactArgs(1),
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
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON body (- for stdin)")
	cmd.Flags().StringVar(&name, "name", "", "Name (overrides body)")
	cmd.Flags().StringVar(&description, "description", "", "Description (overrides body)")
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
		sortBy  string
		sortDir string
		limit   int
		all     bool
	)
	cmd := &cobra.Command{
		Use:   "list <evaluation-set-id>",
		Short: "List items in an evaluation set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := client.ListProcessorsOptions{
				Limit:   limit,
				SortBy:  sortBy,
				SortDir: sortDir,
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
			return renderList(app, pages, []string{"id", "file"}, rows, "No items.")
		},
	}
	cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt)")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results per page")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate")
	return cmd
}

func newEvaluationItemsGetCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <evaluation-set-id> <item-id>",
		Short: "Show one evaluation item",
		Args:  cobra.ExactArgs(2),
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
	return cmd
}

func newEvaluationItemsCreateCommand(app *App) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "create <evaluation-set-id>",
		Short: "Add one or more items to an evaluation set (bulk create)",
		Long: `Add one or more items to an evaluation set in a single request. The body
must match the server's bulk schema: {"items":[{"fileId","expectedOutput"},...]}.
The response wraps the created items in {"evaluationSetItems":[...]}; this
command surfaces that envelope verbatim.`,
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
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON bulk body (- for stdin)")
	_ = cmd.MarkFlagRequired("from-file")
	return cmd
}

func newEvaluationItemsUpdateCommand(app *App) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "update <evaluation-set-id> <item-id>",
		Short: "Update an evaluation item",
		Args:  cobra.ExactArgs(2),
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
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON patch body (- for stdin)")
	_ = cmd.MarkFlagRequired("from-file")
	return cmd
}

func newEvaluationItemsDeleteCommand(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <evaluation-set-id> <item-id>",
		Short: "Delete an evaluation item",
		Args:  cobra.ExactArgs(2),
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
	return cmd
}

func newEvaluationRunsCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Inspect evaluation runs (read-only)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get <run-id>",
		Short: "Show one evaluation run",
		Long: `Show one evaluation run by ID. The server route is
/evaluation_set_runs/{run-id} (no eval-set ID needed in the path).`,
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
	})
	return cmd
}
