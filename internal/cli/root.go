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
	Debug     bool
}

// RootDoc returns the typed documentation tree rooted at the `extend`
// command. The CommandDoc tree is the source of truth for command
// documentation; the cobra command tree (built by NewRoot during the
// migration and progressively replaced in later phases) is one of N
// projections.
//
// As commands migrate from cobra-direct constructors to *CommandDoc
// literals, they are added to Subcommands here. The strict validation
// contract applies to every entry in this tree from day 1.
func RootDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "extend",
		Summary: "CLI for the Extend document AI platform",
		WhenToUse: `Use this CLI when working with the Extend document AI platform: extracting
structured data from PDFs and images, parsing documents to text or
markdown, classifying or splitting multi-document bundles, filling PDF
forms, and running multi-step workflows. The CLI is the agent-native
surface for these operations and integrates with the same Extend
extractors/classifiers/splitters/workflows you configure in the dashboard.`,
		Details: `Authenticate by setting EXTEND_API_KEY in your environment:

    export EXTEND_API_KEY=sk_xxx

Environment variables:
  EXTEND_API_KEY         API key (required)
  EXTEND_BASE_URL        Override base URL (e.g. https://api.extend.ai)
  EXTEND_REGION          Region: us|us2|eu (ignored if EXTEND_BASE_URL is set)
  EXTEND_WORKSPACE_ID    Workspace ID for org-scoped API keys
  EXTEND_API_VERSION     Pin the API version sent with each request
  EXTEND_WEBHOOK_SECRET  Signing secret used by 'extend webhooks verify'

The --workspace and --region flags override their respective env vars.`,
		Subcommands: []*CommandDoc{
			// Actions
			newExtractDoc(app),
			newParseDoc(app),
			newClassifyDoc(app),
			newSplitDoc(app),
			newRunDoc(app),
			newEditDoc(app),
			// Inspection
			newRunsDoc(app),
			newBatchesDoc(app),
			newFilesDoc(app),
			// Resources
			extractorAccessor().doc(app),
			classifierAccessor().doc(app),
			splitterAccessor().doc(app),
			workflowAccessor().doc(app),
			newWebhooksDoc(app),
			newEvaluationsDoc(app),
			// Agent surface
			newSkillDoc(app),
			// Help topics
			newAuthTopicDoc(),
			newOutputTopicDoc(),
			newLifecycleTopicDoc(),
			newErrorsTopicDoc(),
		},
	}
}

// NewRoot builds the production cobra command tree by projecting RootDoc
// via Build(). All command documentation, group structure, and operational
// metadata flow from the typed CommandDoc tree; this function only adds
// the cobra-only behaviour that has no documentation analog: persistent
// flags, version metadata, the NewClient closure on App, the help template,
// and the version subcommand.
func NewRoot() *cobra.Command {
	io := iostreams.System()
	app := &App{IO: io}

	root := RootDoc(app).Build()

	root.Version = versionShort()
	root.SilenceUsage = true
	root.SilenceErrors = true

	// PersistentPreRun fires after Cobra parses flags but before any
	// subcommand's RunE. Used to fall back to env-var defaults when
	// the corresponding flag wasn't passed.
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		applyEnvDefaults(app)
	}

	root.PersistentFlags().StringVarP(&app.Format, "output", "o", "", "Output format: json|yaml|raw|id|table|markdown (or EXTEND_OUTPUT; default: command-specific)")
	root.PersistentFlags().StringVar(&app.JQ, "jq", "", "Filter output with a jq expression")
	root.PersistentFlags().StringVar(&app.Workspace, "workspace", "", "Workspace ID for org-scoped API keys (or EXTEND_WORKSPACE_ID)")
	root.PersistentFlags().StringVar(&app.Region, "region", "", "Region: us|us2|eu (or EXTEND_REGION; ignored if EXTEND_BASE_URL is set)")
	root.PersistentFlags().BoolVar(&app.Debug, "debug", false, "Log every HTTP request to stderr (or EXTEND_DEBUG=1)")

	app.NewClient = func() (*client.Client, error) {
		key := os.Getenv(client.EnvAPIKey)
		if key == "" {
			return nil, fmt.Errorf("%s environment variable is required", client.EnvAPIKey)
		}
		c := client.New(key)

		region := app.Region
		if region == "" {
			region = os.Getenv(client.EnvRegion)
		}
		if region != "" {
			url, ok := client.RegionBaseURL(region)
			if !ok {
				return nil, fmt.Errorf("unknown region %q (known: %v)", region, client.KnownRegions())
			}
			c.BaseURL = url
		}
		if v := os.Getenv(client.EnvBaseURL); v != "" {
			c.BaseURL = v
		}
		if v := os.Getenv(client.EnvAPIVersion); v != "" {
			c.APIVersion = v
		}

		ws := app.Workspace
		if ws == "" {
			ws = os.Getenv(client.EnvWorkspaceID)
		}
		c.WorkspaceID = ws

		// Debug logging: --debug flag wins; otherwise honor EXTEND_DEBUG.
		// Truthy values: "1", "true", "yes", "on" (case-insensitive).
		if app.Debug || debugEnvTruthy(os.Getenv(client.EnvDebug)) {
			c.Debug = app.IO.ErrOut
		}

		return c, nil
	}

	root.AddCommand(newVersionCommand(app))
	installHelpTemplate(root)
	return root
}

// applyEnvDefaults resolves persistent-flag defaults from environment
// variables for the slots Cobra didn't fill at parse time. The flag
// always wins; env fills the gap when the user didn't pass it. Run from
// PersistentPreRun so the order is: parse flags → env-fill → RunE.
//
//	--output    ↔  EXTEND_OUTPUT     (output format: json|yaml|...)
//
// Other flag/env pairings (--workspace ↔ EXTEND_WORKSPACE_ID, --region
// ↔ EXTEND_REGION) are resolved inside NewClient because they only
// apply when an HTTP client is constructed; the output format applies
// to every command, so its fallback lives here.
func applyEnvDefaults(app *App) {
	if app.Format == "" {
		app.Format = os.Getenv(client.EnvOutput)
	}
}

// debugEnvTruthy reports whether EXTEND_DEBUG should enable debug
// logging. Treats "1", "true", "yes", "on" (case-insensitive) as on,
// and the empty string or "0"/"false"/"no"/"off" as off. Anything else
// is treated as on so a user-typed value isn't silently ignored.
func debugEnvTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
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
