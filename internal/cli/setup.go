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

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// newSetupDoc returns the typed documentation for `extend setup`, the
// interactive onboarding wizard. It is intentionally ungrouped (it sits
// under "Additional Commands" alongside version) and elided from the
// agent SKILL.md catalog: a TUI wizard isn't something an agent drives.
func newSetupDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "setup",
		Summary: "Interactive wizard to pick a region and save an API key",
		Triggers: []string{
			"set up the extend cli with an api key",
			"configure extend credentials and region",
			"run the interactive setup wizard",
			"authenticate the cli by pasting an api key",
			"connect the cli to my extend account",
		},
		WhenToUse: `Use for first-time setup or to switch regions/keys. The wizard lets you
pick a region (US or EU), points you to the right dashboard to create an
API key, then validates the key you paste and saves it so every command
works without exporting environment variables.

Prefer setting EXTEND_API_KEY directly in CI, scripts, or any
non-interactive context — the wizard needs a terminal.`,
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
environment variables still win (flag > env > config file > default).`,
		Examples: []Example{
			{Label: "Launch the setup wizard", Cmd: "extend setup"},
		},
		Gotchas: []string{
			"The wizard is interactive and requires a terminal; in CI or scripts set EXTEND_API_KEY (and EXTEND_REGION, plus EXTEND_WORKSPACE_ID for org keys) directly.",
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
		return errors.New("setup is interactive and needs a terminal; set EXTEND_API_KEY (and EXTEND_REGION) directly, or run 'extend setup' in a terminal")
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
		if path, err := installSkillFile(app); err != nil {
			fmt.Fprintf(app.IO.ErrOut, "%s Could not install the agent skill: %v\n", pal.Yellow("!"), err)
		} else {
			fmt.Fprintf(app.IO.ErrOut, "%s Installed the Extend agent skill to %s\n", pal.Green("✓"), path)
			linkSkillAndReport(app, filepath.Dir(path))
		}
	}

	fmt.Fprintf(app.IO.ErrOut, "\nYou're all set. Try: %s\n", pal.Cyan("extend runs list"))
	return nil
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
