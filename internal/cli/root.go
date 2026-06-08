package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/config"
	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/iostreams"
	"github.com/extend-hq/extend-cli/internal/version"
)

func versionShort() string { return version.Short() }

// userAgent renders the CLI's User-Agent header value, e.g.
// "extend-cli/1.2.3". Recomputed per call so tests that override
// version.Short() see fresh output.
func userAgent() string {
	return "extend-cli/" + version.Short()
}

type App struct {
	IO        *iostreams.IOStreams
	NewClient func() (*sdkclient.Client, error)
	Format    string
	JQ        string
	Workspace string
	Region    string
	Env       string // optional environment label (e.g. "test"); selects EXTEND_<UPPER>_API_KEY
	Debug     bool
	// HTTPTimeout caps each individual HTTP request (POST/GET).
	// Distinct from per-command --timeout (the overall wait budget
	// for a run to reach a terminal state). Zero leaves the SDK's
	// default timeout in place.
	HTTPTimeout time.Duration
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

Or run 'extend setup' for an interactive wizard that selects your region,
points you to the dashboard to create a key, validates it, and saves it to
~/.config/extend/config.json (read as a fallback when EXTEND_API_KEY is unset).

Environment variables:
  EXTEND_API_KEY         API key (required)
  EXTEND_BASE_URL        Override base URL (e.g. https://api.extend.ai)
  EXTEND_REGION          Region: us|eu (ignored if EXTEND_BASE_URL is set)
  EXTEND_WORKSPACE_ID    Workspace ID for org-scoped API keys
  EXTEND_API_VERSION     Pin the API version sent with each request
  EXTEND_HTTP_TIMEOUT    Per-HTTP-request timeout, e.g. 60s. Distinct
                         from per-command --timeout (overall wait).
  EXTEND_WEBHOOK_SECRET  Signing secret used by 'extend webhooks verify'
  EXTEND_ENV             Environment label that selects an alternate API key
                         (e.g. --env test reads EXTEND_TEST_API_KEY)

The --workspace, --region, and --http-timeout flags override their
respective env vars.`,
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
			newDownloadDoc(app),
			// Resources
			extractorAccessor().doc(app),
			classifierAccessor().doc(app),
			splitterAccessor().doc(app),
			workflowAccessor().doc(app),
			newWebhooksDoc(app),
			newEvaluationsDoc(app),
			// Agent surface
			newSkillDoc(app),
			// Onboarding (ungrouped: shows under "Additional Commands")
			newSetupDoc(app),
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
	root.PersistentFlags().StringVar(&app.Region, "region", "", "Region: us|eu (or EXTEND_REGION; ignored if EXTEND_BASE_URL is set)")
	root.PersistentFlags().BoolVar(&app.Debug, "debug", false, "Log every HTTP request to stderr (or EXTEND_DEBUG=1)")
	root.PersistentFlags().StringVar(&app.Env, "env", "", "Environment label that selects the API key: e.g. --env test reads EXTEND_TEST_API_KEY instead of EXTEND_API_KEY (or EXTEND_ENV)")
	root.PersistentFlags().DurationVar(&app.HTTPTimeout, "http-timeout", 0, "Per-HTTP-request timeout, e.g. 60s or 2m (or EXTEND_HTTP_TIMEOUT). Distinct from per-command --timeout (overall wait). Defaults to 60s; 0 leaves the client default in place; uploads bypass this and use an untimed client.")

	app.NewClient = func() (*sdkclient.Client, error) {
		rc := resolveCredentials(app.Env, app.Region, os.Getenv, config.Load)
		if rc.key == "" {
			return nil, fmt.Errorf("%s environment variable is required (or run 'extend setup')", apiKeyEnvVar(app.Env))
		}

		cfg := extendx.Config{
			APIKey:      rc.key,
			Region:      rc.region,
			WorkspaceID: app.Workspace,
			UserAgent:   userAgent(),
		}
		if cfg.WorkspaceID == "" {
			cfg.WorkspaceID = os.Getenv(envWorkspaceID)
		}
		// Base-URL precedence (EXTEND_BASE_URL > config file) is resolved
		// in resolveCredentials; a non-empty value overrides the region.
		if rc.baseURL != "" {
			cfg.BaseURL = rc.baseURL
		}
		if v := os.Getenv(envAPIVersion); v != "" {
			cfg.APIVersion = v
		}

		// Debug logging: --debug flag wins; otherwise honor EXTEND_DEBUG.
		// Truthy values: "1", "true", "yes", "on" (case-insensitive).
		if app.Debug || debugEnvTruthy(os.Getenv(envDebug)) {
			cfg.Debug = app.IO.ErrOut
		}

		// Per-HTTP-request timeout: --http-timeout flag wins; otherwise
		// honor EXTEND_HTTP_TIMEOUT. Both override the SDK default.
		if d, ok := resolveHTTPTimeout(app.HTTPTimeout, os.Getenv(envHTTPTimeout)); ok {
			// A flag value of 0 is impossible to express to extendx
			// (0 means "leave default"). We use -1 as a sentinel for
			// "explicitly disable timeout" so the underlying http.Client
			// timeout becomes zero (no end-to-end deadline).
			if d == 0 {
				cfg.HTTPTimeout = -1
			} else {
				cfg.HTTPTimeout = d
			}
		}

		return extendx.NewClient(cfg)
	}

	root.AddCommand(newVersionCommand(app))
	installHelpTemplate(root)
	return root
}

// resolveHTTPTimeout picks the effective per-request timeout to install
// on the HTTP client. Precedence:
//
//  1. --http-timeout flag (any positive value wins, including 0 if the
//     caller explicitly set it via SetHTTPTimeout? — no: flag default is
//     0 == "unset", so we treat positive as "user asked for this").
//  2. EXTEND_HTTP_TIMEOUT env (parseable as time.Duration; "60s",
//     "2m", etc.). Malformed values are silently ignored — a typo
//     shouldn't break every command. Surface it via --debug.
//  3. Neither set → return ok=false; caller leaves the SDK default in
//     place.
//
// A returned timeout of 0 is meaningful: it disables the http.Client
// timeout entirely, leaving the context as the only deadline. Useful
// in tests and for users debugging slow networks.
func resolveHTTPTimeout(flag time.Duration, env string) (time.Duration, bool) {
	if flag > 0 {
		return flag, true
	}
	if env != "" {
		if d, err := time.ParseDuration(env); err == nil && d >= 0 {
			return d, true
		}
	}
	return 0, false
}

// applyEnvDefaults resolves persistent-flag defaults from environment
// variables for the slots Cobra didn't fill at parse time. The flag
// always wins; env fills the gap when the user didn't pass it. Run from
// PersistentPreRun so the order is: parse flags → env-fill → RunE.
//
//	--output    ↔  EXTEND_OUTPUT     (output format: json|yaml|...)
//	--env       ↔  EXTEND_ENV        (environment label for API-key selection)
//
// Other flag/env pairings (--workspace ↔ EXTEND_WORKSPACE_ID, --region
// ↔ EXTEND_REGION) are resolved inside NewClient because they only
// apply when an HTTP client is constructed; the output format and env
// label apply to every command, so their fallbacks live here.
func applyEnvDefaults(app *App) {
	if app.Format == "" {
		app.Format = os.Getenv(envOutput)
	}
	if app.Env == "" {
		app.Env = os.Getenv(envEnv)
	}
}

// resolved holds the credential and routing values after the precedence
// chain has been applied. baseURL is empty when no override is set, in
// which case region selects the URL inside extendx.
type resolved struct {
	key     string
	region  string
	baseURL string
}

// resolveCredentials resolves the API key and routing settings using the
// precedence: flag/env > config file > default. Only the API *key* is
// environment-specific: an --env label selects an alternate key that must
// come from the environment (EXTEND_<LABEL>_API_KEY), never from the
// shared file, since the file holds only the default environment's key.
// Routing settings (region, base URL) are not env-label-specific, so the
// file's values still apply under any --env. Base-URL precedence is
// EXTEND_BASE_URL > file.baseUrl > region (extendx turns region into a
// URL, and a non-empty base URL overrides it). A malformed or unreadable
// file is ignored best-effort so a typo can't break every command. getenv
// and loadFile are injected for testability.
func resolveCredentials(envLabel, regionFlag string, getenv func(string) string, loadFile func() (config.File, error)) resolved {
	var fileCfg config.File
	if loadFile != nil {
		fileCfg, _ = loadFile()
	}

	var r resolved
	r.key = getenv(apiKeyEnvVar(envLabel))
	if r.key == "" && envLabel == "" {
		r.key = fileCfg.APIKey()
	}

	r.region = regionFlag
	if r.region == "" {
		r.region = getenv(envRegion)
	}
	if r.region == "" {
		r.region = fileCfg.Region
	}

	r.baseURL = getenv(envBaseURL)
	if r.baseURL == "" {
		r.baseURL = fileCfg.BaseURL
	}
	return r
}

// apiKeyEnvVar returns the env var name from which the API key should
// be read for the given environment label. Empty label keeps the
// historical EXTEND_API_KEY behavior; "test" maps to EXTEND_TEST_API_KEY,
// "staging" to EXTEND_STAGING_API_KEY, etc. The label is uppercased and
// non-alphanumeric characters are stripped so a stray "Test" or "test-1"
// still resolves to a stable variable name.
func apiKeyEnvVar(envLabel string) string {
	envLabel = strings.TrimSpace(envLabel)
	if envLabel == "" {
		return envAPIKey
	}
	upper := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 32
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_':
			return r
		}
		return -1
	}, envLabel)
	if upper == "" {
		return envAPIKey
	}
	return "EXTEND_" + upper + "_API_KEY"
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
	// Wire Ctrl-C / SIGTERM to context cancellation so in-flight API
	// calls (every command threads cmd.Context() into the SDK) abort
	// promptly instead of the process being hard-killed mid-request.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := NewRoot()
	if err := root.ExecuteContext(ctx); err != nil {
		// A signal-cancelled run is a user action, not a failure to
		// debug. The signal context has no deadline, so a non-nil
		// ctx.Err() here means a signal fired: report it quietly and
		// exit 130 (128+SIGINT), the code shells expect for Ctrl-C.
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return 130
		}
		printError(os.Stderr, err)
		return 1
	}
	return 0
}

func printError(w *os.File, err error) {
	formatError(w, palette{enabled: isTerminal(w)}, err)
}

// formatError renders err to w. Split from printError so it can be
// tested against a buffer with a fixed (no-color) palette; printError
// just supplies the terminal-derived palette for the real os.File.
func formatError(w io.Writer, pal palette, err error) {
	if apiErr, ok := extendx.AsAPIError(err); ok {
		// Prefer the server-side error code when present; fall back to
		// the HTTP status string otherwise.
		head := apiErr.Code
		if head == "" {
			head = fmt.Sprintf("HTTP %d", apiErr.StatusCode)
		}
		fmt.Fprintf(w, "%s %s\n", pal.Red("Error:"), head)
		if msg := strings.TrimSpace(apiErr.Message); msg != "" {
			fmt.Fprintf(w, "       %s\n", msg)
		}
		if apiErr.RequestID != "" {
			fmt.Fprintf(w, "       %s\n", pal.Dimf("request: %s", apiErr.RequestID))
		}
		return
	}

	// Transport-level failures (DNS, connection refused, TLS, or an
	// http.Client timeout from --http-timeout) surface as *url.Error.
	// The raw dump is noisy and looks like a crash; classify it and put
	// the underlying detail on a dim second line.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			fmt.Fprintf(w, "%s request timed out\n", pal.Red("Error:"))
			fmt.Fprintf(w, "       %s\n", pal.Dim("raise --http-timeout or EXTEND_HTTP_TIMEOUT; for long runs use --wait=false and poll with 'extend runs watch'"))
		} else {
			fmt.Fprintf(w, "%s could not reach the Extend API\n", pal.Red("Error:"))
			fmt.Fprintf(w, "       %s\n", pal.Dim("check connectivity, --region/--base-url, and that the API is reachable"))
		}
		fmt.Fprintf(w, "       %s\n", pal.Dimf("detail: %v", urlErr))
		return
	}

	// A bare deadline not wrapped by the HTTP client (rare). Classify it
	// rather than dumping "context deadline exceeded".
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(w, "%s request timed out (raise --http-timeout / EXTEND_HTTP_TIMEOUT)\n", pal.Red("Error:"))
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
