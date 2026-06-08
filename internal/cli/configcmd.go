package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/config"
	"github.com/extend-hq/extend-cli/internal/extendx"
)

// newConfigDoc returns the typed documentation for `extend config`, which
// reports the effective configuration and where each value was resolved
// from. It is intentionally ungrouped (sits beside `setup` under
// "Additional Commands") and reads-only — `extend setup` is the writer.
func newConfigDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "config",
		Summary: "Show the effective configuration and where each value came from",
		Triggers: []string{
			"show the resolved extend cli configuration",
			"check which api key region or workspace is in effect",
			"see where my extend credentials are loaded from",
			"debug why the cli uses the wrong key or region",
		},
		WhenToUse: `Use to see the configuration the CLI will actually use and the source of
each value (command flag, environment variable, config file, or default).
Helpful when a command authenticates as the wrong account or talks to the
wrong region: it reveals when an environment variable is shadowing what
'extend setup' saved. This command only reports configuration; run
'extend setup' to create or change it.`,
		Details: `Prints the effective auth method, API key (masked), region, base URL,
and workspace, each annotated with where it was resolved from. Precedence
is flag > environment variable > config file > default, so a value shown
as coming from an environment variable overrides what 'extend setup'
saved to the config file.

The output is plain text meant for humans; it is deliberately not
machine-readable and ignores -o/--output. The API key is always masked —
there is no flag to reveal it in full.`,
		Examples: []Example{
			{Label: "Show effective configuration", Cmd: "extend config"},
			{Label: "Inspect a labeled environment", Cmd: "extend config --env staging"},
		},
		Gotchas: []string{
			"This reads configuration only; it never writes. Use 'extend setup' to change saved values.",
			"A value sourced from an environment variable overrides the config file even after 'extend setup'; this command shows when that happens.",
			"The API key is always masked and -o/--output is ignored — the output is for humans, not scripts.",
		},
		SeeAlso: []string{"setup", "auth"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputPretty},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfig(app)
		},
	}
}

func runConfig(app *App) error {
	rc := resolveCredentials(app.Env, app.Region, app.Workspace, os.Getenv, config.Load)
	pal := paletteFor(app.IO)

	// Only API keys exist today; this line is where OAuth status surfaces.
	authMethod := "(none)"
	if rc.key.val != "" {
		authMethod = "API key"
	}

	// An unset region/baseURL means extendx falls back to its default (US
	// production); show that rather than a blank.
	region, regionSrc := rc.region.val, rc.region.src
	if region == "" {
		region, regionSrc = "us", "default"
	}
	baseURL, baseSrc := rc.baseURL.val, rc.baseURL.src
	if baseURL == "" {
		if u, ok := extendx.RegionBaseURL(region); ok {
			baseURL = u
		}
		baseSrc = "derived from region"
	}

	type row struct{ label, val, note string }
	rows := []row{
		{"Auth method", authMethod, ""},
		{"API key", maskKey(rc.key.val), rc.key.src},
		{"Region", region, regionSrc},
		{"Base URL", baseURL, baseSrc},
		{"Workspace", orNotSet(rc.workspaceID.val), rc.workspaceID.src},
	}

	if path, err := config.Path(); err == nil {
		note := "not created yet"
		if _, statErr := os.Stat(path); statErr == nil {
			note = "loaded"
		}
		rows = append(rows, row{"Config file", path, note})
	}

	width := 0
	for _, r := range rows {
		if len(r.label) > width {
			width = len(r.label)
		}
	}
	for _, r := range rows {
		line := fmt.Sprintf("%-*s  %s", width, r.label, r.val)
		if r.note != "" {
			line += "  " + pal.Dim("("+r.note+")")
		}
		fmt.Fprintln(app.IO.Out, line)
	}
	return nil
}

// maskKey renders an API key for display: first and last few characters
// only, never the whole secret. Empty keys read as not set.
func maskKey(k string) string {
	if k == "" {
		return "(not set)"
	}
	if len(k) <= 8 {
		return strings.Repeat("•", len(k))
	}
	return k[:4] + "…" + k[len(k)-4:]
}

func orNotSet(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}
