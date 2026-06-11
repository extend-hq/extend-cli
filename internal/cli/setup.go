package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	tea "charm.land/bubbletea/v2"
	extend "github.com/extend-hq/extend-go-sdk"

	"github.com/extend-hq/extend-cli/internal/config"
	"github.com/extend-hq/extend-cli/internal/extendx"
)

// newSetupDoc returns the typed documentation for `extend setup`, the
// interactive onboarding wizard. It is intentionally ungrouped (it sits
// under "Additional Commands" alongside version) and elided from the
// agent SKILL.md catalog: a TUI wizard isn't something an agent drives.
func newSetupDoc(app *App) *CommandDoc {
	var (
		nonInteractive   bool
		skipSkillInstall bool
		apiKey           string
	)
	return &CommandDoc{
		Use:     "setup",
		Summary: "Set up the CLI (interactive wizard or non-interactive guidance)",
		Triggers: []string{
			"set up the extend cli with an api key",
			"configure extend credentials and region",
			"run the interactive setup wizard",
			"authenticate the cli by pasting an api key",
			"connect the cli to my extend account",
		},
		WhenToUse: `Use for first-time setup or to switch regions/keys. In a terminal it runs
an interactive wizard: pick a region (US or EU), open the right dashboard
to create an API key, then paste it to validate and save so every command
works without exporting environment variables.

Without a terminal — an installer, CI, or an agent — it can't prompt, so
it instead confirms when credentials already resolve, prints setup
guidance when they don't, and installs the agent skill. To configure
unattended, pass --api-key: it validates the key and persists it to the
config file exactly like the wizard, no terminal required.`,
		Details: `A save writes ~/.config/extend/config.json (honoring
XDG_CONFIG_HOME) with 0600 permissions. It is read as the lowest-priority
source of the API key, region, base URL, and workspace: command flags and
environment variables still win (flag > env > config file > default).

With --api-key, setup performs the same validate-and-save without any
prompts (works with or without a terminal):

    extend setup --api-key sk_xxx [--region us|eu] [--workspace ws_xxx]

The region comes from --region / EXTEND_REGION (default us) and the
workspace from --workspace / EXTEND_WORKSPACE_ID. A validation failure
exits non-zero and saves nothing; if the error says a workspace is
required, the key is organization-scoped — re-run with --workspace.

Two knobs control how setup behaves; each is a flag with an environment
variable as a fallback (flag > env > default):

  --non-interactive        / EXTEND_NONINTERACTIVE=1
    Force the non-interactive path even with a terminal attached. CI=1
    has the same effect.

  --skip-skill-install     / EXTEND_SKIP_SKILL_INSTALL=1
    Don't install the agent skill. Applies in both the interactive
    wizard (suppresses the skill prompt) and the non-interactive path.

Without a TTY (or with --non-interactive), setup does not prompt. It
prints setup guidance to stdout (the env vars to set and the dashboard
URL per region) when no API key resolves, says nothing about credentials
when one already does, and installs the agent skill (idempotent) unless
--skip-skill-install is set. It exits 0 in both cases, so an installer
can delegate to it safely.`,
		Examples: []Example{
			{Label: "Launch the setup wizard", Cmd: "extend setup"},
			{Label: "Validate and save a key without prompts", Cmd: "extend setup --api-key sk_xxx"},
			{Label: "Org-scoped key in the EU region", Cmd: "extend setup --api-key sk_xxx --region eu --workspace ws_xxx"},
			{Label: "Quiet setup (no wizard, no skill install)", Cmd: "extend setup --non-interactive --skip-skill-install"},
			{Label: "Non-interactive but install the skill", Cmd: "extend setup --non-interactive"},
			{Label: "Same, via environment", Cmd: "EXTEND_NONINTERACTIVE=1 EXTEND_SKIP_SKILL_INSTALL=1 extend setup"},
		},
		Gotchas: []string{
			"Without a terminal the wizard does not run: setup prints setup guidance (or confirms existing credentials), installs the agent skill, and exits 0. Pass --api-key to actually configure unattended.",
			"--api-key validates before saving and exits non-zero on a bad key, unlike the guidance-only non-interactive path which always exits 0.",
			"--skip-skill-install only suppresses the agent skill step; it does not bypass the interactive wizard. Use --non-interactive to skip the wizard even when a TTY is attached (combine the two for a fully silent setup).",
			"Only the saved key is default-environment-only; with --env <label> set EXTEND_<LABEL>_API_KEY yourself. The saved region, base URL, and workspace still apply under any --env.",
			"Organization-scoped keys require a workspace; the wizard prompts for one and saves it, or set EXTEND_WORKSPACE_ID / --workspace yourself.",
			"A saved key is stored in plaintext at ~/.config/extend/config.json (0600); delete that file to sign out.",
		},
		SeeAlso: []string{"auth"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.NoArgs,
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Skip the interactive wizard even with a TTY attached (or EXTEND_NONINTERACTIVE=1)")
			cmd.Flags().BoolVar(&skipSkillInstall, "skip-skill-install", false, "Don't install the agent skill (or EXTEND_SKIP_SKILL_INSTALL=1)")
			cmd.Flags().StringVar(&apiKey, "api-key", "", "Validate this API key and save it to the config file without prompting (with --region/--workspace as needed)")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := setupOptions{
				nonInteractive:   nonInteractive,
				skipSkillInstall: skipSkillInstall,
				apiKey:           apiKey,
			}
			return runSetup(cmd.Context(), app, opts)
		},
	}
}

