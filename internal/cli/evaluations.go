package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"

	"github.com/extend-hq/extend-cli/internal/output"
)

// newEvaluationsDoc returns the typed documentation for the
// `extend evaluations` group and all 9 leaves under it (list/get/create
// at the top level; the items subgroup with list/get/create/update/delete;
// the runs subgroup with get).
func newEvaluationsDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "evaluations",
		Aliases: []string{"evals"},
		Summary: "Manage evaluation sets and items",
		Group:   "Resources",
		WhenToUse: `Use these commands to manage evaluation sets (named bundles of
ground-truth items used to measure processor accuracy) and the items
inside them. Evaluation runs themselves are created via the dashboard;
this CLI surfaces them read-only via 'extend evaluations runs get'.`,
		Details: `An evaluation set is scoped to one extractor, classifier, or splitter.
Each item in the set pairs a file with its expected output. Running the
set against a processor version produces an evaluation run with per-field
accuracy/precision/recall metrics.`,
		Subcommands: []*CommandDoc{
			newEvaluationsListDoc(app),
			newEvaluationsGetDoc(app),
			newEvaluationsCreateDoc(app),
			newEvaluationItemsDoc(app),
			newEvaluationRunsDoc(app),
		},
	}
}

func newEvaluationsListDoc(app *App) *CommandDoc {
	var (
		entity    string
		sortBy    string
		sortDir   string
		limit     int
		maxN      int
		all       bool
		pageToken string
	)
	return &CommandDoc{
		Use:     "list",
		Summary: "List evaluation sets",
		Triggers: []string{
			"list evaluation sets in the workspace",
			"find ground-truth bundles for an extractor",
			"page through eval sets",
			"discover evaluation set ids",
		},
		WhenToUse: `Use to discover evs_ IDs of evaluation sets, optionally filtered by
the processor they're scoped to.`,
		Details: `Filter to those scoped to a specific extractor, classifier, or splitter
with --entity. Evaluation sets contain ground-truth items used to measure
processor accuracy via 'extend evaluations runs get'.

` + paginationGuidance,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend evaluations list"},
			{Label: "Scoped to one processor", Cmd: "extend evaluations list --entity ex_abc --sort-by updatedAt"},
			{Label: "Next page", Cmd: "extend evaluations list --page-token <token-from-previous-response>"},
		},
		SeeAlso: []string{"evaluations get", "evaluations create"},
		Output:  OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			req := &extend.EvaluationSetsListRequest{}
			if entity != "" {
				req.EntityID = extend.String(entity)
			}
			if sortBy != "" {
				sb, err := extend.NewSortByFromString(sortBy)
				if err != nil {
					return fmt.Errorf("--sort-by: %w", err)
				}
				req.SortBy = &sb
			}
			if sortDir != "" {
				sd, err := extend.NewSortDirFromString(sortDir)
				if err != nil {
					return fmt.Errorf("--sort: %w", err)
				}
				req.SortDir = &sd
			}
			if limit > 0 {
				ps := extend.MaxPageSize(limit)
				req.MaxPageSize = &ps
			}
			if pageToken != "" {
				req.NextPageToken = extend.String(pageToken)
			}

			var rows [][]string
			var pages []any
			for {
				page, err := cli.EvaluationSets.List(cmd.Context(), req)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				for _, s := range page.Data {
					rows = append(rows, []string{s.ID, s.Name, relTime(s.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))})
				}
				next := derefString(page.NextPageToken)
				if paginationDone(all, maxN, len(rows), next) {
					break
				}
				req.NextPageToken = extend.String(next)
			}
			rows = capRowsToMax(rows, maxN)
			return renderListForCmd(cmd, app, pages, []string{"id", "name", "created"}, rows, "No evaluation sets.")
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&entity, "entity", "", "Filter by extractor/classifier/splitter ID (ex_/cl_/spl_)")
			cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt)")
			cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
			cmd.Flags().IntVar(&limit, "limit", 20, "Page size used in each API request (advanced)")
			cmd.Flags().IntVar(&maxN, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page)")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&all, "all", false, "Fetch every page (use --max for a bounded fetch)")
		},
	}
}

func newEvaluationsGetDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "get <evaluation-set-id>",
		Summary: "Show one evaluation set",
		Triggers: []string{
			"show metadata for an evaluation set",
			"inspect a single eval set",
			"get the entity scope of an evaluation set",
		},
		WhenToUse: `Use to retrieve metadata for one evaluation set: name, description,
and the processor it is scoped to. Use 'extend evaluations items list <id>'
to see the items it contains.`,
		Details: `Returns the full evaluation set object as JSON.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend evaluations get evs_abc"},
		},
		SeeAlso: []string{"evaluations list", "evaluations items list", "evaluations runs get"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.EvaluationSets.Retrieve(cmd.Context(), args[0], &extend.EvaluationSetsRetrieveRequest{})
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
	}
}

func newEvaluationsCreateDoc(app *App) *CommandDoc {
	var (
		fromFile    string
		name        string
		description string
	)
	return &CommandDoc{
		Use:     "create",
		Summary: "Create an evaluation set",
		Triggers: []string{
			"create a new evaluation set",
			"set up an eval bundle for an extractor",
			"register a ground-truth set",
		},
		WhenToUse: `Use to create an evaluation set scoped to one extractor, classifier,
or splitter. The set is created empty; add ground-truth items afterward
with 'extend evaluations items create <set-id>'.`,
		Details: `Pass --from-file with the API body (inline JSON, path, file:// URI, or -
for stdin); --name and --description override their counterparts in the
body.`,
		Examples: []Example{
			{Label: "From file", Cmd: `extend evaluations create --name "Q3 invoices" --from-file body.json`},
			{Label: "Inline body", Cmd: `extend evaluations create --from-file '{"name":"smoke","entityId":"ex_abc"}'`},
		},
		Gotchas: []string{
			"--name/--description on the CLI override values inside the JSON body.",
			"The set is created empty; add items via 'extend evaluations items create' afterward.",
		},
		SeeAlso: []string{"evaluations list", "evaluations items create"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, map[string]string{"name": name, "description": description})
			if err != nil {
				return err
			}
			var req extend.EvaluationSetsCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return fmt.Errorf("decode body: %w", err)
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			s, err := cli.EvaluationSets.Create(cmd.Context(), &req)
			if err != nil {
				return err
			}
			return renderWithDefault(app, s, output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON body, path, file:// URI, or '-' for stdin")
			cmd.Flags().StringVar(&name, "name", "", "Name (overrides body)")
			cmd.Flags().StringVar(&description, "description", "", "Description (overrides body)")
		},
	}
}

func newEvaluationItemsDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "items",
		Summary: "Manage items inside an evaluation set",
		WhenToUse: `Use this group to list, inspect, create, update, or delete the items
inside an evaluation set. Items pair files with their expected outputs.`,
		Details: `Each item is a {file, expectedOutput} pair. Items are scored by
evaluation runs against a processor version; their expected outputs are
the ground truth.`,
		Subcommands: []*CommandDoc{
			newEvaluationItemsListDoc(app),
			newEvaluationItemsGetDoc(app),
			newEvaluationItemsCreateDoc(app),
			newEvaluationItemsUpdateDoc(app),
			newEvaluationItemsDeleteDoc(app),
		},
	}
}

func newEvaluationItemsListDoc(app *App) *CommandDoc {
	var (
		sortBy    string
		sortDir   string
		limit     int
		maxN      int
		all       bool
		pageToken string
	)
	return &CommandDoc{
		Use:     "list <evaluation-set-id>",
		Summary: "List items in an evaluation set",
		Triggers: []string{
			"list items in an evaluation set",
			"page through eval set ground-truth items",
			"see what files an eval set contains",
		},
		WhenToUse: `Use to enumerate the ground-truth items in an evaluation set: each item
pairs a file with its expected output.`,
		Details: `The set runs every item against a processor version to produce an
accuracy score.

