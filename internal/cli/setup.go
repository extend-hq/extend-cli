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
guidance when they don't, and installs the agent skill. For fully
unattended use set EXTEND_API_KEY directly.`,
		Details: `Walks through these steps:

  1. Region    — US (api.extend.ai) or EU (api.eu1.extend.ai).
  2. API key   — opens the matching dashboard so you can create a key,
                 then accepts it via a hidden (masked) input.
  3. Validate  — calls the API with the key/region; on success writes
                 the configuration to disk.
  4. Workspace — only if the key is organization-scoped: validation
                 reports that a workspace is required, so the wizard
                 prompts for a workspace ID and re-validates. Most keys
                 are workspace-scoped and skip this step.

The configuration is saved to ~/.config/extend/config.json (honoring
XDG_CONFIG_HOME) with 0600 permissions. It is read as the lowest-priority
source of the API key, region, base URL, and workspace: command flags and
environment variables still win (flag > env > config file > default).

Without a TTY, setup does not prompt. It prints setup guidance to stdout
(the env vars to set and the dashboard URL per region) when no API key
resolves, says nothing about credentials when one already does, and
installs the agent skill (idempotent) unless EXTEND_SKIP_SKILL_INSTALL is
set. It exits 0 in both cases, so an installer can delegate to it safely.`,
		Examples: []Example{
			{Label: "Launch the setup wizard", Cmd: "extend setup"},
			{Label: "Non-interactive (installer/CI), skip the skill", Cmd: "EXTEND_SKIP_SKILL_INSTALL=1 extend setup"},
		},
		Gotchas: []string{
			"Without a terminal the wizard does not run: setup prints setup guidance (or confirms existing credentials), installs the agent skill, and exits 0. Set EXTEND_API_KEY (and EXTEND_REGION, plus EXTEND_WORKSPACE_ID for org keys) for unattended use.",
			"In non-interactive mode the agent skill is installed automatically; set EXTEND_SKIP_SKILL_INSTALL=1 to skip it.",
			"Only the saved key is default-environment-only; with --env <label> set EXTEND_<LABEL>_API_KEY yourself. The saved region, base URL, and workspace still apply under any --env.",
			"Organization-scoped keys require a workspace; the wizard prompts for one and saves it, or set EXTEND_WORKSPACE_ID / --workspace yourself.",
			"The key is stored in plaintext at ~/.config/extend/config.json (0600); delete that file to sign out.",
		},
		SeeAlso: []string{"auth"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd.Context(), app)
		},
	}
}

func runSetup(ctx context.Context, app *App) error {
	if !app.IO.IsStdinTTY() || !app.IO.IsStdoutTTY() {
		return runSetupNonInteractive(app)
	}

	pal := paletteFor(app.IO)

	model := newSetupModel(ctx, app.IO.ColorEnabled(), app.Region, defaultSetupValidator)
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
	if m.result.saveErr != nil {
		return fmt.Errorf("save config: %w", m.result.saveErr)
	}

	fmt.Fprintf(app.IO.ErrOut, "%s API key validated.\n", pal.Green("✓"))
	fmt.Fprintf(app.IO.ErrOut, "%s Saved %s (%s) to %s\n",
		pal.Green("✓"), m.result.region.title, m.result.region.api, m.result.path)
	if m.result.workspaceID != "" {
		fmt.Fprintf(app.IO.ErrOut, "%s Workspace %s\n", pal.Green("✓"), m.result.workspaceID)
	}

	if m.result.installSkill {
		installSkillAndReport(app)
	}

	fmt.Fprintf(app.IO.ErrOut, "\nYou're all set. Try: %s\n", pal.Cyan("extend runs list"))
	return nil
}

// runSetupNonInteractive is the fallback when `extend setup` runs without a
// terminal — an installer, CI, or an agent. It can't prompt, so it does the
// useful subset: confirm on stderr when credentials already resolve, print
// copy-pasteable setup guidance to stdout when they don't, and install the
// agent skill (idempotent) unless EXTEND_SKIP_SKILL_INSTALL is set. It
// always exits 0 — "not configured yet" is guidance, not a failure — so an
// installer that delegates here never fails the install.
func runSetupNonInteractive(app *App) error {
	pal := paletteFor(app.IO)
	s := resolveSettings(app.Env, app.Region, app.Workspace, os.Getenv, config.Load)
	if s.key.val == "" {
		printSetupGuidance(app)
	} else {
		region := s.region.val
		if region == "" {
			region = "us (default)"
		}
		fmt.Fprintf(app.IO.ErrOut, "%s Extend CLI is already configured (region %s, API key from %s).\n",
			pal.Green("✓"), region, s.key.src)
	}

	if envTruthy(os.Getenv(envSkipSkillInstall)) {
		fmt.Fprintf(app.IO.ErrOut, "%s Skipping agent skill install (%s set).\n", pal.Yellow("!"), envSkipSkillInstall)
		return nil
	}
	installSkillAndReport(app)
	return nil
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