// setupOptions carries the resolved values of `extend setup`'s local
// knobs. Each field reflects the flag value; env-var fallbacks are
// applied alongside it (flag > env > default) at the resolution sites
// (resolveSkipSkill / forcedNonInteractive).
type setupOptions struct {
	nonInteractive   bool
	skipSkillInstall bool
	apiKey           string
}

func runSetup(ctx context.Context, app *App, opts setupOptions) error {
	if opts.apiKey != "" {
		return runSetupAPIKey(ctx, app, opts, defaultSetupValidator)
	}
	if forcedNonInteractive(opts.nonInteractive, os.Getenv) || !app.IO.IsStdinTTY() || !app.IO.IsStdoutTTY() {
		return runSetupNonInteractive(app, opts)
	}

	pal := paletteFor(app.IO)

	model := newSetupModel(ctx, app.IO.ColorEnabled(), app.Region, defaultSetupValidator)
	model.skipSkill = resolveSkipSkill(opts.skipSkillInstall, os.Getenv)
	if asciiOnlyTerm(os.Getenv("TERM")) {
		// Console-class terminals can't render the braille logo (or
		// the other decorative glyphs); fall back to plain ASCII.
		model.enableASCII()
	}
	// v2 moved alt-screen and mouse-mode out of program options and into
	// View() fields; they're set per-frame in setupModel.View().
	final, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	if err != nil {
		// A signal (SIGTERM) or interrupt during the TUI is a user
		// action, not a failure to debug.
		if ctx.Err() != nil || errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, tea.ErrInterrupted) {
			fmt.Fprintln(app.IO.ErrOut, pal.Yellow("Setup cancelled."))
			return nil
		}
		return err
	}

	m, ok := final.(setupModel)
	if !ok {
		return nil
	}
	if m.canceled {
		fmt.Fprintln(app.IO.ErrOut, pal.Yellow("Setup cancelled."))
		return nil
	}
	if m.result == nil {
		return nil
	}
	return reportSetupResult(app, m.result)
}

// reportSetupResult prints the post-wizard summary to stderr. A saved
// result reports what was written where; a declined save prints the
// env-var alternative the user chose, without echoing the key itself
// (it stays wherever they copied it from). Region is included only when
// it differs from the default (us), workspace only when one was needed.
func reportSetupResult(app *App, res *setupResult) error {
	pal := paletteFor(app.IO)
	if res.saveErr != nil {
		return fmt.Errorf("save config: %w", res.saveErr)
	}

	// A blank-key skip never entered a key, so there is nothing to
	// report as validated.
	if res.apiKey != "" {
		fmt.Fprintf(app.IO.ErrOut, "%s API key validated.\n", pal.Green("✓"))
	}
	if res.saved {
		fmt.Fprintf(app.IO.ErrOut, "%s Saved %s (%s) to %s\n",
			pal.Green("✓"), res.region.title, res.region.api, res.path)
		if res.workspaceID != "" {
			fmt.Fprintf(app.IO.ErrOut, "%s Workspace %s\n", pal.Green("✓"), res.workspaceID)
		}
	} else {
		fmt.Fprintf(app.IO.ErrOut, "%s Nothing was saved. Set the key in your shell to use the CLI:\n\n", pal.Yellow("!"))
		fmt.Fprintf(app.IO.ErrOut, "    export %s=<your key>\n", envAPIKey)
		if res.region.id != "" && res.region.id != "us" {
			fmt.Fprintf(app.IO.ErrOut, "    export %s=%s\n", envRegion, res.region.id)
		}
		if res.workspaceID != "" {
			fmt.Fprintf(app.IO.ErrOut, "    export %s=%s\n", envWorkspaceID, res.workspaceID)
		}
		fmt.Fprintf(app.IO.ErrOut, "\nRun 'extend setup' again anytime to save it instead.\n")
	}

	if res.installSkill {
		installSkillAndReport(app)
	}

	if res.saved {
		fmt.Fprintf(app.IO.ErrOut, "\nYou're all set. Try: %s\n", pal.Cyan("extend files list"))
	} else {
		fmt.Fprintf(app.IO.ErrOut, "\nAfter exporting, try: %s\n", pal.Cyan("extend files list"))
	}
	return nil
}