` + paginationGuidance,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend evaluations items list evs_abc"},
			{Label: "Next page", Cmd: "extend evaluations items list evs_abc --page-token <token-from-previous-response>"},
		},
		SeeAlso: []string{"evaluations get", "evaluations items get", "evaluations items create"},
		Output:  OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			req := &extend.EvaluationSetItemsListRequest{}
			if sortBy != "" {
				sb, err := extend.NewSortByFromString(sortBy)
				if err != nil {
					return fmt.Errorf("--sort-by: %w", err)
				}
				req.SortBy = &sb
			}
			if sortDir != "" {
				sd, err := extend.NewSortDirFromString(sortDir)
				if err != nil {
					return fmt.Errorf("--sort: %w", err)
				}
				req.SortDir = &sd
			}
			if limit > 0 {
				ps := extend.MaxPageSize(limit)
				req.MaxPageSize = &ps
			}
			if pageToken != "" {
				req.NextPageToken = extend.String(pageToken)
			}

			var rows [][]string
			var pages []any
			for {
				page, err := cli.EvaluationSetItems.List(cmd.Context(), args[0], req)
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
				next := derefString(page.NextPageToken)
				if paginationDone(all, maxN, len(rows), next) {
					break
				}
				req.NextPageToken = extend.String(next)
			}
			rows = capRowsToMax(rows, maxN)
			return renderListForCmd(cmd, app, pages, []string{"id", "file"}, rows, "No items.")
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt)")
			cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
			cmd.Flags().IntVar(&limit, "limit", 20, "Page size used in each API request (advanced)")
			cmd.Flags().IntVar(&maxN, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page)")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&all, "all", false, "Fetch every page (use --max for a bounded fetch)")
		},
	}
}

func newEvaluationItemsGetDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "get <evaluation-set-id> <item-id>",
		Summary: "Show one evaluation item",
		Triggers: []string{
			"show one evaluation item",
			"inspect ground-truth for a single eval item",
			"see the expected output of an eval item",
		},
		WhenToUse: `Use to retrieve a single eval-set item: its file reference and expected
output (the ground-truth that processor runs are scored against).`,
		Details: `Returns the full evaluation item object as JSON.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend evaluations items get evs_abc esi_xyz"},
		},
		SeeAlso: []string{"evaluations items list", "evaluations items update", "evaluations items delete"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			it, err := cli.EvaluationSetItems.Retrieve(cmd.Context(), args[0], args[1], &extend.EvaluationSetItemsRetrieveRequest{})
			if err != nil {
				return err
			}
			return renderWithDefault(app, it, output.FormatJSON)
		},
	}
}

func newEvaluationItemsCreateDoc(app *App) *CommandDoc {
	var fromFile string
	return &CommandDoc{
		Use:     "create <evaluation-set-id>",
		Summary: "Add one or more items to an evaluation set (bulk create)",
		Triggers: []string{
			"add items to an evaluation set",
			"bulk create eval set items",
			"register ground-truth pairs for a processor",
		},
		WhenToUse: `Use to add one or more items to an evaluation set in a single request.
This is the only create endpoint; there is no per-item POST.`,
		Details: `The body must match the server's bulk schema:

    {"items":[{"fileId":"file_xxx","expectedOutput":{...}}, ...]}

--from-file accepts inline JSON, a plain path, an absolute file:// URI, or
- for stdin. The response wraps the created items in
{"evaluationSetItems":[...]}; this command surfaces that envelope verbatim.`,
		Examples: []Example{
			{Label: "From file", Cmd: "extend evaluations items create evs_abc --from-file items.json"},
			{Label: "From stdin", Cmd: "cat items.json | extend evaluations items create evs_abc --from-file -"},
		},
		Gotchas: []string{
			"--from-file is required (no inline-flag form for items).",
			"Body must use the bulk shape {items: [...]}; per-item POST is not supported.",
		},
		SeeAlso: []string{"evaluations items list", "evaluations create"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, nil)
			if err != nil {
				return err
			}
			var req extend.EvaluationSetItemsCreateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return fmt.Errorf("decode body: %w", err)
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			resp, err := cli.EvaluationSetItems.Create(cmd.Context(), args[0], &req)
			if err != nil {
				return err
			}
			return renderWithDefault(app, resp, output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON bulk body, path, file:// URI, or '-' for stdin")
			_ = cmd.MarkFlagRequired("from-file")
		},
	}
}

