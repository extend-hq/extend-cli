package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/iostreams"
	"github.com/extend-hq/extend-cli/internal/version"
)

func versionShort() string { return version.Short() }

type App struct {
	IO        *iostreams.IOStreams
	NewClient func() (*client.Client, error)
	Format    string
	JQ        string
	Workspace string
	Region    string
}

func NewRoot() *cobra.Command {
	io := iostreams.System()
	app := &App{IO: io}

	root := &cobra.Command{
		Use:   "extend",
		Short: "CLI for the Extend document AI platform",
		Long: `CLI for the Extend document AI platform.

Authenticate by setting EXTEND_API_KEY in your environment:

    export EXTEND_API_KEY=sk_xxx

Environment variables:
  EXTEND_API_KEY         API key (required)
  EXTEND_BASE_URL        Override base URL (e.g. https://api.extend.ai)
  EXTEND_REGION          Region: us|us2 (ignored if EXTEND_BASE_URL is set)
  EXTEND_WORKSPACE_ID    Workspace ID for org-scoped API keys
  EXTEND_API_VERSION     Pin the API version sent with each request
  EXTEND_WEBHOOK_SECRET  Signing secret used by 'extend webhooks verify'

The --workspace and --region flags override their respective env vars.`,
		Version:       versionShort(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddGroup(
		&cobra.Group{ID: "actions", Title: "Actions:"},
		&cobra.Group{ID: "inspection", Title: "Inspection:"},
		&cobra.Group{ID: "resources", Title: "Resources:"},
	)

	root.PersistentFlags().StringVarP(&app.Format, "output", "o", "", "Output format: json|yaml|raw|id|table|markdown (default: command-specific)")
	root.PersistentFlags().StringVar(&app.JQ, "jq", "", "Filter output with a jq expression")
	root.PersistentFlags().StringVar(&app.Workspace, "workspace", "", "Workspace ID for org-scoped API keys (or EXTEND_WORKSPACE_ID)")
	root.PersistentFlags().StringVar(&app.Region, "region", "", "Region: us|us2 (or EXTEND_REGION; ignored if EXTEND_BASE_URL is set)")

	app.NewClient = func() (*client.Client, error) {
		key := os.Getenv("EXTEND_API_KEY")
		if key == "" {
			return nil, errors.New("EXTEND_API_KEY environment variable is required")
		}
		c := client.New(key)

		region := app.Region
		if region == "" {
			region = os.Getenv("EXTEND_REGION")
		}
		if region != "" {
			url, ok := client.RegionBaseURL(region)
			if !ok {
				return nil, fmt.Errorf("unknown region %q (known: %v)", region, client.KnownRegions())
			}
			c.BaseURL = url
		}
		if v := os.Getenv("EXTEND_BASE_URL"); v != "" {
			c.BaseURL = v
		}
		if v := os.Getenv("EXTEND_API_VERSION"); v != "" {
			c.APIVersion = v
		}

		ws := app.Workspace
		if ws == "" {
			ws = os.Getenv("EXTEND_WORKSPACE_ID")
		}
		c.WorkspaceID = ws

		return c, nil
	}

	addInGroup(root, "actions",
		newExtractCommand(app),
		newParseCommand(app),
		newClassifyCommand(app),
		newSplitCommand(app),
		newRunCommand(app),
		newEditCommand(app),
	)
	addInGroup(root, "inspection",
		newRunsCommand(app),
		newBatchesCommand(app),
		newFilesCommand(app),
	)
	addInGroup(root, "resources",
		extractorAccessor().cmd(app),
		classifierAccessor().cmd(app),
		splitterAccessor().cmd(app),
		workflowAccessor().cmd(app),
		newWebhooksCommand(app),
		newEvaluationsCommand(app),
	)
	root.AddCommand(newVersionCommand(app))
	return root
}

func addInGroup(parent *cobra.Command, groupID string, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = groupID
		parent.AddCommand(c)
	}
}

func Execute() int {
	root := NewRoot()
	if err := root.Execute(); err != nil {
		printError(os.Stderr, err)
		return 1
	}
	return 0
}

func printError(w *os.File, err error) {
	pal := palette{enabled: isTerminal(w)}

	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		fmt.Fprintf(w, "%s %s\n", pal.Red("Error:"), apiErr.Code)
		if msg := strings.TrimSpace(apiErr.Message); msg != "" {
			fmt.Fprintf(w, "       %s\n", msg)
		}
		if apiErr.RequestID != "" {
			fmt.Fprintf(w, "       %s\n", pal.Dimf("request: %s", apiErr.RequestID))
		}
		return
	}
	fmt.Fprintf(w, "%s %v\n", pal.Red("Error:"), err)
}

func isTerminal(f *os.File) bool {
	io := iostreams.System()
	if f == os.Stderr {
		return io.IsStderrTTY()
	}
	if f == os.Stdout {
		return io.IsStdoutTTY()
	}
	return false
}