// runSetupAPIKey is the prompt-free configuration path (`extend setup
// --api-key`): validate the key against the resolved region/workspace and
// persist it to the config file, exactly like the wizard's save. Built for
// agents and scripts — unlike the guidance-only non-interactive path, a
// bad key here is a real failure and exits non-zero.
func runSetupAPIKey(ctx context.Context, app *App, opts setupOptions, validate setupValidator) error {
	pal := paletteFor(app.IO)

	region := app.Region
	if region == "" {
		region = os.Getenv(envRegion)
	}
	if region == "" {
		region = "us"
	}
	if _, ok := extendx.RegionBaseURL(region); !ok {
		return fmt.Errorf("unknown region %q (known: %s)", region, strings.Join(extendx.KnownRegions(), ", "))
	}
	workspace := app.Workspace
	if workspace == "" {
		workspace = os.Getenv(envWorkspaceID)
	}

	if err := validate(ctx, region, opts.apiKey, workspace); err != nil {
		if needsWorkspacePrompt(err) {
			return fmt.Errorf("this API key is organization-scoped; re-run with --workspace ws_xxx (or set %s): %w", envWorkspaceID, err)
		}
		return fmt.Errorf("API key validation failed for region %s: %w", region, err)
	}

	file := config.File{
		Region: region,
		Auth:   &config.Auth{Type: config.AuthAPIKey, APIKey: opts.apiKey},
	}
	if workspace != "" {
		file.WorkspaceID = workspace
	}
	path, err := config.Save(file)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(app.IO.ErrOut, "%s API key validated.\n", pal.Green("✓"))
	fmt.Fprintf(app.IO.ErrOut, "%s Saved region %s to %s\n", pal.Green("✓"), region, path)
	if workspace != "" {
		fmt.Fprintf(app.IO.ErrOut, "%s Workspace %s\n", pal.Green("✓"), workspace)
	}

	if resolveSkipSkill(opts.skipSkillInstall, os.Getenv) {
		fmt.Fprintf(app.IO.ErrOut, "%s Skipping agent skill install.\n", pal.Yellow("!"))
		return nil
	}
	installSkillAndReport(app)
	return nil
}

// runSetupNonInteractive is the fallback when `extend setup` runs without a
// terminal — an installer, CI, or an agent. It can't prompt, so it does the
// useful subset: confirm on stderr when credentials already resolve, print
// copy-pasteable setup guidance to stdout when they don't, and install the
// agent skill (idempotent) unless --skip-skill-install /
// EXTEND_SKIP_SKILL_INSTALL is set. It always exits 0 — "not configured
// yet" is guidance, not a failure — so an installer that delegates here
// never fails the install.
func runSetupNonInteractive(app *App, opts setupOptions) error {
	pal := paletteFor(app.IO)
	s := resolveSettings(app.Env, app.Region, app.Workspace, os.Getenv, config.Load)
	if s.key.val == "" {
		printSetupGuidance(app)
	} else {
		// A custom base URL overrides the region, so report whichever
		// actually determines where requests go.
		where := "region " + s.region.val
		if s.baseURL.val != "" {
			where = "base URL " + s.baseURL.val
		} else if s.region.val == "" {
			where = "region us (default)"
		}
		fmt.Fprintf(app.IO.ErrOut, "%s Extend CLI is already configured (%s, API key from %s).\n",
			pal.Green("✓"), where, s.key.src)
	}

	if resolveSkipSkill(opts.skipSkillInstall, os.Getenv) {
		fmt.Fprintf(app.IO.ErrOut, "%s Skipping agent skill install.\n", pal.Yellow("!"))
		return nil
	}
	installSkillAndReport(app)
	return nil
}