func newEvaluationItemsUpdateDoc(app *App) *CommandDoc {
	var fromFile string
	return &CommandDoc{
		Use:     "update <evaluation-set-id> <item-id>",
		Summary: "Update an evaluation item",
		Triggers: []string{
			"update the expected output of an eval item",
			"patch a ground-truth eval item",
			"change an evaluation set item",
		},
		WhenToUse: `Use to update one item in an evaluation set, typically to change the
expected output as the ground truth evolves.`,
		Details: `--from-file accepts inline JSON, a plain path, an absolute file:// URI,
or - for stdin.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend evaluations items update evs_abc esi_xyz --from-file patch.json"},
		},
		SeeAlso: []string{"evaluations items get", "evaluations items list"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, nil)
			if err != nil {
				return err
			}
			var req extend.EvaluationSetItemsUpdateRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return fmt.Errorf("decode body: %w", err)
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			it, err := cli.EvaluationSetItems.Update(cmd.Context(), args[0], args[1], &req)
			if err != nil {
				return err
			}
			return renderWithDefault(app, it, output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON patch body, path, file:// URI, or '-' for stdin")
			_ = cmd.MarkFlagRequired("from-file")
		},
	}
}

func newEvaluationItemsDeleteDoc(app *App) *CommandDoc {
	var yes bool
	return &CommandDoc{
		Use:     "delete <evaluation-set-id> <item-id>",
		Summary: "Delete an evaluation item",
		Triggers: []string{
			"delete an evaluation item",
			"remove a ground-truth eval item",
			"shrink an evaluation set",
		},
		WhenToUse: `Use to remove one item from an evaluation set. The set is left in
place; only that ground-truth pair is deleted.`,
		Details: `Prompts for confirmation when stdin is a TTY; pass --yes to skip the
prompt (required in non-interactive scripts).`,
		Examples: []Example{
			{Label: "With prompt", Cmd: "extend evaluations items delete evs_abc esi_xyz"},
			{Label: "Skip confirmation", Cmd: "extend evaluations items delete evs_abc esi_xyz --yes"},
		},
		Gotchas: []string{
			"Without --yes in non-TTY contexts, the command refuses to delete.",
		},
		SeeAlso: []string{"evaluations items list", "evaluations items get"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			setID, itemID := args[0], args[1]
			return deleteWithConfirm(cmd.Context(), app, "evaluation item", itemID, yes,
				func(ctx context.Context, _ string) error {
					c, err := app.NewClient()
					if err != nil {
						return err
					}
					_, err = c.EvaluationSetItems.Delete(ctx, setID, itemID, &extend.EvaluationSetItemsDeleteRequest{})
					return err
				})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
		},
	}
}

func newEvaluationRunsDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "runs",
		Summary: "Inspect evaluation runs (read-only)",
		WhenToUse: `Use this group to inspect evaluation runs. Runs are created via the
dashboard, not the CLI; only read-only inspection is exposed.`,
		Details: `Currently only 'get <run-id>' is supported. Listing evaluation runs
is not yet exposed via the external API.`,
		Subcommands: []*CommandDoc{
			newEvaluationRunsGetDoc(app),
		},
	}
}

func newEvaluationRunsGetDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "get <run-id>",
		Summary: "Show one evaluation run",
		Triggers: []string{
			"show one evaluation run",
			"inspect eval set run results",
			"see accuracy metrics for a processor",
			"fetch evaluation run by id",
		},
		WhenToUse: `Use to inspect the per-item results, accuracy metrics, and any diffs
an evaluation run produced. Runs are created via the dashboard.`,
		Details: `The server route is /evaluation_set_runs/{run-id} (no eval-set ID
needed in the path).

Evaluation runs are read-only here; create them via the dashboard. This
command surfaces the per-item results, accuracy metrics, and any diffs
the run produced.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend evaluations runs get esr_abc"},
			{Label: "Just accuracy", Cmd: "extend evaluations runs get esr_abc --jq '.accuracy' -o raw"},
		},
		SeeAlso: []string{"evaluations get", "evaluations items list"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			run, err := cli.EvaluationSetRuns.Retrieve(cmd.Context(), args[0], &extend.EvaluationSetRunsRetrieveRequest{})
			if err != nil {
				return err
			}
			return renderWithDefault(app, run, output.FormatJSON)
		},
	}
}
