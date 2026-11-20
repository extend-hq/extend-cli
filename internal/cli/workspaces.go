package cli

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	"github.com/extend-hq/extend-go-sdk/option"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

const maxWorkspaceNameRunes = 255

func newWorkspacesDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "workspaces",
		Summary: "List, inspect, and create workspaces in your organization",
		Group:   "Resources",
		WhenToUse: `Use to list, inspect, or create workspaces in an organization. Only
available to organization-scoped API keys on the Enterprise plan once
Extend enables workspace management for your organization.`,
		Details: `Workspace management requires an organization-scoped API key on an
Enterprise organization with workspace management enabled (contact
support@extend.ai to enable it). Workspace-scoped API keys and browser
logins ('extend login') receive a 400 error.

Every request — including 'list' and 'create' — still requires --workspace
(or EXTEND_WORKSPACE_ID) naming an existing workspace in the organization.
Use 'extend workspaces list' to discover other workspaces once you have
one; it cannot find your first workspace ID.`,
		SeeAlso: []string{"whoami", "config", "auth"},
		Subcommands: []*CommandDoc{
			newWorkspacesListDoc(app),
			newWorkspacesGetDoc(app),
			newWorkspacesCreateDoc(app),
		},
	}
}

func newWorkspacesListDoc(app *App) *CommandDoc {
	var (
		sortDir   string
		limit     int
		maxN      int
		all       bool
		pageToken string
	)
	return &CommandDoc{
		Use:     "list",
		Summary: "List workspaces in your organization",
		Triggers: []string{
			"list extend workspaces in the organization",
			"find workspace ids for an org-scoped api key",
			"page through organization workspaces",
		},
		WhenToUse: `Use with an organization-scoped API key to enumerate every workspace in
your organization and discover workspace IDs to pass to --workspace.`,
		Details: `Requires an organization-scoped API key on an Enterprise organization with
workspace management enabled, plus --workspace (or EXTEND_WORKSPACE_ID)
naming any existing workspace in the organization.

` + paginationGuidance,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend workspaces list --workspace ws_abc"},
			{Label: "Oldest first, bounded", Cmd: "extend workspaces list --workspace ws_abc --sort asc --max 100"},
			{Label: "Just workspace IDs", Cmd: "extend workspaces list --workspace ws_abc -o id"},
		},
		Gotchas: []string{
			"Requires an organization-scoped API key on an Enterprise plan with workspace management enabled; workspace-scoped keys and 'extend login' get a 400.",
			"Even listing still requires --workspace (or EXTEND_WORKSPACE_ID) naming an existing workspace in the organization.",
			"Page tokens are bound to the originating query; repeat --sort on every paginated call.",
		},
		SeeAlso: []string{"workspaces get", "workspaces create", "whoami"},
		Output:  OutputSpec{TTY: OutputTable, Pipe: OutputTable},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			req := &extend.WorkspacesListRequest{}
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
				page, err := cli.Workspaces.List(cmd.Context(), req)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				for _, ws := range page.Data {
					rows = append(rows, []string{ws.ID, ws.Name, timeCell(ws.CreatedAt), timeCell(ws.UpdatedAt)})
				}
				next := deref(page.NextPageToken)
				if paginationDone(all, maxN, len(rows), next) {
					break
				}
				req.NextPageToken = extend.String(next)
			}
			rows = capRowsToMax(rows, maxN)
			return renderListForCmd(cmd, app, pages, []string{"id", "name", "created", "updated"}, rows, "No workspaces.")
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
			cmd.Flags().IntVar(&limit, "limit", 0, "Page size used in each API request (0 = server default)")
			cmd.Flags().IntVar(&maxN, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page)")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&all, "all", false, "Fetch every page (use --max for a bounded fetch)")
		},
	}
}

func newWorkspacesGetDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "get <workspace-id>",
		Summary: "Show one workspace by ID",
		Triggers: []string{
			"show one extend workspace by id",
			"inspect workspace details and timestamps",
			"fetch an organization workspace record",
		},
		WhenToUse: `Use to retrieve one workspace's metadata (ID, name, createdAt, updatedAt)
within your organization.`,
		Details: `Requires an organization-scoped API key on an Enterprise organization with
workspace management enabled, plus --workspace (or EXTEND_WORKSPACE_ID)
naming any existing workspace in the organization.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend workspaces get ws_xyz --workspace ws_abc"},
			{Label: "Just the name", Cmd: "extend workspaces get ws_xyz --workspace ws_abc --jq '.name' -o raw"},
		},
		Gotchas: []string{
			"Requires an organization-scoped API key on an Enterprise plan with workspace management enabled.",
			"Requires --workspace (or EXTEND_WORKSPACE_ID) naming an existing workspace in the organization, even when fetching a different workspace by ID.",
		},
		SeeAlso: []string{"workspaces list", "workspaces create", "whoami"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			ws, err := cli.Workspaces.Retrieve(cmd.Context(), args[0], &extend.WorkspacesRetrieveRequest{})
			if err != nil {
				return err
			}
			return renderWithDefault(app, apiJSON(ws), output.FormatJSON)
		},
	}
}

func newWorkspacesCreateDoc(app *App) *CommandDoc {
	var name string
	return &CommandDoc{
		Use:     "create",
		Summary: "Create a new workspace in your organization",
		Triggers: []string{
			"create a new extend workspace",
			"provision a workspace in an organization",
			"add an organization workspace with the cli",
		},
		WhenToUse: `Use to provision a new workspace in your organization (for example, one
per tenant or team) using an organization-scoped API key.`,
		Details: `Requires --name (1 to 255 characters after trimming whitespace), an
organization-scoped API key on an Enterprise organization with workspace
management enabled, and --workspace (or EXTEND_WORKSPACE_ID) naming an
existing workspace in the organization.

The request is not retried on 5xx responses, so a transient error cannot
create duplicate workspaces.`,
		Examples: []Example{
			{Label: "Basic", Cmd: `extend workspaces create --name "Acme EU" --workspace ws_abc`},
			{Label: "Capture the new ID", Cmd: `extend workspaces create --name "Acme EU" --workspace ws_abc -o id`},
		},
		Gotchas: []string{
			"Requires an organization-scoped API key on an Enterprise plan with workspace management enabled; workspace-scoped keys and 'extend login' get a 400.",
			"Still requires --workspace (or EXTEND_WORKSPACE_ID) naming an existing workspace in the organization.",
			"--name is required and must be 1 to 255 characters.",
		},
		SeeAlso: []string{"workspaces list", "workspaces get", "whoami"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				return errors.New("--name is required")
			}
			if n := utf8.RuneCountInString(trimmed); n > maxWorkspaceNameRunes {
				return fmt.Errorf("--name is %d characters; maximum is %d", n, maxWorkspaceNameRunes)
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			ws, err := extendx.StartOnce(cmd.Context(), func(opts ...option.RequestOption) (*extend.Workspace, error) {
				return cli.Workspaces.Create(cmd.Context(), &extend.WorkspacesCreateRequest{Name: trimmed}, opts...)
			})
			if err != nil {
				return err
			}
			return renderWithDefault(app, apiJSON(ws), output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&name, "name", "", "Display name for the new workspace (1 to 255 characters, required)")
			_ = cmd.MarkFlagRequired("name")
		},
	}
}