// forcedNonInteractive reports whether setup should bypass the TUI wizard
// even when stdin/stdout are TTYs. Precedence: --non-interactive flag >
// EXTEND_NONINTERACTIVE > CI > false. CI is consulted so an unattended pty
// (docker -t | sh, some CI runners) can't accidentally land in the wizard.
func forcedNonInteractive(flag bool, getenv func(string) string) bool {
	if flag {
		return true
	}
	return envTruthy(getenv(envNonInteractive)) || envTruthy(getenv("CI"))
}

// resolveSkipSkill reports whether the agent skill install step should be
// suppressed. Precedence: --skip-skill-install flag >
// EXTEND_SKIP_SKILL_INSTALL > false.
func resolveSkipSkill(flag bool, getenv func(string) string) bool {
	if flag {
		return true
	}
	return envTruthy(getenv(envSkipSkillInstall))
}

// printSetupGuidance writes copy-pasteable configuration instructions to
// stdout — the relayable channel an agent or installer captures. Only the
// instructions go to stdout; status/skill chatter goes to stderr.
func printSetupGuidance(app *App) {
	out := app.IO.Out
	fmt.Fprintln(out, "The Extend CLI is installed but not configured.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Set an API key to get started:")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "    export %s=sk_...\n", envAPIKey)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Create a key in the dashboard for your region:")
	for _, r := range extendx.AdvertisedRegions() {
		fmt.Fprintf(out, "    - %-15s %s\n", r.Title, r.Dashboard)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "For an organization-scoped key, also set %s.\n", envWorkspaceID)
	fmt.Fprintf(out, "To use the EU region, also set %s=eu.\n", envRegion)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Or run the interactive wizard in a terminal:  extend setup")
	fmt.Fprintln(out, "Check what's resolved at any time with:       extend config")
}

// installSkillAndReport writes the agent skill to its default location,
// symlinks it into ~/.claude/skills, and reports each step to stderr.
// Best-effort: a write failure is a warning, not an error. Shared by the
// wizard and the non-interactive path.
func installSkillAndReport(app *App) {
	pal := paletteFor(app.IO)
	path, err := installSkillFile(app)
	if err != nil {
		fmt.Fprintf(app.IO.ErrOut, "%s Could not install the agent skill: %v\n", pal.Yellow("!"), err)
		return
	}
	fmt.Fprintf(app.IO.ErrOut, "%s Installed the Extend agent skill to %s\n", pal.Green("✓"), path)
	linkSkillAndReport(app, filepath.Dir(path))
}

// installSkillFile renders the SKILL.md and writes it to the default
// cross-client agent skills location, returning the path written.
func installSkillFile(app *App) (string, error) {
	path, err := defaultSkillTarget()
	if err != nil {
		return "", err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create directory: %w", err)
		}
	}
	body := RenderSkill(RootDoc(app))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// defaultSetupValidator is the production key checker: it builds a client
// for the chosen region and makes one cheap authenticated call.
func defaultSetupValidator(ctx context.Context, region, key, workspaceID string) error {
	cfg := extendx.Config{
		APIKey:      key,
		Region:      region,
		WorkspaceID: workspaceID,
		UserAgent:   userAgent(),
		HTTPTimeout: 30 * time.Second,
	}
	// EXTEND_BASE_URL overrides the region for every real command, so
	// validate against it too — otherwise the wizard could report success
	// against the region's URL while commands actually hit a different one.
	if v := os.Getenv(envBaseURL); v != "" {
		cfg.BaseURL = v
	}
	return validateAPIKey(ctx, cfg)
}

// validateAPIKey confirms a key works by listing workflows (the lightest
// authenticated endpoint — every field of the request is optional). Any
// 2xx means the key and region are good; a 401/403 surfaces as a typed
// API error the wizard explains.
func validateAPIKey(ctx context.Context, cfg extendx.Config) error {
	client, err := extendx.NewClient(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = client.Workflows.List(ctx, &extend.WorkflowsListRequest{})
	return err
}

// apiErrorStatus extracts the HTTP status from an API error, if any. Used
// by the wizard to tailor its failure message.
func apiErrorStatus(err error) (int, bool) {
	if apiErr, ok := extendx.AsAPIError(err); ok {
		return apiErr.StatusCode, true
	}
	return 0, false
}

// needsWorkspacePrompt reports whether err is the server's signal that an
// organization-scoped key requires a workspace (a 400 whose message names
// the workspace header). The wizard reacts by collecting a workspace ID
// rather than dead-ending on the key step.
func needsWorkspacePrompt(err error) bool {
	apiErr, ok := extendx.AsAPIError(err)
	if !ok || apiErr.StatusCode != 400 {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.Message), "workspace")
}
